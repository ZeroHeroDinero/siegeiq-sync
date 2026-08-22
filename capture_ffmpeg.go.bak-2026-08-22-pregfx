//go:build windows

// capture_ffmpeg.go - the first capture backend: FFmpeg driven as a child
// process.
//
// # WHY FFMPEG RATHER THAN OUR OWN ENCODER
//
// The genuinely hard part of a game recorder is not grabbing pixels, it is
// talking to three different vendors' hardware encoders and having it work on a
// stranger's machine. NVENC, AMD AMF and Intel QuickSync each have their own
// quirks, driver-version sensitivities and failure modes. FFmpeg already carries
// years of that work and falls back to CPU encoding when none of them are
// present. Reimplementing it would be months of chasing hardware we do not own.
//
// FFmpeg is invoked as a SEPARATE PROCESS, not linked into this binary. That
// keeps the licensing simple (an attribution notice, no source-disclosure
// obligation on Sync itself) and it means a wedged encoder cannot take the tray
// app down with it.
//
// TWO CAPTURE PATHS, IN ORDER OF PREFERENCE
//
//  1. ddagrab - DXGI Desktop Duplication. GPU-side, low overhead, works with
//     fullscreen exclusive DirectX. It captures a DISPLAY, so we crop to the
//     Siege window rectangle. When Siege is fullscreen (how nearly everyone
//     plays) the crop is the whole display and costs nothing.
//
//  2. gdigrab with title= - true single-window capture, but CPU-side and
//     noticeably heavier. This is the fallback for machines whose FFmpeg build
//     has no ddagrab, and it is the path that captures ONLY the game window
//     with no cropping involved at all.
//
// Neither path touches the game process. Both read what the desktop compositor
// has already drawn.
package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

func init() { registerCaptureBackend(ffmpegBackend{}) }

type ffmpegBackend struct{}

func (ffmpegBackend) Name() string { return "ffmpeg" }

// ffmpegCaps is what one particular ffmpeg.exe on this machine can actually do.
// Probing costs two short process launches, so the result is cached per path.
type ffmpegCaps struct {
	Path       string
	HasDdagrab bool
	HasNVENC   bool
	HasAMF     bool
	HasQSV     bool
	HasX264    bool
	Version    string

	// Why each hardware encoder was ruled out, in ffmpeg's own words. Kept so
	// the capture test can say "your graphics card refused this" instead of
	// inventing a reason.
	NVENCWhy string
	AMFWhy   string
	QSVWhy   string
}

// encoderUsable asks an encoder to encode a fraction of a second of blank video
// and reports whether it actually ran. This is the difference between "ffmpeg
// knows this name" and "this machine can do it".
// A failed first attempt is retried once, and that is not defensive padding.
// On this exact laptop the NVIDIA check failed on one launch and passed on the
// next, twenty minutes apart, with nothing changed - a discrete GPU that has
// powered itself down does not always answer the first time it is asked. A
// single false negative is expensive: it silently costs the player hardware
// encoding for the whole session, which is the difference between the recorder
// being free and it taking frames out of their game.
func encoderUsable(path, name string) (bool, string) {
	ok, why := encoderUsableOnce(path, name)
	if ok {
		return true, ""
	}
	time.Sleep(500 * time.Millisecond)
	if ok, _ := encoderUsableOnce(path, name); ok {
		logf("recorder: %s failed the first check and passed the second - using it", name)
		return true, ""
	}
	return false, why
}

func encoderUsableOnce(path, name string) (bool, string) {
	out, err := runFFmpegText(path,
		"-hide_banner", "-loglevel", "error",
		// color= rather than nullsrc=, because nullsrc's frame contents are
		// undefined and some hardware encoders take exception to that. Six
		// frames of black is unambiguous and just as quick.
		"-f", "lavfi", "-i", "color=c=black:s=320x240:r=30:d=0.2",
		"-c:v", name, "-f", "null", "-")
	if err == nil {
		return true, ""
	}
	return false, firstMeaningfulLine(out)
}

