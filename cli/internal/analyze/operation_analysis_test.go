package analyze

import (
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func TestOperationAnalysisBuildsGenericTablesAndRollsStageSignalsIntoParent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operations.jhlog")
	header := jhlog.DefaultSegmentHeader()
	header.RunID[0] = 1
	header.ProcessInstanceID[0] = 2
	header.SessionID[0] = 3
	header.SegmentStartElapsedUS = 1_000_000
	header.CollectorStartElapsedUS = 1_000_000
	header.SegmentStartUnixMS = uint64(time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC).UnixMilli())
	header.TimezoneOffsetMinutes = 180
	closer, writer, err := jhlog.CreateWithHeader(path, header)
	if err != nil {
		t.Fatal(err)
	}
	entries := []jhlog.DictionaryEntry{
		{Kind: jhlog.DictOperation, ID: 1, Value: "content.open"},
		{Kind: jhlog.DictOperation, ID: 2, Value: "database.read"},
		{Kind: jhlog.DictScreen, ID: 3, Value: "CatalogActivity"},
		{Kind: jhlog.DictAttributeKey, ID: 4, Value: "source"},
		{Kind: jhlog.DictAttributeValue, ID: 5, Value: "cold_start"},
		{Kind: jhlog.DictOwner, ID: 6, Value: "CatalogRepository"},
	}
	for index := range entries {
		entry := entries[index]
		writeOperationTestEvent(t, writer, jhlog.Event{Type: jhlog.EventDictionary, Dictionary: &entry})
	}
	rootContext := jhlog.AttributionContext{
		Present: true, Screen: jhlog.LocalSymbol(3), Owner: jhlog.LocalSymbol(6), OperationID: 10,
	}
	stageContext := rootContext
	stageContext.OperationID = 11
	writeOperationTestEvent(t, writer, jhlog.Event{
		Type: jhlog.EventOperation, TimeMS: 1_000, Attribution: rootContext,
		Operation: &jhlog.OperationEvent{
			NameRef: jhlog.LocalSymbol(1), ID: 10, Phase: jhlog.OperationPhaseStarted,
			Kind: jhlog.OperationKindUser, BudgetUS: 400_000,
			Attributes: []jhlog.OperationAttribute{{KeyRef: jhlog.LocalSymbol(4), ValueRef: jhlog.LocalSymbol(5)}},
		},
	})
	writeOperationTestEvent(t, writer, jhlog.Event{
		Type: jhlog.EventOperation, TimeMS: 1_050, Attribution: stageContext,
		Operation: &jhlog.OperationEvent{
			NameRef: jhlog.LocalSymbol(2), ID: 11, ParentID: 10,
			Phase: jhlog.OperationPhaseStarted, Kind: jhlog.OperationKindStage,
		},
	})
	writeOperationTestEvent(t, writer, jhlog.Event{
		Type: jhlog.EventStall, TimeMS: 1_100, Attribution: stageContext,
		Stall: &jhlog.StallEvent{DurationMS: 125},
	})
	writeOperationTestEvent(t, writer, jhlog.Event{
		Type: jhlog.EventIO, TimeMS: 1_150, Attribution: stageContext, Flags: uint64(jhlog.FlagIOBytesKnown),
		IO: &jhlog.IOEvent{
			SourceRef: jhlog.LocalSymbol(6), Operation: jhlog.IOOperationFileRead,
			Outcome: jhlog.IOOutcomeSuccess, DurationUS: 24_000, Bytes: 512,
		},
	})
	writeOperationTestEvent(t, writer, jhlog.Event{
		Type: jhlog.EventOperation, TimeMS: 1_250, Attribution: stageContext,
		Operation: &jhlog.OperationEvent{
			NameRef: jhlog.LocalSymbol(2), ID: 11, ParentID: 10,
			Phase: jhlog.OperationPhaseFinished, Kind: jhlog.OperationKindStage,
			Outcome: jhlog.OperationOutcomeSuccess, DurationUS: 200_000,
		},
	})
	writeOperationTestEvent(t, writer, jhlog.Event{
		Type: jhlog.EventOperation, TimeMS: 1_500, Attribution: rootContext,
		Operation: &jhlog.OperationEvent{
			NameRef: jhlog.LocalSymbol(1), ID: 10, Phase: jhlog.OperationPhaseFinished,
			Kind: jhlog.OperationKindUser, Outcome: jhlog.OperationOutcomeSuccess, DurationUS: 500_000,
			BudgetUS:   400_000,
			Attributes: []jhlog.OperationAttribute{{KeyRef: jhlog.LocalSymbol(4), ValueRef: jhlog.LocalSymbol(5)}},
		},
	})
	if err := closer.Close(); err != nil {
		t.Fatal(err)
	}

	summary, err := InspectFilesWithOptions("operations", []string{path}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	analysis := summary.OperationAnalysis
	if analysis == nil {
		t.Fatal("operation analysis is missing")
	}
	if analysis.Started != 2 || analysis.Completed != 2 || analysis.MissingFinish != 0 {
		t.Fatalf("lifecycle = %+v", analysis)
	}
	root := findOperationStats(t, analysis.Operations, "content.open")
	if root.Count != 1 || root.P95MS != 500 || root.BudgetBreaches != 1 || root.BudgetBreachRatePct != 100 {
		t.Fatalf("root stats = %+v", root)
	}
	if root.P50MS != 500 || root.P90MS != 500 {
		t.Fatalf("root percentiles = %+v", root)
	}
	if root.CorrelatedStalls != 1 || root.CorrelatedStallMaxMS != 125 ||
		root.CorrelatedIO != 1 || root.CorrelatedIODurationUS != 24_000 || root.CorrelatedIOBytes != 512 {
		t.Fatalf("root correlated signals = %+v", root)
	}
	if len(analysis.TimeSlots) != 2 || analysis.TimeSlots[0].Label != "2026-08-20 15:00 (UTC+03:00)" {
		t.Fatalf("time slots = %+v", analysis.TimeSlots)
	}
	rootSlot := findOperationTimeSlot(t, analysis.TimeSlots, "content.open")
	if rootSlot.CorrelatedStalls != 1 || rootSlot.CorrelatedStallMaxMS != 125 ||
		rootSlot.CorrelatedIO != 1 || rootSlot.CorrelatedIODurationUS != 24_000 ||
		rootSlot.CorrelatedIOBytes != 512 {
		t.Fatalf("root time-slot signals = %+v", rootSlot)
	}
	if len(analysis.Dimensions) != 1 || analysis.Dimensions[0].Key != "source" ||
		analysis.Dimensions[0].Value != "cold_start" {
		t.Fatalf("dimensions = %+v", analysis.Dimensions)
	}
	if len(analysis.Stages) != 1 || analysis.Stages[0].Stage != "database.read" ||
		analysis.Stages[0].TotalMS != 200 || analysis.Stages[0].SharePct != 40 {
		t.Fatalf("stages = %+v", analysis.Stages)
	}
	if len(analysis.WorstIncidents) != 2 || analysis.WorstIncidents[0].Operation != "content.open" {
		t.Fatalf("incidents = %+v", analysis.WorstIncidents)
	}
}

