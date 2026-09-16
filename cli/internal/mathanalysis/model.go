package mathanalysis

import (
	"errors"
	"fmt"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
)

type MathReport struct {
	CollectionLimits    []CollectionLimit
	Title               string
	SourcePaths         []string
	IndependentRunCount int
	TimelineGroupCount  int
	Summary             analyze.Summary
	Sections            []MathSection
	Findings            []Finding
	Timeline            []TimelineBucket
	Series              []Series
	RobustStats         []RobustStat
	ChangePoints        []ChangePoint
	Periodic            []PeriodicSignal
	Spectral            []SpectralPeak
	NetworkLoops        []NetworkLoopFinding
	IntegralScores      []IntegralScore
	Markov              MarkovModel
	CausalGraph         CausalGraph
	GraphPaths          []GraphPath
}

type CompareMathReport struct {
	CollectionLimits  []CollectionLimit
	Title             string
	BaselinePaths     []string
	CandidatePaths    []string
	Baseline          MathReport
	Candidate         MathReport
	Comparison        analyze.Comparison
	Sections          []MathSection
	Findings          []Finding
	RobustDeltas      []RobustDelta
	ChangeDeltas      []ChangePointDelta
	NetworkLoopDeltas []NetworkLoopDelta
	IntegralDeltas    []IntegralDelta
	MarkovDeltas      []MarkovDelta
	CausalDeltas      []CausalDelta
}

type MathSection struct {
	ID       string
	Title    string
	Status   string
	Summary  string
	Findings []Finding
}

// CollectionLimit describes unavailable derived analysis, independently of SDK event delivery.
type CollectionLimit struct {
	Work           *SpectralWorkLimit `json:"Work,omitempty"`
	Component      string
	LimitBytes     uint64
	ReservedBytes  uint64
	RequestedBytes uint64
}

type SpectralWorkLimit struct {
	LimitOperations     uint64
	ConsumedOperations  uint64
	RequestedOperations uint64
}

type Finding struct {
	Severity       string
	Title          string
	Detail         string
	Recommendation string
	Evidence       []string
}

type TimelineBucket struct {
	HTTPCountState        string                 `json:",omitempty"`
	HTTPRouteObservations []HTTPRouteObservation `json:",omitempty"`
	StartMS               uint64
	EndMS                 uint64
	HasObservation        bool
	HTTPCount             int
	HTTPFailed            int
	HTTPAvgDurationMS     uint64
	HTTPP95DurationMS     uint64
	DNSCount              int
	DNSDurationMS         uint64
	ConnectCount          int
	ConnectDurationMS     uint64
	TTFBMS                uint64
	HasTTFB               bool
	UIFrames              uint64
	UIJankyFrames         uint64
	StallCount            int
	StallMaxMS            uint64
	MemoryPSSKB           uint64
	HasMemoryPSS          bool
	AvailableMemoryKB     uint64
	HasAvailableMemory    bool
	TrafficRxBytes        uint64
	TrafficTxBytes        uint64
	HasTrafficSample      bool
	TrafficRXKnown        bool
	TrafficTXKnown        bool
	RouteSample           string
	OwnerSample           string
	ScreenSample          string
	NetworkSample         string
}

type Series struct {
	Name     string
	Unit     string
	BucketMS uint64
	Points   []float64
	Present  []bool
}

type RobustStat struct {
	Dimension             string
	Name                  string
	Metric                string
	Unit                  string
	Count                 int
	Median                float64
	P90                   float64
	P95                   float64
	P99                   float64
	MAD                   float64
	TrimmedMean           float64
	Min                   float64
	Max                   float64
	P95ConfidenceLow      float64
	P95ConfidenceHigh     float64
	HasP95Confidence      bool
	SampleQuality         string
	SampleQualitySeverity string
	SampleDetail          string
}

type RobustDelta struct {
	Dimension         string
	Name              string
	Metric            string
	Unit              string
	BaselineCount     int
	CandidateCount    int
	BaselineP95       float64
	CandidateP95      float64
	P95Delta          float64
	P95DeltaPct       float64
	DeltaPctAvailable bool
	CliffDelta        float64
	Comparable        bool
	EffectSize        string
	Confidence        string
	Severity          string
	Summary           string
	Recommendation    string
}

