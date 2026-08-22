// capture_watchdog.go - noticing that capture has stopped producing anything,
// which the rest of the recorder had no way to detect.
//
// # THE BUG THIS EXISTS FOR
//
// Everything in this app decided whether recording was working by asking
// whether the ffmpeg PROCESS was alive. recorder.capturing() is literally
// "session != nil && session.Running()". That is a fine test for "did ffmpeg
// crash" and a useless one for "is ffmpeg recording", and the difference
// between those two questions is the entire bug.
//
// ffmpeg will sit there indefinitely, exit code pending, CPU near zero, if its
// video source hands it no frames. It does not exit. It does not complain on
// stderr in any way that survives to a log line. It just produces nothing. To
// every part of this program the session looked perfectly healthy: the window
// said Live, the status line said capturing, no failure was counted, no
// fallback was tried, and the buffer sat at 0 MB for as long as the player left
// it running.
//
// So the app was not wrong about a detail. It had no opinion at all about the
// only thing that matters, and it stated the opposite with confidence.
//
// # WHY THE SOURCE STOPS HANDING OVER FRAMES
//
// The GPU path is ddagrab, which is DXGI Desktop Duplication. Desktop
// Duplication captures the composited DESKTOP. On a machine with two graphics
// adapters - every gaming laptop, and this one, whose own capture self-test
// reported "graphics chip 1: Failed to enumerate DXGI output 0" - the desktop
// is composited by one adapter while the game renders on the other. While the
// game is windowed or borderless its frames go through the compositor and
// Desktop Duplication sees them. When the game takes the display in EXCLUSIVE
// fullscreen, the compositor is bypassed, and duplication on the desktop's
// adapter keeps returning the last desktop image or nothing at all.
//
// That is why the capture self-test passes every single time and recording
// then produces nothing the moment a match starts. The self-test runs against
// the desktop, which is exactly the condition the failure does not occur in.
// Testing the thing in the state it works in is how a fault survives six
// rounds of "it says it works".
//
// # WHAT THIS DOES ABOUT IT
//
// Two things, in the order they matter.
//
// First it makes the failure VISIBLE. Bytes on disk are the only honest
// measure of whether a recorder is recording, so that is what gets watched. No
// growth for captureStallSeconds while we believe we are capturing is a fault,
// and it is logged and surfaced as one.
//
// Second it RECOVERS without being asked. gdigrab captures the game's window
// through GDI rather than the desktop, and it is unaffected by which adapter
// owns the display, so the recorder switches to it and remembers the choice.
// It is slower and it is the correct answer on a machine where the fast path
// silently does nothing. A recorder that is a little slower beats one that is
// a lot faster at producing zero bytes.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// How long the buffer may go without gaining a byte before the session is
	// declared stalled. Segments are ten seconds, so two full segments plus a
	// margin: long enough that a slow disk flush or a prune is never mistaken
	// for a stall, short enough that a player loses one round rather than a
	// whole evening.
	captureStallSeconds = 25

	// Grace period after a session starts. ffmpeg has to open the device,
	// negotiate the encoder and fill its first segment before anything reaches
	// the disk, and on a cold start that is comfortably several seconds.
	captureWarmupSeconds = 15

	// How often to try again once exclusive fullscreen has been diagnosed.
	//
	// Slow on purpose. There is nothing to be gained by relaunching ffmpeg every
	// five seconds against a display it cannot read, and the only thing that
	// changes the answer is the player changing a setting. A minute is quick
	// enough that doing so feels immediate and slow enough that it costs
	// nothing while they are mid-match and have not.
	fullscreenRetryPeriod = 60 * time.Second
)

// bufferBytes is the total size of everything currently in the buffer folder.
//
// Used for reporting, NOT for deciding whether capture is alive. See
// newestWrite below for why that distinction cost an evening of recording.
func bufferBytes(dir string) int64 {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	var total int64
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".mp4") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		total += info.Size()
	}
	return total
}

