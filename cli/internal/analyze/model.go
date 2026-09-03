package analyze

import "github.com/i-redbyte/jank-hunter/cli/internal/jhlog"

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
	ObfuscationMap             *NameMapping
	ClassGraph                 *ClassGraph
	InstrumentationDiagnostics *InstrumentationDiagnostics
	DependencyInjectionCatalog *DependencyInjectionCatalog
	AndroidComponentCatalog    *AndroidComponentCatalog
	DatabaseEvidence           *DatabaseEvidence
	HeapEvidence               *HeapEvidence
	BaselineHeapEvidence       *HeapEvidence
	CandidateHeapEvidence      *HeapEvidence
	ArtifactDirectory          string
	ArtifactsAutoDiscovered    bool
	ArtifactSymbolNamespace    []byte
}

type RouteStats struct {
	Route                 string
	ServiceSample         string
	InitiatorSample       string
	Count                 int
	ContextCount          int
	Failures              int
	TransportFailures     int
	HTTP4xx               int
	HTTP5xx               int
	Canceled              int
	CacheHits             int
	ReusedConnections     int
	KnownRequestBytes     int
	KnownResponseBytes    int
	Attempts              uint64
	DNSAttempts           uint64
	ConnectAttempts       uint64
	TLSAttempts           uint64
	Retries               uint64
	Redirects             uint64
	ConnectFailures       uint64
	TLSFailures           uint64
	P50MS                 uint64
	P95MS                 uint64
	MaxMS                 uint64
	TotalDurationMS       uint64
	AvgTTFBMS             uint64
	MaxConcurrency        uint64
	PeakConcurrencyAtMS   uint64
	Phases                []HTTPPhaseStats
	BytesRx               uint64
	BytesTx               uint64
	OwnerSample           string
	PeakRequestsPerSecond uint64
	PeakWindowStartMS     uint64
	BurstEstimateStatus   string
}

type HTTPPhaseStats struct {
	Name        string
	SampleCount int
	AvgMS       uint64
	P50MS       uint64
	P95MS       uint64
	MaxMS       uint64
}

type NetworkCallStats struct {
	Route              string
	Service            string
	Initiator          string
	Screen             string
	Operation          string
	Owner              string
	Count              int
	Failures           int
	TransportFailures  int
	HTTP4xx            int
	HTTP5xx            int
	Canceled           int
	CacheHits          int
	ReusedConnections  int
	KnownRequestBytes  int
	KnownResponseBytes int
	Attempts           uint64
	DNSAttempts        uint64
	ConnectAttempts    uint64
	TLSAttempts        uint64
	Retries            uint64
	Redirects          uint64
	ConnectFailures    uint64
	TLSFailures        uint64
	P50MS              uint64
	P95MS              uint64
	MaxMS              uint64
	TotalDurationMS    uint64
	BytesRx            uint64
	BytesTx            uint64
	Phases             []HTTPPhaseStats
}

type NetworkAnalysis struct {
	MaxConcurrency      uint64
	PeakConcurrencyAtMS uint64
	TransportFailures   int
	HTTP4xx             int
	HTTP5xx             int
	Canceled            int
	CacheHits           int
	ReusedConnections   int
	KnownRequestBytes   int
	KnownResponseBytes  int
	Attempts            uint64
	DNSAttempts         uint64
	ConnectAttempts     uint64
	TLSAttempts         uint64
	Retries             uint64
	Redirects           uint64
	ConnectFailures     uint64
	TLSFailures         uint64
	BytesRx             uint64
	BytesTx             uint64
	TotalDurationMS     uint64
	Phases              []HTTPPhaseStats
	StatusCodes         []NamedValue
	FailurePhases       []NamedValue
	FailureKinds        []NamedValue
	Protocols           []NamedValue
	Calls               []NetworkCallStats
}

type WebSocketConnectionStats struct {
	Route          string
	Screen         string
	Operation      string
	Owner          string
	Opened         uint64
	Closed         uint64
	Failures       uint64
	ActiveAtEnd    uint64
	Reconnects     uint64
	ConnectP50MS   uint64
	ConnectP95MS   uint64
	ConnectMaxMS   uint64
	LifetimeP50MS  uint64
	LifetimeP95MS  uint64
	LifetimeMaxMS  uint64
	TextMessages   uint64
	BinaryMessages uint64
	ReceivedBytes  uint64
}

type WebSocketAnalysis struct {
	Opened         uint64
	Closed         uint64
	Failures       uint64
	ActiveAtEnd    uint64
	Reconnects     uint64
	ConnectP50MS   uint64
	ConnectP95MS   uint64
	ConnectMaxMS   uint64
	LifetimeP50MS  uint64
	LifetimeP95MS  uint64
	LifetimeMaxMS  uint64
	TextMessages   uint64
	BinaryMessages uint64
	ReceivedBytes  uint64
	FailureKinds   []NamedValue
	CloseCodes     []NamedValue
	Connections    []WebSocketConnectionStats
}

type DatabaseStatementStats struct {
	Query                  string
	Operation              string
	OperationCode          string
	StatementFingerprint   uint64
	Overall                DatabaseExecutionStats
	Main                   DatabaseExecutionStats
	Background             DatabaseExecutionStats
	Telemetry              DatabaseTelemetryStats
	MainCorrelation        DatabaseCorrelationStats
	BackgroundCorrelation  DatabaseCorrelationStats
	PeakCallsPerSecond     uint64
	PeakWindowStartMS      uint64
	RapidRepeats           uint64
	EstimatedCalls         uint64
	FrequencyEstimateError uint64
	Contexts               []DatabaseStatementContextStats
}

type DatabaseStatementContextStats struct {
	Source                 string
	Framework              string
	Screen                 string
	ContextOwner           string
	ContextOperation       string
	OperationID            uint64
	Process                string
	ProcessInstanceID      string
	SessionID              string
	Overall                DatabaseExecutionStats
	Main                   DatabaseExecutionStats
	Background             DatabaseExecutionStats
	MainCorrelation        DatabaseCorrelationStats
	BackgroundCorrelation  DatabaseCorrelationStats
	PeakCallsPerSecond     uint64
	PeakWindowStartMS      uint64
	RapidRepeats           uint64
	EstimatedCalls         uint64
	FrequencyEstimateError uint64
}

type DatabaseExecutionStats struct {
	Calls                 uint64
	Failures              uint64
	P50DurationUS         uint64
	P95DurationUS         uint64
	MaxDurationUS         uint64
	TotalDurationUS       uint64
	QuantilesApproximated bool
}

// DatabasePhaseStats contains only adapter-provided phase evidence. A zero sample count means that
// the phase was not measured and must never be inferred from the total database call duration.
type DatabasePhaseStats struct {
	Samples               uint64
	P50DurationUS         uint64
	P95DurationUS         uint64
	MaxDurationUS         uint64
	TotalDurationUS       uint64
	QuantilesApproximated bool
}

type DatabaseTelemetryStats struct {
	ResultKnownCalls       uint64
	TransactionLinkedCalls uint64
	PreparedExecutionCalls uint64
	PhaseMeasuredCalls     uint64
	PoolWait               DatabasePhaseStats
	LockWait               DatabasePhaseStats
	Execute                DatabasePhaseStats
	Materialize            DatabasePhaseStats
	Boundaries             []NamedValue
	FailureKinds           []NamedValue
	ResultKinds            []NamedValue
	ResultCountBuckets     []NamedValue
}

type DatabaseTransactionStats struct {
	Source            string
	Screen            string
	ContextOwner      string
	ContextOperation  string
	Process           string
	ProcessInstanceID string
	SessionID         string
	Mode              string
	Outcome           string
	FailureKind       string
	TransactionID     uint64
	ParentID          uint64
	DurationUS        uint64
	StatementCount    uint64
	ReadCount         uint64
	WriteCount        uint64
	MainThread        bool
	Complete          bool
	MissingStart      bool
	Correlation       DatabaseCorrelationStats
}

