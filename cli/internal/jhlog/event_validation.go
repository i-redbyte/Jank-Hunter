package jhlog

import (
	"fmt"
	"math"
)

func validateOperationEvent(event *OperationEvent) error {
	if event.ID == 0 {
		return fmt.Errorf("operation ID must be non-zero")
	}
	if event.ParentID == event.ID {
		return fmt.Errorf("operation cannot be its own parent")
	}
	if event.NameRef.IsUnknown() {
		return fmt.Errorf("operation requires a name reference")
	}
	if event.Kind <= OperationKindUnknown || event.Kind > OperationKindStage {
		return fmt.Errorf("unsupported operation kind %d", event.Kind)
	}
	switch event.Phase {
	case OperationPhaseStarted:
		if event.Outcome != OperationOutcomeUnknown || event.DurationUS != 0 {
			return fmt.Errorf("started operation cannot have outcome or duration")
		}
	case OperationPhaseFinished:
		if event.Outcome <= OperationOutcomeUnknown || event.Outcome > OperationOutcomeTimeout {
			return fmt.Errorf("finished operation requires a supported outcome")
		}
	default:
		return fmt.Errorf("unsupported operation phase %d", event.Phase)
	}
	if len(event.Attributes) > MaxOperationAttributes {
		return fmt.Errorf("operation attribute count %d exceeds %d", len(event.Attributes), MaxOperationAttributes)
	}
	keys := [MaxOperationAttributes]SymbolRef{}
	for index, attribute := range event.Attributes {
		if attribute.KeyRef.IsUnknown() || attribute.ValueRef.IsUnknown() {
			return fmt.Errorf("operation attribute %d requires key and value references", index)
		}
		for previous := 0; previous < index; previous++ {
			if keys[previous] == attribute.KeyRef {
				return fmt.Errorf("operation attribute %d duplicates a key", index)
			}
		}
		keys[index] = attribute.KeyRef
	}
	return nil
}

func validateIOEvent(event *IOEvent, flags uint64) error {
	supportedOperation := event.Operation >= IOOperationFileRead && event.Operation <= IOOperationFileSync ||
		event.Operation >= IOOperationContentRead && event.Operation <= IOOperationContentWrite
	if !supportedOperation {
		return fmt.Errorf("unsupported I/O operation %d", event.Operation)
	}
	if event.Outcome > IOOutcomeFailure {
		return fmt.Errorf("unsupported I/O outcome %d", event.Outcome)
	}
	if event.Outcome == IOOutcomeUnknown {
		return fmt.Errorf("I/O outcome must be success or failure")
	}
	if event.Bytes > 0 && flags&uint64(FlagIOBytesKnown) == 0 {
		return fmt.Errorf("I/O bytes require bytes-known flag")
	}
	allowed := uint64(FlagThreadMain | FlagAppForeground | FlagIOBytesKnown)
	if unsupported := flags &^ allowed; unsupported != 0 {
		return fmt.Errorf("unsupported semantic flag 0x%x for I/O", unsupported)
	}
	return nil
}

func effectiveHTTPStatusCode(event *HTTPEvent) uint16 {
	if event.StatusCode != 0 {
		return event.StatusCode
	}
	switch event.Status {
	case Status1xx:
		return 100
	case Status2xx:
		return 200
	case Status3xx:
		return 300
	case Status4xx:
		return 400
	case Status5xx:
		return 500
	default:
		return 0
	}
}

func validateHTTPEvent(event *HTTPEvent, statusCode uint16) error {
	if statusCode != 0 && (statusCode < 100 || statusCode > 599) {
		return fmt.Errorf("HTTP status code %d is outside 100..599", statusCode)
	}
	statusClass := StatusClassForHTTPCode(statusCode)
	if event.Status > Status5xx {
		return fmt.Errorf("unsupported HTTP status class %d", event.Status)
	}
	if event.Status != StatusUnknown && event.Status != statusClass {
		return fmt.Errorf("HTTP status class %d conflicts with status code %d", event.Status, statusCode)
	}
	if err := validateHTTPPhase("queue", event.QueueMS, event.DurationMS); err != nil {
		return err
	}
	if err := validateHTTPPhase("DNS", event.DNSMS, event.DurationMS); err != nil {
		return err
	}
	if err := validateHTTPPhase("connect", event.ConnectMS, event.DurationMS); err != nil {
		return err
	}
	if err := validateHTTPPhase("TLS", event.TLSMS, event.DurationMS); err != nil {
		return err
	}
	if err := validateHTTPPhase("request", event.RequestMS, event.DurationMS); err != nil {
		return err
	}
	if err := validateHTTPPhase("TTFB", event.TTFBMS, event.DurationMS); err != nil {
		return err
	}
	if err := validateHTTPPhase("response", event.ResponseMS, event.DurationMS); err != nil {
		return err
	}
	if event.FailurePhase > HTTPFailurePhaseCancelled {
		return fmt.Errorf("unsupported HTTP failure phase %d", event.FailurePhase)
	}
	if event.FailureKind > HTTPFailureKindOther {
		return fmt.Errorf("unsupported HTTP failure kind %d", event.FailureKind)
	}
	if event.Protocol > HTTPProtocol3 {
		return fmt.Errorf("unsupported HTTP protocol %d", event.Protocol)
	}
	if event.ConnectFailures > event.ConnectAttempts {
		return fmt.Errorf(
			"HTTP connect failure count %d exceeds connect attempt count %d",
			event.ConnectFailures,
			event.ConnectAttempts,
		)
	}
	if event.TLSFailures > event.TLSAttempts {
		return fmt.Errorf(
			"HTTP TLS failure count %d exceeds TLS attempt count %d",
			event.TLSFailures,
			event.TLSAttempts,
		)
	}
	if event.Redirects > event.Attempts {
		return fmt.Errorf("HTTP redirect count %d exceeds attempt count %d", event.Redirects, event.Attempts)
	}
	return nil
}

