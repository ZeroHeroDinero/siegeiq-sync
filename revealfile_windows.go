// revealfile_windows.go - showing one clip in Explorer with the file selected.
//
// The tray has always had "Open clips folder", which drops the player into a
// folder holding up to a hundred files named by timestamp. Finding the clip they
// were just looking at in the app is then a job of reading dates. This opens the
// same folder with the right file already highlighted.
package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// revealInExplorer opens Explorer at the folder holding path, with that file
// selected.
//
// The command line is built by hand rather than left to exec.Command's own
// quoting. Explorer parses its arguments itself and does not follow the usual
// rules: with the whole "/select,C:\..." token wrapped in quotes - which is what
// Go does the moment the path contains a space - Explorer gives up and opens
// Documents instead. The quotes belong around the PATH and nothing else, and the
// default clips folder lives under a Windows user name, so a space in it is the
// normal case rather than the edge one.
func revealInExplorer(path string) {
	p, err := filepath.Abs(path)
	if err != nil {
		p = path
	}
	p = filepath.Clean(p)
	// A double quote cannot legally appear in a Windows path, so this can only
	// ever be junk - but left in place it would close the quoted section early
	// and hand the remainder to Explorer as further arguments.
	p = strings.ReplaceAll(p, `"`, "")
	cmd := exec.Command("explorer.exe")
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `explorer.exe /select,"` + p + `"`}
	// Explorer returns exit code 1 even on success, so its error is not worth
	// reading and definitely not worth showing anyone.
	_ = cmd.Start()
}
