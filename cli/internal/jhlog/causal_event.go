package jhlog

import "fmt"

type CausalEntityKind uint64

const (
	CausalNone CausalEntityKind = iota
	CausalThread
	CausalExecutorTask
	CausalHandlerPost
	CausalCoroutine
	CausalWorkerAttempt
	CausalResource
	CausalOperation
	CausalSeries
	CausalExecutor
	CausalDirectExecution
)

type CausalSourceOrigin uint64

const (
	CausalOriginUnknown CausalSourceOrigin = iota
	CausalOriginProject
	CausalOriginDependency
)

const (
	CausalHasTargetJavaID uint64 = 1 << iota
	CausalHasTargetTID
	CausalHasSource
	CausalHasAppCaller
	CausalHasCPUNanos
	CausalHasDurationNanos
	CausalHasReadBytes
	CausalHasWrittenBytes
	CausalHasScheduledFor
	CausalHasQueueEntered
	CausalHasOccurrence
	CausalHasWorkerBinding
	CausalHasResourceKind
	CausalHasOperationKind
	CausalHasCallResult
	CausalHasTerminalOutcome
	CausalHasLeaseID
	CausalHasCoroutineBinding
	CausalHasLeaseScope
	CausalHasRequestedPeriod
	CausalHasScheduleCadence
	CausalHasRequestedDelay
)

const causalKnownPresence = (1 << 22) - 1

const (
	CausalOperationFileRead uint64 = 1 + iota
	CausalOperationFileWrite
	CausalOperationFileSync
	CausalOperationDatabase
	CausalOperationHTTP
	CausalOperationLockWait
	CausalOperationMonitorWait
	CausalOperationWakeLock
	CausalOperationContentRead
	CausalOperationContentWrite
)

const (
	CausalResourceAutoCloseable uint64 = 1 + iota
	CausalResourceLock
	CausalResourceMonitor
	CausalResourceWakeLock
)

// CausalEvent is a point observation, never an inferred lifecycle interval.
// Presence distinguishes measured zero from an unavailable value.
type CausalEvent struct {
	Version              uint64             `json:"version"`
	EntityKind           CausalEntityKind   `json:"entity_kind"`
	Phase                uint64             `json:"phase"`
	EntityID             uint64             `json:"entity_id,string"`
	ParentKind           CausalEntityKind   `json:"parent_kind"`
	ParentID             uint64             `json:"parent_id,string,omitempty"`
	SchedulerKind        CausalEntityKind   `json:"scheduler_kind"`
	SchedulerID          uint64             `json:"scheduler_id,string,omitempty"`
	ActorKind            CausalEntityKind   `json:"actor_kind"`
	ActorID              uint64             `json:"actor_id,string,omitempty"`
	ObservedElapsedNanos uint64             `json:"observed_elapsed_nanos,string"`
	SourceOrigin         CausalSourceOrigin `json:"source_origin"`
	Presence             uint64             `json:"presence"`
	TargetJavaID         uint64             `json:"target_java_id,string,omitempty"`
	TargetTID            uint64             `json:"target_tid,omitempty"`
	SourceSiteID         uint64             `json:"source_site_id,string,omitempty"`
	SourceMethod         string             `json:"source_method,omitempty"`
	SourceLine           uint64             `json:"source_line,omitempty"`
	AppCallerSiteID      uint64             `json:"app_caller_site_id,string,omitempty"`
	AppCallerMethod      string             `json:"app_caller_method,omitempty"`
	AppCallerLine        uint64             `json:"app_caller_line,omitempty"`
	CPUNanos             uint64             `json:"cpu_nanos,string,omitempty"`
	DurationNanos        uint64             `json:"duration_nanos,string,omitempty"`
	ReadBytes            uint64             `json:"read_bytes,string,omitempty"`
	WrittenBytes         uint64             `json:"written_bytes,string,omitempty"`
	ScheduledForNanos    uint64             `json:"scheduled_for_elapsed_nanos,string,omitempty"`
	QueueEnteredNanos    uint64             `json:"queue_entered_elapsed_nanos,string,omitempty"`
	Occurrence           uint64             `json:"occurrence,omitempty"`
	WorkerInstanceID     uint64             `json:"worker_instance_id,string,omitempty"`
	RunAttempt           uint64             `json:"run_attempt,omitempty"`
	Generation           uint64             `json:"generation,omitempty"`
	ResourceKind         uint64             `json:"resource_kind,omitempty"`
	OperationKind        uint64             `json:"operation_kind,omitempty"`
	CallResult           uint64             `json:"call_result,omitempty"`
	TerminalOutcome      uint64             `json:"terminal_outcome,omitempty"`
	LeaseID              uint64             `json:"lease_id,string,omitempty"`
	CoroutineToken       uint64             `json:"coroutine_token,string,omitempty"`
	CoroutineGeneration  uint64             `json:"coroutine_generation,omitempty"`
	LeaseScopeKind       CausalEntityKind   `json:"lease_scope_kind,omitempty"`
	LeaseScopeID         uint64             `json:"lease_scope_id,string,omitempty"`
	BoundToScope         uint64             `json:"bound_to_scope,omitempty"`
	RequestedPeriodNanos uint64             `json:"requested_period_nanos,string,omitempty"`
	ScheduleCadence      uint64             `json:"schedule_cadence,omitempty"`
	RequestedDelayNanos  uint64             `json:"requested_delay_nanos,string,omitempty"`
}

