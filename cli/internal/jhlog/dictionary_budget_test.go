package jhlog

import (
	"strings"
	"testing"
)

func TestDictionaryDecodeBoundsExpandedPrefixesBeforeAllocation(t *testing.T) {
	state := decodeSegmentState(nil)
	previous := []byte(strings.Repeat("x", 4096))
	state.dictionaryPrevious[DictOwner] = previous
	// Model the already decoded history without allocating a quarter GiB in a unit test.
	state.dictionaryDecodedBytes = maxDecodedDictionaryBytes - uint64(len(previous))
	decode := func(id uint64) error {
		var data []byte
		for _, n := range []uint64{uint64(DictOwner), id, uint64(len(previous)) << 1, 0} {
			data = appendUvarint(data, n)
		}
		event := Event{Type: EventDictionary}
		return decodeEventPayload(&recordReader{data: data}, &event, "", "", nil, state)
	}
	if err := decode(1); err != nil {
		t.Fatalf("value fitting exactly at budget rejected: %v", err)
	}
	if err := decode(2); err == nil || !strings.Contains(err.Error(), "dictionary decoding limit") {
		t.Fatalf("tiny encoded prefix expanded beyond segment budget: %v", err)
	}
}

func TestDictionaryDecodeBoundsEmptyEntriesAndResetsPerSegment(t *testing.T) {
	state := decodeSegmentState(nil)
	state.dictionaryEntries = maxDecodedDictionaryEntries
	decode := func(state *segmentDecodeState) error {
		event := Event{Type: EventDictionary}
		return decodeEventPayload(&recordReader{data: []byte{byte(DictOwner), 1, 0, 0}}, &event, "", "", nil, state)
	}
	if err := decode(state); err == nil || !strings.Contains(err.Error(), "dictionary decoding limit") {
		t.Fatalf("empty values bypassed dictionary cardinality bound: %v", err)
	}
	if err := decode(decodeSegmentState(nil)); err != nil {
		t.Fatalf("new segment inherited exhausted budget: %v", err)
	}
}

func TestDictionaryDecodeBudgetIncludesTokenExpandedValues(t *testing.T) {
	state := decodeSegmentState(nil)
	state.dictionaryTokens = [][]byte{[]byte("Worker")}
	state.dictionaryDecodedBytes = maxDecodedDictionaryBytes - 5
	// A token reference and separator expand into seven bytes from two encoded atoms.
	data := []byte{byte(DictOwner), 1, 5, 4, 2}
	event := Event{Type: EventDictionary}
	if err := decodeEventPayload(&recordReader{data: data}, &event, "", "", nil, state); err == nil ||
		!strings.Contains(err.Error(), "dictionary decoding limit") {
		t.Fatalf("token value bypassed expanded byte limit: %v", err)
	}
	if event.Dictionary != nil || state.dictionaryDecodedBytes != maxDecodedDictionaryBytes-5 {
		t.Fatal("rejected value was published or consumed dictionary budget")
	}
}
