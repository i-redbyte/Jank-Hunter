package analyze

import (
	"fmt"
	"testing"
)

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

func TestClassGraphIndexCycles(t *testing.T) {
	index := NewClassGraphIndex([]ClassGraphEdge{
		{From: "feature.A", To: "feature.B", Count: 1},
		{From: "feature.B", To: "feature.C", Count: 2},
		{From: "feature.C", To: "feature.A", Count: 3},
		{From: "feature.C", To: "feature.D", Count: 4},
	})

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

func TestGraphIndexesSaturateCountsInsteadOfWrapping(t *testing.T) {
	const maximum = ^uint64(0)
	edges := []ClassGraphEdge{
		{From: "feature.A", To: "feature.Target", CallerMethod: "a()V", CalleeMethod: "load()V", Count: maximum},
		{From: "feature.A", To: "feature.Target", CallerMethod: "a()V", CalleeMethod: "load()V", Count: 1},
		{From: "feature.B", To: "feature.Target", CallerMethod: "b()V", CalleeMethod: "load()V", Count: 1},
		{From: "feature.Target", To: "feature.A", Count: maximum},
	}

	classIndex := NewClassGraphIndex(edges)
	if got := classIndex.Outgoing["feature.A"][0].Count; got != maximum {
		t.Fatalf("class edge count wrapped to %d", got)
	}
	cycles := classIndex.StronglyConnectedComponents(1)
	if len(cycles) != 1 || cycles[0].Weight != maximum {
		t.Fatalf("cycle weight did not saturate: %+v", cycles)
	}

	methodIndex := NewMethodGraphIndex(edges)
	if got := methodIndex.Outgoing["feature.A#a()V"][0].Count; got != maximum {
		t.Fatalf("method edge count wrapped to %d", got)
	}
	methods := methodIndex.HotMethods(map[string]float64{"feature.Target": 1}, nil, 10)
	for _, method := range methods {
		if method.ClassName == "feature.Target" && method.Method == "load()V" && method.Role == "callee" {
			if method.Count != maximum {
				t.Fatalf("hot method count wrapped to %d", method.Count)
			}
			return
		}
	}
	t.Fatalf("target hot method missing: %+v", methods)
}

func TestGraphIndexRetainsEdgesBeyondFormerGlobalLimit(t *testing.T) {
	const edgeCount = 150_001
	edges := make([]ClassGraphEdge, 0, edgeCount)
	for index := range edgeCount {
		edges = append(edges, ClassGraphEdge{
			From:  "feature.Source",
			To:    fmt.Sprintf("feature.Target%06d", index),
			Count: 1,
		})
	}

	index := NewClassGraphIndex(edges)
	if got := len(index.Outgoing["feature.Source"]); got != edgeCount {
		t.Fatalf("retained edges = %d, want %d", got, edgeCount)
	}
}

func TestStronglyConnectedComponentsDoesNotStopAtFormerNodeLimit(t *testing.T) {
	const nodeCount = 10_001
	edges := make([]ClassGraphEdge, 0, nodeCount)
	for index := range nodeCount {
		edges = append(edges, ClassGraphEdge{
			From:  fmt.Sprintf("feature.Node%05d", index),
			To:    fmt.Sprintf("feature.Node%05d", (index+1)%nodeCount),
			Count: 1,
		})
	}

	cycles := NewClassGraphIndex(edges).StronglyConnectedComponents(1)
	if len(cycles) != 1 {
		t.Fatalf("complete SCC count = %d, want 1", len(cycles))
	}
	if len(cycles[0].Nodes) != nodeCount || cycles[0].Weight != nodeCount {
		t.Fatalf("complete SCC was not retained: %+v", cycles[0])
	}
}

func TestHotPathSearchDoesNotStopAtFormerSourceLimit(t *testing.T) {
	const sourceCount = 81
	edges := make([]ClassGraphEdge, 0, sourceCount)
	scores := make(map[string]float64, sourceCount)
	for index := range sourceCount {
		source := fmt.Sprintf("feature.Source%02d", index)
		target := fmt.Sprintf("feature.Leaf%02d", index)
		if index == sourceCount-1 {
			target = "feature.RuntimeTarget"
		}
		edges = append(edges, ClassGraphEdge{From: source, To: target, Count: 1})
		scores[source] = float64(sourceCount - index)
	}

	paths := NewClassGraphIndex(edges).HotPaths(
		scores,
		map[string]struct{}{"feature.RuntimeTarget": {}},
		1,
	)
	if len(paths) != 1 || !paths[0].RuntimeTarget || paths[0].Nodes[1] != "feature.RuntimeTarget" {
		t.Fatalf("late source runtime path was dropped: %+v", paths)
	}
}

func TestDefaultHotPathSearchDoesNotStopAtFormerExplorationLimit(t *testing.T) {
	const branches = 2_100
	edges := make([]ClassGraphEdge, 0, branches*2)
	for branch := 0; branch < branches; branch++ {
		middle := fmt.Sprintf("feature.Middle%04d", branch)
		edges = append(edges, ClassGraphEdge{From: "feature.Source", To: middle, Count: 1})
		if branch == branches-1 {
			edges = append(edges, ClassGraphEdge{From: middle, To: "feature.RuntimeTarget", Count: 1})
		} else {
			edges = append(edges, ClassGraphEdge{From: middle, To: fmt.Sprintf("feature.Leaf%04d", branch), Count: 1})
		}
	}

	paths := NewClassGraphIndex(edges).HotPaths(
		map[string]float64{"feature.Source": 10},
		map[string]struct{}{"feature.RuntimeTarget": {}},
		8,
	)
	if len(paths) != 1 || !paths[0].RuntimeTarget || paths[0].Nodes[len(paths[0].Nodes)-1] != "feature.RuntimeTarget" {
		t.Fatalf("full hot-path search missed late runtime target: %+v", paths)
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