// firstMeaningfulLine pulls the one line of ffmpeg output worth showing a
// player. ffmpeg's failures are usually one blunt sentence buried in noise.
func firstMeaningfulLine(out string) string {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "frame=") || strings.HasPrefix(line, "size=") {
			continue
		}
		if len(line) > 160 {
			line = line[:160] + "..."
		}
		return line
	}
	return "no reason given"
}

var (
	capsMu    sync.Mutex
	capsCache = map[string]*ffmpegCaps{}
)

// findFFmpeg looks in the places a player might reasonably have put it, in the
// order that respects their intent: an explicit setting first, then the copy we
// ship beside the app, then a system install.
func findFFmpeg(rc recorderConfig) (string, error) {
	var tried []string

	try := func(p string) (string, bool) {
		if p == "" {
			return "", false
		}
		tried = append(tried, p)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, true
		}
		return "", false
	}

	if p, ok := try(rc.FFmpegPath); ok {
		return p, nil
	}
	if self, err := os.Executable(); err == nil {
		dir := filepath.Dir(self)
		if p, ok := try(filepath.Join(dir, "ffmpeg.exe")); ok {
			return p, nil
		}
		if p, ok := try(filepath.Join(dir, "ffmpeg", "ffmpeg.exe")); ok {
			return p, nil
		}
	}
	if p, ok := try(filepath.Join(configDir(), "ffmpeg", "ffmpeg.exe")); ok {
		return p, nil
	}
	// Where a player who already has FFmpeg most likely has it. Added 2026-08-13 after a
	// user hit "no capture engine" on a build that shipped without the bundled copy: the
	// three places above are all OURS, so someone with a perfectly good system FFmpeg that
	// is not on PATH got told we could not find one. Package managers put shims on PATH
	// most of the time and not always, and "C:\ffmpeg\bin" is what every guide on the
	// internet tells people to do.
	for _, p := range []string{
		filepath.Join(os.Getenv("USERPROFILE"), "scoop", "shims", "ffmpeg.exe"),
		filepath.Join(os.Getenv("ProgramData"), "chocolatey", "bin", "ffmpeg.exe"),
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Microsoft", "WinGet", "Links", "ffmpeg.exe"),
		filepath.Join(os.Getenv("SystemDrive")+"\\", "ffmpeg", "bin", "ffmpeg.exe"),
		"C:\\ffmpeg\\bin\\ffmpeg.exe",
	} {
		if q, ok := try(p); ok {
			return q, nil
		}
	}
	if p, err := exec.LookPath("ffmpeg.exe"); err == nil {
		return p, nil
	}
	if p, err := exec.LookPath("ffmpeg"); err == nil {
		return p, nil
	}
	// MAKE THE FOLDER WE ARE ABOUT TO TELL THEM ABOUT. Added 2026-08-13, and the reason is
	// embarrassing: the previous version of this message named a folder that does not exist.
	// configDir() creates %APPDATA%\SiegeIQSync, but nothing has ever created the ffmpeg
	// subfolder inside it, so a user following the instruction went looking for a path that
	// was not there and reasonably concluded the advice was wrong. AppData is hidden in
	// Explorer by default too, which turns "you cannot see it" into "this is broken".
	//
	// So: create it, drop a README in it, and only then name it. Once per process, and every
	// failure is ignored - this is a convenience on an error path and must never become a
	// second reason recording will not start.
	ensureFFmpegDropFolder()

	// THE MESSAGE LEADS WITH THE FIX, NOT THE SEARCH PATH. The old text opened with three
	// absolute paths inside the app's own install folder, which reads as a broken install
	// and tells the player nothing they can act on. The recovery folder is named first
	// because it is the one a user can write to without admin rights, and it survives an
	// app update - dropping the file into the install folder does not.
	return "", fmt.Errorf(
		"no video encoder available. To fix it, put ffmpeg.exe in this folder and "+
			"restart SiegeIQ Sync:\n\n    %s\n\n"+
			"Get a Windows build from https://www.gyan.dev/ffmpeg/builds/ (the "+
			"\"essentials\" zip), open it, and copy bin\\ffmpeg.exe into that folder. "+
			"Replay syncing keeps working either way - this only affects recording.\n\n"+
			"(also looked in: %s, and PATH)",
		filepath.Join(configDir(), "ffmpeg"), strings.Join(tried, "; "))
}

