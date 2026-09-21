package mathanalysis

import (
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
)

func TestHeapInformationIsNeutralDataQualityInInspectAndCompare(t *testing.T) {
	summary := analyze.Summary{EventCount: 100, HTTPCount: 100, HeapDiagnostics: []analyze.HeapDiagnostic{{Code: "auto_discovery", Severity: analyze.HeapDiagnosticInfo, Impact: analyze.HeapImpactNone, Message: "automatic attachment"}}}
	for _, findings := range [][]Finding{dataQualityFindingsForRuns(summary, 1), compareFindings(analyze.Comparison{Baseline: summary, Candidate: summary})} {
		found := false
		for _, f := range findings {
			if f.Detail == "automatic attachment" {
				found = true
				if f.Severity != "ok" {
					t.Fatal("information became a quality warning")
				}
			}
		}
		if !found {
			t.Fatal("informational provenance disappeared")
		}
	}
}

func TestHeapUnknownImpactCannotUseInformationalEscape(t *testing.T) {
	summary := analyze.Summary{HeapDiagnostics: []analyze.HeapDiagnostic{{Severity: analyze.HeapDiagnosticInfo, Impact: "future", Message: "unknown impact"}}}
	if len(heapInformationFindings("", summary)) != 0 {
		t.Fatal("unknown impact presented as neutral information")
	}
}
