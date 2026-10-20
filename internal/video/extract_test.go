package video

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestExtractFrame_RealVideo(t *testing.T) {
	requireFFmpeg(t)
	video := makeTestVideo(t, 3)
	out := filepath.Join(t.TempDir(), "frame.jpg")

	if err := ExtractFrame(video, 1500*time.Millisecond, out); err != nil {
		t.Fatalf("ExtractFrame: %v", err)
	}

	info, err := os.Stat(out)
	if err != nil {
		t.Fatalf("output frame missing: %v", err)
	}
	if info.Size() == 0 {
		t.Error("output frame is empty")
	}
}

func TestExtractFrame_UnicodeAndSpecialCharacterPaths(t *testing.T) {
	requireFFmpeg(t)

	dir := t.TempDir()
	videoPath := filepath.Join(dir, `日本語 (2024) [emoji😀] & more.mp4`)
	makeTestVideoAt(t, videoPath, 2)

	outPath := filepath.Join(dir, `frame output "weird".jpg`)
	if err := ExtractFrame(videoPath, 500*time.Millisecond, outPath); err != nil {
		t.Fatalf("ExtractFrame: %v", err)
	}

	info, err := os.Stat(outPath)
	if err != nil {
		t.Fatalf("output frame missing: %v", err)
	}
	if info.Size() == 0 {
		t.Error("output frame is empty")
	}
}

func TestExtractFrame_MissingSourceFails(t *testing.T) {
	requireFFmpeg(t)
	out := filepath.Join(t.TempDir(), "frame.jpg")
	err := ExtractFrame(mustNotExist(t), time.Second, out)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
}

func TestExtractFrame_FfmpegNotFound(t *testing.T) {
	original := ffmpegPath
	ffmpegPath = "this-binary-does-not-exist-xyz"
	defer func() { ffmpegPath = original }()

	err := ExtractFrame("irrelevant.mp4", time.Second, filepath.Join(t.TempDir(), "out.jpg"))
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
}
