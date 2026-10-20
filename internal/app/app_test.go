package app

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
	"sync"
	"testing"

	"github.com/xuri/excelize/v2"

	"github.com/gustavoaveiro/triagent/internal/model"
)

// fakeScorer scripts ScoreImage/ScoreVideo results by source file name
// (matched by suffix), so app-level orchestration tests don't need a real
// ONNX model.
type fakeScorer struct {
	byName map[string]float64
}

func (f *fakeScorer) ScoreImage(path string) (float64, error) {
	if s, ok := f.byName[filepath.Base(path)]; ok {
		return s, nil
	}
	return 0, nil
}

func (f *fakeScorer) ScoreVideo(path string) (float64, int, error) {
	if s, ok := f.byName[filepath.Base(path)]; ok {
		return s, 1, nil
	}
	return 0, 0, nil
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

func TestRun_EndToEndWithInjectedScorer(t *testing.T) {
	in := t.TempDir()
	out := t.TempDir()
	writePNG(t, filepath.Join(in, "explicit.png"))
	writePNG(t, filepath.Join(in, "safe.png"))
	writePNG(t, filepath.Join(in, "suggestive.png"))

	scorer := &fakeScorer{byName: map[string]float64{
		"explicit.png":   0.99,
		"safe.png":       0.01,
		"suggestive.png": 0.70,
	}}

	summary, err := Run(Config{InDir: in, OutDir: out, Scorer: scorer})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if summary.Total != 3 {
		t.Errorf("Total = %d, want 3", summary.Total)
	}
	if summary.Counts[model.StatusMoved] != 1 {
		t.Errorf("Moved count = %d, want 1", summary.Counts[model.StatusMoved])
	}
	if summary.Counts[model.StatusCopiedForReview] != 1 {
		t.Errorf("CopiedForReview count = %d, want 1", summary.Counts[model.StatusCopiedForReview])
	}
	if summary.Counts[model.StatusLeftInPlace] != 1 {
		t.Errorf("LeftInPlace count = %d, want 1", summary.Counts[model.StatusLeftInPlace])
	}
	if _, err := os.Stat(summary.ReportPath); err != nil {
		t.Errorf("report missing: %v", err)
	}

	f, err := excelize.OpenFile(summary.ReportPath)
	if err != nil {
		t.Fatalf("OpenFile(%q): %v", summary.ReportPath, err)
	}
	defer f.Close()
}

func TestRun_MissingInOrOutIsRejected(t *testing.T) {
	if _, err := Run(Config{InDir: "", OutDir: "x", Scorer: &fakeScorer{}}); err == nil {
		t.Error("expected an error when InDir is empty")
	}
	if _, err := Run(Config{InDir: "x", OutDir: "", Scorer: &fakeScorer{}}); err == nil {
		t.Error("expected an error when OutDir is empty")
	}
}

func TestRun_NonExistentInDirIsRejected(t *testing.T) {
	_, err := Run(Config{InDir: filepath.Join(t.TempDir(), "nope"), OutDir: t.TempDir(), Scorer: &fakeScorer{}})
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
}

func TestRun_InDirThatIsAFileIsRejected(t *testing.T) {
	in := t.TempDir()
	filePath := filepath.Join(in, "notadir.png")
	writePNG(t, filePath)

	_, err := Run(Config{InDir: filePath, OutDir: t.TempDir(), Scorer: &fakeScorer{}})
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
}

func TestRun_CannotCreateOutDir(t *testing.T) {
	in := t.TempDir()
	writePNG(t, filepath.Join(in, "photo.png"))

	parent := t.TempDir()
	blocker := filepath.Join(parent, "blocker")
	writePNG(t, blocker)

	_, err := Run(Config{InDir: in, OutDir: filepath.Join(blocker, "sub"), Scorer: &fakeScorer{}})
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
}

func TestRun_InvalidExcludePatternIsRejected(t *testing.T) {
	in := t.TempDir()
	writePNG(t, filepath.Join(in, "photo.png"))

	_, err := Run(Config{InDir: in, OutDir: t.TempDir(), Scorer: &fakeScorer{}, Excludes: []string{"["}})
	if err == nil {
		t.Fatal("expected an error for a malformed exclude pattern, got nil")
	}
}

func TestRun_DefaultExcludesPruneNoiseDirectories(t *testing.T) {
	in := t.TempDir()
	out := t.TempDir()
	writePNG(t, filepath.Join(in, "photo.png"))
	writePNG(t, filepath.Join(in, "node_modules", "pkg", "icon.png"))

	summary, err := Run(Config{InDir: in, OutDir: out, Scorer: &fakeScorer{}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if summary.Total != 1 {
		t.Errorf("Total = %d, want 1 (node_modules pruned by default)", summary.Total)
	}
}

// fakeProgressSink records calls for assertions. Concurrency-safe since
// triage's workers call FileDone from multiple goroutines.
type fakeProgressSink struct {
	mu         sync.Mutex
	totalFiles int
	fileDones  int
}

func (f *fakeProgressSink) SetTotal(files int, bytes int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.totalFiles = files
}

func (f *fakeProgressSink) FileDone(int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fileDones++
}

func TestRun_ReportsProgressWhenSinkProvided(t *testing.T) {
	in := t.TempDir()
	writePNG(t, filepath.Join(in, "a.png"))
	writePNG(t, filepath.Join(in, "b.png"))

	sink := &fakeProgressSink{}
	summary, err := Run(Config{InDir: in, OutDir: t.TempDir(), Scorer: &fakeScorer{}, Progress: sink})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if sink.totalFiles != 2 {
		t.Errorf("totalFiles = %d, want 2", sink.totalFiles)
	}
	if sink.fileDones != summary.Total {
		t.Errorf("fileDones = %d, want %d", sink.fileDones, summary.Total)
	}
}

func TestRun_ReportWriteFailure(t *testing.T) {
	in := t.TempDir()
	writePNG(t, filepath.Join(in, "photo.png"))

	badReportPath := t.TempDir() // a directory can't be overwritten by SaveAs
	_, err := Run(Config{InDir: in, OutDir: t.TempDir(), Scorer: &fakeScorer{}, ReportPath: badReportPath})
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
}

func TestRun_ClassifierLoadFailureWhenScorerNotInjected(t *testing.T) {
	in := t.TempDir()
	writePNG(t, filepath.Join(in, "photo.png"))

	_, err := Run(Config{InDir: in, OutDir: t.TempDir(), ModelPath: "/does/not/exist.onnx", LibraryPath: "/does/not/exist.so"})
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
}

func TestRun_DefaultWorkerCount(t *testing.T) {
	in := t.TempDir()
	writePNG(t, filepath.Join(in, "photo.png"))

	summary, err := Run(Config{InDir: in, OutDir: t.TempDir(), Scorer: &fakeScorer{}, Workers: 0})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if summary.Total != 1 {
		t.Errorf("Total = %d, want 1", summary.Total)
	}
}

// TestRun_RealONNXIntegration proves the full wiring (scanner -> real ONNX
// classifier -> video sampling -> triage -> report) works together, not
// just each piece against a fake. Skipped unless TRIAGENT_TEST_MODEL and
// TRIAGENT_TEST_LIB point at a real model/onnxruntime library.
func TestRun_RealONNXIntegration(t *testing.T) {
	modelPath := os.Getenv("TRIAGENT_TEST_MODEL")
	libPath := os.Getenv("TRIAGENT_TEST_LIB")
	if modelPath == "" || libPath == "" {
		t.Skip("set TRIAGENT_TEST_MODEL and TRIAGENT_TEST_LIB to run this integration test")
	}

	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not found on PATH")
	}

	in := t.TempDir()
	writePNG(t, filepath.Join(in, "photo.png"))
	videoPath := filepath.Join(in, "clip.mp4")
	cmd := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi",
		"-i", "testsrc=duration=1:size=64x64:rate=5", "-y", videoPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate test video: %v: %s", err, out)
	}

	summary, err := Run(Config{
		InDir: in, OutDir: t.TempDir(),
		ModelPath: modelPath, LibraryPath: libPath,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if summary.Total != 2 {
		t.Errorf("Total = %d, want 2", summary.Total)
	}
	if summary.Provider != "cpu" {
		t.Errorf("Provider = %q, want cpu", summary.Provider)
	}
}

// TestRun_RealONNXIntegration_VideoScoreError exercises onnxMediaScorer's
// error-propagation branch for ScoreVideo with the real classifier/ffmpeg
// stack: a file that sniffs as video/mp4 (valid ftyp box) but is too
// truncated for ffprobe to read a duration from.
func TestRun_RealONNXIntegration_VideoScoreError(t *testing.T) {
	modelPath := os.Getenv("TRIAGENT_TEST_MODEL")
	libPath := os.Getenv("TRIAGENT_TEST_LIB")
	if modelPath == "" || libPath == "" {
		t.Skip("set TRIAGENT_TEST_MODEL and TRIAGENT_TEST_LIB to run this integration test")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not found on PATH")
	}

	in := t.TempDir()
	truncatedMP4 := []byte{
		0x00, 0x00, 0x00, 0x18, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm',
		0, 0, 2, 0, 'i', 's', 'o', 'm', 'i', 's', 'o', '2', 'a', 'v', 'c', '1', 'm', 'p', '4', '1',
	}
	if err := os.WriteFile(filepath.Join(in, "broken.mp4"), truncatedMP4, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	summary, err := Run(Config{InDir: in, OutDir: t.TempDir(), ModelPath: modelPath, LibraryPath: libPath})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if summary.Counts[model.StatusFailed] != 1 {
		t.Errorf("Failed count = %d, want 1", summary.Counts[model.StatusFailed])
	}
}

func TestDefaultAssetPath_NextToExecutable(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Skip("os.Executable unavailable in this environment")
	}
	want := filepath.Join(filepath.Dir(exe), "model.onnx")
	if got := DefaultAssetPath("model.onnx"); got != want {
		t.Errorf("DefaultAssetPath(%q) = %q, want %q", "model.onnx", got, want)
	}
}

func TestDefaultAssetPath_FallsBackWhenExecutableLookupFails(t *testing.T) {
	original := osExecutable
	osExecutable = func() (string, error) { return "", errors.New("simulated lookup failure") }
	defer func() { osExecutable = original }()

	if got := DefaultAssetPath("model.onnx"); got != "model.onnx" {
		t.Errorf("DefaultAssetPath(%q) = %q, want the bare name back", "model.onnx", got)
	}
}

func TestDefaultLibraryName_DelegatesToCurrentGOOS(t *testing.T) {
	if DefaultLibraryName() != libraryNameForGOOS(runtime.GOOS) {
		t.Errorf("DefaultLibraryName() = %q, want %q", DefaultLibraryName(), libraryNameForGOOS(runtime.GOOS))
	}
}

func TestLibraryNameForGOOS(t *testing.T) {
	tests := []struct {
		goos string
		want string
	}{
		{"darwin", "libonnxruntime.dylib"},
		{"windows", "onnxruntime.dll"},
		{"linux", "libonnxruntime.so"},
		{"freebsd", "libonnxruntime.so"}, // anything else defaults to the .so name
	}
	for _, tc := range tests {
		if got := libraryNameForGOOS(tc.goos); got != tc.want {
			t.Errorf("libraryNameForGOOS(%q) = %q, want %q", tc.goos, got, tc.want)
		}
	}
}

func TestParseExcludePatterns(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"*.tmp", []string{"*.tmp"}},
		{"*.tmp,.git", []string{"*.tmp", ".git"}},
		{" *.tmp , .git ,", []string{"*.tmp", ".git"}},
	}
	for _, tc := range tests {
		got := ParseExcludePatterns(tc.in)
		if len(got) != len(tc.want) {
			t.Errorf("ParseExcludePatterns(%q) = %v, want %v", tc.in, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("ParseExcludePatterns(%q) = %v, want %v", tc.in, got, tc.want)
			}
		}
	}
}
