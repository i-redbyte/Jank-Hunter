package mathanalysis

import (
	"errors"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
)

func TestCollectionAccountsShareOneLimitAndReleaseTheirOwnStorage(t *testing.T) {
	budget := newCollectionBudget(1024)
	routes, network := budget.account("routes"), budget.account("network")
	if !routes.reserve(512) || !network.reserve(512) || routes.reserve(1) {
		t.Fatal("collectors did not share their limit")
	}
	var exhausted *collectionBudgetError
	if !errors.As(budget.err(), &exhausted) || exhausted.limit.Component != "routes" || budget.used != 1024 || budget.peak != 1024 {
		t.Fatalf("invalid shared limit: %+v", budget)
	}
	routes.close()
	if budget.used != 512 {
		t.Fatal("release removed another collector's lease")
	}
	network.close()
	if budget.used != 0 || network.reserve(1) {
		t.Fatal("terminal exhausted budget was reopened")
	}
}

func TestCollectionBudgetChargesGrowthBeforeReleasingOldArrayAndRejectsOverflow(t *testing.T) {
	budget := newCollectionBudget(1024)
	account := budget.account("array")
	if !account.reserve(256) || !account.reserve(512) {
		t.Fatal(budget.err())
	}
	account.release(256)
	if budget.used != 512 || budget.peak != 768 || account.canReserve(1024) || budget.err() != nil {
		t.Fatal("invalid growth accounting")
	}
	if account.reserveItems(int(^uint(0)>>1), 16) || budget.used != 512 {
		t.Fatal("overflow wrapped the reservation")
	}
	account.close()
	if budget.used != 0 {
		t.Fatal("array lease retained after close")
	}
}

func TestComparisonUsesOneBudgetAcrossBothIndependentCollections(t *testing.T) {
	baseline, candidate := writeDNSLoopFixture(t, true), writeDNSLoopFixture(t, true)
	var peak, retained uint64
	for _, path := range []string{baseline, candidate} {
		budget := newCollectionBudget(0)
		if _, err := analyzeMathInputsWithBudget([]string{path}, analyze.Options{}, budget); err != nil {
			t.Fatal(err)
		}
		if budget.peak > peak {
			peak = budget.peak
		}
		if retained == 0 || budget.used < retained {
			retained = budget.used
		}
	}
	if retained == 0 {
		t.Fatal("collected data was not charged")
	}
	limit := peak + retained/2
	options := analyze.Options{MathMemoryLimitBytes: limit}
	for _, path := range []string{baseline, candidate} {
		individual, err := analyzeInspectForTest(t, []string{path}, options)
		if err != nil || len(individual.CollectionLimits) != 0 {
			t.Fatalf("individual input should fit: %v %+v", err, individual.CollectionLimits)
		}
	}
	combined, err := analyzeCompareForTest(t, []string{baseline}, []string{candidate}, options)
	if err != nil || len(combined.CollectionLimits) != 1 {
		t.Fatalf("comparison exceeded its shared budget without diagnostic: %v %+v", err, combined.CollectionLimits)
	}
}

func TestRepeatedHistogramSamplesDoNotConsumePerEventMemoryBudget(t *testing.T) {
	budget := newCollectionBudget(256 * 1024)
	collector := robustCollector{samples: robustSampleMap{}, account: budget.account("robust samples")}
	for i := 0; i < 100_000; i++ {
		collector.addValue("route", "same", "latency", "ms", 7)
	}
	if budget.err() != nil || budget.peak > budget.limit {
		t.Fatalf("bounded repeated samples exhausted their budget: %v", budget.err())
	}
	stats := summarizeRobustSamples(collector.samples)
	if len(stats) != 1 || stats[0].Count != 100_000 || stats[0].Median != 7 {
		t.Fatalf("histogram lost samples: %+v", stats)
	}
	if budget.used > 4096 {
		t.Fatalf("compressed samples retain%dchargedbytes", budget.used)
	}
}

func TestMathCollectionBudgetProducesExplicitUnavailableAnalysis(t *testing.T) {
	path := writeDNSLoopFixture(t, true)
	report, err := analyzeInspectForTest(t, []string{path}, analyze.Options{MathMemoryLimitBytes: 1024})
	if err != nil {
		t.Fatalf("budget exhaustion must keep a report with explicit collection diagnostics: %v", err)
	}
	if len(report.CollectionLimits) != 1 {
		t.Fatalf("expected one explicit collection limit, got%+v", report.CollectionLimits)
	}
	limit := report.CollectionLimits[0]
	if limit.LimitBytes != 1024 || limit.ReservedBytes > limit.LimitBytes || limit.RequestedBytes == 0 || limit.Component == "" {
		t.Fatalf("invalid collection budget accounting: %+v", limit)
	}
	if len(report.Timeline)+len(report.Series)+len(report.RobustStats)+len(report.NetworkLoops)+len(report.Periodic) != 0 {
		t.Fatal("budget-exhausted analysis exposed partial statistics as complete")
	}
	if report.Summary.HTTPCount == 0 {
		t.Fatal("unavailable mathematics discarded the source problem summary")
	}
}

func TestCompareMathCollectionBudgetCannotSilentlyDropItsDiagnostics(t *testing.T) {
	baseline := writeDNSLoopFixture(t, true)
	candidate := writeDNSLoopFixture(t, false)
	report, err := analyzeCompareForTest(t, []string{baseline}, []string{candidate}, analyze.Options{MathMemoryLimitBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.CollectionLimits) != 1 {
		t.Fatalf("missing comparison collection limit: %+v", report.CollectionLimits)
	}
	if len(report.RobustDeltas)+len(report.NetworkLoopDeltas)+len(report.IntegralDeltas) != 0 {
		t.Fatal("budget-exhausted comparison exposed partial deltas")
	}
}
