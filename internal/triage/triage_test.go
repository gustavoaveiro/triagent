package triage

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/gustavoaveiro/triagent/internal/model"
	"github.com/gustavoaveiro/triagent/internal/scanner"
)

// fakeScorer returns scripted scores without needing a real ONNX model or
// ffmpeg, so this package's decision/action logic is testable in
// isolation.
type fakeScorer struct {
	imageScore      float64
	imageErr        error
	videoScore      float64
	videoSamples    int
	videoErr        error
	imageScoreCalls int
	videoScoreCalls int
}

func (f *fakeScorer) ScoreImage(string) (float64, error) {
	f.imageScoreCalls++
	return f.imageScore, f.imageErr
}

func (f *fakeScorer) ScoreVideo(string) (float64, int, error) {
	f.videoScoreCalls++
	return f.videoScore, f.videoSamples, f.videoErr
}

func writePNG(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.Set(x, y, color.RGBA{100, 150, 200, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func writeMP4(t *testing.T, path string) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not found on PATH")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	cmd := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi",
		"-i", "testsrc=duration=1:size=64x64:rate=5", "-y", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate test video: %v: %s", err, out)
	}
}

func fileEntry(t *testing.T, path string) scanner.Entry {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(%q): %v", path, err)
	}
	return scanner.Entry{Path: path, Info: info, Kind: scanner.KindFile}
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", path, err)
	}
	return b
}

func TestRun_ImageAboveHighThresholdIsMoved(t *testing.T) {
	in := t.TempDir()
	out := t.TempDir()
	src := filepath.Join(in, "photo.png")
	writePNG(t, src)

	entries := make(chan scanner.Entry, 1)
	entries <- fileEntry(t, src)
	close(entries)

	scorer := &fakeScorer{imageScore: 0.99}
	records := Run(entries, Options{OutDir: out, Workers: 1, Thresholds: DefaultThresholds, Scorer: scorer})

	if len(records) != 1 {
		t.Fatalf("got %d records, want 1", len(records))
	}
	rec := records[0]
	if rec.Status != model.StatusMoved {
		t.Errorf("status = %q, want Moved (reason: %s)", rec.Status, rec.Reason)
	}
	if rec.Bucket != model.BucketExplicit {
		t.Errorf("bucket = %q, want Explicit", rec.Bucket)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Error("source file should no longer exist after a move")
	}
	if _, err := os.Stat(rec.DestPath); err != nil {
		t.Errorf("destination file missing: %v", err)
	}
}

func TestRun_ImageBelowLowThresholdIsLeftInPlace(t *testing.T) {
	in := t.TempDir()
	out := t.TempDir()
	src := filepath.Join(in, "photo.png")
	writePNG(t, src)

	entries := make(chan scanner.Entry, 1)
	entries <- fileEntry(t, src)
	close(entries)

	scorer := &fakeScorer{imageScore: 0.01}
	records := Run(entries, Options{OutDir: out, Workers: 1, Thresholds: DefaultThresholds, Scorer: scorer})

	rec := records[0]
	if rec.Status != model.StatusLeftInPlace {
		t.Errorf("status = %q, want Left In Place", rec.Status)
	}
	if rec.Bucket != model.BucketSafe {
		t.Errorf("bucket = %q, want Safe", rec.Bucket)
	}
	if _, err := os.Stat(src); err != nil {
		t.Errorf("source file should be untouched: %v", err)
	}
	if rec.DestPath != "" {
		t.Errorf("DestPath = %q, want empty", rec.DestPath)
	}
}

func TestRun_ImageInBetweenIsCopiedForReview(t *testing.T) {
	in := t.TempDir()
	out := t.TempDir()
	src := filepath.Join(in, "photo.png")
	writePNG(t, src)
	original := readFile(t, src)

	entries := make(chan scanner.Entry, 1)
	entries <- fileEntry(t, src)
	close(entries)

	scorer := &fakeScorer{imageScore: 0.70}
	records := Run(entries, Options{OutDir: out, Workers: 1, Thresholds: DefaultThresholds, Scorer: scorer})

	rec := records[0]
	if rec.Status != model.StatusCopiedForReview {
		t.Errorf("status = %q, want Copied - For Review", rec.Status)
	}
	if rec.Bucket != model.BucketSuggestive {
		t.Errorf("bucket = %q, want Suggestive", rec.Bucket)
	}
	if _, err := os.Stat(src); err != nil {
		t.Errorf("source file should be untouched: %v", err)
	}
	copied := readFile(t, rec.DestPath)
	if !bytes.Equal(copied, original) {
		t.Error("copied file content doesn't match source")
	}
}

