package mathanalysis

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

const maxRobustSamplesPerSignal = 20_000
const maxRobustWeightExpansion = 200_000

type robustKey struct {
	Dimension string
	Name      string
	Metric    string
	Unit      string
}

type robustSampleSet struct {
	values       []float64
	seen         int
	approximated bool
}

type robustSampleMap map[robustKey]*robustSampleSet

func collectRobustSamples(paths []string, options analyze.Options) (robustSampleMap, error) {
	collector := &robustCollector{
		filter:   normalizeTimelineFilter(options.Filter),
		ownerMap: options.OwnerMap,
		samples:  robustSampleMap{},
	}
	for _, path := range paths {
		if err := jhlog.StreamFile(path, func(event jhlog.Event, dict map[uint64]string) error {
			collector.add(event, dict)
			return nil
		}); err != nil {
			return nil, err
		}
	}
	return collector.samples, nil
}

type robustCollector struct {
	filter   analyze.Filter
	ownerMap map[string]string
	samples  robustSampleMap
}

func (c *robustCollector) add(event jhlog.Event, dict map[uint64]string) {
	switch {
	case event.HTTP != nil:
		route := jhlog.Resolve(dict, event.HTTP.RouteID)
		owner := c.resolveOwner(dict, event.HTTP.OwnerID)
		if !timelineContainsFilter(route, c.filter.RouteContains) || !timelineContainsFilter(owner, c.filter.OwnerContains) {
			return
		}
		duration := float64(event.HTTP.DurationMS)
		c.addValue("Маршрут", route, "HTTP задержка", "ms", duration)
		c.addValue("Источник", owner, "HTTP задержка", "ms", duration)
		if event.HTTP.DNSMS > 0 {
			c.addValue("Маршрут", route, "DNS задержка", "ms", float64(event.HTTP.DNSMS))
		}
		if event.HTTP.ConnectMS > 0 {
			c.addValue("Маршрут", route, "Connect задержка", "ms", float64(event.HTTP.ConnectMS))
		}
	case event.UIWindow != nil:
		screen := jhlog.Resolve(dict, event.UIWindow.ScreenID)
		if !timelineContainsFilter(screen, c.filter.ScreenContains) {
			return
		}
		if event.UIWindow.P95MS > 0 {
			c.addValue("Экран", screen, "UI p95 кадра", "ms", float64(event.UIWindow.P95MS))
		}
		if event.UIWindow.FrameCount > 0 {
			c.addValue("Экран", screen, "Доля подтормаживаний UI", "%", jankRate(event.UIWindow.JankCount, event.UIWindow.FrameCount))
		}
	case event.Stall != nil:
		owner := c.resolveOwner(dict, event.Stall.OwnerID)
		if !timelineContainsFilter(owner, c.filter.OwnerContains) {
			return
		}
		c.addValue("Источник", owner, "Пауза главного потока", "ms", float64(event.Stall.DurationMS))
	case event.Retained != nil:
		className := jhlog.Resolve(dict, event.Retained.ClassID)
		if !timelineContainsFilter(className, c.filter.ClassContains) {
			return
		}
		c.addValue("Источник", className, "Возраст удержанного объекта", "ms", float64(event.Retained.AgeMS))
	case event.Memory != nil:
		c.addValue("Память", "процесс", "PSS", "KB", float64(event.Memory.PSSKB))
		c.addValue("Память", "процесс", "Куча Java", "KB", float64(event.Memory.JavaHeapKB))
		c.addValue("Память", "процесс", "Нативная куча", "KB", float64(event.Memory.NativeHeapKB))
	case event.Context != nil:
		c.addValue("Контекст", "устройство", "Доступная память", "KB", float64(event.Context.AvailMemoryKB))
		if event.Context.BatteryPct > 0 {
			c.addValue("Контекст", "устройство", "Батарея", "%", float64(event.Context.BatteryPct))
		}
		if event.Context.BatteryTempDeciC != 0 {
			c.addValue("Контекст", "устройство", "Температура батареи", "0.1 C", float64(event.Context.BatteryTempDeciC))
		}
	case event.Metric != nil && event.Type == jhlog.EventGauge:
		name := jhlog.Resolve(dict, event.Metric.MetricID)
		if name == "" {
			name = fmt.Sprintf("metric:%d", event.Metric.MetricID)
		}
		c.addMetricValue(name, event.Metric)
	}
}

