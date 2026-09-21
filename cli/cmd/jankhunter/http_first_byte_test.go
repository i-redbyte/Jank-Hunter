package main

import (
	"bytes"
	"encoding/csv"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
)

func TestHTTPFirstByteCSVRetainsUnknownAndLegacyCoverageWithoutLatencyComparison(t *testing.T) {
	summary := analyze.Summary{CollectionQuality: analyze.CollectionQuality{
		HTTPFirstByte: &analyze.HTTPFirstByteQuality{Known: 2, Unknown: 3, Legacy: 5},
	}}
	var output bytes.Buffer
	if err := writeComparisonCSV(&output, analyze.Compare(analyze.Summary{}, summary)); err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(&output).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"known": "2", "unknown": "3", "legacy": "5"}
	for _, row := range rows {
		if row[0] != "http_first_byte_coverage" {
			continue
		}
		if row[3] != "" || row[4] != want[row[1]] || row[8] != "false" {
			t.Fatalf("coverage row: %v", row)
		}
		delete(want, row[1])
	}
	if len(want) != 0 {
		t.Fatalf("coverage missing from CSV: %v", want)
	}
}