type ChangePoint struct {
	Signal            string
	Unit              string
	TimeMS            uint64
	BeforeMedian      float64
	AfterMedian       float64
	BeforeMAD         float64
	AfterMAD          float64
	Delta             float64
	DeltaPct          float64
	DeltaPctAvailable bool
	Position          float64
	Score             float64
	Direction         string
	Severity          string
	NearbyRoute       string
	NearbyOwner       string
	NearbyScreen      string
	NearbyNetwork     string
	Recommendation    string
}

type ChangePointDelta struct {
	Signal         string
	Status         string
	TimeMS         uint64
	BaselineTime   uint64
	CandidateTime  uint64
	BaselineScore  float64
	CandidateScore float64
	Severity       string
	Summary        string
}

type PeriodicSignal struct {
	Signal                string
	Unit                  string
	BucketMS              uint64
	SampleCount           int
	TotalBucketCount      int
	ObservedBucketCount   int
	AnalyzedSampleCount   int
	AnalysisBucketMS      uint64
	Status                string
	Summary               string
	FirstSignificantLagMS uint64
	DecayHalfLifeMS       uint64
	SpectralEntropy       float64
	Approximated          bool
	TopLags               []AutocorrelationLag
	Peaks                 []SpectralPeak
}

type AutocorrelationLag struct {
	LagMS       uint64
	Correlation float64
}

type SpectralPeak struct {
	Signal           string
	PeriodMS         uint64
	FrequencyHz      float64
	Power            float64
	PeakToBackground float64
	SpectralEntropy  float64
	Confidence       float64
}

type NetworkLoopFinding struct {
	RouteAttributionStatus string
	OwnerAttributionStatus string
	Route                  string
	Owner                  string
	PeriodMS               uint64
	Confidence             float64
	Motif                  []string
	FirstMS                uint64
	LastMS                 uint64
	BurnScore              float64
	ProbableCause          string
	Path                   GraphPath
}

type NetworkLoopDelta struct {
	Route             string
	Owner             string
	Status            string
	BaselinePeriodMS  uint64
	CandidatePeriodMS uint64
	BaselineBurn      float64
	CandidateBurn     float64
	BurnDelta         float64
	ConfidenceDelta   float64
	Severity          string
	Summary           string
}

type IntegralScore struct {
	ID          string
	Title       string
	Formula     string
	Explanation string
	Unit        string
	Value       float64
	DurationMS  uint64
	RunCount    int
	Severity    string
	Summary     string
}

type IntegralDelta struct {
	ID                  string
	Title               string
	Formula             string
	Unit                string
	BaselineValue       float64
	CandidateValue      float64
	Delta               float64
	DeltaPct            float64
	DeltaPctAvailable   bool
	Comparable          bool
	BaselineDurationMS  uint64
	CandidateDurationMS uint64
	BaselineRunCount    int
	CandidateRunCount   int
	Severity            string
	Summary             string
}

type MarkovModel struct {
	States                  []MarkovBucketState
	Transitions             []MarkovTransition
	SampleCount             int
	TimelineBucketCount     int
	MissingBucketCount      int
	ObservationCoverage     float64
	TransitionEventCount    int
	BadEpisodeCount         int
	IndependentRunCount     int
	TimelineGroupCount      int
	SequenceComparable      bool
	Confidence              string
	ConfidenceReason        string
	HealthyToBadCount       int
	BadToHealthyProbability float64
	HasRecoveryProbability  bool
	ExpectedRecoveryWindows float64
	ExpectedRecoveryMS      float64
	HasExpectedRecovery     bool
	TotalDurationMS         uint64
	BadStateDurationMS      uint64
	BadStateExposure        float64
	StateExposures          []MarkovStateExposure
	StickyStates            []MarkovStickyState
	ContextStickyStates     []MarkovContextStickyState
	Forecast                MarkovForecast
}

type MarkovForecast struct {
	Direction               string
	Label                   string
	Severity                string
	Confidence              string
	ConfidenceReason        string
	Summary                 string
	SegmentWindows          int
	HorizonWindows          int
	HorizonMS               uint64
	EarlyBadExposure        float64
	RecentBadExposure       float64
	ProjectedBadProbability float64
}

type MarkovBucketState struct {
	TimeMS       uint64
	DurationMS   uint64
	State        string
	Reason       string
	Contributors []MarkovSymptomWeight
	Route        string
	Owner        string
	Screen       string
	Network      string
}

