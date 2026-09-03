package analyze

import (
	"sort"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func operationIncident(active *activeOperation, finish *jhlog.OperationEvent, durationMS uint64) OperationIncident {
	budgetMS := microsecondsToMillisecondsCeil(active.budgetUS)
	exceeded := uint64(0)
	if active.budgetUS > 0 && finish.DurationUS > active.budgetUS {
		exceeded = microsecondsToMillisecondsCeil(finish.DurationUS - active.budgetUS)
	}
	incident := OperationIncident{
		instanceKey: active.key,
		StartUnixMS: active.startUnixMS, Operation: active.group.name, Kind: active.group.kind,
		Screen: active.group.screen, Outcome: operationOutcomeName(finish.Outcome), DurationMS: durationMS,
		BudgetMS: budgetMS, BudgetExceededMS: exceeded,
	}
	mergeOperationIncidentSignals(&incident, active.inclusive, active.database)
	incident.Score = operationIncidentScore(incident)
	return incident
}

func mergeOperationIncidentSignals(
	incident *OperationIncident,
	signal operationSignals,
	database operationDatabaseSignals,
) {
	incident.CorrelatedHTTP = saturatingUint64Sum(incident.CorrelatedHTTP, signal.httpCount)
	incident.HTTPFailures = saturatingUint64Sum(incident.HTTPFailures, signal.httpFailures)
	incident.HTTPDurationMS = saturatingUint64Sum(incident.HTTPDurationMS, signal.httpDurationMS)
	incident.CorrelatedWebSocket = saturatingUint64Sum(incident.CorrelatedWebSocket, signal.webSocketCount)
	incident.WebSocketErrors = saturatingUint64Sum(incident.WebSocketErrors, signal.webSocketErrors)
	incident.CorrelatedDatabase = saturatingUint64Sum(incident.CorrelatedDatabase, signal.databaseCount)
	incident.DatabaseErrors = saturatingUint64Sum(incident.DatabaseErrors, signal.databaseErrors)
	incident.DatabaseMainThread = saturatingUint64Sum(incident.DatabaseMainThread, signal.databaseMain)
	incident.DatabaseDurationUS = saturatingUint64Sum(incident.DatabaseDurationUS, signal.databaseUS)
	for index := uint8(0); index < database.count; index++ {
		mergeOperationIncidentDatabaseStatement(incident, database.statements[index])
	}
	incident.CorrelatedCompose = saturatingUint64Sum(incident.CorrelatedCompose, signal.composeCount)
	incident.ComposeDurationMS = saturatingUint64Sum(incident.ComposeDurationMS, signal.composeMS)
	incident.CorrelatedWorkers = saturatingUint64Sum(incident.CorrelatedWorkers, signal.workerCount)
	incident.WorkerFailures = saturatingUint64Sum(incident.WorkerFailures, signal.workerFailures)
	incident.WorkerDurationMS = saturatingUint64Sum(incident.WorkerDurationMS, signal.workerMS)
	incident.CorrelatedStalls = saturatingUint64Sum(incident.CorrelatedStalls, signal.stallCount)
	incident.StallMaxMS = maxUint64(incident.StallMaxMS, signal.stallMaxMS)
	incident.CorrelatedUIFrames = saturatingUint64Sum(incident.CorrelatedUIFrames, signal.uiFrames)
	incident.CorrelatedUIJank = saturatingUint64Sum(incident.CorrelatedUIJank, signal.uiJank)
	incident.CorrelatedIO = saturatingUint64Sum(incident.CorrelatedIO, signal.ioCount)
	incident.IODurationUS = saturatingUint64Sum(incident.IODurationUS, signal.ioDurationUS)
	incident.IOBytes = saturatingUint64Sum(incident.IOBytes, signal.ioBytes)
	incident.CorrelatedProblems = saturatingUint64Sum(incident.CorrelatedProblems, signal.problemCount)
	incident.CorrelatedLogRecords = saturatingUint64Sum(incident.CorrelatedLogRecords, signal.logRecords)
	incident.CorrelatedRuntimeCalls = saturatingUint64Sum(incident.CorrelatedRuntimeCalls, signal.runtimeCalls)
	incident.CorrelatedRuntimeTotalMS = saturatingUint64Sum(incident.CorrelatedRuntimeTotalMS, signal.runtimeTotalMS)
	incident.CorrelatedMetricEvents = saturatingUint64Sum(incident.CorrelatedMetricEvents, signal.metricEvents)
	incident.CorrelatedRetainedObjects = saturatingUint64Sum(incident.CorrelatedRetainedObjects, signal.retainedObjects)
	incident.MaxPSSKB = maxUint64(incident.MaxPSSKB, signal.maxPSSKB)
}

func mergeOperationIncidentDatabaseStatement(
	incident *OperationIncident,
	candidate OperationDatabaseStatementStats,
) {
	for index := uint8(0); index < incident.databaseStatementCount; index++ {
		current := &incident.databaseStatements[index]
		if current.Query != candidate.Query || current.Source != candidate.Source ||
			current.Operation != candidate.Operation {
			continue
		}
		current.Calls = saturatingUint64Sum(current.Calls, candidate.Calls)
		current.Failures = saturatingUint64Sum(current.Failures, candidate.Failures)
		current.MainThreadCalls = saturatingUint64Sum(current.MainThreadCalls, candidate.MainThreadCalls)
		current.TotalDurationUS = saturatingUint64Sum(current.TotalDurationUS, candidate.TotalDurationUS)
		current.MaxDurationUS = maxUint64(current.MaxDurationUS, candidate.MaxDurationUS)
		return
	}
	if incident.databaseStatementCount < operationDatabaseStatementLimit {
		incident.databaseStatements[incident.databaseStatementCount] = candidate
		incident.databaseStatementCount++
		return
	}
	minimum := 0
	for index := 1; index < operationDatabaseStatementLimit; index++ {
		if operationDatabaseStatementLess(incident.databaseStatements[index], incident.databaseStatements[minimum]) {
			minimum = index
		}
	}
	if operationDatabaseStatementLess(incident.databaseStatements[minimum], candidate) {
		incident.databaseStatements[minimum] = candidate
	}
}

func materializeOperationIncidentDatabaseStatements(incident *OperationIncident) {
	if incident.databaseStatementCount == 0 {
		return
	}
	incident.WorstDatabaseStatements = make([]OperationDatabaseStatementStats, incident.databaseStatementCount)
	copy(incident.WorstDatabaseStatements, incident.databaseStatements[:incident.databaseStatementCount])
	sort.Slice(incident.WorstDatabaseStatements, func(i, j int) bool {
		return operationDatabaseStatementLess(incident.WorstDatabaseStatements[j], incident.WorstDatabaseStatements[i])
	})
}

func operationIncidentScore(incident OperationIncident) uint64 {
	rank := uint64(0)
	switch incident.Outcome {
	case "timeout":
		rank = 4
	case "failure":
		rank = 3
	case "cancelled":
		rank = 2
	default:
		if incident.BudgetExceededMS > 0 {
			rank = 1
		}
	}
	score := rank * 1_000_000_000_000_000_000
	score = saturatingUint64Sum(score, minUint64(incident.DurationMS, 999_999_999_999)*1_000_000)
	score = saturatingUint64Sum(score, minUint64(incident.StallMaxMS, 999_999)*1_000)
	score = saturatingUint64Sum(score, minUint64(incident.HTTPFailures+incident.WebSocketErrors+incident.DatabaseErrors+incident.WorkerFailures+incident.CorrelatedProblems, 999))
	return score
}

func (a *operationAnalysisAccumulator) retainIncident(incident OperationIncident) {
	if incident.Score == 0 {
		return
	}
	if a.incidents.Len() < operationIncidentLimit {
		a.incidents.push(incident)
		return
	}
	if incidentWorse(incident, a.incidents[0]) {
		a.incidents.replaceRoot(incident)
	}
}

func (a *operationAnalysisAccumulator) mergeLateSignalIntoIncident(
	key operationInstanceKey,
	signal operationSignals,
	database *OperationDatabaseStatementStats,
) {
	for index := range a.incidents {
		if a.incidents[index].instanceKey != key {
			continue
		}
		incident := &a.incidents[index]
		var databaseSignals operationDatabaseSignals
		if database != nil {
			databaseSignals.add(*database)
		}
		mergeOperationIncidentSignals(incident, signal, databaseSignals)
		incident.Score = operationIncidentScore(*incident)
		a.incidents.restore(index)
		return
	}
}

type operationIncidentHeap []OperationIncident

func (h operationIncidentHeap) Len() int { return len(h) }
func (h operationIncidentHeap) Less(i, j int) bool {
	return incidentWorse(h[j], h[i])
}
func (h operationIncidentHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }

func (h *operationIncidentHeap) push(value OperationIncident) {
	*h = append(*h, value)
	index := len(*h) - 1
	for index > 0 {
		parent := (index - 1) / 2
		if !h.Less(index, parent) {
			break
		}
		h.Swap(index, parent)
		index = parent
	}
}

func (h *operationIncidentHeap) replaceRoot(value OperationIncident) {
	(*h)[0] = value
	index := 0
	for {
		left := index*2 + 1
		if left >= len(*h) {
			return
		}
		smallest := left
		right := left + 1
		if right < len(*h) && h.Less(right, left) {
			smallest = right
		}
		if !h.Less(smallest, index) {
			return
		}
		h.Swap(index, smallest)
		index = smallest
	}
}

func (h *operationIncidentHeap) restore(index int) {
	if index > 0 {
		parent := (index - 1) / 2
		if h.Less(index, parent) {
			for index > 0 {
				parent = (index - 1) / 2
				if !h.Less(index, parent) {
					return
				}
				h.Swap(index, parent)
				index = parent
			}
			return
		}
	}
	for {
		left := index*2 + 1
		if left >= len(*h) {
			return
		}
		smallest := left
		right := left + 1
		if right < len(*h) && h.Less(right, left) {
			smallest = right
		}
		if !h.Less(smallest, index) {
			return
		}
		h.Swap(index, smallest)
		index = smallest
	}
}

func incidentWorse(left, right OperationIncident) bool {
	if left.Score != right.Score {
		return left.Score > right.Score
	}
	if left.StartUnixMS != right.StartUnixMS {
		return left.StartUnixMS < right.StartUnixMS
	}
	if left.Operation != right.Operation {
		return left.Operation < right.Operation
	}
	if left.Screen != right.Screen {
		return left.Screen < right.Screen
	}
	return left.DurationMS > right.DurationMS
}
