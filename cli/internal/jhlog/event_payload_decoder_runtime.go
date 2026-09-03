package jhlog

import "fmt"

func decodeRuntimeCallPayload(
	reader *recordReader,
	event *Event,
	symbolNamespace string,
	runtimeCallScratch []runtimeCallRow,
	segmentState *segmentDecodeState,
) error {
	blockHeader, err := readPayloadValue(reader, "runtime call block header")
	if err != nil {
		return err
	}
	rowCount := blockHeader >> 1
	columnarTuples := blockHeader&1 != 0
	if rowCount == 0 || rowCount > MaxRuntimeCallBlockRows {
		return fmt.Errorf("runtime call row count %d is outside 1..%d", rowCount, MaxRuntimeCallBlockRows)
	}
	var calls []runtimeCallRow
	if int(rowCount) <= len(runtimeCallScratch) {
		calls = runtimeCallScratch[:int(rowCount)]
		clear(calls)
	} else {
		calls = make([]runtimeCallRow, int(rowCount))
	}
	var definitionMask [(MaxRuntimeCallBlockRows + 7) / 8]byte
	definitionBytes := (len(calls) + 7) / 8
	if err := reader.readFull(definitionMask[:definitionBytes]); err != nil {
		return fmt.Errorf("runtime edge definition mask: %w", err)
	}
	if remainder := len(calls) & 7; remainder != 0 &&
		definitionMask[definitionBytes-1]&^byte((1<<remainder)-1) != 0 {
		return fmt.Errorf("runtime edge definition mask has bits outside row count")
	}
	var values [MaxRuntimeCallBlockRows]uint64
	if err := readRuntimeNumericColumn(reader, values[:len(calls)], true, false, 0); err != nil {
		return fmt.Errorf("runtime edge IDs: %w", err)
	}
	for index := range calls {
		calls[index].edgeID = values[index]
		calls[index].edgeDefinition = definitionMask[index>>3]&(1<<(index&7)) != 0
		if calls[index].edgeID == 0 && calls[index].edgeDefinition {
			return fmt.Errorf("runtime inline edge row %d is marked as a definition", index)
		}
	}
	if columnarTuples {
		if err := readRuntimeEdgeTupleColumns(reader, calls, values[:], symbolNamespace, segmentState); err != nil {
			return err
		}
	}
	for index := range calls {
		edgeID := calls[index].edgeID
		definition := calls[index].edgeDefinition
		var edge runtimeEdgeKey
		if edgeID == 0 || definition {
			if !columnarTuples {
				screen, readErr := readPayloadRef(reader, symbolNamespace, segmentState, "runtime edge screen")
				if readErr != nil {
					return readErr
				}
				caller, readErr := readPayloadRef(reader, symbolNamespace, segmentState, "runtime edge caller")
				if readErr != nil {
					return readErr
				}
				operationID, readErr := readPayloadValue(reader, "runtime edge operation")
				if readErr != nil {
					return readErr
				}
				callee, readErr := readPayloadRef(reader, symbolNamespace, segmentState, "runtime edge callee")
				if readErr != nil {
					return readErr
				}
				calls[index].screen = screen
				calls[index].caller = caller
				calls[index].operationID = operationID
				calls[index].callee = callee
			}
			edge = runtimeEdgeKey{
				screen: calls[index].screen, caller: calls[index].caller,
				operationID: calls[index].operationID, callee: calls[index].callee,
			}
			if definition {
				if edgeID == 0 || edgeID != uint64(len(segmentState.runtimeEdges))+1 {
					return fmt.Errorf("runtime edge definition ID %d is not the next sequential ID", edgeID)
				}
				if len(segmentState.runtimeEdges) == maxRuntimeEdges {
					return fmt.Errorf("runtime edge definitions exceed %d", maxRuntimeEdges)
				}
				segmentState.runtimeEdges = append(segmentState.runtimeEdges, edge)
			}
		} else {
			if edgeID == 0 || edgeID > uint64(len(segmentState.runtimeEdges)) {
				return fmt.Errorf("undefined runtime edge ID %d", edgeID)
			}
			edge = segmentState.runtimeEdges[edgeID-1]
		}
		calls[index].screen = edge.screen
		calls[index].caller = edge.caller
		calls[index].operationID = edge.operationID
		calls[index].callee = edge.callee
	}
	if err := readRuntimeNumericColumn(reader, values[:len(calls)], true, true, 1); err != nil {
		return fmt.Errorf("runtime counts: %w", err)
	}
	for index := range calls {
		calls[index].count = values[index]
	}
	if err := readRuntimeNumericColumn(reader, values[:len(calls)], true, true, 0); err != nil {
		return fmt.Errorf("runtime totals: %w", err)
	}
	for index := range calls {
		calls[index].total = values[index]
	}
	if err := readRuntimeNumericColumn(reader, values[:len(calls)], true, true, 0); err != nil {
		return fmt.Errorf("runtime maxima: %w", err)
	}
	for index := range calls {
		calls[index].max = values[index]
	}
	for index := range calls {
		if calls[index].count == 0 {
			return fmt.Errorf("runtime call row %d has zero logical calls", index)
		}
		if calls[index].max > calls[index].total {
			return fmt.Errorf(
				"runtime call row %d max duration %d exceeds total %d",
				index,
				calls[index].max,
				calls[index].total,
			)
		}
	}
	event.runtimeCalls = calls
	return nil
}

