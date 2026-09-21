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

func TestActualHeapClassCandidateIsSeparateFromRuntimeInHTML(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input.jhlog")
	if err := jhlog.WriteSample(input); err != nil {
		t.Fatal(err)
	}
	heap := &analyze.HeapEvidence{Leaks: []analyze.HeapLeakEvidence{{ClassName: "com.app.checkout.CheckoutActivity", Holder: "com.app.checkout.CheckoutPresenter", HolderField: "candidateOnlyField", GCRoot: "sticky class", GCRootObjectID: "0x1cafe", RetainedSizeKB: 65536, RetainedSizeState: analyze.HeapSizeExact,
		ReferencePath: []analyze.HeapPathElement{{ClassName: "GC root: sticky class", Kind: "gc_root"}, {ClassName: "com.app.checkout.CheckoutPresenter", Kind: "root_object"}, {ClassName: "com.app.checkout.CheckoutActivity", FieldName: "candidateOnlyField", Kind: "field"}},
	}}}
	options := analyze.Options{HeapEvidence: heap}
	summary, err := analyze.InspectFilesWithOptions("candidate", []string{input}, options)
	if err != nil {
		t.Fatal(err)
	}
	comparison := analyze.Compare(summary, summary)
	for _, compare := range []bool{false, true} {
		name := "inspect"
		if compare {
			name = "compare"
		}
		t.Run(name, func(t *testing.T) {
			output := t.TempDir()
			leaks := filepath.Join(output, "leaks.html")
			main := filepath.Join(output, "overview.html")
			mathPath := filepath.Join(output, "math.html")
			if compare {
				if err := WriteLeakCompareWithOptions(leaks, analyze.BuildLeakCompareReport(comparison), ReportOptions{}); err != nil {
					t.Fatal(err)
				}
				if err := WriteCompareReportWithOptions(main, comparison, nil, nil, ReportOptions{}); err != nil {
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
				if err := WriteInspectWithOptions(main, summary, ReportOptions{}); err != nil {
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
			assertHTMLContains(t, leaks, "HPROF: экземпляр класса", "Размер экземпляра в дампе", "candidateOnlyField", "64.0 МБ", "0x1cafe")
			assertHTMLNotContains(t, main, "candidateOnlyField", "HPROF подтвердил путь удержания")
			assertHTMLNotContains(t, leaks, "связь с наблюдаемым объектом неизвестна")
			assertHTMLContains(t, mathPath, "связь с наблюдаемым объектом неизвестна")
			data, err := os.ReadFile(mathPath)
			if err != nil {
				t.Fatal(err)
			}
			html := string(data)
			start := strings.Index(html, `id="data-quality"`)
			if start < 0 {
				t.Fatal("missing mathematical data-quality")
			}
			end := strings.Index(html[start:], "</section>")
			const reason = "связь с наблюдаемым объектом неизвестна"
			if end < 0 || !strings.Contains(html[start:start+end], reason) || strings.Contains(html[:start], reason) || strings.Contains(html[start+end:], reason) {
				t.Fatal("association limitation escaped mathematical data-quality")
			}

		})
	}
}