// newestWrite is when anything in the buffer folder was last written to.
//
// # THE BUG THIS REPLACES, WHICH WAS MINE
//
// The watchdog originally asked "has the buffer got BIGGER since last time".
// That works right up until the buffer is full, and then it is catastrophically
// wrong, because a full buffer is SUPPOSED to stop growing. Pruning removes old
// segments at roughly the rate new ones arrive, so the total hovers at the cap
// and dips every time the pruner runs.
//
// Raising the default buffer to 45 minutes is what made this reachable. Once
// the buffer filled, the watchdog saw a total that was not increasing, declared
// a perfectly healthy capture stalled, killed it, restarted it, watched the
// same thing happen, and after three rounds gave up and stopped recording
// entirely. The log is unambiguous: a prune of 297 MB twenty seconds before
// each "STALLED", every single time.
//
// So the question is no longer "is the folder growing" but "is ffmpeg still
// writing", and the answer to that is the newest modification time in the
// folder. It is true whether the buffer is empty, filling, or full and being
// pruned as fast as it fills.
func newestWrite(dir string) time.Time {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return time.Time{}
	}
	var newest time.Time
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".mp4") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(newest) {
			newest = info.ModTime()
		}
	}
	return newest
}

// noteProgress records that ffmpeg has written something, resetting the clock.
func (r *recorder) noteProgress(at time.Time) {
	r.mu.Lock()
	r.bufWroteAt = at
	r.bufGrewAt = time.Now()
	r.mu.Unlock()
}

// resetProgress is called when a session starts, so the previous session's
// measurements can never condemn the new one.
func (r *recorder) resetProgress(dir string) {
	r.mu.Lock()
	r.bufWroteAt = newestWrite(dir)
	r.bufGrewAt = time.Now()
	r.mu.Unlock()
}

// watchProgress is the whole watchdog. Called once per tick while a session is
// believed to be running and capturing the right rectangle.
//
// Returns true if it took action, so the caller knows the session it was
// holding is gone.
func (r *recorder) watchProgress(rc recorderConfig) bool {
	r.mu.Lock()
	started := r.captureStarted
	grewAt := r.bufGrewAt
	prev := r.bufWroteAt
	sess := r.session
	r.mu.Unlock()

	if sess == nil || started.IsZero() {
		return false
	}
	if time.Since(started) < captureWarmupSeconds*time.Second {
		return false
	}

	// "Is ffmpeg still writing", not "is the folder getting bigger". A full
	// buffer is supposed to stop getting bigger.
	wrote := newestWrite(rc.bufferDir())
	if wrote.After(prev) {
		r.noteProgress(wrote)
		return false
	}
	if time.Since(grewAt) < captureStallSeconds*time.Second {
		return false
	}

	// Stalled. Which path was it, and is there a better one left to try?
	backend := sess.Backend()
	onGPU := strings.Contains(backend, "ddagrab")
	stalledFor := time.Since(grewAt).Round(time.Second)

	logf("recorder: STALLED - %s has produced no video for %s. "+
		"The process is alive and receiving no frames.", backend, stalledFor)

	r.stopCapture("capture stalled, no frames were arriving")

	if onGPU && rc.CaptureMethod != "gdigrab" {
		// Switch to window capture and remember it. This is a correction, not a
		// failure, so it deliberately does NOT count towards giving up - burning
		// a strike here would mean two genuine faults later are enough to stop
		// the recorder for the evening.
		r.mu.Lock()
		r.rc.CaptureMethod = "gdigrab"
		if r.cfg != nil {
			r.cfg.Recorder = r.rc
			saveJSON(configPath(), r.cfg)
		}
		r.failures = 0
		r.captureStarted = time.Time{}
		r.nextTry = time.Now().Add(3 * time.Second)
		r.mu.Unlock()

		logf("recorder: switching to window capture and saving that choice. " +
			"The GPU screen grab cannot see a game that has taken the display in " +
			"exclusive fullscreen, which is the usual cause on a PC with two graphics chips.")
		r.setStatus("Recorder: switched to window capture, the GPU grab was producing nothing")
		notifyClipFailed("The fast screen grab was recording nothing, so SiegeIQ switched to " +
			"window capture. If frames feel heavy, set Siege to Borderless in its display settings.")
		return true
	}

	// BOTH paths have now produced nothing while Siege is running, and that
	// combination has exactly one common cause worth naming.
	//
	// ddagrab reads the composited desktop. gdigrab reads a window through GDI.
	// A game holding the display in EXCLUSIVE FULLSCREEN defeats both: the
	// compositor is bypassed, and GDI cannot read a Direct3D surface it does not
	// own, so it returns black or nothing at all.
	//
	// The old behaviour here was to count a failure, and after three of them
	// stop recording for the rest of the session. That is precisely wrong for
	// this cause. The player has not broken anything, there is a thirty second
	// fix, and nobody was ever told what it was. Worse, giving up permanently
	// means that when they DO switch to borderless, nothing starts again.
	//
	// So this state now names itself, says what to change, and keeps trying on
	// a slow timer so that changing it is all somebody has to do.
	if siegeRunning() {
		r.mu.Lock()
		r.failures = 0
		r.gaveUp = false
		r.captureStarted = time.Time{}
		r.nextTry = time.Now().Add(fullscreenRetryPeriod)
		first := !r.saidFullscreen
		r.saidFullscreen = true
		r.lastError = "Siege appears to be in exclusive fullscreen, which no screen grab can read"
		r.mu.Unlock()

		r.setStatus("Siege is in exclusive fullscreen - set Display Mode to Borderless to record")
		if first {
			logf("recorder: both the GPU grab and window capture produced nothing while Siege " +
				"was running. That is the signature of EXCLUSIVE FULLSCREEN, which neither can " +
				"read. Open Siege, then Options, Display, and set Display Mode to Borderless. " +
				"Recording will start on its own within a minute of that change.")
			notifyClipFailed("SiegeIQ cannot record Siege in exclusive fullscreen. " +
				"In Siege go to Options, Display, and set Display Mode to Borderless. " +
				"Recording starts again by itself once you do.")
		}
		return true
	}

	// Siege is not running, so this is an ordinary fault rather than the
	// fullscreen case. Count it.
	r.noteFailure(fmt.Errorf("%s produced no video for %s", backend, stalledFor))
	return true
}

