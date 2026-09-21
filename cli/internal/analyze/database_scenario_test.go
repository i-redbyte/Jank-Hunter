package analyze

import (
	"fmt"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func TestDatabaseScenariosDetectRepeatedOperationAndTransactionBatching(t *testing.T) {
	header := jhlog.DefaultSegmentHeader()
	header.ProcessName = "app"
	header.ProcessInstanceID[0] = 1
	header.SessionID[0] = 2
	dict := map[uint64]string{
		1: "Feed",
		2: "SELECT item FROM feed WHERE id = ?",
		3: "FeedDao.load",
		4: "feed.open",
		5: "INSERT INTO cache(id, value) VALUES (?, ?)",
		6: "CacheDao.put",
	}
	operationContext := jhlog.AttributionContext{
		Present: true, Screen: jhlog.LocalSymbol(1), Owner: jhlog.LocalSymbol(3), OperationID: 7,
	}
	transactionContext := jhlog.AttributionContext{
		Present: true, Screen: jhlog.LocalSymbol(1), Owner: jhlog.LocalSymbol(6),
	}
	events := []jhlog.Event{{
		Type: jhlog.EventOperation, TimeUS: 900_000, TimeMS: 900, Attribution: operationContext,
		Operation: &jhlog.OperationEvent{
			NameRef: jhlog.LocalSymbol(4), ID: 7, Phase: jhlog.OperationPhaseStarted,
			Kind: jhlog.OperationKindUser,
		},
	}}
	for index := 0; index < 4; index++ {
		events = append(events, jhlog.Event{
			Type: jhlog.EventDatabase, TimeUS: 910_000 + uint64(index)*2_000,
			TimeMS: 910 + uint64(index)*2, Attribution: operationContext,
			Flags: uint64(jhlog.FlagThreadMain),
			Database: &jhlog.DatabaseEvent{
				QueryRef: jhlog.LocalSymbol(2), SourceRef: jhlog.LocalSymbol(3),
				StatementFingerprint: 101, Framework: jhlog.DatabaseFrameworkRoom,
				Operation: jhlog.DatabaseOperationQuery, Outcome: jhlog.DatabaseOutcomeSuccess,
				DurationUS: 2_000,
			},
		})
	}
	events = append(events, jhlog.Event{
		Type: jhlog.EventDatabase, TimeUS: 930_000, TimeMS: 930,
		Attribution: jhlog.AttributionContext{
			Present: true, Screen: jhlog.LocalSymbol(1), Owner: jhlog.LocalSymbol(3), OperationID: 8,
		},
		Database: &jhlog.DatabaseEvent{
			QueryRef: jhlog.LocalSymbol(2), SourceRef: jhlog.LocalSymbol(3),
			StatementFingerprint: 101, Framework: jhlog.DatabaseFrameworkRoom,
			Operation: jhlog.DatabaseOperationQuery, Outcome: jhlog.DatabaseOutcomeSuccess,
			DurationUS: 1_000,
		},
	})
	for index := 0; index < 4; index++ {
		events = append(events, jhlog.Event{
			Type: jhlog.EventDatabase, TimeUS: 940_000 + uint64(index)*1_000,
			TimeMS: 940 + uint64(index), Attribution: transactionContext,
			Database: &jhlog.DatabaseEvent{
				QueryRef: jhlog.LocalSymbol(5), SourceRef: jhlog.LocalSymbol(6),
				StatementFingerprint: 202, Framework: jhlog.DatabaseFrameworkRoom,
				Operation: jhlog.DatabaseOperationInsert, Outcome: jhlog.DatabaseOutcomeSuccess,
				TransactionID: 42, ResultKnown: true, ResultKind: jhlog.DatabaseResultAffectedRows,
				ResultCountBucket: jhlog.DatabaseCountOne, DurationUS: 1_500,
			},
		})
	}

	analysis := inspectLogsForTest("database-scenarios", []jhlog.Log{{
		Dict: dict, Events: events, Result: jhlog.StreamResult{Header: header},
	}}).DatabaseAnalysis
	if analysis == nil {
		t.Fatal("database analysis missing")
	}
	if analysis.Scenarios.ObservedOperationScopes != 2 || analysis.Scenarios.ObservedTransactionScopes != 1 {
		t.Fatalf("scope exposure = %+v", analysis.Scenarios)
	}
	repeated := databaseScenarioByKind(analysis.Scenarios.Candidates, "possible_n_plus_one_or_duplicate", "operation")
	if repeated == nil || repeated.ClaimLevel != "hypothesis" || repeated.Query != dict[2] ||
		repeated.AffectedScopes != 1 || repeated.ObservedScopes != 2 || repeated.EstimatedCalls != 4 ||
		repeated.MaxCallsPerScope != 4 || repeated.TotalDurationUS != 8_000 || repeated.CallsPerObservedScope != 2 {
		t.Fatalf("operation repeat = %+v", repeated)
	}
	batch := databaseScenarioByKind(analysis.Scenarios.Candidates, "batch_candidate", "transaction")
	if batch == nil || batch.Query != dict[5] || batch.TransactionLinkedScopes != 1 ||
		batch.EstimatedCalls != 4 || batch.TotalDurationUS != 6_000 {
		t.Fatalf("transaction batch = %+v", batch)
	}
}

func TestDatabaseScenarioScopeIsProcessIsolated(t *testing.T) {
	logs := make([]jhlog.Log, 0, 2)
	for process := byte(1); process <= 2; process++ {
		header := jhlog.DefaultSegmentHeader()
		header.ProcessInstanceID[0] = process
		header.SessionID[0] = process
		events := make([]jhlog.Event, 0, 2)
		for index := 0; index < 2; index++ {
			events = append(events, jhlog.Event{
				Type: jhlog.EventDatabase, TimeUS: 1_000_000 + uint64(index),
				Attribution: jhlog.AttributionContext{Present: true, OperationID: 9},
				Database: &jhlog.DatabaseEvent{
					StatementFingerprint: 777, Operation: jhlog.DatabaseOperationQuery,
					Outcome: jhlog.DatabaseOutcomeSuccess, DurationUS: 1,
				},
			})
		}
		logs = append(logs, jhlog.Log{Events: events, Result: jhlog.StreamResult{Header: header}})
	}
	analysis := inspectLogsForTest("database-scenario-process", logs).DatabaseAnalysis
	if analysis == nil || analysis.Scenarios.ObservedOperationScopes != 2 || len(analysis.Scenarios.Candidates) != 0 {
		t.Fatalf("process-isolated scenarios = %+v", analysis)
	}
}

func TestDatabaseScenarioKeepsSourceAttributionSeparate(t *testing.T) {
	accumulator := newDatabaseScenarioAccumulator(8)
	for sourceIndex, source := range []string{"FirstDao.load", "SecondDao.load"} {
		statement := databaseStatementKey{query: "SELECT item", operation: "query"}
		context := databaseContextKey{
			statement: statement, source: source, contextOperation: "feed.open",
			operationID: uint64(sourceIndex + 1), processInstanceID: jhlog.ID128{1},
		}
		for call := 0; call < 4; call++ {
			accumulator.add(statement, context, &jhlog.DatabaseEvent{
				StatementFingerprint: 1, Operation: jhlog.DatabaseOperationQuery,
				Outcome: jhlog.DatabaseOutcomeSuccess, DurationUS: 1,
			}, 0, 1, uint64(sourceIndex*10+call))
		}
	}

	analysis := accumulator.finalize()
	if len(analysis.Candidates) != 2 || analysis.Candidates[0].Source == analysis.Candidates[1].Source {
		t.Fatalf("source attribution was merged: %+v", analysis.Candidates)
	}
}

func TestDatabaseScenarioScopeCardinalityBecomesFixedSizeApproximation(t *testing.T) {
	var counter databaseDistinctScopeCounter
	const scopes = 10_000
	for id := uint64(1); id <= scopes; id++ {
		counter.add(databaseScenarioScopeHash(databaseScenarioScopeKey{
			processInstanceID: jhlog.ID128{1}, id: id, kind: databaseScenarioOperationScope,
		}))
	}
	estimate := counter.count()
	if !counter.approximate || len(counter.exact) != databaseScenarioExactScopeCap ||
		estimate < scopes*9/10 || estimate > scopes*11/10 {
		t.Fatalf("bounded cardinality = approximate %t, exact %d, estimate %d",
			counter.approximate, len(counter.exact), estimate)
	}
}

func TestDatabaseScenarioRetentionKeepsLateRepeatedScope(t *testing.T) {
	accumulator := newDatabaseScenarioAccumulator(2)
	processID := jhlog.ID128{1}
	for index := 0; index < 32; index++ {
		query := fmt.Sprintf("SELECT %d", index)
		statement := databaseStatementKey{query: query, operation: "query"}
		context := databaseContextKey{
			statement: statement, source: "Dao.load", operationID: uint64(index + 1),
			processInstanceID: processID,
		}
		accumulator.add(statement, context, &jhlog.DatabaseEvent{
			StatementFingerprint: uint64(index + 1), Operation: jhlog.DatabaseOperationQuery,
			Outcome: jhlog.DatabaseOutcomeSuccess, DurationUS: 1,
		}, 0, 1, uint64(index+1))
	}
	lateStatement := databaseStatementKey{query: "SELECT late", operation: "query"}
	lateContext := databaseContextKey{
		statement: lateStatement, source: "LateDao.load", operationID: 999,
		processInstanceID: processID,
	}
	for index := 0; index < 6; index++ {
		accumulator.add(lateStatement, lateContext, &jhlog.DatabaseEvent{
			StatementFingerprint: 999, Operation: jhlog.DatabaseOperationQuery,
			Outcome: jhlog.DatabaseOutcomeSuccess, DurationUS: 1_000,
		}, 0, 1, uint64(100+index))
	}
	analysis := accumulator.finalize()
	late := databaseScenarioByKind(analysis.Candidates, "possible_n_plus_one_or_duplicate", "operation")
	if late == nil || late.Query != "SELECT late" || late.EstimatedCalls < 6 || analysis.EvictedGroups == 0 {
		t.Fatalf("late heavy scenario = %+v", analysis)
	}
}

func TestDatabaseScenarioSequenceIncludesTimestampZero(t *testing.T) {
	accumulator := newDatabaseScenarioAccumulator(1)
	statement := databaseStatementKey{query: "SELECT item", operation: "query"}
	context := databaseContextKey{statement: statement, operationID: 1, processInstanceID: jhlog.ID128{1}}
	event := &jhlog.DatabaseEvent{
		StatementFingerprint: 1, Operation: jhlog.DatabaseOperationQuery,
		Outcome: jhlog.DatabaseOutcomeSuccess, DurationUS: 1,
	}
	for _, timeUS := range []uint64{0, 10, 20, 30} {
		accumulator.add(statement, context, event, 0, 1, timeUS)
	}

	analysis := accumulator.finalize()
	if len(analysis.Candidates) != 1 || analysis.Candidates[0].MaxSequenceSpanUS != 30 {
		t.Fatalf("timestamp-zero sequence = %+v", analysis)
	}
}

func TestDatabaseScenarioEvictionCountsEveryDiscardedEvent(t *testing.T) {
	accumulator := newDatabaseScenarioAccumulator(1)
	firstStatement := databaseStatementKey{query: "SELECT first", operation: "query"}
	firstContext := databaseContextKey{
		statement: firstStatement, operationID: 1, processInstanceID: jhlog.ID128{1},
	}
	firstEvent := &jhlog.DatabaseEvent{
		StatementFingerprint: 1, Operation: jhlog.DatabaseOperationQuery,
		Outcome: jhlog.DatabaseOutcomeSuccess, DurationUS: 1,
	}
	for index := 0; index < 4; index++ {
		accumulator.add(firstStatement, firstContext, firstEvent, 0, 1, uint64(index))
	}

	secondStatement := databaseStatementKey{query: "SELECT second", operation: "query"}
	secondContext := databaseContextKey{
		statement: secondStatement, operationID: 2, processInstanceID: jhlog.ID128{1},
	}
	secondEvent := &jhlog.DatabaseEvent{
		StatementFingerprint: 2, Operation: jhlog.DatabaseOperationQuery,
		Outcome: jhlog.DatabaseOutcomeSuccess, DurationUS: 1,
	}
	for index := 0; index < 5; index++ {
		accumulator.add(secondStatement, secondContext, secondEvent, 0, 1, uint64(10+index))
	}

	analysis := accumulator.finalize()
	if analysis.DroppedEvents != 8 || analysis.EvictedGroups != 1 ||
		len(analysis.Candidates) != 1 || analysis.Candidates[0].Query != "SELECT second" {
		t.Fatalf("eviction accounting = %+v", analysis)
	}
}

func TestDatabaseScenarioSteadyStateDoesNotAllocate(t *testing.T) {
	accumulator := newDatabaseScenarioAccumulator(1)
	statement := databaseStatementKey{query: "SELECT item", operation: "query"}
	context := databaseContextKey{statement: statement, operationID: 1, processInstanceID: jhlog.ID128{1}}
	event := &jhlog.DatabaseEvent{
		StatementFingerprint: 1, Operation: jhlog.DatabaseOperationQuery,
		Outcome: jhlog.DatabaseOutcomeSuccess, DurationUS: 1,
	}
	accumulator.add(statement, context, event, 0, 1, 1)
	allocations := testing.AllocsPerRun(1_000, func() {
		accumulator.add(statement, context, event, 0, 1, 2)
	})
	if allocations != 0 {
		t.Fatalf("steady-state allocations = %f, want 0", allocations)
	}
}

func BenchmarkDatabaseScenarioSteadyState(b *testing.B) {
	accumulator := newDatabaseScenarioAccumulator(1)
	statement := databaseStatementKey{query: "SELECT item", operation: "query"}
	context := databaseContextKey{statement: statement, operationID: 1, processInstanceID: jhlog.ID128{1}}
	event := &jhlog.DatabaseEvent{
		StatementFingerprint: 1, Operation: jhlog.DatabaseOperationQuery,
		Outcome: jhlog.DatabaseOutcomeSuccess, DurationUS: 1,
	}
	accumulator.add(statement, context, event, 0, 1, 1)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		accumulator.add(statement, context, event, 0, 1, uint64(index+2))
	}
}

func databaseScenarioByKind(
	values []DatabaseScenarioStats,
	kind, scope string,
) *DatabaseScenarioStats {
	for index := range values {
		if values[index].Kind == kind && values[index].ScopeKind == scope {
			return &values[index]
		}
	}
	return nil
}
