// tournamentwatch.go - answering "is a SiegeIQ tournament match happening right
// now", which is the one fact ModeTournament needs and never had.
//
// # THE BUG THIS FIXES
//
// recorder.tournamentActive was declared, read in two places, and written by
// nothing. It was therefore false for the entire life of every build that ever
// shipped, so "Tournament matches only" buffered nothing, ever, and
// AutoUploadTournament never routed a single clip down the tournament path. The
// settings card said NOT WIRED UP YET and was telling the truth.
//
// Nothing about it was hard. The consumer was written first and the producer was
// never written, which is the same shape as the keep rules that needed decoded
// round detail: a feature that looks finished from the inside and does nothing.
//
// # WHY THIS POLLS INSTEAD OF BEING PUSHED
//
// There is no socket between the backend and this app and adding one to carry a
// boolean would be a large amount of machinery for a value that changes a handful
// of times a season. A poll is dull, survives a dropped connection without any
// reconnect logic, and cannot leave the recorder stuck on if the server goes away
// - see the staleness rule below, which is the only genuinely subtle part.
package main

import (
	"encoding/json"
	"time"
)

const (
	// Idle players are the overwhelming majority and their answer is always the
	// same, so ask rarely. A tournament match does not start in the sixty seconds
	// between two polls and then finish before the next one.
	tourPollIdle = 60 * time.Second

	// Once a match IS live, the answer matters more and changes sooner.
	tourPollLive = 30 * time.Second

	// How long a "yes" is allowed to survive with no fresh confirmation.
	//
	// THIS IS THE IMPORTANT NUMBER. Without it, one successful poll saying "live"
	// followed by the backend becoming unreachable leaves the recorder buffering
	// forever, because the error path cannot tell "the match ended" apart from "I
	// could not ask". Failing back to false after a few minutes of silence is the
	// safe direction: the cost of a wrongly-off recorder is a missed clip, and the
	// cost of a wrongly-on one is a machine quietly encoding video all night.
	tourStale = 4 * time.Minute
)

type tourState struct {
	Live    bool
	MatchID string
	Status  string
}

// watchTournament runs for the life of the app and keeps rec.tournamentActive
// honest. It is deliberately quiet: only CHANGES are logged, because a line every
// sixty seconds saying nothing happened is how a log stops being read.
func watchTournament() {
	var lastLive bool
	var lastMatch string
	var confirmedAt time.Time

	for {
		wait := tourPollIdle
		if lastLive {
			wait = tourPollLive
		}
		time.Sleep(wait)

		var cfg config
		loadJSON(configPath(), &cfg)

		// Not linked, or the player is not using this mode: do not ask at all.
		// Polling on behalf of somebody who will never look at the answer is a
		// request the server does not need to serve.
		if cfg.DeviceToken == "" || rec.settings().Mode != ModeTournament {
			if lastLive {
				lastLive, lastMatch = false, ""
				rec.setTournamentActive(false, "")
			}
			continue
		}

		st, err := fetchTournamentState(cfg)
		if err != nil {
			// An unreachable backend is NOT evidence the match ended. Hold the
			// last answer until it goes stale, then fail closed.
			if lastLive && time.Since(confirmedAt) > tourStale {
				logf("recorder: no word from SiegeIQ about your tournament match for %s - "+
					"stopping the tournament buffer until it answers again", tourStale)
				lastLive, lastMatch = false, ""
				rec.setTournamentActive(false, "")
			}
			continue
		}

		confirmedAt = time.Now()
		if st.Live == lastLive && st.MatchID == lastMatch {
			continue
		}
		lastLive, lastMatch = st.Live, st.MatchID
		rec.setTournamentActive(st.Live, st.MatchID)
	}
}

func fetchTournamentState(cfg config) (tourState, error) {
	raw, err := coachGet(cfg, "/sync/tournament-state")
	if err != nil {
		return tourState{}, err
	}
	var out struct {
		Live    bool   `json:"live"`
		MatchID string `json:"match_id"`
		Status  string `json:"status"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return tourState{}, err
	}
	return tourState{Live: out.Live, MatchID: out.MatchID, Status: out.Status}, nil
}