var dropFolderOnce sync.Once

// ensureFFmpegDropFolder creates the recovery folder and explains itself in writing.
//
// %APPDATA% is chosen over the install directory on purpose: a normal user can write to it
// without admin rights, and it survives an app update, which the install folder does not.
func ensureFFmpegDropFolder() {
	dropFolderOnce.Do(func() {
		dir := filepath.Join(configDir(), "ffmpeg")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return
		}
		readme := filepath.Join(dir, "PUT ffmpeg.exe IN THIS FOLDER.txt")
		if _, err := os.Stat(readme); err == nil {
			return
		}
		_ = os.WriteFile(readme, []byte(
			"SiegeIQ Sync could not find a video encoder, so recording is switched off.\r\n"+
				"Replay syncing is unaffected and is still working normally.\r\n\r\n"+
				"TO FIX IT\r\n"+
				"1. Download a Windows FFmpeg build. The \"essentials\" zip from\r\n"+
				"   https://www.gyan.dev/ffmpeg/builds/ is the usual one.\r\n"+
				"2. Open the zip and go into the bin folder.\r\n"+
				"3. Copy ffmpeg.exe into THIS folder, next to this text file.\r\n"+
				"4. Restart SiegeIQ Sync.\r\n\r\n"+
				"That is all. Nothing needs installing and you do not need admin rights.\r\n\r\n"+
				"WHY THIS FOLDER AND NOT THE PROGRAM FOLDER\r\n"+
				"This one survives app updates and you can write to it without admin rights.\r\n"+
				"A copy in the install folder gets wiped the next time SiegeIQ Sync updates.\r\n"),
			0o644)
	})
}

// runFFmpegText runs ffmpeg with the given args and returns its combined output.
// Used only for the capability probes, which are short and finite.
//
// The timeout is not decoration. ffmpeg.exe is a 100 MB unsigned binary, and on
// a machine where a security scanner decides to inspect it on first launch, a
// question that normally takes 200 milliseconds can take a very long time. A
// probe that can hang forever will eventually hang something that matters.
func runFFmpegText(path string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	hideConsole(cmd)
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return string(out), fmt.Errorf("timed out after 25s asking ffmpeg what it supports")
	}
	return string(out), err
}

// ---- the warmed capability cache ------------------------------------------
//
// WHY THIS EXISTS, AND THE BUG THAT CAUSED IT
//
// The app window polls for status every couple of seconds. The first version of
// that status call asked probeFFmpeg what the encoder situation was - which, on
// a cold cache, launches ffmpeg three times. So the window's first status
// request blocked on process launches, every later poll queued behind it on the
// same mutex, and the whole Go bridge jammed. The window sat on "Connecting..."
// forever and the capture self-test never even started, because its call was
// stuck in the same queue.
//
// The rule this enforces: ANYTHING THE WINDOW POLLS MUST READ MEMORY ONLY.
// Work that touches the disk, the network or another process happens once, in
// the background, and leaves its answer here.

var (
	warmMu    sync.RWMutex
	warmCaps  *ffmpegCaps
	warmErr   string
	warmDone  bool
	warmOnce  sync.Once
	warmStart time.Time
)

