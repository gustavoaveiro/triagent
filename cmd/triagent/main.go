// Command triagent deep-scans an input directory tree, scores every image
// and video it finds for explicit content using a local ONNX model, and
// moves high-confidence matches (and copies uncertain ones for review) into
// an output directory, writing an .xlsx report of what happened to every
// file.
package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"
	"time"

	"golang.org/x/term"

	"github.com/gustavoaveiro/triagent/internal/app"
	"github.com/gustavoaveiro/triagent/internal/classifier"
	"github.com/gustavoaveiro/triagent/internal/model"
	"github.com/gustavoaveiro/triagent/internal/progress"
	"github.com/gustavoaveiro/triagent/internal/triage"
	"github.com/gustavoaveiro/triagent/internal/video"
)

// version is set at build time via -ldflags "-X main.version=...";
// goreleaser injects the release tag when publishing.
var version = "dev"

func main() {
	var (
		in         = flag.String("in", "", "source directory to scan (required unless -gui)")
		out        = flag.String("out", "", "destination directory for moved/copied files (required unless -gui)")
		workers    = flag.Int("workers", runtime.NumCPU(), "number of concurrent processing workers")
		reportPath = flag.String("report", "", "path for the .xlsx report (default: <out>/triage_report_<timestamp>.xlsx)")
		dryRun     = flag.Bool("dry-run", false, "score files without moving/copying anything")
		exclude    = flag.String("exclude", "", "comma-separated glob patterns (matched against file/directory names) to skip, e.g. \"*.tmp,.git\"")
		noDefaults = flag.Bool("no-default-excludes", false, "disable the built-in excludes (.git, node_modules, __pycache__, *.tmp, etc.)")

		modelPath = flag.String("model", app.DefaultAssetPath("model.onnx"), "path to the .onnx nsfw-detector model")
		libPath   = flag.String("onnxruntime-lib", app.DefaultAssetPath(app.DefaultLibraryName()), "path to the onnxruntime shared library")
		useGPU    = flag.Bool("gpu", true, "attempt GPU acceleration first, falling back to CPU automatically if unavailable")

		highThreshold = flag.Float64("high-threshold", triage.DefaultThresholds.High, "score at/above which a file is moved (Explicit)")
		lowThreshold  = flag.Float64("low-threshold", triage.DefaultThresholds.Low, "score below which a file is left alone (Safe); between the two is copied for review (Suggestive)")

		videoInterval   = flag.Duration("video-interval", video.DefaultSampleConfig.Interval, "target spacing between sampled video frames")
		videoMinSamples = flag.Int("video-min-samples", video.DefaultSampleConfig.MinSamples, "minimum frames sampled per video")
		videoMaxSamples = flag.Int("video-max-samples", video.DefaultSampleConfig.MaxSamples, "maximum frames sampled per video (bounds worst-case time on long videos)")

		useGUI  = flag.Bool("gui", false, "launch the graphical interface instead of running from flags")
		showVer = flag.Bool("version", false, "print the version and exit")
	)
	flag.Parse()

	if *showVer {
		fmt.Println("triagent", version)
		return
	}

	if *useGUI {
		runGUI()
		return
	}

	if *in == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "triagent: -in and -out are required (or pass -gui)")
		flag.Usage()
		os.Exit(2)
	}

	defer classifier.Shutdown()

	tracker := progress.NewTracker()
	stopRender, renderDone := startProgressRendering(tracker)

	summary, err := app.Run(app.Config{
		InDir:             *in,
		OutDir:            *out,
		Workers:           *workers,
		DryRun:            *dryRun,
		ReportPath:        *reportPath,
		Progress:          tracker,
		Excludes:          app.ParseExcludePatterns(*exclude),
		NoDefaultExcludes: *noDefaults,
		ModelPath:         *modelPath,
		LibraryPath:       *libPath,
		PreferGPU:         *useGPU,
		Thresholds:        triage.Thresholds{High: *highThreshold, Low: *lowThreshold},
		VideoSampleConfig: video.SampleConfig{
			Interval:   *videoInterval,
			MinSamples: *videoMinSamples,
			MaxSamples: *videoMaxSamples,
		},
	})

	close(stopRender)
	<-renderDone

	if err != nil {
		fmt.Fprintln(os.Stderr, "triagent:", err)
		os.Exit(1)
	}

	fmt.Printf("Processed %d files using %s\n", summary.Total, summary.Provider)
	for _, status := range []model.Status{
		model.StatusLeftInPlace, model.StatusCopiedForReview, model.StatusMoved,
		model.StatusSkippedSymlink, model.StatusExcluded, model.StatusFailed,
	} {
		if n := summary.Counts[status]; n > 0 {
			fmt.Printf("  %-22s %d\n", status, n)
		}
	}
	fmt.Println("Report written to", summary.ReportPath)
}

// startProgressRendering prints a live progress bar to stderr (so it never
// mixes with stdout output) every 250ms, until stop is closed. It's a
// no-op when stderr isn't a terminal, since overwriting a line with \r
// only makes sense interactively. The returned done channel is closed once
// rendering has fully stopped (including the final 100% line).
func startProgressRendering(t *progress.Tracker) (stop chan struct{}, done chan struct{}) {
	stop = make(chan struct{})
	done = make(chan struct{})

	if !term.IsTerminal(int(os.Stderr.Fd())) {
		close(done)
		return stop, done
	}

	go func() {
		defer close(done)
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				fmt.Fprint(os.Stderr, "\r"+progress.RenderBar(t.Snapshot(), 30)+"\n")
				return
			case <-ticker.C:
				fmt.Fprint(os.Stderr, "\r"+progress.RenderBar(t.Snapshot(), 30))
			}
		}
	}()
	return stop, done
}
