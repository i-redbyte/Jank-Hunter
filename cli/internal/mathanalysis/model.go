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
}

type Series struct {
	Name     string
	Unit     string
	BucketMS uint64
	Points   []float64
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
	return buildInspectReport(summary, paths, timeline, series), nil
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

	baseline := buildInspectReport(baselineSummary, baselinePaths, baselineTimeline, baselineSeries)
	candidate := buildInspectReport(candidateSummary, candidatePaths, candidateTimeline, candidateSeries)
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
		Sections:       compareSections(comparison, findings, baselineTimeline, candidateTimeline),
	}, nil
}

func buildInspectReport(summary analyze.Summary, paths []string, timeline []TimelineBucket, series []Series) MathReport {
	findings := dataQualityFindings(summary)
	return MathReport{
		Title:       titleFromPaths(paths),
		SourcePaths: append([]string(nil), paths...),
		Summary:     summary,
		Findings:    findings,
		Timeline:    timeline,
		Series:      series,
		Sections:    inspectSections(summary, findings, timeline, series),
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

func inspectSections(summary analyze.Summary, findings []Finding, timeline []TimelineBucket, series []Series) []MathSection {
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
			ID:      "robust",
			Title:   "Робастная статистика",
			Status:  "pending",
			Summary: "Раздел зарезервирован под медиану, p90/p95/p99, MAD, усеченное среднее и интервалы доверия.",
		},
		{
			ID:      "change-points",
			Title:   "Точки изменения",
			Status:  "pending",
			Summary: "Здесь появятся сдвиги задержки, jank, базовой памяти и сетевых ошибок с ближайшими событиями.",
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

func compareSections(comparison analyze.Comparison, findings []Finding, baselineTimeline, candidateTimeline []TimelineBucket) []MathSection {
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
			ID:      "robust",
			Title:   "Робастная статистика",
			Status:  "pending",
			Summary: "Раздел подготовлен для размера эффекта, Cliff's delta и доверительных меток базы и кандидата.",
		},
		{
			ID:      "change-points",
			Title:   "Точки изменения",
			Status:  "pending",
			Summary: "Здесь появятся новые, исчезнувшие и усилившиеся точки изменения кандидата относительно базы.",
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
