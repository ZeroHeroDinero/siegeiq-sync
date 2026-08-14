// coachpass.go - proving to SiegeIQ who is sitting at this PC.
//
// # THE PROBLEM THIS SOLVES
//
// Sync holds a DEVICE token from pairing. That proves the machine is approved,
// which is all uploading ever needed. It does not prove a person is signed in,
// and every coaching endpoint on the backend wants a signed-in identity.
//
// Rather than build a second copy of those endpoints for the desktop app, the
// backend now lets Sync trade its device token for a short-lived READ PASS at
// /sync/session. Presented as a normal bearer token, that pass makes every
// existing endpoint work from here untouched - and, importantly, the Pro and
// Squad rules keep being enforced on the server where they already live. There
// is no way for this app to hand somebody a paid feature by mistake, because it
// never decides.
//
// # WHY THAT MATTERS BEYOND CONVENIENCE
//
// It is what keeps the website and this app in step. Change a coaching prompt, a
// voice script or a plan rule on the server, deploy, and Sync shows the new
// behaviour immediately with no new build. That property survives only while
// this app stays a RENDERER. The moment coaching text, thresholds or plan logic
// get copied into Go, the two drift and every future change becomes two changes.
// Do not copy them here.
//
// # THE PASS IS MEMORY ONLY, DELIBERATELY
//
// It is never written to disk. Quitting Sync throws it away while the pairing
// survives, which is what somebody would expect closing an app to mean. It also
// means a stolen config file is worth no more than it was yesterday.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// passEarly is how long before real expiry we treat the pass as stale.
//
// The server issues six hours. Renewing a minute before the end would mean a
// request that leaves valid and arrives expired, which surfaces as a mysterious
// one-off failure rather than anything diagnosable. Five minutes of slack costs
// nothing and removes the whole class of problem.
const passEarly = 5 * time.Minute

type syncPass struct {
	mu    sync.Mutex
	token string
	until time.Time
}

var thePass = &syncPass{}

// get returns a usable pass, fetching a new one if there is none or it is close
// to expiring.
//
// NEVER call this from a bridge binding. It performs network I/O, and bindings
// run on the window's message loop - the thread that also paints the interface.
// Blocking it is how this app once showed "Not Responding" for five minutes.
// Callers live in coachfetch.go, on their own goroutine.
func (p *syncPass) get(cfg config) (string, error) {
	p.mu.Lock()
	if p.token != "" && time.Now().Before(p.until.Add(-passEarly)) {
		tok := p.token
		p.mu.Unlock()
		return tok, nil
	}
	p.mu.Unlock()

	if cfg.DeviceToken == "" {
		return "", fmt.Errorf("this PC is not linked to a SiegeIQ account yet")
	}

	req, err := http.NewRequest("POST", backend+"/sync/session", bytes.NewReader([]byte("{}")))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Device "+cfg.DeviceToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return "", fmt.Errorf("could not reach SiegeIQ (%v)", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))

	switch {
	case resp.StatusCode == 403:
		// The pairing was revoked, or this device was never approved. Saying so
		// plainly beats a generic failure, because the fix is a specific thing
		// the player can do rather than something to report.
		return "", fmt.Errorf("this PC is no longer linked to your account - re-link it from siegeiq.gg")
	case resp.StatusCode == 404:
		// An older backend with no /sync/session. Named separately so nobody
		// spends an evening debugging a client that is working perfectly.
		return "", fmt.Errorf("your SiegeIQ server does not offer coaching results yet")
	case resp.StatusCode >= 400:
		return "", fmt.Errorf("SiegeIQ refused the request (%d)", resp.StatusCode)
	}

	var out struct {
		Pass      string `json:"pass"`
		ExpiresIn int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &out); err != nil || out.Pass == "" {
		return "", fmt.Errorf("SiegeIQ sent a reply this version cannot read")
	}
	ttl := time.Duration(out.ExpiresIn) * time.Second
	if ttl <= 0 {
		ttl = time.Hour
	}

	p.mu.Lock()
	p.token = out.Pass
	p.until = time.Now().Add(ttl)
	p.mu.Unlock()

	logf("coaching: linked to your SiegeIQ account for the next %s", ttl.Round(time.Minute))
	return out.Pass, nil
}

// forget drops the cached pass so the next call fetches a fresh one.
//
// Called when the server answers 401. A pass can stop being valid before its own
// clock says so - the secret rotates, the device is revoked, the server is
// redeployed - and retrying the same dead token forever is the failure mode this
// exists to prevent.
func (p *syncPass) forget() {
	p.mu.Lock()
	p.token = ""
	p.until = time.Time{}
	p.mu.Unlock()
}

// deviceGet performs one GET using the DEVICE token rather than the read pass.
//
// Not every endpoint takes the same credential, and assuming they did cost a
// feature: /sync/clips authenticates the DEVICE, because it was built for the
// uploader long before the pass existed. Sending the pass there returns 401, so
// the Results tab loaded a player's matches and quietly failed to attach any of
// their footage, logging one line nobody would think to look for.
//
// The right fix is to send each endpoint the credential it actually wants,
// rather than widening a device endpoint to accept a pass it has no need for.
func deviceGet(cfg config, path string) ([]byte, error) {
	if cfg.DeviceToken == "" {
		return nil, fmt.Errorf("this PC is not linked to a SiegeIQ account yet")
	}
	req, err := http.NewRequest("GET", backend+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Device "+cfg.DeviceToken)
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not reach SiegeIQ (%v)", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("SiegeIQ returned %d for %s", resp.StatusCode, path)
	}
	return body, nil
}

// coachGet performs one authenticated GET and returns the raw body.
//
// It retries exactly once, and only after a 401, having thrown the pass away
// first. Once, because a second 401 means the problem is not the pass and
// hammering the server with a request it has twice refused helps nobody.
func coachGet(cfg config, path string) ([]byte, error) {
	for attempt := 0; attempt < 2; attempt++ {
		tok, err := thePass.get(cfg)
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequest("GET", backend+path, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+tok)

		resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
		if err != nil {
			return nil, fmt.Errorf("could not reach SiegeIQ (%v)", err)
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
		resp.Body.Close()

		if resp.StatusCode == 401 && attempt == 0 {
			thePass.forget()
			continue
		}
		if resp.StatusCode == 403 {
			// The scope list on the server refused this path. That is a bug in
			// this app rather than anything the player did, so it is logged in
			// those terms instead of being blamed on their account.
			return nil, fmt.Errorf("this app asked for something it is not allowed to read (%s)", path)
		}
		if resp.StatusCode >= 400 {
			return nil, fmt.Errorf("SiegeIQ returned %d for %s", resp.StatusCode, path)
		}
		return body, nil
	}
	return nil, fmt.Errorf("your SiegeIQ session could not be renewed")
}
