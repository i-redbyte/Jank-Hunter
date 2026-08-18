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
	OwnerMap                   *OwnerMap
	ObfuscationMap             *NameMapping
	ClassGraph                 *ClassGraph
	InstrumentationDiagnostics *InstrumentationDiagnostics
	DependencyInjectionCatalog *DependencyInjectionCatalog
	HeapEvidence               *HeapEvidence
	BaselineHeapEvidence       *HeapEvidence
	CandidateHeapEvidence      *HeapEvidence
	ArtifactDirectory          string
	ArtifactsAutoDiscovered    bool
	ArtifactSymbolNamespace    []byte
	// ExternalSymbols opts into resolving stable ASM IDs from OwnerMap instead of the log.
	ExternalSymbols bool
	// RequireExplicitExternalSymbols is enabled by the CLI to prevent silent broken reports.
	RequireExplicitExternalSymbols bool
}

type RouteStats struct {
	Route                 string
	Count                 int
	Failures              int
	P50MS                 uint64
	P95MS                 uint64
	MaxMS                 uint64
	AvgTTFBMS             uint64
	BytesRx               uint64
	BytesTx               uint64
	OwnerSample           string
	PeakRequestsPerSecond uint64
	PeakWindowStartMS     uint64
	BurstEstimateStatus   string
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
	Operation       string
	MainThread      bool
	Count           uint64
	TotalDurationUS uint64
	MaxDurationUS   uint64
	Bytes           uint64
	Screen          string
	Flow            string
	Step            string
	Owner           string
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
	Level                            string                     `json:"level"`
	TrustScorePercent                float64                    `json:"trust_score_percent"`
	TrustScoreModel                  string                     `json:"trust_score_model"`
	TrustLevel                       string                     `json:"trust_level"`
	TrustLevelExplanation            string                     `json:"trust_level_explanation"`
	TrustComponents                  []CollectionTrustComponent `json:"trust_components"`
	Complete                         bool                       `json:"complete"`
	ChainValid                       bool                       `json:"chain_valid"`
	ExactAdmission                   bool                       `json:"exact_admission"`
	ProcessScope                     string                     `json:"process_scope"`
	AllowedProcessCount              uint64                     `json:"allowed_process_count,omitempty"`
	ProcessScopeFingerprint          string                     `json:"process_scope_fingerprint,omitempty"`
	ExpectedProcessCount             uint64                     `json:"expected_process_count"`
	ExpectedProcessFingerprint       string                     `json:"expected_process_fingerprint,omitempty"`
	ObservedProcessCount             uint64                     `json:"observed_process_count"`
	ProcessRosterDeclarationComplete bool                       `json:"process_roster_declaration_complete"`
	ProcessRosterComplete            bool                       `json:"process_roster_complete"`
	RunCohortCount                   uint64                     `json:"run_cohort_count"`
	RunCohortConsistent              bool                       `json:"run_cohort_consistent"`
	AllProcessesConfigured           bool                       `json:"all_processes_configured"`
	ProcessScopeConsistent           bool                       `json:"process_scope_consistent"`
	CounterInvariantsValid           bool                       `json:"counter_invariants_valid"`
	QualityProgressionValid          bool                       `json:"quality_progression_valid"`
	SealedSegments                   int                        `json:"sealed_segments"`
	UnsealedSegments                 int                        `json:"unsealed_segments"`
	SegmentsWithQuality              int                        `json:"segments_with_quality"`
	SegmentsWithoutQuality           int                        `json:"segments_without_quality"`
	AcceptedEvents                   uint64                     `json:"accepted_events"`
	WrittenEvents                    uint64                     `json:"written_events"`
	DecodedCommittedChunks           uint64                     `json:"decoded_committed_chunks"`
	ReportedCommittedChunks          uint64                     `json:"reported_committed_chunks"`
	KnownLostEvents                  uint64                     `json:"known_lost_events"`
	WriterBackpressureCount          uint64                     `json:"writer_backpressure_count"`
	WriterBackpressureNanos          uint64                     `json:"writer_backpressure_nanos"`
	RuntimeHookFailures              uint64                     `json:"runtime_hook_failures,omitempty"`
	CriticalRuntimeHookFailures      uint64                     `json:"critical_runtime_hook_failures,omitempty"`
	RuntimeHookFailureDetails        []RuntimeHookFailureDetail `json:"runtime_hook_failure_details,omitempty"`
	ArchiveEvictedRuns               uint64                     `json:"archive_evicted_runs,omitempty"`
	ArchiveEvictedSegments           uint64                     `json:"archive_evicted_segments,omitempty"`
	ArchiveEvictedBytes              uint64                     `json:"archive_evicted_bytes,omitempty"`
	RuntimeGraphInputEvents          uint64                     `json:"runtime_graph_input_events"`
	RuntimeGraphEmittedEvents        uint64                     `json:"runtime_graph_emitted_events"`
	DecodedRuntimeGraphCalls         uint64                     `json:"decoded_runtime_graph_calls"`
	RuntimeGraphEnabled              bool                       `json:"runtime_graph_enabled"`
	RuntimeGraphCompletenessRatio    float64                    `json:"runtime_graph_completeness_ratio"`
	RuntimeGraphStackMismatches      uint64                     `json:"runtime_graph_stack_mismatches"`
	DamagedSegments                  int                        `json:"damaged_segments"`
	ControlFailures                  uint64                     `json:"control_failures"`
	BoundedEvidenceLoss              uint64                     `json:"bounded_evidence_loss"`
	OtherEvidenceLoss                uint64                     `json:"other_evidence_loss"`
	DictionaryOverflow               uint64                     `json:"dictionary_overflow"`
	DictionaryTruncated              uint64                     `json:"dictionary_truncated"`
	ChainIssues                      []string                   `json:"chain_issues,omitempty"`
	Notices                          []string                   `json:"notices,omitempty"`
	Reasons                          []string                   `json:"reasons,omitempty"`
}

type RuntimeHookFailureDetail struct {
	Reason      string `json:"reason"`
	Count       uint64 `json:"count"`
	Impact      string `json:"impact"`
	Explanation string `json:"explanation"`
}

type CollectionTrustComponent struct {
	ID              string  `json:"id"`
	Label           string  `json:"label"`
	Weight          float64 `json:"weight"`
	Excluded        bool    `json:"excluded"`
	CoveragePercent float64 `json:"coverage_percent"`
	EarnedPoints    float64 `json:"earned_points"`
	MissingPoints   float64 `json:"missing_points"`
	Explanation     string  `json:"explanation"`
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

	Routes               []RouteStats
	Screens              []ScreenStats
	ProcessExits         []ProcessExitStats
	IOOperations         []IOStats
	Owners               []OwnerStats
	Flows                []FlowStats
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
	SymbolsResolved            bool     `json:"symbols_resolved"`
	SymbolMode                 string   `json:"symbol_mode"`
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
	Flows           []string
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
	ProblemComparison ProblemComparison `json:"problem_comparison"`
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
	MaxSeverity         string                     `json:"max_severity"`
	MinConfidence       string                     `json:"min_confidence"`
	RequireCleanCohorts bool                       `json:"require_clean_cohorts"`
	Metrics             map[string]MetricThreshold `json:"metrics"`
	Leaks               LeakThreshold              `json:"leaks"`
	Problems            ProblemGateThreshold       `json:"problems"`
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