func (c *robustCollector) addMetricValue(name string, metric *jhlog.MetricEvent) {
	if metric == nil {
		return
	}
	weight := metric.Count
	if weight == 0 {
		weight = 1
	}
	if metric.Mode == jhlog.MetricModeLast || metric.Mode == jhlog.MetricModeState {
		weight = 1
	}
	c.addWeightedValue("Gauge-метрика", name, "Значение", "знач.", float64(metric.Value), weight)
}

func (c *robustCollector) addValue(dimension, name, metric, unit string, value float64) {
	c.addWeightedValue(dimension, name, metric, unit, value, 1)
}

func (c *robustCollector) addWeightedValue(dimension, name, metric, unit string, value float64, weight uint64) {
	if name == "" || math.IsNaN(value) || math.IsInf(value, 0) {
		return
	}
	if weight == 0 {
		return
	}
	key := robustKey{Dimension: dimension, Name: name, Metric: metric, Unit: unit}
	set := c.samples[key]
	if set == nil {
		set = &robustSampleSet{}
		c.samples[key] = set
	}
	set.addWeighted(value, weight)
}

func (c *robustCollector) resolveOwner(dict map[uint64]string, id uint64) string {
	return analyze.ResolveOwnerAlias(c.ownerMap, jhlog.Resolve(dict, id))
}

func (s *robustSampleSet) add(value float64) {
	s.addWeighted(value, 1)
}

func (s *robustSampleSet) addWeighted(value float64, weight uint64) {
	if weight == 0 {
		return
	}
	expanded := weight
	if expanded > maxRobustWeightExpansion {
		expanded = maxRobustWeightExpansion
		s.approximated = true
	}
	for i := uint64(0); i < expanded; i++ {
		s.addOne(value)
	}
	if skipped := weight - expanded; skipped > 0 {
		s.seen += int(skipped)
	}
}

func (s *robustSampleSet) addOne(value float64) {
	s.seen++
	if len(s.values) < maxRobustSamplesPerSignal {
		s.values = append(s.values, value)
		return
	}
	s.approximated = true
	index := deterministicReservoirIndex(s.seen)
	if index < maxRobustSamplesPerSignal {
		s.values[index] = value
	}
}

func deterministicReservoirIndex(seen int) int {
	x := uint64(seen)*2862933555777941757 + 3037000493
	return int(x % uint64(seen))
}

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
	values := sortedFloatCopy(set.values)
	if len(values) == 0 {
		return RobustStat{}
	}
	median := medianSorted(values)
	quality, severity, detail := sampleQuality(set.seen, len(values), set.approximated)
	low, high, hasCI := bootstrapP95CI(values)
	return RobustStat{
		Dimension:             key.Dimension,
		Name:                  key.Name,
		Metric:                key.Metric,
		Unit:                  key.Unit,
		Count:                 set.seen,
		Median:                median,
		P90:                   percentileFloatSorted(values, 0.90),
		P95:                   percentileFloatSorted(values, 0.95),
		P99:                   percentileFloatSorted(values, 0.99),
		MAD:                   medianAbsoluteDeviation(values, median),
		TrimmedMean:           trimmedMeanSorted(values, 0.10),
		Min:                   values[0],
		Max:                   values[len(values)-1],
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
	baseValues := valuesOrEmpty(baseline)
	candidateValues := valuesOrEmpty(candidate)
	baseCount := sampleCount(baseline)
	candidateCount := sampleCount(candidate)
	baseP95 := percentileFloatSorted(sortedFloatCopy(baseValues), 0.95)
	candidateP95 := percentileFloatSorted(sortedFloatCopy(candidateValues), 0.95)
	delta := candidateP95 - baseP95
	deltaPct := 0.0
	if baseP95 > 0 {
		deltaPct = delta * 100 / baseP95
	}
	cliff := cliffDelta(candidateValues, baseValues)
	effect := effectSizeLabel(cliff)
	confidence := compareConfidence(baseCount, candidateCount)
	severity := robustDeltaSeverity(deltaPct, cliff, baseCount, candidateCount)
	return RobustDelta{
		Dimension:      key.Dimension,
		Name:           key.Name,
		Metric:         key.Metric,
		Unit:           key.Unit,
		BaselineCount:  baseCount,
		CandidateCount: candidateCount,
		BaselineP95:    baseP95,
		CandidateP95:   candidateP95,
		P95Delta:       delta,
		P95DeltaPct:    deltaPct,
		CliffDelta:     cliff,
		EffectSize:     effect,
		Confidence:     confidence,
		Severity:       severity,
		Summary:        robustDeltaSummary(key, baseCount, candidateCount, baseP95, candidateP95, deltaPct, cliff),
		Recommendation: robustDeltaRecommendation(severity),
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
		return "Недостаточно данных для робастной статистики: нет распределений по маршрутам, экранам, источникам или пользовательским gauge-метрикам."
	}
	withCI := 0
	approximated := 0
	for _, stat := range stats {
		if stat.HasP95Confidence {
			withCI++
		}
		if strings.Contains(stat.SampleDetail, "выборка=") {
			approximated++
		}
	}
	return fmt.Sprintf("Посчитано %d распределений: медиана, p90/p95/p99, MAD, 10%% усеченное среднее; bootstrap-интервал для p95 есть у %d сигналов.", len(stats), withCI) + robustApproxSuffix(approximated)
}

