package analyze

import "github.com/i-redbyte/jank-hunter/cli/internal/jhlog"

type databasePhaseAggregate struct {
	durations operationDurationSummary
	totalUS   uint64
}

func (a *databasePhaseAggregate) add(durationUS uint64) {
	a.durations.add(durationUS)
	a.totalUS = saturatingUint64Sum(a.totalUS, durationUS)
}

type databaseTelemetryAggregate struct {
	resultKnown       uint64
	transactionLinked uint64
	preparedExecuted  uint64
	phaseMeasured     uint64
	poolWait          databasePhaseAggregate
	lockWait          databasePhaseAggregate
	execute           databasePhaseAggregate
	materialize       databasePhaseAggregate
	boundaries        [5]uint64
	failureKinds      [8]uint64
	resultKinds       [3]uint64
	resultBuckets     [6]uint64
}

func (a *databaseTelemetryAggregate) add(event *jhlog.DatabaseEvent) {
	if int(event.Boundary) < len(a.boundaries) {
		a.boundaries[event.Boundary]++
	}
	if event.Outcome == jhlog.DatabaseOutcomeFailure && int(event.FailureKind) < len(a.failureKinds) {
		a.failureKinds[event.FailureKind]++
	}
	if event.ResultKnown {
		a.resultKnown++
		if int(event.ResultKind) < len(a.resultKinds) {
			a.resultKinds[event.ResultKind]++
		}
		if int(event.ResultCountBucket) < len(a.resultBuckets) {
			a.resultBuckets[event.ResultCountBucket]++
		}
	}
	if event.TransactionID != 0 {
		a.transactionLinked++
	}
	if event.StatementToken != 0 && event.Operation != jhlog.DatabaseOperationStatement {
		a.preparedExecuted++
	}
	if event.PhaseMask == 0 {
		return
	}
	a.phaseMeasured++
	if event.PhaseMask&jhlog.DatabasePhasePoolWait != 0 {
		a.poolWait.add(event.PoolWaitUS)
	}
	if event.PhaseMask&jhlog.DatabasePhaseLockWait != 0 {
		a.lockWait.add(event.LockWaitUS)
	}
	if event.PhaseMask&jhlog.DatabasePhaseExecute != 0 {
		a.execute.add(event.ExecuteUS)
	}
	if event.PhaseMask&jhlog.DatabasePhaseMaterialize != 0 {
		a.materialize.add(event.MaterializeUS)
	}
}

func databaseTelemetryStats(aggregate *databaseTelemetryAggregate) DatabaseTelemetryStats {
	return DatabaseTelemetryStats{
		ResultKnownCalls:       aggregate.resultKnown,
		TransactionLinkedCalls: aggregate.transactionLinked,
		PreparedExecutionCalls: aggregate.preparedExecuted,
		PhaseMeasuredCalls:     aggregate.phaseMeasured,
		PoolWait:               databasePhaseStats(&aggregate.poolWait),
		LockWait:               databasePhaseStats(&aggregate.lockWait),
		Execute:                databasePhaseStats(&aggregate.execute),
		Materialize:            databasePhaseStats(&aggregate.materialize),
		Boundaries:             databaseNamedValues(aggregate.boundaries[:], databaseBoundaryName),
		FailureKinds:           databaseNamedValues(aggregate.failureKinds[:], databaseFailureKindName),
		ResultKinds:            databaseNamedValues(aggregate.resultKinds[:], databaseResultKindName),
		ResultCountBuckets:     databaseNamedValues(aggregate.resultBuckets[:], databaseResultBucketName),
	}
}

func databasePhaseStats(aggregate *databasePhaseAggregate) DatabasePhaseStats {
	return DatabasePhaseStats{
		Samples:               aggregate.durations.count,
		P50DurationUS:         aggregate.durations.percentile(0.50),
		P95DurationUS:         aggregate.durations.percentile(0.95),
		MaxDurationUS:         aggregate.durations.max,
		TotalDurationUS:       aggregate.totalUS,
		QuantilesApproximated: aggregate.durations.approximated(),
	}
}

func databaseNamedValues(counts []uint64, name func(int) string) []NamedValue {
	result := make([]NamedValue, 0, len(counts)-1)
	for index := 1; index < len(counts); index++ {
		if counts[index] == 0 {
			continue
		}
		result = append(result, NamedValue{Name: name(index), Value: counts[index]})
	}
	return result
}

func databaseBoundaryName(value int) string {
	switch jhlog.DatabaseBoundary(value) {
	case jhlog.DatabaseBoundaryDispatch:
		return "dispatch"
	case jhlog.DatabaseBoundaryExecute:
		return "execute"
	case jhlog.DatabaseBoundaryMaterialize:
		return "materialize"
	case jhlog.DatabaseBoundaryManual:
		return "manual"
	default:
		return "unknown"
	}
}

func databaseFailureKindName(value int) string {
	switch jhlog.DatabaseFailureKind(value) {
	case jhlog.DatabaseFailureCancelled:
		return "cancelled"
	case jhlog.DatabaseFailureBusyLocked:
		return "busy_locked"
	case jhlog.DatabaseFailureConstraint:
		return "constraint"
	case jhlog.DatabaseFailureDiskFull:
		return "disk_full"
	case jhlog.DatabaseFailureCorruption:
		return "corruption"
	case jhlog.DatabaseFailureTimeout:
		return "timeout"
	case jhlog.DatabaseFailureOther:
		return "other"
	default:
		return "unknown"
	}
}

func databaseResultKindName(value int) string {
	switch jhlog.DatabaseResultKind(value) {
	case jhlog.DatabaseResultRows:
		return "rows"
	case jhlog.DatabaseResultAffectedRows:
		return "affected_rows"
	default:
		return "unknown"
	}
}

func databaseResultBucketName(value int) string {
	switch jhlog.DatabaseCountBucket(value) {
	case jhlog.DatabaseCountZero:
		return "0"
	case jhlog.DatabaseCountOne:
		return "1"
	case jhlog.DatabaseCountTwoToTen:
		return "2_10"
	case jhlog.DatabaseCountElevenToHundred:
		return "11_100"
	case jhlog.DatabaseCountOverHundred:
		return "101_plus"
	default:
		return "unknown"
	}
}
