package jhlog

import (
	"encoding/hex"
	"fmt"
)

const FormatMarker = 0x81
const FormatMajor = 3
const FormatMinor = 0
const FormatPatch = 0
const FormatVersionString = "3.0.0"

const magicSize = 11

var Magic = []byte{'J', 'H', 'L', 'O', 'G', '\r', '\n', FormatMarker, FormatMajor, FormatMinor, FormatPatch}

const (
	HeaderSchemaV2 uint64 = 2

	FeatureChunkCRCCommit        uint64 = 1 << 0
	FeatureLengthDelimited       uint64 = 1 << 1
	FeatureSymbolRefs            uint64 = 1 << 2
	FeatureProducerMetadata      uint64 = 1 << 3
	FeatureChunkLocalContext     uint64 = 1 << 4
	FeatureQualityRecords        uint64 = 1 << 5
	FeatureEmbeddedStableSymbols uint64 = 1 << 6
	FeatureLogGrowthRecords      uint64 = 1 << 7
	FeatureExactEventAdmission   uint64 = 1 << 8
	FeatureProcessScope          uint64 = 1 << 9
	FeatureColumnarRuntimeCalls  uint64 = 1 << 10
	FeatureSegmentDigestChain    uint64 = 1 << 11
	FeatureProcessRoster         uint64 = 1 << 12
	FeatureOperationLifecycle    uint64 = 1 << 13
	FeatureDatabaseLifecycle     uint64 = 1 << 14
	FeatureProcessState          uint64 = 1 << 15
	FeatureAndroidComponents     uint64 = 1 << 16
	FeatureBinderIPC             uint64 = 1 << 17
	FeatureGZIPChunks            uint64 = 1 << 0
	RequiredFeatures                    = FeatureChunkCRCCommit | FeatureLengthDelimited | FeatureSymbolRefs |
		FeatureProducerMetadata | FeatureChunkLocalContext | FeatureQualityRecords | FeatureEmbeddedStableSymbols |
		FeatureLogGrowthRecords | FeatureExactEventAdmission | FeatureProcessScope | FeatureColumnarRuntimeCalls |
		FeatureSegmentDigestChain | FeatureProcessRoster | FeatureOperationLifecycle | FeatureDatabaseLifecycle |
		FeatureProcessState | FeatureAndroidComponents | FeatureBinderIPC
	BestEffortFeatures      = RequiredFeatures &^ FeatureExactEventAdmission
	OptionalFeatures        = FeatureGZIPChunks
	MaxRuntimeCallBlockRows = 128
)

type ID128 [16]byte

func (id ID128) IsZero() bool {
	return id == ID128{}
}

func DatabaseStatementFingerprint(template string) uint64 {
	if template == "" {
		return 0
	}
	hash := uint64(14695981039346656037)
	for index := 0; index < len(template); index++ {
		hash ^= uint64(template[index])
		hash *= 1099511628211
	}
	if hash == 0 {
		return 1
	}
	return hash
}

type ProcessScope uint64

const (
	ProcessScopeUnknown ProcessScope = iota
	ProcessScopeAll
	ProcessScopeMainOnly
	ProcessScopeAllowlist
)

func (scope ProcessScope) String() string {
	switch scope {
	case ProcessScopeAll:
		return "all_processes"
	case ProcessScopeMainOnly:
		return "main_process_only"
	case ProcessScopeAllowlist:
		return "process_allowlist"
	default:
		return "unknown"
	}
}

type SegmentHeader struct {
	Schema                           uint64       `json:"schema"`
	RequiredFeatures                 uint64       `json:"required_features"`
	OptionalFeatures                 uint64       `json:"optional_features"`
	RunID                            ID128        `json:"run_id"`
	ProcessInstanceID                ID128        `json:"process_instance_id"`
	SessionID                        ID128        `json:"session_id"`
	SegmentIndex                     uint64       `json:"segment_index"`
	OSPID                            uint64       `json:"os_pid"`
	CollectorStartElapsedUS          uint64       `json:"collector_start_elapsed_us"`
	SegmentStartElapsedUS            uint64       `json:"segment_start_elapsed_us"`
	SegmentStartUnixMS               uint64       `json:"segment_start_unix_ms"`
	TimezoneOffsetMinutes            int64        `json:"timezone_offset_minutes"`
	IdentitySource                   uint64       `json:"identity_source"`
	ProcessName                      string       `json:"process_name"`
	SymbolNamespace                  []byte       `json:"symbol_namespace,omitempty"`
	ProcessScope                     ProcessScope `json:"process_scope"`
	AllowedProcessCount              uint64       `json:"allowed_process_count,omitempty"`
	ProcessScopeFingerprint          []byte       `json:"process_scope_fingerprint,omitempty"`
	PreviousSegmentDigest            []byte       `json:"previous_segment_digest,omitempty"`
	ExpectedProcessCount             uint64       `json:"expected_process_count"`
	ExpectedProcessFingerprint       []byte       `json:"expected_process_fingerprint,omitempty"`
	ProcessRosterDeclarationComplete bool         `json:"process_roster_declaration_complete"`
}

func DefaultSegmentHeader() SegmentHeader {
	return SegmentHeader{
		Schema:           HeaderSchemaV2,
		RequiredFeatures: RequiredFeatures,
		OptionalFeatures: OptionalFeatures,
		ProcessScope:     ProcessScopeAll,
	}
}

type SegmentStatus string

const (
	SegmentStatusClosedClean  SegmentStatus = "closed_clean"
	SegmentStatusOpenClean    SegmentStatus = "open_clean"
	SegmentStatusOpenWithTail SegmentStatus = "open_with_tail"
	SegmentStatusCorrupt      SegmentStatus = "corrupt"
)

type StreamResult struct {
	Source                   string               `json:"source"`
	Header                   SegmentHeader        `json:"header"`
	Status                   SegmentStatus        `json:"status"`
	Sealed                   bool                 `json:"sealed"`
	TailBytes                uint64               `json:"tail_bytes,omitempty"`
	CommittedChunks          uint32               `json:"committed_chunks"`
	TotalRecords             uint64               `json:"total_records"`
	DataRecords              uint64               `json:"data_records"`
	DictionaryRecords        uint64               `json:"dictionary_records"`
	ControlRecords           uint64               `json:"control_records"`
	Events                   uint64               `json:"events"` // Semantic data events delivered to the handler.
	LatestQuality            *QualitySnapshot     `json:"latest_quality,omitempty"`
	SegmentEnd               *SegmentEndEvent     `json:"segment_end,omitempty"`
	RawRecordBytes           uint64               `json:"raw_record_bytes,omitempty"`
	StoredChunkBytes         uint64               `json:"stored_chunk_bytes,omitempty"`
	RecordBytesByType        map[EventType]uint64 `json:"record_bytes_by_type,omitempty"`
	RecordsByType            map[EventType]uint64 `json:"records_by_type,omitempty"`
	RuntimeGraphLogicalCalls uint64               `json:"runtime_graph_logical_calls"`
	LogGrowth                *LogGrowthProjection `json:"log_growth,omitempty"`
	SegmentDigest            []byte               `json:"segment_digest,omitempty"`
	InputBytes               uint64               `json:"input_bytes,omitempty"`
	LatestDataEventUnixMS    uint64               `json:"latest_data_event_unix_ms,omitempty"`
}

type LogGrowthProjection struct {
	Generation     uint64             `json:"generation"`
	LiveGeneration uint64             `json:"live_generation,omitempty"`
	HasHistory     bool               `json:"has_history,omitempty"`
	CapturedAtMS   uint64             `json:"captured_at_ms"`
	Sessions       []LogGrowthSession `json:"sessions,omitempty"`
	Days           []LogGrowthDay     `json:"days,omitempty"`
	Live           *LogGrowthSession  `json:"live,omitempty"`
}

