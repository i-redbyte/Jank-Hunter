package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
	"github.com/i-redbyte/jank-hunter/cli/internal/mathanalysis"
)

func TestFilterGlobalScopeOnlyInMathematicalDataQuality(t *testing.T) {
	path := filepath.Join(t.TempDir(), "input.jhlog")
	if err := jhlog.WriteSample(path); err != nil {
		t.Fatal(err)
	}
	options := analyze.Options{Filter: analyze.Filter{RouteContains: "/checkout"}}
	summary, err := analyze.InspectFilesWithOptions("filtered", []string{path}, options)
	if err != nil {
		t.Fatal(err)
	}
	if summary.HTTPCount != 1 || summary.ContextCount == 0 {
		t.Fatal("fixture lost filtered/global observations")
	}
	inspected, err := mathanalysis.AnalyzeInspectWithSummary([]string{path}, options, summary)
	if err != nil {
		t.Fatal(err)
	}
	compared, err := mathanalysis.AnalyzeCompareWithSummaries([]string{path}, []string{path}, options, summary, summary)
	if err != nil {
		t.Fatal(err)
	}
	for _, compare := range []bool{false, true} {
		dir := t.TempDir()
		overview := filepath.Join(dir, "overview.html")
		mathPath := filepath.Join(dir, "math.html")
		if compare {
			err = WriteCompareReportWithOptions(overview, compared.Comparison, nil, nil, ReportOptions{})
		} else {
			err = WriteInspectWithOptions(overview, summary, ReportOptions{})
		}
		if err != nil {
			t.Fatal(err)
		}
		if compare {
			err = WriteMathCompareWithOptions(mathPath, compared, ReportOptions{})
		} else {
			err = WriteMathInspectWithOptions(mathPath, inspected, ReportOptions{})
		}
		if err != nil {
			t.Fatal(err)
		}
		marker := "показаны глобально"
		assertHTMLNotContains(t, overview, marker)
		data, err := os.ReadFile(mathPath)
		if err != nil {
			t.Fatal(err)
		}
		html := string(data)
		start := strings.Index(html, `id="data-quality"`)
		if start < 0 {
			t.Fatal("missing data-quality")
		}
		end := strings.Index(html[start:], "</section>")
		if end < 0 {
			t.Fatal("missing section end")
		}
		end += start
		if !strings.Contains(html[start:end], marker) || strings.Contains(html[:start]+html[end:], marker) {
			t.Fatal("global filter scope is absent or duplicated outside data-quality")
		}
	}
}
