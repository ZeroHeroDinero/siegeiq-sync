// clipcompress.go - shrinking a clip before it is sent, without touching the
// copy on the player's own disk.
//
// # WHY
//
// The recorder writes footage at capture quality: 1440p, 60 fps, CQ 19, encoded
// as fast as the graphics card can manage because it is running alongside a
// game. That is the right trade for the file the player keeps. It is entirely
// the wrong trade for the file that gets sent, and the numbers make the case
// better than any argument: the first real clip sent from this app was 283 MB
// for five minutes, and a whole match measured 1.8 GB.
//
// Multiply that by every match, by every player, and it is a storage bill that
// grows faster than the thing being sold. It is also a five minute wait on a
// modest connection before anybody sees a coaching report.
//
// # WHAT IS TRADED AWAY, AND WHAT IS NOT
//
// The upload copy is 1080p at CQ 28 with a slow-but-small preset, because the
// clip is being sent to be WATCHED AND READ, not archived. 1080p is already the
// point at which the coaching side stops gaining from extra pixels, and the
// things that have to survive - the kill feed, the operator icons, the round
// timer, the health numbers - are high-contrast overlays that a lower bitrate
// treats kindly. Fast-moving texture on a wall is what gets softened, and
// nothing reads that.
//
// Audio is re-encoded at 96 kbps, which is transparent for game sound and voice
// and a fraction of the 160 kbps the capture uses.
//
// # THE COPY ON DISK IS NEVER TOUCHED
//
// This writes a temporary file, uploads that, and deletes it. The player's own
// clip stays exactly as it was recorded. Somebody who sends a clip and then
// finds their local copy has been quietly downgraded would never trust the
// Send button again, and they would be right not to.
//
// # IT NEVER BLOCKS THE SEND
//
// Every failure path returns the original file. A missing ffmpeg, a full disk,
// an encoder that will not start, or a result that came out LARGER than what we
// began with all fall back to uploading what we already have. Compression is an
// optimisation, and an optimisation that can stop a feature working is a bug.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

const (
	// Below this there is nothing worth spending CPU on. A thirty second clip
	// at 30 MB uploads in seconds either way.
	compressMinBytes = 40 * 1024 * 1024

	// Height of the upload copy. See the note above about why this is not the
	// same as the capture height.
	compressHeight = 1080

	// Frames per second for the upload copy.
	//
	// The capture runs at 60 because the player may want to watch their own
	// footage. Nothing on the coaching side needs 60: a kill feed, a round
	// timer and where somebody was standing are all readable at 30, and halving
	// the frame rate halves the bitrate needed for the same clarity.
	compressFPS = 30

	// Target bitrate for the upload copy, in kilobits per second.
	//
	// # WHY A BITRATE AND NOT A QUALITY NUMBER
	//
	// This was CQ 28, a constant-quality setting, and on real footage it did
	// almost nothing: a 283 MB clip came back 278 MB, a saving of two percent
	// for 49 seconds of work. Constant quality means the encoder spends
	// whatever it needs to hit a look, and on a clean 1080p downscale of an
	// already-compressed source it decided it needed nearly everything.
	//
	// A target bitrate cannot do that. It produces a predictable size, which is
	// the entire point of the exercise: 1.5 Mbps is about 11 MB per minute, so
	// a five minute clip lands near 60 MB and a whole match near 160 MB, every
	// time, on every machine, whichever encoder is doing the work.
	compressKbps = 1500

	// A ceiling for busy scenes, so a smoke-filled site does not spike.
	compressMaxKbps = 3000
)

