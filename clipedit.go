// clipedit.go - trimming a clip by hand, from inside the app window.
//
// # WHY THIS EXISTS
//
// The three smart keep rules in keeprules.go cut WHOLE ROUNDS, because the only
// per-round detail the client is given is a count: kills, died, clutched. There
// is no moment attached to any of them, so "keep the rounds I got a kill in"
// can only mean "keep the action phase of that round", which is typically two
// minutes of footage for a five second moment. That is not a bug in the rule, it
// is the honest consequence of the data available, and it is exactly why the
// clips felt too long.
//
// So: trim by hand. Pick the in and out points against a real preview, and cut.
// When decoded kill times do arrive, this stays useful for every clip where the
// automatic cut lands close but not right.
//
// # THE CUT IS A STREAM COPY, SO IT IS FAST AND LOSSLESS
//
// Same technique trim.go uses on the buffer: no decode, no re-encode, no quality
// loss. The cost is that the cut lands on a keyframe, so a clip can start up to
// a keyframe interval early. Starting slightly early is the harmless direction -
// a clip that begins a moment before the action is normal, one that begins a
// moment after has lost the thing you wanted to see.
//
// # WHY THIS RUNS ON THE CALLING THREAD AND SENDING DOES NOT
//
// clipsend.go is explicit that doing an upload here froze the whole application
// for five minutes, so a send is started and returns immediately. A trim is a
// different shape of work: it is local, it is I/O bound on one file, and it
// finishes in about the time it takes to read the bytes. The page is waiting on
// the answer to redraw its list, so it runs here, with a hard deadline so a
// pathological case cannot hang the window forever.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// trimMaxWait bounds the ffmpeg call. A stream copy of a ten minute clip is
// seconds; anything past this is a fault, not slowness.
const trimMaxWait = 90 * time.Second

type trimRequest struct {
	Path   string    `json:"path"`
	InSec  float64   `json:"in_sec"`
	OutSec float64   `json:"out_sec"`
	Mode   string    `json:"mode"`
	Splits []float64 `json:"splits"`
}

type trimReply struct {
	OK      bool     `json:"ok"`
	Message string   `json:"message,omitempty"`
	Error   string   `json:"error,omitempty"`
	Paths   []string `json:"paths,omitempty"`
}

func trimFail(msg string) string {
	b, _ := json.Marshal(trimReply{OK: false, Error: msg})
	return string(b)
}

