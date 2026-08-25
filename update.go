// update.go - the built-in auto-updater.
//
// Flow: ask the backend (/sync/latest) for the newest version. If we're behind,
// offer to update. On confirm we download the new exe, verify its SHA-256 (when
// the server publishes one), swap it into place, relaunch, and exit. The old
// binary is left as SiegeIQSync.exe.old and deleted on the next launch - you
// can't delete a running exe on Windows, but you CAN rename it, which is what
// makes an in-place self-update possible without an installer or admin rights
// (the per-user install location is writable by the user who runs it).
//
// Every step is best-effort and reversible: a failed download, a bad checksum,
// or an unwritable folder just logs and leaves the current version running.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// latestInfo is what /sync/latest returns. sha256 is optional but recommended -
// when present the downloaded exe is checked against it before we install it.
type latestInfo struct {
	Version string `json:"version"`
	URL     string `json:"url"`
	SHA256  string `json:"sha256"`
}

// versionLess does a real numeric "a.b.c" comparison rather than a string
// compare, so a stale SYNC_LATEST_VERSION (or a beta build ahead of what's
// published) never nags about an OLDER version. Missing/blank parts count as 0,
// so "0.2" < "0.2.1" as expected.
func versionLess(a, b string) bool {
	// Tolerate a leading "v" (e.g. a server that returns "v1.0.0") so it isn't
	// parsed as 0 and mistaken for an older build.
	a = strings.TrimPrefix(strings.TrimSpace(a), "v")
	b = strings.TrimPrefix(strings.TrimSpace(b), "v")
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < 3; i++ {
		var na, nb int
		if i < len(pa) {
			fmt.Sscanf(pa[i], "%d", &na)
		}
		if i < len(pb) {
			fmt.Sscanf(pb[i], "%d", &nb)
		}
		if na != nb {
			return na < nb
		}
	}
	return false
}

// cleanupOldExe removes the SiegeIQSync.exe.old left behind by a previous
// self-update. Safe to call every launch; a no-op when there's nothing to clean.
func cleanupOldExe() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	_ = os.Remove(exe + ".old")
}

// fetchLatest asks the backend what the newest published build is.
func fetchLatest() (*latestInfo, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(backend + "/sync/latest")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var info latestInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}
	return &info, nil
}

// updateAvailable returns the newer build's info, or nil if we're current.
//
// THREE OUTCOMES, NOT TWO, and collapsing them is what made the tray button lie.
//
//	info, nil   an update is ready
//	nil,  nil   we are current
//	nil,  err   we could not ask
//
// It used to return a bare *latestInfo, so "Check for updates" showed "You're up
// to date" after a failed lookup. On this machine every check between 08:51 and
// 20:13 on 2026-08-24 failed with "dial tcp: lookup ... no such host" - Sync starts
// with Windows and asks before DNS is ready - and each one was reported to the
// player as being current. Telling somebody they are on the newest build when you
// could not reach the server is worse than telling them nothing.
func updateAvailable() (*latestInfo, error) {
	info, err := fetchLatest()
	if err != nil {
		logf("update check skipped: %v", err)
		return nil, err
	}
	if info.Version == "" || !versionLess(version, info.Version) {
		return nil, nil
	}
	if info.URL == "" {
		logf("update v%s is available but no download URL was published", info.Version)
		return nil, nil
	}
	if info.SHA256 == "" {
		// Don't even offer an update we know applyUpdate will refuse (see the fail-closed
		// note there) - better to silently stay on the current version than show the user
		// an "Update now" button that always ends in an error dialog.
		logf("update v%s is available but the server published no sha256 - not offering it "+
			"until SYNC_LATEST_SHA256 is set on the backend", info.Version)
		return nil, nil
	}
	return info, nil
}

// startUpdateWatcher keeps asking, because asking once was not enough.
//
// The startup check happens before the network is reliably up - Sync launches with
// Windows - and until 2026-08-25 nothing ever tried again. A player who never opened
// the tray menu stayed on whatever build they installed, forever, which is why fixes
// kept not reaching people who had the app running the whole time.
//
// NEVER MID-MATCH. Installing exits the process to relaunch the new build, so a check
// that fires while Siege is up would end the recording the player is relying on. It
// waits for a quiet moment instead; there is no hurry.
func startUpdateWatcher() {
	go func() {
		// Short retries first, for the boot-time DNS case, then a slow heartbeat.
		for _, d := range []time.Duration{30 * time.Second, 2 * time.Minute, 10 * time.Minute} {
			time.Sleep(d)
			if _, err := updateAvailable(); err == nil {
				break // the server answered; the slow loop takes it from here
			}
		}
		for {
			time.Sleep(6 * time.Hour)
			if siegeRunning() || rec.capturing() {
				continue
			}
			if info, err := updateAvailable(); err == nil && info != nil {
				logf("update available in the background: v%s", info.Version)
				autoUpdate(info) // installs and restarts, exiting this process
			}
		}
	}()
}

