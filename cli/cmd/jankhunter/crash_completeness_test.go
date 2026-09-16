package main

import (
	"bytes"
	"encoding/csv"
	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"strings"
	"testing"
)

func TestComparisonCSVExposesUnknownCompletenessWithoutSentinelArithmetic(t *testing.T) {
	summary := analyze.Summary{CollectionQuality: analyze.CollectionQuality{DiagnosticCompletenessPercent: -1, DiagnosticCompletenessLevel: "unknown", DiagnosticCompletenessModel: "model"}}
	var output bytes.Buffer
	if err := writeComparisonCSV(&output, analyze.Compare(summary, summary)); err != nil {
		t.Fatal(err)
	}
	records, err := csv.NewReader(&output).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range records {
		if record[0] != "collection_quality" {
			continue
		}
		if record[3] != "полнота неизвестна" || record[4] != "полнота неизвестна" || record[8] != "false" || strings.Contains(record[5], "-1") {
			t.Fatalf("invalid completeness row: %v", record)
		}
		return
	}
	t.Fatal("CSV silently omitted unknown collection completeness")
}
