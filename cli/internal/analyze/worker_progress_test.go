package analyze

import (
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func TestWorkerProgressRequiresUniqueActiveAttemptInSameScope(t *testing.T) {
	start, end := uint64(10), uint64(30)
	attempts := []WorkerAttempt{
		{Scope: "session-a", InstanceID: "73", RunAttempt: 2, Generation: 1, StartMS: &start, EndMS: &end},
		{Scope: "session-b", InstanceID: "73", RunAttempt: 2, Generation: 1, StartMS: &start, EndMS: &end},
	}
	progress := []workerRawProgress{
		{scope: "session-b", timeMS: 20, instanceID: 73, runAttempt: 2, generation: 1,
			stage: 1, fields: []jhlog.WorkDataField{{Key: "wrong", Type: "int"}}},
		{scope: "session-a", timeMS: 20, instanceID: 73, runAttempt: 2, generation: 1,
			stage: 1, fields: []jhlog.WorkDataField{{Key: "done", Type: "int"}}},
		{scope: "session-a", timeMS: 25, instanceID: 73, runAttempt: 2, generation: 1,
			stage: 2, omittedUpdates: 4},
	}
	if unmatched := attachWorkerProgress(attempts, progress); unmatched != 0 {
		t.Fatalf("unexpected unmatched progress: %d", unmatched)
	}
	if len(attempts[0].ProgressRequests) != 1 || attempts[0].ProgressRequests[0].Fields[0].Key != "done" ||
		attempts[0].ProgressOmittedUpdates != 4 || attempts[1].ProgressRequests[0].Fields[0].Key != "wrong" {
		t.Fatalf("progress crossed attempt scope: %+v", attempts)
	}
}

func TestWorkerProgressDoesNotGuessWhenAttemptsOverlapOrStartUnknown(t *testing.T) {
	start, end := uint64(10), uint64(30)
	attempts := []WorkerAttempt{
		{Scope: "s", InstanceID: "73", StartMS: &start, EndMS: &end},
		{Scope: "s", InstanceID: "73", StartMS: &start, EndMS: &end},
		{Scope: "s", InstanceID: "74", EndMS: &end},
	}
	progress := []workerRawProgress{{scope: "s", timeMS: 20, instanceID: 73, stage: 1},
		{scope: "s", timeMS: 20, instanceID: 74, stage: 1}}
	if unmatched := attachWorkerProgress(attempts, progress); unmatched != 2 {
		t.Fatalf("ambiguous progress was assigned: %d, %+v", unmatched, attempts)
	}
	for _, attempt := range attempts {
		if len(attempt.ProgressRequests) != 0 {
			t.Fatal("invented progress ownership")
		}
	}
}

func TestWorkerProgressFindsOlderStillActiveAttemptAfterLaterOneEnds(t *testing.T) {
	oldStart, oldEnd, newStart, newEnd := uint64(10), uint64(100), uint64(20), uint64(30)
	attempts := []WorkerAttempt{
		{Scope: "s", InstanceID: "73", StartMS: &oldStart, EndMS: &oldEnd},
		{Scope: "s", InstanceID: "73", StartMS: &newStart, EndMS: &newEnd},
	}
	progress := []workerRawProgress{{scope: "s", instanceID: 73, timeMS: 40, stage: 1,
		fields: []jhlog.WorkDataField{{Key: "old", Type: "int"}}}}
	if unmatched := attachWorkerProgress(attempts, progress); unmatched != 0 ||
		len(attempts[0].ProgressRequests) != 1 || len(attempts[1].ProgressRequests) != 0 {
		t.Fatalf("unique long attempt lost: unmatched=%d attempts=%+v", unmatched, attempts)
	}
}
