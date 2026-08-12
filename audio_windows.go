//go:build windows

// audio_windows.go - finding out whether this PC can record game sound at all.
//
// # THE PROBLEM, STATED HONESTLY
//
// FFmpeg on Windows cannot record what comes out of your speakers. Its only
// Windows audio input is DirectShow, and DirectShow enumerates CAPTURE devices:
// microphones, line inputs, and whatever a sound card chooses to expose. There
// is no WASAPI loopback input device in FFmpeg, so "record the game audio" is
// not a flag that can be switched on.
//
// What CAN work is a capture device that happens to carry the output mix.
// Realtek and some other chipsets expose one called Stereo Mix, usually disabled
// by default. Virtual cables such as VB-Audio and the screen-capture-recorder
// filter provide another. Which of these exists is a property of the machine,
// not something that can be assumed.
//
// So this file does not decide anything. It asks ffmpeg what is present, works
// out whether any of it can carry desktop sound, and reports that plainly. The
// same principle as the capture self-test: on a stranger's PC, find out.
package main

import (
	"strings"
	"sync"
)

// audioDevice is one DirectShow audio input, as ffmpeg reports it.
type audioDevice struct {
	Name     string `json:"name"`
	Loopback bool   `json:"loopback"` // can it carry what the speakers are playing
	Why      string `json:"why"`      // how that was decided, in plain words
}

// loopbackHints are the device names that carry output audio. Matched case
// insensitively against a substring, because vendors decorate these with the
// chipset name: "Stereo Mix (Realtek(R) Audio)".
var loopbackHints = []struct {
	match string
	label string
}{
	{"stereo mix", "the sound card's own output mix"},
	{"what u hear", "Creative's name for the output mix"},
	{"wave out mix", "an older name for the output mix"},
	{"loopback", "a loopback capture device"},
	{"virtual-audio-capturer", "the screen-capture-recorder virtual device"},
	{"cable output", "a VB-Audio virtual cable"},
	{"voicemeeter out", "a VoiceMeeter virtual output"},
}

// classifyAudioDevice decides whether a device can carry game sound.
func classifyAudioDevice(name string) (bool, string) {
	low := strings.ToLower(name)
	for _, h := range loopbackHints {
		if strings.Contains(low, h.match) {
			return true, h.label
		}
	}
	return false, "a microphone or line input, not the game's sound"
}

// parseDshowDevices pulls the audio device names out of ffmpeg's device listing.
//
// ffmpeg prints this to stderr in a loose, human-shaped format that has changed
// wording between versions, so the parse is deliberately forgiving: find a
// quoted name on a line that also mentions audio, and ignore everything else.
func parseDshowDevices(out string) []audioDevice {
	var devs []audioDevice
	seen := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(strings.ToLower(line), "(audio)") {
			continue
		}
		first := strings.Index(line, "\"")
		if first < 0 {
			continue
		}
		rest := line[first+1:]
		last := strings.Index(rest, "\"")
		if last <= 0 {
			continue
		}
		name := strings.TrimSpace(rest[:last])
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		loop, why := classifyAudioDevice(name)
		devs = append(devs, audioDevice{Name: name, Loopback: loop, Why: why})
	}
	return devs
}

// listAudioDevices asks ffmpeg what audio inputs this PC has.
//
// The command is expected to FAIL. "-list_devices true" prints the list and then
// exits with an error because no input was actually opened, so the exit code is
// deliberately ignored and only the text is read.
func listAudioDevices(rc recorderConfig) ([]audioDevice, error) {
	path, err := findFFmpeg(rc)
	if err != nil {
		return nil, err
	}
	out, _ := runFFmpegText(path,
		"-hide_banner", "-list_devices", "true", "-f", "dshow", "-i", "dummy")
	return parseDshowDevices(out), nil
}

// audioVerdict is the one sentence the window shows about sound.
func audioVerdict(devs []audioDevice) (canRecord bool, name string, message string) {
	for _, d := range devs {
		if d.Loopback {
			return true, d.Name, "Game sound can be recorded from " + d.Name + " (" + d.Why + ")."
		}
	}
	if len(devs) == 0 {
		return false, "", "No audio inputs were found at all, so game sound cannot be recorded on this PC yet."
	}
	// This used to end by telling the player to go and enable Stereo Mix, which
	// was correct advice right up until the loopback recorder shipped. After
	// that it appeared in the self-test results two lines below "Sound is being
	// recorded", sending people to hunt through Windows sound settings for a
	// device the app had already stopped needing. Advice that contradicts the
	// screen it is printed on is worse than no advice.
	return false, "", "No audio input on this PC carries game sound, and none is needed - " +
		"Sync records game sound directly from Windows."
}

