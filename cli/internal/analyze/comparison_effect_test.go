package analyze

import (
	"math"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func scopedComparisonPair() (Summary, Summary) {
	baseline := comparisonScopeInput("01000000000000000000000000000000", "11000000000000000000000000000000", "com.example.app")
	candidate := comparisonScopeInput("02000000000000000000000000000000", "22000000000000000000000000000000", "com.example.app")
	return baseline, candidate
}

func appendScopedProfile(summary *Summary, name string, count, failures, totalMS, p95 uint64) {
	p := matchTestProfile(name, summary.OperationAnalysis.Profiles[0].RunID, count)
	p.ProcessInstanceID = summary.OperationAnalysis.Profiles[0].ProcessInstanceID
	p.ProcessName = summary.OperationAnalysis.Profiles[0].ProcessName
	p.Stats.Failures = failures
	p.Stats.Success = count - failures
	p.Stats.TotalMS = totalMS
	p.Stats.P95MS = p95
	summary.OperationAnalysis.Profiles = append(summary.OperationAnalysis.Profiles, p)
}

func metricChangeByName(t *testing.T, changes []MetricChange, name string) MetricChange {
	t.Helper()
	for _, change := range changes {
		if change.Name == name {
			return change
		}
	}
	t.Fatalf("missing metric %s: %+v", name, changes)
	return MetricChange{}
}

func TestScopedEffectsUseCommonBaselineWeightsAndIgnoreUnmatchedWork(t *testing.T) {
	baseline, candidate := scopedComparisonPair()
	baseline.OperationAnalysis.Profiles[0].Stats = OperationStats{Operation: "chat.open", Kind: "user", Screen: "Chat", Count: 90, Success: 81, Failures: 9, TotalMS: 9_000, P95MS: 200}
	candidate.OperationAnalysis.Profiles[0].Stats = OperationStats{Operation: "chat.open", Kind: "user", Screen: "Chat", Count: 10, Success: 10, TotalMS: 800, P95MS: 150}
	appendScopedProfile(&baseline, "search", 10, 9, 2_000, 400)
	appendScopedProfile(&candidate, "search", 90, 72, 13_500, 350)
	// Each scope improved, while the unstandardized aggregate failure rate rose
	// from 18% to 72%. Baseline work weights must preserve the within-scope result.
	appendScopedProfile(&candidate, "attachments", 10_000, 10_000, 9_000_000, 20_000)

	comparison := Compare(baseline, candidate)
	if comparison.Scope.Comparability != ScenarioPartial {
		t.Fatalf("want partial: %+v", comparison.Scope)
	}
	failure := metricChangeByName(t, comparison.Scope.Changes, "Operation failure rate")
	if failure.Change != ChangeImproved || failure.Before == nil || failure.After == nil || *failure.Before != 18 || *failure.After != 8 {
		t.Fatalf("Simpson mixture leaked into common scope: %+v", failure)
	}
	if comparison.Outcome != ChangeImproved {
		t.Fatalf("outcome=%s, changes=%+v", comparison.Outcome, comparison.Scope.Changes)
	}
}

func TestScopedEffectsKeepOpposingDirectionsMixed(t *testing.T) {
	baseline, candidate := scopedComparisonPair()
	baseline.OperationAnalysis.Profiles[0].Stats = OperationStats{Operation: "chat.open", Kind: "user", Screen: "Chat", Count: 40, Success: 38, Failures: 2, TotalMS: 8_000}
	candidate.OperationAnalysis.Profiles[0].Stats = OperationStats{Operation: "chat.open", Kind: "user", Screen: "Chat", Count: 40, Success: 30, Failures: 10, TotalMS: 4_000}
	comparison := Compare(baseline, candidate)
	if comparison.Outcome != ChangeMixed {
		t.Fatalf("opposing effects collapsed: %+v", comparison.Scope.Changes)
	}
}

func TestScopedEffectsDoNotAverageRunPercentilesOrClaimResultFromUnknownData(t *testing.T) {
	baseline, candidate := scopedComparisonPair()
	appendScopedProfile(&baseline, "chat.open", 40, 0, 4_000, 10_000)
	appendScopedProfile(&candidate, "chat.open", 40, 0, 4_000, 1)
	baseline.OperationAnalysis.Profiles[1].RunID = "03000000000000000000000000000000"
	baseline.OperationAnalysis.Profiles[1].ProcessInstanceID = "33000000000000000000000000000000"
	candidate.OperationAnalysis.Profiles[1].RunID = "04000000000000000000000000000000"
	candidate.OperationAnalysis.Profiles[1].ProcessInstanceID = "44000000000000000000000000000000"
	// P95 differs wildly between runs, but the mergeable duration sum/count is equal.
	comparison := Compare(baseline, candidate)
	duration := metricChangeByName(t, comparison.Scope.Changes, "Operation mean duration")
	if duration.Change != ChangeInsufficientData || duration.Interval == nil ||
		duration.Before == nil || duration.After == nil || *duration.Before != *duration.After {
		t.Fatalf("run percentiles were averaged or repeated uncertainty was hidden: %+v", duration)
	}

	candidate.CollectionQuality.KnownLostEvents = 1
	comparison = Compare(baseline, candidate)
	if comparison.Outcome != ChangeInsufficientData || len(comparison.Scope.Changes) != 0 {
		t.Fatalf("loss produced effect: outcome=%s changes=%+v", comparison.Outcome, comparison.Scope.Changes)
	}
}

func TestScopedEffectsDoNotTreatZeroBaselineAsRelativeInfinity(t *testing.T) {
	baseline, candidate := scopedComparisonPair()
	baseline.OperationAnalysis.Profiles[0].Stats.Failures = 0
	candidate.OperationAnalysis.Profiles[0].Stats.Failures = 4
	comparison := Compare(baseline, candidate)
	change := metricChangeByName(t, comparison.Scope.Changes, "Operation failure rate")
	if change.Change != ChangeRegressed || change.Relative != nil || change.Absolute == nil || *change.Absolute != 10 {
		t.Fatalf("zero baseline mishandled: %+v", change)
	}
}

func appendRunMetric(summary *Summary, run byte, meanMS uint64) {
	p := matchTestProfile("chat.open", idHex(run), 10)
	p.RunID = idHex(run)
	p.ProcessInstanceID = idHex(run + 100)
	p.ProcessName = "com.example.app"
	p.Stats.Count = 10
	p.Stats.Success = 10
	p.Stats.TotalMS = meanMS * 10
	summary.OperationAnalysis.Profiles = append(summary.OperationAnalysis.Profiles, p)
}

func idHex(value byte) string {
	const digits = "0123456789abcdef"
	result := make([]byte, 32)
	for i := range result {
		result[i] = '0'
	}
	result[0], result[1] = digits[value>>4], digits[value&15]
	return string(result)
}

func TestRepeatedScopedEffectUsesWelchIntervalForVerdict(t *testing.T) {
	baseline, candidate := scopedComparisonPair()
	baseline.OperationAnalysis.Profiles = nil
	candidate.OperationAnalysis.Profiles = nil
	for index, value := range []uint64{99, 100, 101, 100, 100} {
		appendRunMetric(&baseline, byte(index+1), value)
		appendRunMetric(&candidate, byte(index+21), value+20)
	}
	change := metricChangeByName(t, Compare(baseline, candidate).Scope.Changes, "Operation mean duration")
	if change.Interval == nil || change.Interval.Method != "welch_unpaired" ||
		change.Interval.BaselineGroups != 5 || change.Interval.CandidateGroups != 5 {
		t.Fatalf("missing run interval: %+v", change)
	}
	if change.Change != ChangeRegressed || change.Confidence != ConfidenceStrong ||
		change.Interval.Lower <= 5 || change.Interval.Upper < change.Interval.Lower {
		t.Fatalf("stable regression was not established: %+v", change)
	}
}

func TestRepeatedScopedEffectBecomesInsufficientWhenIntervalCrossesBand(t *testing.T) {
	baseline, candidate := scopedComparisonPair()
	baseline.OperationAnalysis.Profiles = nil
	candidate.OperationAnalysis.Profiles = nil
	for index, value := range []uint64{60, 80, 100, 120, 140} {
		appendRunMetric(&baseline, byte(index+1), value)
		appendRunMetric(&candidate, byte(index+21), value+10)
	}
	change := metricChangeByName(t, Compare(baseline, candidate).Scope.Changes, "Operation mean duration")
	if change.Interval == nil || change.Change != ChangeInsufficientData {
		t.Fatalf("wide interval produced a directional claim: %+v", change)
	}
}

func TestWelchIntervalCalibrationForNormalDifference(t *testing.T) {
	const trials = 100000
	covered := 0
	state := uint64(0x9e3779b97f4a7c15)
	for trial := 0; trial < trials; trial++ {
		left, right := make([]float64, 12), make([]float64, 12)
		for i := range left {
			left[i] = deterministicNormal(&state)
			right[i] = deterministicNormal(&state) + 0.5
		}
		interval, ok := welchDifferenceInterval95(left, right, meanForTest(right)-meanForTest(left))
		if !ok {
			t.Fatal("interval unavailable")
		}
		if interval.Lower <= 0.5 && interval.Upper >= 0.5 {
			covered++
		}
	}
	rate := float64(covered) / trials
	if rate < 0.94 || rate > 0.96 {
		t.Fatalf("coverage %.4f outside [0.94, 0.96]", rate)
	}
}

func TestScopedEffectsIncludeCorrelatedPerformanceDomains(t *testing.T) {
	baseline, candidate := scopedComparisonPair()
	left := &baseline.OperationAnalysis.Profiles[0].Stats
	right := &candidate.OperationAnalysis.Profiles[0].Stats
	left.Count, right.Count = 10, 10
	left.MaxPSSKB, right.MaxPSSKB = 100_000, 80_000
	left.CorrelatedRetainedObjects, right.CorrelatedRetainedObjects = 20, 10
	left.CorrelatedCPUSumX100, left.CorrelatedCPUSamples = 60_000, 10
	right.CorrelatedCPUSumX100, right.CorrelatedCPUSamples = 40_000, 10
	left.CorrelatedIODurationUS, right.CorrelatedIODurationUS = 200_000, 100_000
	left.CorrelatedIOBytes, right.CorrelatedIOBytes = 30_000, 10_000
	left.CorrelatedIOBytesKnown, right.CorrelatedIOBytesKnown = 2, 2
	left.CorrelatedHTTPDurationMS, right.CorrelatedHTTPDurationMS = 2_000, 1_000
	left.CorrelatedHTTPRxBytes, left.CorrelatedHTTPTxBytes, left.CorrelatedHTTPBytesKnown = 40_000, 10_000, 2
	right.CorrelatedHTTPRxBytes, right.CorrelatedHTTPTxBytes, right.CorrelatedHTTPBytesKnown = 20_000, 5_000, 2

	changes := Compare(baseline, candidate).Scope.Changes
	for _, name := range []string{
		"Operation max PSS", "Retained objects per operation", "Operation CPU average",
		"I/O duration per operation", "I/O bytes per operation", "HTTP duration per operation", "Network bytes per operation",
	} {
		change := metricChangeByName(t, changes, name)
		if change.Change != ChangeImproved {
			t.Fatalf("%s = %+v", name, change)
		}
	}
}

func TestScopedPerformanceDomainRequiresCollectorAndKnownByteExposure(t *testing.T) {
	baseline, candidate := scopedComparisonPair()
	baseline.OperationAnalysis.Profiles[0].Stats.CorrelatedIOBytes = 100
	candidate.OperationAnalysis.Profiles[0].Stats.CorrelatedIOBytes = 50
	baseline.OperationAnalysis.Profiles[0].Stats.CorrelatedIOBytesKnown = 1
	if changes := Compare(baseline, candidate).Scope.Changes; hasMetricChange(changes, "I/O bytes per operation") {
		t.Fatalf("unknown candidate byte exposure compared: %+v", changes)
	}
	candidate.CollectorFlagsAll &^= uint64(jhlog.CollectorSystemSampler)
	result := Compare(baseline, candidate)
	if eligibilityByDomain(t, result.Scope.Metrics, DomainCPU).State != EligibilityIneligible ||
		eligibilityByDomain(t, result.Scope.Metrics, DomainMemory).State != EligibilityIneligible {
		t.Fatalf("disabled system collector hidden: %+v", result.Scope.Metrics)
	}
}

func hasMetricChange(changes []MetricChange, name string) bool {
	for _, change := range changes {
		if change.Name == name {
			return true
		}
	}
	return false
}

func meanForTest(values []float64) float64 {
	var sum float64
	for _, value := range values {
		sum += value
	}
	return sum / float64(len(values))
}

func deterministicNormal(state *uint64) float64 {
	*state = *state*6364136223846793005 + 1442695040888963407
	u1 := float64((*state>>11)+1) / float64(uint64(1)<<53)
	*state = *state*6364136223846793005 + 1442695040888963407
	u2 := float64((*state>>11)+1) / float64(uint64(1)<<53)
	return math.Sqrt(-2*math.Log(u1)) * math.Cos(2*math.Pi*u2)
}
