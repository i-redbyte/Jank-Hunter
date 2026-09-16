package report

import (
	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/mathanalysis"
	"path/filepath"
	"testing"
)

func TestCrashCompletenessSentinelIsNotRenderedAsAPercentage(t *testing.T) {
	quality := sampleCollectionQuality()
	quality.DiagnosticCompletenessPercent = -1
	quality.DiagnosticCompletenessLevel = "unknown"
	summary := analyze.Summary{Title: "crash", CollectionQuality: quality}
	inspect := filepath.Join(t.TempDir(), "inspect.html")
	if err := WriteInspectWithOptions(inspect, summary, ReportOptions{}); err != nil {
		t.Fatal(err)
	}
	assertHTMLNotContains(t, inspect, "полнота неизвестна")
	assertHTMLNotContains(t, inspect, "-1.00 из 100", "-1.00%")
	comparison := filepath.Join(t.TempDir(), "compare.html")
	if err := WriteCompareReportWithOptions(comparison, analyze.Compare(summary, summary), nil, nil, ReportOptions{}); err != nil {
		t.Fatal(err)
	}
	assertHTMLNotContains(t, comparison, "полнота неизвестна")
	assertHTMLNotContains(t, comparison, "-1.00%")
	mathInspect := filepath.Join(t.TempDir(), "math.html")
	if err := WriteMathInspectWithOptions(mathInspect, sampleMathReport(summary), ReportOptions{}); err != nil {
		t.Fatal(err)
	}
	assertHTMLContains(t, mathInspect, "полнота неизвестна")
	assertHTMLNotContains(t, mathInspect, "-1.00 из 100", "-1.00%")
	mathCompare := filepath.Join(t.TempDir(), "math-compare.html")
	if err := WriteMathCompareWithOptions(mathCompare, mathanalysis.CompareMathReport{Comparison: analyze.Compare(summary, summary)}, ReportOptions{}); err != nil {
		t.Fatal(err)
	}
	assertHTMLContains(t, mathCompare, "полнота неизвестна")
	assertHTMLNotContains(t, mathCompare, "-1.00%")
}

func TestUnknownCompletenessRemainsVisibleWhenTheOtherReportHasNoModel(t *testing.T) {
	unknown := analyze.Summary{CollectionQuality: analyze.CollectionQuality{DiagnosticCompletenessPercent: -1, DiagnosticCompletenessLevel: "unknown", DiagnosticCompletenessModel: "model"}}
	for _, pair := range [][2]analyze.Summary{{{}, unknown}, {unknown, {}}} {
		path := filepath.Join(t.TempDir(), "compare.html")
		comparison := analyze.Compare(pair[0], pair[1])
		if err := WriteMathCompareWithOptions(path, mathanalysis.CompareMathReport{Comparison: comparison}, ReportOptions{}); err != nil {
			t.Fatal(err)
		}
		assertHTMLContains(t, path, "полнота неизвестна")
		assertHTMLNotContains(t, path, "база 0.00%", "проверяемый прогон 0.00%", "-1.00%")
	}
}