func TestRun_VideoIsScoredViaScoreVideo(t *testing.T) {
	in := t.TempDir()
	out := t.TempDir()
	src := filepath.Join(in, "clip.mp4")
	writeMP4(t, src)

	entries := make(chan scanner.Entry, 1)
	entries <- fileEntry(t, src)
	close(entries)

	scorer := &fakeScorer{videoScore: 0.99, videoSamples: 7}
	records := Run(entries, Options{OutDir: out, Workers: 1, Thresholds: DefaultThresholds, Scorer: scorer})

	rec := records[0]
	if scorer.videoScoreCalls != 1 || scorer.imageScoreCalls != 0 {
		t.Errorf("expected ScoreVideo to be called once and ScoreImage never: video=%d image=%d",
			scorer.videoScoreCalls, scorer.imageScoreCalls)
	}
	if rec.MediaType != model.MediaVideo {
		t.Errorf("MediaType = %q, want Video", rec.MediaType)
	}
	if rec.SamplesChecked != 7 {
		t.Errorf("SamplesChecked = %d, want 7", rec.SamplesChecked)
	}
	if rec.Status != model.StatusMoved {
		t.Errorf("status = %q, want Moved", rec.Status)
	}
}

func TestRun_UnsupportedFileIsLeftInPlace(t *testing.T) {
	in := t.TempDir()
	out := t.TempDir()
	src := filepath.Join(in, "notes.txt")
	if err := os.WriteFile(src, []byte("just some text"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	entries := make(chan scanner.Entry, 1)
	entries <- fileEntry(t, src)
	close(entries)

	scorer := &fakeScorer{}
	records := Run(entries, Options{OutDir: out, Workers: 1, Thresholds: DefaultThresholds, Scorer: scorer})

	rec := records[0]
	if rec.Status != model.StatusLeftInPlace {
		t.Errorf("status = %q, want Left In Place", rec.Status)
	}
	if rec.MediaType != model.MediaUnsupported {
		t.Errorf("MediaType = %q, want Unsupported", rec.MediaType)
	}
	if scorer.imageScoreCalls != 0 || scorer.videoScoreCalls != 0 {
		t.Error("scorer should never be called for an unsupported file")
	}
}

func TestRun_DryRunTouchesNothing(t *testing.T) {
	in := t.TempDir()
	out := t.TempDir()
	src := filepath.Join(in, "photo.png")
	writePNG(t, src)

	entries := make(chan scanner.Entry, 1)
	entries <- fileEntry(t, src)
	close(entries)

	scorer := &fakeScorer{imageScore: 0.99}
	records := Run(entries, Options{OutDir: out, Workers: 1, Thresholds: DefaultThresholds, Scorer: scorer, DryRun: true})

	rec := records[0]
	if rec.Status != model.StatusMoved {
		t.Errorf("status = %q, want Moved (dry-run still reports the hypothetical action)", rec.Status)
	}
	if _, err := os.Stat(src); err != nil {
		t.Errorf("source must survive a dry run: %v", err)
	}
	entriesList, _ := os.ReadDir(out)
	if len(entriesList) != 0 {
		t.Errorf("expected out dir to stay empty during dry run, found %v", entriesList)
	}
}

func TestRun_DryRunSuggestiveAndSafeBuckets(t *testing.T) {
	tests := []struct {
		name       string
		score      float64
		wantStatus model.Status
	}{
		{"suggestive", 0.70, model.StatusCopiedForReview},
		{"safe", 0.01, model.StatusLeftInPlace},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := t.TempDir()
			src := filepath.Join(in, "photo.png")
			writePNG(t, src)

			entries := make(chan scanner.Entry, 1)
			entries <- fileEntry(t, src)
			close(entries)

			scorer := &fakeScorer{imageScore: tc.score}
			records := Run(entries, Options{OutDir: t.TempDir(), Workers: 1, Thresholds: DefaultThresholds, Scorer: scorer, DryRun: true})

			if records[0].Status != tc.wantStatus {
				t.Errorf("status = %q, want %q", records[0].Status, tc.wantStatus)
			}
		})
	}
}

