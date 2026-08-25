// ui_api.go - everything the app window asks for, and everything it can do.
//
// This file deliberately knows nothing about WebView2, windows, or HTML. It
// exposes plain Go functions returning plain data. The window layer binds them;
// swapping the window technology later touches ui_window_windows.go and nothing
// in here.
//
// It is also the reason the recorder's settings finally have somewhere sensible
// to live. Four recording modes and seven keep rules nested two deep in a
// right-click menu was already past what a menu should carry.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ---- bridge tracing --------------------------------------------------------
//
// WHY THIS IS HERE
//
// The window sat on "Connecting..." through two rounds of fixes because a call
// from the page into Go never came back, and there was no way to see WHICH call
// or HOW FAR it got. Two different theories were built and shipped on that
// absence of evidence, and both were wrong.
//
// So every bridge call now writes its start and its duration to the log. It is
// noisy for the first dozen calls and then goes quiet, only speaking up again
// when something takes longer than it should. Cheap, permanent, and it converts
// "the window is stuck" into a line naming the function and the millisecond it
// stopped at.

var bridgeSeq int32

// jsonOf turns any value into a JSON string for the bridge.
//
// EVERY bridge function returns a plain string, and this is why.
//
// On 9 August 2026 the window sat dead through three rounds of fixes. The log
// finally showed that functions taking no arguments and returning a struct were
// never entered AT ALL, while a function taking a string and returning a string
// worked perfectly on the same bridge, in the same window, in the same second.
// Rather than fight whatever the binding layer does with those signatures, every
// call now uses the shape that is proven to work on real hardware, and the
// structure travels as text.
//
// It costs one JSON.parse on the page. That is a small price for a bridge that
// actually delivers.
func jsonOf(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		logf("bridge: could not encode a reply: %v", err)
		return ""
	}
	return string(b)
}

// traceBridge marks the start of a bridge call and returns the function that
// marks its end. Use as: defer traceBridge("status")()
func traceBridge(name string) func() {
	n := atomic.AddInt32(&bridgeSeq, 1)
	start := time.Now()
	verbose := n <= 12
	if verbose {
		logf("bridge: %s START (call %d)", name, n)
	}
	return func() {
		d := time.Since(start)
		if verbose || d > 250*time.Millisecond {
			logf("bridge: %s finished in %s", name, d.Round(time.Millisecond))
		}
	}
}

// traceStep logs a checkpoint inside a slow call, but only for the first few
// calls, so the log shows the shape of a startup without growing forever.
func traceStep(step string) {
	if atomic.LoadInt32(&bridgeSeq) <= 12 {
		logf("bridge:   ... %s", step)
	}
}

