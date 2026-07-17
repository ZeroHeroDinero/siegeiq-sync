// dialog.go - the branded setup popups.
//
// Everything here is a native Windows TaskDialog (TaskDialogIndirect, from
// comctl32.dll) - the same modern dialog Windows itself uses. There is NO
// third-party GUI toolkit and no cgo: just a syscall plus two structs that
// mirror the C TASKDIALOGCONFIG / TASKDIALOG_BUTTON layouts field-for-field.
// This is deliberately conservative - an earlier attempt at a fully custom
// window (github.com/lxn/walk) hit an unfixable internal tooltip bug, so we
// stick to the OS's own dialog and just dress it up: our real icon, a bold
// heading, a footer trust line with a shield, an "Open siegeiq.gg" button, and
// an optional "launch at startup" checkbox. If TaskDialog can't be shown for
// any reason (e.g. a stripped-down Windows build), every popup falls back to a
// plain MessageBox so the player still sees the message.
package main

import (
	_ "embed"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"
)

//go:embed siegeiq_icon.ico
var iconICO []byte

// iconTempFile writes the embedded .ico out once so Win32's LoadImageW (which
// wants a file path, not raw bytes) can load it, without shipping a separate
// .ico next to the .exe.
func iconTempFile() string {
	p := filepath.Join(os.TempDir(), "siegeiq_sync_icon.ico")
	if _, err := os.Stat(p); err != nil {
		_ = os.WriteFile(p, iconICO, 0o644)
	}
	return p
}

// messageBox pops a plain native message box (user32.dll). Standard-library
// only. Kept as the emergency fallback the branded dialog uses if
// TaskDialogIndirect ever fails to show.
func messageBox(title, text string) {
	user32 := syscall.NewLazyDLL("user32.dll")
	proc := user32.NewProc("MessageBoxW")
	textPtr, _ := syscall.UTF16PtrFromString(text)
	titlePtr, _ := syscall.UTF16PtrFromString(title)
	proc.Call(0, uintptr(unsafe.Pointer(textPtr)), uintptr(unsafe.Pointer(titlePtr)), 0x40) // MB_ICONINFORMATION
}

// --- Native Windows TaskDialog -----------------------------------------------

const (
	tdfUseHiconMain        = 0x0002
	tdfAllowCancellation   = 0x0008
	tdfVerificationChecked = 0x0100
	tdfSizeToContent       = 0x01000000
)

// tdShieldIcon is MAKEINTRESOURCE(-4) - the system shield glyph, used in the
// footer to signal "this is the safe, read-only trust note".
const tdShieldIcon = 0xFFFC

// Custom button IDs - anything outside the 1-8 range Windows reserves for
// common buttons (IDOK, IDCANCEL, ...) is fair game.
const (
	btnIDClose    = 101
	btnIDConfirm  = 102
	btnIDOpenSite = 103
)

type taskDialogButton struct {
	nButtonID     int32
	pszButtonText *uint16
}

type taskDialogConfig struct {
	cbSize                  uint32
	hwndParent              uintptr
	hInstance               uintptr
	dwFlags                 uint32
	dwCommonButtons         uint32
	pszWindowTitle          *uint16
	mainIcon                uintptr // union: HICON (TDF_USE_HICON_MAIN) or PCWSTR
	pszMainInstruction      *uint16
	pszContent              *uint16
	cButtons                uint32
	pButtons                uintptr
	nDefaultButton          int32
	cRadioButtons           uint32
	pRadioButtons           uintptr
	nDefaultRadioButton     int32
	pszVerificationText     *uint16
	pszExpandedInformation  *uint16
	pszExpandedControlText  *uint16
	pszCollapsedControlText *uint16
	footerIcon              uintptr
	pszFooter               *uint16
	pfCallback              uintptr
	lpCallbackData          uintptr
	cxWidth                 uint32
}

// dialogSpec is the friendly description a caller fills in; taskDialog turns it
// into the raw TASKDIALOGCONFIG.
type dialogSpec struct {
	instruction   string // big heading line
	content       string // body text
	footer        string // small footer line; "" = none
	verifyText    string // checkbox label; "" = no checkbox
	verifyChecked bool   // initial checkbox state
	buttonText    string // label for the dismiss button; "" -> "Close"
	confirmText   string // label for an affirmative default button; "" = none
	openSiteURL   string // if set, adds an "open a link" button that reopens the dialog
	openSiteText  string // label for that button; "" -> "Open siegeiq.gg"
}

// loadIconHandle loads our embedded .ico as a real HICON via LoadImageW, for
// TDF_USE_HICON_MAIN. Returns 0 if it can't be loaded, in which case the dialog
// just shows no main icon rather than failing.
func loadIconHandle() uintptr {
	user32 := syscall.NewLazyDLL("user32.dll")
	loadImage := user32.NewProc("LoadImageW")
	pathPtr, err := syscall.UTF16PtrFromString(iconTempFile())
	if err != nil {
		return 0
	}
	const imageIcon = 1
	const lrLoadFromFile = 0x00000010
	const lrDefaultSize = 0x00000040
	h, _, _ := loadImage.Call(0, uintptr(unsafe.Pointer(pathPtr)), imageIcon, 0, 0, lrLoadFromFile|lrDefaultSize)
	return h
}