type DatabaseTransactionAnalysis struct {
	Events                    uint64
	Begun                     uint64
	Completed                 uint64
	Incomplete                uint64
	MissingStart              uint64
	DuplicateStart            uint64
	DroppedActiveStarts       uint64
	DroppedTransactionDetails uint64
	EvictedTransactionDetails uint64
	Success                   uint64
	Rollbacks                 uint64
	Failures                  uint64
	MainThread                uint64
	Background                uint64
	Nested                    uint64
	P50DurationUS             uint64
	P95DurationUS             uint64
	MaxDurationUS             uint64
	TotalDurationUS           uint64
	TotalStatementCount       uint64
	TotalReadCount            uint64
	TotalWriteCount           uint64
	MaxStatementCount         uint64
	QuantilesApproximated     bool
	Modes                     []NamedValue
	Outcomes                  []NamedValue
	FailureKinds              []NamedValue
	Transactions              []DatabaseTransactionStats
}

// DatabaseScenarioStats is a privacy-safe repeated-statement hypothesis. Equal normalized
// fingerprints can represent different bind values, so the analyzer never promotes this evidence
// to a proven N+1 diagnosis without an application-provided artifact.
type DatabaseScenarioStats struct {
	Kind                    string
	ClaimLevel              string
	ScopeKind               string
	Query                   string
	Source                  string
	Screen                  string
	ContextOperation        string
	Operation               string
	StatementFingerprint    uint64
	AffectedScopes          uint64
	ObservedScopes          uint64
	TransactionLinkedScopes uint64
	EstimatedCalls          uint64
	RetainedCalls           uint64
	MaxCallsPerScope        uint64
	CallsPerAffectedScope   float64
	CallsPerObservedScope   float64
	TotalDurationUS         uint64
	MaxDurationUS           uint64
	MaxSequenceSpanUS       uint64
	MainThreadCalls         uint64
	Failures                uint64
	CostLowerBound          bool
}

type DatabaseScenarioAnalysis struct {
	ObservedOperationScopes   uint64
	ObservedTransactionScopes uint64
	ScopeCountsApproximated   bool
	DroppedEvents             uint64
	EvictedGroups             uint64
	DroppedCandidates         uint64
	FrequencyEstimateError    uint64
	Candidates                []DatabaseScenarioStats
}

// DatabaseCorrelationStats counts exact half-open interval overlaps. Main-thread overlap is
// actionable evidence that SQL occupied the UI timeline; background overlap is temporal context
// only and must not be presented as proof of causality.
type DatabaseCorrelationStats struct {
	UIWindowOverlaps           uint64
	UIFrames                   uint64
	UIJankyFrames              uint64
	UIOverlapMaxDurationUS     uint64
	StallOverlaps              uint64
	StallOverlapMaxDurationUS  uint64
	HTTPOverlaps               uint64
	HTTPOverlapMaxDurationUS   uint64
	WorkerOverlaps             uint64
	WorkerOverlapMaxDurationUS uint64
	FileIOOverlaps             uint64
	FileIOOverlapMaxDurationUS uint64
	GCOverlaps                 uint64
	GCOverlapMaxDurationUS     uint64
}

type DatabaseAnalysis struct {
	KnownSQLCalls               uint64
	Overall                     DatabaseExecutionStats
	Main                        DatabaseExecutionStats
	Background                  DatabaseExecutionStats
	Telemetry                   DatabaseTelemetryStats
	Evidence                    *DatabaseEvidenceAnalysis    `json:",omitempty"`
	Transactions                *DatabaseTransactionAnalysis `json:",omitempty"`
	Scenarios                   DatabaseScenarioAnalysis
	MainCorrelation             DatabaseCorrelationStats
	BackgroundCorrelation       DatabaseCorrelationStats
	PeakCallsPerSecond          uint64
	PeakWindowStartMS           uint64
	RapidRepeats                uint64
	DroppedStatementEvents      uint64
	DroppedContextEvents        uint64
	EvictedStatementGroups      uint64
	EvictedContextGroups        uint64
	FrequencyEstimateError      uint64
	DroppedDBIntervals          uint64
	EvictedDBIntervals          uint64
	DroppedTransactionIntervals uint64
	EvictedTransactionIntervals uint64
	DroppedUIWindows            uint64
	EvictedUIWindows            uint64
	DroppedStallIntervals       uint64
	EvictedStallIntervals       uint64
	DroppedRelatedIntervals     uint64
	EvictedRelatedIntervals     uint64
	Statements                  []DatabaseStatementStats
}

type DatabaseCoverage struct {
	Status                    string `json:"status"`
	StatusLabel               string `json:"status_label"`
	Explanation               string `json:"explanation"`
	Action                    string `json:"action"`
	CollectorSessions         int    `json:"collector_sessions"`
	RuntimeEnabledSessions    int    `json:"runtime_enabled_sessions"`
	DiagnosticsAvailable      bool   `json:"diagnostics_available"`
	InstrumentedHooks         uint64 `json:"instrumented_hooks"`
	DisabledCandidates        uint64 `json:"disabled_candidates"`
	UnsupportedCandidates     uint64 `json:"unsupported_candidates"`
	ObservedCalls             uint64 `json:"observed_calls"`
	ObservedTransactionEvents uint64 `json:"observed_transaction_events"`
	CompletedTransactions     uint64 `json:"completed_transactions"`
	IncompleteTransactions    uint64 `json:"incomplete_transactions"`
	KnownSQLCalls             uint64 `json:"known_sql_calls"`
	DroppedStatementEvents    uint64 `json:"dropped_statement_events"`
	DroppedContextEvents      uint64 `json:"dropped_context_events"`
	DroppedCorrelationEvents  uint64 `json:"dropped_correlation_events"`
	DroppedTransactionDetails uint64 `json:"dropped_transaction_details"`
	DroppedScenarioEvents     uint64 `json:"dropped_scenario_events"`
	DroppedScenarioCandidates uint64 `json:"dropped_scenario_candidates"`
}

// WorkerAnalysis is reconstructed from the typed enqueue/start/finish lifecycle. Correlated
// values describe signals whose measurement windows overlap Worker execution; they do not imply
// that the Worker caused those values.
type WorkerAnalysis struct {
	Instances         uint64
	Executions        uint64
	Enqueued          uint64
	Started           uint64
	Finished          uint64
	Success           uint64
	Failures          uint64
	Retries           uint64
	Cancelled         uint64
	PeriodicInstances uint64
	MainThreadStarts  uint64
	MissingEnqueue    uint64
	MissingStart      uint64
	MissingFinish     uint64
	WaitSamples       uint64
	WaitP50MS         uint64
	WaitP95MS         uint64
	WaitMaxMS         uint64
	TotalWaitMS       uint64
	RunSamples        uint64
	RunP50MS          uint64
	RunP95MS          uint64
	RunMaxMS          uint64
	TotalRunMS        uint64
	MaxQueued         uint64
	PeakQueuedAtMS    uint64
	MaxRunning        uint64
	PeakRunningAtMS   uint64
	Outcomes          []NamedValue
	StopReasons       []NamedValue
	Workers           []WorkerStats
}

type WorkerStats struct {
	Worker                       string
	Screen                       string
	Operation                    string
	Owner                        string
	Instances                    uint64
	Executions                   uint64
	Enqueued                     uint64
	Started                      uint64
	Finished                     uint64
	Success                      uint64
	Failures                     uint64
	Retries                      uint64
	Cancelled                    uint64
	PeriodicInstances            uint64
	MainThreadStarts             uint64
	MissingEnqueue               uint64
	MissingStart                 uint64
	MissingFinish                uint64
	WaitSamples                  uint64
	WaitP50MS                    uint64
	WaitP95MS                    uint64
	WaitMaxMS                    uint64
	TotalWaitMS                  uint64
	RunSamples                   uint64
	RunP50MS                     uint64
	RunP95MS                     uint64
	RunMaxMS                     uint64
	TotalRunMS                   uint64
	MaxConcurrency               uint64
	CorrelatedHTTPCalls          uint64
	CorrelatedHTTPFailures       uint64
	CorrelatedHTTPDurationMS     uint64
	CorrelatedIOOperations       uint64
	CorrelatedIODurationUS       uint64
	CorrelatedIOBytes            uint64
	CPUSamples                   uint64
	AvgDeviceCPUPercentX100      uint64
	MaxDeviceCPUPercentX100      uint64
	CoreCPUSamples               uint64
	AvgCoreCPUPercentX100        uint64
	MaxCoreCPUPercentX100        uint64
	AllocationSamples            uint64
	AvgAllocationRateBytesPerSec uint64
	MaxAllocationRateBytesPerSec uint64
	GCCount                      uint64
	GCTimeMS                     uint64
	GCBlockingCount              uint64
	GCBlockingTimeMS             uint64
	GCBytesAllocated             uint64
	GCBytesFreed                 uint64
	MemoryPairs                  uint64
	AvgPSSDeltaKB                int64
	MaxPSSDuringKB               uint64
}

