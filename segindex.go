// segindex.go - stamping every buffer segment with the moment it really began.
//
// # WHY A SECOND IS NOT GOOD ENOUGH
//
// ffmpeg names each segment seg_YYYYMMDD-HHMMSS.mp4, and until this file existed
// that name was the only record of when the footage inside it was captured. A
// name is stamped by flooring the clock, so the real start is somewhere in the
// second that follows it. Every clip cut out of the buffer inherited up to a
// second of error, in an unknown direction, on top of the two second keyframe
// snap the trim already costs. A coaching card that says "you peeked here" was
// pointing at a moment that could be a second away from the peek.
//
// # HOW THIS FIXES IT, AND WHAT IT COSTS
//
// Two cheap sources, combined:
//
//  1. A 20 ms poll of the buffer folder. The instant a new segment file appears
//     is the instant ffmpeg opened it, to within one poll. That is a per-segment
//     stamp good to about 20 ms, and it costs one listing of a folder holding a
//     few dozen entries, fifty times a second.
//
//  2. ffmpeg's own segment list (-segment_list), which records each segment's
//     start and end on the capture timeline to the microsecond. It is exact
//     about the SPACING of segments and knows nothing about wall-clock time.
//
// Neither alone is enough. The poll knows what time it is but jitters; the list
// does not jitter but has no idea what time it is. Anchoring the list to the
// clock using every poll stamp at once gives both: one offset, estimated from
// all of them, applied to spacing that is already exact.
//
// Measured on real segmented captures with two second segments: raw poll stamps
// scattered across a range of one to twelve milliseconds, and the anchored times
// then spaced exactly as ffmpeg's own list says, to the microsecond. Against a
// full second of filename error, that is the whole point of this file.
//
// # THE ONE TRAP
//
// The FIRST segment of a session does not fit the pattern. Every later segment's
// file appears a fixed distance after its true start, because the encoder holds
// a few frames before the muxer writes anything - a constant, and a constant is
// harmless because the anchor absorbs it. The first file additionally waits for
// the encoder and the filter graph to be built, measured at a quarter of a second
// on top. Including it would drag the anchor by that much, so it is EXCLUDED from
// the estimate. It still receives a corrected time like every other segment; it
// simply never votes on what the anchor is.
//
// A consequence worth knowing: a corrected time can land BEFORE the second its
// filename names, because the filename is stamped when the file is opened and
// the footage inside it starts slightly earlier. That is the correction doing
// its job, not a fault.
//
// # WHAT IS STILL NOT EXACT
//
// Everything here is measured at the moment ffmpeg writes to disk, which trails
// the moment light hit the screen by however long the encoder holds a frame.
// That delay is the same for every segment, so footage lines up with itself
// perfectly and lines up with the game clock a fixed distance out. Closing that
// last gap needs a timestamp from the capture API itself, which is the native
// backend's job, not this file's.
//
// # WHAT HAPPENS WHEN THIS GOES WRONG
//
// Nothing breaks. The index is advice, not truth: ringbuf.go asks for a precise
// time and falls straight back to the filename when there is not one, or when
// the one on offer disagrees with the filename by more than a few seconds. A
// missing index, an ffmpeg too old for segment lists, a folder copied from
// another machine and a clock that jumped all land in the same safe place as
// before this file was written.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// segIndexName is ours. segListName is ffmpeg's, named in buildArgs.
	segIndexName = "segments.idx"
	segListName  = "segments.csv"

	segPollEvery = 20 * time.Millisecond

	// A precise time further than this from the filename's second is not a
	// better answer, it is a broken one: a clock change, a copied folder, a
	// half-written index. The filename wins whenever this trips.
	segStampSanity = 5 * time.Second
)

// segListRow is one line of ffmpeg's segment list: where this segment sits on
// the capture timeline, in seconds from the start of the capture.
type segListRow struct {
	start time.Duration
	end   time.Duration
}

// segIndex watches one buffer folder for the life of one capture session.
type segIndex struct {
	dir string

	mu sync.Mutex
	// seen maps a segment filename to the moment the poll first saw it. A zero
	// time means the file was already there when the session started, so it
	// belongs to an earlier capture and must never be stamped with now().
	seen map[string]time.Time
	// first is this session's first newly created segment - the one whose stamp
	// is biased by the encoder and filter graph being built, and the only one
	// excluded from the anchor estimate.
	first string

	stop chan struct{}
	done chan struct{}
}

