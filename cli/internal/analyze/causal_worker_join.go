package analyze

import (
	"strconv"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

type causalWorkerJoinKey struct {
	scope               string
	instance            uint64
	attempt, generation uint32
}

// Join only on the exact runtime tuple and run/process/session identity. A
// matching clock, TID, class name, or signed rendering of InstanceID is never
// sufficient to attach a legacy Worker attempt to a causal entity.
func attachCausalWorkerAttempts(causal *CausalAnalysis, workers *WorkerAnalysis) {
	if causal == nil || workers == nil {
		return
	}
	index := make(map[causalWorkerJoinKey][]*WorkerAttempt, len(workers.Attempts))
	for i := range workers.Attempts {
		attempt := &workers.Attempts[i]
		instance, err := strconv.ParseUint(attempt.InstanceID, 10, 64)
		if err != nil || instance == 0 || attempt.Scope == "" {
			continue
		}
		key := causalWorkerJoinKey{attempt.Scope, instance, attempt.RunAttempt, attempt.Generation}
		index[key] = append(index[key], attempt)
	}
	attach := func(views []CausalEntityView) {
		for i := range views {
			view := &views[i]
			view.WorkerAttemptLinked = false
			view.WorkerUUID, view.WorkerOutcome, view.WorkerCoverage = "", "", ""
			if view.Kind != uint64(jhlog.CausalWorkerAttempt) || view.Scope == "" {
				continue
			}
			var binding causalWorkerJoinKey
			var known, conflict bool
			for _, event := range view.Events {
				if event.KnownFields&jhlog.CausalHasWorkerBinding == 0 || event.WorkerInstanceID == 0 ||
					event.RunAttempt > uint64(^uint32(0)) || event.Generation > uint64(^uint32(0)) {
					continue
				}
				candidate := causalWorkerJoinKey{strings.ReplaceAll(view.Scope, "/", ""), event.WorkerInstanceID,
					uint32(event.RunAttempt), uint32(event.Generation)}
				if known && binding != candidate {
					conflict = true
					break
				}
				binding, known = candidate, true
			}
			if !known || conflict || binding.scope == strings.Repeat("0", 96) {
				continue
			}
			matches := index[binding]
			if len(matches) != 1 || matches[0].Ambiguous || matches[0].IdentityAmbiguous {
				continue
			}
			view.WorkerAttemptLinked = true
			view.WorkerUUID = matches[0].UUID
			view.WorkerOutcome = matches[0].Outcome
			view.WorkerCoverage = matches[0].Coverage
		}
	}
	attach(causal.EntitiesForReview)
	attach(causal.AllEntities)
	attachWorkerExecutionSegments(causal, workers)
}
