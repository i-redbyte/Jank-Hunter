package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func TestMathCSVAndJSONExportsHonorScreenFilter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "input.jhlog")
	file, writer, err := jhlog.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	for i, value := range []string{"GET /excluded", "ExcludedScreen", "ExcludedOwner"} {
		if err := writer.WriteEvent(jhlog.Event{Type: jhlog.EventDictionary, Dictionary: &jhlog.DictionaryEntry{Kind: jhlog.DictGeneric, ID: uint64(i + 1), Value: value}}); err != nil {
			t.Fatal(err)
		}
	}
	for i := uint64(0); i < 24; i++ {
		if err := writer.WriteEvent(jhlog.Event{Type: jhlog.EventHTTP, TimeMS: 1000 + i*5000, Attribution: jhlog.AttributionContext{Present: true, Screen: jhlog.LocalSymbol(2), Owner: jhlog.LocalSymbol(3)}, HTTP: &jhlog.HTTPEvent{RouteRef: jhlog.LocalSymbol(1), DurationMS: 2000, Status: jhlog.Status5xx}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.CloseWithReason(jhlog.SegmentEndNormal); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"csv", "json"} {
		for _, screen := range []string{"ExcludedScreen", "SelectedScreen"} {
			t.Run(format+"/"+screen, func(t *testing.T) {
				out := filepath.Join(t.TempDir(), "math."+format)
				if err := runProblems([]string{path, "--dataset", "math-findings", "--format", format, "--screen", screen, "--out", out}); err != nil {
					t.Fatal(err)
				}
				data, err := os.ReadFile(out)
				if err != nil {
					t.Fatal(err)
				}
				present := strings.Contains(string(data), "GET /excluded")
				if present != (screen == "ExcludedScreen") {
					t.Fatalf("screen=%s exported excluded route=%v", screen, present)
				}
			})
		}
	}
}
