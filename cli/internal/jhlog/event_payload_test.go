package jhlog

import (
	"bytes"
	"io"
	"reflect"
	"testing"
)

func encodeEventPayload(w io.Writer, event Event, stableAliases map[uint64]uint64) error {
	return encodeEventPayloadWithState(w, event, stableAliases, nil)
}

func TestDictionaryWireOmitsImplicitStableAliases(t *testing.T) {
	for _, test := range []struct {
		name  string
		entry DictionaryEntry
		want  []byte
	}{
		{
			name:  "local",
			entry: DictionaryEntry{Kind: DictGeneric, ID: 1, Value: "x"},
			want:  []byte{byte(DictGeneric), 1, 0, 1, 'x'},
		},
		{
			name:  "stable",
			entry: DictionaryEntry{Kind: DictStableSymbol, ID: 0x0102030405060708, Alias: 1, Value: "y"},
			want:  readWireGolden(t, "dictionary-stable.bin"),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var encoded bytes.Buffer
			if err := encodeEventPayload(&encoded, Event{Type: EventDictionary, Dictionary: &test.entry}, nil); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(encoded.Bytes(), test.want) {
				t.Fatalf("dictionary wire = %x, want %x", encoded.Bytes(), test.want)
			}
		})
	}
}

func TestRuntimeEdgeWireSeparatesDefinitionMaskFromIDs(t *testing.T) {
	rows := []runtimeCallRow{
		{edgeID: 1, edgeDefinition: true, edgeReady: true, caller: StableSymbol(1), operationID: 10, callee: StableSymbol(2), count: 1},
		{edgeID: 1, edgeReady: true, caller: StableSymbol(1), operationID: 10, callee: StableSymbol(2), count: 1},
		{edgeID: 2, edgeDefinition: true, edgeReady: true, caller: StableSymbol(1), operationID: 20, callee: StableSymbol(3), count: 1},
		{edgeID: 2, edgeReady: true, caller: StableSymbol(1), operationID: 20, callee: StableSymbol(3), count: 1},
	}
	var encoded bytes.Buffer
	if err := encodeEventPayload(&encoded, Event{
		Type: EventRuntimeCall, RuntimeCall: &RuntimeCallEvent{}, runtimeCalls: rows,
	}, map[uint64]uint64{1: 1, 2: 2, 3: 3}); err != nil {
		t.Fatal(err)
	}
	want := readWireGolden(t, "runtime-edge-columnar.bin")
	if raw := encoded.Bytes(); !bytes.Equal(raw, want) {
		t.Fatalf("runtime edge payload = %x, want %x", raw, want)
	}
}