type ScreenStats struct {
	Screen                 string
	WindowCount            int
	WindowMS               uint64
	Frames                 uint64
	JankyFrames            uint64
	JankRatePct            float64
	FPSMeasuredFrames      uint64
	FPSMeasuredWindowMS    uint64
	FPSMeasuredWindowCount int
	FPSStatus              string
	AvgFPS                 float64
	MinFPS                 float64
	FrameP50MS             uint64
	FrameP95MS             uint64
	FrameP99MS             uint64
	FrameSource            string
	FrameDeadlineUS        uint64
	FrameDeadlineStatus    string
	FrameDurationBuckets   []uint64
	FrameDistributionState string
}

type ProcessExitStats struct {
	Reason                uint64
	ReasonLabel           string
	Count                 uint64
	LatestTimestampUnixMS uint64
	Importance            uint64
	MaxPSSKB              uint64
	MaxRSSKB              uint64
	Process               string
}

type IOStats struct {
	Operation               string
	Source                  string
	MainThread              bool
	Count                   uint64
	Failures                uint64
	KnownByteOperations     uint64
	TotalDurationUS         uint64
	KnownByteDurationUS     uint64
	P50DurationUS           uint64
	P95DurationUS           uint64
	MaxDurationUS           uint64
	Bytes                   uint64
	MaxBytes                uint64
	BytesPerSecond          uint64
	PeakOperationsPerSecond uint64
	PeakWindowStartMS       uint64
	Screen                  string
	ContextOperation        string
	Owner                   string
}

// IOAnalysis intentionally excludes database operations: SQL and DAO evidence has a dedicated
// DatabaseAnalysis. Calls retain exact source/context groups for file/content/sync work.
type IOAnalysis struct {
	Operations              uint64
	Failures                uint64
	MainThreadOperations    uint64
	MainThreadDurationUS    uint64
	SyncOperations          uint64
	KnownByteOperations     uint64
	KnownByteDurationUS     uint64
	Bytes                   uint64
	TotalDurationUS         uint64
	P50DurationUS           uint64
	P95DurationUS           uint64
	MaxDurationUS           uint64
	BytesPerSecond          uint64
	PeakOperationsPerSecond uint64
	PeakWindowStartMS       uint64
	MaxConcurrency          uint64
	PeakConcurrencyAtMS     uint64
	SourceCount             int
	Calls                   []IOStats
}

// AsyncAnalysis projects bounded runtime metric windows. Executor samples cover every wrapped
// task; generic Handler/coroutine rows only contain durations above the SDK recording threshold
// plus failures and therefore are intentionally reported as partial observations.
type AsyncAnalysis struct {
	Executors []AsyncExecutorStats
	Tasks     []AsyncTaskStats
}

type AsyncExecutorStats struct {
	Name                   string
	Started                uint64
	Failures               uint64
	WaitSamples            uint64
	AvgWaitMS              uint64
	MaxWaitMS              uint64
	ServiceSamples         uint64
	AvgServiceMS           uint64
	MaxServiceMS           uint64
	QueueSamples           uint64
	AvgQueueDepthX100      uint64
	MaxQueueDepth          uint64
	ActiveSamples          uint64
	AvgActiveCountX100     uint64
	MaxActiveCount         uint64
	MaxPoolSize            uint64
	CompletedHighWatermark uint64
}

type AsyncTaskStats struct {
	Kind            string
	Owner           string
	DurationSamples uint64
	AvgDurationMS   uint64
	MaxDurationMS   uint64
	Failures        uint64
}

// GCAnalysis is process-level evidence. Nearby UI windows are temporal correlations and do not
// attribute a pause to GC.
type GCAnalysis struct {
	CollectionCount              uint64
	TotalTimeMS                  uint64
	BlockingCount                uint64
	BlockingTimeMS               uint64
	BytesAllocated               uint64
	BytesFreed                   uint64
	AllocationRateSamples        uint64
	AvgAllocationRateBytesPerSec uint64
	MaxAllocationRateBytesPerSec uint64
	CollectionWindows            uint64
	JankyUIWindowsNearGC         uint64
}

type StartupAnalysis struct {
	ColdResumeSamples uint64
	AvgColdResumeMS   uint64
	MaxColdResumeMS   uint64
	UIVisibleCount    uint64
	UIHiddenCount     uint64
	Screens           []StartupScreenStats
	Transitions       []NamedValue
}

type StartupScreenStats struct {
	Screen        string
	ResumeSamples uint64
	AvgResumeMS   uint64
	MaxResumeMS   uint64
}

type OwnerStats struct {
	Owner     string
	Count     int
	TotalMS   uint64
	MaxMS     uint64
	Kind      string
	StackHint string
}

type SignalContextStats struct {
	Screen       string
	Operation    string
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
	Screen    string
	Operation string
	Owner     string
	Source    string
	Level     string
	Count     uint64
}

type ProblemWindowStats struct {
	Screen        string
	Operation     string
	Owner         string
	Kind          string
	Windows       int
	Count         uint64
	TotalWindowMS uint64
	MaxMS         uint64
}

type RuntimeCallStats struct {
	Screen    string
	Operation string
	Caller    string
	Callee    string
	Count     uint64
	TotalMS   uint64
	MaxMS     uint64
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
	Operations      []string               `json:"operations,omitempty"`
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
	Operation      string   `json:"operation,omitempty"`
	Route          string   `json:"route,omitempty"`
	Evidence       string   `json:"evidence"`
	Recommendation string   `json:"recommendation"`
	Signals        []string `json:"signals,omitempty"`
}

type MemoryLeakSuspect struct {
	ClassName                string
	Holder                   string
	Screen                   string
	Operation                string
	Count                    uint64
	MaxAgeMS                 uint64
	EvidenceKind             string
	EvidenceLabel            string
	EvidenceConfidence       string
	TimeOnlyCount            uint64
	AfterExplicitGCCount     uint64
	DataQuality              string
	QualityWarnings          []string
	EstimatedRetainedKB      uint64
	HeapEvidence             bool
	HeapCandidate            bool
	HeapSource               string
	GCRoot                   string
	GCRootCategory           string
	ChainFingerprint         string
	HolderField              string
	RetainedObjectCount      uint64
	ReferencePath            []HeapPathElement
	AlternativePaths         [][]HeapPathElement
	AlternativePathSummaries []string
	RetainedClassSample      []string
	LeakPattern              string
	ReferenceMatchers        []string
	RetainedSizeConfidence   string
	RetainedSizeExplanation  string
	DominatorPath            []string
	DominatorTreeConfidence  string
	DominatorTreeExplanation string
	LeakChainConfidence      string
	LeakChainSummary         string
	LeakChainActions         []string
	InvestigationSteps       []string
	FixExamples              []string
	VerificationSteps        []string
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
	ClassName           string              `json:"class_name"`
	Holder              string              `json:"holder,omitempty"`
	HolderField         string              `json:"holder_field,omitempty"`
	GCRoot              string              `json:"gc_root,omitempty"`
	GCRootCategory      string              `json:"gc_root_category,omitempty"`
	ChainFingerprint    string              `json:"chain_fingerprint,omitempty"`
	RetainedSizeKB      uint64              `json:"retained_size_kb,omitempty"`
	RetainedSizeBytes   uint64              `json:"retained_size_bytes,omitempty"`
	RetainedObjectCount uint64              `json:"retained_object_count,omitempty"`
	ReferencePath       []HeapPathElement   `json:"reference_path,omitempty"`
	AlternativePaths    [][]HeapPathElement `json:"alternative_paths,omitempty"`
	DominatorTree       []string            `json:"dominator_tree,omitempty"`
	LeakPattern         string              `json:"leak_pattern,omitempty"`
	ReferenceMatchers   []string            `json:"reference_matchers,omitempty"`
	Source              string              `json:"source,omitempty"`
	Confidence          string              `json:"confidence,omitempty"`
}

