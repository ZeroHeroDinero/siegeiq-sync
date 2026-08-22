// notify_clips.go - the recorder's tray notifications.
//
// Deliberately quiet. A recorder that pops something up after every round is a
// recorder people switch off, and worse, one that risks stealing focus during a
// match. These fire once per match, after it has ended, and go through the same
// notification switches the player already has for uploads - so turning toasts
// off in the tray turns these off too.
package main

import "fmt"

// notifyClipsSaved tells the player where their footage went, once per match.
func notifyClipsSaved(n int, dir string) {
	if n <= 0 {
		return
	}
	word := "clips"
	if n == 1 {
		word = "clip"
	}
	beepClip(true)
	balloon("Clips saved",
		fmt.Sprintf("SiegeIQ saved %d %s from your last match to %s.", n, word, dir),
		true)
}

// notifyClipFailed reports a cut that did not work. Separate from the success
// path so the sound alone tells a player which one happened.
func notifyClipFailed(reason string) {
	beepClip(false)
	balloon("Couldn't save a clip",
		"The recording was running but the clip could not be cut. "+reason+
			" Open the log from the tray if this keeps happening.",
		false)
}
