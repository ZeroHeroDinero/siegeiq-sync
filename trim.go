// trim.go - cutting spans out of the rolling buffer into finished clips.
//
// # WHY THIS IS FAST AND LOSSLESS
//
// The buffer is already encoded, with a keyframe forced every two seconds. So
// producing a clip is a stream COPY: concatenate the segments that overlap the
// span, seek to the right point, copy the packets out. No decode, no re-encode,
// no quality loss, and it finishes in about the time it takes to read the bytes
// off disk rather than in real time.
//
// The cost of that choice is that cuts land on keyframes, so a clip can start up
// to two seconds early. The pre/post padding in the keep rules exists to absorb
// exactly that, and starting slightly early is the harmless direction - a clip
// that begins a moment before the action is normal, one that begins a moment
// after has lost the thing you wanted to see.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// trimResult describes what actually got written, including anything imperfect
// about it. The caller writes this into the clip's sidecar so a player looking
// at a short clip can see why it is short.
type trimResult struct {
	Path     string
	Duration time.Duration
	Gaps     []string
	Bytes    int64
}

// trimSpan writes one keepSpan out of the buffer as a single mp4.
func trimSpan(rc recorderConfig, span keepSpan, outPath string) (*trimResult, error) {
	ff, err := findFFmpeg(rc)
	if err != nil {
		return nil, err
	}

	segs, gaps, err := segmentsCovering(rc.bufferDir(), span.From, span.To)
	if err != nil {
		return nil, err
	}

	// THE OUTPUT FOLDER FIRST. This used to happen further down, just before
	// ffmpeg ran - which was too late, because the concat list below is written
	// into that same folder. The first real match ever recorded produced no clip
	// at all for exactly this reason: "The system cannot find the path
	// specified", on a directory the very next lines would have created.
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return nil, fmt.Errorf("could not create the clip folder: %v", err)
	}

	// The concat demuxer needs a list file. It lives beside the output and is
	// removed afterwards; a leftover .txt in somebody's Videos folder is the kind
	// of small rudeness that makes an app feel unfinished.
	listPath := outPath + ".concat.txt"
	var sb strings.Builder
	for _, s := range segs {
		// Backslashes and apostrophes both need escaping in a concat list.
		p := strings.ReplaceAll(s.Path, `\`, `/`)
		p = strings.ReplaceAll(p, `'`, `'\''`)
		fmt.Fprintf(&sb, "file '%s'\n", p)
	}
	if err := os.WriteFile(listPath, []byte(sb.String()), 0o644); err != nil {
		return nil, fmt.Errorf("could not write concat list: %v", err)
	}
	defer os.Remove(listPath)

	// Offset of the wanted start within the concatenated timeline.
	offset := span.From.Sub(segs[0].Start)
	if offset < 0 {
		offset = 0
	}
	dur := span.To.Sub(span.From)
	if dur <= 0 {
		return nil, fmt.Errorf("span has no duration")
	}

	args := []string{
		"-hide_banner", "-loglevel", "error", "-nostdin", "-y",
		"-f", "concat", "-safe", "0",
		// -ss before -i is an INPUT seek: it snaps back to the preceding
		// keyframe, which keeps the first frame decodable. Output seeking would
		// cut mid-GOP and produce a clip that opens on garbage.
		"-ss", fmt.Sprintf("%.3f", offset.Seconds()),
		"-i", listPath,
		"-t", fmt.Sprintf("%.3f", dur.Seconds()),
		"-c", "copy",
		"-avoid_negative_ts", "make_zero",
		"-movflags", "+faststart",
		outPath,
	}

	cmd := exec.Command(ff, args...)
	hideConsole(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("trim failed: %v (%s)", err, strings.TrimSpace(string(out)))
	}

	info, err := os.Stat(outPath)
	if err != nil {
		return nil, fmt.Errorf("trim produced no file: %v", err)
	}
	if info.Size() == 0 {
		_ = os.Remove(outPath)
		return nil, fmt.Errorf("trim produced an empty file")
	}

	return &trimResult{
		Path:     outPath,
		Duration: dur,
		Gaps:     gaps,
		Bytes:    info.Size(),
	}, nil
}