type LogGrowthSession struct {
	SessionID            string `json:"session_id"`
	DayKey               uint32 `json:"day_key"`
	StartedAtMS          uint64 `json:"started_at_ms"`
	EndedAtMS            uint64 `json:"ended_at_ms"`
	ConfiguredLimitBytes uint64 `json:"configured_limit_bytes"`
	MaximumRetainedBytes uint64 `json:"maximum_retained_bytes"`
	GeneratedBytes       uint64 `json:"generated_bytes"`
	LimitReachedCount    uint64 `json:"limit_reached_count"`
	SegmentRotationCount uint64 `json:"segment_rotation_count"`
	ArchiveEvictedBytes  uint64 `json:"archive_evicted_bytes"`
	FirstLimitReachedMS  uint64 `json:"first_limit_reached_at_ms"`
	LastLimitReachedMS   uint64 `json:"last_limit_reached_at_ms"`
	Completed            bool   `json:"completed"`
	Recovered            bool   `json:"recovered_after_interruption"`
}

type LogGrowthDay struct {
	DayKey                uint32 `json:"day_key"`
	SessionCount          uint64 `json:"session_count"`
	TotalDurationMS       uint64 `json:"total_duration_ms"`
	GeneratedBytes        uint64 `json:"generated_bytes"`
	MaximumRetainedBytes  uint64 `json:"maximum_retained_bytes"`
	MaximumFillPermille   uint64 `json:"maximum_fill_permille"`
	SessionsReachingLimit uint64 `json:"sessions_reaching_limit"`
	LimitReachedCount     uint64 `json:"limit_reached_count"`
	SegmentRotationCount  uint64 `json:"segment_rotation_count"`
	ArchiveEvictedBytes   uint64 `json:"archive_evicted_bytes"`
}

type EventType uint64

const (
	EventDictionary          EventType = 1
	EventSession             EventType = 2
	EventContext             EventType = 3
	EventHTTP                EventType = 4
	EventUIWindow            EventType = 5
	EventStall               EventType = 6
	EventMemory              EventType = 7
	EventRetained            EventType = 8
	EventCounter             EventType = 9
	EventGauge               EventType = 10
	EventOperation           EventType = 11
	EventLogSpam             EventType = 12
	EventProblem             EventType = 13
	EventRuntimeCall         EventType = 14
	EventQualitySnapshot     EventType = 15
	EventSegmentEnd          EventType = 16
	EventLogGrowth           EventType = 17
	EventProcessExit         EventType = 18
	EventIO                  EventType = 19
	EventWorker              EventType = 20
	EventWebSocket           EventType = 21
	EventDatabase            EventType = 22
	EventDatabaseTransaction EventType = 23
	EventProcessState        EventType = 24
	EventAndroidComponent    EventType = 25
	EventBinderTransaction   EventType = 26
)

// IsSemanticData reports whether a record contributes an application/runtime
// observation. Dictionary and control records are transport metadata and must
// not inflate event sample sizes or observed durations.
func (eventType EventType) IsSemanticData() bool {
	switch eventType {
	case EventSession, EventContext, EventHTTP, EventUIWindow, EventStall, EventMemory,
		EventRetained, EventCounter, EventGauge, EventOperation, EventLogSpam, EventProblem, EventRuntimeCall,
		EventProcessExit, EventIO, EventWorker, EventWebSocket, EventDatabase, EventDatabaseTransaction,
		EventProcessState, EventAndroidComponent, EventBinderTransaction:
		return true
	default:
		return false
	}
}

type EnvelopeFlag uint64

const (
	EnvelopeHasTime       EnvelopeFlag = 1 << 0
	EnvelopeHasThread     EnvelopeFlag = 1 << 1
	EnvelopeHasContext    EnvelopeFlag = 1 << 2
	EnvelopeSameContext   EnvelopeFlag = 1 << 3
	EnvelopeHasAttributes EnvelopeFlag = 1 << 4
)

type Flag uint64

const (
	FlagHTTPReusedConnection   Flag = 1 << 0
	FlagHTTPFailed             Flag = 1 << 1
	FlagHTTPTLS                Flag = 1 << 2
	FlagThreadMain             Flag = 1 << 3
	FlagAppForeground          Flag = 1 << 4
	FlagNetworkMetered         Flag = 1 << 5
	FlagContextLowMemory       Flag = 1 << 6
	FlagNetworkValidated       Flag = 1 << 7
	FlagNetworkVPN             Flag = 1 << 8
	FlagDeviceRooted           Flag = 1 << 9
	FlagHTTPCancelled          Flag = 1 << 10
	FlagHTTPCacheHit           Flag = 1 << 11
	FlagHTTPRequestBytesKnown  Flag = 1 << 12
	FlagHTTPResponseBytesKnown Flag = 1 << 13
	FlagHTTPSlow               Flag = 1 << 15
	FlagUIProblem              Flag = 1 << 16
	FlagHTTPClassified         Flag = 1 << 17
	FlagUIClassified           Flag = 1 << 18
	FlagWorkerPeriodic         Flag = 1 << 20
	FlagWorkerStopReasonKnown  Flag = 1 << 21
	FlagIOBytesKnown           Flag = 1 << 22
)

const semanticAttributeMask = uint64((1<<14)-1) |
	uint64(FlagHTTPSlow|FlagUIProblem|FlagHTTPClassified|FlagUIClassified|
		FlagWorkerPeriodic|FlagWorkerStopReasonKnown|FlagIOBytesKnown)

