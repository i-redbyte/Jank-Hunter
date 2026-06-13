package mathanalysis

import "testing"

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

func transitionCount(transitions []MarkovTransition, from, to string) int {
	for _, transition := range transitions {
		if transition.From == from && transition.To == to {
			return transition.Count
		}
	}
	return 0
}
