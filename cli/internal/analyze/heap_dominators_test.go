package analyze

import (
	"fmt"
	"math/rand"
	"reflect"
	"testing"
)

// Independent set-reachability oracle: remove one object and compare reachable
// sets. It uses object IDs/maps and raw edge lists, no DFS/dominator metadata.
func retainedOracle(p *hprofParser, blocked uint64) (uint64, uint64) {
	reach := func(skip uint64) map[uint64]bool {
		seen := map[uint64]bool{}
		queue := []uint64{}
		for _, root := range p.roots {
			if root.id != skip && p.nodeByID(root.id) != nil && !seen[root.id] {
				seen[root.id] = true
				queue = append(queue, root.id)
			}
		}
		for len(queue) > 0 {
			id := queue[0]
			queue = queue[1:]
			node := p.nodeByID(id)
			for slot := node.firstEdge; slot != 0; {
				edge := p.edges[slot-1]
				slot = edge.next
				if edge.to != skip && p.nodeByID(edge.to) != nil && !seen[edge.to] {
					seen[edge.to] = true
					queue = append(queue, edge.to)
				}
			}
		}
		return seen
	}
	all, without := reach(0), reach(blocked)
	var size, count uint64
	for id := range all {
		if !without[id] {
			size = saturatingUint64Sum(size, p.nodeByID(id).shallowSize)
			count++
		}
	}
	return size, count
}

func TestHeapDominatorsMatchIndependentOracle(t *testing.T) {
	for seed := int64(0); seed < 512; seed++ {
		random := rand.New(rand.NewSource(seed))
		p := newHprofParser("oracle.hprof", nil)
		n := 2 + random.Intn(35)
		for i := 1; i <= n; i++ {
			p.ensureNode(uint64(i), fmt.Sprintf("app.C%d", i%7), uint64(random.Intn(10000)))
		}
		p.roots = []heapRoot{{id: 1, kind: "sticky class"}}
		if seed%3 == 0 {
			p.roots = append(p.roots, heapRoot{id: uint64(n / 2), kind: "jni global"}, heapRoot{id: 1, kind: "duplicate"}, heapRoot{id: 99999, kind: "missing"})
		}
		if seed%4 == 0 {
			for i := 2; i <= n; i++ {
				p.addEdge(p.nodeByID(uint64(1+random.Intn(i-1))), uint64(i), "tree", "field")
			}
		}
		edgeCount := random.Intn(n * 5)
		for i := 0; i < edgeCount; i++ {
			from, to := uint64(1+random.Intn(n)), uint64(1+random.Intn(n+3))
			p.addEdge(p.nodeByID(from), to, "ref", "field")
		}
		budget := &heapTraversalBudget{remaining: 1000000}
		d := p.dominators(budget, p.rootBFS().reachable)
		if d == nil {
			t.Fatalf("seed %d exhausted unexpected budget", seed)
		}
		for id := uint64(1); id <= uint64(n); id++ {
			size, count := retainedOracle(p, id)
			v := d.dfs[p.nodeIndexes[id]]
			if v == 0 {
				if count != 0 {
					t.Fatalf("seed %d object %d unexpectedly unreachable", seed, id)
				}
				continue
			}
			if d.size[v] != size || d.count[v] != count {
				t.Fatalf("seed %d object %d got %d/%d want %d/%d", seed, id, d.size[v], d.count[v], size, count)
			}
			_, _, sample, exact := p.retainedByDominator(id, d, budget)
			_, _, wantSample, wantExact := p.retainedSizeForLimited(id, p.rootIDs(), p.rootBFS(), newHeapReachabilityScratch(len(p.nodes)), budget)
			if !exact || !wantExact || !reflect.DeepEqual(sample, wantSample) {
				t.Fatalf("seed %d object %d subtree interval differs: %v vs %v", seed, id, sample, wantSample)
			}
		}
	}
}

func TestHeapDominatorWorkExhaustionIsTerminal(t *testing.T) {
	p := heapBudgetGraph(100, 40)
	full := &heapTraversalBudget{remaining: 1000000}
	if p.dominators(full, p.rootBFS().reachable) == nil {
		t.Fatal("full index unavailable")
	}
	used := 1000000 - full.remaining
	for quota := 0; quota <= used; quota++ {
		budget := &heapTraversalBudget{remaining: quota}
		d := p.dominators(budget, p.rootBFS().reachable)
		if quota < used {
			if d != nil || !budget.exhausted || budget.remaining != 0 {
				t.Fatalf("quota %d/%d returned partial index", quota, used)
			}
			if p.dominators(budget, p.rootBFS().reachable) != nil {
				t.Fatal("exhausted budget restarted")
			}
		} else if d == nil || budget.exhausted {
			t.Fatal("exact admission rejected")
		}
	}
}

func TestHeapDominatorDeepChainAndOverflow(t *testing.T) {
	const n = 20000
	p := newHprofParser("chain.hprof", nil)
	for i := 1; i <= n; i++ {
		p.ensureNode(uint64(i), "app.Node", 16)
	}
	p.nodeByID(n).shallowSize = ^uint64(0)
	p.roots = []heapRoot{{id: 1, kind: "sticky class"}}
	for i := 1; i < n; i++ {
		p.addEdge(p.nodeByID(uint64(i)), uint64(i+1), "next", "field")
	}
	d := p.dominators(&heapTraversalBudget{remaining: maxHprofRetainedWork}, p.rootBFS().reachable)
	if d == nil || d.size[d.dfs[1]] != ^uint64(0) || d.count[d.dfs[1]] != n {
		t.Fatal("deep chain totals missing or wrapped")
	}
}

func TestHeapDominatorSharedSamplesDoNotInvalidateCompletedTotals(t *testing.T) {
	p := heapBudgetGraph(0, 0)
	d := p.dominators(&heapTraversalBudget{remaining: 10000}, p.rootBFS().reachable)
	budget := &heapTraversalBudget{remaining: 0}
	for _, id := range []uint64{1, 2, 3} {
		size, count, sample, exact := p.retainedByDominator(id, d, budget)
		wantSize, wantCount := retainedOracle(p, id)
		if !exact || size != wantSize || count != wantCount || len(sample) != 0 {
			t.Fatalf("completed totals invalidated for %d", id)
		}
	}
	if !budget.exhausted || !p.hasDegradation("retained-sample") {
		t.Fatal("sample exhaustion hidden")
	}
}
