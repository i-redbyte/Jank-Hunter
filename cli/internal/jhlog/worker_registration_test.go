package jhlog

import (
	"path/filepath"
	"testing"
)

func TestWorkerRegistrationObservationRoundTrip(t *testing.T) {
	for _, count := range []int{1, 140} {
		path := filepath.Join(t.TempDir(), "registration.jhlog")
		events := make([]Event, count)
		for i := range events {
			events[i] = Event{Type: EventWorker, TimeMS: uint64(i + 1), Flags: uint64(FlagWorkerPeriodic), Worker: &WorkerEvent{InstanceID: uint64(i + 1), Stage: WorkerStage(4)}}
		}
		writeClosedEvents(t, path, events)
		log, err := readLog(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(log.Events) != count {
			t.Fatalf("count=%d, decoded=%d", count, len(log.Events))
		}
		for i, event := range log.Events {
			if event.Worker.Stage != WorkerStage(4) || event.Worker.InstanceID != uint64(i+1) || event.Flags != uint64(FlagWorkerPeriodic) {
				t.Fatalf("bad observation: %+v", event)
			}
		}
	}
}

func TestWorkerRegistrationARTFixture(t *testing.T) {
	log, err := readLog("../../../wire/testdata/worker-registration-5.1.0.jhlog")
	if err != nil {
		t.Fatal(err)
	}
	instances := make(map[uint64]int)
	periodic := 0
	observations := 0
	for _, event := range log.Events {
		if event.Worker == nil {
			continue
		}
		if event.Worker.Stage != WorkerStageRegisteredObserved {
			t.Fatalf("unexpected stage: %+v", event.Worker)
		}
		instances[event.Worker.InstanceID]++
		observations++
		if event.Flags&uint64(FlagWorkerPeriodic) != 0 {
			periodic++
		}
	}
	if observations != 7 || len(instances) != 6 || periodic != 1 {
		t.Fatalf("observations=%d UUIDs=%d periodic=%d", observations, len(instances), periodic)
	}
	if !log.Result.Sealed || log.Result.Status != SegmentStatusClosedClean {
		t.Fatalf("incomplete fixture: %+v", log.Result)
	}
	q := log.Result.LatestQuality
	if q == nil || q.Counters[QualityAcceptedEventTotal] != 12 || q.Counters[QualityWrittenEventTotal] != 12 {
		t.Fatalf("quality: %+v", q)
	}
}
