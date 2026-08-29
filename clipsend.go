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
	"fmt"
	"path/filepath"
	"strings"
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
	// Added 2026-08-29. "Sent" used to be the last thing this app could say, which
	// was accurate and unhelpful - the clip had arrived somewhere and the player had
	// no idea whether anything would come of it. These two carry the line all the way
	// to a result.
	sendAnalysing = "analysing"
	sendReviewed  = "reviewed"
)

// How long to keep asking the server what happened to a clip. A review normally
// lands inside a minute or two; this is the point at which we stop watching and
// leave the card saying "still working", which is true, rather than declaring a
// failure we cannot actually see.
const reviewWatchFor = 12 * time.Minute

type sendJob struct {
	Path    string    `json:"path"`
	Name    string    `json:"name"`
	Stage   string    `json:"stage"`
	Note    string    `json:"note"`
	Started time.Time `json:"-"`
	Seconds int       `json:"seconds"`

	// ReviewID is what the "Open" button on a finished card needs. Score is shown
	// beside it when there is one; zero means "not scored yet", which is why the
	// window checks the stage rather than the number.
	ReviewID string `json:"review_id"`
	Score    int    `json:"score"`
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

func setSendReview(path, reviewID string, score int) {
	sendMu.Lock()
	defer sendMu.Unlock()
	if j, ok := sendJobs[path]; ok {
		j.ReviewID = reviewID
		j.Score = score
	}
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
		// 90 seconds was right when "done" meant the job was over. It is wrong now:
		// a card that vanishes at 90 seconds takes the finished review with it, and
		// the player comes back to the window to find no trace that anything ever
		// happened. Terminal cards stick around for a quarter of an hour, and a card
		// still waiting on a review is never swept at all.
		terminal := j.Stage == sendFailed || j.Stage == sendReviewed ||
			(j.Stage == sendDone && j.ReviewID == "")
		if terminal && age > 15*time.Minute {
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

		res, done, err := uploadClipFile(&cfg, rc, path, sendPath, kind, matchFolder)
		switch {
		case err == nil:
			setSendStage(path, sendDone, "sent to SiegeIQ")
		case done:
			setSendStage(path, sendFailed, err.Error())
			return
		default:
			setSendStage(path, sendFailed, err.Error()+" (it is still on your PC)")
			return
		}
		if res.Reviewing && res.ClipID != "" {
			watchReview(cfg, path, res.ClipID)
		}
	}()
}

// reviewLabelFor turns a clip's file name into something worth putting in a
// notification: "r03-kills.mp4" reads as "Round 3". A name that does not parse
// falls back to nothing rather than to a guess, and the caller says "Your round".
func reviewLabelFor(path string) string {
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	i := strings.Index(base, "r")
	for i >= 0 && i+3 <= len(base) {
		d := base[i+1:]
		if len(d) >= 2 && d[0] >= '0' && d[0] <= '9' && d[1] >= '0' && d[1] <= '9' {
			n := int(d[0]-'0')*10 + int(d[1]-'0')
			if n > 0 && n < 30 {
				return fmt.Sprintf("round %d", n)
			}
		}
		next := strings.Index(base[i+1:], "r")
		if next < 0 {
			break
		}
		i = i + 1 + next
	}
	return ""
}

// watchReview follows one clip from "sent" through to a finished review.
//
// It runs on the same background goroutine as the send, never on the window's
// message-loop thread - the entire reason clipsend.go exists is that a binding
// which blocks is a frozen application. See the header of this file.
//
// Every failure mode here is treated as "keep waiting", not "it broke". The review
// is running on the server regardless of whether this PC can currently reach it,
// and telling a player their review failed because their wifi dropped for ten
// seconds would be a confident lie about somebody else's machine.
func watchReview(cfg config, path, clipID string) {
	setSendStage(path, sendAnalysing, "analysing your round")
	deadline := time.Now().Add(reviewWatchFor)
	for time.Now().Before(deadline) {
		time.Sleep(5 * time.Second)
		state, reviewID, score, err := clipReviewState(&cfg, clipID)
		if err != nil && state == "" {
			continue // unreachable right now; the review is still running
		}
		switch state {
		case "complete":
			setSendReview(path, reviewID, score)
			setSendStage(path, sendReviewed, "your review is ready")
			logf("recorder: review ready for %s", filepath.Base(path))
			// The card only exists if the window is open, and after a match it
			// usually is not. This is the half that reaches somebody who has gone
			// back into a queue.
			notifyReviewReady(reviewLabelFor(path), score)
			return
		case "failed":
			note := "SiegeIQ could not review that clip"
			if err != nil {
				note = err.Error()
			}
			setSendStage(path, sendFailed, note)
			return
		case "skipped":
			note := "not reviewed"
			if err != nil {
				note = err.Error()
			}
			setSendStage(path, sendDone, note)
			return
		}
	}
	// Out of patience, not out of hope. The clip is up and the review may still
	// finish; the Results tab will show it when it does.
	setSendStage(path, sendDone, "sent - the review is taking a while, check Results")
}
