package video

import (
	"math"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDuration_RealVideo(t *testing.T) {
	requireFFmpeg(t)
	path := makeTestVideo(t, 3)

	got, err := Duration(path)
	if err != nil {
		t.Fatalf("Duration: %v", err)
	}
	if math.Abs(got.Seconds()-3) > 0.2 {
		t.Errorf("Duration = %v, want ~3s", got)
	}
}

func TestDuration_UnicodeAndSpecialCharacterPaths(t *testing.T) {
	requireFFmpeg(t)

	names := []string{
		"日本語の動画.mp4",
		"emoji😀clip.mp4",
		`video (2024) [final] - "copy" & more.mp4`,
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), name)
			makeTestVideoAt(t, path, 2)

			got, err := Duration(path)
			if err != nil {
				t.Fatalf("Duration: %v", err)
			}
			if math.Abs(got.Seconds()-2) > 0.2 {
				t.Errorf("Duration = %v, want ~2s", got)
			}
		})
	}
}

func TestDuration_MissingFileFails(t *testing.T) {
	requireFFmpeg(t)
	_, err := Duration(mustNotExist(t))
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
}

func TestDuration_NotAVideoFails(t *testing.T) {
	requireFFmpeg(t)
	_, err := Duration(touchFile(t, "not a real video file"))
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
}

func TestDuration_FfprobeNotFound(t *testing.T) {
	original := ffprobePath
	ffprobePath = "this-binary-does-not-exist-xyz"
	defer func() { ffprobePath = original }()

	_, err := Duration("irrelevant.mp4")
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
}

func TestDuration_NonNumericDurationFieldFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("relies on a shebang script as a stand-in binary")
	}
	// A fake "ffprobe" that ignores its arguments and always prints valid
	// JSON shaped like ffprobe's real output, but with a non-numeric
	// duration - exercises the ParseFloat failure branch specifically, as
	// opposed to the JSON-syntax-error branch covered by
	// TestDuration_MalformedJSONFails (echo's args wouldn't be valid JSON
	// at all).
	script := filepath.Join(t.TempDir(), "fake-ffprobe.sh")
	content := "#!/bin/sh\necho '{\"format\":{\"duration\":\"not-a-number\"}}'\n"
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	original := ffprobePath
	ffprobePath = script
	defer func() { ffprobePath = original }()

	_, err := Duration("irrelevant.mp4")
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
}

func TestDuration_MalformedJSONFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("relies on /bin/echo as a stand-in binary")
	}
	// Point ffprobePath at something that runs successfully but doesn't
	// produce ffprobe-shaped JSON, to exercise the parse-failure branch
	// without needing a crafted video file.
	original := ffprobePath
	ffprobePath = "echo"
	defer func() { ffprobePath = original }()

	_, err := Duration("irrelevant.mp4")
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
}
