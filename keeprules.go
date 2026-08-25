// keeprules.go - deciding what survives out of the buffer.
//
// This is the file that makes SiegeIQ's recorder different from every other
// recorder a Siege player already has.
//
// Medal, ShadowPlay and OBS all keep a rolling buffer. None of them know what
// happened in the match, so the best they can offer is "the last N seconds" and
// a hotkey the player has to remember to press. We hold the same buffer AND the
// match's replay files, so we can answer questions they structurally cannot:
// keep the action phase and throw away the drone phase, keep only the rounds
// where I died, keep the round I clutched.
//
// # HONESTY RULE FOR THIS FILE
//
// Three of the rules need to know what actually happened inside a round - who
// died, who got a kill, who was last alive. That comes from decoded replay data,
// which the desktop app does not have on its own. When it is missing, the rule
// DEGRADES to keeping the whole action phase and says so in the log and in the
// note attached to the clip. It never guesses which rounds those were, because a
// clip labelled "the round you clutched" that is not the round they clutched is
// worse than no clip at all.
package main

import (
	"fmt"
	"time"
)

// keepSpan is one piece of footage worth writing out.
type keepSpan struct {
	Label      string // becomes part of the filename: r03-action
	RoundIndex int
	From, To   time.Time
	Estimated  bool   // the boundaries were inferred, not decoded
	Reason     string // why this span was kept, for the log and the clip sidecar
}

func (s keepSpan) duration() time.Duration { return s.To.Sub(s.From) }