// warmCaptureCaps probes ffmpeg once, in the background, and remembers the
// answer. Safe to call repeatedly; only the first call does any work.
func warmCaptureCaps(rc recorderConfig) {
	warmOnce.Do(func() {
		warmMu.Lock()
		warmStart = time.Now()
		warmMu.Unlock()

		go func() {
			var caps *ffmpegCaps
			var errText string

			path, err := findFFmpeg(rc)
			if err != nil {
				errText = err.Error()
			} else if c, err := probeFFmpeg(path); err != nil {
				errText = err.Error()
			} else {
				caps = c
			}

			warmMu.Lock()
			warmCaps, warmErr, warmDone = caps, errText, true
			warmMu.Unlock()

			if caps != nil {
				logf("recorder: capture engine ready - %s (GPU grab: %v, nvenc: %v, amf: %v, qsv: %v)",
					caps.Version, caps.HasDdagrab, caps.HasNVENC, caps.HasAMF, caps.HasQSV)
			} else {
				logf("recorder: no usable capture engine - %s", errText)
			}
		}()
	})
}

// captureCaps returns the warmed answer without ever blocking. done is false
// while the probe is still running, which the window shows as "checking" rather
// than pretending to know.
func captureCaps() (caps *ffmpegCaps, errText string, done bool) {
	warmMu.RLock()
	defer warmMu.RUnlock()
	return warmCaps, warmErr, warmDone
}

// invalidateCaptureCaps forces a fresh probe, for when the player has just
// pointed the app at a different ffmpeg.
func invalidateCaptureCaps() {
	warmMu.Lock()
	warmCaps, warmErr, warmDone = nil, "", false
	warmOnce = sync.Once{}
	warmMu.Unlock()
}

// probeFFmpeg asks a specific ffmpeg.exe what it supports. A build without
// ddagrab is common (it arrived in FFmpeg 6.0); a build without any hardware
// encoder is unusual but survivable via libx264.
func probeFFmpeg(path string) (*ffmpegCaps, error) {
	capsMu.Lock()
	defer capsMu.Unlock()
	if c, ok := capsCache[path]; ok {
		return c, nil
	}

	c := &ffmpegCaps{Path: path}

	if out, err := runFFmpegText(path, "-hide_banner", "-version"); err == nil {
		if line, _, ok := strings.Cut(out, "\n"); ok {
			c.Version = strings.TrimSpace(line)
		} else {
			c.Version = strings.TrimSpace(out)
		}
	} else {
		return nil, fmt.Errorf("could not run %s: %v", path, err)
	}

	if out, err := runFFmpegText(path, "-hide_banner", "-filters"); err == nil {
		c.HasDdagrab = strings.Contains(out, "ddagrab")
	}
	if out, err := runFFmpegText(path, "-hide_banner", "-encoders"); err == nil {
		c.HasX264 = strings.Contains(out, "libx264")

		// LISTED IS NOT THE SAME AS USABLE, and treating it as such cost a whole
		// debugging session. A general-purpose ffmpeg build lists h264_nvenc,
		// h264_amf and h264_qsv on EVERY machine, because the list describes
		// what the binary was compiled with - not what silicon is in the PC. So
		// "auto" cheerfully picked NVENC on a laptop with no NVIDIA chip, ffmpeg
		// died in under a second, and the capture self-test reported "no frames
		// arrived" - blaming the screen grab for an encoder fault.
		//
		// Each encoder is now asked to encode a fraction of a second of nothing.
		// It costs three short launches, once, on the warm-up goroutine, and it
		// converts a guess into a fact.
		if strings.Contains(out, "h264_nvenc") {
			c.HasNVENC, c.NVENCWhy = encoderUsable(path, "h264_nvenc")
		}
		if strings.Contains(out, "h264_amf") {
			c.HasAMF, c.AMFWhy = encoderUsable(path, "h264_amf")
		}
		if strings.Contains(out, "h264_qsv") {
			c.HasQSV, c.QSVWhy = encoderUsable(path, "h264_qsv")
		}
	}

	capsCache[path] = c
	return c, nil
}

func (b ffmpegBackend) Available(rc recorderConfig) (bool, string) {
	path, err := findFFmpeg(rc)
	if err != nil {
		return false, err.Error()
	}
	caps, err := probeFFmpeg(path)
	if err != nil {
		return false, err.Error()
	}
	if !caps.HasDdagrab && !caps.HasX264 && !caps.HasNVENC && !caps.HasAMF && !caps.HasQSV {
		return false, "ffmpeg found but it has no usable encoder"
	}
	return true, ""
}