// startSegIndex begins watching dir. Call it BEFORE ffmpeg starts, so the files
// already in the folder are recognised as somebody else's before ffmpeg has a
// chance to add one of its own.
func startSegIndex(dir string) *segIndex {
	x := &segIndex{
		dir:  dir,
		seen: map[string]time.Time{},
		stop: make(chan struct{}),
		done: make(chan struct{}),
	}
	// Seed: everything already present belongs to a previous session. Whatever
	// times the previous session wrote for those files stay valid, because a
	// rewrite only ever replaces rows for files it can still see.
	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				if _, ok := segmentTime(e.Name()); ok {
					x.seen[e.Name()] = time.Time{}
				}
			}
		}
	}
	go x.run()
	return x
}

func (x *segIndex) run() {
	t := time.NewTicker(segPollEvery)
	defer t.Stop()
	for {
		select {
		case <-x.stop:
			// One last look, because the final segment is created moments before
			// ffmpeg exits, and its row in ffmpeg's list is only written as it
			// closes. Without this the newest footage - the footage most likely
			// to be wanted - would be the one piece left on filename accuracy.
			x.scan()
			x.rewrite()
			close(x.done)
			return
		case <-t.C:
			if x.scan() {
				x.rewrite()
			}
		}
	}
}

// Stop ends the watch and writes the index one final time. Safe to call twice,
// because the capture session's own shutdown path is not the only thing that
// can end a recording.
func (x *segIndex) Stop() {
	if x == nil {
		return
	}
	select {
	case <-x.stop:
		return
	default:
	}
	close(x.stop)
	<-x.done
}

// scan records the arrival of any segment file it has not seen before, and
// reports whether there was one.
func (x *segIndex) scan() bool {
	entries, err := os.ReadDir(x.dir)
	if err != nil {
		return false
	}
	now := time.Now()

	x.mu.Lock()
	defer x.mu.Unlock()
	found := false
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			continue
		}
		if _, ok := segmentTime(name); !ok {
			continue
		}
		if _, ok := x.seen[name]; ok {
			continue
		}
		x.seen[name] = now
		if x.first == "" {
			x.first = name
		}
		found = true
	}
	return found
}

// rewrite recomputes every segment's start from scratch and replaces the index
// file. Recomputing all of it rather than appending is deliberate: the anchor
// gets better as more segments arrive, and rows written early deserve to be
// corrected by what was learned later.
func (x *segIndex) rewrite() {
	x.mu.Lock()
	stamps := make(map[string]time.Time, len(x.seen))
	for k, v := range x.seen {
		stamps[k] = v
	}
	first := x.first
	x.mu.Unlock()

	// Forget anything the pruner has deleted, so neither the index file nor the
	// map behind it can grow without limit across a long session.
	for name := range stamps {
		if _, err := os.Stat(filepath.Join(x.dir, name)); err != nil {
			delete(stamps, name)
			x.mu.Lock()
			delete(x.seen, name)
			x.mu.Unlock()
		}
	}
	if len(stamps) == 0 {
		return
	}

	names := make([]string, 0, len(stamps))
	for n := range stamps {
		names = append(names, n)
	}
	// The names carry YYYYMMDD-HHMMSS, so plain string order is time order.
	sort.Strings(names)

	rows := readSegmentList(filepath.Join(x.dir, segListName))
	anchor := estimateAnchor(names, stamps, rows, first)

	// What a previous session worked out about its own segments. Those files can
	// still be in the buffer when the recorder restarts - a resolution change
	// does exactly that - and this rewrite would otherwise drop times that were
	// correct when they were written, quietly putting older footage back on
	// filename accuracy.
	kept := readSegIndex(x.dir)

	var sb strings.Builder
	sb.WriteString("# siegeiq sync segment index v1: name,unix_micros,source\n")
	written := 0
	for i, n := range names {
		when, source := time.Time{}, ""
		switch {
		case !anchor.IsZero() && hasRow(rows, n):
			when, source = anchor.Add(rows[n].start), "list"
		case !anchor.IsZero() && i > 0 && hasRow(rows, names[i-1]):
			// This segment has not closed yet, so it has no row of its own. It
			// began exactly where the segment before it ended.
			when, source = anchor.Add(rows[names[i-1]].end), "list"
		case !stamps[n].IsZero():
			when, source = stamps[n], "poll"
		case !kept[n].IsZero():
			when, source = kept[n], "kept"
		}
		if when.IsZero() || !plausible(n, when) {
			continue
		}
		fmt.Fprintf(&sb, "%s,%d,%s\n", n, when.UnixMicro(), source)
		written++
	}
	if written == 0 {
		return
	}

	// Written whole and renamed into place, so a reader either sees the previous
	// index or this one, never half of either.
	path := filepath.Join(x.dir, segIndexName)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(sb.String()), 0o644); err != nil {
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
	}
}

