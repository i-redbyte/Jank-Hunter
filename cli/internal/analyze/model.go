package analyze

type NamedValue struct {
	Name  string
	Value uint64
	Extra string
}

type InfoItem struct {
	Label  string
	Value  string
	Detail string
}

type RunEnvironment struct {
	Title    string
	Subtitle string
	Items    []InfoItem
}

type Filter struct {
	RouteContains  string
	ScreenContains string
	OwnerContains  string
}

type Options struct {
	Filter     Filter
	OwnerMap   map[string]string
	ClassGraph *ClassGraph
}

type RouteStats struct {
	Route       string
	Count       int
	Failures    int
	P50MS       uint64
	P95MS       uint64
	MaxMS       uint64
	AvgTTFBMS   uint64
	BytesRx     uint64
	BytesTx     uint64
	OwnerSample string
}

type ScreenStats struct {
	Screen      string
	WindowCount int
	WindowMS    uint64
	Frames      uint64
	JankyFrames uint64
	JankRatePct float64
	AvgFPS      float64
	MinFPS      float64
	P95MS       uint64
	MaxP99MS    uint64
}

type OwnerStats struct {
	Owner     string
	Count     int
	TotalMS   uint64
	MaxMS     uint64
	Kind      string
	StackHint string
}

type FlowStats struct {
	Screen       string
	Flow         string
	Step         string
	Owner        string
	RouteSample  string
	HTTPCount    int
	HTTPFailed   int
	HTTPP95MS    uint64
	StallCount   int
	StallMaxMS   uint64
	UIWindows    int
	UIFrames     uint64
	UIJank       uint64
	UIJankPct    float64
	LogSpam      uint64
	ProblemCount uint64
	ProblemMaxMS uint64
	MemoryMaxKB  uint64
}

type LogSpamStats struct {
	Screen string
	Flow   string
	Step   string
	Owner  string
	Source string
	Level  string
	Count  uint64
}

type ProblemWindowStats struct {
	Screen        string
	Flow          string
	Step          string
	Owner         string
	Kind          string
	Windows       int
	Count         uint64
	TotalWindowMS uint64
	MaxMS         uint64
}

type Summary struct {
	Title             string
	LogCount          int
	EventCount        int
	DurationMS        uint64
	Dictionary        int
	HTTPCount         int
	HTTPFailed        int
	HTTPP95MS         uint64
	UIFrames          uint64
	UIJank            uint64
	UIWindowMS        uint64
	UIJankPct         float64
	UIAvgFPS          float64
	UIMinFPS          float64
	StallCount        int
	StallMaxMS        uint64
	ContextCount      int
	BatteryMinPct     uint64
	BatteryLastPct    uint64
	AvailMemoryMinKB  uint64
	LowMemoryCount    int
	TrafficRxMax      uint64
	TrafficTxMax      uint64
	BatteryStateLast  uint64
	BatteryTempDeciC  uint64
	AvailMemoryLastKB uint64
	TotalMemoryKB     uint64
	FreeStorageKB     uint64
	TotalStorageKB    uint64
	NetworkMetered    bool
	NetworkValidated  bool
	NetworkVPN        bool
	DeviceRootKnown   bool
	DeviceRooted      bool
	MemoryMaxKB       uint64
	Retained          uint64
	Environment       RunEnvironment

	Routes             []RouteStats
	Screens            []ScreenStats
	Owners             []OwnerStats
	Flows              []FlowStats
	LogSpam            []LogSpamStats
	ProblemWindows     []ProblemWindowStats
	AppVersions        []NamedValue
	Builds             []NamedValue
	Devices            []NamedValue
	SDKs               []NamedValue
	Cohorts            []NamedValue
	Processes          []NamedValue
	Network            []NamedValue
	Memory             []NamedValue
	RetainedClasses    []NamedValue
	RetainedAgeBuckets []NamedValue
	JankStats          []NamedValue
	Counters           []NamedValue
	Gauges             []NamedValue
	Influence          InfluenceSummary
}

type ClassGraph struct {
	Classes map[string]ClassGraphClass `json:"classes"`
	Edges   []ClassGraphEdge           `json:"edges"`
}

type ClassGraphClass struct {
	Name string `json:"name"`
}

type ClassGraphEdge struct {
	From         string `json:"from"`
	To           string `json:"to"`
	CallerMethod string `json:"caller_method,omitempty"`
	CalleeMethod string `json:"callee_method,omitempty"`
	Count        uint64 `json:"count"`
}

type InfluenceSummary struct {
	Available        bool
	HasClassGraph    bool
	RuntimeNodes     int
	StaticNodes      int
	StaticEdges      int
	ShownNodes       int
	ShownEdges       int
	TopNodes         []InfluenceNode
	TopEdges         []InfluenceEdge
	Heuristic        []InfluenceFinding
	StandaloneReason string
}

type InfluenceNode struct {
	ClassName       string
	Label           string
	Score           float64
	Severity        string
	Status          string
	RuntimeEvidence bool
	Problems        uint64
	LogSpam         uint64
	MainThreadMS    uint64
	NetworkMS       uint64
	MemoryPressure  uint64
	UIJank          uint64
	Retained        uint64
	Flows           []string
	Screens         []string
	Routes          []string
	Reasons         []string
}

type InfluenceEdge struct {
	From             string
	To               string
	Count            uint64
	Influence        float64
	RuntimeConfirmed bool
	Reason           string
}

type InfluenceFinding struct {
	Severity string
	Title    string
	Detail   string
}

type Delta struct {
	Name           string
	Baseline       string
	Candidate      string
	Change         string
	Severity       string
	Confidence     string
	Interval       string
	Unit           string
	BaselineValue  float64
	CandidateValue float64
	ChangeAbs      float64
	ChangePct      float64
	RegressionAbs  float64
	RegressionPct  float64
	SampleSize     uint64
}

type Comparison struct {
	Baseline  Summary
	Candidate Summary
	Deltas    []Delta
	Warnings  []string
}

type ThresholdConfig struct {
	MaxSeverity   string                     `json:"max_severity"`
	MinConfidence string                     `json:"min_confidence"`
	Metrics       map[string]MetricThreshold `json:"metrics"`
}

type MetricThreshold struct {
	MaxSeverity      string  `json:"max_severity"`
	MaxRegressionAbs float64 `json:"max_regression_abs"`
	MaxRegressionPct float64 `json:"max_regression_pct"`
}

type GateResult struct {
	Failed   bool     `json:"failed"`
	Failures []string `json:"failures"`
}
