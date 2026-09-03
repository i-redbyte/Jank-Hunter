package jhlog

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
)

type eventPayloadEncoder struct {
	writer        io.Writer
	stableAliases map[uint64]uint64
}

func encodeEventPayloadWithState(
	w io.Writer,
	event Event,
	stableAliases map[uint64]uint64,
	state *eventPayloadEncodeState,
) error {
	if state == nil {
		state = &eventPayloadEncodeState{}
	}
	encoder := eventPayloadEncoder{writer: w, stableAliases: stableAliases}
	return encoder.encode(event, state)
}

func (encoder eventPayloadEncoder) encode(event Event, state *eventPayloadEncodeState) error {
	switch event.Type {
	case EventDictionary:
		return encoder.encodeDictionary(event.Dictionary)
	case EventSession:
		return encoder.encodeSession(event.Session)
	case EventContext:
		return encoder.encodeContext(event.Context)
	case EventHTTP:
		return encoder.encodeHTTP(event.HTTP)
	case EventUIWindow:
		return encoder.encodeUIWindow(event.UIWindow)
	case EventStall:
		return encoder.encodeStall(event.Stall)
	case EventMemory:
		return encoder.encodeMemory(event.Memory)
	case EventRetained:
		return encoder.encodeRetained(event.Retained)
	case EventCounter, EventGauge:
		return encoder.encodeMetric(event.Metric)
	case EventOperation:
		return encoder.encodeOperation(event.Operation)
	case EventLogSpam:
		return encoder.encodeLogSpam(event.LogSpam)
	case EventProblem:
		return encoder.encodeProblem(event.Problem)
	case EventRuntimeCall:
		return encoder.encodeRuntimeCall(event)
	case EventProcessExit:
		return encoder.encodeProcessExit(event.ProcessExit)
	case EventIO:
		return encoder.encodeIO(event.IO, event.Flags)
	case EventWorker:
		return encoder.encodeWorker(event.Worker, event.Flags)
	case EventWebSocket:
		return encoder.encodeWebSocket(event.WebSocket)
	case EventDatabase:
		return encoder.encodeDatabase(event.Database, state)
	case EventDatabaseTransaction:
		return encoder.encodeDatabaseTransaction(event.DatabaseTransaction, state)
	case EventProcessState:
		return encoder.encodeProcessState(event.ProcessState)
	case EventAndroidComponent:
		return encoder.encodeAndroidComponent(event.AndroidComponent)
	case EventBinderTransaction:
		return encoder.encodeBinderTransaction(event.BinderTransaction, event.Flags)
	case EventQualitySnapshot:
		return encoder.encodeQuality(event.Quality)
	case EventSegmentEnd:
		return encoder.encodeSegmentEnd(event.SegmentEnd)
	case EventLogGrowth:
		return encoder.encodeLogGrowth(event.LogGrowth)
	default:
		return fmt.Errorf("unsupported event type %d", event.Type)
	}
}

func (encoder eventPayloadEncoder) writeValues(values ...uint64) error {
	for _, value := range values {
		if err := writeUvarint(encoder.writer, value); err != nil {
			return err
		}
	}
	return nil
}

func (encoder eventPayloadEncoder) writeRefs(refs ...SymbolRef) error {
	for _, ref := range refs {
		if err := writeSymbolRef(encoder.writer, ref, encoder.stableAliases); err != nil {
			return err
		}
	}
	return nil
}

func equalAttribution(a, b AttributionContext) bool {
	return a.Screen == b.Screen && a.Owner == b.Owner && a.OperationID == b.OperationID
}

func writeAttribution(w io.Writer, context AttributionContext, stableAliases map[uint64]uint64) error {
	mask := uint64(0)
	if !context.Screen.IsUnknown() {
		mask |= 1 << 0
	}
	if !context.Owner.IsUnknown() {
		mask |= 1 << 1
	}
	if context.OperationID != 0 {
		mask |= 1 << 2
	}
	if err := writeUvarint(w, mask); err != nil {
		return err
	}
	if mask&(1<<0) != 0 {
		if err := writeSymbolRef(w, context.Screen, stableAliases); err != nil {
			return err
		}
	}
	if mask&(1<<1) != 0 {
		if err := writeSymbolRef(w, context.Owner, stableAliases); err != nil {
			return err
		}
	}
	if mask&(1<<2) != 0 {
		return writeUvarint(w, context.OperationID)
	}
	return nil
}

func writeSymbolRef(w io.Writer, ref SymbolRef, stableAliases map[uint64]uint64) error {
	if ref.Stable {
		if alias, ok := stableAliases[ref.ID]; ok {
			if alias > math.MaxUint64>>1 {
				return fmt.Errorf("stable symbol alias %d is too large", alias)
			}
			return writeUvarint(w, alias<<1|1)
		}
		if err := writeUvarint(w, 1); err != nil {
			return err
		}
		return writeFixedUint64(w, ref.ID)
	}
	if ref.ID > math.MaxUint64>>1 {
		return fmt.Errorf("local symbol id %d is too large", ref.ID)
	}
	return writeUvarint(w, ref.ID<<1)
}

func writeFixedUint64(w io.Writer, value uint64) error {
	if buffer, ok := w.(*bytes.Buffer); ok {
		writeFixedUint64ToBuffer(buffer, value)
		return nil
	}
	return writeFixedUint64Generic(w, value)
}

func writeFixedUint64ToBuffer(buffer *bytes.Buffer, value uint64) {
	var raw [8]byte
	binary.LittleEndian.PutUint64(raw[:], value)
	_, _ = buffer.Write(raw[:])
}

func writeFixedUint64Generic(w io.Writer, value uint64) error {
	var raw [8]byte
	binary.LittleEndian.PutUint64(raw[:], value)
	return writeAll(w, raw[:])
}
