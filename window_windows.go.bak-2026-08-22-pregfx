//go:build windows

// window_windows.go - finding the Rainbow Six Siege window and working out
// which display it is on.
//
// TRUST BOUNDARY - read this before changing anything here.
//
// Every call in this file is a read of window-manager state that Windows offers
// to any process on the desktop: is a process running, where is its window, how
// big is it, which monitor is it on. Nothing here opens the game process, reads
// its memory, injects anything, or looks at its network traffic. That is the
// same line replay.go holds, and it is the whole reason SiegeIQ Sync is
// irrelevant to BattlEye. Do not weaken it for convenience.
//
// In particular: OpenProcess, ReadProcessMemory, CreateRemoteThread and window
// subclassing are all off limits, permanently, however useful they might look.
package main

import (
	"fmt"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

// Everything here comes out of the standard library's syscall package rather
// than golang.org/x/sys/windows. user32.dll is already loaded into every GUI
// process before this code runs, so the lazy load resolves the module that is
// present rather than searching the disk, and dropping the dependency keeps the
// recorder's Win32 surface auditable in one file with no third-party code in it.
var (
	modUser32 = syscall.NewLazyDLL("user32.dll")

	procEnumWindows         = modUser32.NewProc("EnumWindows")
	procGetWindowThreadPID  = modUser32.NewProc("GetWindowThreadProcessId")
	procIsWindowVisible     = modUser32.NewProc("IsWindowVisible")
	procGetClientRect       = modUser32.NewProc("GetClientRect")
	procClientToScreen      = modUser32.NewProc("ClientToScreen")
	procGetWindowTextW      = modUser32.NewProc("GetWindowTextW")
	procMonitorFromWindow   = modUser32.NewProc("MonitorFromWindow")
	procGetMonitorInfoW     = modUser32.NewProc("GetMonitorInfoW")
	procEnumDisplayMonitors = modUser32.NewProc("EnumDisplayMonitors")
	procGetForegroundWindow = modUser32.NewProc("GetForegroundWindow")
)

const monitorDefaultToNearest = 2

type winRect struct {
	Left, Top, Right, Bottom int32
}

type winPoint struct {
	X, Y int32
}

type monitorInfo struct {
	CbSize    uint32
	RcMonitor winRect
	RcWork    winRect
	DwFlags   uint32
}

// siegeExeNames are the process names Siege runs under. The Vulkan build is a
// separate executable, and the BattlEye launcher is a third that exits once the
// game itself is up - it is listed so "is Siege starting" does not read as "no".
var siegeExeNames = []string{
	"rainbowsix.exe",
	"rainbowsix_vulkan.exe",
	"rainbowsix_be.exe",
}

// siegePIDs returns the process IDs of any running Siege executable. An empty
// slice means Siege is not running, which is the normal case most of the time.
func siegePIDs() []uint32 {
	snap, err := syscall.CreateToolhelp32Snapshot(syscall.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer syscall.CloseHandle(snap)

	var pe syscall.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	if err := syscall.Process32First(snap, &pe); err != nil {
		return nil
	}

	var pids []uint32
	for {
		name := strings.ToLower(syscall.UTF16ToString(pe.ExeFile[:]))
		for _, want := range siegeExeNames {
			if name == want {
				pids = append(pids, pe.ProcessID)
				break
			}
		}
		if err := syscall.Process32Next(snap, &pe); err != nil {
			break
		}
	}
	return pids
}

// siegeRunning is the cheap question the recorder's watch loop asks on a timer.
func siegeRunning() bool { return len(siegePIDs()) > 0 }

// siegeInForeground reports whether the window the player is actually looking at
// belongs to Siege.
//
// # WHY THIS EXISTS, AND WHY IT IS A PROMISE RATHER THAN A FEATURE
//
// The GPU screen grab is DXGI Desktop Duplication. It hands back the composited
// DESKTOP, and the recorder then crops that to the Siege window's rectangle. In
// borderless, Siege's rectangle IS the whole screen - so the crop takes
// everything, and "everything" means whatever window happens to be on top.
//
// That was not a theory. The first clip recorded through this path, on the
// machine this was written for, contained a chat window, an email client and a
// file browser. The app was doing exactly what it was told. What it was told did
// not match what siegeiq.gg promises, which is that Sync captures the game and
// nothing else.
//
// So capture holds while Siege is the window in front, and only then. The cost
// is a gap in the buffer whenever somebody alt-tabs, which is visible and
// slightly annoying. The alternative is a recorder that quietly films a player's
// desktop, and there is no version of that worth shipping.
//
// STILL INSIDE THE TRUST BOUNDARY: GetForegroundWindow and
// GetWindowThreadProcessId are window-manager reads, the same class of call as
// everything else in this file. Nothing is opened, read or injected.
func siegeInForeground() bool {
	hwnd, _, _ := procGetForegroundWindow.Call()
	if hwnd == 0 {
		return false
	}
	var pid uint32
	procGetWindowThreadPID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if pid == 0 {
		return false
	}
	for _, p := range siegePIDs() {
		if p == pid {
			return true
		}
	}
	return false
}

// enumState carries results out of the EnumWindows callback. Win32 callbacks
// cannot safely carry Go pointers in their LPARAM, so a guarded package-level
// value is the honest way to do this. The mutex also serialises concurrent
// callers, which matters because the recorder and the tray can both ask.
var enumState struct {
	sync.Mutex
	wantPIDs map[uint32]bool
	bestHWND syscall.Handle
	bestArea int32
}

var enumWindowsCallback = syscall.NewCallback(func(hwnd syscall.Handle, _ uintptr) uintptr {
	var pid uint32
	procGetWindowThreadPID.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&pid)))
	if !enumState.wantPIDs[pid] {
		return 1 // keep enumerating
	}
	if visible, _, _ := procIsWindowVisible.Call(uintptr(hwnd)); visible == 0 {
		return 1
	}
	var r winRect
	if ok, _, _ := procGetClientRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&r))); ok == 0 {
		return 1
	}
	w, h := r.Right-r.Left, r.Bottom-r.Top
	// Siege spawns small helper windows. The real game window is the biggest one
	// the process owns, and anything under 320x240 is definitely not it.
	if w < 320 || h < 240 {
		return 1
	}
	if area := w * h; area > enumState.bestArea {
		enumState.bestArea = area
		enumState.bestHWND = hwnd
	}
	return 1
})

