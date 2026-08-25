//go:build windows

// probe.go - the capture self-test.
//
// # WHY THIS EXISTS
//
// The first version of the recorder shipped exactly one way of grabbing the
// screen, with no fallback. On the first machine it met that wasn't the
// developer's, it produced no frames, no useful error, and a retry loop that
// hammered the PC for forty minutes. The lesson is not "pick better defaults" -
// it is that screen capture on Windows depends on hardware nobody can predict
// from a config file, so the app has to FIND OUT rather than assume.
//
// So: run several known-good configurations for two seconds each, keep the ones
// that actually produced a video file, and save the winner. It is measurement
// instead of guesswork, and it runs on the player's machine where the answer
// actually lives.
//
// # THE FAILURE THIS WAS BUILT FOR
//
// Laptops with switchable graphics drive the screen from the built-in chip while
// the separate GPU does the work. Asking the wrong chip for the screen returns
// nothing at all - not an error, just silence - and the encoder then fails with
// a message about invalid arguments that sends you looking in entirely the wrong
// place. Testing each adapter in turn settles it in seconds.
package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// probeCandidate is one configuration worth trying.
type probeCandidate struct {
	Label   string `json:"label"`
	Method  string `json:"method"`  // ddagrab | gdigrab
	Adapter int    `json:"adapter"` // -1 = let the system choose
	Encoder string `json:"encoder"` // cpu | nvenc | amf | qsv
}

// probeResult is what happened when we tried it.
type probeResult struct {
	Label   string `json:"label"`
	Method  string `json:"method"`
	Adapter int    `json:"adapter"`
	Encoder string `json:"encoder"`
	OK      bool   `json:"ok"`
	Bytes   int64  `json:"bytes"`
	Detail  string `json:"detail"`
}

// probeSeconds is how long each attempt records for. Two seconds is enough to
// prove frames are flowing and short enough that seven attempts stay under a
// minute.
const probeSeconds = 2

// probeFPS is the frame rate each attempt runs at: the player's own, so the
// encoder is asked for exactly what the recorder will ask for. Clamped only to
// keep a nonsense config from producing a nonsense test.
func probeFPS(rc recorderConfig) int {
	f := rc.FPS
	if f < 10 {
		f = 30
	}
	if f > 240 {
		f = 240
	}
	return f
}

// probeCandidates is stage one: WHICH SCREEN GRAB DELIVERS FRAMES.
//
// Every entry here encodes on the processor, and that is the whole point. The
// first version varied the capture method and the encoder at the same time,
// so when a row failed there was no way to tell which half was at fault - and
// it guessed wrong, out loud, in the results table: four rows blamed "no frames
// arrived" when the frames were fine and the graphics card had refused to
// encode them. One variable at a time, with the reliable encoder held constant.
//
// The adapter variants come first because a laptop with switchable graphics is
// the single most likely cause of a silent failure.
func probeCandidates() []probeCandidate {
	return []probeCandidate{
		{Label: "GPU screen grab, default graphics chip", Method: "ddagrab", Adapter: -1, Encoder: "cpu"},
		{Label: "GPU screen grab, graphics chip 0", Method: "ddagrab", Adapter: 0, Encoder: "cpu"},
		{Label: "GPU screen grab, graphics chip 1", Method: "ddagrab", Adapter: 1, Encoder: "cpu"},
		{Label: "Window capture", Method: "gdigrab", Adapter: -1, Encoder: "cpu"},
	}
}

// encoderCandidates lists the hardware encoders worth trying, in the order that
// makes sense to a person reading the results.
//
// Only encoders that already PROVED they can run on this machine appear here -
// see encoderUsable. What is still unknown at this point is whether they will
// accept frames from a particular screen grab, which is what stage two settles.
func encoderCandidates(caps *ffmpegCaps) []struct{ encoder, label string } {
	var out []struct{ encoder, label string }
	if caps.HasNVENC {
		out = append(out, struct{ encoder, label string }{"nvenc", "NVIDIA encoding"})
	}
	if caps.HasAMF {
		out = append(out, struct{ encoder, label string }{"amf", "AMD encoding"})
	}
	if caps.HasQSV {
		out = append(out, struct{ encoder, label string }{"qsv", "Intel encoding"})
	}
	return out
}