// fakeProgressSink records FileDone calls for assertions.
type fakeProgressSink struct {
	calls int
}

func (f *fakeProgressSink) SetTotal(int, int64) {}
func (f *fakeProgressSink) FileDone(int64)      { f.calls++ }

func TestRun_ReportsProgress(t *testing.T) {
	in := t.TempDir()
	src := filepath.Join(in, "photo.png")
	writePNG(t, src)

	entries := make(chan scanner.Entry, 1)
	entries <- fileEntry(t, src)
	close(entries)

	sink := &fakeProgressSink{}
	records := Run(entries, Options{OutDir: t.TempDir(), Workers: 1, Thresholds: DefaultThresholds, Scorer: &fakeScorer{imageScore: 0.01}, Progress: sink})

	if sink.calls != len(records) {
		t.Errorf("progress calls = %d, want %d", sink.calls, len(records))
	}
}

func TestRun_SkippedSymlinkProducesSkippedRecord(t *testing.T) {
	entries := make(chan scanner.Entry, 1)
	entries <- scanner.Entry{Path: "/some/link.png", Kind: scanner.KindSkippedSymlink}
	close(entries)

	records := Run(entries, Options{OutDir: t.TempDir(), Workers: 1, Scorer: &fakeScorer{}})
	if records[0].Status != model.StatusSkippedSymlink {
		t.Errorf("status = %q, want Skipped - Symlink", records[0].Status)
	}
}

func TestRun_ExcludedEntryProducesExcludedRecord(t *testing.T) {
	entries := make(chan scanner.Entry, 1)
	entries <- scanner.Entry{Path: "/some/.git", Kind: scanner.KindExcluded, MatchedPattern: ".git"}
	close(entries)

	records := Run(entries, Options{OutDir: t.TempDir(), Workers: 1, Scorer: &fakeScorer{}})
	if records[0].Status != model.StatusExcluded {
		t.Errorf("status = %q, want Skipped - Excluded", records[0].Status)
	}
	if records[0].Reason != `matched exclude pattern ".git"` {
		t.Errorf("reason = %q", records[0].Reason)
	}
}

func TestRun_ScanErrorEntryProducesFailedRecord(t *testing.T) {
	entries := make(chan scanner.Entry, 1)
	entries <- scanner.Entry{Path: "/no/such/path", Kind: scanner.KindError, Err: errors.New("permission denied")}
	close(entries)

	records := Run(entries, Options{OutDir: t.TempDir(), Workers: 1, Scorer: &fakeScorer{}})
	if records[0].Status != model.StatusFailed {
		t.Errorf("status = %q, want Failed", records[0].Status)
	}
	if records[0].Reason != "permission denied" {
		t.Errorf("reason = %q", records[0].Reason)
	}
}

func TestDetectMediaType_MissingFileFails(t *testing.T) {
	_, err := detectMediaType(filepath.Join(t.TempDir(), "does-not-exist.png"))
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
}

func TestRun_MediaTypeDetectionFailureProducesFailedRecord(t *testing.T) {
	// A real file for Stat (so the entry is valid), removed before
	// process() gets to read its content for sniffing.
	dir := t.TempDir()
	src := filepath.Join(dir, "vanishing.png")
	writePNG(t, src)
	entry := fileEntry(t, src)
	if err := os.Remove(src); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	entries := make(chan scanner.Entry, 1)
	entries <- entry
	close(entries)

	records := Run(entries, Options{OutDir: t.TempDir(), Workers: 1, Scorer: &fakeScorer{}})
	if records[0].Status != model.StatusFailed {
		t.Errorf("status = %q, want Failed", records[0].Status)
	}
}

func TestRun_ImageScoreErrorProducesFailedRecord(t *testing.T) {
	in := t.TempDir()
	src := filepath.Join(in, "photo.png")
	writePNG(t, src)

	entries := make(chan scanner.Entry, 1)
	entries <- fileEntry(t, src)
	close(entries)

	scorer := &fakeScorer{imageErr: errors.New("inference failure")}
	records := Run(entries, Options{OutDir: t.TempDir(), Workers: 1, Thresholds: DefaultThresholds, Scorer: scorer})

	if records[0].Status != model.StatusFailed {
		t.Errorf("status = %q, want Failed", records[0].Status)
	}
}

