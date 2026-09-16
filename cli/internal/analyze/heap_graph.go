package analyze

import (
	"fmt"
	"math/bits"
	"sort"
)

type heapTraversalBudget struct {
	remaining int
	exhausted bool
}

type heapReachabilityScratch struct {
	marks      []uint32
	queue      []uint32
	generation uint32
}

type heapParent struct {
	from      uint64
	edgeSlot  uint32
	rootIndex uint32 // one-based index in p.roots; zero means not reached
}

type heapParentIndex struct {
	reachable int
	entries   []heapParent
}

func (p *hprofParser) rootBFS() *heapParentIndex {
	parent := &heapParentIndex{entries: make([]heapParent, len(p.nodes))}
	queue := make([]uint32, 0, len(p.roots))
	for rootIndex, root := range p.roots {
		slot := p.nodeIndexes[root.id]
		if slot == 0 {
			continue
		}
		entry := &parent.entries[slot-1]
		if entry.rootIndex != 0 {
			continue
		}
		*entry = heapParent{rootIndex: uint32(rootIndex + 1)}
		queue = append(queue, slot)
	}
	for head := 0; head < len(queue); head++ {
		nodeSlot := queue[head]
		node := &p.nodes[nodeSlot-1]
		rootIndex := parent.entries[nodeSlot-1].rootIndex
		for slot := node.firstEdge; slot != 0; {
			edge := p.edges[slot-1]
			next := edge.next
			targetSlot := p.nodeIndexes[edge.to]
			if targetSlot == 0 {
				slot = next
				continue
			}
			entry := &parent.entries[targetSlot-1]
			if entry.rootIndex != 0 {
				slot = next
				continue
			}
			*entry = heapParent{from: node.id, edgeSlot: slot, rootIndex: rootIndex}
			queue = append(queue, targetSlot)
			slot = next
		}
	}
	parent.reachable = len(queue)
	return parent
}

func (p *hprofParser) parentFor(index *heapParentIndex, id uint64) (heapParent, bool) {
	if index == nil {
		return heapParent{}, false
	}
	slot := p.nodeIndexes[id]
	if slot == 0 {
		return heapParent{}, false
	}
	entry := index.entries[slot-1]
	return entry, entry.rootIndex != 0
}

func (p *hprofParser) pathRoot(parent *heapParentIndex, target uint64) (heapRoot, bool) {
	entry, ok := p.parentFor(parent, target)
	if !ok || uint64(entry.rootIndex) > uint64(len(p.roots)) {
		return heapRoot{}, false
	}
	return p.roots[entry.rootIndex-1], true
}

func (p *hprofParser) parentEdge(parent heapParent) heapEdge {
	if parent.edgeSlot == 0 {
		return heapEdge{}
	}
	edge, _ := p.edgeAt(parent.edgeSlot)
	return edge
}

func (p *hprofParser) targetNodes() map[string][]uint64 {
	out := map[string][]uint64{}
	for index := range p.nodes {
		node := &p.nodes[index]
		if _, isClassObject := p.classes[node.id]; isClassObject {
			continue
		}
		if _, ok := p.targets[node.className]; !ok {
			continue
		}
		out[node.className] = append(out[node.className], node.id)
	}
	return out
}

func (p *hprofParser) retainedSizeForLimited(
	target uint64,
	rootIDs []uint64,
	rootReachability *heapParentIndex,
	scratch *heapReachabilityScratch,
	budget *heapTraversalBudget,
) (uint64, uint64, []string, bool) {
	if scratch == nil || budget == nil || rootReachability == nil || budget.exhausted {
		return p.shallowRetainedFallback(target)
	}
	// Reserve the complete comparison scan even when almost every object is unreachable.
	if !budget.takeN(len(rootReachability.entries)) || !p.markReachableFromLimited(rootIDs, target, scratch, budget) {
		return p.shallowRetainedFallback(target)
	}
	var size uint64
	var count uint64
	classes := map[string]uint64{}
	// Every normally reachable node that becomes unreachable after removing target is dominated by
	// target. Reusing the initial root traversal avoids a second full graph walk for every suspect.
	for index, parent := range rootReachability.entries {
		if parent.rootIndex == 0 || scratch.contains(uint32(index+1)) {
			continue
		}
		node := &p.nodes[index]
		count = saturatingUint64Sum(count, 1)
		if node.shallowSize > 0 {
			size = saturatingUint64Sum(size, node.shallowSize)
		}
		classes[node.className] = saturatingUint64Sum(classes[node.className], 1)
	}
	if size == 0 {
		if node := p.nodeByID(target); node != nil && node.shallowSize > 0 {
			size = node.shallowSize
		}
	}
	sample, ok := retainedClassSampleLimited(classes, budget)
	if !ok {
		p.degrade("retained-sample", "Выборка классов удерживаемого поддерева ограничена бюджетом работы HPROF.")
	}
	return size, count, sample, true
}

