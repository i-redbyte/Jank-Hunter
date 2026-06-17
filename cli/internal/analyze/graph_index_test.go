package analyze

import "testing"

func TestClassGraphIndexRelevantEdgesIncludesSelectedAndRuntimeTargets(t *testing.T) {
	index := NewClassGraphIndex([]ClassGraphEdge{
		{From: "feature.A", To: "feature.B", CallerMethod: "a()V", CalleeMethod: "b", Count: 2},
		{From: "feature.C", To: "feature.A", CallerMethod: "c()V", CalleeMethod: "a", Count: 3},
		{From: "feature.D", To: "feature.Runtime", CallerMethod: "d()V", CalleeMethod: "r", Count: 4},
		{From: "feature.Noise", To: "feature.Other", CallerMethod: "n()V", CalleeMethod: "o", Count: 5},
	})

	edges := index.RelevantEdges(
		map[string]struct{}{"feature.A": {}},
		map[string]struct{}{"feature.Runtime": {}},
	)

	if len(edges) != 3 {
		t.Fatalf("len(edges) = %d, want 3: %+v", len(edges), edges)
	}
	assertGraphEdge(t, edges, "feature.A", "feature.B")
	assertGraphEdge(t, edges, "feature.C", "feature.A")
	assertGraphEdge(t, edges, "feature.D", "feature.Runtime")
}

func TestClassGraphIndexDeduplicatesCyclesAndRepeatedLookups(t *testing.T) {
	index := NewClassGraphIndex([]ClassGraphEdge{
		{From: "feature.A", To: "feature.B", CallerMethod: "a()V", CalleeMethod: "b", Count: 2},
		{From: "feature.A", To: "feature.B", CallerMethod: "a()V", CalleeMethod: "b", Count: 3},
		{From: "feature.B", To: "feature.A", CallerMethod: "b()V", CalleeMethod: "a", Count: 4},
	})

	edges := index.RelevantEdges(
		map[string]struct{}{"feature.A": {}, "feature.B": {}},
		nil,
	)

	if len(edges) != 2 {
		t.Fatalf("len(edges) = %d, want 2: %+v", len(edges), edges)
	}
	for _, edge := range edges {
		if edge.From == "feature.A" && edge.To == "feature.B" && edge.Count != 5 {
			t.Fatalf("dedup count = %d, want 5", edge.Count)
		}
	}
	assertGraphEdge(t, edges, "feature.B", "feature.A")
}

func assertGraphEdge(t *testing.T, edges []ClassGraphEdge, from string, to string) {
	t.Helper()
	for _, edge := range edges {
		if edge.From == from && edge.To == to {
			return
		}
	}
	t.Fatalf("edge %s -> %s not found in %+v", from, to, edges)
}

