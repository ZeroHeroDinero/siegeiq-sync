// clipupload.go - sending a finished clip to SiegeIQ.
//
// Three destinations, and the player chooses which apply:
//
//	local       the clip is written and that is all. Nothing leaves the machine.
//	ai_analyze  the clip goes to the AI coaching pipeline, replacing the
//	            record-it-yourself-then-find-it-then-upload-it dance that is the
//	            main reason people never get round to reviewing their play.
//	tournament  the clip is attached to a SiegeIQ tournament match, alongside
//	            the replay, so the footage and the decoded match data can be
//	            cross-checked against each other.
//
// The same device token the replay uploader uses authenticates these, so linking
// a device once covers both. Nothing here can upload anything the player did not
// ask for: the destination is read from config on every single clip.
//
// BACKEND STATUS: the three endpoints exist as of 2026-08-09, and the switch
// that decides who may use them is off by default on the server. A 403 is
// therefore an ordinary answer, distinct from a 404, and both are handled as
// "not for you yet, the clip is safe on disk" rather than as a repeating error.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

const (
	clipKindAI         = "ai_analyze"
	clipKindTournament = "tournament"
)

// clipEndpointMissing is set once the backend has told us the endpoint is not
// there, so the log gets one clear line rather than one per clip.
var clipEndpointMissing int32

// uploadClip sends one clip, in three steps, and the video never touches the
// SiegeIQ API.
//
// # WHY IT IS NOT ONE POST ANY MORE
//
// It used to be a single multipart POST of the whole file. That is the right
// shape for a replay, which is kilobytes, and completely the wrong shape for
// footage: a whole-match clip off this recorder measured 1.8 GB. The API sits
// behind a host that will refuse a body that size, and even where it would not,
// one upload would hold a server worker busy for minutes copying bytes it has
// no use for on their way to storage.
//
// So Sync asks for permission, gets a one-time signed link, sends the video
// STRAIGHT to storage, and then reports back. Three small calls and one big
// transfer that goes nowhere near the API.
//
// The contract with the caller is unchanged: (done, err), where done means do
// not retry this file.
// sendOutcome is what the caller needs AFTER a successful upload in order to keep
// telling the player what is happening. Added 2026-08-29, when uploading stopped
// being the end of the story: the server now starts a review off the back of
// /sync/clip/done, and without the clip's id there is nothing to ask about it.
type sendOutcome struct {
	ClipID    string
	Reviewing bool // the server said it has started a review for this clip
}

func uploadClip(cfg *config, rc recorderConfig, clipPath, kind, matchFolder string) (bool, error) {
	// Compression happens here for callers that are already off the window
	// thread (the automatic post-match path). The manual Send button goes
	// through startSend, which does it as part of a background job - see
	// clipsend.go for why that distinction cost an application freeze.
	sendPath, cleanup := compressForUpload(rc, clipPath)
	defer cleanup()
	_, done, err := uploadClipFile(cfg, rc, clipPath, sendPath, kind, matchFolder)
	return done, err
}

// uploadClipFile does the three-step transfer for a file that is ALREADY the
// right size. clipPath is the player's original and is used for naming and for
// finding the sidecar; sendPath is what actually goes up the wire.
func uploadClipFile(cfg *config, rc recorderConfig, clipPath, sendPath, kind, matchFolder string) (sendOutcome, bool, error) {
	var res sendOutcome
	if cfg.DeviceToken == "" {
		return res, false, fmt.Errorf("this device is not linked - clip kept locally")
	}

	f, err := os.Open(sendPath)
	if err != nil {
		return res, true, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return res, true, err
	}

	// ---- step 1: say what is coming, get a link -----------------------------
	start := map[string]any{
		"name":         filepath.Base(clipPath),
		"bytes":        info.Size(),
		"kind":         kind,
		"match_folder": matchFolder,
		"keep_rule":    rc.KeepRule,
		"app_version":  version,
		"estimated":    true,
	}
	// The sidecar carries the honesty flag and the round detail. Merging it in
	// rather than sending it as an opaque blob means the backend can index the
	// fields it cares about without parsing a nested document.
	sidecar := strings.TrimSuffix(clipPath, filepath.Ext(clipPath)) + ".json"
	if b, err := os.ReadFile(sidecar); err == nil {
		var meta map[string]any
		if json.Unmarshal(b, &meta) == nil {
			// match_id joined this list on 2026-08-29 and it is the reason the whole
			// sidecar merge exists: it is what lets the server attach the parsed
			// replay to this clip's review. Note it survives a MANUAL send too -
			// apiSendClip passes matchFolder "manual", but the sidecar still knows
			// which match the footage was cut from.
			for _, k := range []string{"duration_sec", "round_index", "label", "reason", "estimated", "match_id"} {
				if v, ok := meta[k]; ok {
					start[k] = v
				}
			}
		}
	}

	var out struct {
		ClipID    string `json:"clipId"`
		UploadURL string `json:"uploadUrl"`
	}
	done, err := clipAPI(cfg, "/sync/clip/start", start, &out, 60*time.Second)
	if err != nil {
		return res, done, err
	}
	if out.UploadURL == "" || out.ClipID == "" {
		return res, true, fmt.Errorf("the server did not return an upload link")
	}
	res.ClipID = out.ClipID

	// ---- step 2: the video itself, straight to storage ----------------------
	//
	// Scaled to the file rather than fixed: roughly a minute per 50 MB with a
	// generous floor, because this is the one call that moves gigabytes and a
	// timeout that fires mid-transfer wastes the whole upload.
	timeout := time.Duration(info.Size()/(50*1024*1024)+1) * time.Minute
	if timeout < 10*time.Minute {
		timeout = 10 * time.Minute
	}
	putErr := putObject(out.UploadURL, f, info.Size(), timeout)

	// ---- step 3: tell the server how it went --------------------------------
	//
	// A failure is reported too. Without this a died-halfway upload leaves a row
	// indistinguishable from one still in flight, and nobody can tell whether a
	// missing clip is late or lost.
	fin := map[string]any{"clipId": out.ClipID, "ok": putErr == nil}
	if putErr != nil {
		fin["error"] = putErr.Error()
	}
	// The reply now says whether a review was started. Believing the server rather
	// than assuming is the point: clip review can be switched off on the server
	// without a new build of this app, and a send card that promises a review that
	// is never coming is worse than one that stops at "sent".
	var fout struct {
		Reviewing bool `json:"reviewing"`
	}
	_, _ = clipAPI(cfg, "/sync/clip/done", fin, &fout, 30*time.Second)
	res.Reviewing = fout.Reviewing

	if putErr != nil {
		return res, false, putErr // storage trouble is worth another go later
	}
	logf("recorder: sent %s (%d MB) to SiegeIQ", filepath.Base(clipPath), info.Size()/(1024*1024))
	return res, true, nil
}

