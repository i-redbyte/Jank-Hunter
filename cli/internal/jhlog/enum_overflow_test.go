package jhlog

import (
	"fmt"
	"testing"
)

// Raw wire values deliberately bypass the typed writer: narrowing an enum before
// validation used to accept 256+n as n and silently change the recorded facts.
func TestPayloadRejectsOverflowingByteEnums(t *testing.T) {
	cases := []struct {
		name   string
		kind   EventType
		values []uint64
		fields []int
	}{
		{"http", EventHTTP, []uint64{2, 2, 2, 1, 0, 0, 0, 0, 0, 0, 0, 200, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, []int{12, 13, 14}},
		{"operation", EventOperation, []uint64{2, 1, 0, 1, 1, 0, 0, 0, 0}, []int{3, 4, 5}},
		{"io", EventIO, []uint64{2, 1, 1, 0, 0}, []int{2}},
		{"websocket", EventWebSocket, []uint64{2, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0}, []int{2, 6}},
		{"component", EventAndroidComponent, []uint64{2, 0, 1, 1, 1, 1, 0, 0, 0}, []int{4, 5, 6}},
		{"binder", EventBinderTransaction, []uint64{2, 2, 1, 1, 0, 1, 0, 0, 0}, []int{3, 5, 6}},
		{"process", EventProcessState, []uint64{1, 1, 100, 1}, []int{0, 1, 3}},
		{"database", EventDatabase, []uint64{1, 2, 2, 1, 1, 1, 1, 1, 0, 0}, []int{4, 5, 6, 7}},
		{"database_failure", EventDatabase, []uint64{1, 2, 2, 1, 1, 1, 1, 2, 0, databasePresenceFailure, 1}, []int{10}},
		{"database_result", EventDatabase, []uint64{1, 2, 2, 1, 1, 1, 1, 1, 0, databasePresenceResult, 1, 1}, []int{10, 11}},
		{"transaction", EventDatabaseTransaction, []uint64{2, 2, 1, transactionPresenceMode, 1}, []int{2, 4}},
		{"transaction_outcome", EventDatabaseTransaction, []uint64{2, 2, 2, transactionPresenceOutcome, 1}, []int{4}},
		{"transaction_failure", EventDatabaseTransaction, []uint64{2, 2, 2, transactionPresenceOutcome | transactionPresenceFailure, 3, 1}, []int{5}},
	}
	decode := func(kind EventType, values []uint64) error {
		var wire []byte
		for _, value := range values {
			wire = appendUvarint(wire, value)
		}
		event := Event{Type: kind}
		return decodeEventPayload(&recordReader{data: wire}, &event, "", "", nil)
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if err := decode(test.kind, test.values); err != nil {
				t.Fatalf("valid control: %v", err)
			}
			for _, field := range test.fields {
				for _, high := range []uint64{1 << 8, 1 << 32, 1 << 63} {
					t.Run(fmt.Sprintf("field_%d/high_%d", field, high), func(t *testing.T) {
						values := append([]uint64(nil), test.values...)
						values[field] += high
						if err := decode(test.kind, values); err == nil {
							t.Fatalf("accepted out-of-range wire value %d", values[field])
						}
					})
				}
			}
		})
	}
}
