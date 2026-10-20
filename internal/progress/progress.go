// Package progress tracks and renders the progress of a running backup:
// how many files/bytes are done out of the known total, and an estimated
// time to completion based on observed throughput.
package progress

import (
	"fmt"
	"strings"
	"sync/atomic"
	"time"
)

// Sink receives progress updates. Implementations must be safe for
// concurrent use: FileDone is called from multiple copier worker
// goroutines, and SetTotal is called once before work starts.
type Sink interface {
	SetTotal(files int, bytes int64)
	FileDone(size int64)
}

// Snapshot is a point-in-time view of a Tracker's progress.
type Snapshot struct {
	FilesDone  int64
	FilesTotal int64
	BytesDone  int64
	BytesTotal int64
	Elapsed    time.Duration
	// ETA is the estimated remaining time, or 0 if it can't be estimated
	// yet (nothing done, or total unknown).
	ETA time.Duration
}

// Percent returns completion in [0,1], preferring bytes (a better proxy
// for work done) and falling back to file count when total size is
// unknown or zero (e.g. every file is empty).
func (s Snapshot) Percent() float64 {
	switch {
	case s.BytesTotal > 0:
		return clamp01(float64(s.BytesDone) / float64(s.BytesTotal))
	case s.FilesTotal > 0:
		return clamp01(float64(s.FilesDone) / float64(s.FilesTotal))
	default:
		return 0
	}
}

func clamp01(f float64) float64 {
	if f < 0 {
		return 0
	}
	if f > 1 {
		return 1
	}
	return f
}

// Tracker is a concurrency-safe Sink, queried by a renderer (CLI or GUI)
// on its own schedule via Snapshot.
type Tracker struct {
	// now is set once at construction, before any other goroutine can
	// observe the Tracker, and never written again - safe to read
	// concurrently without further synchronization.
	now func() time.Time

	start      time.Time
	filesDone  atomic.Int64
	filesTotal atomic.Int64
	bytesDone  atomic.Int64
	bytesTotal atomic.Int64
}

// NewTracker creates a Tracker whose clock starts now.
func NewTracker() *Tracker {
	return newTrackerWithClock(time.Now)
}

// newTrackerWithClock lets tests substitute a deterministic clock instead
// of relying on real elapsed wall-clock time.
func newTrackerWithClock(now func() time.Time) *Tracker {
	return &Tracker{start: now(), now: now}
}

// SetTotal records the total amount of work expected, typically from a
// metadata-only pre-scan.
func (t *Tracker) SetTotal(files int, bytes int64) {
	t.filesTotal.Store(int64(files))
	t.bytesTotal.Store(bytes)
}

// FileDone records that one file finished processing (successfully or
// not), having size bytes - 0 for entries that were never read, such as
// skipped symlinks.
func (t *Tracker) FileDone(size int64) {
	t.filesDone.Add(1)
	t.bytesDone.Add(size)
}

// Snapshot returns the current progress and an ETA estimated from average
// throughput so far.
func (t *Tracker) Snapshot() Snapshot {
	elapsed := t.now().Sub(t.start)
	s := Snapshot{
		FilesDone:  t.filesDone.Load(),
		FilesTotal: t.filesTotal.Load(),
		BytesDone:  t.bytesDone.Load(),
		BytesTotal: t.bytesTotal.Load(),
		Elapsed:    elapsed,
	}

	switch {
	case s.BytesTotal > 0 && s.BytesDone > 0 && s.BytesDone < s.BytesTotal && elapsed > 0:
		rate := float64(s.BytesDone) / elapsed.Seconds()
		s.ETA = secondsToDuration(float64(s.BytesTotal-s.BytesDone) / rate)
	case s.BytesTotal == 0 && s.FilesTotal > 0 && s.FilesDone > 0 && s.FilesDone < s.FilesTotal && elapsed > 0:
		rate := float64(s.FilesDone) / elapsed.Seconds()
		s.ETA = secondsToDuration(float64(s.FilesTotal-s.FilesDone) / rate)
	}
	return s
}

func secondsToDuration(secs float64) time.Duration {
	return time.Duration(secs * float64(time.Second))
}

// FormatBytes renders b as a human-readable size, e.g. "850.0 MB".
func FormatBytes(b int64) string {
	const unit = 1024.0
	units := []string{"B", "KB", "MB", "GB", "TB", "PB"}

	if b < 1024 {
		return fmt.Sprintf("%d B", b)
	}

	f := float64(b)
	i := 0
	for f >= unit && i < len(units)-1 {
		f /= unit
		i++
	}
	return fmt.Sprintf("%.1f %s", f, units[i])
}

// RenderLabel renders a one-line textual summary of a Snapshot, suitable
// for a GUI status label or appending after a CLI progress bar.
func RenderLabel(s Snapshot) string {
	rate := 0.0
	if s.Elapsed > 0 {
		rate = float64(s.BytesDone) / s.Elapsed.Seconds()
	}

	eta := "calculating..."
	switch {
	case s.Percent() >= 1:
		eta = "0s"
	case s.ETA > 0:
		eta = s.ETA.Round(time.Second).String()
	}

	return fmt.Sprintf("%d/%d files - %s/%s - %s/s - ETA %s",
		s.FilesDone, s.FilesTotal,
		FormatBytes(s.BytesDone), FormatBytes(s.BytesTotal),
		FormatBytes(int64(rate)), eta)
}

// RenderBar renders an ASCII progress bar of the given width followed by
// RenderLabel's summary, suitable for a CLI overwritten with \r.
func RenderBar(s Snapshot, width int) string {
	pct := s.Percent()
	filled := int(pct*float64(width) + 0.5)
	bar := strings.Repeat("=", filled) + strings.Repeat(" ", width-filled)
	return fmt.Sprintf("[%s] %5.1f%%  %s", bar, pct*100, RenderLabel(s))
}