// siegeWindow returns the handle of Siege's main window. ok is false whenever
// Siege is not running, or is running but has not put a real window up yet -
// which is a normal state for several seconds during launch.
func siegeWindow() (hwnd syscall.Handle, ok bool) {
	pids := siegePIDs()
	if len(pids) == 0 {
		return 0, false
	}

	enumState.Lock()
	defer enumState.Unlock()

	enumState.wantPIDs = make(map[uint32]bool, len(pids))
	for _, p := range pids {
		enumState.wantPIDs[p] = true
	}
	enumState.bestHWND = 0
	enumState.bestArea = 0

	procEnumWindows.Call(enumWindowsCallback, 0)

	return enumState.bestHWND, enumState.bestHWND != 0
}

// windowTitle reads a window's caption. Used by the gdigrab fallback path, which
// captures by title rather than by rectangle.
func windowTitle(hwnd syscall.Handle) string {
	buf := make([]uint16, 512)
	n, _, _ := procGetWindowTextW.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf[:n])
}

// clientRectOnScreen returns the window's drawable area in screen coordinates -
// the game picture itself, without the title bar or borders. In fullscreen and
// borderless (which is how nearly everybody plays Siege) this is the whole
// display, and the crop below becomes a no-op.
func clientRectOnScreen(hwnd syscall.Handle) (captureRect, error) {
	var r winRect
	if ok, _, _ := procGetClientRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&r))); ok == 0 {
		return captureRect{}, fmt.Errorf("GetClientRect failed")
	}
	origin := winPoint{X: r.Left, Y: r.Top}
	if ok, _, _ := procClientToScreen.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&origin))); ok == 0 {
		return captureRect{}, fmt.Errorf("ClientToScreen failed")
	}
	return captureRect{
		X: int(origin.X),
		Y: int(origin.Y),
		W: int(r.Right - r.Left),
		H: int(r.Bottom - r.Top),
	}, nil
}

// monitorEnumState collects monitor rectangles in enumeration order.
var monitorEnumState struct {
	sync.Mutex
	rects []winRect
}

