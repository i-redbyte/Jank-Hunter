package report

import (
	"bytes"
	"fmt"
	"html/template"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/mathanalysis"
)

type LogReport struct {
	Name    string
	Anchor  string
	Summary analyze.Summary
}

func WriteInspect(path string, summary analyze.Summary) error {
	lang := reportLanguage()
	return execute(path, inspectTemplate, map[string]any{
		"GeneratedAt":         time.Now().Format(time.RFC3339),
		"Summary":             summary,
		"Analysis":            inspectAnalysis(summary, lang),
		"MathReportHref":      MathReportHref(path),
		"InfluenceReportHref": InfluenceReportHrefIfAvailable(path, summary.Influence),
	})
}

func WriteCompare(path string, comparison analyze.Comparison) error {
	return WriteCompareReport(path, comparison, nil, nil)
}

func WriteCompareReport(path string, comparison analyze.Comparison, baselineLogs, candidateLogs []LogReport) error {
	lang := reportLanguage()
	return execute(path, compareTemplate, map[string]any{
		"GeneratedAt":         time.Now().Format(time.RFC3339),
		"Comparison":          comparison,
		"BaselineLogs":        baselineLogs,
		"CandidateLogs":       candidateLogs,
		"Analysis":            compareAnalysis(comparison, lang),
		"MathReportHref":      MathReportHref(path),
		"InfluenceReportHref": InfluenceReportHrefIfAvailable(path, comparison.Candidate.Influence),
	})
}

func WriteMathInspect(path string, mathReport mathanalysis.MathReport) error {
	return execute(path, mathInspectTemplate, map[string]any{
		"GeneratedAt":         time.Now().Format(time.RFC3339),
		"Math":                mathReport,
		"MethodReferences":    mathanalysis.MethodReferences(),
		"InfluenceReportHref": InfluenceReportHrefIfAvailable(path, mathReport.Summary.Influence),
	})
}

func WriteMathCompare(path string, mathReport mathanalysis.CompareMathReport) error {
	return execute(path, mathCompareTemplate, map[string]any{
		"GeneratedAt":         time.Now().Format(time.RFC3339),
		"Math":                mathReport,
		"MethodReferences":    mathanalysis.MethodReferences(),
		"InfluenceReportHref": InfluenceReportHrefIfAvailable(path, mathReport.Comparison.Candidate.Influence),
	})
}

func WriteInfluence(path string, influence analyze.InfluenceSummary, title string) error {
	return execute(path, influenceTemplate, map[string]any{
		"GeneratedAt": time.Now().Format(time.RFC3339),
		"Title":       title,
		"Influence":   influence,
	})
}

func MathReportPath(path string) string {
	ext := filepath.Ext(path)
	if ext == "" {
		return path + "-math.html"
	}
	return strings.TrimSuffix(path, ext) + "-math" + ext
}

func MathReportHref(path string) string {
	return filepath.Base(MathReportPath(path))
}

func InfluenceReportPath(path string) string {
	path = strings.TrimSuffix(path, "-math"+filepath.Ext(path))
	ext := filepath.Ext(path)
	if ext == "" {
		return path + "-influence.html"
	}
	return strings.TrimSuffix(path, ext) + "-influence" + ext
}

func InfluenceReportHref(path string) string {
	return filepath.Base(InfluenceReportPath(path))
}

func InfluenceReportHrefIfAvailable(path string, influence analyze.InfluenceSummary) string {
	if !influence.Available {
		return ""
	}
	return InfluenceReportHref(path)
}

func execute(path, source string, data any) error {
	tmpl, err := template.New("report").Funcs(template.FuncMap{
		"pctWidth": func(value float64) template.CSS {
			return template.CSS(fmt.Sprintf("width:%.2f%%", clampPct(value)))
		},
		"msWidth": func(value uint64) template.CSS {
			width := float64(value) * 100 / 2000
			if width > 100 {
				width = 100
			}
			if width < 1 && value > 0 {
				width = 1
			}
			return template.CSS(fmt.Sprintf("width:%.2f%%", width))
		},
		"deltaWidth": func(value float64) template.CSS {
			width := math.Abs(value)
			if width > 100 {
				width = 100
			}
			if width < 1 && value != 0 {
				width = 1
			}
			return template.CSS(fmt.Sprintf("width:%.2f%%", width))
		},
		"scoreWidth": func(value float64) template.CSS {
			width := value * 12.5
			if width > 100 {
				width = 100
			}
			if width < 1 && value > 0 {
				width = 1
			}
			return template.CSS(fmt.Sprintf("width:%.2f%%", width))
		},
		"ringStyle": func(value float64) template.CSS {
			return template.CSS(fmt.Sprintf("--value:%.2f", clampPct(value)))
		},
		"rate": func(part int, total int) float64 {
			if total <= 0 {
				return 0
			}
			return float64(part) * 100 / float64(total)
		},
		"fpsScore": func(value float64) float64 {
			return clampPct(value * 100 / 60)
		},
		"severityClass": func(value string) string {
			switch value {
			case "high":
				return "sev-high"
			case "medium":
				return "sev-medium"
			default:
				return "sev-ok"
			}
		},
		"statusLabel": func(value string) string {
			switch value {
			case "high":
				return "критично"
			case "medium":
				return "предупреждение"
			case "ok":
				return "готово"
			case "pending":
				return "ожидает данных"
			default:
				return "каркас"
			}
		},
		"sparkline": func(series mathanalysis.Series) template.HTML {
			return sparklineSVG(series)
		},
		"seriesMax": func(series mathanalysis.Series) float64 {
			return seriesMax(series)
		},
		"seriesLast": func(series mathanalysis.Series) float64 {
			return seriesLast(series)
		},
		"bucketRange": func(bucket mathanalysis.TimelineBucket) string {
			return fmt.Sprintf("%.1f-%.1fs", float64(bucket.StartMS)/1000, float64(bucket.EndMS)/1000)
		},
		"humanDuration":          humanDuration,
		"dataSize":               humanDataSizeKB,
		"tip":                    tooltipHTML,
		"metricHelp":             metricHelp,
		"memoryHelp":             memoryMetricHelp,
		"integralHelp":           integralHelp,
		"scoreHelp":              scoreHelp,
		"scoreGuide":             scoreGuideHTML,
		"integralCriteria":       integralCriteria,
		"ownerKind":              ownerKindLabel,
		"problemKind":            problemKindLabel,
		"codeProblemSearchText":  codeProblemSearchText,
		"codeProblemLocation":    codeProblemLocation,
		"codeProblemMetric":      codeProblemMetric,
		"codeProblemCategories":  codeProblemCategoryStats,
		"codeProblemSeverities":  codeProblemSeverityStats,
		"codeProblemCompareRows": codeProblemCompareRows,
		"memoryLeakSearchText":   memoryLeakSearchText,
		"memoryLeakCompareRows":  memoryLeakCompareRows,
		"deltaGroups":            compareDeltaGroups,
		"deltaLabel":             compareDeltaLabel,
		"deltaHelp":              compareDeltaHelp,
		"deltaValue":             compareDeltaValue,
		"deltaChange":            compareDeltaChange,
		"deltaInterval":          compareDeltaInterval,
		"problemDeltas":          problemDeltas,
		"severityLabel":          severityLabel,
		"confidenceLabel": func(value string) string {
			return confidenceLabel(value)
		},
		"routeCompareRows":  routeCompareRows,
		"screenCompareRows": screenCompareRows,
		"ownerCompareRows":  ownerCompareRows,
		"flowCompareRows":   flowCompareRows,
		"flowKeyLabel":      flowKeyLabel,
		"summaryLogSpam":    summaryLogSpamTotal,
		"summaryProblems":   summaryProblemTotal,
		"signedMS":          signedMS,
		"signedDuration":    signedDuration,
		"signedFloat":       signedFloat,
		"bucketClass": func(bucket mathanalysis.TimelineBucket) string {
			if zeroTimelineBucket(bucket) {
				return "bucket-zero"
			}
			return ""
		},
		"robustGroups":                 robustStatGroups,
		"robustDeltaGroups":            robustDeltaGroups,
		"causalGraphSVG":               causalGraphSVG,
		"influenceGraphSVG":            influenceGraphSVG,
		"influenceStatus":              influenceStatusLabel,
		"influenceSeverity":            influenceSeverityLabel,
		"topInfluenceNodes":            topInfluenceNodes,
		"mathHeuristic":                inspectMathHeuristic,
		"compareMathHeuristic":         compareMathHeuristic,
		"significantMathFindings":      significantMathFindings,
		"significantReportFindings":    significantReportFindings,
		"significantMarkovStates":      significantMarkovStates,
		"hiddenMarkovStates":           hiddenMarkovStates,
		"significantMarkovTransitions": significantMarkovTransitions,
		"hiddenMarkovTransitions":      hiddenMarkovTransitions,
		"significantMarkovDeltas":      significantMarkovDeltas,
		"hiddenMarkovDeltas":           hiddenMarkovDeltas,
		"join": func(values []string, separator string) string {
			return strings.Join(values, separator)
		},
		"seconds": func(ms uint64) float64 {
			return float64(ms) / 1000
		},
		"jankPct": func(jankyFrames uint64, frames uint64) float64 {
			if frames == 0 {
				return 0
			}
			return float64(jankyFrames) * 100 / float64(frames)
		},
		"motifText": func(tokens []string) string {
			return mathanalysis.NetworkLoopMotifText(tokens)
		},
		"pathText": func(path mathanalysis.GraphPath) string {
			if len(path.Nodes) == 0 {
				return ""
			}
			return strings.Join(path.Nodes, " -> ")
		},
		"markovState": func(state string) string {
			return mathanalysis.MarkovStateLabel(state)
		},
		"causalKind": func(kind string) string {
			return mathanalysis.CausalKindLabel(kind)
		},
		"percent01": func(value float64) float64 {
			return value * 100
		},
		"notOK": func(value string) bool {
			return value != "" && value != "ok"
		},
		"fallback": func(value string, fallback string) string {
			if value == "" {
				return fallback
			}
			return value
		},
	}).Parse(source)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return err
	}
	html := buf.String()
	if reportLanguage() == "ru" {
		html = localizeRussianHTML(html)
	}
	return os.WriteFile(path, []byte(html), 0o644)
}

func clampPct(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}

func humanDuration(ms uint64) string {
	if ms == 0 {
		return "0 мс"
	}
	if ms < 1000 {
		return fmt.Sprintf("%d мс", ms)
	}
	totalSeconds := ms / 1000
	remMS := ms % 1000
	if totalSeconds < 60 {
		if remMS == 0 {
			return fmt.Sprintf("%d сек", totalSeconds)
		}
		return fmt.Sprintf("%.1f сек", float64(ms)/1000)
	}
	days := totalSeconds / 86400
	totalSeconds %= 86400
	hours := totalSeconds / 3600
	totalSeconds %= 3600
	minutes := totalSeconds / 60
	seconds := totalSeconds % 60
	var parts []string
	if days > 0 {
		parts = append(parts, fmt.Sprintf("%d д", days))
	}
	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%d ч", hours))
	}
	if minutes > 0 {
		parts = append(parts, fmt.Sprintf("%d мин", minutes))
	}
	if seconds > 0 || len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%d сек", seconds))
	}
	return strings.Join(parts, " ")
}

func humanDataSizeKB(kb uint64) string {
	switch {
	case kb >= 1024*1024:
		return fmt.Sprintf("%.1f GB", float64(kb)/1024/1024)
	case kb >= 1024:
		return fmt.Sprintf("%.1f MB", float64(kb)/1024)
	default:
		return fmt.Sprintf("%d KB", kb)
	}
}

func tooltipHTML(label, body string) template.HTML {
	escapedLabel := template.HTMLEscapeString(label)
	escapedBody := template.HTMLEscapeString(body)
	return template.HTML(fmt.Sprintf(`<span class="explain" tabindex="0" data-tip="%s">%s</span>`, escapedBody, escapedLabel))
}