func hasRow(rows map[string]segListRow, name string) bool {
	_, ok := rows[name]
	return ok
}

// estimateAnchor works out what wall-clock moment the capture timeline's zero
// corresponds to, using every segment that has both a poll stamp and a row.
//
// The median rather than the mean, because one segment written while the disk
// stalled should not drag the answer for all the others.
func estimateAnchor(names []string, stamps map[string]time.Time, rows map[string]segListRow, first string) time.Time {
	var zeros []time.Time
	for _, n := range names {
		if n == first {
			continue // startup bias; see the trap in this file's header
		}
		st := stamps[n]
		if st.IsZero() {
			continue // belongs to an earlier session, no stamp of ours
		}
		r, ok := rows[n]
		if !ok {
			continue
		}
		zeros = append(zeros, st.Add(-r.start))
	}
	if len(zeros) == 0 {
		return time.Time{}
	}
	sort.Slice(zeros, func(i, j int) bool { return zeros[i].Before(zeros[j]) })
	return zeros[len(zeros)/2]
}

// plausible is the guard that keeps a broken index harmless. A correct precise
// time sits within a second either side of the filename - after it usually,
// before it for the first segment of a session, and never further than that.
// The margin is deliberately far wider than either case: this exists to catch a
// clock that jumped or a folder copied off another machine, not to second-guess
// a few hundred milliseconds it cannot judge better than the maths above can.
func plausible(name string, when time.Time) bool {
	fromName, ok := segmentTime(name)
	if !ok {
		return false
	}
	d := when.Sub(fromName)
	if d < 0 {
		d = -d
	}
	return d <= segStampSanity
}

// readSegmentList parses ffmpeg's segment list. Anything malformed is skipped
// rather than fatal - a list being appended to while we read it will have a
// short final line, and that is normal rather than a problem.
func readSegmentList(path string) map[string]segListRow {
	rows := map[string]segListRow{}
	b, err := os.ReadFile(path)
	if err != nil {
		return rows
	}
	for _, line := range strings.Split(string(b), "\n") {
		parts := strings.Split(strings.TrimSpace(line), ",")
		if len(parts) != 3 {
			continue
		}
		start, err1 := strconv.ParseFloat(parts[1], 64)
		end, err2 := strconv.ParseFloat(parts[2], 64)
		if err1 != nil || err2 != nil {
			continue
		}
		rows[filepath.Base(parts[0])] = segListRow{
			start: time.Duration(start * float64(time.Second)),
			end:   time.Duration(end * float64(time.Second)),
		}
	}
	return rows
}

// readSegIndex hands ringbuf.go the precise start of every segment it knows
// about. A segment missing from the result is not an error and never has been -
// it simply keeps the start its filename gives it.
func readSegIndex(dir string) map[string]time.Time {
	out := map[string]time.Time{}
	b, err := os.ReadFile(filepath.Join(dir, segIndexName))
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, ",")
		if len(parts) < 2 {
			continue
		}
		micros, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			continue
		}
		when := time.UnixMicro(micros)
		if !plausible(parts[0], when) {
			continue
		}
		out[parts[0]] = when
	}
	return out
}

// removeSegIndex clears both this app's index and ffmpeg's list. Called from
// clearBuffer, because leaving them behind would have the next session's first
// segments anchored against a capture that no longer exists.
func removeSegIndex(dir string) {
	_ = os.Remove(filepath.Join(dir, segIndexName))
	_ = os.Remove(filepath.Join(dir, segIndexName+".tmp"))
	_ = os.Remove(filepath.Join(dir, segListName))
}