// The RECT parameter is declared as a real *winRect rather than a uintptr that
// gets converted. Converting an incoming uintptr to unsafe.Pointer is not
// something the Go memory model actually permits - go vet flags it, correctly -
// and syscall.NewCallback is happy to take a pointer type directly.
var monitorEnumCallback = syscall.NewCallback(func(hmon uintptr, _ uintptr, lprc *winRect, _ uintptr) uintptr {
	if lprc != nil {
		monitorEnumState.rects = append(monitorEnumState.rects, *lprc)
	}
	return 1
})

// monitorIndexFor works out which display a window is on, as a zero-based index.
//
// HONEST LIMITATION: this index is EnumDisplayMonitors order, and ddagrab's
// output index is DXGI adapter-output order. On the overwhelmingly common
// single-GPU, single-or-dual-monitor setup those agree. On an exotic multi-GPU
// machine they can differ, which is exactly why recorderConfig.MonitorIndex
// exists as a manual override. The recorder logs the index it chose so a player
// with a wrong-monitor recording has one number to change rather than a mystery.
func monitorIndexFor(hwnd syscall.Handle) (int, captureRect, error) {
	hmon, _, _ := procMonitorFromWindow.Call(uintptr(hwnd), monitorDefaultToNearest)
	if hmon == 0 {
		return 0, captureRect{}, fmt.Errorf("MonitorFromWindow failed")
	}

	var mi monitorInfo
	mi.CbSize = uint32(unsafe.Sizeof(mi))
	if ok, _, _ := procGetMonitorInfoW.Call(hmon, uintptr(unsafe.Pointer(&mi))); ok == 0 {
		return 0, captureRect{}, fmt.Errorf("GetMonitorInfoW failed")
	}
	target := mi.RcMonitor

	monitorEnumState.Lock()
	defer monitorEnumState.Unlock()
	monitorEnumState.rects = monitorEnumState.rects[:0]
	procEnumDisplayMonitors.Call(0, 0, monitorEnumCallback, 0)

	bounds := captureRect{
		X: int(target.Left),
		Y: int(target.Top),
		W: int(target.Right - target.Left),
		H: int(target.Bottom - target.Top),
	}
	for i, r := range monitorEnumState.rects {
		if r == target {
			return i, bounds, nil
		}
	}
	// Enumeration disagreed with MonitorFromWindow, which should not happen.
	// Returning 0 with the real bounds is better than an error: the recording
	// still works, and it is on the primary display rather than nowhere.
	return 0, bounds, nil
}

// siegeGeometry is the one call the recorder makes. It returns everything needed
// to start a capture: which display, the game's rectangle on it, the crop
// relative to that display, and the window title for the gdigrab fallback.
func siegeGeometry() (monitor int, monitorBounds captureRect, crop captureRect, title string, err error) {
	hwnd, ok := siegeWindow()
	if !ok {
		return 0, captureRect{}, captureRect{}, "", fmt.Errorf("no Siege window")
	}
	title = windowTitle(hwnd)

	client, err := clientRectOnScreen(hwnd)
	if err != nil {
		return 0, captureRect{}, captureRect{}, title, err
	}
	monitor, monitorBounds, err = monitorIndexFor(hwnd)
	if err != nil {
		return 0, captureRect{}, captureRect{}, title, err
	}

	// Crop is expressed relative to the display's top-left, because that is what
	// a display-capture backend hands us. Clamp it into the display so a window
	// straddling two monitors produces a valid rectangle instead of a crash
	// inside the encoder.
	crop = captureRect{
		X: client.X - monitorBounds.X,
		Y: client.Y - monitorBounds.Y,
		W: client.W,
		H: client.H,
	}
	if crop.X < 0 {
		crop.W += crop.X
		crop.X = 0
	}
	if crop.Y < 0 {
		crop.H += crop.Y
		crop.Y = 0
	}
	if crop.X+crop.W > monitorBounds.W {
		crop.W = monitorBounds.W - crop.X
	}
	if crop.Y+crop.H > monitorBounds.H {
		crop.H = monitorBounds.H - crop.Y
	}
	// Encoders want even dimensions for 4:2:0 chroma. An odd width here is a
	// hard failure inside ffmpeg, so round down rather than find out later.
	crop.W -= crop.W % 2
	crop.H -= crop.H % 2

	if !crop.valid() {
		return monitor, monitorBounds, crop, title, fmt.Errorf("Siege window has no usable area")
	}
	return monitor, monitorBounds, crop, title, nil
}
