package video

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
)

// requireFFmpeg skips the test if ffmpeg/ffprobe aren't on PATH, rather
// than failing - they're a documented external dependency (see README),
// not something every contributor's machine is guaranteed to have.
func requireFFmpeg(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not found on PATH")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not found on PATH")
	}
}

// makeTestVideo generates a synthetic test video of the given duration
// using ffmpeg's built-in testsrc filter - fast, deterministic, no fixture
// files to check in.
func makeTestVideo(t *testing.T, seconds float64) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.mp4")
	cmd := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi",
		"-i", "testsrc=duration="+strconv.FormatFloat(seconds, 'f', -1, 64)+":size=320x240:rate=10",
		"-y", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate test video: %v: %s", err, out)
	}
	return path
}

// makeTestVideoAt is like makeTestVideo but writes to a caller-chosen path,
// for testing unicode/special-character filenames.
func makeTestVideoAt(t *testing.T, path string, seconds float64) {
	t.Helper()
	cmd := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi",
		"-i", "testsrc=duration="+strconv.FormatFloat(seconds, 'f', -1, 64)+":size=320x240:rate=10",
		"-y", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate test video: %v: %s", err, out)
	}
}

func mustNotExist(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "does-not-exist.mp4")
}

func touchFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "not-a-video.mp4")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}
