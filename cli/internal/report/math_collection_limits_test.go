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

func TestActualMathBudgetLimitIsVisibleOnlyInsideDataQuality(t *testing.T) {
	t.Setenv("JH_LANG", "ru")
	dir := t.TempDir()
	input := filepath.Join(dir, "input.jhlog")
	if err := jhlog.WriteSample(input); err != nil {
		t.Fatal(err)
	}
	summary, err := analyze.InspectFilesWithOptions("input", []string{input}, analyze.Options{})
	if err != nil {
		t.Fatal(err)
	}
	options := analyze.Options{MathMemoryLimitBytes: 1024}
	for _, compare := range []bool{false, true} {
		name := "inspect"
		if compare {
			name = "compare"
		}
		t.Run(name, func(t *testing.T) {
			out := filepath.Join(dir, name+".html")
			if compare {
				model, err := mathanalysis.AnalyzeCompareWithSummaries([]string{input}, []string{input}, options, summary, summary)
				if err != nil {
					t.Fatal(err)
				}
				if err := WriteMathCompareWithOptions(out, model, ReportOptions{}); err != nil {
					t.Fatal(err)
				}
			} else {
				model, err := mathanalysis.AnalyzeInspectWithSummary([]string{input}, options, summary)
				if err != nil {
					t.Fatal(err)
				}
				if model.Summary.CollectionQuality.DiagnosticCompletenessPercent != summary.CollectionQuality.DiagnosticCompletenessPercent {
					t.Fatal("CLI memory limit changed SDK delivery completeness")
				}
				if err := WriteMathInspectWithOptions(out, model, ReportOptions{}); err != nil {
					t.Fatal(err)
				}
			}
			data, err := os.ReadFile(out)
			if err != nil {
				t.Fatal(err)
			}
			html := string(data)
			if strings.Contains(html, `id="math-verdict"`) {
				t.Fatal("unavailable mathematics must not render an automatic verdict")
			}
			start := strings.Index(html, `id="data-quality"`)
			if start < 0 {
				t.Fatal("missing data-quality section")
			}
			end := strings.Index(html[start:], "</section>")
			if end < 0 {
				t.Fatal("missing data-quality section end")
			}
			const reason = "Математический анализ остановлен по лимиту памяти"
			if !strings.Contains(html[start:start+end], reason) || strings.Contains(html[:start], reason) || strings.Contains(html[start+end:], reason) {
				t.Fatal("collection limit must appear exclusively inside mathematical data-quality")
			}
		})
	}
}

func TestSpectralWorkLimitIsNotReportedAsMemoryOrHealthyAnalysis(t *testing.T) {
	limit := mathanalysis.CollectionLimit{Component: "spectral analysis", Work: &mathanalysis.SpectralWorkLimit{LimitOperations: 100, ConsumedOperations: 80, RequestedOperations: 30}}
	summary := analyze.Summary{}
	for _, compare := range []bool{false, true} {
		path := filepath.Join(t.TempDir(), "math.html")
		var err error
		if compare {
			err = WriteMathCompareWithOptions(path, mathanalysis.CompareMathReport{CollectionLimits: []mathanalysis.CollectionLimit{limit}, Comparison: analyze.Compare(summary, summary)}, ReportOptions{})
		} else {
			err = WriteMathInspectWithOptions(path, mathanalysis.MathReport{CollectionLimits: []mathanalysis.CollectionLimit{limit}, Summary: summary}, ReportOptions{})
		}
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		html := string(data)
		start := strings.Index(html, `id="data-quality"`)
		if start < 0 {
			t.Fatal("missing data-quality")
		}
		const reason = "лимиту вычислительной работы"
		end := strings.Index(html[start:], "</section>")
		if end < 0 || !strings.Contains(html[start:start+end], reason) || strings.Contains(html[:start], reason) || strings.Contains(html[start+end:], reason) {
			t.Fatal("spectral work limit missing or outside mathematical data-quality")
		}
		if strings.Contains(html, "лимит 0 байт") {
			t.Fatal("work limit mislabeled as zero-byte memory limit")
		}
	}
}