type MarkovSymptomWeight struct {
	State  string
	Weight float64
	Reason string
}

type MarkovTransition struct {
	From        string
	To          string
	Count       int
	Probability float64
}

type MarkovStickyState struct {
	State       string
	Count       int
	Probability float64
}

type MarkovStateExposure struct {
	State      string
	Windows    int
	DurationMS uint64
	Exposure   float64
}

type MarkovContextStickyState struct {
	State       string
	Context     string
	Count       int
	Probability float64
}

type MarkovDelta struct {
	Metric             string
	Unit               string
	BaselineValue      float64
	CandidateValue     float64
	Delta              float64
	Comparable         bool
	BaselineAvailable  bool
	CandidateAvailable bool
	Severity           string
	Summary            string
}

type CausalGraph struct {
	Nodes       []CausalNode
	Edges       []CausalEdge
	Paths       []GraphPath
	AllPairs    []GraphPath
	OwnerScores []OwnerBlameScore
}

type CausalNode struct {
	ID    string
	Label string
	Kind  string
}

type CausalEdge struct {
	From        string
	To          string
	FromLabel   string
	ToLabel     string
	Kind        string
	Count       int
	Weight      float64
	Confidence  float64
	Description string
}

type OwnerBlameScore struct {
	Owner string
	Score float64
	Rank  int
}

type CausalDelta struct {
	Kind           string
	Severity       string
	Summary        string
	BaselineValue  float64
	CandidateValue float64
	Delta          float64
}

type GraphPath struct {
	From       string
	To         string
	Nodes      []string
	Cost       float64
	Confidence float64
}

func AnalyzeInspectWithSummary(paths []string, options analyze.Options, summary analyze.Summary) (MathReport, error) {
	inputs, err := analyzeMathInputs(paths, options)
	if err != nil {
		var exhausted *collectionBudgetError
		if errors.As(err, &exhausted) {
			return unavailableMathReport(paths, summary, exhausted.limit), nil
		}
		return MathReport{}, err
	}
	robustStats := summarizeRobustSamples(inputs.RobustSamples)
	changePoints := detectChangePoints(inputs.Timeline)
	periodic, spectral := buildPeriodicAnalysisWithBudget(inputs.Timeline, inputs.Scale, inputs.RouteDefinitions, inputs.budget)
	if inputs.budget.failure != nil {
		return unavailableMathReport(paths, summary, inputs.budget.failure.limit), nil
	}
	integralScores := computeIntegralScoresForRuns(inputs.Timeline, inputs.NetworkLoops, inputs.TimelineGroups)
	markov := buildMarkovModelForRuns(inputs.Timeline, inputs.NetworkLoops, inputs.TimelineGroups)
	causalGraph := buildCausalGraphWithBudget(inputs.Timeline, inputs.NetworkLoops, markov, inputs.budget)
	if inputs.budget.failure != nil {
		return unavailableMathReport(paths, summary, inputs.budget.failure.limit), nil
	}
	return buildInspectReport(summary, paths, inputs.TimelineGroups, inputs.Timeline, inputs.Series, robustStats, changePoints, periodic, spectral, inputs.NetworkLoops, integralScores, markov, causalGraph), nil
}

