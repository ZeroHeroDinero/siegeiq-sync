// coachfetch.go - building the Results tab's picture, off the interface thread.
//
// # WHY THIS IS A BACKGROUND JOB AND NOT A FUNCTION THE PAGE CALLS
//
// Bridge bindings run on the window's message loop. Anything slow there freezes
// the whole app, and this makes three network calls. The same mistake, made once
// with video compression, produced a five minute "Not Responding". So the page
// never waits: it asks for a snapshot, gets whatever is known right now, and
// asks again a moment later. Exactly the pattern clipsend.go uses.
//
// # WHAT MAKES THIS VIEW WORTH BUILDING
//
// Sync is the only piece of software that produced BOTH halves of a match. It
// recorded the footage and it uploaded the replay file, so it already knows
// which clip belongs to which match. The website has to guess at that, or ask.
// Joining them here on match id is the whole reason this tab is better in the
// app than in a browser tab, and it is four lines of code.
//
// # NOTHING IS DECIDED HERE
//
// Plan rules, coaching text and thresholds all stay on the server. This file
// fetches and arranges; it never judges. That is what keeps a change Cipher
// deploys to the website live in this app at the same moment, with no new build.
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// resultsMatch is one match as the tab draws it: the replay numbers, plus the
// clip Sync recorded for it when there is one.
type resultsMatch struct {
	MatchID  string `json:"match_id"`
	Map      string `json:"map"`
	Gamemode string `json:"gamemode"`
	Rounds   int    `json:"rounds"`
	MatchTS  string `json:"match_ts"`
	Focus    string `json:"focus"`
	Kills    *int   `json:"kills"`
	Deaths   *int   `json:"deaths"`

	// ClipPath is a local file when the footage is still on this PC, which is
	// what makes "watch it" instant rather than a download. ClipUploaded says a
	// copy reached SiegeIQ. They are independent: a player can have kept a clip
	// and never sent it, or sent one and since deleted their copy.
	ClipPath     string `json:"clip_path"`
	ClipUploaded bool   `json:"clip_uploaded"`
	ClipStatus   string `json:"clip_status"`
}

type resultsSnapshot struct {
	// State is one of: idle, loading, ready, error. The page shows a spinner for
	// loading and the error text for error, so a failure is visible rather than
	// an empty tab that looks like "you have no matches".
	State string `json:"state"`
	Error string `json:"error"`

	Linked bool   `json:"linked"`
	Plan   string `json:"plan"`
	Name   string `json:"name"`

	// VoiceAllowed mirrors the server's own rule rather than restating it. The
	// tab uses it to show the voice coach as locked instead of pretending it is
	// not there, which is the honest version of an upsell.
	VoiceAllowed bool `json:"voice_allowed"`

	// Coach is which of the four personas this ACCOUNT has chosen. Read from the
	// server rather than stored here, so the app and the website never disagree
	// about who is speaking.
	Coach string `json:"coach"`

	Latest  *resultsMatch  `json:"latest"`
	Recent  []resultsMatch `json:"recent"`
	Totals  map[string]any `json:"totals"`
	FetchAt string         `json:"fetched_at"`
}

var (
	resMu      sync.Mutex
	resState   = resultsSnapshot{State: "idle"}
	resRunning bool
)

// unlimitedPlans are the plans the SERVER treats as paid. Listed here for one
// purpose only: drawing a lock icon before the server has been asked. Every
// actual refusal still comes from the server, so if this list is ever wrong the
// worst case is a lock drawn on something that then works, never a paid feature
// handed out for free.
var unlimitedPlans = map[string]bool{"pro": true, "team": true, "squad": true, "command": true}

func resultsSnapshotNow() resultsSnapshot {
	resMu.Lock()
	defer resMu.Unlock()
	return resState
}

func setResults(fn func(*resultsSnapshot)) {
	resMu.Lock()
	fn(&resState)
	resMu.Unlock()
}

// startResultsFetch kicks off a refresh unless one is already running.
//
// Returns immediately. The guard is not politeness: the page polls, and without
// it every poll while a fetch was in flight would start another one, turning an
// open tab into a slow denial of service against Cipher's own backend.
func startResultsFetch() {
	resMu.Lock()
	if resRunning {
		resMu.Unlock()
		return
	}
	resRunning = true
	resState.State = "loading"
	resState.Error = ""
	resMu.Unlock()

	go func() {
		defer func() {
			resMu.Lock()
			resRunning = false
			resMu.Unlock()
		}()
		fetchResults()
	}()
}