func zeroTimelineBucket(bucket mathanalysis.TimelineBucket) bool {
	return bucket.HTTPCount == 0 &&
		bucket.HTTPFailed == 0 &&
		bucket.HTTPAvgDurationMS == 0 &&
		bucket.HTTPP95DurationMS == 0 &&
		bucket.DNSCount == 0 &&
		bucket.DNSDurationMS == 0 &&
		bucket.ConnectCount == 0 &&
		bucket.ConnectDurationMS == 0 &&
		bucket.TTFBMS == 0 &&
		bucket.UIFrames == 0 &&
		bucket.UIJankyFrames == 0 &&
		bucket.StallCount == 0 &&
		bucket.StallMaxMS == 0 &&
		bucket.MemoryPSSKB == 0 &&
		bucket.AvailableMemoryKB == 0 &&
		bucket.TrafficRxBytes == 0 &&
		bucket.TrafficTxBytes == 0
}

func significantMathFindings(findings []mathanalysis.Finding) []mathanalysis.Finding {
	out := make([]mathanalysis.Finding, 0, len(findings))
	for _, finding := range findings {
		if isSignificantSeverity(finding.Severity) {
			out = append(out, finding)
		}
	}
	return out
}

func significantReportFindings(findings []ReportFinding) []ReportFinding {
	out := make([]ReportFinding, 0, len(findings))
	for _, finding := range findings {
		if isSignificantSeverity(finding.Severity) {
			out = append(out, finding)
		}
	}
	return out
}

func significantMarkovStates(states []mathanalysis.MarkovBucketState) []mathanalysis.MarkovBucketState {
	out := make([]mathanalysis.MarkovBucketState, 0, len(states))
	for _, state := range states {
		if markovStateHasSignal(state) {
			out = append(out, state)
		}
	}
	return out
}

func hiddenMarkovStates(states []mathanalysis.MarkovBucketState) int {
	return len(states) - len(significantMarkovStates(states))
}

func significantMarkovTransitions(transitions []mathanalysis.MarkovTransition) []mathanalysis.MarkovTransition {
	out := make([]mathanalysis.MarkovTransition, 0, len(transitions))
	for _, transition := range transitions {
		if markovTransitionHasSignal(transition) {
			out = append(out, transition)
		}
	}
	return out
}

func hiddenMarkovTransitions(transitions []mathanalysis.MarkovTransition) int {
	return len(transitions) - len(significantMarkovTransitions(transitions))
}

func significantMarkovDeltas(deltas []mathanalysis.MarkovDelta) []mathanalysis.MarkovDelta {
	out := make([]mathanalysis.MarkovDelta, 0, len(deltas))
	for _, delta := range deltas {
		if isSignificantSeverity(delta.Severity) {
			out = append(out, delta)
		}
	}
	return out
}

func hiddenMarkovDeltas(deltas []mathanalysis.MarkovDelta) int {
	return len(deltas) - len(significantMarkovDeltas(deltas))
}

func isSignificantSeverity(severity string) bool {
	return severity == "high" || severity == "medium"
}

func markovStateHasSignal(state mathanalysis.MarkovBucketState) bool {
	switch state.State {
	case "Healthy":
		return false
	case "":
		return false
	default:
		return true
	}
}

func markovTransitionHasSignal(transition mathanalysis.MarkovTransition) bool {
	if transition.Count == 0 {
		return false
	}
	return transition.From != "Healthy" || transition.To != "Healthy"
}

type robustStatGroup struct {
	Title string
	Items []mathanalysis.RobustStat
}

func robustStatGroups(stats []mathanalysis.RobustStat) []robustStatGroup {
	order := []string{"Маршрут", "Экран", "Источник", "Gauge-метрика", "Счетчик", "Память", "Контекст"}
	return groupRobustStats(stats, order)
}

func groupRobustStats(stats []mathanalysis.RobustStat, order []string) []robustStatGroup {
	byDimension := map[string][]mathanalysis.RobustStat{}
	for _, stat := range stats {
		byDimension[stat.Dimension] = append(byDimension[stat.Dimension], stat)
	}
	var groups []robustStatGroup
	seen := map[string]struct{}{}
	for _, dimension := range order {
		items := byDimension[dimension]
		if len(items) == 0 {
			continue
		}
		seen[dimension] = struct{}{}
		groups = append(groups, robustStatGroup{Title: dimension, Items: items})
	}
	var rest []string
	for dimension := range byDimension {
		if _, ok := seen[dimension]; !ok {
			rest = append(rest, dimension)
		}
	}
	sort.Strings(rest)
	for _, dimension := range rest {
		groups = append(groups, robustStatGroup{Title: dimension, Items: byDimension[dimension]})
	}
	return groups
}

type robustDeltaGroup struct {
	Title string
	Items []mathanalysis.RobustDelta
}

func robustDeltaGroups(deltas []mathanalysis.RobustDelta) []robustDeltaGroup {
	byDimension := map[string][]mathanalysis.RobustDelta{}
	for _, delta := range deltas {
		byDimension[delta.Dimension] = append(byDimension[delta.Dimension], delta)
	}
	order := []string{"Маршрут", "Экран", "Источник", "Gauge-метрика", "Счетчик", "Память", "Контекст"}
	seen := map[string]struct{}{}
	var groups []robustDeltaGroup
	for _, dimension := range order {
		items := byDimension[dimension]
		if len(items) == 0 {
			continue
		}
		seen[dimension] = struct{}{}
		groups = append(groups, robustDeltaGroup{Title: dimension, Items: items})
	}
	var rest []string
	for dimension := range byDimension {
		if _, ok := seen[dimension]; !ok {
			rest = append(rest, dimension)
		}
	}
	sort.Strings(rest)
	for _, dimension := range rest {
		groups = append(groups, robustDeltaGroup{Title: dimension, Items: byDimension[dimension]})
	}
	return groups
}

func metricHelp(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.Contains(lower, "gc.bytes_allocated.delta"):
		return "Сколько байт было выделено за интервал или сценарий. 4092288 — примерно 4 МБ новых аллокаций: само по себе не всегда плохо, но при росте рядом с подтормаживаниями UI, GC или падением свободной RAM указывает на давление памяти."
	case strings.Contains(lower, "gc"):
		return "Метрика сборщика мусора или аллокаций. Смотрите не только абсолютное значение, но и совпадение с подтормаживаниями UI, паузами главного потока и ростом PSS."
	case strings.Contains(lower, "queue") || strings.Contains(lower, "executor"):
		return "Очередь или исполнитель задач. Рост значения означает накопление работы; если рядом падает FPS или растут паузы главного потока, очередь может быть причиной задержек."
	case strings.Contains(lower, "network") || strings.Contains(lower, "http") || strings.Contains(lower, "retry") || strings.Contains(lower, "connect"):
		return "Пользовательская сетевая метрика. Высокие значения стоит сопоставлять с HTTP p95, DNS/connect/TTFB и сетевыми циклами."
	case strings.Contains(lower, "jank") || strings.Contains(lower, "frame"):
		return "Метрика кадров или подтормаживаний. Чем выше значение рядом с пользовательским действием, тем выше риск видимой просадки интерфейса."
	default:
		return "Пользовательская метрика из приложения. Для счетчика важна сумма за сценарий, для gauge-метрики — уровень во времени. Интерпретируйте значение рядом с HTTP, UI, памятью и контекстом устройства."
	}
}

func memoryMetricHelp(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.Contains(lower, "pss"):
		return "PSS — proportional set size, пропорциональный размер памяти процесса. Он учитывает долю разделяемых страниц и лучше показывает вклад приложения в потребление RAM, чем одна куча объектов."
	case strings.Contains(lower, "java"):
		return "Куча Java — память объектов JVM/ART. Рост может приводить к более частому GC и паузам, особенно если одновременно растут аллокации."
	case strings.Contains(lower, "native"):
		return "Нативная куча — память нативных аллокаций. Рост может идти от bitmap, JNI, графики или библиотек и не всегда виден в куче Java."
	case strings.Contains(lower, "avail") || strings.Contains(lower, "free"):
		return "Свободная RAM показывает запас системы. Низкий запас усиливает давление памяти: GC, вытеснение кэшей и риск убийства процесса системой."
	default:
		return "Метрика памяти. Смотрите тренд вместе с PSS, кучей Java, нативной кучей, удержанными объектами и свободной RAM."
	}
}

func integralHelp(id string) string {
	switch id {
	case "network_failure_burn":
		return "Сетевое выгорание — накопленная стоимость повторяющихся сетевых проблем: HTTP-ошибок, DNS/connect всплесков и найденных сетевых циклов. Чем выше значение, тем дольше и дороже сеть мешала сценарию."
	case "memory_pressure_area":
		return "Площадь давления памяти объединяет рост PSS относительно базового уровня и длительность низкого запаса свободной RAM. Это не расшифровка PSS, а интегральная оценка риска GC, вытеснения кэшей и убийства процесса."
	case "recovery_debt":
		return "Долг восстановления растет, когда плохие временные окна идут подряд. Он показывает, как долго пользователь остается в деградировавшем состоянии."
	case "latency_pain_area":
		return "Накопленная сетевая задержка выше целевого порога. Учитывает не только пик, но и длительность медленного периода."
	case "jank_pressure_area":
		return "Накопленная доля подтормаживающих UI-кадров во времени. Длинная умеренная просадка может быть важнее короткого пика."
	default:
		return "Интегральная оценка: значение сигнала умножается на длительность временного интервала и суммируется по сценарию."
	}
}

func integralCriteria(id string) string {
	switch id {
	case "jank_pressure_area":
		return "Норма до 60 %*с, предупреждение от 60, критично от 180. Больше означает, что медленные кадры длились дольше или занимали большую долю времени."
	case "latency_pain_area":
		return "Норма до 500 мс*с, предупреждение от 500, критично от 2000. Считается только хвост HTTP p95 выше инженерного порога 300 мс."
	case "network_failure_burn":
		return "Норма до 5 усл.ед., предупреждение от 5, критично от 20. Больше означает повторяемые сетевые ошибки, DNS/connect всплески или цикл запросов."
	case "memory_pressure_area":
		return "Норма до 128 MB*с, предупреждение от 128, критично от 1024. Больше означает длительный рост PSS или долгий период низкой свободной RAM."
	case "recovery_debt":
		return "Норма до 8 с^2, предупреждение от 8, критично от 30. Больше означает, что плохие окна шли сериями и пользователь дольше оставался в деградации."
	default:
		return "Чем выше значение, тем выше накопленная нагрузка. Порог для этой оценки не задан."
	}
}

func scoreHelp(kind string) string {
	switch kind {
	case "change":
		return "Оценка точки изменения показывает, насколько сильный сдвиг сигнала виден на фоне локального шума. Примерно до 3 — слабый сигнал, 3–6 — заметный, выше 6 — сильный. Для задержек, памяти и подтормаживаний больше обычно хуже."
	case "influence":
		return "Оценка влияния — приоритет расследования внутри этого прогона. Она растет от HTTP p95, пауз главного потока, UI-подтормаживаний, памяти, спама логами, проблемных окон и связей флоу. 0–5 низкий риск, 5–15 средний, выше 15 высокий."
	case "network_burn":
		return "Выгорание — условная накопленная стоимость сетевого цикла: учитывает периодичность, повторяемость, ошибки, DNS/connect и длительность окна. До 5 обычно терпимо, 5–20 требует проверки, выше 20 критично для сценария."
	case "confidence":
		return "Доверие лежит в диапазоне 0..1. Чем ближе к 1, тем лучше сигнал подтвержден повторяемостью, количеством наблюдений или совпадением нескольких методов."
	case "path_cost":
		return "Стоимость пути в графе причинности: меньше значит более прямое и сильное объяснение. 1.00 — очень прямая связь, значения выше 2 обычно слабее и требуют подтверждения."
	case "integral":
		return "Интегральная оценка — площадь симптома по времени: значение сигнала умножается на длительность окна и суммируется. Она помогает отличить короткий пик от долгой деградации."
	default:
		return "Оценка — относительный приоритет внутри текущего отчета. Смотрите рядом критерии, доверие, размер выборки и связанный контекст."
	}
}

