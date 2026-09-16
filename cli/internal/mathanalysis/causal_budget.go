package mathanalysis

import "unsafe"

// Graph storage shares the collectors' quota. Working maps and path scratch
// are released when construction ends; returned arrays and strings remain owned.
func buildCausalGraphWithBudget(timeline []TimelineBucket, loops []NetworkLoopFinding, markov MarkovModel, budget *collectionBudget) (graph CausalGraph) {
	b := newCausalGraphBuilderWithBudget(budget)
	defer b.work.close()
	defer func() {
		if !b.work.canReserve(0) {
			b.results.close()
			graph = CausalGraph{}
		}
	}()
	b.addTimeline(timeline, markov)
	b.addNetworkLoops(loops)
	if !b.work.canReserve(0) {
		return CausalGraph{}
	}
	nodes, edges := b.nodeList(), b.edgeList()
	// Dijkstra: node/adjacency/distance/predecessor/visited maps, source and
	// target arrays, growing adjacency slices and at most E+1 heap insertions.
	// The bound covers simultaneous old/new backing arrays during growth.
	scratch := b.work.scratch("causal path workspace")
	defer scratch.close()
	if !scratch.reserve(16*mathMapBaseBytes) ||
		!scratch.reserveItems(len(nodes), 2048) ||
		!scratch.reserveItems(len(edges), 4*uint64(unsafe.Sizeof(CausalEdge{}))+4*uint64(unsafe.Sizeof(causalPathQueueItem{}))) {
		return CausalGraph{}
	}
	if len(nodes) <= maxFloydWarshallNodes && !scratch.reserveItems(len(nodes)*len(nodes), 16) {
		return CausalGraph{}
	}
	// Both algorithms retain at most six paths, each of at most V labels.
	pathsAccount := b.results.scratch("causal path results")
	if !pathsAccount.reserveItems(12, uint64(unsafe.Sizeof(GraphPath{}))+uint64(len(nodes))*16) ||
		!b.results.reserveItems(len(nodes), uint64(unsafe.Sizeof(OwnerBlameScore{}))) {
		pathsAccount.close()
		return CausalGraph{}
	}
	graph = CausalGraph{Nodes: nodes, Edges: edges, Paths: causalShortestPaths(nodes, edges),
		AllPairs: floydWarshallGraphPaths(nodes, edges, 6), OwnerScores: causalOwnerScores(nodes, edges, loops)}
	if pathsAccount != nil {
		actual := uint64(cap(graph.Paths)+cap(graph.AllPairs)) * uint64(unsafe.Sizeof(GraphPath{}))
		for _, paths := range [][]GraphPath{graph.Paths, graph.AllPairs} {
			for _, path := range paths {
				actual += uint64(cap(path.Nodes)) * 16
			}
		}
		pathsAccount.release(pathsAccount.reserved - actual)
	}
	return graph
}

func newCausalGraphBuilderWithBudget(budget *collectionBudget) *causalGraphBuilder {
	b := &causalGraphBuilder{}
	if budget != nil {
		b.work, b.results = budget.account("causal graph maps"), budget.account("causal graph results")
	}
	if b.work.reserve(2 * mathMapBaseBytes) {
		b.nodes = make(map[string]CausalNode)
		b.edges = make(map[causalEdgeKey]*causalEdgeAgg)
	}
	return b
}

func compareCausalGraphsWithBudget(baseline, candidate CausalGraph, budget *collectionBudget) []CausalDelta {
	work, results := budget.account("causal comparison workspace"), budget.account("causal comparison results")
	defer work.close()
	// At most twelve summaries survive selection. Reserve the largest possible
	// formatted summary before construction, including a temporary candidate.
	summaryBytes := uint64(1024)
	pathBytes := uint64(1024)
	for _, graph := range []CausalGraph{baseline, candidate} {
		for _, edge := range graph.Edges {
			summaryBytes = max(summaryBytes, uint64(len(edge.FromLabel)+len(edge.ToLabel))+1024)
		}
		if len(graph.Paths) > 0 {
			for _, label := range graph.Paths[0].Nodes {
				pathBytes += uint64(len(label)) + 4
			}
		}
	}
	summaryBytes = max(summaryBytes, pathBytes)
	if !work.reserve(8*mathMapBaseBytes+4*summaryBytes) ||
		!work.reserveItems(len(baseline.Edges)+len(candidate.Edges), mathMapEntryBytes+uint64(unsafe.Sizeof(CausalEdge{}))+uint64(unsafe.Sizeof(causalEdgeKey{}))) ||
		!results.reserveItems(12, uint64(unsafe.Sizeof(CausalDelta{}))+summaryBytes) {
		results.close()
		return nil
	}
	deltas := compareCausalGraphs(baseline, candidate)
	actual := uint64(cap(deltas)) * uint64(unsafe.Sizeof(CausalDelta{}))
	for _, delta := range deltas {
		actual += uint64(len(delta.Summary))
	}
	results.release(results.reserved - actual)
	return deltas
}