func AnalyzeCompareWithSummaries(
	baselinePaths,
	candidatePaths []string,
	options analyze.Options,
	baselineSummary,
	candidateSummary analyze.Summary,
) (CompareMathReport, error) {
	baselineOptions := options
	candidateOptions := options
	if options.BaselineHeapEvidence != nil {
		baselineOptions.HeapEvidence = options.BaselineHeapEvidence
	}
	if options.CandidateHeapEvidence != nil {
		candidateOptions.HeapEvidence = options.CandidateHeapEvidence
	}

	budget := newCollectionBudgetWithWork(options.MathMemoryLimitBytes, options.MathSpectralWorkLimitOperations)
	baselineInputs, err := analyzeMathInputsWithBudget(baselinePaths, baselineOptions, budget)
	if err != nil {
		var exhausted *collectionBudgetError
		if errors.As(err, &exhausted) {
			return unavailableCompareMathReport(baselinePaths, candidatePaths, baselineSummary, candidateSummary, exhausted.limit), nil
		}
		return CompareMathReport{}, err
	}
	candidateInputs, err := analyzeMathInputsWithBudget(candidatePaths, candidateOptions, budget)
	if err != nil {
		var exhausted *collectionBudgetError
		if errors.As(err, &exhausted) {
			return unavailableCompareMathReport(baselinePaths, candidatePaths, baselineSummary, candidateSummary, exhausted.limit), nil
		}
		return CompareMathReport{}, err
	}

	baselineRobustStats := summarizeRobustSamples(baselineInputs.RobustSamples)
	candidateRobustStats := summarizeRobustSamples(candidateInputs.RobustSamples)
	robustDeltas := compareRobustSamples(baselineInputs.RobustSamples, candidateInputs.RobustSamples)
	baselineChangePoints := detectChangePoints(baselineInputs.Timeline)
	candidateChangePoints := detectChangePoints(candidateInputs.Timeline)
	changeDeltas := compareChangePoints(baselineChangePoints, candidateChangePoints)
	baselinePeriodic, baselineSpectral := buildPeriodicAnalysisWithBudget(baselineInputs.Timeline, baselineInputs.Scale, baselineInputs.RouteDefinitions, budget)
	candidatePeriodic, candidateSpectral := buildPeriodicAnalysisWithBudget(candidateInputs.Timeline, candidateInputs.Scale, candidateInputs.RouteDefinitions, budget)
	if budget.failure != nil {
		return unavailableCompareMathReport(baselinePaths, candidatePaths, baselineSummary, candidateSummary, budget.failure.limit), nil
	}
	networkLoopDeltas := compareNetworkLoops(baselineInputs.NetworkLoops, candidateInputs.NetworkLoops)
	baselineIntegralScores := computeIntegralScoresForRuns(baselineInputs.Timeline, baselineInputs.NetworkLoops, baselineInputs.TimelineGroups)
	candidateIntegralScores := computeIntegralScoresForRuns(candidateInputs.Timeline, candidateInputs.NetworkLoops, candidateInputs.TimelineGroups)
	integralDeltas := compareIntegralScores(baselineIntegralScores, candidateIntegralScores)
	baselineMarkov := buildMarkovModelForRuns(baselineInputs.Timeline, baselineInputs.NetworkLoops, baselineInputs.TimelineGroups)
	candidateMarkov := buildMarkovModelForRuns(candidateInputs.Timeline, candidateInputs.NetworkLoops, candidateInputs.TimelineGroups)
	markovDeltas := compareMarkovModels(baselineMarkov, candidateMarkov)
	baselineCausalGraph := buildCausalGraphWithBudget(baselineInputs.Timeline, baselineInputs.NetworkLoops, baselineMarkov, budget)
	candidateCausalGraph := buildCausalGraphWithBudget(candidateInputs.Timeline, candidateInputs.NetworkLoops, candidateMarkov, budget)
	if budget.failure != nil {
		return unavailableCompareMathReport(baselinePaths, candidatePaths, baselineSummary, candidateSummary, budget.failure.limit), nil
	}
	causalDeltas := compareCausalGraphsWithBudget(baselineCausalGraph, candidateCausalGraph, budget)
	if budget.failure != nil {
		return unavailableCompareMathReport(baselinePaths, candidatePaths, baselineSummary, candidateSummary, budget.failure.limit), nil
	}
	baseline := buildInspectReport(baselineSummary, baselinePaths, baselineInputs.TimelineGroups, baselineInputs.Timeline, baselineInputs.Series, baselineRobustStats, baselineChangePoints, baselinePeriodic, baselineSpectral, baselineInputs.NetworkLoops, baselineIntegralScores, baselineMarkov, baselineCausalGraph)
	candidate := buildInspectReport(candidateSummary, candidatePaths, candidateInputs.TimelineGroups, candidateInputs.Timeline, candidateInputs.Series, candidateRobustStats, candidateChangePoints, candidatePeriodic, candidateSpectral, candidateInputs.NetworkLoops, candidateIntegralScores, candidateMarkov, candidateCausalGraph)
	comparison := analyze.Compare(baselineSummary, candidateSummary)

	findings := compareFindings(comparison)
	return CompareMathReport{
		Title:             "базовый прогон против проверяемого",
		BaselinePaths:     append([]string(nil), baselinePaths...),
		CandidatePaths:    append([]string(nil), candidatePaths...),
		Baseline:          baseline,
		Candidate:         candidate,
		Comparison:        comparison,
		Findings:          findings,
		Sections:          compareSections(comparison, findings, baselineInputs.Timeline, candidateInputs.Timeline, robustDeltas, changeDeltas, baselinePeriodic, candidatePeriodic, networkLoopDeltas, integralDeltas, markovDeltas, causalDeltas),
		RobustDeltas:      robustDeltas,
		ChangeDeltas:      changeDeltas,
		NetworkLoopDeltas: networkLoopDeltas,
		IntegralDeltas:    integralDeltas,
		MarkovDeltas:      markovDeltas,
		CausalDeltas:      causalDeltas,
	}, nil
}

