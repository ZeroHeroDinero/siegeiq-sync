// config.go - config/state files, paths, logging, and the app-wide constants.
//
// SiegeIQ Sync keeps two tiny JSON files plus a plain-text log in
// %APPDATA%\SiegeIQSync\ - nothing hidden, nothing anywhere else on disk. The
// "View log" tray item opens sync.log so a curious player can see exactly what
// Sync has done.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// backend is the SiegeIQ API root. version is the running build - keep it in
// lockstep with winres/winres.json and the installer's AppVersion.
const backend = "https://siegeiq-backend-production.up.railway.app"
const version = "1.5.8"

const (
	scanEvery = 20 * time.Second // how often the watch loop rescans the replay folder
	// How long a match folder must be quiet before it counts as finished.
	//
	// Two values, because the right answer depends on whether the game is still
	// open. Siege writes one .rec at the END of each round, so quiet time is NOT
	// evidence a match is over while somebody is still playing - it is just the
	// middle of the next round. See matchReady.
	settleFor    = 45 * time.Second // Siege has exited: the match is definitely over
	matchOverFor = 5 * time.Minute  // Siege still running: longer than any single round

	maxFilesPerMatch = 21 // hard cap on round files sent per match
)

// launchedByWindows is true when the process was started by the run-at-startup
// registry entry rather than by a person. It decides whether the app window
// appears on launch.
var launchedByWindows bool

// paused is an atomic bool (0 = watching, 1 = paused via the tray menu).
var paused int32

type config struct {
	DeviceToken string `json:"device_token"`

	// CoachLang is the language the spoken coaching is written and voiced in.
	//
	// Stored on THIS MACHINE, unlike the chosen coach, which lives on the account so
	// that siegeiq.gg and this app cannot disagree about it. That inconsistency is
	// deliberate and worth knowing about: putting language on the account needs a
	// column and an endpoint, and a player who wants German here almost certainly
	// wants German everywhere, so this should move to the account when there is a
	// reason to touch that table anyway. Empty means English.
	CoachLang string `json:"coach_lang,omitempty"`
	ReplayDir string `json:"replay_dir"`
	// Stored as OFF switches so an absent key means ON. A config file written by an
	// older build therefore turns the notifications on rather than silently leaving
	// a new feature dead for every existing install.
	NotifySoundOff bool `json:"notify_sound_off,omitempty"`
	NotifyToastOff bool `json:"notify_toast_off,omitempty"`

	// Paths to .wav files the player would rather hear than the built-in ones.
	// Empty means use the sound compiled into the app. Anything in
	// C:\Windows\Media works, and so does any other .wav on the machine.
	SoundOKFile   string `json:"sound_ok_file,omitempty"`
	SoundFailFile string `json:"sound_fail_file,omitempty"`
	SoundClipFile string `json:"sound_clip_file,omitempty"`

	// Recorder settings live inside the same config file. An install that has
	// never seen the recorder gets the zero value here, which normalise() turns
	// into the defaults - so upgrading does not require the player to do
	// anything, and downgrading leaves the block harmlessly ignored.
	Recorder recorderConfig `json:"recorder"`

	// Settings sharing. Stored as ON switches, which is the opposite of every
	// other preference in this file, and deliberately so: an absent key here
	// must mean OFF. Reading a player's GameSettings.ini because their config
	// predates the feature and therefore has no opinion is not a default worth
	// shipping. See settingsshare.go.
	SettingsAimOn  bool `json:"settings_aim_on,omitempty"`
	SettingsPerfOn bool `json:"settings_perf_on,omitempty"`

	// MouseDPI is typed by the player and is optional. GameSettings.ini has no
	// DPI key at any Siege version - the game cannot see it - so eDPI and cm/360
	// stay empty until this is filled in. Zero means not stated.
	MouseDPI int `json:"mouse_dpi,omitempty"`

	// WindowShown records that the app window has been opened at least once.
	// A fresh install opens it automatically so somebody who has just run the
	// installer sees the app rather than hunting for a tray icon they were not
	// told about. Afterwards it stays out of the way and opens only on request.
	WindowShown bool `json:"window_shown,omitempty"`
}

// ClipFileOrEmpty is a small helper so callers can compare against the stored
// value without caring whether the config has ever been written.
func (c config) ClipFileOrEmpty() string { return c.SoundClipFile }

// configPath is the one place the config filename is spelled.
func configPath() string { return filepath.Join(configDir(), "config.json") }

// notifyPrefs reads just the two notification switches. onReady needs them to draw the
// tray checkboxes before runSync has loaded the config, and reading a small JSON file
// twice at startup is cheaper than restructuring the startup order around it.
func notifyPrefs() (sound, toast bool) {
	var c config
	loadJSON(configPath(), &c)
	return !c.NotifySoundOff, !c.NotifyToastOff
}

type state struct {
	Uploaded map[string]string `json:"uploaded"` // folder name -> "ok" | "failed"
	// Clipped is the recorder's own record of which matches it has already cut
	// footage for. Kept separate from Uploaded on purpose: a match can be
	// clipped without ever being uploaded (syncing paused) or uploaded without
	// ever being clipped (recorder off), and conflating the two would make one
	// feature silently skip work because the other had already run.
	Clipped map[string]string `json:"clipped,omitempty"`
}

// configDir returns %APPDATA%\SiegeIQSync (created if missing), falling back to
// the home directory if APPDATA is somehow unset.
func configDir() string {
	base := os.Getenv("APPDATA")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = home
	}
	dir := filepath.Join(base, "SiegeIQSync")
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

func loadJSON(path string, v any) {
	b, err := os.ReadFile(path)
	if err == nil {
		_ = json.Unmarshal(b, v)
	}
}

func saveJSON(path string, v any) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err == nil {
		_ = os.WriteFile(path, b, 0o644)
	}
}

// maxLogBytes is when the log gets rolled over to sync.log.old.
//
// The log had reached 410 KB and eleven months of history, with entries from
// four different versions interleaved. Opening it to answer "what is this build
// doing" meant scrolling past a v0.3.4 session from July. One rollover keeps the
// current picture readable and still keeps the previous file for comparison.
const maxLogBytes = 512 * 1024

// logf appends one timestamped line to %APPDATA%\SiegeIQSync\sync.log.
//
// Every line carries the version. It used to appear only on the "starting" line,
// which is fine until two builds have written to the same file - and on a
// machine with an installed copy AND a locally built one, that is every day.
// Reading a log and having to scroll up to find out which build wrote a line is
// how time gets wasted diagnosing a bug that was fixed two versions ago.
func logf(format string, a ...any) {
	line := fmt.Sprintf("[%s v%s] %s", time.Now().Format("15:04:05"), version,
		fmt.Sprintf(format, a...))

	path := filepath.Join(configDir(), "sync.log")
	if st, err := os.Stat(path); err == nil && st.Size() > maxLogBytes {
		_ = os.Remove(path + ".old")
		_ = os.Rename(path, path+".old")
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err == nil {
		fmt.Fprintln(f, line)
		f.Close()
	}
}