func robustFindings(stats []RobustStat) []Finding {
	if len(stats) == 0 {
		return []Finding{{
			Severity:       "medium",
			Title:          "Недостаточно данных для робастной статистики",
			Detail:         "В логах нет достаточных распределений по маршрутам, экранам, источникам или пользовательским gauge-метрикам.",
			Recommendation: "Соберите прогон с HTTP/UI событиями или включенными Android gauge-метриками.",
		}}
	}
	findings := []Finding{{
		Severity: "ok",
		Title:    "Робастная статистика посчитана",
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
			Recommendation: "Соберите несколько повторов сценария или более длинный тестовый прогон перед выводом о регрессии.",
		})
	}
	return findings
}

func compareRobustStatus(deltas []RobustDelta) string {
	if len(deltas) == 0 {
		return "medium"
	}
	status := "ok"
	for _, delta := range deltas {
		if delta.Severity == "high" {
			return "high"
		}
		if delta.Severity == "medium" {
			status = "medium"
		}
	}
	return status
}

func compareRobustSummary(deltas []RobustDelta) string {
	if len(deltas) == 0 {
		return "Недостаточно пересекающихся распределений для робастного сравнения."
	}
	return fmt.Sprintf("Сравнено %d распределений: дельта p95, дельта Клиффа и метка доверия.", len(deltas))
}

func compareRobustFindings(deltas []RobustDelta) []Finding {
	if len(deltas) == 0 {
		return []Finding{{
			Severity:       "medium",
			Title:          "Нет распределений для робастного сравнения",
			Detail:         "База и кандидат не имеют сопоставимых выборок по маршрутам, экранам, источникам или пользовательским gauge-метрикам.",
			Recommendation: "Проверьте, что сценарии базы и кандидата проходят одни и те же экраны, маршруты и источники.",
		}}
	}
	for _, delta := range deltas {
		if delta.Severity == "high" || delta.Severity == "medium" {
			return []Finding{{
				Severity:       delta.Severity,
				Title:          "Найдена робастная регрессия",
				Detail:         delta.Summary,
				Recommendation: delta.Recommendation,
				Evidence: []string{
					fmt.Sprintf("%s · %s · %s", delta.Dimension, delta.Name, delta.Metric),
					fmt.Sprintf("дельта Клиффа %.3f, эффект: %s, доверие: %s", delta.CliffDelta, delta.EffectSize, delta.Confidence),
				},
			}}
		}
	}
	return []Finding{{
		Severity: "ok",
		Title:    "Явных робастных регрессий не найдено",
		Detail:   compareRobustSummary(deltas),
	}}
}

