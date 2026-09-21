package jhlog

import "fmt"

func (encoder eventPayloadEncoder) encodeHTTP(payload *HTTPEvent) error {
	if payload == nil {
		return fmt.Errorf("http payload is nil")
	}
	statusCode := effectiveHTTPStatusCode(payload)
	if err := validateHTTPEvent(payload, statusCode); err != nil {
		return err
	}
	if err := encoder.writeRefs(payload.RouteRef, payload.ServiceRef, payload.InitiatorRef); err != nil {
		return err
	}
	return encoder.writeValues(
		payload.DurationMS,
		payload.QueueMS,
		payload.DNSMS,
		payload.ConnectMS,
		payload.TLSMS,
		payload.RequestMS,
		payload.TTFBMS,
		payload.ResponseMS,
		uint64(statusCode),
		uint64(payload.FailurePhase),
		uint64(payload.FailureKind),
		uint64(payload.Protocol),
		payload.RxBytes,
		payload.TxBytes,
		uint64(payload.Attempts),
		uint64(payload.DNSAttempts),
		uint64(payload.ConnectAttempts),
		uint64(payload.TLSAttempts),
		uint64(payload.ConnectFailures),
		uint64(payload.TLSFailures),
		uint64(payload.Redirects),
	)
}

func (encoder eventPayloadEncoder) encodeOperation(payload *OperationEvent) error {
	if payload == nil {
		return fmt.Errorf("operation payload is nil")
	}
	if err := validateOperationEvent(payload); err != nil {
		return err
	}
	if err := writeSymbolRef(encoder.writer, payload.NameRef, encoder.stableAliases); err != nil {
		return err
	}
	if err := encoder.writeValues(
		payload.ID,
		payload.ParentID,
		uint64(payload.Phase),
		uint64(payload.Kind),
		uint64(payload.Outcome),
		payload.DurationUS,
		payload.BudgetUS,
		uint64(len(payload.Attributes)),
	); err != nil {
		return err
	}
	for _, attribute := range payload.Attributes {
		if err := encoder.writeRefs(attribute.KeyRef, attribute.ValueRef); err != nil {
			return err
		}
	}
	return nil
}

func (encoder eventPayloadEncoder) encodeIO(payload *IOEvent, semanticFlags uint64) error {
	if payload == nil {
		return fmt.Errorf("I/O payload is nil")
	}
	if err := validateIOEvent(payload, semanticFlags); err != nil {
		return err
	}
	if err := writeSymbolRef(encoder.writer, payload.SourceRef, encoder.stableAliases); err != nil {
		return err
	}
	return encoder.writeValues(
		uint64(payload.Operation),
		uint64(payload.Outcome),
		payload.DurationUS,
		payload.Bytes,
	)
}

func (encoder eventPayloadEncoder) encodeWorker(payload *WorkerEvent, semanticFlags uint64) error {
	if payload == nil {
		return fmt.Errorf("worker payload is nil")
	}
	if err := validateWorkerEvent(payload, semanticFlags); err != nil {
		return err
	}
	if err := writeSymbolRef(encoder.writer, payload.WorkerRef, encoder.stableAliases); err != nil {
		return err
	}
	return encoder.writeValues(
		payload.InstanceID,
		uint64(payload.Stage),
		uint64(payload.Outcome),
		payload.DurationMS,
		uint64(payload.RunAttempt),
		uint64(payload.Generation),
		uint64(payload.StopReason),
	)
}

func (encoder eventPayloadEncoder) encodeWebSocket(payload *WebSocketEvent) error {
	if payload == nil {
		return fmt.Errorf("WebSocket payload is nil")
	}
	if err := validateWebSocketEvent(payload); err != nil {
		return err
	}
	if err := writeSymbolRef(encoder.writer, payload.RouteRef, encoder.stableAliases); err != nil {
		return err
	}
	return encoder.writeValues(
		payload.ConnectionID,
		uint64(payload.Stage),
		payload.DurationMS,
		uint64(payload.StatusCode),
		uint64(payload.CloseCode),
		uint64(payload.FailureKind),
		payload.TextMessages,
		payload.BinaryMessages,
		payload.ReceivedBytes,
		uint64(payload.ReconnectOrdinal),
	)
}

func (encoder eventPayloadEncoder) encodeAndroidComponent(payload *AndroidComponentEvent) error {
	if payload == nil {
		return fmt.Errorf("android component payload is nil")
	}
	if err := validateAndroidComponentEvent(payload); err != nil {
		return err
	}
	if err := encoder.writeRefs(payload.ComponentRef, payload.ActionRef); err != nil {
		return err
	}
	return encoder.writeValues(
		payload.InstanceID,
		payload.FlowID,
		uint64(payload.Kind),
		uint64(payload.Stage),
		uint64(payload.Outcome),
		payload.DurationUS,
		uint64(payload.Flags),
	)
}

func (encoder eventPayloadEncoder) encodeBinderTransaction(
	payload *BinderTransactionEvent,
	semanticFlags uint64,
) error {
	if payload == nil {
		return fmt.Errorf("binder transaction payload is nil")
	}
	if err := validateBinderTransactionEvent(payload, semanticFlags); err != nil {
		return err
	}
	if err := encoder.writeRefs(payload.DescriptorRef, payload.MethodRef); err != nil {
		return err
	}
	return encoder.writeValues(
		payload.CallID,
		uint64(payload.Direction),
		uint64(payload.TransactionCode),
		uint64(payload.Outcome),
		uint64(payload.FailureKind),
		payload.DurationUS,
		uint64(payload.Flags),
	)
}
