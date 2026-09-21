package jhlog

import (
	"fmt"
	"math"
)

type runtimeEdgeKey struct {
	screen      SymbolRef
	caller      SymbolRef
	operationID uint64
	callee      SymbolRef
}

// runtimeBlockEncoder owns bounded runtime-call staging and the segment-local edge registry.
type runtimeBlockEncoder struct {
	edges        map[runtimeEdgeKey]uint64
	rows         [MaxRuntimeCallBlockRows]runtimeCallRow
	addedEdges   [MaxRuntimeCallBlockRows]runtimeEdgeKey
	logicalCalls uint64
}

func newRuntimeBlockEncoder() runtimeBlockEncoder {
	return runtimeBlockEncoder{edges: make(map[runtimeEdgeKey]uint64, 256)}
}

func (encoder *runtimeBlockEncoder) prepare(events []Event) (Event, uint64, int, error) {
	if len(events) == 0 || len(events) > MaxRuntimeCallBlockRows {
		return Event{}, 0, 0, fmt.Errorf(
			"runtime call block row count %d is outside 1..%d", len(events), MaxRuntimeCallBlockRows,
		)
	}
	firstElapsedUS, firstHasTime, err := eventElapsedUS(events[0])
	if err != nil {
		return Event{}, 0, 0, err
	}
	firstHasThread := events[0].Producer.HasThread || events[0].Producer.ThreadID != 0
	rows := encoder.rows[:len(events)]
	logicalCalls := uint64(0)
	for index := range events {
		event := events[index]
		if event.Type != EventRuntimeCall || event.RuntimeCall == nil {
			return Event{}, 0, 0, fmt.Errorf("runtime call block row %d is not a runtime call", index)
		}
		elapsedUS, hasTime, err := eventElapsedUS(event)
		if err != nil {
			return Event{}, 0, 0, err
		}
		hasThread := event.Producer.HasThread || event.Producer.ThreadID != 0
		if hasTime != firstHasTime || elapsedUS != firstElapsedUS || hasThread != firstHasThread ||
			event.Producer.ThreadID != events[0].Producer.ThreadID ||
			eventAttributes(event) != eventAttributes(events[0]) {
			return Event{}, 0, 0, fmt.Errorf("runtime call block row %d has different producer metadata", index)
		}
		if event.RuntimeCall.Count == 0 {
			return Event{}, 0, 0, fmt.Errorf("runtime call block row %d has zero logical calls", index)
		}
		if event.RuntimeCall.MaxMS > event.RuntimeCall.TotalMS {
			return Event{}, 0, 0, fmt.Errorf(
				"runtime call block row %d max duration %d exceeds total %d",
				index, event.RuntimeCall.MaxMS, event.RuntimeCall.TotalMS,
			)
		}
		if math.MaxUint64-logicalCalls < event.RuntimeCall.Count {
			return Event{}, 0, 0, fmt.Errorf("runtime call block logical call count overflows uint64")
		}
		logicalCalls += event.RuntimeCall.Count
		context := eventAttribution(event)
		rows[index] = runtimeCallRow{
			screen: context.Screen, caller: context.Owner, operationID: context.OperationID,
			callee: event.RuntimeCall.CalleeRef, count: event.RuntimeCall.Count,
			total: event.RuntimeCall.TotalMS, max: event.RuntimeCall.MaxMS,
		}
	}
	if math.MaxUint64-encoder.logicalCalls < logicalCalls {
		return Event{}, 0, 0, fmt.Errorf("runtime graph logical call total overflows uint64")
	}
	addedCount := encoder.prepareEdges(rows)
	synthetic := events[0]
	synthetic.runtimeCalls = rows
	return synthetic, logicalCalls, addedCount, nil
}

func (encoder *runtimeBlockEncoder) prepareEdges(calls []runtimeCallRow) int {
	addedCount := 0
	for index := range calls {
		key := runtimeEdgeKey{
			screen: calls[index].screen, caller: calls[index].caller,
			operationID: calls[index].operationID, callee: calls[index].callee,
		}
		if edgeID := encoder.edges[key]; edgeID != 0 {
			calls[index].edgeID = edgeID
			calls[index].edgeDefinition = false
			calls[index].edgeReady = true
			continue
		}
		if len(encoder.edges) == maxRuntimeEdges {
			calls[index].edgeID = 0
			calls[index].edgeDefinition = false
			calls[index].edgeReady = true
			continue
		}
		edgeID := uint64(len(encoder.edges)) + 1
		encoder.edges[key] = edgeID
		encoder.addedEdges[addedCount] = key
		addedCount++
		calls[index].edgeID = edgeID
		calls[index].edgeDefinition = true
		calls[index].edgeReady = true
	}
	return addedCount
}

func (encoder *runtimeBlockEncoder) commit(logicalCalls uint64) {
	encoder.logicalCalls += logicalCalls
}

func (encoder *runtimeBlockEncoder) rollback(addedCount int) {
	for index := addedCount - 1; index >= 0; index-- {
		delete(encoder.edges, encoder.addedEdges[index])
	}
}
