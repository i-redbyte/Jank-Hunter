package analyze

import (
	"fmt"
	"sort"
)

type heapTraversalBudget struct {
	remaining int
	exhausted bool
}

type heapReachabilityScratch struct {
	marks      map[uint64]uint32
	queue      []uint64
	generation uint32
}

type heapParent struct {
	from   uint64
	root   string
	edge   heapEdge
	hasAny bool
}

func (p *hprofParser) rootBFS() map[uint64]heapParent {
	parent := map[uint64]heapParent{}
	queue := make([]uint64, 0, len(p.roots))
	for _, root := range p.roots {
		if root.id == 0 {
			continue
		}
		if _, ok := parent[root.id]; ok {
			continue
		}
		parent[root.id] = heapParent{root: root.kind, hasAny: true}
		queue = append(queue, root.id)
	}
	for head := 0; head < len(queue); head++ {
		id := queue[head]
		node := p.nodes[id]
		if node == nil {
			continue
		}
		for _, edge := range node.edges {
			if edge.to == 0 {
				continue
			}
			if _, ok := parent[edge.to]; ok {
				continue
			}
			parent[edge.to] = heapParent{from: id, edge: edge, hasAny: true}
			queue = append(queue, edge.to)
		}
	}
	return parent
}

func (p *hprofParser) targetNodes(parent map[uint64]heapParent) map[string][]uint64 {
	out := map[string][]uint64{}
	for id, node := range p.nodes {
		if node == nil {
			continue
		}
		if _, isClassObject := p.classes[id]; isClassObject {
			continue
		}
		if _, ok := p.targets[node.className]; !ok {
			continue
		}
		if _, reachable := parent[id]; !reachable {
			continue
		}
		out[node.className] = append(out[node.className], id)
	}
	return out
}

func (p *hprofParser) retainedSizeForLimited(
	target uint64,
	rootIDs []uint64,
	rootReachability map[uint64]heapParent,
	scratch *heapReachabilityScratch,
	budget *heapTraversalBudget,
	allowExact bool,
) (uint64, uint64, []string, bool) {
	if !allowExact || scratch == nil || budget == nil || budget.exhausted {
		return p.shallowRetainedFallback(target)
	}
	if !p.markReachableFromLimited(rootIDs, target, scratch, budget) {
		return p.shallowRetainedFallback(target)
	}
	var size uint64
	var count uint64
	classes := map[string]uint64{}
	// Every normally reachable node that becomes unreachable after removing target is dominated by
	// target. Reusing the initial root traversal avoids a second full graph walk for every suspect.
	for id := range rootReachability {
		if scratch.contains(id) {
			continue
		}
		node := p.nodes[id]
		if node == nil {
			continue
		}
		count = saturatingUint64Sum(count, 1)
		if node.shallowSize > 0 {
			size = saturatingUint64Sum(size, node.shallowSize)
		}
		classes[node.className] = saturatingUint64Sum(classes[node.className], 1)
	}
	if size == 0 {
		if node := p.nodes[target]; node != nil && node.shallowSize > 0 {
			size = node.shallowSize
		}
	}
	return size, count, retainedClassSample(classes), true
}

func (p *hprofParser) shallowRetainedFallback(target uint64) (uint64, uint64, []string, bool) {
	node := p.nodes[target]
	if node == nil {
		return 0, 0, nil, false
	}
	classes := map[string]uint64{node.className: 1}
	return node.shallowSize, 1, retainedClassSample(classes), false
}

func newHeapReachabilityScratch(capacity int) *heapReachabilityScratch {
	return &heapReachabilityScratch{
		marks: make(map[uint64]uint32, capacity),
		queue: make([]uint64, 0, capacity),
	}
}

func (s *heapReachabilityScratch) reset() {
	s.generation++
	if s.generation == 0 {
		clear(s.marks)
		s.generation = 1
	}
	s.queue = s.queue[:0]
}

func (s *heapReachabilityScratch) contains(id uint64) bool {
	return s.marks[id] == s.generation
}

func (s *heapReachabilityScratch) add(id uint64) {
	s.marks[id] = s.generation
	s.queue = append(s.queue, id)
}

func (p *hprofParser) markReachableFromLimited(
	start []uint64,
	blocked uint64,
	scratch *heapReachabilityScratch,
	budget *heapTraversalBudget,
) bool {
	scratch.reset()
	for _, id := range start {
		if id == 0 || id == blocked || scratch.contains(id) {
			continue
		}
		if !budget.take() {
			return false
		}
		scratch.add(id)
	}
	for head := 0; head < len(scratch.queue); head++ {
		node := p.nodes[scratch.queue[head]]
		if node == nil {
			continue
		}
		for _, edge := range node.edges {
			if edge.to == 0 || edge.to == blocked || scratch.contains(edge.to) {
				continue
			}
			if !budget.take() {
				return false
			}
			scratch.add(edge.to)
		}
	}
	return true
}

func (b *heapTraversalBudget) take() bool {
	if b.remaining <= 0 {
		b.exhausted = true
		return false
	}
	b.remaining--
	return true
}

func (p *hprofParser) rootIDs() []uint64 {
	ids := make([]uint64, 0, len(p.roots))
	for _, root := range p.roots {
		ids = append(ids, root.id)
	}
	return ids
}

