package mathanalysis

import (
	"strings"
	"testing"
)

func TestBuildMarkovModelClassifiesKnownSequence(t *testing.T) {
	timeline := []TimelineBucket{
		{StartMS: 0, EndMS: 1000},
		{StartMS: 1000, EndMS: 2000, UIFrames: 100, UIJankyFrames: 10},
		{StartMS: 2000, EndMS: 3000, UIFrames: 100, UIJankyFrames: 20},
		{StartMS: 3000, EndMS: 4000, UIFrames: 100},
		{StartMS: 4000, EndMS: 5000},
		{StartMS: 5000, EndMS: 6000, HTTPCount: 1, HTTPP95DurationMS: 650},
	}

	model := buildMarkovModel(timeline, nil)
	wantStates := []string{markovHealthy, markovJanky, markovJanky, markovRecovering, markovHealthy, markovNetworkSlow}
	if len(model.States) != len(wantStates) {
		t.Fatalf("len(states) = %d, want %d", len(model.States), len(wantStates))
	}
	for index, want := range wantStates {
		if got := model.States[index].State; got != want {
			t.Fatalf("state[%d] = %q, want %q", index, got, want)
		}
	}
	if model.HealthyToBadCount != 2 {
		t.Fatalf("HealthyToBadCount = %d, want 2", model.HealthyToBadCount)
	}
	assertFloat(t, model.BadToHealthyProbability, 0.5)
	assertFloat(t, model.ExpectedRecoveryWindows, 2)
	assertFloat(t, model.ExpectedRecoveryMS, 2000)
	assertFloat(t, model.BadStateExposure, 0.5)
	if model.Confidence != "medium" {
		t.Fatalf("Confidence = %q, want medium", model.Confidence)
	}
	if transitionCount(model.Transitions, markovJanky, markovJanky) != 1 {
		t.Fatalf("expected Janky -> Janky transition: %+v", model.Transitions)
	}
}

func TestBuildMarkovModelMarksNetworkLoopWindow(t *testing.T) {
	timeline := []TimelineBucket{
		{StartMS: 0, EndMS: 1000, HTTPCount: 1},
		{StartMS: 1000, EndMS: 2000, HTTPCount: 1},
	}
	loops := []NetworkLoopFinding{{FirstMS: 0, LastMS: 1000, Confidence: 0.8}}

	model := buildMarkovModel(timeline, loops)
	for index, state := range model.States {
		if state.State != markovNetworkLoop {
			t.Fatalf("state[%d] = %q, want NetworkLoop", index, state.State)
		}
	}
}

func TestBuildMarkovModelDisablesForecastForIndependentRuns(t *testing.T) {
	model := buildMarkovModelForRuns(markovForecastTimeline(30, func(int) bool { return false }), nil, 3)

	if model.IndependentRunCount != 3 || model.SequenceComparable || len(model.Transitions) != 0 || model.Forecast.Direction != markovForecastInsufficient || model.Forecast.HorizonWindows != 0 {
		t.Fatalf("multi-run forecast must be disabled: %+v", model)
	}
	if model.Confidence != "low" || !strings.Contains(model.ConfidenceReason, "прогоны: 3") {
		t.Fatalf("multi-run confidence reason is missing: %+v", model)
	}
}

func TestBuildMarkovModelDoesNotTreatMissingBucketAsHealthyRecovery(t *testing.T) {
	model := buildMarkovModel([]TimelineBucket{
		{StartMS: 0, EndMS: 1_000, HasObservation: true, UIFrames: 100, UIJankyFrames: 20},
		{StartMS: 1_000, EndMS: 2_000},
		{StartMS: 2_000, EndMS: 3_000, HasObservation: true, UIFrames: 100},
	}, nil)

	if len(model.States) != 2 || model.States[1].State != markovHealthy {
		t.Fatalf("missing bucket must be omitted and break recovery: %+v", model.States)
	}
	if model.TransitionEventCount != 0 || model.HasRecoveryProbability || model.HasExpectedRecovery {
		t.Fatalf("gap must not create a recovery transition: %+v", model)
	}
	if model.MissingBucketCount != 1 || model.Forecast.Direction != markovForecastInsufficient {
		t.Fatalf("coverage limitation is not explicit: %+v", model)
	}
}

func TestCompareMarkovModelsRejectsAggregatedSequences(t *testing.T) {
	baseline := buildMarkovModelForRuns(markovForecastTimeline(30, func(int) bool { return false }), nil, 2)
	candidate := buildMarkovModel(markovForecastTimeline(30, func(int) bool { return false }), nil)

	deltas := compareMarkovModels(baseline, candidate)
	if len(deltas) != 1 || deltas[0].Comparable || !strings.Contains(deltas[0].Summary, "не рассчитаны") {
		t.Fatalf("aggregated Markov sequences must be incomparable: %+v", deltas)
	}
}