func scoreGuideHTML(kind string) template.HTML {
	switch kind {
	case "code":
		return template.HTML(`<div class="score-guide"><div class="score-guide-card"><strong>Шкала реестра кода</strong><span class="score-band sev-ok">0-5: низкий риск</span><span class="score-band sev-medium">5-15: предупреждение</span><span class="score-band sev-high">15+: критично</span><p>Оценка показывает приоритет расследования внутри текущего прогона. Она растет от совпадения сетевых хвостов, пауз главного потока, подтормаживаний UI, памяти, логов, флоу и графа влияния.</p></div></div>`)
	case "leak":
		return template.HTML(`<div class="score-guide"><div class="score-guide-card"><strong>Шкала утечек памяти</strong><span class="score-band sev-ok">до 7: наблюдать</span><span class="score-band sev-medium">7-16: проверить</span><span class="score-band sev-high">16+: критично</span><p>Риск дополнительно повышают возраст удержания от 15 секунд, повторяемость, удержанный размер от 4 МБ и связь с пользовательским держателем.</p></div></div>`)
	case "math":
		return template.HTML(`<div class="score-guide"><div class="score-guide-card"><strong>Шкала математических оценок</strong><span class="score-band sev-ok">0-3: слабый сигнал</span><span class="score-band sev-medium">3-6: заметный сигнал</span><span class="score-band sev-high">6+: сильный сигнал</span><p>Для задержек, памяти, подтормаживаний и сетевых циклов больше обычно хуже. Доверие 0..1 показывает, насколько вывод подтвержден данными.</p></div></div>`)
	case "compare":
		return template.HTML(`<div class="score-guide"><div class="score-guide-card"><strong>Шкала сравнения</strong><span class="score-band sev-ok">зеленый: без регрессии</span><span class="score-band sev-medium">желтый: нужна проверка</span><span class="score-band sev-high">красный: критично</span><p>Положительная дельта задержек, ошибок, памяти, подтормаживаний и спама обычно означает ухудшение кандидата. Смотрите дельту вместе с доверием и размером выборки.</p></div></div>`)
	default:
		return template.HTML(`<div class="score-guide"><div class="score-guide-card"><strong>Как читать оценку</strong><p>Оценка - это относительный приоритет внутри текущего отчета. Смотрите рядом критерии, доверие, размер выборки и контекст.</p></div></div>`)
	}
}

type registryStat struct {
	Name     string
	Count    int
	Score    float64
	Severity string
}

func codeProblemCategoryStats(items []analyze.CodeProblemStats) []registryStat {
	stats := map[string]*registryStat{}
	for _, item := range items {
		for _, category := range item.Categories {
			if category == "" {
				continue
			}
			stat := stats[category]
			if stat == nil {
				stat = &registryStat{Name: category, Severity: "ok"}
				stats[category] = stat
			}
			stat.Count++
			stat.Score += item.Score
			stat.Severity = maxSeverity(stat.Severity, item.Severity)
		}
	}
	return sortedRegistryStats(stats)
}

func codeProblemSeverityStats(items []analyze.CodeProblemStats) []registryStat {
	stats := map[string]*registryStat{
		"high":   {Name: "high", Severity: "high"},
		"medium": {Name: "medium", Severity: "medium"},
		"ok":     {Name: "ok", Severity: "ok"},
	}
	for _, item := range items {
		key := item.Severity
		if key == "" {
			key = "ok"
		}
		stat := stats[key]
		if stat == nil {
			stat = &registryStat{Name: key, Severity: key}
			stats[key] = stat
		}
		stat.Count++
		stat.Score += item.Score
	}
	ordered := []registryStat{}
	for _, key := range []string{"high", "medium", "ok"} {
		if stat := stats[key]; stat != nil && stat.Count > 0 {
			ordered = append(ordered, *stat)
		}
	}
	return ordered
}

func sortedRegistryStats(stats map[string]*registryStat) []registryStat {
	result := make([]registryStat, 0, len(stats))
	for _, stat := range stats {
		result = append(result, *stat)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Count != result[j].Count {
			return result[i].Count > result[j].Count
		}
		if result[i].Score != result[j].Score {
			return result[i].Score > result[j].Score
		}
		return result[i].Name < result[j].Name
	})
	return result
}

func maxSeverity(a, b string) string {
	rank := map[string]int{"ok": 1, "medium": 2, "high": 3}
	if rank[b] > rank[a] {
		return b
	}
	return a
}

func ownerKindLabel(kind string) string {
	switch kind {
	case "http":
		return "HTTP"
	case "main_thread_stall":
		return "пауза главного потока"
	case "retained_object":
		return "удержанный объект"
	default:
		return strings.ReplaceAll(kind, "_", " ")
	}
}

func problemKindLabel(kind string) string {
	switch kind {
	case "http_slow_or_failed":
		return "медленный или ошибочный HTTP"
	case "main_thread_stall":
		return "пауза главного потока"
	case "ui_jank":
		return "подтормаживания UI"
	case "wrapped_runnable":
		return "долгая Runnable-задача"
	case "wrapped_callable":
		return "долгая Callable-задача"
	case "wrapped_coroutine":
		return "долгая coroutine-задача"
	case "wrapped_executor":
		return "долгая executor-задача"
	case "wrapped_click":
		return "долгий click-handler"
	case "retained_object":
		return "удержанный объект"
	case "main_thread_dispatch":
		return "медленный dispatch главного потока"
	case "log_spam":
		return "спам логами"
	default:
		return strings.ReplaceAll(kind, "_", " ")
	}
}

func codeProblemLocation(row analyze.CodeProblemStats) string {
	if row.Method == "" {
		return row.ClassName
	}
	return row.ClassName + "." + row.Method
}

func codeProblemSearchText(row analyze.CodeProblemStats) string {
	parts := []string{
		row.ClassName,
		row.Method,
		row.Owner,
		row.Severity,
		row.Impact,
		row.Recommendation,
		row.Evidence,
	}
	parts = append(parts, row.Categories...)
	parts = append(parts, row.Problems...)
	parts = append(parts, row.Screens...)
	parts = append(parts, row.Flows...)
	parts = append(parts, row.Steps...)
	parts = append(parts, row.Routes...)
	for _, signal := range row.Signals {
		parts = append(parts, signal.Name, signal.Category, signal.Detail)
	}
	return strings.ToLower(strings.Join(parts, " "))
}

func codeProblemMetric(signal analyze.CodeProblemSignal) string {
	parts := []string{}
	if signal.Count > 0 {
		parts = append(parts, fmt.Sprintf("кол-во %d", signal.Count))
	}
	if signal.TotalMS > 0 {
		parts = append(parts, fmt.Sprintf("итого %s", humanDuration(signal.TotalMS)))
	}
	if signal.MaxMS > 0 {
		parts = append(parts, fmt.Sprintf("макс. %d мс", signal.MaxMS))
	}
	if signal.Value > 0 {
		unit := signal.Unit
		if unit == "" {
			unit = "значение"
		}
		parts = append(parts, fmt.Sprintf("%d %s", signal.Value, unit))
	}
	if len(parts) == 0 {
		return "сигнал"
	}
	return strings.Join(parts, " · ")
}

func memoryLeakSearchText(item analyze.MemoryLeakSuspect) string {
	return strings.ToLower(strings.Join([]string{
		item.ClassName,
		item.Holder,
		item.Screen,
		item.Flow,
		item.Step,
		item.ObjectKind,
		item.HolderQuality,
		item.HeapSource,
		item.GCRoot,
		item.HolderField,
		item.LeakChainConfidence,
		item.LeakChainSummary,
		strings.Join(item.LeakChainActions, " "),
		item.Impact,
		item.Recommendation,
		item.Evidence,
	}, " "))
}

type memoryLeakCompareRow struct {
	Candidate     analyze.MemoryLeakSuspect
	BaselineScore float64
	BaselineCount uint64
	BaselineAgeMS uint64
	DeltaScore    float64
	DeltaCount    int64
	DeltaAgeMS    int64
	Status        string
	Severity      string
}

