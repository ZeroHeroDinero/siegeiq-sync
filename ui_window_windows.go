//go:build windows

// ui_window_windows.go - the actual app window.
//
// # WHAT THIS IS, AND WHAT IT IS NOT
//
// It is a real Windows application window, using the WebView2 runtime that ships
// with Windows 11 and arrives with Edge on Windows 10. It is NOT a browser tab,
// and it does NOT open a network port - there is no local web server anywhere in
// this app. The page is loaded from a file on disk and the JavaScript talks to Go
// through WebView2's own function binding, which is an in-process call rather
// than an HTTP request.
//
// That distinction matters for the same reason the rest of Sync is written the
// way it is. "This app opens no ports and touches no processes" is a claim worth
// being able to make plainly, and a localhost server would have cost it for the
// sake of saving an afternoon.
//
// # THREADING
//
// A window needs a message loop, and a message loop belongs to one OS thread.
// systray already owns the main thread, so the window runs on its own locked
// thread. Windows is fine with this - message queues are per-thread - but the
// LockOSThread call is not optional and removing it produces a window that
// randomly stops responding.
//
// THIS FILE IS THE ONE PIECE THAT COULD NOT BE COMPILE-CHECKED before it reached
// a Windows machine, because it is the only part with a third-party dependency.
// Everything it calls is kept deliberately thin for exactly that reason: if the
// library's API differs, the fix is in here and nowhere else.
package main

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/jchv/go-webview2"
)

var (
	modDwmapi                = syscall.NewLazyDLL("dwmapi.dll")
	procDwmSetWindowAttrib   = modDwmapi.NewProc("DwmSetWindowAttribute")
	modUser32Win             = syscall.NewLazyDLL("user32.dll")
	procShowWindow           = modUser32Win.NewProc("ShowWindow")
	procSetForegroundWindowW = modUser32Win.NewProc("SetForegroundWindow")
	procSendMessageW         = modUser32Win.NewProc("SendMessageW")
	procLoadImageW           = modUser32Win.NewProc("LoadImageW")
	procGetSystemMetrics     = modUser32Win.NewProc("GetSystemMetrics")
)

const (
	swHide     = 0
	swMaximize = 3
	swRestore  = 9

	// Dark title bar. Windows renamed this attribute mid-life: builds from
	// 20H1 onward use 20, and the 1809-to-1909 range used 19. Setting both is
	// harmless - the one the running build does not recognise is ignored - and
	// it saves detecting the OS version to pick between them.
	dwmUseImmersiveDarkModeOld = 19
	dwmUseImmersiveDarkMode    = 20

	// Window icons. A window has two: the small one drawn in its own title bar
	// and in Alt-Tab, and the big one used on the taskbar. Setting only one
	// leaves the other as whatever Windows guessed, which is how you end up with
	// the right icon in one place and a generic square in another.
	wmSetIcon = 0x0080
	iconSmall = 0
	iconBig   = 1

	smCXIcon   = 11 // the system's "big icon" width
	smCXSmIcon = 49 // the system's "small icon" width

	smCXScreen = 0 // primary screen width
	smCYScreen = 1 // primary screen height
)

// loadIconSized loads the embedded SiegeIQ icon at a specific pixel size.
//
// Asking for an exact size rather than letting Windows pick matters on a
// high-DPI screen: the default is chosen for a 96-DPI display and then scaled
// up, which is what makes an icon look soft and slightly wrong next to
// everything else in the title bar.
func loadIconSized(px uintptr) uintptr {
	pathPtr, err := syscall.UTF16PtrFromString(iconTempFile())
	if err != nil {
		return 0
	}
	const imageIcon = 1
	const lrLoadFromFile = 0x00000010
	h, _, _ := procLoadImageW.Call(0, uintptr(unsafe.Pointer(pathPtr)), imageIcon,
		px, px, lrLoadFromFile)
	return h
}

