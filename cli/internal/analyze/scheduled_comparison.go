package analyze

// Index once per comparison; metric names alone did not identify legacy scheduled executors.
func scheduledTimingTransitions(baseline, candidate *AsyncAnalysis) map[string]bool {
	if baseline == nil || candidate == nil {
		return nil
	}
	before := make(map[string]AsyncExecutorStats, len(baseline.Executors))
	for _, executor := range baseline.Executors {
		before[executor.Name] = executor
	}
	changed := make(map[string]bool)
	for _, after := range candidate.Executors {
		prior, present := before[after.Name]
		if !present {
			continue
		}
		priorScheduled := prior.ScheduledDelaySamples > 0 || prior.ScheduledLatenessSamples > 0
		afterScheduled := after.ScheduledDelaySamples > 0 || after.ScheduledLatenessSamples > 0
		if priorScheduled != afterScheduled && ((!priorScheduled && prior.WaitSamples > 0) || (!afterScheduled && after.WaitSamples > 0)) {
			changed[after.Name] = true
		}
	}
	return changed
}

func queueFindingUsesChangedTiming(finding ProblemFinding, changed map[string]bool) bool {
	if finding.DetectorID != "cpu.async_queue" {
		return false
	}
	for _, location := range finding.Where {
		if changed[location.Owner] {
			return true
		}
	}
	return false
}
