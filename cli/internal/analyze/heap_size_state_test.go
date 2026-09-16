package analyze

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestHeapRetainedSizeStateSurvivesJSONAndLegacyInput(t *testing.T) {
	for _, tc := range []struct{ input, want string }{{`"exact"`, "exact"}, {`"estimated"`, "estimated"}, {`"unknown"`, "unknown"}, {`"unsupported-future"`, "unknown"}, {`null`, "unknown"}} {
		t.Run(tc.input, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "heap.json")
			data := `{"leaks":[{"class_name":"app.Screen","retained_size_bytes":8192,"retained_size_state":` + tc.input + `}]}`
			if err := os.WriteFile(path, []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
			heap, err := LoadHeapEvidenceFiles([]string{path}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(heap.Leaks) != 1 || string(heap.Leaks[0].RetainedSizeState) != tc.want || heap.Leaks[0].RetainedSizeBytes != 8192 {
				t.Fatalf("size semantics changed: %+v", heap)
			}
		})
	}
}

func TestHeapRetainedSizeStatesReflectWorkAndGraphCompleteness(t *testing.T) {
	for _, tc := range []struct {
		name     string
		budget   int
		degraded bool
		want     HeapSizeState
	}{{"exact", 10000, false, HeapSizeExact}, {"budget", 0, false, HeapSizeEstimated}, {"graph", 10000, true, HeapSizeUnknown}} {
		t.Run(tc.name, func(t *testing.T) {
			p := heapBudgetGraph(0, 0)
			if tc.degraded {
				p.degrade("edges", "test incomplete edges")
			}
			heap := p.evidenceWithBudget(&heapTraversalBudget{remaining: tc.budget})
			if len(heap.Leaks) != 1 || heap.Leaks[0].RetainedSizeState != tc.want {
				t.Fatalf("expected %s: %+v", tc.want, heap)
			}
			raw, err := json.Marshal(heap)
			if err != nil {
				t.Fatal(err)
			}
			var roundTrip HeapEvidence
			if err = json.Unmarshal(raw, &roundTrip); err != nil {
				t.Fatal(err)
			}
			if roundTrip.Leaks[0].RetainedSizeState != tc.want {
				t.Fatal("state lost in JSON")
			}
		})
	}
}
