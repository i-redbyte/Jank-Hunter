package report

import (
	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/mathanalysis"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestActualUIDTrafficQualityAppearsOnlyInMathematicalDataQuality(t *testing.T) {
	paths := []string{"../../../wire/testdata/uid-traffic-5.1.0.jhlog"}
	summary, err := analyze.InspectFilesWithOptions("UID traffic", paths, analyze.Options{})
	if err != nil {
		t.Fatal(err)
	}
	const reason = "неизвестное происхождение снимков RX/TX"
	comparison := analyze.Compare(summary, summary)
	for _, compare := range []bool{false, true} {
		name := "inspect"
		if compare {
			name = "compare"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			overview := filepath.Join(dir, "overview.html")
			mathPath := filepath.Join(dir, "math.html")
			leaks := filepath.Join(dir, "leaks.html")
			if compare {
				if err := WriteCompareReportWithOptions(overview, comparison, nil, nil, ReportOptions{}); err != nil {
					t.Fatal(err)
				}
				if err := WriteLeakCompareWithOptions(leaks, analyze.BuildLeakCompareReport(comparison), ReportOptions{}); err != nil {
					t.Fatal(err)
				}
				report, err := mathanalysis.AnalyzeCompareWithSummaries(paths, paths, analyze.Options{}, summary, summary)
				if err != nil {
					t.Fatal(err)
				}
				if err := WriteMathCompareWithOptions(mathPath, report, ReportOptions{}); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := WriteInspectWithOptions(overview, summary, ReportOptions{}); err != nil {
					t.Fatal(err)
				}
				if err := WriteLeakInspectWithOptions(leaks, analyze.BuildLeakReport(summary), ReportOptions{}); err != nil {
					t.Fatal(err)
				}
				report, err := mathanalysis.AnalyzeInspectWithSummary(paths, analyze.Options{}, summary)
				if err != nil {
					t.Fatal(err)
				}
				if err := WriteMathInspectWithOptions(mathPath, report, ReportOptions{}); err != nil {
					t.Fatal(err)
				}
			}
			assertHTMLNotContains(t, overview, reason)
			assertHTMLNotContains(t, leaks, reason)
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
			if end < 0 || !strings.Contains(html[start:start+end], reason) || strings.Contains(html[:start], reason) || strings.Contains(html[start+end:], reason) {
				t.Fatal("UID traffic collection issue escaped math/data-quality")
			}
		})
	}
}
