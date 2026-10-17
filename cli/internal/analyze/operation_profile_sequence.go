package analyze

import "github.com/i-redbyte/jank-hunter/cli/internal/jhlog"

const operationProfileStepLimit = 64

type OperationProfileStep struct {
	Operation string         `json:"operation"`
	Kind      string         `json:"kind"`
	Screen    string         `json:"screen"`
	Stats     OperationStats `json:"stats"`
}

// Sequence identity contains no durations, outcomes, or error counts. Matching
// on those would select away the very changes being measured.
func (a *operationAnalysisAccumulator) startProfileStage(active *activeOperation) {
	if active.parentID == 0 || active.group.kind != "stage" {
		return
	}
	parent := a.active[operationInstanceKey{process: active.key.process, id: active.parentID}]
	if parent == nil {
		active.profileValid = false
		return
	}
	parent.profilePendingStages++
	active.profileStageRegistered = true
	step := OperationProfileStep{Operation: active.group.name, Kind: active.group.kind, Screen: active.group.screen}
	if !active.profileValid || !parent.included || !active.included ||
		len(parent.profileSteps) >= operationProfileStepLimit ||
		!profileAttributeValid(step.Operation) || !profileAttributeValid(step.Kind) || !profileStepScreenValid(step.Screen) {
		parent.profileValid = false
		return
	}
	parent.profileSteps = append(parent.profileSteps, step)
	active.profileStepIndex = len(parent.profileSteps) - 1
	// This is only an index accelerator, not a security hash. Every hit is
	// checked against the complete sequence in recordProfile, so collisions
	// become explicit invalid evidence rather than a false scenario match.
	hash := parent.profileDigest
	if len(parent.profileSteps) == 1 {
		hash = 14695981039346656037
	}
	for _, value := range [3]string{step.Operation, step.Kind, step.Screen} {
		hash = (hash ^ uint64(len(value))) * 1099511628211
		for i := 0; i < len(value); i++ {
			hash = (hash ^ uint64(value[i])) * 1099511628211
		}
	}
	parent.profileDigest = hash
}
func profileStepScreenValid(value string) bool {
	return value == "unknown" || profileAttributeValid(value)
}
func (a *operationAnalysisAccumulator) finishProfileStage(active *activeOperation, finish *jhlog.OperationEvent) {
	if !active.profileStageRegistered {
		return
	}
	parent := a.active[operationInstanceKey{process: active.key.process, id: active.parentID}]
	if parent == nil {
		active.profileValid = false
		return
	}
	if parent.profilePendingStages == 0 {
		parent.profileValid = false
		active.profileValid = false
		return
	}
	parent.profilePendingStages--
	if !active.profileValid || active.profilePendingStages != 0 {
		parent.profileValid = false
	}
	if active.profileStepIndex < 0 || active.profileStepIndex >= len(parent.profileSteps) {
		parent.profileValid = false
		return
	}
	parent.profileSteps[active.profileStepIndex].Stats = singleOperationStats(active, finish)
}
func profileStepsEqual(left, right []OperationProfileStep) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if !scenarioStepEqual(left[i], right[i]) {
			return false
		}
	}
	return true
}

func singleOperationStats(active *activeOperation, finish *jhlog.OperationEvent) OperationStats {
	durationMS := microsecondsToMillisecondsCeil(finish.DurationUS)
	stats := OperationStats{
		Operation: active.group.name, Kind: active.group.kind, Screen: active.group.screen,
		Count: 1, TotalMS: durationMS, P50MS: durationMS, P90MS: durationMS, P95MS: durationMS, MaxMS: durationMS,
		CorrelatedHTTP: active.inclusive.httpCount, CorrelatedHTTPFailures: active.inclusive.httpFailures,
		CorrelatedHTTPDurationMS: active.inclusive.httpDurationMS,
		CorrelatedHTTPRxBytes:    active.inclusive.httpRxBytes, CorrelatedHTTPTxBytes: active.inclusive.httpTxBytes,
		CorrelatedHTTPBytesKnown: active.inclusive.httpBytesKnown,
		CorrelatedDatabase:       active.inclusive.databaseCount, CorrelatedDatabaseErrors: active.inclusive.databaseErrors,
		CorrelatedDatabaseMain: active.inclusive.databaseMain, CorrelatedDatabaseUS: active.inclusive.databaseUS,
		CorrelatedUIFrames: active.inclusive.uiFrames, CorrelatedUIJank: active.inclusive.uiJank,
		CorrelatedIO: active.inclusive.ioCount, CorrelatedIODurationUS: active.inclusive.ioDurationUS,
		CorrelatedIOBytes: active.inclusive.ioBytes, CorrelatedRetainedObjects: active.inclusive.retainedObjects,
		CorrelatedIOBytesKnown: active.inclusive.ioBytesKnown,
		CorrelatedCPUSumX100:   active.inclusive.cpuSumX100, CorrelatedCPUSamples: active.inclusive.cpuSamples,
		CorrelatedMetricEvents: active.inclusive.metricEvents, MaxPSSKB: active.inclusive.maxPSSKB,
	}
	switch finish.Outcome {
	case jhlog.OperationOutcomeSuccess:
		stats.Success = 1
	case jhlog.OperationOutcomeFailure:
		stats.Failures = 1
	case jhlog.OperationOutcomeCancelled:
		stats.Cancelled = 1
	case jhlog.OperationOutcomeTimeout:
		stats.Timeouts = 1
	}
	if active.budgetUS > 0 {
		stats.Budgeted = 1
		if finish.DurationUS > active.budgetUS {
			stats.BudgetBreaches = 1
			stats.BudgetBreachRatePct = 100
		}
	}
	if stats.CorrelatedUIFrames > 0 {
		stats.CorrelatedUIJankRatePct = float64(stats.CorrelatedUIJank) * 100 / float64(stats.CorrelatedUIFrames)
	}
	return stats
}
func profileStepsLess(left, right []OperationProfileStep) bool {
	for i := 0; i < len(left) && i < len(right); i++ {
		if left[i].Operation != right[i].Operation {
			return left[i].Operation < right[i].Operation
		}
		if left[i].Kind != right[i].Kind {
			return left[i].Kind < right[i].Kind
		}
		if left[i].Screen != right[i].Screen {
			return left[i].Screen < right[i].Screen
		}
	}
	return len(left) < len(right)
}
