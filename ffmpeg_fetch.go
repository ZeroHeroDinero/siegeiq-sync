// ffmpeg_fetch.go - fetch the recorder when somebody wants it, instead of bundling
// 98 MB into every install. 2026-09-18.
//
// WHY THIS EXISTS
//
// Sync does two unrelated things. Reading .rec replay files needs no encoder, no
// permissions and about 9 MB. Recording the screen needs ffmpeg: 98 MB on disk, about
// 22 MB once the installer compresses it, and an unsigned 100 MB binary that antivirus
// software takes a close interest in on the way in.
//
// Measured 2026-09-18: 20 people opened the Sync page and 9 installed it. Asking a
// stranger to accept a screen recorder before they have seen anything work at all is
// the wrong first ask, and the replay reading is the half only SiegeIQ does.
//
// So the recorder is fetched when it is actually wanted. Most of this already existed:
// findFFmpeg has looked in %APPDATA%\SiegeIQSync\ffmpeg since 2026-08-13, and
// ensureFFmpegDropFolder already creates that folder and writes instructions into it.
// This lands the file in exactly that folder, so a user who fixed it by hand and a user
// who pressed a button end up in an identical state, and the manual route keeps working.
//
// FAILING CLOSED IS NOT OPTIONAL HERE.
// applyUpdate in update.go refuses to install an exe when the server publishes no
// sha256, and every word of its reasoning applies here: this downloads an executable
// and then runs it. No hash, no install. When the backend offers nothing, the screen
// keeps the manual instructions it has today and nothing regresses.
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// recorderOffer is what the backend says about the downloadable recorder.
// Available is false whenever anything is missing, which is the normal state until
// SYNC_FFMPEG_URL and SYNC_FFMPEG_SHA256 are set on Railway.
type recorderOffer struct {
	Available bool   `json:"available"`
	URL       string `json:"url"`
	SHA256    string `json:"sha256"`
	SizeMB    int    `json:"size_mb"`
	Version   string `json:"version"`
}

var (
	recMu      sync.Mutex
	recState   string // "", "working", "done", or a human-readable failure
	recOfferCa *recorderOffer
	recOfferAt time.Time
)

func recorderStatus() string {
	recMu.Lock()
	defer recMu.Unlock()
	return recState
}

func setRecorderStatus(s string) {
	recMu.Lock()
	recState = s
	recMu.Unlock()
}

// fetchRecorderOffer asks the backend what it has. Cached for a minute so a UI that
// polls status cannot turn into a request loop.
func fetchRecorderOffer() *recorderOffer {
	recMu.Lock()
	if recOfferCa != nil && time.Since(recOfferAt) < time.Minute {
		o := *recOfferCa
		recMu.Unlock()
		return &o
	}
	recMu.Unlock()

	out := &recorderOffer{}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(backend + "/sync/ffmpeg")
	if err != nil {
		logf("recorder offer: %v", err)
		return out
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		logf("recorder offer: HTTP %d", resp.StatusCode)
		return out
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		logf("recorder offer: bad json: %v", err)
		return &recorderOffer{}
	}
	// Trust nothing the caller did not check. An offer without a hash is not an offer.
	if out.URL == "" || out.SHA256 == "" || !strings.HasPrefix(out.URL, "https://") {
		out.Available = false
	}
	recMu.Lock()
	cp := *out
	recOfferCa = &cp
	recOfferAt = time.Now()
	recMu.Unlock()
	return out
}

// recorderInstalled reports whether the fetched copy is already sitting in the drop
// folder. The wider search in findFFmpeg still runs at capture time; this only answers
// "did WE put one there", which is what the button needs to know.
func recorderInstalled() bool {
	p := filepath.Join(configDir(), "ffmpeg", "ffmpeg.exe")
	st, err := os.Stat(p)
	return err == nil && !st.IsDir() && st.Size() > 1<<20
}

// installRecorder downloads, verifies and installs ffmpeg.exe into the drop folder.
// Runs in the background; the UI polls recorderStatus().
func installRecorder() {
	if recorderStatus() == "working" {
		return
	}
	setRecorderStatus("working")
	go func() {
		if err := doInstallRecorder(); err != nil {
			logf("recorder install failed: %v", err)
			setRecorderStatus(err.Error())
			return
		}
		setRecorderStatus("done")
		logf("recorder installed into %s", filepath.Join(configDir(), "ffmpeg"))
	}()
}

func doInstallRecorder() error {
	offer := fetchRecorderOffer()
	if !offer.Available {
		return fmt.Errorf("no recorder download is published yet - use the manual steps in the folder")
	}
	tmp, err := downloadToTemp(offer.URL)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer os.Remove(tmp)

	sum, err := sha256File(tmp)
	if err != nil {
		return fmt.Errorf("could not read the download back: %w", err)
	}
	if !strings.EqualFold(sum, offer.SHA256) {
		// Deliberately not installed, and deliberately loud. A mismatch here is either a
		// corrupted download or something worse, and both deserve the same answer.
		return fmt.Errorf("checksum mismatch, refusing to install (got %s, expected %s)", sum, offer.SHA256)
	}

	dir := filepath.Join(configDir(), "ffmpeg")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("could not create %s: %w", dir, err)
	}
	dest := filepath.Join(dir, "ffmpeg.exe")
	// Write beside it and rename, so a half-written file is never left looking valid.
	staging := dest + ".part"
	_ = os.Remove(staging)
	if err := copyFile(tmp, staging); err != nil {
		return fmt.Errorf("could not write into %s: %w", dir, err)
	}
	_ = os.Remove(dest)
	if err := os.Rename(staging, dest); err != nil {
		_ = os.Remove(staging)
		return fmt.Errorf("could not put ffmpeg.exe in place: %w", err)
	}
	// The capability probe caches what it found, including the fact that it found
	// nothing. Clear it so the very next check sees the file we just put there.
	invalidateCaptureCaps()
	return nil
}
