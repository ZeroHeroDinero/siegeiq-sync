// notify.go - the sound and the balloon that tell a player their match went up.
//
// WHY THIS EXISTS. Before this file, a finished upload changed the tray tooltip and a
// disabled menu item and nothing else. Nobody looks at a tray tooltip. A player had no
// way to know a replay had uploaded, and worse, no way to know one had FAILED - which is
// how somebody discovers at check-in that the match they are about to be asked for was
// never sent.
//
// WHAT IT DELIBERATELY DOES NOT DO. It never opens a message box. dialog.go already has
// MessageBoxW and TaskDialog and either would have been four lines, but a modal window
// takes keyboard focus, and taking focus from somebody in a ranked round is worse than
// the problem being solved. A balloon does not take focus and Windows suppresses it
// during exclusive fullscreen by itself.
//
// TIMING MAKES THIS SAFE. A match folder must be quiet for settleFor (45s) before Sync
// uploads it, so an upload cannot start mid-match by construction. The sound always
// lands after a round has ended.
//
// THE SOUND IS THE GUARANTEED PART. MessageBeep needs no window and cannot fail in a way
// that matters. The balloon needs the tray window that the systray package owns, which
// this file finds by class name - a third-party internal detail that could change on a
// dependency bump. So the balloon is BEST EFFORT and says so in the log when it cannot
// be shown, rather than failing silently and leaving somebody wondering. If the log
// ever fills with "balloon unavailable", the class name below is what changed.
package main

import (
	_ "embed"
	"sync/atomic"
	"syscall"
	"unsafe"
)

// The two notification sounds, compiled into the exe.
//
// They are ours rather than Windows scheme aliases, and that is the point. The
// old sound was SystemAsterisk - whatever the player has mapped to a generic
// notification, shared with every other app on the machine, and on many systems
// a sound they have learned to ignore. These are short, low and specific, so a
// finished upload sounds like SiegeIQ rather than like Windows.
//
// The upload sound. ONE sound, not a menu of them.
//
// A previous build shipped five and let the player choose. That was solving the
// wrong problem: the difficulty was never that people want different sounds, it
// was that the first few sounds were bad. A settings screen is not the place to
// resolve an authoring failure, and every extra option is something that has to
// be understood before it can be ignored.
//
// This one is a dry wooden mallet tap, synthesised for SiegeIQ. It is not a
// Windows scheme sound and not a chime. It is loudness-matched to sit under
// game audio rather than over it: the peak is about -29 dBFS and the short-term
// level about -40 dBFS.
//
// It fades to true zero and carries a triangular dither floor. That is not
// decoration. A decay tail that lands below roughly 250/32768 stops being a
// curve in 16-bit and becomes a staircase of a handful of values, which is
// audible as the crackle that made three earlier attempts unusable. If this is
// ever regenerated, keep the fade and the dither.
//
//go:embed sound_ok.wav
var soundOK []byte

//go:embed sound_fail.wav
var soundFail []byte

// The recorder gets its own, quieter sound. Syncing and recording are separate
// features that can run independently, so "your match uploaded" and "your clip
// was saved" are different events and should not be indistinguishable. This one
// is measured at 45 percent of the sync sound's loudness, because a clip being
// cut is the app doing its job rather than something needing attention.
//
//go:embed sound_clip.wav
var soundClip []byte

var (
	user32           = syscall.NewLazyDLL("user32.dll")
	shell32          = syscall.NewLazyDLL("shell32.dll")
	winmm            = syscall.NewLazyDLL("winmm.dll")
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	procFindWindowW  = user32.NewProc("FindWindowW")
	procShellNotifyW = shell32.NewProc("Shell_NotifyIconW")
	procPlaySoundW   = winmm.NewProc("PlaySoundW")
	procBeep         = kernel32.NewProc("Beep")
)

const (
	nimModify = 0x00000001
	nifInfo   = 0x00000010

	niifInfo    = 0x00000001
	niifWarning = 0x00000002
	niifError   = 0x00000003
	niifNoSound = 0x00000010 // we play our own, so never let the shell play one too

	// PlaySoundW flags. SND_NODEFAULT is the important one: without it, a scheme with
	// no sound mapped to the alias silently "succeeds" by playing the default sound, so
	// we could never tell the difference between working and doing nothing.
	sndAsync     = 0x0001
	sndNodefault = 0x0002
	sndMemory    = 0x0004
	sndFilename  = 0x00020000
	sndAlias     = 0x00010000
)

// The window class the systray package registers on Windows. Checked against
// getlantern/systray v1.2.2. If a dependency bump changes it, the balloon stops and the
// log says so; the sound is unaffected.
const systrayClass = "SystrayClass"

// Both default to on. Set once at startup from the saved config, then read from the
// watch loop and written from the tray menu, so they are atomics rather than plain bools.
var (
	notifySoundOn int32 = 1
	notifyToastOn int32 = 1
)

func setNotifySound(on bool) {
	if on {
		atomic.StoreInt32(&notifySoundOn, 1)
	} else {
		atomic.StoreInt32(&notifySoundOn, 0)
	}
}