// clipReviewState asks what became of one clip. Device-token auth, same as every
// other call in this file.
//
// A failure here is deliberately quiet and non-terminal. The review is running on
// the server whatever this poll says, so a dropped wifi connection should leave the
// card sitting on "analysing" rather than declaring a failure that did not happen.
func clipReviewState(cfg *config, clipID string) (string, string, int, error) {
	req, err := http.NewRequest("GET", backend+"/sync/clip/"+clipID+"/review", nil)
	if err != nil {
		return "", "", 0, err
	}
	req.Header.Set("Authorization", "Device "+cfg.DeviceToken)
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return "", "", 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return "", "", 0, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var out struct {
		ReviewState string `json:"reviewState"`
		ReviewID    string `json:"reviewId"`
		Score       *int   `json:"score"`
		Error       string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", "", 0, err
	}
	score := 0
	if out.Score != nil {
		score = *out.Score
	}
	if out.ReviewState == "failed" && out.Error != "" {
		return out.ReviewState, out.ReviewID, score, fmt.Errorf("%s", out.Error)
	}
	return out.ReviewState, out.ReviewID, score, nil
}

// putObject streams the file to the signed link.
//
// The body is the open file rather than a buffer, so a 1.8 GB clip is never
// held in memory. ContentLength is set explicitly because a signed PUT is
// rejected outright if the length is unknown and the request goes out chunked.
func putObject(url string, body io.Reader, size int64, timeout time.Duration) error {
	req, err := http.NewRequest("PUT", url, body)
	if err != nil {
		return err
	}
	req.ContentLength = size
	req.Header.Set("Content-Type", "video/mp4")

	resp, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return fmt.Errorf("storage refused the upload (HTTP %d)", resp.StatusCode)
}

// clipAPI is the small JSON call used by steps one and three, with the status
// handling both share.
func clipAPI(cfg *config, path string, in any, out any, timeout time.Duration) (bool, error) {
	b, err := json.Marshal(in)
	if err != nil {
		return true, err
	}
	req, err := http.NewRequest("POST", backend+path, bytes.NewReader(b))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Device "+cfg.DeviceToken)

	resp, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		if out != nil {
			if err := json.Unmarshal(raw, out); err != nil {
				return true, fmt.Errorf("could not read the server's reply: %v", err)
			}
		}
		return true, nil

	case resp.StatusCode == 404:
		// An older server that has never heard of clip upload. Said once, then
		// the orchestrator stops trying for the rest of the session.
		if atomic.CompareAndSwapInt32(&clipEndpointMissing, 0, 1) {
			logf("recorder: this SiegeIQ server does not have clip upload yet - clips are being kept on disk only")
		}
		return true, fmt.Errorf("clip upload not available yet")

	case resp.StatusCode == 403:
		// Deliberately NOT the same as 404. The endpoint is there and this
		// account is not allowed to use it yet, which is a different sentence to
		// say to the player and a different thing to do about it.
		if atomic.CompareAndSwapInt32(&clipEndpointMissing, 0, 1) {
			logf("recorder: clip upload is not switched on for this account - clips are being kept on disk only")
		}
		return true, fmt.Errorf("clip upload is not enabled for this account yet")

	case resp.StatusCode == 401:
		return false, fmt.Errorf("this device is no longer linked - re-pair from siegeiq.gg")

	case resp.StatusCode == 413:
		return true, fmt.Errorf("that clip is larger than the server accepts")

	case resp.StatusCode >= 400 && resp.StatusCode < 500:
		return true, fmt.Errorf("rejected (HTTP %d): %s", resp.StatusCode, strings.TrimSpace(string(raw)))

	default:
		return false, fmt.Errorf("server error (HTTP %d) - will retry", resp.StatusCode)
	}
}

// clipEndpointKnownMissing lets the orchestrator skip pointless upload attempts
// for the rest of the session once the backend has said the endpoint is absent.
func clipEndpointKnownMissing() bool { return atomic.LoadInt32(&clipEndpointMissing) == 1 }
