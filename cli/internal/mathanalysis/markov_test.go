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
		if delta.Metric == "Здоровые -> плохие состояния" && delta.Severity == "medium" {
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
