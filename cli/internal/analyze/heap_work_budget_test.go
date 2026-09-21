package analyze

import "testing"

func heapBudgetGraph(extraEdges, extraNodes int) *hprofParser {
	p := newHprofParser("budget.hprof", map[string]struct{}{"app.Target": {}})
	p.ensureNode(1, "app.Root", 16)
	p.ensureNode(2, "app.Target", 32)
	p.ensureNode(3, "app.Child", 64)
	p.roots = []heapRoot{{id: 1, kind: "sticky class"}}
	p.addEdge(p.nodeByID(1), 2, "target", "field")
	p.addEdge(p.nodeByID(1), 3, "child", "field")
	for i := 0; i < extraEdges; i++ {
		p.addEdge(p.nodeByID(3), 3, "duplicate", "field")
	}
	for i := 0; i < extraNodes; i++ {
		p.ensureNode(uint64(100+i), "app.Unreachable", 8)
	}
	return p
}
func TestRetainedWorkChargesDuplicateEdges(t *testing.T) {
	p := heapBudgetGraph(100000, 0)
	budget := &heapTraversalBudget{remaining: 3}
	size, count, _, exact := p.retainedSizeForLimited(2, p.rootIDs(), p.rootBFS(), newHeapReachabilityScratch(len(p.nodes)), budget)
	if exact || !budget.exhausted || size != 32 || count != 1 {
		t.Fatalf("duplicate edges bypassed work limit: exact=%v exhausted=%v fallback=%d/%d", exact, budget.exhausted, size, count)
	}
}
func TestRetainedWorkChargesFullNodeScan(t *testing.T) {
	p := heapBudgetGraph(0, 10000)
	budget := &heapTraversalBudget{remaining: 3}
	_, _, _, exact := p.retainedSizeForLimited(1, p.rootIDs(), p.rootBFS(), newHeapReachabilityScratch(len(p.nodes)), budget)
	if exact || !budget.exhausted {
		t.Fatalf("full node scan bypassed work limit: exact=%v exhausted=%v", exact, budget.exhausted)
	}
}
func TestRetainedWorkChargesRepeatedAndMissingRoots(t *testing.T) {
	p := heapBudgetGraph(0, 0)
	start := make([]uint64, 10000)
	start[0] = 1
	budget := &heapTraversalBudget{remaining: 10}
	if p.markReachableFromLimited(start, 2, newHeapReachabilityScratch(len(p.nodes)), budget) || !budget.exhausted {
		t.Fatal("repeated or missing roots bypassed work limit")
	}
}
func TestHeapManySmallTargetsKeepExactSizes(t *testing.T) {
	p := newHprofParser("many.hprof", map[string]struct{}{"app.Target": {}})
	p.ensureNode(1, "app.Root", 16)
	p.roots = []heapRoot{{id: 1, kind: "sticky class"}}
	for i := 0; i < 600; i++ {
		id := uint64(i + 2)
		p.ensureNode(id, "app.Target", uint64(32+i)*1024*1024)
		p.addEdge(p.nodeByID(1), id, "target", "field")
	}
	e := p.evidence()
	if warningContains(e.Warnings, "Точный удержанный размер ограничен") {
		t.Fatalf("small graph unnecessarily loses exactness across targets: %v", e.Warnings)
	}
}
func BenchmarkHeapRetainedWork(b *testing.B) {
	for _, scenario := range []struct {
		name                       string
		targets, nodes, duplicates int
	}{{"single", 1, 2000, 0}, {"four", 4, 2000, 0}, {"five", 5, 2000, 0}, {"many", 512, 2000, 0}, {"duplicates", 512, 3, 100000}} {
		b.Run(scenario.name, func(b *testing.B) {
			p := heapBudgetGraph(scenario.duplicates, scenario.nodes)
			for i := 1; i < scenario.targets; i++ {
				id := uint64(100000 + i)
				p.ensureNode(id, "app.Target", 32)
				p.addEdge(p.nodeByID(1), id, "target", "field")
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if len(p.evidence().Leaks) != 1 {
					b.Fatal("missing evidence")
				}
			}
		})
	}
}

func TestRetainedSampleLimitPreservesComputedTotals(t *testing.T) {
	p := heapBudgetGraph(0, 0)
	budget := &heapTraversalBudget{remaining: 4} // three-node comparison and one blocked root
	size, count, sample, exact := p.retainedSizeForLimited(1, p.rootIDs(), p.rootBFS(), newHeapReachabilityScratch(len(p.nodes)), budget)
	if !exact || size != 112 || count != 3 || len(sample) != 0 || !budget.exhausted || !p.hasDegradation("retained-sample") {
		t.Fatalf("completed totals discarded on sample limit: %d/%d exact=%v sample=%v", size, count, exact, sample)
	}
}

func TestHeapIncomingAndAlternativeWorkCannotRestartBudget(t *testing.T) {
	p := heapBudgetGraph(10000, 0)
	budget := &heapTraversalBudget{remaining: 5}
	if p.incomingEdges(map[uint64]struct{}{3: {}}, budget) != nil || !budget.exhausted {
		t.Fatal("incoming duplicate edges bypass budget")
	}
	parent := p.rootBFS()
	incoming := map[uint64][]heapIncomingEdge{2: {{from: 3, edge: heapEdge{to: 2, label: "other", kind: "field"}}}}
	if paths := p.alternativeReferencePaths(parent, incoming, 2, p.referencePath(parent, 2), budget); len(paths) != 0 || budget.remaining != 0 {
		t.Fatal("alternative paths restarted exhausted work")
	}
}

func BenchmarkHeapRetainedScale(b *testing.B) { benchmarkHeapRetainedScale(b, 200000) }

func BenchmarkHeapRetainedParserLimit(b *testing.B) { benchmarkHeapRetainedScale(b, 2000000) }

func benchmarkHeapRetainedScale(b *testing.B, nodes int) {
	p := heapScaleGraph(nodes)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e := p.evidence()
		if len(e.Leaks) != 1 {
			b.Fatal("missing evidence")
		}
		if warningContains(e.Warnings, "Точный удержанный размер ограничен") {
			b.ReportMetric(1, "retained-limit")
		} else {
			b.ReportMetric(0, "retained-limit")
		}
	}
}

func heapScaleGraph(nodes int) *hprofParser {
	p := newHprofParser("scale.hprof", map[string]struct{}{"app.Target": {}})
	for i := 1; i <= nodes; i++ {
		class := "app.Node"
		if i > 1 && i <= 513 {
			class = "app.Target"
		}
		p.ensureNode(uint64(i), class, 32)
	}
	p.roots = []heapRoot{{id: 1, kind: "sticky class"}}
	for i := 1; i <= nodes; i++ {
		p.addEdge(p.nodeByID(uint64(i)), uint64(i%nodes+1), "next", "field")
		p.addEdge(p.nodeByID(uint64(i)), uint64((i+17)%nodes+1), "jump", "field")
	}
	return p
}

func TestHeapEdgeInspectionBudgetCountsRejectedEndpoints(t *testing.T) {
	for _, to := range []uint64{2, 3, 99999} {
		p := heapBudgetGraph(0, 0)
		for i := 0; i < 10000; i++ {
			p.addEdge(p.nodeByID(3), to, "duplicate", "field")
		}
		budget := &heapTraversalBudget{remaining: 64}
		if p.markReachableFromLimited(p.rootIDs(), 2, newHeapReachabilityScratch(len(p.nodes)), budget) || !budget.exhausted {
			t.Fatalf("endpoint %d bypasses work budget", to)
		}
	}
}