// uiStatus is the top panel: what is happening right now, in one object.
type uiStatus struct {
	AppVersion string `json:"app_version"`

	Recording  bool   `json:"recording"`
	StatusLine string `json:"status_line"`
	Backend    string `json:"backend"`

	SiegeRunning   bool `json:"siege_running"`
	RecorderPaused bool `json:"recorder_paused"`
	SyncPaused     bool `json:"sync_paused"`
	Linked         bool `json:"linked"`

	Mode     string `json:"mode"`
	KeepRule string `json:"keep_rule"`

	// Armed only means anything in ModeManual. It was previously settable ONLY
	// from the tray menu, which is how somebody ends up watching a buffer sit at
	// zero with no indication that the app is waiting to be told to start. The
	// window now shows the state and offers the switch.
	Armed bool `json:"armed"`

	// Stalled means we believe we are capturing and no bytes are arriving.
	Stalled bool `json:"stalled"`

	// Fullscreen means both capture paths produced nothing while Siege was
	// running, which has one cause and one fix worth putting on screen.
	Fullscreen bool `json:"fullscreen"`

	// Clips currently being compressed or uploaded, so the window can show
	// progress instead of appearing to have done nothing.
	Sending []sendJob `json:"sending,omitempty"`

	BufferFrom    string `json:"buffer_from"`
	BufferTo      string `json:"buffer_to"`
	BufferMinutes int    `json:"buffer_minutes"`
	BufferMB      int64  `json:"buffer_mb"`
	BufferCapMB   int    `json:"buffer_cap_mb"`

	ClipCount  int    `json:"clip_count"`
	ClipsMB    int64  `json:"clips_mb"`
	ClipsCapMB int    `json:"clips_cap_mb"`
	ClipDir    string `json:"clip_dir"`

	CaptureReady   bool   `json:"capture_ready"`
	CaptureProblem string `json:"capture_problem"`
	FFmpegVersion  string `json:"ffmpeg_version"`
	Encoder        string `json:"encoder"`
	HasDdagrab     bool   `json:"has_ddagrab"`

	// HasGfxCapture is the one that changes what a player can DO: on this path
	// recording carries on while Siege is behind another window, so the window
	// says so rather than leaving them to discover it.
	HasGfxCapture bool `json:"has_gfxcapture"`

	// GaveUp is set once the recorder has stopped retrying a capture that keeps
	// failing. Surfacing it is the whole point: the first version said
	// "Recording" while producing nothing at all, which is the worst possible
	// combination of confident and wrong.
	GaveUp        bool   `json:"gave_up"`
	CaptureError  string `json:"capture_error"`
	CaptureMethod string `json:"capture_method"`

	// CaptureLive is how frames are being grabbed RIGHT NOW, empty when nothing
	// is capturing. CaptureMethod above is only what the settings file asks for,
	// and the two disagreeing is exactly the state that hid a black recording
	// behind a card reading "GPU screen grab".
	CaptureLive string `json:"capture_live"`
	Adapter     int    `json:"adapter"`

	// Sound. AudioNote is the whole story in one sentence, because "no audio"
	// with no reason attached is the kind of thing that generates support
	// questions nobody can answer.
	// ClipUploadLive is false while the backend has no clip endpoint. The window
	// uses it to hide the Send button rather than offer an action that is known
	// to fail - a control that always errors teaches people to distrust every
	// other control next to it.
	ClipUploadLive bool `json:"clip_upload_live"`

	// The window reads these once, on the first status, and applies them as
	// attributes on the root element. Absent means the default look.
	UITheme   string `json:"ui_theme,omitempty"`
	UIFont    string `json:"ui_font,omitempty"`
	UIDensity string `json:"ui_density,omitempty"`
	UIClipView string `json:"ui_clip_view,omitempty"`

	AudioOn     bool   `json:"audio_on"`
	AudioDevice string `json:"audio_device"`
	AudioNote   string `json:"audio_note"`
}

// uiClip is one row in the clip list.
type uiClip struct {
	Path  string `json:"path"`
	Name  string `json:"name"`
	Thumb string `json:"thumb"`

	// ThumbPending says a thumbnail is being made right now, in the background.
	// The window uses it to come back and look again. Without it the picture
	// never appeared at all: thumbnails were generated after the reply had been
	// sent, and nothing on the page ever asked a second time - so a freshly
	// saved clip showed "no preview" until something unrelated forced a refresh.
	ThumbPending bool     `json:"thumb_pending,omitempty"`
	Match        string   `json:"match"`
	Round        int      `json:"round"`
	Label        string   `json:"label"`
	Reason       string   `json:"reason"`
	KeepRule     string   `json:"keep_rule"`
	DurationSec  float64  `json:"duration_sec"`
	SizeMB       float64  `json:"size_mb"`
	Created      string   `json:"created"`
	Estimated    bool     `json:"estimated"`
	Gaps         []string `json:"gaps,omitempty"`
}

