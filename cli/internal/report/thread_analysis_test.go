package report

import (
	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
	"path/filepath"
	"strings"
	"testing"
)

func TestThreadComparisonRejectsBackwardObservationClock(t *testing.T) {
	makeSummary := func(start, end uint64) analyze.Summary {
		return analyze.Summary{
			ThreadAnalysis: &analyze.ThreadAnalysis{Processes: []analyze.ThreadProcessAnalysis{{
				Process: "app", SamplingIntervalMS: 5000, StartAtMS: start, LastAtMS: end,
			}}},
			CollectionQuality: analyze.CollectionQuality{Complete: true, ProcessRosterComplete: true,
				ProcessScope: "all", ProcessScopeFingerprint: "app"},
		}
	}
	rows := threadComparison(analyze.Comparison{Baseline: makeSummary(200, 100), Candidate: makeSummary(100, 300),
		Scope: analyze.ComparisonScope{Comparability: analyze.ScenarioFull}})
	if len(rows) != 1 || rows[0].Comparable || strings.Contains(rows[0].Explanation, "18446744") ||
		!strings.Contains(rows[0].Explanation, "Временная ось") {
		t.Fatalf("backward observation clock became a numeric comparison: %+v", rows)
	}
}

func TestThreadReportHasObservationBoundsAndInteractiveViews(t *testing.T) {
	path := filepath.Join(t.TempDir(), "threads.html")
	summary := analyze.Summary{ThreadAnalysis: &analyze.ThreadAnalysis{Processes: []analyze.ThreadProcessAnalysis{{Process: "app", StartCount: 2, PeakCount: 3, LastCount: 1}}}}
	if err := WriteInspectWithOptions(path, summary, ReportOptions{}); err != nil {
		t.Fatal(err)
	}
	assertHTMLContains(t, path, `href="#threads"`, `id="threads"`, "Старт наблюдения", "между снимками", "data-thread-analysis", "Сравнить с №", "data-thread-link", "data-thread-exclude-owned", "data-thread-source", "data-thread-origin", "data-thread-factory-quality", "Поколение в событии попытки не записано")
}

func TestThreadComparisonDoesNotTurnMissingTelemetryIntoZero(t *testing.T) {
	rows := threadComparison(analyze.Comparison{Candidate: analyze.Summary{ThreadAnalysis: &analyze.ThreadAnalysis{Processes: []analyze.ThreadProcessAnalysis{{Process: "app", StartCount: 2, PeakCount: 3, LastCount: 1}}}}})
	if len(rows) != 1 || rows[0].Comparable || rows[0].Baseline != "нет наблюдений" {
		t.Fatalf("false comparison: %+v", rows)
	}
}

func TestThreadComparisonRejectsDifferentSamplingCoverage(t *testing.T) {
	makeSummary := func(interval uint64) analyze.Summary {
		return analyze.Summary{ThreadAnalysis: &analyze.ThreadAnalysis{Processes: []analyze.ThreadProcessAnalysis{{Process: "app", Snapshots: []analyze.ThreadSnapshotPoint{{Snapshot: &jhlog.ThreadSnapshotEvent{IntervalMS: interval}}}}}}}
	}
	rows := threadComparison(analyze.Comparison{Baseline: makeSummary(500), Candidate: makeSummary(5000)})
	if len(rows) != 1 || rows[0].Comparable {
		t.Fatalf("different observation coverage compared: %+v", rows)
	}
}

func TestThreadComparisonShowsNormalizedAppearanceChangeOnlyWhenMeasured(t *testing.T) {
	makeSummary := func(rate *float64) analyze.Summary {
		return analyze.Summary{ThreadAnalysis: &analyze.ThreadAnalysis{Processes: []analyze.ThreadProcessAnalysis{{
			Process: "app", SamplingIntervalMS: 5000, StartAtMS: 10, LastAtMS: 60010,
			Dynamics: &analyze.ThreadDynamics{AppearancesPerMinute: rate},
		}}}, CollectionQuality: analyze.CollectionQuality{Complete: true, ProcessRosterComplete: true,
			ProcessScope: "all", ProcessScopeFingerprint: "app"}}
	}
	a, b := 2.0, 3.0
	rows := threadComparison(analyze.Comparison{Baseline: makeSummary(&a), Candidate: makeSummary(&b),
		Scope: analyze.ComparisonScope{Comparability: analyze.ScenarioFull}})
	if len(rows) != 1 || !rows[0].Comparable || !strings.Contains(rows[0].Explanation, "изменение появлений/мин +1.00") {
		t.Fatalf("normalized rate not compared: %+v", rows)
	}
	rows = threadComparison(analyze.Comparison{Baseline: makeSummary(nil), Candidate: makeSummary(&b),
		Scope: analyze.ComparisonScope{Comparability: analyze.ScenarioFull}})
	if strings.Contains(rows[0].Explanation, "изменение появлений/мин") {
		t.Fatal("missing baseline rate treated as zero")
	}
}