// encoderArgs picks the video encoder and its quality flags.
//
// Preference order when set to auto is NVENC, then AMF, then QuickSync, then
// libx264 on the CPU. Hardware first is not about quality - libx264 at the same
// bitrate looks better - it is about not stealing frames from the game while
// somebody is trying to play it.
// bitrateCapKbps is the ceiling handed to whichever encoder is in use.
//
// An explicit setting wins. Otherwise it is derived from what is actually being
// encoded, because a cap that is right for 720p30 is absurd at 1080p60 and vice
// versa: pixels per second is the thing that drives the number, so that is what
// it scales on. The reference point is 2 Mbps for 720p30, which is comfortable
// for gameplay review, and it scales linearly from there - about 9 Mbps at
// 1080p60. Generous enough that the quality target is what normally decides the
// file size, and low enough that a chaotic scene cannot triple it.
func bitrateCapKbps(rc recorderConfig) int {
	if rc.MaxMbps > 0 {
		return rc.MaxMbps * 1000
	}
	h := rc.HeightCap
	if h <= 0 {
		h = 1080
	}
	fps := rc.FPS
	if fps <= 0 {
		fps = 60
	}
	const refKbps, refH, refFPS = 2000, 720, 30
	kbps := refKbps * (h * h) / (refH * refH) * fps / refFPS
	if kbps < 1500 {
		kbps = 1500
	}
	if kbps > 60000 {
		kbps = 60000
	}
	return kbps
}

func encoderArgs(caps *ffmpegCaps, rc recorderConfig) (name string, args []string) {
	q := rc.Quality

	// Every encoder below gets the same ceiling. bufsize at twice the cap lets a
	// hectic second borrow from a quiet one without the average running away.
	ceiling := bitrateCapKbps(rc)
	limit := []string{
		"-maxrate", itoa(ceiling) + "k",
		"-bufsize", itoa(ceiling*2) + "k",
	}

	nvenc := func() (string, []string) {
		return "h264_nvenc", append([]string{
			"-c:v", "h264_nvenc", "-preset", "p5", "-tune", "hq",
			"-rc", "vbr", "-cq", itoa(q), "-b:v", "0",
		}, limit...)
	}
	amf := func() (string, []string) {
		return "h264_amf", append([]string{
			"-c:v", "h264_amf", "-quality", "balanced",
			"-rc", "vbr_peak", "-qp_i", itoa(q), "-qp_p", itoa(q),
		}, limit...)
	}
	qsv := func() (string, []string) {
		return "h264_qsv", append([]string{
			"-c:v", "h264_qsv", "-global_quality", itoa(q),
		}, limit...)
	}
	cpu := func() (string, []string) {
		return "libx264", append([]string{
			"-c:v", "libx264", "-preset", "veryfast", "-crf", itoa(q),
		}, limit...)
	}

	switch rc.Encoder {
	case "nvenc":
		if caps.HasNVENC {
			return nvenc()
		}
	case "amf":
		if caps.HasAMF {
			return amf()
		}
	case "qsv":
		if caps.HasQSV {
			return qsv()
		}
	case "cpu":
		return cpu()
	}

	switch {
	case caps.HasNVENC:
		return nvenc()
	case caps.HasAMF:
		return amf()
	case caps.HasQSV:
		return qsv()
	default:
		return cpu()
	}
}

