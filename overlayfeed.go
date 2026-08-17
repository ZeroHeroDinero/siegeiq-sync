// overlayfeed.go - what the between-rounds overlay says, and when it says it.
//
// # THE TRIGGER IS A FILE, AND THAT IS THE WHOLE DESIGN
//
// Siege writes one .rec per round, at the moment the round ends. matchReady already
// counts those files every scan, so a round ending is already observable here with
// no new machinery, no reading of the game, and nothing that could be mistaken for
// in-match assistance. The overlay hangs off that count going up.
//
// It deliberately does NOT fire for the first sighting of a folder. A fresh install
// scanning a MatchReplay directory full of last week's matches would otherwise put a
// reminder on screen for every round the player has ever recorded.
package main

import (
	"encoding/json"
	"sync"
	"time"
)

var (
	ofMu     sync.Mutex
	ofLine   string
	ofAt     time.Time
	ofLastAt time.Time
)

// overlayFocusLine returns the player's active focus, cached for ten minutes.
// A focus is something they chose on the dashboard; it does not change mid-session,
// so asking the server on every round would be noise.
func overlayFocusLine() string {
	ofMu.Lock()
	if ofLine != "" && time.Since(ofAt) < 10*time.Minute {
		line := ofLine
		ofMu.Unlock()
		return line
	}
	ofMu.Unlock()

	var cfg config
	loadJSON(configPath(), &cfg)
	if cfg.DeviceToken == "" {
		return ""
	}
	raw, err := coachGet(cfg, "/sync/focus-line")
	if err != nil {
		return ""
	}
	var out struct {
		Line string `json:"line"`
	}
	if json.Unmarshal(raw, &out) != nil || out.Line == "" {
		return ""
	}
	ofMu.Lock()
	ofLine, ofAt = out.Line, time.Now()
	ofMu.Unlock()
	return out.Line
}

// overlayPreview draws the reminder on demand.
//
// This exists because the only other way to find out whether the overlay renders
// on a given machine was to play a whole match and hope. That is a terrible
// feedback loop for a feature whose entire job is to appear correctly over a game,
// and it is the reason nobody could tell "switched off" apart from "broken".
//
// It ignores the on/off setting on purpose: it answers "does this draw on my PC",
// which is a question worth answering even when the feature is turned off.
func overlayPreview() {
	line := overlayFocusLine()
	if line == "" {
		// No focus chosen yet, or not linked. Still worth drawing something, because
		// the point of this button is to prove the window appears.
		line = "This is where your focus appears between rounds."
	}
	overlayShowForced(line)
}

// overlayRoundEnded is called when a new .rec appears in a match folder.
//
// Rate limited to once a minute. A round cannot end twice in that window, so this
// only ever suppresses a duplicate caused by two scans racing, which is exactly the
// bug that would otherwise stack reminders on top of each other.
func overlayRoundEnded() {
	rc := rec.settings()
	if !rc.OverlayOn {
		return
	}
	ofMu.Lock()
	if time.Since(ofLastAt) < time.Minute {
		ofMu.Unlock()
		return
	}
	ofLastAt = time.Now()
	ofMu.Unlock()

	go func() {
		if line := overlayFocusLine(); line != "" {
			overlayShow(line)
		}
	}()
}
