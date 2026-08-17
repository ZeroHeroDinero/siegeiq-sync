// overlaywin_windows.go - the between-rounds reminder, drawn over the game.
//
// # WHY THIS IS A PLAIN WIN32 LAYERED WINDOW AND NOT WEBVIEW2
//
// overlay.html already exists and is styled, so reusing it looks like the obvious
// move. It is the wrong one here, for three reasons:
//
//  1. WebView2 means an Edge process sitting on top of a game that is trying to
//     hold 144 frames a second. This window is a rectangle with one line of text
//     in it. A browser is several hundred megabytes of machinery to draw that.
//  2. Transparency. A layered window gets it from the OS in one call. WebView2
//     needs put_DefaultBackgroundColor through a COM interface that go-webview2
//     does not expose, and the failure mode is an opaque panel covering the game.
//  3. Anything injected over a competitive shooter should be as small and as dull
//     as it can possibly be. A GDI rectangle is easy to explain to Ubisoft. A
//     embedded browser is not.
//
// The fade is free as a result: SetLayeredWindowAttributes takes a global alpha,
// so fading is a number going down on a timer rather than a compositing problem.
//
// # WHAT THIS WINDOW CANNOT DO, ON PURPOSE
//
// It never reads the game. It has no idea what is happening in a round. It is
// shown by one signal only - a .rec file landing on disk, which is Siege telling
// the filesystem a round just ended - and it shows text the player already chose
// on their own dashboard. It is click-through, never takes focus, and never
// appears while a round is being played.
package main

import (
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

var (
	modGdi32 = syscall.NewLazyDLL("gdi32.dll")

	procCreateWindowExW   = modUser32.NewProc("CreateWindowExW")
	procDefWindowProcW    = modUser32.NewProc("DefWindowProcW")
	procRegisterClassExW  = modUser32.NewProc("RegisterClassExW")
	procSetWindowPos      = modUser32.NewProc("SetWindowPos")
	procSetLayeredWinAttr = modUser32.NewProc("SetLayeredWindowAttributes")
	procGetMessageW       = modUser32.NewProc("GetMessageW")
	procTranslateMessage  = modUser32.NewProc("TranslateMessage")
	procDispatchMessageW  = modUser32.NewProc("DispatchMessageW")
	procInvalidateRect    = modUser32.NewProc("InvalidateRect")
	procBeginPaint        = modUser32.NewProc("BeginPaint")
	procEndPaint          = modUser32.NewProc("EndPaint")
	procFillRect          = modUser32.NewProc("FillRect")
	procDrawTextW         = modUser32.NewProc("DrawTextW")
	procGetModuleHandleW  = syscall.NewLazyDLL("kernel32.dll").NewProc("GetModuleHandleW")

	procCreateSolidBrush = modGdi32.NewProc("CreateSolidBrush")
	procDeleteObject     = modGdi32.NewProc("DeleteObject")
	procCreateFontW      = modGdi32.NewProc("CreateFontW")
	procSelectObject     = modGdi32.NewProc("SelectObject")
	procSetTextColor     = modGdi32.NewProc("SetTextColor")
	procSetBkMode        = modGdi32.NewProc("SetBkMode")
)

const (
	wsExLayered     = 0x00080000
	wsExTransparent = 0x00000020
	wsExToolWindow  = 0x00000080
	wsExNoActivate  = 0x08000000
	wsExTopmost     = 0x00000008
	wsPopup         = 0x80000000

	// swHide lives in ui_window_windows.go; only the no-activate show is new here.
	swShowNA  = 8
	lwaAlpha  = 0x00000002
	lwaColKey = 0x00000001

	swpNoActivate = 0x0010
	swpNoSize     = 0x0001
	hwndTopmost   = ^uintptr(0) // (HWND)-1

	wmPaint   = 0x000F
	wmDestroy = 0x0002

	dtLeft         = 0x0000
	dtWordEllipsis = 0x00040000
	dtNoPrefix     = 0x0800

	transparentBk = 1

	// The colour key. Any pixel painted exactly this colour becomes a hole in the
	// window. Magenta because nothing in the design will ever legitimately be it.
	colourKey = 0x00FF00FF

	overlayW = 460
	overlayH = 74
)

type wndClassExW struct {
	cbSize, style               uint32
	lpfnWndProc                 uintptr
	cbClsExtra, cbWndExtra      int32
	hInstance, hIcon, hCursor   uintptr
	hbrBackground               uintptr
	lpszMenuName, lpszClassName *uint16
	hIconSm                     uintptr
}

type msgW struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      struct{ x, y int32 }
}