// buildArgs assembles the whole ffmpeg command line for a rolling-buffer capture.
//
// The output is a SEGMENTED stream: many short mp4 files rather than one growing
// one. Three reasons, and all three matter:
//
//   - Pruning the oldest footage is deleting a whole file, not rewriting a
//     container. That is what makes a fixed disk budget possible.
//   - A crash costs at most one segment instead of the entire session.
//   - Each segment is named with the wall-clock time it started (-strftime),
//     which is precisely what lets us map "the replay file landed at 21:04:37"
//     onto a position in the footage later, without parsing anything.
//
// Keyframes are forced every 2 seconds so a later trim can cut by stream copy -
// no re-encode, no quality loss, near-instant - and still land within about two
// seconds of the requested moment. The pre/post padding in the keep rules is
// sized to absorb exactly that.
// buildArgs assembles the whole ffmpeg command line for a rolling-buffer capture.
//
// The output is a SEGMENTED stream: many short mp4 files rather than one growing
// one. Three reasons, and all three matter:
//
//   - Pruning the oldest footage is deleting a whole file, not rewriting a
//     container. That is what makes a fixed disk budget possible.
//   - A crash costs at most one segment instead of the entire session.
//   - Each segment is named with the wall-clock time it started (-strftime),
//     which is precisely what lets us map "the replay file landed at 21:04:37"
//     onto a position in the footage later, without parsing anything.
//
// Keyframes are forced every 2 seconds so a later trim can cut by stream copy -
// no re-encode, no quality loss, near-instant - and still land within about two
// seconds of the requested moment. The pre/post padding in the keep rules is
// sized to absorb exactly that.
//
// # THE ADAPTER, AND WHY THIS FUNCTION LOOKS FUSSIER THAN IT DID
//
// The first version asked Windows for the screen without saying which graphics
// chip owned it. On a desktop with one card that is harmless. On a laptop with
// switchable graphics it is fatal and SILENT: the screen is driven by the
// built-in chip while the separate GPU does the work, so asking the wrong one
// returns no frames, no error, and an encoder that then fails with "invalid
// argument" for reasons that have nothing to do with the encoder.
//
// So the device is now NAMED and bound explicitly to the filters with
// -filter_hw_device. That also stops the encoder inheriting a device it cannot
// use, which was the other candidate for the same failure.
func buildArgs(spec captureSpec, rc recorderConfig, caps *ffmpegCaps, useDDA bool, loop *loopbackCapture) []string {
	args := []string{"-hide_banner", "-loglevel", "warning"}

	// Sound, if this PC turned out to have a device that carries it. Only a
	// device that has already been opened and proven gets this far, because a
	// second input that fails to open takes the WHOLE recording down, not just
	// the audio. See audio_windows.go.
	sound, ready := audioAnswer()
	withAudio := ready && sound.Enabled && (sound.Loopback || sound.Device != "")
	useLoop := loop != nil
	if useLoop {
		withAudio = true
	} else if ready && sound.Loopback {
		// Loopback was the plan and it did not start. Do NOT quietly fall back
		// to a DirectShow device: on this machine that would be a microphone,
		// and a clip of the player's room labelled as game sound is worse than
		// a silent one.
		withAudio = false
	}

	var filters []string
	if useDDA {
		// A named device, optionally pinned to a specific adapter, and handed to
		// the filter graph only.
		dev := "d3d11va=dda"
		if rc.Adapter >= 0 {
			dev = fmt.Sprintf("d3d11va=dda:%d", rc.Adapter)
		}
		args = append(args, "-init_hw_device", dev, "-filter_hw_device", "dda")

		src := fmt.Sprintf("ddagrab=output_idx=%d:framerate=%d", spec.MonitorIndex, spec.FPS)
		// ddagrab hands back GPU frames. Pull them to system memory so the
		// ordinary crop/scale filters can work on them.
		filters = append(filters, src, "hwdownload", "format=bgra")
		if spec.Crop.valid() {
			filters = append(filters, fmt.Sprintf("crop=%d:%d:%d:%d",
				spec.Crop.W, spec.Crop.H, spec.Crop.X, spec.Crop.Y))
		}
	} else {
		// gdigrab captures the named window directly - no crop needed, and
		// nothing outside the game window is ever read. Slower, but it asks
		// nothing of the GPU, which is why it is the reliable fallback.
		args = append(args,
			"-f", "gdigrab",
			"-framerate", itoa(spec.FPS),
			"-i", "title="+spec.WindowTitle,
		)
	}

	// AUDIO GOES LAST, ALWAYS, and the reason is worth writing down. The GPU
	// path needs -init_hw_device before anything else, and it produces its video
	// from a filter source rather than an -i, so it has no video input at all.
	// Putting sound last means the index is knowable in both cases without
	// reading the rest of this function: with the GPU grab the audio is input 0
	// because it is the only input, and with window capture it is input 1.
	//
	// thread_queue_size is the fix for the warning that turns into dropped
	// frames: the audio input runs on its own thread and needs somewhere to put
	// packets while the video thread is busy.
	audioIdx := 0
	if withAudio {
		if !useDDA {
			audioIdx = 1
		}
		if useLoop {
			args = append(args, loop.Format.ffmpegArgs(loop.PipeName)...)
		} else {
			args = append(args,
				"-thread_queue_size", "1024",
				"-f", "dshow",
				"-i", "audio="+sound.Device,
			)
		}
	}

	if spec.HeightCap > 0 && spec.Crop.H > spec.HeightCap {
		filters = append(filters, fmt.Sprintf("scale=-2:%d:flags=bicubic", spec.HeightCap))
	}
	filters = append(filters, "format=yuv420p")

	if useDDA {
		args = append(args, "-filter_complex", strings.Join(filters, ",")+"[v]", "-map", "[v]")
	} else if withAudio {
		// Explicit rather than relying on ffmpeg's default stream selection.
		// Without a map it picks one stream per type from whichever input has
		// one, which happens to be right here and stops being right the moment
		// the input order changes.
		args = append(args, "-map", "0:v", "-filter:v", strings.Join(filters, ","))
	} else {
		args = append(args, "-vf", strings.Join(filters, ","))
	}

	encName, encArgs := encoderArgs(caps, rc)
	_ = encName
	args = append(args, encArgs...)

	if withAudio {
		// aresample=async=1 stretches or pads the audio to keep it against the
		// video clock. A sound card and a screen grab have different ideas about
		// what a second is, and over a twelve minute buffer that difference is
		// audible as drift.
		args = append(args,
			"-map", itoa(audioIdx)+":a",
			"-c:a", "aac", "-b:a", "160k", "-ac", "2", "-ar", "48000",
			"-af", "aresample=async=1",
		)
	} else {
		args = append(args, "-an")
	}

	args = append(args,
		"-g", itoa(spec.FPS*2),
		"-force_key_frames", "expr:gte(t,n_forced*2)",
		"-f", "segment",
		"-segment_time", itoa(spec.SegmentSecs),
		"-segment_format", "mp4",
		"-reset_timestamps", "1",
		"-strftime", "1",
		filepath.Join(spec.SegmentDir, segmentPattern),
	)
	return args
}

