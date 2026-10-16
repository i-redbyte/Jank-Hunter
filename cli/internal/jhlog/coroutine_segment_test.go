package jhlog

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestCoroutineSegmentRoundTripPreservesThreadMigrationEvidence(t *testing.T) {
	want := &CoroutineSegmentEvent{Version: 1, Token: 81, Generation: 4, JavaThreadID: 11,
		ExitJavaThreadID: 11, StartedElapsedMS: 100, EndedElapsedMS: 120,
		State: 0, Owner: "app.FeedRepository.load"}
	path := filepath.Join(t.TempDir(), "coroutine.jhlog")
	writeClosedEvents(t, path, []Event{{Type: EventCoroutineSegment,
		DiagnosticEvent: &DiagnosticEvent{CoroutineSegment: want}}})
	got, err := readLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Events) != 1 || !reflect.DeepEqual(got.Events[0].CoroutineSegment, want) {
		t.Fatalf("coroutine segment lost identity or time: %+v", got.Events)
	}
}

func TestCoroutineSegmentV2KeepsNativeThreadEndpointsWithoutSnapshots(t *testing.T) {
	want := &CoroutineSegmentEvent{Version: 2, Token: 81, Generation: 4, JavaThreadID: 11,
		ExitJavaThreadID: 22, StartedElapsedMS: 100, EndedElapsedMS: 120,
		State: 1, EntryTID: 151, ExitTID: 252, Owner: "app.FeedRepository.load"}
	path := filepath.Join(t.TempDir(), "coroutine-tid.jhlog")
	writeClosedEvents(t, path, []Event{{Type: EventCoroutineSegment,
		DiagnosticEvent: &DiagnosticEvent{CoroutineSegment: want}}})
	got, err := readLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Events) != 1 || !reflect.DeepEqual(got.Events[0].CoroutineSegment, want) {
		t.Fatalf("native endpoints lost: %+v", got.Events)
	}
}

func TestCoroutineSegmentV3RunningObservationHasNoExit(t *testing.T) {
	want := &CoroutineSegmentEvent{Version: 3, Token: 81, Generation: 4, JavaThreadID: 11,
		StartedElapsedMS: 100, EndedElapsedMS: 120, State: 4,
		EntryTID: 151, Owner: "app.FeedRepository.load"}
	path := filepath.Join(t.TempDir(), "coroutine-running.jhlog")
	writeClosedEvents(t, path, []Event{{Type: EventCoroutineSegment,
		DiagnosticEvent: &DiagnosticEvent{CoroutineSegment: want}}})
	got, err := readLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Events) != 1 || !reflect.DeepEqual(got.Events[0].CoroutineSegment, want) {
		t.Fatalf("running segment gained an invented exit: %+v", got.Events)
	}
}

func TestCoroutineSegmentRejectsContradictoryRunningExit(t *testing.T) {
	for _, v := range []*CoroutineSegmentEvent{
		{Version: 3, Token: 1, JavaThreadID: 11, ExitJavaThreadID: 22, EntryTID: 151,
			StartedElapsedMS: 100, EndedElapsedMS: 120, State: 4, Owner: "owner"},
		{Version: 2, Token: 1, JavaThreadID: 11, EntryTID: 151,
			StartedElapsedMS: 100, EndedElapsedMS: 120, State: 4, Owner: "owner"},
	} {
		if validCoroutineSegment(v) {
			t.Fatalf("contradictory running segment accepted: %+v", v)
		}
	}
}
