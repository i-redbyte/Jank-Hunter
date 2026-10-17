package jhlog

import "fmt"

func decodeEventPayload(
	reader *recordReader,
	event *Event,
	processName string,
	symbolNamespace string,
	runtimeCallScratch []runtimeCallRow,
	optionalSegmentState ...*segmentDecodeState,
) error {
	segmentState := decodeSegmentState(optionalSegmentState)
	var valueScratch [21]uint64
	switch event.Type {
	case EventDictionary, EventSession, EventContext, EventUIWindow, EventStall, EventMemory,
		EventRetained, EventCounter, EventGauge, EventLogSpam, EventProblem, EventProcessExit, EventProcessState:
		return decodeCorePayload(reader, event, processName, symbolNamespace, segmentState, valueScratch[:])
	case EventHTTP:
		return decodeHTTPPayload(reader, event, symbolNamespace, segmentState, valueScratch[:])
	case EventOperation:
		return decodeOperationPayload(reader, event, symbolNamespace, segmentState, valueScratch[:])
	case EventRuntimeCall:
		return decodeRuntimeCallPayload(reader, event, symbolNamespace, runtimeCallScratch, segmentState)
	case EventIO:
		return decodeIOPayload(reader, event, symbolNamespace, segmentState, valueScratch[:])
	case EventWorkSubmission:
		return decodeWorkSubmission(reader, event)
	case EventWorkChainEdge:
		return decodeWorkChainEdge(reader, event)
	case EventWorkerFailure:
		return decodeWorkerFailure(reader, event)
	case EventWorkerProgress:
		return decodeWorkerProgress(reader, event)
	case EventCoroutineSegment:
		return decodeCoroutineSegment(reader, event)
	case EventCausalEvent:
		if segmentState.legacyCausalCollectionState {
			return fmt.Errorf("causal event requires feature32")
		}
		return decodeCausalEvent(reader, event)
	case EventWorkChainRoot:
		if err := decodeWorkSubmission(reader, event); err != nil {
			return err
		}
		event.WorkChainRoot = event.WorkSubmission
		event.WorkSubmission = nil
		return nil
	case EventThreadLifecycle:
		return decodeThreadLifecycle(reader, event)
	case EventWorkerIdentity:
		return decodeWorkerIdentity(reader, event)
	case EventWorkRegistry:
		return decodeWorkRegistry(reader, event)
	case EventWorkerCatalog:
		return decodeWorkerCatalog(reader, event)
	case EventThreadSnapshot:
		return decodeThreadSnapshot(reader, event)
	case EventWorker:
		return decodeWorkerPayload(reader, event, symbolNamespace, segmentState, valueScratch[:])
	case EventWebSocket:
		return decodeWebSocketPayload(reader, event, symbolNamespace, segmentState, valueScratch[:])
	case EventDatabase:
		return decodeDatabasePayload(reader, event, symbolNamespace, segmentState, valueScratch[:])
	case EventDatabaseTransaction:
		return decodeDatabaseTransactionPayload(reader, event, symbolNamespace, segmentState, valueScratch[:])
	case EventAndroidComponent:
		return decodeAndroidComponentPayload(reader, event, symbolNamespace, segmentState, valueScratch[:])
	case EventBinderTransaction:
		return decodeBinderTransactionPayload(reader, event, symbolNamespace, segmentState, valueScratch[:])
	case EventQualitySnapshot:
		return decodeQualityPayload(reader, event, segmentState, valueScratch[:])
	case EventSegmentEnd:
		return decodeSegmentEndPayload(reader, event, valueScratch[:])
	case EventLogGrowth:
		return decodeLogGrowthPayload(reader, event, segmentState, valueScratch[:])
	default:
		return fmt.Errorf("unsupported event type %d", event.Type)
	}
}

func readPayloadValue(reader *recordReader, name string) (uint64, error) {
	value, err := reader.readUvarint()
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	return value, nil
}

func readPayloadRef(
	reader *recordReader,
	symbolNamespace string,
	segmentState *segmentDecodeState,
	name string,
) (SymbolRef, error) {
	ref, err := readSymbolRef(reader, symbolNamespace, segmentState.stableAliases, &segmentState.origins)
	if err != nil {
		return SymbolRef{}, fmt.Errorf("%s: %w", name, err)
	}
	return ref, nil
}

func readPayloadValues(reader *recordReader, scratch []uint64, names ...string) ([]uint64, error) {
	if len(names) > len(scratch) {
		return nil, fmt.Errorf("payload field count %d exceeds scratch capacity %d", len(names), len(scratch))
	}
	values := scratch[:len(names)]
	for index, name := range names {
		value, err := readPayloadValue(reader, name)
		if err != nil {
			return nil, err
		}
		values[index] = value
	}
	return values, nil
}

// Check the wire width before converting to a byte-backed enum. Semantic
// validators then reject unsupported values without losing their high bits.
func validatePayloadByteFields(values []uint64, indices ...int) error {
	for _, index := range indices {
		if values[index] > 255 {
			return fmt.Errorf("payload field %d value %d exceeds byte range", index, values[index])
		}
	}
	return nil
}

func readPayloadByteValue(reader *recordReader, name string) (uint64, error) {
	value, err := readPayloadValue(reader, name)
	if err != nil {
		return 0, err
	}
	if value > 255 {
		return 0, fmt.Errorf("%s value %d exceeds byte range", name, value)
	}
	return value, nil
}
