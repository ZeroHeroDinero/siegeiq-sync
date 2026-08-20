// settingsshare.go - the two opt-in switches, and the loop that posts a
// settings snapshot when it has genuinely changed.
//
// BOTH SWITCHES ARE OFF UNTIL THE PLAYER TURNS THEM ON, and they are stored as
// ON switches for exactly that reason. Every other preference in this app is
// stored as an OFF switch so an absent key means the feature is enabled for
// somebody upgrading. This one is inverted on purpose: reading a file the
// player never agreed to have read, because their config predates the feature,
// is not a default anybody should ship.
//
// The snapshot is posted only when its hash differs from the last one sent, so
// a player who never touches their sensitivity generates one row and then never
// again. The backend repeats that check, because a Sync restart forgets the
// in-memory hash and would otherwise re-post the same values once per launch.
package main

import (
	"sync/atomic"
	"time"
)

// settingsCheckEvery is deliberately slow. Nobody changes their sensitivity
// twice in five minutes, and the only cost of being late is that a change shows
// up in coaching a few minutes after the game closed.
const settingsCheckEvery = 5 * time.Minute

// settingsFirstDelay lets pairing, the first replay scan and the update check
// finish before this adds anything to startup.
const settingsFirstDelay = 45 * time.Second

var settingsAimFlag int32  // 1 = share the aim block
var settingsPerfFlag int32 // 1 = share the performance block

// lastSettingsHash is the aim+perf hash of the most recent snapshot this
// process posted. Memory only: a restart re-posts once and the backend drops it
// as a duplicate, which is cheaper than another file to keep in sync.
var lastSettingsHash atomic.Value

func settingsAimOn() bool  { return atomic.LoadInt32(&settingsAimFlag) == 1 }
func settingsPerfOn() bool { return atomic.LoadInt32(&settingsPerfFlag) == 1 }

func setSettingsAimOn(on bool) {
	var v int32
	if on {
		v = 1
	}
	atomic.StoreInt32(&settingsAimFlag, v)
}

func setSettingsPerfOn(on bool) {
	var v int32
	if on {
		v = 1
	}
	atomic.StoreInt32(&settingsPerfFlag, v)
}

// settingsSharePrefs reads the two switches and the DPI before runSync has
// loaded anything, so the tray checkboxes are drawn in the right state from the
// first frame rather than flicking into place a second later.
func settingsSharePrefs() (aim, perf bool, dpi int) {
	var c config
	loadJSON(configPath(), &c)
	return c.SettingsAimOn, c.SettingsPerfOn, c.MouseDPI
}

// saveSettingsSharePrefs writes just these three values back, preserving
// everything else in the file. It re-reads first rather than holding a config
// pointer, because the watch loop owns that pointer and can rewrite the file
// underneath the tray goroutine.
func saveSettingsSharePrefs(dpi int) {
	var c config
	loadJSON(configPath(), &c)
	c.SettingsAimOn = settingsAimOn()
	c.SettingsPerfOn = settingsPerfOn()
	if dpi >= 0 {
		c.MouseDPI = dpi
	}
	saveJSON(configPath(), &c)
	// Any change here can make the current snapshot newly sendable, so forget
	// what was last sent and let the next tick decide from scratch.
	lastSettingsHash.Store("")
}

// startSettingsWatch runs the check on its own goroutine, following the same
// pattern as the other background pollers. It is deliberately NOT part of the
// replay watch loop: a slow HTTP post there would delay every replay scan.
func startSettingsWatch() {
	go func() {
		time.Sleep(settingsFirstDelay)
		for {
			checkSettingsOnce()
			time.Sleep(settingsCheckEvery)
		}
	}()
}

func checkSettingsOnce() {
	if !settingsAimOn() && !settingsPerfOn() {
		return
	}
	var c config
	loadJSON(configPath(), &c)
	if c.DeviceToken == "" {
		return // not paired yet; nothing to attach a snapshot to
	}

	snap := readGameSettings(c.ReplayDir, settingsAimOn(), settingsPerfOn(), c.MouseDPI)
	if snap == nil {
		return
	}

	combined := snap.AimHash + "/" + snap.PerfHash
	if prev, _ := lastSettingsHash.Load().(string); prev == combined {
		return
	}

	var out struct {
		OK      bool `json:"ok"`
		Changed bool `json:"changed"`
	}
	settled, err := clipAPI(&c, "/sync/settings", snap, &out, 15*time.Second)
	if err != nil {
		// Not worth a notification. The next tick tries again, and a settings
		// snapshot that arrives five minutes late costs nothing.
		if settled {
			logf("settings: not sent (%v)", err)
			lastSettingsHash.Store(combined) // settled means do not retry this one
		}
		return
	}
	lastSettingsHash.Store(combined)
	if out.Changed {
		logf("settings: change recorded (aim=%v perf=%v)", snap.Aim != nil, snap.Perf != nil)
	}
}

// settingsFileForPlayer is what the tray's "What exactly is read?" item opens.
// Showing somebody the actual file is a better answer than any wording we could
// write about it.
func settingsFileForPlayer() string {
	var c config
	loadJSON(configPath(), &c)
	return gameSettingsPath(c.ReplayDir)
}
