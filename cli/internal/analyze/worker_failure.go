package analyze

import (
	"encoding/hex"
	"sort"
	"strconv"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

const (
	workerFailureLimit = 50000
	workerFailureBytes = 16 << 20
)

func (c *collector) recordWorkerFailure(event jhlog.Event) {
	failure := event.WorkerFailure
	if failure == nil {
		return
	}
	context := c.eventContext("", "")
	excluded := !c.matchesFilters("", context, []string{context.Owner, failure.ExceptionClass}, context.Owner, failure.ExceptionClass)
	c.enableWorkerCorrelation()
	c.workerSeenInLog = true
	if !excluded {
		c.markCohort()
	}
	estimate := 128 + len(c.currentProcess) + len(failure.ExceptionClass) + len(failure.Stack)
	if len(c.workerFailures) >= workerFailureLimit || c.workerFailureBytes+estimate > workerFailureBytes {
		c.workerFailureDropped++
		return
	}
	c.workerFailureBytes += estimate
	c.workerFailures = append(c.workerFailures, workerRawFailure{
		logIndex: c.currentLogIndex, process: c.currentProcess, timeMS: failure.ObservedElapsedNanos / 1_000_000,
		instanceID: failure.InstanceID, runAttempt: uint32(failure.RunAttempt),
		generation: uint32(failure.Generation), exceptionClass: failure.ExceptionClass,
		stack: failure.Stack, stackTruncated: failure.StackTruncated, excluded: excluded,
	})
}

// Missing starts are possible owners before their observed finish, but neither
// a repeated runtime tuple nor an earlier failure-only record proves a lifetime.
func attachWorkerFailures(attempts *[]WorkerAttempt, failures []workerRawFailure) uint64 {
	indices := workerAttemptIntervals(*attempts, true)
	var dropped uint64
	for _, failure := range failures {
		scope := hex.EncodeToString([]byte(failure.scope))
		key := attemptJoinKey{scope, failure.instanceID, failure.runAttempt, failure.generation}
		index := -1
		if intervals := indices[key]; intervals != nil {
			if candidate, unique := intervals.uniqueAt(failure.timeMS); unique {
				index = candidate
			}
		}
		if index < 0 {
			if failure.excluded {
				continue
			}
			if len(*attempts) >= workerAttemptLimit {
				dropped++
				continue
			}
			*attempts = append(*attempts, WorkerAttempt{
				Scope: scope, Process: failure.process, InstanceID: strconv.FormatUint(failure.instanceID, 10),
				RunAttempt: failure.runAttempt, Generation: failure.generation,
				Outcome: "unknown", Coverage: "failure_only",
			})
			index = len(*attempts) - 1
		}
		attempt := &(*attempts)[index]
		attempt.FailureCount++
		if attempt.FailureClass == "" || attempt.FailureObservedMS == nil || failure.timeMS < *attempt.FailureObservedMS {
			at := failure.timeMS
			attempt.FailureObservedMS = &at
			attempt.FailureClass = failure.exceptionClass
			attempt.FailureStack = failure.stack
			attempt.FailureStackTruncated = failure.stackTruncated
		}
	}
	sort.SliceStable(*attempts, func(i, j int) bool {
		left, right := (*attempts)[i], (*attempts)[j]
		if left.Scope != right.Scope {
			return left.Scope < right.Scope
		}
		return attemptObservedTime(left) < attemptObservedTime(right)
	})
	return dropped
}
