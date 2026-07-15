// SiegeIQ Sync v0.1 - watches the Siege replay folder and uploads new matches.
//
// TRUST GUARANTEES (also printed in the app - never weaken these):
//   - Reads files only. Never touches the game process, memory, or network traffic.
//   - Watches exactly one directory tree: ...\My Games\Rainbow Six - Siege\<id>\MatchReplay
//   - Uploads only replay (.rec) files. Nothing else on disk.
//   - Pause with Ctrl+C. Unlink anytime from siegeiq.gg (Profile -> SiegeIQ Sync).
//
// Standard library only - no dependencies. Build: go build (see README.md).
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
)

const backend = "https://siegeiq-backend-production.up.railway.app"
const version = "0.1.0"
const scanEvery = 20 * time.Second
const settleFor = 45 * time.Second // a match folder must be quiet this long before upload
const maxFilesPerMatch = 21

type config struct {
	DeviceToken string `json:"device_token"`
	ReplayDir   string `json:"replay_dir"`
}

type state struct {
	Uploaded map[string]string `json:"uploaded"` // folder name -> "ok" | "failed"
}

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

func logf(format string, a ...any) {
	line := fmt.Sprintf("[%s] %s", time.Now().Format("15:04:05"), fmt.Sprintf(format, a...))
	fmt.Println(line)
	f, err := os.OpenFile(filepath.Join(configDir(), "sync.log"),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err == nil {
		fmt.Fprintln(f, line)
		f.Close()
	}
}

// findReplayDir locates ...\Documents\My Games\Rainbow Six - Siege\<profile id>\MatchReplay.
func findReplayDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	root := filepath.Join(home, "Documents", "My Games", "Rainbow Six - Siege")
	entries, err := os.ReadDir(root)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		mr := filepath.Join(root, e.Name(), "MatchReplay")
		if st, err := os.Stat(mr); err == nil && st.IsDir() {
			return mr
		}
	}
	return ""
}

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

// pair runs the device-code flow: show a 6-char code, the player enters it on
// siegeiq.gg (Profile -> SiegeIQ Sync), and we poll until approved.
func pair(cfg *config, cfgPath string) error {
	host, _ := os.Hostname()
	var start struct {
		UserCode   string `json:"user_code"`
		DeviceCode string `json:"device_code"`
		ExpiresMin int    `json:"expires_in_min"`
	}
	if err := postJSON("/sync/pair/new", map[string]string{"device_name": host}, &start); err != nil {
		return err
	}
	fmt.Println()
	fmt.Println("  ==============================================")
	fmt.Printf("   Your pairing code:   %s\n", start.UserCode)
	fmt.Println("  ==============================================")
	fmt.Println("   1. Open siegeiq.gg and sign in")
	fmt.Println("   2. Click your avatar -> find 'SiegeIQ Sync'")
	fmt.Println("   3. Type the code above and click 'Link device'")
	fmt.Printf("   (code expires in %d minutes)\n", start.ExpiresMin)
	fmt.Println()
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

// matchReady reports whether a match folder has settled (no writes for settleFor)
// and returns its .rec files, newest last.
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

// checkUpdate asks the server for the latest version and prints a one-line notice if this
// agent is behind. Public endpoint, best-effort: any error is ignored (never blocks startup).
func checkUpdate() {
	resp, err := http.Get(backend + "/sync/latest")
	if err != nil {
		return
	}
	defer resp.Body.Close()
	var latest struct {
		Version string `json:"version"`
		URL     string `json:"url"`
	}
	if json.NewDecoder(resp.Body).Decode(&latest) == nil &&
		latest.Version != "" && latest.Version != version {
		logf("a newer SiegeIQ Sync (v%s) is available at %s", latest.Version, latest.URL)
	}
}

func main() {
	fmt.Printf("SiegeIQ Sync v%s\n", version)
	fmt.Println("Reads replay files only. Never touches the game. Ctrl+C to pause anytime.")
	checkUpdate()

	cfgPath := filepath.Join(configDir(), "config.json")
	stPath := filepath.Join(configDir(), "state.json")
	cfg := &config{}
	st := &state{Uploaded: map[string]string{}}
	loadJSON(cfgPath, cfg)
	loadJSON(stPath, st)
	if st.Uploaded == nil {
		st.Uploaded = map[string]string{}
	}

	if cfg.ReplayDir == "" {
		cfg.ReplayDir = findReplayDir()
		if cfg.ReplayDir == "" {
			logf("could not find the MatchReplay folder - is Siege installed and has a match been played?")
			logf("you can set it manually in %s", cfgPath)
			fmt.Println("Press Enter to exit."); fmt.Scanln()
			return
		}
		saveJSON(cfgPath, cfg)
	}
	logf("watching: %s", cfg.ReplayDir)

	if cfg.DeviceToken == "" {
		if err := pair(cfg, cfgPath); err != nil {
			logf("pairing failed: %v", err)
			fmt.Println("Press Enter to exit."); fmt.Scanln()
			return
		}
	}

	for {
		entries, err := os.ReadDir(cfg.ReplayDir)
		if err != nil {
			logf("cannot read replay folder: %v", err)
			time.Sleep(scanEvery)
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			name := e.Name()
			if st.Uploaded[name] != "" {
				continue
			}
			files, ready := matchReady(filepath.Join(cfg.ReplayDir, name))
			if !ready {
				continue
			}
			logf("new match: %s (%d round files) - uploading...", name, len(files))
			done, err := upload(cfg, files)
			if err != nil {
				logf("  %v", err)
				if strings.Contains(err.Error(), "re-pair") {
					cfg.DeviceToken = ""
					saveJSON(cfgPath, cfg)
					if perr := pair(cfg, cfgPath); perr != nil {
						logf("re-pairing failed: %v", perr)
						time.Sleep(scanEvery)
					}
					continue
				}
			}
			if done {
				if err == nil {
					st.Uploaded[name] = "ok"
					logf("  synced ✓")
				} else {
					st.Uploaded[name] = "failed"
				}
				saveJSON(stPath, st)
			}
		}
		time.Sleep(scanEvery)
	}
}
