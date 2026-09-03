package main

import (
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
)

func TestHTTPSummaryLineSeparatesTransportAndStatusFailures(t *testing.T) {
	summary := analyze.Summary{
		HTTPCount:  3_609,
		HTTPFailed: 5,
		HTTPP95MS:  349,
		NetworkAnalysis: &analyze.NetworkAnalysis{
			TransportFailures: 5,
			HTTP4xx:           1_563,
			HTTP5xx:           2,
		},
	}

	line := httpSummaryLine(summary)
	for _, expected := range []string{
		"count=3609",
		"operational_failed=5",
		"transport_failed=5",
		"http_4xx=1563",
		"http_5xx=2",
		"p95=349ms",
	} {
		if !strings.Contains(line, expected) {
			t.Fatalf("HTTP summary %q does not contain %q", line, expected)
		}
	}
}