// probeArgs builds a short, single-file recording rather than the segmented
// rolling buffer. Deliberately minimal: no keyframe forcing, no segmenting, no
// cropping. Every extra option is another thing that could fail for an
// unrelated reason and send the diagnosis sideways.
//
// IT ENCODES AT THE PLAYER'S REAL HEIGHT AND FRAME RATE, AND THAT IS THE POINT.
// The first version scaled every attempt to 480p at 30fps. On a machine
// recording 2400x1350 at 60, h264_nvenc opened happily at 480p and then refused
// -22 "Could not open encoder" on the real thing, so the test passed, the
// recorder failed three times and stopped, and the only apparent cure was
// running the test again - which fixed nothing and merely cleared the strike
// count. A check that does not exercise what the recorder actually does is not
// a check. Same lesson as the installer size threshold: ask the tool, at the
// real settings, or do not ask at all.
func probeArgs(c probeCandidate, rc recorderConfig, caps *ffmpegCaps, out string) []string {
	args := []string{"-hide_banner", "-loglevel", "error"}

	var filters []string
	if c.Method == "ddagrab" {
		dev := "d3d11va=dda"
		if c.Adapter >= 0 {
			dev = fmt.Sprintf("d3d11va=dda:%d", c.Adapter)
		}
		args = append(args, "-init_hw_device", dev, "-filter_hw_device", "dda")
		filters = append(filters, fmt.Sprintf("ddagrab=output_idx=0:framerate=%d", probeFPS(rc)), "hwdownload", "format=bgra")
	} else {
		// "desktop" rather than a window title, because Siege is usually closed
		// while somebody is sitting in the settings screen running this test.
		args = append(args, "-f", "gdigrab", "-framerate", itoa(probeFPS(rc)), "-i", "desktop")
	}
	if rc.HeightCap > 0 {
		filters = append(filters, fmt.Sprintf("scale=-2:%d:flags=bicubic", rc.HeightCap))
	}
	filters = append(filters, "format=yuv420p")

	if c.Method == "ddagrab" {
		args = append(args, "-filter_complex", strings.Join(filters, ",")+"[v]", "-map", "[v]")
	} else {
		args = append(args, "-vf", strings.Join(filters, ","))
	}

	probeRC := rc
	probeRC.Encoder = c.Encoder
	if c.Encoder == "auto" {
		probeRC.Encoder = "auto"
	}
	_, encArgs := encoderArgs(caps, probeRC)
	args = append(args, encArgs...)

	args = append(args, "-t", itoa(probeSeconds), "-an", "-y", out)
	return args
}

