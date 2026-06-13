package analyze

type NamedValue struct {
	Name  string
	Value uint64
	Extra string
}

type Filter struct {
	RouteContains  string
	ScreenContains string
	OwnerContains  string
}

type Options struct {
	Filter   Filter
	OwnerMap map[string]string
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

type Summary struct {
	Title            string
	LogCount         int
	EventCount       int
	DurationMS       uint64
	Dictionary       int
	HTTPCount        int
	HTTPFailed       int
	HTTPP95MS        uint64
	UIFrames         uint64
	UIJank           uint64
	UIWindowMS       uint64
	UIJankPct        float64
	UIAvgFPS         float64
	UIMinFPS         float64
	StallCount       int
	StallMaxMS       uint64
	ContextCount     int
	BatteryMinPct    uint64
	BatteryLastPct   uint64
	AvailMemoryMinKB uint64
	LowMemoryCount   int
	TrafficRxMax     uint64
	TrafficTxMax     uint64
	MemoryMaxKB      uint64
	Retained         uint64

	Routes             []RouteStats
	Screens            []ScreenStats
	Owners             []OwnerStats
	Processes          []NamedValue
	Network            []NamedValue
	Memory             []NamedValue
	RetainedClasses    []NamedValue
	RetainedAgeBuckets []NamedValue
	JankStats          []NamedValue
	Counters           []NamedValue
	Gauges             []NamedValue
}

type Delta struct {
	Name       string
	Baseline   string
	Candidate  string
	Change     string
	Severity   string
	Confidence string
}

type Comparison struct {
	Baseline  Summary
	Candidate Summary
	Deltas    []Delta
}
