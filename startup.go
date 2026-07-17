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
	return strings.EqualFold(strings.Trim(v, `"`), exe)
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
		return k.SetStringValue(runValueName, `"`+exe+`"`)
	}
	err = k.DeleteValue(runValueName)
	if err == registry.ErrNotExist {
		return nil // already off - not an error
	}
	return err
}
