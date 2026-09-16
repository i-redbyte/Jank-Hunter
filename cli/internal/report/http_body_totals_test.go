package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
)

func TestHTTPBodyReportIdentifiesIncompleteTotals(t *testing.T) {
	for _, complete := range []bool{false, true} {
		name := "partial"
		known := 0
		if complete {
			name, known = "complete", 2
		}
		t.Run(name, func(t *testing.T) {
			summary := analyze.Summary{Title: name, HTTPCount: 2, NetworkAnalysis: &analyze.NetworkAnalysis{
				BytesRx: 130, BytesTx: 300, KnownResponseBytes: known, KnownRequestBytes: known,
			}, Routes: []analyze.RouteStats{{Route: "GET /body", Count: 2, BytesRx: 130, BytesTx: 300,
				KnownRequestBytes: known, KnownResponseBytes: known}}}
			path := filepath.Join(t.TempDir(), "http.html")
			if err := WriteMathInspectWithOptions(path, sampleMathReport(summary), ReportOptions{}); err != nil {
				t.Fatal(err)
			}
			payload, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			html := string(payload)
			if got := strings.Contains(html, "Неполные размеры HTTP-тел"); got == complete {
				t.Fatalf("incomplete body warning=%t, complete=%t", got, complete)
			}
		})
	}
}
