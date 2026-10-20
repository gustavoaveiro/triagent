package video

import (
	"math"
	"math/rand"
	"time"
)

// SampleConfig controls how densely a video is sampled for classification.
type SampleConfig struct {
	// Interval is the target spacing between samples for videos short
	// enough not to hit MaxSamples.
	Interval time.Duration
	// MinSamples is the floor, so even very short clips get checked.
	MinSamples int
	// MaxSamples bounds worst-case processing time on long videos; the
	// full duration is still covered, just with sparser sampling.
	MaxSamples int
}

// DefaultSampleConfig: ~1 sample every 2 seconds, at least 5, at most 120.
var DefaultSampleConfig = SampleConfig{
	Interval:   2 * time.Second,
	MinSamples: 5,
	MaxSamples: 120,
}

// Sampler picks one random timestamp within each of N evenly-sized
// segments spanning a video's full duration, guaranteeing full-duration
// coverage regardless of length (rather than e.g. a fixed time interval
// that would only cover the first portion of a long video once capped).
type Sampler struct {
	rng *rand.Rand
}

// NewSampler creates a Sampler seeded with seed. Production code should
// seed with something like time.Now().UnixNano(); tests use a fixed seed
// for deterministic, assertable output.
func NewSampler(seed int64) *Sampler {
	return &Sampler{rng: rand.New(rand.NewSource(seed))}
}

// Timestamps returns one timestamp per segment.
func (s *Sampler) Timestamps(duration time.Duration, cfg SampleConfig) []time.Duration {
	if duration <= 0 {
		return nil
	}

	n := segmentCount(duration, cfg)
	segmentLen := duration / time.Duration(n)

	timestamps := make([]time.Duration, n)
	for i := 0; i < n; i++ {
		segStart := time.Duration(i) * segmentLen
		offset := time.Duration(s.rng.Int63n(int64(segmentLen) + 1))
		ts := segStart + offset
		if ts >= duration {
			// Only possible when the random offset lands on the exact
			// upper bound of the final segment, which can coincide with
			// duration itself due to integer-division rounding.
			ts = duration - 1
		}
		timestamps[i] = ts
	}
	return timestamps
}

// segmentCount is the pure sizing policy, separated out for direct,
// fast unit tests independent of any random sampling.
func segmentCount(duration time.Duration, cfg SampleConfig) int {
	n := int(math.Round(float64(duration) / float64(cfg.Interval)))
	if n < cfg.MinSamples {
		n = cfg.MinSamples
	}
	if n > cfg.MaxSamples {
		n = cfg.MaxSamples
	}
	if n < 1 {
		n = 1
	}
	return n
}
