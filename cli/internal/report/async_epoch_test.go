package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
)

func TestAsyncLifecycleDetailsRemainInsideMathematicalDataQuality(t *testing.T) {
	summary := analyze.Summary{CollectionQuality: analyze.CollectionQuality{
		AsyncLifecycle: &analyze.AsyncLifecycleQuality{StaleCompletions: 3, DuplicateCompletions: 2,
			UnfinishedHTTP: 11, UnfinishedDatabase: 13, UnfinishedWorker: 17, UnfinishedDatabaseTransactions: 19,
			CompletionsInProgressAtStop: 23, LegacyHTTPCompletions: 29},
	}}
	directory := t.TempDir()
	mainPath := filepath.Join(directory, "inspect.html")
	if err := WriteInspectWithOptions(mainPath, summary, ReportOptions{Links: ReportLinks{Math: "math.html"}}); err != nil {
		t.Fatal(err)
	}
	assertHTMLNotContains(t, mainPath, "Границы сессий асинхронной аналитики", "Незавершённые HTTP", "Повторные завершения")
	comparison := analyze.Compare(summary, summary)
	comparePath := filepath.Join(directory, "compare.html")
	if err := WriteCompareReportWithOptions(comparePath, comparison, nil, nil, ReportOptions{Links: ReportLinks{Math: "math-compare.html"}}); err != nil {
		t.Fatal(err)
	}
	assertHTMLNotContains(t, comparePath, "Границы сессий асинхронной аналитики", "Незавершённые HTTP", "Повторные завершения")
	paths := []string{filepath.Join(directory, "math.html"), filepath.Join(directory, "math-compare.html")}
	if err := WriteMathInspectWithOptions(paths[0], sampleMathReport(summary), ReportOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := WriteMathCompareWithOptions(paths[1], sampleCompareMathReport(comparison, summary), ReportOptions{}); err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		html := string(data)
		quality := strings.Index(html, `id="data-quality"`)
		if quality < 0 {
			t.Fatal("missing mathematical data-quality section")
		}
		for _, text := range []string{"Границы сессий асинхронной аналитики", "Незавершённые HTTP", "Повторные завершения", "не означает потерю такого же числа событий"} {
			if strings.Contains(html[:quality], text) {
				t.Fatalf("%s: %q escaped the dedicated quality section", path, text)
			}
			if !strings.Contains(html[quality:], text) {
				t.Fatalf("%s: %q is missing from the dedicated quality section", path, text)
			}
		}
	}
}
