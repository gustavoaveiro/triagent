package classifier

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// These tests exercise the real ONNX Runtime session against a real model
// file, since the whole point of this package is that wiring - unlike
// score.go/preprocess.go's pure-Go logic (covered above without any
// external dependency), there's no meaningful way to fake "cgo loads a
// native library and runs inference" without just... doing it. They're
// skipped unless TRIAGENT_TEST_MODEL and TRIAGENT_TEST_LIB point at a real
// vit-base-nsfw-detector ONNX export and onnxruntime shared library
// (CI provides both; see internal/classifier/README or the CI workflow).
func testArtifacts(t *testing.T) (modelPath, libPath string) {
	t.Helper()
	modelPath = os.Getenv("TRIAGENT_TEST_MODEL")
	libPath = os.Getenv("TRIAGENT_TEST_LIB")
	if modelPath == "" || libPath == "" {
		t.Skip("set TRIAGENT_TEST_MODEL and TRIAGENT_TEST_LIB to run classifier integration tests")
	}
	return modelPath, libPath
}

func writeSolidPNG(t *testing.T, path string, c color.RGBA) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			img.Set(x, y, c)
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

func TestNew_LoadsModelAndScoresAnImage(t *testing.T) {
	modelPath, libPath := testArtifacts(t)

	c, err := New(Options{ModelPath: modelPath, LibraryPath: libPath})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer c.Close()
	defer Shutdown()

	if c.Provider() != "cpu" {
		t.Errorf("Provider() = %q, want cpu (PreferGPU not set)", c.Provider())
	}

	dir := t.TempDir()
	imgPath := filepath.Join(dir, "solid.png")
	writeSolidPNG(t, imgPath, color.RGBA{120, 170, 200, 255}) // a plain color swatch

	score, err := c.Score(imgPath)
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if score < 0 || score > 1 {
		t.Errorf("score = %v, want a probability in [0,1]", score)
	}
}

func TestNew_MissingModelFileFails(t *testing.T) {
	_, libPath := testArtifacts(t)

	_, err := New(Options{ModelPath: "/does/not/exist.onnx", LibraryPath: libPath})
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
}

func TestScore_MissingImageFileFails(t *testing.T) {
	modelPath, libPath := testArtifacts(t)

	c, err := New(Options{ModelPath: modelPath, LibraryPath: libPath})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer c.Close()
	defer Shutdown()

	_, err = c.Score(filepath.Join(t.TempDir(), "nope.png"))
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
}

func TestScoreReader_InvalidImageDataFails(t *testing.T) {
	modelPath, libPath := testArtifacts(t)

	c, err := New(Options{ModelPath: modelPath, LibraryPath: libPath})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer c.Close()
	defer Shutdown()

	_, err = c.ScoreReader(bytes.NewReader([]byte("not an image")))
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
}

func TestPreferGPU_FallsBackToCPUWithoutGPU(t *testing.T) {
	modelPath, libPath := testArtifacts(t)

	// On a machine without a usable GPU provider (this dev/CI environment),
	// PreferGPU must gracefully fall back to CPU rather than failing New.
	c, err := New(Options{ModelPath: modelPath, LibraryPath: libPath, PreferGPU: true})
	if err != nil {
		t.Fatalf("New with PreferGPU: %v", err)
	}
	defer c.Close()
	defer Shutdown()

	if c.Provider() == "" {
		t.Error("Provider() is empty")
	}
}