// runCaptureProbe tries every candidate and reports what happened.
//
// Returns the full result list (so the window can show the whole picture, not
// just the verdict) and the first configuration that worked, or nil.
func runCaptureProbe(rc recorderConfig) ([]probeResult, *probeCandidate, error) {
	path, err := findFFmpeg(rc)
	if err != nil {
		return nil, nil, err
	}
	caps, err := probeFFmpeg(path)
	if err != nil {
		return nil, nil, err
	}

	dir := filepath.Join(configDir(), "capture-test")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, nil, err
	}
	defer os.RemoveAll(dir)

	seq := 0
	try := func(c probeCandidate) probeResult {
		seq++
		return runOneProbe(path, dir, seq, c, rc, caps)
	}

	var results []probeResult
	var working []probeCandidate

	// ---- stage one: find every screen grab that produces frames ------------
	logf("capture test: stage 1 - finding a screen grab that produces frames")
	for _, c := range probeCandidates() {
		if c.Method == "ddagrab" && !caps.HasDdagrab {
			results = append(results, probeResult{
				Label: c.Label, Method: c.Method, Adapter: c.Adapter, Encoder: c.Encoder,
				Detail: "this ffmpeg build has no GPU screen grab filter",
			})
			continue
		}
		res := try(c)
		results = append(results, res)
		if res.OK {
			working = append(working, c)
		}
	}

	if len(working) == 0 {
		logf("capture test: nothing worked - every screen grab failed")
		return results, nil, nil
	}

	// ---- stage two: can the graphics card encode any of them? --------------
	//
	// EVERY working capture path is tried, not just the first, and that is the
	// point rather than thoroughness for its own sake. A hardware encoder does
	// not fail or succeed on its own - it fails against a PARTICULAR graphics
	// device. AMD encoding refusing the default chip says nothing about whether
	// it would accept chip 0, and the version that only tested the first winner
	// reported "AMD encoding does not work here" on the strength of one
	// combination out of three.
	winner := &working[0]
	encs := encoderCandidates(caps)

	if len(encs) == 0 {
		logf("capture test: stage 2 skipped - no hardware encoder is usable on this PC")
		for _, why := range []struct{ name, reason string }{
			{"NVIDIA", caps.NVENCWhy}, {"AMD", caps.AMFWhy}, {"Intel", caps.QSVWhy},
		} {
			if why.reason != "" {
				results = append(results, probeResult{
					Label:   why.name + " encoding",
					Encoder: "hardware",
					Adapter: -1,
					Detail:  "not available on this PC - " + why.reason,
				})
			}
		}
	} else {
		logf("capture test: stage 2 - %d hardware encoder(s) against %d working screen grab(s)",
			len(encs), len(working))
	found:
		for _, base := range working {
			for _, e := range encs {
				c := base
				c.Encoder = e.encoder
				c.Label = base.Label + ", " + e.label
				res := try(c)
				results = append(results, res)
				if !res.OK {
					continue
				}
				// Hardware encoding BEATS the processor, because it is the
				// difference between costing the player frames in game and not.
				// First hit wins: stage one is ordered best-first, so the
				// earliest working pair is also the preferred one.
				//
				// ONE EXCEPTION, and it is not a detail. Window capture cannot
				// see a game running in true fullscreen - it returns black. So a
				// hardware encoder that only works with window capture is NOT
				// worth dropping the GPU screen grab for: a lighter encoder is
				// no use attached to a capture path that records nothing. When
				// that happens, keep the GPU grab and encode on the processor.
				if c.Method == "gdigrab" && winner.Method == "ddagrab" {
					logf("capture test: %s works, but window capture cannot see fullscreen games - keeping the GPU screen grab", c.Label)
					continue
				}
				pick := c
				winner = &pick
				break found
			}
		}
	}

	// ---- sound: DOES THE PATH THAT ACTUALLY RECORDS IT WORK? --------------
	//
	// IT NOW TESTS THE RECORDER'S OWN CAPTURE, NOT A DEVICE LIST.
	//
	// Until 2026-08-25 this row asked Windows for dshow INPUT devices and
	// reported that the machine had none carrying game sound. That is true of
	// nearly every PC and has nothing to do with how Sync records: it takes the
	// sound straight off the playback device in loopback. So the row sat in a
	// column of green "works" ticks, on a machine whose sound was demonstrably
	// fine, saying something that reads as a failure. The message even ended
	// with "and none is needed", which nobody reaches after the first half has
	// already told them something is wrong.
	//
	// Starting the real loopback and stopping it again is one second of work and
	// it is the only answer worth printing. Same principle as asking the
	// compiler whether the recorder is in the installer instead of guessing from
	// the file size, and as reporting the running capture rather than the
	// setting: measure the thing, do not describe your intentions about it.
	if lb, err := startLoopback(); err == nil {
		msg := fmt.Sprintf("Game sound is being recorded straight from Windows, %d Hz, %d channel(s). No microphone or input device is involved.",
			lb.Format.SampleRate, lb.Format.Channels)
		lb.Stop()
		logf("capture test: sound - %s", msg)
		results = append(results, probeResult{
			Label: "Sound", Encoder: "audio", Adapter: -1, OK: true, Detail: msg,
		})
	} else {
		msg := "Game sound could not be recorded on this PC: " + err.Error()
		logf("capture test: sound - %s", msg)
		results = append(results, probeResult{
			Label: "Sound", Encoder: "audio", Adapter: -1, OK: false, Detail: msg,
		})
	}

	// The input devices stay listed BELOW the verdict, and only as information.
	// They are not what records the game, so none of them gets an OK or a cross -
	// a row with no verdict beside a row that has one is how a reader tells
	// "this is context" from "this is the answer".
	if devs, err := listAudioDevices(rc); err == nil {
		for _, d := range devs {
			results = append(results, probeResult{
				Label:   "Also on this PC: " + d.Name,
				Encoder: "audio",
				Adapter: -1,
				Detail:  d.Why,
			})
		}
	}

	logf("capture test: winner is %q (method=%s adapter=%d encoder=%s)",
		winner.Label, winner.Method, winner.Adapter, winner.Encoder)
	return results, winner, nil
}

