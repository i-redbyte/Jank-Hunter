package analyze

// influenceAdjacency stores edge indexes in compressed-sparse-row form. Both directions share
// two backing allocations: one for offsets and one for edge indexes.
type influenceAdjacency struct {
	offsets []uint32
	edges   []uint32
}

func newInfluenceAdjacency(
	nodeByID map[string]uint32,
	edges []InfluenceEdge,
) (influenceAdjacency, influenceAdjacency) {
	nodeCount := len(nodeByID)
	offsetStorage := make([]uint32, 2*(nodeCount+1))
	outgoing := influenceAdjacency{offsets: offsetStorage[: nodeCount+1 : nodeCount+1]}
	incoming := influenceAdjacency{offsets: offsetStorage[nodeCount+1:]}
	validEdges := 0
	for index := range edges {
		fromSlot := nodeByID[edges[index].From]
		toSlot := nodeByID[edges[index].To]
		if fromSlot == 0 || toSlot == 0 {
			continue
		}
		outgoing.offsets[fromSlot]++
		incoming.offsets[toSlot]++
		validEdges++
	}
	for index := 1; index <= nodeCount; index++ {
		outgoing.offsets[index] += outgoing.offsets[index-1]
		incoming.offsets[index] += incoming.offsets[index-1]
	}
	edgeStorage := make([]uint32, 2*validEdges)
	outgoing.edges = edgeStorage[:validEdges:validEdges]
	incoming.edges = edgeStorage[validEdges:]
	for index := range edges {
		fromSlot := nodeByID[edges[index].From]
		toSlot := nodeByID[edges[index].To]
		if fromSlot == 0 || toSlot == 0 {
			continue
		}
		outgoing.offsets[fromSlot]--
		outgoing.edges[outgoing.offsets[fromSlot]] = uint32(index)
		incoming.offsets[toSlot]--
		incoming.edges[incoming.offsets[toSlot]] = uint32(index)
	}
	for index := 0; index < nodeCount; index++ {
		outgoing.offsets[index] = outgoing.offsets[index+1]
		incoming.offsets[index] = incoming.offsets[index+1]
	}
	outgoing.offsets[nodeCount] = uint32(validEdges)
	incoming.offsets[nodeCount] = uint32(validEdges)
	return outgoing, incoming
}

func (a influenceAdjacency) edgesForSlot(slot uint32) []uint32 {
	if slot == 0 || int(slot) >= len(a.offsets) {
		return nil
	}
	index := slot - 1
	return a.edges[a.offsets[index]:a.offsets[index+1]]
}
