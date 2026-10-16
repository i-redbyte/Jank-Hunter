package jhlog

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestWorkChainEdgeRoundTripKeepsDeclarationDistinctFromEnqueue(t *testing.T) {
	want := &WorkChainEdgeEvent{Version: 1, ParentMost: 1, ParentLeast: 2, ChildMost: 3, ChildLeast: 4, ObservedElapsedNanos: 500, CallerSource: "example.Caller#chain", SourceTruncated: true}
	path := filepath.Join(t.TempDir(), "chain.jhlog")
	writeClosedEvents(t, path, []Event{{Type: EventWorkChainEdge, DiagnosticEvent: &DiagnosticEvent{WorkChainEdge: want}}})
	got, err := readLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Events) != 1 || got.Events[0].Type != EventWorkChainEdge || !reflect.DeepEqual(got.Events[0].WorkChainEdge, want) {
		t.Fatalf("round trip = %#v", got.Events)
	}
}

func TestMinifiedAndroidWorkChainRecordIsReadable(t *testing.T) {
	log, err := readLog("../../../wire/testdata/work-chain-r8-5.1.0.jhlog")
	if err != nil {
		t.Fatal(err)
	}
	var edges int
	for _, event := range log.Events {
		if event.DiagnosticEvent == nil || event.WorkChainEdge == nil {
			continue
		}
		edges++
		edge := event.WorkChainEdge
		if edge.Version != 1 || edge.ParentUUID() == edge.ChildUUID() || edge.CallerSource == "" {
			t.Fatalf("invalid minified Android edge: %+v", edge)
		}
	}
	if edges != 1 {
		t.Fatalf("expected one real chain edge, got %d", edges)
	}
}
