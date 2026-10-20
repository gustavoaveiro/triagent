package video

import (
	"fmt"
	"os"
	"path/filepath"
)

// Scorer scores a single image file for explicit content, in [0,1].
// Satisfied by *classifier.Classifier.
type Scorer interface {
	Score(path string) (float64, error)
}

// Result is the outcome of scoring one video.
type Result struct {
	// MaxScore is the highest score seen across all sampled frames - a
	// single explicit frame should flag the whole video, not get diluted
	// by averaging against mostly-safe frames.
	MaxScore float64
	// SamplesChecked is how many frames were actually extracted and
	// scored (may be less than the sampler targeted, if some timestamps
	// failed to extract).
	SamplesChecked int
}

// ScoreVideo samples frames from videoPath per cfg, scores each with
// scorer, and returns the aggregated Result.
func ScoreVideo(videoPath string, scorer Scorer, sampler *Sampler, cfg SampleConfig) (Result, error) {
	duration, err := Duration(videoPath)
	if err != nil {
		return Result{}, fmt.Errorf("probe duration: %w", err)
	}

	timestamps := sampler.Timestamps(duration, cfg)

	tmpDir, err := os.MkdirTemp("", "triagent-frames-*")
	if err != nil {
		return Result{}, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	var result Result
	for i, ts := range timestamps {
		framePath := filepath.Join(tmpDir, fmt.Sprintf("frame_%d.jpg", i))
		if err := ExtractFrame(videoPath, ts, framePath); err != nil {
			continue
		}
		score, err := scorer.Score(framePath)
		if err != nil {
			continue
		}
		result.SamplesChecked++
		if score > result.MaxScore {
			result.MaxScore = score
		}
	}

	if result.SamplesChecked == 0 {
		return Result{}, fmt.Errorf("no frames could be extracted or classified from %d sample points", len(timestamps))
	}
	return result, nil
}