func (p *hprofParser) shallowRetainedFallback(target uint64) (uint64, uint64, []string, bool) {
	node := p.nodeByID(target)
	if node == nil {
		return 0, 0, nil, false
	}
	classes := map[string]uint64{node.className: 1}
	return node.shallowSize, 1, retainedClassSample(classes), false
}

func newHeapReachabilityScratch(capacity int) *heapReachabilityScratch {
	return &heapReachabilityScratch{
		marks: make([]uint32, capacity),
		queue: make([]uint32, 0, capacity),
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

func (s *heapReachabilityScratch) contains(slot uint32) bool {
	return slot > 0 && int(slot) <= len(s.marks) && s.marks[slot-1] == s.generation
}

func (s *heapReachabilityScratch) add(slot uint32) {
	s.marks[slot-1] = s.generation
	s.queue = append(s.queue, slot)
}

func (p *hprofParser) markReachableFromLimited(
	start []uint64,
	blocked uint64,
	scratch *heapReachabilityScratch,
	budget *heapTraversalBudget,
) bool {
	if scratch.generation == ^uint32(0) && !budget.takeN(len(scratch.marks)) {
		return false
	}
	scratch.reset()
	blockedSlot := p.nodeIndexes[blocked]
	for _, id := range start {
		if !budget.take() {
			return false
		}
		slot := p.nodeIndexes[id]
		if slot == 0 || slot == blockedSlot || scratch.contains(slot) {
			continue
		}
		scratch.add(slot)
	}
	for head := 0; head < len(scratch.queue); head++ {
		if !budget.take() {
			return false
		}
		node := &p.nodes[scratch.queue[head]-1]
		for slot := node.firstEdge; slot != 0; {
			if !budget.take() {
				return false
			}
			edge := p.edges[slot-1]
			slot = edge.next
			targetSlot := p.nodeIndexes[edge.to]
			if targetSlot == 0 || targetSlot == blockedSlot || scratch.contains(targetSlot) {
				continue
			}
			scratch.add(targetSlot)
		}
	}
	return true
}

// Work units are graph entries/comparisons, not CPU cycles. Failed admission is sticky.
func (b *heapTraversalBudget) take() bool { return b.takeN(1) }

func (b *heapTraversalBudget) takeN(n int) bool {
	if b.exhausted || n < 0 || n > b.remaining {
		b.exhausted = true
		b.remaining = 0
		return false
	}
	b.remaining -= n
	return true
}

func retainedClassSampleLimited(classes map[string]uint64, budget *heapTraversalBudget) ([]string, bool) {
	// Charge enumeration and comparison-sort work before materializing class rows.
	n := len(classes)
	if !budget.takeN(n * (bits.Len(uint(n)) + 1)) {
		return nil, false
	}
	return retainedClassSample(classes), true
}

func (p *hprofParser) rootIDs() []uint64 {
	ids := make([]uint64, 0, len(p.roots))
	for _, root := range p.roots {
		ids = append(ids, root.id)
	}
	return ids
}

func (p *hprofParser) incomingEdges(targets map[uint64]struct{}, budget *heapTraversalBudget) map[uint64][]heapIncomingEdge {
	out := map[uint64][]heapIncomingEdge{}
	if !budget.takeN(len(p.nodes)) {
		return nil
	}
	for index := range p.nodes {
		node := &p.nodes[index]
		for slot := node.firstEdge; slot != 0; {
			if !budget.take() {
				return nil
			}
			edge, next := p.edgeAt(slot)
			slot = next
			if edge.to == 0 {
				continue
			}
			if _, relevant := targets[edge.to]; !relevant {
				continue
			}
			out[edge.to] = append(out[edge.to], heapIncomingEdge{from: node.id, edge: edge})
		}
	}
	for id := range out {
		n := len(out[id])
		if !budget.takeN(n * (bits.Len(uint(n)) + 1)) {
			return nil
		}
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

func (p *hprofParser) referencePath(parent *heapParentIndex, target uint64) []HeapPathElement {
	root, ok := p.pathRoot(parent, target)
	if !ok {
		return nil
	}
	var reversed []HeapPathElement
	current := target
	reachedRoot := false
	for i := 0; i < maxHprofPathElements; i++ {
		step, ok := p.parentFor(parent, current)
		if !ok {
			return nil
		}
		node := p.nodeByID(current)
		className := ""
		if node != nil {
			className = node.className
		}
		if step.from == 0 {
			reachedRoot = true
			if className != "" {
				reversed = append(reversed, HeapPathElement{
					ClassName: className,
					ObjectID:  fmt.Sprintf("0x%x", current),
					Kind:      "root_object",
				})
			}
			reversed = append(reversed, HeapPathElement{
				ClassName: "GC root: " + root.kind,
				ObjectID:  fmt.Sprintf("0x%x", current),
				Kind:      "gc_root",
			})
			break
		}
		edge := p.parentEdge(step)
		reversed = append(reversed, HeapPathElement{
			ClassName: className,
			FieldName: edge.label,
			ObjectID:  fmt.Sprintf("0x%x", current),
			Kind:      edge.kind,
		})
		current = step.from
	}
	if !reachedRoot {
		// Preserve the real root and an explicit gap, plus the closest target-side
		// edges. Root identity never depends on where this display fragment starts.
		reversed = reversed[:maxHprofPathElements-2]
		reversed = append(reversed,
			HeapPathElement{ClassName: "…", Kind: "truncated"},
			HeapPathElement{ClassName: p.nodeClassName(root.id), ObjectID: fmt.Sprintf("0x%x", root.id), Kind: "root_object"},
			HeapPathElement{ClassName: "GC root: " + root.kind, ObjectID: fmt.Sprintf("0x%x", root.id), Kind: "gc_root"},
		)
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
	parent *heapParentIndex,
	incoming map[uint64][]heapIncomingEdge,
	target uint64,
	primary []HeapPathElement,
	budget *heapTraversalBudget,
) [][]HeapPathElement {
	if len(incoming) == 0 {
		return nil
	}
	if !budget.takeN(4 * maxHprofPathElements) {
		return nil
	}
	primaryIDs := p.heapPathNodeIDs(parent, target)
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
			if !budget.take() {
				return out
			}
			if incomingEdge.from == 0 || incomingEdge.from == primaryPredecessor {
				continue
			}
			// Reserve bounded path construction, intersection and fingerprint work.
			if _, ok := p.parentFor(parent, incomingEdge.from); !ok {
				continue
			}
			if !budget.takeN(maxHprofPathElements*maxHprofPathElements + 12*maxHprofPathElements) {
				return out
			}
			prefixIDs := p.heapPathNodeIDs(parent, incomingEdge.from)
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
				step, ok := p.parentFor(parent, suffixID)
				if !ok {
					path = nil
					break
				}
				path = append(path, p.pathElement(suffixID, p.parentEdge(step)))
			}
			if len(path) == 0 {
				continue
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

func (p *hprofParser) heapPathNodeIDs(parent *heapParentIndex, target uint64) []uint64 {
	if _, ok := p.parentFor(parent, target); !ok {
		return nil
	}
	reversed := make([]uint64, 0, maxHprofPathElements)
	current := target
	reachedRoot := false
	for len(reversed) < maxHprofPathElements {
		step, ok := p.parentFor(parent, current)
		if !ok {
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
	if node := p.nodeByID(id); node != nil {
		return node.className
	}
	return ""
}