// apiStatus gathers the live picture. Cheap enough to poll every couple of
// seconds from the window.
func apiStatus() uiStatus {
	defer traceBridge("status")()

	traceStep("reading recorder settings")
	rc := rec.settings()

	s := uiStatus{
		AppVersion:     version,
		Recording:      rec.capturing(),
		StatusLine:     rec.currentStatus(),
		Backend:        rec.backendName(),
		SiegeRunning:   siegeRunning(),
		RecorderPaused: recorderIsPaused(),
		SyncPaused:     syncIsPaused(),
		Mode:           rc.Mode,
		KeepRule:       rc.KeepRule,
		Armed:          rec.isArmed(),
		BufferMinutes:  rc.BufferMinutes,
		BufferCapMB:    rc.BufferDiskMB,
		ClipsCapMB:     rc.ClipDiskMB,
		ClipDir:        rc.ClipDir,
	}

	traceStep("reading config file")
	var cfg config
	loadJSON(configPath(), &cfg)
	s.Linked = cfg.DeviceToken != ""
	s.UITheme, s.UIFont, s.UIDensity = cfg.UITheme, cfg.UIFont, cfg.UIDensity
	s.UIClipView = cfg.UIClipView

	traceStep("checking capture trouble")
	s.GaveUp, s.CaptureError = rec.captureTrouble()
	s.CaptureMethod = rc.CaptureMethod

	// What is running, not what is configured. When a session is live its own
	// label wins, because that is the thing producing the frames. See
	// recorder.liveBackend for why this distinction earned its own field.
	switch rec.liveBackend() {
	case "ffmpeg/ddagrab":
		s.CaptureLive = "ddagrab"
	case "ffmpeg/gdigrab":
		s.CaptureLive = "gdigrab"
	case "ffmpeg/gfxcapture":
		s.CaptureLive = "gfxcapture"
	}

	// "Recording" used to mean "the ffmpeg process is alive", which is not the
	// same claim and on this machine was the wrong one. If we believe we are
	// capturing but nothing has reached the disk, say so here rather than
	// letting the card read Live over an empty buffer. The watchdog in
	// capture_watchdog.go acts on this a few seconds later; this is the window
	// telling the truth in the meantime.
	s.Stalled = rec.stalledFor() > 0
	s.Sending = sendSnapshot()
	s.Fullscreen = rec.inFullscreenTrap()

	// Memory only. warmAudio did the process launches once, in the background,
	// which is the rule this whole status call lives by.
	s.ClipUploadLive = !clipEndpointKnownMissing()

	traceStep("reading the warmed sound answer")
	if pick, done := audioAnswer(); done {
		s.AudioOn = pick.Enabled
		s.AudioDevice = pick.Device
		s.AudioNote = pick.Note
	} else {
		s.AudioNote = "checking what this PC can record sound from..."
	}
	s.Adapter = rc.Adapter

	if from, to, ok := bufferSpan(rc.bufferDir()); ok {
		s.BufferFrom = from.Format("15:04:05")
		s.BufferTo = to.Format("15:04:05")
	}
	traceStep("measuring the buffer folder")
	s.BufferMB = dirSizeMB(rc.bufferDir())

	traceStep("listing clips")
	if clips, err := listClips(rc.ClipDir); err == nil {
		s.ClipCount = len(clips)
		var total int64
		for _, c := range clips {
			total += c.Size
		}
		s.ClipsMB = total / (1024 * 1024)
	}

	// Report the capture engine from the WARMED cache. This function is polled
	// every couple of seconds by the window, so it must never launch a process,
	// touch the network, or take a lock that something slow might be holding.
	// An earlier version of these six lines probed ffmpeg here and jammed the
	// entire interface; see the note in capture_ffmpeg.go.
	traceStep("reading the warmed capture answer")
	caps, capErr, done := captureCaps()
	switch {
	case !done:
		s.CaptureProblem = "checking what this PC can do..."
	case caps == nil:
		s.CaptureProblem = capErr
	default:
		s.CaptureReady = true
		s.FFmpegVersion = caps.Version
		s.HasDdagrab = caps.HasDdagrab
		s.HasGfxCapture = caps.HasGfxCapture
		s.Encoder, _ = encoderArgs(caps, rc)
		if !caps.HasDdagrab && !caps.HasGfxCapture {
			s.CaptureProblem = "this ffmpeg build has no GPU screen grab - falling back to slower window capture"
		}
	}

	return s
}

// apiSettings hands the window the current recorder settings.
func apiSettings() recorderConfig {
	defer traceBridge("settings")()
	return rec.settings()
}

// apiSaveSettings takes the whole settings object back as JSON. Whole-object
// rather than key-by-key on purpose: normalise() then gets to see every field
// together, so one silly value cannot leave the rest in a half-applied state.
// soundPrefs is the small block of notification settings that live on the
// config rather than inside the recorder block, kept separate so the recorder
// settings shape does not have to change.
type soundPrefs struct {
	OKFile   string `json:"sound_ok_file"`
	FailFile string `json:"sound_fail_file"`
	ClipFile string `json:"sound_clip_file"`
}

// apiArm turns manual recording on or off from the app window.
//
// # WHY THIS EXISTS
//
// "Only when I arm it" was reachable from the tray menu and nowhere else, and
// the tray menu is not where somebody looks when the window in front of them
// says the buffer is 0 MB. The result was a recorder that was working exactly
// as designed and looked completely broken. Any mode a player can select must
// have its controls in the same place they selected it.
func apiArm(on string) string {
	defer traceBridge("arm")()
	want := on == "on" || on == "true" || on == "1"
	rec.setArmed(want)
	return ""
}

