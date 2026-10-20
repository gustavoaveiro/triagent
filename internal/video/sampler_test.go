package video

import (
	"testing"
	"time"
)

func TestSegmentCount(t *testing.T) {
	cfg := SampleConfig{Interval: 2 * time.Second, MinSamples: 5, MaxSamples: 120}

	tests := []struct {
		name     string
		duration time.Duration
		want     int
	}{
		{"10s clamps to min", 10 * time.Second, 5},
		{"60s natural", 60 * time.Second, 30},
		{"10min clamps to max", 10 * time.Minute, 120},
		{"2h clamps to max", 2 * time.Hour, 120},
		{"zero duration still returns at least 1", 0, 5}, // min applies
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := segmentCount(tc.duration, cfg); got != tc.want {
				t.Errorf("segmentCount(%v) = %d, want %d", tc.duration, got, tc.want)
			}
		})
	}
}

func TestSegmentCount_FloorsAtOneEvenIfMinSamplesIsZero(t *testing.T) {
	// A misconfigured MinSamples of 0 (e.g. an unvalidated CLI flag)
	// shouldn't produce a zero-length sample set.
	cfg := SampleConfig{Interval: time.Hour, MinSamples: 0, MaxSamples: 120}
	if got := segmentCount(time.Second, cfg); got != 1 {
		t.Errorf("segmentCount = %d, want 1", got)
	}
}

func TestSampler_TimestampsSpanFullDuration(t *testing.T) {
	s := NewSampler(42)
	duration := 2 * time.Hour
	cfg := DefaultSampleConfig

	timestamps := s.Timestamps(duration, cfg)

	if len(timestamps) != 120 {
		t.Fatalf("got %d timestamps, want 120", len(timestamps))
	}
	// Full-duration coverage: the last timestamp must be in the final
	// segment, not bunched up near the start (the bug in a naive capped
	// fixed-interval approach).
	lastSegmentStart := duration * 119 / 120
	if timestamps[119] < lastSegmentStart {
		t.Errorf("last timestamp = %v, want >= %v (final segment)", timestamps[119], lastSegmentStart)
	}
	for i, ts := range timestamps {
		if ts < 0 || ts >= duration {
			t.Errorf("timestamps[%d] = %v, out of [0, %v)", i, ts, duration)
		}
	}
}

func TestSampler_DeterministicGivenSameSeed(t *testing.T) {
	cfg := DefaultSampleConfig
	a := NewSampler(7).Timestamps(90*time.Second, cfg)
	b := NewSampler(7).Timestamps(90*time.Second, cfg)

	if len(a) != len(b) {
		t.Fatalf("length mismatch: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Errorf("timestamps[%d] = %v, want %v (same seed must reproduce)", i, b[i], a[i])
		}
	}
}

func TestSampler_DifferentSeedsDiffer(t *testing.T) {
	cfg := DefaultSampleConfig
	a := NewSampler(1).Timestamps(90*time.Second, cfg)
	b := NewSampler(2).Timestamps(90*time.Second, cfg)

	allSame := true
	for i := range a {
		if a[i] != b[i] {
			allSame = false
			break
		}
	}
	if allSame {
		t.Error("expected different seeds to produce different timestamps")
	}
}

func TestSampler_ZeroOrNegativeDurationReturnsNil(t *testing.T) {
	s := NewSampler(1)
	if got := s.Timestamps(0, DefaultSampleConfig); got != nil {
		t.Errorf("Timestamps(0) = %v, want nil", got)
	}
	if got := s.Timestamps(-time.Second, DefaultSampleConfig); got != nil {
		t.Errorf("Timestamps(negative) = %v, want nil", got)
	}
}

func TestSampler_ClampsTimestampLandingExactlyOnDuration(t *testing.T) {
	// Seed 1 with this exact (duration, cfg) is verified (by brute-force
	// search) to draw a random offset that lands the last segment's
	// timestamp exactly on duration, requiring the clamp.
	s := NewSampler(1)
	duration := 5 * time.Nanosecond
	cfg := SampleConfig{Interval: time.Hour, MinSamples: 5, MaxSamples: 120}

	timestamps := s.Timestamps(duration, cfg)

	for i, ts := range timestamps {
		if ts >= duration {
			t.Errorf("timestamps[%d] = %v, want < duration (%v)", i, ts, duration)
		}
	}
}

func TestSampler_VeryShortDurationStaysInBounds(t *testing.T) {
	s := NewSampler(3)
	// Shorter than MinSamples * 1ms: segments are tiny, must not panic or
	// produce timestamps outside [0, duration).
	timestamps := s.Timestamps(3*time.Millisecond, DefaultSampleConfig)
	for i, ts := range timestamps {
		if ts < 0 || ts >= 3*time.Millisecond {
			t.Errorf("timestamps[%d] = %v, out of bounds", i, ts)
		}
	}
}