func setNotifyToast(on bool) {
	if on {
		atomic.StoreInt32(&notifyToastOn, 1)
	} else {
		atomic.StoreInt32(&notifyToastOn, 0)
	}
}

func notifySoundEnabled() bool { return atomic.LoadInt32(&notifySoundOn) == 1 }
func notifyToastEnabled() bool { return atomic.LoadInt32(&notifyToastOn) == 1 }

// notifyIconData is NOTIFYICONDATAW. Field order and the fixed array sizes are the
// struct contract with the shell - do not reorder or resize them.
type notifyIconData struct {
	CbSize           uint32
	HWnd             syscall.Handle
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            syscall.Handle
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UVersion         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         syscall.GUID
	HBalloonIcon     syscall.Handle
}

func copyUTF16(dst []uint16, s string) {
	enc, err := syscall.UTF16FromString(s)
	if err != nil {
		return
	}
	if len(enc) > len(dst) {
		enc = enc[:len(dst)]
		enc[len(enc)-1] = 0
	}
	copy(dst, enc)
}

// beep plays one short sound. Non-blocking, no window, cannot steal focus.
//
// WHY THIS IS NOT MessageBeep. It was, and on a machine whose sound scheme has nothing
// mapped to the Asterisk event it played nothing at all. MessageBeep's documented
// fallback in that case is the motherboard PC speaker, which desktops have not had for
// years, so it fails to a device that does not exist and reports success either way.
//
// So: try the named system sound first, with SND_NODEFAULT so a missing mapping REPORTS
// failure instead of hiding it, and if that fails synthesise a real tone with
// kernel32!Beep. Since Windows 7 that goes through the default audio device rather than
// the PC speaker, so it is audible on any machine with working sound, whatever the user
// has done to their sound scheme.
var beepPathLogged int32
var soundFileWarned int32

func beep(ok bool) { play(pickSound(ok, false), ok, soundFileFor(ok, false)) }

// beepClip is the recorder's quieter confirmation, kept separate from the sync
// sound so a player can tell the two apart without looking at anything.
func beepClip(ok bool) { play(pickSound(ok, true), ok, soundFileFor(ok, true)) }

func pickSound(ok, recorder bool) []byte {
	if !ok {
		return soundFail
	}
	if recorder {
		return soundClip
	}
	return soundOK
}

// soundFileFor returns a .wav path the player has chosen, or empty.
//
// # WHY THIS EXISTS
//
// Two rounds were spent authoring these sounds and neither landed, because the
// person who has to like them cannot be in the room with the person making
// them. Authoring audio by describing it back and forth does not converge. A
// file path does: Windows ships a folder of professionally produced short
// sounds at C:\Windows\Media, the player already knows which ones they can
// live with, and any .wav on the machine works.
//
// The built-in sounds remain the default, so this changes nothing for anyone
// who never opens the setting.
func soundFileFor(ok, recorder bool) string {
	var c config
	loadJSON(configPath(), &c)
	switch {
	case !ok:
		return c.SoundFailFile
	case recorder:
		return c.SoundClipFile
	default:
		return c.SoundOKFile
	}
}

// play sounds one clip. ok is carried separately from the bytes because the
// Windows fallbacks below still have to pick a success or failure sound, and a
// shipped build must not answer "something happened" when the real answer was
// "that failed".
func play(wav []byte, ok bool, file string) {
	if !notifySoundEnabled() {
		return
	}

	// A file the player picked wins over anything shipped. SND_FILENAME with
	// SND_NODEFAULT means a path that has been moved or deleted reports failure
	// and falls through to the built-in sound, rather than playing nothing at
	// all and looking like the notification never fired.
	if file != "" {
		if p, err := syscall.UTF16PtrFromString(file); err == nil {
			ret, _, _ := procPlaySoundW.Call(uintptr(unsafe.Pointer(p)), 0,
				uintptr(sndFilename|sndAsync|sndNodefault))
			if ret != 0 {
				return
			}
			if atomic.CompareAndSwapInt32(&soundFileWarned, 0, 1) {
				logf("sound: could not play %s, using the built-in sound instead", file)
			}
		}
	}

	// Our own sound, played straight out of the executable's memory. No temp
	// file to write, nothing to clean up, and nothing that depends on what the
	// player has done to their Windows sound scheme.
	if len(wav) > 0 {
		ret, _, _ := procPlaySoundW.Call(uintptr(unsafe.Pointer(&wav[0])), 0,
			uintptr(sndMemory|sndAsync|sndNodefault))
		if ret != 0 {
			return
		}
		if atomic.CompareAndSwapInt32(&beepPathLogged, 0, 1) {
			logf("sound: the built-in sound would not play, falling back to the Windows scheme")
		}
	}

	// Fallbacks, in the order they are least likely to be wrong. See the note
	// above about MessageBeep: a missing scheme mapping must REPORT failure
	// rather than silently routing to a PC speaker that no longer exists.
	alias := "SystemAsterisk"
	if !ok {
		alias = "SystemHand"
	}
	if p, err := syscall.UTF16PtrFromString(alias); err == nil {
		ret, _, _ := procPlaySoundW.Call(uintptr(unsafe.Pointer(p)), 0,
			uintptr(sndAlias|sndAsync|sndNodefault))
		if ret != 0 {
			return
		}
	}
	// Last resort. Beep BLOCKS for the whole duration, so it must not run on the
	// tray event loop or the menu freezes for a fifth of a second every time.
	freq, dur := uintptr(880), uintptr(140)
	if !ok {
		freq, dur = 320, 260
	}
	go func() { _, _, _ = procBeep.Call(freq, dur) }()
}

