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
		map[string]float64{"feature.A": 5, "feature.C": 10},
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
	if len(paths[0].Nodes) != 3 {
		t.Fatalf("hot path should preserve multi-hop chain: %+v", paths[0])
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

	methods := index.HotMethods(
		map[string]float64{"feature.B": 10},
		map[string]struct{}{"feature.B": {}},
		4,
	)
	if len(methods) == 0 {
		t.Fatal("HotMethods() returned no rows")
	}
	if methods[0].Method != "load()V" || methods[0].Role != "callee" {
		t.Fatalf("unexpected top method: %+v", methods[0])
	}
}

func TestClassGraphIndexPrunesLargeSparseGraphByWeight(t *testing.T) {
	index := NewClassGraphIndexWithBudget([]ClassGraphEdge{
		{From: "feature.LowA", To: "feature.LowB", Count: 1},
		{From: "feature.HotA", To: "feature.HotB", Count: 50},
		{From: "feature.MidA", To: "feature.MidB", Count: 20},
		{From: "feature.ColdA", To: "feature.ColdB", Count: 2},
	}, GraphIndexBudget{MaxEdges: 2})

	if _, ok := index.Outgoing["feature.HotA"]; !ok {
		t.Fatalf("hot edge was pruned: %+v", index.Outgoing)
	}
	if _, ok := index.Outgoing["feature.MidA"]; !ok {
		t.Fatalf("mid edge was pruned: %+v", index.Outgoing)
	}
	if _, ok := index.Outgoing["feature.LowA"]; ok {
		t.Fatalf("low edge survived pruning: %+v", index.Outgoing)
	}
}

func TestClassGraphIndexBudgetsRelevantEdgesAndShortestPath(t *testing.T) {
	index := NewClassGraphIndexWithBudget([]ClassGraphEdge{
		{From: "feature.A", To: "feature.B", Count: 10},
		{From: "feature.B", To: "feature.C", Count: 8},
		{From: "feature.D", To: "feature.A", Count: 6},
		{From: "feature.E", To: "feature.A", Count: 4},
	}, GraphIndexBudget{MaxRelevantEdges: 2, MaxShortestPathVisits: 2})

	edges := index.RelevantEdges(map[string]struct{}{"feature.A": {}}, nil)
	if len(edges) != 2 {
		t.Fatalf("len(edges) = %d, want 2: %+v", len(edges), edges)
	}
	for _, edge := range edges {
		if edge.Count < 6 {
			t.Fatalf("low-priority relevant edge survived budget: %+v", edges)
		}
	}

	path := index.ShortestPath("feature.A", "feature.C", 4)
	if path != nil {
		t.Fatalf("ShortestPath should respect visit budget, got %+v", path)
	}
}

func TestClassGraphIndexSkipsCyclesWhenGraphExceedsBudget(t *testing.T) {
	index := NewClassGraphIndexWithBudget([]ClassGraphEdge{
		{From: "feature.A", To: "feature.B", Count: 1},
		{From: "feature.B", To: "feature.A", Count: 1},
		{From: "feature.C", To: "feature.D", Count: 1},
	}, GraphIndexBudget{MaxSCCNodes: 2})

	if cycles := index.StronglyConnectedComponents(8); len(cycles) != 0 {
		t.Fatalf("cycles should be skipped when SCC budget is exceeded: %+v", cycles)
	}
}

func TestMethodGraphIndexPrunesByWeight(t *testing.T) {
	index := NewMethodGraphIndexWithBudget([]ClassGraphEdge{
		{From: "feature.Low", To: "feature.Target", CallerMethod: "low()V", CalleeMethod: "load()V", Count: 1},
		{From: "feature.Hot", To: "feature.Target", CallerMethod: "hot()V", CalleeMethod: "load()V", Count: 10},
	}, GraphIndexBudget{MaxEdges: 1})

	if _, ok := index.Outgoing["feature.Hot#hot()V"]; !ok {
		t.Fatalf("hot method edge was pruned: %+v", index.Outgoing)
	}
	if _, ok := index.Outgoing["feature.Low#low()V"]; ok {
		t.Fatalf("low method edge survived pruning: %+v", index.Outgoing)
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
