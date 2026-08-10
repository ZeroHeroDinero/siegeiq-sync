// clipsend.go - sending a clip WITHOUT freezing the window.
//
// # THE BUG THIS EXISTS FOR
//
// apiSendClip did the whole job inline: shrink the file, then upload it, then
// return. Every bridge binding runs on the window's message-loop thread, so
// "then return" means the window cannot repaint, cannot answer a click, and
// cannot service its own status poll until the work is finished.
//
// With a 283 MB upload that was thirteen seconds of sluggishness. With
// compression added it became five minutes and six seconds, measured, on a
// fourteen minute clip. Windows put "(Not Responding)" in the title bar,
// the log filled with the window complaining that its own status call had not
// come back, and the only visible sign of progress was a spinner that had
// stopped spinning because nothing could draw it.
//
// The work was completely fine. It shrank 390 MB to 159 MB and uploaded it
// successfully. The failure was entirely in doing it on the wrong thread.
//
// # THE RULE THIS ENFORCES
//
// A bridge binding must return quickly. Anything that can take longer than an
// eyeblink starts a goroutine, records its state here, and lets the window ask
// how it is going. That is not a nicety - a binding that blocks is a frozen
// application, and a frozen application looks broken no matter how well the
// work underneath it is going.
package main

import (
	"path/filepath"
	"sync"
	"time"
)

// Stages a send passes through. These strings reach the window, so they are
// written as something a person can read rather than as internal names.
const (
	sendCompressing = "compressing"
	sendUploading   = "uploading"
	sendDone        = "done"
	sendFailed      = "failed"
)

type sendJob struct {
	Path    string    `json:"path"`
	Name    string    `json:"name"`
	Stage   string    `json:"stage"`
	Note    string    `json:"note"`
	Started time.Time `json:"-"`
	Seconds int       `json:"seconds"`
}

var (
	sendMu   sync.Mutex
	sendJobs = map[string]*sendJob{}
)

// sendInFlight reports whether this clip is already on its way, so a second
// click cannot start a second copy of the same expensive job.
func sendInFlight(path string) bool {
	sendMu.Lock()
	defer sendMu.Unlock()
	j, ok := sendJobs[path]
	return ok && (j.Stage == sendCompressing || j.Stage == sendUploading)
}

func setSendStage(path, stage, note string) {
	sendMu.Lock()
	defer sendMu.Unlock()
	j, ok := sendJobs[path]
	if !ok {
		return
	}
	j.Stage = stage
	j.Note = note
}

// sendSnapshot is what the window reads every couple of seconds.
//
// Finished jobs are kept for a short while rather than deleted the instant they
// end. A "done" that vanishes before the next poll is a send that, from the
// player's side, simply stopped happening with no result either way.
func sendSnapshot() []sendJob {
	sendMu.Lock()
	defer sendMu.Unlock()
	out := make([]sendJob, 0, len(sendJobs))
	for path, j := range sendJobs {
		age := time.Since(j.Started)
		if (j.Stage == sendDone || j.Stage == sendFailed) && age > 90*time.Second {
			delete(sendJobs, path)
			continue
		}
		c := *j
		c.Seconds = int(age.Seconds())
		out = append(out, c)
	}
	return out
}

// startSend kicks the whole thing off and returns immediately.
//
// The returned error is only ever about whether the job could be STARTED. What
// happens afterwards is reported through sendSnapshot, because by then the
// window has long since had its answer back.
func startSend(cfg config, rc recorderConfig, path, kind, matchFolder string) {
	sendMu.Lock()
	sendJobs[path] = &sendJob{
		Path:    path,
		Name:    filepath.Base(path),
		Stage:   sendCompressing,
		Note:    "making a smaller copy to send",
		Started: time.Now(),
	}
	sendMu.Unlock()

	go func() {
		// compressForUpload is the slow half and it reports itself, so the
		// stage is set before it starts rather than after.
		sendPath, cleanup := compressForUpload(rc, path)
		defer cleanup()

		setSendStage(path, sendUploading, "uploading to SiegeIQ")

		done, err := uploadClipFile(&cfg, rc, path, sendPath, kind, matchFolder)
		switch {
		case err == nil:
			setSendStage(path, sendDone, "sent to SiegeIQ")
		case done:
			setSendStage(path, sendFailed, err.Error())
		default:
			setSendStage(path, sendFailed, err.Error()+" (it is still on your PC)")
		}
	}()
}
