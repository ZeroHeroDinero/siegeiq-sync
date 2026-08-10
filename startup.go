// startup.go - the "launch when Windows starts" toggle.
//
// This is a single per-user registry value under
// HKCU\Software\Microsoft\Windows\CurrentVersion\Run. Per-user means no admin
// rights are ever needed, and it only ever launches this exact exe. The
// installer sets the same value when you tick "run at startup"; the tray menu
// and the setup checkbox flip it too, and everything stays consistent because
// they all write the same value name and the current exe path.
package main

import (
	"os"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const (
	runKeyPath   = `Software\Microsoft\Windows\CurrentVersion\Run`
	runValueName = "SiegeIQ Sync"
)

// startupEnabled reports whether the Run entry exists and points at THIS exe.
// (If it points somewhere else - e.g. an old install location - we treat it as
// not-us so a fresh toggle rewrites it to the right path.)
func startupEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetStringValue(runValueName)
	if err != nil {
		return false
	}
	exe, err := os.Executable()
	if err != nil {
		return v != ""
	}
	return strings.EqualFold(exePathFromRunValue(v), exe)
}

// exePathFromRunValue pulls the executable out of a Run value that may carry
// arguments after it, e.g. `"C:\...\SiegeIQSync.exe" -startup`.
//
// The -startup flag is how the app knows it was launched by Windows at sign-in
// rather than double-clicked. Launched by hand, it shows its window; launched
// at sign-in, it stays quietly in the tray. Without the flag it cannot tell the
// two apart, and either choice is wrong half the time.
func exePathFromRunValue(v string) string {
	v = strings.TrimSpace(v)
	if strings.HasPrefix(v, `"`) {
		if end := strings.Index(v[1:], `"`); end >= 0 {
			return v[1 : end+1]
		}
	}
	if i := strings.Index(v, " -"); i > 0 {
		return strings.TrimSpace(v[:i])
	}
	return v
}

// setStartup adds or removes the Run entry. The path is quoted so it survives
// spaces (e.g. an install under "Program Files").
func setStartup(on bool) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if on {
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		// The flag tells the app it is being started by Windows, so it does
		// not throw a window at somebody who has just signed in.
		return k.SetStringValue(runValueName, `"`+exe+`" -startup`)
	}
	err = k.DeleteValue(runValueName)
	if err == registry.ErrNotExist {
		return nil // already off - not an error
	}
	return err
}
