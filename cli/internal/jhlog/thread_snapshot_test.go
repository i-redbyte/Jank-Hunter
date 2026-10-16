package jhlog

import (
	"bytes"
	"path/filepath"
	"reflect"
	"testing"
)

func TestThreadSnapshotRoundTrip(t *testing.T) {
	snapshot := &ThreadSnapshotEvent{Version: 1, ElapsedMS: 1000, ScanDurationMS: 3, IntervalMS: 5000, TicksPerSecond: 100, Complete: true,
		Threads: []ThreadObservation{{TID: 42, StartTicks: 100, Name: "pool ) worker", OSState: "S", UserTicks: 11, SystemTicks: 7, Priority: -5, Nice: 2}}}
	path := filepath.Join(t.TempDir(), "threads.jhlog")
	writeClosedEvents(t, path, []Event{{Type: EventThreadSnapshot, TimeMS: 1000, DiagnosticEvent: &DiagnosticEvent{ThreadSnapshot: snapshot}}})
	log, err := readLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(log.Events) != 1 || !reflect.DeepEqual(log.Events[0].ThreadSnapshot, snapshot) {
		t.Fatalf("round trip: %+v", log.Events)
	}
}

func TestAndroidThreadSnapshotFixture(t *testing.T) {
	log, err := readLog("../../../wire/testdata/thread-snapshot-5.1.0.jhlog")
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, event := range log.Events {
		if event.DiagnosticEvent != nil && event.ThreadSnapshot != nil {
			s := event.ThreadSnapshot
			count++
			if !s.Complete || len(s.Threads) == 0 || s.TicksPerSecond == 0 {
				t.Fatalf("invalid Android snapshot: %+v", s)
			}
		}
	}
	if count != 3 || !log.Result.Sealed {
		t.Fatalf("snapshots=%d sealed=%v", count, log.Result.Sealed)
	}
}

func TestThreadSnapshotRejectsDuplicateKernelIdentity(t *testing.T) {
	row := ThreadObservation{TID: 1, OSState: "S"}
	snapshot := &ThreadSnapshotEvent{Version: 1, Complete: true, Threads: []ThreadObservation{row, row}}
	var output bytes.Buffer
	if err := (eventPayloadEncoder{writer: &output}).encodeThreadSnapshot(snapshot); err == nil {
		t.Fatal("duplicate thread inflated a full snapshot")
	}
}

func TestJavaThreadSnapshotRoundTripKeepsIdentitiesAndStatesSeparate(t *testing.T) {
	snapshot := &ThreadSnapshotEvent{Version: 2, Complete: true, DroppedJavaIdentities: 3, Threads: []ThreadObservation{{TID: 42, StartTicks: 100, Name: "kernel", OSState: "S", Java: &JavaThreadObservation{ID: 9007199254740993, Name: "Java name", State: "RUNNABLE", Daemon: true, FirstObservedElapsedNanos: 1, Source: "app.Task.run"}}}}
	path := filepath.Join(t.TempDir(), "java-threads.jhlog")
	writeClosedEvents(t, path, []Event{{Type: EventThreadSnapshot, DiagnosticEvent: &DiagnosticEvent{ThreadSnapshot: snapshot}}})
	log, err := readLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(log.Events) != 1 || !reflect.DeepEqual(log.Events[0].ThreadSnapshot, snapshot) {
		t.Fatalf("lost Java/kernel distinction: %+v", log.Events)
	}
}

func TestSchemaThreeOwnershipRoundTripAndLegacyUnknown(t *testing.T) {
	yes := true
	owned := &ThreadSnapshotEvent{Version: 3, Complete: true, Threads: []ThreadObservation{{TID: 42, OSState: "S", Java: &JavaThreadObservation{ID: 7, State: "RUNNABLE", OwnedByJankHunter: &yes}}}}
	path := filepath.Join(t.TempDir(), "owned.jhlog")
	writeClosedEvents(t, path, []Event{{Type: EventThreadSnapshot, DiagnosticEvent: &DiagnosticEvent{ThreadSnapshot: owned}}})
	log, err := readLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(log.Events[0].ThreadSnapshot, owned) {
		t.Fatalf("ownership lost: %+v", log.Events[0].ThreadSnapshot)
	}
}

