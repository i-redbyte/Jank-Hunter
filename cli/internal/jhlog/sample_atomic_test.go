package jhlog

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteSampleAtomicallyReplacesExistingOutput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.jhlog")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := WriteSample(path); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("mode = %o, want preserved 600", got)
	}
	result, err := StreamFileWithResult(path, func(Event, map[uint64]string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if result.Events == 0 || result.Status != SegmentStatusClosedClean {
		t.Fatalf("sample output is not a sealed jhlog: %+v", result)
	}
}
