package analyze

import (
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
