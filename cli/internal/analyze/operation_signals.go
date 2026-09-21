package analyze

import (
	"sort"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func (a *operationAnalysisAccumulator) recordSignal(event jhlog.Event, operationID uint64, owner ...string) {
	if operationID == 0 || event.Operation != nil {
		return
	}
	caller := ""
	if len(owner) > 0 {
		caller = owner[0]
	}
	signal, tracked := operationSignal(event, caller)
	if !tracked {
		return
	}
	a.recordResolvedSignal(operationID, signal, nil)
}

func (a *operationAnalysisAccumulator) recordDatabaseStatement(
	operationID uint64,
	query, source, operation string,
	database *jhlog.DatabaseEvent,
	flags uint64,
) {
	if operationID == 0 || database == nil {
		return
	}
	signal, _ := operationSignal(jhlog.Event{Database: database, Flags: flags}, "")
	statement := OperationDatabaseStatementStats{
		Query: query, Source: source, Operation: operation, Calls: 1,
		TotalDurationUS: database.DurationUS, MaxDurationUS: database.DurationUS,
	}
	if database.Outcome == jhlog.DatabaseOutcomeFailure {
		statement.Failures = 1
	}
	if flags&uint64(jhlog.FlagThreadMain) != 0 {
		statement.MainThreadCalls = 1
	}
	a.recordResolvedSignal(operationID, signal, &statement)
}

func (a *operationAnalysisAccumulator) recordResolvedSignal(
	operationID uint64,
	signal operationSignals,
	database *OperationDatabaseStatementStats,
) {
	key := operationInstanceKey{process: a.header.ProcessInstanceID, id: operationID}
	active := a.active[key]
	if active != nil {
		active.inclusive.merge(signal)
		if database != nil {
			active.database.add(*database)
		}
		return
	}
	if _, droppedByLimit := a.ignoredActive[key]; droppedByLimit {
		a.droppedSignalEvents++
		return
	}
	if _, exists := a.completedContexts.get(key); !exists {
		a.unmatchedSignals++
		return
	}
	a.lateSignalEvents++
	a.rollUpLateSignal(key, signal, database)
}

func (s *operationDatabaseSignals) merge(other operationDatabaseSignals) {
	for index := uint8(0); index < other.count; index++ {
		s.add(other.statements[index])
	}
}

func (s *operationDatabaseSignals) add(candidate OperationDatabaseStatementStats) {
	for index := uint8(0); index < s.count; index++ {
		current := &s.statements[index]
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
	if s.count < operationDatabaseStatementLimit {
		s.statements[s.count] = candidate
		s.count++
		return
	}
	minimum := 0
	for index := 1; index < operationDatabaseStatementLimit; index++ {
		if operationDatabaseStatementLess(s.statements[index], s.statements[minimum]) {
			minimum = index
		}
	}
	if operationDatabaseStatementLess(s.statements[minimum], candidate) {
		s.statements[minimum] = candidate
	}
}

func operationDatabaseStatementLess(left, right OperationDatabaseStatementStats) bool {
	if left.Failures != right.Failures {
		return left.Failures < right.Failures
	}
	if left.MainThreadCalls != right.MainThreadCalls {
		return left.MainThreadCalls < right.MainThreadCalls
	}
	if left.TotalDurationUS != right.TotalDurationUS {
		return left.TotalDurationUS < right.TotalDurationUS
	}
	if left.MaxDurationUS != right.MaxDurationUS {
		return left.MaxDurationUS < right.MaxDurationUS
	}
	if left.Calls != right.Calls {
		return left.Calls < right.Calls
	}
	if left.Query != right.Query {
		return left.Query > right.Query
	}
	if left.Source != right.Source {
		return left.Source > right.Source
	}
	return left.Operation > right.Operation
}

func operationDatabaseStatements(signals operationDatabaseSignals) []OperationDatabaseStatementStats {
	if signals.count == 0 {
		return nil
	}
	result := make([]OperationDatabaseStatementStats, signals.count)
	copy(result, signals.statements[:signals.count])
	sort.Slice(result, func(i, j int) bool {
		return operationDatabaseStatementLess(result[j], result[i])
	})
	return result
}

func operationSignal(event jhlog.Event, owner string) (operationSignals, bool) {
	var signal operationSignals
	switch {
	case event.HTTP != nil:
		signal.httpCount = 1
		signal.httpDurationMS = event.HTTP.DurationMS
		if httpEventFailed(event.HTTP, event.Flags) {
			signal.httpFailures = 1
		}
	case event.WebSocket != nil:
		signal.webSocketCount = 1
		if event.WebSocket.Stage == jhlog.WebSocketStageFailed {
			signal.webSocketErrors = 1
		}
	case event.Database != nil:
		signal.databaseCount = 1
		signal.databaseUS = event.Database.DurationUS
		if event.Database.Outcome == jhlog.DatabaseOutcomeFailure {
			signal.databaseErrors = 1
		}
		if event.Flags&uint64(jhlog.FlagThreadMain) != 0 {
			signal.databaseMain = 1
		}
	case event.Worker != nil:
		if event.Worker.Stage != jhlog.WorkerStageFinished {
			return operationSignals{}, false
		}
		signal.workerCount = 1
		signal.workerMS = event.Worker.DurationMS
		if event.Worker.Outcome != jhlog.WorkerOutcomeSuccess {
			signal.workerFailures = 1
		}
	case event.Stall != nil:
		signal.stallCount = 1
		signal.stallMaxMS = event.Stall.DurationMS
	case event.UIWindow != nil:
		signal.uiFrames = event.UIWindow.FrameCount
		signal.uiJank = event.UIWindow.JankCount
	case event.IO != nil:
		signal.ioCount = 1
		signal.ioDurationUS = event.IO.DurationUS
		if event.Flags&uint64(jhlog.FlagIOBytesKnown) != 0 {
			signal.ioBytes = event.IO.Bytes
		}
	case event.Problem != nil:
		signal.problemCount = maxUint64(event.Problem.Count, 1)
	case event.LogSpam != nil:
		signal.logRecords = event.LogSpam.Count
	case event.RuntimeCall != nil:
		signal.runtimeCalls = event.RuntimeCall.Count
		signal.runtimeTotalMS = event.RuntimeCall.TotalMS
		if strings.HasPrefix(owner, "jankhunter.semantic.v1.compose.") {
			signal.composeCount = event.RuntimeCall.Count
			signal.composeMS = event.RuntimeCall.TotalMS
		}
	case event.Metric != nil:
		signal.metricEvents = 1
	case event.Retained != nil:
		signal.retainedObjects = event.Retained.Count
	case event.Memory != nil:
		signal.maxPSSKB = event.Memory.PSSKB
	default:
		return operationSignals{}, false
	}
	return signal, true
}

func (a *operationAnalysisAccumulator) rollUpLateSignal(
	key operationInstanceKey,
	signal operationSignals,
	database *OperationDatabaseStatementStats,
) {
	var visited [operationParentDepthLimit]operationInstanceKey
	for depth := 0; depth < operationParentDepthLimit; depth++ {
		for index := 0; index < depth; index++ {
			if visited[index] == key {
				a.droppedSignalRollups++
				return
			}
		}
		visited[depth] = key
		context, exists := a.completedContexts.get(key)
		if !exists {
			return
		}
		if context.included {
			if context.aggregate != nil {
				context.aggregate.signals.merge(signal)
				if database != nil {
					context.aggregate.database.add(*database)
				}
			}
			if context.slotAggregate != nil {
				context.slotAggregate.signals.merge(signal)
				if database != nil {
					context.slotAggregate.database.add(*database)
				}
			}
			a.mergeLateSignalIntoIncident(key, signal, database)
		}
		if context.parentID == 0 {
			return
		}
		key = operationInstanceKey{process: key.process, id: context.parentID}
		if parent := a.active[key]; parent != nil {
			parent.inclusive.merge(signal)
			if database != nil {
				parent.database.add(*database)
			}
			return
		}
	}
	a.droppedSignalRollups++
}
