//go:build windows

package main

import "runtime"

// COM is thread-affine: the apartment belongs to the thread that initialised it,
// and calling into it from another thread is undefined behaviour that fails
// intermittently rather than immediately.
func lockThread()   { runtime.LockOSThread() }
func unlockThread() { runtime.UnlockOSThread() }
