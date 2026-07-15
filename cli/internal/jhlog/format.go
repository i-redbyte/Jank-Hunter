package jhlog

import "fmt"

const FormatVersion = 9

const magicSize = 8

var Magic = []byte{'J', 'H', 'L', 'O', 'G', '\r', '\n', FormatVersion}

const (
	HeaderSchemaV1 uint64 = 1

	FeatureChunkCRCCommit        uint64 = 1 << 0
	FeatureLengthDelimited       uint64 = 1 << 1
	FeatureSymbolRefs            uint64 = 1 << 2
	FeatureProducerMetadata      uint64 = 1 << 3
	FeatureChunkLocalContext     uint64 = 1 << 4
	FeatureQualityRecords        uint64 = 1 << 5
	FeatureEmbeddedStableSymbols uint64 = 1 << 6
	FeatureGZIPChunks            uint64 = 1 << 0
	RequiredFeaturesV9                  = FeatureChunkCRCCommit | FeatureLengthDelimited | FeatureSymbolRefs |
		FeatureProducerMetadata | FeatureChunkLocalContext | FeatureQualityRecords | FeatureEmbeddedStableSymbols
	OptionalFeaturesV9 = FeatureGZIPChunks
)

type ID128 [16]byte

func (id ID128) IsZero() bool {
	return id == ID128{}
}

type SegmentHeader struct {
	Schema                  uint64 `json:"schema"`
	RequiredFeatures        uint64 `json:"required_features"`
	OptionalFeatures        uint64 `json:"optional_features"`
	RunID                   ID128  `json:"run_id"`
	ProcessInstanceID       ID128  `json:"process_instance_id"`
	SessionID               ID128  `json:"session_id"`
	SegmentIndex            uint64 `json:"segment_index"`
	OSPID                   uint64 `json:"os_pid"`
	PID                     uint64 `json:"-"` // Compatibility alias for OSPID.
	CollectorStartElapsedUS uint64 `json:"collector_start_elapsed_us"`
	SegmentStartElapsedUS   uint64 `json:"segment_start_elapsed_us"`
	SegmentStartUnixMS      uint64 `json:"segment_start_unix_ms"`
	SegmentStartWallUnixMS  uint64 `json:"-"` // Compatibility alias for SegmentStartUnixMS.
	IdentitySource          uint64 `json:"identity_source"`
	ProcessName             string `json:"process_name"`
	SymbolNamespace         []byte `json:"symbol_namespace,omitempty"`
}

