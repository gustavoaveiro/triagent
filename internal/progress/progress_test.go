package progress

import (
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSnapshot_Percent(t *testing.T) {
	tests := []struct {
		name string
		s    Snapshot
		want float64
	}{
		{"zero totals", Snapshot{}, 0},
		{"bytes-based", Snapshot{BytesDone: 50, BytesTotal: 200}, 0.25},
		{"bytes over total clamps to 1", Snapshot{BytesDone: 300, BytesTotal: 200}, 1},
		{"falls back to files when bytes total is zero", Snapshot{FilesDone: 3, FilesTotal: 4}, 0.75},
		{"files over total clamps to 1", Snapshot{FilesDone: 9, FilesTotal: 4}, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.s.Percent(); got != tc.want {
				t.Errorf("Percent() = %v, want %v", got, tc.want)
			}
		})
	}
}

// fakeClock lets tests advance time deterministically instead of sleeping.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func TestTracker_SnapshotReflectsTotalsAndProgress(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	tr := newTrackerWithClock(clock.Now)

	tr.SetTotal(10, 1000)
	s := tr.Snapshot()
	if s.FilesTotal != 10 || s.BytesTotal != 1000 {
		t.Fatalf("totals = %+v", s)
	}
	if s.ETA != 0 {
		t.Errorf("ETA with nothing done yet = %v, want 0", s.ETA)
	}

	clock.Advance(10 * time.Second)
	tr.FileDone(100)
	s = tr.Snapshot()
	if s.FilesDone != 1 || s.BytesDone != 100 {
		t.Fatalf("progress = %+v", s)
	}
	// 100 bytes in 10s = 10 B/s; 900 bytes remaining -> 90s ETA.
	if s.ETA != 90*time.Second {
		t.Errorf("ETA = %v, want 90s", s.ETA)
	}

	clock.Advance(90 * time.Second)
	for i := 0; i < 9; i++ {
		tr.FileDone(100)
	}
	s = tr.Snapshot()
	if s.BytesDone != 1000 || s.FilesDone != 10 {
		t.Fatalf("final progress = %+v", s)
	}
	if s.ETA != 0 {
		t.Errorf("ETA once fully done = %v, want 0 (no remaining work)", s.ETA)
	}
	if s.Percent() != 1 {
		t.Errorf("Percent() = %v, want 1", s.Percent())
	}
}

func TestTracker_ETAFallsBackToFileRateWhenBytesTotalUnknown(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	tr := newTrackerWithClock(clock.Now)

	tr.SetTotal(4, 0) // e.g. every file turned out to be empty
	clock.Advance(2 * time.Second)
	tr.FileDone(0)

	s := tr.Snapshot()
	// 1 file in 2s = 0.5 files/s; 3 remaining -> 6s ETA.
	if s.ETA != 6*time.Second {
		t.Errorf("ETA = %v, want 6s", s.ETA)
	}
}

func TestTracker_ConcurrentFileDoneIsRaceFree(t *testing.T) {
	tr := NewTracker()
	tr.SetTotal(100, 1000)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tr.FileDone(10)
		}()
	}
	wg.Wait()

	s := tr.Snapshot()
	if s.FilesDone != 100 || s.BytesDone != 1000 {
		t.Errorf("after concurrent FileDone calls: %+v", s)
	}
}

func TestClamp01(t *testing.T) {
	tests := []struct {
		in   float64
		want float64
	}{
		{-0.5, 0},
		{0, 0},
		{0.5, 0.5},
		{1, 1},
		{1.5, 1},
	}
	for _, tc := range tests {
		if got := clamp01(tc.in); got != tc.want {
			t.Errorf("clamp01(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1024 * 1024, "1.0 MB"},
		{1024 * 1024 * 1024, "1.0 GB"},
		{1024 * 1024 * 1024 * 1024, "1.0 TB"},
		{1024 * 1024 * 1024 * 1024 * 1024, "1.0 PB"},
		{1024 * 1024 * 1024 * 1024 * 1024 * 1024, "1024.0 PB"}, // caps at PB
	}
	for _, tc := range tests {
		if got := FormatBytes(tc.in); got != tc.want {
			t.Errorf("FormatBytes(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestRenderLabel(t *testing.T) {
	s := Snapshot{FilesDone: 3, FilesTotal: 10, BytesDone: 300, BytesTotal: 1000, Elapsed: 10 * time.Second, ETA: 20 * time.Second}
	label := RenderLabel(s)
	for _, want := range []string{"3/10 files", "300 B", "1000 B", "ETA 20s"} {
		if !strings.Contains(label, want) {
			t.Errorf("RenderLabel() = %q, missing %q", label, want)
		}
	}
}

func TestRenderLabel_FullyDoneShowsZeroETA(t *testing.T) {
	s := Snapshot{FilesDone: 5, FilesTotal: 5, BytesDone: 100, BytesTotal: 100}
	label := RenderLabel(s)
	if !strings.Contains(label, "ETA 0s") {
		t.Errorf("RenderLabel() = %q, want ETA 0s", label)
	}
}

func TestRenderLabel_NoProgressYetShowsCalculating(t *testing.T) {
	label := RenderLabel(Snapshot{})
	if !strings.Contains(label, "calculating...") {
		t.Errorf("RenderLabel() = %q, want it to mention calculating", label)
	}
}

func TestRenderBar(t *testing.T) {
	s := Snapshot{BytesDone: 50, BytesTotal: 100, FilesDone: 1, FilesTotal: 2}
	bar := RenderBar(s, 10)
	if !strings.Contains(bar, "50.0%") {
		t.Errorf("RenderBar() = %q, want it to contain 50.0%%", bar)
	}
	if !strings.HasPrefix(bar, "[") {
		t.Errorf("RenderBar() = %q, want it to start with [", bar)
	}
}

func TestRenderBar_FullyDoneFillsTheBar(t *testing.T) {
	s := Snapshot{BytesDone: 100, BytesTotal: 100}
	bar := RenderBar(s, 5)
	if !strings.Contains(bar, "[=====]") {
		t.Errorf("RenderBar() = %q, want a fully filled bar", bar)
	}
}

func TestRenderBar_EmptyBar(t *testing.T) {
	s := Snapshot{BytesTotal: 100}
	bar := RenderBar(s, 5)
	if !strings.Contains(bar, "[     ]") {
		t.Errorf("RenderBar() = %q, want an empty bar", bar)
	}
}