// taskDialog shows one native TaskDialog. It returns the clicked button ID, the
// final checkbox state, and shown=false if the dialog couldn't be shown at all
// (so the caller can fall back to a plain MessageBox).
func taskDialog(spec dialogSpec) (clicked int32, verified bool, shown bool) {
	// TaskDialog is happiest with COM initialized on its thread; best-effort
	// only - it still works without this on most systems.
	ole32 := syscall.NewLazyDLL("ole32.dll")
	if coInit := ole32.NewProc("CoInitializeEx"); coInit.Find() == nil {
		coInit.Call(0, 2) // COINIT_APARTMENTTHREADED
	}

	comctl32 := syscall.NewLazyDLL("comctl32.dll")
	proc := comctl32.NewProc("TaskDialogIndirect")
	if proc.Find() != nil {
		return 0, false, false
	}

	title, _ := syscall.UTF16PtrFromString("SiegeIQ Sync")
	instruction, _ := syscall.UTF16PtrFromString(spec.instruction)
	contentPtr, _ := syscall.UTF16PtrFromString(spec.content)

	// Build the button row: optional "open link" button, optional affirmative
	// (default) button, then the dismiss button - which is always present so the
	// dialog can always be closed.
	var buttons []taskDialogButton
	if spec.openSiteURL != "" {
		label := spec.openSiteText
		if label == "" {
			label = "Open siegeiq.gg"
		}
		p, _ := syscall.UTF16PtrFromString(label)
		buttons = append(buttons, taskDialogButton{nButtonID: btnIDOpenSite, pszButtonText: p})
	}
	defaultButton := int32(btnIDClose)
	if spec.confirmText != "" {
		p, _ := syscall.UTF16PtrFromString(spec.confirmText)
		buttons = append(buttons, taskDialogButton{nButtonID: btnIDConfirm, pszButtonText: p})
		defaultButton = btnIDConfirm
	}
	dismiss := spec.buttonText
	if dismiss == "" {
		dismiss = "Close"
	}
	dp, _ := syscall.UTF16PtrFromString(dismiss)
	buttons = append(buttons, taskDialogButton{nButtonID: btnIDClose, pszButtonText: dp})

	cfg := taskDialogConfig{
		dwFlags:            tdfAllowCancellation | tdfSizeToContent,
		pszWindowTitle:     title,
		pszMainInstruction: instruction,
		pszContent:         contentPtr,
		cButtons:           uint32(len(buttons)),
		pButtons:           uintptr(unsafe.Pointer(&buttons[0])),
		nDefaultButton:     defaultButton,
	}
	cfg.cbSize = uint32(unsafe.Sizeof(cfg))

	if icon := loadIconHandle(); icon != 0 {
		cfg.dwFlags |= tdfUseHiconMain
		cfg.mainIcon = icon
	}
	if spec.footer != "" {
		footerPtr, _ := syscall.UTF16PtrFromString(spec.footer)
		cfg.pszFooter = footerPtr
		cfg.footerIcon = tdShieldIcon
	}
	if spec.verifyText != "" {
		verifyPtr, _ := syscall.UTF16PtrFromString(spec.verifyText)
		cfg.pszVerificationText = verifyPtr
		if spec.verifyChecked {
			cfg.dwFlags |= tdfVerificationChecked
		}
	}

	var clickedID int32
	var verifiedFlag int32
	ret, _, _ := proc.Call(
		uintptr(unsafe.Pointer(&cfg)),
		uintptr(unsafe.Pointer(&clickedID)),
		0,
		uintptr(unsafe.Pointer(&verifiedFlag)),
	)
	// Keep cfg (and the strings/buttons it points at) alive until the call
	// returns - the dialog is modal so this always holds, but be explicit.
	runtime.KeepAlive(buttons)
	runtime.KeepAlive(&cfg)
	if ret != 0 { // non-zero HRESULT = failed
		return 0, false, false
	}
	return clickedID, verifiedFlag != 0, true
}

// runDialog shows the dialog on its own dedicated, locked OS thread and blocks
// until it closes. If the player clicks the "Open siegeiq.gg" button we open the
// link and reopen the dialog (carrying the checkbox state) so the code/steps
// stay on screen. Returns the dismissing button ID and the checkbox state;
// shown=false means it fell back to a plain MessageBox.
func runDialog(spec dialogSpec) (clicked int32, verified bool, shown bool) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		// The recover MUST live in this same goroutine - a panic on one
		// goroutine is never caught by a recover on another.
		defer func() {
			if r := recover(); r != nil {
				logf("branded dialog failed (%v) - falling back to a plain dialog", r)
				shown = false
			}
		}()

		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		cur := spec
		for {
			c, v, ok := taskDialog(cur)
			if !ok {
				shown = false
				return
			}
			clicked, verified, shown = c, v, true
			if c == btnIDOpenSite {
				openURL(spec.openSiteURL)
				cur.verifyChecked = v // keep the checkbox where the user left it
				continue
			}
			return
		}
	}()
	<-done
	return
}

// showDialog shows an informational popup and returns the final checkbox state
// (false when there is no checkbox or it fell back).
func showDialog(spec dialogSpec) (verified bool) {
	_, v, shown := runDialog(spec)
	if !shown {
		messageBox("SiegeIQ Sync", spec.instruction+"\r\n\r\n"+spec.content)
		return spec.verifyChecked
	}
	return v
}

// showConfirm shows a two-button (confirm + dismiss) popup and reports whether
// the affirmative button was clicked, plus the checkbox state. On fallback it
// returns (false, verifyChecked) so nothing destructive happens by default.
func showConfirm(spec dialogSpec) (confirmed bool, verified bool) {
	c, v, shown := runDialog(spec)
	if !shown {
		messageBox("SiegeIQ Sync", spec.instruction+"\r\n\r\n"+spec.content)
		return false, spec.verifyChecked
	}
	return c == btnIDConfirm, v
}

// openURL opens a link in the player's default browser.
func openURL(url string) {
	_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}

// openInNotepad opens the sync log so a troubleshooting player can see exactly
// what Sync has been doing - the on-demand equivalent of the old console window.
func openInNotepad(path string) {
	_ = exec.Command("notepad.exe", path).Start()
}
