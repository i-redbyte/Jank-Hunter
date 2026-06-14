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
		"GeneratedAt":    time.Now().Format(time.RFC3339),
		"Summary":        summary,
		"Analysis":       inspectAnalysis(summary, lang),
		"MathReportHref": MathReportHref(path),
	})
}

func WriteCompare(path string, comparison analyze.Comparison) error {
	return WriteCompareReport(path, comparison, nil, nil)
}

func WriteCompareReport(path string, comparison analyze.Comparison, baselineLogs, candidateLogs []LogReport) error {
	lang := reportLanguage()
	return execute(path, compareTemplate, map[string]any{
		"GeneratedAt":    time.Now().Format(time.RFC3339),
		"Comparison":     comparison,
		"BaselineLogs":   baselineLogs,
		"CandidateLogs":  candidateLogs,
		"Analysis":       compareAnalysis(comparison, lang),
		"MathReportHref": MathReportHref(path),
	})
}

func WriteMathInspect(path string, mathReport mathanalysis.MathReport) error {
	return execute(path, mathInspectTemplate, map[string]any{
		"GeneratedAt":      time.Now().Format(time.RFC3339),
		"Math":             mathReport,
		"MethodReferences": mathanalysis.MethodReferences(),
	})
}

func WriteMathCompare(path string, mathReport mathanalysis.CompareMathReport) error {
	return execute(path, mathCompareTemplate, map[string]any{
		"GeneratedAt":      time.Now().Format(time.RFC3339),
		"Math":             mathReport,
		"MethodReferences": mathanalysis.MethodReferences(),
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
		"humanDuration": humanDuration,
		"tip":           tooltipHTML,
		"metricHelp":    metricHelp,
		"memoryHelp":    memoryMetricHelp,
		"integralHelp":  integralHelp,
		"ownerKind":     ownerKindLabel,
		"bucketClass": func(bucket mathanalysis.TimelineBucket) string {
			if zeroTimelineBucket(bucket) {
				return "bucket-zero"
			}
			return ""
		},
		"robustGroups":         robustStatGroups,
		"robustDeltaGroups":    robustDeltaGroups,
		"causalGraphSVG":       causalGraphSVG,
		"mathHeuristic":        inspectMathHeuristic,
		"compareMathHeuristic": compareMathHeuristic,
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
	if len(summary.Cards) == 0 {
		summary.Cards = append(summary.Cards, heuristicCard{Severity: "ok", Title: "Что проверить первым", Detail: "Сохраните сравнение как контрольную точку. При следующей регрессии начните с разделов робастных дельт, сетевых циклов и графа причинности."})
	}
	return summary
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
		`Candidate Device Context`, `Контекст устройства кандидата`,
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
		`>Candidate Detail<`, `>Детали кандидата<`,
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
		`Candidate Deep Summary`, `Подробная сводка кандидата`,
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
