// Package atomicfile writes complete files without exposing partially generated output.
package atomicfile

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
)

// Write creates a temporary file next to path, lets write populate it, durably closes it, and
// atomically replaces path. Existing permission bits are preserved; mode is used for a new file.
func Write(path string, mode fs.FileMode, write func(*os.File) error) (resultErr error) {
	if path == "" {
		return fmt.Errorf("atomic output path is empty")
	}
	if write == nil {
		return fmt.Errorf("atomic output writer is nil")
	}

	effectiveMode, err := outputMode(path, mode)
	if err != nil {
		return err
	}
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary output for %q: %w", path, err)
	}
	temporaryPath := temporary.Name()
	closed := false
	renamed := false
	defer func() {
		if !closed {
			resultErr = errors.Join(resultErr, temporary.Close())
		}
		if !renamed {
			removeErr := os.Remove(temporaryPath)
			if removeErr != nil && !errors.Is(removeErr, fs.ErrNotExist) {
				resultErr = errors.Join(resultErr, fmt.Errorf("remove temporary output %q: %w", temporaryPath, removeErr))
			}
		}
	}()

	if err := write(temporary); err != nil {
		return fmt.Errorf("write temporary output for %q: %w", path, err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync temporary output for %q: %w", path, err)
	}
	if err := temporary.Close(); err != nil {
		closed = true
		return fmt.Errorf("close temporary output for %q: %w", path, err)
	}
	closed = true
	if err := os.Chmod(temporaryPath, effectiveMode); err != nil {
		return fmt.Errorf("set temporary output mode for %q: %w", path, err)
	}
	if err := syncFile(temporaryPath); err != nil {
		return fmt.Errorf("sync temporary output metadata for %q: %w", path, err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace output %q: %w", path, err)
	}
	renamed = true
	if err := syncDirectory(directory); err != nil {
		return fmt.Errorf("sync output directory %q: %w", directory, err)
	}
	return nil
}

// WriteFile atomically replaces path with data.
func WriteFile(path string, data []byte, mode fs.FileMode) error {
	return Write(path, mode, func(file *os.File) error {
		if _, err := file.Write(data); err != nil {
			return fmt.Errorf("write bytes: %w", err)
		}
		return nil
	})
}

func outputMode(path string, fallback fs.FileMode) (fs.FileMode, error) {
	info, err := os.Stat(path)
	switch {
	case err == nil:
		if !info.Mode().IsRegular() {
			return 0, fmt.Errorf("atomic output %q is not a regular file", path)
		}
		return info.Mode().Perm(), nil
	case errors.Is(err, fs.ErrNotExist):
		return fallback.Perm(), nil
	default:
		return 0, fmt.Errorf("inspect output mode for %q: %w", path, err)
	}
}

func syncFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(file.Sync(), file.Close())
}

func syncDirectory(path string) error {
	// Windows does not expose a portable directory fsync. The file itself was
	// synced before rename; keeping the old destination on rename failure is
	// safer than emulating replacement with a non-atomic remove-and-rename.
	if runtime.GOOS == "windows" {
		return nil
	}
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(directory.Sync(), directory.Close())
}