func TestOperationAnalysisUsesSelfContainedFinishWhenStartIsMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing-start.jhlog")
	header := jhlog.DefaultSegmentHeader()
	header.RunID[0], header.ProcessInstanceID[0], header.SessionID[0] = 1, 2, 3
	header.SegmentStartElapsedUS = 1_000_000
	header.SegmentStartUnixMS = uint64(time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC).UnixMilli())
	closer, writer, err := jhlog.CreateWithHeader(path, header)
	if err != nil {
		t.Fatal(err)
	}
	entries := []jhlog.DictionaryEntry{
		{Kind: jhlog.DictOperation, ID: 1, Value: "content.open"},
		{Kind: jhlog.DictScreen, ID: 2, Value: "CatalogActivity"},
		{Kind: jhlog.DictAttributeKey, ID: 3, Value: "source"},
		{Kind: jhlog.DictAttributeValue, ID: 4, Value: "notification"},
	}
	for index := range entries {
		entry := entries[index]
		writeOperationTestEvent(t, writer, jhlog.Event{Type: jhlog.EventDictionary, Dictionary: &entry})
	}
	writeOperationTestEvent(t, writer, jhlog.Event{
		Type: jhlog.EventOperation, TimeMS: 3_500,
		Attribution: jhlog.AttributionContext{Present: true, Screen: jhlog.LocalSymbol(2), OperationID: 42},
		Operation: &jhlog.OperationEvent{
			NameRef: jhlog.LocalSymbol(1), ID: 42, Phase: jhlog.OperationPhaseFinished,
			Kind: jhlog.OperationKindScreen, Outcome: jhlog.OperationOutcomeTimeout,
			DurationUS: 2_500_000, BudgetUS: 1_000_000,
			Attributes: []jhlog.OperationAttribute{{KeyRef: jhlog.LocalSymbol(3), ValueRef: jhlog.LocalSymbol(4)}},
		},
	})
	if err := closer.Close(); err != nil {
		t.Fatal(err)
	}

	summary, err := InspectFilesWithOptions("missing-start", []string{path}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	analysis := summary.OperationAnalysis
	if analysis == nil || analysis.MissingStart != 1 || analysis.Completed != 1 {
		t.Fatalf("operation lifecycle = %+v", analysis)
	}
	stats := findOperationStats(t, analysis.Operations, "content.open")
	if stats.Count != 1 || stats.Timeouts != 1 || stats.P95MS != 2_500 || stats.BudgetBreaches != 1 {
		t.Fatalf("recovered operation stats = %+v", stats)
	}
	if len(analysis.Dimensions) != 1 || analysis.Dimensions[0].Value != "notification" {
		t.Fatalf("recovered dimensions = %+v", analysis.Dimensions)
	}
	if len(analysis.WorstIncidents) != 1 || analysis.WorstIncidents[0].StartUnixMS != header.SegmentStartUnixMS {
		t.Fatalf("recovered incidents = %+v", analysis.WorstIncidents)
	}
}

