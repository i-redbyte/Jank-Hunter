package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
)

func TestWorkerComparisonKeepsExistingSectionsAndUnknownTelemetry(t *testing.T) {
	c := analyze.Compare(analyze.Summary{}, analyze.Summary{WorkerAnalysis: &analyze.WorkerAnalysis{Started: 2}})
	path := filepath.Join(t.TempDir(), "compare.html")
	if err := WriteCompareReportWithOptions(path, c, nil, nil, ReportOptions{}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`id="worker-compare"`, `id="comparison-overview"`, `data-worker-registry-compare`, `нет данных`, `UUID не сопоставляет`} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("missing %q", want)
		}
	}
}

func TestWorkerComparisonRendersBoundedClassRowsWithExplicitComparability(t *testing.T) {
	c := analyze.Compare(analyze.Summary{}, analyze.Summary{WorkerAnalysis: &analyze.WorkerAnalysis{}})
	c.Workers.Classes = []analyze.WorkerClassComparison{{Class: "app.Sync", BaselineStarts: 2, CandidateStarts: 3}}
	c.Workers.OmittedClasses = 4
	path := filepath.Join(t.TempDir(), "compare.html")
	if err := WriteCompareReportWithOptions(path, c, nil, nil, ReportOptions{}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"app.Sync", "Сопоставление классов воркеров", "ещё 4 классов", "не сравнивается", "нет данных"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("missing %q", want)
		}
	}
}
