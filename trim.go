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
	"regexp"
	"strconv"
	"strings"
	"time"
)

// trimResult describes what actually got written, including anything imperfect
// about it. The caller writes this into the clip's sidecar so a player looking
// at a short clip can see why it is short.
type trimResult struct {
	Path     string
	Duration time.Duration // what was ASKED for
	Gaps     []string
	Bytes    int64

	// Measured off the file that was actually produced, not copied from the
	// capture config. Added 2026-08-25: a sidecar that describes what was
	// intended rather than what exists is the same class of lie as writing 0
	// for "not measured". Zero means the measurement failed, and the caller
	// should fall back rather than publish a made-up number.
	ActualDuration time.Duration
	Width          int
	Height         int
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

	// ── VERIFY WHAT WE ACTUALLY WROTE ──────────────────────────────────────────
	//
	// Added 2026-08-25 after two kill clips shipped that a player could not watch.
	// Both were 14,648 bytes, byte-identical, and contained a SINGLE AAC STREAM
	// AND NO VIDEO AT ALL - 0.68 seconds of audio. The sidecar beside them claimed
	// 81 seconds at 2400x1350. ffmpeg had exited 0 because the file it produced was
	// a perfectly valid container; the requested span simply was not in the buffer
	// any more, so there was nothing to copy but a sliver of audio.
	//
	// The old checks here were "the file exists" and "the size is not zero". A
	// 14KB audio-only mp4 passes both. So a failed capture was indistinguishable
	// from a good one until somebody pressed play, which is the worst possible
	// place to find out.
	//
	// Uses ffmpeg, not ffprobe, on purpose: ffmpeg is already located and known to
	// exist, ffprobe is not guaranteed to ship beside it. `-map 0:v:0 -c copy`
	// fails loudly when there is no video stream and does not decode, so it costs
	// milliseconds.
	if err := verifyHasVideo(ff, outPath); err != nil {
		_ = os.Remove(outPath)
		return nil, fmt.Errorf("the clip was written but is not playable: %v. "+
			"This usually means the moment had already rolled out of the buffer", err)
	}

	// The video-stream check above catches a container with no picture in it. It
	// does NOT catch a clip that has video and is wildly shorter than the span
	// that was asked for, which is the same buffer-miss failure landing one step
	// further along. Measure what was produced and refuse anything that is a
	// small fraction of the request.
	actual, w, h, mErr := measureOutput(ff, outPath)
	if mErr != nil {
		// Measurement failing is not itself a reason to throw away a clip that
		// already passed the video check. Ship it, leave the measured fields at
		// zero, and let the sidecar fall back rather than invent numbers.
		logf("recorder: could not measure the finished clip: %v", mErr)
	} else if actual < time.Duration(float64(dur)*minKeptFraction) && actual < dur-2*time.Second {
		_ = os.Remove(outPath)
		return nil, fmt.Errorf("the clip came out %s long but %s was asked for. "+
			"This usually means most of the moment had already rolled out of the buffer",
			actual.Round(time.Second), dur.Round(time.Second))
	}

	return &trimResult{
		Path:           outPath,
		Duration:       dur,
		Gaps:           gaps,
		Bytes:          info.Size(),
		ActualDuration: actual,
		Width:          w,
		Height:         h,
	}, nil
}

// minKeptFraction is how much of the requested span must actually survive into
// the finished clip. Deliberately loose: a stream copy snaps to keyframes, so a
// clip is routinely a second or two off in either direction, and a tight bound
// would reject good clips. The failure this exists to catch was 0.68 seconds
// against an 81 second request, so anything near half is comfortably enough.
const minKeptFraction = 0.5

// measureOutput reads the duration and frame size back off a finished file.
//
// Uses ffmpeg rather than ffprobe for the same reason verifyHasVideo does:
// ffmpeg is already located and known to exist, ffprobe is not guaranteed to
// ship beside it. `ffmpeg -i file` with no output prints the stream summary to
// stderr and exits non-zero by design, so the error is expected and ignored;
// only the parse matters.
func measureOutput(ff, path string) (time.Duration, int, int, error) {
	cmd := exec.Command(ff, "-hide_banner", "-nostdin", "-i", path)
	hideConsole(cmd)
	out, _ := cmd.CombinedOutput()
	text := string(out)

	var dur time.Duration
	if m := durationRe.FindStringSubmatch(text); m != nil {
		hh, _ := strconv.Atoi(m[1])
		mm, _ := strconv.Atoi(m[2])
		ss, _ := strconv.ParseFloat(m[3], 64)
		dur = time.Duration(hh)*time.Hour + time.Duration(mm)*time.Minute +
			time.Duration(ss*float64(time.Second))
	}
	var w, h int
	if m := sizeRe.FindStringSubmatch(text); m != nil {
		w, _ = strconv.Atoi(m[1])
		h, _ = strconv.Atoi(m[2])
	}
	if dur == 0 && w == 0 {
		return 0, 0, 0, fmt.Errorf("could not read duration or frame size back")
	}
	return dur, w, h, nil
}

var (
	durationRe = regexp.MustCompile(`Duration:\s*(\d+):(\d\d):(\d\d(?:\.\d+)?)`)
	// Matches the WxH in a video stream line. Anchored on "Video:" so an audio
	// stream or a metadata field carrying two numbers cannot match by accident.
	sizeRe = regexp.MustCompile(`Video:[^\n]*?\b(\d{2,5})x(\d{2,5})\b`)
)

// verifyHasVideo returns nil only if outPath contains a usable video stream.
//
// Deliberately narrow. It answers one question - "is there video in here" - and
// answers it the cheapest way that cannot be fooled by a valid-but-empty
// container. It does not decode frames and it does not check duration.
func verifyHasVideo(ff, outPath string) error {
	cmd := exec.Command(ff,
		"-hide_banner", "-loglevel", "error", "-nostdin",
		"-i", outPath,
		"-map", "0:v:0", // no video stream -> ffmpeg fails here
		"-c", "copy",
		"-f", "null", "-")
	hideConsole(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("no video stream (%s)", msg)
	}
	return nil
}