func apiSoundPrefs() soundPrefs {
	defer traceBridge("soundPrefs")()
	var c config
	loadJSON(configPath(), &c)
	return soundPrefs{OKFile: c.SoundOKFile, FailFile: c.SoundFailFile, ClipFile: c.SoundClipFile}
}

// apiSaveSoundPrefs stores the chosen files and plays the one that changed, so
// picking a sound proves itself immediately instead of at the end of the next
// match.
func apiSaveSoundPrefs(raw string) string {
	defer traceBridge("saveSoundPrefs")()
	var in soundPrefs
	if err := json.Unmarshal([]byte(raw), &in); err != nil {
		return "could not read those settings: " + err.Error()
	}
	var c config
	loadJSON(configPath(), &c)
	changedClip := in.ClipFile != c.ClipFileOrEmpty()
	c.SoundOKFile = strings.TrimSpace(in.OKFile)
	c.SoundFailFile = strings.TrimSpace(in.FailFile)
	c.SoundClipFile = strings.TrimSpace(in.ClipFile)
	saveJSON(configPath(), &c)
	logf("sound: files set - ok=%q fail=%q clip=%q", c.SoundOKFile, c.SoundFailFile, c.SoundClipFile)
	if changedClip {
		beepClip(true)
	} else {
		beep(true)
	}
	return ""
}

// sharePrefs is the settings-sharing block the app window reads and writes.
// Kept separate from recorderConfig on purpose: these live on config, not on the
// recorder, and folding them in would mean a recorder settings save could
// silently rewrite a consent switch.
type sharePrefs struct {
	AimOn    bool   `json:"aim_on"`
	PerfOn   bool   `json:"perf_on"`
	MouseDPI int    `json:"mouse_dpi"`
	File     string `json:"file"`
}

func apiSharePrefs() sharePrefs {
	defer traceBridge("sharePrefs")()
	var c config
	loadJSON(configPath(), &c)
	return sharePrefs{
		AimOn:    c.SettingsAimOn,
		PerfOn:   c.SettingsPerfOn,
		MouseDPI: c.MouseDPI,
		// The real path, shown in the window. Somebody deciding whether to switch
		// this on should be able to see the exact file rather than a description
		// of it, and an empty string here is the honest answer when Siege has
		// never been run on this machine.
		File: gameSettingsPath(c.ReplayDir),
	}
}

// apiSaveSharePrefs stores the two consent switches and the DPI, and mirrors the
// switches into the atomics the tray and the poller read, so the tray checkbox
// and this window can never end up disagreeing.
func apiSaveSharePrefs(raw string) string {
	defer traceBridge("saveSharePrefs")()
	var in sharePrefs
	if err := json.Unmarshal([]byte(raw), &in); err != nil {
		return "could not read those settings: " + err.Error()
	}
	// A DPI outside this range is a typo, not a mouse. Clamping to zero rather
	// than rejecting keeps the save working and simply leaves eDPI blank.
	if in.MouseDPI < 100 || in.MouseDPI > 32000 {
		in.MouseDPI = 0
	}
	setSettingsAimOn(in.AimOn)
	setSettingsPerfOn(in.PerfOn)
	saveSettingsSharePrefs(in.MouseDPI)
	logf("settings sharing: aim=%v perf=%v dpi=%d (from the app window)", in.AimOn, in.PerfOn, in.MouseDPI)
	return ""
}

// apiOpenSettingsFile shows the player the file itself. The strongest answer to
// "what are you reading" is the actual bytes.
func apiOpenSettingsFile(string) string {
	defer traceBridge("openSettingsFile")()
	p := settingsFileForPlayer()
	if p == "" {
		return "Could not find your Siege settings file. Launch Siege once and try again."
	}
	openInNotepad(p)
	return ""
}

