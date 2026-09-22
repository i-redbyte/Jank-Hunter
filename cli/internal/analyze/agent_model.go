package analyze

type AgentCapabilitySummary struct {
	Requested     uint64
	Potential     uint64
	Granted       uint64
	Active        uint64
	Missing       uint64
	RequestedList []string
	ActiveList    []string
}

type AgentQualitySummary struct {
	QueueHighWatermark         uint64
	QueueFullTotal             uint64
	AdmissionContentionTotal   uint64
	OtherNativeLossTotal       uint64
	SequenceGaps               uint64
	IncompleteWindows          int
	ClockSyncCount             uint64
	MaxClockUncertaintyNS      uint64
	CompatibleClockCalibration bool
}

type AgentInterval struct {
	StartNS      uint64
	DurationNS   uint64
	DurationMS   float64
	ThreadToken  uint64
	ContextToken uint64
	RelatedToken uint64
	Reason       uint64
	Sequence     uint64
	Source       string
}

type AgentIntervalSummary struct {
	Count   uint64
	TotalNS uint64
	TotalMS float64
	MaxNS   uint64
	MaxMS   float64
	Top     []AgentInterval
}

type AgentThreadSummary struct {
	Starts          uint64
	Ends            uint64
	EstimatedActive uint64
	MaxConcurrent   uint64
	UniqueObserved  int
}

type AgentStackHotspot struct {
	Fingerprint string
	Samples     uint64
	ThreadToken uint64
	Context     string
	Methods     []string
}

type AgentStackSummary struct {
	Samples            uint64
	Definitions        uint64
	TruncatedSamples   uint64
	MissingDefinitions uint64
	Hotspots           []AgentStackHotspot
}

type AgentEvidenceStep struct {
	Level       string
	Statement   string
	Measurement string
	EventIDs    []uint64
}

type AgentFinding struct {
	ID                       string
	EvidenceLevel            string
	Symptom                  string
	SuspectedCause           string
	EvidenceChain            []AgentEvidenceStep
	ExactMeasurements        []string
	Screen                   string
	Flow                     string
	Owner                    string
	ThreadToken              uint64
	Method                   string
	ConfidenceScore          int
	Confidence               string
	PositiveEvidence         []string
	MissingOrCounterEvidence []string
	DataQualityPenalties     []string
	AlternativeExplanations  []string
	Actions                  []string
	TimelineReference        string
}

type AgentSummary struct {
	Available         bool
	Availability      string
	Reason            string
	StatusCode        uint64
	StatusDetail      uint64
	EffectivePreset   string
	ConfigHash        string
	NativeMemoryBytes uint64
	EventCount        uint64
	Capabilities      AgentCapabilitySummary
	Quality           AgentQualitySummary
	GC                AgentIntervalSummary
	Contention        AgentIntervalSummary
	Threads           AgentThreadSummary
	Stacks            AgentStackSummary
	Findings          []AgentFinding
	DataGaps          []string
	Limitations       []string
	ConnectionGuide   []string
	PlatformLimits    []string
	CapabilityGaps    []string
}

type AgentComparison struct {
	ConfigMismatch     bool
	CapabilityMismatch bool
	Warnings           []string
	NewStackSuspects   []string
	ResolvedSuspects   []string
	CausalChanges      []string
}
