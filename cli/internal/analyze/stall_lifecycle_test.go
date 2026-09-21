package analyze

import (
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func TestStallIDsAreOpaqueAndNeedNotArriveInNumericOrder(t *testing.T) {
	c := newCollector("opaque IDs", 1, Options{})
	c.startLog(jhlog.SegmentHeader{})
	for _, id := range []uint64{7, 3} {
		c.add(nil, jhlog.Event{Type: jhlog.EventStall, TimeMS: 200,
			Stall: &jhlog.StallEvent{IncidentID: id, State: jhlog.StallStateRecovered, DurationMS: 200},
		})
	}
	c.finishLog()
	if c.stallLifecycleError != nil || c.summary.StallCount != 2 {
		t.Fatalf("distinct IDs were rejected or conflated: count=%d error=%v", c.summary.StallCount, c.stallLifecycleError)
	}
}

func TestStallLifecycleUpdatesCountAsOneIncident(t *testing.T) {
	c := newCollector("stall lifecycle", 1, Options{})
	c.startLog(jhlog.SegmentHeader{})
	for _, state := range []jhlog.StallState{jhlog.StallStateOngoing, jhlog.StallStateRecovered} {
		c.add(nil, jhlog.Event{Type: jhlog.EventStall, TimeMS: uint64(state) * 200,
			Stall: &jhlog.StallEvent{IncidentID: 1, State: state, DurationMS: uint64(state) * 200},
		})
	}
	c.finishLog()
	summary := c.finish()
	if summary.StallCount != 1 || summary.StallMaxMS != 400 {
		t.Fatalf("one incident counted as %d stalls, max=%d", summary.StallCount, summary.StallMaxMS)
	}
}

func TestUnrecoveredStallRetainsExplicitStateInSummary(t *testing.T) {
	c := newCollector("ongoing stall", 1, Options{})
	c.startLog(jhlog.SegmentHeader{})
	c.add(nil, jhlog.Event{Type: jhlog.EventStall, TimeMS: 200, Stall: &jhlog.StallEvent{IncidentID: 1, State: jhlog.StallStateOngoing, DurationMS: 200}})
	c.finishLog()
	summary := c.finish()
	if summary.StallCount != 1 || summary.StallStates.Ongoing != 1 || summary.StallStates.Recovered != 0 {
		t.Fatalf("ongoing state lost or presented as recovered: count=%d states=%+v", summary.StallCount, summary.StallStates)
	}
}

func TestUnfinishedStallWarnsThatDurationIsOnlyObserved(t *testing.T) {
	for _, state := range []jhlog.StallState{jhlog.StallStateOngoing, jhlog.StallStateInterrupted} {
		c := newCollector("incomplete duration", 1, Options{})
		c.startLog(jhlog.SegmentHeader{})
		c.add(nil, jhlog.Event{Type: jhlog.EventStall, TimeMS: 200, Stall: &jhlog.StallEvent{IncidentID: 1, State: state, DurationMS: 200}})
		c.finishLog()
		summary := c.finish()
		if !warningsContain(summary.Warnings, "нижняя граница") {
			t.Errorf("incomplete stall %d looked like an exact duration: %v", state, summary.Warnings)
		}
	}
}

func TestUnfinishedStallCannotPassAnExactRegressionGate(t *testing.T) {
	baseline := Summary{StallCount: 1, StallMaxMS: 200, StallStates: StallStateCounts{Recovered: 1}}
	for _, states := range []StallStateCounts{{Ongoing: 1}, {Interrupted: 1}, {Unknown: 1}} {
		candidate := Summary{StallCount: 1, StallMaxMS: 100, StallStates: states}
		comparison := Compare(baseline, candidate)
		for _, metric := range comparison.Deltas {
			if metric.Name == "Main-thread stall max" && metric.Comparable {
				t.Errorf("observed lower bound/unknown %v presented as exact improvement", states)
			}
		}
		gate := EvaluateGate(comparison, ThresholdConfig{Metrics: map[string]MetricThreshold{
			"Main-thread stall max": {MaxRegressionAbs: floatPointer(100)},
		}})
		if !gate.Failed {
			t.Errorf("incomplete stall %v passed exact gate", states)
		}
	}
}
