package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
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

func TestComparisonMachineFormatsShareScopeProblemAndGateStates(t *testing.T) {
	before, after, absolute := 10.0, 12.0, 2.0
	interval := &analyze.EffectInterval{Lower: 1, Upper: 3, ConfidenceLevel: .95, BaselineGroups: 5, CandidateGroups: 6, Method: "welch_unpaired"}
	comparison := analyze.Comparison{
		SchemaVersion: analyze.ComparisonSchemaVersion,
		Outcome:       analyze.ChangeRegressed,
		Scope: analyze.ComparisonScope{
			Comparability:       analyze.ScenarioPartial,
			Outcome:             analyze.ChangeRegressed,
			Baseline:            analyze.ScenarioCoverage{Total: 20, Matched: 10, TotalKnown: true, TotalDurationMS: 5000, MatchedDurationMS: 3000, DurationKnown: true},
			Candidate:           analyze.ScenarioCoverage{Matched: 12, TotalKnown: false, MatchedDurationMS: 2500, DurationKnown: false},
			ExactMatchGroups:    2,
			CommonSubpathGroups: 1,
			Changes: []analyze.MetricChange{{
				Name: "Operation mean duration", Domain: analyze.DomainLatency, Unit: "ms",
				Eligibility: analyze.MetricEligibility{Domain: analyze.DomainLatency, State: analyze.EligibilityEligible},
				Before:      &before, After: &after, Absolute: &absolute, Change: analyze.ChangeRegressed,
				Evidence: analyze.EvidenceObserved, Confidence: analyze.ConfidenceLimited, Samples: 10, Interval: interval, Scope: "root",
			}},
		},
		ProblemComparison: analyze.ProblemComparison{Deltas: []analyze.ProblemDelta{{
			Fingerprint: "chat", Observation: analyze.ObservationTargetNotExercised,
			Change: analyze.ChangeInsufficientData, Evidence: analyze.EvidenceObserved,
			Confidence: analyze.ConfidenceLimited,
		}}},
	}
	gate := analyze.GateResult{
		Status: analyze.GateInconclusive, Failed: true,
		Checks: []analyze.GateCheck{{Status: analyze.GateInconclusive, Message: "partial scenario coverage"}},
	}

	var jsonOutput bytes.Buffer
	if err := writeComparisonJSON(&jsonOutput, comparison, gate); err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(jsonOutput.Bytes(), &document); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, jsonOutput.String())
	}
	if document["schema_version"] != analyze.ComparisonSchemaVersion {
		t.Fatalf("schema = %#v", document["schema_version"])
	}
	gateObject, ok := document["gate"].(map[string]any)
	if !ok || gateObject["status"] != string(analyze.GateInconclusive) {
		t.Fatalf("gate = %#v", document["gate"])
	}

	var csvOutput bytes.Buffer
	if err := writeComparisonCSV(&csvOutput, comparison, gate); err != nil {
		t.Fatal(err)
	}
	records, err := csv.NewReader(strings.NewReader(csvOutput.String())).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	header := csvHeaderIndex(t, records[0])
	rows := map[string][]string{}
	for _, row := range records[1:] {
		rows[row[header["record_type"]]] = row
	}
	scope := rows["comparison_scope"]
	if scope[header["comparability"]] != string(analyze.ScenarioPartial) || scope[header["change"]] != string(analyze.ChangeRegressed) ||
		scope[header["baseline_total"]] != "20" || scope[header["candidate_total"]] != "" ||
		scope[header["baseline_duration_matched_ms"]] != "3000" || scope[header["baseline_duration_total_ms"]] != "5000" ||
		scope[header["candidate_duration_matched_ms"]] != "2500" || scope[header["candidate_duration_total_ms"]] != "" ||
		scope[header["exact_match_groups"]] != "2" || scope[header["common_subpath_groups"]] != "1" {
		t.Fatalf("scope row = %#v", scope)
	}
	metric := rows["scope_metric"]
	if metric[header["eligibility"]] != string(analyze.EligibilityEligible) || metric[header["evidence"]] != string(analyze.EvidenceObserved) {
		t.Fatalf("metric row = %#v", metric)
	}
	if metric[header["interval_lower"]] != "1" || metric[header["interval_upper"]] != "3" ||
		metric[header["baseline_groups"]] != "5" || metric[header["candidate_groups"]] != "6" || metric[header["metric_scope"]] != "root" {
		t.Fatalf("metric interval row = %#v", metric)
	}
	problem := rows["problem_transition"]
	if problem[header["observation"]] != string(analyze.ObservationTargetNotExercised) || problem[header["change"]] != string(analyze.ChangeInsufficientData) {
		t.Fatalf("problem row = %#v", problem)
	}
	gateRow := rows["gate_check"]
	if gateRow[header["gate_status"]] != string(analyze.GateInconclusive) {
		t.Fatalf("gate row = %#v", gateRow)
	}

	var cliOutput bytes.Buffer
	printComparisonScope(&cliOutput, comparison, gate, true)
	for _, expected := range []string{"В сопоставленной части стало хуже", "сопоставлена часть сценариев", "result=regressed", "scope=partial", "change=regressed", "eligibility=eligible", "gate=inconclusive"} {
		if !strings.Contains(cliOutput.String(), expected) {
			t.Fatalf("CLI output does not contain %q:\n%s", expected, cliOutput.String())
		}
	}
	if got := comparisonScopeCLIText(analyze.ComparisonScope{Comparability: analyze.ScenarioNone}); !strings.Contains(got, "запишите одинаковый сценарий") {
		t.Fatalf("none scope guidance = %q", got)
	}
	if got := comparisonScopeCLIText(analyze.ComparisonScope{Comparability: analyze.ScenarioPartial, CommonSubpathGroups: 1}); !strings.Contains(got, "общая последовательность действий") || !strings.Contains(got, "метрики целых сценариев не сравниваются") {
		t.Fatalf("common-subpath guidance = %q", got)
	}
}

func csvHeaderIndex(t *testing.T, header []string) map[string]int {
	t.Helper()
	result := make(map[string]int, len(header))
	for index, name := range header {
		result[name] = index
	}
	for _, required := range []string{"record_type", "change", "comparability", "eligibility", "observation", "evidence", "baseline_total", "candidate_total", "gate_status", "exact_match_groups", "common_subpath_groups", "baseline_duration_matched_ms", "candidate_duration_matched_ms", "baseline_duration_total_ms", "candidate_duration_total_ms", "interval_lower", "interval_upper", "metric_scope", "stage"} {
		if _, ok := result[required]; !ok {
			t.Fatalf("missing CSV column %q: %#v", required, header)
		}
	}
	return result
}