// evaluateKeepRule turns a located match plus the player's chosen rule into a
// list of spans to cut. notes carries anything the player should know: a rule
// that degraded, a round that was skipped, an estimate that was used.
func evaluateKeepRule(plan matchPlan, rc recorderConfig) (spans []keepSpan, notes []string) {
	if len(plan.Rounds) == 0 {
		return nil, []string{"no rounds found in this match"}
	}

	pre := time.Duration(rc.PrePadSec) * time.Second
	post := time.Duration(rc.PostPadSec) * time.Second

	// Two ways the detail can be missing, and they are not the same thing. No events
	// at all means the match was never uploaded - syncing is paused, the upload failed,
	// or Verified Stats is offline. Events without UploaderDetail means the match WAS
	// read but this account could not be picked out of its own roster, so every
	// uploader_* field arrived absent and unmarshalled to a confident zero. Believing
	// that second case keeps nothing and reports "no rounds matched", which reads like
	// a quiet evening rather than a failure. Both degrade to the action phase.
	rule := rc.KeepRule
	if rc.needsEvents() && (plan.Events == nil || !plan.Events.UploaderDetail) {
		why := "this match has not been read by SiegeIQ yet (is syncing paused?)"
		if plan.Events != nil {
			why = "SiegeIQ read this match but could not tell which player is you"
		}
		notes = append(notes, fmt.Sprintf(
			"%q needs decoded round detail - %s, so the action phase of every round was kept instead",
			rule, why))
		rule = KeepActionOnly
	}

	add := func(label string, idx int, from, to time.Time, exact bool, reason string) {
		from = from.Add(-pre)
		to = to.Add(post)
		if !to.After(from) {
			return
		}
		spans = append(spans, keepSpan{
			Label:      label,
			RoundIndex: idx,
			From:       from,
			To:         to,
			Estimated:  !exact,
			Reason:     reason,
		})
	}

	switch rule {
	case KeepNothing:
		return nil, append(notes, "keep rule is 'nothing' - the buffer runs but nothing is written out")

	case KeepWholeMatch:
		from, to, ok := plan.span()
		if !ok {
			return nil, append(notes, "could not locate the match in time")
		}
		exact := true
		for _, r := range plan.Rounds {
			if !r.Exact {
				exact = false
				break
			}
		}
		add("match", 0, from, to, exact, "whole match")
		return spans, notes

	case KeepActionOnly:
		for _, r := range plan.Rounds {
			add(fmt.Sprintf("r%02d-action", r.Index), r.Index,
				r.ActionStart, r.End, r.Exact, "action phase")
		}
		return spans, notes

	case KeepLastSeconds:
		n := time.Duration(rc.LastSeconds) * time.Second
		for _, r := range plan.Rounds {
			from := r.End.Add(-n)
			if from.Before(r.Start) {
				from = r.Start
			}
			add(fmt.Sprintf("r%02d-last%ds", r.Index, rc.LastSeconds), r.Index,
				from, r.End, r.Exact, fmt.Sprintf("last %d seconds", rc.LastSeconds))
		}
		return spans, notes

	case KeepKillMoments:
		// CUT TO THE KILL, not to the round it happened in.
		//
		// This is the rule the other three could not be. They keep whole rounds
		// because a count is all the client is given: "you got two kills" says
		// nothing about when. The backend now sends a time for each kill, in
		// seconds before that round ended, which is the same anchor every other
		// span here uses.
		//
		// ABSENT MEANS DEGRADE, and the distinction matters. The backend emits
		// nothing at all for a round it could not place a kill on the clock
		// without guessing - a plant makes a countdown value ambiguous, and a
		// clip labelled "your kill" that opens somewhere else is worse than no
		// clip. So a round with kills but no times keeps its action phase and
		// says so, rather than silently keeping nothing.
		kept := 0
		lead := time.Duration(rc.KillLeadSec) * time.Second
		trail := time.Duration(rc.KillTrailSec) * time.Second
		degraded := 0
		for _, r := range plan.Rounds {
			e, ok := plan.eventForRound(r)
			if !ok {
				continue
			}
			if len(e.KillTimes) == 0 {
				if e.UploaderKills > 0 {
					degraded++
					add(fmt.Sprintf("r%02d-kills", r.Index), r.Index,
						r.ActionStart, r.End, r.Exact,
						fmt.Sprintf("%d kill(s), exact times not decoded for this round", e.UploaderKills))
				}
				continue
			}
			wins := momentWindows(r.End, e.KillTimes, lead, trail)
			for i, w := range wins {
				label := fmt.Sprintf("r%02d-kill%d", r.Index, i+1)
				if len(wins) == 1 {
					label = fmt.Sprintf("r%02d-kill", r.Index)
				}
				add(label, r.Index, w[0], w[1], r.Exact, "the moment of a kill")
			}
			kept++
		}
		if degraded > 0 {
			notes = append(notes, fmt.Sprintf(
				"%d round(s) had kills but no decoded kill times, so the whole action phase was kept for those", degraded))
		}
		if kept == 0 && degraded == 0 {
			notes = append(notes, "no kills in this match, so nothing was kept")
		}
		return spans, notes

	case KeepDeathMoments:
		// CUT TO THE DEATH. The mirror of the rule above, and the one that makes
		// coaching specific: "you died this round" is a boolean, but the five
		// seconds before a death contain the whole mistake - where you were
		// standing, what you were looking at, who was already dead.
		//
		// Same degrade contract as kills. A round the backend could not place a
		// death on the clock keeps its action phase and says so, rather than
		// quietly keeping nothing.
		kept := 0
		lead := time.Duration(rc.KillLeadSec) * time.Second
		trail := time.Duration(rc.KillTrailSec) * time.Second
		degraded := 0
		for _, r := range plan.Rounds {
			e, ok := plan.eventForRound(r)
			if !ok {
				continue
			}
			if len(e.DeathTimes) == 0 {
				if e.UploaderDied {
					degraded++
					add(fmt.Sprintf("r%02d-death", r.Index), r.Index,
						r.ActionStart, r.End, r.Exact,
						"you died this round, exact time not decoded")
				}
				continue
			}
			wins := momentWindows(r.End, e.DeathTimes, lead, trail)
			for i, w := range wins {
				label := fmt.Sprintf("r%02d-death%d", r.Index, i+1)
				if len(wins) == 1 {
					label = fmt.Sprintf("r%02d-death", r.Index)
				}
				add(label, r.Index, w[0], w[1], r.Exact, "the moment of a death")
			}
			kept++
		}
		if degraded > 0 {
			notes = append(notes, fmt.Sprintf(
				"%d round(s) had a death but no decoded time, so the whole action phase was kept for those", degraded))
		}
		if kept == 0 && degraded == 0 {
			notes = append(notes, "no deaths in this match, so nothing was kept")
		}
		return spans, notes

	case KeepPostPlant:
		// FROM THE PLANT TO THE END OF THE ROUND.
		//
		// Rounds with no plant are SKIPPED, not degraded - a round that never
		// reached a plant genuinely has no post-plant, and keeping its action
		// phase instead would quietly turn this rule back into "action only".
		//
		// A short lead is included so the clip opens on the plant going down
		// rather than on the animation already finished.
		kept, noPlant := 0, 0
		lead := time.Duration(rc.KillLeadSec) * time.Second
		for _, r := range plan.Rounds {
			e, ok := plan.eventForRound(r)
			if !ok {
				continue
			}
			if e.PlantBeforeEnd <= 0 {
				noPlant++
				continue
			}
			at := r.End.Add(-time.Duration(e.PlantBeforeEnd * float64(time.Second)))
			from := at.Add(-lead)
			if from.Before(r.ActionStart) {
				from = r.ActionStart
			}
			reason := "the post-plant"
			if e.UploaderPlant {
				reason = "the post-plant, you planted"
			}
			add(fmt.Sprintf("r%02d-postplant", r.Index), r.Index, from, r.End, r.Exact, reason)
			kept++
		}
		if kept == 0 {
			notes = append(notes, fmt.Sprintf(
				"no plant was decoded in any of the %d round(s), so nothing was kept", noPlant))
		}
		return spans, notes

	case KeepOpeningDuel:
		// THE FIRST KILL OF THE ROUND, and only when you were in it.
		//
		// The opening duel decides more rounds than any other single moment, and
		// losing it repeatedly is invisible on a scoreboard that only reports a
		// total. Rounds where somebody else took the first kill are skipped, not
		// degraded, for the same reason as the post-plant above.
		kept, notYours := 0, 0
		lead := time.Duration(rc.KillLeadSec) * time.Second
		trail := time.Duration(rc.KillTrailSec) * time.Second
		for _, r := range plan.Rounds {
			e, ok := plan.eventForRound(r)
			if !ok {
				continue
			}
			if e.OpeningBeforeEnd <= 0 || e.OpeningRole == "" {
				notYours++
				continue
			}
			at := r.End.Add(-time.Duration(e.OpeningBeforeEnd * float64(time.Second)))
			reason := "you took the opening kill"
			if e.OpeningRole == "victim" {
				reason = "you lost the opening duel"
			}
			add(fmt.Sprintf("r%02d-opening", r.Index), r.Index,
				at.Add(-lead), at.Add(trail), r.Exact, reason)
			kept++
		}
		if kept == 0 {
			notes = append(notes, fmt.Sprintf(
				"you were not in the opening duel of any of the %d round(s), so nothing was kept", notYours))
		}
		return spans, notes

	case KeepMyDeaths, KeepMyKills, KeepClutches:
		kept := 0
		for _, r := range plan.Rounds {
			e, ok := plan.eventForRound(r)
			if !ok {
				continue
			}
			var want bool
			var reason string
			switch rule {
			case KeepMyDeaths:
				want, reason = e.UploaderDied, "you died this round"
			case KeepMyKills:
				want, reason = e.UploaderKills > 0, fmt.Sprintf("%d kill(s)", e.UploaderKills)
			case KeepClutches:
				want, reason = e.UploaderClutch, "clutch round"
			}
			if !want {
				continue
			}
			kept++
			add(fmt.Sprintf("r%02d-%s", r.Index, shortRule(rule)), r.Index,
				r.ActionStart, r.End, r.Exact, reason)
		}
		if kept == 0 {
			notes = append(notes, fmt.Sprintf("no rounds in this match matched %q - nothing kept", rule))
		}
		return spans, notes
	}

	return nil, append(notes, fmt.Sprintf("unknown keep rule %q - nothing kept", rule))
}

func shortRule(rule string) string {
	switch rule {
	case KeepMyDeaths, KeepDeathMoments:
		return "death"
	case KeepMyKills:
		return "kills"
	case KeepClutches:
		return "clutch"
	case KeepPostPlant:
		return "postplant"
	case KeepOpeningDuel:
		return "opening"
	}
	return "clip"
}

// momentWindows turns "seconds before the round ended" into clip windows on the
// wall clock, merging any two that overlap.
//
// ONE DERIVATION, shared by every moment rule. Two kills three seconds apart
// must produce one clip rather than two overlapping ones, and a second copy of
// that merge written for deaths is a second place for it to be wrong. The input
// is assumed sorted earliest-first, which is what the backend sends.
func momentWindows(end time.Time, times []float64, lead, trail time.Duration) [][2]time.Time {
	var wins [][2]time.Time
	for _, t := range times {
		at := end.Add(-time.Duration(t * float64(time.Second)))
		from, to := at.Add(-lead), at.Add(trail)
		if n := len(wins); n > 0 && !from.After(wins[n-1][1]) {
			if to.After(wins[n-1][1]) {
				wins[n-1][1] = to
			}
			continue
		}
		wins = append(wins, [2]time.Time{from, to})
	}
	return wins
}