type paintStruct struct {
	hdc         uintptr
	fErase      int32
	rcPaint     rectW
	fRestore    int32
	fIncUpdate  int32
	rgbReserved [32]byte
}

type rectW struct{ left, top, right, bottom int32 }

var (
	ovMu      sync.Mutex
	ovHwnd    uintptr
	ovText    string
	ovStarted bool
)

// overlayShow puts one line on screen and fades it out. Safe to call from any
// goroutine and safe to call when the overlay is switched off.
func overlayShow(line string) {
	if !rec.settings().OverlayOn {
		return
	}
	overlayShowForced(line)
}

// overlayShowForced skips the on/off check. Only the preview button uses it.
func overlayShowForced(line string) {
	if line == "" {
		return
	}
	ensureOverlayThread()

	ovMu.Lock()
	ovText = line
	hwnd := ovHwnd
	ovMu.Unlock()
	if hwnd == 0 {
		return
	}

	positionOverlay(hwnd)
	procInvalidateRect.Call(hwnd, 0, 1)
	procShowWindow.Call(hwnd, swShowNA)
	setOverlayAlpha(hwnd, 255)

	go fadeOverlay(hwnd)
}

// fadeOverlay holds the line, then takes it away.
//
// Six seconds is long enough to read one sentence twice and short enough that it
// is gone before the next round's prep phase matters. The fade is a second on top
// of that, because something vanishing is more distracting than something leaving.
func fadeOverlay(hwnd uintptr) {
	time.Sleep(6 * time.Second)
	for a := 255; a > 0; a -= 15 {
		setOverlayAlpha(hwnd, byte(a))
		time.Sleep(60 * time.Millisecond)
	}
	procShowWindow.Call(hwnd, swHide)
}

func setOverlayAlpha(hwnd uintptr, a byte) {
	procSetLayeredWinAttr.Call(hwnd, uintptr(colourKey), uintptr(a), lwaAlpha|lwaColKey)
}

// positionOverlay parks the window over Siege's own client area, low and left,
// where Siege itself draws nothing important between rounds.
func positionOverlay(hwnd uintptr) {
	x, y := int32(60), int32(60)
	if sh, ok := siegeWindow(); ok {
		if r, err := clientRectOnScreen(sh); err == nil {
			x = int32(r.X + 48)
			y = int32(r.Y + r.H - overlayH - 130)
		}
	}
	procSetWindowPos.Call(hwnd, hwndTopmost, uintptr(x), uintptr(y),
		uintptr(overlayW), uintptr(overlayH), swpNoActivate)
}

func ensureOverlayThread() {
	ovMu.Lock()
	if ovStarted {
		ovMu.Unlock()
		return
	}
	ovStarted = true
	ovMu.Unlock()
	go overlayThread()
	// Give the window a moment to exist before the first show tries to use it.
	time.Sleep(250 * time.Millisecond)
}