func TestOperationAnalysisPreservesEverySupportedSignalInAllDiagnosticViews(t *testing.T) {
	accumulator := newOperationAnalysisAccumulator()
	header := jhlog.DefaultSegmentHeader()
	header.ProcessInstanceID[0] = 1
	accumulator.startLog(header)
	dict := map[uint64]string{1: "content.open"}
	operation := jhlog.OperationEvent{
		NameRef: jhlog.LocalSymbol(1), ID: 10, Phase: jhlog.OperationPhaseStarted,
		Kind: jhlog.OperationKindUser, BudgetUS: 50_000,
	}
	accumulator.recordLifecycle(dict, jhlog.Event{TimeMS: 100, Operation: &operation}, "Screen", Filter{})
	for _, event := range []jhlog.Event{
		{HTTP: &jhlog.HTTPEvent{DurationMS: 11}, Flags: uint64(jhlog.FlagHTTPFailed)},
		{WebSocket: &jhlog.WebSocketEvent{Stage: jhlog.WebSocketStageFailed}},
		{Database: &jhlog.DatabaseEvent{DurationUS: 17_000, Outcome: jhlog.DatabaseOutcomeFailure}, Flags: uint64(jhlog.FlagThreadMain)},
		{Worker: &jhlog.WorkerEvent{Stage: jhlog.WorkerStageFinished, Outcome: jhlog.WorkerOutcomeRetry, DurationMS: 18}},
		{Stall: &jhlog.StallEvent{DurationMS: 12}},
		{UIWindow: &jhlog.UIWindowEvent{FrameCount: 20, JankCount: 3}},
		{IO: &jhlog.IOEvent{DurationUS: 13_000, Bytes: 256}, Flags: uint64(jhlog.FlagIOBytesKnown)},
		{Problem: &jhlog.ProblemEvent{Count: 2}},
		{LogSpam: &jhlog.LogSpamEvent{Count: 4}},
		{RuntimeCall: &jhlog.RuntimeCallEvent{Count: 5, TotalMS: 15}},
		{Metric: &jhlog.MetricEvent{Value: 1}},
		{Retained: &jhlog.RetainedEvent{Count: 6}},
		{Memory: &jhlog.MemoryEvent{PSSKB: 700}},
	} {
		accumulator.recordSignal(event, operation.ID)
	}
	accumulator.recordSignal(jhlog.Event{RuntimeCall: &jhlog.RuntimeCallEvent{
		Count: 7, TotalMS: 21,
	}}, operation.ID, "jankhunter.semantic.v1.compose.composition.main")
	operation.Phase = jhlog.OperationPhaseFinished
	operation.Outcome = jhlog.OperationOutcomeSuccess
	operation.DurationUS = 100_000
	accumulator.recordLifecycle(dict, jhlog.Event{TimeMS: 200, Operation: &operation}, "Screen", Filter{})

	analysis := accumulator.finalize()
	stats := findOperationStats(t, analysis.Operations, "content.open")
	if stats.CorrelatedHTTP != 1 || stats.CorrelatedHTTPFailures != 1 || stats.CorrelatedHTTPDurationMS != 11 ||
		stats.CorrelatedWebSocket != 1 || stats.CorrelatedWebSocketErrors != 1 ||
		stats.CorrelatedDatabase != 1 || stats.CorrelatedDatabaseErrors != 1 || stats.CorrelatedDatabaseMain != 1 || stats.CorrelatedDatabaseUS != 17_000 ||
		stats.CorrelatedCompose != 7 || stats.CorrelatedComposeMS != 21 ||
		stats.CorrelatedWorkers != 1 || stats.CorrelatedWorkerFailures != 1 || stats.CorrelatedWorkerMS != 18 ||
		stats.CorrelatedStalls != 1 || stats.CorrelatedStallMaxMS != 12 ||
		stats.CorrelatedUIFrames != 20 || stats.CorrelatedUIJank != 3 || stats.CorrelatedUIJankRatePct != 15 ||
		stats.CorrelatedIO != 1 || stats.CorrelatedIODurationUS != 13_000 || stats.CorrelatedIOBytes != 256 ||
		stats.CorrelatedProblems != 2 || stats.CorrelatedLogRecords != 4 ||
		stats.CorrelatedRuntimeCalls != 12 || stats.CorrelatedRuntimeTotalMS != 36 ||
		stats.CorrelatedMetricEvents != 1 || stats.CorrelatedRetainedObjects != 6 || stats.MaxPSSKB != 700 {
		t.Fatalf("operation signals = %+v", stats)
	}
	slot := findOperationTimeSlot(t, analysis.TimeSlots, "content.open")
	if slot.CorrelatedHTTPDurationMS != 11 || slot.CorrelatedStallMaxMS != 12 ||
		slot.CorrelatedWebSocket != 1 || slot.CorrelatedWebSocketErrors != 1 ||
		slot.CorrelatedDatabase != 1 || slot.CorrelatedDatabaseUS != 17_000 || slot.CorrelatedDatabaseMain != 1 ||
		slot.CorrelatedCompose != 7 || slot.CorrelatedComposeMS != 21 ||
		slot.CorrelatedWorkers != 1 || slot.CorrelatedWorkerMS != 18 ||
		slot.CorrelatedUIFrames != 20 || slot.CorrelatedUIJankRatePct != 15 ||
		slot.CorrelatedIODurationUS != 13_000 || slot.CorrelatedIOBytes != 256 ||
		slot.CorrelatedLogRecords != 4 || slot.CorrelatedRuntimeCalls != 12 ||
		slot.CorrelatedMetricEvents != 1 || slot.CorrelatedRetainedObjects != 6 || slot.MaxPSSKB != 700 {
		t.Fatalf("time-slot signals = %+v", slot)
	}
	if len(analysis.WorstIncidents) != 1 {
		t.Fatalf("incidents = %+v", analysis.WorstIncidents)
	}
	incident := analysis.WorstIncidents[0]
	if incident.HTTPDurationMS != 11 || incident.StallMaxMS != 12 ||
		incident.CorrelatedWebSocket != 1 || incident.WebSocketErrors != 1 ||
		incident.CorrelatedDatabase != 1 || incident.DatabaseDurationUS != 17_000 || incident.DatabaseMainThread != 1 ||
		incident.CorrelatedCompose != 7 || incident.ComposeDurationMS != 21 ||
		incident.CorrelatedWorkers != 1 || incident.WorkerDurationMS != 18 ||
		incident.CorrelatedUIFrames != 20 || incident.CorrelatedUIJank != 3 ||
		incident.IODurationUS != 13_000 || incident.IOBytes != 256 ||
		incident.CorrelatedLogRecords != 4 || incident.CorrelatedRuntimeCalls != 12 ||
		incident.CorrelatedMetricEvents != 1 || incident.CorrelatedRetainedObjects != 6 || incident.MaxPSSKB != 700 {
		t.Fatalf("incident signals = %+v", incident)
	}
}

