// capture.go - the capture layer's interface, and the registry that lets a
// second backend drop in later without touching anything above it.
//
// # WHY THIS FILE EXISTS AT ALL
//
// There are two real ways to grab frames from a game on Windows:
//
//  1. FFmpeg, driven as a child process, using ddagrab (GPU-side desktop
//     duplication) or gdigrab (CPU-side window capture). Everything hard about
//     talking to NVENC, AMF and QuickSync is already solved inside it, and it
//     is one binary that behaves the same on every vendor's hardware.
//
//  2. Windows.Graphics.Capture, the modern WinRT API that OBS itself moved to.
//     It captures a single window directly, keeps frames on the GPU, and is the
//     better ceiling. It is also a C++ interop project reached from Go through
//     cgo, so it is weeks of work rather than days.
//
// Rather than pick one and regret it, the recorder above this line only ever
// talks to captureBackend. FFmpeg ships first because it actually runs today.
// A native backend registers itself here later and the rest of the recorder
// never learns the difference.
//
// Nothing in this layer, in either backend, touches the game process, its memory
// or its network traffic. Capture reads what the desktop compositor is already
// putting on screen. That is the same bright line the replay watcher holds, and
// it is what keeps Sync irrelevant to BattlEye.
package main

import (
	"fmt"
	"sort"
	"strings"
)

// captureRect is a pixel rectangle in virtual-desktop coordinates.
type captureRect struct {
	X, Y, W, H int
}

func (r captureRect) valid() bool { return r.W > 0 && r.H > 0 }

func (r captureRect) String() string {
	return fmt.Sprintf("%dx%d+%d+%d", r.W, r.H, r.X, r.Y)
}

// captureSpec is everything a backend needs to start recording. It is a value,
// not a pointer, so a backend cannot quietly mutate the caller's settings.
type captureSpec struct {
	// Where to grab from. MonitorIndex is the display the Siege window is on.
	// Crop is the window's rectangle within that display; a backend that can
	// capture a window directly may ignore Crop and use WindowTitle instead.
	MonitorIndex int
	WindowTitle  string
	Crop         captureRect

	// How to encode. HeightCap of 0 means "do not downscale".
	FPS       int
	HeightCap int
	Encoder   string
	Quality   int

	// Where the rolling buffer's segments are written. The backend writes
	// numbered segment files here and nothing else; pruning is somebody else's
	// job (see ringbuf.go), because the backend must never delete a file it is
	// still writing.
	SegmentDir  string
	SegmentSecs int
}

// captureSession is one running capture. Stop must be safe to call twice, and
// must leave the last segment playable rather than truncated.
type captureSession interface {
	Stop() error
	Running() bool
	// Wait blocks until the session ends on its own and returns why. A session
	// that was asked to Stop returns nil.
	Wait() error
	// Backend names which implementation produced this session, for the log.
	Backend() string
}

// captureBackend produces sessions. Available is asked before every start so a
// backend can report a fixable problem ("ffmpeg.exe not found") in words the
// player can act on, rather than failing silently.
type captureBackend interface {
	Name() string
	Available(rc recorderConfig) (bool, string)
	Start(spec captureSpec, rc recorderConfig) (captureSession, error)
}

var captureBackends = map[string]captureBackend{}

// registerCaptureBackend is called from each backend's init(). Registering twice
// under the same name is a programming error and panics at startup rather than
// silently picking one.
func registerCaptureBackend(b captureBackend) {
	name := strings.ToLower(b.Name())
	if _, dup := captureBackends[name]; dup {
		panic("capture backend registered twice: " + name)
	}
	captureBackends[name] = b
}

// captureBackendNames lists what is compiled in, sorted, for the log and the
// tray's About text.
func captureBackendNames() []string {
	var names []string
	for n := range captureBackends {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// pickCaptureBackend honours the configured choice when it is usable, and
// otherwise falls back to any backend that reports itself available. The reason
// a preferred backend was skipped is returned so it can be logged - a recorder
// that quietly used a slower path than the player asked for is worse than one
// that says why.
func pickCaptureBackend(rc recorderConfig) (captureBackend, string, error) {
	if len(captureBackends) == 0 {
		return nil, "", fmt.Errorf("no capture backends compiled in")
	}

	want := strings.ToLower(rc.CaptureBackend)
	if b, ok := captureBackends[want]; ok {
		if okNow, why := b.Available(rc); okNow {
			return b, "", nil
		} else {
			// Preferred backend is present but not usable right now. Try the rest
			// before giving up, and carry the reason forward.
			for _, name := range captureBackendNames() {
				if name == want {
					continue
				}
				alt := captureBackends[name]
				if okAlt, _ := alt.Available(rc); okAlt {
					return alt, fmt.Sprintf("%s unavailable (%s) - using %s instead", want, why, name), nil
				}
			}
			return nil, "", fmt.Errorf("%s unavailable: %s", want, why)
		}
	}

	// Configured name is not one we know. Take the first usable one and say so.
	for _, name := range captureBackendNames() {
		b := captureBackends[name]
		if okNow, _ := b.Available(rc); okNow {
			return b, fmt.Sprintf("unknown capture backend %q - using %s", rc.CaptureBackend, name), nil
		}
	}
	return nil, "", fmt.Errorf("no usable capture backend (have: %s)", strings.Join(captureBackendNames(), ", "))
}
