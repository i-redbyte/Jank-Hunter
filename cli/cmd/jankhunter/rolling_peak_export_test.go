package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
)

func TestRollingPeakLowerBoundSurvivesProblemExports(t *testing.T) {
	summary := analyze.Summary{IOAnalysis: &analyze.IOAnalysis{Calls: []analyze.IOStats{{Operation: "file_read", Source: "app.Reader", Count: 100, PeakOperationsPerSecond: 25, BurstEstimateStatus: "lower_bound_rolling_second", KnownByteOperations: 100, MaxBytes: 32}}}}
	report, err := analyze.BuildProblemReport(summary)
	if err != nil {
		t.Fatal(err)
	}
	summary.Problems = report.Problems
	for _, format := range []string{"json", "csv"} {
		var output bytes.Buffer
		if format == "json" {
			err = writeProblemsDatasetJSON(&output, datasetProblems, summary, nil)
		} else {
			err = writeProblemsDatasetCSV(&output, datasetProblems, summary, nil)
		}
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(output.String(), "≥ 25") {
			t.Fatalf("%s lost lower bound: %s", format, output.String())
		}
	}
}
