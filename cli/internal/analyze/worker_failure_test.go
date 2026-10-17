package analyze

import (
	"encoding/hex"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func TestWorkerFailureJoinsOnlySameScopedAttempt(t *testing.T) {
	start := workerRawEvent{scope: "session-a", process: "app", instanceID: 73,
		runAttempt: 2, generation: 1, stage: jhlog.WorkerStageStarted, timeMS: 10}
	finish := start
	finish.stage, finish.timeMS = jhlog.WorkerStageFinished, 30
	other := start
	other.scope, other.timeMS = "session-b", 11
	attempts, _ := buildWorkerAttempts([]workerRawEvent{start, finish, other})
	failures := []workerRawFailure{{scope: "session-a", instanceID: 73, runAttempt: 2,
		generation: 1, timeMS: 20, exceptionClass: "app.SyncException", stack: "app.Sync.run:42"}}
	attachWorkerFailures(&attempts, failures)
	if attempts[0].FailureClass != "app.SyncException" || attempts[0].FailureStack != "app.Sync.run:42" ||
		attempts[1].FailureClass != "" {
		t.Fatalf("failure crossed attempt scope: %+v", attempts)
	}
}

func TestWorkerFailureRequiresUniqueContainingAttempt(t *testing.T) {
	for _, at := range []uint64{25, 40} {
		t.Run(optionalUnsignedID(at), func(t *testing.T) {
			oldStart, oldEnd, newStart, newEnd := uint64(10), uint64(100), uint64(20), uint64(30)
			scope := hex.EncodeToString([]byte("s"))
			attempts := []WorkerAttempt{
				{Scope: scope, InstanceID: "73", StartMS: &oldStart, EndMS: &oldEnd},
				{Scope: scope, InstanceID: "73", StartMS: &newStart, EndMS: &newEnd},
			}
			attachWorkerFailures(&attempts, []workerRawFailure{{scope: "s", instanceID: 73, timeMS: at, exceptionClass: "app.Failure"}})
			if at == 25 {
				for _, a := range attempts {
					if a.StartMS != nil && a.FailureCount != 0 {
						t.Fatalf("failure assigned to an overlapping attempt: %+v", a)
					}
				}
				if len(attempts) != 3 || attempts[2].Coverage != "failure_only" {
					t.Fatalf("unattributed evidence lost: %+v", attempts)
				}
			} else if len(attempts) != 2 || attempts[0].FailureCount != 1 || attempts[1].FailureCount != 0 {
				t.Fatalf("older unique interval was missed: %+v", attempts)
			}
		})
	}
}

func TestWorkerFailureUsesObservationClockInsteadOfDelayedEnvelope(t *testing.T) {
	c := newCollector("delayed failure", 1, Options{})
	c.startLog(jhlog.SegmentHeader{})
	c.recordWorkerFailure(jhlog.Event{Type: jhlog.EventWorkerFailure, TimeMS: 40,
		DiagnosticEvent: &jhlog.DiagnosticEvent{WorkerFailure: &jhlog.WorkerFailureEvent{
			Version: 1, InstanceID: 73, ObservedElapsedNanos: 20_000_000, ExceptionClass: "app.Failure",
		}}})
	if len(c.workerFailures) != 1 || c.workerFailures[0].timeMS != 20 {
		t.Fatalf("failure observation shifted to envelope clock: %+v", c.workerFailures)
	}
}

func TestWorkerFailureDoesNotCreateForeignRowsUnderClassFilter(t *testing.T) {
	c := newCollector("filtered failures", 1, Options{Filter: Filter{ClassContains: "SelectedWorker"}})
	c.startLog(jhlog.SegmentHeader{})
	c.add(map[uint64]string{1: "app.OtherWorker"}, jhlog.Event{Type: jhlog.EventWorkerFailure, TimeMS: 20,
		Attribution: jhlog.AttributionContext{Present: true, Owner: jhlog.LocalSymbol(1)},
		DiagnosticEvent: &jhlog.DiagnosticEvent{WorkerFailure: &jhlog.WorkerFailureEvent{
			Version: 1, InstanceID: 73, ObservedElapsedNanos: 20_000_000, ExceptionClass: "app.Failure",
		}}})
	if got := c.finalizeWorkerAnalysis(); got != nil && len(got.Attempts) != 0 {
		t.Fatalf("filtered-out worker leaked into attempts: %+v", got.Attempts)
	}
}

func TestWorkerFailureEnrichesSelectedLifecycleWithoutOwnAttribution(t *testing.T) {
	c := newCollector("selected lifecycle", 1, Options{Filter: Filter{ClassContains: "SelectedWorker"}})
	c.startLog(jhlog.SegmentHeader{})
	dict := map[uint64]string{1: "app.SelectedWorker"}
	c.add(dict, jhlog.Event{Type: jhlog.EventWorker, TimeMS: 10, Worker: &jhlog.WorkerEvent{
		WorkerRef: jhlog.LocalSymbol(1), InstanceID: 73, Stage: jhlog.WorkerStageStarted,
	}})
	c.add(dict, jhlog.Event{Type: jhlog.EventWorkerFailure, TimeMS: 20,
		DiagnosticEvent: &jhlog.DiagnosticEvent{WorkerFailure: &jhlog.WorkerFailureEvent{
			Version: 1, InstanceID: 73, ObservedElapsedNanos: 20_000_000, ExceptionClass: "app.Failure",
		}}})
	got := c.finalizeWorkerAnalysis()
	if got == nil || len(got.Attempts) != 1 || got.Attempts[0].FailureClass != "app.Failure" {
		t.Fatalf("selected lifecycle lost un-attributed failure evidence: %+v", got)
	}
}

func TestWorkerFailureMissingStartPreventsFalseUniqueOwnership(t *testing.T) {
	start, end := uint64(10), uint64(100)
	attempts := []WorkerAttempt{
		{Scope: hex.EncodeToString([]byte("s")), InstanceID: "73", StartMS: &start, EndMS: &end},
		{Scope: hex.EncodeToString([]byte("s")), InstanceID: "73", EndMS: &end},
	}
	attachWorkerFailures(&attempts, []workerRawFailure{{scope: "s", instanceID: 73, timeMS: 20, exceptionClass: "app.Failure"}})
	if len(attempts) != 3 {
		t.Fatalf("missing start was incorrectly treated as proof of non-overlap: %+v", attempts)
	}
	for _, attempt := range attempts {
		if attempt.Coverage != "failure_only" && attempt.FailureCount != 0 {
			t.Fatalf("ambiguous failure attached to lifecycle: %+v", attempt)
		}
	}
}

func TestWorkerFailureDoesNotMergeUnobservedAttempts(t *testing.T) {
	var attempts []WorkerAttempt
	failures := []workerRawFailure{{scope: "s", instanceID: 73, timeMS: 20, exceptionClass: "app.First"},
		{scope: "s", instanceID: 73, timeMS: 40, exceptionClass: "app.Second"}}
	attachWorkerFailures(&attempts, failures)
	if len(attempts) != 2 || attempts[0].FailureCount != 1 || attempts[1].FailureCount != 1 {
		t.Fatalf("unobserved lifetimes merged: %+v", attempts)
	}
}

func TestWorkerFailureReportsCapacityLoss(t *testing.T) {
	attempts := make([]WorkerAttempt, workerAttemptLimit)
	if dropped := attachWorkerFailures(&attempts, []workerRawFailure{{scope: "s", instanceID: 73, timeMS: 20,
		exceptionClass: "app.Failure"}}); dropped != 1 || len(attempts) != workerAttemptLimit {
		t.Fatalf("capacity loss was hidden: dropped=%d attempts=%d", dropped, len(attempts))
	}
}

func TestWorkerFailureKeepsEarliestObservedExceptionWithReorderedRecords(t *testing.T) {
	start, end := uint64(10), uint64(100)
	attempts := []WorkerAttempt{{Scope: hex.EncodeToString([]byte("s")), InstanceID: "73", StartMS: &start, EndMS: &end}}
	attachWorkerFailures(&attempts, []workerRawFailure{
		{scope: "s", instanceID: 73, timeMS: 50, exceptionClass: "app.Later"},
		{scope: "s", instanceID: 73, timeMS: 20, exceptionClass: "app.Earlier"},
	})
	if attempts[0].FailureCount != 2 || attempts[0].FailureClass != "app.Earlier" || *attempts[0].FailureObservedMS != 20 {
		t.Fatalf("file order replaced observation order: %+v", attempts[0])
	}
}

func TestWorkerFailureWithoutLifecycleRemainsVisible(t *testing.T) {
	var attempts []WorkerAttempt
	attachWorkerFailures(&attempts, []workerRawFailure{{scope: "session-a", process: "app", instanceID: 73,
		timeMS: 20, exceptionClass: "app.SyncException"}})
	if len(attempts) != 1 || attempts[0].Coverage != "failure_only" ||
		attempts[0].Outcome != "unknown" || attempts[0].FailureClass != "app.SyncException" {
		t.Fatalf("failure-only evidence was lost or invented lifecycle: %+v", attempts)
	}
}

func BenchmarkWorkerFailureReverseObservationOrder(b *testing.B) {
	for _, count := range []int{10000, workerFailureLimit} {
		b.Run(optionalUnsignedID(uint64(count)), func(b *testing.B) {
			failures := make([]workerRawFailure, count)
			for i := range failures {
				failures[i] = workerRawFailure{scope: "s", instanceID: 73, timeMS: uint64(count - i), exceptionClass: "app.Failure"}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				var attempts []WorkerAttempt
				if dropped := attachWorkerFailures(&attempts, failures); dropped != 0 || len(attempts) != count {
					b.Fatalf("evidence lost: dropped=%d attempts=%d", dropped, len(attempts))
				}
			}
		})
	}
}