func validCausalRef(kind CausalEntityKind, id uint64) bool {
	return (kind == CausalNone && id == 0) || (kind > CausalNone && kind <= CausalDirectExecution && id > 0)
}

func validCausalPhase(kind CausalEntityKind, phase uint64) bool {
	if phase == 0 {
		return false
	}
	switch kind {
	case CausalThread:
		return phase <= 11
	case CausalExecutorTask:
		return phase <= 8
	case CausalResource:
		return phase <= 7
	case CausalHandlerPost:
		return phase <= 8
	case CausalExecutor:
		return phase <= 7
	case CausalCoroutine:
		return phase <= 6
	case CausalWorkerAttempt:
		return phase <= 6
	case CausalOperation:
		return phase == 1
	case CausalSeries:
		return phase <= 4
	case CausalDirectExecution:
		return phase <= 2
	default:
		return false
	}
}

func validateCausalEvent(v *CausalEvent) error {
	if v == nil || v.Version != 1 || v.EntityID == 0 || !validCausalPhase(v.EntityKind, v.Phase) {
		return fmt.Errorf("invalid causal event identity or phase")
	}
	if !validCausalRef(v.ParentKind, v.ParentID) || !validCausalRef(v.SchedulerKind, v.SchedulerID) || !validCausalRef(v.ActorKind, v.ActorID) {
		return fmt.Errorf("invalid causal relation")
	}
	if v.EntityKind == CausalDirectExecution && v.SchedulerID != 0 {
		return fmt.Errorf("direct execution cannot claim scheduler handoff")
	}
	if v.SourceOrigin > CausalOriginDependency || v.Presence&^uint64(causalKnownPresence) != 0 {
		return fmt.Errorf("unsupported causal origin or presence")
	}
	if v.Presence&CausalHasTargetJavaID != 0 && v.TargetJavaID == 0 ||
		v.Presence&CausalHasTargetTID != 0 && v.TargetTID == 0 {
		return fmt.Errorf("invalid causal target thread")
	}
	if v.Presence&CausalHasSource != 0 {
		if v.SourceMethod == "" || len(v.SourceMethod) > 512 {
			return fmt.Errorf("invalid causal source")
		}
	} else if v.SourceOrigin != CausalOriginUnknown || v.SourceSiteID != 0 || v.SourceMethod != "" || v.SourceLine != 0 {
		return fmt.Errorf("causal origin without source")
	}
	if v.Presence&CausalHasAppCaller != 0 {
		if v.AppCallerMethod == "" || len(v.AppCallerMethod) > 512 {
			return fmt.Errorf("invalid causal app caller")
		}
	} else if v.AppCallerSiteID != 0 || v.AppCallerMethod != "" || v.AppCallerLine != 0 {
		return fmt.Errorf("causal app caller without presence")
	}
	if v.Presence&CausalHasOccurrence != 0 && v.Occurrence == 0 ||
		v.Presence&CausalHasWorkerBinding != 0 && v.WorkerInstanceID == 0 ||
		v.Presence&CausalHasCoroutineBinding != 0 && v.CoroutineToken == 0 ||
		v.Presence&CausalHasLeaseID != 0 && v.LeaseID == 0 {
		return fmt.Errorf("invalid causal binding")
	}
	if v.Presence&CausalHasResourceKind != 0 && (v.EntityKind != CausalResource || v.ResourceKind < CausalResourceAutoCloseable || v.ResourceKind > CausalResourceWakeLock) ||
		v.Presence&CausalHasOperationKind != 0 && (v.EntityKind != CausalOperation || v.OperationKind < CausalOperationFileRead || v.OperationKind > CausalOperationContentWrite) ||
		v.Presence&CausalHasLeaseID != 0 && v.EntityKind != CausalOperation ||
		v.Presence&CausalHasCoroutineBinding != 0 && v.EntityKind != CausalCoroutine {
		return fmt.Errorf("invalid causal kind-specific field")
	}
	if v.EntityKind == CausalOperation && v.Presence&(CausalHasOperationKind|CausalHasDurationNanos) != CausalHasOperationKind|CausalHasDurationNanos {
		return fmt.Errorf("operation cost lacks kind or duration")
	}
	if v.EntityKind == CausalResource && v.Presence&CausalHasResourceKind == 0 ||
		v.EntityKind == CausalWorkerAttempt && v.Phase == 1 && v.Presence&CausalHasWorkerBinding == 0 {
		return fmt.Errorf("resource or worker binding missing")
	}
	if v.Presence&CausalHasCallResult != 0 && (v.CallResult < 1 || v.CallResult > 3) ||
		v.Presence&CausalHasTerminalOutcome != 0 && (v.TerminalOutcome < 1 || v.TerminalOutcome > 5) {
		return fmt.Errorf("invalid causal outcome")
	}
	if v.Presence&CausalHasLeaseScope != 0 {
		if v.EntityKind != CausalResource || !validCausalRef(v.LeaseScopeKind, v.LeaseScopeID) || v.BoundToScope > 1 ||
			v.LeaseScopeID == 0 && v.BoundToScope != 0 {
			return fmt.Errorf("invalid causal lease scope")
		}
	} else if v.LeaseScopeKind != 0 || v.LeaseScopeID != 0 || v.BoundToScope != 0 {
		return fmt.Errorf("causal lease scope without presence")
	}
	if v.EntityKind == CausalResource && v.Phase == 6 && v.Presence&CausalHasLeaseScope == 0 {
		return fmt.Errorf("resource scope transfer without destination")
	}
	if v.Presence&CausalHasRequestedPeriod != 0 && v.RequestedPeriodNanos == 0 ||
		v.Presence&CausalHasScheduleCadence != 0 && (v.ScheduleCadence < 1 || v.ScheduleCadence > 3) ||
		v.Presence&CausalHasScheduleCadence != 0 && v.ScheduleCadence >= 2 && v.Presence&CausalHasRequestedPeriod == 0 {
		return fmt.Errorf("invalid causal scheduling cadence")
	}
	if v.Presence&(CausalHasRequestedPeriod|CausalHasScheduleCadence) != 0 &&
		v.EntityKind != CausalSeries && v.EntityKind != CausalExecutorTask {
		return fmt.Errorf("scheduling cadence on unsupported causal kind")
	}
	if v.Presence&CausalHasRequestedDelay != 0 && (v.EntityKind != CausalHandlerPost || v.Phase != 1) ||
		v.Presence&CausalHasRequestedDelay == 0 && v.RequestedDelayNanos != 0 {
		return fmt.Errorf("requested Handler delay without post invocation")
	}
	return nil
}

