package analyze

import (
	"encoding/json"
	"os"
	"testing"
)

// Opt-in ART acceptance: run ConcurrencyRegistryFootprintTest, pull the HPROF,
// then set JH_CAUSAL_REGISTRY_HEAP and (for R8) JH_CAUSAL_REGISTRY_CLASS.
func TestCausalRegistryRetainedBudgetOnART(t *testing.T) {
	path := os.Getenv("JH_CAUSAL_REGISTRY_HEAP")
	if path == "" {
		t.Skip("requires the real saturated ART registry heap")
	}
	className := os.Getenv("JH_CAUSAL_REGISTRY_CLASS")
	if className == "" {
		className = "io.jankhunter.runtime.RuntimeCausalRegistry"
	}
	parser := newHprofParser(path, targetClassSet([]string{className}))
	if err := parser.parse(); err != nil {
		t.Fatal(err)
	}
	entryClass := os.Getenv("JH_CAUSAL_REGISTRY_ENTRY_CLASS")
	if entryClass == "" {
		entryClass = "io.jankhunter.runtime.RuntimeCausalRegistry$Entry"
	}
	parents := parser.rootBFS()
	maxEntries := 0
	for _, id := range parser.targetNodes()[className] {
		scratch := newHeapReachabilityScratch(len(parser.nodes))
		if !parser.markReachableFromLimited(parser.rootIDs(), id, scratch, &heapTraversalBudget{remaining: maxHprofRetainedWork}) {
			t.Fatal("could not prove the registry-owned entry population")
		}
		entries := 0
		for index, parent := range parents.entries {
			if parent.rootIndex != 0 && !scratch.contains(uint32(index+1)) && parser.nodes[index].className == entryClass {
				entries++
			}
		}
		if entries > maxEntries {
			maxEntries = entries
		}
	}
	if maxEntries != 4096 {
		t.Fatalf("expected exactly 4096 registry-owned weak entries, got %d", maxEntries)
	}
	evidence := parser.evidence()
	if len(evidence.Warnings) != 0 {
		t.Fatalf("incomplete heap analysis: %v", evidence.Warnings)
	}
	if len(evidence.Leaks) != 1 {
		t.Fatalf("expected registry evidence, got %d entries", len(evidence.Leaks))
	}
	registry := evidence.Leaks[0]
	if registry.RetainedSizeState != HeapSizeExact {
		t.Fatalf("retained size is not exact: %+v", registry)
	}
	if registry.RetainedObjectCount < 4096 {
		t.Fatalf("registry was not saturated: %d retained objects", registry.RetainedObjectCount)
	}
	if registry.RetainedSizeBytes >= 16*1024*1024 {
		t.Fatalf("registry retained budget exceeded: %d bytes", registry.RetainedSizeBytes)
	}
	t.Logf("saturated registry owns %d weak entries", maxEntries)
	data, err := json.Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	t.Log(string(data))
	if output := os.Getenv("JH_CAUSAL_REGISTRY_RESULT"); output != "" {
		report, err := json.Marshal(map[string]any{"entry_count": maxEntries, "retained_bytes": registry.RetainedSizeBytes, "retained_size_state": registry.RetainedSizeState})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(output, append(report, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
}