type HeapPathElement struct {
	ClassName string `json:"class_name,omitempty"`
	FieldName string `json:"field_name,omitempty"`
	ObjectID  string `json:"object_id,omitempty"`
	Kind      string `json:"kind,omitempty"`
}

type CollectionSegment struct {
	Source                           string
	Status                           string
	Sealed                           bool
	TailBytes                        uint64
	TotalRecords                     uint64
	DataRecords                      uint64
	DictionaryRecords                uint64
	ControlRecords                   uint64
	RuntimeGraphLogicalCalls         uint64
	RunID                            string
	ProcessInstanceID                string
	SessionID                        string
	SegmentIndex                     uint64
	ProcessName                      string
	ProcessScope                     string
	AllowedProcessCount              uint64
	ProcessScopeFingerprint          string
	ExpectedProcessCount             uint64
	ExpectedProcessFingerprint       string
	ProcessRosterDeclarationComplete bool
	EndReason                        string
	EndReasonCode                    uint64
	QualitySequence                  uint64
	QualityCounters                  []NamedValue
}

type CollectionQuality struct {
	Level                             string                            `json:"level"`
	DiagnosticCompletenessPercent     float64                           `json:"diagnostic_completeness_percent"`
	DiagnosticCompletenessModel       string                            `json:"diagnostic_completeness_model"`
	DiagnosticCompletenessLevel       string                            `json:"diagnostic_completeness_level"`
	DiagnosticCompletenessExplanation string                            `json:"diagnostic_completeness_explanation"`
	DiagnosticCompletenessComponents  []DiagnosticCompletenessComponent `json:"diagnostic_completeness_components"`
	Complete                          bool                              `json:"complete"`
	ChainValid                        bool                              `json:"chain_valid"`
	ExactAdmission                    bool                              `json:"exact_admission"`
	ProcessScope                      string                            `json:"process_scope"`
	AllowedProcessCount               uint64                            `json:"allowed_process_count,omitempty"`
	ProcessScopeFingerprint           string                            `json:"process_scope_fingerprint,omitempty"`
	ExpectedProcessCount              uint64                            `json:"expected_process_count"`
	ExpectedProcessFingerprint        string                            `json:"expected_process_fingerprint,omitempty"`
	ObservedProcessCount              uint64                            `json:"observed_process_count"`
	ProcessRosterDeclarationComplete  bool                              `json:"process_roster_declaration_complete"`
	ProcessRosterComplete             bool                              `json:"process_roster_complete"`
	RunCohortCount                    uint64                            `json:"run_cohort_count"`
	RunCohortConsistent               bool                              `json:"run_cohort_consistent"`
	AllProcessesConfigured            bool                              `json:"all_processes_configured"`
	ProcessScopeConsistent            bool                              `json:"process_scope_consistent"`
	CounterInvariantsValid            bool                              `json:"counter_invariants_valid"`
	QualityProgressionValid           bool                              `json:"quality_progression_valid"`
	SealedSegments                    int                               `json:"sealed_segments"`
	UnsealedSegments                  int                               `json:"unsealed_segments"`
	SegmentsWithQuality               int                               `json:"segments_with_quality"`
	SegmentsWithoutQuality            int                               `json:"segments_without_quality"`
	AcceptedEvents                    uint64                            `json:"accepted_events"`
	WrittenEvents                     uint64                            `json:"written_events"`
	DecodedCommittedChunks            uint64                            `json:"decoded_committed_chunks"`
	ReportedCommittedChunks           uint64                            `json:"reported_committed_chunks"`
	KnownLostEvents                   uint64                            `json:"known_lost_events"`
	PreAdmissionLostEvents            uint64                            `json:"pre_admission_lost_events"`
	PostAdmissionLostEvents           uint64                            `json:"post_admission_lost_events"`
	AdmissionContentionLostEvents     uint64                            `json:"admission_contention_lost_events"`
	WriterBackpressureCount           uint64                            `json:"writer_backpressure_count"`
	WriterBackpressureNanos           uint64                            `json:"writer_backpressure_nanos"`
	RuntimeGraphBackpressureCount     uint64                            `json:"runtime_graph_backpressure_count"`
	RuntimeGraphBackpressureNanos     uint64                            `json:"runtime_graph_backpressure_nanos"`
	RuntimeGraphProducerCapacityLoss  uint64                            `json:"runtime_graph_producer_capacity_loss"`
	RuntimeEventBackpressureCount     uint64                            `json:"runtime_event_backpressure_count"`
	RuntimeEventBackpressureNanos     uint64                            `json:"runtime_event_backpressure_nanos"`
	RuntimeHookFailures               uint64                            `json:"runtime_hook_failures,omitempty"`
	CriticalRuntimeHookFailures       uint64                            `json:"critical_runtime_hook_failures,omitempty"`
	RuntimeHookFailureDetails         []RuntimeHookFailureDetail        `json:"runtime_hook_failure_details,omitempty"`
	ArchiveEvictedRuns                uint64                            `json:"archive_evicted_runs,omitempty"`
	ArchiveEvictedSegments            uint64                            `json:"archive_evicted_segments,omitempty"`
	ArchiveEvictedBytes               uint64                            `json:"archive_evicted_bytes,omitempty"`
	RuntimeGraphInputEvents           uint64                            `json:"runtime_graph_input_events"`
	RuntimeGraphEmittedEvents         uint64                            `json:"runtime_graph_emitted_events"`
	DecodedRuntimeGraphCalls          uint64                            `json:"decoded_runtime_graph_calls"`
	RuntimeGraphEnabled               bool                              `json:"runtime_graph_enabled"`
	RuntimeGraphCompletenessRatio     float64                           `json:"runtime_graph_completeness_ratio"`
	RuntimeGraphStackMismatches       uint64                            `json:"runtime_graph_stack_mismatches"`
	DamagedSegments                   int                               `json:"damaged_segments"`
	ControlFailures                   uint64                            `json:"control_failures"`
	BoundedEvidenceLoss               uint64                            `json:"bounded_evidence_loss"`
	OtherEvidenceLoss                 uint64                            `json:"other_evidence_loss"`
	DictionaryOverflow                uint64                            `json:"dictionary_overflow"`
	DictionaryTruncated               uint64                            `json:"dictionary_truncated"`
	ChainIssues                       []string                          `json:"chain_issues,omitempty"`
	Notices                           []string                          `json:"notices,omitempty"`
	Reasons                           []string                          `json:"reasons,omitempty"`
}

type RuntimeHookFailureDetail struct {
	Reason      string `json:"reason"`
	Count       uint64 `json:"count"`
	Impact      string `json:"impact"`
	Explanation string `json:"explanation"`
}

type DiagnosticCompletenessComponent struct {
	ID              string  `json:"id"`
	Label           string  `json:"label"`
	Weight          float64 `json:"weight"`
	Excluded        bool    `json:"excluded"`
	CoveragePercent float64 `json:"coverage_percent"`
	EarnedPoints    float64 `json:"earned_points"`
	MissingPoints   float64 `json:"missing_points"`
	Explanation     string  `json:"explanation"`
}

// OperationAnalysis is a product-agnostic view of measured user, screen, background, system,
// and nested stage work. Correlated signals are attributed by the operation instance ID carried
// directly by each event; they are evidence of overlap, not proof of causality.
type OperationAnalysis struct {
	Started                   uint64                    `json:"started"`
	Completed                 uint64                    `json:"completed"`
	MissingFinish             uint64                    `json:"missing_finish"`
	MissingStart              uint64                    `json:"missing_start"`
	DuplicateStart            uint64                    `json:"duplicate_start"`
	InconsistentLifecycle     uint64                    `json:"inconsistent_lifecycle"`
	MissingParent             uint64                    `json:"missing_parent"`
	UnmatchedSignalEvents     uint64                    `json:"unmatched_signal_events"`
	LateSignalEvents          uint64                    `json:"late_signal_events"`
	DroppedActiveStarts       uint64                    `json:"dropped_active_starts"`
	DroppedSignalEvents       uint64                    `json:"dropped_signal_events"`
	DroppedSignalRollups      uint64                    `json:"dropped_signal_rollups"`
	CompletedContextEvictions uint64                    `json:"completed_context_evictions"`
	DroppedOperationSamples   uint64                    `json:"dropped_operation_samples"`
	DroppedTimeSlotSamples    uint64                    `json:"dropped_time_slot_samples"`
	DroppedDimensionSamples   uint64                    `json:"dropped_dimension_samples"`
	DroppedStageSamples       uint64                    `json:"dropped_stage_samples"`
	Operations                []OperationStats          `json:"operations"`
	TimeSlots                 []OperationTimeSlot       `json:"time_slots"`
	Dimensions                []OperationDimensionStats `json:"dimensions"`
	Stages                    []OperationStageStats     `json:"stages"`
	WorstIncidents            []OperationIncident       `json:"worst_incidents"`
}