// apiTrimClip is the one call the editor needs. Everything else the editor does
// happens in the page, because the window is loaded from file:// and can open a
// clip on the same disk directly.
func apiTrimClip(raw string) string {
	var req trimRequest
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &req); err != nil {
		return trimFail("the editor sent something this build could not read")
	}

	// Same boundary check as apiDeleteClip, and for the same reason: this
	// function is reachable from JavaScript and takes a path.
	if !underClipDir(req.Path) {
		return trimFail("that file is not in your clips folder")
	}
	if !fileExists(req.Path) {
		return trimFail("that clip is no longer on disk")
	}
	if req.OutSec <= req.InSec {
		return trimFail("the out point has to come after the in point")
	}
	if req.InSec < 0 {
		req.InSec = 0
	}

	rc := rec.settings()
	ff, err := findFFmpeg(rc)
	if err != nil {
		return trimFail("no capture engine was found, so nothing can be cut")
	}

	// Build the pieces. Without splits that is one piece; with them it is one
	// piece per gap, in order, ignoring anything outside the in and out points.
	type piece struct{ from, to float64 }
	var pieces []piece
	if req.Mode == "split" && len(req.Splits) > 0 {
		cuts := make([]float64, 0, len(req.Splits))
		for _, s := range req.Splits {
			if s > req.InSec && s < req.OutSec {
				cuts = append(cuts, s)
			}
		}
		sort.Float64s(cuts)
		prev := req.InSec
		for _, c := range cuts {
			if c-prev > 0.2 {
				pieces = append(pieces, piece{prev, c})
			}
			prev = c
		}
		if req.OutSec-prev > 0.2 {
			pieces = append(pieces, piece{prev, req.OutSec})
		}
	}
	if len(pieces) == 0 {
		pieces = []piece{{req.InSec, req.OutSec}}
	}

	// Read the original sidecar so every output can inherit what is true about
	// it, rather than losing the reason the clip existed the moment it is cut.
	var meta clipMeta
	base := strings.TrimSuffix(req.Path, filepath.Ext(req.Path))
	loadJSON(base+".json", &meta)

	replacing := req.Mode == "replace"
	if replacing && len(pieces) > 1 {
		return trimFail("replacing the original only makes sense with one piece")
	}

	var written []string
	for i, p := range pieces {
		out := trimOutPath(req.Path, i, len(pieces), replacing)
		if err := ffmpegCut(ff, req.Path, out, p.from, p.to-p.from); err != nil {
			// Anything already written stays: a partial result the player can
			// see beats silently deleting work that succeeded.
			return trimFail("the cut did not finish: " + err.Error())
		}
		written = append(written, out)

		m := meta
		m.DurationSec = p.to - p.from
		m.CreatedAt = time.Now()
		m.SpanFrom = time.Time{}
		m.SpanTo = time.Time{}
		note := fmt.Sprintf("trimmed by hand from %s (%s to %s of the original)",
			filepath.Base(req.Path), shortClock(p.from), shortClock(p.to))
		m.Notes = append(append([]string{}, meta.Notes...), note)
		writeClipMeta(out, m)
	}

	// Replacing swaps the file in place, once every piece is safely written.
	if replacing {
		tmp := written[0]
		if err := os.Remove(req.Path); err != nil {
			return trimFail("the trimmed copy was made but the original could not be replaced: " + err.Error())
		}
		if err := os.Rename(tmp, req.Path); err != nil {
			return trimFail("the trimmed copy was made but could not take the original's name: " + err.Error())
		}
		_ = os.Remove(strings.TrimSuffix(tmp, filepath.Ext(tmp)) + ".json")
		m := meta
		m.DurationSec = pieces[0].to - pieces[0].from
		m.CreatedAt = time.Now()
		m.Notes = append(append([]string{}, meta.Notes...),
			fmt.Sprintf("trimmed by hand to %s to %s", shortClock(pieces[0].from), shortClock(pieces[0].to)))
		writeClipMeta(req.Path, m)
		written = []string{req.Path}
		logf("recorder: %s trimmed in place from the editor", filepath.Base(req.Path))
	}

	// Sending reuses the existing path, which starts the job and returns. The
	// same guard applies: an unlinked device cannot send anything.
	if req.Mode == "send" {
		var cfg config
		loadJSON(configPath(), &cfg)
		if cfg.DeviceToken == "" {
			b, _ := json.Marshal(trimReply{
				OK: true, Paths: written,
				Message: "Trimmed. This device is not linked to a SiegeIQ account yet, so it was not sent.",
			})
			return string(b)
		}
		if sendInFlight(written[0]) {
			b, _ := json.Marshal(trimReply{OK: true, Paths: written, Message: "Trimmed. That clip is already being sent."})
			return string(b)
		}
		startSend(cfg, rc, written[0], clipKindAI, "editor")
		b, _ := json.Marshal(trimReply{OK: true, Paths: written,
			Message: fmt.Sprintf("Trimmed to %s and sending to SiegeIQ.", shortClock(pieces[0].to-pieces[0].from))})
		return string(b)
	}

	msg := fmt.Sprintf("Saved a %s clip.", shortClock(pieces[0].to-pieces[0].from))
	if len(written) > 1 {
		msg = fmt.Sprintf("Saved %d pieces.", len(written))
	}
	if replacing {
		msg = fmt.Sprintf("Replaced the original with a %s clip.", shortClock(pieces[0].to-pieces[0].from))
	}
	logf("recorder: editor wrote %d file(s) from %s", len(written), filepath.Base(req.Path))
	b, _ := json.Marshal(trimReply{OK: true, Paths: written, Message: msg})
	return string(b)
}