func TestOperationAnalysisReportsUnfinishedLifecycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "unfinished.jhlog")
	header := jhlog.DefaultSegmentHeader()
	header.RunID[0], header.ProcessInstanceID[0], header.SessionID[0] = 1, 2, 3
	closer, writer, err := jhlog.CreateWithHeader(path, header)
	if err != nil {
		t.Fatal(err)
	}
	entry := jhlog.DictionaryEntry{Kind: jhlog.DictOperation, ID: 1, Value: "background.sync"}
	writeOperationTestEvent(t, writer, jhlog.Event{Type: jhlog.EventDictionary, Dictionary: &entry})
	writeOperationTestEvent(t, writer, jhlog.Event{
		Type: jhlog.EventOperation, TimeMS: 1,
		Operation: &jhlog.OperationEvent{
			NameRef: jhlog.LocalSymbol(1), ID: 1, Phase: jhlog.OperationPhaseStarted,
			Kind: jhlog.OperationKindBackground,
		},
	})
	if err := closer.Close(); err != nil {
		t.Fatal(err)
	}

	summary, err := InspectFilesWithOptions("unfinished", []string{path}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if summary.OperationAnalysis == nil || summary.OperationAnalysis.MissingFinish != 1 {
		t.Fatalf("operation analysis = %+v", summary.OperationAnalysis)
	}
}

func TestOperationComparisonDoesNotCallCandidateOnlyRowsRegressions(t *testing.T) {
	baseline := Summary{OperationAnalysis: &OperationAnalysis{Operations: []OperationStats{
		{Operation: "content.open", Kind: "user", Screen: "Catalog", Count: 20, P95MS: 400},
	}}}
	candidate := Summary{OperationAnalysis: &OperationAnalysis{Operations: []OperationStats{
		{Operation: "content.open", Kind: "user", Screen: "Catalog", Count: 25, P95MS: 900},
		{Operation: "search.open", Kind: "screen", Screen: "Search", Count: 30, P95MS: 1_200},
	}}}

	rows := compareOperationAnalysis(baseline, candidate)
	if len(rows) != 2 {
		t.Fatalf("operation deltas = %+v", rows)
	}
	if rows[0].Operation != "content.open" || rows[0].Status != "regressed" || rows[0].Severity != "high" {
		t.Fatalf("matched delta = %+v", rows[0])
	}
	if rows[1].Operation != "search.open" || rows[1].Status != "new" || rows[1].Severity != "ok" || rows[1].Comparable {
		t.Fatalf("candidate-only delta = %+v", rows[1])
	}
}

func TestOperationComparisonRequiresTwentySamplesPerCohort(t *testing.T) {
	baseline := Summary{OperationAnalysis: &OperationAnalysis{Operations: []OperationStats{{
		Operation: "content.open", Kind: "user", Count: 19, P95MS: 100,
	}}}}
	candidate := Summary{OperationAnalysis: &OperationAnalysis{Operations: []OperationStats{{
		Operation: "content.open", Kind: "user", Count: 100, P95MS: 1_000,
	}}}}

	rows := compareOperationAnalysis(baseline, candidate)
	if len(rows) != 1 || rows[0].Comparable || rows[0].Status != "insufficient_data" {
		t.Fatalf("operation deltas = %+v", rows)
	}
	if rows[0].Note != "Для описательного сравнения нужно минимум 20 завершений в каждом наборе; сейчас минимум 19." {
		t.Fatalf("comparison note = %q", rows[0].Note)
	}
}

