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

func TestActualRetentionHeapWarningsStayInsideMathematicalDataQuality(t *testing.T) {
	testActualRetentionHeapWarningPlacement(t, "граф HPROF неполон из-за безопасных ограничений парсера")
}

func TestActualHeapWorkWarningsStayInsideMathematicalDataQuality(t *testing.T) {
	testActualRetentionHeapWarningPlacement(t, "Исчерпан общий бюджет работы HPROF")
}

func TestActualHeapInstanceSizeWarningsStayInsideMathematicalDataQuality(t *testing.T) {
	testActualRetentionHeapWarningPlacement(t, "Для 1 объектов HPROF отсутствует размер из описания их класса: удержанный размер неизвестен; сохранены предварительные оценки.")
}

func TestActualHeapPathTruncationWarningsStayInsideMathematicalDataQuality(t *testing.T) {
	testActualRetentionHeapWarningPlacement(t, "Цепочка HPROF превысила лимит глубины 48: показан только ограниченный фрагмент пути.")
}

func testActualRetentionHeapWarningPlacement(t *testing.T, reason string) {
	t.Helper()
	dir := t.TempDir()
	input := filepath.Join(dir, "retention.jhlog")
	file, writer, err := jhlog.Create(input)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := writer.WriteEvent(jhlog.Event{Type: jhlog.EventDictionary, Dictionary: &jhlog.DictionaryEntry{Kind: jhlog.DictClass, ID: 1, Value: "example.Screen"}}); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteEvent(jhlog.Event{Type: jhlog.EventRetained, TimeMS: 100, Retained: &jhlog.RetainedEvent{ClassRef: jhlog.LocalSymbol(1), AgeMS: 60000, Count: 1, Evidence: jhlog.RetentionEvidenceAfterExplicitGC}}); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	heap := &analyze.HeapEvidence{Warnings: []string{reason}, Leaks: []analyze.HeapLeakEvidence{{ClassName: "example.Screen", Confidence: "низкое: " + reason}}}
	summary, err := analyze.InspectFilesWithOptions("retention", []string{input}, analyze.Options{HeapEvidence: heap})
	if err != nil {
		t.Fatal(err)
	}
	if len(summary.MemoryLeaks) != 1 || len(summary.MemoryLeaks[0].QualityWarnings) == 0 {
		t.Fatal("fixture lost real retention or heap diagnostics")
	}
	options := analyze.Options{HeapEvidence: heap}
	comparison := analyze.Compare(summary, summary)
	for _, compare := range []bool{false, true} {
		name := "inspect"
		if compare {
			name = "compare"
		}
		t.Run(name, func(t *testing.T) {
			output := t.TempDir()
			leaks := filepath.Join(output, "leaks.html")
			overview := filepath.Join(output, "overview.html")
			mathPath := filepath.Join(output, "math.html")
			if compare {
				if err := WriteLeakCompareWithOptions(leaks, analyze.BuildLeakCompareReport(comparison), ReportOptions{}); err != nil {
					t.Fatal(err)
				}
				if err := WriteCompareReportWithOptions(overview, comparison, nil, nil, ReportOptions{}); err != nil {
					t.Fatal(err)
				}
				report, err := mathanalysis.AnalyzeCompareWithSummaries([]string{input}, []string{input}, options, summary, summary)
				if err != nil {
					t.Fatal(err)
				}
				if err := WriteMathCompareWithOptions(mathPath, report, ReportOptions{}); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := WriteLeakInspectWithOptions(leaks, analyze.BuildLeakReport(summary), ReportOptions{}); err != nil {
					t.Fatal(err)
				}
				if err := WriteInspectWithOptions(overview, summary, ReportOptions{}); err != nil {
					t.Fatal(err)
				}
				report, err := mathanalysis.AnalyzeInspectWithSummary([]string{input}, options, summary)
				if err != nil {
					t.Fatal(err)
				}
				if err := WriteMathInspectWithOptions(mathPath, report, ReportOptions{}); err != nil {
					t.Fatal(err)
				}
			}
			assertHTMLNotContains(t, leaks, reason)
			assertHTMLNotContains(t, overview, reason)
			data, err := os.ReadFile(mathPath)
			if err != nil {
				t.Fatal(err)
			}
			html := string(data)
			start := strings.Index(html, `id="data-quality"`)
			if start < 0 {
				t.Fatal("missing math/data-quality")
			}
			end := strings.Index(html[start:], "</section>")
			if end < 0 || !strings.Contains(html[start:start+end], reason) || strings.Contains(html[:start], reason) || strings.Contains(html[start+end:], reason) {
				t.Fatal("heap diagnostic is not confined to math/data-quality")
			}
		})
	}
}
