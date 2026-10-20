package scanner

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"testing"
)

func drain(t *testing.T, root string, excludes []string) []Entry {
	t.Helper()
	var entries []Entry
	for e := range Walk(root, excludes) {
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries
}

func TestWalk_FindsNestedRegularFiles(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "top.txt"), "top")
	mustMkdir(t, filepath.Join(root, "sub", "deeper"))
	mustWriteFile(t, filepath.Join(root, "sub", "deeper", "nested.txt"), "nested")

	entries := drain(t, root, nil)

	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2: %+v", len(entries), entries)
	}
	for _, e := range entries {
		if e.Kind != KindFile {
			t.Errorf("entry %q kind = %v, want KindFile", e.Path, e.Kind)
		}
		if e.Info == nil {
			t.Errorf("entry %q has nil Info", e.Path)
		}
	}
}

func TestWalk_SkipsSymlinksWithoutFollowing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevated privileges on windows")
	}

	root := t.TempDir()
	target := filepath.Join(root, "real.txt")
	mustWriteFile(t, target, "real content")

	link := filepath.Join(root, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	// A symlink to a directory must not be recursed into.
	linkedDir := filepath.Join(root, "dirlink")
	realDir := filepath.Join(root, "realdir")
	mustMkdir(t, realDir)
	mustWriteFile(t, filepath.Join(realDir, "inside.txt"), "inside")
	if err := os.Symlink(realDir, linkedDir); err != nil {
		t.Fatalf("Symlink dir: %v", err)
	}

	entries := drain(t, root, nil)

	var files, symlinks int
	for _, e := range entries {
		switch e.Kind {
		case KindFile:
			files++
		case KindSkippedSymlink:
			symlinks++
		}
	}

	// real.txt, inside.txt (via realdir) = 2 files; link.txt, dirlink = 2 symlinks.
	if files != 2 {
		t.Errorf("files = %d, want 2", files)
	}
	if symlinks != 2 {
		t.Errorf("symlinks = %d, want 2", symlinks)
	}
}

func TestCount_FilesAndSymlinkCountSeparately(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevated privileges on windows")
	}

	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "a.txt"), "12345")      // 5 bytes
	mustWriteFile(t, filepath.Join(root, "b.txt"), "1234567890") // 10 bytes
	if err := os.Symlink(filepath.Join(root, "a.txt"), filepath.Join(root, "link.txt")); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	files, totalBytes := Count(root, nil)

	if files != 3 { // 2 regular files + 1 symlink entry
		t.Errorf("files = %d, want 3", files)
	}
	if totalBytes != 15 { // symlink doesn't contribute its own size
		t.Errorf("totalBytes = %d, want 15", totalBytes)
	}
}

func TestCount_ExcludedFilesCountedButNotSized(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "keep.txt"), "12345") // 5 bytes
	mustWriteFile(t, filepath.Join(root, "noisy.tmp"), "1234567890123456789012345")

	files, totalBytes := Count(root, []string{"*.tmp"})

	if files != 2 { // the excluded file is still an entry
		t.Errorf("files = %d, want 2", files)
	}
	if totalBytes != 5 { // but its size isn't counted as work to do
		t.Errorf("totalBytes = %d, want 5", totalBytes)
	}
}

func TestCount_EmptyDirectory(t *testing.T) {
	files, totalBytes := Count(t.TempDir(), nil)
	if files != 0 || totalBytes != 0 {
		t.Errorf("Count() = (%d, %d), want (0, 0)", files, totalBytes)
	}
}

func TestWalk_ExcludedDirectoryIsPrunedEntirely(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "keep.txt"), "keep")
	mustMkdir(t, filepath.Join(root, "node_modules", "pkg"))
	mustWriteFile(t, filepath.Join(root, "node_modules", "pkg", "index.js"), "noise")

	entries := drain(t, root, []string{"node_modules"})

	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1 (node_modules pruned): %+v", len(entries), entries)
	}
	if filepath.Base(entries[0].Path) != "keep.txt" {
		t.Errorf("entry = %q, want keep.txt", entries[0].Path)
	}
}

func TestWalk_ExcludedFileReportsMatchedPattern(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "keep.txt"), "keep")
	mustWriteFile(t, filepath.Join(root, "chat.crypt14"), "encrypted blob")

	entries := drain(t, root, []string{"*.crypt14"})

	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2: %+v", len(entries), entries)
	}
	for _, e := range entries {
		if filepath.Base(e.Path) == "chat.crypt14" {
			if e.Kind != KindExcluded {
				t.Errorf("chat.crypt14 kind = %v, want KindExcluded", e.Kind)
			}
			if e.MatchedPattern != "*.crypt14" {
				t.Errorf("MatchedPattern = %q, want %q", e.MatchedPattern, "*.crypt14")
			}
		} else if e.Kind != KindFile {
			t.Errorf("keep.txt kind = %v, want KindFile", e.Kind)
		}
	}
}

func TestWalk_NoExcludesMatchesEverything(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "a.txt"), "a")
	mustWriteFile(t, filepath.Join(root, "b.txt"), "b")

	entries := drain(t, root, []string{"*.doesnotmatch"})
	if len(entries) != 2 {
		t.Errorf("got %d entries, want 2 (no pattern matched)", len(entries))
	}
}