func TestOperationComparisonMarksApproximateQuantiles(t *testing.T) {
	baseline := Summary{OperationAnalysis: &OperationAnalysis{Operations: []OperationStats{{
		Operation: "content.open", Kind: "user", Count: 100, P95MS: 100,
		QuantilesApproximated: true,
	}}}}
	candidate := Summary{OperationAnalysis: &OperationAnalysis{Operations: []OperationStats{{
		Operation: "content.open", Kind: "user", Count: 100, P95MS: 300,
		QuantilesApproximated: true,
	}}}}

	rows := compareOperationAnalysis(baseline, candidate)
	if len(rows) != 1 {
		t.Fatalf("operation deltas = %+v", rows)
	}
	row := rows[0]
	if !row.BaselineQuantilesApproximated || !row.CandidateQuantilesApproximated {
		t.Fatalf("approximation flags were lost: %+v", row)
	}
	if row.Confidence != "medium" {
		t.Fatalf("confidence = %q, want medium", row.Confidence)
	}
	if row.Note != "Описательное сравнение одинаковой операции; причинность требует одинаковых условий и достаточной выборки. Граница 95% оценена потоковым алгоритмом с ограниченной памятью." {
		t.Fatalf("comparison note = %q", row.Note)
	}
}

func TestOperationComparisonDoesNotTreatMissingBudgetAsZeroBreaches(t *testing.T) {
	baseline := Summary{OperationAnalysis: &OperationAnalysis{Operations: []OperationStats{{
		Operation: "content.open", Kind: "user", Count: 100, P95MS: 300,
	}}}}
	candidate := Summary{OperationAnalysis: &OperationAnalysis{Operations: []OperationStats{{
		Operation: "content.open", Kind: "user", Count: 100, P95MS: 300,
		Budgeted: 100, BudgetBreaches: 100, BudgetBreachRatePct: 100,
	}}}}

	rows := compareOperationAnalysis(baseline, candidate)
	if len(rows) != 1 {
		t.Fatalf("operation deltas = %+v", rows)
	}
	row := rows[0]
	if row.BaselineBudgeted != 0 || row.CandidateBudgeted != 100 || row.BudgetComparable {
		t.Fatalf("budget sample metadata = %+v", row)
	}
	if row.BudgetBreachChangePP != 0 || row.Status != "stable" || row.Severity != "ok" {
		t.Fatalf("missing baseline budget affected regression verdict: %+v", row)
	}
	if !strings.Contains(row.Note, "нарушений бюджета не учитывалось") {
		t.Fatalf("comparison note = %q", row.Note)
	}
}

func TestOperationComparisonRequiresEnoughBudgetedSamplesForBudgetRegression(t *testing.T) {
	baseline := Summary{OperationAnalysis: &OperationAnalysis{Operations: []OperationStats{{
		Operation: "content.open", Kind: "user", Count: 100, P95MS: 300,
		Budgeted: 19, BudgetBreaches: 0, BudgetBreachRatePct: 0,
	}}}}
	candidate := Summary{OperationAnalysis: &OperationAnalysis{Operations: []OperationStats{{
		Operation: "content.open", Kind: "user", Count: 100, P95MS: 300,
		Budgeted: 100, BudgetBreaches: 100, BudgetBreachRatePct: 100,
	}}}}

	row := compareOperationAnalysis(baseline, candidate)[0]
	if row.BudgetComparable || row.BudgetBreachChangePP != 0 || row.Status != "stable" {
		t.Fatalf("undersampled budget affected regression verdict: %+v", row)
	}

	baseline.OperationAnalysis.Operations[0].Budgeted = operationComparisonMinSample
	row = compareOperationAnalysis(baseline, candidate)[0]
	if !row.BudgetComparable || row.BudgetBreachChangePP != 100 || row.Status != "regressed" || row.Severity != "high" {
		t.Fatalf("comparable budget regression was not detected: %+v", row)
	}
}

