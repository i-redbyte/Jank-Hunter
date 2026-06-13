package mathanalysis

import (
	"fmt"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
)

type MathReport struct {
	Title        string
	SourcePaths  []string
	Summary      analyze.Summary
	Sections     []MathSection
	Findings     []Finding
	Timeline     []TimelineBucket
	Series       []Series
	RobustStats  []RobustStat
	ChangePoints []ChangePoint
	Spectral     []SpectralPeak
	NetworkLoops []NetworkLoopFinding
	GraphPaths   []GraphPath
}

type CompareMathReport struct {
	Title          string
	BaselinePaths  []string
	CandidatePaths []string
	Baseline       MathReport
	Candidate      MathReport
	Comparison     analyze.Comparison
	Sections       []MathSection
	Findings       []Finding
	RobustDeltas   []RobustDelta
	ChangeDeltas   []ChangePointDelta
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

type SpectralPeak struct {
	Signal      string
	PeriodMS    uint64
	FrequencyHz float64
	Power       float64
	Confidence  float64
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
	return buildInspectReport(summary, paths, timeline, series, robustStats, changePoints), nil
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
	baseline := buildInspectReport(baselineSummary, baselinePaths, baselineTimeline, baselineSeries, baselineRobustStats, baselineChangePoints)
	candidate := buildInspectReport(candidateSummary, candidatePaths, candidateTimeline, candidateSeries, candidateRobustStats, candidateChangePoints)
	comparison := analyze.Compare(baselineSummary, candidateSummary)

	findings := compareFindings(comparison)
	return CompareMathReport{
		Title:          "база против кандидата",
		BaselinePaths:  append([]string(nil), baselinePaths...),
		CandidatePaths: append([]string(nil), candidatePaths...),
		Baseline:       baseline,
		Candidate:      candidate,
		Comparison:     comparison,
		Findings:       findings,
		Sections:       compareSections(comparison, findings, baselineTimeline, candidateTimeline, robustDeltas, changeDeltas),
		RobustDeltas:   robustDeltas,
		ChangeDeltas:   changeDeltas,
	}, nil
}

func buildInspectReport(summary analyze.Summary, paths []string, timeline []TimelineBucket, series []Series, robustStats []RobustStat, changePoints []ChangePoint) MathReport {
	findings := dataQualityFindings(summary)
	return MathReport{
		Title:        titleFromPaths(paths),
		SourcePaths:  append([]string(nil), paths...),
		Summary:      summary,
		Findings:     findings,
		Timeline:     timeline,
		Series:       series,
		RobustStats:  robustStats,
		ChangePoints: changePoints,
		Sections:     inspectSections(summary, findings, timeline, series, robustStats, changePoints),
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
			Recommendation: "Проверь, что рантайм писал .jhlog во время сценария, и повтори команду inspect с непустым логом.",
		}}
	case summary.HTTPCount < 5 && summary.UIFrames < 300 && summary.ContextCount < 3:
		return []Finding{{
			Severity:       "medium",
			Title:          "Недостаточно данных для надежного анализа",
			Detail:         fmt.Sprintf("Собрано %d событий, HTTP=%d, UI-кадры=%d, сэмплы контекста=%d. Этого мало для устойчивых выводов.", summary.EventCount, summary.HTTPCount, summary.UIFrames, summary.ContextCount),
			Recommendation: "Собери более длинный прогон или несколько повторов того же сценария.",
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
			Recommendation: "Проверь, что база и кандидат собраны на сопоставимых устройствах, версиях и сетях.",
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

func inspectSections(summary analyze.Summary, findings []Finding, timeline []TimelineBucket, series []Series, robustStats []RobustStat, changePoints []ChangePoint) []MathSection {
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
			ID:      "periodic",
			Title:   "Периодические сигналы",
			Status:  "pending",
			Summary: "Раздел подготовлен для автокорреляции, FFT/DFT, спектральных пиков и предупреждений о периодичности.",
		},
		{
			ID:      "network-loops",
			Title:   "Сетевые циклы",
			Status:  "pending",
			Summary: "Детектор циклов DNS, connect, reconnect и всплесков запросов будет подключен после появления таймлайна и спектральных сигналов.",
		},
		{
			ID:      "markov",
			Title:   "Марковская модель состояний",
			Status:  "pending",
			Summary: "Раздел готов для состояний: здоровое, медленная сеть, jank, stall, давление памяти и восстановление.",
		},
		{
			ID:      "graph",
			Title:   "Граф причинности",
			Status:  "pending",
			Summary: "Здесь будут кратчайшие объясняющие пути от симптомов к screen, owner и route.",
		},
	}
}

func compareSections(comparison analyze.Comparison, findings []Finding, baselineTimeline, candidateTimeline []TimelineBucket, robustDeltas []RobustDelta, changeDeltas []ChangePointDelta) []MathSection {
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
			ID:      "periodic",
			Title:   "Периодические сигналы",
			Status:  "pending",
			Summary: "Раздел зарезервирован под дельту периодов, спектральную энтропию и отношение пика к фону.",
		},
		{
			ID:      "network-loops",
			Title:   "Сетевые циклы",
			Status:  "pending",
			Summary: "Здесь появятся признаки появления или исчезновения цикла, изменение периода, оценка сетевого выгорания и доверие.",
		},
		{
			ID:      "markov",
			Title:   "Марковская модель состояний",
			Status:  "pending",
			Summary: "Раздел готов для сравнения матрицы переходов и вероятности восстановления.",
		},
		{
			ID:      "graph",
			Title:   "Граф причинности",
			Status:  "pending",
			Summary: "Сюда будет выведена дельта причинных ребер и кратчайшие объясняющие пути.",
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