// systemMetric wraps GetSystemMetrics, falling back to a sane default when the
// call fails rather than asking for a zero-pixel icon.
func systemMetric(index uintptr, fallback uintptr) uintptr {
	v, _, _ := procGetSystemMetrics.Call(index)
	if v == 0 {
		return fallback
	}
	return v
}

// windowHandle pulls the native HWND out of the webview.
//
// The uintptr conversion is written this way on purpose: it compiles whether
// the library returns an unsafe.Pointer or a uintptr, which matters because
// this file is the one piece that cannot be compile-checked before it reaches
// a Windows machine.
func windowHandle(w webview2.WebView) uintptr {
	return uintptr(w.Window())
}

// dressWindow applies the two things WebView2 does not do for us: a dark title
// bar to match the page, and the SiegeIQ mark in place of a generic icon.
//
// It deliberately does NOT show the window. See revealAppWindow - everything
// here happens while the window is still hidden, so none of it is visible as a
// change to a window already on screen.
func dressWindow(w webview2.WebView) {
	hwnd := windowHandle(w)
	if hwnd == 0 {
		return
	}

	on := int32(1)
	for _, attr := range []uintptr{dwmUseImmersiveDarkMode, dwmUseImmersiveDarkModeOld} {
		procDwmSetWindowAttrib.Call(hwnd, attr, uintptr(unsafe.Pointer(&on)), unsafe.Sizeof(on))
	}

	// The SiegeIQ mark in the title bar, Alt-Tab and the taskbar. Without this
	// the window shows whatever generic icon Windows picks for an application
	// that never said, which looks like somebody else's software.
	if small := loadIconSized(systemMetric(smCXSmIcon, 16)); small != 0 {
		procSendMessageW.Call(hwnd, wmSetIcon, iconSmall, small)
	}
	if big := loadIconSized(systemMetric(smCXIcon, 32)); big != 0 {
		procSendMessageW.Call(hwnd, wmSetIcon, iconBig, big)
	}

}

var (
	windowMu   sync.Mutex
	windowOpen bool

	// windowHWND is the live window's native handle, kept so the tray can bring
	// a hidden window back without rebuilding it. Read and written under
	// windowMu; zero whenever no window exists.
	windowHWND uintptr

	// windowRevealed guards the one-time reveal. See revealAppWindow.
	windowRevealed bool
)

// revealAppWindow shows the window, maximised, exactly once per open.
//
// # WHY THE WINDOW STARTS HIDDEN
//
// WebView2 paints white until the page's first frame lands, and the window
// itself is white before WebView2 paints at all. Creating the window, showing
// it, and then maximising it therefore produced a visible sequence of two white
// rectangles - the frame, then the browser control - followed by a jump to full
// screen, followed by the dark page appearing. Four states before the app
// looked like the app.
//
// So nothing is shown at all. The window is built, hidden, dressed and
// navigated off screen, and appears only once the page reports that it has
// painted - by which point the first thing on screen is the finished interface.
// The fallback timer exists because a window that never appears because a
// script did not run would be far worse than a flash.
func revealAppWindow() {
	windowMu.Lock()
	hwnd := windowHWND
	if hwnd == 0 || windowRevealed {
		windowMu.Unlock()
		return
	}
	windowRevealed = true
	windowMu.Unlock()

	procShowWindow.Call(hwnd, swMaximize)
	procSetForegroundWindowW.Call(hwnd)
	logf("app window: shown")
}

// hideAppWindow puts the window away without tearing it down.
//
// The first version of "Hide to tray" called Terminate() from inside the
// binding callback - which runs ON the window's own message-loop thread. Ending
// the loop from inside a call the loop is still dispatching left the window on
// screen with a dead message pump: visibly frozen, and windowOpen stuck true so
// the tray refused to reopen it. ShowWindow is the correct tool and is safe to
// call from any thread; the window stays alive and reappearing is instant.
func hideAppWindow() {
	windowMu.Lock()
	hwnd := windowHWND
	windowMu.Unlock()
	if hwnd == 0 {
		return
	}
	procShowWindow.Call(hwnd, swHide)
	logf("app window: hidden to tray")
}

