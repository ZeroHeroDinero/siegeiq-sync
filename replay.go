// replay.go - finding the MatchReplay folder, pairing, and uploading matches.
//
// This is the part that actually does the work, and it is standard-library-only
// Go (net/http, os, mime/multipart). It reads .rec files and POSTs them to the
// SiegeIQ backend - it never touches the game process, memory, or network
// traffic, and it only ever reads the one MatchReplay directory tree.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/getlantern/systray"
)

func postJSON(path string, body any, out any) error {
	b, _ := json.Marshal(body)
	resp, err := http.Post(backend+path, "application/json", bytes.NewReader(b))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s -> HTTP %d: %s", path, resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

// readReplayEntries returns the names of the sub-folders in the replay dir -
// one folder per match.
func readReplayEntries(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names, nil
}

// isRepairNeeded reports whether an upload error means the device token is no
// longer valid and we should pair again.
func isRepairNeeded(err error) bool {
	return err != nil && strings.Contains(err.Error(), "re-pair")
}

// findReplayDir locates ...\Documents\My Games\Rainbow Six - Siege\<profile id>\MatchReplay.
func findReplayDir() string {
	// The Ubisoft Connect / Documents-library layout: one subfolder per Siege profile ID.
	if home, err := os.UserHomeDir(); err == nil {
		root := filepath.Join(home, "Documents", "My Games", "Rainbow Six - Siege")
		if entries, err := os.ReadDir(root); err == nil {
			for _, e := range entries {
				if !e.IsDir() {
					continue
				}
				mr := filepath.Join(root, e.Name(), "MatchReplay")
				if st, err := os.Stat(mr); err == nil && st.IsDir() {
					return mr
				}
			}
		}
	}

	// The Steam layout: some Steam installs write straight into the game folder
	// itself, no per-profile subfolder and no Documents involved at all. Check
	// every Steam library this machine has (default drive + any others
	// discovered via libraryfolders.vdf), since Steam installs are not always
	// on C:.
	for _, lib := range steamLibraryDirs() {
		mr := filepath.Join(lib, "steamapps", "common", "Tom Clancy's Rainbow Six Siege", "MatchReplay")
		if st, err := os.Stat(mr); err == nil && st.IsDir() {
			return mr
		}
	}

	return ""
}

// steamLibraryDirs returns every Steam library root this machine knows about:
// the default install under Program Files, plus any additional libraries Steam
// lists in libraryfolders.vdf (players who moved their library to another drive).
func steamLibraryDirs() []string {
	seen := map[string]bool{}
	var dirs []string
	add := func(p string) {
		if p == "" || seen[p] {
			return
		}
		seen[p] = true
		dirs = append(dirs, p)
	}

	for _, envVar := range []string{"ProgramFiles(x86)", "ProgramFiles"} {
		if v := os.Getenv(envVar); v != "" {
			add(filepath.Join(v, "Steam"))
		}
	}

	// libraryfolders.vdf lists any extra Steam libraries on other drives. It's a
	// simple text format - we don't need a real VDF parser, just the quoted path
	// that follows each numbered library entry.
	for _, lib := range append([]string{}, dirs...) {
		vdf := filepath.Join(lib, "steamapps", "libraryfolders.vdf")
		b, err := os.ReadFile(vdf)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(b), "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, `"path"`) {
				continue
			}
			parts := strings.Split(line, `"`)
			// Expect: "path" "C:\\SteamLibrary" -> parts = ["", "path", "", "C:\\SteamLibrary", ""]
			if len(parts) >= 4 {
				add(strings.ReplaceAll(parts[3], `\\`, `\`))
			}
		}
	}

	return dirs
}

// pair runs the device-code flow: show a 6-char code, the player enters it on
// siegeiq.gg (Profile -> SiegeIQ Sync), and we poll until approved. mStatus is
// updated live so the tray tooltip/menu reflects what's happening.
func pair(cfg *config, cfgPath string, mStatus *systray.MenuItem) error {
	host, _ := os.Hostname()
	var start struct {
		UserCode   string `json:"user_code"`
		DeviceCode string `json:"device_code"`
		ExpiresMin int    `json:"expires_in_min"`
	}
	if err := postJSON("/sync/pair/new", map[string]string{"device_name": host}, &start); err != nil {
		return err
	}
	if mStatus != nil {
		mStatus.SetTitle("Pair at siegeiq.gg - code: " + start.UserCode)
	}
	logf("pairing code: %s (enter it at siegeiq.gg -> Profile -> SiegeIQ Sync)", start.UserCode)

	// The pairing popup shows the code big and bold, with a live siegeiq.gg link
	// and a "launch at startup" checkbox. It blocks until the player clicks Done;
	// then we start polling for their approval.
	wantStartup := showDialog(dialogSpec{
		instruction: start.UserCode,
		content: "Enter this code at siegeiq.gg to link this device:\r\n\r\n" +
			"1.  Open siegeiq.gg and sign in  (use the button below)\r\n" +
			"2.  Go to your avatar  ->  SiegeIQ Sync  ->  Link device\r\n" +
			"3.  Type the code above, then click Link device\r\n\r\n" +
			"Once you've entered it, click Done - Sync finishes linking and keeps running in the tray.",
		footer:        "Reads only your Siege replays. Nothing else on your PC is touched.",
		verifyText:    "Launch SiegeIQ Sync when Windows starts",
		verifyChecked: startupEnabled(),
		openSiteURL:   "https://siegeiq.gg",
		openSiteText:  "Open siegeiq.gg",
		buttonText:    "Done",
	})
	if err := setStartup(wantStartup); err != nil {
		logf("could not update run-at-startup: %v", err)
	}

	deadline := time.Now().Add(time.Duration(start.ExpiresMin) * time.Minute)
	for time.Now().Before(deadline) {
		time.Sleep(5 * time.Second)
		var poll struct {
			Status      string `json:"status"`
			DeviceToken string `json:"device_token"`
		}
		if err := postJSON("/sync/pair/poll", map[string]string{"device_code": start.DeviceCode}, &poll); err != nil {
			logf("pair poll error (will retry): %v", err)
			continue
		}
		if poll.Status == "approved" && poll.DeviceToken != "" {
			cfg.DeviceToken = poll.DeviceToken
			saveJSON(cfgPath, cfg)
			logf("device linked - you're all set")
			return nil
		}
	}
	return fmt.Errorf("pairing code expired - restart the app for a fresh one")
}

// matchReady reports whether a match folder has settled (no writes for
// settleFor) and returns its .rec files, newest last.
func matchReady(dir string) ([]string, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, false
	}
	var recs []string
	newest := time.Time{}
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".rec") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(newest) {
			newest = info.ModTime()
		}
		recs = append(recs, filepath.Join(dir, e.Name()))
	}
	if len(recs) == 0 || time.Since(newest) < settleFor {
		return nil, false
	}
	sort.Strings(recs)
	if len(recs) > maxFilesPerMatch {
		recs = recs[:maxFilesPerMatch]
	}
	return recs, true
}

// upload sends one match's .rec files to /sync/match with the device token.
// Returns (permanentlyDone, err): 4xx responses are recorded and not retried.
func upload(cfg *config, files []string) (bool, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for _, p := range files {
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		part, err := w.CreateFormFile("files", filepath.Base(p))
		if err == nil {
			_, _ = io.Copy(part, f)
		}
		f.Close()
	}
	w.Close()
	req, err := http.NewRequest("POST", backend+"/sync/match", &buf)
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "Device "+cfg.DeviceToken)
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return false, err // network trouble: retry next scan
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	switch {
	case resp.StatusCode == 200:
		return true, nil
	case resp.StatusCode == 401 || resp.StatusCode == 403:
		return false, fmt.Errorf("unlinked (HTTP %d) - re-pair needed", resp.StatusCode)
	case resp.StatusCode >= 400 && resp.StatusCode < 500:
		return true, fmt.Errorf("rejected (HTTP %d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	default:
		return false, fmt.Errorf("server error (HTTP %d) - will retry", resp.StatusCode)
	}
}
