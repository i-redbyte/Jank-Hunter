package analyze

import (
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func TestWorkerForegroundStaysWithinUniqueAttempt(t *testing.T) {
	start, end := uint64(10), uint64(40)
	attempts := []WorkerAttempt{{Scope: "73", InstanceID: "7", StartMS: &start, EndMS: &end}}
	events := []workerRawEvent{
		{scope: "s", instanceID: 7, timeMS: 20, stage: jhlog.WorkerStageForegroundEntered},
		{scope: "s", instanceID: 7, timeMS: 35, stage: jhlog.WorkerStageForegroundAttemptBoundary},
	}
	if unmatched := attachWorkerForeground(attempts, events); unmatched != 0 {
		t.Fatalf("unmatched foreground: %d", unmatched)
	}
	if attempts[0].ForegroundEnteredMS == nil || *attempts[0].ForegroundEnteredMS != 20 ||
		attempts[0].ForegroundBoundaryMS == nil || *attempts[0].ForegroundBoundaryMS != 35 {
		t.Fatalf("foreground interval lost: %+v", attempts[0])
	}
}

func TestWorkerForegroundDoesNotAssignOverlappingAttempts(t *testing.T) {
	start, end := uint64(10), uint64(40)
	attempts := []WorkerAttempt{
		{Scope: "73", InstanceID: "7", StartMS: &start, EndMS: &end},
		{Scope: "73", InstanceID: "7", StartMS: &start, EndMS: &end},
	}
	events := []workerRawEvent{{scope: "s", instanceID: 7, timeMS: 20, stage: jhlog.WorkerStageForegroundEntered}}
	if unmatched := attachWorkerForeground(attempts, events); unmatched != 1 || attempts[0].ForegroundEnteredMS != nil {
		t.Fatalf("ambiguous foreground ownership: unmatched=%d attempts=%+v", unmatched, attempts)
	}
}
