package jhlog

import (
	"bytes"
	"path/filepath"
	"reflect"
	"testing"
)

func TestThreadLifecycleRoundTripDoesNotInventKernelStart(t *testing.T) {
	values := []*ThreadLifecycleEvent{
		{Version: 1, Stage: ThreadRunObserved, JavaID: 9, TargetTID: 42, ObservedElapsedNanos: 123, Name: "short", Source: "app.Task.run", Daemon: true},
		{Version: 1, Stage: ThreadStartReturned, JavaID: 9, ObservedElapsedNanos: 124, Name: "short", Source: "app.Caller.launch"},
		{Version: 1, Stage: ThreadFactoryReturned, JavaID: 10, ObservedElapsedNanos: 125, Name: "new", Source: "app.Caller.factory", Factory: "app.Factory"},
	}
	path := filepath.Join(t.TempDir(), "thread-events.jhlog")
	events := make([]Event, len(values))
	for i, v := range values {
		events[i] = Event{Type: EventThreadLifecycle, DiagnosticEvent: &DiagnosticEvent{ThreadLifecycle: v}}
	}
	writeClosedEvents(t, path, events)
	log, err := readLog(path)
	if err != nil {
		t.Fatal(err)
	}
	for i, v := range values {
		if !reflect.DeepEqual(log.Events[i].ThreadLifecycle, v) {
			t.Fatalf("lost observation: %+v", log.Events[i])
		}
	}
}
func TestThreadLifecycleRejectsUnsupportedStageAndFabricatedTargetTID(t *testing.T) {
	for _, v := range []*ThreadLifecycleEvent{
		{Version: 1, Stage: 259},
		{Version: 1, Stage: ThreadRunObserved},
		{Version: 1, Stage: ThreadStartReturned, TargetTID: 42},
		{Version: 1, Stage: ThreadFactoryReturned, TargetTID: 42},
	} {
		var output bytes.Buffer
		if err := (eventPayloadEncoder{writer: &output}).encodeThreadLifecycle(v); err == nil {
			t.Fatalf("invalid event accepted: %+v", v)
		}
	}
}

func TestMinifiedShortThreadLifecycleFixture(t *testing.T) {
	log, err := readLog("../../../wire/testdata/thread-lifecycle-r8-5.1.0.jhlog")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[ThreadLifecycleStage]bool{}
	for _, event := range log.Events {
		if event.DiagnosticEvent == nil || event.ThreadLifecycle == nil || event.ThreadLifecycle.Name != "jh-short-lived-probe" {
			continue
		}
		p := event.ThreadLifecycle
		seen[p.Stage] = true
		if p.Stage != ThreadRunObserved && (!event.Attribution.Present || event.Attribution.OperationID == 0 || p.TargetTID != 0) {
			t.Fatalf("lost caller operation: %+v", event)
		}
		if p.Stage == ThreadRunObserved && p.TargetTID != event.Producer.ThreadID {
			t.Fatal("run TID disagrees with producer")
		}
	}
	if len(seen) != 3 {
		t.Fatalf("missing stages: %+v", seen)
	}
}
