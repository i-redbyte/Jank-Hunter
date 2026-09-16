package jhlog

import "testing"

func TestStallTrackerBoundsOpenIncidentsAndReleasesTerminalSlot(t *testing.T) {
	var tracker StallTracker
	for id := uint64(1); id <= MaxPendingStallIncidents; id++ {
		if pending, err := tracker.Observe(&StallEvent{IncidentID: id, State: StallStateOngoing}); err != nil || !pending {
			t.Fatalf("admit %d: pending=%v error=%v", id, pending, err)
		}
	}
	if _, err := tracker.Observe(&StallEvent{IncidentID: 65, State: StallStateOngoing}); err == nil {
		t.Fatal("open incident budget was not enforced")
	}
	if _, err := tracker.Observe(&StallEvent{IncidentID: 1, State: StallStateOngoing}); err != nil {
		t.Fatal("an update consumed another slot", err)
	}
	if pending, err := tracker.Observe(&StallEvent{IncidentID: 1, State: StallStateInterrupted}); err != nil || pending {
		t.Fatalf("complete pending=%v error=%v", pending, err)
	}
	if _, err := tracker.Observe(&StallEvent{IncidentID: 65, State: StallStateOngoing}); err != nil {
		t.Fatal("terminal observation failed to release its slot", err)
	}
	for _, state := range []StallState{StallStateOngoing, StallStateRecovered, StallStateInterrupted} {
		if _, err := tracker.Observe(&StallEvent{IncidentID: 1, State: state}); err == nil {
			t.Fatalf("terminal ID was reused with state %d", state)
		}
	}
	tracker.Reset()
	if pending, err := tracker.Observe(&StallEvent{IncidentID: 1, State: StallStateOngoing}); err != nil || !pending {
		t.Fatalf("a new session could not reuse ID 1: %v", err)
	}
}

func TestStallTrackerIdentityBudgetFailsExplicitlyAndResetReleasesMemory(t *testing.T) {
	var tracker StallTracker
	for id := uint64(1); id <= MaxStallIncidentIDs; id++ {
		if _, err := tracker.Observe(&StallEvent{IncidentID: id, State: StallStateRecovered}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := tracker.Observe(&StallEvent{IncidentID: MaxStallIncidentIDs + 1, State: StallStateRecovered}); err == nil {
		t.Fatal("identity budget was not enforced")
	}
	tracker.Reset()
	if tracker.states != nil || tracker.open != 0 {
		t.Fatal("large prior session still retained after reset")
	}
}