func buildInspectReport(summary analyze.Summary, paths []string, timelineGroupCount int, timeline []TimelineBucket, series []Series, robustStats []RobustStat, changePoints []ChangePoint, periodic []PeriodicSignal, spectral []SpectralPeak, networkLoops []NetworkLoopFinding, integralScores []IntegralScore, markov MarkovModel, causalGraph CausalGraph) MathReport {
	findings := dataQualityFindingsForRuns(summary, timelineGroupCount)
	acquisition := analyze.AcquisitionEvidenceFor(summary)
	markov.IndependentRunCount = acquisition.IndependentGroups
	return MathReport{
		Title:               titleFromPaths(paths),
		SourcePaths:         append([]string(nil), paths...),
		IndependentRunCount: acquisition.IndependentGroups,
		TimelineGroupCount:  normalizedRunCount(timelineGroupCount),
		Summary:             summary,
		Findings:            findings,
		Timeline:            timeline,
		Series:              series,
		RobustStats:         robustStats,
		ChangePoints:        changePoints,
		Periodic:            periodic,
		Spectral:            spectral,
		NetworkLoops:        networkLoops,
		IntegralScores:      integralScores,
		Markov:              markov,
		CausalGraph:         causalGraph,
		GraphPaths:          causalGraph.Paths,
		Sections:            inspectSections(summary, findings, timeline, series, robustStats, changePoints, periodic, networkLoops, integralScores, markov, causalGraph),
	}
}

func titleFromPaths(paths []string) string {
	if len(paths) == 0 {
		return "без исходных логов"
	}
	return strings.Join(paths, ", ")
}

func unavailableMathReport(paths []string, summary analyze.Summary, limit CollectionLimit) MathReport {
	return MathReport{Title: titleFromPaths(paths), SourcePaths: append([]string(nil), paths...), Summary: summary, CollectionLimits: []CollectionLimit{limit}}
}

func unavailableCompareMathReport(baselinePaths, candidatePaths []string, baseline, candidate analyze.Summary, limit CollectionLimit) CompareMathReport {
	return CompareMathReport{Title: "базовый прогон против проверяемого", BaselinePaths: append([]string(nil), baselinePaths...),
		CandidatePaths: append([]string(nil), candidatePaths...), Baseline: unavailableMathReport(baselinePaths, baseline, limit),
		Candidate: unavailableMathReport(candidatePaths, candidate, limit), Comparison: analyze.Compare(baseline, candidate), CollectionLimits: []CollectionLimit{limit}}
}

