package analyze

import (
	"math"
	"math/rand"
	"testing"
)

func TestZeroEventUpperBoundPolicyCoverageSimulation(t *testing.T) {
	const exposure = uint64(100)
	upper, ok := zeroEventUpperRate95(exposure)
	if !ok {
		t.Fatal("upper bound unavailable")
	}
	if exactZeroProbability := math.Pow(1-upper, float64(exposure)); exactZeroProbability > 0.05 {
		t.Fatalf("rule-of-three undercovers: P(zero)=%.6f", exactZeroProbability)
	}
	random := rand.New(rand.NewSource(32151))
	const trials = 100_000
	zero := 0
	for range trials {
		observed := false
		for range exposure {
			if random.Float64() < upper {
				observed = true
				break
			}
		}
		if !observed {
			zero++
		}
	}
	coverageFailure := float64(zero) / trials
	if coverageFailure > 0.05 || coverageFailure < 0.04 {
		t.Fatalf("seeded coverage simulation outside calibrated range: %.6f", coverageFailure)
	}
}

func TestComparisonEffectDoesNotInventEvidence(t *testing.T) {
	valid := comparisonMeasurement{Value: 100, Samples: 20, State: measurementObserved}
	policy := comparisonBand{Absolute: 1, Relative: .05}
	for _, tc := range []struct {
		name          string
		before, after comparisonMeasurement
		eligible      bool
		want          ComparisonChange
	}{
		{"missing is not zero", comparisonMeasurement{}, valid, true, ChangeInsufficientData},
		{"missing after is not improvement", valid, comparisonMeasurement{}, true, ChangeInsufficientData},
		{"incompatible observations", valid, valid, false, ChangeNotComparable},
		{"zero sample", comparisonMeasurement{State: measurementObserved, Value: 100}, valid, true, ChangeInsufficientData},
		{"nan", comparisonMeasurement{State: measurementObserved, Value: math.NaN(), Samples: 20}, valid, true, ChangeInsufficientData},
		{"infinity", valid, comparisonMeasurement{State: measurementObserved, Value: math.Inf(1), Samples: 20}, true, ChangeInsufficientData},
		{"small difference", valid, comparisonMeasurement{State: measurementObserved, Value: 104, Samples: 20}, true, ChangeUnchanged},
		{"boundary", valid, comparisonMeasurement{State: measurementObserved, Value: 105, Samples: 20}, true, ChangeUnchanged},
		{"regression", valid, comparisonMeasurement{State: measurementObserved, Value: 106, Samples: 20}, true, ChangeRegressed},
		{"improvement", valid, comparisonMeasurement{State: measurementObserved, Value: 94, Samples: 20}, true, ChangeImproved},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := compareMeasurement(tc.before, tc.after, policy, tc.eligible, true)
			if got.Change != tc.want {
				t.Fatalf("got %+v, want %s", got, tc.want)
			}
		})
	}
}

func TestComparisonEffectDirectionAndZeroBaseline(t *testing.T) {
	before := comparisonMeasurement{State: measurementObserved, Value: 0, Samples: 20}
	after := comparisonMeasurement{State: measurementObserved, Value: 10, Samples: 20}
	got := compareMeasurement(before, after, comparisonBand{Absolute: 1, Relative: .05}, true, false)
	if got.Change != ChangeImproved || got.RelativeKnown || got.Absolute != 10 {
		t.Fatalf("zero baseline or direction lost: %+v", got)
	}
	before.Value = 100
	after.Value = 90
	got = compareMeasurement(before, after, comparisonBand{Absolute: 1, Relative: .05}, true, false)
	if got.Change != ChangeRegressed || !got.RelativeKnown || math.Abs(got.Relative+.1) > 1e-12 {
		t.Fatalf("higher is better: %+v", got)
	}
	for _, band := range []comparisonBand{{Absolute: -1}, {Relative: -1}, {Absolute: math.NaN()}, {Relative: math.Inf(1)}} {
		if got := compareMeasurement(before, after, band, true, true); got.Change != ChangeInsufficientData {
			t.Fatalf("invalid band accepted: %+v", got)
		}
	}
}

func TestComparisonOutcomePreservesMixedAndUnknown(t *testing.T) {
	for _, tc := range []struct {
		changes []ComparisonChange
		want    ComparisonChange
	}{
		{nil, ChangeInsufficientData},
		{[]ComparisonChange{ChangeUnchanged}, ChangeUnchanged},
		{[]ComparisonChange{ChangeUnchanged, ChangeInsufficientData}, ChangeInsufficientData},
		{[]ComparisonChange{ChangeImproved, ChangeRegressed}, ChangeMixed},
		{[]ComparisonChange{ChangeRegressed, ChangeImproved}, ChangeMixed},
		{[]ComparisonChange{ChangeImproved, ChangeUnchanged}, ChangeImproved},
		{[]ComparisonChange{ChangeNotComparable}, ChangeNotComparable},
		{[]ComparisonChange{ChangeMixed, ChangeUnchanged}, ChangeMixed},
	} {
		var aggregate comparisonOutcome
		for _, change := range tc.changes {
			aggregate.add(change)
		}
		if got := aggregate.change(); got != tc.want {
			t.Fatalf("%v: got %s, want %s", tc.changes, got, tc.want)
		}
	}
}

func TestComparisonEffectUsesNoHeapAllocations(t *testing.T) {
	before := comparisonMeasurement{State: measurementObserved, Value: 100, Samples: 20}
	after := comparisonMeasurement{State: measurementObserved, Value: 110, Samples: 20}
	if got := testing.AllocsPerRun(1000, func() {
		comparisonEffectSink = compareMeasurement(before, after, comparisonBand{Absolute: 1, Relative: .05}, true, true)
	}); got != 0 {
		t.Fatalf("allocations per metric: %v", got)
	}
}

var comparisonEffectSink comparisonEffect

func BenchmarkComparisonEffect(b *testing.B) {
	before := comparisonMeasurement{State: measurementObserved, Value: 100, Samples: 20}
	after := comparisonMeasurement{State: measurementObserved, Value: 110, Samples: 20}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		comparisonEffectSink = compareMeasurement(before, after, comparisonBand{Absolute: 1, Relative: .05}, true, true)
	}
}