// wantDDA decides whether to use the fast GPU path, honouring an explicit
// choice from the player or the capture self-test.
func wantDDA(rc recorderConfig, caps *ffmpegCaps, spec captureSpec) bool {
	switch rc.CaptureMethod {
	case "gdigrab":
		return false
	case "ddagrab":
		return caps.HasDdagrab
	}
	if !caps.HasDdagrab || spec.MonitorIndex < 0 {
		return false
	}
	return true
}

type ffmpegSession struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser

	mu      sync.Mutex
	stopped bool
	running bool
	err     error

	done   chan struct{}
	stderr *lineRing
	label  string

	// loop is the WASAPI capture feeding this session's audio pipe, if there is
	// one. Owned here so it dies with the recording rather than outliving it.
	loop *loopbackCapture
}

func (s *ffmpegSession) Backend() string { return s.label }

func (s *ffmpegSession) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

func (s *ffmpegSession) Wait() error {
	<-s.done
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// Stop asks ffmpeg to finish cleanly by sending "q" on stdin, which makes it
// finalise the segment it is currently writing. A truncated final mp4 has no
// moov atom and is unplayable, so this matters: killing the process outright
// would throw away the most recent - and most likely wanted - few seconds.
func (s *ffmpegSession) Stop() error {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return nil
	}
	s.stopped = true
	stdin := s.stdin
	s.mu.Unlock()

	// The audio pipe closes first. ffmpeg is about to be told to finish, and an
	// input still being written to while it flushes is how a last segment ends
	// up truncated.
	s.loop.Stop()

	if stdin != nil {
		_, _ = io.WriteString(stdin, "q\n")
		_ = stdin.Close()
	}

	select {
	case <-s.done:
		return nil
	case <-time.After(8 * time.Second):
		logf("recorder: ffmpeg did not exit after being asked politely - terminating")
		if s.cmd != nil && s.cmd.Process != nil {
			_ = s.cmd.Process.Kill()
		}
		<-s.done
		return nil
	}
}

