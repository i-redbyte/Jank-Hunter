package retrace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMappingSnapshotRejectsReplacementAndPreservesValidatedBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mapping.txt")
	original := []byte("Original -> a:\n")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	digest := mappingDigest(t, path)
	snapshot, cleanup, err := snapshotMapping(context.Background(), path, digest)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	info, err := os.Stat(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("snapshot exposed: %v", info.Mode())
	}
	if err := os.WriteFile(path, []byte("Foreign -> a:\n"), 0600); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(snapshot)
	if err != nil || string(data) != string(original) {
		t.Fatalf("snapshot changed: %q %v", data, err)
	}
	if _, _, err := snapshotMapping(context.Background(), path, digest); err == nil || !strings.Contains(err.Error(), "SHA-256 changed") {
		t.Fatalf("replacement accepted: %v", err)
	}
	cleanup()
	if _, err := os.Stat(snapshot); !os.IsNotExist(err) {
		t.Fatalf("snapshot not removed: %v", err)
	}
}

func TestCancelledMappingSnapshotDoesNotProceed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mapping.txt")
	if err := os.WriteFile(path, []byte("Original -> a:\n"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := snapshotMapping(ctx, path, mappingDigest(t, path)); err != context.Canceled {
		t.Fatalf("cancel ignored: %v", err)
	}
}
