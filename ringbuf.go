// ringbuf.go - the rolling buffer on disk: what a segment is, how to find the
// footage covering a moment in time, and how the disk budget is enforced.
//
// THE KEY IDEA: SEGMENTS ARE NAMED BY WALL-CLOCK TIME
//
// ffmpeg writes each segment as seg_YYYYMMDD-HHMMSS.mp4, stamped with the moment
// that segment started. That turns "when did this round end" into "which file,
// and how far into it" with nothing but arithmetic. No parsing, no index, no
// database, and it survives the app being killed mid-session because the answer
// is written into the filenames themselves.
//
// It is also why the buffer can be pruned safely. Deleting the oldest footage is
// deleting whole files, so there is no container to rewrite and no risk of
// corrupting the segment ffmpeg is still writing into.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// segmentPattern is handed to ffmpeg's -strftime. Change this and segmentTime
// below must change with it.
const segmentPattern = "seg_%Y%m%d-%H%M%S.mp4"

const segmentTimeLayout = "20060102-150405"

type segment struct {
	Path  string
	Start time.Time
	End   time.Time // inferred: the next segment's start, or this file's mtime
	Size  int64
}

func (s segment) duration() time.Duration { return s.End.Sub(s.Start) }

// covers reports whether this segment contains the given instant.
func (s segment) covers(t time.Time) bool {
	return !t.Before(s.Start) && t.Before(s.End)
}

