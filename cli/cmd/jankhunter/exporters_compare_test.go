package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
)

func TestComparisonCSVExportsDatabaseAndAndroidComponentMetricsFromModel(t *testing.T) {
	comparison := analyze.Comparison{OperationDeltas: []analyze.OperationDelta{{
		Operation: "feed.open", Screen: "Feed", DatabaseComparable: true,
		BaselineDatabaseCallsPerOperation: 2, CandidateDatabaseCallsPerOperation: 3,
		BaselineDatabaseMainRatePct: 25, CandidateDatabaseMainRatePct: 50,
		BaselineDatabaseFailureRatePct: 5, CandidateDatabaseFailureRatePct: 10,
		BaselineDatabaseWallMSPerOperation: 100, CandidateDatabaseWallMSPerOperation: 300,
		Severity: "high", Confidence: "high",
	}}, AndroidComponents: analyze.AndroidComponentComparison{Metrics: []analyze.Delta{{
		Name: "Binder client p95", Baseline: "20.00 мс", Candidate: "40.00 мс",
		Change: "+100.0%", Severity: "high", Confidence: "high", Comparable: true,
	}}}, Database: analyze.DatabaseComparison{
		Comparable: true,
		Metrics: []analyze.Delta{
			{
				Name: "DB calls per minute", Baseline: "100.00 выз./мин", Candidate: "125.00 выз./мин",
				Change: "+25.0%", Severity: "high", Confidence: "high", Comparable: true,
			},
			{
				Name: "DB transaction p95", Baseline: "40.00 мс", Candidate: "80.00 мс",
				Change: "+100.0%", Severity: "high", Confidence: "high", Comparable: true,
			},
		},
		Statements: []analyze.DatabaseStatementDelta{{
			Query: "SELECT value FROM sample WHERE id = ?", Operation: "query",
			BaselinePresent: true, CandidatePresent: true, Comparable: true,
			BaselineCalls: 100, CandidateCalls: 125,
		}},
	}}
	var output bytes.Buffer
	if err := writeComparisonCSV(&output, comparison); err != nil {
		t.Fatal(err)
	}
	csv := output.String()
	for _, expected := range []string{
		"record_type,name,operation,baseline,candidate,change,severity,confidence,comparable,note",
		"database_metric,DB calls per minute",
		"database_metric,DB transaction p95",
		"android_component_metric,Binder client p95",
		"database_statement,SELECT value FROM sample WHERE id = ?,query,100,125",
		"database_operation,feed.open,Feed,2.00,3.00",
	} {
		if !strings.Contains(csv, expected) {
			t.Fatalf("comparison CSV does not contain %q:\n%s", expected, csv)
		}
	}
}