func TestValidatePatterns(t *testing.T) {
	if err := ValidatePatterns([]string{"*.txt", "node_modules"}); err != nil {
		t.Errorf("ValidatePatterns with valid patterns: %v", err)
	}
	if err := ValidatePatterns(nil); err != nil {
		t.Errorf("ValidatePatterns(nil): %v", err)
	}
	if err := ValidatePatterns([]string{"["}); err == nil {
		t.Error("ValidatePatterns with a malformed pattern: expected an error, got nil")
	}
}

func TestMatchingPattern(t *testing.T) {
	if got := matchingPattern("notes.txt", []string{"*.pdf", "*.txt"}); got != "*.txt" {
		t.Errorf("matchingPattern = %q, want %q", got, "*.txt")
	}
	if got := matchingPattern("notes.txt", []string{"*.pdf"}); got != "" {
		t.Errorf("matchingPattern = %q, want empty", got)
	}
	if got := matchingPattern("notes.txt", nil); got != "" {
		t.Errorf("matchingPattern = %q, want empty", got)
	}
	// A malformed pattern is treated as a non-match rather than panicking;
	// ValidatePatterns is what's responsible for rejecting these upfront.
	if got := matchingPattern("notes.txt", []string{"["}); got != "" {
		t.Errorf("matchingPattern with a malformed pattern = %q, want empty", got)
	}
}

func TestWalk_FindsUnicodeAndEmojiFilenames(t *testing.T) {
	root := t.TempDir()
	names := []string{
		"日本語のファイル名.png",
		"emoji😀📸.jpg",
		"café_naïve.png",
	}
	for _, name := range names {
		mustWriteFile(t, filepath.Join(root, name), "content")
	}

	entries := drain(t, root, nil)
	if len(entries) != len(names) {
		t.Fatalf("got %d entries, want %d: %+v", len(entries), len(names), entries)
	}
	found := map[string]bool{}
	for _, e := range entries {
		found[filepath.Base(e.Path)] = true
	}
	for _, name := range names {
		if !found[name] {
			t.Errorf("expected to find %q among walked entries", name)
		}
	}
}

func TestWalk_FindsVeryLongFilename(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has its own (different, often shorter) path length rules")
	}
	root := t.TempDir()
	name := strings.Repeat("a", 251) + ".png" // 255 bytes: the ext4 limit
	mustWriteFile(t, filepath.Join(root, name), "content")

	entries := drain(t, root, nil)
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	if filepath.Base(entries[0].Path) != name {
		t.Errorf("entry name = %q, want %q", filepath.Base(entries[0].Path), name)
	}
}

func TestWalk_EmptyDirectoryProducesNoEntries(t *testing.T) {
	root := t.TempDir()
	entries := drain(t, root, nil)
	if len(entries) != 0 {
		t.Errorf("got %d entries, want 0: %+v", len(entries), entries)
	}
}

func TestWalk_UnreadableDirectoryProducesErrorEntry(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits behave differently on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root ignores directory permissions")
	}

	root := t.TempDir()
	blocked := filepath.Join(root, "blocked")
	mustMkdir(t, blocked)
	mustWriteFile(t, filepath.Join(blocked, "secret.txt"), "secret")

	if err := os.Chmod(blocked, 0o000); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	defer os.Chmod(blocked, 0o755)

	entries := drain(t, root, nil)

	var sawError bool
	for _, e := range entries {
		if e.Kind == KindError {
			sawError = true
		}
	}
	if !sawError {
		t.Errorf("expected at least one KindError entry, got %+v", entries)
	}
}

func TestWalk_SkipsNonRegularFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("FIFOs are not supported on windows")
	}

	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "regular.txt"), "regular")

	fifo := filepath.Join(root, "pipe")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatalf("Mkfifo: %v", err)
	}

	entries := drain(t, root, nil)

	var files int
	for _, e := range entries {
		if e.Kind == KindFile {
			files++
		}
	}
	if files != 1 {
		t.Errorf("files = %d, want 1 (fifo must be skipped): %+v", files, entries)
	}
}

// infoFailFS wraps a real fs.FS and makes DirEntry.Info() fail for one
// specific entry name. The io/fs.DirEntry contract allows Info() to fail
// (e.g. for fs.FS implementations backed by something other than a local
// disk), but the real OS-backed os.DirFS never actually triggers it - so
// this fake is how walkFS's defensive handling for that case gets exercised.
type infoFailFS struct {
	fs.FS
	failName string
}

func (f infoFailFS) ReadDir(name string) ([]fs.DirEntry, error) {
	entries, err := fs.ReadDir(f.FS, name)
	if err != nil {
		return nil, err
	}
	out := make([]fs.DirEntry, len(entries))
	for i, d := range entries {
		if d.Name() == f.failName {
			out[i] = infoFailDirEntry{d}
		} else {
			out[i] = d
		}
	}
	return out, nil
}

type infoFailDirEntry struct {
	fs.DirEntry
}

func (infoFailDirEntry) Info() (fs.FileInfo, error) {
	return nil, errors.New("simulated Info() failure")
}

func TestWalkFS_DirEntryInfoErrorProducesErrorEntry(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "bad.txt"), "content")

	fsys := infoFailFS{FS: os.DirFS(root), failName: "bad.txt"}

	out := make(chan Entry)
	go func() {
		defer close(out)
		walkFS(fsys, root, out, nil)
	}()

	var entries []Entry
	for e := range out {
		entries = append(entries, e)
	}

	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1: %+v", len(entries), entries)
	}
	if entries[0].Kind != KindError {
		t.Errorf("kind = %v, want KindError", entries[0].Kind)
	}
	if entries[0].Err == nil {
		t.Error("expected a non-nil Err")
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", path, err)
	}
}
