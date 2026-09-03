package jhlog

import (
	"fmt"
	"io"
)

func (encoder eventPayloadEncoder) encodeRuntimeCall(event Event) error {
	payload := event.RuntimeCall
	if payload == nil {
		return fmt.Errorf("runtime call payload is nil")
	}
	calls := event.runtimeCalls
	var single [1]runtimeCallRow
	if len(calls) == 0 {
		context := eventAttribution(event)
		single[0] = runtimeCallRow{
			screen:      context.Screen,
			caller:      context.Owner,
			operationID: context.OperationID,
			callee:      payload.CalleeRef,
			count:       payload.Count,
			total:       payload.TotalMS,
			max:         payload.MaxMS,
		}
		calls = single[:]
	}
	if len(calls) > MaxRuntimeCallBlockRows {
		return fmt.Errorf("runtime call block row count %d exceeds %d", len(calls), MaxRuntimeCallBlockRows)
	}
	columnarTuples := canColumnarizeRuntimeEdgeTuples(calls, encoder.stableAliases)
	blockHeader := uint64(len(calls)) << 1
	if columnarTuples {
		blockHeader |= 1
	}
	if err := encoder.writeValues(blockHeader); err != nil {
		return err
	}
	var values [MaxRuntimeCallBlockRows]uint64
	for index := range calls {
		if calls[index].edgeReady {
			values[index] = calls[index].edgeID
		}
	}
	if err := writeRuntimeEdgeDefinitionMask(encoder.writer, calls); err != nil {
		return err
	}
	if err := writeRuntimeNumericColumn(encoder.writer, values[:len(calls)], true, false, 0); err != nil {
		return fmt.Errorf("runtime edge IDs: %w", err)
	}
	if columnarTuples {
		if err := encoder.writeRuntimeEdgeTupleColumns(calls, values[:len(calls)]); err != nil {
			return err
		}
	} else {
		for index := range calls {
			if values[index] != 0 && !calls[index].edgeDefinition {
				continue
			}
			if err := encoder.writeRefs(calls[index].screen, calls[index].caller); err != nil {
				return err
			}
			if err := writeUvarint(encoder.writer, calls[index].operationID); err != nil {
				return err
			}
			if err := writeSymbolRef(encoder.writer, calls[index].callee, encoder.stableAliases); err != nil {
				return err
			}
		}
	}
	for index := range calls {
		values[index] = calls[index].count
	}
	if err := writeRuntimeNumericColumn(encoder.writer, values[:len(calls)], true, true, 1); err != nil {
		return fmt.Errorf("runtime counts: %w", err)
	}
	for index := range calls {
		values[index] = calls[index].total
	}
	if err := writeRuntimeNumericColumn(encoder.writer, values[:len(calls)], true, true, 0); err != nil {
		return fmt.Errorf("runtime totals: %w", err)
	}
	for index := range calls {
		values[index] = calls[index].max
	}
	if err := writeRuntimeNumericColumn(encoder.writer, values[:len(calls)], true, true, 0); err != nil {
		return fmt.Errorf("runtime maxima: %w", err)
	}
	return nil
}

func writeRuntimeEdgeDefinitionMask(writer io.Writer, calls []runtimeCallRow) error {
	if byteWriter, ok := writer.(io.ByteWriter); ok {
		for start := 0; start < len(calls); start += 8 {
			var mask byte
			end := min(start+8, len(calls))
			for index := start; index < end; index++ {
				if calls[index].edgeDefinition {
					mask |= 1 << (index - start)
				}
			}
			if err := byteWriter.WriteByte(mask); err != nil {
				return err
			}
		}
		return nil
	}
	var mask [(MaxRuntimeCallBlockRows + 7) / 8]byte
	for index := range calls {
		if calls[index].edgeDefinition {
			mask[index>>3] |= 1 << (index & 7)
		}
	}
	return writeAll(writer, mask[:(len(calls)+7)/8])
}

func canColumnarizeRuntimeEdgeTuples(calls []runtimeCallRow, stableAliases map[uint64]uint64) bool {
	for index := range calls {
		if calls[index].edgeID != 0 && !calls[index].edgeDefinition {
			continue
		}
		if calls[index].screen.Stable || !calls[index].caller.Stable || !calls[index].callee.Stable ||
			stableAliases[calls[index].caller.ID] == 0 || stableAliases[calls[index].callee.ID] == 0 {
			return false
		}
	}
	return true
}

func (encoder eventPayloadEncoder) writeRuntimeEdgeTupleColumns(calls []runtimeCallRow, edgeIDs []uint64) error {
	var values [MaxRuntimeCallBlockRows]uint64
	tupleCount := 0
	for index := range calls {
		if edgeIDs[index] == 0 || calls[index].edgeDefinition {
			values[tupleCount] = calls[index].screen.ID
			tupleCount++
		}
	}
	if tupleCount == 0 {
		return nil
	}
	if err := writeRuntimeNumericColumn(encoder.writer, values[:tupleCount], true, true, 0); err != nil {
		return fmt.Errorf("runtime edge screens: %w", err)
	}
	tupleCount = 0
	for index := range calls {
		if edgeIDs[index] == 0 || calls[index].edgeDefinition {
			values[tupleCount] = encoder.stableAliases[calls[index].caller.ID]
			tupleCount++
		}
	}
	if err := writeRuntimeNumericColumn(encoder.writer, values[:tupleCount], true, false, 0); err != nil {
		return fmt.Errorf("runtime edge callers: %w", err)
	}
	tupleCount = 0
	for index := range calls {
		if edgeIDs[index] == 0 || calls[index].edgeDefinition {
			values[tupleCount] = calls[index].operationID
			tupleCount++
		}
	}
	if err := writeRuntimeNumericColumn(encoder.writer, values[:tupleCount], true, true, 0); err != nil {
		return fmt.Errorf("runtime edge operations: %w", err)
	}
	tupleCount = 0
	for index := range calls {
		if edgeIDs[index] == 0 || calls[index].edgeDefinition {
			values[tupleCount] = encoder.stableAliases[calls[index].callee.ID]
			tupleCount++
		}
	}
	if err := writeRuntimeNumericColumn(encoder.writer, values[:tupleCount], true, false, 0); err != nil {
		return fmt.Errorf("runtime edge callees: %w", err)
	}
	return nil
}
