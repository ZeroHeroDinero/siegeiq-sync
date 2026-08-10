// pausing.go - the two independent switches, in one place.
//
// Sync and the recorder pause separately, and now there are two ways to flip
// each: the tray menu and the app window. Keeping the flags here, with the tray
// menu items registered alongside them, is what stops the window and the tray
// disagreeing - flip it in one and the other's label updates too.
//
// Before this file existed the flags were toggled inline in two different menu
// handlers, which was fine while the menu was the only way to reach them.
package main

import (
	"sync/atomic"

	"github.com/getlantern/systray"
)

// The tray items, registered as they are created so a toggle from the window
// can relabel them. Nil is fine and expected before the tray has been built.
var (
	traySyncPauseItem     *systray.MenuItem
	trayRecorderPauseItem *systray.MenuItem
)

func registerSyncPauseItem(m *systray.MenuItem)     { traySyncPauseItem = m }
func registerRecorderPauseItem(m *systray.MenuItem) { trayRecorderPauseItem = m }

func syncIsPaused() bool     { return atomic.LoadInt32(&paused) == 1 }
func recorderIsPaused() bool { return atomic.LoadInt32(&recorderPaused) == 1 }

func setSyncPaused(on bool) {
	if on {
		atomic.StoreInt32(&paused, 1)
	} else {
		atomic.StoreInt32(&paused, 0)
	}
	if traySyncPauseItem != nil {
		if on {
			traySyncPauseItem.SetTitle("Resume syncing")
		} else {
			traySyncPauseItem.SetTitle("Pause syncing")
		}
	}
	logf("sync %s", map[bool]string{true: "paused", false: "resumed"}[on])
}

func setRecorderPaused(on bool) {
	if on {
		atomic.StoreInt32(&recorderPaused, 1)
	} else {
		atomic.StoreInt32(&recorderPaused, 0)
	}
	if trayRecorderPauseItem != nil {
		if on {
			trayRecorderPauseItem.SetTitle("Resume recording")
		} else {
			trayRecorderPauseItem.SetTitle("Pause recording")
		}
	}
	logf("recorder %s", map[bool]string{true: "paused", false: "resumed"}[on])
}

// toggleSyncPause and toggleRecorderPause return the NEW paused state, so a
// caller can update its own control without asking again.
func toggleSyncPause() bool {
	now := !syncIsPaused()
	setSyncPaused(now)
	return now
}

func toggleRecorderPause() bool {
	now := !recorderIsPaused()
	setRecorderPaused(now)
	return now
}
