// gamesettings.go - reading Rainbow Six Siege's own settings file.
//
// WHY THIS EXISTS
//
//	Players who change their sensitivity every few days never build the muscle
//	memory that makes aim consistent, and nobody measures it. Siege writes its
//	settings to a plain INI file that sits next to the replay folder Sync is
//	already watching, so the change history is readable without asking the
//	player to type anything or remember anything.
//
// WHAT IT WILL NOT READ
//
//	Anything outside GameSettings.ini, and only the sections named below even
//	inside it. [HARDWARE_INFO] is deliberately excluded: it is the one section
//	that describes the machine rather than the game, and "we read your graphics
//	settings" is a sentence worth being able to say without an asterisk.
//	[ONLINE], [AUDIO] and [GAMEPLAY] are excluded for the same reason.
//
// WHAT IS NOT IN THE FILE AT ALL
//
//	Mouse DPI. The game cannot see it, so no Siege version has ever had a key
//	for it. eDPI and cm/360 therefore stay empty until the player types a DPI
//	into the Sync settings window, and every consumer has to cope with that.
package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// perfSections are the only sections the performance group ever reads. A fixed
// list rather than "everything except X" so that a section Ubisoft adds in a
// future season is excluded by default instead of included by accident.
var perfSections = []string{"DISPLAY_SETTINGS", "QUALITY_SETTINGS"}

type aimBlock struct {
	// MouseSensitivity is the 0-100 number the Siege menu shows and the one a
	// player says out loud. MouseYawSensitivity is a different, lower level value
	// in the same section; charting that one would give them a graph they do not
	// recognise as their own settings.
	MouseSens      *float64 `json:"mouse_sens,omitempty"`
	MouseYaw       *float64 `json:"mouse_yaw,omitempty"`
	MousePitch     *float64 `json:"mouse_pitch,omitempty"`
	SensMultiplier *float64 `json:"sens_multiplier,omitempty"`
	ADSMouse       *float64 `json:"ads_mouse,omitempty"`
	XFactorAiming  *float64 `json:"xfactor_aiming,omitempty"`
	RawInput       *bool    `json:"raw_input,omitempty"`
	FOV            *float64 `json:"fov,omitempty"`
}

type settingsSnapshot struct {
	Aim      *aimBlock         `json:"aim,omitempty"`
	Perf     map[string]string `json:"perf,omitempty"`
	MouseDPI int               `json:"mouse_dpi,omitempty"`
	AimHash  string            `json:"aim_hash"`
	PerfHash string            `json:"perf_hash,omitempty"`
}

// gameSettingsPath finds GameSettings.ini.
//
// The cheap case first: it lives one folder up from the MatchReplay directory
// the watch loop already resolved. The fallback exists because a Steam install
// can put MatchReplay inside the game folder while Siege still writes its
// settings under Documents, so the two paths are not always related.
func gameSettingsPath(replayDir string) string {
	if replayDir != "" {
		p := filepath.Join(filepath.Dir(replayDir), "GameSettings.ini")
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	root := filepath.Join(home, "Documents", "My Games", "Rainbow Six - Siege")
	entries, err := os.ReadDir(root)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		p := filepath.Join(root, e.Name(), "GameSettings.ini")
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

// parseINI reads section -> key -> value. Keys are lower-cased so a rename that
// only changes capitalisation does not read as a settings change.
func parseINI(path string) (map[string]map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	out := map[string]map[string]string{}
	section := ""
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 512*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.ToUpper(strings.TrimSpace(line[1 : len(line)-1]))
			if out[section] == nil {
				out[section] = map[string]string{}
			}
			continue
		}
		eq := strings.Index(line, "=")
		if eq <= 0 || section == "" {
			continue
		}
		k := strings.ToLower(strings.TrimSpace(line[:eq]))
		v := strings.TrimSpace(line[eq+1:])
		out[section][k] = v
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func numOf(m map[string]string, key string) *float64 {
	if m == nil {
		return nil
	}
	raw, ok := m[strings.ToLower(key)]
	if !ok {
		return nil
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return nil
	}
	return &v
}

func boolOf(m map[string]string, key string) *bool {
	n := numOf(m, key)
	if n == nil {
		return nil
	}
	b := *n != 0
	return &b
}

// readGameSettings builds the snapshot for whichever groups are switched on.
// Returns nil when both are off, the file is missing, or nothing readable came
// out of it - callers treat nil as "nothing to send", never as an error worth
// bothering the player about.
func readGameSettings(replayDir string, wantAim, wantPerf bool, dpi int) *settingsSnapshot {
	if !wantAim && !wantPerf {
		return nil
	}
	path := gameSettingsPath(replayDir)
	if path == "" {
		return nil
	}
	ini, err := parseINI(path)
	if err != nil {
		logf("settings: could not read %s: %v", path, err)
		return nil
	}

	snap := &settingsSnapshot{}

	if wantAim {
		in := ini["INPUT"]
		disp := ini["DISPLAY_SETTINGS"]
		a := &aimBlock{
			MouseSens:      numOf(in, "MouseSensitivity"),
			MouseYaw:       numOf(in, "MouseYawSensitivity"),
			MousePitch:     numOf(in, "MousePitchSensitivity"),
			SensMultiplier: numOf(in, "MouseSensitivityMultiplierUnit"),
			ADSMouse:       numOf(in, "AimDownSightsMouse"),
			XFactorAiming:  numOf(in, "XFactorAiming"),
			RawInput:       boolOf(in, "RawInputMouseKeyboard"),
			FOV:            numOf(disp, "DefaultFOV"),
		}
		if a.MouseSens != nil || a.MouseYaw != nil || a.SensMultiplier != nil || a.ADSMouse != nil {
			snap.Aim = a
			snap.MouseDPI = dpi
			snap.AimHash = hashFields(
				fnum(a.MouseSens), fnum(a.MouseYaw), fnum(a.MousePitch), fnum(a.SensMultiplier),
				fnum(a.ADSMouse), fnum(a.XFactorAiming), fbool(a.RawInput), fnum(a.FOV),
				strconv.Itoa(dpi),
			)
		}
	}

	if wantPerf {
		perf := map[string]string{}
		for _, sec := range perfSections {
			for k, v := range ini[sec] {
				perf[strings.ToLower(sec)+"."+k] = v
			}
		}
		if len(perf) > 0 {
			snap.Perf = perf
			keys := make([]string, 0, len(perf))
			for k := range perf {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			parts := make([]string, 0, len(keys))
			for _, k := range keys {
				parts = append(parts, k+"="+perf[k])
			}
			snap.PerfHash = hashFields(parts...)
		}
	}

	if snap.Aim == nil && snap.Perf == nil {
		return nil
	}
	if snap.AimHash == "" {
		// The backend keys change detection off aim_hash and the column is NOT
		// NULL, so a perf-only snapshot still needs a stable value here.
		snap.AimHash = "perf-only:" + snap.PerfHash
	}
	return snap
}

func fnum(p *float64) string {
	if p == nil {
		return "-"
	}
	return strconv.FormatFloat(*p, 'f', 6, 64)
}

func fbool(p *bool) string {
	if p == nil {
		return "-"
	}
	if *p {
		return "1"
	}
	return "0"
}

func hashFields(parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(h[:])[:32]
}