func DefaultSegmentHeader() SegmentHeader {
	return SegmentHeader{
		Schema:           HeaderSchemaV1,
		RequiredFeatures: RequiredFeaturesV9,
		OptionalFeatures: OptionalFeaturesV9,
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
	Source            string               `json:"source"`
	Version           uint8                `json:"version"`
	Header            SegmentHeader        `json:"header"`
	Status            SegmentStatus        `json:"status"`
	Sealed            bool                 `json:"sealed"`
	TailBytes         uint64               `json:"tail_bytes,omitempty"`
	CommittedChunks   uint32               `json:"committed_chunks"`
	TotalRecords      uint64               `json:"total_records"`
	DataRecords       uint64               `json:"data_records"`
	DictionaryRecords uint64               `json:"dictionary_records"`
	ControlRecords    uint64               `json:"control_records"`
	Events            uint64               `json:"events"` // Known semantic data events delivered to the handler.
	LatestQuality     *QualitySnapshot     `json:"latest_quality,omitempty"`
	SegmentEnd        *SegmentEndEvent     `json:"segment_end,omitempty"`
	Warnings          []string             `json:"warnings,omitempty"`
	RawRecordBytes    uint64               `json:"raw_record_bytes,omitempty"`
	StoredChunkBytes  uint64               `json:"stored_chunk_bytes,omitempty"`
	RecordBytesByType map[EventType]uint64 `json:"record_bytes_by_type,omitempty"`
	RecordsByType     map[EventType]uint64 `json:"records_by_type,omitempty"`
}

type EventType uint64

const (
	EventDictionary      EventType = 1
	EventSession         EventType = 2
	EventContext         EventType = 3
	EventHTTP            EventType = 4
	EventUIWindow        EventType = 5
	EventStall           EventType = 6
	EventMemory          EventType = 7
	EventRetained        EventType = 8
	EventCounter         EventType = 9
	EventGauge           EventType = 10
	EventFlow            EventType = 11
	EventLogSpam         EventType = 12
	EventProblem         EventType = 13
	EventRuntimeCall     EventType = 14
	EventQualitySnapshot EventType = 15
	EventSegmentEnd      EventType = 16

	EventDictionaryDefinition = EventDictionary
	EventSessionMetadata      = EventSession
	EventDeviceContext        = EventContext
	EventFlowTransition       = EventFlow
)

// IsSemanticData reports whether a record contributes an application/runtime
// observation. Dictionary and control records are transport metadata and must
// not inflate event sample sizes or observed durations.
func (eventType EventType) IsSemanticData() bool {
	return eventType >= EventSession && eventType <= EventRuntimeCall
}

type EnvelopeFlag uint64

const (
	EnvelopeHasTime       EnvelopeFlag = 1 << 0
	EnvelopeHasThread     EnvelopeFlag = 1 << 1
	EnvelopeHasContext    EnvelopeFlag = 1 << 2
	EnvelopeSameContext   EnvelopeFlag = 1 << 3
	EnvelopeHasAttributes EnvelopeFlag = 1 << 4

	EnvelopeFlagHasTime       = EnvelopeHasTime
	EnvelopeFlagHasThread     = EnvelopeHasThread
	EnvelopeFlagHasContext    = EnvelopeHasContext
	EnvelopeFlagSameContext   = EnvelopeSameContext
	EnvelopeFlagHasAttributes = EnvelopeHasAttributes
)

type Flag uint64

const (
	FlagHTTPReusedConnection Flag = 1 << 0
	FlagHTTPFailed           Flag = 1 << 1
	FlagHTTPTLS              Flag = 1 << 2
	FlagThreadMain           Flag = 1 << 3
	FlagAppForeground        Flag = 1 << 4
	FlagNetworkMetered       Flag = 1 << 5
	FlagContextLowMemory     Flag = 1 << 6
	FlagNetworkValidated     Flag = 1 << 7
	FlagNetworkVPN           Flag = 1 << 8
	FlagDeviceRooted         Flag = 1 << 9
	FlagHTTPSlow             Flag = 1 << 15
	FlagUIProblem            Flag = 1 << 16
	FlagHTTPClassified       Flag = 1 << 17
	FlagUIClassified         Flag = 1 << 18

	// Compatibility aliases. In v9 attribution is encoded in the record envelope,
	// never in event attributes.
	FlagHasScreen   Flag = 1 << 10
	FlagHasOwner    Flag = 1 << 11
	FlagHasFlow     Flag = 1 << 12
	FlagHasStep     Flag = 1 << 13
	FlagSameContext Flag = 1 << 14
)

const semanticAttributeMask = uint64((1<<10)-1) |
	uint64(FlagHTTPSlow|FlagUIProblem|FlagHTTPClassified|FlagUIClassified)

type SymbolRef struct {
	LocalID   uint64 `json:"local_id,omitempty"`
	StableID  uint64 `json:"stable_id,omitempty"`
	Stable    bool   `json:"stable,omitempty"`
	Namespace string `json:"namespace,omitempty"`
}

func LocalSymbol(id uint64) SymbolRef  { return SymbolRef{LocalID: id} }
func StableSymbol(id uint64) SymbolRef { return SymbolRef{StableID: id, Stable: true} }
func StableSymbolInNamespace(id uint64, namespace []byte) SymbolRef {
	return SymbolRef{StableID: id, Stable: true, Namespace: fmt.Sprintf("%x", namespace)}
}
func (r SymbolRef) IsUnknown() bool { return !r.Stable && r.LocalID == 0 }

func (r SymbolRef) LegacyID() uint64 {
	if r.Stable {
		return 0
	}
	return r.LocalID
}

type ProducerMetadata struct {
	ElapsedUS uint64 `json:"elapsed_us,omitempty"`
	ThreadID  uint64 `json:"thread_id,omitempty"`
	HasTime   bool   `json:"has_time,omitempty"`
	HasThread bool   `json:"has_thread,omitempty"`
}

type AttributionContext struct {
	Present bool      `json:"present,omitempty"`
	Screen  SymbolRef `json:"screen,omitempty"`
	Owner   SymbolRef `json:"owner,omitempty"`
	Flow    SymbolRef `json:"flow,omitempty"`
	Step    SymbolRef `json:"step,omitempty"`
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
	DictFlow
	DictStep
	DictLogSource
	// DictStableSymbol uses the dictionary record envelope with an ASM-assigned stable ID.
	// It lives in a separate namespace and must never be inserted into the local-ID dictionary.
	DictStableSymbol
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
	Type        EventType          `json:"type"`
	TimeUS      uint64             `json:"time_us,omitempty"`
	DeltaUS     int64              `json:"delta_us,omitempty"`
	TimeMS      uint64             `json:"time_ms"`
	DeltaMS     uint64             `json:"delta_ms,omitempty"`
	Flags       uint64             `json:"flags,omitempty"`
	Producer    ProducerMetadata   `json:"producer,omitempty"`
	Attribution AttributionContext `json:"attribution,omitempty"`
	Position    RecordPosition     `json:"position,omitempty"`
	Source      string             `json:"source,omitempty"`
	Warnings    []string           `json:"warnings,omitempty"`

	Dictionary  *DictionaryEntry  `json:"dictionary,omitempty"`
	Session     *SessionEvent     `json:"session,omitempty"`
	Context     *ContextEvent     `json:"context,omitempty"`
	HTTP        *HTTPEvent        `json:"http,omitempty"`
	UIWindow    *UIWindowEvent    `json:"ui_window,omitempty"`
	Stall       *StallEvent       `json:"stall,omitempty"`
	Memory      *MemoryEvent      `json:"memory,omitempty"`
	Retained    *RetainedEvent    `json:"retained,omitempty"`
	Metric      *MetricEvent      `json:"metric,omitempty"`
	Flow        *FlowEvent        `json:"flow,omitempty"`
	LogSpam     *LogSpamEvent     `json:"log_spam,omitempty"`
	Problem     *ProblemEvent     `json:"problem,omitempty"`
	RuntimeCall *RuntimeCallEvent `json:"runtime_call,omitempty"`
	Quality     *QualitySnapshot  `json:"quality,omitempty"`
	SegmentEnd  *SegmentEndEvent  `json:"segment_end,omitempty"`
}

type SessionEvent struct {
	AppVersionID     uint64 `json:"app_version_id"`
	BuildID          uint64 `json:"build_id"`
	DeviceID         uint64 `json:"device_id"`
	SDKInt           uint64 `json:"sdk_int"`
	ProcessID        uint64 `json:"process_id,omitempty"`
	ProcessName      string `json:"process_name,omitempty"`
	AndroidReleaseID uint64 `json:"android_release_id,omitempty"`
	SecurityPatchID  uint64 `json:"security_patch_id,omitempty"`
	PrimaryABIID     uint64 `json:"primary_abi_id,omitempty"`
	SupportedABIsID  uint64 `json:"supported_abis_id,omitempty"`
	ManufacturerID   uint64 `json:"manufacturer_id,omitempty"`
	BrandID          uint64 `json:"brand_id,omitempty"`
	HardwareID       uint64 `json:"hardware_id,omitempty"`
	BoardID          uint64 `json:"board_id,omitempty"`
	ProductID        uint64 `json:"product_id,omitempty"`
	DeviceRooted     bool   `json:"device_rooted,omitempty"`

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
	OwnerID    uint64      `json:"owner_id"`
	RouteID    uint64      `json:"route_id"`
	OwnerRef   SymbolRef   `json:"owner_ref,omitempty"`
	RouteRef   SymbolRef   `json:"route_ref,omitempty"`
	DurationMS uint64      `json:"duration_ms"`
	DNSMS      uint64      `json:"dns_ms"`
	ConnectMS  uint64      `json:"connect_ms"`
	TTFBMS     uint64      `json:"ttfb_ms"`
	Status     StatusClass `json:"status"`
	RxBytes    uint64      `json:"rx_bytes"`
	TxBytes    uint64      `json:"tx_bytes"`
}

type UIWindowEvent struct {
	ScreenID   uint64    `json:"screen_id"`
	ScreenRef  SymbolRef `json:"screen_ref,omitempty"`
	WindowMS   uint64    `json:"window_ms"`
	FrameCount uint64    `json:"frame_count"`
	JankCount  uint64    `json:"jank_count"`
	P50MS      uint64    `json:"p50_ms"`
	P95MS      uint64    `json:"p95_ms"`
	P99MS      uint64    `json:"p99_ms"`
}

type StallEvent struct {
	OwnerID    uint64    `json:"owner_id"`
	StackID    uint64    `json:"stack_id"`
	OwnerRef   SymbolRef `json:"owner_ref,omitempty"`
	StackRef   SymbolRef `json:"stack_ref,omitempty"`
	DurationMS uint64    `json:"duration_ms"`
}

type MemoryEvent struct {
	PSSKB        uint64 `json:"pss_kb"`
	JavaHeapKB   uint64 `json:"java_heap_kb"`
	NativeHeapKB uint64 `json:"native_heap_kb"`
}

type RetainedEvent struct {
	ScreenID  uint64            `json:"screen_id,omitempty"`
	OwnerID   uint64            `json:"owner_id,omitempty"`
	FlowID    uint64            `json:"flow_id,omitempty"`
	StepID    uint64            `json:"step_id,omitempty"`
	ClassID   uint64            `json:"class_id"`
	HolderID  uint64            `json:"holder_id,omitempty"`
	ClassRef  SymbolRef         `json:"class_ref,omitempty"`
	HolderRef SymbolRef         `json:"holder_ref,omitempty"`
	AgeMS     uint64            `json:"age_ms"`
	Count     uint64            `json:"count"`
	Evidence  RetentionEvidence `json:"evidence"`
}

type MetricEvent struct {
	MetricID  uint64     `json:"metric_id"`
	MetricRef SymbolRef  `json:"metric_ref,omitempty"`
	Value     uint64     `json:"value"`
	Count     uint64     `json:"count,omitempty"`
	Sum       uint64     `json:"sum,omitempty"`
	Max       uint64     `json:"max,omitempty"`
	Mode      MetricMode `json:"mode,omitempty"`
}

type FlowEvent struct {
	ScreenID   uint64 `json:"screen_id,omitempty"`
	OwnerID    uint64 `json:"owner_id,omitempty"`
	FlowID     uint64 `json:"flow_id,omitempty"`
	StepID     uint64 `json:"step_id,omitempty"`
	Phase      uint64 `json:"phase,omitempty"`
	InstanceID uint64 `json:"instance_id,omitempty"`
}

type LogSpamEvent struct {
	ScreenID  uint64    `json:"screen_id,omitempty"`
	OwnerID   uint64    `json:"owner_id,omitempty"`
	FlowID    uint64    `json:"flow_id,omitempty"`
	StepID    uint64    `json:"step_id,omitempty"`
	SourceID  uint64    `json:"source_id"`
	SourceRef SymbolRef `json:"source_ref,omitempty"`
	Level     uint64    `json:"level"`
	Count     uint64    `json:"count"`
}

type ProblemEvent struct {
	ScreenID uint64    `json:"screen_id,omitempty"`
	OwnerID  uint64    `json:"owner_id,omitempty"`
	FlowID   uint64    `json:"flow_id,omitempty"`
	StepID   uint64    `json:"step_id,omitempty"`
	KindID   uint64    `json:"kind_id"`
	KindRef  SymbolRef `json:"kind_ref,omitempty"`
	WindowMS uint64    `json:"window_ms"`
	Count    uint64    `json:"count"`
	MaxMS    uint64    `json:"max_ms"`
}

type RuntimeCallEvent struct {
	ScreenID  uint64    `json:"screen_id,omitempty"`
	CallerID  uint64    `json:"caller_id,omitempty"`
	FlowID    uint64    `json:"flow_id,omitempty"`
	StepID    uint64    `json:"step_id,omitempty"`
	CallerRef SymbolRef `json:"caller_ref,omitempty"`
	CalleeID  uint64    `json:"callee_id"`
	CalleeRef SymbolRef `json:"callee_ref,omitempty"`
	Count     uint64    `json:"count"`
	TotalMS   uint64    `json:"total_ms"`
	MaxMS     uint64    `json:"max_ms"`
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

const (
	QualityAcceptedEventTotal            uint64 = 1
	QualityWrittenEventTotal             uint64 = 2
	QualityQueueFullTotal                uint64 = 3
	QualityNotAcceptingTotal             uint64 = 4
	QualityControlLaneFullTotal          uint64 = 5
	QualityControlTimeoutTotal           uint64 = 6
	QualityControlInterruptedTotal       uint64 = 7
	QualityWriterIOErrorTotal            uint64 = 8
	QualityEventLostAfterIOTotal         uint64 = 9
	QualityEventLostAfterIORetryTotal           = QualityEventLostAfterIOTotal // Compatibility alias.
	QualityDictionaryOverflowTotal       uint64 = 10
	QualityDictionaryValueTruncated      uint64 = 11
	QualityDictionaryValueTruncatedTotal        = QualityDictionaryValueTruncated
	QualityOversizedRecordTotal          uint64 = 12
	QualityCommittedChunkTotal           uint64 = 13
	QualityFailedChunkTotal              uint64 = 14
	QualityRecoveryTotal                 uint64 = 15
	QualityCloseTimeoutTotal             uint64 = 16
	QualityEventLostAfterSizeLimitTotal  uint64 = 17

	QualityMetricCardinalityLoss    uint64 = 0x2000
	QualityInvalidMetric            uint64 = 0x2001
	QualityRuntimeGraphCapacityLoss uint64 = 0x2002
	QualityRuntimeStackMismatch     uint64 = 0x2003
	QualityLogSpamCardinalityLoss   uint64 = 0x2004
	QualityHandlerEntryLimit        uint64 = 0x2005
	QualityHandlerWrapperLimit      uint64 = 0x2006
	QualityLifecycleRegistryLimit   uint64 = 0x2007
	QualityObjectWatcherLimit       uint64 = 0x2008
	QualityJankStatsHandleLimit     uint64 = 0x2009
	QualityMetricFlushTimeout       uint64 = 0x200a
)

type QualityLossReason uint64

const (
	QualityLossQueueFull    QualityLossReason = 1
	QualityLossNotAccepting QualityLossReason = 2
	QualityLossIOLost       QualityLossReason = 3
	QualityLossIORetry                        = QualityLossIOLost // Compatibility alias.
	QualityLossOversized    QualityLossReason = 4
	QualityLossSizeLimit    QualityLossReason = 5
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
	case QualityMetricCardinalityLoss:
		return "metric_cardinality_loss_total"
	case QualityInvalidMetric:
		return "invalid_metric_total"
	case QualityRuntimeGraphCapacityLoss:
		return "runtime_graph_capacity_loss_total"
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
	}
	if id >= 0x1000 && id < 0x2000 {
		return fmt.Sprintf("event_%d_reason_%d_total", (id-0x1000)/16, (id-0x1000)%16)
	}
	return fmt.Sprintf("quality_%d", id)
}

type Log struct {
	Source   string
	Version  uint8
	Events   []Event
	Dict     map[uint64]string
	Kinds    map[uint64]DictKind
	Warnings []string
	Result   StreamResult
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

func Resolve(dict map[uint64]string, id uint64) string {
	return ResolveSymbol(dict, LocalSymbol(id))
}

func ResolveSymbol(dict map[uint64]string, ref SymbolRef) string {
	if ref.Stable {
		if ref.Namespace != "" {
			return fmt.Sprintf("stable:%s:0x%016x", ref.Namespace, ref.StableID)
		}
		return fmt.Sprintf("stable:0x%016x", ref.StableID)
	}
	if ref.LocalID == 0 {
		return "unknown"
	}
	if value, ok := dict[ref.LocalID]; ok && value != "" {
		return value
	}
	return "id:" + formatUint(ref.LocalID)
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