func TestOperationAnalysisCorrelatesLateSignalsWithCompletedOperationAndParent(t *testing.T) {
	accumulator := newOperationAnalysisAccumulator()
	header := jhlog.DefaultSegmentHeader()
	header.ProcessInstanceID[0] = 1
	accumulator.startLog(header)
	dict := map[uint64]string{1: "content.open", 2: "database.read"}

	parentStart := jhlog.OperationEvent{
		NameRef: jhlog.LocalSymbol(1), ID: 10, Phase: jhlog.OperationPhaseStarted,
		Kind: jhlog.OperationKindUser,
	}
	childStart := jhlog.OperationEvent{
		NameRef: jhlog.LocalSymbol(2), ID: 11, ParentID: 10, Phase: jhlog.OperationPhaseStarted,
		Kind: jhlog.OperationKindStage,
	}
	accumulator.recordLifecycle(dict, jhlog.Event{Operation: &parentStart}, "Screen", Filter{})
	accumulator.recordLifecycle(dict, jhlog.Event{Operation: &childStart}, "Screen", Filter{})
	childFinish := childStart
	childFinish.Phase = jhlog.OperationPhaseFinished
	childFinish.Outcome = jhlog.OperationOutcomeSuccess
	childFinish.DurationUS = 100_000
	accumulator.recordLifecycle(dict, jhlog.Event{Operation: &childFinish}, "Screen", Filter{})
	parentFinish := parentStart
	parentFinish.Phase = jhlog.OperationPhaseFinished
	parentFinish.Outcome = jhlog.OperationOutcomeSuccess
	parentFinish.DurationUS = 200_000
	accumulator.recordLifecycle(dict, jhlog.Event{Operation: &parentFinish}, "Screen", Filter{})

	accumulator.recordSignal(jhlog.Event{RuntimeCall: &jhlog.RuntimeCallEvent{
		Count: 7, TotalMS: 70,
	}}, 11)

	if got := accumulator.activeName(11); got != "database.read" {
		t.Fatalf("completed operation name = %q, want database.read", got)
	}
	analysis := accumulator.finalize()
	child := findOperationStats(t, analysis.Operations, "database.read")
	parent := findOperationStats(t, analysis.Operations, "content.open")
	if child.CorrelatedRuntimeCalls != 7 || child.CorrelatedRuntimeTotalMS != 70 {
		t.Fatalf("late child signal = %+v", child)
	}
	if parent.CorrelatedRuntimeCalls != 7 || parent.CorrelatedRuntimeTotalMS != 70 {
		t.Fatalf("late signal was not rolled into completed parent: %+v", parent)
	}
	childSlot := findOperationTimeSlot(t, analysis.TimeSlots, "database.read")
	parentSlot := findOperationTimeSlot(t, analysis.TimeSlots, "content.open")
	if childSlot.CorrelatedRuntimeCalls != 7 || childSlot.CorrelatedRuntimeTotalMS != 70 ||
		parentSlot.CorrelatedRuntimeCalls != 7 || parentSlot.CorrelatedRuntimeTotalMS != 70 {
		t.Fatalf("late signal was not added to operation time slots: child=%+v parent=%+v", childSlot, parentSlot)
	}
	if analysis.LateSignalEvents != 1 || analysis.UnmatchedSignalEvents != 0 {
		t.Fatalf("late signal counters = %+v", analysis)
	}
}

func TestOperationAnalysisBoundsCompletedOperationContext(t *testing.T) {
	accumulator := newOperationAnalysisAccumulator()
	header := jhlog.DefaultSegmentHeader()
	header.ProcessInstanceID[0] = 1
	accumulator.startLog(header)
	dict := map[uint64]string{1: "content.open"}

	for id := uint64(1); id <= operationCompletedContextLimit+1; id++ {
		start := jhlog.OperationEvent{
			NameRef: jhlog.LocalSymbol(1), ID: id, Phase: jhlog.OperationPhaseStarted,
			Kind: jhlog.OperationKindUser,
		}
		accumulator.recordLifecycle(dict, jhlog.Event{Operation: &start}, "Screen", Filter{})
		finish := start
		finish.Phase = jhlog.OperationPhaseFinished
		finish.Outcome = jhlog.OperationOutcomeSuccess
		finish.DurationUS = 1_000
		accumulator.recordLifecycle(dict, jhlog.Event{Operation: &finish}, "Screen", Filter{})
	}

	if got := accumulator.activeName(1); got != "unknown" {
		t.Fatalf("evicted completed operation name = %q", got)
	}
	if got := accumulator.activeName(operationCompletedContextLimit + 1); got != "content.open" {
		t.Fatalf("latest completed operation name = %q", got)
	}
	analysis := accumulator.finalize()
	if analysis.CompletedContextEvictions != 1 {
		t.Fatalf("completed context evictions = %d, want 1", analysis.CompletedContextEvictions)
	}
}

func TestCompletedOperationStorePreservesNewestWindowUnderChurn(t *testing.T) {
	var store completedOperationStore
	const total = operationCompletedContextLimit * 3
	for id := uint64(1); id <= total; id++ {
		key := operationInstanceKey{id: id}
		key.process[id%uint64(len(key.process))] = byte(id)
		evicted := store.put(key, completedOperationContext{parentID: id - 1})
		if evicted != (id > operationCompletedContextLimit) {
			t.Fatalf("put(%d) evicted = %t", id, evicted)
		}
	}

	cutoff := uint64(total - operationCompletedContextLimit)
	for id := uint64(1); id <= total; id++ {
		key := operationInstanceKey{id: id}
		key.process[id%uint64(len(key.process))] = byte(id)
		context, found := store.get(key)
		wantFound := id > cutoff
		if found != wantFound {
			t.Fatalf("get(%d) found = %t, want %t", id, found, wantFound)
		}
		if found && context.parentID != id-1 {
			t.Fatalf("get(%d) parent = %d", id, context.parentID)
		}
	}

	latestKey := operationInstanceKey{id: total}
	latestKey.process[total%uint64(len(latestKey.process))] = byte(total % 256)
	if store.put(latestKey, completedOperationContext{parentID: 777}) {
		t.Fatal("updating an existing key unexpectedly evicted an entry")
	}
	if context, found := store.get(latestKey); !found || context.parentID != 777 {
		t.Fatalf("updated context = %+v, found %t", context, found)
	}
}

