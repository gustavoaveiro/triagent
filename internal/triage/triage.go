package triage

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gabriel-vasile/mimetype"

	"github.com/gustavoaveiro/triagent/internal/model"
	"github.com/gustavoaveiro/triagent/internal/progress"
	"github.com/gustavoaveiro/triagent/internal/scanner"
)

// MediaScorer scores images and videos for explicit content. Satisfied by
// an adapter wrapping *classifier.Classifier (+ *video.Sampler for video).
// Kept as an interface here so this package's decision/action logic is
// testable without a real ONNX model or ffmpeg.
type MediaScorer interface {
	ScoreImage(path string) (float64, error)
	// ScoreVideo returns the aggregated score and how many frames were
	// sampled.
	ScoreVideo(path string) (score float64, samplesChecked int, err error)
}

// Options configures a Run.
type Options struct {
	OutDir     string
	Workers    int
	Thresholds Thresholds
	Scorer     MediaScorer
	// DryRun scores files without moving/copying anything.
	DryRun bool
	// Progress, if non-nil, is notified once per entry as it finishes.
	Progress progress.Sink
}

// Run consumes entries until the channel is closed, processing them with a
// pool of opts.Workers goroutines, and returns one Record per entry.
//
// Note: classification is inference-bound and a single MediaScorer
// typically serializes inference internally (see classifier.Classifier),
// so more workers mainly help overlap frame extraction/file IO with the
// next inference call rather than running inference itself in parallel.
func Run(entries <-chan scanner.Entry, opts Options) []model.Record {
	workers := opts.Workers
	if workers < 1 {
		workers = 1
	}

	results := make(chan model.Record)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for e := range entries {
				rec := process(e, opts)
				if opts.Progress != nil {
					opts.Progress.FileDone(rec.SizeBytes)
				}
				results <- rec
			}
		}()
	}
	go func() {
		wg.Wait()
		close(results)
	}()

	var records []model.Record
	for r := range results {
		records = append(records, r)
	}
	return records
}

func process(e scanner.Entry, opts Options) model.Record {
	rec := model.Record{
		SourcePath: e.Path,
		FileName:   filepath.Base(e.Path),
		Timestamp:  time.Now().UTC(),
	}

	switch e.Kind {
	case scanner.KindSkippedSymlink:
		rec.Status = model.StatusSkippedSymlink
		rec.Reason = "symbolic link - not followed"
		return rec
	case scanner.KindExcluded:
		rec.Status = model.StatusExcluded
		rec.Reason = fmt.Sprintf("matched exclude pattern %q", e.MatchedPattern)
		return rec
	case scanner.KindError:
		rec.Status = model.StatusFailed
		rec.Reason = e.Err.Error()
		return rec
	}

	rec.SizeBytes = e.Info.Size()

	mediaType, err := detectMediaType(e.Path)
	if err != nil {
		rec.Status = model.StatusFailed
		rec.Reason = fmt.Sprintf("detect media type: %v", err)
		return rec
	}
	rec.MediaType = mediaType

	if mediaType == model.MediaUnsupported {
		rec.Status = model.StatusLeftInPlace
		rec.Reason = "not an image or video"
		return rec
	}

	var score float64
	switch mediaType {
	case model.MediaImage:
		score, err = opts.Scorer.ScoreImage(e.Path)
	case model.MediaVideo:
		var samples int
		score, samples, err = opts.Scorer.ScoreVideo(e.Path)
		rec.SamplesChecked = samples
	}
	if err != nil {
		rec.Status = model.StatusFailed
		rec.Reason = fmt.Sprintf("classify: %v", err)
		return rec
	}
	rec.Score = score
	rec.Bucket = opts.Thresholds.Bucket(score)

	if opts.DryRun {
		applyDryRun(&rec, opts.Thresholds)
		return rec
	}

	switch rec.Bucket {
	case model.BucketSafe:
		rec.Status = model.StatusLeftInPlace
		rec.Reason = fmt.Sprintf("score %.4f below review threshold %.2f", score, opts.Thresholds.Low)
	case model.BucketSuggestive:
		applyAction(&rec, e.Path, opts.OutDir, model.StatusCopiedForReview, "copy", copyFile,
			fmt.Sprintf("score %.4f between thresholds (%.2f-%.2f) - needs manual review", score, opts.Thresholds.Low, opts.Thresholds.High))
	default: // BucketExplicit
		applyAction(&rec, e.Path, opts.OutDir, model.StatusMoved, "move", moveFile,
			fmt.Sprintf("score %.4f >= move threshold %.2f", score, opts.Thresholds.High))
	}
	return rec
}

func applyDryRun(rec *model.Record, t Thresholds) {
	switch rec.Bucket {
	case model.BucketExplicit:
		rec.Status = model.StatusMoved
	case model.BucketSuggestive:
		rec.Status = model.StatusCopiedForReview
	default:
		rec.Status = model.StatusLeftInPlace
	}
	rec.Reason = fmt.Sprintf("dry run - score %.4f, not moved/copied", rec.Score)
}

// applyAction resolves a destination under outDir/<bucket>/ and runs op
// (copyFile or moveFile) to get the source file there.
func applyAction(rec *model.Record, srcPath, outDir string, status model.Status, verb string, op func(src, dst string) error, reason string) {
	dest, renamed, err := resolveDestination(filepath.Join(outDir, string(rec.Bucket)), rec.FileName)
	if err != nil {
		rec.Status = model.StatusFailed
		rec.Reason = fmt.Sprintf("resolve destination: %v", err)
		return
	}

	if err := op(srcPath, dest); err != nil {
		rec.Status = model.StatusFailed
		rec.Reason = fmt.Sprintf("%s: %v", verb, err)
		return
	}

	rec.DestPath = dest
	rec.Status = status
	if renamed {
		reason += "; destination name collision, auto-renamed"
	}
	rec.Reason = reason
}

func detectMediaType(path string) (model.MediaType, error) {
	mt, err := mimetype.DetectFile(path)
	if err != nil {
		return "", err
	}
	s := mt.String()
	switch {
	case strings.HasPrefix(s, "image/"):
		return model.MediaImage, nil
	case strings.HasPrefix(s, "video/"):
		return model.MediaVideo, nil
	default:
		return model.MediaUnsupported, nil
	}
}
