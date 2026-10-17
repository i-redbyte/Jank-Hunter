package analyze

import "github.com/i-redbyte/jank-hunter/cli/internal/jhlog"

// This is the state of a UUID in WorkInfo, not proof that a captured attempt is
// still executing. WorkInfo and WorkerParameters use different attempt counters;
// periodic runs can also reuse the same counter and generation.
type WorkerJobObservation struct {
	State, WorkInfoRunAttempt, Generation uint64
	ObservedElapsedMS                     uint64
	ObservedUnixMS, AgeAtCaptureMS        *uint64
	Freshness                             string
	AbsentFromLaterCompleteSnapshot       bool
	Progress                              []jhlog.WorkDataField
	OmittedProgressKeys                   uint64
}

type observedWorkerJobKey struct{ scope, uuid string }

func attachWorkerJobObservations(analysis *WorkerAnalysis, registry *WorkRegistryAnalysis) {
	if analysis == nil || registry == nil {
		return
	}
	// Restrict the index to retained attempts: at most workerAttemptLimit entries.
	latest := make(map[observedWorkerJobKey]*WorkerJobObservation)
	for _, a := range analysis.Attempts {
		if a.UUID != "" && !a.IdentityAmbiguous {
			latest[observedWorkerJobKey{a.Scope, a.UUID}] = nil
		}
	}
	latestComplete := make(map[string]uint64)
	for _, s := range registry.Snapshots {
		if s.Complete && s.ObservedElapsedMS > latestComplete[s.Scope] {
			latestComplete[s.Scope] = s.ObservedElapsedMS
		}
		if s.Unavailable != 0 || s.CoverageReason == "conflicting_packets" || s.CoverageReason == "invalid_rows" {
			continue
		}
		for _, job := range s.Jobs {
			key := observedWorkerJobKey{s.Scope, job.UUID}
			prior, wanted := latest[key]
			if !wanted || (prior != nil && prior.ObservedElapsedMS >= s.ObservedElapsedMS) {
				continue
			}
			freshness := "unknown"
			if s.AgeAtCaptureMS != nil && s.IntervalMS > 0 {
				freshness = "fresh"
				if *s.AgeAtCaptureMS > s.IntervalMS {
					freshness = "stale"
				}
			}
			latest[key] = &WorkerJobObservation{State: job.State, WorkInfoRunAttempt: job.RunAttempt, Generation: job.Generation, ObservedElapsedMS: s.ObservedElapsedMS, ObservedUnixMS: s.ObservedUnixMS, AgeAtCaptureMS: s.AgeAtCaptureMS, Freshness: freshness, Progress: job.Progress, OmittedProgressKeys: job.OmittedProgressKeys}
		}
	}
	for i := range analysis.Attempts {
		a := &analysis.Attempts[i]
		if a.IdentityAmbiguous {
			continue
		}
		a.LastJobObservation = latest[observedWorkerJobKey{a.Scope, a.UUID}]
		if a.LastJobObservation != nil {
			a.LastJobObservation.AbsentFromLaterCompleteSnapshot = latestComplete[a.Scope] > a.LastJobObservation.ObservedElapsedMS
		}
	}
}
