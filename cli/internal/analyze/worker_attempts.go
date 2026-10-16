package analyze

import (
	"encoding/hex"
	"sort"
	"strconv"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

// A producer thread is evidence at one lifecycle event, never the attempt's exclusive executor.
type WorkerAttempt struct {
	StartUnixMS, EndUnixMS                *uint64
	LastJobObservation                    *WorkerJobObservation
	UUID                                  string
	IdentityAmbiguous                     bool
	Scope, Process, Worker, InstanceID    string
	RunAttempt, Generation                uint32
	StartMS, EndMS, DurationMS            *uint64
	StartThreadID, EndThreadID            string
	Outcome, Coverage                     string
	StopReason                            *int32
	FailureObservedMS                     *uint64
	FailureClass, FailureStack            string
	FailureStackTruncated                 bool
	FailureCount                          uint32
	ProgressRequests                      []WorkerProgressRequest
	ProgressOmittedUpdates                uint64
	Screen, Operation, Owner, OperationID string
	OperationContextAmbiguous             bool
	Ambiguous                             bool

	ForegroundEnteredMS, ForegroundBoundaryMS        *uint64
	ExecutionSegments                                []WorkerExecutionSegment
	ExecutionSegmentsOmitted, ExecutionEventsOmitted uint64
}

type WorkerProgressRequest struct {
	ObservedMS                   uint64
	Fields                       []jhlog.WorkDataField
	OmittedKeys, TruncatedFields uint64
}

const workerAttemptLimit = 50000

type attemptJoinKey struct {
	scope               string
	instance            uint64
	attempt, generation uint32
}

func buildWorkerAttempts(events []workerRawEvent) ([]WorkerAttempt, uint64) {
	var result []WorkerAttempt
	pending := make(map[attemptJoinKey][]int)
	var dropped uint64
	for _, event := range events {
		if event.stage != jhlog.WorkerStageStarted && event.stage != jhlog.WorkerStageFinished {
			continue
		}
		key := attemptJoinKey{event.scope, event.instanceID, event.runAttempt, event.generation}
		if event.stage == jhlog.WorkerStageFinished && len(pending[key]) > 0 {
			indices := pending[key]
			index := indices[0]
			if len(indices) == 1 {
				delete(pending, key)
			} else {
				pending[key] = indices[1:]
			}
			finishAttempt(&result[index], event)
			continue
		}
		if len(result) >= workerAttemptLimit {
			dropped++
			continue
		}
		item := WorkerAttempt{Scope: hex.EncodeToString([]byte(event.scope)), Process: event.process, Worker: event.worker,
			InstanceID: strconv.FormatUint(event.instanceID, 10), RunAttempt: event.runAttempt, Generation: event.generation,
			Screen: event.screen, Operation: event.operation, Owner: event.owner,
			OperationID: optionalUnsignedID(event.operationID), Outcome: "unknown"}
		if event.stage == jhlog.WorkerStageStarted {
			at := event.timeMS
			item.StartMS = &at
			item.StartUnixMS = event.unixMS
			item.StartThreadID = event.threadID
			item.Coverage = "missing_finish"
			if indices := pending[key]; len(indices) > 0 {
				// Older pending entries already carry ambiguity; touch only the first.
				result[indices[0]].Ambiguous = true
				item.Ambiguous = true
			}
			pending[key] = append(pending[key], len(result))
		} else {
			item.Coverage = "missing_start"
			finishAttempt(&item, event)
		}
		result = append(result, item)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Scope != result[j].Scope {
			return result[i].Scope < result[j].Scope
		}
		return attemptObservedTime(result[i]) < attemptObservedTime(result[j])
	})
	return result, dropped
}

func attemptObservedTime(a WorkerAttempt) uint64 {
	if a.StartMS != nil {
		return *a.StartMS
	}
	if a.EndMS != nil {
		return *a.EndMS
	}
	if a.FailureObservedMS != nil {
		return *a.FailureObservedMS
	}
	return 0
}

func finishAttempt(item *WorkerAttempt, event workerRawEvent) {
	finishOperationID := optionalUnsignedID(event.operationID)
	if item.StartMS == nil || item.OperationID == "" || finishOperationID == "" ||
		item.OperationID != finishOperationID {
		item.OperationContextAmbiguous = item.OperationID != "" && finishOperationID != "" &&
			item.OperationID != finishOperationID
		item.OperationID = ""
	}
	at, duration := event.timeMS, event.durationMS
	item.EndMS = &at
	item.EndUnixMS = event.unixMS
	item.DurationMS = &duration
	item.EndThreadID = event.threadID
	item.Outcome = attemptOutcome(event.outcome)
	if item.StartMS != nil {
		item.Coverage = "start_and_finish_observed"
	}
	if event.flags&uint64(jhlog.FlagWorkerStopReasonKnown) != 0 {
		reason := int32(event.stopReason)
		item.StopReason = &reason
	}
	if item.Worker == "" || item.Worker == "unknown" {
		item.Worker = event.worker
	}
}

func attemptOutcome(value jhlog.WorkerOutcome) string {
	switch value {
	case jhlog.WorkerOutcomeSuccess:
		return "success"
	case jhlog.WorkerOutcomeFailure:
		return "failure"
	case jhlog.WorkerOutcomeRetry:
		return "retry"
	case jhlog.WorkerOutcomeCancelled:
		return "cancelled"
	case jhlog.WorkerOutcomeStopped:
		return "stopped"
	default:
		return "unknown"
	}
}

func workerProducerThread(producer jhlog.ProducerMetadata) string {
	if !producer.HasThread {
		return ""
	}
	return strconv.FormatUint(producer.ThreadID, 10)
}

const workerLifecycleEventLimit = 100000

func (c *collector) retainWorkerLifecycle(worker string, context SignalContextStats) bool {
	estimate := 256 + len(worker) + len(context.Screen) + len(context.Operation) + len(context.Owner) + len(c.currentProcess)
	if len(c.workerEvents) >= workerLifecycleEventLimit || c.workerLifecycleBytes+estimate > 32<<20 {
		c.workerLifecycleDropped++
		return false
	}
	c.workerLifecycleBytes += estimate
	return true
}
