package analyze

import (
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func causalMetricRun(state *causalCollectorState, kind jhlog.CausalEntityKind, id uint64, source string,
	start, end, cpu, tid uint64, parentKind jhlog.CausalEntityKind, parentID uint64) {
	startPhase, endPhase := uint64(1), uint64(2)
	if kind == jhlog.CausalThread {
		startPhase, endPhase = 6, 7
	}
	begin := &jhlog.CausalEvent{EntityKind: kind, EntityID: id, Phase: startPhase, ParentKind: parentKind, ParentID: parentID,
		ObservedElapsedNanos: start, Presence: jhlog.CausalHasTargetTID, TargetTID: tid}
	causalTestSource(begin, jhlog.CausalOriginProject, source, 12)
	state.record(begin)
	finish := *begin
	finish.Phase, finish.ObservedElapsedNanos = endPhase, end
	finish.Presence |= jhlog.CausalHasDurationNanos | jhlog.CausalHasCPUNanos
	finish.DurationNanos, finish.CPUNanos = end-start, cpu
	state.record(&finish)
}

func TestCausalMetricsDoNotDoubleCountNestedInclusiveCPU(t *testing.T) {
	header := causalTestHeader(1, 2, 3)
	var state causalCollectorState
	causalTestOpen(&state, header)
	causalMetricRun(&state, jhlog.CausalThread, 1, "app.Pool#run()V", 100, 1_000, 300, 42, 0, 0)
	causalMetricRun(&state, jhlog.CausalDirectExecution, 2, "app.Task#run()V", 200, 500, 100, 42, jhlog.CausalThread, 1)
	causalMetricRun(&state, jhlog.CausalDirectExecution, 3, "app.Other#run()V", 200, 400, 50, 43, 0, 0)
	causalTestClose(&state, header)
	a := state.finalize()
	if a.Metrics.KnownCPUNS != 350 || a.Metrics.KnownCPUIntervals != 2 || a.Metrics.AmbiguousCPUIntervals != 0 {
		t.Fatalf("nested CPU must contribute once per enclosing same-TID interval: %+v", a.Metrics)
	}
	var inclusive uint64
	for _, group := range a.CreatorGroups {
		inclusive += group.CPUInclusiveNS
	}
	if inclusive != 450 {
		t.Fatalf("per-site inclusive CPU should remain explicit and not be used as global sum: %d", inclusive)
	}
	if a.Metrics.ThreadGenerations != 1 || a.Metrics.RunStarts != 3 {
		t.Fatalf("event-only short work lost from counts: %+v", a.Metrics)
	}
}

func TestCausalMetricsDoNotGuessPartialOverlapOrCrossTIDCPU(t *testing.T) {
	header := causalTestHeader(1, 2, 3)
	var state causalCollectorState
	causalTestOpen(&state, header)
	causalMetricRun(&state, jhlog.CausalDirectExecution, 1, "app.A#run()V", 100, 600, 200, 42, 0, 0)
	causalMetricRun(&state, jhlog.CausalDirectExecution, 2, "app.B#run()V", 400, 800, 150, 42, 0, 0)
	start := &jhlog.CausalEvent{EntityKind: jhlog.CausalDirectExecution, EntityID: 3, Phase: 1,
		ObservedElapsedNanos: 900, Presence: jhlog.CausalHasTargetTID, TargetTID: 42}
	causalTestSource(start, jhlog.CausalOriginProject, "app.Migrated#run()V", 10)
	state.record(start)
	finish := *start
	finish.Phase, finish.ObservedElapsedNanos, finish.TargetTID = 2, 1_000, 43
	finish.Presence |= jhlog.CausalHasDurationNanos | jhlog.CausalHasCPUNanos
	finish.DurationNanos, finish.CPUNanos = 100, 60
	state.record(&finish)
	causalTestClose(&state, header)
	a := state.finalize()
	if a.Metrics.KnownCPUNS != 0 || a.Metrics.AmbiguousCPUIntervals != 2 || a.Metrics.UnknownCPURuns != 1 {
		t.Fatalf("partial overlap and cross-TID CPU cannot be summed or interpolated: %+v", a.Metrics)
	}
}

func TestCausalMetricsRejectUnpairedMeasuredDuration(t *testing.T) {
	header := causalTestHeader(1, 2, 3)
	var state causalCollectorState
	causalTestOpen(&state, header)
	start := &jhlog.CausalEvent{EntityKind: jhlog.CausalDirectExecution, EntityID: 3, Phase: 1,
		ObservedElapsedNanos: 100, Presence: jhlog.CausalHasTargetTID, TargetTID: 42}
	causalTestSource(start, jhlog.CausalOriginProject, "app.Task#run()V", 10)
	state.record(start)
	end := *start
	end.Phase, end.ObservedElapsedNanos = 2, 200
	end.Presence |= jhlog.CausalHasDurationNanos | jhlog.CausalHasCPUNanos
	end.DurationNanos, end.CPUNanos = 90, 50 // Measured segment starts at 110, not the retained start at 100.
	state.record(&end)
	causalTestClose(&state, header)
	a := state.finalize()
	if a.Metrics.KnownCPUNS != 0 || a.Metrics.KnownCPUIntervals != 0 || a.Metrics.UnknownCPURuns != 1 {
		t.Fatalf("unpaired run CPU was counted as known: %+v", a.Metrics)
	}
}

func TestCausalMetricsKeepMeasuredZeroAndUnknownBytesSeparate(t *testing.T) {
	header := causalTestHeader(1, 2, 3)
	var state causalCollectorState
	causalTestOpen(&state, header)
	for id := uint64(1); id <= 2; id++ {
		event := &jhlog.CausalEvent{EntityKind: jhlog.CausalOperation, EntityID: id, Phase: 1,
			Presence:      jhlog.CausalHasOperationKind | jhlog.CausalHasDurationNanos,
			OperationKind: jhlog.CausalOperationFileRead, DurationNanos: 40}
		if id == 1 {
			event.Presence |= jhlog.CausalHasReadBytes
		}
		causalTestSource(event, jhlog.CausalOriginProject, "app.File#read()V", 19)
		state.record(event)
	}
	causalTestClose(&state, header)
	a := state.finalize()
	if a.Metrics.Operations != 2 || a.Metrics.KnownReadBytes != 0 ||
		a.Metrics.KnownReadOperations != 1 || a.Metrics.UnknownReadOperations != 1 ||
		a.Metrics.KnownWriteOperations != 0 {
		t.Fatalf("known zero and unknown bytes conflated: %+v", a.Metrics)
	}
}

func TestCausalCoroutineSegmentsCanMigrateBetweenThreadsWithoutCPUInterpolation(t *testing.T) {
	header := causalTestHeader(1, 2, 3)
	var state causalCollectorState
	causalTestOpen(&state, header)
	created := &jhlog.CausalEvent{EntityKind: jhlog.CausalCoroutine, EntityID: 7, Phase: 1, ObservedElapsedNanos: 50}
	causalTestSource(created, jhlog.CausalOriginProject, "app.Screen#launch()V", 21)
	state.record(created)
	for index, tid := range []uint64{42, 43} {
		startAt := uint64(100 + index*200)
		resume := *created
		resume.Phase, resume.ObservedElapsedNanos = 2, startAt
		resume.Presence |= jhlog.CausalHasTargetTID
		resume.TargetTID = tid
		state.record(&resume)
		end := resume
		end.Phase, end.ObservedElapsedNanos = 3, startAt+100
		if index == 1 {
			end.Phase = 5
		}
		end.Presence |= jhlog.CausalHasDurationNanos | jhlog.CausalHasCPUNanos
		end.DurationNanos, end.CPUNanos = 100, 50+uint64(index)*10
		state.record(&end)
	}
	causalTestClose(&state, header)
	a := state.finalize()
	if a.Metrics.CoroutineStarts != 1 || a.Metrics.RunStarts != 2 || a.Metrics.KnownCPUNS != 110 ||
		a.Metrics.KnownCPUIntervals != 2 || a.Metrics.WithoutObservedEnd != 0 {
		t.Fatalf("migrated coroutine segments lost or merged: %+v", a.Metrics)
	}
}

func TestCausalObservedStateMachineDoesNotClaimCoroutineCreation(t *testing.T) {
	header := causalTestHeader(1, 2, 3)
	var state causalCollectorState
	causalTestOpen(&state, header)
	observed := &jhlog.CausalEvent{EntityKind: jhlog.CausalCoroutine, EntityID: 8, Phase: 6,
		ObservedElapsedNanos: 100}
	causalTestSource(observed, jhlog.CausalOriginProject, "app.Load$1#invokeSuspend(Ljava/lang/Object;)Ljava/lang/Object;", 34)
	state.record(observed)
	resume := *observed
	resume.Phase, resume.ObservedElapsedNanos = 2, 200
	state.record(&resume)
	causalTestClose(&state, header)
	a := state.finalize()
	if a.Metrics.CoroutineStarts != 0 || a.Metrics.CoroutineObservedMachines != 1 || a.Metrics.RunStarts != 1 {
		t.Fatalf("first observed state machine falsely counted as creation: %+v", a.Metrics)
	}
	if len(a.CreatorGroups) != 1 || a.CreatorGroups[0].Role != "код наблюдаемого действия" ||
		len(a.AllEntities) != 1 || a.AllEntities[0].LifecycleNote != "впервые наблюдалось выполнение кода корутины; место и время создания неизвестны" {
		t.Fatalf("invokeSuspend was shown as a creator: groups=%+v entities=%+v", a.CreatorGroups, a.AllEntities)
	}
}

func TestCausalObservedStateMachineDoesNotDoubleCountVerifiedCreation(t *testing.T) {
	header := causalTestHeader(1, 2, 3)
	var state causalCollectorState
	causalTestOpen(&state, header)
	created := &jhlog.CausalEvent{EntityKind: jhlog.CausalCoroutine, EntityID: 9, Phase: 1,
		ObservedElapsedNanos: 100}
	causalTestSource(created, jhlog.CausalOriginProject, "app.Screen#launch()V", 34)
	state.record(created)
	observed := *created
	observed.Phase, observed.ObservedElapsedNanos = 6, 200
	state.record(&observed)
	causalTestClose(&state, header)
	a := state.finalize()
	if a.Metrics.CoroutineStarts != 1 || a.Metrics.CoroutineObservedMachines != 0 ||
		len(a.AllEntities) != 1 || a.AllEntities[0].LifecycleNote == "впервые наблюдалось выполнение кода корутины; место и время создания неизвестны" {
		t.Fatalf("verified creation and later observation counted as two coroutines: metrics=%+v entities=%+v", a.Metrics, a.AllEntities)
	}
}