func apiSaveSettings(raw string) string {
	var incoming recorderConfig
	if err := json.Unmarshal([]byte(raw), &incoming); err != nil {
		// Silent since 2026-08-22. The window is open and already says it failed.
		return "could not read those settings: " + err.Error()
	}
	incoming.normalise()

	var cfg config
	loadJSON(configPath(), &cfg)
	cfg.Recorder = incoming
	saveJSON(configPath(), &cfg)

	rec.configure(&cfg)
	// Any settings change is a fresh reason to believe capture might work, so
	// the give-up state is cleared rather than leaving the player stuck with a
	// recorder that refuses to try after they have fixed the cause.
	rec.clearFailures()

	// The sound answer is stale the moment audio settings change, and it must be
	// worked out again off the bridge thread rather than here. warmAudio does
	// the work in the background; resetAudio is what lets it run a second time.
	resetAudio()
	warmAudio(incoming)

	logf("recorder: settings changed from the app window - %s", recorderSummary(incoming))

	// A mode change should take effect now, not at the next five-second tick.
	if incoming.Mode == ModeOff {
		rec.stopCapture("recorder switched off from the app window")
		clearBuffer(incoming.bufferDir())
	}

	// Silent since 2026-08-22. Saving a setting is the app doing as it was told,
	// and the player is looking at the window when it happens.
	return ""
}

// apiClips lists finished clips, newest first, generating any missing
// thumbnails as it goes.
//
// Thumbnail generation is capped per call. A player with six hundred clips
// should not wait for six hundred ffmpeg launches to see the window; the ones
// off screen fill in on the next refresh.
func apiClips() []uiClip {
	defer traceBridge("clips")()
	rc := rec.settings()
	entries, err := listClips(rc.ClipDir)
	if err != nil {
		return nil
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].Mod.After(entries[j].Mod) })

	const maxThumbsPerCall = 12
	made := 0
	var missingThumbs []string

	out := make([]uiClip, 0, len(entries))
	for _, e := range entries {
		c := uiClip{
			Path:    e.Path,
			Name:    filepath.Base(e.Path),
			SizeMB:  float64(e.Size) / (1024 * 1024),
			Created: e.Mod.Format("2 Jan, 15:04"),
		}

		sidecar := strings.TrimSuffix(e.Path, filepath.Ext(e.Path)) + ".json"
		if b, err := os.ReadFile(sidecar); err == nil {
			var m clipMeta
			if json.Unmarshal(b, &m) == nil {
				c.Match = m.MatchFolder
				c.Round = m.RoundIndex
				c.Label = m.Label
				c.Reason = m.Reason
				c.KeepRule = m.KeepRule
				c.DurationSec = m.DurationSec
				c.Estimated = m.BoundariesEstimated
				c.Gaps = m.Gaps
			}
		}

		// Only ever use a thumbnail that ALREADY exists here. Making one runs
		// ffmpeg, and this function is called from the window's load path -
		// exactly the sort of slow work that must never sit between the page
		// and its first paint. Missing thumbnails are filled in afterwards.
		if t := thumbPath(e.Path); fileExists(t) {
			c.Thumb = fileURL(t)
		} else if made < maxThumbsPerCall {
			made++
			c.ThumbPending = true
			missingThumbs = append(missingThumbs, e.Path)
		}

		out = append(out, c)
	}

	// Generate any missing thumbnails in the background. They appear on the next
	// refresh, and the window never waits on them.
	if len(missingThumbs) > 0 {
		go func(paths []string) {
			for _, p := range paths {
				ensureThumb(rc, p)
			}
		}(missingThumbs)
	}
	return out
}

// apiPlayClip hands the clip to whatever the player normally watches video in.
// Playing it inside the window was considered and rejected: their own player has
// the scrubbing, speed controls and keyboard shortcuts they already know.
func apiPlayClip(path string) string {
	if !underClipDir(path) {
		return "that file is not in your clips folder"
	}
	if !fileExists(path) {
		return "that clip is no longer on disk"
	}
	openURL(path)
	return ""
}

func apiOpenClipFolder() string {
	rec.openClipFolder()
	return ""
}

// apiDeleteClip removes a clip and everything that belongs to it.
//
// Guarded by underClipDir on purpose. This function is reachable from
// JavaScript, and a delete-by-path with no boundary check is the single most
// dangerous thing in the whole window.
func apiDeleteClip(path string) string {
	if !underClipDir(path) {
		return "that file is not in your clips folder"
	}
	if err := os.Remove(path); err != nil {
		return "could not delete it: " + err.Error()
	}
	base := strings.TrimSuffix(path, filepath.Ext(path))
	_ = os.Remove(base + ".json")
	_ = os.Remove(base + ".jpg")
	logf("recorder: %s deleted from the app window", filepath.Base(path))
	return ""
}