// showExistingWindow brings a hidden window back. Reports false if there isn't
// one, in which case the caller builds a new one.
func showExistingWindow() bool {
	windowMu.Lock()
	hwnd := windowHWND
	open := windowOpen
	windowMu.Unlock()
	if !open || hwnd == 0 {
		return false
	}
	procShowWindow.Call(hwnd, swRestore)
	procShowWindow.Call(hwnd, swMaximize)
	procSetForegroundWindowW.Call(hwnd)
	logf("app window: brought back from the tray")
	return true
}

// uiPagePath writes the page out and returns where it landed. Rewritten on every
// open so an app update never leaves a stale interface behind.
func uiPagePath() (string, error) {
	dir := filepath.Join(configDir(), "ui")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	p := filepath.Join(dir, "window.html")
	if err := os.WriteFile(p, []byte(uiHTML), 0o644); err != nil {
		return "", err
	}
	return p, nil
}

// openAppWindow shows the window. If one already exists - including one that
// has been hidden to the tray - it is brought back rather than rebuilt, so
// "Hide to tray" then "Open SiegeIQ Sync" is instant and keeps its scroll
// position. Safe to call from a menu click.
func openAppWindow() {
	if showExistingWindow() {
		return
	}

	windowMu.Lock()
	if windowOpen {
		// A window exists but has no handle yet: it is still being built, and a
		// second one must not be started alongside it.
		windowMu.Unlock()
		logf("app window: already opening")
		return
	}
	windowOpen = true
	windowMu.Unlock()

	go runAppWindow()
}

func runAppWindow() {
	// The window owns this thread for its whole life. Without this the message
	// loop can end up on a different thread than the window it is pumping.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	defer func() {
		windowMu.Lock()
		windowOpen = false
		windowHWND = 0
		windowMu.Unlock()
		// A panic in here must not take the tray down with it. The recorder and
		// the uploader keep running whatever the window does.
		if r := recover(); r != nil {
			logf("app window: closed after an error: %v", r)
		}
	}()

	page, err := uiPagePath()
	if err != nil {
		logf("app window: could not write the page: %v", err)
		return
	}

	w := webview2.NewWithOptions(webview2.WebViewOptions{
		Debug:     false,
		AutoFocus: true,
		// Built at screen size rather than 1180x860, because the window is
		// going to be maximised anyway. Creating it small and then maximising
		// makes WebView2 lay the page out twice, and the second pass is a
		// visible reflow. Starting at the size it will end up at means one
		// layout, done before the window is ever on screen.
		WindowOptions: webview2.WindowOptions{
			Title:  "SiegeIQ Sync",
			Width:  uint(systemMetric(smCXScreen, 1600)),
			Height: uint(systemMetric(smCYScreen, 900)),
			Center: true,
		},
	})
	if w == nil {
		// Almost always a missing WebView2 runtime on an older Windows 10 build.
		logf("app window: WebView2 is not available on this machine - the tray menu still works")
		showDialog(dialogSpec{
			instruction: "Couldn't open the window",
			content: "SiegeIQ Sync needs the Microsoft WebView2 runtime to show its window. " +
				"It comes with Windows 11 and with Microsoft Edge on Windows 10.\r\n\r\n" +
				"Everything still works from the tray icon in the meantime.",
			footer:       "You can install WebView2 free from Microsoft.",
			openSiteURL:  "https://developer.microsoft.com/microsoft-edge/webview2/",
			openSiteText: "Get WebView2",
			buttonText:   "Close",
		})
		return
	}
	defer w.Destroy()

	// Off screen before anything else happens. Every step below - binding,
	// dressing, navigating, laying out at full size - is work the player should
	// never watch, and hiding first is what turns it from a four-stage flicker
	// into the window simply being there.
	hwnd := windowHandle(w)
	if hwnd != 0 {
		procShowWindow.Call(hwnd, swHide)
	}

	windowMu.Lock()
	windowHWND = hwnd
	windowRevealed = false
	windowMu.Unlock()

	dressWindow(w)
	bindUI(w)
	w.Navigate("file:///" + filepath.ToSlash(page))

	// The page calls goReady the moment it has painted. This is the safety net
	// for the case where it never does.
	go func() {
		time.Sleep(2500 * time.Millisecond)
		revealAppWindow()
	}()

	logf("app window: opened")
	w.Run()
	logf("app window: closed")
}

