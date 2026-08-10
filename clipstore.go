// clipstore.go - where finished clips live, what sits beside them, and how the
// folder is stopped from eating a drive.
//
// Clips go somewhere a player will actually find them - Videos\SiegeIQ - not
// into a hidden application folder. Somebody who uninstalls Sync should still
// have their footage, and somebody who wants to drag a clip into Discord should
// not have to be told where to look.
//
// Every clip gets a small .json sidecar recording what it is and, crucially,
// how sure we are about it. If the round boundaries were estimated rather than
// decoded, the sidecar says so. That is the same honesty rule the replay
// pipeline holds: a number that was inferred never gets presented as measured.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// clipMeta is the sidecar written next to every clip.
type clipMeta struct {
	SchemaVersion int       `json:"schema_version"`
	CreatedAt     time.Time `json:"created_at"`
	AppVersion    string    `json:"app_version"`

	MatchFolder string    `json:"match_folder"`
	RoundIndex  int       `json:"round_index"`
	Label       string    `json:"label"`
	Reason      string    `json:"reason"`
	KeepRule    string    `json:"keep_rule"`
	SpanFrom    time.Time `json:"span_from"`
	SpanTo      time.Time `json:"span_to"`
	DurationSec float64   `json:"duration_sec"`

	// BoundariesEstimated is the honest flag. True means the round start was
	// inferred from replay-file timestamps rather than decoded from the replay
	// itself, and the clip may be a few seconds out at the front.
	BoundariesEstimated bool     `json:"boundaries_estimated"`
	Gaps                []string `json:"gaps,omitempty"`
	Notes               []string `json:"notes,omitempty"`

	CaptureBackend string `json:"capture_backend"`
	Width          int    `json:"width"`
	Height         int    `json:"height"`
	FPS            int    `json:"fps"`
}

// clipFileName builds a name that sorts chronologically and reads clearly in a
// file browser: 2026-08-08_2104_r03-action.mp4
func clipFileName(when time.Time, label string) string {
	safe := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		}
		return '-'
	}, label)
	return fmt.Sprintf("%s_%s.mp4", when.Format("2006-01-02_1504"), safe)
}

// clipDirFor groups a match's clips into one dated subfolder, so a session of
// six matches does not produce sixty loose files.
func clipDirFor(rc recorderConfig, matchFolder string, when time.Time) string {
	short := matchFolder
	if len(short) > 12 {
		short = short[:12]
	}
	return filepath.Join(rc.ClipDir, fmt.Sprintf("%s_%s", when.Format("2006-01-02"), short))
}

func writeClipMeta(clipPath string, m clipMeta) {
	m.SchemaVersion = 1
	m.AppVersion = version
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(strings.TrimSuffix(clipPath, ".mp4")+".json", b, 0o644)
}

// clipEntry is one clip on disk, for the retention pass.
type clipEntry struct {
	Path string
	Mod  time.Time
	Size int64
}

// listClips walks the clip folder. Only files this app produced are considered -
// anything that is not an mp4 with a matching sidecar is left strictly alone,
// because the clip folder is a normal folder in the player's Videos and may well
// contain their own recordings.
func listClips(root string) ([]clipEntry, error) {
	var out []clipEntry
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			// Never descend into the rolling buffer; it has its own budget.
			if info.Name() == ".buffer" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.EqualFold(filepath.Ext(path), ".mp4") {
			return nil
		}
		sidecar := strings.TrimSuffix(path, filepath.Ext(path)) + ".json"
		if _, err := os.Stat(sidecar); err != nil {
			return nil // not ours
		}
		out = append(out, clipEntry{Path: path, Mod: info.ModTime(), Size: info.Size()})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Mod.Before(out[j].Mod) })
	return out, nil
}

// pruneClips enforces the age and disk limits on finished clips, oldest first.
//
// Deleting a player's footage is the most destructive thing this app does, so it
// only ever removes files it can prove it wrote - an mp4 with a matching sidecar
// inside the configured clip folder - and it logs every deletion.
func pruneClips(rc recorderConfig) {
	clips, err := listClips(rc.ClipDir)
	if err != nil || len(clips) == 0 {
		return
	}

	maxBytes := int64(rc.ClipDiskMB) * 1024 * 1024
	cutoff := time.Now().AddDate(0, 0, -rc.ClipKeepDays)

	var total int64
	for _, c := range clips {
		total += c.Size
	}

	removed, freed := 0, int64(0)
	for _, c := range clips {
		tooOld := c.Mod.Before(cutoff)
		tooBig := total > maxBytes
		if !tooOld && !tooBig {
			break
		}
		if err := os.Remove(c.Path); err != nil {
			continue
		}
		_ = os.Remove(strings.TrimSuffix(c.Path, filepath.Ext(c.Path)) + ".json")
		total -= c.Size
		freed += c.Size
		removed++
	}
	if removed > 0 {
		logf("recorder: removed %d old clips (%d MB) to stay inside the %d MB / %d day limits",
			removed, freed/(1024*1024), rc.ClipDiskMB, rc.ClipKeepDays)
	}
	pruneEmptyClipDirs(rc.ClipDir)
}

// pruneEmptyClipDirs tidies up the dated subfolders left behind once their clips
// are gone. Only removes directories that are genuinely empty.
func pruneEmptyClipDirs(root string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() || e.Name() == ".buffer" {
			continue
		}
		p := filepath.Join(root, e.Name())
		if kids, err := os.ReadDir(p); err == nil && len(kids) == 0 {
			_ = os.Remove(p)
		}
	}
}