func (e eventPayloadEncoder) encodeCausalEvent(v *CausalEvent) error {
	if err := validateCausalEvent(v); err != nil {
		return err
	}
	if err := e.writeValues(v.Version, uint64(v.EntityKind), v.Phase, v.EntityID,
		uint64(v.ParentKind), v.ParentID, uint64(v.SchedulerKind), v.SchedulerID,
		uint64(v.ActorKind), v.ActorID, v.ObservedElapsedNanos, uint64(v.SourceOrigin), v.Presence); err != nil {
		return err
	}
	for _, field := range []struct{ bit, value uint64 }{
		{CausalHasTargetJavaID, v.TargetJavaID}, {CausalHasTargetTID, v.TargetTID},
	} {
		if v.Presence&field.bit != 0 {
			if err := e.writeValues(field.value); err != nil {
				return err
			}
		}
	}
	if v.Presence&CausalHasSource != 0 {
		if err := e.writeValues(v.SourceSiteID); err != nil {
			return err
		}
		if err := e.threadString(v.SourceMethod); err != nil {
			return err
		}
		if err := e.writeValues(v.SourceLine); err != nil {
			return err
		}
	}
	if v.Presence&CausalHasAppCaller != 0 {
		if err := e.writeValues(v.AppCallerSiteID); err != nil {
			return err
		}
		if err := e.threadString(v.AppCallerMethod); err != nil {
			return err
		}
		if err := e.writeValues(v.AppCallerLine); err != nil {
			return err
		}
	}
	for _, field := range []struct{ bit, value uint64 }{
		{CausalHasCPUNanos, v.CPUNanos}, {CausalHasDurationNanos, v.DurationNanos},
		{CausalHasReadBytes, v.ReadBytes}, {CausalHasWrittenBytes, v.WrittenBytes},
		{CausalHasScheduledFor, v.ScheduledForNanos}, {CausalHasQueueEntered, v.QueueEnteredNanos},
		{CausalHasOccurrence, v.Occurrence},
	} {
		if v.Presence&field.bit != 0 {
			if err := e.writeValues(field.value); err != nil {
				return err
			}
		}
	}
	if v.Presence&CausalHasWorkerBinding != 0 {
		if err := e.writeValues(v.WorkerInstanceID, v.RunAttempt, v.Generation); err != nil {
			return err
		}
	}
	for _, field := range []struct{ bit, value uint64 }{
		{CausalHasResourceKind, v.ResourceKind}, {CausalHasOperationKind, v.OperationKind},
		{CausalHasCallResult, v.CallResult}, {CausalHasTerminalOutcome, v.TerminalOutcome}, {CausalHasLeaseID, v.LeaseID},
	} {
		if v.Presence&field.bit != 0 {
			if err := e.writeValues(field.value); err != nil {
				return err
			}
		}
	}
	if v.Presence&CausalHasCoroutineBinding != 0 {
		if err := e.writeValues(v.CoroutineToken, v.CoroutineGeneration); err != nil {
			return err
		}
	}
	if v.Presence&CausalHasLeaseScope != 0 {
		if err := e.writeValues(uint64(v.LeaseScopeKind), v.LeaseScopeID, v.BoundToScope); err != nil {
			return err
		}
	}
	if v.Presence&CausalHasRequestedPeriod != 0 {
		if err := e.writeValues(v.RequestedPeriodNanos); err != nil {
			return err
		}
	}
	if v.Presence&CausalHasScheduleCadence != 0 {
		if err := e.writeValues(v.ScheduleCadence); err != nil {
			return err
		}
	}
	if v.Presence&CausalHasRequestedDelay != 0 {
		return e.writeValues(v.RequestedDelayNanos)
	}
	return nil
}

