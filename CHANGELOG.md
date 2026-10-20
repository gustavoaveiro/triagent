# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Initial implementation of Triagent: a CLI (and Fyne-based GUI, built
  with `-tags gui`) that deep-scans a source directory, scores every
  image and video it finds for explicit content using a local ONNX
  model (no network calls), and triages each file into Safe (left in
  place), Suggestive (copied to a review folder), or Explicit (moved
  out of the source tree) based on configurable thresholds.
- Content-based media detection (not extension-based) via magic-byte
  sniffing, consistent across images and videos.
- Video scoring via frame sampling: a random timestamp per evenly-sized
  segment spanning the full duration (not just the first portion of a
  long video), bounded between a minimum and maximum sample count;
  aggregated as the maximum score across sampled frames.
- GPU acceleration attempted automatically (CUDA on Linux/Windows,
  CoreML on macOS) with transparent fallback to CPU, reported in both
  the CLI output and the GUI.
- File/directory exclusion via `-exclude` and `-no-default-excludes`,
  with a built-in default list for common VCS/build noise.
- Live progress reporting with ETA, shared with the CLI/GUI from a
  metadata-only pre-scan.
- One `.xlsx` report per run with one row per file: media type, bucket,
  score, samples checked (for video), status, and the exact
  threshold comparison behind each decision. Failed rows highlighted
  red, Moved rows highlighted amber.
- GitHub Actions CI: `gofmt`, `go vet`, the full test suite with the
  race detector and real ONNX/ffmpeg integration tests (not mocked),
  and a per-package coverage gate (100% everywhere except
  `internal/classifier`'s GPU-provider branches, which need matching
  hardware to exercise).
- Release automation: pushing a `v*` tag builds platform-native
  binaries (linux/amd64, darwin/arm64, windows/amd64 - cgo prevents
  cross-compiling these like a pure-Go tool) bundled with the ONNX
  model and the matching onnxruntime library in each archive, published
  as a GitHub Release with checksums.
- MIT license (model and onnxruntime library carry their own permissive
  licenses - see README Credits).

[Unreleased]: https://github.com/gustavoaveiro/triagent/commits/master
