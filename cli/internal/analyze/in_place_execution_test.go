package analyze

import (
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func TestLongInPlaceExecutionRemainsDiagnosableWithoutWrapper(t *testing.T) {
	for _, duration := range []uint64{999_999_999, 1_000_000_000, 2_000_000_000} {
		var state causalCollectorState
		header := causalTestHeader(1, 2, 3)
		causalTestOpen(&state, header)
		causalMetricRun(&state, jhlog.CausalDirectExecution, 7, "app.Task#run()V", 100, 100+duration, 20_000_000, 42, 0, 0)
		causalTestClose(&state, header)
		analysis := state.finalize()
		found := false
		for _, finding := range analysis.Observations {
			if finding.Rule == "long_task" {
				found = true
				if finding.Source != "app.Task#run()V" || !strings.Contains(finding.Consequence, "CPU") {
					t.Fatalf("missing execution attribution or CPU evidence: %+v", finding)
				}
			}
		}
		if found != (duration >= longTaskReviewLimitNS) {
			t.Fatalf("duration %d: expected long direct execution signal, got %+v", duration, analysis.Observations)
		}
	}
}

func TestLongCoroutineSegmentIsNotItsSuspendedLifetime(t *testing.T) {
	var state causalCollectorState
	header := causalTestHeader(1, 2, 3)
	causalTestOpen(&state, header)
	start := &jhlog.CausalEvent{EntityKind: jhlog.CausalCoroutine, EntityID: 7, Phase: 2, ObservedElapsedNanos: 100}
	causalTestSource(start, jhlog.CausalOriginProject, "app.Worker#invokeSuspend", 12)
	state.record(start)
	end := *start
	end.Phase, end.ObservedElapsedNanos, end.DurationNanos = 3, 1_000_000_100, 1_000_000_000
	end.Presence |= jhlog.CausalHasDurationNanos
	state.record(&end)
	resume := *start
	resume.ObservedElapsedNanos = 100_000_000_000
	state.record(&resume)
	finish := resume
	finish.Phase, finish.ObservedElapsedNanos, finish.DurationNanos = 5, 100_000_000_010, 10
	finish.Presence |= jhlog.CausalHasDurationNanos
	state.record(&finish)
	causalTestClose(&state, header)
	analysis := state.finalize()
	count := 0
	for _, finding := range analysis.Observations {
		if finding.Rule == "long_task" {
			count++
			if finding.MaxDurationMS != 1000 {
				t.Fatalf("suspension attributed to running segment: %+v", finding)
			}
		}
	}
	if count != 1 {
		t.Fatalf("expected one slow segment, got %+v", analysis.Observations)
	}
}

func TestConflictingCausalParentsAreNotAnExactExecutionLink(t *testing.T) {
	var state causalCollectorState
	header := causalTestHeader(1, 2, 3)
	causalTestOpen(&state, header)
	begin := &jhlog.CausalEvent{EntityKind: jhlog.CausalDirectExecution, EntityID: 7, Phase: 1,
		ParentKind: jhlog.CausalWorkerAttempt, ParentID: 1, ObservedElapsedNanos: 100}
	causalTestSource(begin, jhlog.CausalOriginProject, "app.Task#run()V", 12)
	state.record(begin)
	end := *begin
	end.ParentID, end.Phase, end.ObservedElapsedNanos = 2, 2, 200
	state.record(&end)
	causalTestClose(&state, header)
	analysis := state.finalize()
	for _, view := range analysis.AllEntities {
		if view.ID == 7 && (view.ParentID != 0 || !view.ParentAmbiguous) {
			t.Fatalf("conflicting parents retained as exact: %+v", view)
		}
	}
}

func TestLongCoroutineAfterDetailBudgetIsNotLost(t *testing.T) {
	var state causalCollectorState
	header := causalTestHeader(1, 2, 3)
	causalTestOpen(&state, header)
	for i := uint64(0); i < 21; i++ {
		start := &jhlog.CausalEvent{EntityKind: jhlog.CausalCoroutine, EntityID: 7, Phase: 2,
			ObservedElapsedNanos: i * 10_000_000_000}
		causalTestSource(start, jhlog.CausalOriginProject, "app.Worker#invokeSuspend", 12)
		state.record(start)
		end := *start
		end.Phase, end.DurationNanos = 3, 10
		if i == 20 {
			end.DurationNanos = 2_000_000_000
		}
		end.ObservedElapsedNanos += end.DurationNanos
		end.Presence |= jhlog.CausalHasDurationNanos
		state.record(&end)
	}
	causalTestClose(&state, header)
	analysis := state.finalize()
	for _, finding := range analysis.Observations {
		if finding.Rule == "long_task" && finding.MaxDurationMS == 2000 {
			return
		}
	}
	t.Fatalf("late slow segment lost after details limit: %+v", analysis.Observations)
}

func TestOverlappingExecutionStartsDoNotClaimExactCPU(t *testing.T) {
	var state causalCollectorState
	header := causalTestHeader(1, 2, 3)
	causalTestOpen(&state, header)
	start := &jhlog.CausalEvent{EntityKind: jhlog.CausalDirectExecution, EntityID: 7, Phase: 1,
		ObservedElapsedNanos: 100, TargetTID: 42, Presence: jhlog.CausalHasTargetTID}
	causalTestSource(start, jhlog.CausalOriginProject, "app.Task#run()V", 12)
	state.record(start)
	second := *start
	second.ObservedElapsedNanos = 110
	state.record(&second)
	end := second
	end.Phase, end.ObservedElapsedNanos, end.DurationNanos, end.CPUNanos = 2, 200, 90, 30
	end.Presence |= jhlog.CausalHasDurationNanos | jhlog.CausalHasCPUNanos
	state.record(&end)
	causalTestClose(&state, header)
	m := state.finalize().Metrics
	if m.KnownCPUIntervals != 0 || m.KnownCPUNS != 0 || m.UnknownCPURuns == 0 {
		t.Fatalf("overlapping starts were treated as an exact CPU interval: %+v", m)
	}
}
