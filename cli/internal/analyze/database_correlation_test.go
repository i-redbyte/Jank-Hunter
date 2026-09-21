package analyze

import (
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

var benchmarkDatabaseCorrelationResult databaseCorrelationResult

func TestDatabaseIntervalsCorrelateWithJankyUIAndStallInsideSameOperation(t *testing.T) {
	header := jhlog.DefaultSegmentHeader()
	header.ProcessName = "app"
	header.ProcessInstanceID[0] = 1
	header.SessionID[0] = 2
	dict := map[uint64]string{
		1: "Feed",
		2: "SELECT item FROM feed WHERE id = ?",
		3: "FeedDao.load",
		4: "feed.open",
	}
	context := jhlog.AttributionContext{
		Present: true, Screen: jhlog.LocalSymbol(1), Owner: jhlog.LocalSymbol(3), OperationID: 7,
	}
	events := []jhlog.Event{
		{
			Type: jhlog.EventOperation, TimeUS: 900_000, TimeMS: 900,
			Attribution: context,
			Operation: &jhlog.OperationEvent{
				NameRef: jhlog.LocalSymbol(4), ID: 7, Phase: jhlog.OperationPhaseStarted,
				Kind: jhlog.OperationKindUser, BudgetUS: 100_000,
			},
		},
		{
			Type: jhlog.EventDatabase, TimeUS: 1_010_000, TimeMS: 1_010,
			Flags: uint64(jhlog.FlagThreadMain), Attribution: context,
			Database: &jhlog.DatabaseEvent{
				QueryRef: jhlog.LocalSymbol(2), SourceRef: jhlog.LocalSymbol(3),
				Framework: jhlog.DatabaseFrameworkRoom, Operation: jhlog.DatabaseOperationQuery,
				Outcome: jhlog.DatabaseOutcomeSuccess, DurationUS: 20_000,
			},
		},
		{
			Type: jhlog.EventStall, TimeUS: 1_015_000, TimeMS: 1_015,
			Attribution: context, Stall: &jhlog.StallEvent{DurationMS: 10},
		},
		{
			Type: jhlog.EventUIWindow, TimeUS: 1_020_000, TimeMS: 1_020,
			Flags: uint64(jhlog.FlagUIProblem), Attribution: context,
			UIWindow: &jhlog.UIWindowEvent{
				WindowMS: 30, FrameCount: 60, JankCount: 6, P95MS: 40, P99MS: 64,
				Source: jhlog.UIFrameSourceJankStats, FrameDeadlineUS: 16_667,
			},
		},
		{
			Type: jhlog.EventOperation, TimeUS: 1_100_000, TimeMS: 1_100,
			Attribution: context,
			Operation: &jhlog.OperationEvent{
				NameRef: jhlog.LocalSymbol(4), ID: 7, Phase: jhlog.OperationPhaseFinished,
				Kind: jhlog.OperationKindUser, Outcome: jhlog.OperationOutcomeSuccess,
				DurationUS: 200_000, BudgetUS: 100_000,
			},
		},
	}

	summary := inspectLogsForTest("database-correlation", []jhlog.Log{{
		Dict: dict, Events: events, Result: jhlog.StreamResult{Header: header},
	}})
	if summary.DatabaseAnalysis == nil || len(summary.DatabaseAnalysis.Statements) != 1 {
		t.Fatalf("database analysis = %+v", summary.DatabaseAnalysis)
	}
	statement := summary.DatabaseAnalysis.Statements[0]
	if statement.MainCorrelation.UIWindowOverlaps != 1 ||
		statement.MainCorrelation.UIFrames != 60 ||
		statement.MainCorrelation.UIJankyFrames != 6 ||
		statement.MainCorrelation.StallOverlaps != 1 {
		t.Fatalf("statement main correlation = %+v", statement.MainCorrelation)
	}
	if len(statement.Contexts) != 1 || statement.Contexts[0].MainCorrelation != statement.MainCorrelation {
		t.Fatalf("context correlation = %+v, statement = %+v", statement.Contexts, statement.MainCorrelation)
	}
	if summary.OperationAnalysis == nil || len(summary.OperationAnalysis.Operations) != 1 {
		t.Fatalf("operation analysis = %+v", summary.OperationAnalysis)
	}
	worst := summary.OperationAnalysis.Operations[0].WorstDatabaseStatements
	if len(worst) != 1 || worst[0].Query != dict[2] || worst[0].Source != dict[3] ||
		worst[0].Calls != 1 || worst[0].MainThreadCalls != 1 || worst[0].TotalDurationUS != 20_000 {
		t.Fatalf("worst operation SQL = %+v", worst)
	}
}

func TestDatabaseIntervalRightBoundaryIsExclusive(t *testing.T) {
	correlation := databaseCorrelationAccumulator{}
	header := jhlog.DefaultSegmentHeader()
	header.ProcessInstanceID[0] = 1
	header.SessionID[0] = 2
	correlation.startLog(header, 1)
	context := databaseTimelineContext{screen: "Feed", operationID: 7}
	correlation.addDatabase(databaseContextKey{}, context, jhlog.Event{
		TimeUS: 1_000_000, Flags: uint64(jhlog.FlagThreadMain),
		Database: &jhlog.DatabaseEvent{DurationUS: 100_000},
	}, 1)
	correlation.addUIWindow(context, jhlog.Event{
		TimeUS: 1_100_000, Flags: uint64(jhlog.FlagUIProblem),
		UIWindow: &jhlog.UIWindowEvent{WindowMS: 100, FrameCount: 6, JankCount: 1},
	})

	result := correlation.finalize()
	if result.total.Main.UIWindowOverlaps != 0 {
		t.Fatalf("touching half-open intervals correlated: %+v", result.total.Main)
	}
}

func TestDatabaseCorrelationCountsIntervalsWithoutJoinContext(t *testing.T) {
	header := jhlog.DefaultSegmentHeader()
	header.ProcessInstanceID[0], header.SessionID[0] = 1, 2
	correlation := databaseCorrelationAccumulator{}
	correlation.startLog(header, 1)
	missingContext := databaseTimelineContext{}
	correlation.addDatabase(databaseContextKey{}, missingContext, jhlog.Event{
		TimeUS: 1_000_000, Database: &jhlog.DatabaseEvent{DurationUS: 100_000},
	}, 1)
	correlation.addTransaction(databaseTransactionKey{transactionID: 1}, missingContext, jhlog.Event{
		TimeUS: 1_000_000, DatabaseTransaction: &jhlog.DatabaseTransactionEvent{
			TransactionID: 1, Stage: jhlog.DatabaseTransactionTerminal, DurationUS: 100_000,
		},
	})
	correlation.addHTTP(missingContext, jhlog.Event{
		TimeUS: 1_000_000, HTTP: &jhlog.HTTPEvent{DurationMS: 100},
	})

	if correlation.database.dropped != 1 || correlation.transactions.dropped != 1 ||
		correlation.http.dropped != 1 {
		t.Fatalf("missing-context drops = DB %d, transaction %d, HTTP %d",
			correlation.database.dropped, correlation.transactions.dropped, correlation.http.dropped)
	}
}

func TestDatabaseCorrelationUsesScreenWhenOperationContextIsAbsent(t *testing.T) {
	header := jhlog.DefaultSegmentHeader()
	header.ProcessInstanceID[0], header.SessionID[0] = 1, 2
	correlation := databaseCorrelationAccumulator{}
	correlation.startLog(header, 1)
	context := databaseTimelineContext{screen: "Feed"}
	correlation.addDatabase(databaseContextKey{}, context, jhlog.Event{
		TimeUS: 1_000_000, Flags: uint64(jhlog.FlagThreadMain),
		Database: &jhlog.DatabaseEvent{DurationUS: 100_000},
	}, 1)
	correlation.addUIWindow(context, jhlog.Event{
		TimeUS: 1_000_000, Flags: uint64(jhlog.FlagUIProblem),
		UIWindow: &jhlog.UIWindowEvent{WindowMS: 100, FrameCount: 6, JankCount: 1},
	})

	result := correlation.finalize()
	if correlation.database.dropped != 0 || correlation.ui.dropped != 0 ||
		result.total.Main.UIWindowOverlaps != 1 {
		t.Fatalf("screen-only correlation = drops %d/%d, result %+v",
			correlation.database.dropped, correlation.ui.dropped, result.total.Main)
	}
}

func TestDatabaseAndTransactionIntervalsCorrelateWithRelatedSubsystems(t *testing.T) {
	correlation := databaseCorrelationAccumulator{}
	header := jhlog.DefaultSegmentHeader()
	header.ProcessInstanceID[0] = 1
	header.SessionID[0] = 2
	correlation.startLog(header, 1)
	context := databaseTimelineContext{screen: "Feed", operationID: 7}
	correlation.addDatabase(databaseContextKey{}, context, jhlog.Event{
		TimeUS: 1_000_000, Flags: uint64(jhlog.FlagThreadMain),
		Database: &jhlog.DatabaseEvent{DurationUS: 100_000},
	}, 1)
	transactionKey := databaseTransactionScopeKey(header.ProcessInstanceID, 1, 42)
	correlation.addTransaction(transactionKey, context, jhlog.Event{
		TimeUS: 1_000_000, Flags: uint64(jhlog.FlagThreadMain),
		DatabaseTransaction: &jhlog.DatabaseTransactionEvent{
			TransactionID: 42, Stage: jhlog.DatabaseTransactionTerminal,
			Outcome: jhlog.DatabaseTransactionSuccess, DurationUS: 100_000,
		},
	})
	correlation.addHTTP(context, jhlog.Event{
		TimeUS: 980_000, HTTP: &jhlog.HTTPEvent{DurationMS: 50},
	})
	correlation.addIO(context, jhlog.Event{
		TimeUS: 960_000, IO: &jhlog.IOEvent{DurationUS: 10_000},
	})
	correlation.addWorker(context, jhlog.Event{
		TimeUS: 990_000, Worker: &jhlog.WorkerEvent{
			Stage: jhlog.WorkerStageFinished, DurationMS: 80,
		},
	})
	correlation.addGC(context, "gc.count.delta", jhlog.Event{
		TimeUS: 950_000, Metric: &jhlog.MetricEvent{Value: 1},
	})

	result := correlation.finalize()
	stats := result.total.Main
	if stats.HTTPOverlaps != 1 || stats.FileIOOverlaps != 1 ||
		stats.WorkerOverlaps != 1 || stats.GCOverlaps != 1 {
		t.Fatalf("related DB overlaps = %+v", stats)
	}
	transaction := result.transactions[transactionKey]
	if transaction.HTTPOverlaps != 1 || transaction.FileIOOverlaps != 1 ||
		transaction.WorkerOverlaps != 1 || transaction.GCOverlaps != 1 {
		t.Fatalf("related transaction overlaps = %+v", transaction)
	}
}

func TestInspectWiresRelatedSubsystemCorrelationIntoStatementAndTransaction(t *testing.T) {
	header := jhlog.DefaultSegmentHeader()
	header.ProcessInstanceID[0] = 1
	header.SessionID[0] = 2
	dict := map[uint64]string{
		1: "Feed", 2: "SELECT item FROM feed", 3: "FeedDao.load",
		4: "gc.count.delta", 5: "feed.open", 6: "SyncWorker", 7: "FileStore.read",
	}
	context := jhlog.AttributionContext{
		Present: true, Screen: jhlog.LocalSymbol(1), Owner: jhlog.LocalSymbol(3), OperationID: 7,
	}
	events := []jhlog.Event{
		{Type: jhlog.EventOperation, TimeUS: 800_000, Attribution: context, Operation: &jhlog.OperationEvent{
			NameRef: jhlog.LocalSymbol(5), ID: 7, Phase: jhlog.OperationPhaseStarted,
		}},
		{Type: jhlog.EventDatabase, TimeUS: 1_000_000, Attribution: context, Flags: uint64(jhlog.FlagThreadMain), Database: &jhlog.DatabaseEvent{
			QueryRef: jhlog.LocalSymbol(2), SourceRef: jhlog.LocalSymbol(3),
			StatementFingerprint: 101, Operation: jhlog.DatabaseOperationQuery,
			Outcome: jhlog.DatabaseOutcomeSuccess, DurationUS: 100_000,
		}},
		{Type: jhlog.EventDatabaseTransaction, TimeUS: 1_000_000, Attribution: context, Flags: uint64(jhlog.FlagThreadMain), DatabaseTransaction: &jhlog.DatabaseTransactionEvent{
			SourceRef: jhlog.LocalSymbol(3), TransactionID: 42,
			Stage: jhlog.DatabaseTransactionTerminal, Outcome: jhlog.DatabaseTransactionSuccess,
			DurationUS: 100_000, StatementCount: 1, ReadCount: 1,
		}},
		{Type: jhlog.EventHTTP, TimeUS: 950_000, Attribution: context, HTTP: &jhlog.HTTPEvent{DurationMS: 10}},
		{Type: jhlog.EventIO, TimeUS: 960_000, Attribution: context, IO: &jhlog.IOEvent{
			SourceRef: jhlog.LocalSymbol(7), Operation: jhlog.IOOperationFileRead,
			Outcome: jhlog.IOOutcomeSuccess, DurationUS: 10_000,
		}},
		{Type: jhlog.EventWorker, TimeUS: 970_000, Attribution: context, Worker: &jhlog.WorkerEvent{
			WorkerRef: jhlog.LocalSymbol(6), InstanceID: 1,
			Stage: jhlog.WorkerStageFinished, Outcome: jhlog.WorkerOutcomeSuccess, DurationMS: 20,
		}},
		{Type: jhlog.EventCounter, TimeUS: 980_000, Attribution: context, Metric: &jhlog.MetricEvent{
			MetricRef: jhlog.LocalSymbol(4), Value: 1,
		}},
	}
	summary := inspectLogsForTest("database-related-correlation", []jhlog.Log{{
		Dict: dict, Events: events, Result: jhlog.StreamResult{Header: header},
	}})
	statement := summary.DatabaseAnalysis.Statements[0].MainCorrelation
	if statement.HTTPOverlaps != 1 || statement.FileIOOverlaps != 1 ||
		statement.WorkerOverlaps != 1 || statement.GCOverlaps != 1 {
		t.Fatalf("statement related correlation = %+v", statement)
	}
	transaction := summary.DatabaseAnalysis.Transactions.Transactions[0].Correlation
	if transaction.HTTPOverlaps != 1 || transaction.FileIOOverlaps != 1 ||
		transaction.WorkerOverlaps != 1 || transaction.GCOverlaps != 1 {
		t.Fatalf("transaction related correlation = %+v", transaction)
	}
}

func TestDatabaseIntervalsDoNotCrossProcessOrSessionBoundary(t *testing.T) {
	first := jhlog.DefaultSegmentHeader()
	first.ProcessInstanceID[0], first.SessionID[0] = 1, 2
	for name, mutate := range map[string]func(*jhlog.SegmentHeader){
		"process": func(header *jhlog.SegmentHeader) { header.ProcessInstanceID[0] = 3 },
		"session": func(header *jhlog.SegmentHeader) { header.SessionID[0] = 3 },
	} {
		t.Run(name, func(t *testing.T) {
			correlation := databaseCorrelationAccumulator{}
			context := databaseTimelineContext{screen: "Feed", operationID: 7}
			correlation.startLog(first, 1)
			correlation.addDatabase(databaseContextKey{}, context, jhlog.Event{
				TimeUS: 1_000_000, Flags: uint64(jhlog.FlagThreadMain),
				Database: &jhlog.DatabaseEvent{DurationUS: 100_000},
			}, 1)
			second := first
			mutate(&second)
			correlation.startLog(second, 2)
			correlation.addUIWindow(context, jhlog.Event{
				TimeUS: 1_000_000, Flags: uint64(jhlog.FlagUIProblem),
				UIWindow: &jhlog.UIWindowEvent{WindowMS: 100, FrameCount: 6, JankCount: 1},
			})

			result := correlation.finalize()
			if result.total.Main.UIWindowOverlaps != 0 {
				t.Fatalf("independent identity correlated: %+v", result.total.Main)
			}
		})
	}
}

func TestDatabaseIntervalsRequireSameScreenAndOperation(t *testing.T) {
	header := jhlog.DefaultSegmentHeader()
	header.ProcessInstanceID[0], header.SessionID[0] = 1, 2
	for name, uiContext := range map[string]databaseTimelineContext{
		"screen":    {screen: "Other", operationID: 7},
		"operation": {screen: "Feed", operationID: 8},
	} {
		t.Run(name, func(t *testing.T) {
			correlation := databaseCorrelationAccumulator{}
			correlation.startLog(header, 1)
			correlation.addDatabase(databaseContextKey{}, databaseTimelineContext{
				screen: "Feed", operationID: 7,
			}, jhlog.Event{
				TimeUS: 1_000_000, Flags: uint64(jhlog.FlagThreadMain),
				Database: &jhlog.DatabaseEvent{DurationUS: 100_000},
			}, 1)
			correlation.addUIWindow(uiContext, jhlog.Event{
				TimeUS: 1_000_000, Flags: uint64(jhlog.FlagUIProblem),
				UIWindow: &jhlog.UIWindowEvent{WindowMS: 100, FrameCount: 6, JankCount: 1},
			})
			if got := correlation.finalize().total.Main.UIWindowOverlaps; got != 0 {
				t.Fatalf("mismatched context correlated: %d", got)
			}
		})
	}
}

func TestBackgroundDatabaseOverlapIsKeptSeparateFromMainThreadEvidence(t *testing.T) {
	header := jhlog.DefaultSegmentHeader()
	header.ProcessInstanceID[0], header.SessionID[0] = 1, 2
	correlation := databaseCorrelationAccumulator{}
	correlation.startLog(header, 1)
	context := databaseTimelineContext{screen: "Feed", operationID: 7}
	correlation.addDatabase(databaseContextKey{}, context, jhlog.Event{
		TimeUS: 1_000_000, Database: &jhlog.DatabaseEvent{DurationUS: 100_000},
	}, 1)
	correlation.addUIWindow(context, jhlog.Event{
		TimeUS: 1_000_000, Flags: uint64(jhlog.FlagUIProblem),
		UIWindow: &jhlog.UIWindowEvent{WindowMS: 100, FrameCount: 6, JankCount: 1},
	})
	result := correlation.finalize().total
	if result.Main.UIWindowOverlaps != 0 || result.Background.UIWindowOverlaps != 1 {
		t.Fatalf("background correlation was promoted to main: %+v", result)
	}
}

func TestDatabaseCorrelationRetentionIsBoundedAndKeepsLateHighPriorityEvidence(t *testing.T) {
	store := newDatabaseWindowTimeline(2)
	store.add(databaseCorrelationWindow{startUS: 1}, databaseTimelineRank{class: 0, primary: 1, hash: 1})
	store.add(databaseCorrelationWindow{startUS: 2}, databaseTimelineRank{class: 0, primary: 2, hash: 2})
	store.add(databaseCorrelationWindow{startUS: 3}, databaseTimelineRank{class: 0, primary: 20, hash: 3})
	store.add(databaseCorrelationWindow{startUS: 4}, databaseTimelineRank{class: 4, primary: 1, hash: 4})
	if len(store.values) != 2 || store.evicted != 2 || store.dropped != 2 {
		t.Fatalf("bounded retention counters = len %d, evicted %d, dropped %d", len(store.values), store.evicted, store.dropped)
	}
	seen := map[uint64]bool{}
	for _, entry := range store.values {
		seen[entry.startUS] = true
	}
	if !seen[3] || !seen[4] {
		t.Fatalf("late frequent/failed evidence was not retained: %+v", store.values)
	}

	correlation := databaseCorrelationAccumulator{}
	header := jhlog.DefaultSegmentHeader()
	header.ProcessInstanceID[0], header.SessionID[0] = 1, 2
	correlation.startLog(header, 1)
	context := databaseTimelineContext{screen: "Feed", operationID: 7}
	for index := 0; index < databaseIntervalLimit+128; index++ {
		correlation.addDatabase(databaseContextKey{}, context, jhlog.Event{
			TimeUS:   uint64(index+2) * 1_000,
			Database: &jhlog.DatabaseEvent{DurationUS: 1_000},
		}, uint64(index+1))
	}
	for index := 0; index < databaseUIWindowLimit+64; index++ {
		correlation.addUIWindow(context, jhlog.Event{
			TimeUS: uint64(index+2) * 1_000, Flags: uint64(jhlog.FlagUIProblem),
			UIWindow: &jhlog.UIWindowEvent{WindowMS: 1, FrameCount: 1, JankCount: 1},
		})
	}
	for index := 0; index < databaseStallLimit+64; index++ {
		correlation.addStall(context, jhlog.Event{
			TimeUS: uint64(index+2) * 1_000, Stall: &jhlog.StallEvent{DurationMS: 1},
		})
	}
	if len(correlation.database.values) != databaseIntervalLimit ||
		len(correlation.ui.values) != databaseUIWindowLimit ||
		len(correlation.stalls.values) != databaseStallLimit ||
		correlation.database.dropped == 0 || correlation.ui.dropped == 0 || correlation.stalls.dropped == 0 {
		t.Fatalf("correlation retention is not bounded: db=%d ui=%d stalls=%d drops=%d/%d/%d",
			len(correlation.database.values), len(correlation.ui.values), len(correlation.stalls.values),
			correlation.database.dropped, correlation.ui.dropped, correlation.stalls.dropped,
		)
	}
}

func BenchmarkDatabaseCorrelationFinalize(b *testing.B) {
	correlation := databaseCorrelationAccumulator{}
	header := jhlog.DefaultSegmentHeader()
	header.ProcessInstanceID[0], header.SessionID[0] = 1, 2
	correlation.startLog(header, 1)
	context := databaseTimelineContext{screen: "Feed", operationID: 7}
	contextKey := databaseContextKey{source: "FeedDao.load", operationID: 7}
	for index := 0; index < databaseIntervalLimit; index++ {
		endUS := uint64(index+2) * 1_000
		correlation.addDatabase(contextKey, context, jhlog.Event{
			TimeUS: endUS, Database: &jhlog.DatabaseEvent{DurationUS: 750},
		}, uint64(index+1))
	}
	for index := 0; index < databaseRelatedIntervalLimit; index++ {
		endUS := uint64(index+2) * 1_000
		correlation.addHTTP(context, jhlog.Event{
			TimeUS: endUS, HTTP: &jhlog.HTTPEvent{DurationMS: 1},
		})
		correlation.addIO(context, jhlog.Event{
			TimeUS: endUS, IO: &jhlog.IOEvent{
				Operation: jhlog.IOOperationFileRead, DurationUS: 500,
			},
		})
		correlation.addWorker(context, jhlog.Event{
			TimeUS: endUS, Worker: &jhlog.WorkerEvent{
				Stage: jhlog.WorkerStageFinished, DurationMS: 1,
			},
		})
		correlation.addGC(context, workerMetricGCCount, jhlog.Event{
			TimeUS: endUS, Metric: &jhlog.MetricEvent{Value: 1},
		})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		benchmarkDatabaseCorrelationResult = correlation.finalize()
	}
}

func TestLateDatabaseSignalIsEnrichedWithoutDoubleCounting(t *testing.T) {
	accumulator := newOperationAnalysisAccumulator()
	header := jhlog.DefaultSegmentHeader()
	header.ProcessInstanceID[0] = 1
	accumulator.startLog(header)
	dict := map[uint64]string{1: "feed.open"}
	operation := jhlog.OperationEvent{
		NameRef: jhlog.LocalSymbol(1), ID: 7, Phase: jhlog.OperationPhaseStarted,
		Kind: jhlog.OperationKindUser,
	}
	accumulator.recordLifecycle(dict, jhlog.Event{Operation: &operation}, "Feed", Filter{})
	operation.Phase = jhlog.OperationPhaseFinished
	operation.Outcome = jhlog.OperationOutcomeSuccess
	operation.DurationUS = 10_000
	accumulator.recordLifecycle(dict, jhlog.Event{Operation: &operation}, "Feed", Filter{})
	database := &jhlog.DatabaseEvent{DurationUS: 3_000, Outcome: jhlog.DatabaseOutcomeSuccess}
	accumulator.recordDatabaseStatement(7, "SELECT 1", "FeedDao.load", "чтение", database, 0)

	analysis := accumulator.finalize()
	if analysis.LateSignalEvents != 1 || len(analysis.Operations) != 1 ||
		analysis.Operations[0].CorrelatedDatabase != 1 ||
		len(analysis.Operations[0].WorstDatabaseStatements) != 1 {
		t.Fatalf("late database enrichment = %+v", analysis)
	}
}

func BenchmarkDatabaseCorrelationBoundedJoin(b *testing.B) {
	header := jhlog.DefaultSegmentHeader()
	header.ProcessInstanceID[0], header.SessionID[0] = 1, 2
	context := databaseTimelineContext{screen: "Feed", operationID: 7}
	database := &jhlog.DatabaseEvent{DurationUS: 20_000, Outcome: jhlog.DatabaseOutcomeSuccess}
	window := &jhlog.UIWindowEvent{WindowMS: 1_000, FrameCount: 60, JankCount: 3}
	stall := &jhlog.StallEvent{DurationMS: 100}
	b.ReportAllocs()
	for range b.N {
		correlation := databaseCorrelationAccumulator{}
		correlation.startLog(header, 1)
		for index := 0; index < databaseIntervalLimit; index++ {
			correlation.addDatabase(databaseContextKey{}, context, jhlog.Event{
				TimeUS: uint64(index+1) * 10_000, Database: database,
			}, uint64(index+1))
		}
		for index := 0; index < databaseUIWindowLimit; index++ {
			timeUS := uint64(index+1) * 20_000
			correlation.addUIWindow(context, jhlog.Event{
				TimeUS: timeUS, Flags: uint64(jhlog.FlagUIProblem), UIWindow: window,
			})
			correlation.addStall(context, jhlog.Event{TimeUS: timeUS, Stall: stall})
		}
		benchmarkDatabaseCorrelation = correlation.finalize()
	}
}

func BenchmarkDatabaseCorrelationSingleContextAdds(b *testing.B) {
	header := jhlog.DefaultSegmentHeader()
	header.ProcessInstanceID[0], header.SessionID[0] = 1, 2
	context := databaseTimelineContext{screen: "Feed", operationID: 7}
	database := &jhlog.DatabaseEvent{DurationUS: 20_000}
	window := &jhlog.UIWindowEvent{WindowMS: 1_000, FrameCount: 60, JankCount: 3}
	b.Run("database", func(b *testing.B) {
		correlation := databaseCorrelationAccumulator{}
		correlation.startLog(header, 1)
		b.ReportAllocs()
		for index := 0; index < b.N; index++ {
			correlation.addDatabase(databaseContextKey{}, context, jhlog.Event{
				TimeUS: uint64(index+1) * 10_000, Database: database,
			}, uint64(index+1))
		}
	})
	b.Run("ui", func(b *testing.B) {
		correlation := databaseCorrelationAccumulator{}
		correlation.startLog(header, 1)
		b.ReportAllocs()
		for index := 0; index < b.N; index++ {
			correlation.addUIWindow(context, jhlog.Event{
				TimeUS: uint64(index+1) * 10_000, Flags: uint64(jhlog.FlagUIProblem), UIWindow: window,
			})
		}
	})
}

var benchmarkDatabaseCorrelation databaseCorrelationResult