// balloon shows a tray balloon on the systray icon. Best effort: returns quietly if the
// tray window cannot be found, having said so in the log exactly once per run.
var balloonWarned int32

func balloon(title, text string, ok bool) {
	if !notifyToastEnabled() {
		return
	}
	cls, err := syscall.UTF16PtrFromString(systrayClass)
	if err != nil {
		return
	}
	hwnd, _, _ := procFindWindowW.Call(uintptr(unsafe.Pointer(cls)), 0)
	if hwnd == 0 {
		if atomic.CompareAndSwapInt32(&balloonWarned, 0, 1) {
			logf("balloon unavailable: no %s window found, sounds still work", systrayClass)
		}
		return
	}

	var nid notifyIconData
	nid.CbSize = uint32(unsafe.Sizeof(nid))
	nid.HWnd = syscall.Handle(hwnd)
	nid.UID = 1 // systray registers its icon as id 1
	nid.UFlags = nifInfo
	nid.DwInfoFlags = niifInfo | niifNoSound
	if !ok {
		nid.DwInfoFlags = niifError | niifNoSound
	}
	copyUTF16(nid.SzInfoTitle[:], title)
	copyUTF16(nid.SzInfo[:], text)

	ret, _, _ := procShellNotifyW.Call(uintptr(nimModify), uintptr(unsafe.Pointer(&nid)))
	if ret == 0 && atomic.CompareAndSwapInt32(&balloonWarned, 0, 1) {
		logf("balloon unavailable: the shell refused it, sounds still work")
	}
}

// notifyUploadOK is the quiet one. A player wants reassurance, not a fanfare.
func notifyUploadOK(match string) {
	beep(true)
	balloon("Match uploaded", "SiegeIQ has your replay for "+match+". Nothing else to do.", true)
}

// notifyUploadFailed is the one that actually matters. A silent failure is how somebody
// finds out at check-in that the replay they are being asked for was never sent.
func notifyUploadFailed(match string, reason string) {
	beep(false)
	msg := "SiegeIQ did not get the replay for " + match + "."
	if reason != "" {
		msg += " " + reason
	}
	msg += " Sync will try again. Open the log from the tray if it keeps happening."
	balloon("Upload failed", msg, false)
}

// notifyUnlinked fires when the backend rejects this device's token, which is the one
// failure mode that looks EXACTLY like nothing happening: Sync is running, the tray icon
// is there, and no match will ever upload again until the player re-pairs.
//
// Deduped to once per run on purpose. The watch loop retries on a timer, so alerting on
// every scan would turn a problem into a siren and get the whole feature switched off.
var unlinkedWarned int32

func notifyUnlinked() {
	if !atomic.CompareAndSwapInt32(&unlinkedWarned, 0, 1) {
		return
	}
	beep(false)
	balloon("SiegeIQ Sync needs re-linking",
		"This device is no longer linked, so your matches have stopped uploading. "+
			"Open siegeiq.gg and pair Sync again.", false)
}

// notifyUpdated fires once, on the first run AFTER a self-update installed. The update
// itself is silent by design, so this is the only thing that tells the player it
// happened - which matters when the update is what fixed their problem.
func notifyUpdated(v string) {
	beep(true)
	balloon("SiegeIQ Sync updated",
		"Now running v"+v+". Nothing to do, syncing carries on as normal.", true)
}

// notifyPaired is the one-off on a successful first link.
func notifyPaired() {
	beep(true)
	balloon("SiegeIQ Sync is linked",
		"Your matches will upload on their own from now on. You can close this.", true)
}

// onOff keeps the log lines readable without a conditional at every call site.
func onOff(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

// saveNotifyPrefs writes just the two switches back, preserving everything else in the
// file. It re-reads first rather than holding a config pointer, because the watch loop
// owns that pointer and can rewrite the file underneath the tray goroutine.
func saveNotifyPrefs() {
	var c config
	loadJSON(configPath(), &c)
	c.NotifySoundOff = !notifySoundEnabled()
	c.NotifyToastOff = !notifyToastEnabled()
	saveJSON(configPath(), &c)
}

// shortReason turns an upload error into something a balloon can carry without becoming
// a wall of text. The full error is already in the log.
func shortReason(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	const max = 90
	if len(s) > max {
		s = s[:max] + "..."
	}
	return "Reason: " + s
}
