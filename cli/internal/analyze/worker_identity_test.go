package analyze

import (
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
	"testing"
)

func identityEvent(id, uuid uint64) jhlog.Event {
	return jhlog.Event{Type: jhlog.EventWorkerIdentity, DiagnosticEvent: &jhlog.DiagnosticEvent{WorkerIdentity: &jhlog.WorkerIdentityEvent{Version: 1, InstanceID: id, UUIDLeast: uuid}}}
}
func TestAttemptUUIDUsesOnlyExplicitSameSessionBinding(t *testing.T) {
	events := []jhlog.Event{identityEvent(1, 7), attemptEvent(10, 1, jhlog.WorkerStageStarted, 0)}
	a := inspectLogsForTest("binding", []jhlog.Log{{Events: events}, {Events: []jhlog.Event{attemptEvent(20, 1, jhlog.WorkerStageStarted, 0)}}}).WorkerAnalysis
	bound, unknown := 0, 0
	for _, v := range a.Attempts {
		if v.UUID == "00000000-0000-0000-0000-000000000007" {
			bound++
		} else if v.UUID == "" {
			unknown++
		}
	}
	if bound != 1 || unknown != 1 {
		t.Fatalf("bad binding: %+v", a.Attempts)
	}
}
func TestConflictingUUIDsNeverChooseAnArbitraryBinding(t *testing.T) {
	a := inspectLogsForTest("collision", []jhlog.Log{{Events: []jhlog.Event{identityEvent(1, 7), identityEvent(1, 8), attemptEvent(10, 1, jhlog.WorkerStageStarted, 0)}}}).WorkerAnalysis
	if a.Attempts[0].UUID != "" || !a.Attempts[0].IdentityAmbiguous {
		t.Fatalf("hidden collision: %+v", a.Attempts)
	}
}

func TestIdentityOverflowDoesNotRetainUnboundedSessionMetadata(t *testing.T) {
	c := newCollector("bounded identity", 1, Options{})
	c.startLog(jhlog.SegmentHeader{})
	for i := 0; i < workerIdentityLimit; i++ {
		c.recordWorkerIdentity(identityEvent(uint64(i+1), uint64(i+1)))
	}
	c.workerCollectorState.startLog()
	c.currentLogIndex++
	c.recordWorkerIdentity(identityEvent(999999, 9))
	c.workerCollectorState.finishLog(c.currentLogIndex, jhlog.StreamResult{Source: "overflow"})
	if len(c.workerIdentity.bindings) != workerIdentityLimit || c.workerIdentity.dropped != 1 || len(c.workerLogIdentities) != 0 {
		t.Fatalf("unbounded scope metadata: %d", len(c.workerLogIdentities))
	}
}

func TestMinifiedWorkerIdentityFixtureBindsEveryCapturedAttempt(t *testing.T) {
	summary, err := InspectFilesWithOptions("R8 UUID", []string{"../../../wire/testdata/worker-identity-r8-5.1.0.jhlog"}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if summary.WorkerAnalysis == nil || len(summary.WorkerAnalysis.Attempts) != 11 {
		t.Fatalf("missing attempts: %+v", summary.WorkerAnalysis)
	}
	for _, a := range summary.WorkerAnalysis.Attempts {
		if len(a.UUID) != 36 || a.IdentityAmbiguous {
			t.Fatalf("missing UUID: %+v", a)
		}
	}
}