func validateHTTPPhase(name string, phaseDuration, requestDuration uint64) error {
	if phaseDuration > requestDuration {
		return fmt.Errorf("HTTP %s duration %d exceeds request duration %d", name, phaseDuration, requestDuration)
	}
	return nil
}

func validateWorkerEvent(event *WorkerEvent, flags uint64) error {
	if event.InstanceID == 0 {
		return fmt.Errorf("worker instance ID must be non-zero")
	}
	if event.Stage <= WorkerStageUnknown || event.Stage > WorkerStageFinished {
		return fmt.Errorf("unsupported worker stage %d", event.Stage)
	}
	if event.Outcome > WorkerOutcomeCancelled {
		return fmt.Errorf("unsupported worker outcome %d", event.Outcome)
	}
	finished := event.Stage == WorkerStageFinished
	if !finished && event.Outcome != WorkerOutcomeUnknown {
		return fmt.Errorf("worker outcome is only valid for finished stage")
	}
	if !finished && event.DurationMS != 0 {
		return fmt.Errorf("worker duration is only valid for finished stage")
	}
	if finished && event.Outcome == WorkerOutcomeUnknown {
		return fmt.Errorf("finished worker requires an outcome")
	}
	if !finished && flags&uint64(FlagWorkerStopReasonKnown) != 0 {
		return fmt.Errorf("worker stop reason is only valid for finished stage")
	}
	if flags&uint64(FlagWorkerStopReasonKnown) == 0 && event.StopReason != 0 {
		return fmt.Errorf("worker stop reason requires the known flag")
	}
	if event.Stage != WorkerStageEnqueued && event.WorkerRef.ID == 0 {
		return fmt.Errorf("started or finished worker requires a worker reference")
	}
	return nil
}

func validateWebSocketEvent(event *WebSocketEvent) error {
	if event.ConnectionID == 0 {
		return fmt.Errorf("WebSocket connection ID must be non-zero")
	}
	if event.Stage <= WebSocketStageUnknown || event.Stage > WebSocketStageFailed {
		return fmt.Errorf("unsupported WebSocket stage %d", event.Stage)
	}
	if event.StatusCode != 0 && (event.StatusCode < 100 || event.StatusCode > 599) {
		return fmt.Errorf("WebSocket status code %d is outside 100..599", event.StatusCode)
	}
	if event.Stage != WebSocketStageClosed && event.CloseCode != 0 {
		return fmt.Errorf("WebSocket close code is only valid for closed stage")
	}
	if event.CloseCode != 0 && (event.CloseCode < 1000 || event.CloseCode > 4999) {
		return fmt.Errorf("WebSocket close code %d is outside 1000..4999", event.CloseCode)
	}
	if event.Stage != WebSocketStageFailed && event.FailureKind != WebSocketFailureUnknown {
		return fmt.Errorf("WebSocket failure kind is only valid for failed stage")
	}
	if event.Stage == WebSocketStageFailed && event.FailureKind == WebSocketFailureUnknown {
		return fmt.Errorf("failed WebSocket requires a failure kind")
	}
	if event.FailureKind > WebSocketFailureOther {
		return fmt.Errorf("unsupported WebSocket failure kind %d", event.FailureKind)
	}
	if event.Stage == WebSocketStageOpened &&
		(event.TextMessages != 0 || event.BinaryMessages != 0 || event.ReceivedBytes != 0) {
		return fmt.Errorf("WebSocket traffic is only valid for terminal stages")
	}
	return nil
}

