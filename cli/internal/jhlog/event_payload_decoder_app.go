package jhlog

import (
	"fmt"
	"math"
)

func decodeHTTPPayload(
	reader *recordReader,
	event *Event,
	symbolNamespace string,
	segmentState *segmentDecodeState,
	valueScratch []uint64,
) error {
	route, err := readPayloadRef(reader, symbolNamespace, segmentState, "route")
	if err != nil {
		return err
	}
	service, err := readPayloadRef(reader, symbolNamespace, segmentState, "service")
	if err != nil {
		return err
	}
	initiator, err := readPayloadRef(reader, symbolNamespace, segmentState, "initiator")
	if err != nil {
		return err
	}
	values, err := readPayloadValues(reader, valueScratch,
		"duration", "queue", "DNS", "connect", "TLS", "request", "TTFB", "response",
		"status code", "failure phase", "failure kind", "protocol", "rx bytes", "tx bytes",
		"attempts", "DNS attempts", "connect attempts", "TLS attempts", "connect failures", "TLS failures",
		"redirects",
	)
	if err != nil {
		return err
	}
	for index, value := range values[14:21] {
		if value > math.MaxUint16 {
			return fmt.Errorf("HTTP count %d value %d exceeds %d", index, value, uint64(math.MaxUint16))
		}
	}
	if values[8] > math.MaxUint16 {
		return fmt.Errorf("HTTP status code %d exceeds %d", values[8], uint64(math.MaxUint16))
	}
	httpEvent := &HTTPEvent{
		RouteRef: route, ServiceRef: service, InitiatorRef: initiator,
		DurationMS: values[0], QueueMS: values[1], DNSMS: values[2], ConnectMS: values[3],
		TLSMS: values[4], RequestMS: values[5], TTFBMS: values[6], ResponseMS: values[7],
		StatusCode: uint16(values[8]), FailurePhase: HTTPFailurePhase(values[9]),
		FailureKind: HTTPFailureKind(values[10]), Protocol: HTTPProtocol(values[11]),
		RxBytes: values[12], TxBytes: values[13], Attempts: uint16(values[14]),
		DNSAttempts: uint16(values[15]), ConnectAttempts: uint16(values[16]),
		TLSAttempts: uint16(values[17]), ConnectFailures: uint16(values[18]),
		TLSFailures: uint16(values[19]), Redirects: uint16(values[20]),
	}
	httpEvent.Status = StatusClassForHTTPCode(httpEvent.StatusCode)
	if err := validateHTTPEvent(httpEvent, httpEvent.StatusCode); err != nil {
		return err
	}
	event.HTTP = httpEvent
	return nil
}

func decodeOperationPayload(
	reader *recordReader,
	event *Event,
	symbolNamespace string,
	segmentState *segmentDecodeState,
	valueScratch []uint64,
) error {
	nameRef, err := readPayloadRef(reader, symbolNamespace, segmentState, "operation name")
	if err != nil {
		return err
	}
	values, err := readPayloadValues(reader, valueScratch,
		"operation ID",
		"operation parent ID",
		"operation phase",
		"operation kind",
		"operation outcome",
		"operation duration",
		"operation budget",
		"operation attribute count",
	)
	if err != nil {
		return err
	}
	if values[7] > MaxOperationAttributes {
		return fmt.Errorf("operation attribute count %d exceeds %d", values[7], MaxOperationAttributes)
	}
	operation := &OperationEvent{
		NameRef: nameRef,
		ID:      values[0], ParentID: values[1], Phase: OperationPhase(values[2]),
		Kind: OperationKind(values[3]), Outcome: OperationOutcome(values[4]),
		DurationUS: values[5], BudgetUS: values[6],
	}
	if values[7] > 0 {
		operation.Attributes = make([]OperationAttribute, int(values[7]))
		for index := range operation.Attributes {
			keyRef, readErr := readPayloadRef(reader, symbolNamespace, segmentState, "operation attribute key")
			if readErr != nil {
				return readErr
			}
			valueRef, readErr := readPayloadRef(reader, symbolNamespace, segmentState, "operation attribute value")
			if readErr != nil {
				return readErr
			}
			operation.Attributes[index] = OperationAttribute{KeyRef: keyRef, ValueRef: valueRef}
		}
	}
	if err := validateOperationEvent(operation); err != nil {
		return err
	}
	event.Operation = operation
	return nil
}

func decodeIOPayload(
	reader *recordReader,
	event *Event,
	symbolNamespace string,
	segmentState *segmentDecodeState,
	valueScratch []uint64,
) error {
	sourceRef, err := readPayloadRef(reader, symbolNamespace, segmentState, "I/O source")
	if err != nil {
		return err
	}
	values, err := readPayloadValues(reader, valueScratch, "I/O operation", "I/O outcome", "I/O duration", "I/O bytes")
	if err != nil {
		return err
	}
	ioEvent := &IOEvent{
		SourceRef: sourceRef, Operation: IOOperationKind(values[0]), Outcome: IOOutcome(values[1]),
		DurationUS: values[2], Bytes: values[3],
	}
	if err := validateIOEvent(ioEvent, event.Flags); err != nil {
		return err
	}
	event.IO = ioEvent
	return nil
}