// bindUI wires the page's JavaScript to the functions in ui_api.go.
//
// Every name here is prefixed go... so that reading the page's script makes it
// obvious which calls cross into Go. Each one returns either data or an error
// STRING - never a Go error - because an empty string is the natural "it worked"
// on the JavaScript side and avoids a rejected promise for an ordinary refusal.
func bindUI(w webview2.WebView) {
	// EVERY function here has the SAME SIGNATURE: func(string) string.
	//
	// This is not a style choice. Functions taking no arguments and returning a
	// struct were silently never invoked on a real machine - the page got a
	// promise that never settled and the Go side was never entered, with no
	// error anywhere. A function taking a string and returning a string worked
	// on the identical bridge at the identical moment. So this is the shape,
	// everywhere, without exception. Structured replies travel as JSON text and
	// the page unpacks them.
	//
	// If you add a call here, give it this signature. A new one that takes no
	// arguments will appear to work in testing and then quietly do nothing.
	bind := func(name string, fn func(string) string) {
		if err := w.Bind(name, fn); err != nil {
			logf("app window: could not bind %s: %v", name, err)
		}
	}

	bind("goStatus", func(string) string { return jsonOf(apiStatus()) })
	bind("goSettings", func(string) string { return jsonOf(apiSettings()) })
	bind("goClips", func(string) string { return jsonOf(apiClips()) })
	bind("goSaveSettings", func(raw string) string { return apiSaveSettings(raw) })
	bind("goSoundPrefs", func(string) string { return jsonOf(apiSoundPrefs()) })
	bind("goSaveSoundPrefs", func(raw string) string { return apiSaveSoundPrefs(raw) })
	bind("goArm", func(on string) string { return apiArm(on) })
	bind("goPlayClip", func(path string) string { return apiPlayClip(path) })
	bind("goDeleteClip", func(path string) string { return apiDeleteClip(path) })
	bind("goSendClip", func(path string) string { return apiSendClip(path) })
	bind("goSaveLast", func(mins string) string { return apiSaveLast(mins) })
	bind("goOpenFolder", func(string) string { return apiOpenClipFolder() })
	bind("goOpenLog", func(string) string { return apiOpenLog() })
	bind("goResults", func(string) string { return jsonOf(apiResults()) })
	bind("goCoachSpeak", func(raw string) string { return apiCoachSpeak(raw) })
	bind("goCoachAudio", func(string) string { return jsonOf(apiCoachAudio()) })
	bind("goSetCoach", func(key string) string { return apiSetCoach(key) })
	bind("goRefreshResults", func(string) string { return apiRefreshResults() })
	bind("goCaptureTest", func(string) string { return apiStartCaptureTest() })
	bind("goCaptureTestState", func(string) string { return jsonOf(apiCaptureTestState()) })

	bind("goTogglePause", func(string) string {
		return jsonOf(map[string]bool{"paused": apiToggleRecorderPause()})
	})
	bind("goToggleSync", func(string) string {
		return jsonOf(map[string]bool{"paused": apiToggleSyncPause()})
	})

	// NOT w.Terminate(). See hideAppWindow - terminating from inside a binding
	// callback ends the message loop that is still dispatching that very call,
	// and the window freezes on screen.
	bind("goHide", func(string) string {
		hideAppWindow()
		return ""
	})

	// goReady is the page saying "I have painted, you can show me now".
	bind("goReady", func(string) string {
		revealAppWindow()
		return ""
	})

	// goLog is the one call that worked when nothing else did, which is how the
	// real fault was finally found. It stays exactly as it is.
	bind("goLog", func(msg string) string {
		logf("app window: %s", msg)
		return ""
	})
}
