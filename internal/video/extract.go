package video

import (
	"bytes"
	"fmt"
	"os/exec"
	"strconv"
	"time"
)

// ExtractFrame writes a single JPEG frame from videoPath at timestamp at
// to outPath, overwriting it if present.
func ExtractFrame(videoPath string, at time.Duration, outPath string) error {
	cmd := exec.Command(ffmpegPath,
		"-v", "error",
		"-ss", strconv.FormatFloat(at.Seconds(), 'f', 3, 64),
		"-i", videoPath,
		"-frames:v", "1",
		"-q:v", "2",
		"-y", outPath,
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg: %w: %s", err, stderr.String())
	}
	return nil
}
