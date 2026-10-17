package analyze

import (
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
	"strconv"
)

type WorkSubmissionAnalysis struct {
	CapturedCalls, UniqueCalls, PeriodicCalls uint64
	HistoryOmitted                            bool
	Observations                              []WorkSubmissionObservation
	DroppedCalls, OmittedRequests             uint64
}
type WorkSubmissionObservation struct {
	UUID, Scope, Process, TimeBasis       string
	TimeMS                                uint64
	ContextCaptured                       bool
	Screen, Operation, OperationID, Owner string
	Submission                            *jhlog.WorkSubmissionEvent
}
type workSubmissionState struct {
	observations     []WorkSubmissionObservation
	retainedBytes    int
	dropped, omitted uint64
}

func (c *collector) recordWorkSubmission(event jhlog.Event) {
	s := &c.workSubmissions
	p := event.WorkSubmission
	s.omitted = saturatingUint64Sum(s.omitted, p.OmittedRequests)
	context := c.eventContext("", "")
	if !event.Attribution.Present {
		context = SignalContextStats{}
	}
	estimate := 288 + len(p.UniqueName) + len(p.CallerSource) + len(c.currentProcess) + len(context.Screen) + len(context.Operation) + len(context.Owner)
	if len(s.observations) >= 50000 || s.retainedBytes+estimate > 16<<20 {
		s.dropped = saturatingUint64Sum(s.dropped, 1)
		return
	}
	s.retainedBytes += estimate
	v := WorkSubmissionObservation{UUID: p.UUID(), Scope: c.workRegistry.scope, Process: c.currentProcess, TimeBasis: "record_time", TimeMS: event.TimeMS, ContextCaptured: event.Attribution.Present, Screen: context.Screen, Operation: context.Operation, Owner: context.Owner, Submission: p}
	if event.Attribution.Present && event.Attribution.OperationID != 0 {
		v.OperationID = strconv.FormatUint(event.Attribution.OperationID, 10)
	}
	if at := observationUnixMS(c.workRegistry.anchorElapsedMS, c.workRegistry.anchorUnixMS, p.ObservedElapsedNanos/1000000); at != nil {
		v.TimeMS = *at
		v.TimeBasis = "observation_clock"
	}
	s.observations = append(s.observations, v)
}
func (s *workSubmissionState) finalize() *WorkSubmissionAnalysis {
	if len(s.observations) == 0 && s.dropped == 0 {
		return nil
	}
	result := &WorkSubmissionAnalysis{CapturedCalls: uint64(len(s.observations)), Observations: s.observations, DroppedCalls: s.dropped, OmittedRequests: s.omitted}
	for _, v := range s.observations {
		if v.Submission.HasUnique {
			result.UniqueCalls++
		}
		if v.Submission.Periodic {
			result.PeriodicCalls++
		}
	}
	return result
}