// ---- proving a device before the recorder depends on it --------------------
//
// # WHY THIS IS NOT OPTIONAL
//
// Audio is a SECOND INPUT to the same ffmpeg process that records the video. If
// that input cannot be opened, ffmpeg does not record silently and carry on. It
// exits. So a bad audio device does not cost you sound, it costs you the whole
// recording, and the recorder's failure backoff would then blame the screen
// grab for a fault in the sound card.
//
// That is the same shape as the encoder bug earlier in this build: a thing that
// was listed, assumed to work, and quietly took something else down with it. So
// the device is opened for a fraction of a second and made to produce audio
// before the recorder is allowed to depend on it.

// audioDeviceUsable opens a DirectShow audio device briefly and reports whether
// it produced anything.
func audioDeviceUsable(rc recorderConfig, name string) (bool, string) {
	path, err := findFFmpeg(rc)
	if err != nil {
		return false, err.Error()
	}
	out, err := runFFmpegText(path,
		"-hide_banner", "-loglevel", "error",
		"-f", "dshow", "-i", "audio="+name,
		"-t", "0.4", "-f", "null", "-")
	if err != nil {
		return false, firstMeaningfulLine(out)
	}
	return true, ""
}

// ---- the warmed audio answer ----------------------------------------------
//
// Same rule as the capture capability cache: the window polls status, and
// anything it polls must read memory only. Enumerating devices launches ffmpeg,
// so it happens once, in the background, and leaves its answer here.

type audioPick struct {
	Enabled bool          // is there a way to record game sound at all
	Device  string        // the DirectShow name, when that is the route
	Note    string        // what to tell the player, in plain words
	Devices []audioDevice // everything found, for the capture test to show

	// Loopback is set when Windows itself will provide the sound, which is the
	// normal case on a machine with no Stereo Mix. It is preferred over a
	// DirectShow device because it needs nothing installed and captures exactly
	// what is being played rather than whatever a sound card chose to expose.
	Loopback bool
}

var (
	audioMu   sync.RWMutex
	audioAns  audioPick
	audioDone bool
	audioOnce sync.Once
)

// warmAudio works out the sound situation once, in the background.
func warmAudio(rc recorderConfig) {
	audioOnce.Do(func() {
		go func() {
			pick := decideAudio(rc)
			audioMu.Lock()
			audioAns = pick
			audioDone = true
			audioMu.Unlock()
			logf("recorder: sound - %s", pick.Note)
		}()
	})
}

// audioAnswer returns the warmed result without ever blocking.
func audioAnswer() (audioPick, bool) {
	audioMu.RLock()
	defer audioMu.RUnlock()
	return audioAns, audioDone
}

// resetAudio forgets the answer so a settings change is picked up. The sync.Once
// is replaced rather than reset, because there is no way to rearm one.
func resetAudio() {
	audioMu.Lock()
	audioAns = audioPick{}
	audioDone = false
	audioOnce = sync.Once{}
	audioMu.Unlock()
}

// decideAudio picks the device to record from, if there is one.
//
// An explicit choice in settings is honoured and still verified, because a
// device that was there last week may be a headset that is now unplugged.
func decideAudio(rc recorderConfig) audioPick {
	if rc.AudioOff {
		return audioPick{Note: "game sound is switched off in settings"}
	}

	devs, err := listAudioDevices(rc)
	if err != nil {
		return audioPick{Note: "could not ask ffmpeg what audio inputs exist: " + err.Error()}
	}

	// Loopback first. It works on every Windows machine, needs no device to
	// exist, and is the only route that captures the game rather than a
	// microphone. A named device in settings still wins, because somebody who
	// picked one meant it.
	if rc.AudioDevice == "" {
		return audioPick{Enabled: true, Loopback: true, Devices: devs,
			Note: "recording game sound directly from Windows, no extra device needed"}
	}

	if rc.AudioDevice != "" {
		if ok, why := audioDeviceUsable(rc, rc.AudioDevice); ok {
			return audioPick{Enabled: true, Device: rc.AudioDevice, Devices: devs,
				Note: "recording sound from " + rc.AudioDevice + ", chosen in settings"}
		} else {
			return audioPick{Devices: devs,
				Note: "the chosen audio device " + rc.AudioDevice + " would not open (" + why +
					"), so this recording has no sound"}
		}
	}

	var rejected []string
	for _, d := range devs {
		if !d.Loopback {
			continue
		}
		if ok, why := audioDeviceUsable(rc, d.Name); ok {
			return audioPick{Enabled: true, Device: d.Name, Devices: devs,
				Note: "recording sound from " + d.Name + ", " + d.Why}
		} else {
			rejected = append(rejected, d.Name+" ("+why+")")
		}
	}

	if len(rejected) > 0 {
		return audioPick{Devices: devs,
			Note: "found a device that should carry game sound but it would not open: " +
				strings.Join(rejected, "; ")}
	}
	_, _, msg := audioVerdict(devs)
	return audioPick{Devices: devs, Note: msg}
}
