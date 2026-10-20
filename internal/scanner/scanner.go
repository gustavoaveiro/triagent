// Package scanner walks a source directory tree and streams back every
// regular file it finds. Symbolic links are never followed - they are
// reported back as skipped entries instead.
package scanner

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Kind identifies what a scanner Entry represents.
type Kind int

const (
	// KindFile is a regular, readable file ready to be classified/copied.
	KindFile Kind = iota
	// KindSkippedSymlink is a symbolic link that was not followed.
	KindSkippedSymlink
	// KindExcluded is a file that matched an exclude pattern and was
	// never read.
	KindExcluded
	// KindError is a path the walk could not stat/read (e.g. permission denied).
	KindError
)

// DefaultExcludes are glob patterns (matched against a file or directory's
// base name, not its full path) skipped unless the caller opts out. They
// cover version-control metadata and language/tool build caches that are
// reproducible, not meaningful to back up, and often make up the bulk of a
// project tree's file count.
var DefaultExcludes = []string{
	".git", ".svn", ".hg",
	"node_modules",
	"__pycache__", ".pytest_cache", ".mypy_cache", ".tox",
	".DS_Store", "Thumbs.db", "desktop.ini",
	"*.tmp", "*.temp", "*.part", "*.crdownload",
}

// Entry is one item produced while walking the source tree.
type Entry struct {
	Path string
	Info fs.FileInfo // nil for KindError
	Kind Kind
	// MatchedPattern is set for KindExcluded, naming the pattern that
	// matched.
	MatchedPattern string
	Err            error // set for KindError
}

// ValidatePatterns reports an error if any of patterns is not a valid
// glob pattern, so a typo is caught before a long scan starts rather than
// silently matching nothing.
func ValidatePatterns(patterns []string) error {
	for _, p := range patterns {
		if _, err := filepath.Match(p, "probe"); err != nil {
			return fmt.Errorf("invalid exclude pattern %q: %w", p, err)
		}
	}
	return nil
}

// Walk recursively walks root and sends one Entry per file, symlink,
// excluded match or unreadable path found. A directory whose name matches
// one of excludes is pruned entirely (never descended into); a file whose
// name matches is reported as KindExcluded instead of KindFile. The
// returned channel is closed once the walk is complete.
func Walk(root string, excludes []string) <-chan Entry {
	out := make(chan Entry)

	go func() {
		defer close(out)
		walkFS(os.DirFS(root), root, out, excludes)
	}()

	return out
}

// Count walks root and returns the number of entries found and the total
// size of regular, non-excluded files among them. It's meant for a fast,
// metadata-only pre-scan so callers (e.g. a progress reporter) know the
// total amount of work before the real pass over the same tree begins.
func Count(root string, excludes []string) (files int, totalBytes int64) {
	for e := range Walk(root, excludes) {
		files++
		if e.Kind == KindFile {
			totalBytes += e.Info.Size()
		}
	}
	return files, totalBytes
}

func matchingPattern(name string, patterns []string) string {
	for _, p := range patterns {
		if ok, _ := filepath.Match(p, name); ok {
			return p
		}
	}
	return ""
}

// walkFS is the injectable core of Walk: fsys supplies directory entries,
// while root is only used to reconstruct absolute paths for Entry.Path.
// Separating it from Walk lets tests exercise fs.DirEntry failure modes
// that can't happen with the real OS filesystem but are part of the
// io/fs.DirEntry contract.
func walkFS(fsys fs.FS, root string, out chan<- Entry, excludes []string) {
	_ = fs.WalkDir(fsys, ".", func(relPath string, d fs.DirEntry, err error) error {
		path := filepath.Join(root, relPath)

		if err != nil {
			out <- Entry{Path: path, Kind: KindError, Err: err}
			return nil
		}

		if d.IsDir() {
			if matchingPattern(d.Name(), excludes) != "" {
				return fs.SkipDir
			}
			return nil
		}

		info, infoErr := d.Info()
		if infoErr != nil {
			out <- Entry{Path: path, Kind: KindError, Err: infoErr}
			return nil
		}

		if pattern := matchingPattern(d.Name(), excludes); pattern != "" {
			out <- Entry{Path: path, Info: info, Kind: KindExcluded, MatchedPattern: pattern}
			return nil
		}

		if info.Mode()&fs.ModeSymlink != 0 {
			out <- Entry{Path: path, Info: info, Kind: KindSkippedSymlink}
			return nil
		}

		if !info.Mode().IsRegular() {
			return nil
		}

		out <- Entry{Path: path, Info: info, Kind: KindFile}
		return nil
	})
}
