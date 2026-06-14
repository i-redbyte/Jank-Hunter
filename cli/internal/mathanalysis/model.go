package mathanalysis

import (
	"fmt"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
)

type MathReport struct {
	Title          string
	SourcePaths    []string
	Summary        analyze.Summary
	Sections       []MathSection
	Findings       []Finding
	Timeline       []TimelineBucket
	Series         []Series
	RobustStats    []RobustStat
	ChangePoints   []ChangePoint
	Periodic       []PeriodicSignal
	Spectral       []SpectralPeak
	NetworkLoops   []NetworkLoopFinding
	IntegralScores []IntegralScore
	Markov         MarkovModel
	CausalGraph    CausalGraph
	GraphPaths     []GraphPath
}

type CompareMathReport struct {
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

type Finding struct {
	Severity       string
	Title          string
	Detail         string
	Recommendation string
	Evidence       []string
}

type TimelineBucket struct {
	StartMS           uint64
	EndMS             uint64
	HTTPCount         int
	HTTPFailed        int
	HTTPAvgDurationMS uint64
	HTTPP95DurationMS uint64
	DNSCount          int
	DNSDurationMS     uint64
	ConnectCount      int
	ConnectDurationMS uint64
	TTFBMS            uint64
	UIFrames          uint64
	UIJankyFrames     uint64
	StallCount        int
	StallMaxMS        uint64
	MemoryPSSKB       uint64
	AvailableMemoryKB uint64
	TrafficRxBytes    uint64
	TrafficTxBytes    uint64
	RouteSample       string
	OwnerSample       string
	ScreenSample      string
	NetworkSample     string
}

type Series struct {
	Name     string
	Unit     string
	BucketMS uint64
	Points   []float64
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
	Dimension      string
	Name           string
	Metric         string
	Unit           string
	BaselineCount  int
	CandidateCount int
	BaselineP95    float64
	CandidateP95   float64
	P95Delta       float64
	P95DeltaPct    float64
	CliffDelta     float64
	EffectSize     string
	Confidence     string
	Severity       string
	Summary        string
	Recommendation string
}

type ChangePoint struct {
	Signal         string
	Unit           string
	TimeMS         uint64
	BeforeMedian   float64
	AfterMedian    float64
	BeforeMAD      float64
	AfterMAD       float64
	Delta          float64
	DeltaPct       float64
	Score          float64
	Direction      string
	Severity       string
	NearbyRoute    string
	NearbyOwner    string
	NearbyScreen   string
	NearbyNetwork  string
	Recommendation string
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
	Route         string
	Owner         string
	PeriodMS      uint64
	Confidence    float64
	Motif         []string
	FirstMS       uint64
	LastMS        uint64
	BurnScore     float64
	ProbableCause string
	Path          GraphPath
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
	Severity    string
	Summary     string
}

type IntegralDelta struct {
	ID             string
	Title          string
	Formula        string
	Unit           string
	BaselineValue  float64
	CandidateValue float64
	Delta          float64
	DeltaPct       float64
	Severity       string
	Summary        string
}

type MarkovModel struct {
	States                  []MarkovBucketState
	Transitions             []MarkovTransition
	HealthyToBadCount       int
	BadToHealthyProbability float64
	ExpectedRecoveryWindows float64
	StickyStates            []MarkovStickyState
}

type MarkovBucketState struct {
	TimeMS  uint64
	State   string
	Reason  string
	Route   string
	Owner   string
	Screen  string
	Network string
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

type MarkovDelta struct {
	Metric         string
	Unit           string
	BaselineValue  float64
	CandidateValue float64
	Delta          float64
	Severity       string
	Summary        string
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

func AnalyzeInspect(paths []string, options analyze.Options) (MathReport, error) {
	summary, err := analyze.InspectFilesWithOptions(titleFromPaths(paths), paths, options)
	if err != nil {
		return MathReport{}, err
	}
	timeline, series, err := buildTimeline(paths, options)
	if err != nil {
		return MathReport{}, err
	}
	robustSamples, err := collectRobustSamples(paths, options)
	if err != nil {
		return MathReport{}, err
	}
	robustStats := summarizeRobustSamples(robustSamples)
	changePoints := detectChangePoints(timeline)
	periodic, spectral, err := buildPeriodicAnalysis(paths, options, timeline)
	if err != nil {
		return MathReport{}, err
	}
	networkLoops, err := detectNetworkLoops(paths, options, timeline)
	if err != nil {
		return MathReport{}, err
	}
	integralScores := computeIntegralScores(timeline, networkLoops)
	markov := buildMarkovModel(timeline, networkLoops)
	causalGraph := buildCausalGraph(timeline, networkLoops, markov)
	return buildInspectReport(summary, paths, timeline, series, robustStats, changePoints, periodic, spectral, networkLoops, integralScores, markov, causalGraph), nil
}

func AnalyzeCompare(baselinePaths, candidatePaths []string, options analyze.Options) (CompareMathReport, error) {
	baselineSummary, err := analyze.InspectFilesWithOptions("baseline", baselinePaths, options)
	if err != nil {
		return CompareMathReport{}, err
	}
	candidateSummary, err := analyze.InspectFilesWithOptions("candidate", candidatePaths, options)
	if err != nil {
		return CompareMathReport{}, err
	}

	baselineTimeline, baselineSeries, err := buildTimeline(baselinePaths, options)
	if err != nil {
		return CompareMathReport{}, err
	}
	candidateTimeline, candidateSeries, err := buildTimeline(candidatePaths, options)
	if err != nil {
		return CompareMathReport{}, err
	}
	baselineRobustSamples, err := collectRobustSamples(baselinePaths, options)
	if err != nil {
		return CompareMathReport{}, err
	}
	candidateRobustSamples, err := collectRobustSamples(candidatePaths, options)
	if err != nil {
		return CompareMathReport{}, err
	}

	baselineRobustStats := summarizeRobustSamples(baselineRobustSamples)
	candidateRobustStats := summarizeRobustSamples(candidateRobustSamples)
	robustDeltas := compareRobustSamples(baselineRobustSamples, candidateRobustSamples)
	baselineChangePoints := detectChangePoints(baselineTimeline)
	candidateChangePoints := detectChangePoints(candidateTimeline)
	changeDeltas := compareChangePoints(baselineChangePoints, candidateChangePoints)
	baselinePeriodic, baselineSpectral, err := buildPeriodicAnalysis(baselinePaths, options, baselineTimeline)
	if err != nil {
		return CompareMathReport{}, err
	}
	candidatePeriodic, candidateSpectral, err := buildPeriodicAnalysis(candidatePaths, options, candidateTimeline)
	if err != nil {
		return CompareMathReport{}, err
	}
	baselineNetworkLoops, err := detectNetworkLoops(baselinePaths, options, baselineTimeline)
	if err != nil {
		return CompareMathReport{}, err
	}
	candidateNetworkLoops, err := detectNetworkLoops(candidatePaths, options, candidateTimeline)
	if err != nil {
		return CompareMathReport{}, err
	}
	networkLoopDeltas := compareNetworkLoops(baselineNetworkLoops, candidateNetworkLoops)
	baselineIntegralScores := computeIntegralScores(baselineTimeline, baselineNetworkLoops)
	candidateIntegralScores := computeIntegralScores(candidateTimeline, candidateNetworkLoops)
	integralDeltas := compareIntegralScores(baselineIntegralScores, candidateIntegralScores)
	baselineMarkov := buildMarkovModel(baselineTimeline, baselineNetworkLoops)
	candidateMarkov := buildMarkovModel(candidateTimeline, candidateNetworkLoops)
	markovDeltas := compareMarkovModels(baselineMarkov, candidateMarkov)
	baselineCausalGraph := buildCausalGraph(baselineTimeline, baselineNetworkLoops, baselineMarkov)
	candidateCausalGraph := buildCausalGraph(candidateTimeline, candidateNetworkLoops, candidateMarkov)
	causalDeltas := compareCausalGraphs(baselineCausalGraph, candidateCausalGraph)
	baseline := buildInspectReport(baselineSummary, baselinePaths, baselineTimeline, baselineSeries, baselineRobustStats, baselineChangePoints, baselinePeriodic, baselineSpectral, baselineNetworkLoops, baselineIntegralScores, baselineMarkov, baselineCausalGraph)
	candidate := buildInspectReport(candidateSummary, candidatePaths, candidateTimeline, candidateSeries, candidateRobustStats, candidateChangePoints, candidatePeriodic, candidateSpectral, candidateNetworkLoops, candidateIntegralScores, candidateMarkov, candidateCausalGraph)
	comparison := analyze.Compare(baselineSummary, candidateSummary)

	findings := compareFindings(comparison)
	return CompareMathReport{
		Title:             "база против кандидата",
		BaselinePaths:     append([]string(nil), baselinePaths...),
		CandidatePaths:    append([]string(nil), candidatePaths...),
		Baseline:          baseline,
		Candidate:         candidate,
		Comparison:        comparison,
		Findings:          findings,
		Sections:          compareSections(comparison, findings, baselineTimeline, candidateTimeline, robustDeltas, changeDeltas, baselinePeriodic, candidatePeriodic, networkLoopDeltas, integralDeltas, markovDeltas, causalDeltas),
		RobustDeltas:      robustDeltas,
		ChangeDeltas:      changeDeltas,
		NetworkLoopDeltas: networkLoopDeltas,
		IntegralDeltas:    integralDeltas,
		MarkovDeltas:      markovDeltas,
		CausalDeltas:      causalDeltas,
	}, nil
}

func buildInspectReport(summary analyze.Summary, paths []string, timeline []TimelineBucket, series []Series, robustStats []RobustStat, changePoints []ChangePoint, periodic []PeriodicSignal, spectral []SpectralPeak, networkLoops []NetworkLoopFinding, integralScores []IntegralScore, markov MarkovModel, causalGraph CausalGraph) MathReport {
	findings := dataQualityFindings(summary)
	return MathReport{
		Title:          titleFromPaths(paths),
		SourcePaths:    append([]string(nil), paths...),
		Summary:        summary,
		Findings:       findings,
		Timeline:       timeline,
		Series:         series,
		RobustStats:    robustStats,
		ChangePoints:   changePoints,
		Periodic:       periodic,
		Spectral:       spectral,
		NetworkLoops:   networkLoops,
		IntegralScores: integralScores,
		Markov:         markov,
		CausalGraph:    causalGraph,
		GraphPaths:     causalGraph.Paths,
		Sections:       inspectSections(summary, findings, timeline, series, robustStats, changePoints, periodic, networkLoops, integralScores, markov, causalGraph),
	}
}

func titleFromPaths(paths []string) string {
	if len(paths) == 0 {
		return "без исходных логов"
	}
	return strings.Join(paths, ", ")
}

func dataQualityFindings(summary analyze.Summary) []Finding {
	switch {
	case summary.EventCount == 0:
		return []Finding{{
			Severity:       "high",
			Title:          "Нет событий для математического анализа",
			Detail:         "Лог не содержит событий, поэтому отчет показывает только структуру будущих разделов.",
			Recommendation: "Проверьте, что runtime писал .jhlog во время сценария, и повторите команду inspect с непустым логом.",
		}}
	case summary.HTTPCount < 5 && summary.UIFrames < 300 && summary.ContextCount < 3:
		return []Finding{{
			Severity:       "medium",
			Title:          "Недостаточно данных для надежного анализа",
			Detail:         fmt.Sprintf("Собрано %d событий, HTTP=%d, UI-кадры=%d, сэмплы контекста=%d. Этого мало для устойчивых выводов.", summary.EventCount, summary.HTTPCount, summary.UIFrames, summary.ContextCount),
			Recommendation: "Соберите более длинный прогон или несколько повторов того же сценария.",
		}}
	default:
		return []Finding{{
			Severity: "ok",
			Title:    "Данных достаточно для каркаса математического отчета",
			Detail:   fmt.Sprintf("Собрано %d событий из %d логов. Подробные вычисления будут заполнять эти разделы по следующим этапам.", summary.EventCount, summary.LogCount),
		}}
	}
}

func compareFindings(comparison analyze.Comparison) []Finding {
	findings := make([]Finding, 0, len(comparison.Warnings)+1)
	for _, warning := range comparison.Warnings {
		findings = append(findings, Finding{
			Severity:       "medium",
			Title:          "Предупреждение о честности сравнения",
			Detail:         warning,
			Recommendation: "Проверьте, что база и кандидат собраны на сопоставимых устройствах, версиях и сетях.",
		})
	}
	if len(findings) == 0 {
		findings = append(findings, Finding{
			Severity: "ok",
			Title:    "Сравнение готово для математического слоя",
			Detail:   "База и кандидат агрегированы; последующие этапы добавят робастные интервалы, циклы, состояния и граф причинности.",
		})
	}
	return findings
}

func inspectSections(summary analyze.Summary, findings []Finding, timeline []TimelineBucket, series []Series, robustStats []RobustStat, changePoints []ChangePoint, periodic []PeriodicSignal, networkLoops []NetworkLoopFinding, integralScores []IntegralScore, markov MarkovModel, causalGraph CausalGraph) []MathSection {
	return []MathSection{
		{
			ID:       "quality",
			Title:    "Качество данных",
			Status:   sectionStatus(findings),
			Summary:  fmt.Sprintf("Логи=%d, события=%d, длительность=%d ms, HTTP=%d, UI-кадры=%d, сэмплы контекста=%d.", summary.LogCount, summary.EventCount, summary.DurationMS, summary.HTTPCount, summary.UIFrames, summary.ContextCount),
			Findings: findings,
		},
		{
			ID:       "timeline",
			Title:    "Таймлайн сигналов",
			Status:   timelineStatus(timeline),
			Summary:  timelineSummary(timeline, series),
			Findings: timelineFindings(timeline),
		},
		{
			ID:       "robust",
			Title:    "Робастная статистика",
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
			Title:    "Граф причинности",
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
			Summary:  fmt.Sprintf("Логи базы=%d, логи кандидата=%d, сравнительных метрик=%d.", comparison.Baseline.LogCount, comparison.Candidate.LogCount, len(comparison.Deltas)),
			Findings: findings,
		},
		{
			ID:       "timeline",
			Title:    "Таймлайн сигналов",
			Status:   compareTimelineStatus(baselineTimeline, candidateTimeline),
			Summary:  compareTimelineSummary(baselineTimeline, candidateTimeline),
			Findings: compareTimelineFindings(baselineTimeline, candidateTimeline),
		},
		{
			ID:       "robust",
			Title:    "Робастная статистика",
			Status:   compareRobustStatus(robustDeltas),
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
			Status:   compareNetworkLoopStatus(networkLoopDeltas),
			Summary:  compareNetworkLoopSummary(networkLoopDeltas),
			Findings: compareNetworkLoopFindings(networkLoopDeltas),
		},
		{
			ID:       "integral",
			Title:    "Интегральная нагрузка",
			Status:   compareIntegralStatus(integralDeltas),
			Summary:  compareIntegralSummary(integralDeltas),
			Findings: compareIntegralFindings(integralDeltas),
		},
		{
			ID:       "markov",
			Title:    "Марковская модель состояний",
			Status:   compareMarkovStatus(markovDeltas),
			Summary:  compareMarkovSummary(markovDeltas),
			Findings: compareMarkovFindings(markovDeltas),
		},
		{
			ID:       "graph",
			Title:    "Граф причинности",
			Status:   compareCausalGraphStatus(causalDeltas),
			Summary:  compareCausalGraphSummary(causalDeltas),
			Findings: compareCausalGraphFindings(causalDeltas),
		},
	}
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
