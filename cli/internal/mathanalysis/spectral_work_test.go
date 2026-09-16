package mathanalysis

import (
	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"testing"
)

func TestSpectralWorkLimitMakesInspectAndCompareUnavailable(t *testing.T) {
	path := writeDNSLoopFixture(t, true)
	options := analyze.Options{MathSpectralWorkLimitOperations: 1}
	inspect, err := analyzeInspectForTest(t, []string{path}, options)
	if err != nil {
		t.Fatal(err)
	}
	if len(inspect.CollectionLimits) != 1 || inspect.CollectionLimits[0].Work == nil {
		t.Fatal("spectral work exceeded the limit without an explicit unavailable report")
	}
	limit := inspect.CollectionLimits[0].Work
	if limit.LimitOperations != 1 || limit.ConsumedOperations > 1 || limit.RequestedOperations <= 1 {
		t.Fatalf("invalid work bound: %+v", limit)
	}
	if len(inspect.NetworkLoops)+len(inspect.Periodic)+len(inspect.RobustStats) != 0 || inspect.Summary.HTTPCount == 0 {
		t.Fatal("work failure published partial mathematics or lost source summary")
	}
	comparison, err := analyzeCompareForTest(t, []string{path}, []string{path}, options)
	if err != nil {
		t.Fatal(err)
	}
	if len(comparison.CollectionLimits) != 1 || comparison.CollectionLimits[0].Work == nil || len(comparison.NetworkLoopDeltas) != 0 {
		t.Fatal("comparison hid spectral work exhaustion")
	}
}

func TestSpectralWorkIsSharedAcrossSignalsAndScratchIsReleased(t *testing.T) {
	points := make([]float64, 128)
	for i := range points {
		points[i] = float64(i % 4)
	}
	cost := uint64(len(points)) + spectralWorkRequirement(len(points))
	budget := newCollectionBudgetWithWork(0, 2*cost-1)
	first := analyzePeriodicSignalWithBudget("first", "count", 1000, points, budget)
	if budget.err() != nil || budget.workUsed != cost || first.AnalyzedSampleCount != 128 || budget.used != 0 {
		t.Fatal("first analysis lost exact admission or retained scratch")
	}
	second := analyzePeriodicSignalWithBudget("second", "count", 1000, points, budget)
	if budget.failure == nil || budget.failure.limit.Work == nil || budget.workUsed > budget.workLimit || second.AnalyzedSampleCount != 0 {
		t.Fatal("second signal bypassed shared work quota")
	}
	if budget.chargeSpectralWork(0) {
		t.Fatal("exhausted work quota reopened")
	}
}

func TestComparisonSharesSpectralWorkAcrossBothSides(t *testing.T) {
	path := writeDNSLoopFixture(t, true)
	budget := newCollectionBudget(0)
	inputs, err := analyzeMathInputsWithBudget([]string{path}, analyze.Options{}, budget)
	if err != nil {
		t.Fatal(err)
	}
	buildPeriodicAnalysisWithBudget(inputs.Timeline, inputs.Scale, inputs.RouteDefinitions, budget)
	if budget.err() != nil || budget.workUsed == 0 {
		t.Fatal("fixture must complete nonzero work")
	}
	options := analyze.Options{MathSpectralWorkLimitOperations: budget.workUsed*2 - 1}
	single, err := analyzeInspectForTest(t, []string{path}, options)
	if err != nil || len(single.CollectionLimits) != 0 {
		t.Fatal("one side must fit")
	}
	combined, err := analyzeCompareForTest(t, []string{path}, []string{path}, options)
	if err != nil || len(combined.CollectionLimits) != 1 || combined.CollectionLimits[0].Work == nil {
		t.Fatal("two sides silently obtained independent work quotas")
	}
}

func TestImpossibleSparseNetworkCandidatesAreRejectedBeforeSpectralWork(t *testing.T) {
	budget := newCollectionBudgetWithWork(0, 1)
	collector := networkLoopCollector{budget: budget, bucketSize: 50000, signals: map[string]*networkLoopSignal{
		"one": {name: "one", sparse: bucketSeries{positiveBuckets: 1}},
		"two": {name: "two", sparse: bucketSeries{positiveBuckets: 2}},
	}}
	if got := collector.findings(); len(got) != 0 || budget.err() != nil || budget.workUsed != 0 {
		t.Fatal("impossible candidate reached expensive analysis")
	}
}

func TestIrregularRawEventTimesBecomeUniformPeriodicBuckets(t *testing.T) {
	// Fixture events are clustered at100/180/260ms and separated by long gaps.
	// FFT consumes the complete fixed-width time grid, not raw event indexes.
	path := writeDNSLoopFixture(t, true)
	inputs, err := analyzeMathInputs([]string{path}, analyze.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for i, bucket := range inputs.Timeline {
		if bucket.EndMS-bucket.StartMS != inputs.Scale.bucketMS || (i > 0 && bucket.StartMS != inputs.Timeline[i-1].EndMS) {
			t.Fatal("irregular arrivals were concatenated as uniform observations")
		}
	}
	signals, _ := buildPeriodicAnalysisWithBudget(inputs.Timeline, inputs.Scale, inputs.RouteDefinitions, inputs.budget)
	for _, signal := range signals {
		if signal.Signal == "HTTP запросы" {
			if signal.FirstSignificantLagMS != 4000 || signal.AnalyzedSampleCount != len(inputs.Timeline) {
				t.Fatalf("raw arrival spacing altered periodic timebase: %+v", signal)
			}
			return
		}
	}
	t.Fatal("lost HTTP periodic signal")
}
