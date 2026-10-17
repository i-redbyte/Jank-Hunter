package analyze

import (
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func TestWorkerExecutionSegmentsFollowOnlyExactCausalAncestry(t *testing.T) {
	scope := strings.Repeat("1", 32) + "/" + strings.Repeat("2", 32) + "/" + strings.Repeat("3", 32)
	worker := CausalEntityView{Kind: uint64(jhlog.CausalWorkerAttempt), Scope: scope, ID: 7, Anchor: "worker-7",
		Events: []CausalEventView{{KnownFields: jhlog.CausalHasWorkerBinding, WorkerInstanceID: 8}}}
	segment := CausalEntityView{Kind: uint64(jhlog.CausalDirectExecution), Scope: scope, ID: 9,
		ParentKind: worker.Kind, ParentID: 7, Source: "app.Task#run()V", Events: []CausalEventView{
			{Phase: 1, ObservedNS: 100, TargetTID: 42, KnownFields: jhlog.CausalHasTargetTID},
			{Phase: 2, ObservedNS: 200, DurationNS: 100, CPUNS: 20, TargetTID: 42,
				KnownFields: jhlog.CausalHasDurationNanos | jhlog.CausalHasCPUNanos | jhlog.CausalHasTargetTID},
		}}
	foreign := segment
	foreign.Scope, foreign.ID = strings.Repeat("4", 96), 10
	workers := &WorkerAnalysis{Attempts: []WorkerAttempt{{Scope: strings.ReplaceAll(scope, "/", ""), InstanceID: "8"}}}
	causal := &CausalAnalysis{AllEntities: []CausalEntityView{worker, segment, foreign}}
	attachCausalWorkerAttempts(causal, workers)
	attempt := workers.Attempts[0]
	if len(attempt.ExecutionSegments) != 1 || attempt.ExecutionSegments[0].TargetTID != "42" ||
		attempt.ExecutionSegments[0].CPUNS == nil || *attempt.ExecutionSegments[0].CPUNS != 20 {
		t.Fatalf("exact segment missing or foreign thread attributed: %+v", attempt)
	}
	attachCausalWorkerAttempts(causal, workers)
	if len(workers.Attempts[0].ExecutionSegments) != 1 {
		t.Fatal("repeat enrichment duplicated segments")
	}
	workers.Attempts[0].Ambiguous = true
	attachCausalWorkerAttempts(causal, workers)
	if len(workers.Attempts[0].ExecutionSegments) != 0 {
		t.Fatal("ambiguous worker retained exact execution attribution")
	}
}

func TestWorkerSegmentsRejectOverlappingStartsAndForeignThreadCPU(t *testing.T) {
	view := CausalEntityView{Kind: uint64(jhlog.CausalCoroutine), Events: []CausalEventView{
		{Phase: 2, ObservedNS: 100, TargetTID: 42, KnownFields: jhlog.CausalHasTargetTID},
		{Phase: 2, ObservedNS: 110, TargetTID: 42, KnownFields: jhlog.CausalHasTargetTID},
		{Phase: 3, ObservedNS: 200, DurationNS: 90, CPUNS: 30, TargetTID: 42,
			KnownFields: jhlog.CausalHasDurationNanos | jhlog.CausalHasCPUNanos | jhlog.CausalHasTargetTID},
	}}
	if got := workerSegments(view); len(got) != 0 {
		t.Fatalf("overlapping starts guessed as exact segment: %+v", got)
	}
	view.Events = append(view.Events[:1], view.Events[2])
	view.Events[1].DurationNS, view.Events[1].TargetTID = 100, 43
	got := workerSegments(view)
	if len(got) != 1 || got[0].CPUNS != nil {
		t.Fatalf("CPU from another thread retained: %+v", got)
	}
}

func TestWorkerExecutionSegmentsDoNotInventCPUOrSuspendedIntervals(t *testing.T) {
	view := CausalEntityView{Kind: uint64(jhlog.CausalCoroutine), Source: "app.Worker#invokeSuspend", Events: []CausalEventView{
		{Phase: 2, ObservedNS: 100},
		{Phase: 3, ObservedNS: 200, DurationNS: 100, KnownFields: jhlog.CausalHasDurationNanos},
		{Phase: 2, ObservedNS: 10_000},
		{Phase: 5, ObservedNS: 10_010, DurationNS: 10, KnownFields: jhlog.CausalHasDurationNanos},
	}}
	segments := workerSegments(view)
	if len(segments) != 2 || segments[0].DurationNS != 100 || segments[1].DurationNS != 10 || segments[0].CPUNS != nil {
		t.Fatalf("suspension or unknown CPU fabricated: %+v", segments)
	}
}
