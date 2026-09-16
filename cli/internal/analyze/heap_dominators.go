package analyze

// Lengauer–Tarjan, simple link/eval with path compression:
// https://doi.org/10.1145/357062.357071. One synthetic root connects every GC root.
// Dense DFS indices and CSR predecessors use O(V+E) storage; all unbounded loops
// share the analysis work budget. DFS and compression are iterative, including
// adversarial chains. The original BFS remains the shortest reference-path index.
type heapDominatorNode struct {
	slot, parent, semi, label, ancestor, dom, bucket, next uint32
	root                                                   bool
}

type heapDominatorIndex struct {
	dfs           []uint32 // heap slot -> DFS index; zero means unreachable
	size, count   []uint64
	starts, order []uint32
}

type heapDFSFrame struct{ vertex, edge uint32 }

func (p *hprofParser) dominators(budget *heapTraversalBudget, reachableCount int) *heapDominatorIndex {
	n := len(p.nodes) + 2
	// Account for zero-initializing owned arrays before allocating them.
	capacity := reachableCount + 2
	if !budget.takeN(n + 2*capacity) {
		return nil
	}
	d := &heapDominatorIndex{dfs: make([]uint32, n)}
	vertices := make([]heapDominatorNode, 2, capacity)
	discover := func(slot, parent uint32) uint32 {
		v := uint32(len(vertices))
		d.dfs[slot] = v
		vertices = append(vertices, heapDominatorNode{slot: slot, parent: parent, semi: v, label: v})
		return v
	}
	vertices[1] = heapDominatorNode{semi: 1, label: 1}
	stack := make([]heapDFSFrame, 0)
	for _, root := range p.roots {
		if !budget.take() {
			return nil
		}
		slot := p.nodeIndexes[root.id]
		if slot == 0 {
			continue
		}
		if d.dfs[slot] != 0 {
			vertices[d.dfs[slot]].root = true
			continue
		}
		v := discover(slot, 1)
		vertices[v].root = true
		stack = append(stack, heapDFSFrame{v, p.nodes[slot-1].firstEdge})
		for len(stack) > 0 {
			if !budget.take() {
				return nil
			}
			top := &stack[len(stack)-1]
			if top.edge == 0 {
				stack = stack[:len(stack)-1]
				continue
			}
			edge := p.edges[top.edge-1]
			top.edge = edge.next
			target := p.nodeIndexes[edge.to]
			if target == 0 || d.dfs[target] != 0 {
				continue
			}
			child := discover(target, top.vertex)
			stack = append(stack, heapDFSFrame{child, p.nodes[target-1].firstEdge})
		}
	}
	count := len(vertices)
	if !budget.takeN(2 * count) {
		return nil
	}
	offsets := make([]uint32, count+1)
	// Count every inspected edge, including duplicates, cycles and absent endpoints.
	// Parallel arcs do not change dominance. Reuse the not-yet-live bucket link
	// as a per-destination predecessor stamp, avoiding duplicate CSR entries.
	for v := 2; v < count; v++ {
		if !budget.take() {
			return nil
		}
		for e := p.nodes[vertices[v].slot-1].firstEdge; e != 0; {
			if !budget.take() {
				return nil
			}
			edge := p.edges[e-1]
			e = edge.next
			to := d.dfs[p.nodeIndexes[edge.to]]
			if to != 0 && vertices[to].next != uint32(v) {
				vertices[to].next = uint32(v)
				offsets[to+1]++
			}
		}
	}
	for i := 1; i < len(offsets); i++ {
		offsets[i] += offsets[i-1]
	}
	if !budget.takeN(int(offsets[count])) {
		return nil
	}
	predecessors := make([]uint32, offsets[count])
	for v := 2; v < count; v++ {
		vertices[v].bucket = offsets[v]
	}
	for v := 2; v < count; v++ {
		if !budget.take() {
			return nil
		}
		for e := p.nodes[vertices[v].slot-1].firstEdge; e != 0; {
			if !budget.take() {
				return nil
			}
			edge := p.edges[e-1]
			e = edge.next
			to := d.dfs[p.nodeIndexes[edge.to]]
			if to != 0 && vertices[to].next != uint32(v+count) {
				vertices[to].next = uint32(v + count)
				predecessors[vertices[to].bucket] = uint32(v)
				vertices[to].bucket++
			}
		}
	}
	if !budget.takeN(count) {
		return nil
	}
	for v := range vertices {
		vertices[v].bucket = 0
	}
	// Reuse the DFS stack's storage for iterative path compression.
	stack = stack[:0]
	eval := func(v uint32) (uint32, bool) {
		if !budget.take() {
			return 0, false
		}
		current := v
		for vertices[vertices[current].ancestor].ancestor != 0 {
			if !budget.take() {
				return 0, false
			}
			stack = append(stack, heapDFSFrame{vertex: current})
			current = vertices[current].ancestor
		}
		for len(stack) > 0 {
			if !budget.take() {
				return 0, false
			}
			x := stack[len(stack)-1].vertex
			stack = stack[:len(stack)-1]
			a := vertices[x].ancestor
			if vertices[vertices[a].label].semi < vertices[vertices[x].label].semi {
				vertices[x].label = vertices[a].label
			}
			vertices[x].ancestor = vertices[a].ancestor
		}
		return vertices[v].label, true
	}
	for w := uint32(count - 1); w > 1; w-- {
		if !budget.take() {
			return nil
		}
		node := &vertices[w]
		if node.root {
			node.semi = 1
		}
		for _, v := range predecessors[offsets[w]:offsets[w+1]] {
			u, ok := eval(v)
			if !ok {
				return nil
			}
			if vertices[u].semi < node.semi {
				node.semi = vertices[u].semi
			}
		}
		node.next = vertices[node.semi].bucket
		vertices[node.semi].bucket = w
		node.ancestor = node.parent
		for v := vertices[node.parent].bucket; v != 0; {
			u, ok := eval(v)
			if !ok {
				return nil
			}
			if vertices[u].semi < vertices[v].semi {
				vertices[v].dom = u
			} else {
				vertices[v].dom = node.parent
			}
			v = vertices[v].next
		}
		vertices[node.parent].bucket = 0
	}
	if !budget.takeN(9 * count) {
		return nil
	}
	d.size = make([]uint64, count)
	d.count = make([]uint64, count)
	d.starts = offsets[:count] // CSR offsets are no longer needed.
	d.order = make([]uint32, 0, count-2)
	for v := range vertices {
		vertices[v].bucket = 0
	}
	for w := 2; w < count; w++ {
		node := &vertices[w]
		if node.dom != node.semi {
			node.dom = vertices[node.dom].dom
		}
		node.next = vertices[node.dom].bucket
		vertices[node.dom].bucket = uint32(w)
		d.size[w] = p.nodes[node.slot-1].shallowSize
		d.count[w] = 1
	}
	// Every immediate dominator precedes its descendants in DFS order.
	for w := count - 1; w > 1; w-- {
		parent := vertices[w].dom
		d.size[parent] = saturatingUint64Sum(d.size[parent], d.size[w])
		d.count[parent] = saturatingUint64Sum(d.count[parent], d.count[w])
	}
	// A dominator subtree occupies one contiguous interval in this preorder.
	// Only intervals/totals survive construction; union-find and CSR scratch die here.
	for current := vertices[1].bucket; current != 0; {
		d.starts[current] = uint32(len(d.order))
		d.order = append(d.order, vertices[current].slot)
		if child := vertices[current].bucket; child != 0 {
			current = child
			continue
		}
		for current != 1 && vertices[current].next == 0 {
			current = vertices[current].dom
		}
		if current == 1 {
			break
		}
		current = vertices[current].next
	}
	return d
}

func (p *hprofParser) retainedByDominator(target uint64, d *heapDominatorIndex, budget *heapTraversalBudget) (uint64, uint64, []string, bool) {
	if d == nil {
		return p.shallowRetainedFallback(target)
	}
	v := d.dfs[p.nodeIndexes[target]]
	if v == 0 {
		return p.shallowRetainedFallback(target)
	}
	// The totals are already exact for every vertex. Class samples are supplemental
	// and independently bounded; exhaustion must not discard completed totals.
	classes := map[string]uint64{}
	for _, slot := range d.order[uint64(d.starts[v]) : uint64(d.starts[v])+d.count[v]] {
		if !budget.take() {
			p.degrade("retained-sample", "Выборка классов удерживаемого поддерева ограничена бюджетом работы HPROF.")
			return d.size[v], d.count[v], nil, true
		}
		classes[p.nodes[slot-1].className]++
	}

	sample, ok := retainedClassSampleLimited(classes, budget)
	if !ok {
		p.degrade("retained-sample", "Выборка классов удерживаемого поддерева ограничена бюджетом работы HPROF.")
	}
	return d.size[v], d.count[v], sample, true
}