func TestMinifiedAndroidOwnedThreadFixture(t *testing.T) {
	log, err := readLog("../../../wire/testdata/thread-owned-r8-5.1.0.jhlog")
	if err != nil {
		t.Fatal(err)
	}
	owned, app := 0, 0
	for _, event := range log.Events {
		if event.DiagnosticEvent == nil || event.ThreadSnapshot == nil {
			continue
		}
		if event.ThreadSnapshot.Version != 3 {
			t.Fatalf("unexpected thread schema: %d", event.ThreadSnapshot.Version)
		}
		for _, row := range event.ThreadSnapshot.Threads {
			if row.Java == nil || row.Java.OwnedByJankHunter == nil {
				continue
			}
			if *row.Java.OwnedByJankHunter {
				if row.Java.Source != "Jank Hunter maintenance" {
					t.Fatalf("unexpected owner: %+v", row.Java)
				}
				owned++
			} else if row.Java.Name == "jh-identity-r8-java" {
				app++
			}
		}
	}
	if owned < 2 || app < 2 || !log.Result.Sealed {
		t.Fatalf("owned=%d app=%d sealed=%v", owned, app, log.Result.Sealed)
	}
}

func TestMinifiedAndroidOwnedRuntimeConsumers(t *testing.T) {
	log, err := readLog("../../../wire/testdata/thread-owned-expanded-r8-5.1.0.jhlog")
	if err != nil {
		t.Fatal(err)
	}
	roles := map[string]bool{}
	app := false
	for _, event := range log.Events {
		if event.DiagnosticEvent == nil || event.ThreadSnapshot == nil {
			continue
		}
		for _, row := range event.ThreadSnapshot.Threads {
			if row.Java == nil || row.Java.OwnedByJankHunter == nil {
				continue
			}
			if *row.Java.OwnedByJankHunter {
				roles[row.Java.Source] = true
			}
			if row.Java.Name == "jh-identity-r8-java" && !*row.Java.OwnedByJankHunter {
				app = true
			}
		}
	}
	for _, role := range []string{"Jank Hunter maintenance", "Jank Hunter event consumer", "Jank Hunter writer", "Jank Hunter main watchdog"} {
		if !roles[role] {
			t.Fatalf("missing verified role %s: %+v", role, roles)
		}
	}
	if !app || !log.Result.Sealed {
		t.Fatalf("app=%v sealed=%v", app, log.Result.Sealed)
	}
}

func TestJavaThreadSnapshotRejectsUnknownStateOrMissingIdentity(t *testing.T) {
	for _, j := range []*JavaThreadObservation{{State: "RUNNABLE"}, {ID: 1, State: "S"}} {
		s := &ThreadSnapshotEvent{Version: 2, Threads: []ThreadObservation{{TID: 1, OSState: "S", Java: j}}}
		var output bytes.Buffer
		if err := (eventPayloadEncoder{writer: &output}).encodeThreadSnapshot(s); err == nil {
			t.Fatal("invalid Java observation accepted")
		}
	}
}

func TestSchemaOneDoesNotSilentlyDiscardJavaCoverage(t *testing.T) {
	var output bytes.Buffer
	if err := (eventPayloadEncoder{writer: &output}).encodeThreadSnapshot(&ThreadSnapshotEvent{Version: 1, DroppedJavaIdentities: 1}); err == nil {
		t.Fatal("schema1 discarded Java loss")
	}
}

func TestMinifiedJavaThreadIdentityFixture(t *testing.T) {
	log, err := readLog("../../../wire/testdata/thread-java-r8-5.1.0.jhlog")
	if err != nil {
		t.Fatal(err)
	}
	linked := 0
	for _, e := range log.Events {
		if e.DiagnosticEvent == nil || e.ThreadSnapshot == nil {
			continue
		}
		for _, r := range e.ThreadSnapshot.Threads {
			if j := r.Java; j != nil && j.Name == "jh-identity-r8-java" {
				linked++
				if j.State != "TIMED_WAITING" || !j.Daemon || j.ID == 0 || j.FirstObservedElapsedNanos > e.ThreadSnapshot.ElapsedMS*1000000 {
					t.Fatalf("invalid real link: %+v", j)
				}
			}
		}
	}
	if linked != 2 {
		t.Fatalf("expected two retained observations, got %d", linked)
	}
}