// apiSendClip pushes one clip to SiegeIQ by hand.
func apiSendClip(path string) string {
	if !underClipDir(path) {
		return "that file is not in your clips folder"
	}
	var cfg config
	loadJSON(configPath(), &cfg)
	if cfg.DeviceToken == "" {
		return "this device is not linked to a SiegeIQ account yet"
	}

	if sendInFlight(path) {
		return "that clip is already being sent"
	}
	// Returns the moment the job is started. Doing the work here would run it
	// on the window's message-loop thread, which is what froze the whole
	// application for five minutes. See clipsend.go.
	startSend(cfg, rec.settings(), path, clipKindAI, "manual")
	return ""
}

// apiSaveLast writes out the last N minutes of the buffer right now.
func apiSaveLast(raw string) string {
	defer traceBridge("saveLast")()
	// The ceiling is the buffer's own length, because nothing longer than that
	// exists to be cut. It used to be a hard 30, and anything above it fell back
	// to TWO MINUTES - so a "whole match" button would have quietly handed back
	// a two minute clip and looked like it worked. Out of range now clamps to
	// the most that can actually be produced, and says so in the log.
	rc := rec.settings()
	maxMin := float64(rc.BufferMinutes)
	if maxMin < 1 {
		maxMin = 1
	}
	minutes, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || minutes <= 0 {
		minutes = 2
	}
	if minutes > maxMin {
		logf("recorder: asked for the last %.0f minutes, but the buffer only holds %.0f - saving %.0f",
			minutes, maxMin, maxMin)
		minutes = maxMin
	}
	path, err := rec.saveLast(time.Duration(minutes * float64(time.Minute)))
	if err != nil {
		return err.Error()
	}
	logf("recorder: manual save from the app window -> %s", path)
	return ""
}

// captureTestReport is what the window shows after a self-test.
type captureTestReport struct {
	Results []probeResult `json:"results"`
	Winner  string        `json:"winner"`
	Applied bool          `json:"applied"`
	Error   string        `json:"error"`
}

// captureTestState is what the window polls while a test is running.
type captureTestState struct {
	Running bool               `json:"running"`
	Step    string             `json:"step"`
	Report  *captureTestReport `json:"report"`
}

var (
	testMu      sync.Mutex
	testRunning bool
	testStep    string
	testReport  *captureTestReport
)

// apiStartCaptureTest kicks the self-test off and returns IMMEDIATELY.
//
// It runs in the background for a reason learned the hard way: a bound function
// that blocks for a minute blocks every other bound function with it, and the
// window goes dead while pretending to be busy. Anything slow starts a
// goroutine and reports progress through a separate, instant poll.
func apiStartCaptureTest() string {
	testMu.Lock()
	if testRunning {
		testMu.Unlock()
		return "A capture test is already running."
	}
	testRunning = true
	testStep = "starting"
	testReport = nil
	testMu.Unlock()

	go func() {
		defer func() {
			testMu.Lock()
			testRunning = false
			testMu.Unlock()
		}()

		rc := rec.settings()

		// Stop any live capture first. Two ffmpeg processes fighting over the
		// same screen grab would make the test lie about what works.
		rec.stopCapture("running the capture self-test")

		setTestStep("trying each way of capturing your screen")
		results, winner, err := runCaptureProbe(rc)

		rep := &captureTestReport{Results: results}
		switch {
		case err != nil:
			rep.Error = err.Error()
		case winner == nil:
			rep.Error = "None of the configurations produced any video on this PC."
		default:
			rep.Winner = winner.Label
			_caps, _, _ := captureCaps()
			if err := applyProbeWinner(*winner, _caps); err != nil {
				rep.Error = err.Error()
			} else {
				rep.Applied = true
			}
		}

		testMu.Lock()
		testReport = rep
		testStep = "finished"
		testMu.Unlock()
	}()

	return ""
}

func setTestStep(step string) {
	testMu.Lock()
	testStep = step
	testMu.Unlock()
}