func fetchResults() {
	var cfg config
	loadJSON(configPath(), &cfg)

	if cfg.DeviceToken == "" {
		setResults(func(s *resultsSnapshot) {
			s.State = "ready"
			s.Linked = false
			s.Latest, s.Recent, s.Totals = nil, nil, nil
		})
		return
	}

	fail := func(err error) {
		logf("coaching: could not load your results - %v", err)
		setResults(func(s *resultsSnapshot) {
			s.State = "error"
			s.Error = err.Error()
		})
	}

	// ---- who is this, and what are they entitled to -------------------------
	meRaw, err := coachGet(cfg, "/me")
	if err != nil {
		fail(err)
		return
	}
	var me struct {
		Plan        string `json:"plan"`
		DisplayName string `json:"display_name"`
		R6Username  string `json:"r6_username"`
		CoachKey    string `json:"coach_key"`
	}
	_ = json.Unmarshal(meRaw, &me)

	// ---- the replay side ----------------------------------------------------
	mineRaw, err := coachGet(cfg, "/replay/mine")
	if err != nil {
		fail(err)
		return
	}
	var mine struct {
		Totals  map[string]any `json:"totals"`
		Matches []struct {
			MatchID  string `json:"matchId"`
			Map      string `json:"map"`
			Gamemode string `json:"gamemode"`
			Rounds   int    `json:"rounds"`
			MatchTS  string `json:"matchDate"`
			Focus    string `json:"focus"`
			K        *int   `json:"k"`
			D        *int   `json:"d"`
		} `json:"matches"`
	}
	if err := json.Unmarshal(mineRaw, &mine); err != nil {
		fail(err)
		return
	}

	// ---- the clip side, so the two arrive as one thing ----------------------
	//
	// A failure here is NOT fatal. Somebody who has never sent a clip should see
	// their match numbers, not an error page. The footage is an enrichment.
	// The clips this account has sent, and - the useful part - the only place
	// that maps a MATCH FOLDER to a match id. Sync names its footage by folder;
	// the replay side knows matches by id; this table is where the two meet
	// because /sync/clip/done wrote both. A clip that was never sent therefore
	// cannot be attached to a match yet, which is honest rather than clever: the
	// association genuinely does not exist until something records it.
	uploaded := map[string]string{}   // match id -> upload status
	folderToMatch := map[string]string{}
	if raw, err := deviceGet(cfg, "/sync/clips"); err == nil {
		var cl struct {
			Clips []struct {
				MatchID     string `json:"match_id"`
				MatchFolder string `json:"match_folder"`
				Status      string `json:"status"`
			} `json:"clips"`
		}
		if json.Unmarshal(raw, &cl) == nil {
			for _, c := range cl.Clips {
				if c.MatchID == "" {
					continue
				}
				uploaded[c.MatchID] = c.Status
				if c.MatchFolder != "" {
					folderToMatch[c.MatchFolder] = c.MatchID
				}
			}
		}
	} else {
		// Not fatal on purpose. Somebody who has never sent a clip should see
		// their match numbers, not an error page. Footage is an enrichment.
		logf("coaching: match numbers loaded, clip list did not (%v)", err)
	}

	// Local footage, the half no server can know about. It is why "watch it" is
	// instant here and a download everywhere else.
	local := map[string]string{}
	for folder, path := range localClipsByFolder(cfg.Recorder.ClipDir) {
		if id := folderToMatch[folder]; id != "" {
			local[id] = path
		}
	}

	out := make([]resultsMatch, 0, len(mine.Matches))
	for _, m := range mine.Matches {
		st, up := uploaded[m.MatchID]
		out = append(out, resultsMatch{
			MatchID: m.MatchID, Map: m.Map, Gamemode: m.Gamemode,
			Rounds: m.Rounds, MatchTS: m.MatchTS, Focus: m.Focus,
			Kills: m.K, Deaths: m.D,
			ClipPath: local[m.MatchID], ClipUploaded: up, ClipStatus: st,
		})
	}

	name := me.R6Username
	if name == "" {
		name = me.DisplayName
	}

	setResults(func(s *resultsSnapshot) {
		s.State = "ready"
		s.Error = ""
		s.Linked = true
		s.Plan = me.Plan
		s.Name = name
		s.VoiceAllowed = unlimitedPlans[me.Plan]
		s.Coach = me.CoachKey
		if s.Coach == "" {
			s.Coach = "cipher"
		}
		s.Totals = mine.Totals
		s.Recent = out
		if len(out) > 0 {
			first := out[0]
			s.Latest = &first
		} else {
			s.Latest = nil
		}
		s.FetchAt = time.Now().Format("15:04")
	})
}

// localClipsByFolder maps a match folder name to the newest clip still on this
// PC for it.
//
// Newest rather than first: a player who saved the same match twice wants the
// take they kept, and the later file is the better guess at that. Reading the
// sidecar is what makes this safe to run over a folder inside somebody's Videos
// - a file without one was not written by this app and is never touched.
func localClipsByFolder(root string) map[string]string {
	out := map[string]string{}
	if root == "" {
		return out
	}
	entries, err := listClips(root)
	if err != nil {
		return out
	}
	best := map[string]time.Time{}
	for _, e := range entries {
		meta, err := readClipMeta(e.Path)
		if err != nil || meta.MatchFolder == "" {
			continue
		}
		if when, seen := best[meta.MatchFolder]; seen && !e.Mod.After(when) {
			continue
		}
		best[meta.MatchFolder] = e.Mod
		out[meta.MatchFolder] = e.Path
	}
	return out
}

// readClipMeta reads the sidecar clipstore.go wrote next to a clip.
//
// It lives here rather than beside writeClipMeta because nothing needed to read
// one until now. A missing or unreadable sidecar returns an error and the caller
// skips that file, which is the same rule listClips uses: if this app cannot
// prove it wrote a file, it does not touch it.
func readClipMeta(clipPath string) (clipMeta, error) {
	var m clipMeta
	b, err := os.ReadFile(strings.TrimSuffix(clipPath, filepath.Ext(clipPath)) + ".json")
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return m, err
	}
	return m, nil
}
