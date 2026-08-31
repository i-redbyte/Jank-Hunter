package analyze

import (
	"strconv"
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func TestCompareDatabaseNormalizesLoadAndMatchesCanonicalStatements(t *testing.T) {
	baseline := databaseComparisonSummary(60_000, 100, 80, 20, 5, 20, 2_000_000, 100)
	candidate := databaseComparisonSummary(120_000, 200, 140, 60, 20, 40, 6_000_000, 200)
	baseline.DatabaseAnalysis.Statements = []DatabaseStatementStats{{
		Query: "SELECT value FROM sample WHERE id = ?", Operation: "query",
		Overall:      DatabaseExecutionStats{Calls: 100, Failures: 5, TotalDurationUS: 2_000_000},
		Main:         DatabaseExecutionStats{Calls: 80, P95DurationUS: 20_000, MaxDurationUS: 25_000},
		Background:   DatabaseExecutionStats{Calls: 20, P95DurationUS: 40_000, MaxDurationUS: 60_000},
		RapidRepeats: 20,
		Contexts:     []DatabaseStatementContextStats{{Source: "FirstDao.load", Framework: "Room"}},
	}}
	candidate.DatabaseAnalysis.Statements = []DatabaseStatementStats{{
		Query: "SELECT value FROM sample WHERE id = ?", Operation: "query",
		Overall:      DatabaseExecutionStats{Calls: 200, Failures: 20, TotalDurationUS: 6_000_000},
		Main:         DatabaseExecutionStats{Calls: 140, P95DurationUS: 35_000, MaxDurationUS: 45_000},
		Background:   DatabaseExecutionStats{Calls: 60, P95DurationUS: 70_000, MaxDurationUS: 90_000},
		RapidRepeats: 40,
		Contexts:     []DatabaseStatementContextStats{{Source: "SecondDao.load", Framework: "Room"}},
	}}

	database := Compare(baseline, candidate).Database
	if _, gated := deltasByName(Compare(baseline, candidate).Deltas)["DB main-thread p95"]; gated {
		t.Fatal("new DB metrics entered the generic CI gate before dedicated thresholds were stabilized")
	}
	metrics := deltasByName(database.Metrics)
	for _, name := range []string{
		"DB main-thread p95", "DB background p95", "DB calls per minute",
		"DB calls per operation", "DB main-thread rate", "DB failure rate",
		"DB rapid-repeat rate", "DB wall per minute", "DB wall per operation",
	} {
		if _, ok := metrics[name]; !ok {
			t.Fatalf("missing %q: %+v", name, database.Metrics)
		}
	}
	if got := metrics["DB calls per minute"]; got.BaselineValue != 100 || got.CandidateValue != 100 {
		t.Fatalf("duration-normalized DB calls = %+v", got)
	}
	if got := metrics["DB calls per operation"]; got.BaselineValue != 1 || got.CandidateValue != 1 {
		t.Fatalf("operation-normalized DB calls = %+v", got)
	}
	if len(database.Statements) != 1 {
		t.Fatalf("canonical statement deltas = %+v", database.Statements)
	}
	row := database.Statements[0]
	if !row.Comparable || row.BaselineCallsPerMinute != 100 || row.CandidateCallsPerMinute != 100 ||
		row.BaselineMainP95US != 20_000 || row.CandidateMainP95US != 35_000 ||
		row.BaselineFailureRatePct != 5 || row.CandidateFailureRatePct != 10 {
		t.Fatalf("statement delta = %+v", row)
	}
}

func TestCompareOperationDatabaseLoadUsesMatchedOperationExposure(t *testing.T) {
	baseline := Summary{OperationAnalysis: &OperationAnalysis{Operations: []OperationStats{{
		Operation: "feed.open", Kind: "user", Screen: "Feed", Count: 20,
		CorrelatedDatabase: 40, CorrelatedDatabaseMain: 10, CorrelatedDatabaseErrors: 2,
		CorrelatedDatabaseUS: 2_000_000,
	}}}}
	candidate := Summary{OperationAnalysis: &OperationAnalysis{Operations: []OperationStats{{
		Operation: "feed.open", Kind: "user", Screen: "Feed", Count: 40,
		CorrelatedDatabase: 120, CorrelatedDatabaseMain: 60, CorrelatedDatabaseErrors: 12,
		CorrelatedDatabaseUS: 12_000_000,
	}}}}

	rows := compareOperationAnalysis(baseline, candidate)
	if len(rows) != 1 {
		t.Fatalf("operation deltas = %+v", rows)
	}
	row := rows[0]
	if !row.DatabaseComparable || row.BaselineDatabaseCallsPerOperation != 2 ||
		row.CandidateDatabaseCallsPerOperation != 3 ||
		row.BaselineDatabaseMainRatePct != 25 || row.CandidateDatabaseMainRatePct != 50 ||
		row.BaselineDatabaseFailureRatePct != 5 || row.CandidateDatabaseFailureRatePct != 10 ||
		row.BaselineDatabaseWallMSPerOperation != 100 || row.CandidateDatabaseWallMSPerOperation != 300 {
		t.Fatalf("operation DB delta = %+v", row)
	}
}

func TestCompareDatabaseDoesNotTreatMissingTelemetryAsZero(t *testing.T) {
	baseline := Summary{
		CollectorSessions: 1,
		CollectorFlagsAny: uint64(jhlog.CollectorDatabase),
		CollectorFlagsAll: uint64(jhlog.CollectorDatabase),
		DurationMS:        60_000,
	}
	candidate := databaseComparisonSummary(60_000, 100, 100, 0, 0, 0, 1_000_000, 100)

	database := Compare(baseline, candidate).Database
	if database.Comparable {
		t.Fatalf("missing baseline observations became comparable: %+v", database)
	}
	for _, metric := range database.Metrics {
		if metric.Comparable || metric.Severity != "ok" || !strings.Contains(metric.ComparisonNote, "не зафиксированы") {
			t.Fatalf("missing DB metric became a regression: %+v", metric)
		}
	}
}

func TestCompareDatabaseLatencyRequiresThreadSpecificSample(t *testing.T) {
	baseline := databaseComparisonSummary(60_000, 20, 1, 19, 0, 0, 2_000_000, 20)
	candidate := databaseComparisonSummary(60_000, 20, 1, 19, 0, 0, 2_000_000, 20)
	baseline.DatabaseAnalysis.Main.P95DurationUS = 1_000
	candidate.DatabaseAnalysis.Main.P95DurationUS = 100_000

	metric := deltasByName(Compare(baseline, candidate).Database.Metrics)["DB main-thread p95"]
	if metric.Comparable || metric.Severity != "ok" || !strings.Contains(metric.ComparisonNote, "20") {
		t.Fatalf("small main-thread sample produced a latency regression: %+v", metric)
	}
}

func TestCompareDatabaseTransactionsUsesCompletedLifecycleAndExposure(t *testing.T) {
	baseline := databaseComparisonSummary(60_000, 0, 0, 0, 0, 0, 0, 40)
	candidate := databaseComparisonSummary(60_000, 0, 0, 0, 0, 0, 0, 40)
	baseline.DatabaseAnalysis.Transactions = &DatabaseTransactionAnalysis{
		Events: 80, Begun: 40, Completed: 40, Success: 38, Rollbacks: 1, Failures: 1,
		MainThread: 10, Background: 30, P95DurationUS: 40_000,
		TotalStatementCount: 200,
	}
	candidate.DatabaseAnalysis.Transactions = &DatabaseTransactionAnalysis{
		Events: 84, Begun: 42, Completed: 40, Incomplete: 2, Success: 32, Rollbacks: 3, Failures: 5,
		MainThread: 20, Background: 22, P95DurationUS: 80_000,
		TotalStatementCount: 400,
	}

	database := Compare(baseline, candidate).Database
	if !database.Comparable {
		t.Fatalf("transaction-only DB telemetry is not comparable: %+v", database)
	}
	metrics := deltasByName(database.Metrics)
	for _, name := range []string{
		"DB transactions per minute", "DB transaction p95", "DB main-thread transaction rate",
		"DB transaction failure rate", "DB transaction rollback rate",
		"DB incomplete transaction rate", "DB statements per transaction",
	} {
		if _, ok := metrics[name]; !ok {
			t.Fatalf("missing transaction metric %q: %+v", name, database.Metrics)
		}
	}
	if got := metrics["DB transaction p95"]; !got.Comparable || got.BaselineValue != 40_000 || got.CandidateValue != 80_000 {
		t.Fatalf("transaction p95 delta = %+v", got)
	}
	if got := metrics["DB statements per transaction"]; !got.Comparable || got.BaselineValue != 5 || got.CandidateValue != 10 {
		t.Fatalf("statement fan-out delta = %+v", got)
	}
	if got := metrics["DB incomplete transaction rate"]; !got.Comparable || got.BaselineValue != 0 || got.CandidateValue == 0 {
		t.Fatalf("incomplete transaction delta = %+v", got)
	}
}

func TestCompareDatabaseScenariosNormalizesRepeatedCallsByObservedScopes(t *testing.T) {
	baseline := databaseComparisonSummary(60_000, 40, 0, 40, 0, 0, 40_000, 40)
	candidate := databaseComparisonSummary(60_000, 80, 0, 80, 0, 0, 80_000, 40)
	baseline.DatabaseAnalysis.Scenarios = DatabaseScenarioAnalysis{
		ObservedOperationScopes: 40,
		Candidates: []DatabaseScenarioStats{{
			Kind: "possible_n_plus_one_or_duplicate", ScopeKind: "operation", EstimatedCalls: 40,
		}},
	}
	candidate.DatabaseAnalysis.Scenarios = DatabaseScenarioAnalysis{
		ObservedOperationScopes: 40,
		Candidates: []DatabaseScenarioStats{{
			Kind: "possible_n_plus_one_or_duplicate", ScopeKind: "operation", EstimatedCalls: 120,
		}},
	}

	metrics := deltasByName(Compare(baseline, candidate).Database.Metrics)
	repeated := metrics["DB repeated calls per operation scope"]
	if !repeated.Comparable || repeated.BaselineValue != 1 || repeated.CandidateValue != 3 ||
		!strings.Contains(repeated.ComparisonNote, "40") {
		t.Fatalf("scenario exposure delta = %+v", repeated)
	}
}

func TestCompareDatabaseScenariosRejectsBoundedDataLoss(t *testing.T) {
	baseline := databaseComparisonSummary(60_000, 40, 0, 40, 0, 0, 40_000, 40)
	candidate := databaseComparisonSummary(60_000, 40, 0, 40, 0, 0, 40_000, 40)
	baseline.DatabaseAnalysis.Scenarios = DatabaseScenarioAnalysis{
		ObservedOperationScopes: 40, DroppedEvents: 1,
		Candidates: []DatabaseScenarioStats{{
			Kind: "possible_n_plus_one_or_duplicate", ScopeKind: "operation", EstimatedCalls: 40,
		}},
	}
	candidate.DatabaseAnalysis.Scenarios = DatabaseScenarioAnalysis{
		ObservedOperationScopes: 40,
		Candidates: []DatabaseScenarioStats{{
			Kind: "possible_n_plus_one_or_duplicate", ScopeKind: "operation", EstimatedCalls: 40,
		}},
	}

	metric := deltasByName(Compare(baseline, candidate).Database.Metrics)["DB repeated calls per operation scope"]
	if metric.Comparable || !strings.Contains(metric.ComparisonNote, "bounded") {
		t.Fatalf("incomplete scenario evidence became comparable: %+v", metric)
	}
}

func TestCompareDatabaseStatementSeverityIncludesNormalizedFrequency(t *testing.T) {
	baseline := databaseComparisonSummary(60_000, 100, 50, 50, 0, 0, 1_000_000, 100)
	candidate := databaseComparisonSummary(60_000, 200, 100, 100, 0, 0, 2_000_000, 100)
	baseline.DatabaseAnalysis.Statements = []DatabaseStatementStats{{
		Query: "SELECT value FROM sample WHERE id = ?", Operation: "query",
		Overall:    DatabaseExecutionStats{Calls: 100, TotalDurationUS: 1_000_000},
		Main:       DatabaseExecutionStats{Calls: 50, P95DurationUS: 10_000},
		Background: DatabaseExecutionStats{Calls: 50, P95DurationUS: 10_000},
	}}
	candidate.DatabaseAnalysis.Statements = []DatabaseStatementStats{{
		Query: "SELECT value FROM sample WHERE id = ?", Operation: "query",
		Overall:    DatabaseExecutionStats{Calls: 200, TotalDurationUS: 2_000_000},
		Main:       DatabaseExecutionStats{Calls: 100, P95DurationUS: 10_000},
		Background: DatabaseExecutionStats{Calls: 100, P95DurationUS: 10_000},
	}}

	row := Compare(baseline, candidate).Database.Statements[0]
	if row.Severity != "high" {
		t.Fatalf("normalized statement frequency regression was not prioritized: %+v", row)
	}
}

func BenchmarkCompareDatabaseAnalysisBoundedStatements(b *testing.B) {
	baseline := databaseComparisonSummary(60_000, 409_600, 204_800, 204_800, 0, 0, 4_096_000_000, 100_000)
	candidate := databaseComparisonSummary(60_000, 409_600, 204_800, 204_800, 0, 0, 4_096_000_000, 100_000)
	baseline.DatabaseAnalysis.Statements = benchmarkDatabaseStatements(4_096)
	candidate.DatabaseAnalysis.Statements = benchmarkDatabaseStatements(4_096)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = compareDatabaseAnalysis(baseline, candidate)
	}
}