func validateDatabaseEvent(event *DatabaseEvent) error {
	if event.SourceRef.ID == 0 {
		return fmt.Errorf("database source is required")
	}
	if event.Framework <= DatabaseFrameworkUnknown || event.Framework > DatabaseFrameworkCustom {
		return fmt.Errorf("unsupported database framework %d", event.Framework)
	}
	if event.Operation <= DatabaseOperationUnknown || event.Operation > DatabaseOperationStatement {
		return fmt.Errorf("unsupported database operation %d", event.Operation)
	}
	if event.Outcome <= DatabaseOutcomeUnknown || event.Outcome > DatabaseOutcomeFailure {
		return fmt.Errorf("unsupported database outcome %d", event.Outcome)
	}
	if event.Boundary <= DatabaseBoundaryUnknown || event.Boundary > DatabaseBoundaryManual {
		return fmt.Errorf("database boundary is required and must be supported, got %d", event.Boundary)
	}
	if event.FailureKind > DatabaseFailureOther {
		return fmt.Errorf("unsupported database failure kind %d", event.FailureKind)
	}
	if event.Outcome == DatabaseOutcomeSuccess && event.FailureKind != DatabaseFailureNone {
		return fmt.Errorf("successful database call cannot have failure kind")
	}
	if event.Outcome == DatabaseOutcomeFailure && event.FailureKind == DatabaseFailureNone {
		return fmt.Errorf("failed database call requires failure kind")
	}
	if !event.ResultKnown {
		if event.ResultKind != DatabaseResultUnknown || event.ResultCountBucket != DatabaseCountUnknown {
			return fmt.Errorf("database result fields require known flag")
		}
	} else {
		if event.ResultKind <= DatabaseResultUnknown || event.ResultKind > DatabaseResultAffectedRows {
			return fmt.Errorf("known database result requires supported kind")
		}
		if event.ResultCountBucket <= DatabaseCountUnknown || event.ResultCountBucket > DatabaseCountOverHundred {
			return fmt.Errorf("known database result requires count bucket")
		}
	}
	if event.PhaseMask & ^(DatabasePhasePoolWait|DatabasePhaseLockWait|DatabasePhaseExecute|DatabasePhaseMaterialize) != 0 {
		return fmt.Errorf("unsupported database phase mask 0x%x", event.PhaseMask)
	}
	phases := [...]struct {
		bit   DatabasePhase
		value uint64
	}{
		{DatabasePhasePoolWait, event.PoolWaitUS},
		{DatabasePhaseLockWait, event.LockWaitUS},
		{DatabasePhaseExecute, event.ExecuteUS},
		{DatabasePhaseMaterialize, event.MaterializeUS},
	}
	remaining := event.DurationUS
	for _, phase := range phases {
		if event.PhaseMask&phase.bit == 0 && phase.value != 0 {
			return fmt.Errorf("database phase value %d is present without mask 0x%x", phase.value, phase.bit)
		}
		if phase.value > remaining {
			return fmt.Errorf("database phases exceed total duration")
		}
		remaining -= phase.value
	}
	return nil
}

func validateDatabaseTransactionEvent(event *DatabaseTransactionEvent) error {
	if event.SourceRef.ID == 0 {
		return fmt.Errorf("database transaction source is required")
	}
	if event.TransactionID == 0 {
		return fmt.Errorf("database transaction ID is required")
	}
	if event.ParentID == event.TransactionID {
		return fmt.Errorf("database transaction cannot be its own parent")
	}
	if event.Stage <= DatabaseTransactionStageUnknown || event.Stage > DatabaseTransactionTerminal {
		return fmt.Errorf("unsupported database transaction stage %d", event.Stage)
	}
	if event.Mode > DatabaseTransactionReadOnly {
		return fmt.Errorf("unsupported database transaction mode %d", event.Mode)
	}
	if event.FailureKind > DatabaseFailureOther {
		return fmt.Errorf("unsupported database transaction failure kind %d", event.FailureKind)
	}
	if event.Stage == DatabaseTransactionBegin {
		if event.Outcome != DatabaseTransactionOutcomeUnknown || event.FailureKind != DatabaseFailureNone ||
			event.DurationUS != 0 || event.StatementCount != 0 || event.ReadCount != 0 || event.WriteCount != 0 {
			return fmt.Errorf("database transaction begin cannot contain terminal fields")
		}
		return nil
	}
	if event.Outcome <= DatabaseTransactionOutcomeUnknown || event.Outcome > DatabaseTransactionFailure {
		return fmt.Errorf("database transaction terminal requires supported outcome")
	}
	if event.Outcome == DatabaseTransactionFailure && event.FailureKind == DatabaseFailureNone {
		return fmt.Errorf("failed database transaction requires failure kind")
	}
	if event.Outcome != DatabaseTransactionFailure && event.FailureKind != DatabaseFailureNone {
		return fmt.Errorf("non-failed database transaction cannot have failure kind")
	}
	if event.ReadCount > event.StatementCount || event.WriteCount > event.StatementCount-event.ReadCount {
		return fmt.Errorf("database transaction read/write counts exceed statement count")
	}
	return nil
}