// apiResults hands the page whatever is known about the player's coaching right
// now, and never waits for the network.
//
// The first call finds nothing and starts a fetch; the page polls and the answer
// arrives a moment later. That is the only shape allowed here - this binding runs
// on the window's message loop, and a network call on that thread freezes the
// whole app. See coachfetch.go.
func apiResults() resultsSnapshot {
	snap := resultsSnapshotNow()
	if snap.State == "idle" {
		startResultsFetch()
		snap = resultsSnapshotNow()
	}
	return snap
}

// apiRefreshResults is the Refresh button. Returns immediately; the tab watches
// the snapshot change underneath it.
func apiRefreshResults() string {
	startResultsFetch()
	return ""
}

// apiCoachSpeak asks for a match to be read aloud. Returns instantly; the page
// polls apiCoachAudio for the result.
func apiCoachSpeak(raw string) string {
	var in struct {
		MatchID string `json:"match_id"`
		Coach   string `json:"coach"`
	}
	if err := json.Unmarshal([]byte(raw), &in); err != nil || in.MatchID == "" {
		return "no match to read out"
	}
	startCoachAudio(in.MatchID, in.Coach)
	return ""
}

func apiCoachAudio() coachAudio { return coachAudioSnapshot() }

// apiOverlayPreview draws the in-game reminder immediately, whatever the setting
// says. The one honest way to answer "does this work on my machine".
func apiOverlayPreview() string {
	go overlayPreview()
	return ""
}

// apiCoachSample auditions a voice. Same state and same player as the readout, so
// the page polls goCoachAudio for this exactly as it does for a match.
func apiCoachSample(coach string) string {
	if coach == "" {
		return "no coach selected"
	}
	startCoachSample(coach)
	return ""
}

// apiCoachLang reports the saved language when asked with an empty string, and
// saves one otherwise. Two jobs in one binding because the page needs both and the
// bridge passes exactly one string each way.
func apiCoachLang(lang string) string {
	if lang == "" {
		var cfg config
		loadJSON(configPath(), &cfg)
		if cfg.CoachLang == "" {
			return "en"
		}
		return cfg.CoachLang
	}
	return setCoachLang(lang)
}

// apiSetCoach changes the coach on the ACCOUNT. Network call, so it is the one
// place here that can be slow - the page shows the picker as busy while it runs.
func apiSetCoach(key string) string { return setCoachKey(key) }

// captureTestRunning lets the recorder loop know to keep its hands off the
// screen grab while the self-test is measuring it. See recorder.shouldCapture.
func captureTestRunning() bool {
	testMu.Lock()
	defer testMu.Unlock()
	return testRunning
}

// apiCaptureTestState is the instant poll the window uses while the test runs.
func apiCaptureTestState() captureTestState {
	testMu.Lock()
	defer testMu.Unlock()
	return captureTestState{Running: testRunning, Step: testStep, Report: testReport}
}

func apiToggleRecorderPause() bool { return toggleRecorderPause() }
func apiToggleSyncPause() bool     { return toggleSyncPause() }

// apiOpenLog opens sync.log, the same thing the tray item does.
func apiOpenLog() string {
	openInNotepad(filepath.Join(configDir(), "sync.log"))
	return ""
}

// ---- helpers --------------------------------------------------------------

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Size() > 0
}

// fileURL turns a Windows path into something the window can load as an image.
func fileURL(p string) string {
	return "file:///" + strings.ReplaceAll(p, `\`, "/")
}

// underClipDir is the safety boundary for every path that arrives from
// JavaScript. Compared case-insensitively because Windows paths are, and after
// resolving both sides so a path full of ".." cannot climb out.
func underClipDir(p string) bool {
	rc := rec.settings()
	root, err := filepath.Abs(rc.ClipDir)
	if err != nil {
		return false
	}
	target, err := filepath.Abs(p)
	if err != nil {
		return false
	}
	root = strings.ToLower(filepath.Clean(root))
	target = strings.ToLower(filepath.Clean(target))
	return target != root && strings.HasPrefix(target, root+string(filepath.Separator))
}

func dirSizeMB(dir string) int64 {
	var total int64
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if info, err := e.Info(); err == nil {
			total += info.Size()
		}
	}
	return total / (1024 * 1024)
}

// prettyDuration formats a clip length the way a person reads it.
func prettyDuration(sec float64) string {
	if sec < 60 {
		return fmt.Sprintf("%.0fs", sec)
	}
	return fmt.Sprintf("%d:%02d", int(sec)/60, int(sec)%60)
}
