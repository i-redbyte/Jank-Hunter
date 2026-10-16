package jhlog

import "testing"

func FuzzEventPayloadDecoder(f *testing.F) {
	for _, kind := range []EventType{
		EventDictionary, EventSession, EventContext, EventUIWindow, EventStall,
		EventHTTP, EventOperation, EventIO, EventWorker, EventWebSocket,
		EventDatabase, EventDatabaseTransaction, EventAndroidComponent, EventBinderTransaction,
		EventProcessState, EventRuntimeCall, EventQualitySnapshot, EventSegmentEnd,
		EventWorkRegistry, EventThreadSnapshot, EventCausalEvent,
	} {
		f.Add(uint64(kind), []byte{})
		f.Add(uint64(kind), []byte{2, 1, 1, 1, 1, 0, 0, 0, 0})
	}
	f.Add(uint64(EventIO), []byte{2, 1, 0x81, 2, 0, 0}) // outcome 257
	f.Fuzz(func(t *testing.T, kind uint64, payload []byte) {
		if len(payload) > 64<<10 {
			t.Skip()
		}
		event := Event{Type: EventType(kind)}
		// The streaming caller checks trailing bytes and classifies corruption.
		// Any payload, including unknown types, must return without panicking.
		_ = decodeEventPayload(&recordReader{data: payload}, &event, "", "", nil)
	})
}