func TestCompareMarkovFindingsKeepsUnavailableMetricAndRegression(t *testing.T) {
	deltas := []MarkovDelta{
		{Metric: "Нет данных", Comparable: false, Severity: "medium", Summary: "метрика недоступна"},
		{Metric: "Регрессия", Comparable: true, Severity: "high", Summary: "плохая экспозиция выросла"},
	}

	findings := compareMarkovFindings(deltas)
	if len(findings) != 2 || findings[0].Severity != "medium" || findings[1].Severity != "high" {
		t.Fatalf("unavailable metric and regression must both remain visible: %+v", findings)
	}
}

func TestCompareMarkovModelsReportsRegression(t *testing.T) {
	baseline := buildMarkovModel([]TimelineBucket{
		{StartMS: 0, EndMS: 1000},
		{StartMS: 1000, EndMS: 2000},
		{StartMS: 2000, EndMS: 3000},
	}, nil)
	candidate := buildMarkovModel([]TimelineBucket{
		{StartMS: 0, EndMS: 1000},
		{StartMS: 1000, EndMS: 2000, UIFrames: 100, UIJankyFrames: 10},
		{StartMS: 2000, EndMS: 3000},
	}, nil)

	deltas := compareMarkovModels(baseline, candidate)
	for _, delta := range deltas {
		if delta.Metric == "Здоровые → плохие состояния" && delta.Severity == "medium" {
			return
		}
	}
	t.Fatalf("Healthy -> bad regression was not reported: %+v", deltas)
}

func TestBuildMarkovModelKeepsContributingSymptoms(t *testing.T) {
	model := buildMarkovModel([]TimelineBucket{
		{StartMS: 0, EndMS: 1000, UIFrames: 100, UIJankyFrames: 10, HTTPCount: 1, HTTPP95DurationMS: 800},
		{StartMS: 1000, EndMS: 2000},
	}, nil)

	if got := model.States[0].State; got != markovJanky {
		t.Fatalf("state = %q, want dominant Janky", got)
	}
	if !stateHasContributor(model.States[0], markovJanky) || !stateHasContributor(model.States[0], markovNetworkSlow) {
		t.Fatalf("expected jank and network contributors: %+v", model.States[0].Contributors)
	}
}

func TestBuildMarkovModelReportsContextStickiness(t *testing.T) {
	model := buildMarkovModel([]TimelineBucket{
		{StartMS: 0, EndMS: 1000, HTTPCount: 1, HTTPP95DurationMS: 700, RouteSample: "GET /feed", OwnerSample: "FeedRepository"},
		{StartMS: 1000, EndMS: 2000, HTTPCount: 1, HTTPP95DurationMS: 800, RouteSample: "GET /feed", OwnerSample: "FeedRepository"},
		{StartMS: 2000, EndMS: 3000, HTTPCount: 1, HTTPP95DurationMS: 900, RouteSample: "GET /feed", OwnerSample: "FeedRepository"},
		{StartMS: 3000, EndMS: 4000},
	}, nil)

	if len(model.ContextStickyStates) == 0 {
		t.Fatalf("expected context sticky states")
	}
	sticky := model.ContextStickyStates[0]
	if sticky.State != markovNetworkSlow || sticky.Count != 2 {
		t.Fatalf("unexpected context sticky state: %+v", sticky)
	}
	if !strings.Contains(sticky.Context, "FeedRepository") || !strings.Contains(sticky.Context, "GET /feed") {
		t.Fatalf("context should include owner and route: %+v", sticky)
	}
}

func TestCompareMarkovModelsReportsMatrixDivergence(t *testing.T) {
	baseline := buildMarkovModel([]TimelineBucket{
		{StartMS: 0, EndMS: 1000},
		{StartMS: 1000, EndMS: 2000},
		{StartMS: 2000, EndMS: 3000},
		{StartMS: 3000, EndMS: 4000},
	}, nil)
	candidate := buildMarkovModel([]TimelineBucket{
		{StartMS: 0, EndMS: 1000},
		{StartMS: 1000, EndMS: 2000, UIFrames: 100, UIJankyFrames: 10},
		{StartMS: 2000, EndMS: 3000, UIFrames: 100, UIJankyFrames: 12},
		{StartMS: 3000, EndMS: 4000},
	}, nil)

	for _, delta := range compareMarkovModels(baseline, candidate) {
		if delta.Metric == "Расхождение матрицы переходов" {
			if delta.CandidateValue <= 0 || delta.Severity == "ok" {
				t.Fatalf("matrix divergence should be significant: %+v", delta)
			}
			return
		}
	}
	t.Fatalf("matrix divergence delta was not reported")
}

