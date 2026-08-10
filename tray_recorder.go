// tray_recorder.go - the recorder's half of the tray menu.
//
// Kept out of main.go on purpose. main.go owns the sync watcher's menu and the
// two features are deliberately independent, so their menus are too: switching
// the recorder off does not touch syncing, and pausing syncing does not touch
// the recorder.
//
// The menu is built to answer the two questions a player actually has, in the
// order they have them: "is it recording right now?" and "what is it keeping?"
package main

import (
	"fmt"
	"time"

	"github.com/getlantern/systray"
)

// recorderPrefs reads the recorder block out of config.json before runSync has
// loaded anything, so the tray checkboxes are drawn in the right state from the
// first frame rather than flicking into place a second later.
func recorderPrefs() recorderConfig {
	var c config
	loadJSON(configPath(), &c)
	rc := c.Recorder
	rc.normalise()
	return rc
}

type recorderMenu struct {
	root   *systray.MenuItem
	status *systray.MenuItem
	pause  *systray.MenuItem

	modeItems map[string]*systray.MenuItem
	ruleItems map[string]*systray.MenuItem

	saveLast   *systray.MenuItem
	openFolder *systray.MenuItem
}

// modeLabels and ruleLabels are the player-facing wording. Order matters - it is
// the order they appear in the menu, easiest first.
var modeLabels = []struct{ key, label, tip string }{
	{ModeSiegeRunning, "While Siege is running", "Buffer footage the whole time you are in game"},
	{ModeTournament, "Tournament matches only", "Only buffer during a SiegeIQ tournament match"},
	{ModeManual, "Only when I arm it", "Buffer nothing until you switch it on from this menu"},
	{ModeOff, "Off", "No recording at all. Syncing is unaffected"},
}

var ruleLabels = []struct{ key, label, tip string }{
	{KeepActionOnly, "Action phase of every round", "Skips the prep phase. The usual choice"},
	{KeepWholeMatch, "The whole match", "One long clip from first round to last"},
	{KeepLastSeconds, "Last 30 seconds of each round", "Just the endings"},
	{KeepMyDeaths, "Only rounds I died in", "Needs decoded round detail from SiegeIQ"},
	{KeepMyKills, "Only rounds I got a kill", "Needs decoded round detail from SiegeIQ"},
	{KeepClutches, "Only my clutch rounds", "Needs decoded round detail from SiegeIQ"},
	{KeepNothing, "Nothing automatically", "Buffer only. You save moments by hand"},
}

func buildRecorderMenu() *recorderMenu {
	rc := recorderPrefs()

	m := &recorderMenu{
		modeItems: map[string]*systray.MenuItem{},
		ruleItems: map[string]*systray.MenuItem{},
	}

	m.root = systray.AddMenuItem("Recorder", "Record your matches and cut them into clips")
	m.status = m.root.AddSubMenuItem("Starting up...", "What the recorder is doing right now")
	m.status.Disable()

	m.pause = m.root.AddSubMenuItem("Pause recording", "Stop buffering until resumed. Syncing is unaffected")
	registerRecorderPauseItem(m.pause)

	record := m.root.AddSubMenuItem("Record...", "When the recorder is allowed to run")
	for _, opt := range modeLabels {
		it := record.AddSubMenuItemCheckbox(opt.label, opt.tip, rc.Mode == opt.key)
		m.modeItems[opt.key] = it
	}

	keep := m.root.AddSubMenuItem("Keep...", "What gets written out once a match ends")
	for _, opt := range ruleLabels {
		it := keep.AddSubMenuItemCheckbox(opt.label, opt.tip, rc.KeepRule == opt.key)
		m.ruleItems[opt.key] = it
	}

	m.saveLast = m.root.AddSubMenuItem("Save the last 2 minutes",
		"Write whatever is in the buffer right now to a clip")
	m.openFolder = m.root.AddSubMenuItem("Open clips folder", "Show your saved clips in Explorer")

	return m
}

// setStatus is handed to the recorder so its status lands in the menu without
// the recorder needing to know anything about systray.
func (m *recorderMenu) setStatus(s string) {
	if m == nil || m.status == nil {
		return
	}
	m.status.SetTitle(s)
}

// checkOnly gives a set of checkboxes radio-button behaviour, which systray does
// not offer natively.
func checkOnly(items map[string]*systray.MenuItem, chosen string) {
	for key, it := range items {
		if key == chosen {
			it.Check()
		} else {
			it.Uncheck()
		}
	}
}

func (m *recorderMenu) run() {
	rec.setStatusFn(m.setStatus)

	// The two option groups are fanned into one channel each, ONCE, by permanent
	// goroutines. Building them inside the loop instead would drop a click every
	// time the select woke on a different case, which is the sort of bug that
	// shows up as "the menu sometimes ignores me" and is miserable to chase.
	modeCh := fanIn(m.modeItems)
	ruleCh := fanIn(m.ruleItems)

	// One select over every menu item. Each case is small on purpose: anything
	// that touches the network or the disk is pushed onto its own goroutine so a
	// slow operation can never freeze the tray menu.
	for {
		select {
		case <-m.pause.ClickedCh:
			// Routed through the shared helper so the app window's own pause
			// control and this menu item can never end up disagreeing.
			toggleRecorderPause()

		case <-m.saveLast.ClickedCh:
			go func() {
				path, err := rec.saveLast(2 * time.Minute)
				if err != nil {
					logf("recorder: manual save failed: %v", err)
					notifyClipFailed(err.Error())
					return
				}
				notifyClipsSaved(1, path)
			}()

		case <-m.openFolder.ClickedCh:
			go rec.openClipFolder()

		case key := <-modeCh:
			checkOnly(m.modeItems, key)
			rec.setMode(key)
			if key == ModeManual {
				rec.setArmed(true)
			}

		case key := <-ruleCh:
			checkOnly(m.ruleItems, key)
			rec.setKeepRule(key)
		}
	}
}

// fanIn merges several menu items' click channels into one, tagging each click
// with the option it came from. One long-lived goroutine per item, started once,
// so no click is ever dropped between iterations of the select above.
func fanIn(items map[string]*systray.MenuItem) <-chan string {
	out := make(chan string)
	for key, it := range items {
		go func(key string, it *systray.MenuItem) {
			for range it.ClickedCh {
				out <- key
			}
		}(key, it)
	}
	return out
}

// recorderSummary is a one-line description of the current settings, for the log
// at startup so a support question can be answered from sync.log alone.
func recorderSummary(rc recorderConfig) string {
	return fmt.Sprintf("mode=%s keep=%s %dfps cap=%dp buffer=%dmin/%dMB clips=%dMB/%dd dest=%s tournament_auto=%v",
		rc.Mode, rc.KeepRule, rc.FPS, rc.HeightCap,
		rc.BufferMinutes, rc.BufferDiskMB, rc.ClipDiskMB, rc.ClipKeepDays,
		rc.SendToAI, rc.AutoUploadTournament)
}
