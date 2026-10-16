package jhlog

import (
	"bytes"
	"path/filepath"
	"reflect"
	"testing"
)

func TestWorkerProgressRequestRoundTripKeepsMetadataWithoutValues(t *testing.T) {
	size := uint64(4)
	want := &WorkerProgressEvent{Version: 1, Stage: 1, InstanceID: 73, RunAttempt: 2, Generation: 1,
		ObservedElapsedNanos: 987654321, Fields: []WorkDataField{{Key: "done", Type: "int", PayloadBytes: &size}},
		OmittedKeys: 2, TruncatedFields: 1}
	path := filepath.Join(t.TempDir(), "progress.jhlog")
	writeClosedEvents(t, path, []Event{{Type: EventWorkerProgress,
		DiagnosticEvent: &DiagnosticEvent{WorkerProgress: want}}})
	got, err := readLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Events) != 1 || !reflect.DeepEqual(got.Events[0].WorkerProgress, want) {
		t.Fatalf("progress metadata round-trip: %+v", got.Events)
	}
}

func TestWorkerProgressRejectsInvalidStageAndUnboundedMetadata(t *testing.T) {
	base := WorkerProgressEvent{Version: 1, Stage: 1, InstanceID: 1}
	for _, change := range []func(*WorkerProgressEvent){
		func(v *WorkerProgressEvent) { v.Version = 2 },
		func(v *WorkerProgressEvent) { v.Stage = 3 },
		func(v *WorkerProgressEvent) { v.OmittedUpdates = 1 },
		func(v *WorkerProgressEvent) { v.InstanceID = 0 },
		func(v *WorkerProgressEvent) { v.Fields = make([]WorkDataField, 9) },
		func(v *WorkerProgressEvent) {
			v.Fields = []WorkDataField{{Key: "secret", Type: string(make([]byte, 33))}}
		},
		func(v *WorkerProgressEvent) { v.Stage = 2; v.Fields = []WorkDataField{{Key: "x", Type: "int"}} },
	} {
		bad := base
		change(&bad)
		var encoded bytes.Buffer
		if err := encodeEventPayload(&encoded, Event{Type: EventWorkerProgress,
			DiagnosticEvent: &DiagnosticEvent{WorkerProgress: &bad}}, nil); err == nil {
			t.Fatalf("accepted invalid progress: %+v", bad)
		}
	}
}
