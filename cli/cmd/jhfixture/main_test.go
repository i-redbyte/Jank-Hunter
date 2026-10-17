package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/benchfixture"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func TestFixtureRejectsMetadataOverwritingCapture(t *testing.T) {
	for _, alias := range []string{"same path", "hardlink", "symlink", "symlinked directory"} {
		t.Run(alias, func(t *testing.T) {
			dir := t.TempDir()
			out := filepath.Join(dir, "capture.jhlog")
			metadata := out
			original := []byte("existing capture")
			if err := os.WriteFile(out, original, 0o600); err != nil {
				t.Fatal(err)
			}
			switch alias {
			case "hardlink":
				metadata = filepath.Join(dir, "metadata.json")
				if err := os.Link(out, metadata); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				metadata = filepath.Join(dir, "metadata.json")
				if err := os.Symlink(out, metadata); err != nil {
					t.Fatal(err)
				}
			case "symlinked directory":
				link := filepath.Join(t.TempDir(), "alias")
				if err := os.Symlink(dir, link); err != nil {
					t.Fatal(err)
				}
				metadata = filepath.Join(link, "capture.jhlog")
			}
			if _, err := writeFixture(out, metadata, "smoke"); err == nil || !strings.Contains(err.Error(), "overlap") {
				t.Fatalf("capture/metadata collision accepted: %v", err)
			}
			got, err := os.ReadFile(out)
			if err != nil || string(got) != string(original) {
				t.Fatalf("collision modified existing capture: %q, %v", got, err)
			}
		})
	}
}

func TestFixtureRejectsIdenticalNewOutputPaths(t *testing.T) {
	out := filepath.Join(t.TempDir(), "new.jhlog")
	if _, err := writeFixture(out, out, "smoke"); err == nil {
		t.Fatal("identical new paths accepted")
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatalf("collision created output: %v", err)
	}
}

func TestFixtureWritesSeparateReadableCaptureAndMetadata(t *testing.T) {
	dir := t.TempDir()
	out, metadataPath := filepath.Join(dir, "capture.jhlog"), filepath.Join(dir, "metadata.json")
	want, err := writeFixture(out, metadataPath, "smoke")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(metadataPath)
	if err != nil {
		t.Fatal(err)
	}
	var got benchfixture.Metadata
	if err := json.Unmarshal(data, &got); err != nil || got != want {
		t.Fatalf("metadata mismatch: %+v, %v", got, err)
	}
	if _, err := jhlog.StreamFileWithResult(out, func(jhlog.Event, map[uint64]string) error { return nil }); err != nil {
		t.Fatalf("fixture no longer readable: %v", err)
	}
}

func TestFixtureRejectsDanglingMetadataAliasToNewCapture(t *testing.T) {
	dir := t.TempDir()
	out, metadata := filepath.Join(dir, "new.jhlog"), filepath.Join(dir, "metadata.json")
	if err := os.Symlink(out, metadata); err != nil {
		t.Fatal(err)
	}
	if _, err := writeFixture(out, metadata, "smoke"); err == nil || !strings.Contains(err.Error(), "overlap") {
		t.Fatalf("new capture overwritten through dangling alias: %v", err)
	}
	if _, err := jhlog.StreamFileWithResult(out, func(jhlog.Event, map[uint64]string) error { return nil }); err != nil {
		t.Fatalf("collision left a damaged capture: %v", err)
	}
}
