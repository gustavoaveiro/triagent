// Package triage decides what to do with each scanned media file based on
// its explicit-content score, and carries out that decision (move, copy
// for review, or leave in place).
package triage

import "github.com/gustavoaveiro/triagent/internal/model"

// Thresholds configures the score cutoffs between buckets.
type Thresholds struct {
	// High is the score at or above which a file is bucketed Explicit and
	// physically moved out of the source tree.
	High float64
	// Low is the score below which a file is bucketed Safe and left
	// untouched. Scores in [Low, High) are bucketed Suggestive and copied
	// to a review folder without touching the source.
	Low float64
}

// DefaultThresholds is deliberately conservative on the move side: only
// very high-confidence matches get physically relocated, since a wrong
// high-confidence call would otherwise move an innocent file out of its
// original location.
var DefaultThresholds = Thresholds{High: 0.97, Low: 0.40}

// Bucket classifies a score according to the thresholds.
func (t Thresholds) Bucket(score float64) model.Bucket {
	switch {
	case score >= t.High:
		return model.BucketExplicit
	case score < t.Low:
		return model.BucketSafe
	default:
		return model.BucketSuggestive
	}
}
