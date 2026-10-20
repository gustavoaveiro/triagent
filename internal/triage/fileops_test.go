package triage

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestResolveDestination_FreshFile(t *testing.T) {
	dir := t.TempDir()
	path, renamed, err := resolveDestination(dir, "notes.png")
	if err != nil {
		t.Fatalf("resolveDestination: %v", err)
	}
	if renamed {
		t.Error("renamed = true, want false for a fresh name")
	}
	if path != filepath.Join(dir, "notes.png") {
		t.Errorf("path = %q", path)
	}
}

func TestResolveDestination_CollisionRenames(t *testing.T) {
	dir := t.TempDir()
	writePNG(t, filepath.Join(dir, "notes.png"))

	path, renamed, err := resolveDestination(dir, "notes.png")
	if err != nil {
		t.Fatalf("resolveDestination: %v", err)
	}
	if !renamed {
		t.Error("renamed = false, want true")
	}
	if path != filepath.Join(dir, "notes_1.png") {
		t.Errorf("path = %q, want notes_1.png", path)
	}
}

func TestResolveDestination_DotfileCollisionPreservesName(t *testing.T) {
	dir := t.TempDir()
	writePNG(t, filepath.Join(dir, ".hidden"))

	path, renamed, err := resolveDestination(dir, ".hidden")
	if err != nil {
		t.Fatalf("resolveDestination: %v", err)
	}
	if !renamed {
		t.Error("renamed = false, want true")
	}
	want := filepath.Join(dir, ".hidden_1")
	if path != want {
		t.Errorf("path = %q, want %q (not _1.hidden)", path, want)
	}
}

func TestResolveDestination_DotfileFreshName(t *testing.T) {
	dir := t.TempDir()
	path, renamed, err := resolveDestination(dir, ".hidden")
	if err != nil {
		t.Fatalf("resolveDestination: %v", err)
	}
	if renamed {
		t.Error("renamed = true, want false for a fresh name")
	}
	if path != filepath.Join(dir, ".hidden") {
		t.Errorf("path = %q", path)
	}
}

func TestResolveDestination_StatErrorPropagates(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits behave differently on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root ignores directory permissions")
	}

	dir := t.TempDir()
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	defer os.Chmod(dir, 0o755)

	_, _, err := resolveDestination(dir, "notes.png")
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
}

func TestResolveDestination_ExhaustsCollisionAttempts(t *testing.T) {
	original := maxCollisionAttempts
	maxCollisionAttempts = 2
	defer func() { maxCollisionAttempts = original }()

	dir := t.TempDir()
	writePNG(t, filepath.Join(dir, "notes.png"))
	writePNG(t, filepath.Join(dir, "notes_1.png"))
	writePNG(t, filepath.Join(dir, "notes_2.png"))

	_, _, err := resolveDestination(dir, "notes.png")
	if err == nil {
		t.Fatal("expected an exhausted-collisions error, got nil")
	}
}

func TestCopyFile_MkdirAllFailsWhenAncestorIsAFile(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	writePNG(t, blocker)

	err := copyFile(blocker, filepath.Join(blocker, "sub", "dst.png"))
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
}

func TestCopyFile_MissingSourceFails(t *testing.T) {
	err := copyFile(filepath.Join(t.TempDir(), "nope.png"), filepath.Join(t.TempDir(), "dst.png"))
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
}

func TestCopyFile_CreateDestFailsWhenDirNotWritable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits behave differently on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root ignores directory permissions")
	}

	dir := t.TempDir()
	src := filepath.Join(dir, "src.png")
	writePNG(t, src)

	readonly := filepath.Join(dir, "readonly")
	if err := os.MkdirAll(readonly, 0o555); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	defer os.Chmod(readonly, 0o755)

	err := copyFile(src, filepath.Join(readonly, "dst.png"))
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
}

func TestCopyFile_ReadingFromADirectoryFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("opening a directory as a file behaves differently on windows")
	}

	dir := t.TempDir()
	srcDir := filepath.Join(dir, "imasrcdir")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	err := copyFile(srcDir, filepath.Join(dir, "dst.png"))
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
}

func TestMoveFile_Success(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.png")
	writePNG(t, src)
	dst := filepath.Join(dir, "dst.png")

	if err := moveFile(src, dst); err != nil {
		t.Fatalf("moveFile: %v", err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Error("source should no longer exist")
	}
	if _, err := os.Stat(dst); err != nil {
		t.Errorf("destination missing: %v", err)
	}
}

func TestMoveFile_CopyFailurePropagates(t *testing.T) {
	err := moveFile(filepath.Join(t.TempDir(), "nope.png"), filepath.Join(t.TempDir(), "dst.png"))
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
}

func TestMoveFile_RemoveFailurePropagates(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits behave differently on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root ignores directory permissions")
	}

	parent := t.TempDir()
	srcDir := filepath.Join(parent, "srcdir")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	src := filepath.Join(srcDir, "src.png")
	writePNG(t, src)
	dst := filepath.Join(parent, "dst.png")

	// Removing the file requires write permission on its parent directory.
	if err := os.Chmod(srcDir, 0o555); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	defer os.Chmod(srcDir, 0o755)

	err := moveFile(src, dst)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
}
