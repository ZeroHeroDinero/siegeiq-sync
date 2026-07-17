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

// updateAvailable returns the newer build's info, or nil if we're current or
// the check failed (failures are logged, never fatal - they must never block
// startup).
func updateAvailable() *latestInfo {
	info, err := fetchLatest()
	if err != nil {
		logf("update check skipped: %v", err)
		return nil
	}
	if info.Version == "" || !versionLess(version, info.Version) {
		return nil
	}
	if info.URL == "" {
		logf("update v%s is available but no download URL was published", info.Version)
		return nil
	}
	return info
}

// promptAndUpdate asks the player, then (on yes) downloads and installs the
// update and exits the process - the freshly-launched new version takes over.
// On decline or failure it returns and Sync keeps running as-is.
func promptAndUpdate(info *latestInfo) {
	verifyNote := "Downloaded from siegeiq.gg over HTTPS."
	if info.SHA256 != "" {
		verifyNote = "Verified from siegeiq.gg - SHA-256 checked before installing."
	}
	confirmed, _ := showConfirm(dialogSpec{
		instruction: "Update available",
		content: fmt.Sprintf(
			"SiegeIQ Sync v%s is ready to install (you have v%s).\r\n\r\n"+
				"It takes a few seconds and Sync restarts itself. Your link and settings are kept.",
			info.Version, version),
		footer:      verifyNote,
		confirmText: "Update now",
		buttonText:  "Later",
	})
	if !confirmed {
		logf("update to v%s postponed", info.Version)
		return
	}

	logf("updating to v%s...", info.Version)
	if err := applyUpdate(info); err != nil {
		logf("update failed: %v", err)
		showDialog(dialogSpec{
			instruction: "Update didn't finish",
			content: "Sync will keep running on the current version.\r\n\r\n" +
				"You can grab the latest build any time from siegeiq.gg/sync.",
			footer:       "",
			openSiteURL:  "https://siegeiq.gg/sync",
			openSiteText: "Open siegeiq.gg/sync",
			buttonText:   "Close",
		})
		return
	}
	logf("update installed - restarting into v%s", info.Version)
	os.Exit(0)
}

// applyUpdate downloads, verifies, and swaps in the new exe, then relaunches it.
func applyUpdate(info *latestInfo) error {
	tmp, err := downloadToTemp(info.URL)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer os.Remove(tmp)

	if info.SHA256 != "" {
		sum, err := sha256File(tmp)
		if err != nil {
			return err
		}
		if !strings.EqualFold(sum, info.SHA256) {
			return fmt.Errorf("checksum mismatch (got %s, expected %s)", sum, info.SHA256)
		}
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
