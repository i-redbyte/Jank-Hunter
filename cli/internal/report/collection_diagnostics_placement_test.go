package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/mathanalysis"
)

func TestCollectionDiagnosticsAppearOnlyInMathematicalDataQuality(t *testing.T) {
	summary := analyze.Summary{HTTPCount: 3, CollectionQuality: sampleCollectionQuality(),
		NetworkAnalysis: &analyze.NetworkAnalysis{KnownResponseBytes: 1, KnownRequestBytes: 1}}
	summary.CollectionQuality.DiagnosticCompletenessPercent = -1
	summary.CollectionQuality.DiagnosticCompletenessLevel = "unknown"
	summary.CollectionQuality.AsyncAttribution = &analyze.AsyncAttributionQuality{Status: "unknown", HandlerPostsWithoutContext: 7}
	summary.CollectionQuality.HTTPFirstByte = &analyze.HTTPFirstByteQuality{Known: 1, Unknown: 1, Legacy: 1}
	const graphStorageReason = "исчерпан бюджет памяти сборщиков графа"
	const legacyReason = "lifecycle/binding-покрытие частичное: для полного автоматического наблюдения обновите Gradle plugin"
	summary.Warnings = append(summary.Warnings, "Качество сбора: "+legacyReason)
	summary.CollectionQuality.Reasons = append(summary.CollectionQuality.Reasons, graphStorageReason)
	summary.Warnings = append(summary.Warnings, "Качество сбора: "+graphStorageReason)
	comparison := analyze.Compare(summary, summary)
	for _, compare := range []bool{false, true} {
		name := "inspect"
		if compare {
			name = "compare"
		}
		t.Run(name, func(t *testing.T) {
			mainPath := filepath.Join(t.TempDir(), "overview.html")
			mathPath := filepath.Join(t.TempDir(), "math.html")
			var mainErr, mathErr error
			if compare {
				mainErr = WriteCompareReportWithOptions(mainPath, comparison, nil, nil, ReportOptions{Links: ReportLinks{Math: "math.html"}})
				mathErr = WriteMathCompareWithOptions(mathPath, sampleCompareMathReport(comparison, summary), ReportOptions{})
			} else {
				mainErr = WriteInspectWithOptions(mainPath, summary, ReportOptions{Links: ReportLinks{Math: "math.html"}})
				mathErr = WriteMathInspectWithOptions(mathPath, sampleMathReport(summary), ReportOptions{})
			}
			if mainErr != nil || mathErr != nil {
				t.Fatalf("render: %v / %v", mainErr, mathErr)
			}
			assertHTMLNotContains(t, mainPath, "post без контекста:", "полнота неизвестна", "Неполные размеры HTTP-тел", "Покрытие измерения первого байта", graphStorageReason, legacyReason)
			data, err := os.ReadFile(mathPath)
			if err != nil {
				t.Fatal(err)
			}
			html := string(data)
			start := strings.Index(html, `id="data-quality"`)
			if start < 0 {
				t.Fatal("missing data-quality section")
			}
			for _, text := range []string{"post без контекста:", "полнота неизвестна", "Неполные размеры HTTP-тел", "Покрытие измерения первого байта", graphStorageReason, legacyReason} {
				if strings.Contains(html[:start], text) || !strings.Contains(html[start:], text) {
					t.Fatalf("%q must occur only inside data-quality", text)
				}
			}
		})
	}
}

func TestActualComparisonKeepsCollectionWarningsInsideDedicatedMathSection(t *testing.T) {
	baselinePaths := []string{"../../../wire/testdata/runtime-generation-5.1.0.jhlog"}
	candidatePaths := []string{"../../../wire/testdata/runtime-graph-storage-5.1.0.jhlog"}
	baseline, err := analyze.InspectFilesWithOptions("baseline", baselinePaths, analyze.Options{})
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := analyze.InspectFilesWithOptions("candidate", candidatePaths, analyze.Options{})
	if err != nil {
		t.Fatal(err)
	}
	comparison := analyze.Compare(baseline, candidate)
	mathReport, err := mathanalysis.AnalyzeCompareWithSummaries(baselinePaths, candidatePaths, analyze.Options{}, baseline, candidate)
	if err != nil {
		t.Fatal(err)
	}
	mathPath := filepath.Join(t.TempDir(), "math.html")
	leakPath := filepath.Join(t.TempDir(), "leaks.html")
	if err := WriteMathCompareWithOptions(mathPath, mathReport, ReportOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := WriteLeakCompareWithOptions(leakPath, analyze.BuildLeakCompareReport(comparison), ReportOptions{}); err != nil {
		t.Fatal(err)
	}
	const phrase = "исчерпан бюджет памяти сборщиков графа"
	data, err := os.ReadFile(mathPath)
	if err != nil {
		t.Fatal(err)
	}
	html := string(data)
	start := strings.Index(html, `id="data-quality"`)
	if start < 0 {
		t.Fatal("missing data-quality section")
	}
	end := strings.Index(html[start:], `</section>`)
	if start < 0 || end < 0 {
		t.Fatal("missing data-quality section")
	}
	end += start
	if strings.Contains(html[:start], phrase) || !strings.Contains(html[start:end], phrase) || strings.Contains(html[end:], phrase) {
		t.Error("collection warning must appear exclusively inside data-quality, not in the mathematical findings")
	}
	assertHTMLNotContains(t, leakPath, phrase)
}
