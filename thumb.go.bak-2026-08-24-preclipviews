// thumb.go - one still frame per clip, so the app window shows pictures rather
// than a list of filenames.
//
// Thumbnails are generated lazily and cached next to the clip. Doing it at clip
// creation time would add a second of work to the moment a match ends, which is
// exactly when the machine is busiest and the player is least patient.
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// thumbPath is the cached jpeg for a clip. Kept beside the clip and beside its
// json sidecar, so deleting a clip through the window removes all three and
// leaves nothing orphaned.
func thumbPath(clipPath string) string {
	return strings.TrimSuffix(clipPath, filepath.Ext(clipPath)) + ".jpg"
}

var thumbMu sync.Mutex

// ensureThumb returns the path to a clip's thumbnail, making it if needed.
// Returns "" when it cannot be produced - the window then shows a placeholder
// rather than a broken image.
func ensureThumb(rc recorderConfig, clipPath string) string {
	out := thumbPath(clipPath)
	if st, err := os.Stat(out); err == nil && st.Size() > 0 {
		return out
	}

	ff, err := findFFmpeg(rc)
	if err != nil {
		return ""
	}

	thumbMu.Lock()
	defer thumbMu.Unlock()
	// Re-check under the lock: two window refreshes can race on the same clip.
	if st, err := os.Stat(out); err == nil && st.Size() > 0 {
		return out
	}

	// One second in, rather than frame zero. The first frame of a trimmed clip
	// is often the tail of a loading screen or a fade, which makes every
	// thumbnail look identical and useless.
	cmd := exec.Command(ff,
		"-hide_banner", "-loglevel", "error", "-nostdin", "-y",
		"-ss", "1",
		"-i", clipPath,
		"-frames:v", "1",
		"-vf", "scale=360:-2",
		"-q:v", "5",
		out,
	)
	hideConsole(cmd)
	if err := cmd.Run(); err != nil {
		// A clip shorter than a second lands here. Try frame zero before giving up.
		cmd = exec.Command(ff,
			"-hide_banner", "-loglevel", "error", "-nostdin", "-y",
			"-i", clipPath, "-frames:v", "1", "-vf", "scale=360:-2", "-q:v", "5", out)
		hideConsole(cmd)
		if err := cmd.Run(); err != nil {
			return ""
		}
	}
	if st, err := os.Stat(out); err != nil || st.Size() == 0 {
		return ""
	}
	return out
}
