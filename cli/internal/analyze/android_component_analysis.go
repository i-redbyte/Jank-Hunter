package analyze

import (
	"fmt"
	"sort"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

const (
	serviceSlowCallbackUS       = 100_000
	receiverSyncSlowUS          = 10_000
	receiverDeadlineRiskUS      = 9_000_000
	binderMainThreadSlowUS      = 16_000
	binderCorrelationTolerance  = uint64(2)
	binderOnewayCorrelationMS   = uint64(2_000)
	binderCorrelationEventLimit = 250_000
	binderFlowOutputLimit       = 2_000
)

type AndroidComponentAnalysis struct {
	Available      bool                      `json:"available"`
	Partial        bool                      `json:"partial"`
	PartialReasons []string                  `json:"partial_reasons,omitempty"`
	Coverage       AndroidComponentCoverage  `json:"coverage"`
	ProcessState   AndroidProcessStateStats  `json:"process_state"`
	Services       AndroidServiceAnalysis    `json:"services"`
	Receivers      AndroidReceiverAnalysis   `json:"receivers"`
	Binder         AndroidBinderAnalysis     `json:"binder"`
	Findings       []AndroidComponentFinding `json:"findings,omitempty"`
}

type AndroidComponentCoverage struct {
	CatalogAvailable        bool   `json:"catalog_available"`
	Components              uint64 `json:"components"`
	Services                uint64 `json:"services"`
	Receivers               uint64 `json:"receivers"`
	Full                    uint64 `json:"full"`
	Partial                 uint64 `json:"partial"`
	None                    uint64 `json:"none"`
	EntryPoints             uint64 `json:"entry_points"`
	InstrumentedEntryPoints uint64 `json:"instrumented_entry_points"`
	UncoveredEntryPoints    uint64 `json:"uncovered_entry_points"`
	AIDLInterfaces          uint64 `json:"aidl_interfaces"`
	AIDLTransactions        uint64 `json:"aidl_transactions"`
}

type AndroidProcessStateStats struct {
	Samples                        uint64 `json:"samples"`
	VisibleUISamples               uint64 `json:"visible_ui_samples"`
	HiddenUISamples                uint64 `json:"hidden_ui_samples"`
	ForegroundServiceSamples       uint64 `json:"foreground_service_samples"`
	HiddenForegroundServiceSamples uint64 `json:"hidden_foreground_service_samples"`
	ServiceImportanceSamples       uint64 `json:"service_importance_samples"`
	CachedSamples                  uint64 `json:"cached_samples"`
}

type AndroidServiceAnalysis struct {
	Callbacks             uint64                `json:"callbacks"`
	Failures              uint64                `json:"failures"`
	Timeouts              uint64                `json:"timeouts"`
	SlowCallbacks         uint64                `json:"slow_callbacks"`
	ForegroundEntries     uint64                `json:"foreground_entries"`
	ForegroundExits       uint64                `json:"foreground_exits"`
	ActiveInstancesAtEnd  uint64                `json:"active_instances_at_end"`
	ActiveForegroundAtEnd uint64                `json:"active_foreground_at_end"`
	Components            []AndroidServiceStats `json:"components,omitempty"`
}

type AndroidServiceStats struct {
	Component             string `json:"component"`
	Process               string `json:"process"`
	Callbacks             uint64 `json:"callbacks"`
	Failures              uint64 `json:"failures"`
	Timeouts              uint64 `json:"timeouts"`
	SlowCallbacks         uint64 `json:"slow_callbacks"`
	ForegroundEntries     uint64 `json:"foreground_entries"`
	ForegroundExits       uint64 `json:"foreground_exits"`
	ActiveInstancesAtEnd  uint64 `json:"active_instances_at_end"`
	ActiveForegroundAtEnd uint64 `json:"active_foreground_at_end"`
	P95DurationUS         uint64 `json:"p95_duration_us"`
	MaxDurationUS         uint64 `json:"max_duration_us"`
}

type AndroidReceiverAnalysis struct {
	Completed          uint64                 `json:"completed"`
	Failures           uint64                 `json:"failures"`
	AsyncCompleted     uint64                 `json:"async_completed"`
	ActiveAtEnd        uint64                 `json:"active_at_end"`
	MissingStarts      uint64                 `json:"missing_starts"`
	SyncSlowCallbacks  uint64                 `json:"sync_slow_callbacks"`
	AsyncDeadlineRisks uint64                 `json:"async_deadline_risks"`
	Components         []AndroidReceiverStats `json:"components,omitempty"`
}

type AndroidReceiverStats struct {
	Component          string `json:"component"`
	Action             string `json:"action"`
	Process            string `json:"process"`
	Completed          uint64 `json:"completed"`
	Failures           uint64 `json:"failures"`
	AsyncCompleted     uint64 `json:"async_completed"`
	ActiveAtEnd        uint64 `json:"active_at_end"`
	SyncSlowCallbacks  uint64 `json:"sync_slow_callbacks"`
	AsyncDeadlineRisks uint64 `json:"async_deadline_risks"`
	P95DurationUS      uint64 `json:"p95_duration_us"`
	MaxDurationUS      uint64 `json:"max_duration_us"`
}

type AndroidBinderAnalysis struct {
	ClientCalls              uint64                        `json:"client_calls"`
	ServerCalls              uint64                        `json:"server_calls"`
	OneWayCalls              uint64                        `json:"oneway_calls"`
	MainThreadClientCalls    uint64                        `json:"main_thread_client_calls"`
	SlowMainThreadCalls      uint64                        `json:"slow_main_thread_calls"`
	Failures                 uint64                        `json:"failures"`
	DeadObjectFailures       uint64                        `json:"dead_object_failures"`
	Unhandled                uint64                        `json:"unhandled"`
	CorrelatedPairs          uint64                        `json:"correlated_pairs"`
	CrossProcessPairs        uint64                        `json:"cross_process_pairs"`
	AmbiguousClients         uint64                        `json:"ambiguous_clients"`
	UnmatchedClients         uint64                        `json:"unmatched_clients"`
	UnmatchedServers         uint64                        `json:"unmatched_servers"`
	UncorrelatableEvents     uint64                        `json:"uncorrelatable_events"`
	CorrelationDroppedEvents uint64                        `json:"correlation_dropped_events"`
	P95ClientDurationUS      uint64                        `json:"p95_client_duration_us"`
	MaxClientDurationUS      uint64                        `json:"max_client_duration_us"`
	Interfaces               []AndroidBinderInterfaceStats `json:"interfaces,omitempty"`
	Flows                    []AndroidBinderFlow           `json:"flows,omitempty"`
}

type AndroidBinderInterfaceStats struct {
	Descriptor          string `json:"descriptor"`
	Method              string `json:"method"`
	TransactionCode     uint32 `json:"transaction_code"`
	ClientCalls         uint64 `json:"client_calls"`
	ServerCalls         uint64 `json:"server_calls"`
	Failures            uint64 `json:"failures"`
	Unhandled           uint64 `json:"unhandled"`
	MainThreadCalls     uint64 `json:"main_thread_calls"`
	SlowMainThreadCalls uint64 `json:"slow_main_thread_calls"`
	P95ClientDurationUS uint64 `json:"p95_client_duration_us"`
	MaxClientDurationUS uint64 `json:"max_client_duration_us"`
}

type AndroidBinderFlow struct {
	Descriptor        string `json:"descriptor"`
	Method            string `json:"method"`
	TransactionCode   uint32 `json:"transaction_code"`
	ClientProcess     string `json:"client_process"`
	ServerProcess     string `json:"server_process"`
	ClientStartUnixMS uint64 `json:"client_start_unix_ms"`
	ClientEndUnixMS   uint64 `json:"client_end_unix_ms"`
	ServerStartUnixMS uint64 `json:"server_start_unix_ms"`
	ServerEndUnixMS   uint64 `json:"server_end_unix_ms"`
	ClientDurationUS  uint64 `json:"client_duration_us"`
	ServerDurationUS  uint64 `json:"server_duration_us"`
	OneWay            bool   `json:"oneway"`
	CrossProcess      bool   `json:"cross_process"`
	Confidence        string `json:"confidence"`
	ClaimLevel        string `json:"claim_level"`
	Evidence          string `json:"evidence"`
}

type AndroidComponentFinding struct {
	ID            string `json:"id"`
	Severity      string `json:"severity"`
	Title         string `json:"title"`
	Explanation   string `json:"explanation"`
	Component     string `json:"component,omitempty"`
	Action        string `json:"action,omitempty"`
	Descriptor    string `json:"descriptor,omitempty"`
	Method        string `json:"method,omitempty"`
	Process       string `json:"process,omitempty"`
	Count         uint64 `json:"count"`
	MaxDurationUS uint64 `json:"max_duration_us,omitempty"`
	ClaimLevel    string `json:"claim_level"`
}

type androidComponentAnalysisAccumulator struct {
	catalog               *AndroidComponentCatalog
	header                jhlog.SegmentHeader
	available             bool
	processState          AndroidProcessStateStats
	services              map[androidServiceKey]*androidServiceAggregate
	serviceInstances      map[androidInstanceKey]*androidServiceInstance
	receivers             map[androidReceiverKey]*androidReceiverAggregate
	activeReceivers       map[androidFlowKey]androidReceiverKey
	missingReceiverStarts uint64
	binder                androidBinderAccumulator
	findings              map[androidFindingKey]*AndroidComponentFinding
}

type androidServiceKey struct {
	component string
	process   string
}

type androidReceiverKey struct {
	component string
	action    string
	process   string
}

type androidInstanceKey struct {
	process jhlog.ID128
	id      uint64
}

type androidFlowKey struct {
	process jhlog.ID128
	id      uint64
}

type androidServiceInstance struct {
	key        androidServiceKey
	foreground bool
}

type androidServiceAggregate struct {
	stats     AndroidServiceStats
	durations operationDurationSummary
}

type androidReceiverAggregate struct {
	stats     AndroidReceiverStats
	durations operationDurationSummary
}

type androidFindingKey struct {
	id      string
	subject string
}

type androidBinderAccumulator struct {
	catalog        *AndroidComponentCatalog
	stats          AndroidBinderAnalysis
	clientDuration operationDurationSummary
	interfaces     map[androidBinderInterfaceKey]*androidBinderInterfaceAggregate
	records        []androidBinderRecord
}

type androidBinderInterfaceKey struct {
	descriptor string
	method     string
	code       uint32
}

type androidBinderInterfaceAggregate struct {
	stats          AndroidBinderInterfaceStats
	clientDuration operationDurationSummary
}

type androidBinderRecord struct {
	runID       jhlog.ID128
	processID   jhlog.ID128
	process     string
	descriptor  string
	method      string
	code        uint32
	startUnixMS uint64
	endUnixMS   uint64
	durationUS  uint64
	direction   jhlog.BinderDirection
	oneway      bool
	matched     bool
}

type androidBinderCorrelationKey struct {
	runID      jhlog.ID128
	descriptor string
	code       uint32
}

func newAndroidComponentAnalysisAccumulator(catalog *AndroidComponentCatalog) *androidComponentAnalysisAccumulator {
	return &androidComponentAnalysisAccumulator{
		catalog:          catalog,
		services:         make(map[androidServiceKey]*androidServiceAggregate),
		serviceInstances: make(map[androidInstanceKey]*androidServiceInstance),
		receivers:        make(map[androidReceiverKey]*androidReceiverAggregate),
		activeReceivers:  make(map[androidFlowKey]androidReceiverKey),
		binder: androidBinderAccumulator{
			catalog:    catalog,
			interfaces: make(map[androidBinderInterfaceKey]*androidBinderInterfaceAggregate),
		},
		findings: make(map[androidFindingKey]*AndroidComponentFinding),
	}
}

func (a *androidComponentAnalysisAccumulator) startLog(header jhlog.SegmentHeader) {
	a.header = header
}

func (a *androidComponentAnalysisAccumulator) addProcessState(event jhlog.Event) {
	state := event.ProcessState
	if state == nil {
		return
	}
	a.available = true
	a.processState.Samples++
	switch state.UIVisibility {
	case jhlog.ProcessUIVisible:
		a.processState.VisibleUISamples++
	case jhlog.ProcessUIHidden:
		a.processState.HiddenUISamples++
	}
	switch state.Importance {
	case jhlog.ProcessImportanceForegroundService:
		a.processState.ForegroundServiceSamples++
		if state.UIVisibility == jhlog.ProcessUIHidden {
			a.processState.HiddenForegroundServiceSamples++
		}
	case jhlog.ProcessImportanceService:
		a.processState.ServiceImportanceSamples++
	case jhlog.ProcessImportanceCached:
		a.processState.CachedSamples++
	}
}

func (a *androidComponentAnalysisAccumulator) addComponent(component, action string, event jhlog.Event) {
	value := event.AndroidComponent
	if value == nil {
		return
	}
	a.available = true
	component = attrValue(component)
	action = attrValue(action)
	switch value.Kind {
	case jhlog.ComponentKindService:
		a.addService(component, event)
	case jhlog.ComponentKindReceiver:
		a.addReceiver(component, action, event)
	}
}

func (a *androidComponentAnalysisAccumulator) addService(component string, event jhlog.Event) {
	value := event.AndroidComponent
	key := androidServiceKey{component: component, process: firstNonEmpty(a.header.ProcessName, "unknown")}
	aggregate := a.services[key]
	if aggregate == nil {
		aggregate = &androidServiceAggregate{stats: AndroidServiceStats{Component: key.component, Process: key.process}}
		a.services[key] = aggregate
	}
	instanceKey := androidInstanceKey{process: a.header.ProcessInstanceID, id: value.InstanceID}
	instance := a.serviceInstances[instanceKey]
	if instance == nil {
		instance = &androidServiceInstance{key: key}
		a.serviceInstances[instanceKey] = instance
	}
	switch value.Stage {
	case jhlog.ComponentServiceForegroundEnter:
		aggregate.stats.ForegroundEntries++
		instance.foreground = true
	case jhlog.ComponentServiceForegroundExit:
		aggregate.stats.ForegroundExits++
		instance.foreground = false
	case jhlog.ComponentServiceDestroyed:
		aggregate.stats.Callbacks++
		aggregate.durations.add(value.DurationUS)
		delete(a.serviceInstances, instanceKey)
	default:
		aggregate.stats.Callbacks++
		aggregate.durations.add(value.DurationUS)
		if value.Stage == jhlog.ComponentServiceCreated && value.Outcome != jhlog.ComponentOutcomeSuccess {
			delete(a.serviceInstances, instanceKey)
		}
	}
	if value.Outcome == jhlog.ComponentOutcomeFailure {
		aggregate.stats.Failures++
		a.addFinding("android.service.callback_failure", key.component+"\x00"+key.process, AndroidComponentFinding{
			ID: "android.service.callback_failure", Severity: "high", Title: "Ошибка метода жизненного цикла Service",
			Explanation: "Перехваченный метод Service завершился исключением.",
			Component:   key.component, Process: key.process, ClaimLevel: "linked",
		}, value.DurationUS)
	}
	if value.Outcome == jhlog.ComponentOutcomeTimeout || value.Stage == jhlog.ComponentServiceTimeout {
		aggregate.stats.Timeouts++
		a.addFinding("android.service.timeout", key.component+"\x00"+key.process, AndroidComponentFinding{
			ID: "android.service.timeout", Severity: "high", Title: "Service превысил системный лимит времени",
			Explanation: "Android вызвал onTimeout у наблюдаемого Service.",
			Component:   key.component, Process: key.process, ClaimLevel: "linked",
		}, value.DurationUS)
	}
	if value.DurationUS >= serviceSlowCallbackUS {
		aggregate.stats.SlowCallbacks++
		a.addFinding("android.service.callback_slow", key.component+"\x00"+key.process, AndroidComponentFinding{
			ID: "android.service.callback_slow", Severity: "medium", Title: "Долгий метод жизненного цикла Service",
			Explanation: "Метод Service выполнялся не менее 100 мс; проверьте синхронную работу и блокировки.",
			Component:   key.component, Process: key.process, ClaimLevel: "linked",
		}, value.DurationUS)
	}
}

func (a *androidComponentAnalysisAccumulator) addReceiver(component, action string, event jhlog.Event) {
	value := event.AndroidComponent
	key := androidReceiverKey{component: component, action: action, process: firstNonEmpty(a.header.ProcessName, "unknown")}
	aggregate := a.receivers[key]
	if aggregate == nil {
		aggregate = &androidReceiverAggregate{stats: AndroidReceiverStats{
			Component: key.component, Action: key.action, Process: key.process,
		}}
		a.receivers[key] = aggregate
	}
	flowKey := androidFlowKey{process: a.header.ProcessInstanceID, id: value.FlowID}
	switch value.Stage {
	case jhlog.ComponentReceiverStarted:
		a.activeReceivers[flowKey] = key
	case jhlog.ComponentReceiverAsyncStarted:
		if _, exists := a.activeReceivers[flowKey]; !exists {
			a.activeReceivers[flowKey] = key
		}
	case jhlog.ComponentReceiverFinished:
		if _, exists := a.activeReceivers[flowKey]; !exists {
			a.missingReceiverStarts++
		}
		delete(a.activeReceivers, flowKey)
		aggregate.stats.Completed++
		aggregate.durations.add(value.DurationUS)
		if value.Flags&jhlog.ComponentFlagAsync != 0 {
			aggregate.stats.AsyncCompleted++
			if value.DurationUS >= receiverDeadlineRiskUS {
				aggregate.stats.AsyncDeadlineRisks++
				a.addFinding("android.receiver.async_deadline_risk", key.component+"\x00"+key.action+"\x00"+key.process, AndroidComponentFinding{
					ID: "android.receiver.async_deadline_risk", Severity: "high", Title: "BroadcastReceiver близок к системному дедлайну",
					Explanation: "Асинхронная обработка BroadcastReceiver заняла не менее 9 секунд; возрастает риск превышения системного лимита времени и ANR.",
					Component:   key.component, Action: key.action, Process: key.process, ClaimLevel: "linked",
				}, value.DurationUS)
			}
		} else if value.DurationUS >= receiverSyncSlowUS {
			aggregate.stats.SyncSlowCallbacks++
			a.addFinding("android.receiver.sync_slow", key.component+"\x00"+key.action+"\x00"+key.process, AndroidComponentFinding{
				ID: "android.receiver.sync_slow", Severity: "medium", Title: "Синхронная работа в BroadcastReceiver",
				Explanation: "onReceive выполнялся не менее 10 мс; тяжёлую работу следует вынести из этого метода.",
				Component:   key.component, Action: key.action, Process: key.process, ClaimLevel: "linked",
			}, value.DurationUS)
		}
		if value.Outcome == jhlog.ComponentOutcomeFailure {
			aggregate.stats.Failures++
			a.addFinding("android.receiver.failure", key.component+"\x00"+key.action+"\x00"+key.process, AndroidComponentFinding{
				ID: "android.receiver.failure", Severity: "high", Title: "Ошибка BroadcastReceiver",
				Explanation: "Инструментированный onReceive завершился исключением.",
				Component:   key.component, Action: key.action, Process: key.process, ClaimLevel: "linked",
			}, value.DurationUS)
		}
	}
}

func (a *androidComponentAnalysisAccumulator) addBinder(descriptor, method string, event jhlog.Event) {
	value := event.BinderTransaction
	if value == nil {
		return
	}
	a.available = true
	a.binder.add(a.header, strings.TrimSpace(descriptor), strings.TrimSpace(method), event)
}

func (a *androidComponentAnalysisAccumulator) addFinding(
	id, subject string,
	finding AndroidComponentFinding,
	durationUS uint64,
) {
	key := androidFindingKey{id: id, subject: subject}
	existing := a.findings[key]
	if existing == nil {
		finding.Count = 1
		finding.MaxDurationUS = durationUS
		a.findings[key] = &finding
		return
	}
	existing.Count++
	existing.MaxDurationUS = maxUint64(existing.MaxDurationUS, durationUS)
}

func (a *androidComponentAnalysisAccumulator) finalize(quality CollectionQuality) *AndroidComponentAnalysis {
	analysis := &AndroidComponentAnalysis{
		Available: a.available, Coverage: androidComponentCoverage(a.catalog), ProcessState: a.processState,
	}
	a.finalizeServices(&analysis.Services)
	a.finalizeReceivers(&analysis.Receivers)
	analysis.Binder = a.binder.finalize()
	for _, finding := range a.findings {
		analysis.Findings = append(analysis.Findings, *finding)
	}
	for _, finding := range a.binder.findings() {
		analysis.Findings = append(analysis.Findings, finding)
	}
	analysis.PartialReasons = androidAnalysisPartialReasons(
		quality,
		analysis.Binder.CorrelationDroppedEvents,
		analysis.Binder.UncorrelatableEvents,
	)
	analysis.Partial = len(analysis.PartialReasons) > 0
	if analysis.Receivers.ActiveAtEnd > 0 {
		analysis.Findings = append(analysis.Findings, AndroidComponentFinding{
			ID: "android.receiver.open_at_capture_end", Severity: "warning", Title: "BroadcastReceiver не завершён к концу записи",
			Explanation: "Это может быть активная работа на границе записи или отсутствующее событие завершения; превышение системного лимита времени не доказано.",
			Count:       analysis.Receivers.ActiveAtEnd, ClaimLevel: "hypothesis",
		})
	}
	sort.Slice(analysis.Findings, func(i, j int) bool {
		if analysis.Findings[i].Severity != analysis.Findings[j].Severity {
			return androidSeverityRank(analysis.Findings[i].Severity) > androidSeverityRank(analysis.Findings[j].Severity)
		}
		if analysis.Findings[i].ID != analysis.Findings[j].ID {
			return analysis.Findings[i].ID < analysis.Findings[j].ID
		}
		return analysis.Findings[i].Component < analysis.Findings[j].Component
	})
	return analysis
}

func androidComponentCoverage(catalog *AndroidComponentCatalog) AndroidComponentCoverage {
	if catalog == nil || !catalog.Available {
		return AndroidComponentCoverage{}
	}
	coverage := AndroidComponentCoverage{
		CatalogAvailable: true, Components: uint64(len(catalog.Components)),
	}
	for index := range catalog.Components {
		component := &catalog.Components[index]
		switch component.Kind {
		case "service":
			coverage.Services++
		case "receiver":
			coverage.Receivers++
		}
		switch component.Coverage {
		case "full":
			coverage.Full++
		case "partial":
			coverage.Partial++
		default:
			coverage.None++
		}
		coverage.EntryPoints += uint64(len(component.EntryPoints))
		coverage.InstrumentedEntryPoints += uint64(len(component.InstrumentedEntryPoints))
		coverage.UncoveredEntryPoints += uint64(len(component.UncoveredEntryPoints))
		if component.AIDLDescriptor != "" {
			coverage.AIDLInterfaces++
		}
		coverage.AIDLTransactions += uint64(len(component.Transactions))
	}
	return coverage
}

func (a *androidComponentAnalysisAccumulator) finalizeServices(result *AndroidServiceAnalysis) {
	for _, instance := range a.serviceInstances {
		aggregate := a.services[instance.key]
		if aggregate == nil {
			continue
		}
		aggregate.stats.ActiveInstancesAtEnd++
		if instance.foreground {
			aggregate.stats.ActiveForegroundAtEnd++
		}
	}
	for _, aggregate := range a.services {
		aggregate.stats.P95DurationUS = aggregate.durations.percentile(0.95)
		aggregate.stats.MaxDurationUS = aggregate.durations.max
		result.Callbacks += aggregate.stats.Callbacks
		result.Failures += aggregate.stats.Failures
		result.Timeouts += aggregate.stats.Timeouts
		result.SlowCallbacks += aggregate.stats.SlowCallbacks
		result.ForegroundEntries += aggregate.stats.ForegroundEntries
		result.ForegroundExits += aggregate.stats.ForegroundExits
		result.ActiveInstancesAtEnd += aggregate.stats.ActiveInstancesAtEnd
		result.ActiveForegroundAtEnd += aggregate.stats.ActiveForegroundAtEnd
		result.Components = append(result.Components, aggregate.stats)
	}
	sort.Slice(result.Components, func(i, j int) bool {
		if result.Components[i].Component != result.Components[j].Component {
			return result.Components[i].Component < result.Components[j].Component
		}
		return result.Components[i].Process < result.Components[j].Process
	})
}

func (a *androidComponentAnalysisAccumulator) finalizeReceivers(result *AndroidReceiverAnalysis) {
	for _, key := range a.activeReceivers {
		if aggregate := a.receivers[key]; aggregate != nil {
			aggregate.stats.ActiveAtEnd++
		}
	}
	result.MissingStarts = a.missingReceiverStarts
	for _, aggregate := range a.receivers {
		aggregate.stats.P95DurationUS = aggregate.durations.percentile(0.95)
		aggregate.stats.MaxDurationUS = aggregate.durations.max
		result.Completed += aggregate.stats.Completed
		result.Failures += aggregate.stats.Failures
		result.AsyncCompleted += aggregate.stats.AsyncCompleted
		result.ActiveAtEnd += aggregate.stats.ActiveAtEnd
		result.SyncSlowCallbacks += aggregate.stats.SyncSlowCallbacks
		result.AsyncDeadlineRisks += aggregate.stats.AsyncDeadlineRisks
		result.Components = append(result.Components, aggregate.stats)
	}
	sort.Slice(result.Components, func(i, j int) bool {
		left, right := result.Components[i], result.Components[j]
		if left.Component != right.Component {
			return left.Component < right.Component
		}
		if left.Action != right.Action {
			return left.Action < right.Action
		}
		return left.Process < right.Process
	})
}

func (a *androidBinderAccumulator) add(
	header jhlog.SegmentHeader,
	descriptor, method string,
	event jhlog.Event,
) {
	value := event.BinderTransaction
	if method == "" && a.catalog != nil {
		method, _ = a.catalog.AIDLMethod(descriptor, value.TransactionCode)
	}
	key := androidBinderInterfaceKey{descriptor: attrValue(descriptor), method: attrValue(method), code: value.TransactionCode}
	aggregate := a.interfaces[key]
	if aggregate == nil {
		aggregate = &androidBinderInterfaceAggregate{stats: AndroidBinderInterfaceStats{
			Descriptor: key.descriptor, Method: key.method, TransactionCode: key.code,
		}}
		a.interfaces[key] = aggregate
	}
	mainThread := event.Flags&uint64(jhlog.FlagThreadMain) != 0
	if value.Direction == jhlog.BinderDirectionClient {
		a.stats.ClientCalls++
		aggregate.stats.ClientCalls++
		a.clientDuration.add(value.DurationUS)
		aggregate.clientDuration.add(value.DurationUS)
		if mainThread {
			a.stats.MainThreadClientCalls++
			aggregate.stats.MainThreadCalls++
			if value.DurationUS >= binderMainThreadSlowUS {
				a.stats.SlowMainThreadCalls++
				aggregate.stats.SlowMainThreadCalls++
			}
		}
	} else {
		a.stats.ServerCalls++
		aggregate.stats.ServerCalls++
	}
	if value.Direction == jhlog.BinderDirectionClient && value.Flags&jhlog.BinderFlagOneway != 0 {
		a.stats.OneWayCalls++
	}
	if value.Outcome == jhlog.BinderOutcomeFailure {
		a.stats.Failures++
		aggregate.stats.Failures++
		if value.FailureKind == jhlog.BinderFailureDeadObject {
			a.stats.DeadObjectFailures++
		}
	}
	if value.Outcome == jhlog.BinderOutcomeUnhandled {
		a.stats.Unhandled++
		aggregate.stats.Unhandled++
	}
	if descriptor == "" || header.RunID.IsZero() || header.SegmentStartUnixMS == 0 {
		a.stats.UncorrelatableEvents++
		return
	}
	if len(a.records) >= binderCorrelationEventLimit {
		a.stats.CorrelationDroppedEvents++
		return
	}
	endUnixMS := operationEventUnixMS(header, event.TimeMS)
	durationMS := microsecondsToMillisecondsCeil(value.DurationUS)
	startUnixMS := uint64(0)
	if durationMS < endUnixMS {
		startUnixMS = endUnixMS - durationMS
	}
	a.records = append(a.records, androidBinderRecord{
		runID: header.RunID, processID: header.ProcessInstanceID,
		process: firstNonEmpty(header.ProcessName, "unknown"), descriptor: descriptor, method: method,
		code: value.TransactionCode, startUnixMS: startUnixMS, endUnixMS: endUnixMS,
		durationUS: value.DurationUS, direction: value.Direction,
		oneway: value.Flags&jhlog.BinderFlagOneway != 0,
	})
}

func (a *androidBinderAccumulator) finalize() AndroidBinderAnalysis {
	a.stats.P95ClientDurationUS = a.clientDuration.percentile(0.95)
	a.stats.MaxClientDurationUS = a.clientDuration.max
	groups := make(map[androidBinderCorrelationKey][]int)
	for index := range a.records {
		record := &a.records[index]
		key := androidBinderCorrelationKey{runID: record.runID, descriptor: record.descriptor, code: record.code}
		groups[key] = append(groups[key], index)
	}
	for _, indices := range groups {
		a.correlateGroup(indices)
	}
	for index := range a.records {
		record := &a.records[index]
		if record.matched {
			continue
		}
		if record.direction == jhlog.BinderDirectionClient {
			a.stats.UnmatchedClients++
		} else {
			a.stats.UnmatchedServers++
		}
	}
	for _, aggregate := range a.interfaces {
		aggregate.stats.P95ClientDurationUS = aggregate.clientDuration.percentile(0.95)
		aggregate.stats.MaxClientDurationUS = aggregate.clientDuration.max
		a.stats.Interfaces = append(a.stats.Interfaces, aggregate.stats)
	}
	sort.Slice(a.stats.Interfaces, func(i, j int) bool {
		left, right := a.stats.Interfaces[i], a.stats.Interfaces[j]
		if left.Descriptor != right.Descriptor {
			return left.Descriptor < right.Descriptor
		}
		if left.TransactionCode != right.TransactionCode {
			return left.TransactionCode < right.TransactionCode
		}
		return left.Method < right.Method
	})
	sort.Slice(a.stats.Flows, func(i, j int) bool {
		if a.stats.Flows[i].ClientStartUnixMS != a.stats.Flows[j].ClientStartUnixMS {
			return a.stats.Flows[i].ClientStartUnixMS < a.stats.Flows[j].ClientStartUnixMS
		}
		return a.stats.Flows[i].Descriptor < a.stats.Flows[j].Descriptor
	})
	return a.stats
}

func (a *androidBinderAccumulator) correlateGroup(indices []int) {
	clients := make([]int, 0, len(indices))
	servers := make([]int, 0, len(indices))
	for _, index := range indices {
		if a.records[index].direction == jhlog.BinderDirectionClient {
			clients = append(clients, index)
		} else {
			servers = append(servers, index)
		}
	}
	sort.Slice(clients, func(i, j int) bool { return a.records[clients[i]].startUnixMS < a.records[clients[j]].startUnixMS })
	sort.Slice(servers, func(i, j int) bool { return a.records[servers[i]].startUnixMS < a.records[servers[j]].startUnixMS })
	for _, clientIndex := range clients {
		client := &a.records[clientIndex]
		candidate := -1
		candidateCount := 0
		lower := saturatingSub(client.startUnixMS, binderCorrelationTolerance)
		upper := saturatingUint64Sum(client.endUnixMS, binderCorrelationTolerance)
		if client.oneway {
			upper = saturatingUint64Sum(client.endUnixMS, binderOnewayCorrelationMS)
		}
		start := sort.Search(len(servers), func(index int) bool {
			return a.records[servers[index]].startUnixMS >= lower
		})
		for _, serverIndex := range servers[start:] {
			server := &a.records[serverIndex]
			if server.startUnixMS > upper {
				break
			}
			if server.matched || binderMethodsConflict(client.method, server.method) ||
				!binderRecordsOverlap(*client, *server) {
				continue
			}
			candidate = serverIndex
			candidateCount++
			if candidateCount > 1 {
				break
			}
		}
		if candidateCount != 1 {
			if candidateCount > 1 {
				a.stats.AmbiguousClients++
			}
			continue
		}
		server := &a.records[candidate]
		client.matched = true
		server.matched = true
		a.stats.CorrelatedPairs++
		crossProcess := client.processID != server.processID
		if crossProcess {
			a.stats.CrossProcessPairs++
		}
		method := firstNonEmpty(client.method, server.method)
		confidence := "medium"
		if !client.oneway && method != "" && method != "unknown" {
			confidence = "high"
		}
		if len(a.stats.Flows) < binderFlowOutputLimit {
			a.stats.Flows = append(a.stats.Flows, AndroidBinderFlow{
				Descriptor: client.descriptor, Method: attrValue(method), TransactionCode: client.code,
				ClientProcess: client.process, ServerProcess: server.process,
				ClientStartUnixMS: client.startUnixMS, ClientEndUnixMS: client.endUnixMS,
				ServerStartUnixMS: server.startUnixMS, ServerEndUnixMS: server.endUnixMS,
				ClientDurationUS: client.durationUS, ServerDurationUS: server.durationUS,
				OneWay: client.oneway, CrossProcess: crossProcess, Confidence: confidence,
				ClaimLevel: "correlated",
				Evidence:   binderCorrelationEvidence(client.oneway, confidence),
			})
		}
	}
}

func binderMethodsConflict(client, server string) bool {
	return client != "" && server != "" && client != server
}

func binderRecordsOverlap(client, server androidBinderRecord) bool {
	lower := saturatingSub(client.startUnixMS, binderCorrelationTolerance)
	if server.startUnixMS < lower {
		return false
	}
	if client.oneway {
		upper := saturatingUint64Sum(client.endUnixMS, binderOnewayCorrelationMS)
		return server.startUnixMS >= lower && server.startUnixMS <= upper
	}
	return server.startUnixMS <= saturatingUint64Sum(client.endUnixMS, binderCorrelationTolerance) &&
		server.endUnixMS >= saturatingSub(client.startUnixMS, binderCorrelationTolerance)
}

func binderCorrelationEvidence(oneway bool, confidence string) string {
	if oneway {
		return "Уникальный серверный кандидат с тем же запуском приложения, дескриптором и кодом найден в ограниченном окне после одностороннего клиентского вызова; общего идентификатора вызова нет."
	}
	if confidence == "high" {
		return "Уникальный серверный интервал с тем же запуском приложения, дескриптором, кодом и известным методом AIDL вложен во временное окно клиентского вызова; общего идентификатора вызова нет."
	}
	return "Уникальный серверный интервал с тем же запуском приложения, дескриптором и кодом пересекает клиентский вызов; метод не определён и общего идентификатора вызова нет."
}

func (a *androidBinderAccumulator) findings() []AndroidComponentFinding {
	findings := make([]AndroidComponentFinding, 0, 5)
	if a.stats.SlowMainThreadCalls > 0 {
		findings = append(findings, AndroidComponentFinding{
			ID: "android.binder.main_thread_slow", Severity: "high", Title: "Медленный Binder-вызов на главном потоке",
			Explanation: "Синхронная клиентская транзакция заняла не менее 16 мс и способна задерживать UI.",
			Count:       a.stats.SlowMainThreadCalls, MaxDurationUS: a.stats.MaxClientDurationUS, ClaimLevel: "linked",
		})
	}
	if a.stats.Failures > 0 {
		findings = append(findings, AndroidComponentFinding{
			ID: "android.binder.failure", Severity: "high", Title: "Ошибка Binder-транзакции",
			Explanation: "Клиентская или серверная сторона Binder завершилась структурированной ошибкой; содержимое Parcel и сообщения исключений не анализировались.",
			Count:       a.stats.Failures, ClaimLevel: "linked",
		})
	}
	if a.stats.Unhandled > 0 {
		findings = append(findings, AndroidComponentFinding{
			ID: "android.binder.unhandled", Severity: "medium", Title: "Необработанный код транзакции",
			Explanation: "onTransact или transact вернул false для наблюдаемого кода.",
			Count:       a.stats.Unhandled, ClaimLevel: "linked",
		})
	}
	if a.stats.AmbiguousClients > 0 || a.stats.UnmatchedClients > 0 || a.stats.UnmatchedServers > 0 ||
		a.stats.UncorrelatableEvents > 0 || a.stats.CorrelationDroppedEvents > 0 {
		findings = append(findings, AndroidComponentFinding{
			ID: "android.binder.correlation_incomplete", Severity: "warning", Title: "Цепочка IPC восстановлена частично",
			Explanation: fmt.Sprintf(
				"Без чтения Parcel и общего межпроцессного идентификатора вызова: неоднозначных клиентов=%d, клиентов без пары=%d, серверов без пары=%d, без данных для связи=%d, не сохранено из-за лимита=%d.",
				a.stats.AmbiguousClients, a.stats.UnmatchedClients, a.stats.UnmatchedServers,
				a.stats.UncorrelatableEvents, a.stats.CorrelationDroppedEvents,
			),
			Count: a.stats.AmbiguousClients + a.stats.UnmatchedClients + a.stats.UnmatchedServers +
				a.stats.UncorrelatableEvents + a.stats.CorrelationDroppedEvents,
			ClaimLevel: "unknown",
		})
	}
	return findings
}

func androidAnalysisPartialReasons(quality CollectionQuality, dropped, uncorrelatable uint64) []string {
	reasons := make([]string, 0, 4)
	if quality.ProcessScope == jhlog.ProcessScopeMainOnly.String() {
		reasons = append(reasons, "включён только основной процесс: события Service, Binder и BroadcastReceiver из дополнительных процессов намеренно не собирались")
	}
	if !quality.ProcessRosterDeclarationComplete {
		reasons = append(reasons, "список ожидаемых процессов объявлен не полностью")
	}
	if quality.ExpectedProcessCount > 0 && !quality.ProcessRosterComplete {
		reasons = append(reasons, fmt.Sprintf(
			"записано %d из %d ожидаемых процессов", quality.ObservedProcessCount, quality.ExpectedProcessCount,
		))
	}
	if dropped > 0 {
		reasons = append(reasons, fmt.Sprintf("для восстановления цепочек Binder не сохранено %d событий из-за лимита памяти", dropped))
	}
	if uncorrelatable > 0 {
		reasons = append(reasons, fmt.Sprintf(
			"для восстановления цепочек Binder пропущено %d событий без дескриптора, идентификатора запуска или привязки ко времени",
			uncorrelatable,
		))
	}
	return uniqueStrings(reasons)
}

func androidSeverityRank(value string) int {
	switch value {
	case "high":
		return 3
	case "medium":
		return 2
	case "warning":
		return 1
	default:
		return 0
	}
}

func (b *problemBuilder) detectAndroidComponents() {
	analysis := b.summary.AndroidComponents
	if analysis == nil {
		return
	}
	for _, source := range analysis.Findings {
		if source.ID == "android.receiver.open_at_capture_end" || source.ID == "android.binder.correlation_incomplete" {
			continue
		}
		confidence, reasons, limitations := problemConfidence(b.summary, source.Count, 1, true)
		if analysis.Partial {
			confidence = capProblemConfidence(confidence, "medium")
			reasons = append(reasons, "Данные компонентов Android и IPC для выбранных процессов неполны.")
			limitations = append(limitations, analysis.PartialReasons...)
		}
		if source.ClaimLevel == "hypothesis" {
			confidence = capProblemConfidence(confidence, "medium")
		} else if source.ClaimLevel == "unknown" {
			confidence = "low"
		}
		where := []ProblemLocation{{
			Process: source.Process, Class: source.Component, Method: firstKnown(source.Method, source.Action),
		}}
		evidence := []ProblemEvidence{{
			Name: "Число наблюдений", Observed: fmt.Sprint(source.Count), Unit: "events",
			Sample: u64ptr(source.Count), Source: androidFindingEvidenceSource(source.ID),
		}}
		if source.MaxDurationUS > 0 {
			evidence = append(evidence, ProblemEvidence{
				Name: "Максимальная длительность", Observed: fmt.Sprint(microsecondsToMillisecondsCeil(source.MaxDurationUS)),
				Unit: "ms", Source: androidFindingEvidenceSource(source.ID),
			})
		}
		impact, magnitude := androidFindingPriority(source.Severity)
		cost := &ProblemCost{WallTimeMS: nonZeroU64Ptr(microsecondsToMillisecondsCeil(source.MaxDurationUS))}
		if source.ID == "android.binder.main_thread_slow" {
			cost.MainThreadBlockedMS = cost.WallTimeMS
		}
		limitations = append(limitations,
			"Parcel payload, extras, raw UID/PID and exception messages are intentionally unavailable.",
		)
		b.add(ProblemFinding{
			DetectorID: source.ID, DetectorVersion: b.cfg.Version,
			Category: ProblemCategoryAndroidComponents, Subcategory: strings.TrimPrefix(source.ID, "android."),
			Status: "observed", Confidence: confidence, ConfidenceReasons: uniqueStrings(reasons),
			Title: source.Title, WhatHappened: source.Explanation, Where: where,
			Why: ProblemWhy{
				ClaimLevel: source.ClaimLevel,
				Summary:    "Вывод построен по типизированному lifecycle/transaction событию; межпроцессные связи отдельно маркируются как корреляция, а не exact linkage.",
			},
			Impact: androidFindingImpact(source.ID), Evidence: evidence,
			Frequency: &ProblemFrequency{Count: source.Count}, Cost: cost,
			PriorityBreakdown: priority(
				impact, magnitude, min(20, 4+int(source.Count)*2), 3, 2,
				"влияние Android component/IPC boundary", "тип и длительность наблюдаемого события",
				"число типизированных наблюдений", "конкретный component или interface", "component/IPC контекст",
			),
			Recommendations: []ProblemRecommendation{androidFindingRecommendation(source.ID)},
			Limitations:     uniqueStrings(limitations),
			Drilldowns:      []ProblemDrilldown{{Label: "Android Components и IPC", Anchor: "android-components", Filter: firstKnown(source.Component, source.Descriptor)}},
		})
	}
}

func androidFindingEvidenceSource(id string) string {
	if strings.HasPrefix(id, "android.binder.") {
		return "typed_binder_transaction"
	}
	return "typed_android_component_lifecycle"
}

func androidFindingPriority(severity string) (int, int) {
	switch severity {
	case "high":
		return 32, 22
	case "medium":
		return 24, 15
	default:
		return 12, 8
	}
}

func androidFindingImpact(id string) []string {
	switch id {
	case "android.binder.main_thread_slow", "android.receiver.sync_slow":
		return []string{"Блокировка главного потока может задерживать input, lifecycle и отрисовку кадров"}
	case "android.receiver.async_deadline_risk", "android.service.timeout":
		return []string{"Android может завершить компонент по timeout и зафиксировать ANR либо незавершённую работу"}
	case "android.binder.failure", "android.binder.unhandled":
		return []string{"Межпроцессная операция не достигает ожидаемого результата или требует восстановления соединения"}
	default:
		return []string{"Lifecycle Android-компонента завершился ошибкой или занял значимое время"}
	}
}

func androidFindingRecommendation(id string) ProblemRecommendation {
	switch id {
	case "android.binder.main_thread_slow":
		return ProblemRecommendation{
			Action:       "Убрать синхронный Binder round-trip с главного потока либо сократить server critical path",
			Rationale:    "IPC включает scheduling и работу другого процесса, поэтому задержка напрямую блокирует вызывающий UI thread.",
			Verification: "Повторить сценарий и проверить отсутствие main-thread transact ≥16 мс и улучшение frame/stall tail.",
		}
	case "android.binder.failure", "android.binder.unhandled":
		return ProblemRecommendation{
			Action:       "Проверить version/transaction mapping, обработать binder death и сделать повтор безопасным",
			Rationale:    "Typed outcome локализует сбой на IPC boundary без доступа к payload.",
			Verification: "Повторить тот же AIDL-метод и убедиться, что failure/unhandled события исчезли.",
		}
	case "android.receiver.async_deadline_risk", "android.receiver.sync_slow":
		return ProblemRecommendation{
			Action:       "Оставить в onReceive только маршрутизацию, тяжёлую работу передать scheduler/Worker и гарантировать PendingResult.finish в finally",
			Rationale:    "BroadcastReceiver ограничен системным временем выполнения и блокирует жизненный цикл процесса.",
			Verification: "Повторить action и проверить p95/max receiver duration и отсутствие открытых flow в конце capture.",
		}
	default:
		return ProblemRecommendation{
			Action:       "Сократить синхронную работу callback и явно обработать lifecycle failure/timeout",
			Rationale:    "Component callback является системной границей с ограниченным временем и контролируемым потоком.",
			Verification: "Повторить component flow и сравнить число ошибок, timeout и верхнюю границу длительности.",
		}
	}
}
