package jhlog

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestLegacy500StallsRemainReadable(t *testing.T) {
	path := filepath.Join("..", "..", "..", "wire", "testdata", "legacy-5.0.0.jhlog")
	if _, err := ReadSessionHeader(path); err != nil {
		t.Fatal(err)
	}
	stalls := 0
	result, err := StreamFileWithResult(path, func(event Event, _ map[uint64]string) error {
		if event.Stall != nil {
			stalls++
			if event.Stall.IncidentID != 0 || event.Stall.State != StallStateUnknown {
				t.Errorf("legacy evidence acquired an invented lifecycle: %+v", event.Stall)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Events != 3901 || stalls == 0 {
		t.Fatalf("legacy events=%d stalls=%d", result.Events, stalls)
	}
	profile, err := ProfileFiles([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if len(profile.Files) != 1 || profile.Files[0].Format != "jhlog-5.0.0" {
		t.Fatalf("legacy log was relabeled with CLI version: %+v", profile.Files)
	}
}

func TestStallCodecRejectsImpossibleLifecycle(t *testing.T) {
	for _, payload := range []StallEvent{
		{State: 4, IncidentID: 1},
		{State: StallStateOngoing},
		{State: StallStateInterrupted},
	} {
		var encoded bytes.Buffer
		if err := (eventPayloadEncoder{writer: &encoded}).encodeStall(&payload); err == nil {
			t.Errorf("accepted impossible lifecycle: %+v", payload)
		}
	}
}

func TestStallLifecycleRoundTripPreservesIncidentAndState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stall-lifecycle.jhlog")
	closer, writer, err := Create(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []uint64{1, 2, 3} {
		id := uint64(17)
		if state == 3 {
			id = 18
		}
		data, err := json.Marshal(map[string]any{
			"type": EventStall, "time_ms": 200,
			"stall": map[string]uint64{"duration_ms": 200, "incident_id": id, "state": state},
		})
		if err != nil {
			t.Fatal(err)
		}
		var event Event
		if err := json.Unmarshal(data, &event); err != nil {
			t.Fatal(err)
		}
		if err := writer.WriteEvent(event); err != nil {
			t.Fatal(err)
		}
	}
	if err := closer.Close(); err != nil {
		t.Fatal(err)
	}
	var states []uint64
	if err := StreamFile(path, func(event Event, _ map[uint64]string) error {
		if event.Stall == nil {
			return nil
		}
		data, err := json.Marshal(event.Stall)
		if err != nil {
			return err
		}
		var fields struct {
			IncidentID uint64 `json:"incident_id"`
			State      uint64 `json:"state"`
		}
		if err := json.Unmarshal(data, &fields); err != nil {
			return err
		}
		expectedID := uint64(17)
		if fields.State == 3 {
			expectedID = 18
		}
		if fields.IncidentID != expectedID {
			t.Errorf("stall lost its incident identity: %s", data)
		}
		states = append(states, fields.State)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(states) != 3 || states[0] != 1 || states[1] != 2 || states[2] != 3 {
		t.Fatalf("stall lifecycle states = %v, want ongoing/recovered/interrupted", states)
	}
}