func (b ffmpegBackend) Start(spec captureSpec, rc recorderConfig) (captureSession, error) {
	path, err := findFFmpeg(rc)
	if err != nil {
		return nil, err
	}
	caps, err := probeFFmpeg(path)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(spec.SegmentDir, 0o755); err != nil {
		return nil, fmt.Errorf("cannot create buffer folder: %v", err)
	}

	useDDA := wantDDA(rc, caps, spec)
	if !useDDA && spec.WindowTitle == "" {
		return nil, fmt.Errorf("cannot capture: no GPU screen grab available and no window to fall back to")
	}

	// Sound is started BEFORE ffmpeg, and the pipe exists before ffmpeg is told
	// its name. The other order leaves ffmpeg blocked on a pipe nobody is
	// serving, which turns "no audio" into "no recording at all".
	var loop *loopbackCapture
	if sound, ready := audioAnswer(); ready && sound.Enabled && sound.Loopback {
		lc, err := startLoopback()
		if err != nil {
			logf("recorder: game sound is unavailable this session (%v) - recording video only", err)
		} else {
			loop = lc
		}
	}

	args := buildArgs(spec, rc, caps, useDDA, loop)
	cmd := exec.Command(path, args...)
	hideConsole(cmd)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}

	label := "ffmpeg/gdigrab"
	if useDDA {
		label = "ffmpeg/ddagrab"
	}
	encName, _ := encoderArgs(caps, rc)

	s := &ffmpegSession{
		cmd:    cmd,
		stdin:  stdin,
		done:   make(chan struct{}),
		stderr: newLineRing(40),
		label:  label,
	}

	s.loop = loop

	if err := cmd.Start(); err != nil {
		loop.Stop()
		return nil, fmt.Errorf("could not start ffmpeg: %v", err)
	}
	s.mu.Lock()
	s.running = true
	s.mu.Unlock()

	// Put ffmpeg in the kill-on-close job IMMEDIATELY after it starts, before
	// anything else can fail and return early. From this line on, this ffmpeg
	// cannot outlive the app, whatever happens to either of them. See
	// capture_joblimit_windows.go for why that guarantee has to live in the
	// kernel rather than in a shutdown handler.
	if cmd.Process != nil {
		superviseProcess(cmd.Process.Pid)
	}

	adapter := "system default"
	if rc.Adapter >= 0 {
		adapter = itoa(rc.Adapter)
	}
	logf("recorder: capturing via %s, encoder %s, adapter %s, %dx%d @%dfps -> %s",
		label, encName, adapter, spec.Crop.W, spec.Crop.H, spec.FPS, spec.SegmentDir)

	// ffmpeg is chatty on stderr and most of it is noise. Keep the last few lines
	// so that if it dies there is something concrete to put in the log, rather
	// than "exit status 1".
	go func() {
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			s.stderr.add(sc.Text())
		}
	}()

	go func() {
		err := cmd.Wait()
		s.mu.Lock()
		s.running = false
		if err != nil && !s.stopped {
			s.err = fmt.Errorf("ffmpeg exited: %v; last output: %s", err, s.stderr.joined())
		}
		s.mu.Unlock()
		close(s.done)
	}()

	return s, nil
}

// hideConsole stops every ffmpeg launch from flashing a black console window
// over the game. Without it, a rolling buffer that restarts on a resolution
// change would strobe a window on top of somebody's ranked match.
func hideConsole(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}
