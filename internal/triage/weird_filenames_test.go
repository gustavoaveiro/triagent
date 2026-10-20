package triage

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/gustavoaveiro/triagent/internal/model"
	"github.com/gustavoaveiro/triagent/internal/scanner"
)

// Stress tests for the kind of real-world filenames that break naive file
// tools: unicode/emoji, names near the filesystem's length limit, special
// characters, and permission-denied source files. Inspired by
// offline-nsfw-scanner's README noting it needed a dedicated retry/cache
// system for exactly these cases.

func TestRun_UnicodeAndEmojiFilenames(t *testing.T) {
	names := []string{
		"café_photo.png",
		"日本語のファイル名.png",
		"emoji😀📸🎉.png",
		"файл.png", // Cyrillic
		"naïve_résumé.png",
	}

	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			in := t.TempDir()
			out := t.TempDir()
			src := filepath.Join(in, name)
			writePNG(t, src)

			entries := make(chan scanner.Entry, 1)
			entries <- fileEntry(t, src)
			close(entries)

			records := Run(entries, Options{OutDir: out, Workers: 1, Thresholds: DefaultThresholds, Scorer: &fakeScorer{imageScore: 0.99}})
			if len(records) != 1 {
				t.Fatalf("got %d records, want 1", len(records))
			}
			rec := records[0]
			if rec.Status != model.StatusMoved {
				t.Fatalf("status = %q, want Moved (reason: %s)", rec.Status, rec.Reason)
			}
			if rec.FileName != name {
				t.Errorf("FileName = %q, want %q", rec.FileName, name)
			}
			if _, err := os.Stat(rec.DestPath); err != nil {
				t.Errorf("destination missing: %v", err)
			}
			if !strings.HasSuffix(rec.DestPath, name) {
				t.Errorf("DestPath = %q, want it to end with %q", rec.DestPath, name)
			}
		})
	}
}

func TestRun_FilenameWithSpacesAndSpecialCharacters(t *testing.T) {
	// Parens, brackets, quotes, ampersands, dollar signs, semicolons -
	// the kind of characters that break naive shell-concatenation
	// approaches. Our code never builds a shell string from a filename
	// (exec.Command always takes args as a slice), so this should just
	// work, but it's worth proving rather than assuming.
	name := `My Photo (2024) [final] - "copy" & more; v2.png`

	in := t.TempDir()
	out := t.TempDir()
	src := filepath.Join(in, name)
	writePNG(t, src)

	entries := make(chan scanner.Entry, 1)
	entries <- fileEntry(t, src)
	close(entries)

	records := Run(entries, Options{OutDir: out, Workers: 1, Thresholds: DefaultThresholds, Scorer: &fakeScorer{imageScore: 0.99}})
	rec := records[0]
	if rec.Status != model.StatusMoved {
		t.Fatalf("status = %q, want Moved (reason: %s)", rec.Status, rec.Reason)
	}
	if _, err := os.Stat(rec.DestPath); err != nil {
		t.Errorf("destination missing: %v", err)
	}
}

func TestRun_VeryLongFilenameNearFilesystemLimit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has its own (different, often shorter) path length rules")
	}

	// Most Linux filesystems cap a single path component at 255 bytes.
	// Build a name right at that limit (".png" = 4 bytes).
	longBase := strings.Repeat("a", 251)
	name := longBase + ".png"
	if len(name) != 255 {
		t.Fatalf("test setup: name is %d bytes, want 255", len(name))
	}

	in := t.TempDir()
	out := t.TempDir()
	src := filepath.Join(in, name)
	writePNG(t, src)

	entries := make(chan scanner.Entry, 1)
	entries <- fileEntry(t, src)
	close(entries)

	records := Run(entries, Options{OutDir: out, Workers: 1, Thresholds: DefaultThresholds, Scorer: &fakeScorer{imageScore: 0.99}})
	rec := records[0]
	if rec.Status != model.StatusMoved {
		t.Fatalf("status = %q, want Moved (reason: %s)", rec.Status, rec.Reason)
	}
	if _, err := os.Stat(rec.DestPath); err != nil {
		t.Errorf("destination missing: %v", err)
	}
}

func TestRun_CollisionRenameOverflowingFilesystemLimitFailsGracefully(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has its own (different, often shorter) path length rules")
	}

	// A 255-byte name that's already at the limit: appending "_1" during
	// a collision rename would overflow it. This must fail cleanly (a
	// Failed record with a reason) rather than panic.
	longBase := strings.Repeat("a", 251)
	name := longBase + ".png"

	in := t.TempDir()
	out := t.TempDir()
	src := filepath.Join(in, name)
	writePNG(t, src)

	// Pre-create a colliding file of the same name at the destination so
	// a rename is attempted.
	existing := filepath.Join(out, string(model.BucketExplicit), name)
	writePNG(t, existing)

	entries := make(chan scanner.Entry, 1)
	entries <- fileEntry(t, src)
	close(entries)

	records := Run(entries, Options{OutDir: out, Workers: 1, Thresholds: DefaultThresholds, Scorer: &fakeScorer{imageScore: 0.99}})
	rec := records[0]
	if rec.Status != model.StatusFailed {
		t.Errorf("status = %q, want Failed (an overflowing rename must fail cleanly, not panic)", rec.Status)
	}
	if rec.Reason == "" {
		t.Error("expected a non-empty failure reason")
	}
}

func TestRun_UnreadableSourceFileProducesFailedRecord(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits behave differently on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root ignores file permissions")
	}

	in := t.TempDir()
	src := filepath.Join(in, "secret.png")
	writePNG(t, src)
	if err := os.Chmod(src, 0o000); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	defer os.Chmod(src, 0o644)

	entries := make(chan scanner.Entry, 1)
	entries <- fileEntry(t, src)
	close(entries)

	records := Run(entries, Options{OutDir: t.TempDir(), Workers: 1, Thresholds: DefaultThresholds, Scorer: &fakeScorer{imageScore: 0.99}})
	rec := records[0]
	if rec.Status != model.StatusFailed {
		t.Errorf("status = %q, want Failed", rec.Status)
	}
}

func TestRun_FilenameThatIsOnlyADotfileWithNoExtension(t *testing.T) {
	// e.g. ".gitignore"-shaped names landing in the media pipeline
	// (classified Unsupported since it's plain text, but should still be
	// walked/reported correctly rather than confusing path handling).
	in := t.TempDir()
	src := filepath.Join(in, ".config")
	if err := os.WriteFile(src, []byte("not an image"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	entries := make(chan scanner.Entry, 1)
	entries <- fileEntry(t, src)
	close(entries)

	records := Run(entries, Options{OutDir: t.TempDir(), Workers: 1, Thresholds: DefaultThresholds, Scorer: &fakeScorer{}})
	rec := records[0]
	if rec.Status != model.StatusLeftInPlace {
		t.Errorf("status = %q, want Left In Place", rec.Status)
	}
	if rec.FileName != ".config" {
		t.Errorf("FileName = %q, want %q", rec.FileName, ".config")
	}
}