// segmentTime pulls the wall-clock start out of a segment filename. Anything
// that does not match the pattern is not ours and is ignored rather than
// deleted - the buffer folder is inside the player's clip folder, and deleting
// a stranger's file because it was in the wrong place is unforgivable.
func segmentTime(name string) (time.Time, bool) {
	base := filepath.Base(name)
	if !strings.HasPrefix(base, "seg_") || !strings.HasSuffix(base, ".mp4") {
		return time.Time{}, false
	}
	stamp := strings.TrimSuffix(strings.TrimPrefix(base, "seg_"), ".mp4")
	t, err := time.ParseInLocation(segmentTimeLayout, stamp, time.Local)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// listSegments returns every buffer segment in start order, with End filled in.
//
// The last segment's End comes from its modification time, because it may still
// be being written. That makes End a slight under-estimate for the live segment,
// which is the safe direction: we would rather believe we have slightly less
// footage than we do than trim past the end of a file.
func listSegments(dir string) ([]segment, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var segs []segment
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		start, ok := segmentTime(e.Name())
		if !ok {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		segs = append(segs, segment{
			Path:  filepath.Join(dir, e.Name()),
			Start: start,
			End:   info.ModTime(),
			Size:  info.Size(),
		})
	}

	sort.Slice(segs, func(i, j int) bool { return segs[i].Start.Before(segs[j].Start) })

	for i := 0; i < len(segs)-1; i++ {
		next := segs[i+1].Start
		// Trust the next segment's start over mtime, unless the clock did
		// something strange (DST, a manual change) and produced a negative span.
		if next.After(segs[i].Start) {
			segs[i].End = next
		}
	}
	// A zero-length final segment means ffmpeg has only just opened it.
	if n := len(segs); n > 0 && !segs[n-1].End.After(segs[n-1].Start) {
		segs[n-1].End = segs[n-1].Start
	}
	return segs, nil
}

// bufferSpan reports the wall-clock window the buffer currently holds. Used to
// tell a player honestly that a moment they asked for is already gone.
func bufferSpan(dir string) (from, to time.Time, ok bool) {
	segs, err := listSegments(dir)
	if err != nil || len(segs) == 0 {
		return time.Time{}, time.Time{}, false
	}
	return segs[0].Start, segs[len(segs)-1].End, true
}

// segmentsCovering returns the segments needed to reconstruct footage between
// two instants, in order. A gap in coverage is reported rather than silently
// stitched, because a clip that jumps two minutes without saying so is a clip
// that will make somebody think the AI misread their match.
func segmentsCovering(dir string, from, to time.Time) (segs []segment, gaps []string, err error) {
	all, err := listSegments(dir)
	if err != nil {
		return nil, nil, err
	}
	if len(all) == 0 {
		return nil, nil, fmt.Errorf("buffer is empty")
	}

	var picked []segment
	for _, s := range all {
		if s.End.After(from) && s.Start.Before(to) {
			picked = append(picked, s)
		}
	}
	if len(picked) == 0 {
		return nil, nil, fmt.Errorf("no footage covering %s - %s (buffer holds %s - %s)",
			from.Format("15:04:05"), to.Format("15:04:05"),
			all[0].Start.Format("15:04:05"), all[len(all)-1].End.Format("15:04:05"))
	}

	// Allow a second of slop: segment boundaries are not perfectly abutting.
	const slop = 1500 * time.Millisecond
	for i := 0; i < len(picked)-1; i++ {
		if picked[i+1].Start.Sub(picked[i].End) > slop {
			gaps = append(gaps, fmt.Sprintf("%s-%s",
				picked[i].End.Format("15:04:05"), picked[i+1].Start.Format("15:04:05")))
		}
	}
	if picked[0].Start.Sub(from) > slop {
		gaps = append(gaps, fmt.Sprintf("start %s (asked for %s)",
			picked[0].Start.Format("15:04:05"), from.Format("15:04:05")))
	}
	if to.Sub(picked[len(picked)-1].End) > slop {
		gaps = append(gaps, fmt.Sprintf("end %s (asked for %s)",
			picked[len(picked)-1].End.Format("15:04:05"), to.Format("15:04:05")))
	}

	return picked, gaps, nil
}

var pruneMu sync.Mutex

// pruneBuffer enforces the two limits the player set: how far back the buffer
// reaches, and how much disk it may occupy. Oldest goes first in both cases.
//
// The newest segment is NEVER deleted, even if it alone exceeds the budget -
// ffmpeg is probably still writing into it, and deleting an open file on Windows
// either fails or produces a recording that silently stops working.
func pruneBuffer(dir string, maxAge time.Duration, maxBytes int64) {
	pruneMu.Lock()
	defer pruneMu.Unlock()

	segs, err := listSegments(dir)
	if err != nil || len(segs) <= 1 {
		return
	}

	cutoff := time.Now().Add(-maxAge)
	var total int64
	for _, s := range segs {
		total += s.Size
	}

	removed, freed := 0, int64(0)
	for i := 0; i < len(segs)-1; i++ {
		tooOld := segs[i].End.Before(cutoff)
		tooBig := total > maxBytes
		if !tooOld && !tooBig {
			break
		}
		if err := os.Remove(segs[i].Path); err != nil {
			continue
		}
		total -= segs[i].Size
		freed += segs[i].Size
		removed++
	}
	if removed > 0 {
		logf("recorder: pruned %d buffer segments (%d MB)", removed, freed/(1024*1024))
	}
}

// clearBuffer empties the scratch folder. Called when the recorder is switched
// off, so turning it off actually reclaims the disk rather than leaving several
// gigabytes sitting there until the next session.
func clearBuffer(dir string) {
	segs, err := listSegments(dir)
	if err != nil {
		return
	}
	for _, s := range segs {
		_ = os.Remove(s.Path)
	}
}

// ---- small shared helpers -------------------------------------------------

func itoa(n int) string { return fmt.Sprintf("%d", n) }

// lineRing keeps the last N lines of a child process's output so a failure can
// be explained with something concrete instead of an exit code.
type lineRing struct {
	mu    sync.Mutex
	lines []string
	max   int
}

func newLineRing(max int) *lineRing { return &lineRing{max: max} }

func (r *lineRing) add(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, line)
	if len(r.lines) > r.max {
		r.lines = r.lines[len(r.lines)-r.max:]
	}
}

func (r *lineRing) joined() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.lines) == 0 {
		return "(no output)"
	}
	n := len(r.lines)
	if n > 4 {
		n = 4
	}
	return strings.Join(r.lines[len(r.lines)-n:], " | ")
}
