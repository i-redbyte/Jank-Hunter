package analyze

import "sort"

type agentSequenceRange struct {
	first uint64
	last  uint64
}

type agentProducerKey struct {
	source     string
	producerID uint64
}

type agentSequenceState struct {
	ranges        []agentSequenceRange
	lostPrecision bool
}

func (a *agentAggregator) observeSequence(source string, producerID, sequence uint64) {
	if sequence == 0 {
		return
	}
	key := agentProducerKey{source: source, producerID: producerID}
	state := a.sequences[key]
	if state == nil {
		if len(a.sequences) >= maxAgentSources {
			a.sourceCapacityLost = true
			return
		}
		state = &agentSequenceState{}
		a.sequences[key] = state
	}
	state.add(sequence)
}

func (a *agentAggregator) observeSequenceRange(key agentProducerKey, interval agentSequenceRange) {
	state := a.sequences[key]
	if state == nil {
		state = &agentSequenceState{}
		a.sequences[key] = state
	}
	state.addRange(interval)
}

func (state *agentSequenceState) add(sequence uint64) {
	if len(state.ranges) == 0 {
		state.ranges = append(state.ranges, agentSequenceRange{first: sequence, last: sequence})
		return
	}
	last := &state.ranges[len(state.ranges)-1]
	if sequence >= last.first {
		if sequence <= last.last || (last.last != ^uint64(0) && sequence == last.last+1) {
			last.last = maxUint64(last.last, sequence)
			return
		}
		if len(state.ranges) >= maxAgentSequenceRanges {
			state.lostPrecision = true
			return
		}
		state.ranges = append(state.ranges, agentSequenceRange{first: sequence, last: sequence})
		return
	}
	state.addRange(agentSequenceRange{first: sequence, last: sequence})
}

func (state *agentSequenceState) addRange(value agentSequenceRange) {
	state.ranges = append(state.ranges, value)
	sort.Slice(state.ranges, func(i, j int) bool {
		return state.ranges[i].first < state.ranges[j].first
	})
	merged := state.ranges[:0]
	for _, current := range state.ranges {
		lastIndex := len(merged) - 1
		if lastIndex < 0 ||
			(merged[lastIndex].last != ^uint64(0) && current.first > merged[lastIndex].last+1) {
			merged = append(merged, current)
			continue
		}
		merged[lastIndex].last = maxUint64(merged[lastIndex].last, current.last)
	}
	state.ranges = merged
	if len(state.ranges) > maxAgentSequenceRanges {
		state.lostPrecision = true
		state.ranges = state.ranges[:maxAgentSequenceRanges]
	}
}

func (state *agentSequenceState) gaps() uint64 {
	var gaps uint64
	if len(state.ranges) > 0 && state.ranges[0].first > 1 {
		gaps = state.ranges[0].first - 1
	}
	for index := 1; index < len(state.ranges); index++ {
		gaps = saturatingAdd(gaps, state.ranges[index].first-state.ranges[index-1].last-1)
	}
	return gaps
}
