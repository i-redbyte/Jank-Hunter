package analyze

import (
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
	"testing"
)

func attemptEvent(at, instance uint64, stage jhlog.WorkerStage, attempt uint32) jhlog.Event {
	return jhlog.Event{Type: jhlog.EventWorker, TimeMS: at, Producer: jhlog.ProducerMetadata{HasThread: true, ThreadID: 7}, Worker: &jhlog.WorkerEvent{WorkerRef: jhlog.LocalSymbol(1), InstanceID: instance, Stage: stage, RunAttempt: attempt, Outcome: jhlog.WorkerOutcomeRetry, DurationMS: 10}}
}
func TestWorkerAttemptsKeepRepeatedPeriodicRunsAndUnfinishedEvidence(t *testing.T) {
	events := []jhlog.Event{attemptEvent(10, 1, jhlog.WorkerStageStarted, 0), attemptEvent(20, 1, jhlog.WorkerStageFinished, 0), attemptEvent(30, 1, jhlog.WorkerStageStarted, 0)}
	a := inspectLogsForTest("attempts", []jhlog.Log{{Dict: map[uint64]string{1: "app.Worker"}, Events: events}}).WorkerAnalysis
	if len(a.Attempts) != 2 {
		t.Fatalf("attempts: %+v", a.Attempts)
	}
	first, last := a.Attempts[0], a.Attempts[1]
	if first.StartMS == nil || *first.StartMS != 10 || first.EndMS == nil || *first.EndMS != 20 || first.Outcome != "retry" || first.DurationMS == nil || *first.DurationMS != 10 {
		t.Fatalf("complete: %+v", first)
	}
	if last.EndMS != nil || last.DurationMS != nil || last.Outcome != "unknown" || last.Coverage != "missing_finish" {
		t.Fatalf("invented finish: %+v", last)
	}
	if first.StartThreadID != "7" || first.EndThreadID != "7" {
		t.Fatalf("producer threads: %+v", first)
	}
}

func TestWorkerAttemptRetainsObservedApplicationOperationIdentity(t *testing.T) {
	start := attemptEvent(10, 1, jhlog.WorkerStageStarted, 0)
	start.Attribution = jhlog.AttributionContext{Present: true, OperationID: 77}
	finish := attemptEvent(20, 1, jhlog.WorkerStageFinished, 0)
	finish.Attribution = jhlog.AttributionContext{Present: true, OperationID: 77}
	analysis := inspectLogsForTest("worker context", []jhlog.Log{{Events: []jhlog.Event{start, finish}}}).WorkerAnalysis
	if len(analysis.Attempts) != 1 || analysis.Attempts[0].OperationID != "77" {
		t.Fatalf("application operation identity lost: %+v", analysis.Attempts)
	}
}

func TestWorkerAttemptRejectsUnconfirmedOrConflictingOperationIdentity(t *testing.T) {
	start := attemptEvent(10, 1, jhlog.WorkerStageStarted, 0)
	start.Attribution = jhlog.AttributionContext{Present: true, OperationID: 77}
	finish := attemptEvent(20, 1, jhlog.WorkerStageFinished, 0)
	finish.Attribution = jhlog.AttributionContext{Present: true, OperationID: 88}
	analysis := inspectLogsForTest("conflicting context", []jhlog.Log{{Events: []jhlog.Event{start, finish}}}).WorkerAnalysis
	if len(analysis.Attempts) != 1 || analysis.Attempts[0].OperationID != "" ||
		!analysis.Attempts[0].OperationContextAmbiguous {
		t.Fatalf("conflicting context treated as exact: %+v", analysis.Attempts)
	}
	finish.Attribution = jhlog.AttributionContext{}
	analysis = inspectLogsForTest("missing finish context", []jhlog.Log{{Events: []jhlog.Event{start, finish}}}).WorkerAnalysis
	if len(analysis.Attempts) != 1 || analysis.Attempts[0].OperationID != "" {
		t.Fatalf("unconfirmed context treated as exact: %+v", analysis.Attempts)
	}
}
func TestWorkerAttemptsNeverInventStartOrMergeIndependentSessions(t *testing.T) {
	logs := []jhlog.Log{{Events: []jhlog.Event{attemptEvent(10, 1, jhlog.WorkerStageStarted, 0)}}, {Events: []jhlog.Event{attemptEvent(20, 1, jhlog.WorkerStageFinished, 0)}}}
	a := inspectLogsForTest("sessions", logs).WorkerAnalysis
	if len(a.Attempts) != 2 {
		t.Fatalf("joined sessions: %+v", a.Attempts)
	}
	for _, v := range a.Attempts {
		if v.EndMS != nil && (v.StartMS != nil || v.Coverage != "missing_start") {
			t.Fatalf("invented start: %+v", v)
		}
	}
}
func TestWorkerAttemptsDoNotPairDifferentGenerationOrAttempt(t *testing.T) {
	events := []jhlog.Event{attemptEvent(10, 1, jhlog.WorkerStageStarted, 0), attemptEvent(20, 1, jhlog.WorkerStageFinished, 1)}
	a := inspectLogsForTest("attempt mismatch", []jhlog.Log{{Events: events}}).WorkerAnalysis
	if len(a.Attempts) != 2 || a.Attempts[0].EndMS != nil || a.Attempts[1].StartMS != nil {
		t.Fatalf("paired distinct attempts: %+v", a.Attempts)
	}
}