func decodeCausalEvent(r *recordReader, event *Event) error {
	var scratch [13]uint64
	v, err := readPayloadValues(r, scratch[:], "version", "kind", "phase", "entity ID", "parent kind", "parent ID", "scheduler kind", "scheduler ID", "actor kind", "actor ID", "observed nanos", "source origin", "presence")
	if err != nil {
		return err
	}
	c := &CausalEvent{Version: v[0], EntityKind: CausalEntityKind(v[1]), Phase: v[2], EntityID: v[3],
		ParentKind: CausalEntityKind(v[4]), ParentID: v[5], SchedulerKind: CausalEntityKind(v[6]), SchedulerID: v[7],
		ActorKind: CausalEntityKind(v[8]), ActorID: v[9], ObservedElapsedNanos: v[10], SourceOrigin: CausalSourceOrigin(v[11]), Presence: v[12]}
	if c.Presence&^uint64(causalKnownPresence) != 0 {
		return fmt.Errorf("unsupported causal presence 0x%x", c.Presence)
	}
	for _, field := range []struct {
		bit    uint64
		name   string
		target *uint64
	}{
		{CausalHasTargetJavaID, "target Java ID", &c.TargetJavaID}, {CausalHasTargetTID, "target TID", &c.TargetTID},
	} {
		if c.Presence&field.bit != 0 {
			*field.target, err = readPayloadValue(r, field.name)
			if err != nil {
				return err
			}
		}
	}
	if c.Presence&CausalHasSource != 0 {
		if c.SourceSiteID, err = readPayloadValue(r, "source site ID"); err != nil {
			return err
		}
		if c.SourceMethod, err = readThreadString(r, 512); err != nil {
			return err
		}
		if c.SourceLine, err = readPayloadValue(r, "source line"); err != nil {
			return err
		}
	}
	if c.Presence&CausalHasAppCaller != 0 {
		if c.AppCallerSiteID, err = readPayloadValue(r, "app caller site ID"); err != nil {
			return err
		}
		if c.AppCallerMethod, err = readThreadString(r, 512); err != nil {
			return err
		}
		if c.AppCallerLine, err = readPayloadValue(r, "app caller line"); err != nil {
			return err
		}
	}
	for _, field := range []struct {
		bit    uint64
		name   string
		target *uint64
	}{
		{CausalHasCPUNanos, "CPU nanos", &c.CPUNanos}, {CausalHasDurationNanos, "duration nanos", &c.DurationNanos},
		{CausalHasReadBytes, "read bytes", &c.ReadBytes}, {CausalHasWrittenBytes, "written bytes", &c.WrittenBytes},
		{CausalHasScheduledFor, "scheduled elapsed nanos", &c.ScheduledForNanos}, {CausalHasQueueEntered, "queue entered nanos", &c.QueueEnteredNanos},
		{CausalHasOccurrence, "occurrence", &c.Occurrence},
	} {
		if c.Presence&field.bit != 0 {
			*field.target, err = readPayloadValue(r, field.name)
			if err != nil {
				return err
			}
		}
	}
	if c.Presence&CausalHasWorkerBinding != 0 {
		if c.WorkerInstanceID, err = readPayloadValue(r, "worker instance ID"); err != nil {
			return err
		}
		if c.RunAttempt, err = readPayloadValue(r, "run attempt"); err != nil {
			return err
		}
		if c.Generation, err = readPayloadValue(r, "worker generation"); err != nil {
			return err
		}
	}
	for _, field := range []struct {
		bit    uint64
		name   string
		target *uint64
	}{
		{CausalHasResourceKind, "resource kind", &c.ResourceKind}, {CausalHasOperationKind, "operation kind", &c.OperationKind},
		{CausalHasCallResult, "call result", &c.CallResult}, {CausalHasTerminalOutcome, "terminal outcome", &c.TerminalOutcome},
		{CausalHasLeaseID, "lease ID", &c.LeaseID},
	} {
		if c.Presence&field.bit != 0 {
			*field.target, err = readPayloadValue(r, field.name)
			if err != nil {
				return err
			}
		}
	}
	if c.Presence&CausalHasCoroutineBinding != 0 {
		if c.CoroutineToken, err = readPayloadValue(r, "coroutine token"); err != nil {
			return err
		}
		if c.CoroutineGeneration, err = readPayloadValue(r, "coroutine generation"); err != nil {
			return err
		}
	}
	if c.Presence&CausalHasLeaseScope != 0 {
		var scopeKind uint64
		if scopeKind, err = readPayloadValue(r, "lease scope kind"); err != nil {
			return err
		}
		c.LeaseScopeKind = CausalEntityKind(scopeKind)
		if c.LeaseScopeID, err = readPayloadValue(r, "lease scope ID"); err != nil {
			return err
		}
		if c.BoundToScope, err = readPayloadValue(r, "bound to scope"); err != nil {
			return err
		}
	}
	if c.Presence&CausalHasRequestedPeriod != 0 {
		if c.RequestedPeriodNanos, err = readPayloadValue(r, "requested period nanos"); err != nil {
			return err
		}
	}
	if c.Presence&CausalHasScheduleCadence != 0 {
		if c.ScheduleCadence, err = readPayloadValue(r, "schedule cadence"); err != nil {
			return err
		}
	}
	if c.Presence&CausalHasRequestedDelay != 0 {
		if c.RequestedDelayNanos, err = readPayloadValue(r, "requested Handler delay nanos"); err != nil {
			return err
		}
	}
	if err := validateCausalEvent(c); err != nil {
		return err
	}
	event.DiagnosticEvent = &DiagnosticEvent{CausalEvent: c}
	return nil
}