func readRuntimeEdgeTupleColumns(
	reader *recordReader,
	calls []runtimeCallRow,
	values []uint64,
	symbolNamespace string,
	segmentState *segmentDecodeState,
) error {
	tupleCount := 0
	for index := range calls {
		if calls[index].edgeID == 0 || calls[index].edgeDefinition {
			tupleCount++
		}
	}
	if tupleCount == 0 {
		return nil
	}
	if err := readRuntimeNumericColumn(reader, values[:tupleCount], true, true, 0); err != nil {
		return fmt.Errorf("runtime edge screens: %w", err)
	}
	for index, tuple := 0, 0; index < len(calls); index++ {
		if calls[index].edgeID == 0 || calls[index].edgeDefinition {
			calls[index].screen = LocalSymbol(values[tuple])
			tuple++
		}
	}
	if err := readRuntimeNumericColumn(reader, values[:tupleCount], true, false, 0); err != nil {
		return fmt.Errorf("runtime edge callers: %w", err)
	}
	for index, tuple := 0, 0; index < len(calls); index++ {
		if calls[index].edgeID == 0 || calls[index].edgeDefinition {
			ref, err := runtimeStableAlias(values[tuple], symbolNamespace, segmentState, "caller")
			if err != nil {
				return err
			}
			calls[index].caller = ref
			tuple++
		}
	}
	if err := readRuntimeNumericColumn(reader, values[:tupleCount], true, true, 0); err != nil {
		return fmt.Errorf("runtime edge operations: %w", err)
	}
	for index, tuple := 0, 0; index < len(calls); index++ {
		if calls[index].edgeID == 0 || calls[index].edgeDefinition {
			calls[index].operationID = values[tuple]
			tuple++
		}
	}
	if err := readRuntimeNumericColumn(reader, values[:tupleCount], true, false, 0); err != nil {
		return fmt.Errorf("runtime edge callees: %w", err)
	}
	for index, tuple := 0, 0; index < len(calls); index++ {
		if calls[index].edgeID == 0 || calls[index].edgeDefinition {
			ref, err := runtimeStableAlias(values[tuple], symbolNamespace, segmentState, "callee")
			if err != nil {
				return err
			}
			calls[index].callee = ref
			tuple++
		}
	}
	return nil
}

func runtimeStableAlias(
	alias uint64,
	symbolNamespace string,
	segmentState *segmentDecodeState,
	field string,
) (SymbolRef, error) {
	stableID, ok := segmentState.stableAliases[alias]
	if alias == 0 || !ok {
		return SymbolRef{}, fmt.Errorf("runtime edge %s has undefined stable alias %d", field, alias)
	}
	return SymbolRef{ID: stableID, Namespace: symbolNamespace, Stable: true}, nil
}
