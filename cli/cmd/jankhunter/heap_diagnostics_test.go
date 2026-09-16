package main

import (
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func TestAutoDiscoveredHeapMessageHasInformationalProvenance(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "capture.jhlog")
	if err := jhlog.WriteSample(log); err != nil {
		t.Fatal(err)
	}
	data := append([]byte("JAVA PROFILE 1.0.3\x00"), make([]byte, 12)...)
	binary.BigEndian.PutUint32(data[len(data)-12:], 4)
	if err := os.WriteFile(filepath.Join(dir, "capture.hprof"), data, 0600); err != nil {
		t.Fatal(err)
	}
	options, err := optionsWithHeapEvidence("auto", []string{log}, analyze.Options{}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	data, err = json.Marshal(options.HeapEvidence)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err = json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	items, ok := raw["diagnostics"].([]any)
	if ok {
		for _, item := range items {
			d := item.(map[string]any)
			if d["code"] == "auto_discovery" && d["severity"] == "info" && d["impact"] == "none" {
				return
			}
		}
	}
	t.Fatalf("auto discovery has no informational provenance: %s", data)
}