type OperationStats struct {
	Operation                 string                            `json:"operation"`
	Kind                      string                            `json:"kind"`
	Screen                    string                            `json:"screen"`
	Count                     uint64                            `json:"count"`
	Success                   uint64                            `json:"success"`
	Failures                  uint64                            `json:"failures"`
	Cancelled                 uint64                            `json:"cancelled"`
	Timeouts                  uint64                            `json:"timeouts"`
	P50MS                     uint64                            `json:"p50_ms"`
	P90MS                     uint64                            `json:"p90_ms"`
	P95MS                     uint64                            `json:"p95_ms"`
	QuantilesApproximated     bool                              `json:"quantiles_approximated"`
	MaxMS                     uint64                            `json:"max_ms"`
	TotalMS                   uint64                            `json:"total_ms"`
	Budgeted                  uint64                            `json:"budgeted"`
	BudgetBreaches            uint64                            `json:"budget_breaches"`
	BudgetBreachRatePct       float64                           `json:"budget_breach_rate_pct"`
	CorrelatedHTTP            uint64                            `json:"correlated_http"`
	CorrelatedHTTPFailures    uint64                            `json:"correlated_http_failures"`
	CorrelatedHTTPDurationMS  uint64                            `json:"correlated_http_duration_ms"`
	CorrelatedWebSocket       uint64                            `json:"correlated_websocket"`
	CorrelatedWebSocketErrors uint64                            `json:"correlated_websocket_errors"`
	CorrelatedDatabase        uint64                            `json:"correlated_database"`
	CorrelatedDatabaseErrors  uint64                            `json:"correlated_database_errors"`
	CorrelatedDatabaseMain    uint64                            `json:"correlated_database_main_thread"`
	CorrelatedDatabaseUS      uint64                            `json:"correlated_database_duration_us"`
	WorstDatabaseStatements   []OperationDatabaseStatementStats `json:"worst_database_statements,omitempty"`
	CorrelatedCompose         uint64                            `json:"correlated_compose"`
	CorrelatedComposeMS       uint64                            `json:"correlated_compose_duration_ms"`
	CorrelatedWorkers         uint64                            `json:"correlated_workers"`
	CorrelatedWorkerFailures  uint64                            `json:"correlated_worker_failures"`
	CorrelatedWorkerMS        uint64                            `json:"correlated_worker_duration_ms"`
	CorrelatedStalls          uint64                            `json:"correlated_stalls"`
	CorrelatedStallMaxMS      uint64                            `json:"correlated_stall_max_ms"`
	CorrelatedUIFrames        uint64                            `json:"correlated_ui_frames"`
	CorrelatedUIJank          uint64                            `json:"correlated_ui_jank"`
	CorrelatedUIJankRatePct   float64                           `json:"correlated_ui_jank_rate_pct"`
	CorrelatedIO              uint64                            `json:"correlated_io"`
	CorrelatedIODurationUS    uint64                            `json:"correlated_io_duration_us"`
	CorrelatedIOBytes         uint64                            `json:"correlated_io_bytes"`
	CorrelatedProblems        uint64                            `json:"correlated_problems"`
	CorrelatedLogRecords      uint64                            `json:"correlated_log_records"`
	CorrelatedRuntimeCalls    uint64                            `json:"correlated_runtime_calls"`
	CorrelatedRuntimeTotalMS  uint64                            `json:"correlated_runtime_total_ms"`
	CorrelatedMetricEvents    uint64                            `json:"correlated_metric_events"`
	CorrelatedRetainedObjects uint64                            `json:"correlated_retained_objects"`
	MaxPSSKB                  uint64                            `json:"max_pss_kb"`
}

type OperationTimeSlot struct {
	StartUnixMS               uint64                            `json:"start_unix_ms"`
	TimezoneOffsetMin         int64                             `json:"timezone_offset_min"`
	Label                     string                            `json:"label"`
	Operation                 string                            `json:"operation"`
	Kind                      string                            `json:"kind"`
	Screen                    string                            `json:"screen"`
	Count                     uint64                            `json:"count"`
	Failures                  uint64                            `json:"failures"`
	P50MS                     uint64                            `json:"p50_ms"`
	P90MS                     uint64                            `json:"p90_ms"`
	P95MS                     uint64                            `json:"p95_ms"`
	QuantilesApproximated     bool                              `json:"quantiles_approximated"`
	MaxMS                     uint64                            `json:"max_ms"`
	Budgeted                  uint64                            `json:"budgeted"`
	BudgetBreaches            uint64                            `json:"budget_breaches"`
	BudgetBreachRatePct       float64                           `json:"budget_breach_rate_pct"`
	CorrelatedHTTP            uint64                            `json:"correlated_http"`
	CorrelatedHTTPFailures    uint64                            `json:"correlated_http_failures"`
	CorrelatedHTTPDurationMS  uint64                            `json:"correlated_http_duration_ms"`
	CorrelatedWebSocket       uint64                            `json:"correlated_websocket"`
	CorrelatedWebSocketErrors uint64                            `json:"correlated_websocket_errors"`
	CorrelatedDatabase        uint64                            `json:"correlated_database"`
	CorrelatedDatabaseErrors  uint64                            `json:"correlated_database_errors"`
	CorrelatedDatabaseMain    uint64                            `json:"correlated_database_main_thread"`
	CorrelatedDatabaseUS      uint64                            `json:"correlated_database_duration_us"`
	WorstDatabaseStatements   []OperationDatabaseStatementStats `json:"worst_database_statements,omitempty"`
	CorrelatedCompose         uint64                            `json:"correlated_compose"`
	CorrelatedComposeMS       uint64                            `json:"correlated_compose_duration_ms"`
	CorrelatedWorkers         uint64                            `json:"correlated_workers"`
	CorrelatedWorkerFailures  uint64                            `json:"correlated_worker_failures"`
	CorrelatedWorkerMS        uint64                            `json:"correlated_worker_duration_ms"`
	CorrelatedStalls          uint64                            `json:"correlated_stalls"`
	CorrelatedStallMaxMS      uint64                            `json:"correlated_stall_max_ms"`
	CorrelatedUIFrames        uint64                            `json:"correlated_ui_frames"`
	CorrelatedUIJank          uint64                            `json:"correlated_ui_jank"`
	CorrelatedUIJankRatePct   float64                           `json:"correlated_ui_jank_rate_pct"`
	CorrelatedIO              uint64                            `json:"correlated_io"`
	CorrelatedIODurationUS    uint64                            `json:"correlated_io_duration_us"`
	CorrelatedIOBytes         uint64                            `json:"correlated_io_bytes"`
	CorrelatedProblems        uint64                            `json:"correlated_problems"`
	CorrelatedLogRecords      uint64                            `json:"correlated_log_records"`
	CorrelatedRuntimeCalls    uint64                            `json:"correlated_runtime_calls"`
	CorrelatedRuntimeTotalMS  uint64                            `json:"correlated_runtime_total_ms"`
	CorrelatedMetricEvents    uint64                            `json:"correlated_metric_events"`
	CorrelatedRetainedObjects uint64                            `json:"correlated_retained_objects"`
	MaxPSSKB                  uint64                            `json:"max_pss_kb"`
}

