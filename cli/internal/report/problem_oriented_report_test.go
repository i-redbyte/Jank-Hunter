package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
)

func TestCompanionPagesDoNotDuplicateDetailedRetentionAndContextRegistries(t *testing.T) {
	summary := analyze.Summary{
		MemoryLeaks: []analyze.MemoryLeakSuspect{{
			ClassName: "com.example.LeakedActivity", Holder: "com.example.AppCache",
			Screen: "Feed", Operation: "open", Count: 2, MaxAgeMS: 30_000,
		}},
		SignalContexts: []analyze.SignalContextStats{{
			Screen: "Feed", Operation: "open", Owner: "com.example.FeedPresenter", HTTPCount: 2,
		}},
	}
	directory := t.TempDir()
	inspectPath := filepath.Join(directory, "inspect.html")
	if err := WriteInspectWithOptions(inspectPath, summary, ReportOptions{Links: ReportLinks{
		Math: "inspect-math.html", Leaks: "inspect-leaks.html",
	}}); err != nil {
		t.Fatal(err)
	}
	inspectHTML := readReportTestHTML(t, inspectPath)
	for _, want := range []string{
		`href="inspect-leaks.html"`, "Открыть разбор удержаний памяти",
		`href="inspect-math.html#operation-contexts"`, "Открыть исходные измерения",
	} {
		if !strings.Contains(inspectHTML, want) {
			t.Fatalf("companion link text %q is missing", want)
		}
	}
	for _, duplicate := range []string{
		"Фильтр реестра утечек памяти", `id="signal-context-table"`,
	} {
		if strings.Contains(inspectHTML, duplicate) {
			t.Fatalf("overview duplicates companion content %q", duplicate)
		}
	}

	mathPath := filepath.Join(directory, "inspect-math.html")
	if err := WriteMathInspectWithOptions(mathPath, sampleMathReport(summary), ReportOptions{Links: ReportLinks{
		Main: "inspect.html", Leaks: "inspect-leaks.html",
	}}); err != nil {
		t.Fatal(err)
	}
	mathHTML := readReportTestHTML(t, mathPath)
	if !strings.Contains(mathHTML, `href="inspect-leaks.html"`) ||
		!strings.Contains(mathHTML, "Подробный разбор удержаний открыт в отдельном разделе") {
		t.Fatal("math page does not link to the dedicated retention report")
	}
	if strings.Contains(mathHTML, "Фильтр реестра утечек памяти") {
		t.Fatal("math page duplicates the detailed retention registry")
	}
}

func TestStandaloneOverviewKeepsDetailedRegistries(t *testing.T) {
	summary := analyze.Summary{
		MemoryLeaks:    []analyze.MemoryLeakSuspect{{ClassName: "com.example.LeakedActivity", Count: 1}},
		SignalContexts: []analyze.SignalContextStats{{Screen: "Feed", Operation: "open"}},
	}
	path := filepath.Join(t.TempDir(), "standalone.html")
	if err := WriteInspectWithOptions(path, summary, ReportOptions{}); err != nil {
		t.Fatal(err)
	}
	html := readReportTestHTML(t, path)
	for _, want := range []string{"Фильтр реестра утечек памяти", `id="signal-context-table"`} {
		if !strings.Contains(html, want) {
			t.Fatalf("standalone overview lost %q", want)
		}
	}
}

func readReportTestHTML(t *testing.T, path string) string {
	t.Helper()
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(payload)
}