// overlayThread owns the window for the life of the app.
//
// LockOSThread is not optional. A window belongs to the thread that created it and
// only that thread may pump its messages; without this the Go runtime would move
// the goroutine and the window would stop repainting with no error anywhere.
func overlayThread() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	hInst, _, _ := procGetModuleHandleW.Call(0)
	className, _ := syscall.UTF16PtrFromString("SiegeIQOverlay")

	wndProc := syscall.NewCallback(func(hwnd uintptr, msg uint32, wp, lp uintptr) uintptr {
		switch msg {
		case wmPaint:
			paintOverlay(hwnd)
			return 0
		case wmDestroy:
			return 0
		}
		r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wp, lp)
		return r
	})

	keyBrush, _, _ := procCreateSolidBrush.Call(uintptr(colourKey))
	wc := wndClassExW{
		cbSize:        uint32(unsafe.Sizeof(wndClassExW{})),
		lpfnWndProc:   wndProc,
		hInstance:     hInst,
		hbrBackground: keyBrush,
		lpszClassName: className,
	}
	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	title, _ := syscall.UTF16PtrFromString("SiegeIQ")
	hwnd, _, _ := procCreateWindowExW.Call(
		uintptr(wsExLayered|wsExTransparent|wsExToolWindow|wsExNoActivate|wsExTopmost),
		uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(title)),
		uintptr(uint32(wsPopup)),
		60, 60, overlayW, overlayH,
		0, 0, hInst, 0)
	if hwnd == 0 {
		logf("overlay: could not create the window; the in-game reminder is off for this session")
		return
	}

	ovMu.Lock()
	ovHwnd = hwnd
	ovMu.Unlock()
	setOverlayAlpha(hwnd, 0)

	var m msgW
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			return
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func paintOverlay(hwnd uintptr) {
	var ps paintStruct
	hdc, _, _ := procBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	defer procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	if hdc == 0 {
		return
	}

	ovMu.Lock()
	line := ovText
	ovMu.Unlock()

	full := rectW{0, 0, overlayW, overlayH}
	// Everything outside the card is painted the key colour, which the OS turns
	// into a hole. That is what makes this a floating card rather than a box.
	keyBrush, _, _ := procCreateSolidBrush.Call(uintptr(colourKey))
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&full)), keyBrush)
	procDeleteObject.Call(keyBrush)

	// COLORREF is 0x00BBGGRR, not RGB. Getting this backwards is the classic way
	// to end up with an orange panel where a blue one was intended.
	panel := rectW{0, 0, overlayW, overlayH}
	panelBrush, _, _ := procCreateSolidBrush.Call(uintptr(0x001B0F07)) // #070F1B
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&panel)), panelBrush)
	procDeleteObject.Call(panelBrush)

	accent := rectW{0, 0, 3, overlayH}
	accentBrush, _, _ := procCreateSolidBrush.Call(uintptr(0x00D1AE18)) // #18AED1
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&accent)), accentBrush)
	procDeleteObject.Call(accentBrush)

	procSetBkMode.Call(hdc, transparentBk)

	capFont, _, _ := procCreateFontW.Call(13, 0, 0, 0, 700, 0, 0, 0, 0, 0, 0, 0, 0, 0)
	old, _, _ := procSelectObject.Call(hdc, capFont)
	procSetTextColor.Call(hdc, uintptr(0x00D1AE18))
	capR := rectW{18, 10, overlayW - 14, 26}
	capTxt, _ := syscall.UTF16PtrFromString("SIEGEIQ FOCUS")
	procDrawTextW.Call(hdc, uintptr(unsafe.Pointer(capTxt)), ^uintptr(0),
		uintptr(unsafe.Pointer(&capR)), dtLeft|dtNoPrefix)
	procSelectObject.Call(hdc, old)
	procDeleteObject.Call(capFont)

	txtFont, _, _ := procCreateFontW.Call(19, 0, 0, 0, 600, 0, 0, 0, 0, 0, 0, 0, 0, 0)
	old2, _, _ := procSelectObject.Call(hdc, txtFont)
	procSetTextColor.Call(hdc, uintptr(0x00F8F5EA)) // #EAF5F8
	txtR := rectW{18, 28, overlayW - 14, overlayH - 8}
	t, _ := syscall.UTF16PtrFromString(line)
	procDrawTextW.Call(hdc, uintptr(unsafe.Pointer(t)), ^uintptr(0),
		uintptr(unsafe.Pointer(&txtR)), dtLeft|dtNoPrefix|dtWordEllipsis)
	procSelectObject.Call(hdc, old2)
	procDeleteObject.Call(txtFont)
}
