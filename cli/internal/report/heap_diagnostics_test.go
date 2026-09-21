package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/mathanalysis"
)

func TestHeapInformationRendersOnlyInMathematicalDataQuality(t *testing.T) {
	const marker = "CLI автоматически подключил HPROF: test.hprof"
	summary := analyze.Summary{HeapDiagnostics: []analyze.HeapDiagnostic{{Code: "auto_discovery", Severity: analyze.HeapDiagnosticInfo, Impact: analyze.HeapImpactNone, Message: marker}}}
	for _, compare := range []bool{false, true} {
		path := filepath.Join(t.TempDir(), "math.html")
		var err error
		if compare {
			err = WriteMathCompareWithOptions(path, mathanalysis.CompareMathReport{Comparison: analyze.Comparison{Baseline: summary, Candidate: summary}, Baseline: mathanalysis.MathReport{Summary: summary}, Candidate: mathanalysis.MathReport{Summary: summary}}, ReportOptions{})
		} else {
			err = WriteMathInspectWithOptions(path, mathanalysis.MathReport{Summary: summary}, ReportOptions{})
		}
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		start := strings.Index(text, `id="data-quality"`)
		if start < 0 {
			t.Fatal("missing data quality")
		}
		end := start + strings.Index(text[start:], "</section>")
		if end < start || !strings.Contains(text[start:end], marker) || strings.Contains(text[:start]+text[end:], marker) {
			t.Fatalf("compare=%t: information missing or outside math data-quality", compare)
		}
		if strings.Contains(text, `class="warning">`+marker) {
			t.Fatal("neutral information rendered as warning")
		}
	}
}