func TestOperationAnalysisBoundsConcurrentOperationsAndRecoversTheirFinishes(t *testing.T) {
	accumulator := newOperationAnalysisAccumulator()
	header := jhlog.DefaultSegmentHeader()
	header.ProcessInstanceID[0] = 1
	accumulator.startLog(header)
	dict := map[uint64]string{1: "content.open"}

	for id := uint64(1); id <= operationActiveLimit+1; id++ {
		accumulator.recordLifecycle(dict, jhlog.Event{Operation: &jhlog.OperationEvent{
			NameRef: jhlog.LocalSymbol(1), ID: id, Phase: jhlog.OperationPhaseStarted,
			Kind: jhlog.OperationKindUser,
		}}, "Screen", Filter{})
	}
	overflowID := uint64(operationActiveLimit + 1)
	accumulator.recordSignal(jhlog.Event{HTTP: &jhlog.HTTPEvent{}}, overflowID)
	accumulator.recordLifecycle(dict, jhlog.Event{Operation: &jhlog.OperationEvent{
		NameRef: jhlog.LocalSymbol(1), ID: overflowID, Phase: jhlog.OperationPhaseFinished,
		Kind: jhlog.OperationKindUser, Outcome: jhlog.OperationOutcomeSuccess, DurationUS: 10_000,
	}}, "Screen", Filter{})

	analysis := accumulator.finalize()
	if len(accumulator.active) != operationActiveLimit {
		t.Fatalf("active operations = %d, want %d", len(accumulator.active), operationActiveLimit)
	}
	if analysis.DroppedActiveStarts != 1 || analysis.DroppedSignalEvents != 1 {
		t.Fatalf("bounded lifecycle counters = %+v", analysis)
	}
	if analysis.MissingStart != 0 || analysis.Completed != 1 || len(analysis.Operations) != 1 {
		t.Fatalf("recovered overflow finish = %+v", analysis)
	}
}

func TestOperationAnalysisBoundsHighCardinalityGroups(t *testing.T) {
	accumulator := newOperationAnalysisAccumulator()
	header := jhlog.DefaultSegmentHeader()
	header.ProcessInstanceID[0] = 1
	accumulator.startLog(header)
	dict := make(map[uint64]string, operationGroupLimit+1)

	for id := uint64(1); id <= operationGroupLimit+1; id++ {
		dict[id] = fmt.Sprintf("operation.%d", id)
		start := jhlog.OperationEvent{
			NameRef: jhlog.LocalSymbol(id), ID: id, Phase: jhlog.OperationPhaseStarted,
			Kind: jhlog.OperationKindBackground,
		}
		accumulator.recordLifecycle(dict, jhlog.Event{Operation: &start}, "unknown", Filter{})
		finish := start
		finish.Phase = jhlog.OperationPhaseFinished
		finish.Outcome = jhlog.OperationOutcomeSuccess
		finish.DurationUS = id * 1_000
		accumulator.recordLifecycle(dict, jhlog.Event{Operation: &finish}, "unknown", Filter{})
	}

	analysis := accumulator.finalize()
	if len(analysis.Operations) != operationGroupLimit || analysis.DroppedOperationSamples != 1 {
		t.Fatalf("operation cardinality = rows %d, dropped %d", len(analysis.Operations), analysis.DroppedOperationSamples)
	}
	if len(analysis.TimeSlots) != operationGroupLimit || analysis.DroppedTimeSlotSamples != 0 {
		t.Fatalf("time slot cardinality = rows %d, dropped %d", len(analysis.TimeSlots), analysis.DroppedTimeSlotSamples)
	}
}

func TestOperationDurationSummaryKeepsSmallSamplesExact(t *testing.T) {
	var summary operationDurationSummary
	for value := uint64(1); value <= operationExactDurationLimit; value++ {
		summary.add(value)
	}

	if summary.approximated() {
		t.Fatal("small duration sample unexpectedly uses approximation")
	}
	if got := summary.percentile(0.50); got != 64 {
		t.Fatalf("p50 = %d, want 64", got)
	}
	if got := summary.percentile(0.90); got != 116 {
		t.Fatalf("p90 = %d, want 116", got)
	}
	if got := summary.percentile(0.95); got != 122 {
		t.Fatalf("p95 = %d, want 122", got)
	}
	if summary.max != operationExactDurationLimit {
		t.Fatalf("max = %d, want %d", summary.max, operationExactDurationLimit)
	}
}

func TestOperationDurationSummaryUsesBoundedDeterministicApproximation(t *testing.T) {
	const sampleSize = uint64(100_000)
	first := operationDurationSummary{}
	second := operationDurationSummary{}
	for value := uint64(1); value <= sampleSize; value++ {
		first.add(value)
		second.add(value)
	}

	if !first.approximated() || first.exact != nil {
		t.Fatalf("large duration sample retained exact values: %+v", first)
	}
	if first.count != sampleSize || first.max != sampleSize {
		t.Fatalf("summary bounds = count %d, max %d", first.count, first.max)
	}
	quantiles := []struct {
		probability float64
		want        uint64
	}{
		{probability: 0.50, want: 50_000},
		{probability: 0.90, want: 90_000},
		{probability: 0.95, want: 95_000},
	}
	for _, quantile := range quantiles {
		got := first.percentile(quantile.probability)
		repeated := second.percentile(quantile.probability)
		if got != repeated {
			t.Fatalf("p%.0f is not deterministic: %d != %d", quantile.probability*100, got, repeated)
		}
		error := absoluteUint64Difference(got, quantile.want)
		if error > sampleSize/100 {
			t.Fatalf("p%.0f = %d, want within 1%% of %d", quantile.probability*100, got, quantile.want)
		}
	}
}

