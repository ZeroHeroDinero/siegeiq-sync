// focuslog.go - remembering when Siege was NOT the window in front, so that
// footage of everything else never reaches a saved clip.
//
// # THE LEAK THIS CLOSES
//
// shouldCapture() lets the encoder keep running for focusGrace after Siege stops
// being the foreground window. That grace is deliberate and worth keeping: focus
// flickers constantly during normal play, and stopping and restarting ffmpeg on
// every Discord popup would churn processes all evening and punch holes in
// footage nobody actually left.
//
// What was NOT deliberate is where those frames went. During the grace the
// capture is still running and the screen is no longer the game, so up to four
// seconds of desktop went into the rolling buffer on every alt-tab - long enough
// to catch an email, a DM, a bank tab or a typed password - and could then be
// written into a clip. The published Terms say recording pauses when you tab out.
// This file is what makes that sentence true.
//
// # WHY EXCLUDE AT TRIM RATHER THAN NEVER CAPTURE
//
// Not capturing means stopping ffmpeg, which is the churn the grace exists to
// avoid. Cutting a hole in the middle of a finished clip is not cheap either:
// trimming is a lossless stream COPY over concatenated segments, and an interior
// excision would mean re-encoding. So the buffer still contains those seconds,
// on the player's own disk, pruned on the usual schedule - and the clip boundary
// is pulled in so they are never written out, never uploaded, and never sent
// anywhere. That is the difference that matters.
package main

import (
	"fmt"
	"sync"
	"time"
)

// focusGapWindow is one period during which capture ran while Siege was behind
// another window.
type focusGapWindow struct {
	From, To time.Time
}

var (
	fgMu      sync.Mutex
	fgWindows []focusGapWindow
	fgOpen    time.Time // zero when Siege is in front
)

// noteUnfocusedNow is called on every recorder tick that finds Siege behind
// another window while capture is still running.
func noteUnfocusedNow(now time.Time) {
	fgMu.Lock()
	defer fgMu.Unlock()
	if fgOpen.IsZero() {
		fgOpen = now
	}
}

// noteFocusedNow closes any open window. Called the moment Siege is in front
// again, and also when capture stops for any other reason, so a window can never
// be left open and swallow the rest of the evening.
func noteFocusedNow(now time.Time) {
	fgMu.Lock()
	defer fgMu.Unlock()
	if fgOpen.IsZero() {
		return
	}
	fgWindows = append(fgWindows, focusGapWindow{From: fgOpen, To: now})
	fgOpen = time.Time{}
	// Only the buffer's worth of history can ever matter, and this list is walked
	// per clip. Keep it short.
	if len(fgWindows) > 400 {
		fgWindows = fgWindows[len(fgWindows)-400:]
	}
}

// unfocusedWindows returns the recorded windows plus, if one is open right now,
// that one too - closed at the current instant so a clip cut mid-tab-out is
// still judged correctly.
func unfocusedWindows(now time.Time) []focusGapWindow {
	fgMu.Lock()
	defer fgMu.Unlock()
	out := make([]focusGapWindow, len(fgWindows))
	copy(out, fgWindows)
	if !fgOpen.IsZero() {
		out = append(out, focusGapWindow{From: fgOpen, To: now})
	}
	return out
}

// excludeUnfocused pulls a span's boundaries in so it contains no footage that
// was captured while Siege was not in front.
//
// It only ever trims from the ENDS. An unfocused window strictly inside the span
// would need the clip cut in two, which a stream copy cannot do cheaply, so that
// case keeps the longer focused side and says so. Returns ok=false when nothing
// usable is left, and the caller skips the clip rather than writing a recording
// of somebody's desktop.
func excludeUnfocused(span keepSpan, now time.Time) (keepSpan, string, bool) {
	wins := unfocusedWindows(now)
	if len(wins) == 0 {
		return span, "", true
	}

	from, to := span.From, span.To
	note := ""
	for _, w := range wins {
		if !w.To.After(from) || !to.After(w.From) {
			continue // no overlap
		}
		switch {
		case !w.From.After(from) && !w.To.Before(to):
			// Swallows the whole span.
			return span, "the whole of this span was captured while Siege was not the window in front", false
		case !w.From.After(from):
			from = w.To
			note = "trimmed the start: Siege was not the window in front"
		case !w.To.Before(to):
			to = w.From
			note = "trimmed the end: Siege was not the window in front"
		default:
			// Interior. Keep whichever focused side is longer.
			if w.From.Sub(from) >= to.Sub(w.To) {
				to = w.From
			} else {
				from = w.To
			}
			note = "shortened: you tabbed out of Siege partway through"
		}
	}

	if !to.After(from) || to.Sub(from) < 2*time.Second {
		return span, "nothing was left of this span once the tabbed-out footage was excluded", false
	}
	if note != "" {
		note = fmt.Sprintf("%s (%s)", note, span.Label)
	}
	span.From, span.To = from, to
	return span, note, true
}