func dataQualityFindingsForRuns(summary analyze.Summary, timelineGroupCount int) []Finding {
	findings := warningFindings("", summary.Warnings, "Проверьте целостность входных .jhlog и фильтры команды перед тем, как доверять математическим выводам.")
	findings = append(findings, heapInformationFindings("", summary)...)
	if normalizedRunCount(timelineGroupCount) > 1 {
		findings = append(findings, Finding{
			Severity:       "medium",
			Title:          "Объединены отдельные временные шкалы",
			Detail:         fmt.Sprintf("Качество сбора: количество совмещённых временных шкал: %d. Они совмещены по относительному времени от начала каждого прогона, поэтому временная шкала описывает общий профиль сценария, а не одну непрерывную историю. Устойчивые распределения используют все реальные наблюдения. Марковский прогноз отключён, потому что переходы между объединёнными интервалами нельзя честно считать будущей траекторией одного запуска.", normalizedRunCount(timelineGroupCount)),
			Recommendation: "Для анализа последовательности состояний откройте каждый прогон отдельно. Для сравнения объединённых результатов используйте одинаковое число повторов одного сценария.",
		})
	}
	switch {
	case summary.EventCount == 0:
		findings = append(findings, Finding{
			Severity:       "high",
			Title:          "Нет событий для математического анализа",
			Detail:         "Лог не содержит событий, поэтому численные результаты недоступны. Пустые таблицы и предупреждения не означают нулевую нагрузку приложения.",
			Recommendation: "Проверьте, что события выполнения писались в .jhlog во время сценария, и повторите команду inspect с непустым логом.",
		})
	case summary.HTTPCount < 5 && summary.UIFrames < 300 && summary.ContextCount < 3:
		findings = append(findings, Finding{
			Severity:       "medium",
			Title:          "Недостаточно данных для надежного анализа",
			Detail:         fmt.Sprintf("Собрано %d событий, HTTP=%d, UI-кадры=%d, замеры контекста=%d. Этого мало для устойчивых выводов.", summary.EventCount, summary.HTTPCount, summary.UIFrames, summary.ContextCount),
			Recommendation: "Соберите более длинный прогон или несколько повторов того же сценария.",
		})
	default:
		findings = append(findings, Finding{
			Severity: "ok",
			Title:    "Данных достаточно для первичного математического анализа",
			Detail:   fmt.Sprintf("Собрано %d событий из %d логов. Выводы относятся только к записанному сценарию и не переносятся автоматически на все поведение приложения.", summary.EventCount, summary.LogCount),
		})
	}
	return findings
}

func compareFindings(comparison analyze.Comparison) []Finding {
	findings := make([]Finding, 0, len(comparison.Warnings)+len(comparison.Baseline.Warnings)+len(comparison.Candidate.Warnings)+1)
	for _, warning := range comparison.Warnings {
		findings = append(findings, Finding{
			Severity:       "medium",
			Title:          "Предупреждение о честности сравнения",
			Detail:         warning,
			Recommendation: "Проверьте, что базовый и проверяемый прогоны собраны на сопоставимых устройствах, версиях и сетях.",
		})
	}
	findings = append(findings, warningFindings("Базовый прогон", comparison.Baseline.Warnings, "Проверьте целостность журналов базового прогона перед выводом об ухудшении.")...)
	findings = append(findings, warningFindings("Проверяемый прогон", comparison.Candidate.Warnings, "Проверьте целостность журналов проверяемого прогона перед выводом об ухудшении.")...)
	findings = append(findings, heapInformationFindings("Базовый прогон", comparison.Baseline)...)
	findings = append(findings, heapInformationFindings("Проверяемый прогон", comparison.Candidate)...)
	if len(findings) == 0 {
		findings = append(findings, Finding{
			Severity: "ok",
			Title:    "Оба прогона пригодны для первичного сравнения",
			Detail:   "Расчеты выполнены по переданным прогонам. Отсутствие предупреждений о составе данных не доказывает, что сценарии полностью одинаковы.",
		})
	}
	return findings
}

func warningFindings(prefix string, warnings []string, recommendation string) []Finding {
	if len(warnings) == 0 {
		return nil
	}
	findings := make([]Finding, 0, len(warnings))
	for _, warning := range warnings {
		warning = strings.TrimSpace(warning)
		if warning == "" || isInternalCollectionWarning(warning) {
			continue
		}
		title := "Предупреждение о качестве данных"
		if prefix != "" {
			title = fmt.Sprintf("%s: предупреждение о качестве данных", prefix)
			warning = fmt.Sprintf("%s: %s", prefix, warning)
		}
		findingRecommendation := warningRecommendation(warning, recommendation)
		findings = append(findings, Finding{
			Severity:       "medium",
			Title:          title,
			Detail:         warning,
			Recommendation: findingRecommendation,
		})
	}
	return findings
}

