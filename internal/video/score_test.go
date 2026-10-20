package video

import (
	"errors"
	"testing"
)

// fakeScorer returns a scripted sequence of scores/errors, one per call,
// so tests can exercise ScoreVideo's aggregation logic without needing a
// real ONNX classifier.
type fakeScorer struct {
	scores []float64
	err    error // if set, every call fails with this error
	calls  int
}

func (f *fakeScorer) Score(string) (float64, error) {
	if f.err != nil {
		return 0, f.err
	}
	score := f.scores[f.calls%len(f.scores)]
	f.calls++
	return score, nil
}

func TestScoreVideo_PicksMaxScoreAcrossFrames(t *testing.T) {
	requireFFmpeg(t)
	video := makeTestVideo(t, 2)

	scorer := &fakeScorer{scores: []float64{0.1, 0.9, 0.3, 0.05, 0.2}}
	cfg := SampleConfig{Interval: 2, MinSamples: 5, MaxSamples: 5}

	result, err := ScoreVideo(video, scorer, NewSampler(1), cfg)
	if err != nil {
		t.Fatalf("ScoreVideo: %v", err)
	}
	if result.MaxScore != 0.9 {
		t.Errorf("MaxScore = %v, want 0.9", result.MaxScore)
	}
	if result.SamplesChecked != 5 {
		t.Errorf("SamplesChecked = %d, want 5", result.SamplesChecked)
	}
}

func TestScoreVideo_DurationProbeFailureIsReported(t *testing.T) {
	requireFFmpeg(t)
	_, err := ScoreVideo(mustNotExist(t), &fakeScorer{scores: []float64{0}}, NewSampler(1), DefaultSampleConfig)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
}

func TestScoreVideo_AllScoringFailuresReportsError(t *testing.T) {
	requireFFmpeg(t)
	video := makeTestVideo(t, 2)

	scorer := &fakeScorer{err: errors.New("simulated classifier failure")}
	_, err := ScoreVideo(video, scorer, NewSampler(1), SampleConfig{Interval: 2, MinSamples: 3, MaxSamples: 3})
	if err == nil {
		t.Fatal("expected an error when every frame fails to score, got nil")
	}
}

func TestScoreVideo_TempDirCreationFailureIsReported(t *testing.T) {
	requireFFmpeg(t)
	video := makeTestVideo(t, 2)

	t.Setenv("TMPDIR", "/this/path/does/not/exist/xyz")

	_, err := ScoreVideo(video, &fakeScorer{scores: []float64{0.1}}, NewSampler(1), DefaultSampleConfig)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
}

func TestScoreVideo_AllExtractionFailuresReportsError(t *testing.T) {
	requireFFmpeg(t)
	video := makeTestVideo(t, 2)

	original := ffmpegPath
	ffmpegPath = "this-binary-does-not-exist-xyz"
	defer func() { ffmpegPath = original }()

	_, err := ScoreVideo(video, &fakeScorer{scores: []float64{0.5}}, NewSampler(1), SampleConfig{Interval: 2, MinSamples: 3, MaxSamples: 3})
	if err == nil {
		t.Fatal("expected an error when every frame fails to extract, got nil")
	}
}