type OperationDimensionStats struct {
	Operation             string  `json:"operation"`
	Kind                  string  `json:"kind"`
	Screen                string  `json:"screen"`
	Key                   string  `json:"key"`
	Value                 string  `json:"value"`
	Count                 uint64  `json:"count"`
	Failures              uint64  `json:"failures"`
	P90MS                 uint64  `json:"p90_ms"`
	P95MS                 uint64  `json:"p95_ms"`
	QuantilesApproximated bool    `json:"quantiles_approximated"`
	MaxMS                 uint64  `json:"max_ms"`
	Budgeted              uint64  `json:"budgeted"`
	BudgetBreaches        uint64  `json:"budget_breaches"`
	BudgetBreachRatePct   float64 `json:"budget_breach_rate_pct"`
}

type OperationStageStats struct {
	ParentOperation       string  `json:"parent_operation"`
	ParentKind            string  `json:"parent_kind"`
	Screen                string  `json:"screen"`
	Stage                 string  `json:"stage"`
	Count                 uint64  `json:"count"`
	P50MS                 uint64  `json:"p50_ms"`
	P90MS                 uint64  `json:"p90_ms"`
	P95MS                 uint64  `json:"p95_ms"`
	QuantilesApproximated bool    `json:"quantiles_approximated"`
	MaxMS                 uint64  `json:"max_ms"`
	TotalMS               uint64  `json:"total_ms"`
	ParentTotalMS         uint64  `json:"parent_total_ms"`
	SharePct              float64 `json:"share_pct"`
}

type OperationIncident struct {
	StartUnixMS               uint64                            `json:"start_unix_ms"`
	Operation                 string                            `json:"operation"`
	Kind                      string                            `json:"kind"`
	Screen                    string                            `json:"screen"`
	Outcome                   string                            `json:"outcome"`
	DurationMS                uint64                            `json:"duration_ms"`
	BudgetMS                  uint64                            `json:"budget_ms"`
	BudgetExceededMS          uint64                            `json:"budget_exceeded_ms"`
	CorrelatedHTTP            uint64                            `json:"correlated_http"`
	HTTPFailures              uint64                            `json:"http_failures"`
	HTTPDurationMS            uint64                            `json:"http_duration_ms"`
	CorrelatedWebSocket       uint64                            `json:"correlated_websocket"`
	WebSocketErrors           uint64                            `json:"websocket_errors"`
	CorrelatedDatabase        uint64                            `json:"correlated_database"`
	DatabaseErrors            uint64                            `json:"database_errors"`
	DatabaseMainThread        uint64                            `json:"database_main_thread"`
	DatabaseDurationUS        uint64                            `json:"database_duration_us"`
	WorstDatabaseStatements   []OperationDatabaseStatementStats `json:"worst_database_statements,omitempty"`
	CorrelatedCompose         uint64                            `json:"correlated_compose"`
	ComposeDurationMS         uint64                            `json:"compose_duration_ms"`
	CorrelatedWorkers         uint64                            `json:"correlated_workers"`
	WorkerFailures            uint64                            `json:"worker_failures"`
	WorkerDurationMS          uint64                            `json:"worker_duration_ms"`
	CorrelatedStalls          uint64                            `json:"correlated_stalls"`
	StallMaxMS                uint64                            `json:"stall_max_ms"`
	CorrelatedUIFrames        uint64                            `json:"correlated_ui_frames"`
	CorrelatedUIJank          uint64                            `json:"correlated_ui_jank"`
	CorrelatedIO              uint64                            `json:"correlated_io"`
	IODurationUS              uint64                            `json:"io_duration_us"`
	IOBytes                   uint64                            `json:"io_bytes"`
	CorrelatedProblems        uint64                            `json:"correlated_problems"`
	CorrelatedLogRecords      uint64                            `json:"correlated_log_records"`
	CorrelatedRuntimeCalls    uint64                            `json:"correlated_runtime_calls"`
	CorrelatedRuntimeTotalMS  uint64                            `json:"correlated_runtime_total_ms"`
	CorrelatedMetricEvents    uint64                            `json:"correlated_metric_events"`
	CorrelatedRetainedObjects uint64                            `json:"correlated_retained_objects"`
	MaxPSSKB                  uint64                            `json:"max_pss_kb"`
	Score                     uint64                            `json:"score"`
	instanceKey               operationInstanceKey
	databaseStatements        [operationDatabaseStatementLimit]OperationDatabaseStatementStats
	databaseStatementCount    uint8
}

type OperationDatabaseStatementStats struct {
	Query           string `json:"query"`
	Source          string `json:"source"`
	Operation       string `json:"operation"`
	Calls           uint64 `json:"calls"`
	Failures        uint64 `json:"failures"`
	MainThreadCalls uint64 `json:"main_thread_calls"`
	TotalDurationUS uint64 `json:"total_duration_us"`
	MaxDurationUS   uint64 `json:"max_duration_us"`
}

type Summary struct {
	Title                    string
	LogCount                 int
	EventCount               int
	TotalRecordCount         uint64
	DataRecordCount          uint64
	DictionaryRecords        uint64
	ControlRecords           uint64
	DurationMS               uint64
	Dictionary               int
	HTTPCount                int
	HTTPFailed               int
	HTTPP95MS                uint64
	UIFrames                 uint64
	UIJank                   uint64
	UIWindowMS               uint64
	UIJankPct                float64
	UIFPSMeasuredFrames      uint64
	UIFPSMeasuredWindowMS    uint64
	UIFPSMeasuredWindowCount int
	UIFPSStatus              string
	UIAvgFPS                 float64
	UIMinFPS                 float64
	StallCount               int
	StallMaxMS               uint64
	ContextCount             int
	MemoryCount              int
	BatteryMinPct            uint64
	BatteryLastPct           uint64
	AvailMemoryMinKB         uint64
	LowMemoryCount           int
	TrafficRxMax             uint64
	TrafficTxMax             uint64
	BatteryStateLast         uint64
	BatteryTempDeciC         int64
	AvailMemoryLastKB        uint64
	TotalMemoryKB            uint64
	FreeStorageKB            uint64
	TotalStorageKB           uint64
	NetworkMetered           bool
	NetworkValidated         bool
	NetworkVPN               bool
	DeviceRootKnown          bool
	DeviceRooted             bool
	MemoryMaxKB              uint64
	Retained                 uint64
	Environment              RunEnvironment
	Warnings                 []string
	CollectionSegments       []CollectionSegment
	CollectionQuality        CollectionQuality
	AnalysisInputs           AnalysisInputCompleteness
	LogGrowth                LogGrowthSummary
	EvidenceQuality          EvidenceQualityVector `json:"evidence_quality"`
	CollectorSessions        int
	CollectorFlagsAny        uint64
	CollectorFlagsAll        uint64
	DatabaseCoverage         DatabaseCoverage          `json:"database_coverage"`
	NetworkAnalysis          *NetworkAnalysis          `json:",omitempty"`
	WebSocketAnalysis        *WebSocketAnalysis        `json:",omitempty"`
	DatabaseAnalysis         *DatabaseAnalysis         `json:",omitempty"`
	WorkerAnalysis           *WorkerAnalysis           `json:",omitempty"`
	IOAnalysis               *IOAnalysis               `json:",omitempty"`
	AsyncAnalysis            *AsyncAnalysis            `json:",omitempty"`
	GCAnalysis               *GCAnalysis               `json:",omitempty"`
	StartupAnalysis          *StartupAnalysis          `json:",omitempty"`
	OperationAnalysis        *OperationAnalysis        `json:",omitempty"`
	AndroidComponents        *AndroidComponentAnalysis `json:",omitempty"`

	Routes               []RouteStats
	Screens              []ScreenStats
	ProcessExits         []ProcessExitStats
	Owners               []OwnerStats
	SignalContexts       []SignalContextStats
	LogSpam              []LogSpamStats
	ProblemWindows       []ProblemWindowStats
	RuntimeCalls         []RuntimeCallStats
	CodeProblems         []CodeProblemStats
	MemoryLeaks          []MemoryLeakSuspect
	AppVersions          []NamedValue
	Builds               []NamedValue
	Devices              []NamedValue
	SDKs                 []NamedValue
	Cohorts              []NamedValue
	Processes            []NamedValue
	Network              []NamedValue
	Memory               []NamedValue
	RetainedClasses      []NamedValue
	RetainedAgeBuckets   []NamedValue
	JankStats            []NamedValue
	Counters             []NamedValue
	Gauges               []NamedValue
	Influence            InfluenceSummary
	ProblemSchemaVersion string             `json:"problem_schema_version"`
	ProblemSummary       ProblemSummary     `json:"problem_summary"`
	Problems             []ProblemFinding   `json:"problems"`
	ProblemIncidents     []ProblemFinding   `json:"problem_incidents"`
	CategoryCoverage     []CategoryCoverage `json:"category_coverage"`
	Detectors            []DetectorMetadata `json:"detectors"`
}