func TestWorkerAttemptHistoryLimitIsExplicitAndStillAcceptsRetainedFinish(t *testing.T) {
	events := make([]workerRawEvent, workerAttemptLimit+2)
	for i := 0; i <= workerAttemptLimit; i++ {
		events[i] = workerRawEvent{scope: "s", instanceID: uint64(i + 1), stage: jhlog.WorkerStageStarted, timeMS: uint64(i)}
	}
	events[len(events)-1] = workerRawEvent{scope: "s", instanceID: 1, stage: jhlog.WorkerStageFinished, timeMS: 99999}
	attempts, dropped := buildWorkerAttempts(events)
	if len(attempts) != workerAttemptLimit || dropped != 1 || attempts[0].EndMS == nil {
		t.Fatalf("bounded history: %d/%d", len(attempts), dropped)
	}
}
func TestWorkerLifecycleRetentionReportsDroppedEvents(t *testing.T) {
	c := newCollector("bounded", 1, Options{})
	c.startLog(jhlog.SegmentHeader{})
	for i := 0; i <= workerLifecycleEventLimit; i++ {
		c.recordWorker(nil, attemptEvent(uint64(i), uint64(i+1), jhlog.WorkerStageStarted, 0))
	}
	a := c.finalizeWorkerAnalysis()
	if len(c.workerEvents) > workerLifecycleEventLimit || a.DroppedLifecycleEvents != 1 {
		t.Fatalf("unbounded or silent loss: %d/%d", len(c.workerEvents), a.DroppedLifecycleEvents)
	}
}

func TestRepeatedConcurrentStartsAreExplicitlyAmbiguous(t *testing.T) {
	events := []workerRawEvent{{scope: "s", instanceID: 1, stage: jhlog.WorkerStageStarted, timeMS: 1}, {scope: "s", instanceID: 1, stage: jhlog.WorkerStageStarted, timeMS: 2}, {scope: "s", instanceID: 1, stage: jhlog.WorkerStageFinished, timeMS: 3}}
	attempts, _ := buildWorkerAttempts(events)
	if len(attempts) != 2 || !attempts[0].Ambiguous || !attempts[1].Ambiguous {
		t.Fatalf("lost ambiguity: %+v", attempts)
	}
}

func BenchmarkWorkerAttemptsRepeatedStarts(b *testing.B) {
	events := make([]workerRawEvent, 100000)
	for i := range events {
		events[i] = workerRawEvent{scope: "s", instanceID: 1, stage: jhlog.WorkerStageStarted, timeMS: uint64(i)}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		attempts, dropped := buildWorkerAttempts(events)
		if len(attempts) != workerAttemptLimit || dropped != 50000 {
			b.Fatal("bad bounded history")
		}
	}
}

func TestLifecycleOverflowDoesNotRetainNewSessionMetadata(t *testing.T) {
	c := newCollector("bounded scopes", 1, Options{})
	c.startLog(jhlog.SegmentHeader{})
	for i := 0; i < workerLifecycleEventLimit; i++ {
		c.recordWorker(nil, attemptEvent(uint64(i), uint64(i+1), jhlog.WorkerStageStarted, 0))
	}
	c.workerCollectorState.startLog()
	c.currentLogIndex++
	c.recordWorker(nil, attemptEvent(200000, 999999, jhlog.WorkerStageStarted, 0))
	c.workerCollectorState.finishLog(c.currentLogIndex, jhlog.StreamResult{Source: "overflow"})
	if len(c.workerLogIdentities) != 0 {
		t.Fatal("retained metadata without retained events")
	}
}

func TestWorkerAttemptThreadLinksCarryCalendarAnchorSeparately(t *testing.T) {
	c := newCollector("clock", 1, Options{})
	c.startDiagnosticSegment(jhlog.SegmentHeader{SegmentStartElapsedUS: 100000, SegmentStartUnixMS: 1000000})
	c.recordWorker(map[uint64]string{1: "app.Work"}, attemptEvent(150, 1, jhlog.WorkerStageStarted, 0))
	c.recordWorker(map[uint64]string{1: "app.Work"}, attemptEvent(180, 1, jhlog.WorkerStageFinished, 0))
	a := c.finalizeWorkerAnalysis().Attempts[0]
	if a.StartUnixMS == nil || *a.StartUnixMS != 1000050 || a.EndUnixMS == nil || *a.EndUnixMS != 1000080 {
		t.Fatalf("missing calendar endpoints: %+v", a)
	}
	if *a.StartMS != 150 || *a.EndMS != 180 {
		t.Fatal("changed monotonic timeline")
	}
}