type SymbolRef struct {
	ID        uint64 `json:"id,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Stable    bool   `json:"stable,omitempty"`
}

func LocalSymbol(id uint64) SymbolRef  { return SymbolRef{ID: id} }
func StableSymbol(id uint64) SymbolRef { return SymbolRef{ID: id, Stable: true} }
func StableSymbolInNamespace(id uint64, namespace []byte) SymbolRef {
	return SymbolRef{ID: id, Stable: true, Namespace: hex.EncodeToString(namespace)}
}
func (r SymbolRef) IsUnknown() bool { return !r.Stable && r.ID == 0 }

type ProducerMetadata struct {
	ElapsedUS uint64 `json:"elapsed_us,omitempty"`
	ThreadID  uint64 `json:"thread_id,omitempty"`
	HasTime   bool   `json:"has_time,omitempty"`
	HasThread bool   `json:"has_thread,omitempty"`
}

type AttributionContext struct {
	Present     bool      `json:"present,omitempty"`
	Screen      SymbolRef `json:"screen,omitempty"`
	Owner       SymbolRef `json:"owner,omitempty"`
	OperationID uint64    `json:"operation_id,omitempty"`
}

type RecordPosition struct {
	ChunkSequence uint32 `json:"chunk_sequence"`
	RecordIndex   uint32 `json:"record_index"`
}

type DictKind uint64

const (
	DictGeneric DictKind = iota
	DictOwner
	DictRoute
	DictScreen
	DictClass
	DictStack
	DictMetric
	DictDevice
	DictAppVersion
	DictBuild
	DictProcess
	DictLogSource
	// DictStableSymbol uses the dictionary record envelope with an ASM-assigned stable ID.
	// It lives in a separate namespace and must never be inserted into the local-ID dictionary.
	DictStableSymbol
	DictOperation
	DictAttributeKey
	DictAttributeValue
)

type NetworkKind uint64

const (
	NetworkUnknown NetworkKind = iota
	NetworkOffline
	NetworkWiFi
	NetworkCellular
	NetworkEthernet
	NetworkVPN
)

type StatusClass uint64

const (
	StatusUnknown StatusClass = iota
	Status1xx
	Status2xx
	Status3xx
	Status4xx
	Status5xx
)

type HTTPFailurePhase uint8

const (
	HTTPFailurePhaseUnknown HTTPFailurePhase = iota
	HTTPFailurePhaseCall
	HTTPFailurePhaseQueue
	HTTPFailurePhaseDNS
	HTTPFailurePhaseConnect
	HTTPFailurePhaseTLS
	HTTPFailurePhaseRequest
	HTTPFailurePhaseResponse
	HTTPFailurePhaseCancelled
)

type HTTPFailureKind uint8

const (
	HTTPFailureKindUnknown HTTPFailureKind = iota
	HTTPFailureKindDNS
	HTTPFailureKindTimeout
	HTTPFailureKindConnection
	HTTPFailureKindTLS
	HTTPFailureKindProtocol
	HTTPFailureKindCancelled
	HTTPFailureKindIO
	HTTPFailureKindOther
)

type HTTPProtocol uint8

const (
	HTTPProtocolUnknown HTTPProtocol = iota
	HTTPProtocol1_0
	HTTPProtocol1_1
	HTTPProtocol2
	HTTPProtocol3
)

func StatusClassForHTTPCode(code uint16) StatusClass {
	if code < 100 || code > 599 {
		return StatusUnknown
	}
	return StatusClass(code / 100)
}

type MetricMode uint64

const (
	MetricModeUnknown MetricMode = iota
	MetricModeAverage
	MetricModeLast
	MetricModeState
	MetricModeBooleanRate
)

// RetentionEvidence describes what the Android runtime did before emitting a retained event.
// Neither runtime value proves a leak; a confirmed reference path is produced only by HPROF
// analysis and therefore does not have a wire value here.
type RetentionEvidence uint64

const (
	RetentionEvidenceUnknown RetentionEvidence = iota
	RetentionEvidenceTimeOnly
	RetentionEvidenceAfterExplicitGC
)

func (e RetentionEvidence) Effective() RetentionEvidence {
	if e == RetentionEvidenceAfterExplicitGC {
		return e
	}
	return RetentionEvidenceTimeOnly
}

type CollectorFlag uint64

const (
	CollectorFPS CollectorFlag = 1 << iota
	CollectorJankStats
	CollectorProcessExit
	CollectorIOTracing
	CollectorSystemSampler
	CollectorMainThreadStalls
	CollectorRetainedObjects
	CollectorCompose
	CollectorRoom
	CollectorWorker
	CollectorDatabase
)

const CollectorKnownMask = CollectorFPS | CollectorJankStats | CollectorProcessExit | CollectorIOTracing |
	CollectorSystemSampler | CollectorMainThreadStalls | CollectorRetainedObjects | CollectorCompose |
	CollectorRoom | CollectorWorker | CollectorDatabase

type UIFrameSource uint64

const (
	UIFrameSourceUnknown UIFrameSource = iota
	UIFrameSourceJankStats
	UIFrameSourceChoreographer
)

// UIFrameHistogramUpperUS is the shared mergeable frame-duration histogram contract. The final
// bucket is overflow (> the last boundary). Changing these boundaries requires a wire-major bump.
var UIFrameHistogramUpperUS = [...]uint64{8_000, 12_000, 16_000, 20_000, 24_000, 32_000, 40_000, 50_000, 67_000, 100_000, 250_000, 1_000_000}

const UIFrameHistogramBucketCount = len(UIFrameHistogramUpperUS) + 1

func UIFrameHistogramQuantileMS(buckets []uint64, percentile uint64) uint64 {
	if percentile > 100 || len(buckets) != UIFrameHistogramBucketCount {
		return 0
	}
	var total uint64
	for _, count := range buckets {
		total += count
	}
	if total == 0 {
		return 0
	}
	target := ((total-1)*percentile)/100 + 1
	var seen uint64
	for index, count := range buckets {
		seen += count
		if seen < target {
			continue
		}
		if index < len(UIFrameHistogramUpperUS) {
			return (UIFrameHistogramUpperUS[index] + 999) / 1_000
		}
		return UIFrameHistogramUpperUS[len(UIFrameHistogramUpperUS)-1]/1_000 + 1
	}
	return 0
}

type IOOperationKind uint64

const (
	IOOperationUnknown IOOperationKind = iota
	IOOperationFileRead
	IOOperationFileWrite
	IOOperationFileSync
	_
	_
	IOOperationContentRead
	IOOperationContentWrite
)

func (e RetentionEvidence) String() string {
	switch e.Effective() {
	case RetentionEvidenceAfterExplicitGC:
		return "after_explicit_gc"
	default:
		return "time_only"
	}
}

type DictionaryEntry struct {
	Kind     DictKind `json:"kind"`
	ID       uint64   `json:"id"`
	Encoding uint64   `json:"encoding,omitempty"`
	Data     []byte   `json:"data,omitempty"`
	Value    string   `json:"value"`
}

type Event struct {
	Type                EventType                 `json:"type"`
	TimeUS              uint64                    `json:"time_us,omitempty"`
	DeltaUS             int64                     `json:"delta_us,omitempty"`
	TimeMS              uint64                    `json:"time_ms"`
	DeltaMS             uint64                    `json:"delta_ms,omitempty"`
	Flags               uint64                    `json:"flags,omitempty"`
	Producer            ProducerMetadata          `json:"producer,omitempty"`
	Attribution         AttributionContext        `json:"attribution,omitempty"`
	Position            RecordPosition            `json:"position,omitempty"`
	Source              string                    `json:"source,omitempty"`
	Dictionary          *DictionaryEntry          `json:"dictionary,omitempty"`
	Session             *SessionEvent             `json:"session,omitempty"`
	Context             *ContextEvent             `json:"context,omitempty"`
	HTTP                *HTTPEvent                `json:"http,omitempty"`
	UIWindow            *UIWindowEvent            `json:"ui_window,omitempty"`
	Stall               *StallEvent               `json:"stall,omitempty"`
	Memory              *MemoryEvent              `json:"memory,omitempty"`
	Retained            *RetainedEvent            `json:"retained,omitempty"`
	Metric              *MetricEvent              `json:"metric,omitempty"`
	Operation           *OperationEvent           `json:"operation,omitempty"`
	LogSpam             *LogSpamEvent             `json:"log_spam,omitempty"`
	Problem             *ProblemEvent             `json:"problem,omitempty"`
	RuntimeCall         *RuntimeCallEvent         `json:"runtime_call,omitempty"`
	ProcessExit         *ProcessExitEvent         `json:"process_exit,omitempty"`
	IO                  *IOEvent                  `json:"io,omitempty"`
	Worker              *WorkerEvent              `json:"worker,omitempty"`
	WebSocket           *WebSocketEvent           `json:"websocket,omitempty"`
	Database            *DatabaseEvent            `json:"database,omitempty"`
	DatabaseTransaction *DatabaseTransactionEvent `json:"database_transaction,omitempty"`
	ProcessState        *ProcessStateEvent        `json:"process_state,omitempty"`
	AndroidComponent    *AndroidComponentEvent    `json:"android_component,omitempty"`
	BinderTransaction   *BinderTransactionEvent   `json:"binder_transaction,omitempty"`
	Quality             *QualitySnapshot          `json:"quality,omitempty"`
	SegmentEnd          *SegmentEndEvent          `json:"segment_end,omitempty"`
	LogGrowth           *LogGrowthRecord          `json:"log_growth,omitempty"`

	// runtimeCalls exists only while one columnar wire record is expanded into semantic events.
	runtimeCalls []runtimeCallRow
}

type LogGrowthRecordKind uint64

const (
	LogGrowthHistory LogGrowthRecordKind = 1
	LogGrowthLive    LogGrowthRecordKind = 2
)

type LogGrowthRecord struct {
	Kind       LogGrowthRecordKind  `json:"kind"`
	Generation uint64               `json:"generation"`
	Projection *LogGrowthProjection `json:"projection,omitempty"`
	Live       *LogGrowthSession    `json:"live,omitempty"`
	Raw        []byte               `json:"-"`
}

type SessionEvent struct {
	AppVersionRef     SymbolRef `json:"app_version_ref,omitempty"`
	BuildRef          SymbolRef `json:"build_ref,omitempty"`
	DeviceRef         SymbolRef `json:"device_ref,omitempty"`
	AndroidReleaseRef SymbolRef `json:"android_release_ref,omitempty"`
	SecurityPatchRef  SymbolRef `json:"security_patch_ref,omitempty"`
	PrimaryABIRef     SymbolRef `json:"primary_abi_ref,omitempty"`
	SupportedABIsRef  SymbolRef `json:"supported_abis_ref,omitempty"`
	ManufacturerRef   SymbolRef `json:"manufacturer_ref,omitempty"`
	BrandRef          SymbolRef `json:"brand_ref,omitempty"`
	HardwareRef       SymbolRef `json:"hardware_ref,omitempty"`
	BoardRef          SymbolRef `json:"board_ref,omitempty"`
	ProductRef        SymbolRef `json:"product_ref,omitempty"`
	SDKInt            uint64    `json:"sdk_int"`
	CollectorFlags    uint64    `json:"collector_flags,omitempty"`
	ProcessName       string    `json:"process_name,omitempty"`
	DeviceRooted      bool      `json:"device_rooted,omitempty"`
}

type ContextEvent struct {
	Network          NetworkKind `json:"network"`
	BatteryPct       uint64      `json:"battery_pct"`
	AvailMemoryKB    uint64      `json:"avail_memory_kb"`
	TotalMemoryKB    uint64      `json:"total_memory_kb,omitempty"`
	BatteryState     uint64      `json:"battery_state,omitempty"`
	BatteryTempDeciC int64       `json:"battery_temp_deci_c,omitempty"`
	LowMemory        bool        `json:"low_memory,omitempty"`
	NetworkMetered   bool        `json:"network_metered,omitempty"`
	NetworkValidated bool        `json:"network_validated,omitempty"`
	NetworkVPN       bool        `json:"network_vpn,omitempty"`
	RxBytes          uint64      `json:"rx_bytes,omitempty"`
	TxBytes          uint64      `json:"tx_bytes,omitempty"`
	FreeStorageKB    uint64      `json:"free_storage_kb,omitempty"`
	TotalStorageKB   uint64      `json:"total_storage_kb,omitempty"`
}

type HTTPEvent struct {
	RouteRef        SymbolRef        `json:"route_ref,omitempty"`
	ServiceRef      SymbolRef        `json:"service_ref,omitempty"`
	InitiatorRef    SymbolRef        `json:"initiator_ref,omitempty"`
	DurationMS      uint64           `json:"duration_ms"`
	QueueMS         uint64           `json:"queue_ms"`
	DNSMS           uint64           `json:"dns_ms"`
	ConnectMS       uint64           `json:"connect_ms"`
	TLSMS           uint64           `json:"tls_ms"`
	RequestMS       uint64           `json:"request_ms"`
	TTFBMS          uint64           `json:"ttfb_ms"`
	ResponseMS      uint64           `json:"response_ms"`
	RxBytes         uint64           `json:"rx_bytes"`
	TxBytes         uint64           `json:"tx_bytes"`
	Status          StatusClass      `json:"status_class"`
	StatusCode      uint16           `json:"status_code"`
	Attempts        uint16           `json:"attempts"`
	DNSAttempts     uint16           `json:"dns_attempts"`
	ConnectAttempts uint16           `json:"connect_attempts"`
	TLSAttempts     uint16           `json:"tls_attempts"`
	ConnectFailures uint16           `json:"connect_failures"`
	TLSFailures     uint16           `json:"tls_failures"`
	Redirects       uint16           `json:"redirects"`
	FailurePhase    HTTPFailurePhase `json:"failure_phase"`
	FailureKind     HTTPFailureKind  `json:"failure_kind"`
	Protocol        HTTPProtocol     `json:"protocol"`
}

type UIWindowEvent struct {
	WindowMS             uint64        `json:"window_ms"`
	FrameCount           uint64        `json:"frame_count"`
	JankCount            uint64        `json:"jank_count"`
	Source               UIFrameSource `json:"source"`
	FrameDeadlineUS      uint64        `json:"frame_deadline_us"`
	FrameDurationBuckets []uint64      `json:"frame_duration_buckets"`
	P50MS                uint64        `json:"derived_p50_ms"`
	P95MS                uint64        `json:"derived_p95_ms"`
	P99MS                uint64        `json:"derived_p99_ms"`
}

type StallEvent struct {
	StackRef   SymbolRef `json:"stack_ref,omitempty"`
	DurationMS uint64    `json:"duration_ms"`
}

type MemoryEvent struct {
	PSSKB        uint64 `json:"pss_kb"`
	JavaHeapKB   uint64 `json:"java_heap_kb"`
	NativeHeapKB uint64 `json:"native_heap_kb"`
}

type RetainedEvent struct {
	ClassRef  SymbolRef         `json:"class_ref,omitempty"`
	HolderRef SymbolRef         `json:"holder_ref,omitempty"`
	AgeMS     uint64            `json:"age_ms"`
	Count     uint64            `json:"count"`
	Evidence  RetentionEvidence `json:"evidence"`
}

type MetricEvent struct {
	MetricRef SymbolRef  `json:"metric_ref,omitempty"`
	Value     uint64     `json:"value"`
	Count     uint64     `json:"count,omitempty"`
	Sum       uint64     `json:"sum,omitempty"`
	Max       uint64     `json:"max,omitempty"`
	Mode      MetricMode `json:"mode,omitempty"`
}

const MaxOperationAttributes = 8

type OperationPhase uint8

const (
	OperationPhaseUnknown OperationPhase = iota
	OperationPhaseStarted
	OperationPhaseFinished
)

type OperationKind uint8

const (
	OperationKindUnknown OperationKind = iota
	OperationKindUser
	OperationKindScreen
	OperationKindBackground
	OperationKindSystem
	OperationKindStage
)

type OperationOutcome uint8

const (
	OperationOutcomeUnknown OperationOutcome = iota
	OperationOutcomeSuccess
	OperationOutcomeFailure
	OperationOutcomeCancelled
	OperationOutcomeTimeout
)

type OperationAttribute struct {
	KeyRef   SymbolRef `json:"key_ref"`
	ValueRef SymbolRef `json:"value_ref"`
}

type OperationEvent struct {
	NameRef    SymbolRef            `json:"name_ref"`
	ID         uint64               `json:"id"`
	ParentID   uint64               `json:"parent_id,omitempty"`
	Phase      OperationPhase       `json:"phase"`
	Kind       OperationKind        `json:"kind"`
	Outcome    OperationOutcome     `json:"outcome,omitempty"`
	DurationUS uint64               `json:"duration_us,omitempty"`
	BudgetUS   uint64               `json:"budget_us,omitempty"`
	Attributes []OperationAttribute `json:"attributes,omitempty"`
}

type LogSpamEvent struct {
	SourceRef SymbolRef `json:"source_ref,omitempty"`
	Level     uint64    `json:"level"`
	Count     uint64    `json:"count"`
}

type ProblemEvent struct {
	KindRef  SymbolRef `json:"kind_ref,omitempty"`
	WindowMS uint64    `json:"window_ms"`
	Count    uint64    `json:"count"`
	MaxMS    uint64    `json:"max_ms"`
}

type RuntimeCallEvent struct {
	CalleeRef SymbolRef `json:"callee_ref,omitempty"`
	Count     uint64    `json:"count"`
	TotalMS   uint64    `json:"total_ms"`
	MaxMS     uint64    `json:"max_ms"`
}

type ProcessExitEvent struct {
	Reason          uint64    `json:"reason"`
	TimestampUnixMS uint64    `json:"timestamp_unix_ms"`
	Importance      uint64    `json:"importance"`
	PSSKB           uint64    `json:"pss_kb"`
	RSSKB           uint64    `json:"rss_kb"`
	ProcessRef      SymbolRef `json:"process_ref,omitempty"`
}

type IOEvent struct {
	SourceRef  SymbolRef       `json:"source_ref,omitempty"`
	Operation  IOOperationKind `json:"operation"`
	Outcome    IOOutcome       `json:"outcome"`
	DurationUS uint64          `json:"duration_us"`
	Bytes      uint64          `json:"bytes,omitempty"`
}

type IOOutcome uint8

const (
	IOOutcomeUnknown IOOutcome = iota
	IOOutcomeSuccess
	IOOutcomeFailure
)

type WorkerStage uint8

const (
	WorkerStageUnknown WorkerStage = iota
	WorkerStageEnqueued
	WorkerStageStarted
	WorkerStageFinished
)

type WorkerOutcome uint8

const (
	WorkerOutcomeUnknown WorkerOutcome = iota
	WorkerOutcomeSuccess
	WorkerOutcomeFailure
	WorkerOutcomeRetry
	WorkerOutcomeCancelled
)

type WorkerEvent struct {
	WorkerRef  SymbolRef     `json:"worker_ref,omitempty"`
	InstanceID uint64        `json:"instance_id"`
	Stage      WorkerStage   `json:"stage"`
	Outcome    WorkerOutcome `json:"outcome"`
	DurationMS uint64        `json:"duration_ms"`
	RunAttempt uint32        `json:"run_attempt"`
	Generation uint32        `json:"generation"`
	StopReason uint32        `json:"stop_reason"`
}

type WebSocketStage uint8

const (
	WebSocketStageUnknown WebSocketStage = iota
	WebSocketStageOpened
	WebSocketStageClosed
	WebSocketStageFailed
)

type WebSocketFailureKind uint8

const (
	WebSocketFailureUnknown WebSocketFailureKind = iota
	WebSocketFailureTimeout
	WebSocketFailureConnection
	WebSocketFailureTLS
	WebSocketFailureProtocol
	WebSocketFailureIO
	WebSocketFailureOther
)

// WebSocketEvent is a bounded lifecycle snapshot. Message bodies, URLs and
// throwable strings are deliberately excluded from the wire format.
type WebSocketEvent struct {
	RouteRef         SymbolRef            `json:"route_ref,omitempty"`
	ConnectionID     uint64               `json:"connection_id"`
	Stage            WebSocketStage       `json:"stage"`
	DurationMS       uint64               `json:"duration_ms"`
	StatusCode       uint16               `json:"status_code,omitempty"`
	CloseCode        uint16               `json:"close_code,omitempty"`
	FailureKind      WebSocketFailureKind `json:"failure_kind,omitempty"`
	TextMessages     uint64               `json:"text_messages,omitempty"`
	BinaryMessages   uint64               `json:"binary_messages,omitempty"`
	ReceivedBytes    uint64               `json:"received_bytes,omitempty"`
	ReconnectOrdinal uint32               `json:"reconnect_ordinal,omitempty"`
}

type DatabaseFramework uint8

const (
	DatabaseFrameworkUnknown DatabaseFramework = iota
	DatabaseFrameworkSQLite
	DatabaseFrameworkSupportSQLite
	DatabaseFrameworkRoom
	DatabaseFrameworkCustom
)

type DatabaseOperation uint8

const (
	DatabaseOperationUnknown DatabaseOperation = iota
	DatabaseOperationQuery
	DatabaseOperationInsert
	DatabaseOperationUpdate
	DatabaseOperationDelete
	DatabaseOperationExecute
	DatabaseOperationStatement
)

type DatabaseOutcome uint8

const (
	DatabaseOutcomeUnknown DatabaseOutcome = iota
	DatabaseOutcomeSuccess
	DatabaseOutcomeFailure
)

type DatabaseFailureKind uint8

const (
	DatabaseFailureNone DatabaseFailureKind = iota
	DatabaseFailureCancelled
	DatabaseFailureBusyLocked
	DatabaseFailureConstraint
	DatabaseFailureDiskFull
	DatabaseFailureCorruption
	DatabaseFailureTimeout
	DatabaseFailureOther
)

type DatabaseBoundary uint8

const (
	DatabaseBoundaryUnknown DatabaseBoundary = iota
	DatabaseBoundaryDispatch
	DatabaseBoundaryExecute
	DatabaseBoundaryMaterialize
	DatabaseBoundaryManual
)

type DatabaseResultKind uint8

const (
	DatabaseResultUnknown DatabaseResultKind = iota
	DatabaseResultRows
	DatabaseResultAffectedRows
)

type DatabaseCountBucket uint8

const (
	DatabaseCountUnknown DatabaseCountBucket = iota
	DatabaseCountZero
	DatabaseCountOne
	DatabaseCountTwoToTen
	DatabaseCountElevenToHundred
	DatabaseCountOverHundred
)

type DatabasePhase uint64

const (
	DatabasePhasePoolWait DatabasePhase = 1 << iota
	DatabasePhaseLockWait
	DatabasePhaseExecute
	DatabasePhaseMaterialize
)

type DatabaseEvent struct {
	QueryRef             SymbolRef           `json:"query_ref,omitempty"`
	SourceRef            SymbolRef           `json:"source_ref"`
	StatementFingerprint uint64              `json:"statement_fingerprint,omitempty"`
	Framework            DatabaseFramework   `json:"framework"`
	Operation            DatabaseOperation   `json:"operation"`
	Outcome              DatabaseOutcome     `json:"outcome"`
	FailureKind          DatabaseFailureKind `json:"failure_kind,omitempty"`
	Boundary             DatabaseBoundary    `json:"boundary"`
	ResultKnown          bool                `json:"result_known"`
	ResultKind           DatabaseResultKind  `json:"result_kind,omitempty"`
	ResultCountBucket    DatabaseCountBucket `json:"result_count_bucket,omitempty"`
	TransactionID        uint64              `json:"transaction_id,omitempty"`
	StatementToken       uint64              `json:"statement_token,omitempty"`
	PhaseMask            DatabasePhase       `json:"phase_mask,omitempty"`
	PoolWaitUS           uint64              `json:"pool_wait_us,omitempty"`
	LockWaitUS           uint64              `json:"lock_wait_us,omitempty"`
	ExecuteUS            uint64              `json:"execute_us,omitempty"`
	MaterializeUS        uint64              `json:"materialize_us,omitempty"`
	DurationUS           uint64              `json:"duration_us"`
}

type DatabaseTransactionStage uint8

const (
	DatabaseTransactionStageUnknown DatabaseTransactionStage = iota
	DatabaseTransactionBegin
	DatabaseTransactionTerminal
)

type DatabaseTransactionMode uint8

const (
	DatabaseTransactionModeUnknown DatabaseTransactionMode = iota
	DatabaseTransactionDeferred
	DatabaseTransactionImmediate
	DatabaseTransactionExclusive
	DatabaseTransactionReadOnly
)

type DatabaseTransactionOutcome uint8

const (
	DatabaseTransactionOutcomeUnknown DatabaseTransactionOutcome = iota
	DatabaseTransactionSuccess
	DatabaseTransactionRollback
	DatabaseTransactionFailure
)

type DatabaseTransactionEvent struct {
	SourceRef      SymbolRef                  `json:"source_ref"`
	TransactionID  uint64                     `json:"transaction_id"`
	ParentID       uint64                     `json:"parent_id,omitempty"`
	Stage          DatabaseTransactionStage   `json:"stage"`
	Mode           DatabaseTransactionMode    `json:"mode,omitempty"`
	Outcome        DatabaseTransactionOutcome `json:"outcome,omitempty"`
	FailureKind    DatabaseFailureKind        `json:"failure_kind,omitempty"`
	DurationUS     uint64                     `json:"duration_us,omitempty"`
	StatementCount uint64                     `json:"statement_count,omitempty"`
	ReadCount      uint64                     `json:"read_count,omitempty"`
	WriteCount     uint64                     `json:"write_count,omitempty"`
}

type ProcessUIVisibility uint8

const (
	ProcessUIUnknown ProcessUIVisibility = iota
	ProcessUIHidden
	ProcessUIVisible
)

type ProcessImportance uint8

const (
	ProcessImportanceUnknown ProcessImportance = iota
	ProcessImportanceForeground
	ProcessImportanceForegroundService
	ProcessImportanceVisible
	ProcessImportancePerceptible
	ProcessImportanceService
	ProcessImportanceCached
)

type ProcessStateReason uint8

const (
	ProcessStateReasonUnknown ProcessStateReason = iota
	ProcessStateReasonPeriodicSample
	ProcessStateReasonUILifecycle
	ProcessStateReasonComponentLifecycle
)

type ProcessStateEvent struct {
	UIVisibility      ProcessUIVisibility `json:"ui_visibility"`
	Importance        ProcessImportance   `json:"importance"`
	AndroidImportance uint32              `json:"android_importance"`
	Reason            ProcessStateReason  `json:"reason"`
}

type ComponentKind uint8

const (
	ComponentKindUnknown ComponentKind = iota
	ComponentKindService
	ComponentKindReceiver
)

type ComponentStage uint8

const (
	ComponentStageUnknown           ComponentStage = 0
	ComponentServiceCreated         ComponentStage = 1
	ComponentServiceStartCommand    ComponentStage = 2
	ComponentServiceBind            ComponentStage = 3
	ComponentServiceUnbind          ComponentStage = 4
	ComponentServiceRebind          ComponentStage = 5
	ComponentServiceTaskRemoved     ComponentStage = 6
	ComponentServiceForegroundEnter ComponentStage = 7
	ComponentServiceForegroundExit  ComponentStage = 8
	ComponentServiceDestroyed       ComponentStage = 9
	ComponentServiceTimeout         ComponentStage = 10
	ComponentReceiverStarted        ComponentStage = 20
	ComponentReceiverAsyncStarted   ComponentStage = 21
	ComponentReceiverFinished       ComponentStage = 22
)

type ComponentOutcome uint8

const (
	ComponentOutcomeUnknown ComponentOutcome = iota
	ComponentOutcomeSuccess
	ComponentOutcomeFailure
	ComponentOutcomeTimeout
	ComponentOutcomeCancelled
)

type ComponentFlag uint64

const (
	ComponentFlagForeground ComponentFlag = 1 << iota
	ComponentFlagAsync
	ComponentFlagOrdered
	ComponentFlagSticky
	ComponentFlagBound
	componentFlagKnownMask = ComponentFlagForeground | ComponentFlagAsync | ComponentFlagOrdered |
		ComponentFlagSticky | ComponentFlagBound
)

type AndroidComponentEvent struct {
	ComponentRef SymbolRef        `json:"component_ref"`
	ActionRef    SymbolRef        `json:"action_ref,omitempty"`
	InstanceID   uint64           `json:"instance_id"`
	FlowID       uint64           `json:"flow_id"`
	Kind         ComponentKind    `json:"kind"`
	Stage        ComponentStage   `json:"stage"`
	Outcome      ComponentOutcome `json:"outcome,omitempty"`
	DurationUS   uint64           `json:"duration_us,omitempty"`
	Flags        ComponentFlag    `json:"flags,omitempty"`
}

type BinderDirection uint8

const (
	BinderDirectionUnknown BinderDirection = iota
	BinderDirectionClient
	BinderDirectionServer
)

type BinderOutcome uint8

const (
	BinderOutcomeUnknown BinderOutcome = iota
	BinderOutcomeSuccess
	BinderOutcomeFailure
	BinderOutcomeUnhandled
)

type BinderFailureKind uint8

const (
	BinderFailureNone BinderFailureKind = iota
	BinderFailureRemote
	BinderFailureDeadObject
	BinderFailureSecurity
	BinderFailureTimeout
	BinderFailureOther
)

type BinderFlag uint64

const (
	BinderFlagOneway    BinderFlag = 1 << iota
	binderFlagKnownMask            = BinderFlagOneway
)

type BinderTransactionEvent struct {
	DescriptorRef   SymbolRef         `json:"descriptor_ref,omitempty"`
	MethodRef       SymbolRef         `json:"method_ref,omitempty"`
	CallID          uint64            `json:"call_id"`
	Direction       BinderDirection   `json:"direction"`
	TransactionCode uint32            `json:"transaction_code"`
	Outcome         BinderOutcome     `json:"outcome"`
	FailureKind     BinderFailureKind `json:"failure_kind,omitempty"`
	DurationUS      uint64            `json:"duration_us,omitempty"`
	Flags           BinderFlag        `json:"flags,omitempty"`
}

type runtimeCallRow struct {
	screen      SymbolRef
	caller      SymbolRef
	operationID uint64
	callee      SymbolRef
	count       uint64
	total       uint64
	max         uint64
}

type QualitySnapshot struct {
	Sequence          uint64            `json:"sequence"`
	CapturedElapsedUS uint64            `json:"captured_elapsed_us"`
	Counters          map[uint64]uint64 `json:"counters"`
}

type SegmentEndReason uint64

const (
	SegmentEndNormal SegmentEndReason = iota
	SegmentEndSizeLimit
	SegmentEndIOError
	SegmentEndShutdown
	SegmentEndRotation
	SegmentEndStorageBudget
)

func (reason SegmentEndReason) String() string {
	switch reason {
	case SegmentEndNormal:
		return "normal"
	case SegmentEndSizeLimit:
		return "size_limit"
	case SegmentEndIOError:
		return "io_error"
	case SegmentEndShutdown:
		return "shutdown"
	case SegmentEndRotation:
		return "rotation"
	case SegmentEndStorageBudget:
		return "storage_budget_exhausted"
	default:
		return fmt.Sprintf("unknown(%d)", uint64(reason))
	}
}

type SegmentEndEvent struct {
	Reason                 SegmentEndReason `json:"reason"`
	TotalEventRecords      uint64           `json:"total_event_records"`
	TotalDictionaryRecords uint64           `json:"total_dictionary_records"`
	LastQualitySequence    uint64           `json:"last_quality_sequence"`
}

func (reason SegmentEndReason) supported() bool {
	return reason >= SegmentEndNormal && reason <= SegmentEndStorageBudget
}

const (
	QualityAcceptedEventTotal             uint64 = 1
	QualityWrittenEventTotal              uint64 = 2
	QualityQueueFullTotal                 uint64 = 3
	QualityNotAcceptingTotal              uint64 = 4
	QualityControlLaneFullTotal           uint64 = 5
	QualityControlTimeoutTotal            uint64 = 6
	QualityControlInterruptedTotal        uint64 = 7
	QualityWriterIOErrorTotal             uint64 = 8
	QualityEventLostAfterIOTotal          uint64 = 9
	QualityDictionaryOverflowTotal        uint64 = 10
	QualityDictionaryValueTruncated       uint64 = 11
	QualityOversizedRecordTotal           uint64 = 12
	QualityCommittedChunkTotal            uint64 = 13
	QualityFailedChunkTotal               uint64 = 14
	QualityRecoveryTotal                  uint64 = 15
	QualityCloseTimeoutTotal              uint64 = 16
	QualityEventLostAfterSizeLimitTotal   uint64 = 17
	QualityWriterAdmissionContentionTotal uint64 = 18
	QualityEventLostAfterStorageBudget    uint64 = 19

	QualityMetricCardinalityLoss             uint64 = 0x2000
	QualityInvalidMetric                     uint64 = 0x2001
	QualityRuntimeStackMismatch              uint64 = 0x2003
	QualityLogSpamCardinalityLoss            uint64 = 0x2004
	QualityHandlerEntryLimit                 uint64 = 0x2005
	QualityHandlerWrapperLimit               uint64 = 0x2006
	QualityLifecycleRegistryLimit            uint64 = 0x2007
	QualityObjectWatcherLimit                uint64 = 0x2008
	QualityJankStatsHandleLimit              uint64 = 0x2009
	QualityMetricFlushTimeout                uint64 = 0x200a
	QualityRuntimeGraphShutdownLoss          uint64 = 0x200f
	QualityRuntimeGraphWriterRejectionLoss   uint64 = 0x2010
	QualityHandlerContentionBypass           uint64 = 0x2013
	QualityRuntimeEventBufferCapacityLoss    uint64 = 0x2017
	QualityRuntimeEventRegistryCapacityLoss  uint64 = 0x2018
	QualityMethodCounterCardinalityLoss      uint64 = 0x2019
	QualityRuntimeEventWriterRejectionLoss   uint64 = 0x201a
	QualityRuntimeGraphInputTotal            uint64 = 0x201b
	QualityRuntimeGraphEmittedTotal          uint64 = 0x201c
	QualityRuntimeGraphBackpressureCount     uint64 = 0x201f
	QualityRuntimeGraphBackpressureNanos     uint64 = 0x2020
	QualityWriterBackpressureCount           uint64 = 0x2021
	QualityWriterBackpressureNanos           uint64 = 0x2022
	QualityRuntimeEventBackpressureCount     uint64 = 0x2023
	QualityRuntimeEventBackpressureNanos     uint64 = 0x2024
	QualityRuntimeGraphDisabled              uint64 = 0x2025
	QualityRuntimeHookFailureTotal           uint64 = 0x2026
	QualityArchiveEvictedRunTotal            uint64 = 0x2027
	QualityArchiveEvictedSegmentTotal        uint64 = 0x2028
	QualityArchiveEvictedBytesTotal          uint64 = 0x2029
	QualityRuntimeHookInstrumentationFailure uint64 = 0x202a
	QualityRuntimeHookAsyncWrapperFailure    uint64 = 0x202b
	QualityRuntimeHookLifecycleFailure       uint64 = 0x202c
	QualityRuntimeHookCollectorFailure       uint64 = 0x202d
	QualityRuntimeHookContextFailure         uint64 = 0x202e
	QualityRuntimeHookSchedulerFailure       uint64 = 0x202f
	QualityJankStatsDependencyMissing        uint64 = 0x2030
	QualityJankStatsInstallFailure           uint64 = 0x2031
	QualityJankStatsFrameFailure             uint64 = 0x2032
	QualityJankStatsControlFailure           uint64 = 0x2033
	QualityRuntimeHookUnclassifiedFailure    uint64 = 0x2034
	QualityPreparedStatementRegistryEviction uint64 = 0x2035
	QualityPreparedStatementResolutionMiss   uint64 = 0x2036
	QualityReceiverAsyncRegistryEviction     uint64 = 0x2037
	QualityReceiverAsyncResolutionMiss       uint64 = 0x2038
)

type QualityLossReason uint64

const (
	QualityLossQueueFull           QualityLossReason = 1
	QualityLossNotAccepting        QualityLossReason = 2
	QualityLossIOLost              QualityLossReason = 3
	QualityLossOversized           QualityLossReason = 4
	QualityLossSizeLimit           QualityLossReason = 5
	QualityLossAdmissionContention QualityLossReason = 6
	QualityLossStorageBudget       QualityLossReason = 7
)

func EventQualityCounterID(eventType EventType, reason QualityLossReason) uint64 {
	return 0x1000 + uint64(eventType)*16 + uint64(reason)
}

func QualityCounterName(id uint64) string {
	switch id {
	case QualityAcceptedEventTotal:
		return "accepted_event_total"
	case QualityWrittenEventTotal:
		return "written_event_total"
	case QualityQueueFullTotal:
		return "queue_full_total"
	case QualityNotAcceptingTotal:
		return "not_accepting_total"
	case QualityControlLaneFullTotal:
		return "control_lane_full_total"
	case QualityControlTimeoutTotal:
		return "control_timeout_total"
	case QualityControlInterruptedTotal:
		return "control_interrupted_total"
	case QualityWriterIOErrorTotal:
		return "writer_io_error_total"
	case QualityEventLostAfterIOTotal:
		return "event_lost_after_io_total"
	case QualityDictionaryOverflowTotal:
		return "dictionary_overflow_total"
	case QualityDictionaryValueTruncated:
		return "dictionary_value_truncated_total"
	case QualityOversizedRecordTotal:
		return "oversized_record_total"
	case QualityCommittedChunkTotal:
		return "committed_chunk_total"
	case QualityFailedChunkTotal:
		return "failed_chunk_total"
	case QualityRecoveryTotal:
		return "recovery_total"
	case QualityCloseTimeoutTotal:
		return "close_timeout_total"
	case QualityEventLostAfterSizeLimitTotal:
		return "event_lost_after_size_limit_total"
	case QualityWriterAdmissionContentionTotal:
		return "writer_admission_contention_total"
	case QualityEventLostAfterStorageBudget:
		return "event_lost_after_storage_budget_total"
	case QualityMetricCardinalityLoss:
		return "metric_cardinality_loss_total"
	case QualityInvalidMetric:
		return "invalid_metric_total"
	case QualityRuntimeStackMismatch:
		return "runtime_stack_mismatch_total"
	case QualityLogSpamCardinalityLoss:
		return "log_spam_cardinality_loss_total"
	case QualityHandlerEntryLimit:
		return "handler_entry_limit_total"
	case QualityHandlerWrapperLimit:
		return "handler_wrapper_limit_total"
	case QualityLifecycleRegistryLimit:
		return "lifecycle_registry_limit_total"
	case QualityObjectWatcherLimit:
		return "object_watcher_limit_total"
	case QualityJankStatsHandleLimit:
		return "jankstats_handle_limit_total"
	case QualityMetricFlushTimeout:
		return "metric_flush_timeout_total"
	case QualityRuntimeGraphShutdownLoss:
		return "runtime_graph_shutdown_loss_total"
	case QualityRuntimeGraphWriterRejectionLoss:
		return "runtime_graph_writer_rejection_loss_total"
	case QualityHandlerContentionBypass:
		return "handler_contention_bypass_total"
	case QualityRuntimeEventBufferCapacityLoss:
		return "runtime_event_buffer_capacity_loss_total"
	case QualityRuntimeEventRegistryCapacityLoss:
		return "runtime_event_registry_capacity_loss_total"
	case QualityMethodCounterCardinalityLoss:
		return "method_counter_cardinality_loss_total"
	case QualityRuntimeEventWriterRejectionLoss:
		return "runtime_event_writer_rejection_loss_total"
	case QualityRuntimeGraphInputTotal:
		return "runtime_graph_input_total"
	case QualityRuntimeGraphEmittedTotal:
		return "runtime_graph_emitted_total"
	case QualityRuntimeGraphBackpressureCount:
		return "runtime_graph_backpressure_count_total"
	case QualityRuntimeGraphBackpressureNanos:
		return "runtime_graph_backpressure_nanos_total"
	case QualityWriterBackpressureCount:
		return "writer_backpressure_count_total"
	case QualityWriterBackpressureNanos:
		return "writer_backpressure_nanos_total"
	case QualityRuntimeEventBackpressureCount:
		return "runtime_event_backpressure_count_total"
	case QualityRuntimeEventBackpressureNanos:
		return "runtime_event_backpressure_nanos_total"
	case QualityRuntimeGraphDisabled:
		return "runtime_graph_disabled_total"
	case QualityRuntimeHookFailureTotal:
		return "runtime_hook_failure_total"
	case QualityArchiveEvictedRunTotal:
		return "archive_evicted_run_total"
	case QualityArchiveEvictedSegmentTotal:
		return "archive_evicted_segment_total"
	case QualityArchiveEvictedBytesTotal:
		return "archive_evicted_bytes_total"
	case QualityRuntimeHookInstrumentationFailure:
		return "runtime_hook_instrumentation_failure_total"
	case QualityRuntimeHookAsyncWrapperFailure:
		return "runtime_hook_async_wrapper_failure_total"
	case QualityRuntimeHookLifecycleFailure:
		return "runtime_hook_lifecycle_failure_total"
	case QualityRuntimeHookCollectorFailure:
		return "runtime_hook_collector_failure_total"
	case QualityRuntimeHookContextFailure:
		return "runtime_hook_context_failure_total"
	case QualityRuntimeHookSchedulerFailure:
		return "runtime_hook_scheduler_failure_total"
	case QualityJankStatsDependencyMissing:
		return "jankstats_dependency_missing_total"
	case QualityJankStatsInstallFailure:
		return "jankstats_install_failure_total"
	case QualityJankStatsFrameFailure:
		return "jankstats_frame_failure_total"
	case QualityJankStatsControlFailure:
		return "jankstats_control_failure_total"
	case QualityRuntimeHookUnclassifiedFailure:
		return "runtime_hook_unclassified_failure_total"
	case QualityPreparedStatementRegistryEviction:
		return "prepared_statement_registry_eviction_total"
	case QualityPreparedStatementResolutionMiss:
		return "prepared_statement_resolution_miss_after_eviction_total"
	case QualityReceiverAsyncRegistryEviction:
		return "receiver_async_registry_eviction_total"
	case QualityReceiverAsyncResolutionMiss:
		return "receiver_async_resolution_miss_after_eviction_total"
	}
	if id >= 0x1000 && id < 0x2000 {
		return fmt.Sprintf("event_%d_reason_%d_total", (id-0x1000)/16, (id-0x1000)%16)
	}
	return fmt.Sprintf("quality_%d", id)
}

func IsKnownQualityCounter(id uint64) bool {
	if id >= QualityAcceptedEventTotal && id <= QualityEventLostAfterStorageBudget {
		return true
	}
	switch id {
	case QualityMetricCardinalityLoss,
		QualityInvalidMetric,
		QualityRuntimeStackMismatch,
		QualityLogSpamCardinalityLoss,
		QualityHandlerEntryLimit,
		QualityHandlerWrapperLimit,
		QualityLifecycleRegistryLimit,
		QualityObjectWatcherLimit,
		QualityJankStatsHandleLimit,
		QualityMetricFlushTimeout,
		QualityRuntimeGraphShutdownLoss,
		QualityRuntimeGraphWriterRejectionLoss,
		QualityHandlerContentionBypass,
		QualityRuntimeEventBufferCapacityLoss,
		QualityRuntimeEventRegistryCapacityLoss,
		QualityMethodCounterCardinalityLoss,
		QualityRuntimeEventWriterRejectionLoss,
		QualityRuntimeGraphInputTotal,
		QualityRuntimeGraphEmittedTotal,
		QualityRuntimeGraphBackpressureCount,
		QualityRuntimeGraphBackpressureNanos,
		QualityWriterBackpressureCount,
		QualityWriterBackpressureNanos,
		QualityRuntimeEventBackpressureCount,
		QualityRuntimeEventBackpressureNanos,
		QualityRuntimeGraphDisabled,
		QualityRuntimeHookFailureTotal,
		QualityArchiveEvictedRunTotal,
		QualityArchiveEvictedSegmentTotal,
		QualityArchiveEvictedBytesTotal,
		QualityRuntimeHookInstrumentationFailure,
		QualityRuntimeHookAsyncWrapperFailure,
		QualityRuntimeHookLifecycleFailure,
		QualityRuntimeHookCollectorFailure,
		QualityRuntimeHookContextFailure,
		QualityRuntimeHookSchedulerFailure,
		QualityJankStatsDependencyMissing,
		QualityJankStatsInstallFailure,
		QualityJankStatsFrameFailure,
		QualityJankStatsControlFailure,
		QualityRuntimeHookUnclassifiedFailure,
		QualityPreparedStatementRegistryEviction,
		QualityPreparedStatementResolutionMiss,
		QualityReceiverAsyncRegistryEviction,
		QualityReceiverAsyncResolutionMiss:
		return true
	}
	if id < 0x1000 || id >= 0x2000 {
		return false
	}
	eventType := EventType((id - 0x1000) / 16)
	reason := QualityLossReason((id - 0x1000) % 16)
	return eventType >= EventDictionary && eventType <= EventBinderTransaction &&
		reason >= QualityLossQueueFull && reason <= QualityLossStorageBudget
}

type Log struct {
	Source string
	Events []Event
	Dict   map[uint64]string
	Kinds  map[uint64]DictKind
	Result StreamResult
}

func NetworkName(n NetworkKind) string {
	switch n {
	case NetworkOffline:
		return "offline"
	case NetworkWiFi:
		return "wifi"
	case NetworkCellular:
		return "cellular"
	case NetworkEthernet:
		return "ethernet"
	case NetworkVPN:
		return "vpn"
	default:
		return "unknown"
	}
}

func ResolveSymbol(dict map[uint64]string, ref SymbolRef) string {
	if ref.Stable {
		if ref.Namespace != "" {
			return fmt.Sprintf("stable:%s:0x%016x", ref.Namespace, ref.ID)
		}
		return fmt.Sprintf("stable:0x%016x", ref.ID)
	}
	if ref.ID == 0 {
		return "unknown"
	}
	if value, ok := dict[ref.ID]; ok && value != "" {
		return value
	}
	return "id:" + formatUint(ref.ID)
}

func formatUint(v uint64) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