func TestRun_VideoScoreErrorProducesFailedRecord(t *testing.T) {
	in := t.TempDir()
	src := filepath.Join(in, "clip.mp4")
	writeMP4(t, src)

	entries := make(chan scanner.Entry, 1)
	entries <- fileEntry(t, src)
	close(entries)

	scorer := &fakeScorer{videoErr: errors.New("ffmpeg failure")}
	records := Run(entries, Options{OutDir: t.TempDir(), Workers: 1, Thresholds: DefaultThresholds, Scorer: scorer})

	if records[0].Status != model.StatusFailed {
		t.Errorf("status = %q, want Failed", records[0].Status)
	}
}

func TestRun_ResolveDestinationErrorProducesFailedRecord(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits behave differently on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root ignores directory permissions")
	}

	in := t.TempDir()
	out := t.TempDir()
	src := filepath.Join(in, "photo.png")
	writePNG(t, src)

	bucketDir := filepath.Join(out, string(model.BucketExplicit))
	if err := os.MkdirAll(bucketDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.Chmod(bucketDir, 0o000); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	defer os.Chmod(bucketDir, 0o755)

	entries := make(chan scanner.Entry, 1)
	entries <- fileEntry(t, src)
	close(entries)

	scorer := &fakeScorer{imageScore: 0.99}
	records := Run(entries, Options{OutDir: out, Workers: 1, Thresholds: DefaultThresholds, Scorer: scorer})

	if records[0].Status != model.StatusFailed {
		t.Errorf("status = %q, want Failed (reason: %s)", records[0].Status, records[0].Reason)
	}
}

func TestRun_MoveFailureProducesFailedRecord(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits behave differently on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root ignores directory permissions")
	}

	in := t.TempDir()
	out := t.TempDir()
	src := filepath.Join(in, "photo.png")
	writePNG(t, src)

	if err := os.Chmod(out, 0o555); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	defer os.Chmod(out, 0o755)

	entries := make(chan scanner.Entry, 1)
	entries <- fileEntry(t, src)
	close(entries)

	scorer := &fakeScorer{imageScore: 0.99}
	records := Run(entries, Options{OutDir: out, Workers: 1, Thresholds: DefaultThresholds, Scorer: scorer})

	if records[0].Status != model.StatusFailed {
		t.Errorf("status = %q, want Failed (reason: %s)", records[0].Status, records[0].Reason)
	}
}

func TestRun_NameCollisionIsAutoRenamed(t *testing.T) {
	in := t.TempDir()
	out := t.TempDir()
	src := filepath.Join(in, "photo.png")
	writePNG(t, src)

	existing := filepath.Join(out, string(model.BucketExplicit), "photo.png")
	writePNG(t, existing)

	entries := make(chan scanner.Entry, 1)
	entries <- fileEntry(t, src)
	close(entries)

	scorer := &fakeScorer{imageScore: 0.99}
	records := Run(entries, Options{OutDir: out, Workers: 1, Thresholds: DefaultThresholds, Scorer: scorer})

	rec := records[0]
	if rec.Status != model.StatusMoved {
		t.Fatalf("status = %q, want Moved (reason: %s)", rec.Status, rec.Reason)
	}
	want := filepath.Join(out, string(model.BucketExplicit), "photo_1.png")
	if rec.DestPath != want {
		t.Errorf("DestPath = %q, want %q", rec.DestPath, want)
	}
}

func TestRun_DefaultsToOneWorkerWhenUnset(t *testing.T) {
	in := t.TempDir()
	src := filepath.Join(in, "photo.png")
	writePNG(t, src)

	entries := make(chan scanner.Entry, 1)
	entries <- fileEntry(t, src)
	close(entries)

	records := Run(entries, Options{OutDir: t.TempDir(), Workers: 0, Thresholds: DefaultThresholds, Scorer: &fakeScorer{imageScore: 0.01}})
	if len(records) != 1 {
		t.Fatalf("got %d records, want 1", len(records))
	}
}
