package mathanalysis

import (
	"fmt"
	"math"
	"sort"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/datavalue"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

type robustKey struct {
	Dimension string
	Name      string
	Metric    string
	Unit      string
}

type robustSampleSet struct {
	values             []float64
	denseCounts        []uint64
	denseOffset        uint64
	outlierCounts      map[uint64]uint64
	orderedFrequencies []robustFrequency
	cumulativeCounts   []uint64
	seen               int
	sorted             bool
	nextPromotionCheck int
}

type robustFrequency struct {
	value float64
	count uint64
}

type robustSampleMap map[robustKey]*robustSampleSet

type robustCollector struct {
	filter  analyze.Filter
	samples robustSampleMap
}

func (c *robustCollector) add(event jhlog.Event, dict map[uint64]string, symbols *mathSymbolResolver) {
	switch {
	case event.HTTP != nil:
		route := symbols.resolve(dict, event.HTTP.RouteRef)
		owner := symbols.resolve(dict, event.Attribution.Owner)
		if !timelineContainsFilter(route, c.filter.RouteContains) || !timelineContainsFilter(owner, c.filter.OwnerContains) {
			return
		}
		duration := float64(event.HTTP.DurationMS)
		c.addValue("Маршрут", route, "HTTP задержка", "мс", duration)
		c.addValue("Источник", owner, "HTTP задержка", "мс", duration)
		if event.HTTP.DNSMS > 0 {
			c.addValue("Маршрут", route, "DNS задержка", "мс", float64(event.HTTP.DNSMS))
		}
		if event.HTTP.ConnectMS > 0 {
			c.addValue("Маршрут", route, "Задержка соединения", "мс", float64(event.HTTP.ConnectMS))
		}
	case event.UIWindow != nil:
		screen := symbols.resolve(dict, event.Attribution.Screen)
		if !timelineContainsFilter(screen, c.filter.ScreenContains) {
			return
		}
		if event.UIWindow.P95MS > 0 {
			c.addValue("Экран", screen, "UI window-p95", "мс", float64(event.UIWindow.P95MS))
		}
		if event.UIWindow.FrameCount > 0 {
			c.addValue("Экран", screen, "Доля подтормаживаний UI", "%", jankRate(event.UIWindow.JankCount, event.UIWindow.FrameCount))
		}
	case event.Stall != nil:
		if isMathDiagnosticStall(event, dict, symbols) {
			return
		}
		owner := symbols.resolve(dict, event.Attribution.Owner)
		if !timelineContainsFilter(owner, c.filter.OwnerContains) {
			return
		}
		c.addValue("Источник", owner, "Пауза главного потока", "мс", float64(event.Stall.DurationMS))
	case event.Retained != nil:
		className := symbols.resolve(dict, event.Retained.ClassRef)
		if !timelineContainsFilter(className, c.filter.ClassContains) {
			return
		}
		c.addValue("Источник", className, "Возраст удержанного объекта", "мс", float64(event.Retained.AgeMS))
	case event.Memory != nil:
		if event.Memory.PSSKB > 0 {
			c.addValue("Память", "процесс", "PSS", "КБ", float64(event.Memory.PSSKB))
		}
		if event.Memory.JavaHeapKB > 0 {
			c.addValue("Память", "процесс", "Куча Java", "КБ", float64(event.Memory.JavaHeapKB))
		}
		if event.Memory.NativeHeapKB > 0 {
			c.addValue("Память", "процесс", "Нативная куча", "КБ", float64(event.Memory.NativeHeapKB))
		}
	case event.Metric != nil && event.Type == jhlog.EventGauge:
		name := symbols.resolve(dict, event.Metric.MetricRef)
		c.addMetricValue(name, event.Metric)
	}
}

func (c *robustCollector) addMetricValue(name string, metric *jhlog.MetricEvent) {
	if metric == nil {
		return
	}
	c.addValue("Пользовательская метрика", name, "Значение", "знач.", float64(metric.Value))
}

func (c *robustCollector) addValue(dimension, name, metric, unit string, value float64) {
	if datavalue.IsUnknown(name) || math.IsNaN(value) || math.IsInf(value, 0) {
		return
	}
	key := robustKey{Dimension: dimension, Name: name, Metric: metric, Unit: unit}
	set := c.samples[key]
	if set == nil {
		set = &robustSampleSet{}
		c.samples[key] = set
	}
	set.add(value)
}

func (s *robustSampleSet) add(value float64) {
	if s.seen == 0 {
		s.nextPromotionCheck = robustSampleSetPromotionThreshold
	}
	s.seen++
	if len(s.denseCounts) > 0 {
		integer, integral := exactNonNegativeInteger(value)
		if integral && integer >= s.denseOffset && integer-s.denseOffset < uint64(len(s.denseCounts)) {
			s.denseCounts[integer-s.denseOffset]++
		} else {
			if s.outlierCounts == nil {
				s.outlierCounts = make(map[uint64]uint64)
			}
			s.outlierCounts[math.Float64bits(value)]++
		}
		s.sorted = false
		return
	}
	s.values = append(s.values, value)
	s.sorted = false
	if s.seen >= s.nextPromotionCheck {
		if !s.promoteDenseIfBeneficial() && s.nextPromotionCheck <= s.seen {
			maxInt := int(^uint(0) >> 1)
			if s.seen > maxInt/robustSampleSetPromotionBackoff {
				s.nextPromotionCheck = maxInt
			} else {
				s.nextPromotionCheck = s.seen * robustSampleSetPromotionBackoff
			}
		}
	}
}

func (s *robustSampleSet) sortedValues() []float64 {
	if s == nil || s.seen == 0 || len(s.denseCounts) > 0 {
		return nil
	}
	if !s.sorted {
		sort.Float64s(s.values)
		s.sorted = true
	}
	return s.values
}

func (s *robustSampleSet) compacted() bool {
	return s != nil && len(s.denseCounts) > 0
}

func (s *robustSampleSet) promoteDenseIfBeneficial() bool {
	if len(s.values) == 0 {
		return false
	}
	minInteger, integral := exactNonNegativeInteger(s.values[0])
	if !integral {
		return false
	}
	maxInteger := minInteger
	for _, value := range s.values[1:] {
		integer, exact := exactNonNegativeInteger(value)
		if !exact {
			return false
		}
		if integer < minInteger {
			minInteger = integer
		}
		if integer > maxInteger {
			maxInteger = integer
		}
	}
	if maxInteger-minInteger >= robustSampleSetMaxDenseBins {
		return false
	}
	span := int(maxInteger-minInteger) + 1
	if span > s.seen/robustSampleSetMinimumCompression {
		return false
	}
	counts := make([]uint64, span)
	for _, value := range s.values {
		integer, _ := exactNonNegativeInteger(value)
		counts[integer-minInteger]++
	}
	s.denseCounts = counts
	s.denseOffset = minInteger
	s.values = nil
	s.sorted = false
	return true
}

func (s *robustSampleSet) prepareOrderedFrequencies() {
	if s == nil || s.sorted || len(s.denseCounts) == 0 {
		return
	}
	unique := len(s.outlierCounts)
	for _, count := range s.denseCounts {
		if count > 0 {
			unique++
		}
	}
	frequencies := make([]robustFrequency, 0, unique)
	for index, count := range s.denseCounts {
		if count > 0 {
			frequencies = append(frequencies, robustFrequency{
				value: float64(s.denseOffset + uint64(index)),
				count: count,
			})
		}
	}
	for bits, count := range s.outlierCounts {
		frequencies = append(frequencies, robustFrequency{value: math.Float64frombits(bits), count: count})
	}
	sort.Slice(frequencies, func(i, j int) bool { return frequencies[i].value < frequencies[j].value })
	cumulative := make([]uint64, len(frequencies))
	var total uint64
	for index, frequency := range frequencies {
		total += frequency.count
		cumulative[index] = total
	}
	s.orderedFrequencies = frequencies
	s.cumulativeCounts = cumulative
	s.sorted = true
}

func exactNonNegativeInteger(value float64) (uint64, bool) {
	if value < 0 || value > float64(^uint64(0)>>1) {
		return 0, false
	}
	integer := uint64(value)
	return integer, float64(integer) == value
}

const (
	robustSampleSetPromotionThreshold = 4_096
	robustSampleSetPromotionBackoff   = 8
	robustSampleSetMaxDenseBins       = 65_536
	robustSampleSetMinimumCompression = 2
)

func summarizeRobustSamples(samples robustSampleMap) []RobustStat {
	stats := make([]RobustStat, 0, len(samples))
	for key, set := range samples {
		stat := summarizeRobustSet(key, set)
		if stat.Count > 0 {
			stats = append(stats, stat)
		}
	}
	sortRobustStats(stats)
	return stats
}

func summarizeRobustSet(key robustKey, set *robustSampleSet) RobustStat {
	if sampleCount(set) == 0 {
		return RobustStat{}
	}
	median := set.median()
	quality, severity, detail := sampleQuality(set.seen)
	low, high, hasCI := bootstrapP95CISet(set)
	return RobustStat{
		Dimension:             key.Dimension,
		Name:                  key.Name,
		Metric:                key.Metric,
		Unit:                  key.Unit,
		Count:                 set.seen,
		Median:                median,
		P90:                   set.percentile(0.90),
		P95:                   set.percentile(0.95),
		P99:                   set.percentile(0.99),
		MAD:                   set.medianAbsoluteDeviation(median),
		TrimmedMean:           set.trimmedMean(0.10),
		Min:                   set.minimum(),
		Max:                   set.maximum(),
		P95ConfidenceLow:      low,
		P95ConfidenceHigh:     high,
		HasP95Confidence:      hasCI,
		SampleQuality:         quality,
		SampleQualitySeverity: severity,
		SampleDetail:          detail,
	}
}

func compareRobustSamples(baseline, candidate robustSampleMap) []RobustDelta {
	keys := make(map[robustKey]struct{}, len(baseline)+len(candidate))
	for key := range baseline {
		keys[key] = struct{}{}
	}
	for key := range candidate {
		keys[key] = struct{}{}
	}

	deltas := make([]RobustDelta, 0, len(keys))
	for key := range keys {
		baseSet := baseline[key]
		candidateSet := candidate[key]
		if sampleCount(baseSet) == 0 && sampleCount(candidateSet) == 0 {
			continue
		}
		deltas = append(deltas, compareRobustSet(key, baseSet, candidateSet))
	}
	sort.Slice(deltas, func(i, j int) bool {
		if severityRank(deltas[i].Severity) != severityRank(deltas[j].Severity) {
			return severityRank(deltas[i].Severity) > severityRank(deltas[j].Severity)
		}
		if math.Abs(deltas[i].P95DeltaPct) != math.Abs(deltas[j].P95DeltaPct) {
			return math.Abs(deltas[i].P95DeltaPct) > math.Abs(deltas[j].P95DeltaPct)
		}
		if deltas[i].Dimension != deltas[j].Dimension {
			return dimensionRank(deltas[i].Dimension) < dimensionRank(deltas[j].Dimension)
		}
		return deltas[i].Name < deltas[j].Name
	})
	return deltas
}

func compareRobustSet(key robustKey, baseline, candidate *robustSampleSet) RobustDelta {
	baseCount := sampleCount(baseline)
	candidateCount := sampleCount(candidate)
	baseP95 := robustPercentile(baseline, 0.95)
	candidateP95 := robustPercentile(candidate, 0.95)
	delta := candidateP95 - baseP95
	deltaPct := 0.0
	comparable := baseCount > 0 && candidateCount > 0
	deltaPctAvailable := comparable && baseP95 != 0
	if deltaPctAvailable {
		deltaPct = delta * 100 / baseP95
	}
	cliff := 0.0
	effect := "не применимо"
	if comparable {
		cliff = cliffDeltaSets(candidate, baseline)
		effect = effectSizeLabel(cliff)
	}
	confidence := compareConfidence(baseCount, candidateCount, deltaPct, cliff)
	severity := robustDeltaSeverity(key, deltaPct, deltaPctAvailable, cliff, baseCount, candidateCount)
	recommendation := robustDeltaRecommendation(severity)
	if !comparable {
		recommendation = "Проверьте, что базовый и проверяемый прогоны проходили одинаковый сценарий и собирали одинаковые типы событий. Распределение есть только с одной стороны, поэтому вывод об ухудшении невозможен."
	}
	return RobustDelta{
		Dimension:         key.Dimension,
		Name:              key.Name,
		Metric:            key.Metric,
		Unit:              key.Unit,
		BaselineCount:     baseCount,
		CandidateCount:    candidateCount,
		BaselineP95:       baseP95,
		CandidateP95:      candidateP95,
		P95Delta:          delta,
		P95DeltaPct:       deltaPct,
		DeltaPctAvailable: deltaPctAvailable,
		CliffDelta:        cliff,
		Comparable:        comparable,
		EffectSize:        effect,
		Confidence:        confidence,
		Severity:          severity,
		Summary:           robustDeltaSummary(key, baseCount, candidateCount, baseP95, candidateP95, deltaPct, cliff),
		Recommendation:    recommendation,
	}
}

func robustStatus(stats []RobustStat) string {
	if len(stats) == 0 {
		return "medium"
	}
	for _, stat := range stats {
		if stat.SampleQualitySeverity == "medium" {
			return "medium"
		}
	}
	return "ok"
}

func robustSummary(stats []RobustStat) string {
	if len(stats) == 0 {
		return "Недостаточно данных для устойчивой статистики: нет распределений по маршрутам, экранам, местам запуска или пользовательским метрикам."
	}
	withCI := 0
	for _, stat := range stats {
		if stat.HasP95Confidence {
			withCI++
		}
	}
	return fmt.Sprintf("Посчитано %d распределений: медиана, p90/p95/p99, MAD, 10%% усечённое среднее; интервал p95 методом повторной выборки есть у %d сигналов.", len(stats), withCI)
}

func robustFindings(stats []RobustStat) []Finding {
	if len(stats) == 0 {
		return []Finding{{
			Severity:       "medium",
			Title:          "Недостаточно данных для устойчивой статистики",
			Detail:         "В журналах нет достаточных распределений по маршрутам, экранам, местам запуска или пользовательским метрикам.",
			Recommendation: "Соберите прогон с событиями HTTP/UI или включёнными пользовательскими метриками Android.",
		}}
	}
	findings := []Finding{{
		Severity: "ok",
		Title:    "Устойчивая статистика посчитана",
		Detail:   robustSummary(stats),
	}}
	lowSample := 0
	for _, stat := range stats {
		if stat.SampleQualitySeverity == "medium" {
			lowSample++
		}
	}
	if lowSample > 0 {
		findings = append(findings, Finding{
			Severity:       "medium",
			Title:          "Есть распределения с малым размером выборки",
			Detail:         fmt.Sprintf("%d сигналов имеют ограниченную выборку. Для них p95/p99 и дельта Клиффа менее устойчивы.", lowSample),
			Recommendation: "Соберите несколько повторов сценария или более длинный тестовый прогон перед выводом об ухудшении.",
		})
	}
	return findings
}

func compareRobustSummary(deltas []RobustDelta) string {
	if len(deltas) == 0 {
		return "Недостаточно пересекающихся распределений для устойчивого сравнения."
	}
	comparable := 0
	for _, delta := range deltas {
		if delta.Comparable {
			comparable++
		}
	}
	return fmt.Sprintf("Сопоставимо %d из %d распределений. Для остальных сигнал есть только в одном прогоне, поэтому дельта Клиффа и относительное изменение не рассчитываются.", comparable, len(deltas))
}

func compareRobustFindings(deltas []RobustDelta) []Finding {
	if len(deltas) == 0 {
		return []Finding{{
			Severity:       "medium",
			Title:          "Нет распределений для устойчивого сравнения",
			Detail:         "Базовый и проверяемый прогоны не имеют сопоставимых выборок по маршрутам, экранам, местам запуска или пользовательским метрикам.",
			Recommendation: "Проверьте, что базовый и проверяемый прогоны проходят одни и те же экраны, маршруты и места запуска.",
		}}
	}
	for _, delta := range deltas {
		if delta.Severity == "high" || delta.Severity == "medium" {
			title := "Найдено устойчивое ухудшение"
			evidence := []string{fmt.Sprintf("%s · %s · %s", delta.Dimension, delta.Name, delta.Metric)}
			if delta.Comparable {
				evidence = append(evidence, fmt.Sprintf("дельта Клиффа %.3f, эффект: %s, доверие: %s", delta.CliffDelta, delta.EffectSize, delta.Confidence))
			} else {
				title = "Распределение есть только в одном прогоне"
				evidence = append(evidence, fmt.Sprintf("наблюдений: базовый прогон=%d, проверяемый прогон=%d; размер эффекта и относительное изменение не рассчитываются", delta.BaselineCount, delta.CandidateCount))
			}
			return []Finding{{
				Severity:       delta.Severity,
				Title:          title,
				Detail:         delta.Summary,
				Recommendation: delta.Recommendation,
				Evidence:       evidence,
			}}
		}
	}
	return []Finding{{
		Severity: "ok",
		Title:    "Явных устойчивых ухудшений не найдено",
		Detail:   compareRobustSummary(deltas),
	}}
}

func sampleQuality(total int) (string, string, string) {
	quality := "хорошая"
	severity := "ok"
	switch {
	case total < 5:
		quality = "малая"
		severity = "medium"
	case total < 20:
		quality = "ограниченная"
		severity = "medium"
	case total < 50:
		quality = "достаточная"
	}
	return quality, severity, fmt.Sprintf("наблюдений=%d", total)
}

func compareConfidence(baseCount, candidateCount int, deltaPct, cliff float64) string {
	if baseCount == 0 || candidateCount == 0 {
		return "не применимо: сигнал есть только в одном прогоне"
	}
	minCount := baseCount
	if candidateCount < minCount {
		minCount = candidateCount
	}
	absDeltaPct := math.Abs(deltaPct)
	absCliff := math.Abs(cliff)
	switch {
	case minCount >= 50 && absCliff >= 0.33 && absDeltaPct >= 20:
		return "высокое: повторяемая выборка и крупный эффект"
	case minCount >= 50:
		return "среднее+: выборка хорошая, эффект умеренный"
	case minCount >= 20 && absCliff >= 0.147 && absDeltaPct >= 10:
		return "среднее: достаточная выборка и заметный эффект"
	case minCount >= 20:
		return "среднее-: выборка достаточная, эффект слабый"
	default:
		return "низкое: нужна повторная выборка"
	}
}

func robustDeltaSeverity(key robustKey, deltaPct float64, deltaPctAvailable bool, cliff float64, baseCount, candidateCount int) string {
	if key.Dimension == "Пользовательская метрика" {
		return "ok"
	}
	if baseCount == 0 && candidateCount > 0 {
		return "medium"
	}
	if candidateCount == 0 {
		return "ok"
	}
	if !deltaPctAvailable {
		if cliff >= 0.474 {
			return "medium"
		}
		return "ok"
	}
	confidenceTier := robustConfidenceTier(baseCount, candidateCount, deltaPct, cliff)
	if confidenceTier >= 2 && deltaPct >= 50 && cliff >= 0.33 {
		return "high"
	}
	if confidenceTier >= 1 && deltaPct >= 20 && cliff >= 0.147 {
		return "medium"
	}
	if confidenceTier == 0 && deltaPct >= 80 && cliff >= 0.474 {
		return "medium"
	}
	return "ok"
}

func robustConfidenceTier(baseCount, candidateCount int, deltaPct, cliff float64) int {
	minCount := baseCount
	if candidateCount < minCount {
		minCount = candidateCount
	}
	absDeltaPct := math.Abs(deltaPct)
	absCliff := math.Abs(cliff)
	switch {
	case minCount >= 50 && absCliff >= 0.33 && absDeltaPct >= 20:
		return 2
	case minCount >= 20 && absCliff >= 0.147 && absDeltaPct >= 10:
		return 1
	default:
		return 0
	}
}

func robustDeltaSummary(key robustKey, baseCount, candidateCount int, baseP95, candidateP95, deltaPct, cliff float64) string {
	if baseCount == 0 {
		return fmt.Sprintf("Сигнал %s/%s появился только в проверяемом прогоне: p95 %.1f %s, наблюдений=%d.", key.Name, key.Metric, candidateP95, key.Unit, candidateCount)
	}
	if candidateCount == 0 {
		return fmt.Sprintf("Сигнал %s/%s исчез в проверяемом прогоне: p95 базового прогона %.1f %s, наблюдений=%d.", key.Name, key.Metric, baseP95, key.Unit, baseCount)
	}
	if baseP95 == 0 {
		return fmt.Sprintf("%s/%s: p95 изменился с нуля до %.1f %s. Процент не рассчитывается, потому что делить на нулевую базу нельзя.", key.Name, key.Metric, candidateP95, key.Unit)
	}
	if key.Dimension == "Пользовательская метрика" {
		return fmt.Sprintf("%s/%s: p95 изменился с %.1f до %.1f %s (%+.1f%%), дельта Клиффа %.3f. Для пользовательской метрики неизвестно, какое направление лучше, поэтому изменение не помечается как ухудшение автоматически.", key.Name, key.Metric, baseP95, candidateP95, key.Unit, deltaPct, cliff)
	}
	return fmt.Sprintf("%s/%s: p95 изменился с %.1f до %.1f %s (%+.1f%%), дельта Клиффа %.3f.", key.Name, key.Metric, baseP95, candidateP95, key.Unit, deltaPct, cliff)
}

func robustDeltaRecommendation(severity string) string {
	switch severity {
	case "high":
		return "Проверьте место запуска и маршрут вокруг этого сигнала в основном отчёте и на временной шкале; эффект крупный и похож на реальное ухудшение."
	case "medium":
		return "Проверьте повторяемость на еще одном прогоне; эффект заметный, но зависит от размера выборки и шума сценария."
	default:
		return ""
	}
}

func effectSizeLabel(delta float64) string {
	absDelta := math.Abs(delta)
	switch {
	case absDelta < 0.147:
		return "пренебрежимый"
	case absDelta < 0.33:
		return "малый"
	case absDelta < 0.474:
		return "средний"
	default:
		return "крупный"
	}
}

func cliffDeltaSorted(candidate, baseline []float64) float64 {
	if len(candidate) == 0 || len(baseline) == 0 {
		return 0
	}
	var greater int64
	var less int64
	for _, value := range candidate {
		lessCount := sort.SearchFloat64s(baseline, value)
		greaterCount := len(baseline) - sort.Search(len(baseline), func(i int) bool {
			return baseline[i] > value
		})
		greater += int64(lessCount)
		less += int64(greaterCount)
	}
	return float64(greater-less) / float64(len(candidate)*len(baseline))
}

func cliffDeltaSets(candidate, baseline *robustSampleSet) float64 {
	if sampleCount(candidate) == 0 || sampleCount(baseline) == 0 {
		return 0
	}
	if !candidate.compacted() && !baseline.compacted() {
		return cliffDeltaSorted(candidate.sortedValues(), baseline.sortedValues())
	}
	var greater float64
	var less float64
	visitRobustFrequencies(candidate, func(value float64, count uint64) {
		baselineLess, baselineGreater := baseline.lessAndGreater(value)
		greater += float64(count) * float64(baselineLess)
		less += float64(count) * float64(baselineGreater)
	})
	denominator := float64(candidate.seen) * float64(baseline.seen)
	return (greater - less) / denominator
}

func visitRobustFrequencies(set *robustSampleSet, visit func(value float64, count uint64)) {
	if set == nil {
		return
	}
	if !set.compacted() {
		values := set.sortedValues()
		for index := 0; index < len(values); {
			next := index + 1
			for next < len(values) && values[next] == values[index] {
				next++
			}
			visit(values[index], uint64(next-index))
			index = next
		}
		return
	}
	set.prepareOrderedFrequencies()
	for _, frequency := range set.orderedFrequencies {
		visit(frequency.value, frequency.count)
	}
}

func (s *robustSampleSet) lessAndGreater(value float64) (uint64, uint64) {
	if s == nil || s.seen == 0 {
		return 0, 0
	}
	if !s.compacted() {
		values := s.sortedValues()
		less := sort.SearchFloat64s(values, value)
		greaterStart := sort.Search(len(values), func(index int) bool { return values[index] > value })
		return uint64(less), uint64(len(values) - greaterStart)
	}
	s.prepareOrderedFrequencies()
	first := sort.Search(len(s.orderedFrequencies), func(index int) bool {
		return s.orderedFrequencies[index].value >= value
	})
	after := sort.Search(len(s.orderedFrequencies), func(index int) bool {
		return s.orderedFrequencies[index].value > value
	})
	less := uint64(0)
	if first > 0 {
		less = s.cumulativeCounts[first-1]
	}
	lessOrEqual := uint64(0)
	if after > 0 {
		lessOrEqual = s.cumulativeCounts[after-1]
	}
	return less, uint64(s.seen) - lessOrEqual
}

func bootstrapP95CI(values []float64) (float64, float64, bool) {
	if len(values) < 20 {
		return 0, 0, false
	}
	base := bootstrapBase(values, 512)
	return bootstrapP95CIFromBase(base, len(values))
}

func bootstrapP95CISet(set *robustSampleSet) (float64, float64, bool) {
	if sampleCount(set) < 20 {
		return 0, 0, false
	}
	if !set.compacted() {
		return bootstrapP95CI(set.sortedValues())
	}
	const limit = 512
	base := make([]float64, 0, limit)
	step := float64(set.seen-1) / float64(limit-1)
	for index := 0; index < limit; index++ {
		base = append(base, set.valueAt(int(math.Round(float64(index)*step))))
	}
	return bootstrapP95CIFromBase(base, set.seen)
}

func bootstrapP95CIFromBase(base []float64, originalCount int) (float64, float64, bool) {
	const rounds = 200
	boot := make([]float64, 0, rounds)
	seed := uint64(originalCount)*1469598103934665603 + 1099511628211
	for round := 0; round < rounds; round++ {
		resample := make([]float64, len(base))
		for i := range resample {
			seed = seed*2862933555777941757 + 3037000493
			resample[i] = base[int(seed%uint64(len(base)))]
		}
		sort.Float64s(resample)
		boot = append(boot, percentileSorted(resample, 0.95))
	}
	sort.Float64s(boot)
	return percentileSorted(boot, 0.025), percentileSorted(boot, 0.975), true
}

func bootstrapBase(values []float64, limit int) []float64 {
	if len(values) <= limit {
		return append([]float64(nil), values...)
	}
	out := make([]float64, 0, limit)
	step := float64(len(values)-1) / float64(limit-1)
	for i := 0; i < limit; i++ {
		out = append(out, values[int(math.Round(float64(i)*step))])
	}
	return out
}

func (s *robustSampleSet) percentile(p float64) float64 {
	if s == nil || s.seen == 0 {
		return 0
	}
	rank := int(math.Ceil(float64(s.seen)*p)) - 1
	if rank < 0 {
		rank = 0
	}
	if rank >= s.seen {
		rank = s.seen - 1
	}
	return s.valueAt(rank)
}

func robustPercentile(set *robustSampleSet, p float64) float64 {
	if set == nil {
		return 0
	}
	return set.percentile(p)
}

func (s *robustSampleSet) valueAt(rank int) float64 {
	if s == nil || s.seen == 0 {
		return 0
	}
	if rank < 0 {
		rank = 0
	}
	if rank >= s.seen {
		rank = s.seen - 1
	}
	if !s.compacted() {
		return s.sortedValues()[rank]
	}
	s.prepareOrderedFrequencies()
	target := uint64(rank + 1)
	index := sort.Search(len(s.cumulativeCounts), func(index int) bool {
		return s.cumulativeCounts[index] >= target
	})
	if index >= len(s.orderedFrequencies) {
		return s.orderedFrequencies[len(s.orderedFrequencies)-1].value
	}
	return s.orderedFrequencies[index].value
}

func (s *robustSampleSet) median() float64 {
	if s == nil || s.seen == 0 {
		return 0
	}
	mid := s.seen / 2
	if s.seen%2 == 1 {
		return s.valueAt(mid)
	}
	return (s.valueAt(mid-1) + s.valueAt(mid)) / 2
}

func (s *robustSampleSet) minimum() float64 {
	return s.valueAt(0)
}

func (s *robustSampleSet) maximum() float64 {
	if s == nil {
		return 0
	}
	return s.valueAt(s.seen - 1)
}

func (s *robustSampleSet) medianAbsoluteDeviation(median float64) float64 {
	if s == nil || s.seen == 0 {
		return 0
	}
	if !s.compacted() {
		return medianAbsoluteDeviation(s.sortedValues(), median)
	}
	deviations := make([]robustFrequency, 0, len(s.orderedFrequencies))
	visitRobustFrequencies(s, func(value float64, count uint64) {
		deviations = append(deviations, robustFrequency{value: math.Abs(value - median), count: count})
	})
	sort.Slice(deviations, func(i, j int) bool { return deviations[i].value < deviations[j].value })
	leftRank := (s.seen - 1) / 2
	rightRank := s.seen / 2
	left := weightedFrequencyValueAt(deviations, leftRank)
	right := weightedFrequencyValueAt(deviations, rightRank)
	return (left + right) / 2
}

func (s *robustSampleSet) trimmedMean(ratio float64) float64 {
	if s == nil || s.seen == 0 {
		return 0
	}
	if !s.compacted() {
		return trimmedMeanSorted(s.sortedValues(), ratio)
	}
	trim := int(math.Floor(float64(s.seen) * ratio))
	if trim*2 >= s.seen {
		trim = 0
	}
	start := uint64(trim)
	end := uint64(s.seen - trim)
	var position uint64
	var sum float64
	var count uint64
	visitRobustFrequencies(s, func(value float64, frequency uint64) {
		frequencyStart := position
		frequencyEnd := position + frequency
		includedStart := max(frequencyStart, start)
		includedEnd := min(frequencyEnd, end)
		if includedEnd > includedStart {
			included := includedEnd - includedStart
			sum += value * float64(included)
			count += included
		}
		position = frequencyEnd
	})
	if count == 0 {
		return 0
	}
	return sum / float64(count)
}

func weightedFrequencyValueAt(frequencies []robustFrequency, rank int) float64 {
	if len(frequencies) == 0 {
		return 0
	}
	target := uint64(rank + 1)
	var seen uint64
	for _, frequency := range frequencies {
		seen += frequency.count
		if seen >= target {
			return frequency.value
		}
	}
	return frequencies[len(frequencies)-1].value
}

func sortedFloatCopy(values []float64) []float64 {
	out := append([]float64(nil), values...)
	sort.Float64s(out)
	return out
}

func medianSorted(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	mid := len(values) / 2
	if len(values)%2 == 1 {
		return values[mid]
	}
	return (values[mid-1] + values[mid]) / 2
}

func medianAbsoluteDeviation(values []float64, median float64) float64 {
	deviations := make([]float64, 0, len(values))
	for _, value := range values {
		deviations = append(deviations, math.Abs(value-median))
	}
	sort.Float64s(deviations)
	return medianSorted(deviations)
}

func trimmedMeanSorted(values []float64, ratio float64) float64 {
	if len(values) == 0 {
		return 0
	}
	trim := int(math.Floor(float64(len(values)) * ratio))
	if trim*2 >= len(values) {
		trim = 0
	}
	var sum float64
	count := 0
	for _, value := range values[trim : len(values)-trim] {
		sum += value
		count++
	}
	if count == 0 {
		return 0
	}
	return sum / float64(count)
}

func sampleCount(set *robustSampleSet) int {
	if set == nil {
		return 0
	}
	return set.seen
}

func sortRobustStats(stats []RobustStat) {
	sort.Slice(stats, func(i, j int) bool {
		if stats[i].Dimension != stats[j].Dimension {
			return dimensionRank(stats[i].Dimension) < dimensionRank(stats[j].Dimension)
		}
		if stats[i].Name != stats[j].Name {
			return stats[i].Name < stats[j].Name
		}
		if stats[i].P95 != stats[j].P95 {
			return stats[i].P95 > stats[j].P95
		}
		return stats[i].Metric < stats[j].Metric
	})
}

func dimensionRank(value string) int {
	switch value {
	case "Маршрут":
		return 0
	case "Экран":
		return 1
	case "Источник":
		return 2
	case "Пользовательская метрика":
		return 3
	case "Память":
		return 4
	case "Контекст":
		return 5
	default:
		return 9
	}
}

func severityRank(value string) int {
	switch value {
	case "high":
		return 3
	case "medium":
		return 2
	case "ok":
		return 1
	default:
		return 0
	}
}
