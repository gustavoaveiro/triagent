// Package video samples frames from a video file and scores each as an
// image, aggregating to a single video-level explicit-content score. It
// shells out to ffmpeg/ffprobe rather than linking a video decoding
// library, since that's the one dependency that reliably handles the
// breadth of real-world containers/codecs users will actually have.
package video

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"time"
)

// ffprobePath and ffmpegPath are package variables so tests can point them
// at anything on PATH (or fail fast with a clear message if neither tool
// is installed), rather than assuming a specific install location.
var (
	ffprobePath = "ffprobe"
	ffmpegPath  = "ffmpeg"
)

type ffprobeFormat struct {
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
}

// Duration returns a video file's duration.
func Duration(path string) (time.Duration, error) {
	cmd := exec.Command(ffprobePath, "-v", "quiet", "-print_format", "json", "-show_format", path)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return 0, fmt.Errorf("ffprobe: %w: %s", err, stderr.String())
	}

	var parsed ffprobeFormat
	if err := json.Unmarshal(stdout.Bytes(), &parsed); err != nil {
		return 0, fmt.Errorf("parse ffprobe output: %w", err)
	}

	seconds, err := strconv.ParseFloat(parsed.Format.Duration, 64)
	if err != nil {
		return 0, fmt.Errorf("parse duration %q: %w", parsed.Format.Duration, err)
	}
	return time.Duration(seconds * float64(time.Second)), nil
}
