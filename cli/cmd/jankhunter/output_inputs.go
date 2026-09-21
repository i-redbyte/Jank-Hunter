package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
)

// Check the filesystem identity, including hardlinks, final symlinks and symlinked directories.
// Atomic rename preserves a symlink's target but replaces the input alias itself; reject both.
// A new destination requires one stat and no input stats. Existing destinations cost O(inputs).
func rejectOutputInputOverlap(output string, inputs []string) error {
	if output == "" {
		return nil
	}
	target, err := os.Stat(output)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect output %q: %w", output, err)
	}
	for _, input := range inputs {
		if input == "" {
			continue
		}
		source, err := os.Stat(input)
		// Optional artifact sidecars can be absent; required inputs are validated by their readers.
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("inspect input %q before output: %w", input, err)
		}
		if os.SameFile(target, source) {
			return fmt.Errorf("output %q overlaps input %q; choose a separate output file", output, input)
		}
	}
	return nil
}