func TestBuildMarkovModelForecastsStableHealthyRun(t *testing.T) {
	model := buildMarkovModel(markovForecastTimeline(30, func(int) bool { return false }), nil)

	if model.Forecast.Direction != markovForecastStable {
		t.Fatalf("Direction = %q, want stable", model.Forecast.Direction)
	}
	if !strings.Contains(model.Forecast.Label, "состояние в норме") {
		t.Fatalf("Label = %q, want normal state", model.Forecast.Label)
	}
	if model.Forecast.Confidence != "high" {
		t.Fatalf("Confidence = %q, want high", model.Forecast.Confidence)
	}
	if model.Forecast.SegmentWindows != 10 {
		t.Fatalf("SegmentWindows = %d, want 10", model.Forecast.SegmentWindows)
	}
	assertFloat(t, model.Forecast.ProjectedBadProbability, 0)
}

func TestBuildMarkovModelForecastsDegradation(t *testing.T) {
	model := buildMarkovModel(markovForecastTimeline(30, func(index int) bool { return index >= 10 }), nil)

	if model.Forecast.Direction != markovForecastDegrading {
		t.Fatalf("Direction = %q, want degrading: %+v", model.Forecast.Direction, model.Forecast)
	}
	if model.Forecast.Severity != "high" {
		t.Fatalf("Severity = %q, want high", model.Forecast.Severity)
	}
	if model.Forecast.RecentBadExposure <= model.Forecast.EarlyBadExposure {
		t.Fatalf("recent exposure must exceed early exposure: %+v", model.Forecast)
	}
}

func TestBuildMarkovModelForecastsImprovement(t *testing.T) {
	model := buildMarkovModel(markovForecastTimeline(30, func(index int) bool { return index < 10 }), nil)

	if model.Forecast.Direction != markovForecastImproving {
		t.Fatalf("Direction = %q, want improving: %+v", model.Forecast.Direction, model.Forecast)
	}
	if model.Forecast.RecentBadExposure >= model.Forecast.EarlyBadExposure {
		t.Fatalf("recent exposure must be below early exposure: %+v", model.Forecast)
	}
}

func TestBuildMarkovModelForecastReportsInsufficientHistory(t *testing.T) {
	model := buildMarkovModel(markovForecastTimeline(11, func(int) bool { return false }), nil)

	if model.Forecast.Direction != markovForecastInsufficient {
		t.Fatalf("Direction = %q, want insufficient", model.Forecast.Direction)
	}
	if model.Forecast.HorizonWindows != 0 {
		t.Fatalf("HorizonWindows = %d, want 0", model.Forecast.HorizonWindows)
	}
}

func TestMarkovForecastDirectionReportsConflictingSignals(t *testing.T) {
	if direction := markovForecastDirection(0.20, -0.20); direction != markovForecastUncertain {
		t.Fatalf("Direction = %q, want uncertain", direction)
	}
}

func TestBuildMarkovModelForecastKeepsStableProblemsVisible(t *testing.T) {
	model := buildMarkovModel(markovForecastTimeline(36, func(index int) bool { return index%4 == 1 }), nil)

	if model.Forecast.Direction != markovForecastStable {
		t.Fatalf("Direction = %q, want stable: %+v", model.Forecast.Direction, model.Forecast)
	}
	if !strings.Contains(model.Forecast.Label, "не подтверждены") {
		t.Fatalf("Label = %q, want no confirmed trajectory", model.Forecast.Label)
	}
}

func TestBuildMarkovModelForecastWeightsExposureByDuration(t *testing.T) {
	var startMS uint64
	timeline := make([]TimelineBucket, 0, 12)
	for index := range 12 {
		durationMS := uint64(1000)
		if index == 0 {
			durationMS = 4000
		}
		bucket := TimelineBucket{StartMS: startMS, EndMS: startMS + durationMS}
		if index == 0 {
			bucket.UIFrames = 100
			bucket.UIJankyFrames = 20
		}
		timeline = append(timeline, bucket)
		startMS += durationMS
	}

	model := buildMarkovModel(timeline, nil)
	assertFloat(t, model.Forecast.EarlyBadExposure, 4.0/7.0)
	if model.Forecast.Direction != markovForecastImproving {
		t.Fatalf("Direction = %q, want improving: %+v", model.Forecast.Direction, model.Forecast)
	}
}

func markovForecastTimeline(count int, bad func(int) bool) []TimelineBucket {
	timeline := make([]TimelineBucket, 0, count)
	for index := range count {
		bucket := TimelineBucket{
			StartMS: uint64(index * 1000),
			EndMS:   uint64((index + 1) * 1000),
		}
		if bad(index) {
			bucket.UIFrames = 100
			bucket.UIJankyFrames = 20
		}
		timeline = append(timeline, bucket)
	}
	return timeline
}

func transitionCount(transitions []MarkovTransition, from, to string) int {
	for _, transition := range transitions {
		if transition.From == from && transition.To == to {
			return transition.Count
		}
	}
	return 0
}

func stateHasContributor(state MarkovBucketState, contributor string) bool {
	for _, item := range state.Contributors {
		if item.State == contributor {
			return true
		}
	}
	return false
}
