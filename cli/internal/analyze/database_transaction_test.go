package analyze

import (
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func TestDatabaseTransactionAnalysisJoinsAcrossSegmentsByProcess(t *testing.T) {
	process := jhlog.ID128{1}
	summary := inspectLogsForTest("transactions", []jhlog.Log{
		transactionLog(process, 1, []jhlog.Event{
			transactionEvent(10, 7, 0, jhlog.DatabaseTransactionBegin, jhlog.DatabaseTransactionOutcomeUnknown, 0),
		}),
		transactionLog(process, 1, []jhlog.Event{
			transactionEvent(30, 7, 0, jhlog.DatabaseTransactionTerminal, jhlog.DatabaseTransactionSuccess, 20_000),
		}),
	})

	analysis := summary.DatabaseAnalysis
	if analysis == nil || analysis.Transactions == nil {
		t.Fatal("transaction analysis is absent")
	}
	transactions := analysis.Transactions
	if transactions.Begun != 1 || transactions.Completed != 1 || transactions.Incomplete != 0 ||
		transactions.MissingStart != 0 || transactions.Success != 1 {
		t.Fatalf("transaction lifecycle = %+v", transactions)
	}
	if transactions.P50DurationUS != 20_000 || transactions.P95DurationUS != 20_000 ||
		transactions.MaxDurationUS != 20_000 || len(transactions.Transactions) != 1 {
		t.Fatalf("transaction durations/details = %+v", transactions)
	}
	row := transactions.Transactions[0]
	if !row.Complete || row.TransactionID != 7 || row.Source != "FeedStore.refresh" ||
		row.ProcessInstanceID == "unknown" {
		t.Fatalf("transaction row = %+v", row)
	}
}

func TestDatabaseTransactionAnalysisIsolatesProcessesAndKeepsIncomplete(t *testing.T) {
	firstProcess := jhlog.ID128{1}
	secondProcess := jhlog.ID128{2}
	summary := inspectLogsForTest("transaction-isolation", []jhlog.Log{
		transactionLog(firstProcess, 1, []jhlog.Event{
			transactionEvent(10, 9, 0, jhlog.DatabaseTransactionBegin, jhlog.DatabaseTransactionOutcomeUnknown, 0),
		}),
		transactionLog(secondProcess, 1, []jhlog.Event{
			transactionEvent(20, 9, 0, jhlog.DatabaseTransactionTerminal, jhlog.DatabaseTransactionFailure, 500_000),
		}),
	})

	transactions := summary.DatabaseAnalysis.Transactions
	if transactions.Completed != 1 || transactions.Incomplete != 1 || transactions.MissingStart != 1 ||
		transactions.Failures != 1 {
		t.Fatalf("transaction isolation = %+v", transactions)
	}
	var incomplete *DatabaseTransactionStats
	for index := range transactions.Transactions {
		if !transactions.Transactions[index].Complete {
			incomplete = &transactions.Transactions[index]
			break
		}
	}
	if incomplete == nil || incomplete.DurationUS != 0 || incomplete.Outcome != "incomplete" {
		t.Fatalf("incomplete transaction = %+v", incomplete)
	}
}

func TestDatabaseTransactionAnalysisClassifiesNestedRollbackAndStatementFanout(t *testing.T) {
	event := transactionEvent(
		200,
		2,
		1,
		jhlog.DatabaseTransactionTerminal,
		jhlog.DatabaseTransactionRollback,
		180_000,
	)
	event.DatabaseTransaction.StatementCount = 42
	event.DatabaseTransaction.ReadCount = 40
	event.DatabaseTransaction.WriteCount = 2
	summary := inspectLogsForTest("nested-rollback", []jhlog.Log{
		transactionLog(jhlog.ID128{1}, 1, []jhlog.Event{
			transactionEvent(20, 2, 1, jhlog.DatabaseTransactionBegin, jhlog.DatabaseTransactionOutcomeUnknown, 0),
			event,
		}),
	})

	transactions := summary.DatabaseAnalysis.Transactions
	if transactions.Rollbacks != 1 || transactions.Nested != 1 || transactions.MaxStatementCount != 42 {
		t.Fatalf("nested rollback stats = %+v", transactions)
	}
	if got := namedValueCount(transactions.Outcomes, "rollback"); got != 1 {
		t.Fatalf("rollback outcome count = %d, values=%+v", got, transactions.Outcomes)
	}
	if got := namedValueCount(transactions.Modes, "immediate"); got != 1 {
		t.Fatalf("immediate mode count = %d, values=%+v", got, transactions.Modes)
	}
}

func TestDatabaseTelemetryUsesOnlyExplicitResultAndPhaseEvidence(t *testing.T) {
	dict := map[uint64]string{1: "SELECT value FROM sample", 2: "SampleDao.load"}
	events := []jhlog.Event{
		{
			Type: jhlog.EventDatabase, TimeMS: 10,
			Database: &jhlog.DatabaseEvent{
				QueryRef: jhlog.LocalSymbol(1), SourceRef: jhlog.LocalSymbol(2),
				Framework: jhlog.DatabaseFrameworkRoom, Operation: jhlog.DatabaseOperationQuery,
				Outcome: jhlog.DatabaseOutcomeSuccess, Boundary: jhlog.DatabaseBoundaryExecute,
				ResultKnown: true, ResultKind: jhlog.DatabaseResultAffectedRows,
				ResultCountBucket: jhlog.DatabaseCountTwoToTen,
				TransactionID:     7, StatementToken: 11,
				PhaseMask:  jhlog.DatabasePhaseLockWait | jhlog.DatabasePhaseExecute,
				LockWaitUS: 4_000, ExecuteUS: 16_000, DurationUS: 25_000,
			},
		},
		{
			Type: jhlog.EventDatabase, TimeMS: 20,
			Database: &jhlog.DatabaseEvent{
				QueryRef: jhlog.LocalSymbol(1), SourceRef: jhlog.LocalSymbol(2),
				Framework: jhlog.DatabaseFrameworkRoom, Operation: jhlog.DatabaseOperationQuery,
				Outcome: jhlog.DatabaseOutcomeFailure, FailureKind: jhlog.DatabaseFailureBusyLocked,
				Boundary: jhlog.DatabaseBoundaryDispatch, DurationUS: 90_000,
			},
		},
	}
	log := jhlog.Log{Dict: dict, Events: events, Result: jhlog.StreamResult{Header: jhlog.SegmentHeader{
		ProcessName: "main", ProcessInstanceID: jhlog.ID128{1}, SessionID: jhlog.ID128{2},
	}}}
	summary := inspectLogsForTest("database-evidence", []jhlog.Log{log})

	telemetry := summary.DatabaseAnalysis.Telemetry
	if telemetry.ResultKnownCalls != 1 || telemetry.TransactionLinkedCalls != 1 ||
		telemetry.PreparedExecutionCalls != 1 || telemetry.PhaseMeasuredCalls != 1 {
		t.Fatalf("database telemetry = %+v", telemetry)
	}
	if telemetry.LockWait.Samples != 1 || telemetry.LockWait.TotalDurationUS != 4_000 ||
		telemetry.Execute.Samples != 1 || telemetry.Execute.TotalDurationUS != 16_000 ||
		telemetry.PoolWait.Samples != 0 || telemetry.Materialize.Samples != 0 {
		t.Fatalf("phase evidence = %+v", telemetry)
	}
	if namedValueCount(telemetry.FailureKinds, "busy_locked") != 1 ||
		namedValueCount(telemetry.ResultCountBuckets, "2_10") != 1 ||
		namedValueCount(telemetry.Boundaries, "execute") != 1 ||
		namedValueCount(telemetry.Boundaries, "dispatch") != 1 {
		t.Fatalf("database taxonomy = %+v", telemetry)
	}
	statementTelemetry := summary.DatabaseAnalysis.Statements[0].Telemetry
	if statementTelemetry.ResultKnownCalls != 1 || statementTelemetry.PhaseMeasuredCalls != 1 {
		t.Fatalf("statement telemetry = %+v", statementTelemetry)
	}
}

func TestDatabaseTransactionRetentionIsBoundedAndKeepsLateFailure(t *testing.T) {
	accumulator := databaseTransactionAccumulator{}
	context := SignalContextStats{Screen: "Feed", Operation: "feed.refresh", Owner: "FeedStore"}
	process := jhlog.ID128{1}
	for index := uint64(1); index <= databaseTransactionDetailLimit+10; index++ {
		accumulator.add(
			&jhlog.DatabaseTransactionEvent{
				SourceRef: jhlog.LocalSymbol(1), TransactionID: index,
				Stage: jhlog.DatabaseTransactionTerminal, Mode: jhlog.DatabaseTransactionDeferred,
				Outcome: jhlog.DatabaseTransactionSuccess, DurationUS: index,
			},
			0, 1, "FeedStore.refresh", context, "main", process, jhlog.ID128{2},
		)
	}
	lateFailureID := uint64(databaseTransactionDetailLimit + 100)
	accumulator.add(
		&jhlog.DatabaseTransactionEvent{
			SourceRef: jhlog.LocalSymbol(1), TransactionID: lateFailureID,
			Stage: jhlog.DatabaseTransactionTerminal, Mode: jhlog.DatabaseTransactionImmediate,
			Outcome: jhlog.DatabaseTransactionFailure, FailureKind: jhlog.DatabaseFailureConstraint,
			DurationUS: 1,
		},
		0, 1, "FeedStore.refresh", context, "main", process, jhlog.ID128{2},
	)

	analysis := accumulator.finalize()
	if len(analysis.Transactions) != databaseTransactionDetailLimit ||
		analysis.DroppedTransactionDetails != 11 || analysis.EvictedTransactionDetails == 0 {
		t.Fatalf("bounded transaction retention = %+v", analysis)
	}
	foundFailure := false
	for _, transaction := range analysis.Transactions {
		if transaction.TransactionID == lateFailureID && transaction.Outcome == "failure" {
			foundFailure = true
			break
		}
	}
	if !foundFailure {
		t.Fatal("late failure was not retained")
	}
}

func TestDatabaseTransactionActiveStateHasHardLimit(t *testing.T) {
	accumulator := databaseTransactionAccumulator{}
	context := SignalContextStats{Screen: "Feed", Operation: "feed.refresh", Owner: "FeedStore"}
	for index := uint64(1); index <= databaseTransactionActiveLimit+1; index++ {
		accumulator.add(
			&jhlog.DatabaseTransactionEvent{
				TransactionID: index, Stage: jhlog.DatabaseTransactionBegin,
				Mode: jhlog.DatabaseTransactionDeferred,
			},
			0, 1, "FeedStore.refresh", context, "main", jhlog.ID128{1}, jhlog.ID128{2},
		)
	}

	analysis := accumulator.finalize()
	if analysis.Begun != databaseTransactionActiveLimit+1 ||
		analysis.Incomplete != databaseTransactionActiveLimit ||
		analysis.DroppedActiveStarts != 1 || len(analysis.Transactions) != databaseTransactionDetailLimit {
		t.Fatalf("bounded active transaction state = %+v", analysis)
	}
}

func TestDatabaseTransactionSteadyStateHasNoPerLifecycleAllocation(t *testing.T) {
	accumulator := databaseTransactionAccumulator{}
	context := SignalContextStats{Screen: "Feed", Operation: "feed.refresh", Owner: "FeedStore"}
	process := jhlog.ID128{1}
	session := jhlog.ID128{2}
	for index := uint64(1); index <= databaseTransactionDetailLimit; index++ {
		accumulator.add(
			&jhlog.DatabaseTransactionEvent{
				TransactionID: index, Stage: jhlog.DatabaseTransactionTerminal,
				Mode: jhlog.DatabaseTransactionDeferred, Outcome: jhlog.DatabaseTransactionSuccess,
				DurationUS: databaseTransactionBackgroundSlowUS + index,
			},
			0, 1, "FeedStore.refresh", context, "main", process, session,
		)
	}
	var transactionID uint64 = databaseTransactionDetailLimit + 1
	begin := jhlog.DatabaseTransactionEvent{Stage: jhlog.DatabaseTransactionBegin, Mode: jhlog.DatabaseTransactionDeferred}
	terminal := jhlog.DatabaseTransactionEvent{
		Stage: jhlog.DatabaseTransactionTerminal, Mode: jhlog.DatabaseTransactionDeferred,
		Outcome: jhlog.DatabaseTransactionSuccess, DurationUS: 1,
	}
	allocations := testing.AllocsPerRun(1_000, func() {
		begin.TransactionID = transactionID
		accumulator.add(
			&begin,
			0, 1, "FeedStore.refresh", context, "main", process, session,
		)
		terminal.TransactionID = transactionID
		accumulator.add(
			&terminal,
			0, 1, "FeedStore.refresh", context, "main", process, session,
		)
		transactionID++
	})
	if allocations != 0 {
		t.Fatalf("steady-state transaction lifecycle allocations = %.2f, want 0", allocations)
	}
	if accumulator.active.count != 0 {
		t.Fatalf("active transaction count = %d, want 0", accumulator.active.count)
	}
	for index := range accumulator.active.slots {
		if accumulator.active.slots[index].value.source != "" {
			t.Fatalf("completed transaction source retained in active slot %d", index)
		}
	}
}

func BenchmarkDatabaseTransactionSteadyState(b *testing.B) {
	accumulator := databaseTransactionAccumulator{}
	context := SignalContextStats{Screen: "Feed", Operation: "feed.refresh", Owner: "FeedStore"}
	process := jhlog.ID128{1}
	session := jhlog.ID128{2}
	for index := uint64(1); index <= databaseTransactionDetailLimit; index++ {
		accumulator.add(
			&jhlog.DatabaseTransactionEvent{
				TransactionID: index, Stage: jhlog.DatabaseTransactionTerminal,
				Mode: jhlog.DatabaseTransactionDeferred, Outcome: jhlog.DatabaseTransactionSuccess,
				DurationUS: databaseTransactionBackgroundSlowUS + index,
			},
			0, 1, "FeedStore.refresh", context, "main", process, session,
		)
	}
	b.ReportAllocs()
	b.ResetTimer()
	begin := jhlog.DatabaseTransactionEvent{Stage: jhlog.DatabaseTransactionBegin, Mode: jhlog.DatabaseTransactionDeferred}
	terminal := jhlog.DatabaseTransactionEvent{
		Stage: jhlog.DatabaseTransactionTerminal, Mode: jhlog.DatabaseTransactionDeferred,
		Outcome: jhlog.DatabaseTransactionSuccess, DurationUS: 1,
	}
	for index := 0; index < b.N; index++ {
		transactionID := uint64(databaseTransactionDetailLimit + index + 1)
		begin.TransactionID = transactionID
		accumulator.add(
			&begin,
			0, 1, "FeedStore.refresh", context, "main", process, session,
		)
		terminal.TransactionID = transactionID
		accumulator.add(
			&terminal,
			0, 1, "FeedStore.refresh", context, "main", process, session,
		)
	}
}

func TestProblemReportExplainsTransactionFailureWithoutCallingTotalDurationContention(t *testing.T) {
	summary := Summary{
		DurationMS: 30_000,
		DatabaseAnalysis: &DatabaseAnalysis{Transactions: &DatabaseTransactionAnalysis{
			Completed: 1, Failures: 1,
			Transactions: []DatabaseTransactionStats{{
				Source: "SyncStore.replace", Process: "main", Screen: "Inbox",
				ContextOperation: "sync.apply", TransactionID: 7, Complete: true,
				Outcome: "failure", FailureKind: "busy_locked", DurationUS: 250_000,
				StatementCount: 12,
			}},
		}},
	}
	report, err := BuildProblemReport(summary)
	if err != nil {
		t.Fatal(err)
	}
	var finding *ProblemFinding
	for index := range report.Problems {
		if report.Problems[index].DetectorID == "io.database_transaction" {
			finding = &report.Problems[index]
			break
		}
	}
	if finding == nil {
		t.Fatalf("transaction finding is absent: %+v", report.Problems)
	}
	if !strings.Contains(finding.WhatHappened, "БД занята или заблокирована") ||
		strings.Contains(strings.ToLower(finding.Why.Summary), "contention") ||
		strings.Contains(strings.ToLower(finding.Why.Summary), "блокировк") {
		t.Fatalf("transaction explanation overclaims evidence: %+v", finding)
	}
}

func transactionLog(process jhlog.ID128, sourceID uint64, events []jhlog.Event) jhlog.Log {
	return jhlog.Log{
		Dict:   map[uint64]string{sourceID: "FeedStore.refresh"},
		Events: events,
		Result: jhlog.StreamResult{Header: jhlog.SegmentHeader{
			ProcessName: "main", ProcessInstanceID: process, SessionID: jhlog.ID128{9},
		}},
	}
}

func transactionEvent(
	timeMS, transactionID, parentID uint64,
	stage jhlog.DatabaseTransactionStage,
	outcome jhlog.DatabaseTransactionOutcome,
	durationUS uint64,
) jhlog.Event {
	return jhlog.Event{
		Type: jhlog.EventDatabaseTransaction, TimeMS: timeMS,
		DatabaseTransaction: &jhlog.DatabaseTransactionEvent{
			SourceRef: jhlog.LocalSymbol(1), TransactionID: transactionID, ParentID: parentID,
			Stage: stage, Mode: jhlog.DatabaseTransactionImmediate,
			Outcome: outcome, DurationUS: durationUS,
		},
	}
}

func namedValueCount(values []NamedValue, name string) uint64 {
	for _, value := range values {
		if value.Name == name {
			return value.Value
		}
	}
	return 0
}
