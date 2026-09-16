package jhlog

import "fmt"

const MaxPendingStallIncidents = 64
const MaxStallIncidentIDs = 1 << 20

// StallTracker validates one session's opaque IDs without retaining completed payloads.
// Its zero value is ready for use. Budget exhaustion is an error, never silent eviction.
type StallTracker struct {
	states map[uint64]StallState
	open   int
}

// HasIncident allows a consumer to reserve storage before Observe admits a new opaque identity.
func (tracker *StallTracker) HasIncident(id uint64) bool {
	_, exists := tracker.states[id]
	return exists
}

// Observe returns true while the latest observation must wait for completion or end of input.
func (tracker *StallTracker) Observe(stall *StallEvent) (bool, error) {
	if err := validateStallLifecycle(stall); err != nil {
		return false, err
	}
	if stall.IncidentID == 0 {
		return false, nil
	}
	previous, seen := tracker.states[stall.IncidentID]
	if seen && previous != StallStateOngoing {
		return false, fmt.Errorf("stall incident %d has an update after its terminal state", stall.IncidentID)
	}
	if !seen && len(tracker.states) >= MaxStallIncidentIDs {
		return false, fmt.Errorf("stall identity budget exceeded: more than %d incidents in one session", MaxStallIncidentIDs)
	}
	pending := stall.State == StallStateOngoing
	if !seen && pending && tracker.open >= MaxPendingStallIncidents {
		return false, fmt.Errorf("more than %d simultaneous unfinished main-thread stalls", MaxPendingStallIncidents)
	}
	if tracker.states == nil {
		tracker.states = make(map[uint64]StallState)
	}
	tracker.states[stall.IncidentID] = stall.State
	if !seen && pending {
		tracker.open++
	}
	if seen && !pending {
		tracker.open--
	}
	return pending, nil
}

func (tracker *StallTracker) Reset() {
	// Avoid retaining a large prior session's map for every following small session.
	if len(tracker.states) > 4096 {
		tracker.states = nil
	} else {
		clear(tracker.states)
	}
	tracker.open = 0
}
