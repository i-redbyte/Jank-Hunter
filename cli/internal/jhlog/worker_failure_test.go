package jhlog

import (
	"bytes"
	"path/filepath"
	"reflect"
	"testing"
)

func TestWorkerFailureRoundTripIsSeparateFromLifecycleOutcome(t *testing.T) {
	want := &WorkerFailureEvent{Version: 1, InstanceID: 73, RunAttempt: 2, Generation: 1,
		ObservedElapsedNanos: 987654321, ExceptionClass: "app.SyncException",
		Stack: "app.SyncWorker.doWork:42\napp.Repository.load:18", StackTruncated: true}
	path := filepath.Join(t.TempDir(), "failure.jhlog")
	writeClosedEvents(t, path, []Event{{Type: EventWorkerFailure,
		DiagnosticEvent: &DiagnosticEvent{WorkerFailure: want}}})
	got, err := readLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Events) != 1 || got.Events[0].Type != EventWorkerFailure ||
		!reflect.DeepEqual(got.Events[0].WorkerFailure, want) || got.Events[0].Worker != nil {
		t.Fatalf("failure mixed with lifecycle result: %#v", got.Events)
	}
}

func TestWorkerFailureRejectsOversizedAndUnknownFields(t *testing.T) {
	value := &WorkerFailureEvent{Version: 1, InstanceID: 1, ExceptionClass: "Failure"}
	for _, change := range []func(*WorkerFailureEvent){
		func(v *WorkerFailureEvent) { v.Version = 2 },
		func(v *WorkerFailureEvent) { v.InstanceID = 0 },
		func(v *WorkerFailureEvent) { v.ExceptionClass = "" },
		func(v *WorkerFailureEvent) { v.Stack = string(make([]byte, 1025)) },
	} {
		bad := *value
		change(&bad)
		var encoded bytes.Buffer
		if err := encodeEventPayload(&encoded, Event{Type: EventWorkerFailure,
			DiagnosticEvent: &DiagnosticEvent{WorkerFailure: &bad}}, nil); err == nil {
			t.Fatalf("accepted invalid failure: %+v", bad)
		}
	}
}
