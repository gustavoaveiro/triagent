// Package app wires the scanner, classifier/video scoring and report
// packages into the single Run entry point shared by the CLI and the GUI.
package app

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/gustavoaveiro/triagent/internal/classifier"
	"github.com/gustavoaveiro/triagent/internal/model"
	"github.com/gustavoaveiro/triagent/internal/progress"
	"github.com/gustavoaveiro/triagent/internal/report"
	"github.com/gustavoaveiro/triagent/internal/scanner"
	"github.com/gustavoaveiro/triagent/internal/triage"
	"github.com/gustavoaveiro/triagent/internal/video"
)

// Config describes one triage run.
type Config struct {
	InDir  string
	OutDir string
	// Workers is the number of concurrent processing workers. Defaults to
	// 4 if < 1. Since classification is inference-bound and effectively
	// serialized by the classifier, more workers mainly help overlap
	// frame extraction/file IO rather than run inference in parallel.
	Workers int
	// DryRun scores files without moving/copying anything.
	DryRun bool
	// ReportPath is where the .xlsx report is written. If empty, a
	// timestamped file is created inside OutDir.
	ReportPath string
	// Progress, if non-nil, receives a SetTotal call after a metadata-only
	// pre-scan of InDir, then a FileDone call per entry as work proceeds.
	Progress progress.Sink
	// Excludes are additional glob patterns (matched against a file or
	// directory's base name) to skip, on top of scanner.DefaultExcludes
	// unless NoDefaultExcludes is set.
	Excludes []string
	// NoDefaultExcludes disables scanner.DefaultExcludes, using only
	// Excludes.
	NoDefaultExcludes bool

	// ModelPath is the .onnx nsfw-detector model file. Required unless
	// Scorer is set directly.
	ModelPath string
	// LibraryPath is the onnxruntime shared library for the current
	// platform. Required unless Scorer is set directly.
	LibraryPath string
	// PreferGPU attempts a platform-appropriate GPU execution provider
	// before falling back to CPU.
	PreferGPU bool
	// Thresholds configures the score cutoffs between buckets. Defaults
	// to triage.DefaultThresholds if zero-valued.
	Thresholds triage.Thresholds
	// VideoSampleConfig configures how densely videos are sampled.
	// Defaults to video.DefaultSampleConfig if zero-valued.
	VideoSampleConfig video.SampleConfig

	// Scorer, if set, is used directly instead of loading a real ONNX
	// model from ModelPath/LibraryPath - lets tests (and potentially
	// alternative scoring backends) bypass ONNX entirely.
	Scorer triage.MediaScorer
}

// DefaultAssetPath looks for name next to the running executable, which is
// where release archives bundle the model and onnxruntime library. Falls
// back to the bare name (resolved relative to the working directory) if
// the executable's own path can't be determined.
func DefaultAssetPath(name string) string {
	exe, err := osExecutable()
	if err != nil {
		return name
	}
	return filepath.Join(filepath.Dir(exe), name)
}

// osExecutable is a var (not a direct os.Executable call) so tests can
// simulate the lookup failing - genuinely possible on some restricted
// systems, but not something that happens in a normal test run.
var osExecutable = os.Executable

// DefaultLibraryName is the onnxruntime shared library's conventional file
// name on the current platform.
func DefaultLibraryName() string {
	return libraryNameForGOOS(runtime.GOOS)
}

// libraryNameForGOOS is the pure, parameterized half of DefaultLibraryName,
// split out so all three platform branches are directly unit-testable
// regardless of which OS the tests actually run on.
func libraryNameForGOOS(goos string) string {
	switch goos {
	case "darwin":
		return "libonnxruntime.dylib"
	case "windows":
		return "onnxruntime.dll"
	default:
		return "libonnxruntime.so"
	}
}

// Summary is returned after a run completes.
type Summary struct {
	ReportPath string
	Total      int
	Counts     map[model.Status]int
	// Provider is which execution provider the classifier used ("cpu",
	// "cuda", "coreml", "directml"), empty if Scorer was injected directly.
	Provider string
}

