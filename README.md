# Triagent

[![test](https://github.com/gustavoaveiro/triagent/actions/workflows/test.yml/badge.svg)](https://github.com/gustavoaveiro/triagent/actions/workflows/test.yml)
[![release](https://img.shields.io/github/v/release/gustavoaveiro/triagent)](https://github.com/gustavoaveiro/triagent/releases)
[![license](https://img.shields.io/github/license/gustavoaveiro/triagent)](LICENSE)

**Triagent** scans a folder of images and videos, scores each one locally
for explicit content using an on-device ML model, and triages them: files
it's highly confident about get moved out for you; borderline ones get
copied to a review folder instead of being touched destructively; clearly
safe files are left exactly where they are. Every file gets a row in an
`.xlsx` report with its score and what happened to it.

**Scope note:** this is a personal-media triage tool for adult (18+)
nudity/explicit content in a library you already have legal rights to. It
is **not** a CSAM detection tool - that's a different, specialized problem
(hash-matching against law-enforcement databases like NCMEC/PhotoDNA, with
mandatory reporting obligations this tool doesn't implement). If a scan
ever turns up suspected child sexual abuse material, report it to the
authorities (e.g. NCMEC's CyberTipline), don't run a sorting tool on it.

## How it decides

Every image (and every sampled video frame) gets a single score in `[0,1]`
from the model: the probability it's explicit content.

| Score | Bucket | What happens |
|---|---|---|
| `>= -high-threshold` (default 0.97) | **Explicit** | Moved to `<out>/Explicit/` - removed from the source |
| between the two thresholds | **Suggestive** | Copied to `<out>/Suggestive/` for you to review - source untouched |
| `< -low-threshold` (default 0.40) | **Safe** | Left exactly where it is - never touched |

The move threshold defaults conservatively high on purpose: a wrong
high-confidence call would otherwise relocate an innocent file out of its
original folder. Everything in between gets surfaced for a human decision
instead of an automatic one.

Videos are never fully decoded frame-by-frame (too slow). Instead, a
configurable number of frames are sampled across the *entire* duration -
full-duration coverage, bounded worst-case time on long videos - and
scored like images; the video's score is the **maximum** across sampled
frames, since one explicit moment should flag the whole clip rather than
being diluted by averaging against mostly-safe frames.

## Installation

Download a release archive for your platform from the
[Releases page](https://github.com/gustavoaveiro/triagent/releases).
Each archive already contains everything needed - the `triagent` binary,
the ONNX model, and the matching onnxruntime library - just unzip and run.

- **Linux/Windows**: x86-64.
- **macOS**: Apple Silicon (arm64) only, for now.

Or build from source (needs a C compiler for cgo - see
[Building from source](#building-from-source)):

```sh
git clone https://github.com/gustavoaveiro/triagent.git
cd triagent
go build -o triagent ./cmd/triagent
```

A build from source still needs the model and onnxruntime library
available at runtime - see `-model`/`-onnxruntime-lib` below.

## Usage

```sh
triagent -in ~/phone-backup/Pictures -out ~/triaged
```

```
Usage of triagent:
  -dry-run
        score files without moving/copying anything
  -exclude string
        comma-separated glob patterns (matched against file/directory names) to skip, e.g. "*.tmp,.git"
  -gpu
        attempt GPU acceleration first, falling back to CPU automatically if unavailable (default true)
  -gui
        launch the graphical interface instead of running from flags
  -high-threshold float
        score at/above which a file is moved (Explicit) (default 0.97)
  -in string
        source directory to scan (required unless -gui)
  -low-threshold float
        score below which a file is left alone (Safe); between the two is copied for review (Suggestive) (default 0.4)
  -model string
        path to the .onnx nsfw-detector model (default: next to the binary)
  -no-default-excludes
        disable the built-in excludes (.git, node_modules, __pycache__, *.tmp, etc.)
  -onnxruntime-lib string
        path to the onnxruntime shared library (default: next to the binary)
  -out string
        destination directory for moved/copied files (required unless -gui)
  -report string
        path for the .xlsx report (default: <out>/triage_report_<timestamp>.xlsx)
  -version
        print the version and exit
  -video-interval duration
        target spacing between sampled video frames (default 2s)
  -video-max-samples int
        maximum frames sampled per video (default 120)
  -video-min-samples int
        minimum frames sampled per video (default 5)
  -workers int
        number of concurrent processing workers
```

### Examples

Preview what *would* happen without moving or copying anything:

```sh
triagent -in ~/library -out ~/triaged -dry-run
```

Be stricter about what counts as a confident match (only move at 0.99+,
widen the review range):

```sh
triagent -in ~/library -out ~/triaged -high-threshold 0.99 -low-threshold 0.25
```

Force CPU-only (skip the automatic GPU attempt):

```sh
triagent -in ~/library -out ~/triaged -gpu=false
```

## Excluding files

Same mechanism as general-purpose file tools: a built-in default list
(`.git`, `node_modules`, `__pycache__`, `*.tmp`, `.DS_Store`, etc.) is
skipped unless `-no-default-excludes` is set, and `-exclude` adds more
glob patterns (matched against file/directory base names) on top.

## The report

Every run produces an `.xlsx` file with one row per file encountered:

| Column | Meaning |
|---|---|
| Source Path | Where the file was found |
| Destination Path | Where it was moved/copied to (blank if untouched) |
| File Name | Original file name |
| Media Type | `Image`, `Video`, or `Unsupported` |
| Bucket | `Safe`, `Suggestive`, or `Explicit` |
| Score | The model's explicit-content probability, 0-1 |
| Samples Checked | Frames sampled, for videos only |
| Status | `Left In Place`, `Copied - For Review`, `Moved`, `Skipped - Symlink`, `Skipped - Excluded`, or `Failed` |
| Reason | Why, with the exact threshold comparison that produced the decision |
| Size (bytes) | File size |
| Timestamp | When it was processed |

`Failed` rows are highlighted red; `Moved` rows are highlighted amber,
since those are the ones that actually changed your source tree.

## GPU acceleration

`-gpu` (on by default) attempts a platform-appropriate execution provider
before falling back to CPU automatically:

- **Linux/Windows**: CUDA (needs an NVIDIA GPU with the CUDA runtime
  already installed - not bundled, it's too large and license-restricted
  to redistribute).
- **macOS**: CoreML (uses Apple's Neural Engine/GPU, built into macOS,
  nothing extra to install).

This repo's CI only has CPU-only runners, so the CPU path and the
fallback-on-failure logic are fully tested, but the actual GPU-accelerated
paths aren't verified by automated tests - if you have a GPU, please
report back whether `-gpu` actually uses it (the CLI prints which
provider ended up active) or falls back silently.

## Building from source

Requires a C compiler (cgo, for the ONNX Runtime bindings) - `gcc`/`clang`
on Linux/macOS, MSVC or MinGW on Windows. No special system packages
needed otherwise (unlike a GUI toolkit, ONNX Runtime loads its shared
library dynamically at a path you specify, so nothing needs installing
system-wide).

```sh
go build -o triagent ./cmd/triagent
```

You'll also need the model and onnxruntime library at runtime - either
grab them from a release archive, or fetch them yourself:

```sh
# the model (Apache-2.0, see Credits)
curl -LO https://huggingface.co/AdamCodd/vit-base-nsfw-detector/resolve/main/onnx/model_quantized.onnx

# onnxruntime (MIT) - pick the asset matching your OS/arch from
# https://github.com/microsoft/onnxruntime/releases
```

Point `-model`/`-onnxruntime-lib` at them, or place them next to the
binary (the default search location).

### Building the GUI

```sh
go build -tags gui -o triagent ./cmd/triagent
./triagent -gui
```

The GUI additionally needs [Fyne](https://fyne.io)'s system dependencies
(OpenGL + X11/Wayland dev headers on Linux), e.g. on Debian/Ubuntu:

```sh
sudo apt install libgl1-mesa-dev libxkbcommon-dev libwayland-dev wayland-protocols \
  libxrandr-dev libxinerama-dev libxcursor-dev libxi-dev libxxf86vm-dev
```

## Development

```sh
go test ./internal/...   # run the test suite (needs ffmpeg on PATH)
go build ./cmd/triagent  # build the CLI
gofmt -l .               # check formatting
```

Two env vars are needed to run the classifier/app integration tests for
real rather than skipping them: `TRIAGENT_TEST_MODEL` and
`TRIAGENT_TEST_LIB`, pointing at a real `.onnx` model and onnxruntime
shared library (see [Building from source](#building-from-source) for
where to get them).

Coverage is 100% everywhere except `internal/classifier`'s GPU
execution-provider selection, which can only be exercised on matching
GPU hardware - CI enforces 100% on every other package and a floor on
that one, rather than papering over the gap with a lower blanket number.

See [CHANGELOG.md](CHANGELOG.md) for release history.

## Credits

- [AdamCodd/vit-base-nsfw-detector](https://huggingface.co/AdamCodd/vit-base-nsfw-detector)
  (Apache-2.0) - the classification model.
- [microsoft/onnxruntime](https://github.com/microsoft/onnxruntime) (MIT) -
  inference engine.
- [yalue/onnxruntime_go](https://github.com/yalue/onnxruntime_go) (MIT) -
  Go bindings.

## License

[MIT](LICENSE) for this project's code. The bundled model and onnxruntime
library carry their own licenses (both permissive - Apache-2.0 and MIT
respectively), included in each release archive.
