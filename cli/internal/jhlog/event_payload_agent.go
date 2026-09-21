package jhlog

import "fmt"

type AgentSemanticType uint64

const (
	AgentStatus AgentSemanticType = 1 + iota
	AgentCapability
	AgentQualitySnapshot
	AgentThreadStart
	AgentThreadEnd
	AgentGCInterval
	AgentMonitorContentionInterval
	AgentThreadStackSample
	AgentStackDefinition
	AgentClockSync
	AgentCorrelationLink
	AgentMethodDefinition
)

const AgentFlagContextDefinition uint64 = 1

// AgentEvent is the canonical, storage-independent fixed part of an ART TI event.
// Meaning of Payload0..3 is versioned by SemanticType and SchemaVersion.
type AgentEvent struct {
	SemanticType     AgentSemanticType `json:"semantic_type"`
	SchemaVersion    uint64            `json:"schema_version"`
	ProducerSequence uint64            `json:"producer_sequence,omitempty"`
	ProducerID       uint64            `json:"producer_id,omitempty"`
	ThreadToken      uint64            `json:"thread_token,omitempty"`
	ContextToken     uint64            `json:"context_token,omitempty"`
	EventFlags       uint64            `json:"event_flags,omitempty"`
	Payload0         uint64            `json:"payload0,omitempty"`
	Payload1         uint64            `json:"payload1,omitempty"`
	Payload2         uint64            `json:"payload2,omitempty"`
	Payload3         uint64            `json:"payload3,omitempty"`
	MethodRef        SymbolRef         `json:"method_ref,omitempty"`
}

func (encoder eventPayloadEncoder) encodeAgent(payload *AgentEvent) error {
	if payload == nil {
		return fmt.Errorf("agent payload is nil")
	}
	if payload.SchemaVersion != 1 {
		return fmt.Errorf("unsupported agent event schema %d", payload.SchemaVersion)
	}
	if err := encoder.writeValues(
		uint64(payload.SemanticType),
		payload.SchemaVersion,
		payload.ProducerSequence,
		payload.ProducerID,
		payload.ThreadToken,
		payload.ContextToken,
		payload.EventFlags,
		payload.Payload0,
		payload.Payload1,
		payload.Payload2,
		payload.Payload3,
	); err != nil {
		return err
	}
	if payload.SemanticType == AgentMethodDefinition && !payload.MethodRef.IsUnknown() {
		return encoder.writeRefs(payload.MethodRef)
	}
	return nil
}
