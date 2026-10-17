package analyze

import (
	"sort"
	"strconv"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

// Segments are inclusive observations, never a sum of worker CPU or its full lifetime.
type WorkerExecutionSegment struct {
	Source, TargetTID, Kind string
	StartNS                 uint64  `json:",string"`
	EndNS                   uint64  `json:",string"`
	DurationNS              uint64  `json:",string"`
	CPUNS                   *uint64 `json:",string"`
}

type workerCausalKey struct {
	scope    string
	kind, id uint64
}

func attachWorkerExecutionSegments(causal *CausalAnalysis, workers *WorkerAnalysis) {
	views := make(map[workerCausalKey]*CausalEntityView, len(causal.AllEntities))
	bound := make(map[workerCausalKey]*WorkerAttempt)
	attempts := make(map[causalWorkerJoinKey]*WorkerAttempt)
	for i := range workers.Attempts {
		a := &workers.Attempts[i]
		a.ExecutionSegments = nil
		a.ExecutionSegmentsOmitted, a.ExecutionEventsOmitted = 0, 0
		if a.Ambiguous || a.IdentityAmbiguous {
			continue
		}
		id, err := strconv.ParseUint(a.InstanceID, 10, 64)
		if err != nil || id == 0 {
			continue
		}
		key := causalWorkerJoinKey{a.Scope, id, a.RunAttempt, a.Generation}
		if _, exists := attempts[key]; exists {
			attempts[key] = nil
		} else {
			attempts[key] = a
		}
	}
	for i := range causal.AllEntities {
		v := &causal.AllEntities[i]
		key := workerCausalKey{v.Scope, v.Kind, v.ID}
		views[key] = v
		if !v.WorkerAttemptLinked {
			continue
		}
		for _, event := range v.Events {
			if event.KnownFields&jhlog.CausalHasWorkerBinding == 0 {
				continue
			}
			binding := causalWorkerJoinKey{strings.ReplaceAll(v.Scope, "/", ""), event.WorkerInstanceID,
				uint32(event.RunAttempt), uint32(event.Generation)}
			if a := attempts[binding]; a != nil {
				bound[key] = a
			}
		}
	}
	for _, v := range causal.AllEntities {
		if v.ParentAmbiguous || v.IsJankHunter {
			continue
		}
		parent := workerCausalKey{v.Scope, v.ParentKind, v.ParentID}
		var attempt *WorkerAttempt
		for depth := 0; depth < 16 && parent.id != 0; depth++ {
			if attempt = bound[parent]; attempt != nil {
				break
			}
			next := views[parent]
			if next == nil || next.ParentAmbiguous {
				break
			}
			parent = workerCausalKey{next.Scope, next.ParentKind, next.ParentID}
		}
		if attempt == nil {
			continue
		}
		attempt.ExecutionEventsOmitted = causalSaturatingAdd(attempt.ExecutionEventsOmitted, v.OmittedEvents)
		for _, segment := range workerSegments(v) {
			attempt.ExecutionSegments = append(attempt.ExecutionSegments, segment)
			sort.SliceStable(attempt.ExecutionSegments, func(i, j int) bool {
				a, b := attempt.ExecutionSegments[i], attempt.ExecutionSegments[j]
				if a.DurationNS != b.DurationNS {
					return a.DurationNS > b.DurationNS
				}
				if a.StartNS != b.StartNS {
					return a.StartNS < b.StartNS
				}
				return a.Source < b.Source
			})
			if len(attempt.ExecutionSegments) > 16 {
				attempt.ExecutionSegments = attempt.ExecutionSegments[:16]
				attempt.ExecutionSegmentsOmitted++
			}
		}
	}
}

func workerSegments(v CausalEntityView) []WorkerExecutionSegment {
	kind := jhlog.CausalEntityKind(v.Kind)
	if kind != jhlog.CausalDirectExecution && kind != jhlog.CausalCoroutine && kind != jhlog.CausalExecutorTask {
		return nil
	}
	var result []WorkerExecutionSegment
	var start *CausalEventView
	ambiguous := false
	for _, event := range v.Events {
		if causalRunStart(kind, event.Phase) {
			ambiguous = ambiguous || start != nil
			copy := event
			start = &copy
			continue
		}
		if !causalRunEnd(kind, event.Phase) {
			continue
		}
		if !ambiguous && start != nil && event.KnownFields&jhlog.CausalHasDurationNanos != 0 &&
			event.ObservedNS >= start.ObservedNS && event.DurationNS == event.ObservedNS-start.ObservedNS {
			segment := WorkerExecutionSegment{Source: v.Source, Kind: strconv.FormatUint(v.Kind, 10),
				StartNS: start.ObservedNS, EndNS: event.ObservedNS, DurationNS: event.DurationNS}
			if start.KnownFields&jhlog.CausalHasTargetTID != 0 && start.TargetTID != 0 {
				segment.TargetTID = strconv.FormatUint(start.TargetTID, 10)
				if event.KnownFields&(jhlog.CausalHasTargetTID|jhlog.CausalHasCPUNanos) ==
					jhlog.CausalHasTargetTID|jhlog.CausalHasCPUNanos && event.TargetTID == start.TargetTID {
					cpu := event.CPUNS
					segment.CPUNS = &cpu
				}
			}
			result = append(result, segment)
		}
		start = nil
		ambiguous = false
	}
	return result
}