// runOneProbe runs a single configuration for two seconds and reports what
// happened - including, when it fails, what ffmpeg actually said.
//
// The verdict is still the FILE, not the exit code: ffmpeg can exit non-zero
// having written good video, and exit zero having written an empty container.
// But the exit code and the error text decide HOW the failure is described,
// which is the part that was previously invented.
func runOneProbe(path, dir string, seq int, c probeCandidate, rc recorderConfig, caps *ffmpegCaps) probeResult {
	res := probeResult{Label: c.Label, Method: c.Method, Adapter: c.Adapter, Encoder: c.Encoder}

	out := filepath.Join(dir, fmt.Sprintf("test%d.mp4", seq))
	cmd := exec.Command(path, probeArgs(c, rc, caps, out)...)
	hideConsole(cmd)

	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf

	if err := cmd.Start(); err != nil {
		res.Detail = "could not start ffmpeg: " + err.Error()
		return res
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	var runErr error
	select {
	case runErr = <-done:
	case <-time.After(15 * time.Second):
		_ = cmd.Process.Kill()
		<-done
		res.Detail = "timed out"
		logf("capture test: %s -> timed out", c.Label)
		return res
	}

	if st, err := os.Stat(out); err == nil && st.Size() > 1024 {
		res.OK = true
		res.Bytes = st.Size()
		res.Detail = fmt.Sprintf("produced %d KB of video", st.Size()/1024)
		logf("capture test: %s -> WORKS", c.Label)
		return res
	}

	why := firstMeaningfulLine(errBuf.String())
	switch {
	case runErr != nil && why != "no reason given":
		res.Detail = why
	case runErr != nil:
		res.Detail = "ffmpeg stopped early: " + runErr.Error()
	default:
		res.Detail = "ran, but produced an empty file - no frames arrived"
	}
	logf("capture test: %s -> %s", c.Label, res.Detail)
	return res
}

// applyProbeWinner saves the working configuration so the recorder uses it from
// now on, and clears the failure counter so it will actually try again.
//
// # SAVING IS NOT APPLYING, AND THAT COST A WHOLE EVENING
//
// This used to write the winner to the config file, hand it to the recorder,
// and stop there. What it never did was stop the capture that was ALREADY
// RUNNING - and a running ffmpeg has its command line baked in at launch, so it
// carried on grabbing the screen exactly the way it had been before the test.
//
// The result was the worst kind of bug, the kind that reports success. The test
// correctly found that the GPU screen grab worked. It correctly saved it. The
// window correctly displayed "GPU screen grab". And the recorder correctly kept
// running window capture for another twenty minutes, filling the buffer with
// black frames, because nothing ever told it to restart. The only way out was
// to quit the app, which is not something a player should have to discover.
//
// So: if the test changed how frames are grabbed, the current capture is now
// wrong by definition and gets stopped. The recorder's own loop starts a fresh
// one within a few seconds using the settings that just won.
func applyProbeWinner(c probeCandidate) error {
	var cfg config
	loadJSON(configPath(), &cfg)
	cfg.Recorder.normalise()

	was := cfg.Recorder
	cfg.Recorder.CaptureMethod = c.Method
	cfg.Recorder.Adapter = c.Adapter
	// Save whatever won, hardware or not. The old version only ever wrote "cpu"
	// and left everything else on "auto", which meant a proven-good graphics
	// card encoder was thrown away and re-guessed on the next launch.
	switch c.Encoder {
	case "cpu", "nvenc", "amf", "qsv":
		cfg.Recorder.Encoder = c.Encoder
	}
	saveJSON(configPath(), &cfg)

	rec.configure(&cfg)
	rec.clearFailures()
	logf("recorder: capture settings updated from the self-test - method=%s adapter=%d encoder=%s",
		c.Method, c.Adapter, cfg.Recorder.Encoder)

	if was.CaptureMethod != cfg.Recorder.CaptureMethod ||
		was.Adapter != cfg.Recorder.Adapter ||
		was.Encoder != cfg.Recorder.Encoder {
		logf("recorder: the way frames are grabbed has changed (%s/adapter %d/%s -> %s/adapter %d/%s), so the current recording is being restarted to use it",
			was.CaptureMethod, was.Adapter, was.Encoder,
			cfg.Recorder.CaptureMethod, cfg.Recorder.Adapter, cfg.Recorder.Encoder)
		rec.stopCapture("the capture self-test chose a different way of grabbing the screen")
	}
	return nil
}
