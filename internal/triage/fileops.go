package triage

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// maxCollisionAttempts is a var (not const) so tests can shrink it to
// exercise the exhaustion path without creating thousands of files.
var maxCollisionAttempts = 10000

// resolveDestination finds a free path for fileName under dir, appending
// "_N" before the extension on a name collision.
func resolveDestination(dir, fileName string) (path string, renamed bool, err error) {
	ext := filepath.Ext(fileName)
	base := strings.TrimSuffix(fileName, ext)

	for n := 0; n <= maxCollisionAttempts; n++ {
		candidateName := fileName
		if n > 0 {
			candidateName = fmt.Sprintf("%s_%d%s", base, n, ext)
		}
		candidate := filepath.Join(dir, candidateName)

		_, statErr := os.Stat(candidate)
		switch {
		case os.IsNotExist(statErr):
			return candidate, n > 0, nil
		case statErr != nil:
			return "", false, statErr
		}
	}
	return "", false, fmt.Errorf("could not find a free destination name after %d attempts", maxCollisionAttempts)
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("create destination: %w", err)
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("write destination: %w", err)
	}
	return out.Close()
}

// moveFile copies src to dst then removes src, rather than using
// os.Rename, since -in and -out are typically different drives for this
// tool and os.Rename fails with "invalid cross-device link" in that case.
func moveFile(src, dst string) error {
	if err := copyFile(src, dst); err != nil {
		return err
	}
	if err := os.Remove(src); err != nil {
		return fmt.Errorf("remove original after move: %w", err)
	}
	return nil
}
