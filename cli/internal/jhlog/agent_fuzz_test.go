package jhlog

import (
	"bytes"
	"testing"
)

func FuzzAgentPayloadNeverPanics(f *testing.F) {
	seeds := [][]byte{
		{},
		{byte(AgentGCInterval), 1, 1, 0, 0, 0, 0, 1, 2, 3, 4},
		{byte(AgentMethodDefinition), 1, 0, 0, 0, 0, 0, 1},
		{0xff, 0xff, 0xff, 0xff, 0x0f},
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input []byte) {
		event := Event{Type: EventAgent}
		_, _ = decodeEventPayload(bytes.NewReader(input), &event, DefaultSegmentHeader())
	})
}