func validateProcessStateEvent(event *ProcessStateEvent) error {
	if event.UIVisibility > ProcessUIVisible {
		return fmt.Errorf("unsupported process UI visibility %d", event.UIVisibility)
	}
	if event.Importance > ProcessImportanceCached {
		return fmt.Errorf("unsupported process importance %d", event.Importance)
	}
	if event.Reason < ProcessStateReasonPeriodicSample || event.Reason > ProcessStateReasonComponentLifecycle {
		return fmt.Errorf("unsupported process state reason %d", event.Reason)
	}
	return nil
}

func validateAndroidComponentEvent(event *AndroidComponentEvent) error {
	if event.ComponentRef.ID == 0 {
		return fmt.Errorf("android component reference is required")
	}
	if event.InstanceID == 0 || event.FlowID == 0 {
		return fmt.Errorf("android component instance and flow IDs must be non-zero")
	}
	if !validComponentStage(event.Kind, event.Stage) {
		return fmt.Errorf("unsupported Android component kind/stage %d/%d", event.Kind, event.Stage)
	}
	if event.Outcome > ComponentOutcomeCancelled {
		return fmt.Errorf("unsupported Android component outcome %d", event.Outcome)
	}
	if unsupported := event.Flags &^ componentFlagKnownMask; unsupported != 0 {
		return fmt.Errorf("unsupported Android component flags 0x%x", unsupported)
	}
	return nil
}

func validComponentStage(kind ComponentKind, stage ComponentStage) bool {
	switch kind {
	case ComponentKindService:
		return stage >= ComponentServiceCreated && stage <= ComponentServiceTimeout
	case ComponentKindReceiver:
		return stage >= ComponentReceiverStarted && stage <= ComponentReceiverFinished
	default:
		return false
	}
}

func validateBinderTransactionEvent(event *BinderTransactionEvent, semanticFlags uint64) error {
	if event.CallID == 0 {
		return fmt.Errorf("binder transaction call ID must be non-zero")
	}
	if event.Direction < BinderDirectionClient || event.Direction > BinderDirectionServer {
		return fmt.Errorf("unsupported Binder direction %d", event.Direction)
	}
	if event.Outcome < BinderOutcomeSuccess || event.Outcome > BinderOutcomeUnhandled {
		return fmt.Errorf("unsupported Binder outcome %d", event.Outcome)
	}
	if event.FailureKind > BinderFailureOther {
		return fmt.Errorf("unsupported Binder failure kind %d", event.FailureKind)
	}
	if event.Outcome == BinderOutcomeFailure && event.FailureKind == BinderFailureNone {
		return fmt.Errorf("failed Binder transaction requires failure kind")
	}
	if event.Outcome != BinderOutcomeFailure && event.FailureKind != BinderFailureNone {
		return fmt.Errorf("non-failed Binder transaction cannot have failure kind")
	}
	if unsupported := event.Flags &^ binderFlagKnownMask; unsupported != 0 {
		return fmt.Errorf("unsupported Binder flags 0x%x", unsupported)
	}
	allowedSemanticFlags := uint64(FlagThreadMain | FlagAppForeground)
	if unsupported := semanticFlags &^ allowedSemanticFlags; unsupported != 0 {
		return fmt.Errorf("unsupported semantic flags 0x%x for Binder", unsupported)
	}
	return nil
}

func validateUIWindow(window *UIWindowEvent) error {
	if window.WindowMS == 0 {
		return fmt.Errorf("UI window duration must be positive")
	}
	if window.JankCount > window.FrameCount {
		return fmt.Errorf("UI jank count %d exceeds frame count %d", window.JankCount, window.FrameCount)
	}
	if window.Source <= UIFrameSourceUnknown || window.Source > UIFrameSourceChoreographer {
		return fmt.Errorf("unsupported UI frame source %d", window.Source)
	}
	if window.FrameDeadlineUS == 0 {
		return fmt.Errorf("UI frame deadline must be positive")
	}
	if len(window.FrameDurationBuckets) != UIFrameHistogramBucketCount {
		return fmt.Errorf("UI frame histogram has %d buckets; want %d", len(window.FrameDurationBuckets), UIFrameHistogramBucketCount)
	}
	total := uint64(0)
	for _, count := range window.FrameDurationBuckets {
		if math.MaxUint64-total < count {
			return fmt.Errorf("UI frame histogram count overflow")
		}
		total += count
	}
	if total != window.FrameCount {
		return fmt.Errorf("UI frame histogram count %d differs from frame count %d", total, window.FrameCount)
	}
	return nil
}
