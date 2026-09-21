package jhlog

import (
	"bytes"
	"testing"
)

func TestAgentMethodDefinitionPayloadRoundtrip(t *testing.T) {
	event := Event{
		Type: EventAgent, TimeUS: 900_000,
		Agent: &AgentEvent{
			SemanticType: AgentMethodDefinition, SchemaVersion: 1, Payload0: 44,
			MethodRef: LocalSymbol(40),
		},
	}
	var buf bytes.Buffer
	state := eventPayloadEncodeState{}
	if err := encodeEventPayloadWithState(&buf, event, stableAliasTable{}, &state); err != nil {
		t.Fatal(err)
	}
	t.Logf("payload hex: % x", buf.Bytes())
	reader := recordReader{data: buf.Bytes()}
	decoded := Event{Type: EventAgent}
	segment := &segmentDecodeState{}
	scratch := make([]uint64, 16)
	if err := decodeAgentPayload(&reader, &decoded, "main", segment, scratch); err != nil {
		t.Fatal(err)
	}
	if reader.Len() != 0 {
		t.Fatalf("left %d bytes: % x", reader.Len(), reader.data[reader.offset:])
	}
}
