package analyze

import (
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func TestHTTPBodyProblemCostRetainsItsPartialDataLimit(t *testing.T) {
	summary := completeProblemFixture()
	report, err := BuildProblemReport(summary)
	if err != nil {
		t.Fatal(err)
	}
	finding := findingByDetector(report.Problems, "network.route_health")
	if finding == nil || finding.Cost == nil || finding.Cost.Bytes == nil {
		t.Fatal("network finding lost observed byte cost")
	}
	if !strings.Contains(strings.Join(finding.Limitations, " "), "Размеры HTTP-тел неполны") {
		t.Fatal("partial byte cost has no lower-bound limitation")
	}
}

func TestHTTPBodyTotalsKnownnessSeparatesLegacyMultipleExchanges(t *testing.T) {
	// Approved JHLOG 5.1.0 HTTP_BODY_TOTALS wire bit; literal also detects codec drift.
	const bodyTotals = uint64(1 << 23)
	const known = uint64(jhlog.FlagHTTPRequestBytesKnown | jhlog.FlagHTTPResponseBytesKnown)
	for _, test := range []struct {
		name      string
		attempts  uint16
		redirects uint16
		flags     uint64
		wantKnown int
	}{
		{"legacy single", 1, 0, known, 1},
		{"legacy retry", 2, 0, known, 0},
		{"legacy redirect", 2, 1, known, 0},
		{"marked retry", 2, 0, known | bodyTotals, 1},
		{"marked but incomplete", 2, 0, bodyTotals, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			var aggregate httpAggregate
			event := jhlog.HTTPEvent{Attempts: test.attempts, Redirects: test.redirects, TxBytes: 300, RxBytes: 130}
			aggregate.add(&event, test.flags, 0, 100, false)
			if aggregate.knownRequestBytes != test.wantKnown || aggregate.knownResponseBytes != test.wantKnown {
				t.Fatalf("known request/response = %d/%d, want %d/%d", aggregate.knownRequestBytes, aggregate.knownResponseBytes, test.wantKnown, test.wantKnown)
			}
			if aggregate.bytesTx != 300 || aggregate.bytesRx != 130 {
				t.Fatalf("observed lower-bound bytes lost: tx=%d rx=%d", aggregate.bytesTx, aggregate.bytesRx)
			}
		})
	}
}