type EvidenceQualityVector struct {
	SchemaVersion string                     `json:"schema_version"`
	Overall       string                     `json:"overall"`
	Headline      string                     `json:"headline"`
	Dimensions    []EvidenceQualityDimension `json:"dimensions"`
}

type EvidenceQualityDimension struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Status      string `json:"status"`
	Explanation string `json:"explanation"`
}

type AnalysisInputCompleteness struct {
	Status                     string   `json:"status"`
	Complete                   bool     `json:"complete"`
	RuntimeEvidence            bool     `json:"runtime_evidence"`
	ClassGraph                 bool     `json:"class_graph"`
	InstrumentationDiagnostics bool     `json:"instrumentation_diagnostics"`
	HeapEvidence               bool     `json:"heap_evidence"`
	ArtifactDirectory          string   `json:"artifact_directory,omitempty"`
	ArtifactsAutoDiscovered    bool     `json:"artifacts_auto_discovered"`
	ArtifactIdentityVerified   bool     `json:"artifact_identity_verified"`
	Missing                    []string `json:"missing,omitempty"`
	Explanation                string   `json:"explanation"`
}

type LogGrowthSummary struct {
	Available         bool                     `json:"available"`
	HistoryGeneration uint64                   `json:"history_generation"`
	CapturedAtMS      uint64                   `json:"captured_at_ms"`
	LiveCapturedAtMS  uint64                   `json:"live_captured_at_ms,omitempty"`
	LatestDataEventMS uint64                   `json:"latest_data_event_at_ms,omitempty"`
	InputBytes        uint64                   `json:"input_bytes,omitempty"`
	SnapshotLagMS     uint64                   `json:"snapshot_lag_ms,omitempty"`
	SnapshotLagBytes  uint64                   `json:"snapshot_lag_bytes,omitempty"`
	FreshnessStatus   string                   `json:"freshness_status"`
	FreshnessReason   string                   `json:"freshness_reason,omitempty"`
	Sessions          []jhlog.LogGrowthSession `json:"sessions,omitempty"`
	Days              []jhlog.LogGrowthDay     `json:"days,omitempty"`
	CurrentSession    *jhlog.LogGrowthSession  `json:"current_session,omitempty"`
}

type ClassGraph struct {
	Format  int                        `json:"format,omitempty"`
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
	TotalNodes       int
	TotalEdges       int
	ShownNodes       int
	ShownEdges       int
	TopNodes         []InfluenceNode
	TopEdges         []InfluenceEdge
	Views            []InfluenceGraphView
	Workspace        InfluenceGraphWorkspace
	HotPaths         []InfluencePath
	MethodHotspots   []InfluenceMethod
	Cycles           []InfluenceCycle
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
	RuntimeWallMS   uint64
	NetworkMS       uint64
	MemoryPressure  uint64
	UIJank          uint64
	Retained        uint64
	HeapEvidence    bool
	Operations      []string
	Screens         []string
	Routes          []string
	Reasons         []string
}

type InfluenceEdge struct {
	From             string
	To               string
	Count            uint64
	RuntimeCount     uint64
	StaticCount      uint64
	Influence        float64
	RuntimeConfirmed bool
	Evidence         string
	Reason           string
}

type InfluenceGraphView struct {
	ID              string
	Mode            string
	Title           string
	Explanation     string
	Filters         InfluenceGraphFilters
	Nodes           []InfluenceGraphNode
	Edges           []InfluenceGraphEdge
	TotalNodes      int
	TotalEdges      int
	ShownNodes      int
	ShownEdges      int
	OmittedNodes    int
	OmittedEdges    int
	OmissionReasons []string
	Limits          InfluenceGraphLimits
	Legend          []InfluenceGraphLegend
}

type InfluenceGraphFilters struct {
	Query        string
	PackageDepth int
	SelectedNode string
	Direction    string
	Depth        int
	RuntimeOnly  bool
	ContextKind  string
	ContextValue string
}

type InfluenceGraphLimits struct {
	MaxNodes int
	MaxEdges int
}

type InfluenceGraphLegend struct {
	Kind  string
	Label string
	Help  string
}

type InfluenceGraphNode struct {
	InfluenceNode
	ID                   string
	Kind                 string
	Package              string
	Breadcrumbs          []string
	Aggregate            bool
	Connector            bool
	ChildCount           int
	RuntimeClassCount    int
	StaticOnlyClassCount int
	ProblemClassCount    int
	Children             []string
	Explanation          string
}

type InfluenceGraphEdge struct {
	InfluenceEdge
	ID        string
	Aggregate bool
}

type InfluenceGraphWorkspace struct {
	Nodes           []InfluenceGraphNode
	Edges           []InfluenceGraphEdge
	TotalNodes      int
	TotalEdges      int
	ShownNodes      int
	ShownEdges      int
	OmittedNodes    int
	OmittedEdges    int
	Contexts        []InfluenceGraphContext
	TotalContexts   int
	ShownContexts   int
	OmissionReasons []string
}

type InfluenceGraphContext struct {
	ID           string
	Kind         string
	Value        string
	RuntimeNodes int
	ProblemNodes int
}

type InfluencePath struct {
	Nodes         []string
	Weight        float64
	RuntimeTarget bool
	Reason        string
}

type InfluenceMethod struct {
	ClassName      string
	Method         string
	Role           string
	Count          uint64
	Weight         float64
	RuntimeTouched bool
}

type InfluenceCycle struct {
	Nodes          []string
	Weight         uint64
	RuntimeTouched bool
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
	Comparable     bool
	ComparisonNote string
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
	Baseline          Summary
	Candidate         Summary
	Deltas            []Delta
	Warnings          []string
	CohortWarnings    []string
	QualityWarnings   []string
	ExposureWarnings  []string
	Database          DatabaseComparison         `json:"database"`
	AndroidComponents AndroidComponentComparison `json:"android_components"`
	OperationDeltas   []OperationDelta           `json:"operation_deltas,omitempty"`
	ProblemComparison ProblemComparison          `json:"problem_comparison"`
}

type AndroidComponentComparison struct {
	Comparable bool    `json:"comparable"`
	Partial    bool    `json:"partial"`
	Note       string  `json:"note,omitempty"`
	Metrics    []Delta `json:"metrics"`
}

type DatabaseComparison struct {
	Comparable bool                     `json:"comparable"`
	Note       string                   `json:"note,omitempty"`
	Metrics    []Delta                  `json:"metrics"`
	Statements []DatabaseStatementDelta `json:"statements"`
}

type DatabaseStatementDelta struct {
	Query                       string  `json:"query"`
	Operation                   string  `json:"operation"`
	BaselinePresent             bool    `json:"baseline_present"`
	CandidatePresent            bool    `json:"candidate_present"`
	Comparable                  bool    `json:"comparable"`
	LatencyComparable           bool    `json:"latency_comparable"`
	ExposureComparable          bool    `json:"exposure_comparable"`
	Status                      string  `json:"status"`
	Severity                    string  `json:"severity"`
	Confidence                  string  `json:"confidence"`
	Note                        string  `json:"note,omitempty"`
	BaselineCalls               uint64  `json:"baseline_calls"`
	CandidateCalls              uint64  `json:"candidate_calls"`
	BaselineCallsPerMinute      float64 `json:"baseline_calls_per_minute"`
	CandidateCallsPerMinute     float64 `json:"candidate_calls_per_minute"`
	BaselineCallsPerOperation   float64 `json:"baseline_calls_per_operation"`
	CandidateCallsPerOperation  float64 `json:"candidate_calls_per_operation"`
	BaselineMainP95US           uint64  `json:"baseline_main_p95_us"`
	CandidateMainP95US          uint64  `json:"candidate_main_p95_us"`
	BaselineBackgroundP95US     uint64  `json:"baseline_background_p95_us"`
	CandidateBackgroundP95US    uint64  `json:"candidate_background_p95_us"`
	BaselineMaxUS               uint64  `json:"baseline_max_us"`
	CandidateMaxUS              uint64  `json:"candidate_max_us"`
	BaselineMainRatePct         float64 `json:"baseline_main_rate_pct"`
	CandidateMainRatePct        float64 `json:"candidate_main_rate_pct"`
	BaselineFailureRatePct      float64 `json:"baseline_failure_rate_pct"`
	CandidateFailureRatePct     float64 `json:"candidate_failure_rate_pct"`
	BaselineRapidRepeatRatePct  float64 `json:"baseline_rapid_repeat_rate_pct"`
	CandidateRapidRepeatRatePct float64 `json:"candidate_rapid_repeat_rate_pct"`
	BaselineWallMSPerMinute     float64 `json:"baseline_wall_ms_per_minute"`
	CandidateWallMSPerMinute    float64 `json:"candidate_wall_ms_per_minute"`
	BaselineWallMSPerOperation  float64 `json:"baseline_wall_ms_per_operation"`
	CandidateWallMSPerOperation float64 `json:"candidate_wall_ms_per_operation"`
}