// trimOutPath picks a name that does not already exist. A replace writes to a
// temporary name first and is renamed over the original only once ffmpeg has
// finished, so a failed cut can never destroy the footage it was cutting.
func trimOutPath(src string, idx, total int, replacing bool) string {
	base := strings.TrimSuffix(src, filepath.Ext(src))
	if replacing {
		return base + "-trimming.mp4"
	}
	suffix := "-cut"
	if total > 1 {
		suffix = fmt.Sprintf("-part%d", idx+1)
	}
	candidate := base + suffix + ".mp4"
	for n := 2; fileExists(candidate); n++ {
		candidate = fmt.Sprintf("%s%s%d.mp4", base, suffix, n)
	}
	return candidate
}

// ffmpegCut is the copy. -ss BEFORE -i is an input seek, which snaps back to the
// preceding keyframe and keeps the first frame decodable; output seeking would
// cut mid-GOP and open on garbage. Same choice, and the same reason, as trim.go.
func ffmpegCut(ff, in, out string, start, dur float64) error {
	if dur <= 0 {
		return fmt.Errorf("that piece has no length")
	}
	ctx, cancel := context.WithTimeout(context.Background(), trimMaxWait)
	defer cancel()

	args := []string{
		"-hide_banner", "-loglevel", "error", "-nostdin", "-y",
		"-ss", fmt.Sprintf("%.3f", start),
		"-i", in,
		"-t", fmt.Sprintf("%.3f", dur),
		"-c", "copy",
		"-avoid_negative_ts", "make_zero",
		"-movflags", "+faststart",
		out,
	}
	cmd := exec.CommandContext(ctx, ff, args...)
	hideConsole(cmd)
	res, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		_ = os.Remove(out)
		return fmt.Errorf("it took longer than %s and was stopped", trimMaxWait)
	}
	if err != nil {
		_ = os.Remove(out)
		return fmt.Errorf("%v (%s)", err, strings.TrimSpace(string(res)))
	}
	st, err := os.Stat(out)
	if err != nil || st.Size() == 0 {
		_ = os.Remove(out)
		return fmt.Errorf("it produced an empty file")
	}
	return nil
}

// shortClock prints seconds the way the editor shows them, so the message the
// player reads matches the number they set.
func shortClock(sec float64) string {
	if sec < 0 {
		sec = 0
	}
	m := int(sec) / 60
	s := sec - float64(m*60)
	return fmt.Sprintf("%d:%04.1f", m, s)
}

// ── appearance ─────────────────────────────────────────────────────────────
//
// Theme, typeface and density are stored by the app rather than by the page.
// The page is rewritten to disk on every launch (see ui_window_windows.go), so
// anything it kept locally would not survive an update.

type uiPrefs struct {
	Theme   string `json:"theme"`
	Font    string `json:"font"`
	Density string `json:"density"`
}

func oneOf(v string, allowed ...string) bool {
	for _, a := range allowed {
		if v == a {
			return true
		}
	}
	return false
}

func apiSaveUIPrefs(raw string) string {
	var p uiPrefs
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &p); err != nil {
		return trimFail("that appearance setting could not be read")
	}
	// Validated against a fixed list rather than stored as typed. This value ends
	// up as an attribute on the root element, and an unknown one would simply
	// leave the window with no theme at all.
	if !oneOf(p.Theme, "cyan", "slate", "amber", "violet") {
		p.Theme = "cyan"
	}
	if !oneOf(p.Font, "ui", "condensed", "mono") {
		p.Font = "ui"
	}
	if !oneOf(p.Density, "compact", "cosy") {
		p.Density = "compact"
	}
	var c config
	loadJSON(configPath(), &c)
	c.UITheme, c.UIFont, c.UIDensity = p.Theme, p.Font, p.Density
	saveJSON(configPath(), &c)
	b, _ := json.Marshal(trimReply{OK: true})
	return string(b)
}
