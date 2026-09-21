package jhlog

import "fmt"

func decodeAgentPayload(
	reader *recordReader,
	event *Event,
	symbolNamespace string,
	segmentState *segmentDecodeState,
	valueScratch []uint64,
) error {
	prefix, err := readPayloadValues(reader, valueScratch, "agent semantic type", "agent schema version")
	if err != nil {
		return err
	}
	if prefix[1] != 1 {
		return fmt.Errorf("unsupported agent event schema %d", prefix[1])
	}
	values, err := readPayloadValues(
		reader,
		valueScratch,
		"agent producer sequence",
		"agent producer id",
		"agent thread token",
		"agent context token",
		"agent flags",
		"agent payload 0",
		"agent payload 1",
		"agent payload 2",
		"agent payload 3",
	)
	if err != nil {
		return err
	}
	agent := &AgentEvent{
		SemanticType:     AgentSemanticType(prefix[0]),
		SchemaVersion:    prefix[1],
		ProducerSequence: values[0],
		ProducerID:       values[1],
		ThreadToken:      values[2],
		ContextToken:     values[3],
		EventFlags:       values[4],
		Payload0:         values[5],
		Payload1:         values[6],
		Payload2:         values[7],
		Payload3:         values[8],
	}
	if agent.SemanticType == AgentMethodDefinition && reader.Len() > 0 {
		ref, readErr := readPayloadRef(reader, symbolNamespace, segmentState, "agent method")
		if readErr != nil {
			return readErr
		}
		agent.MethodRef = ref
	}
	event.Agent = agent
	return nil
}
