package analyze

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func TestDatabaseCoverageJSONUsesSQLCallTerminology(t *testing.T) {
	raw, err := json.Marshal(DatabaseCoverage{KnownSQLCalls: 7})
	if err != nil {
		t.Fatal(err)
	}
	encoded := string(raw)
	if !strings.Contains(encoded, `"known_sql_calls":7`) || strings.Contains(encoded, "known_query_calls") {
		t.Fatalf("database coverage JSON uses stale query-only terminology: %s", encoded)
	}
}

func TestDatabaseCoverageDistinguishesDisabledNoHooksNoObservationsAndDegraded(t *testing.T) {
	tests := []struct {
		name        string
		summary     Summary
		diagnostics *InstrumentationDiagnostics
		wantStatus  string
		wantAction  string
		wantDrops   [3]uint64
	}{
		{
			name:       "disabled",
			summary:    Summary{CollectorSessions: 1},
			wantStatus: "disabled", wantAction: "databaseTracing",
		},
		{
			name: "no hooks",
			summary: Summary{
				CollectorSessions: 1,
				CollectorFlagsAny: uint64(jhlog.CollectorDatabase),
				CollectorFlagsAll: uint64(jhlog.CollectorDatabase),
			},
			diagnostics: &InstrumentationDiagnostics{Available: true, ClassCount: 10},
			wantStatus:  "no_hooks", wantAction: "includePackages",
		},
		{
			name: "no observations",
			summary: Summary{
				CollectorSessions: 1,
				CollectorFlagsAny: uint64(jhlog.CollectorDatabase),
				CollectorFlagsAll: uint64(jhlog.CollectorDatabase),
			},
			diagnostics: &InstrumentationDiagnostics{
				Available: true, ClassCount: 10,
				Hooks: []InstrumentationHookSummary{{Intent: "database.room.query", Count: 4}},
			},
			wantStatus: "no_observations", wantAction: "сценарий",
		},
		{
			name: "degraded details",
			summary: Summary{
				CollectorSessions: 1,
				CollectorFlagsAny: uint64(jhlog.CollectorDatabase),
				CollectorFlagsAll: uint64(jhlog.CollectorDatabase),
				DatabaseAnalysis: &DatabaseAnalysis{
					Overall: DatabaseExecutionStats{Calls: 100}, KnownSQLCalls: 60,
					DroppedStatementEvents: 5, DroppedContextEvents: 7,
					DroppedDBIntervals: 2, DroppedTransactionIntervals: 3,
					DroppedUIWindows: 5, DroppedStallIntervals: 7, DroppedRelatedIntervals: 11,
					Scenarios: DatabaseScenarioAnalysis{DroppedEvents: 13, DroppedCandidates: 17},
				},
			},
			wantStatus: "degraded", wantAction: "SQL-шаблон", wantDrops: [3]uint64{5, 13, 17},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			coverage := buildDatabaseCoverage(test.summary, test.diagnostics)
			if coverage.Status != test.wantStatus || !strings.Contains(coverage.Action, test.wantAction) {
				t.Fatalf("coverage = %+v", coverage)
			}
			if coverage.DroppedCorrelationEvents != test.wantDrops[0] ||
				coverage.DroppedScenarioEvents != test.wantDrops[1] ||
				coverage.DroppedScenarioCandidates != test.wantDrops[2] {
				t.Fatalf("coverage drops = correlation %d, scenario events %d, candidates %d",
					coverage.DroppedCorrelationEvents, coverage.DroppedScenarioEvents,
					coverage.DroppedScenarioCandidates)
			}
		})
	}
}

func TestDatabaseCoverageExplainsImpactWithoutInternalLossCounters(t *testing.T) {
	coverage := buildDatabaseCoverage(Summary{
		CollectorSessions: 1,
		CollectorFlagsAny: uint64(jhlog.CollectorDatabase),
		CollectorFlagsAll: uint64(jhlog.CollectorDatabase),
		DatabaseAnalysis: &DatabaseAnalysis{
			Overall: DatabaseExecutionStats{Calls: 100}, KnownSQLCalls: 60,
			DroppedStatementEvents: 25, DroppedContextEvents: 17,
		},
	}, nil)

	visible := strings.ToLower(coverage.Explanation + " " + coverage.Action)
	for _, internal := range []string{"dropped", "detail", "bounded", "statement/context", "25", "17", "unknown"} {
		if strings.Contains(visible, internal) {
			t.Fatalf("database coverage exposes internal counter %q: %s", internal, visible)
		}
	}
	for _, actionable := range []string{"часть", "sql-шаблон", "место вызова", "повторите"} {
		if !strings.Contains(visible, actionable) {
			t.Fatalf("database coverage misses actionable phrase %q: %s", actionable, visible)
		}
	}
}

func TestDatabaseCoverageCountsOnlyDatabaseDiagnostics(t *testing.T) {
	coverage := buildDatabaseCoverage(Summary{
		CollectorSessions: 1,
		CollectorFlagsAny: uint64(jhlog.CollectorDatabase),
		CollectorFlagsAll: uint64(jhlog.CollectorDatabase),
	}, &InstrumentationDiagnostics{
		Available: true, ClassCount: 4,
		Hooks: []InstrumentationHookSummary{
			{Intent: "database.room.query", Count: 3},
			{Intent: "http.okhttp.execute", Count: 9},
		},
		Decisions: []InstrumentationDecisionSummary{
			{Kind: "disabled", Family: "database", Count: 2},
			{Kind: "unsupported", Family: "database", Count: 4},
			{Kind: "unsupported", Family: "http", Count: 8},
		},
	})

	if coverage.InstrumentedHooks != 3 || coverage.DisabledCandidates != 2 ||
		coverage.UnsupportedCandidates != 4 {
		t.Fatalf("database diagnostics = %+v", coverage)
	}
}

func TestDatabaseCoverageDoesNotDegradeForUnjoinableAuxiliarySignals(t *testing.T) {
	coverage := buildDatabaseCoverage(Summary{
		CollectorSessions: 1,
		CollectorFlagsAny: uint64(jhlog.CollectorDatabase),
		CollectorFlagsAll: uint64(jhlog.CollectorDatabase),
		DatabaseAnalysis: &DatabaseAnalysis{
			Overall: DatabaseExecutionStats{Calls: 1}, KnownSQLCalls: 1,
			DroppedUIWindows: 5, DroppedStallIntervals: 7, DroppedRelatedIntervals: 11,
		},
	}, &InstrumentationDiagnostics{
		Available: true,
		Hooks:     []InstrumentationHookSummary{{Intent: "database.room.query", Count: 1}},
	})

	if coverage.Status != "observed" || coverage.DroppedCorrelationEvents != 0 {
		t.Fatalf("auxiliary correlation changed DB coverage = %+v", coverage)
	}
}
