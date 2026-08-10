//go:build windows

// audio_loopback_windows.go - recording what the speakers are playing, without
// Stereo Mix and without installing anything.
//
// # WHY THIS FILE EXISTS
//
// FFmpeg on Windows can only open CAPTURE devices: microphones and line inputs.
// It has no way to read a playback device, so "record the game audio" is not a
// flag it offers. The usual workaround is Stereo Mix, a capture device some
// sound cards expose that carries the output mix. Plenty of modern laptops do
// not expose one at all, and this developer's does not, even with disabled
// devices shown.
//
// The other common workaround is shipping a virtual audio device. That installs
// a system-wide component that shows up in everybody's sound settings, needs
// registering at install time, and carries its own licence. On a product whose
// entire pitch is that it touches nothing, that is a real cost.
//
// Windows has a proper answer: WASAPI loopback. An ordinary audio client opened
// on the RENDER endpoint with the loopback flag receives exactly what is being
// played. It is read-only, it lives inside this process, it installs nothing, it
// needs no elevation, and it works on every Windows machine since Vista. It is
// more code than the alternatives, and it is the right cost to pay.
//
// # HOW IT REACHES FFMPEG
//
// The captured audio is raw float samples. It is written to a named pipe, and
// FFmpeg reads that pipe as an ordinary input. A named pipe rather than a
// socket, deliberately: a socket would open a port, and "this app opens no
// ports" is a claim worth keeping literally true.
//
// # THE SAFETY RULE
//
// Audio is a SECOND INPUT to the same FFmpeg process that records the video, so
// anything that goes wrong here can take the whole recording with it. Every
// failure path in this file therefore ends the same way: report it, capture no
// audio, and let the video record silently. A player with no sound has a smaller
// problem than a player with no footage.
package main