// compressForUpload returns a path to send and a cleanup function.
//
// The cleanup function is ALWAYS safe to call and always non-nil, so callers
// can defer it without checking anything. When compression was skipped it does
// nothing, which is what stops a fallback path from deleting the player's clip.
func compressForUpload(rc recorderConfig, clipPath string) (string, func()) {
	noop := func() {}

	info, err := os.Stat(clipPath)
	if err != nil || info.Size() < compressMinBytes {
		return clipPath, noop
	}

	path, err := findFFmpeg(rc)
	if err != nil {
		logf("recorder: sending the clip as it is, no ffmpeg to shrink it with (%v)", err)
		return clipPath, noop
	}
	caps, err := probeFFmpeg(path)
	if err != nil {
		return clipPath, noop
	}

	out := filepath.Join(os.TempDir(),
		fmt.Sprintf("siegeiq-send-%d-%s", time.Now().UnixNano(), filepath.Base(clipPath)))

	args := []string{"-hide_banner", "-loglevel", "error", "-y", "-i", clipPath,
		"-vf", fmt.Sprintf("scale=-2:%d:flags=bicubic,fps=%d", compressHeight, compressFPS)}
	args = append(args, compressEncoder(caps)...)
	args = append(args,
		"-c:a", "aac", "-b:a", "96k", "-ac", "2",
		// faststart moves the index to the front so the file can be played
		// before it has finished downloading. It costs one extra pass over a
		// file we have already written and it is what makes a 200 MB clip
		// start playing immediately instead of after the whole download.
		"-movflags", "+faststart",
		out)

	started := time.Now()
	cmd := exec.Command(path, args...)
	hideConsole(cmd)
	if err := cmd.Run(); err != nil {
		_ = os.Remove(out)
		logf("recorder: could not shrink the clip (%v) - sending it at full size", err)
		return clipPath, noop
	}

	small, err := os.Stat(out)
	if err != nil || small.Size() == 0 || small.Size() >= info.Size() {
		// A result no smaller than the original means the work was wasted, and
		// sending it would be strictly worse than sending what we had.
		_ = os.Remove(out)
		logf("recorder: shrinking gained nothing - sending the clip at full size")
		return clipPath, noop
	}

	logf("recorder: shrank %s from %d MB to %d MB for sending, in %s (your own copy is untouched)",
		filepath.Base(clipPath), info.Size()/(1024*1024), small.Size()/(1024*1024),
		time.Since(started).Round(time.Second))
	return out, func() { _ = os.Remove(out) }
}

// compressEncoder picks the encoder for the upload copy.
//
// # A CORRECTION, WITH THE MEASUREMENT THAT FORCED IT
//
// This first shipped preferring libx264 at preset "slow", reasoning that
// nothing is competing for the machine after a match and the smallest file
// wins. That reasoning was wrong in a way only a real clip could show. On a
// fourteen minute recording it took FIVE MINUTES AND SIX SECONDS to turn 390 MB
// into 159 MB, on a machine with a perfectly good NVIDIA encoder sitting idle.
//
// A factor of 2.4 in size is not worth five minutes of a fan spinning, and it
// is certainly not worth it when the graphics card can do the same job in well
// under a minute for a file perhaps a tenth larger. Time is a cost the player
// pays and notices; a few megabytes is a cost nobody notices at all.
//
// So hardware goes first now. libx264 remains the fallback for machines with no
// hardware encoder, at "veryfast" rather than "slow", because on those machines
// the CPU is the only thing available and taking five minutes of it is exactly
// the outcome being avoided.
func compressEncoder(caps *ffmpegCaps) []string {
	b := itoa(compressKbps) + "k"
	m := itoa(compressMaxKbps) + "k"
	buf := itoa(compressMaxKbps*2) + "k"
	switch {
	case caps.HasNVENC:
		return []string{"-c:v", "h264_nvenc", "-preset", "p5", "-rc", "vbr",
			"-b:v", b, "-maxrate", m, "-bufsize", buf}
	case caps.HasAMF:
		return []string{"-c:v", "h264_amf", "-quality", "balanced", "-rc", "vbr_peak",
			"-b:v", b, "-maxrate", m}
	case caps.HasQSV:
		return []string{"-c:v", "h264_qsv", "-preset", "faster",
			"-b:v", b, "-maxrate", m}
	default:
		return []string{"-c:v", "libx264", "-preset", "veryfast",
			"-b:v", b, "-maxrate", m, "-bufsize", buf, "-pix_fmt", "yuv420p"}
	}
}
