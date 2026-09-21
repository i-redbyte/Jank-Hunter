package jhlog

import "testing"

var decodedMemorySink *MemoryEvent

func TestMemoryPayloadDecodeAllocatesOnlyRetainedModel(t *testing.T) {
	state := decodeSegmentState(nil)
	payload := []byte{1, 2, 3}
	checksum := uint64(0)
	allocations := testing.AllocsPerRun(1_000, func() {
		reader := recordReader{data: payload}
		event := Event{Type: EventMemory}
		if err := decodeEventPayload(&reader, &event, "", "", nil, state); err != nil {
			panic(err)
		}
		decodedMemorySink = event.Memory
		checksum += decodedMemorySink.PSSKB
	})
	if allocations != 1 {
		t.Fatalf("memory payload decode allocations = %.2f, want retained model only", allocations)
	}
	if checksum == 0 {
		t.Fatal("decoded payload was not consumed")
	}
}