func TestThreadComparisonShowsMeasuredCoreLoadChangeOnlyWithComparableSamples(t *testing.T) {
	makeSummary := func(cpu *float64, samples int) analyze.Summary {
		return analyze.Summary{ThreadAnalysis: &analyze.ThreadAnalysis{Processes: []analyze.ThreadProcessAnalysis{{
			Process: "app", SamplingIntervalMS: 5000, StartAtMS: 10, LastAtMS: 60010,
			Dynamics: &analyze.ThreadDynamics{MeanObservedThreadCorePercent: cpu, CPUSamples: samples},
		}}}, CollectionQuality: analyze.CollectionQuality{Complete: true, ProcessRosterComplete: true,
			ProcessScope: "all", ProcessScopeFingerprint: "app"}}
	}
	a, b := 30.0, 45.0
	rows := threadComparison(analyze.Comparison{Baseline: makeSummary(&a, 5), Candidate: makeSummary(&b, 8),
		Scope: analyze.ComparisonScope{Comparability: analyze.ScenarioFull}})
	if len(rows) != 1 || !strings.Contains(rows[0].Explanation, "изменение средней нагрузки +15.00 п.п.") ||
		!strings.Contains(rows[0].Explanation, "состав наблюдавшихся потоков") {
		t.Fatalf("CPU comparison missing its evidence or caveat: %+v", rows)
	}
	rows = threadComparison(analyze.Comparison{Baseline: makeSummary(nil, 0), Candidate: makeSummary(&b, 8),
		Scope: analyze.ComparisonScope{Comparability: analyze.ScenarioFull}})
	if strings.Contains(rows[0].Explanation, "изменение средней нагрузки") {
		t.Fatal("missing CPU measurement became zero")
	}
}

func TestThreadComparisonPreservesCadenceAfterSnapshotProjection(t *testing.T) {
	summary := analyze.Summary{ThreadAnalysis: &analyze.ThreadAnalysis{HistoryOmitted: true, Processes: []analyze.ThreadProcessAnalysis{{Process: "app", SamplingIntervalMS: 5000}}}}
	summary.CollectionQuality = analyze.CollectionQuality{Complete: true, ProcessRosterComplete: true, ProcessScope: "all", ProcessScopeFingerprint: "app"}
	rows := threadComparison(analyze.Comparison{Baseline: summary, Candidate: summary, Scope: analyze.ComparisonScope{Comparability: analyze.ScenarioFull}})
	if len(rows) != 1 || !rows[0].Comparable {
		t.Fatalf("lost cadence: %+v", rows)
	}
}

func TestThreadComparisonRejectsUnknownScenarioAndIncompleteProcessScope(t *testing.T) {
	s := analyze.Summary{ThreadAnalysis: &analyze.ThreadAnalysis{Processes: []analyze.ThreadProcessAnalysis{{Process: "app", SamplingIntervalMS: 5000}}}}
	if threadComparison(analyze.Comparison{Baseline: s, Candidate: s})[0].Comparable {
		t.Fatal("unknown scenario produced a thread delta")
	}
	if threadComparison(analyze.Comparison{Baseline: s, Candidate: s, Scope: analyze.ComparisonScope{Comparability: analyze.ScenarioFull}})[0].Comparable {
		t.Fatal("unknown process roster produced a thread delta")
	}
}

func TestThreadDynamicsReportShowsMeasuredCPUAndSignalLimitations(t *testing.T) {
	cpu := 30.0
	path := filepath.Join(t.TempDir(), "dynamics.html")
	s := analyze.Summary{ThreadAnalysis: &analyze.ThreadAnalysis{Processes: []analyze.ThreadProcessAnalysis{{Process: "app", Dynamics: &analyze.ThreadDynamics{CPUSamples: 2, MeanObservedThreadCorePercent: &cpu, Signals: []analyze.ThreadSignal{{Code: "observed_growth", Evidence: "4 снимка", Limitation: "не доказательство утечки"}}}}}}}
	if err := WriteInspectWithOptions(path, s, ReportOptions{}); err != nil {
		t.Fatal(err)
	}
	assertHTMLContains(t, path, "30.00% одного ядра", "не доказательство утечки", "Это не полное CPU-время процесса")
}

func TestInspectShowsRunningCoroutineWithoutInventedEnd(t *testing.T) {
	observedThrough := uint64(120)
	path := filepath.Join(t.TempDir(), "running-coroutine.html")
	summary := analyze.Summary{ThreadAnalysis: &analyze.ThreadAnalysis{CoroutineSegments: []analyze.ThreadCoroutineSegment{{
		Scope: "scope-a", Process: "app", Token: "81", Generation: "4", Owner: "app.Feed.load",
		EntryJavaThreadID: "11", EntryTID: "151", StartedElapsedMS: 100,
		ObservedThroughElapsedMS: &observedThrough, State: "running_observed",
	}}}}
	if err := WriteInspectWithOptions(path, summary, ReportOptions{}); err != nil {
		t.Fatal(err)
	}
	assertHTMLContains(t, path, "Корутины, наблюдавшиеся на потоках", "running_observed", `"EndedElapsedMS":null`)
}