// dumpDiagnostics writes a copy of the log next to the clips.
//
// # WHY
//
// The log lives in %APPDATA%\SiegeIQSync, which is not a folder anybody browses
// and not one that can be reached from a support conversation. Every diagnosis
// in this project so far has begun with asking the player to go and find it.
// The clips folder is already open on his screen, already the place he looks
// when footage is missing, and is the natural home for the answer to "why is
// there no footage".
//
// Best effort by design. A failure to copy a log must never affect recording.
func dumpDiagnostics(clipDir string) {
	if clipDir == "" {
		return
	}
	src := filepath.Join(configDir(), "sync.log")
	b, err := os.ReadFile(src)
	if err != nil {
		return
	}
	_ = os.MkdirAll(clipDir, 0o755)
	_ = os.WriteFile(filepath.Join(clipDir, "siegeiq-sync-log.txt"), b, 0o644)
}

// inFullscreenTrap reports the diagnosed exclusive-fullscreen state, so the
// window can put the fix on screen rather than leaving it in the log.
func (r *recorder) inFullscreenTrap() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.saidFullscreen && r.session == nil
}

// stalledFor reports how long the buffer has gone without growing while a
// session is believed to be running, or zero when all is well.
//
// The window reads this so the Recorder card cannot say Live over a buffer
// that is not filling. That combination was on screen for hours and nothing in
// the app was capable of noticing it.
func (r *recorder) stalledFor() time.Duration {
	r.mu.Lock()
	sess := r.session
	started := r.captureStarted
	grewAt := r.bufGrewAt
	r.mu.Unlock()

	if sess == nil || !sess.Running() || started.IsZero() {
		return 0
	}
	if time.Since(started) < captureWarmupSeconds*time.Second {
		return 0
	}
	if d := time.Since(grewAt); d >= captureStallSeconds*time.Second {
		return d
	}
	return 0
}
