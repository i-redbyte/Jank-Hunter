package analyze

import (
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
	"testing"
)

func TestWorkerRegistrationObservationDoesNotInventQueueOrFutureStart(t *testing.T) {
	for _, late := range []bool{false, true} {
		events := []jhlog.Event{
			{Type: jhlog.EventWorker, TimeMS: 10, Worker: &jhlog.WorkerEvent{InstanceID: 1, Stage: jhlog.WorkerStage(4)}},
			{Type: jhlog.EventWorker, TimeMS: 20, Worker: &jhlog.WorkerEvent{InstanceID: 2, Stage: jhlog.WorkerStage(4)}},
			{Type: jhlog.EventWorker, TimeMS: 30, Worker: &jhlog.WorkerEvent{InstanceID: 2, WorkerRef: jhlog.LocalSymbol(1), Stage: jhlog.WorkerStageStarted}},
			{Type: jhlog.EventWorker, TimeMS: 40, Worker: &jhlog.WorkerEvent{InstanceID: 2, WorkerRef: jhlog.LocalSymbol(1), Stage: jhlog.WorkerStageFinished, Outcome: jhlog.WorkerOutcomeSuccess, DurationMS: 10}},
		}
		if late {
			events[1].TimeMS = 50
		}
		a := inspectLogsForTest("observed", []jhlog.Log{{Dict: map[uint64]string{1: "app.Worker"}, Events: events}}).WorkerAnalysis
		if a.RegisteredObserved != 2 || a.Enqueued != 0 || a.MissingStart != 0 || a.WaitSamples != 0 || a.MaxQueued != 0 || a.Executions != 1 {
			t.Fatalf("late=%v: observation invented an enqueue or lost registration: %+v", late, a)
		}
		if a.Workers[0].RegisteredObserved+a.Workers[1].RegisteredObserved != 2 {
			t.Fatal("per-worker observations lost")
		}
	}
}
