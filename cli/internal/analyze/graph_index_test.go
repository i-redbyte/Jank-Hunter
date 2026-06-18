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

func TestClassGraphIndexShortestPathAndCycles(t *testing.T) {
	index := NewClassGraphIndex([]ClassGraphEdge{
		{From: "feature.A", To: "feature.B", Count: 1},
		{From: "feature.B", To: "feature.C", Count: 2},
		{From: "feature.C", To: "feature.A", Count: 3},
		{From: "feature.C", To: "feature.D", Count: 4},
	})

	path := index.ShortestPath("feature.A", "feature.D", 4)
	if got := len(path); got != 3 {
		t.Fatalf("len(path) = %d, want 3: %+v", got, path)
	}
	if path[0].From != "feature.A" || path[2].To != "feature.D" {
		t.Fatalf("unexpected path: %+v", path)
	}

	cycles := index.StronglyConnectedComponents(8)
	if len(cycles) != 1 {
		t.Fatalf("len(cycles) = %d, want 1: %+v", len(cycles), cycles)
	}
	if len(cycles[0].Nodes) != 3 || cycles[0].Weight != 6 {
		t.Fatalf("unexpected cycle: %+v", cycles[0])
	}
}

func TestClassGraphIndexHotPathsPrioritizeRuntimeTargets(t *testing.T) {
	index := NewClassGraphIndex([]ClassGraphEdge{
		{From: "feature.A", To: "feature.B", Count: 4},
		{From: "feature.B", To: "feature.C", Count: 2},
		{From: "feature.Noise", To: "feature.Other", Count: 100},
	})

	paths := index.HotPaths(
		map[string]float64{"feature.C": 10},
		map[string]struct{}{"feature.C": {}},
		4,
	)
	if len(paths) == 0 {
		t.Fatal("HotPaths() returned no paths")
	}
	if got := paths[0].Nodes[len(paths[0].Nodes)-1]; got != "feature.C" {
		t.Fatalf("hot path target = %q, want feature.C: %+v", got, paths[0])
	}
	if !paths[0].RuntimeTarget {
		t.Fatalf("hot path did not preserve runtime target marker: %+v", paths[0])
	}
}

func TestMethodGraphIndexUsesCallerAndCalleeMethods(t *testing.T) {
	index := NewMethodGraphIndex([]ClassGraphEdge{
		{From: "feature.A", To: "feature.B", CallerMethod: "open()V", CalleeMethod: "load()V", Count: 2},
		{From: "feature.A", To: "feature.B", CallerMethod: "open()V", CalleeMethod: "load()V", Count: 3},
	})

	edges := index.Outgoing["feature.A#open()V"]
	if len(edges) != 1 {
		t.Fatalf("len(method edges) = %d, want 1: %+v", len(edges), edges)
	}
	if edges[0].ToMethod != "load()V" || edges[0].Count != 5 {
		t.Fatalf("unexpected method edge: %+v", edges[0])
	}
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
