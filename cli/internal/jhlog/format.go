package jhlog

const FormatVersion = 6

var Magic = []byte{'J', 'H', 'L', 'O', 'G', '\r', '\n', FormatVersion}

type EventType uint64

const (
	EventDictionary  EventType = 1
	EventSession     EventType = 2
	EventContext     EventType = 3
	EventHTTP        EventType = 4
	EventUIWindow    EventType = 5
	EventStall       EventType = 6
	EventMemory      EventType = 7
	EventRetained    EventType = 8
	EventCounter     EventType = 9
	EventGauge       EventType = 10
	EventFlow        EventType = 11
	EventLogSpam     EventType = 12
	EventProblem     EventType = 13
	EventRuntimeCall EventType = 14
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
	FlagHasScreen            Flag = 1 << 10
	FlagHasOwner             Flag = 1 << 11
	FlagHasFlow              Flag = 1 << 12
	FlagHasStep              Flag = 1 << 13
)

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

type DictionaryEntry struct {
	Kind  DictKind `json:"kind"`
	ID    uint64   `json:"id"`
	Value string   `json:"value"`
}

type Event struct {
	Type     EventType `json:"type"`
	TimeMS   uint64    `json:"time_ms"`
	DeltaMS  uint64    `json:"delta_ms,omitempty"`
	Flags    uint64    `json:"flags,omitempty"`
	Source   string    `json:"source,omitempty"`
	Warnings []string  `json:"warnings,omitempty"`

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
}

type SessionEvent struct {
	AppVersionID     uint64 `json:"app_version_id"`
	BuildID          uint64 `json:"build_id"`
	DeviceID         uint64 `json:"device_id"`
	SDKInt           uint64 `json:"sdk_int"`
	ProcessID        uint64 `json:"process_id,omitempty"`
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
	DurationMS uint64      `json:"duration_ms"`
	DNSMS      uint64      `json:"dns_ms"`
	ConnectMS  uint64      `json:"connect_ms"`
	TTFBMS     uint64      `json:"ttfb_ms"`
	Status     StatusClass `json:"status"`
	RxBytes    uint64      `json:"rx_bytes"`
	TxBytes    uint64      `json:"tx_bytes"`
}

type UIWindowEvent struct {
	ScreenID   uint64 `json:"screen_id"`
	WindowMS   uint64 `json:"window_ms"`
	FrameCount uint64 `json:"frame_count"`
	JankCount  uint64 `json:"jank_count"`
	P50MS      uint64 `json:"p50_ms"`
	P95MS      uint64 `json:"p95_ms"`
	P99MS      uint64 `json:"p99_ms"`
}

type StallEvent struct {
	OwnerID    uint64 `json:"owner_id"`
	StackID    uint64 `json:"stack_id"`
	DurationMS uint64 `json:"duration_ms"`
}

type MemoryEvent struct {
	PSSKB        uint64 `json:"pss_kb"`
	JavaHeapKB   uint64 `json:"java_heap_kb"`
	NativeHeapKB uint64 `json:"native_heap_kb"`
}

type RetainedEvent struct {
	ScreenID uint64 `json:"screen_id,omitempty"`
	OwnerID  uint64 `json:"owner_id,omitempty"`
	FlowID   uint64 `json:"flow_id,omitempty"`
	StepID   uint64 `json:"step_id,omitempty"`
	ClassID  uint64 `json:"class_id"`
	HolderID uint64 `json:"holder_id,omitempty"`
	AgeMS    uint64 `json:"age_ms"`
	Count    uint64 `json:"count"`
}

type MetricEvent struct {
	MetricID uint64     `json:"metric_id"`
	Value    uint64     `json:"value"`
	Count    uint64     `json:"count,omitempty"`
	Sum      uint64     `json:"sum,omitempty"`
	Max      uint64     `json:"max,omitempty"`
	Mode     MetricMode `json:"mode,omitempty"`
}

type FlowEvent struct {
	ScreenID uint64 `json:"screen_id,omitempty"`
	OwnerID  uint64 `json:"owner_id,omitempty"`
	FlowID   uint64 `json:"flow_id,omitempty"`
	StepID   uint64 `json:"step_id,omitempty"`
}

type LogSpamEvent struct {
	ScreenID uint64 `json:"screen_id,omitempty"`
	OwnerID  uint64 `json:"owner_id,omitempty"`
	FlowID   uint64 `json:"flow_id,omitempty"`
	StepID   uint64 `json:"step_id,omitempty"`
	SourceID uint64 `json:"source_id"`
	Level    uint64 `json:"level"`
	Count    uint64 `json:"count"`
}

type ProblemEvent struct {
	ScreenID uint64 `json:"screen_id,omitempty"`
	OwnerID  uint64 `json:"owner_id,omitempty"`
	FlowID   uint64 `json:"flow_id,omitempty"`
	StepID   uint64 `json:"step_id,omitempty"`
	KindID   uint64 `json:"kind_id"`
	WindowMS uint64 `json:"window_ms"`
	Count    uint64 `json:"count"`
	MaxMS    uint64 `json:"max_ms"`
}

type RuntimeCallEvent struct {
	ScreenID uint64 `json:"screen_id,omitempty"`
	CallerID uint64 `json:"caller_id,omitempty"`
	FlowID   uint64 `json:"flow_id,omitempty"`
	StepID   uint64 `json:"step_id,omitempty"`
	CalleeID uint64 `json:"callee_id"`
	Count    uint64 `json:"count"`
	TotalMS  uint64 `json:"total_ms"`
	MaxMS    uint64 `json:"max_ms"`
}

type Log struct {
	Source   string
	Version  uint8
	Events   []Event
	Dict     map[uint64]string
	Kinds    map[uint64]DictKind
	Warnings []string
}

func TypeName(t EventType) string {
	switch t {
	case EventDictionary:
		return "dictionary"
	case EventSession:
		return "session"
	case EventContext:
		return "context"
	case EventHTTP:
		return "http"
	case EventUIWindow:
		return "ui_window"
	case EventStall:
		return "main_thread_stall"
	case EventMemory:
		return "memory"
	case EventRetained:
		return "retained_object"
	case EventCounter:
		return "counter"
	case EventGauge:
		return "gauge"
	case EventFlow:
		return "flow_context"
	case EventLogSpam:
		return "log_spam"
	case EventProblem:
		return "problem_window"
	case EventRuntimeCall:
		return "runtime_call"
	default:
		return "unknown"
	}
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

func StatusName(s StatusClass) string {
	switch s {
	case Status1xx:
		return "1xx"
	case Status2xx:
		return "2xx"
	case Status3xx:
		return "3xx"
	case Status4xx:
		return "4xx"
	case Status5xx:
		return "5xx"
	default:
		return "unknown"
	}
}

func Resolve(dict map[uint64]string, id uint64) string {
	if id == 0 {
		return "unknown"
	}
	if value, ok := dict[id]; ok {
		return value
	}
	return "id:" + formatUint(id)
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