func robustApproxSuffix(count int) string {
	if count == 0 {
		return ""
	}
	return fmt.Sprintf(" Для %d сигналов использована ограниченная детерминированная выборка, потому что лог слишком большой.", count)
}

func sampleQuality(total, sampled int, approximated bool) (string, string, string) {
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
	if approximated {
		return quality, severity, fmt.Sprintf("сэмплов=%d, выборка=%d", total, sampled)
	}
	return quality, severity, fmt.Sprintf("сэмплов=%d", total)
}

func compareConfidence(baseCount, candidateCount int) string {
	minCount := baseCount
	if candidateCount < minCount {
		minCount = candidateCount
	}
	switch {
	case minCount >= 50:
		return "высокое"
	case minCount >= 20:
		return "среднее"
	default:
		return "низкое"
	}
}

func robustDeltaSeverity(deltaPct, cliff float64, baseCount, candidateCount int) string {
	if baseCount == 0 || candidateCount == 0 {
		return "medium"
	}
	if deltaPct >= 50 && cliff >= 0.33 {
		return "high"
	}
	if deltaPct >= 20 && cliff >= 0.147 {
		return "medium"
	}
	return "ok"
}

func robustDeltaSummary(key robustKey, baseCount, candidateCount int, baseP95, candidateP95, deltaPct, cliff float64) string {
	if baseCount == 0 {
		return fmt.Sprintf("Сигнал %s/%s появился только у кандидата: p95 %.1f %s, сэмплов=%d.", key.Name, key.Metric, candidateP95, key.Unit, candidateCount)
	}
	if candidateCount == 0 {
		return fmt.Sprintf("Сигнал %s/%s исчез у кандидата: p95 базы %.1f %s, сэмплов=%d.", key.Name, key.Metric, baseP95, key.Unit, baseCount)
	}
	return fmt.Sprintf("%s/%s: p95 изменился с %.1f до %.1f %s (%+.1f%%), дельта Клиффа %.3f.", key.Name, key.Metric, baseP95, candidateP95, key.Unit, deltaPct, cliff)
}

func robustDeltaRecommendation(severity string) string {
	switch severity {
	case "high":
		return "Проверьте источник и маршрут вокруг этого сигнала в основном отчете и в таймлайне; эффект крупный и похож на реальную регрессию."
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

func cliffDelta(candidate, baseline []float64) float64 {
	if len(candidate) == 0 || len(baseline) == 0 {
		return 0
	}
	baseSorted := sortedFloatCopy(baseline)
	var greater int64
	var less int64
	for _, value := range candidate {
		lessCount := sort.SearchFloat64s(baseSorted, value)
		greaterCount := len(baseSorted) - sort.Search(len(baseSorted), func(i int) bool {
			return baseSorted[i] > value
		})
		greater += int64(lessCount)
		less += int64(greaterCount)
	}
	return float64(greater-less) / float64(len(candidate)*len(baseline))
}

func bootstrapP95CI(values []float64) (float64, float64, bool) {
	if len(values) < 20 {
		return 0, 0, false
	}
	base := bootstrapBase(values, 512)
	const rounds = 200
	boot := make([]float64, 0, rounds)
	seed := uint64(len(values))*1469598103934665603 + 1099511628211
	for round := 0; round < rounds; round++ {
		resample := make([]float64, len(base))
		for i := range resample {
			seed = seed*2862933555777941757 + 3037000493
			resample[i] = base[int(seed%uint64(len(base)))]
		}
		sort.Float64s(resample)
		boot = append(boot, percentileFloatSorted(resample, 0.95))
	}
	sort.Float64s(boot)
	return percentileFloatSorted(boot, 0.025), percentileFloatSorted(boot, 0.975), true
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

func sortedFloatCopy(values []float64) []float64 {
	out := append([]float64(nil), values...)
	sort.Float64s(out)
	return out
}

func percentileFloatSorted(values []float64, p float64) float64 {
	if len(values) == 0 {
		return 0
	}
	index := int(math.Ceil(float64(len(values))*p)) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(values) {
		index = len(values) - 1
	}
	return values[index]
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

func valuesOrEmpty(set *robustSampleSet) []float64 {
	if set == nil {
		return nil
	}
	return set.values
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
	case "Gauge-метрика":
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