func benchmarkDatabaseStatements(count int) []DatabaseStatementStats {
	result := make([]DatabaseStatementStats, count)
	for index := range result {
		result[index] = DatabaseStatementStats{
			Query:      "SELECT value FROM sample_" + strconv.Itoa(index) + " WHERE id = ?",
			Operation:  "query",
			Overall:    DatabaseExecutionStats{Calls: 100, TotalDurationUS: 1_000_000},
			Main:       DatabaseExecutionStats{Calls: 50, P95DurationUS: 10_000},
			Background: DatabaseExecutionStats{Calls: 50, P95DurationUS: 10_000},
		}
	}
	return result
}

func databaseComparisonSummary(
	durationMS, calls, main, background, failures, repeats, wallUS, operations uint64,
) Summary {
	return Summary{
		LogCount:          5,
		EventCount:        500,
		DurationMS:        durationMS,
		CollectorSessions: 1,
		CollectorFlagsAny: uint64(jhlog.CollectorDatabase),
		CollectorFlagsAll: uint64(jhlog.CollectorDatabase),
		DatabaseAnalysis: &DatabaseAnalysis{
			Overall: DatabaseExecutionStats{Calls: calls, Failures: failures, TotalDurationUS: wallUS},
			Main:    DatabaseExecutionStats{Calls: main, P95DurationUS: 20_000, MaxDurationUS: 30_000},
			Background: DatabaseExecutionStats{
				Calls: background, P95DurationUS: 40_000, MaxDurationUS: 60_000,
			},
			RapidRepeats: repeats,
		},
		OperationAnalysis: &OperationAnalysis{Completed: operations},
	}
}
