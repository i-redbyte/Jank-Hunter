package mathanalysis

import (
	"math"
	"testing"
)

func TestMarkovComparisonDoesNotTurnMissingStatesIntoHealthyBaseline(t *testing.T) {
	known := buildMarkovModel([]TimelineBucket{{StartMS: 0, EndMS: 1000}, {StartMS: 1000, EndMS: 2000}}, nil)
	for _, models := range [][2]MarkovModel{{{}, known}, {known, {}}, {{}, {}}} {
		for _, delta := range compareMarkovModels(models[0], models[1]) {
			if delta.Comparable {
				t.Fatalf("missing states treated as measured zero: %+v", delta)
			}
		}
	}
}

func TestMarkovStickyComparisonUsesFullMatrixWhenDisplayTopThreeChanges(t *testing.T) {
	build := func(longBlock int) MarkovModel {
		var timeline []TimelineBucket
		for block := 0; block < 4; block++ {
			count := 3
			if block == longBlock {
				count = 10
			}
			for i := 0; i < count; i++ {
				at := uint64(len(timeline)) * 1000
				b := TimelineBucket{StartMS: at, EndMS: at + 1000}
				switch block {
				case 1:
					b.UIFrames, b.UIJankyFrames = 100, 10
				case 2:
					b.HTTPCount, b.HTTPP95DurationMS = 1, 1000
				case 3:
					b.StallCount, b.StallMaxMS = 1, 1000
				}
				timeline = append(timeline, b)
			}
		}
		return buildMarkovModel(timeline, nil)
	}
	baseline := build(-1)
	if len(baseline.StickyStates) != 3 {
		t.Fatal("fixture must truncate sticky display")
	}
	for block := 0; block < 3; block++ {
		candidate := build(block)
		for _, delta := range compareMarkovModels(baseline, candidate) {
			for _, tr := range baseline.Transitions {
				if tr.From == tr.To && delta.Metric == "Долгое состояние: "+MarkovStateLabel(tr.From) &&
					math.Abs(delta.BaselineValue-tr.Probability*100) > 0.001 {
					t.Fatalf("display omission replaced measured probability: %+v, transition=%+v", delta, tr)
				}
			}
		}
	}
}

func TestMarkovComparisonDoesNotInferChangedMatrixWithoutTransitions(t *testing.T) {
	one := buildMarkovModel([]TimelineBucket{{StartMS: 0, EndMS: 1000}}, nil)
	two := buildMarkovModel([]TimelineBucket{{StartMS: 0, EndMS: 1000}, {StartMS: 1000, EndMS: 2000}}, nil)
	found := false
	for _, delta := range compareMarkovModels(one, two) {
		if delta.Metric == "Расхождение матрицы переходов" || delta.Metric == "Здоровые → плохие состояния" {
			found = true
			if delta.Comparable || delta.BaselineAvailable || !delta.CandidateAvailable {
				t.Fatalf("unobserved transition distribution reported changed: %+v", delta)
			}
		}
	}
	if !found {
		t.Fatal("missing-transition limitation was hidden")
	}
}

func TestMarkovStickyProbabilityDistinguishesObservedZeroFromMissingTransitions(t *testing.T) {
	transitions := []MarkovTransition{{From: "healthy", To: "janky", Count: 1, Probability: 1}}
	if value, known := stickyProbability(transitions, "healthy"); value != 0 || !known {
		t.Fatalf("observed immediate departure must be known zero: %v %v", value, known)
	}
	if _, known := stickyProbability(transitions, "janky"); known {
		t.Fatal("terminal state with no observed departure has an unknown self-transition probability")
	}
}