func (p *hprofParser) incomingEdges() map[uint64][]heapIncomingEdge {
	out := map[uint64][]heapIncomingEdge{}
	for from, node := range p.nodes {
		if node == nil {
			continue
		}
		for _, edge := range node.edges {
			if edge.to == 0 {
				continue
			}
			out[edge.to] = append(out[edge.to], heapIncomingEdge{from: from, edge: edge})
		}
	}
	for id := range out {
		sort.Slice(out[id], func(i, j int) bool {
			left := out[id][i]
			right := out[id][j]
			if left.edge.kind == right.edge.kind {
				if left.edge.label == right.edge.label {
					return p.nodeClassName(left.from) < p.nodeClassName(right.from)
				}
				return left.edge.label < right.edge.label
			}
			return left.edge.kind < right.edge.kind
		})
	}
	return out
}

func (p *hprofParser) referencePath(parent map[uint64]heapParent, target uint64) []HeapPathElement {
	var reversed []HeapPathElement
	current := target
	for i := 0; i < maxHprofPathElements; i++ {
		step, ok := parent[current]
		if !ok || !step.hasAny {
			break
		}
		node := p.nodes[current]
		className := ""
		if node != nil {
			className = node.className
		}
		if step.from == 0 {
			if className != "" {
				reversed = append(reversed, HeapPathElement{
					ClassName: className,
					ObjectID:  fmt.Sprintf("0x%x", current),
					Kind:      "root_object",
				})
			}
			reversed = append(reversed, HeapPathElement{
				ClassName: "GC root: " + step.root,
				ObjectID:  fmt.Sprintf("0x%x", current),
				Kind:      "gc_root",
			})
			break
		}
		reversed = append(reversed, HeapPathElement{
			ClassName: className,
			FieldName: step.edge.label,
			ObjectID:  fmt.Sprintf("0x%x", current),
			Kind:      step.edge.kind,
		})
		current = step.from
	}
	if step, ok := parent[current]; ok && step.hasAny && step.from != 0 && len(reversed) >= maxHprofPathElements {
		p.degrade("reference-path-depth", fmt.Sprintf(
			"Цепочка HPROF превысила лимит глубины %d: показан только ограниченный фрагмент пути.",
			maxHprofPathElements,
		))
	}
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	return reversed
}

func (p *hprofParser) alternativeReferencePaths(
	parent map[uint64]heapParent,
	incoming map[uint64][]heapIncomingEdge,
	target uint64,
	primary []HeapPathElement,
) [][]HeapPathElement {
	primaryIDs := heapPathNodeIDs(parent, target)
	if len(primaryIDs) < 2 {
		return nil
	}
	seenFingerprints := map[string]struct{}{}
	if fp := pathFingerprint(primary); fp != "" {
		seenFingerprints[fp] = struct{}{}
	}
	out := make([][]HeapPathElement, 0, maxHprofAlternativePaths)
	for mergeIndex := len(primaryIDs) - 1; mergeIndex > 0 && len(out) < maxHprofAlternativePaths; mergeIndex-- {
		mergeID := primaryIDs[mergeIndex]
		primaryPredecessor := primaryIDs[mergeIndex-1]
		for _, incomingEdge := range incoming[mergeID] {
			if incomingEdge.from == 0 || incomingEdge.from == primaryPredecessor {
				continue
			}
			prefixIDs := heapPathNodeIDs(parent, incomingEdge.from)
			if len(prefixIDs) == 0 || pathsIntersect(prefixIDs, primaryIDs[mergeIndex:]) {
				continue
			}
			path := p.referencePath(parent, incomingEdge.from)
			remaining := len(primaryIDs) - mergeIndex
			if len(path)+remaining > maxHprofPathElements+1 {
				continue
			}
			path = append(path, p.pathElement(mergeID, incomingEdge.edge))
			for suffixIndex := mergeIndex + 1; suffixIndex < len(primaryIDs); suffixIndex++ {
				suffixID := primaryIDs[suffixIndex]
				path = append(path, p.pathElement(suffixID, parent[suffixID].edge))
			}
			fingerprint := pathFingerprint(path)
			if fingerprint == "" {
				continue
			}
			if _, exists := seenFingerprints[fingerprint]; exists {
				continue
			}
			seenFingerprints[fingerprint] = struct{}{}
			out = append(out, path)
			if len(out) == maxHprofAlternativePaths {
				break
			}
		}
	}
	return out
}

func heapPathNodeIDs(parent map[uint64]heapParent, target uint64) []uint64 {
	if step, ok := parent[target]; !ok || !step.hasAny {
		return nil
	}
	reversed := make([]uint64, 0, maxHprofPathElements)
	current := target
	reachedRoot := false
	for len(reversed) < maxHprofPathElements {
		step, ok := parent[current]
		if !ok || !step.hasAny {
			return nil
		}
		reversed = append(reversed, current)
		if step.from == 0 {
			reachedRoot = true
			break
		}
		current = step.from
	}
	if !reachedRoot {
		return nil
	}
	for left, right := 0, len(reversed)-1; left < right; left, right = left+1, right-1 {
		reversed[left], reversed[right] = reversed[right], reversed[left]
	}
	return reversed
}

func pathsIntersect(left, right []uint64) bool {
	for _, leftID := range left {
		for _, rightID := range right {
			if leftID == rightID {
				return true
			}
		}
	}
	return false
}

func (p *hprofParser) pathElement(id uint64, edge heapEdge) HeapPathElement {
	return HeapPathElement{
		ClassName: p.nodeClassName(id),
		FieldName: edge.label,
		ObjectID:  fmt.Sprintf("0x%x", id),
		Kind:      edge.kind,
	}
}

func (p *hprofParser) nodeClassName(id uint64) string {
	if node := p.nodes[id]; node != nil {
		return node.className
	}
	return ""
}