type OperationDelta struct {
	Operation                           string  `json:"operation"`
	Kind                                string  `json:"kind"`
	Screen                              string  `json:"screen"`
	BaselineCount                       uint64  `json:"baseline_count"`
	CandidateCount                      uint64  `json:"candidate_count"`
	BaselineP95MS                       uint64  `json:"baseline_p95_ms"`
	CandidateP95MS                      uint64  `json:"candidate_p95_ms"`
	BaselineQuantilesApproximated       bool    `json:"baseline_quantiles_approximated"`
	CandidateQuantilesApproximated      bool    `json:"candidate_quantiles_approximated"`
	P95ChangeMS                         float64 `json:"p95_change_ms"`
	P95ChangePct                        float64 `json:"p95_change_pct"`
	BaselineBudgeted                    uint64  `json:"baseline_budgeted"`
	CandidateBudgeted                   uint64  `json:"candidate_budgeted"`
	BaselineBudgetBreachPct             float64 `json:"baseline_budget_breach_pct"`
	CandidateBudgetBreachPct            float64 `json:"candidate_budget_breach_pct"`
	BudgetBreachChangePP                float64 `json:"budget_breach_change_pp"`
	BudgetComparable                    bool    `json:"budget_comparable"`
	BaselineFailureRatePct              float64 `json:"baseline_failure_rate_pct"`
	CandidateFailureRatePct             float64 `json:"candidate_failure_rate_pct"`
	FailureRateChangePP                 float64 `json:"failure_rate_change_pp"`
	DatabaseComparable                  bool    `json:"database_comparable"`
	BaselineDatabaseCallsPerOperation   float64 `json:"baseline_database_calls_per_operation"`
	CandidateDatabaseCallsPerOperation  float64 `json:"candidate_database_calls_per_operation"`
	BaselineDatabaseMainRatePct         float64 `json:"baseline_database_main_rate_pct"`
	CandidateDatabaseMainRatePct        float64 `json:"candidate_database_main_rate_pct"`
	BaselineDatabaseFailureRatePct      float64 `json:"baseline_database_failure_rate_pct"`
	CandidateDatabaseFailureRatePct     float64 `json:"candidate_database_failure_rate_pct"`
	BaselineDatabaseWallMSPerOperation  float64 `json:"baseline_database_wall_ms_per_operation"`
	CandidateDatabaseWallMSPerOperation float64 `json:"candidate_database_wall_ms_per_operation"`
	Comparable                          bool    `json:"comparable"`
	Status                              string  `json:"status"`
	Severity                            string  `json:"severity"`
	Confidence                          string  `json:"confidence"`
	Note                                string  `json:"note,omitempty"`
}

type ProblemComparison struct {
	SchemaVersion string         `json:"schema_version"`
	Summary       ProblemSummary `json:"summary"`
	Deltas        []ProblemDelta `json:"deltas"`
}

type ProblemDelta struct {
	Fingerprint string          `json:"fingerprint"`
	Status      string          `json:"status"`
	Comparable  bool            `json:"comparable"`
	Note        string          `json:"note,omitempty"`
	Baseline    *ProblemFinding `json:"baseline,omitempty"`
	Candidate   *ProblemFinding `json:"candidate,omitempty"`
}

type ThresholdConfig struct {
	MaxSeverity         string                        `json:"max_severity"`
	MinConfidence       string                        `json:"min_confidence"`
	RequireCleanCohorts bool                          `json:"require_clean_cohorts"`
	Metrics             map[string]MetricThreshold    `json:"metrics"`
	Leaks               LeakThreshold                 `json:"leaks"`
	Problems            ProblemGateThreshold          `json:"problems"`
	AndroidComponents   AndroidComponentGateThreshold `json:"android_components"`
}

type AndroidComponentGateThreshold struct {
	Enabled                                    bool     `json:"enabled"`
	AllowPartial                               bool     `json:"allow_partial"`
	MaxServiceFailureRateIncreasePP            *float64 `json:"max_service_failure_rate_increase_pp,omitempty"`
	MaxServiceTimeoutRateIncreasePP            *float64 `json:"max_service_timeout_rate_increase_pp,omitempty"`
	MaxServiceSlowRateIncreasePP               *float64 `json:"max_service_slow_rate_increase_pp,omitempty"`
	MaxReceiverFailureRateIncreasePP           *float64 `json:"max_receiver_failure_rate_increase_pp,omitempty"`
	MaxReceiverAsyncDeadlineRiskRateIncreasePP *float64 `json:"max_receiver_async_deadline_risk_rate_increase_pp,omitempty"`
	MaxReceiverSyncSlowRateIncreasePP          *float64 `json:"max_receiver_sync_slow_rate_increase_pp,omitempty"`
	MaxBinderClientP95IncreasePct              *float64 `json:"max_binder_client_p95_increase_pct,omitempty"`
	MaxBinderSlowMainThreadRateIncreasePP      *float64 `json:"max_binder_slow_main_thread_rate_increase_pp,omitempty"`
	MaxBinderFailureRateIncreasePP             *float64 `json:"max_binder_failure_rate_increase_pp,omitempty"`
	MaxBinderUnhandledRateIncreasePP           *float64 `json:"max_binder_unhandled_rate_increase_pp,omitempty"`
	MinBinderCorrelationCoveragePct            *float64 `json:"min_binder_correlation_coverage_pct,omitempty"`
}

type ProblemGateThreshold struct {
	MaxCritical       *int     `json:"max_critical,omitempty"`
	MaxHigh           *int     `json:"max_high,omitempty"`
	MaxMedium         *int     `json:"max_medium,omitempty"`
	MaxSeverity       string   `json:"max_severity,omitempty"`
	MinConfidence     string   `json:"min_confidence,omitempty"`
	FailOnNew         bool     `json:"fail_on_new,omitempty"`
	FailOnRegressed   bool     `json:"fail_on_regressed,omitempty"`
	ExcludeCategories []string `json:"exclude_categories,omitempty"`
	ExcludeDetectors  []string `json:"exclude_detectors,omitempty"`
	RequiredCoverage  []string `json:"required_coverage,omitempty"`
}

type MetricThreshold struct {
	MaxSeverity      string  `json:"max_severity"`
	MaxRegressionAbs float64 `json:"max_regression_abs"`
	MaxRegressionPct float64 `json:"max_regression_pct"`
}

type LeakThreshold struct {
	MaxCandidateTotal  int  `json:"max_candidate_total"`
	MaxNew             int  `json:"max_new"`
	MaxWorse           int  `json:"max_worse"`
	MaxHigh            int  `json:"max_high"`
	MaxRuntimeOnly     int  `json:"max_runtime_only"`
	FailOnNew          bool `json:"fail_on_new"`
	FailOnWorse        bool `json:"fail_on_worse"`
	FailOnNewHigh      bool `json:"fail_on_new_high"`
	RequireHeapForHigh bool `json:"require_heap_for_high"`
}

type GateResult struct {
	Failed   bool     `json:"failed"`
	Failures []string `json:"failures"`
}