func decodeWorkerPayload(
	reader *recordReader,
	event *Event,
	symbolNamespace string,
	segmentState *segmentDecodeState,
	valueScratch []uint64,
) error {
	workerRef, err := readPayloadRef(reader, symbolNamespace, segmentState, "worker")
	if err != nil {
		return err
	}
	values, err := readPayloadValues(reader, valueScratch,
		"worker instance ID", "worker stage", "worker outcome", "worker duration",
		"worker run attempt", "worker generation", "worker stop reason",
	)
	if err != nil {
		return err
	}
	if values[4] > math.MaxUint32 || values[5] > math.MaxUint32 || values[6] > math.MaxUint32 {
		return fmt.Errorf("worker count or stop reason exceeds %d", uint64(math.MaxUint32))
	}
	worker := &WorkerEvent{
		WorkerRef: workerRef, InstanceID: values[0], Stage: WorkerStage(values[1]),
		Outcome: WorkerOutcome(values[2]), DurationMS: values[3], RunAttempt: uint32(values[4]),
		Generation: uint32(values[5]), StopReason: uint32(values[6]),
	}
	if err := validateWorkerEvent(worker, event.Flags); err != nil {
		return err
	}
	event.Worker = worker
	return nil
}

func decodeWebSocketPayload(
	reader *recordReader,
	event *Event,
	symbolNamespace string,
	segmentState *segmentDecodeState,
	valueScratch []uint64,
) error {
	routeRef, err := readPayloadRef(reader, symbolNamespace, segmentState, "WebSocket route")
	if err != nil {
		return err
	}
	values, err := readPayloadValues(reader, valueScratch,
		"WebSocket connection ID", "WebSocket stage", "WebSocket duration",
		"WebSocket status code", "WebSocket close code", "WebSocket failure kind",
		"WebSocket text messages", "WebSocket binary messages", "WebSocket received bytes",
		"WebSocket reconnect ordinal",
	)
	if err != nil {
		return err
	}
	if values[3] > math.MaxUint16 || values[4] > math.MaxUint16 || values[9] > math.MaxUint32 {
		return fmt.Errorf("WebSocket status, close code or reconnect ordinal exceeds wire bounds")
	}
	webSocket := &WebSocketEvent{
		RouteRef: routeRef, ConnectionID: values[0], Stage: WebSocketStage(values[1]),
		DurationMS: values[2], StatusCode: uint16(values[3]), CloseCode: uint16(values[4]),
		FailureKind: WebSocketFailureKind(values[5]), TextMessages: values[6],
		BinaryMessages: values[7], ReceivedBytes: values[8], ReconnectOrdinal: uint32(values[9]),
	}
	if err := validateWebSocketEvent(webSocket); err != nil {
		return err
	}
	event.WebSocket = webSocket
	return nil
}

func decodeAndroidComponentPayload(
	reader *recordReader,
	event *Event,
	symbolNamespace string,
	segmentState *segmentDecodeState,
	valueScratch []uint64,
) error {
	componentRef, err := readPayloadRef(reader, symbolNamespace, segmentState, "Android component")
	if err != nil {
		return err
	}
	actionRef, err := readPayloadRef(reader, symbolNamespace, segmentState, "Android component action")
	if err != nil {
		return err
	}
	values, err := readPayloadValues(reader, valueScratch,
		"Android component instance ID",
		"Android component flow ID",
		"Android component kind",
		"Android component stage",
		"Android component outcome",
		"Android component duration",
		"Android component flags",
	)
	if err != nil {
		return err
	}
	component := &AndroidComponentEvent{
		ComponentRef: componentRef, ActionRef: actionRef, InstanceID: values[0], FlowID: values[1],
		Kind: ComponentKind(values[2]), Stage: ComponentStage(values[3]),
		Outcome: ComponentOutcome(values[4]), DurationUS: values[5], Flags: ComponentFlag(values[6]),
	}
	if err := validateAndroidComponentEvent(component); err != nil {
		return err
	}
	event.AndroidComponent = component
	return nil
}

func decodeBinderTransactionPayload(
	reader *recordReader,
	event *Event,
	symbolNamespace string,
	segmentState *segmentDecodeState,
	valueScratch []uint64,
) error {
	descriptorRef, err := readPayloadRef(reader, symbolNamespace, segmentState, "Binder descriptor")
	if err != nil {
		return err
	}
	methodRef, err := readPayloadRef(reader, symbolNamespace, segmentState, "Binder method")
	if err != nil {
		return err
	}
	values, err := readPayloadValues(reader, valueScratch,
		"Binder call ID",
		"Binder direction",
		"Binder transaction code",
		"Binder outcome",
		"Binder failure kind",
		"Binder duration",
		"Binder flags",
	)
	if err != nil {
		return err
	}
	if values[2] > math.MaxUint32 {
		return fmt.Errorf("binder transaction code exceeds %d", uint64(math.MaxUint32))
	}
	binder := &BinderTransactionEvent{
		DescriptorRef: descriptorRef, MethodRef: methodRef, CallID: values[0],
		Direction: BinderDirection(values[1]), TransactionCode: uint32(values[2]),
		Outcome: BinderOutcome(values[3]), FailureKind: BinderFailureKind(values[4]),
		DurationUS: values[5], Flags: BinderFlag(values[6]),
	}
	if err := validateBinderTransactionEvent(binder, event.Flags); err != nil {
		return err
	}
	event.BinderTransaction = binder
	return nil
}
