package analyze

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func TestInspectSampleIncludesFPSAndGauges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.jhlog")
	if err := jhlog.WriteSample(path); err != nil {
		t.Fatalf("WriteSample() error = %v", err)
	}
	log, err := jhlog.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	summary := Inspect("sample", []jhlog.Log{log})
	if summary.UIAvgFPS <= 0 {
		t.Fatalf("UIAvgFPS = %.2f, want > 0", summary.UIAvgFPS)
	}
	if summary.UIMinFPS <= 0 {
		t.Fatalf("UIMinFPS = %.2f, want > 0", summary.UIMinFPS)
	}
	if len(summary.Gauges) == 0 {
		t.Fatalf("expected gauges")
	}
	if summary.HTTPCount != 3 {
		t.Fatalf("HTTPCount = %d, want 3", summary.HTTPCount)
	}
}

func TestInspectFilesAppliesOwnerMap(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "sample.jhlog")
	if err := jhlog.WriteSample(logPath); err != nil {
		t.Fatalf("WriteSample() error = %v", err)
	}
	mapPath := filepath.Join(dir, "owner-map.json")
	if err := os.WriteFile(mapPath, []byte(`{"owners":{"FeedRepository.refresh":"feed owner"}}`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	ownerMap, err := LoadOwnerMap(mapPath)
	if err != nil {
		t.Fatalf("LoadOwnerMap() error = %v", err)
	}

	summary, err := InspectFilesWithOptions("sample", []string{logPath}, Options{OwnerMap: ownerMap})
	if err != nil {
		t.Fatalf("InspectFilesWithOptions() error = %v", err)
	}
	found := false
	for _, owner := range summary.Owners {
		if owner.Owner == "feed owner" {
			found = true
		}
	}
	if !found {
		t.Fatalf("owner map was not applied: %+v", summary.Owners)
	}
}

func TestInspectFilesStreamsSample(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.jhlog")
	if err := jhlog.WriteSample(path); err != nil {
		t.Fatalf("WriteSample() error = %v", err)
	}

	summary, err := InspectFiles("sample", []string{path})
	if err != nil {
		t.Fatalf("InspectFiles() error = %v", err)
	}
	if summary.EventCount == 0 || summary.HTTPCount != 3 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if summary.Dictionary == 0 {
		t.Fatalf("expected dictionary count")
	}
}

func TestInspectFilesAppliesRouteFilter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.jhlog")
	if err := jhlog.WriteSample(path); err != nil {
		t.Fatalf("WriteSample() error = %v", err)
	}

	summary, err := InspectFilesWithFilter("sample", []string{path}, Filter{RouteContains: "/checkout"})
	if err != nil {
		t.Fatalf("InspectFilesWithFilter() error = %v", err)
	}
	if summary.HTTPCount != 1 {
		t.Fatalf("HTTPCount = %d, want 1", summary.HTTPCount)
	}
	if len(summary.Routes) != 1 || summary.Routes[0].Route != "POST /checkout" {
		t.Fatalf("unexpected routes: %+v", summary.Routes)
	}
}