func TestOperationDurationSummaryKeepsApproximateQuantilesOrderedAndBounded(t *testing.T) {
	var summary operationDurationSummary
	for index := uint64(0); index < 10_000; index++ {
		value := uint64(1)
		if index%7 == 0 {
			value = 1_000_000 - index
		} else if index%5 == 0 {
			value = index * index
		}
		summary.add(value)
	}

	p50 := summary.percentile(0.50)
	p90 := summary.percentile(0.90)
	p95 := summary.percentile(0.95)
	if p50 > p90 || p90 > p95 || p95 > summary.max {
		t.Fatalf("invalid approximate quantiles: p50=%d p90=%d p95=%d max=%d", p50, p90, p95, summary.max)
	}
}

func TestOperationIncidentHeapRetainsOnlyWorstIncidents(t *testing.T) {
	accumulator := newOperationAnalysisAccumulator()
	for score := uint64(1); score <= 1_000; score++ {
		accumulator.retainIncident(OperationIncident{Score: score, DurationMS: score})
	}

	if accumulator.incidents.Len() != operationIncidentLimit {
		t.Fatalf("incident count = %d, want %d", accumulator.incidents.Len(), operationIncidentLimit)
	}
	if accumulator.incidents[0].Score != 901 {
		t.Fatalf("least retained score = %d, want 901", accumulator.incidents[0].Score)
	}
	for _, incident := range accumulator.incidents {
		if incident.Score < 901 {
			t.Fatalf("retained non-worst incident: %+v", incident)
		}
	}
}

func TestOperationTimeSlotsRemainStableAtIntegerBoundaries(t *testing.T) {
	if got := operationSlot(0, -180); got != 0 {
		t.Fatalf("pre-epoch local slot = %d, want 0", got)
	}
	if got := operationSlot(math.MaxUint64, 840); got != math.MaxUint64-math.MaxUint64%operationHourMS {
		t.Fatalf("overflow fallback slot = %d", got)
	}
	nearSignedLimit := uint64(math.MaxInt64) - operationHourMS/2
	if got := operationSlot(nearSignedLimit, 840); got > nearSignedLimit {
		t.Fatalf("slot %d starts after event %d", got, nearSignedLimit)
	}
	if label := operationSlotLabel(nearSignedLimit, 840); !strings.Contains(label, "время вне диапазона") {
		t.Fatalf("overflow label = %q", label)
	}
	if label := operationSlotLabel(0, -180); label != "1969-12-31 21:00 (UTC-03:00)" {
		t.Fatalf("negative local time label = %q", label)
	}
	if _, ok := operationTimezoneOffsetMagnitudeMS(math.MinInt64); ok {
		t.Fatal("minimum integer timezone offset unexpectedly accepted")
	}
}

func BenchmarkOperationAnalysisHighVolume(b *testing.B) {
	dict := map[uint64]string{1: "content.open"}
	header := jhlog.DefaultSegmentHeader()
	header.ProcessInstanceID[0] = 1
	for iteration := 0; iteration < b.N; iteration++ {
		accumulator := newOperationAnalysisAccumulator()
		accumulator.startLog(header)
		for id := uint64(1); id <= 100_000; id++ {
			start := jhlog.OperationEvent{
				NameRef: jhlog.LocalSymbol(1), ID: id, Phase: jhlog.OperationPhaseStarted,
				Kind: jhlog.OperationKindUser, BudgetUS: 500_000,
			}
			accumulator.recordLifecycle(dict, jhlog.Event{Operation: &start}, "Screen", Filter{})
			finish := start
			finish.Phase = jhlog.OperationPhaseFinished
			finish.Outcome = jhlog.OperationOutcomeSuccess
			finish.DurationUS = (id%100_000 + 1) * 1_000
			accumulator.recordLifecycle(dict, jhlog.Event{Operation: &finish}, "Screen", Filter{})
		}
		benchmarkOperationAnalysisSink = accumulator.finalize()
	}
}

var benchmarkOperationAnalysisSink *OperationAnalysis

func absoluteUint64Difference(left, right uint64) uint64 {
	if left >= right {
		return left - right
	}
	return right - left
}

func writeOperationTestEvent(t *testing.T, writer *jhlog.Writer, event jhlog.Event) {
	t.Helper()
	if err := writer.WriteEvent(event); err != nil {
		t.Fatal(err)
	}
}

func findOperationStats(t *testing.T, values []OperationStats, name string) OperationStats {
	t.Helper()
	for _, value := range values {
		if value.Operation == name {
			return value
		}
	}
	t.Fatalf("operation %q not found in %+v", name, values)
	return OperationStats{}
}

func findOperationTimeSlot(t *testing.T, values []OperationTimeSlot, name string) OperationTimeSlot {
	t.Helper()
	for _, value := range values {
		if value.Operation == name {
			return value
		}
	}
	t.Fatalf("operation time slot %q not found in %+v", name, values)
	return OperationTimeSlot{}
}
