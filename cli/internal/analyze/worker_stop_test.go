package analyze

import (
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
	"testing"
)

func TestWorkerStopIsNotFailureOrCancellation(t *testing.T) {
	summary := inspectLogsForTest("stop", []jhlog.Log{{Dict: map[uint64]string{1: "app.Worker"}, Events: []jhlog.Event{{Type: jhlog.EventWorker, TimeMS: 100, Worker: &jhlog.WorkerEvent{WorkerRef: jhlog.LocalSymbol(1), InstanceID: 1, Stage: jhlog.WorkerStageFinished, Outcome: jhlog.WorkerOutcomeStopped, DurationMS: 10}}}}})
	a := summary.WorkerAnalysis
	if a == nil || a.Stopped != 1 || a.Cancelled != 0 || a.Failures != 0 || a.Workers[0].Stopped != 1 {
		t.Fatalf("misclassified stop: %+v", a)
	}
}

func TestUnfinishedWorkerContributesOnlyItsObservedStartToConcurrency(t *testing.T) {
	event := func(time, id uint64, stage jhlog.WorkerStage, duration uint64) jhlog.Event {
		outcome := jhlog.WorkerOutcomeUnknown
		if stage == jhlog.WorkerStageFinished {
			outcome = jhlog.WorkerOutcomeSuccess
		}
		return jhlog.Event{Type: jhlog.EventWorker, TimeMS: time, Worker: &jhlog.WorkerEvent{WorkerRef: jhlog.LocalSymbol(1), InstanceID: id, Stage: stage, Outcome: outcome, DurationMS: duration}}
	}
	// Worker 1 has a known 10..100 interval. Worker 2 is observed starting at50, worker3 at200.
	// Worker2's missing finish must not invent continued execution at200.
	a := inspectLogsForTest("incomplete", []jhlog.Log{{Dict: map[uint64]string{1: "app.Worker"}, Events: []jhlog.Event{
		event(10, 1, jhlog.WorkerStageStarted, 0), event(50, 2, jhlog.WorkerStageStarted, 0), event(100, 1, jhlog.WorkerStageFinished, 90), event(200, 3, jhlog.WorkerStageStarted, 0),
	}}}).WorkerAnalysis
	if a.MaxRunning != 2 || a.RunSamples != 1 || a.MissingFinish != 2 {
		t.Fatalf("incorrect observed concurrency: %+v", a)
	}
}
