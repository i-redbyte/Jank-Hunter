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
	ClassContains  string
}

type Options struct {
	Filter                     Filter
	OwnerMap                   map[string]string
	ClassGraph                 *ClassGraph
	InstrumentationDiagnostics *InstrumentationDiagnostics
	HeapEvidence               *HeapEvidence
	BaselineHeapEvidence       *HeapEvidence
	CandidateHeapEvidence      *HeapEvidence
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

type RuntimeCallStats struct {
	Screen  string
	Flow    string
	Step    string
	Caller  string
	Callee  string
	Count   uint64
	TotalMS uint64
	MaxMS   uint64
}

type CodeProblemSignal struct {
	Name     string  `json:"name"`
	Category string  `json:"category"`
	Severity string  `json:"severity"`
	Score    float64 `json:"score"`
	Count    uint64  `json:"count,omitempty"`
	TotalMS  uint64  `json:"total_ms,omitempty"`
	MaxMS    uint64  `json:"max_ms,omitempty"`
	Value    uint64  `json:"value,omitempty"`
	Unit     string  `json:"unit,omitempty"`
	Detail   string  `json:"detail,omitempty"`
}

type CodeProblemStats struct {
	ClassName       string                 `json:"class_name"`
	Method          string                 `json:"method,omitempty"`
	Owner           string                 `json:"owner,omitempty"`
	Score           float64                `json:"score"`
	Severity        string                 `json:"severity"`
	RuntimeEvidence bool                   `json:"runtime_evidence"`
	Categories      []string               `json:"categories,omitempty"`
	Problems        []string               `json:"problems,omitempty"`
	Signals         []CodeProblemSignal    `json:"signals,omitempty"`
	Screens         []string               `json:"screens,omitempty"`
	Flows           []string               `json:"flows,omitempty"`
	Steps           []string               `json:"steps,omitempty"`
	Routes          []string               `json:"routes,omitempty"`
	DrillDown       []CodeProblemDrillDown `json:"drill_down,omitempty"`
	Impact          string                 `json:"impact,omitempty"`
	Recommendation  string                 `json:"recommendation,omitempty"`
	Evidence        string                 `json:"evidence,omitempty"`
}

type CodeProblemDrillDown struct {
	ClassName      string   `json:"class_name"`
	Method         string   `json:"method,omitempty"`
	Screen         string   `json:"screen,omitempty"`
	Flow           string   `json:"flow,omitempty"`
	Step           string   `json:"step,omitempty"`
	Route          string   `json:"route,omitempty"`
	Evidence       string   `json:"evidence"`
	Recommendation string   `json:"recommendation"`
	Signals        []string `json:"signals,omitempty"`
}

type MemoryLeakSuspect struct {
	ClassName                string
	Holder                   string
	Screen                   string
	Flow                     string
	Step                     string
	Count                    uint64
	MaxAgeMS                 uint64
	EstimatedRetainedKB      uint64
	HeapEvidence             bool
	HeapSource               string
	GCRoot                   string
	HolderField              string
	RetainedObjectCount      uint64
	RetainedSizeConfidence   string
	RetainedSizeExplanation  string
	DominatorPath            []string
	DominatorTreeConfidence  string
	DominatorTreeExplanation string
	LeakChainConfidence      string
	LeakChainSummary         string
	LeakChainActions         []string
	Score                    float64
	Severity                 string
	ObjectKind               string
	HolderQuality            string
	UserOwned                bool
	SystemRetained           bool
	Impact                   string
	Recommendation           string
	Evidence                 string
}

type HeapEvidence struct {
	Sources  []string           `json:"sources,omitempty"`
	Leaks    []HeapLeakEvidence `json:"leaks"`
	Warnings []string           `json:"warnings,omitempty"`
}

type HeapLeakEvidence struct {
	ClassName           string            `json:"class_name"`
	Holder              string            `json:"holder,omitempty"`
	HolderField         string            `json:"holder_field,omitempty"`
	GCRoot              string            `json:"gc_root,omitempty"`
	RetainedSizeKB      uint64            `json:"retained_size_kb,omitempty"`
	RetainedSizeBytes   uint64            `json:"retained_size_bytes,omitempty"`
	RetainedObjectCount uint64            `json:"retained_object_count,omitempty"`
	ReferencePath       []HeapPathElement `json:"reference_path,omitempty"`
	DominatorTree       []string          `json:"dominator_tree,omitempty"`
	Source              string            `json:"source,omitempty"`
	Confidence          string            `json:"confidence,omitempty"`
}

type HeapPathElement struct {
	ClassName string `json:"class_name,omitempty"`
	FieldName string `json:"field_name,omitempty"`
	ObjectID  string `json:"object_id,omitempty"`
	Kind      string `json:"kind,omitempty"`
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
	Warnings          []string

	Routes             []RouteStats
	Screens            []ScreenStats
	Owners             []OwnerStats
	Flows              []FlowStats
	LogSpam            []LogSpamStats
	ProblemWindows     []ProblemWindowStats
	RuntimeCalls       []RuntimeCallStats
	CodeProblems       []CodeProblemStats
	MemoryLeaks        []MemoryLeakSuspect
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
	HasRuntimeGraph  bool
	RuntimeNodes     int
	RuntimeEdges     int
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
