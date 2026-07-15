package atomicfile

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteAtomicallyReplacesFileAndPreservesMode(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "report.html")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := Write(path, 0o644, func(file *os.File) error {
		if filepath.Dir(file.Name()) != directory {
			t.Fatalf("temporary file directory = %q, want %q", filepath.Dir(file.Name()), directory)
		}
		if _, err := file.WriteString("new"); err != nil {
			return err
		}
		current, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if string(current) != "old" {
			t.Fatalf("destination changed before rename: %q", current)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "new" {
		t.Fatalf("content = %q, want new", content)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("mode = %o, want preserved 600", got)
	}
	assertNoTemporaryOutputs(t, directory)
}

func TestWriteFailureKeepsDestinationAndRemovesTemporaryFile(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "events.jsonl")
	if err := os.WriteFile(path, []byte("complete\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wantErr := errors.New("encode failed")

	err := Write(path, 0o644, func(file *os.File) error {
		if _, err := file.WriteString("partial"); err != nil {
			return err
		}
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Write() error = %v, want %v", err, wantErr)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "complete\n" {
		t.Fatalf("destination changed after failure: %q", content)
	}
	assertNoTemporaryOutputs(t, directory)
}

func TestWriteFileUsesRequestedModeForNewOutput(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "scorecard.json")

	if err := WriteFile(path, []byte("{}\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o640 {
		t.Fatalf("mode = %o, want 640", got)
	}
	assertNoTemporaryOutputs(t, directory)
}

func assertNoTemporaryOutputs(t *testing.T, directory string) {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name()[0] == '.' {
			t.Fatalf("temporary output was not removed: %s", entry.Name())
		}
	}
}