// ParseExcludePatterns splits a comma-separated list of glob patterns
// (as taken from a CLI flag or GUI text field) into a slice, trimming
// whitespace and dropping empty entries.
func ParseExcludePatterns(s string) []string {
	var patterns []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			patterns = append(patterns, p)
		}
	}
	return patterns
}

// Run scans cfg.InDir, scores and triages every image/video into
// cfg.OutDir, and writes the .xlsx report, returning a Summary.
func Run(cfg Config) (Summary, error) {
	if cfg.InDir == "" || cfg.OutDir == "" {
		return Summary{}, fmt.Errorf("both input and output directories are required")
	}
	info, err := os.Stat(cfg.InDir)
	if err != nil {
		return Summary{}, fmt.Errorf("input directory: %w", err)
	}
	if !info.IsDir() {
		return Summary{}, fmt.Errorf("input path %q is not a directory", cfg.InDir)
	}

	if err := os.MkdirAll(cfg.OutDir, 0o755); err != nil {
		return Summary{}, fmt.Errorf("create output directory: %w", err)
	}

	workers := cfg.Workers
	if workers < 1 {
		workers = 4
	}

	excludes := cfg.Excludes
	if !cfg.NoDefaultExcludes {
		excludes = append(append([]string{}, scanner.DefaultExcludes...), excludes...)
	}
	if err := scanner.ValidatePatterns(excludes); err != nil {
		return Summary{}, err
	}

	scorer := cfg.Scorer
	provider := ""
	if scorer == nil {
		cls, err := classifier.New(classifier.Options{
			ModelPath:   cfg.ModelPath,
			LibraryPath: cfg.LibraryPath,
			PreferGPU:   cfg.PreferGPU,
		})
		if err != nil {
			return Summary{}, fmt.Errorf("load classifier: %w", err)
		}
		defer cls.Close()
		provider = cls.Provider()

		videoCfg := cfg.VideoSampleConfig
		if videoCfg == (video.SampleConfig{}) {
			videoCfg = video.DefaultSampleConfig
		}
		scorer = &onnxMediaScorer{
			classifier: cls,
			sampler:    video.NewSampler(time.Now().UnixNano()),
			cfg:        videoCfg,
		}
	}

	thresholds := cfg.Thresholds
	if thresholds == (triage.Thresholds{}) {
		thresholds = triage.DefaultThresholds
	}

	if cfg.Progress != nil {
		files, bytes := scanner.Count(cfg.InDir, excludes)
		cfg.Progress.SetTotal(files, bytes)
	}

	entries := scanner.Walk(cfg.InDir, excludes)
	records := triage.Run(entries, triage.Options{
		OutDir:     cfg.OutDir,
		Workers:    workers,
		Thresholds: thresholds,
		Scorer:     scorer,
		DryRun:     cfg.DryRun,
		Progress:   cfg.Progress,
	})

	reportPath := cfg.ReportPath
	if reportPath == "" {
		reportPath = filepath.Join(cfg.OutDir, fmt.Sprintf("triage_report_%s.xlsx", time.Now().Format("20060102_150405")))
	}
	if err := report.Write(reportPath, records); err != nil {
		return Summary{}, fmt.Errorf("write report: %w", err)
	}

	summary := Summary{ReportPath: reportPath, Total: len(records), Counts: map[model.Status]int{}, Provider: provider}
	for _, r := range records {
		summary.Counts[r.Status]++
	}
	return summary, nil
}

// onnxMediaScorer adapts a *classifier.Classifier (+ video sampling) to
// triage.MediaScorer.
type onnxMediaScorer struct {
	classifier *classifier.Classifier
	sampler    *video.Sampler
	cfg        video.SampleConfig
}

func (s *onnxMediaScorer) ScoreImage(path string) (float64, error) {
	return s.classifier.Score(path)
}

func (s *onnxMediaScorer) ScoreVideo(path string) (float64, int, error) {
	result, err := video.ScoreVideo(path, s.classifier, s.sampler, s.cfg)
	if err != nil {
		return 0, 0, err
	}
	return result.MaxScore, result.SamplesChecked, nil
}