// updatedMarkerPath is where a self-update leaves a note for the version that replaces
// it. The process exits mid-update, so the only way the NEW build can know it arrived
// via an update is for the old one to write it down first.
func updatedMarkerPath() string { return filepath.Join(configDir(), "updated_to.txt") }

// consumeUpdatedMarker returns the version we just updated INTO, once, and clears it.
// Empty string means this was an ordinary start.
func consumeUpdatedMarker() string {
	b, err := os.ReadFile(updatedMarkerPath())
	if err != nil {
		return ""
	}
	_ = os.Remove(updatedMarkerPath())
	return strings.TrimSpace(string(b))
}

// autoUpdate downloads, verifies and installs the update WITHOUT asking, then exits so
// the freshly-launched new version takes over.
//
// Changed 2026-08-06. This used to be promptAndUpdate: it opened a confirm dialog and,
// on anything other than an explicit click, logged "postponed" and left the player on
// the old build forever. That is a fine default for a feature. It is the wrong default
// for a FIX — v0.3.1 was the release that added upload notifications, it was published
// correctly with a checksum, and players still did not have it because the dialog was
// easy to miss behind a full-screen game. A verified, checksum-matched build from our
// own backend does not need permission to install itself.
//
// The security posture is unchanged: applyUpdate still refuses anything without a
// matching SHA-256. Silent means no dialog, not unverified.
func autoUpdate(info *latestInfo) {
	logf("updating to v%s (silent)...", info.Version)
	if err := applyUpdate(info); err != nil {
		// No modal here either. A failed update is not the player's problem to solve
		// mid-match; Sync keeps working on the current build and the log has the detail.
		logf("update failed, staying on v%s: %v", version, err)
		return
	}
	if err := os.WriteFile(updatedMarkerPath(), []byte(info.Version), 0o600); err != nil {
		logf("could not write the updated-to marker: %v", err)
	}
	logf("update installed - restarting into v%s", info.Version)
	os.Exit(0)
}

// applyUpdate downloads, verifies, and swaps in the new exe, then relaunches it.
func applyUpdate(info *latestInfo) error {
	// Fail CLOSED: a missing sha256 is refused, not silently trusted. This used to be
	// "verify IF the server sent a hash," which meant that for as long as the backend's
	// /sync/latest never actually populated the sha256 field (true today - see
	// siegeiq_stripe.py's /sync/latest, which returns no sha256 at all) every single
	// auto-update installed an exe from SYNC_DOWNLOAD_URL with NO integrity check beyond
	// "the download happened over HTTPS." That's exactly the unsigned-auto-updater risk
	// called out in the pre-launch security review: a compromised download URL or a MITM
	// on that connection could hand every installed copy of Sync a malicious exe and it
	// would just be installed.
	//
	// Requiring the hash means self-update is a no-op (logs and returns, current version
	// keeps running - see updateAvailable's caller) until the backend actually publishes a
	// real SYNC_LATEST_SHA256 for each release. That is the correct failure mode: refuse an
	// unverifiable binary rather than install it. Users can still grab the current, SignPath
	// code-signed build by hand from siegeiq.gg/sync in the meantime.
	if info.SHA256 == "" {
		return fmt.Errorf("update server did not publish a sha256 for v%s - refusing to "+
			"install an unverified binary (see SYNC_LATEST_SHA256 on the backend)", info.Version)
	}

	tmp, err := downloadToTemp(info.URL)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer os.Remove(tmp)

	sum, err := sha256File(tmp)
	if err != nil {
		return err
	}
	if !strings.EqualFold(sum, info.SHA256) {
		return fmt.Errorf("checksum mismatch (got %s, expected %s)", sum, info.SHA256)
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}

	old := exe + ".old"
	_ = os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		return fmt.Errorf("could not move the current exe aside (is the folder writable?): %w", err)
	}
	if err := copyFile(tmp, exe); err != nil {
		_ = os.Rename(old, exe) // roll back so the app still works
		return fmt.Errorf("could not write the new exe: %w", err)
	}

	if err := exec.Command(exe).Start(); err != nil {
		// The update IS installed (new exe on disk; this process is still
		// running from the .old image). We just couldn't relaunch - so return an
		// error and let the caller keep THIS instance alive rather than exiting
		// into nothing. The new version takes over on the next launch.
		logf("update installed but relaunch failed: %v", err)
		return fmt.Errorf("update installed, but couldn't reopen automatically - please reopen SiegeIQ Sync")
	}
	return nil
}

func downloadToTemp(url string) (string, error) {
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	f, err := os.CreateTemp("", "SiegeIQSync-*.exe")
	if err != nil {
		return "", err
	}
	_, cpErr := io.Copy(f, resp.Body)
	clErr := f.Close()
	if cpErr != nil {
		os.Remove(f.Name())
		return "", cpErr
	}
	if clErr != nil {
		os.Remove(f.Name())
		return "", clErr
	}
	return f.Name(), nil
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	_, cpErr := io.Copy(out, in)
	clErr := out.Close()
	if cpErr != nil {
		return cpErr
	}
	return clErr
}