import (
	"fmt"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

var (
	modOle32           = syscall.NewLazyDLL("ole32.dll")
	procCoInitializeEx = modOle32.NewProc("CoInitializeEx")
	procCoUninit       = modOle32.NewProc("CoUninitialize")
	procCoCreateInst   = modOle32.NewProc("CoCreateInstance")
	procCoTaskMemFree  = modOle32.NewProc("CoTaskMemFree")

	modKernel32Pipe      = syscall.NewLazyDLL("kernel32.dll")
	procCreateNamedPipe  = modKernel32Pipe.NewProc("CreateNamedPipeW")
	procConnectNamedPipe = modKernel32Pipe.NewProc("ConnectNamedPipe")
	procDisconnectPipe   = modKernel32Pipe.NewProc("DisconnectNamedPipe")
)

// COM identifiers. These are fixed by Windows and are transcribed from the
// Core Audio headers; a single wrong digit produces a silent failure to create
// the object, which is why they are written out in full rather than parsed.
var (
	clsidMMDeviceEnumerator = guid{0xBCDE0395, 0xE52F, 0x467C, [8]byte{0x8E, 0x3D, 0xC4, 0x57, 0x92, 0x91, 0x69, 0x2E}}
	iidIMMDeviceEnumerator  = guid{0xA95664D2, 0x9614, 0x4F35, [8]byte{0xA7, 0x46, 0xDE, 0x8D, 0xB6, 0x36, 0x17, 0xE6}}
	iidIAudioClient         = guid{0x1CB9AD4C, 0xDBFA, 0x4C32, [8]byte{0xB1, 0x78, 0xC2, 0xF5, 0x68, 0xA7, 0x03, 0xB2}}
	iidIAudioCaptureClient  = guid{0xC8ADBD64, 0xE71E, 0x48A0, [8]byte{0xA4, 0xDE, 0x18, 0x5C, 0x39, 0x5C, 0xD3, 0x17}}
)

type guid struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

const (
	coinitApartmentThreaded = 0x2
	coinitDisableOLE1DDE    = 0x4

	clsctxAll = 0x17

	eRender  = 0
	eConsole = 0

	shareModeShared       = 0
	streamFlagsLoopback   = 0x00020000
	streamFlagsAutoConv   = 0x80000000
	streamFlagsSRCDefault = 0x08000000

	// A tenth of a second of buffer, in 100-nanosecond units. Long enough that
	// an ordinary scheduling hiccup does not drop audio, short enough that the
	// sound stays close to the picture.
	bufferDuration100ns = 1000000

	waveFormatPCM        = 1
	waveFormatIEEEFloat  = 3
	waveFormatExtensible = 0xFFFE

	// Silence is a real result, not an error. The capture client reports it so a
	// caller can write zeroes rather than nothing, which is what keeps the audio
	// timeline the same length as the video.
	bufferFlagsSilent = 0x2
)

// waveFormatEx mirrors the Windows structure of the same name. cbSize describes
// how many extra bytes follow, which is how WAVEFORMATEXTENSIBLE is layered on
// top of it.
type waveFormatEx struct {
	FormatTag      uint16
	Channels       uint16
	SamplesPerSec  uint32
	AvgBytesPerSec uint32
	BlockAlign     uint16
	BitsPerSample  uint16
	CbSize         uint16
}

// ---- the thin COM call helpers --------------------------------------------
//
// Every Core Audio object is a pointer to a vtable. Calling method N means
// reading the Nth function pointer out of that table and calling it with the
// object as the first argument. There is no type safety here at all, so the
// method indexes are named constants rather than bare numbers.

// COM objects are held as unsafe.Pointer rather than uintptr throughout this
// file. That is not style: a uintptr is just a number as far as the garbage
// collector and the race detector are concerned, and converting one back into a
// pointer is exactly the pattern go vet warns about. Keeping the pointer a
// pointer the whole way through removes the warning by removing the hazard.
func vcall(obj unsafe.Pointer, index int, args ...uintptr) uintptr {
	vtbl := *(**[64]uintptr)(obj)
	fn := vtbl[index]
	full := append([]uintptr{uintptr(obj)}, args...)
	switch len(full) {
	case 1:
		r, _, _ := syscall.SyscallN(fn, full[0])
		return r
	case 2:
		r, _, _ := syscall.SyscallN(fn, full[0], full[1])
		return r
	case 3:
		r, _, _ := syscall.SyscallN(fn, full[0], full[1], full[2])
		return r
	case 4:
		r, _, _ := syscall.SyscallN(fn, full[0], full[1], full[2], full[3])
		return r
	case 5:
		r, _, _ := syscall.SyscallN(fn, full[0], full[1], full[2], full[3], full[4])
		return r
	case 6:
		r, _, _ := syscall.SyscallN(fn, full[0], full[1], full[2], full[3], full[4], full[5])
		return r
	default:
		r, _, _ := syscall.SyscallN(fn, full...)
		return r
	}
}

const (
	// IUnknown
	mRelease = 2
	// IMMDeviceEnumerator
	mGetDefaultAudioEndpoint = 4
	// IMMDevice
	mActivate = 3
	// IAudioClient
	mInitialize    = 3
	mGetBufferSize = 4
	mGetMixFormat  = 8
	mStart         = 10
	mStop          = 11
	mGetService    = 14
	// IAudioCaptureClient
	mGetBuffer         = 3
	mReleaseBuffer     = 4
	mGetNextPacketSize = 5
)

// loopbackFormat is what the sound card is actually mixing at. FFmpeg has to be
// told this exactly, because raw samples carry no header.
type loopbackFormat struct {
	SampleRate int
	Channels   int
	Float      bool // true for 32-bit float, which is what shared mode almost always gives
	Bits       int
}

// ffmpegArgs describes this stream to FFmpeg.
func (f loopbackFormat) ffmpegArgs(pipeName string) []string {
	codec := "f32le"
	if !f.Float {
		switch f.Bits {
		case 16:
			codec = "s16le"
		case 32:
			codec = "s32le"
		}
	}
	return []string{
		"-thread_queue_size", "1024",
		"-f", codec,
		"-ar", itoa(f.SampleRate),
		"-ac", itoa(f.Channels),
		"-i", pipeName,
	}
}

// loopbackCapture is one running capture. Stop is safe to call more than once.
type loopbackCapture struct {
	Format   loopbackFormat
	PipeName string

	stopOnce sync.Once
	stop     chan struct{}
	done     chan struct{}
	pipe     syscall.Handle
}

func (c *loopbackCapture) Stop() {
	if c == nil {
		return
	}
	c.stopOnce.Do(func() {
		close(c.stop)
		select {
		case <-c.done:
		case <-time.After(2 * time.Second):
			logf("sound: loopback did not stop within two seconds, abandoning it")
		}
	})
}

// startLoopback opens the default playback device in loopback mode and starts
// pushing raw samples into a fresh named pipe.
//
// Returns the capture and the format FFmpeg needs. On ANY failure it returns an
// error and nothing is left running, because the caller's contract is simple:
// no capture means record the video silently.
func startLoopback() (*loopbackCapture, error) {
	fmtCh := make(chan loopbackFormat, 1)
	errCh := make(chan error, 1)

	pipeName := fmt.Sprintf(`\\.\pipe\siegeiq_audio_%d`, time.Now().UnixNano())

	c := &loopbackCapture{
		PipeName: pipeName,
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}

	go c.run(pipeName, fmtCh, errCh)

	select {
	case f := <-fmtCh:
		c.Format = f
		return c, nil
	case err := <-errCh:
		return nil, err
	case <-time.After(5 * time.Second):
		close(c.stop)
		return nil, fmt.Errorf("timed out starting the loopback capture")
	}
}

// run owns the COM apartment for the whole life of the capture. COM is
// thread-affine, so this goroutine is locked to one OS thread and every COM
// call in this file happens on it.
func (c *loopbackCapture) run(pipeName string, fmtCh chan<- loopbackFormat, errCh chan<- error) {
	defer close(c.done)

	// LockOSThread is not optional. Initialising COM on one thread and calling
	// into it from another is undefined, and the failure is intermittent rather
	// than immediate, which is the worst kind.
	runtimeLockOSThread()
	defer runtimeUnlockOSThread()

	hr, _, _ := procCoInitializeEx.Call(0, coinitApartmentThreaded|coinitDisableOLE1DDE)
	// S_FALSE means COM was already initialised on this thread, which is fine.
	if int32(hr) < 0 {
		errCh <- fmt.Errorf("could not start COM: 0x%08X", uint32(hr))
		return
	}
	defer procCoUninit.Call()

	var enum unsafe.Pointer
	r, _, _ := procCoCreateInst.Call(
		uintptr(unsafe.Pointer(&clsidMMDeviceEnumerator)), 0, clsctxAll,
		uintptr(unsafe.Pointer(&iidIMMDeviceEnumerator)), uintptr(unsafe.Pointer(&enum)))
	if int32(r) < 0 || enum == nil {
		errCh <- fmt.Errorf("could not reach the Windows audio system: 0x%08X", uint32(r))
		return
	}
	defer vcall(enum, mRelease)

	// eRender is the PLAYBACK side. Asking the capture side would give a
	// microphone, which is the entire mistake this file exists to avoid.
	var device unsafe.Pointer
	if hr := vcall(enum, mGetDefaultAudioEndpoint, eRender, eConsole,
		uintptr(unsafe.Pointer(&device))); int32(hr) < 0 || device == nil {
		errCh <- fmt.Errorf("no default playback device: 0x%08X", uint32(hr))
		return
	}
	defer vcall(device, mRelease)

	var client unsafe.Pointer
	if hr := vcall(device, mActivate, uintptr(unsafe.Pointer(&iidIAudioClient)),
		clsctxAll, 0, uintptr(unsafe.Pointer(&client))); int32(hr) < 0 || client == nil {
		errCh <- fmt.Errorf("could not open the playback device: 0x%08X", uint32(hr))
		return
	}
	defer vcall(client, mRelease)

	var pwfx *waveFormatEx
	if hr := vcall(client, mGetMixFormat, uintptr(unsafe.Pointer(&pwfx))); int32(hr) < 0 || pwfx == nil {
		errCh <- fmt.Errorf("could not read the audio format: 0x%08X", uint32(hr))
		return
	}
	defer procCoTaskMemFree.Call(uintptr(unsafe.Pointer(pwfx)))

	format := loopbackFormat{
		SampleRate: int(pwfx.SamplesPerSec),
		Channels:   int(pwfx.Channels),
		Bits:       int(pwfx.BitsPerSample),
		Float:      pwfx.FormatTag == waveFormatIEEEFloat,
	}
	// WAVEFORMATEXTENSIBLE hides the real type in a sub-format GUID. Shared mode
	// on every machine seen so far is 32-bit float, and the bit depth is the
	// reliable tell when the tag says "extensible".
	if pwfx.FormatTag == waveFormatExtensible && pwfx.BitsPerSample == 32 {
		format.Float = true
	}
	if format.SampleRate <= 0 || format.Channels <= 0 {
		errCh <- fmt.Errorf("the audio format made no sense: %d Hz, %d channels",
			format.SampleRate, format.Channels)
		return
	}

	if hr := vcall(client, mInitialize, shareModeShared,
		streamFlagsLoopback, uintptr(bufferDuration100ns), 0,
		uintptr(unsafe.Pointer(pwfx)), 0); int32(hr) < 0 {
		errCh <- fmt.Errorf("could not start loopback capture: 0x%08X", uint32(hr))
		return
	}

	var capture unsafe.Pointer
	if hr := vcall(client, mGetService, uintptr(unsafe.Pointer(&iidIAudioCaptureClient)),
		uintptr(unsafe.Pointer(&capture))); int32(hr) < 0 || capture == nil {
		errCh <- fmt.Errorf("could not get the capture channel: 0x%08X", uint32(hr))
		return
	}
	defer vcall(capture, mRelease)

	// The pipe is created BEFORE ffmpeg is told about it, and before Start, so
	// there is never a window where ffmpeg opens a pipe nobody is serving. An
	// ffmpeg blocked on a dead pipe records nothing at all, which would turn a
	// missing-sound problem into a missing-footage one.
	pipe, err := createPipe(pipeName)
	if err != nil {
		errCh <- err
		return
	}
	c.pipe = pipe
	defer syscall.CloseHandle(pipe)

	if hr := vcall(client, mStart); int32(hr) < 0 {
		errCh <- fmt.Errorf("could not start the audio stream: 0x%08X", uint32(hr))
		return
	}
	defer vcall(client, mStop)

	// Everything is up. Tell the caller the format so ffmpeg can be launched.
	fmtCh <- format
	logf("sound: loopback capture running at %d Hz, %d channels", format.SampleRate, format.Channels)

	// Wait for ffmpeg to open its end. ConnectNamedPipe returns zero with
	// ERROR_PIPE_CONNECTED when the client got there first, which is a success.
	procConnectNamedPipe.Call(uintptr(pipe), 0)
	defer procDisconnectPipe.Call(uintptr(pipe))

	frameBytes := format.Channels * (format.Bits / 8)
	if frameBytes <= 0 {
		frameBytes = 8
	}
	silence := make([]byte, 4096*frameBytes)

	for {
		select {
		case <-c.stop:
			return
		default:
		}

		var packet uint32
		if hr := vcall(capture, mGetNextPacketSize, uintptr(unsafe.Pointer(&packet))); int32(hr) < 0 {
			logf("sound: loopback stopped reading: 0x%08X", uint32(hr))
			return
		}
		if packet == 0 {
			// Nothing playing. WASAPI simply reports no data rather than
			// silence, so a quiet moment produces no packets at all.
			time.Sleep(8 * time.Millisecond)
			continue
		}

		var data unsafe.Pointer
		var frames, flags uint32
		if hr := vcall(capture, mGetBuffer,
			uintptr(unsafe.Pointer(&data)),
			uintptr(unsafe.Pointer(&frames)),
			uintptr(unsafe.Pointer(&flags)), 0, 0); int32(hr) < 0 {
			logf("sound: loopback buffer error: 0x%08X", uint32(hr))
			return
		}

		n := int(frames) * frameBytes
		if n > 0 {
			if flags&bufferFlagsSilent != 0 || data == nil {
				// Real silence still has to be written, or the audio track
				// becomes shorter than the video and everything after the gap
				// drifts earlier.
				for len(silence) < n {
					silence = append(silence, make([]byte, n-len(silence))...)
				}
				writeAll(pipe, silence[:n])
			} else {
				writeAll(pipe, unsafe.Slice((*byte)(data), n))
			}
		}

		vcall(capture, mReleaseBuffer, uintptr(frames))
	}
}

// createPipe makes the server end of a byte-mode named pipe.
func createPipe(name string) (syscall.Handle, error) {
	p, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return 0, err
	}
	const (
		pipeAccessOutbound = 0x00000002
		pipeTypeByte       = 0x00000000
		pipeWait           = 0x00000000
		outBuf             = 1 << 20
		inBuf              = 1 << 16
	)
	h, _, e := procCreateNamedPipe.Call(
		uintptr(unsafe.Pointer(p)),
		pipeAccessOutbound,
		pipeTypeByte|pipeWait,
		1, outBuf, inBuf, 0, 0)
	if syscall.Handle(h) == syscall.InvalidHandle {
		return 0, fmt.Errorf("could not create the audio pipe: %v", e)
	}
	return syscall.Handle(h), nil
}

// writeAll pushes every byte, because a partial write silently desynchronises
// the stream from that point on.
func writeAll(h syscall.Handle, b []byte) {
	for len(b) > 0 {
		var wrote uint32
		if err := syscall.WriteFile(h, b, &wrote, nil); err != nil {
			return // ffmpeg has gone; the capture loop will notice and stop
		}
		if wrote == 0 {
			return
		}
		b = b[wrote:]
	}
}

// runtimeLockOSThread and its partner exist so this file does not import
// runtime purely for two calls that read better named for what they guard.
func runtimeLockOSThread()   { lockThread() }
func runtimeUnlockOSThread() { unlockThread() }