func isInternalCollectionWarning(warning string) bool {
	normalized := strings.ToLower(strings.TrimSpace(warning))
	for _, marker := range [...]string{
		"качество сбора:",
		"переполненные внутренние списки событий",
		"writer " + "отклонил",
		"admission " + "lock",
		"модуль записи отклонил",
		"при строгой записи поток ожидал",
		"реестр prepared statement вытеснил",
		"элементов " + "evidence",
		"сборщики во время выполнения потеряли",
	} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

func warningRecommendation(warning string, fallback string) string {
	switch {
	case strings.Contains(warning, "ASM-диагностика не передана"):
		return "Передайте в CLI артефакт ASM-диагностики через --instrumentation-diagnostics <path>/instrumentation-diagnostics.jsonl. Если файла нет, пересоберите приложение после интеграции Jank Hunter, проверьте namespace модуля и при необходимости includePackages, затем повторите inspect/compare."
	case strings.Contains(warning, "ASM-диагностика пустая"):
		return "Пересоберите Android-модуль с включёнными ASM-хуками, проверьте списки включённых и исключённых пакетов и убедитесь, что Gradle-задача создала instrumentation-diagnostics.jsonl."
	case strings.Contains(warning, "ASM проверил классы, но не нашёл ASM-хуки") ||
		strings.Contains(warning, "ASM "+"прошел по классам, но не нашел "+"hooks"):
		return "Проверьте includePackages, excludePackages, включённые адаптеры и версии библиотек. После пересборки передайте свежий instrumentation-diagnostics.jsonl в CLI."
	default:
		return fallback
	}
}

func inspectSections(summary analyze.Summary, findings []Finding, timeline []TimelineBucket, series []Series, robustStats []RobustStat, changePoints []ChangePoint, periodic []PeriodicSignal, networkLoops []NetworkLoopFinding, integralScores []IntegralScore, markov MarkovModel, causalGraph CausalGraph) []MathSection {
	return []MathSection{
		{
			ID:       "quality",
			Title:    "Качество данных",
			Status:   sectionStatus(findings),
			Summary:  fmt.Sprintf("Журналы=%d, события=%d, длительность=%d мс, HTTP=%d, UI-кадры=%d, замеры контекста=%d.", summary.LogCount, summary.EventCount, summary.DurationMS, summary.HTTPCount, summary.UIFrames, summary.ContextCount),
			Findings: findings,
		},
		{
			ID:       "timeline",
			Title:    "Временная шкала сигналов",
			Status:   timelineStatus(timeline),
			Summary:  timelineSummary(timeline, series),
			Findings: timelineFindings(timeline),
		},
		{
			ID:       "robust",
			Title:    "Устойчивая статистика",
			Status:   robustStatus(robustStats),
			Summary:  robustSummary(robustStats),
			Findings: robustFindings(robustStats),
		},
		{
			ID:       "change-points",
			Title:    "Точки изменения",
			Status:   changePointStatus(timeline, changePoints),
			Summary:  changePointSummary(timeline, changePoints),
			Findings: changePointFindings(timeline, changePoints),
		},
		{
			ID:       "periodic",
			Title:    "Периодические сигналы",
			Status:   periodicStatus(periodic),
			Summary:  periodicSummary(periodic),
			Findings: periodicFindings(periodic),
		},
		{
			ID:       "network-loops",
			Title:    "Сетевые циклы",
			Status:   networkLoopStatus(networkLoops),
			Summary:  networkLoopSummary(networkLoops),
			Findings: networkLoopFindings(networkLoops),
		},
		{
			ID:       "integral",
			Title:    "Интегральная нагрузка",
			Status:   integralStatus(integralScores),
			Summary:  integralSummary(integralScores),
			Findings: integralFindings(integralScores),
		},
		{
			ID:       "markov",
			Title:    "Марковская модель состояний",
			Status:   markovStatus(markov),
			Summary:  markovSummary(markov),
			Findings: markovFindings(markov),
		},
		{
			ID:       "graph",
			Title:    "Граф связей и гипотез",
			Status:   causalGraphStatus(causalGraph),
			Summary:  causalGraphSummary(causalGraph),
			Findings: causalGraphFindings(causalGraph),
		},
	}
}

func compareSections(comparison analyze.Comparison, findings []Finding, baselineTimeline, candidateTimeline []TimelineBucket, robustDeltas []RobustDelta, changeDeltas []ChangePointDelta, baselinePeriodic, candidatePeriodic []PeriodicSignal, networkLoopDeltas []NetworkLoopDelta, integralDeltas []IntegralDelta, markovDeltas []MarkovDelta, causalDeltas []CausalDelta) []MathSection {
	return []MathSection{
		{
			ID:       "quality",
			Title:    "Качество сравнения",
			Status:   sectionStatus(findings),
			Summary:  fmt.Sprintf("Журналы базового прогона=%d, журналы проверяемого прогона=%d, сравнительных метрик=%d.", comparison.Baseline.LogCount, comparison.Candidate.LogCount, len(comparison.Deltas)),
			Findings: findings,
		},
		{
			ID:       "timeline",
			Title:    "Временная шкала сигналов",
			Status:   compareTimelineStatus(baselineTimeline, candidateTimeline),
			Summary:  compareTimelineSummary(baselineTimeline, candidateTimeline),
			Findings: compareTimelineFindings(baselineTimeline, candidateTimeline),
		},
		{
			ID:       "robust",
			Title:    "Устойчивая статистика",
			Status:   comparisonStatus(robustDeltas, "medium"),
			Summary:  compareRobustSummary(robustDeltas),
			Findings: compareRobustFindings(robustDeltas),
		},
		{
			ID:       "change-points",
			Title:    "Точки изменения",
			Status:   compareChangePointStatus(baselineTimeline, candidateTimeline, changeDeltas),
			Summary:  compareChangePointSummary(baselineTimeline, candidateTimeline, changeDeltas),
			Findings: compareChangePointFindings(changeDeltas),
		},
		{
			ID:       "periodic",
			Title:    "Периодические сигналы",
			Status:   comparePeriodicStatus(baselinePeriodic, candidatePeriodic),
			Summary:  comparePeriodicSummary(baselinePeriodic, candidatePeriodic),
			Findings: comparePeriodicFindings(baselinePeriodic, candidatePeriodic),
		},
		{
			ID:       "network-loops",
			Title:    "Сетевые циклы",
			Status:   comparisonStatus(networkLoopDeltas, "ok"),
			Summary:  compareNetworkLoopSummary(networkLoopDeltas),
			Findings: compareNetworkLoopFindings(networkLoopDeltas),
		},
		{
			ID:       "integral",
			Title:    "Интегральная нагрузка",
			Status:   comparisonStatus(integralDeltas, "medium"),
			Summary:  compareIntegralSummary(integralDeltas),
			Findings: compareIntegralFindings(integralDeltas),
		},
		{
			ID:       "markov",
			Title:    "Марковская модель состояний",
			Status:   comparisonStatus(markovDeltas, "medium"),
			Summary:  compareMarkovSummary(markovDeltas),
			Findings: compareMarkovFindings(markovDeltas),
		},
		{
			ID:       "graph",
			Title:    "Граф связей и гипотез",
			Status:   comparisonStatus(causalDeltas, "ok"),
			Summary:  compareCausalGraphSummary(causalDeltas),
			Findings: compareCausalGraphFindings(causalDeltas),
		},
	}
}

type comparisonDelta interface {
	comparisonSeverity() string
}

func (delta RobustDelta) comparisonSeverity() string      { return delta.Severity }
func (delta NetworkLoopDelta) comparisonSeverity() string { return delta.Severity }
func (delta IntegralDelta) comparisonSeverity() string    { return delta.Severity }
func (delta MarkovDelta) comparisonSeverity() string      { return delta.Severity }
func (delta CausalDelta) comparisonSeverity() string      { return delta.Severity }

func comparisonStatus[T comparisonDelta](deltas []T, emptyStatus string) string {
	if len(deltas) == 0 {
		return emptyStatus
	}
	status := "ok"
	for _, delta := range deltas {
		switch delta.comparisonSeverity() {
		case "high":
			return "high"
		case "medium":
			status = "medium"
		}
	}
	return status
}

func sectionStatus(findings []Finding) string {
	for _, finding := range findings {
		if finding.Severity == "high" {
			return "high"
		}
		if finding.Severity == "medium" {
			return "medium"
		}
	}
	return "ok"
}

func heapInformationFindings(prefix string, summary analyze.Summary) []Finding {
	var findings []Finding
	for _, d := range summary.HeapDiagnostics {
		if d.Informational() {
			title := "Сведения о подключении HPROF"
			if prefix != "" {
				title = prefix + ": " + title
			}
			findings = append(findings, Finding{Severity: "ok", Title: title, Detail: d.Message})
		}
	}
	return findings
}
