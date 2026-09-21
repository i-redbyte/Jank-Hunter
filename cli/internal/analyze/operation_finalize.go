package analyze

import (
	"sort"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func (a *operationAnalysisAccumulator) finalize() *OperationAnalysis {
	if a.started == 0 && a.completed == 0 && a.missingStart == 0 {
		return nil
	}
	result := &OperationAnalysis{
		Started:                   a.started,
		Completed:                 a.completed,
		MissingFinish:             uint64(len(a.active) + len(a.ignoredActive)),
		MissingStart:              a.missingStart,
		DuplicateStart:            a.duplicateStart,
		InconsistentLifecycle:     a.inconsistentLifecycle,
		MissingParent:             a.missingParent,
		UnmatchedSignalEvents:     a.unmatchedSignals,
		LateSignalEvents:          a.lateSignalEvents,
		DroppedActiveStarts:       a.droppedActiveStarts,
		DroppedSignalEvents:       a.droppedSignalEvents,
		DroppedSignalRollups:      a.droppedSignalRollups,
		CompletedContextEvictions: a.completedContextEvictions,
		DroppedOperationSamples:   a.droppedOperationSamples,
		DroppedTimeSlotSamples:    a.droppedTimeSlotSamples,
		DroppedDimensionSamples:   a.droppedDimensionSamples,
		DroppedStageSamples:       a.droppedStageSamples,
	}
	for key, aggregate := range a.operations {
		result.Operations = append(result.Operations, operationStats(key, aggregate))
	}
	for key, aggregate := range a.timeSlots {
		stats := operationStats(key.group, aggregate)
		result.TimeSlots = append(result.TimeSlots, OperationTimeSlot{
			StartUnixMS: key.startUnixMS, TimezoneOffsetMin: key.offsetMin,
			Label:     operationSlotLabel(key.startUnixMS, key.offsetMin),
			Operation: stats.Operation, Kind: stats.Kind, Screen: stats.Screen,
			Count: stats.Count, Failures: stats.Failures + stats.Timeouts,
			P50MS: stats.P50MS, P90MS: stats.P90MS, P95MS: stats.P95MS, MaxMS: stats.MaxMS,
			QuantilesApproximated: stats.QuantilesApproximated,
			Budgeted:              stats.Budgeted, BudgetBreaches: stats.BudgetBreaches,
			BudgetBreachRatePct: stats.BudgetBreachRatePct,
			CorrelatedHTTP:      stats.CorrelatedHTTP, CorrelatedHTTPFailures: stats.CorrelatedHTTPFailures,
			CorrelatedHTTPDurationMS: stats.CorrelatedHTTPDurationMS,
			CorrelatedWebSocket:      stats.CorrelatedWebSocket, CorrelatedWebSocketErrors: stats.CorrelatedWebSocketErrors,
			CorrelatedDatabase: stats.CorrelatedDatabase, CorrelatedDatabaseErrors: stats.CorrelatedDatabaseErrors,
			CorrelatedDatabaseMain: stats.CorrelatedDatabaseMain, CorrelatedDatabaseUS: stats.CorrelatedDatabaseUS,
			WorstDatabaseStatements: stats.WorstDatabaseStatements,
			CorrelatedCompose:       stats.CorrelatedCompose, CorrelatedComposeMS: stats.CorrelatedComposeMS,
			CorrelatedWorkers: stats.CorrelatedWorkers, CorrelatedWorkerFailures: stats.CorrelatedWorkerFailures,
			CorrelatedWorkerMS: stats.CorrelatedWorkerMS,
			CorrelatedStalls:   stats.CorrelatedStalls, CorrelatedStallMaxMS: stats.CorrelatedStallMaxMS,
			CorrelatedUIFrames: stats.CorrelatedUIFrames, CorrelatedUIJank: stats.CorrelatedUIJank,
			CorrelatedUIJankRatePct: stats.CorrelatedUIJankRatePct,
			CorrelatedIO:            stats.CorrelatedIO, CorrelatedIODurationUS: stats.CorrelatedIODurationUS,
			CorrelatedIOBytes: stats.CorrelatedIOBytes, CorrelatedProblems: stats.CorrelatedProblems,
			CorrelatedLogRecords:      stats.CorrelatedLogRecords,
			CorrelatedRuntimeCalls:    stats.CorrelatedRuntimeCalls,
			CorrelatedRuntimeTotalMS:  stats.CorrelatedRuntimeTotalMS,
			CorrelatedMetricEvents:    stats.CorrelatedMetricEvents,
			CorrelatedRetainedObjects: stats.CorrelatedRetainedObjects,
			MaxPSSKB:                  stats.MaxPSSKB,
		})
	}
	for key, aggregate := range a.dimensions {
		stats := operationStats(key.group, aggregate)
		result.Dimensions = append(result.Dimensions, OperationDimensionStats{
			Operation: stats.Operation, Kind: stats.Kind, Screen: stats.Screen,
			Key: key.key, Value: key.value, Count: stats.Count, Failures: stats.Failures + stats.Timeouts,
			P90MS: stats.P90MS, P95MS: stats.P95MS, MaxMS: stats.MaxMS, Budgeted: stats.Budgeted,
			QuantilesApproximated: stats.QuantilesApproximated,
			BudgetBreaches:        stats.BudgetBreaches, BudgetBreachRatePct: stats.BudgetBreachRatePct,
		})
	}
	for key, aggregate := range a.stages {
		parentTotal := uint64(0)
		if parent := a.operations[key.parent]; parent != nil {
			parentTotal = parent.totalMS
		}
		share := 0.0
		if parentTotal > 0 {
			share = float64(aggregate.totalMS) * 100 / float64(parentTotal)
		}
		result.Stages = append(result.Stages, OperationStageStats{
			ParentOperation: key.parent.name, ParentKind: key.parent.kind, Screen: key.parent.screen,
			Stage: key.stage, Count: aggregate.count, P50MS: aggregate.durationsMS.percentile(0.50),
			P90MS: aggregate.durationsMS.percentile(0.90),
			P95MS: aggregate.durationsMS.percentile(0.95), MaxMS: aggregate.durationsMS.max,
			QuantilesApproximated: aggregate.durationsMS.approximated(),
			TotalMS:               aggregate.totalMS, ParentTotalMS: parentTotal, SharePct: share,
		})
	}
	for index := range a.incidents {
		materializeOperationIncidentDatabaseStatements(&a.incidents[index])
	}
	result.WorstIncidents = append(result.WorstIncidents, a.incidents...)
	sort.Slice(result.WorstIncidents, func(i, j int) bool {
		return incidentWorse(result.WorstIncidents[i], result.WorstIncidents[j])
	})
	sortOperationAnalysis(result)
	return result
}

func (a *operationAnalysisAccumulator) release() {
	a.header = jhlog.SegmentHeader{}
	a.active = nil
	a.ignoredActive = nil
	a.freeActive = nil
	a.completedContexts.release()
	a.operations = nil
	a.timeSlots = nil
	a.dimensions = nil
	a.stages = nil
	a.incidents = nil
}

func operationStats(key operationGroupKey, aggregate *operationAggregate) OperationStats {
	stats := OperationStats{
		Operation: key.name, Kind: key.kind, Screen: key.screen,
		Count: aggregate.count, Success: aggregate.success, Failures: aggregate.failures,
		Cancelled: aggregate.cancelled, Timeouts: aggregate.timeouts,
		P50MS: aggregate.durationsMS.percentile(0.50), P90MS: aggregate.durationsMS.percentile(0.90),
		P95MS:                 aggregate.durationsMS.percentile(0.95),
		QuantilesApproximated: aggregate.durationsMS.approximated(),
		MaxMS:                 aggregate.durationsMS.max, TotalMS: aggregate.totalMS,
		Budgeted: aggregate.budgeted, BudgetBreaches: aggregate.budgetBreaches,
		CorrelatedHTTP:            aggregate.signals.httpCount,
		CorrelatedHTTPFailures:    aggregate.signals.httpFailures,
		CorrelatedHTTPDurationMS:  aggregate.signals.httpDurationMS,
		CorrelatedWebSocket:       aggregate.signals.webSocketCount,
		CorrelatedWebSocketErrors: aggregate.signals.webSocketErrors,
		CorrelatedDatabase:        aggregate.signals.databaseCount,
		CorrelatedDatabaseErrors:  aggregate.signals.databaseErrors,
		CorrelatedDatabaseMain:    aggregate.signals.databaseMain,
		CorrelatedDatabaseUS:      aggregate.signals.databaseUS,
		WorstDatabaseStatements:   operationDatabaseStatements(aggregate.database),
		CorrelatedCompose:         aggregate.signals.composeCount,
		CorrelatedComposeMS:       aggregate.signals.composeMS,
		CorrelatedWorkers:         aggregate.signals.workerCount,
		CorrelatedWorkerFailures:  aggregate.signals.workerFailures,
		CorrelatedWorkerMS:        aggregate.signals.workerMS,
		CorrelatedStalls:          aggregate.signals.stallCount,
		CorrelatedStallMaxMS:      aggregate.signals.stallMaxMS,
		CorrelatedUIFrames:        aggregate.signals.uiFrames,
		CorrelatedUIJank:          aggregate.signals.uiJank,
		CorrelatedIO:              aggregate.signals.ioCount,
		CorrelatedIODurationUS:    aggregate.signals.ioDurationUS,
		CorrelatedIOBytes:         aggregate.signals.ioBytes,
		CorrelatedProblems:        aggregate.signals.problemCount,
		CorrelatedLogRecords:      aggregate.signals.logRecords,
		CorrelatedRuntimeCalls:    aggregate.signals.runtimeCalls,
		CorrelatedRuntimeTotalMS:  aggregate.signals.runtimeTotalMS,
		CorrelatedMetricEvents:    aggregate.signals.metricEvents,
		CorrelatedRetainedObjects: aggregate.signals.retainedObjects,
		MaxPSSKB:                  aggregate.signals.maxPSSKB,
	}
	if stats.Budgeted > 0 {
		stats.BudgetBreachRatePct = float64(stats.BudgetBreaches) * 100 / float64(stats.Budgeted)
	}
	if stats.CorrelatedUIFrames > 0 {
		stats.CorrelatedUIJankRatePct = float64(stats.CorrelatedUIJank) * 100 / float64(stats.CorrelatedUIFrames)
	}
	return stats
}
