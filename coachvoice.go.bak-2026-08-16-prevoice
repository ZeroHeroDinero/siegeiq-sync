// coachvoice.go - fetching the spoken coaching, and letting the player pick who
// says it.
//
// # WHY THE AUDIO COMES THROUGH GO RATHER THAN THE PAGE FETCHING IT
//
// The page has no credentials and should never have any. The read pass lives in
// Go (see coachpass.go) and stays there, so the window cannot leak it, and a
// script injected into the page - which should never happen, but this is the
// assumption worth holding - finds nothing worth stealing.
//
// So Go asks the backend, and hands the page the audio itself.
//
// # WHY BASE64 AND NOT A FILE ON DISK
//
// A temporary mp3 would mean writing a player's coaching to their disk and then
// being responsible for deleting it, including after a crash. The audio is a
// couple of hundred kilobytes and is wanted exactly once, so it travels as text
// and lives in memory. Nothing to clean up, nothing left behind.
//
// # THE COST RULE IS THE SERVER'S, NOT OURS
//
// The backend caches each match's audio in R2 and only ever bills once per match
// per coach. This file therefore does not need its own rate limit and must not
// invent one: a second press of play is free, and a client-side limit would only
// stop somebody hearing something already paid for.
package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

type coachAudio struct {
	State   string `json:"state"` // idle | loading | ready | error
	Error   string `json:"error"`
	MatchID string `json:"match_id"`
	Coach   string `json:"coach"`

	// Audio is a base64 mp3, empty until ready. The page turns it into a data URL
	// and plays it through a Web Audio analyser so the mouth moves in time with
	// the actual sound rather than on a timer that drifts.
	Audio string `json:"audio"`
}

var (
	cvMu      sync.Mutex
	cvState   = coachAudio{State: "idle"}
	cvRunning bool
)

func coachAudioSnapshot() coachAudio {
	cvMu.Lock()
	defer cvMu.Unlock()
	return cvState
}

// coachPost sends one authenticated POST and returns the raw body.
//
// Same single retry after a 401 as coachGet, and for the same reason: a pass can
// die before its own clock says so, and repeating a request the server has twice
// refused helps nobody.
func coachPost(cfg config, path string, payload any) ([]byte, int, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, err
	}
	for attempt := 0; attempt < 2; attempt++ {
		tok, err := thePass.get(cfg)
		if err != nil {
			return nil, 0, err
		}
		req, err := http.NewRequest("POST", backend+path, bytes.NewReader(body))
		if err != nil {
			return nil, 0, err
		}
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Content-Type", "application/json")

		resp, err := (&http.Client{Timeout: 120 * time.Second}).Do(req)
		if err != nil {
			return nil, 0, fmt.Errorf("could not reach SiegeIQ (%v)", err)
		}
		out, _ := io.ReadAll(io.LimitReader(resp.Body, 16*1024*1024))
		code := resp.StatusCode
		resp.Body.Close()

		if code == 401 && attempt == 0 {
			thePass.forget()
			continue
		}
		return out, code, nil
	}
	return nil, 0, fmt.Errorf("your SiegeIQ session could not be renewed")
}

// startCoachAudio asks the server to speak a match. Returns immediately.
//
// The first generation takes a while - a language model writes the words and a
// voice service says them - which is exactly why this cannot happen on the
// window's thread. Every later request for the same match and coach is served
// from the server's cache and comes back quickly.
func startCoachAudio(matchID, coach string) {
	cvMu.Lock()
	if cvRunning {
		cvMu.Unlock()
		return
	}
	cvRunning = true
	cvState = coachAudio{State: "loading", MatchID: matchID, Coach: coach}
	cvMu.Unlock()

	go func() {
		defer func() {
			cvMu.Lock()
			cvRunning = false
			cvMu.Unlock()
		}()

		var cfg config
		loadJSON(configPath(), &cfg)

		set := func(fn func(*coachAudio)) {
			cvMu.Lock()
			fn(&cvState)
			cvMu.Unlock()
		}

		payload := map[string]string{"match_id": matchID}
		if coach != "" {
			payload["coach"] = coach
		}
		body, code, err := coachPost(cfg, "/coach/match-readout", payload)
		if err != nil {
			logf("coaching: voice failed - %v", err)
			set(func(c *coachAudio) { c.State, c.Error = "error", err.Error() })
			return
		}

		// Each refusal is turned into the sentence a player can act on, because
		// "402" tells somebody nothing about what to do next.
		switch code {
		case 200:
		case 402:
			set(func(c *coachAudio) {
				c.State = "error"
				c.Error = "Spoken coaching is part of Pro and Squad."
			})
			return
		case 409:
			set(func(c *coachAudio) {
				c.State = "error"
				c.Error = "This replay was recorded by someone else, so it is not read out as your match."
			})
			return
		case 404:
			set(func(c *coachAudio) {
				c.State = "error"
				c.Error = "SiegeIQ does not have this match yet."
			})
			return
		default:
			msg := fmt.Sprintf("SiegeIQ could not produce the audio (%d)", code)
			// The server sends a reason in JSON for most refusals. Preferring it
			// over the status code means a new server-side rule explains itself
			// in the app without the app being rebuilt.
			var detail struct {
				Detail string `json:"detail"`
			}
			if json.Unmarshal(body, &detail) == nil && detail.Detail != "" {
				msg = detail.Detail
			}
			set(func(c *coachAudio) { c.State, c.Error = "error", msg })
			return
		}

		if len(body) < 512 {
			set(func(c *coachAudio) {
				c.State, c.Error = "error", "SiegeIQ returned no audio for this match."
			})
			return
		}
		enc := base64.StdEncoding.EncodeToString(body)
		logf("coaching: %s read this match back, %d KB of audio", coach, len(body)/1024)
		set(func(c *coachAudio) {
			c.State, c.Error, c.Audio = "ready", "", enc
		})
	}()
}

// setCoachKey saves the chosen coach on the ACCOUNT, not in this app.
//
// The whole point: a player picks Vera here and is read to by Vera on the
// website as well, because there is one setting rather than two that disagree.
// It is also the only write this app is allowed to make with its read pass, and
// the server enforces that rather than trusting us.
func setCoachKey(key string) string {
	var cfg config
	loadJSON(configPath(), &cfg)
	body, code, err := coachPost(cfg, "/me/coach", map[string]string{"coach_key": key})
	if err != nil {
		return err.Error()
	}
	if code >= 400 {
		var detail struct {
			Detail string `json:"detail"`
		}
		if json.Unmarshal(body, &detail) == nil && detail.Detail != "" {
			return detail.Detail
		}
		return fmt.Sprintf("could not save your coach (%d)", code)
	}
	logf("coaching: your coach is now %s, on this PC and on siegeiq.gg", key)
	// The Results tab reads the coach from /me, so make the next read tell the
	// truth rather than the value we just replaced.
	startResultsFetch()
	return ""
}