func memoryLeakCompareRows(comparison analyze.Comparison) []memoryLeakCompareRow {
	baselineByKey := map[string]analyze.MemoryLeakSuspect{}
	for _, row := range comparison.Baseline.MemoryLeaks {
		baselineByKey[memoryLeakCompareKey(row)] = row
	}
	out := make([]memoryLeakCompareRow, 0, len(comparison.Candidate.MemoryLeaks))
	for _, row := range comparison.Candidate.MemoryLeaks {
		before, found := baselineByKey[memoryLeakCompareKey(row)]
		delta := row.Score
		if found {
			delta = row.Score - before.Score
		}
		status := "без сильного изменения"
		severity := row.Severity
		switch {
		case !found:
			status = "новое удержание кандидата"
			if severity == "ok" {
				severity = "medium"
			}
		case delta >= 8 || row.Count >= before.Count+5 || row.MaxAgeMS >= before.MaxAgeMS+30_000:
			status = "утечка усилилась"
			severity = "high"
		case delta >= 3 || row.Count > before.Count || row.MaxAgeMS > before.MaxAgeMS:
			status = "подозрение выросло"
			if severity == "ok" {
				severity = "medium"
			}
		case delta <= -3 || row.Count < before.Count || row.MaxAgeMS < before.MaxAgeMS:
			status = "стало легче"
			severity = "ok"
		}
		out = append(out, memoryLeakCompareRow{
			Candidate:     row,
			BaselineScore: before.Score,
			BaselineCount: before.Count,
			BaselineAgeMS: before.MaxAgeMS,
			DeltaScore:    math.Round(delta*10) / 10,
			DeltaCount:    int64(row.Count) - int64(before.Count),
			DeltaAgeMS:    int64(row.MaxAgeMS) - int64(before.MaxAgeMS),
			Status:        status,
			Severity:      severity,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Severity == out[j].Severity {
			if out[i].DeltaScore == out[j].DeltaScore {
				return out[i].Candidate.Score > out[j].Candidate.Score
			}
			return out[i].DeltaScore > out[j].DeltaScore
		}
		return reportSeverityRank(out[i].Severity) > reportSeverityRank(out[j].Severity)
	})
	if len(out) > 80 {
		out = out[:80]
	}
	return out
}

func memoryLeakCompareKey(row analyze.MemoryLeakSuspect) string {
	return strings.Join([]string{
		strings.ToLower(row.ClassName),
		strings.ToLower(row.Holder),
		strings.ToLower(row.Screen),
		strings.ToLower(row.Flow),
		strings.ToLower(row.Step),
	}, "\x00")
}

type codeProblemCompareRow struct {
	Candidate     analyze.CodeProblemStats
	BaselineScore float64
	DeltaScore    float64
	Status        string
	Severity      string
}

func codeProblemCompareRows(comparison analyze.Comparison) []codeProblemCompareRow {
	baseline := comparison.Baseline.CodeProblems
	if len(baseline) == 0 {
		baseline = analyze.BuildCodeProblemRegistry(comparison.Baseline)
	}
	candidate := comparison.Candidate.CodeProblems
	if len(candidate) == 0 {
		candidate = analyze.BuildCodeProblemRegistry(comparison.Candidate)
	}
	baselineByLocation := map[string]analyze.CodeProblemStats{}
	for _, row := range baseline {
		baselineByLocation[codeProblemLocation(row)] = row
	}
	out := make([]codeProblemCompareRow, 0, len(candidate))
	for _, row := range candidate {
		before, found := baselineByLocation[codeProblemLocation(row)]
		delta := row.Score
		if found {
			delta = row.Score - before.Score
		}
		status := "без сильного изменения"
		severity := row.Severity
		switch {
		case !found:
			status = "новая проблема кандидата"
			if severity == "ok" {
				severity = "medium"
			}
		case delta >= 8:
			status = "сильное усиление"
			severity = "high"
		case delta >= 3:
			status = "усиление"
			if severity == "ok" {
				severity = "medium"
			}
		case delta <= -3:
			status = "стало легче"
			severity = "ok"
		}
		out = append(out, codeProblemCompareRow{
			Candidate:     row,
			BaselineScore: before.Score,
			DeltaScore:    math.Round(delta*10) / 10,
			Status:        status,
			Severity:      severity,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Severity == out[j].Severity {
			if out[i].DeltaScore == out[j].DeltaScore {
				return out[i].Candidate.Score > out[j].Candidate.Score
			}
			return out[i].DeltaScore > out[j].DeltaScore
		}
		return reportSeverityRank(out[i].Severity) > reportSeverityRank(out[j].Severity)
	})
	if len(out) > 120 {
		out = out[:120]
	}
	return out
}

func reportSeverityRank(value string) int {
	switch value {
	case "high":
		return 3
	case "medium":
		return 2
	default:
		return 1
	}
}

type compareDeltaGroup struct {
	Title  string
	Detail string
	Items  []analyze.Delta
}

func compareDeltaGroups(deltas []analyze.Delta) []compareDeltaGroup {
	order := []string{"network", "ui", "memory", "context", "other"}
	titles := map[string]string{
		"network": "Сеть и трафик",
		"ui":      "UI и главный поток",
		"memory":  "Память и удержания",
		"context": "Контекст и когорты",
		"other":   "Остальные сигналы",
	}
	details := map[string]string{
		"network": "HTTP-задержки, ошибки и UID-трафик.",
		"ui":      "Плавность интерфейса, FPS и максимальные паузы главного потока.",
		"memory":  "PSS, свободная RAM и удержанные объекты.",
		"context": "Версия приложения, SDK, устройство, процесс, сеть и рут-доступ.",
		"other":   "Сигналы без отдельной категории.",
	}
	byCategory := map[string][]analyze.Delta{}
	for _, delta := range deltas {
		category := compareDeltaCategory(delta.Name)
		byCategory[category] = append(byCategory[category], delta)
	}
	var groups []compareDeltaGroup
	for _, category := range order {
		items := byCategory[category]
		if len(items) == 0 {
			continue
		}
		groups = append(groups, compareDeltaGroup{
			Title:  titles[category],
			Detail: details[category],
			Items:  items,
		})
	}
	return groups
}

func problemDeltas(deltas []analyze.Delta) []analyze.Delta {
	var out []analyze.Delta
	for _, delta := range deltas {
		if delta.Severity != "" && delta.Severity != "ok" {
			out = append(out, delta)
		}
	}
	return out
}

func compareDeltaCategory(name string) string {
	switch name {
	case "HTTP p95", "HTTP failures", "UID RX max", "UID TX max", "Network mix":
		return "network"
	case "UI jank rate", "UI avg FPS", "Main-thread stall max", "Log spam", "Problem windows":
		return "ui"
	case "Max PSS", "Min available memory", "Retained objects":
		return "memory"
	case "Process mix", "App version mix", "SDK mix", "Device mix", "Cohort mix":
		return "context"
	default:
		return "other"
	}
}

func compareDeltaLabel(name string) string {
	switch name {
	case "HTTP p95":
		return "HTTP p95-задержка"
	case "HTTP failures":
		return "HTTP-ошибки"
	case "UI jank rate":
		return "Доля подтормаживаний UI"
	case "UI avg FPS":
		return "Средний FPS UI"
	case "Main-thread stall max":
		return "Макс. пауза главного потока"
	case "Max PSS":
		return "Макс. PSS"
	case "Min available memory":
		return "Мин. свободная память"
	case "UID RX max":
		return "Макс. входящий трафик приложения"
	case "UID TX max":
		return "Макс. исходящий трафик приложения"
	case "Retained objects":
		return "Удержанные объекты"
	case "Log spam":
		return "Спам логами"
	case "Problem windows":
		return "Проблемные окна"
	case "Process mix":
		return "Состав процессов"
	case "App version mix":
		return "Состав версий приложения"
	case "SDK mix":
		return "Состав SDK"
	case "Device mix":
		return "Состав устройств"
	case "Network mix":
		return "Состав сети"
	case "Cohort mix":
		return "Состав когорт"
	default:
		return strings.ReplaceAll(name, "_", " ")
	}
}

func compareDeltaHelp(name string) string {
	switch name {
	case "HTTP p95":
		return "95-й процентиль длительности HTTP-запросов. Рост обычно означает, что хвост сетевых задержек стал хуже."
	case "HTTP failures":
		return "Количество HTTP-вызовов с ошибкой или статусом 5xx. Рост почти всегда ухудшает пользовательский сценарий."
	case "UI jank rate":
		return "Доля медленных UI-кадров. Рост в процентных пунктах показывает, что интерфейс стал чаще дергаться."
	case "UI avg FPS":
		return "Средняя частота кадров. Для FPS ухудшением считается падение значения."
	case "Main-thread stall max":
		return "Самая длинная зафиксированная пауза главного потока. Даже один большой пик может быть причиной риска АНР."
	case "Max PSS":
		return "Максимальный PSS процесса. Рост показывает больший вклад приложения в потребление RAM."
	case "Min available memory":
		return "Минимум свободной RAM. Здесь ухудшением считается падение, потому что запас памяти стал меньше."
	case "UID RX max", "UID TX max":
		return "Максимальный трафик UID приложения. Рост сам по себе не всегда плох, но важен рядом с сетевой задержкой и ошибками."
	case "Retained objects":
		return "Количество удержанных объектов. Рост может указывать на утечки или слишком долгие ссылки."
	case "Log spam":
		return "Суммарное количество вызовов android.util.Log.* и Timber.*. Рост может давить на главный поток, I/O и засорять диагностику."
	case "Problem windows":
		return "Агрегированные окна, где Jank Hunter уже увидел причину: медленный HTTP, паузу главного потока, UI-подтормаживания, удержания или спам логами."
	case "Process mix", "App version mix", "SDK mix", "Device mix", "Network mix", "Cohort mix":
		return "Проверка честности сравнения: база и кандидат должны быть собраны в сопоставимых условиях."
	default:
		return "Сравнительная метрика: смотрите направление изменения, доверие и размер выборки."
	}
}

func compareDeltaValue(value string) string {
	replacer := strings.NewReplacer(
		" ms", " мс",
		" count", " шт",
		" bytes", " байт",
		" kb", " KB",
		" fps", " FPS",
		" pp", " п.п.",
		"same", "без изменений",
		"changed", "изменилось",
		"+new", "появилось",
	)
	return replacer.Replace(value)
}

func compareDeltaChange(value string) string {
	return compareDeltaValue(value)
}

func compareDeltaInterval(value string) string {
	if value == "" {
		return "нет интервала"
	}
	replacer := strings.NewReplacer(
		"approx", "примерно",
		" ms", " мс",
		" count", " шт",
		" bytes", " байт",
		" kb", " KB",
		" fps", " FPS",
		" pp", " п.п.",
	)
	return replacer.Replace(value)
}

func severityLabel(value string) string {
	switch value {
	case "high":
		return "критично"
	case "medium":
		return "предупреждение"
	case "ok":
		return "норма"
	default:
		return "норма"
	}
}

func confidenceLabel(value string) string {
	switch value {
	case "high":
		return "высокое"
	case "medium":
		return "среднее"
	case "low":
		return "низкое"
	default:
		return "неизвестно"
	}
}

type routeCompareRow struct {
	Route             string
	BaselineCount     int
	CandidateCount    int
	BaselineFailures  int
	CandidateFailures int
	BaselineP95MS     uint64
	CandidateP95MS    uint64
	DeltaP95MS        int64
	BaselineOwner     string
	CandidateOwner    string
	Severity          string
}

func routeCompareRows(baseline, candidate analyze.Summary) []routeCompareRow {
	base := map[string]analyze.RouteStats{}
	cand := map[string]analyze.RouteStats{}
	names := map[string]struct{}{}
	for _, route := range baseline.Routes {
		base[route.Route] = route
		names[route.Route] = struct{}{}
	}
	for _, route := range candidate.Routes {
		cand[route.Route] = route
		names[route.Route] = struct{}{}
	}
	rows := make([]routeCompareRow, 0, len(names))
	for name := range names {
		b := base[name]
		c := cand[name]
		delta := int64(c.P95MS) - int64(b.P95MS)
		rows = append(rows, routeCompareRow{
			Route:             name,
			BaselineCount:     b.Count,
			CandidateCount:    c.Count,
			BaselineFailures:  b.Failures,
			CandidateFailures: c.Failures,
			BaselineP95MS:     b.P95MS,
			CandidateP95MS:    c.P95MS,
			DeltaP95MS:        delta,
			BaselineOwner:     b.OwnerSample,
			CandidateOwner:    c.OwnerSample,
			Severity:          latencyDeltaSeverity(b.P95MS, c.P95MS),
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		if severityRank(rows[i].Severity) != severityRank(rows[j].Severity) {
			return severityRank(rows[i].Severity) > severityRank(rows[j].Severity)
		}
		if absInt64(rows[i].DeltaP95MS) != absInt64(rows[j].DeltaP95MS) {
			return absInt64(rows[i].DeltaP95MS) > absInt64(rows[j].DeltaP95MS)
		}
		return rows[i].Route < rows[j].Route
	})
	return rows
}

type screenCompareRow struct {
	Screen           string
	BaselineFrames   uint64
	CandidateFrames  uint64
	BaselineJankPct  float64
	CandidateJankPct float64
	DeltaJankPct     float64
	BaselineAvgFPS   float64
	CandidateAvgFPS  float64
	DeltaFPS         float64
	BaselineP95MS    uint64
	CandidateP95MS   uint64
	Severity         string
}

func screenCompareRows(baseline, candidate analyze.Summary) []screenCompareRow {
	base := map[string]analyze.ScreenStats{}
	cand := map[string]analyze.ScreenStats{}
	names := map[string]struct{}{}
	for _, screen := range baseline.Screens {
		base[screen.Screen] = screen
		names[screen.Screen] = struct{}{}
	}
	for _, screen := range candidate.Screens {
		cand[screen.Screen] = screen
		names[screen.Screen] = struct{}{}
	}
	rows := make([]screenCompareRow, 0, len(names))
	for name := range names {
		b := base[name]
		c := cand[name]
		deltaJank := c.JankRatePct - b.JankRatePct
		deltaFPS := c.AvgFPS - b.AvgFPS
		rows = append(rows, screenCompareRow{
			Screen:           name,
			BaselineFrames:   b.Frames,
			CandidateFrames:  c.Frames,
			BaselineJankPct:  b.JankRatePct,
			CandidateJankPct: c.JankRatePct,
			DeltaJankPct:     deltaJank,
			BaselineAvgFPS:   b.AvgFPS,
			CandidateAvgFPS:  c.AvgFPS,
			DeltaFPS:         deltaFPS,
			BaselineP95MS:    b.P95MS,
			CandidateP95MS:   c.P95MS,
			Severity:         screenDeltaSeverity(deltaJank, deltaFPS),
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		if severityRank(rows[i].Severity) != severityRank(rows[j].Severity) {
			return severityRank(rows[i].Severity) > severityRank(rows[j].Severity)
		}
		if math.Abs(rows[i].DeltaJankPct) != math.Abs(rows[j].DeltaJankPct) {
			return math.Abs(rows[i].DeltaJankPct) > math.Abs(rows[j].DeltaJankPct)
		}
		return rows[i].Screen < rows[j].Screen
	})
	return rows
}

type ownerCompareRow struct {
	Owner            string
	Kind             string
	BaselineCount    int
	CandidateCount   int
	BaselineMaxMS    uint64
	CandidateMaxMS   uint64
	DeltaMaxMS       int64
	BaselineTotalMS  uint64
	CandidateTotalMS uint64
	Severity         string
}

func ownerCompareRows(baseline, candidate analyze.Summary) []ownerCompareRow {
	base := map[string]analyze.OwnerStats{}
	cand := map[string]analyze.OwnerStats{}
	names := map[string]struct{}{}
	for _, owner := range baseline.Owners {
		base[owner.Owner] = owner
		names[owner.Owner] = struct{}{}
	}
	for _, owner := range candidate.Owners {
		cand[owner.Owner] = owner
		names[owner.Owner] = struct{}{}
	}
	rows := make([]ownerCompareRow, 0, len(names))
	for name := range names {
		b := base[name]
		c := cand[name]
		kind := firstNonEmpty(c.Kind, b.Kind)
		delta := int64(c.MaxMS) - int64(b.MaxMS)
		rows = append(rows, ownerCompareRow{
			Owner:            name,
			Kind:             kind,
			BaselineCount:    b.Count,
			CandidateCount:   c.Count,
			BaselineMaxMS:    b.MaxMS,
			CandidateMaxMS:   c.MaxMS,
			DeltaMaxMS:       delta,
			BaselineTotalMS:  b.TotalMS,
			CandidateTotalMS: c.TotalMS,
			Severity:         latencyDeltaSeverity(b.MaxMS, c.MaxMS),
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		if severityRank(rows[i].Severity) != severityRank(rows[j].Severity) {
			return severityRank(rows[i].Severity) > severityRank(rows[j].Severity)
		}
		if absInt64(rows[i].DeltaMaxMS) != absInt64(rows[j].DeltaMaxMS) {
			return absInt64(rows[i].DeltaMaxMS) > absInt64(rows[j].DeltaMaxMS)
		}
		return rows[i].Owner < rows[j].Owner
	})
	return rows
}

type flowCompareRow struct {
	Screen              string
	Flow                string
	Step                string
	Owner               string
	BaselineProblems    uint64
	CandidateProblems   uint64
	DeltaProblems       int64
	BaselineLogSpam     uint64
	CandidateLogSpam    uint64
	DeltaLogSpam        int64
	BaselineHTTPP95MS   uint64
	CandidateHTTPP95MS  uint64
	DeltaHTTPP95MS      int64
	BaselineStallMaxMS  uint64
	CandidateStallMaxMS uint64
	DeltaStallMaxMS     int64
	BaselineJankPct     float64
	CandidateJankPct    float64
	DeltaJankPct        float64
	Severity            string
}

func flowCompareRows(baseline, candidate analyze.Summary) []flowCompareRow {
	base := map[string]analyze.FlowStats{}
	cand := map[string]analyze.FlowStats{}
	keys := map[string]struct{}{}
	for _, flow := range baseline.Flows {
		key := flowStatsKey(flow)
		base[key] = flow
		keys[key] = struct{}{}
	}
	for _, flow := range candidate.Flows {
		key := flowStatsKey(flow)
		cand[key] = flow
		keys[key] = struct{}{}
	}
	rows := make([]flowCompareRow, 0, len(keys))
	for key := range keys {
		b := base[key]
		c := cand[key]
		problemDelta := int64(c.ProblemCount) - int64(b.ProblemCount)
		logDelta := int64(c.LogSpam) - int64(b.LogSpam)
		httpDelta := int64(c.HTTPP95MS) - int64(b.HTTPP95MS)
		stallDelta := int64(c.StallMaxMS) - int64(b.StallMaxMS)
		jankDelta := c.UIJankPct - b.UIJankPct
		rows = append(rows, flowCompareRow{
			Screen:              firstNonEmpty(c.Screen, b.Screen),
			Flow:                firstNonEmpty(c.Flow, b.Flow),
			Step:                firstNonEmpty(c.Step, b.Step),
			Owner:               firstNonEmpty(c.Owner, b.Owner),
			BaselineProblems:    b.ProblemCount,
			CandidateProblems:   c.ProblemCount,
			DeltaProblems:       problemDelta,
			BaselineLogSpam:     b.LogSpam,
			CandidateLogSpam:    c.LogSpam,
			DeltaLogSpam:        logDelta,
			BaselineHTTPP95MS:   b.HTTPP95MS,
			CandidateHTTPP95MS:  c.HTTPP95MS,
			DeltaHTTPP95MS:      httpDelta,
			BaselineStallMaxMS:  b.StallMaxMS,
			CandidateStallMaxMS: c.StallMaxMS,
			DeltaStallMaxMS:     stallDelta,
			BaselineJankPct:     b.UIJankPct,
			CandidateJankPct:    c.UIJankPct,
			DeltaJankPct:        jankDelta,
			Severity:            flowDeltaSeverity(problemDelta, logDelta, httpDelta, stallDelta, jankDelta),
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		if severityRank(rows[i].Severity) != severityRank(rows[j].Severity) {
			return severityRank(rows[i].Severity) > severityRank(rows[j].Severity)
		}
		left := absInt64(rows[i].DeltaProblems)*10_000 + absInt64(rows[i].DeltaLogSpam)*10 + absInt64(rows[i].DeltaStallMaxMS) + absInt64(rows[i].DeltaHTTPP95MS)
		right := absInt64(rows[j].DeltaProblems)*10_000 + absInt64(rows[j].DeltaLogSpam)*10 + absInt64(rows[j].DeltaStallMaxMS) + absInt64(rows[j].DeltaHTTPP95MS)
		if left != right {
			return left > right
		}
		return flowKeyLabel(rows[i].Screen, rows[i].Flow, rows[i].Step, rows[i].Owner) < flowKeyLabel(rows[j].Screen, rows[j].Flow, rows[j].Step, rows[j].Owner)
	})
	return rows
}

func flowStatsKey(flow analyze.FlowStats) string {
	return strings.Join([]string{flow.Screen, flow.Flow, flow.Step, flow.Owner}, "\x00")
}

func flowKeyLabel(screen, flow, step, owner string) string {
	parts := []string{
		firstNonEmpty(screen, "unknown"),
		firstNonEmpty(flow, "unknown"),
		firstNonEmpty(step, "unknown"),
		firstNonEmpty(owner, "unknown"),
	}
	return strings.Join(parts, " / ")
}

func flowDeltaSeverity(problemDelta, logDelta, httpDelta, stallDelta int64, jankDelta float64) string {
	if problemDelta >= 10 || stallDelta >= 500 || httpDelta >= 500 || jankDelta >= 3 {
		return "high"
	}
	if problemDelta > 0 || logDelta >= 50 || stallDelta >= 100 || httpDelta >= 100 || jankDelta >= 1 {
		return "medium"
	}
	return "ok"
}

func summaryLogSpamTotal(summary analyze.Summary) uint64 {
	var total uint64
	for _, item := range summary.LogSpam {
		total += item.Count
	}
	return total
}

func summaryProblemTotal(summary analyze.Summary) uint64 {
	var total uint64
	for _, item := range summary.ProblemWindows {
		total += item.Count
	}
	return total
}

func latencyDeltaSeverity(baseline, candidate uint64) string {
	if baseline == 0 && candidate > 0 {
		if candidate >= 1000 {
			return "high"
		}
		return "medium"
	}
	if candidate <= baseline {
		return "ok"
	}
	delta := candidate - baseline
	pct := 0.0
	if baseline > 0 {
		pct = float64(delta) * 100 / float64(baseline)
	}
	if delta >= 500 || pct >= 50 {
		return "high"
	}
	if delta >= 100 || pct >= 15 {
		return "medium"
	}
	return "ok"
}

func screenDeltaSeverity(deltaJankPct, deltaFPS float64) string {
	if deltaJankPct >= 3 || deltaFPS <= -5 {
		return "high"
	}
	if deltaJankPct >= 1 || deltaFPS <= -2 {
		return "medium"
	}
	return "ok"
}

func signedMS(value int64) string {
	if value == 0 {
		return "0 мс"
	}
	return fmt.Sprintf("%+d мс", value)
}

func signedDuration(value int64) string {
	if value == 0 {
		return "0 мс"
	}
	sign := "+"
	if value < 0 {
		sign = "-"
		value = -value
	}
	return sign + humanDuration(uint64(value))
}

func signedFloat(value float64, unit string) string {
	if value == 0 {
		return "0 " + unit
	}
	return fmt.Sprintf("%+.2f %s", value, unit)
}

func absInt64(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}

type heuristicCard struct {
	Severity string
	Title    string
	Detail   string
}

type heuristicSummary struct {
	Severity string
	Status   string
	Summary  string
	Cards    []heuristicCard
}

func inspectMathHeuristic(report mathanalysis.MathReport) heuristicSummary {
	summary := heuristicSummary{Severity: "ok", Status: "Математический профиль спокоен", Summary: "Критичных математических сигналов не найдено."}
	for _, section := range report.Sections {
		if severityRank(section.Status) > severityRank(summary.Severity) {
			summary.Severity = section.Status
		}
	}
	switch summary.Severity {
	case "high":
		summary.Status = "Требуется разбор"
		summary.Summary = "Есть сильные математические сигналы деградации. Начните с карточек ниже и проверьте связанные маршруты, источники и контекст."
	case "medium":
		summary.Status = "Есть сигналы для проверки"
		summary.Summary = "Обнаружены предупреждения. Их стоит подтвердить повторным прогоном и связать с конкретными владельцами работ."
	}
	if len(report.NetworkLoops) > 0 {
		loop := report.NetworkLoops[0]
		target := firstNonEmpty(loop.Route, loop.Owner, "сетевой сценарий")
		summary.Cards = append(summary.Cards, heuristicCard{Severity: networkLoopCardSeverity(loop.Confidence, loop.BurnScore), Title: "Сетевой цикл", Detail: fmt.Sprintf("Проверьте %s: период %.1f сек, доверие %.2f, оценка выгорания %.1f.", target, float64(loop.PeriodMS)/1000, loop.Confidence, loop.BurnScore)})
	}
	if len(report.CausalGraph.OwnerScores) > 0 {
		owner := report.CausalGraph.OwnerScores[0]
		summary.Cards = append(summary.Cards, heuristicCard{Severity: "medium", Title: "Главный источник", Detail: fmt.Sprintf("Наибольший вклад у %s: оценка %.2f. Используйте это как приоритет для просмотра карты источников и трассировок.", owner.Owner, owner.Score)})
	}
	if flow, ok := topProblemFlow(report.Summary); ok {
		summary.Cards = append(summary.Cards, heuristicCard{Severity: flowCardSeverity(flow), Title: "Флоу с причинами", Detail: fmt.Sprintf("%s: проблем %d, спам логами %d, HTTP p95 %d мс, макс. пауза %d мс.", flowKeyLabel(flow.Screen, flow.Flow, flow.Step, flow.Owner), flow.ProblemCount, flow.LogSpam, flow.HTTPP95MS, flow.StallMaxMS)})
	}
	if score, ok := topIntegralScore(report.IntegralScores); ok {
		summary.Cards = append(summary.Cards, heuristicCard{Severity: score.Severity, Title: score.Title, Detail: fmt.Sprintf("%.1f %s. %s", score.Value, score.Unit, score.Explanation)})
	}
	if len(summary.Cards) == 0 {
		summary.Cards = append(summary.Cards, heuristicCard{Severity: "ok", Title: "Что проверить первым", Detail: "Используйте отчет как базу: сравните следующий прогон, а при появлении предупреждений начните с сети, UI, памяти и графа причинности."})
	}
	return summary
}

func compareMathHeuristic(report mathanalysis.CompareMathReport) heuristicSummary {
	summary := heuristicSummary{Severity: "ok", Status: "Сравнение выглядит стабильным", Summary: "Сильных математических ухудшений между базой и кандидатом не найдено."}
	for _, section := range report.Sections {
		if severityRank(section.Status) > severityRank(summary.Severity) {
			summary.Severity = section.Status
		}
	}
	switch summary.Severity {
	case "high":
		summary.Status = "Кандидат требует расследования"
		summary.Summary = "Есть сильные математические дельты. Проверьте, совпадают ли они с изменениями маршрутов, экранов, памяти или контекста устройства."
	case "medium":
		summary.Status = "Есть предупреждения по кандидату"
		summary.Summary = "Найдены умеренные отличия. Подтвердите их повторным прогоном перед инженерным выводом."
	}
	if len(report.RobustDeltas) > 0 {
		for _, delta := range report.RobustDeltas {
			if delta.Severity == "high" || delta.Severity == "medium" {
				summary.Cards = append(summary.Cards, heuristicCard{Severity: delta.Severity, Title: "Распределение изменилось", Detail: fmt.Sprintf("%s / %s: p95 изменился на %+.1f %s (%+.1f%%), доверие %s.", delta.Dimension, delta.Metric, delta.P95Delta, delta.Unit, delta.P95DeltaPct, delta.Confidence)})
				break
			}
		}
	}
	if len(report.NetworkLoopDeltas) > 0 {
		delta := report.NetworkLoopDeltas[0]
		target := firstNonEmpty(delta.Route, delta.Owner, "сетевой цикл")
		summary.Cards = append(summary.Cards, heuristicCard{Severity: delta.Severity, Title: "Изменение сетевого цикла", Detail: fmt.Sprintf("%s: изменение выгорания %+.1f, изменение доверия %+.2f.", target, delta.BurnDelta, delta.ConfidenceDelta)})
	}
	if len(report.CausalDeltas) > 0 {
		delta := report.CausalDeltas[0]
		summary.Cards = append(summary.Cards, heuristicCard{Severity: delta.Severity, Title: "Граф причинности изменился", Detail: delta.Summary})
	}
	if row, ok := topFlowDelta(report.Comparison.Baseline, report.Comparison.Candidate); ok && row.Severity != "ok" {
		summary.Cards = append(summary.Cards, heuristicCard{Severity: row.Severity, Title: "Флоу ухудшился", Detail: fmt.Sprintf("%s: Δ проблем %d, Δ спама %d, Δ HTTP p95 %d мс, Δ UI %+.2f п.п.", flowKeyLabel(row.Screen, row.Flow, row.Step, row.Owner), row.DeltaProblems, row.DeltaLogSpam, row.DeltaHTTPP95MS, row.DeltaJankPct)})
	}
	if len(summary.Cards) == 0 {
		summary.Cards = append(summary.Cards, heuristicCard{Severity: "ok", Title: "Что проверить первым", Detail: "Сохраните сравнение как контрольную точку. При следующей регрессии начните с разделов робастных дельт, сетевых циклов и графа причинности."})
	}
	return summary
}

func topProblemFlow(summary analyze.Summary) (analyze.FlowStats, bool) {
	if len(summary.Flows) == 0 {
		return analyze.FlowStats{}, false
	}
	best := summary.Flows[0]
	for _, flow := range summary.Flows[1:] {
		if flowProblemScore(flow) > flowProblemScore(best) {
			best = flow
		}
	}
	if flowProblemScore(best) == 0 {
		return analyze.FlowStats{}, false
	}
	return best, true
}

func flowProblemScore(flow analyze.FlowStats) uint64 {
	return flow.ProblemCount*10_000 + flow.LogSpam*10 + uint64(flow.StallCount)*1_000 + flow.StallMaxMS + flow.HTTPP95MS + uint64(flow.UIJank)
}

func flowCardSeverity(flow analyze.FlowStats) string {
	if flow.ProblemCount >= 10 || flow.StallMaxMS >= 1000 || flow.HTTPP95MS >= 1500 {
		return "high"
	}
	if flow.ProblemCount > 0 || flow.LogSpam >= 50 || flow.StallMaxMS >= 250 || flow.HTTPP95MS >= 500 {
		return "medium"
	}
	return "ok"
}

func topFlowDelta(baseline, candidate analyze.Summary) (flowCompareRow, bool) {
	rows := flowCompareRows(baseline, candidate)
	if len(rows) == 0 {
		return flowCompareRow{}, false
	}
	return rows[0], true
}

func networkLoopCardSeverity(confidence, burn float64) string {
	if confidence >= 0.70 && burn >= 8 {
		return "high"
	}
	if confidence >= 0.45 || burn >= 4 {
		return "medium"
	}
	return "ok"
}

func topIntegralScore(scores []mathanalysis.IntegralScore) (mathanalysis.IntegralScore, bool) {
	if len(scores) == 0 {
		return mathanalysis.IntegralScore{}, false
	}
	best := scores[0]
	for _, score := range scores[1:] {
		if severityRank(score.Severity) > severityRank(best.Severity) || (score.Severity == best.Severity && score.Value > best.Value) {
			best = score
		}
	}
	return best, true
}

func sparklineSVG(series mathanalysis.Series) template.HTML {
	const (
		width  = 360.0
		height = 86.0
		pad    = 5.0
	)
	if len(series.Points) == 0 {
		return template.HTML(`<svg class="sparkline" viewBox="0 0 360 86" role="img" aria-label="нет данных"></svg>`)
	}
	maxValue := seriesMax(series)
	minValue := series.Points[0]
	for _, point := range series.Points {
		if point < minValue {
			minValue = point
		}
	}
	if maxValue == minValue {
		minValue = 0
	}
	if maxValue == minValue {
		maxValue = minValue + 1
	}
	scaleY := func(value float64) float64 {
		return height - pad - ((value - minValue) * (height - 2*pad) / (maxValue - minValue))
	}
	step := width - 2*pad
	if len(series.Points) > 1 {
		step = (width - 2*pad) / float64(len(series.Points)-1)
	}
	barWidth := step * 0.62
	if barWidth < 1.2 {
		barWidth = 1.2
	}
	if barWidth > 11 {
		barWidth = 11
	}

	var bars strings.Builder
	var line strings.Builder
	for i, point := range series.Points {
		x := pad
		if len(series.Points) > 1 {
			x += float64(i) * step
		}
		y := scaleY(point)
		barHeight := height - pad - y
		if barHeight < 1 && point > 0 {
			barHeight = 1
		}
		if point > 0 {
			fmt.Fprintf(&bars, `<rect x="%.2f" y="%.2f" width="%.2f" height="%.2f" rx="1.4"></rect>`, x-barWidth/2, height-pad-barHeight, barWidth, barHeight)
		}
		if i > 0 {
			line.WriteByte(' ')
		}
		fmt.Fprintf(&line, "%.2f,%.2f", x, y)
	}

	var out strings.Builder
	fmt.Fprintf(&out, `<svg class="sparkline" viewBox="0 0 %.0f %.0f" role="img" aria-label="%s">`, width, height, template.HTMLEscapeString(series.Name))
	out.WriteString(`<line class="spark-axis" x1="5" y1="81" x2="355" y2="81"></line>`)
	out.WriteString(`<g class="spark-bars">`)
	out.WriteString(bars.String())
	out.WriteString(`</g><polyline class="spark-line" points="`)
	out.WriteString(line.String())
	out.WriteString(`"></polyline></svg>`)
	return template.HTML(out.String())
}

func causalGraphSVG(graph mathanalysis.CausalGraph) template.HTML {
	if len(graph.Nodes) == 0 || len(graph.Edges) == 0 {
		return template.HTML(`<div class="muted">Недостаточно узлов и связей для визуального графа.</div>`)
	}
	const (
		maxEdges = 48
		maxNodes = 30
		width    = 960.0
		height   = 460.0
	)
	edges := append([]mathanalysis.CausalEdge(nil), graph.Edges...)
	if len(edges) > maxEdges {
		edges = edges[:maxEdges]
	}
	nodeByID := map[string]mathanalysis.CausalNode{}
	for _, node := range graph.Nodes {
		nodeByID[node.ID] = node
	}
	used := map[string]struct{}{}
	for _, edge := range edges {
		used[edge.From] = struct{}{}
		used[edge.To] = struct{}{}
		if len(used) >= maxNodes {
			break
		}
	}
	var nodes []mathanalysis.CausalNode
	for id := range used {
		if node, ok := nodeByID[id]; ok {
			nodes = append(nodes, node)
		}
	}
	sort.Slice(nodes, func(i, j int) bool {
		if graphKindColumn(nodes[i].Kind) != graphKindColumn(nodes[j].Kind) {
			return graphKindColumn(nodes[i].Kind) < graphKindColumn(nodes[j].Kind)
		}
		return nodes[i].Label < nodes[j].Label
	})
	position := layoutCausalNodes(nodes, width, height)
	var out strings.Builder
	out.WriteString(`<div class="causal-graph-card">`)
	out.WriteString(`<svg class="causal-graph" viewBox="0 0 960 460" role="img" aria-label="Обзор графа причинности">`)
	out.WriteString(`<defs><marker id="arrow" markerWidth="8" markerHeight="8" refX="7" refY="3.5" orient="auto"><path d="M0,0 L8,3.5 L0,7 Z" fill="rgba(111,247,255,0.58)"></path></marker></defs>`)
	for _, edge := range edges {
		from, okFrom := position[edge.From]
		to, okTo := position[edge.To]
		if !okFrom || !okTo {
			continue
		}
		opacity := 0.28 + clampPct(edge.Confidence*100)/140
		fmt.Fprintf(&out, `<line class="causal-edge" x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" opacity="%.2f" marker-end="url(#arrow)"><title>%s → %s · %s · доверие %.2f</title></line>`,
			from.x+112, from.y+24, to.x, to.y+24, opacity,
			template.HTMLEscapeString(edge.FromLabel),
			template.HTMLEscapeString(edge.ToLabel),
			template.HTMLEscapeString(mathanalysis.CausalKindLabel(edge.Kind)),
			edge.Confidence,
		)
	}
	for _, node := range nodes {
		pos := position[node.ID]
		label := truncateRunes(node.Label, 28)
		kind := mathanalysis.CausalKindLabel(node.Kind)
		fmt.Fprintf(&out, `<g class="causal-node" transform="translate(%.1f %.1f)"><title>%s · %s</title><rect width="124" height="48"></rect><text x="10" y="19">%s</text><text class="kind" x="10" y="36">%s</text></g>`,
			pos.x, pos.y,
			template.HTMLEscapeString(node.Label),
			template.HTMLEscapeString(kind),
			template.HTMLEscapeString(label),
			template.HTMLEscapeString(truncateRunes(kind, 22)),
		)
	}
	out.WriteString(`</svg>`)
	if len(graph.Edges) > len(edges) || len(graph.Nodes) > len(nodes) {
		fmt.Fprintf(&out, `<div class="help-text">Показаны самые сильные связи: %d из %d ребер и %d из %d узлов. Полная детализация находится в таблицах ниже.</div>`, len(edges), len(graph.Edges), len(nodes), len(graph.Nodes))
	}
	out.WriteString(`</div>`)
	return template.HTML(out.String())
}

func influenceGraphSVG(influence analyze.InfluenceSummary) template.HTML {
	if !influence.Available || len(influence.TopNodes) == 0 {
		return template.HTML(`<div class="muted">Недостаточно данных для графа влияния кода.</div>`)
	}
	const (
		maxNodes = 30
		maxEdges = 72
		width    = 1040.0
		nodeW    = 214.0
		nodeH    = 74.0
		topY     = 72.0
		gapY     = 22.0
	)
	nodeByName := map[string]analyze.InfluenceNode{}
	for _, node := range influence.TopNodes {
		if node.ClassName != "" {
			nodeByName[node.ClassName] = node
		}
	}

	visible := map[string]struct{}{}
	var order []string
	addVisible := func(name string) bool {
		if name == "" {
			return true
		}
		if _, ok := visible[name]; ok {
			return true
		}
		if len(order) >= maxNodes {
			return false
		}
		visible[name] = struct{}{}
		order = append(order, name)
		return true
	}

	var edges []analyze.InfluenceEdge
	for _, edge := range influence.TopEdges {
		if len(edges) >= maxEdges {
			break
		}
		if edge.From == "" || edge.To == "" || edge.From == edge.To {
			continue
		}
		_, fromVisible := visible[edge.From]
		_, toVisible := visible[edge.To]
		needed := 0
		if !fromVisible {
			needed++
		}
		if !toVisible {
			needed++
		}
		if len(order)+needed > maxNodes {
			if !fromVisible || !toVisible {
				continue
			}
		}
		addVisible(edge.From)
		addVisible(edge.To)
		edges = append(edges, edge)
	}
	for _, node := range influence.TopNodes {
		if len(order) >= maxNodes {
			break
		}
		addVisible(node.ClassName)
	}
	if len(order) == 0 {
		return template.HTML(`<div class="muted">Недостаточно узлов для визуального графа влияния.</div>`)
	}

	inDegree := map[string]int{}
	outDegree := map[string]int{}
	for _, edge := range edges {
		outDegree[edge.From]++
		inDegree[edge.To]++
	}

	type visibleNode struct {
		node   analyze.InfluenceNode
		column int
		degree int
	}
	nodes := make([]visibleNode, 0, len(order))
	for index, name := range order {
		node, ok := nodeByName[name]
		if !ok {
			node = analyze.InfluenceNode{
				ClassName: name,
				Label:     influenceGraphLabel(name),
				Severity:  "ok",
				Status:    "static_only",
				Reasons:   []string{"есть связь в графе"},
			}
		}
		if node.Label == "" {
			node.Label = influenceGraphLabel(name)
		}
		if node.Severity == "" {
			node.Severity = "ok"
		}
		column := 1
		switch {
		case len(edges) == 0:
			column = index % 3
		case outDegree[name] > 0 && inDegree[name] == 0:
			column = 0
		case outDegree[name] > 0 && inDegree[name] > 0:
			column = 1
		default:
			column = 2
		}
		nodes = append(nodes, visibleNode{
			node:   node,
			column: column,
			degree: inDegree[name] + outDegree[name],
		})
	}
	sort.SliceStable(nodes, func(i, j int) bool {
		if nodes[i].column != nodes[j].column {
			return nodes[i].column < nodes[j].column
		}
		if nodes[i].node.RuntimeEvidence != nodes[j].node.RuntimeEvidence {
			return nodes[i].node.RuntimeEvidence
		}
		if nodes[i].node.Score != nodes[j].node.Score {
			return nodes[i].node.Score > nodes[j].node.Score
		}
		if nodes[i].degree != nodes[j].degree {
			return nodes[i].degree > nodes[j].degree
		}
		return nodes[i].node.ClassName < nodes[j].node.ClassName
	})

	columnX := []float64{52, 413, 774}
	columnCounts := [3]int{}
	for _, node := range nodes {
		if node.column >= 0 && node.column < len(columnCounts) {
			columnCounts[node.column]++
		}
	}
	maxRows := 1
	for _, count := range columnCounts {
		if count > maxRows {
			maxRows = count
		}
	}
	height := math.Max(520, topY+float64(maxRows)*(nodeH+gapY)+34)
	columnRow := [3]int{}
	positions := map[string]graphPoint{}
	for _, item := range nodes {
		row := columnRow[item.column]
		columnRow[item.column]++
		positions[item.node.ClassName] = graphPoint{
			x: columnX[item.column],
			y: topY + float64(row)*(nodeH+gapY),
		}
	}

	var out strings.Builder
	out.WriteString(`<div class="influence-graph-card">`)
	out.WriteString(`<div class="influence-tools" role="toolbar" aria-label="Режим выделения графа"><button type="button" data-influence-mode="node">Вершина</button><button type="button" class="is-active" data-influence-mode="paths">Пути</button><button type="button" data-influence-mode="tree">Остов</button><button type="button" data-influence-reset>Сброс</button></div>`)
	out.WriteString(`<div class="influence-selection" data-influence-selection>Наведите мышью на вершину или сфокусируйте ее клавиатурой, чтобы подсветить все исходящие пути от нее.</div>`)
	fmt.Fprintf(&out, `<svg class="influence-graph" viewBox="0 0 %.0f %.0f" role="img" aria-label="Граф влияния кода">`, width, height)
	out.WriteString(`<defs><marker id="influence-arrow" markerWidth="10" markerHeight="10" refX="9" refY="5" orient="auto"><path d="M0,0 L10,5 L0,10 Z" fill="rgba(111,247,255,0.72)"></path></marker></defs>`)
	out.WriteString(`<text class="influence-layer-label" x="52" y="34">Источники вызовов</text>`)
	out.WriteString(`<text class="influence-layer-label" x="413" y="34">Связующие классы</text>`)
	out.WriteString(`<text class="influence-layer-label" x="774" y="34">Проблемные узлы</text>`)
	for _, edge := range edges {
		from, okFrom := positions[edge.From]
		to, okTo := positions[edge.To]
		if !okFrom || !okTo {
			continue
		}
		opacity := 0.22 + math.Min(edge.Influence/80, 0.62)
		strokeWidth := 1.4 + math.Min(edge.Influence/45, 4.6)
		className := "influence-edge"
		if edge.RuntimeConfirmed {
			className += " confirmed"
		}
		x1 := from.x + nodeW
		y1 := from.y + nodeH/2
		x2 := to.x
		y2 := to.y + nodeH/2
		direction := 1.0
		if x2 < x1 {
			direction = -1
			x1 = from.x
			x2 = to.x + nodeW
		}
		curve := math.Max(70, math.Abs(x2-x1)*0.42)
		path := fmt.Sprintf("M%.1f %.1f C%.1f %.1f %.1f %.1f %.1f %.1f", x1, y1, x1+direction*curve, y1, x2-direction*curve, y2, x2, y2)
		fmt.Fprintf(&out, `<path class="%s" data-from="%s" data-to="%s" d="%s" opacity="%.2f" stroke-width="%.2f" marker-end="url(#influence-arrow)"><title>%s → %s · вес %.1f · вызовов %d · %s</title></path>`,
			className,
			template.HTMLEscapeString(edge.From),
			template.HTMLEscapeString(edge.To),
			path,
			opacity,
			strokeWidth,
			template.HTMLEscapeString(edge.From),
			template.HTMLEscapeString(edge.To),
			edge.Influence,
			edge.Count,
			template.HTMLEscapeString(edge.Reason),
		)
	}
	for _, item := range nodes {
		node := item.node
		pos := positions[node.ClassName]
		scoreRadius := 15 + math.Min(node.Score*0.55, 9)
		className := "influence-node " + node.Severity
		if !node.RuntimeEvidence {
			className += " static-only"
		}
		reasons := strings.Join(node.Reasons, ", ")
		if reasons == "" {
			reasons = influenceStatusLabel(node.Status)
		}
		fmt.Fprintf(&out, `<g class="%s" data-node="%s" tabindex="0" role="button" aria-label="%s, оценка %.1f" transform="translate(%.1f %.1f)"><title>%s · оценка %.1f · %s</title><rect class="node-card" width="%.0f" height="%.0f"></rect><circle cx="28" cy="28" r="%.1f"></circle><text class="node-score-text" x="28" y="31">%s</text><text class="node-label" x="55" y="23">%s</text><text class="node-kind" x="55" y="42">%s</text><text class="node-reason" x="55" y="59">%s</text></g>`,
			className,
			template.HTMLEscapeString(node.ClassName),
			template.HTMLEscapeString(node.ClassName),
			node.Score,
			pos.x,
			pos.y,
			template.HTMLEscapeString(node.ClassName),
			node.Score,
			template.HTMLEscapeString(reasons),
			nodeW,
			nodeH,
			scoreRadius,
			template.HTMLEscapeString(influenceGraphScoreLabel(node.Score)),
			template.HTMLEscapeString(truncateRunes(node.Label, 24)),
			template.HTMLEscapeString(influenceGraphNodeKind(node)),
			template.HTMLEscapeString(truncateRunes(reasons, 34)),
		)
	}
	out.WriteString(`</svg>`)
	if len(edges) == 0 {
		out.WriteString(`<div class="help-text">В этом прогоне видны проблемные узлы, но между выбранными классами нет подтвержденных связей. Передайте статический ` + "`--class-graph`" + ` или включите ` + "`runtimeCallGraph`" + `, чтобы получить ребра.</div>`)
	}
	if influence.ShownNodes > len(nodes) || influence.ShownEdges > len(edges) {
		fmt.Fprintf(&out, `<div class="help-text">Показаны ключевые узлы и связи: %d из %d узлов, %d из %d ребер. Полная детализация находится в таблицах ниже.</div>`, len(nodes), influence.ShownNodes, len(edges), influence.ShownEdges)
	}
	out.WriteString(`</div>`)
	return template.HTML(out.String())
}

func influenceGraphLabel(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "unknown"
	}
	parts := strings.Split(name, ".")
	if len(parts) >= 2 {
		return parts[len(parts)-2] + "." + parts[len(parts)-1]
	}
	return name
}

func influenceGraphScoreLabel(score float64) string {
	if score <= 0 {
		return "0"
	}
	if score >= 100 {
		return fmt.Sprintf("%.0f", score)
	}
	return fmt.Sprintf("%.1f", score)
}

func influenceGraphNodeKind(node analyze.InfluenceNode) string {
	if node.RuntimeEvidence {
		return influenceSeverityLabel(node.Severity) + " · выполнение"
	}
	return "статическая связь"
}

func influenceStatusLabel(value string) string {
	switch value {
	case "runtime":
		return "есть доказательства выполнения"
	case "static_only":
		return "статическая связь без проявления"
	default:
		return "нет данных"
	}
}

func influenceSeverityLabel(value string) string {
	switch value {
	case "high":
		return "высокий риск"
	case "medium":
		return "средний риск"
	default:
		return "низкий риск"
	}
}

func topInfluenceNodes(influence analyze.InfluenceSummary, limit int) []analyze.InfluenceNode {
	if limit <= 0 || len(influence.TopNodes) <= limit {
		return influence.TopNodes
	}
	return influence.TopNodes[:limit]
}

type graphPoint struct {
	x float64
	y float64
}

func layoutCausalNodes(nodes []mathanalysis.CausalNode, width, height float64) map[string]graphPoint {
	byColumn := map[int][]mathanalysis.CausalNode{}
	for _, node := range nodes {
		col := graphKindColumn(node.Kind)
		byColumn[col] = append(byColumn[col], node)
	}
	xs := []float64{32, 196, 360, 524, 688, 804}
	positions := map[string]graphPoint{}
	for col, items := range byColumn {
		x := xs[len(xs)-1]
		if col >= 0 && col < len(xs) {
			x = xs[col]
		}
		step := (height - 84) / float64(len(items)+1)
		for i, node := range items {
			positions[node.ID] = graphPoint{x: x, y: 28 + step*float64(i+1)}
		}
	}
	return positions
}

func graphKindColumn(kind string) int {
	switch kind {
	case "state", "symptom":
		return 0
	case "network", "phase":
		return 1
	case "loop":
		return 2
	case "route":
		return 3
	case "owner":
		return 4
	case "screen":
		return 5
	default:
		return 2
	}
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	if limit <= 1 {
		return string(runes[:limit])
	}
	return string(runes[:limit-1]) + "…"
}

func seriesMax(series mathanalysis.Series) float64 {
	var maxValue float64
	for _, point := range series.Points {
		if point > maxValue {
			maxValue = point
		}
	}
	return maxValue
}

func seriesLast(series mathanalysis.Series) float64 {
	if len(series.Points) == 0 {
		return 0
	}
	return series.Points[len(series.Points)-1]
}

func reportLanguage() string {
	return "ru"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func localizeRussianHTML(html string) string {
	replacer := strings.NewReplacer(
		`<html lang="en">`, `<html lang="ru">`,
		`<title>Jank Hunter Inspect</title>`, `<title>Jank Hunter: отчет</title>`,
		`<title>Jank Hunter Compare</title>`, `<title>Jank Hunter: сравнение</title>`,
		`Runtime Signal Report`, `Отчет по сигналам выполнения`,
		`Regression Control Deck`, `Панель контроля регрессий`,
		`Candidate Device Context`, `Контекст сравнения`,
		`Device Context`, `Контекст устройства`,
		`runtime context unavailable`, `контекст выполнения недоступен`,
		`unknown device`, `неизвестное устройство`,
		`No session/context metadata.`, `Нет метаданных сессии и контекста.`,
		`generated `, `создан `,
		`standalone offline HTML`, `автономный HTML`,
		`compare first, then drill into every baseline and candidate log`, `сначала сравнение, затем детальный просмотр логов базы и кандидата`,
		`>Logs <strong>`, `>Логи <strong>`,
		`>Events <strong>`, `>События <strong>`,
		`>Duration <strong>`, `>Длительность <strong>`,
		`>Baseline logs <strong>`, `>Логи базы <strong>`,
		`>Candidate logs <strong>`, `>Логи кандидата <strong>`,
		`>Deltas <strong>`, `>Дельты <strong>`,
		`>Overview<`, `>Обзор<`,
		`>Network<`, `>Сеть<`,
		`>Owners<`, `>Источники<`,
		`>Memory<`, `>Память<`,
		`>Metrics<`, `>Метрики<`,
		`>Context<`, `>Контекст<`,
		`>Verdict<`, `>Итог<`,
		`>Comparison<`, `>Сравнение<`,
		`>Regressions<`, `>Регрессии<`,
		`>Candidate Detail<`, `>Где изменилось<`,
		`>Per-log Drill-down<`, `>Детали логов<`,
		`>Cohorts<`, `>Когорты<`,
		`Executive Signal Matrix`, `Матрица ключевых сигналов`,
		`Fast read of the run: latency, smoothness, stalls, memory and traffic.`, `Быстрый срез прогона: задержки, плавность, паузы главного потока, память и трафик.`,
		`offline report`, `автономный отчет`,
		`Comparative Scoreboard`, `Сводная панель сравнения`,
		`Baseline vs candidate across latency, smoothness, memory, traffic, retention and cohort mix.`, `База и кандидат по задержкам, плавности, памяти, трафику, удержанным объектам и составу когорт.`,
		`standalone HTML`, `автономный HTML`,
		`Regression Matrix`, `Матрица регрессий`,
		`Severity is adjusted for confidence and sample size. Bars show regression magnitude capped at 100%.`, `Серьезность учитывает доверие и размер выборки. Полосы показывают величину регрессии с ограничением в 100%.`,
		`Worst Regression Cards`, `Худшие регрессии`,
		`Candidate Deep Summary`, `Где изменилось`,
		`The aggregate candidate profile after all filters.`, `Агрегированный профиль кандидата после всех фильтров.`,
		`Per-log Drill-down`, `Детали по каждому логу`,
		`Open any source log to inspect its own network, UI, memory, metrics and attribution profile.`, `Откройте любой исходный лог, чтобы увидеть его сеть, UI, память, метрики и профиль влияния.`,
		`Baseline Logs`, `Логи базы`,
		`Candidate Logs`, `Логи кандидата`,
		`Cohort Breakdown`, `Разбивка по когортам`,
		`Use this to check whether the comparison is fair across app version, SDK, device, process and network.`, `Используйте это, чтобы проверить честность сравнения по версии приложения, SDK, устройству, процессу и сети.`,
		`Process Mix`, `Состав процессов`,
		`Network Routes`, `Сетевые маршруты`,
		`Slowest routes by p95 latency, failures, bytes and owner attribution.`, `Самые медленные маршруты по p95-задержке, ошибкам, байтам и влиянию источников.`,
		`Route Table`, `Таблица маршрутов`,
		`UI Smoothness`, `Плавность UI`,
		`Screens ranked by jank rate and frame latency.`, `Экраны, отсортированные по доле подтормаживаний и задержке кадров.`,
		`Screen Table`, `Таблица экранов`,
		`Attribution Hotspots`, `Горячие точки влияния`,
		`Owners, classes and stack hints with the largest measured impact.`, `Источники, классы и подсказки стека с наибольшим измеренным вкладом.`,
		`Memory And Retention`, `Память и удержанные объекты`,
		`PSS, available memory, low-memory samples and retained object age buckets.`, `PSS, свободная память, сигналы низкой памяти и возраст удержанных объектов.`,
		`Custom Metrics`, `Пользовательские метрики`,
		`Counters, gauges and AndroidX JankStats bridge metrics when available.`, `Счетчики, gauge-метрики и AndroidX JankStats bridge, если они доступны.`,
		`Run Context`, `Контекст прогона`,
		`Cohorts keep comparisons honest: app, build, SDK, device, process and network.`, `Когорты помогают честно сравнивать версию приложения, сборку, SDK, устройство, процесс и сеть.`,
		`Health Gauges`, `Индикаторы здоровья`,
		`Signal Rings`, `Кольцевые индикаторы`,
		`>Battery<`, `>Батарея<`,
		`>Free RAM<`, `>Свободная RAM<`,
		`>Free storage<`, `>Свободное хранилище<`,
		`>Android<`, `>Android<`,
		`>CPU ABI<`, `>CPU ABI<`,
		`>Hardware<`, `>Железо<`,
		`>Brand<`, `>Бренд<`,
		`Route Details`, `Детали маршрутов`,
		`Screen Details`, `Детали экранов`,
		`Owner Details`, `Детали источников`,
		`Memory Details`, `Детали памяти`,
		`Metric Details`, `Детали метрик`,
		`Context Details`, `Детали контекста`,
		`Candidate Route, Screen And Owner Details`, `Детали маршрутов, экранов и источников кандидата`,
		`Cohort Details`, `Детали когорт`,
		`Heuristic Verdict`, `Эвристический итог`,
		`Rule-based triage over all collected signals. Treat it as a review checklist, not as a mathematical proof.`, `Эвристический разбор всех собранных сигналов. Это проверочный список для ревью, а не математическое доказательство.`,
		`Rule-based triage over all comparison deltas and cohort warnings. Treat it as a review checklist, not as a mathematical proof.`, `Эвристический разбор изменений и предупреждений по когортам. Это проверочный список для ревью, а не математическое доказательство.`,
		`Overall status`, `Общий статус`,
		`Findings`, `Находки`,
		`Recommendations`, `Рекомендации`,
		`No heuristic findings.`, `Нет эвристических находок.`,
		`No extra recommendations.`, `Нет дополнительных рекомендаций.`,
		`>Routes<`, `>Маршруты<`,
		`>Route<`, `>Маршрут<`,
		`>Count<`, `>Количество<`,
		`>Failures<`, `>Ошибки<`,
		`>Avg TTFB<`, `>Средний TTFB<`,
		`>Owner / Class<`, `>Источник / класс<`,
		`>Owner<`, `>Источник<`,
		`>Screens<`, `>Экраны<`,
		`>Screen<`, `>Экран<`,
		`>Windows<`, `>Окна<`,
		`>Frames<`, `>Кадры<`,
		`>Janky<`, `>Медленные кадры<`,
		`>Jank rate<`, `>Доля подтормаживаний<`,
		`>Avg FPS<`, `>Средний FPS<`,
		`>Min FPS<`, `>Мин. FPS<`,
		`>p95 frame<`, `>p95 кадра<`,
		`>max p99<`, `>макс. p99<`,
		`>Kind<`, `>Тип<`,
		`>Total<`, `>Итого<`,
		`>Stack hint<`, `>Подсказка стека<`,
		`>Value<`, `>Значение<`,
		`>Details<`, `>Детали<`,
		`>Class / Owner<`, `>Класс / источник<`,
		`>Age<`, `>Возраст<`,
		`>Name<`, `>Имя<`,
		`>Average<`, `>Среднее<`,
		`>Metric<`, `>Метрика<`,
		`>App Versions<`, `>Версии приложения<`,
		`>Devices<`, `>Устройства<`,
		`>Process Breakdown<`, `>Разбивка по процессам<`,
		`>Process<`, `>Процесс<`,
		`>Sessions<`, `>Сессии<`,
		`>Network Samples<`, `>Сэмплы сети<`,
		`>Combined Cohorts<`, `>Объединенные когорты<`,
		`>Counters<`, `>Счетчики<`,
		`>Gauges<`, `>Gauge-метрики<`,
		`>Memory And Metrics<`, `>Память и метрики<`,
		`>Signal<`, `>Сигнал<`,
		`>Cohort<`, `>Когорта<`,
		`>Baseline process<`, `>Процесс базы<`,
		`>Candidate process<`, `>Процесс кандидата<`,
		`>Change<`, `>Изменение<`,
		`>Regression<`, `>Регрессия<`,
		`>Severity<`, `>Серьезность<`,
		`>Confidence<`, `>Доверие<`,
		`>Sample<`, `>Выборка<`,
		`>Interval<`, `>Интервал<`,
		`HTTP p95`, `HTTP p95`,
		`HTTP failures`, `HTTP ошибки`,
		`UI jank rate`, `Доля подтормаживаний UI`,
		`UI avg FPS`, `Средний FPS UI`,
		`Main-thread stall max`, `Макс. пауза главного потока`,
		`Max PSS`, `Макс. PSS`,
		`Min available memory`, `Мин. доступная память`,
		`UID RX max`, `Макс. UID RX`,
		`UID TX max`, `Макс. UID TX`,
		` ms`, ` мс`,
		`Retained objects`, `Удержанные объекты`,
		`Process mix`, `Состав процессов`,
		`App version mix`, `Состав версий приложения`,
		`SDK mix`, `Состав SDK`,
		`Device mix`, `Состав устройств`,
		`Network mix`, `Состав сетей`,
		`Cohort mix`, `Состав когорт`,
		`<div class="label">Average FPS</div>`, `<div class="label">Средний FPS</div>`,
		`<div class="label">Max stall</div>`, `<div class="label">Макс. пауза</div>`,
		`<div class="label">UID RX max</div>`, `<div class="label">Макс. UID RX</div>`,
		` requests, `, ` запросов, `,
		` requests<`, ` запросов<`,
		` failed`, ` ошибок`,
		` frames`, ` кадров`,
		`min free`, `мин. свободно`,
		`min `, `мин. `,
		` stall events`, ` событий пауз`,
		`retained `, `удержано `,
		`TX max `, `макс. TX `,
		`validated yes`, `проверена: да`,
		`validated no`, `проверена: нет`,
		`metered yes`, `лимитная: да`,
		`metered no`, `лимитная: нет`,
		`VPN yes`, `VPN да`,
		`VPN no`, `VPN нет`,
		`not charging`, `не заряжается`,
		`charging`, `заряжается`,
		`discharging`, `разряжается`,
		`full`, `полная`,
		`total`, `всего`,
		`supported`, `поддерживаются`,
		`security patch unknown`, `патч безопасности неизвестен`,
		`security patch`, `патч безопасности`,
		`app data partition`, `раздел данных приложения`,
		`board `, `плата `,
		`product `, `продукт `,
		`brand `, `бренд `,
		`process `, `процесс `,
		`avg FPS`, `средний FPS`,
		`candidate jank`, `подтормаживания кандидата`,
		`candidate fail`, `ошибки кандидата`,
		`candidate FPS`, `FPS кандидата`,
		`avg FPS`, `средний FPS`,
		`No HTTP events.`, `Нет HTTP-событий.`,
		`No UI window events.`, `Нет событий UI-окон.`,
		`No owner attribution yet.`, `Атрибуция источников пока недоступна.`,
		`No memory events.`, `Нет событий памяти.`,
		`No retained-object events.`, `Нет событий удержанных объектов.`,
		`No counters.`, `Нет счетчиков.`,
		`No gauges.`, `Нет gauge-метрик.`,
		`No JankStats metrics.`, `Нет метрик JankStats.`,
		`No process metadata.`, `Нет метаданных процессов.`,
		`No context events.`, `Нет событий контекста.`,
		`No cohort metadata.`, `Нет метаданных когорт.`,
		`No per-log baseline details were embedded.`, `Детали логов базы не встроены.`,
		`No per-log candidate details were embedded.`, `Детали логов кандидата не встроены.`,
		`No owners.`, `Нет источников.`,
		`content: "open";`, `content: "открыть";`,
		`content: "close";`, `content: "закрыть";`,
		`<td class="sev-high">high</td>`, `<td class="sev-high">высокая</td>`,
		`<td class="sev-medium">medium</td>`, `<td class="sev-medium">средняя</td>`,
		`<td class="sev-ok">ok</td>`, `<td class="sev-ok">норма</td>`,
		`<td>high</td>`, `<td>высокая</td>`,
		`<td>medium</td>`, `<td>средняя</td>`,
		`<td>low</td>`, `<td>низкая</td>`,
		`<td>same</td>`, `<td>без изменений</td>`,
		`changed`, `изменено`,
		`app version mix differs: baseline`, `состав версий приложения отличается: база`,
		`SDK mix differs: baseline`, `состав SDK отличается: база`,
		`device mix differs: baseline`, `состав устройств отличается: база`,
		`process mix differs: baseline`, `состав процессов отличается: база`,
		`network mix differs: baseline`, `состав сетей отличается: база`,
		`cohort mix differs: baseline`, `состав когорт отличается: база`,
		`, candidate`, `, кандидат`,
	)
	return replacer.Replace(html)
}
