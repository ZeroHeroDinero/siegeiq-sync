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
const version = "0.3.0"

const (
	scanEvery        = 20 * time.Second // how often the watch loop rescans the replay folder
	settleFor        = 45 * time.Second // a match folder must be quiet this long before upload
	maxFilesPerMatch = 21               // hard cap on round files sent per match
)

// paused is an atomic bool (0 = watching, 1 = paused via the tray menu).
var paused int32

type config struct {
	DeviceToken string `json:"device_token"`
	ReplayDir   string `json:"replay_dir"`
}

type state struct {
	Uploaded map[string]string `json:"uploaded"` // folder name -> "ok" | "failed"
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

// logf appends one timestamped line to %APPDATA%\SiegeIQSync\sync.log. There is
// no console window, so this log (plus the tray tooltip/menu) is where status
// shows up - see the "View log" tray item.
func logf(format string, a ...any) {
	line := fmt.Sprintf("[%s] %s", time.Now().Format("15:04:05"), fmt.Sprintf(format, a...))
	f, err := os.OpenFile(filepath.Join(configDir(), "sync.log"),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err == nil {
		fmt.Fprintln(f, line)
		f.Close()
	}
}
