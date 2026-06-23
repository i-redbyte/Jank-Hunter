package mathanalysis

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

const (
	minPeriodicPoints = 12
	maxSpectralPoints = 2048
	maxAutocorrLag    = 60
)

type periodicDefinition struct {
	name   string
	unit   string
	points []float64
}

func buildPeriodicAnalysisWithRouteDefinitions(timeline []TimelineBucket, scale timelineScale, routeDefinitions []periodicDefinition) ([]PeriodicSignal, []SpectralPeak) {
	definitions := timelinePeriodicDefinitions(timeline)
	definitions = append(definitions, routeDefinitions...)

	signals := make([]PeriodicSignal, 0, len(definitions))
	var peaks []SpectralPeak
	for _, definition := range definitions {
		if !hasNonZeroFloat(definition.points) {
			continue
		}
		signal := analyzePeriodicSignal(definition.name, definition.unit, scale.bucketMSOrDefault(), definition.points)
		signals = append(signals, signal)
		peaks = append(peaks, signal.Peaks...)
	}
	sort.Slice(signals, func(i, j int) bool {
		if periodicSignalRank(signals[i]) != periodicSignalRank(signals[j]) {
			return periodicSignalRank(signals[i]) > periodicSignalRank(signals[j])
		}
		if topPeakConfidence(signals[i]) != topPeakConfidence(signals[j]) {
			return topPeakConfidence(signals[i]) > topPeakConfidence(signals[j])
		}
		return signals[i].Signal < signals[j].Signal
	})
	return signals, peaks
}

func timelinePeriodicDefinitions(timeline []TimelineBucket) []periodicDefinition {
	defs := []struct {
		name  string
		unit  string
		value func(TimelineBucket) float64
	}{
		{name: "Доля подтормаживаний UI", unit: "%", value: func(b TimelineBucket) float64 { return jankRate(b.UIJankyFrames, b.UIFrames) }},
		{name: "HTTP запросы", unit: "шт", value: func(b TimelineBucket) float64 { return float64(b.HTTPCount) }},
		{name: "HTTP ошибки", unit: "шт", value: func(b TimelineBucket) float64 { return float64(b.HTTPFailed) }},
		{name: "DNS количество", unit: "шт", value: func(b TimelineBucket) float64 { return float64(b.DNSCount) }},
		{name: "DNS среднее", unit: "мс", value: func(b TimelineBucket) float64 { return float64(b.DNSDurationMS) }},
		{name: "Количество соединений", unit: "шт", value: func(b TimelineBucket) float64 { return float64(b.ConnectCount) }},
		{name: "Среднее время соединения", unit: "мс", value: func(b TimelineBucket) float64 { return float64(b.ConnectDurationMS) }},
	}
	out := make([]periodicDefinition, 0, len(defs))
	for _, def := range defs {
		points := make([]float64, 0, len(timeline))
		for _, bucket := range timeline {
			points = append(points, def.value(bucket))
		}
		out = append(out, periodicDefinition{name: def.name, unit: def.unit, points: points})
	}
	return out
}

func newRouteSeriesCollector(options analyze.Options, scale timelineScale) *routeSeriesCollector {
	return &routeSeriesCollector{
		filter:   normalizeTimelineFilter(options.Filter),
		ownerMap: options.OwnerMap,
		scale:    scale,
		routes:   map[string][]float64{},
	}
}

type routeSeriesCollector struct {
	filter   analyze.Filter
	ownerMap map[string]string
	scale    timelineScale
	routes   map[string][]float64
}

func (c *routeSeriesCollector) add(event jhlog.Event, dict map[uint64]string) {
	if event.HTTP == nil || !c.scale.hasData || c.scale.bucketCount == 0 {
		return
	}
	route := jhlog.Resolve(dict, event.HTTP.RouteID)
	owner := c.resolveOwner(dict, event.HTTP.OwnerID)
	if !timelineContainsFilter(route, c.filter.RouteContains) || !timelineContainsFilter(owner, c.filter.OwnerContains) {
		return
	}
	indexValue, ok := c.scale.index(event.TimeMS)
	if !ok {
		return
	}
	index := int(indexValue)
	points := c.routes[route]
	if points == nil {
		points = make([]float64, c.scale.bucketCount)
		c.routes[route] = points
	}
	points[index]++
}

func (c *routeSeriesCollector) definitions(limit int) []periodicDefinition {
	type routeTotal struct {
		route string
		total float64
	}
	totals := make([]routeTotal, 0, len(c.routes))
	for route, points := range c.routes {
		var total float64
		for _, point := range points {
			total += point
		}
		totals = append(totals, routeTotal{route: route, total: total})
	}
	sort.Slice(totals, func(i, j int) bool {
		if totals[i].total != totals[j].total {
			return totals[i].total > totals[j].total
		}
		return totals[i].route < totals[j].route
	})
	if len(totals) > limit {
		totals = totals[:limit]
	}
	out := make([]periodicDefinition, 0, len(totals))
	for _, item := range totals {
		out = append(out, periodicDefinition{
			name:   "Маршрут " + item.route + " запросы",
			unit:   "шт",
			points: c.routes[item.route],
		})
	}
	return out
}

func (c *routeSeriesCollector) resolveOwner(dict map[uint64]string, id uint64) string {
	return analyze.ResolveOwnerAlias(c.ownerMap, jhlog.Resolve(dict, id))
}

func analyzePeriodicSignal(name string, unit string, bucketMS uint64, points []float64) PeriodicSignal {
	signal := PeriodicSignal{
		Signal:      name,
		Unit:        unit,
		BucketMS:    bucketMS,
		SampleCount: len(points),
	}
	if len(points) < minPeriodicPoints {
		signal.Status = "medium"
		signal.Summary = fmt.Sprintf("Недостаточно данных: нужно хотя бы %d временных интервалов, сейчас %d.", minPeriodicPoints, len(points))
		return signal
	}
	analysisPoints := downsampleFloat(points, maxSpectralPoints)
	signal.Approximated = len(analysisPoints) < len(points)
	lags := autocorrelationLags(analysisPoints, bucketMS)
	signal.TopLags = topAutocorrelationLags(lags, 3)
	signal.FirstSignificantLagMS = firstSignificantLag(lags)
	signal.DecayHalfLifeMS = decayHalfLife(lags)
	signal.Peaks, signal.SpectralEntropy = spectralPeaks(name, bucketMS, analysisPoints, 3)
	signal.Status = periodicSignalStatus(signal)
	signal.Summary = periodicSignalSummary(signal)
	return signal
}

func autocorrelationLags(points []float64, bucketMS uint64) []AutocorrelationLag {
	if len(points) < 2 {
		return nil
	}
	centered := centeredValues(points)
	var denominator float64
	for _, value := range centered {
		denominator += value * value
	}
	if denominator == 0 {
		return nil
	}
	limit := len(centered) / 2
	if limit > maxAutocorrLag {
		limit = maxAutocorrLag
	}
	lags := make([]AutocorrelationLag, 0, limit)
	for lag := 1; lag <= limit; lag++ {
		var numerator float64
		for i := 0; i+lag < len(centered); i++ {
			numerator += centered[i] * centered[i+lag]
		}
		lags = append(lags, AutocorrelationLag{
			LagMS:       uint64(lag) * bucketMS,
			Correlation: numerator / denominator,
		})
	}
	return lags
}

func topAutocorrelationLags(lags []AutocorrelationLag, limit int) []AutocorrelationLag {
	positive := make([]AutocorrelationLag, 0, len(lags))
	for _, lag := range lags {
		if lag.Correlation > 0 {
			positive = append(positive, lag)
		}
	}
	sort.Slice(positive, func(i, j int) bool {
		if positive[i].Correlation != positive[j].Correlation {
			return positive[i].Correlation > positive[j].Correlation
		}
		return positive[i].LagMS < positive[j].LagMS
	})
	if len(positive) > limit {
		positive = positive[:limit]
	}
	return positive
}

func firstSignificantLag(lags []AutocorrelationLag) uint64 {
	for _, lag := range lags {
		if lag.Correlation >= 0.55 {
			return lag.LagMS
		}
	}
	return 0
}

func decayHalfLife(lags []AutocorrelationLag) uint64 {
	for _, lag := range lags {
		if lag.Correlation <= 0.5 {
			return lag.LagMS
		}
	}
	return 0
}

func spectralPeaks(signalName string, bucketMS uint64, points []float64, limit int) ([]SpectralPeak, float64) {
	windowed := hannWindow(detrendMean(points))
	powers := dftPowers(windowed)
	entropy := spectralEntropy(powers)
	if len(powers) == 0 {
		return nil, entropy
	}
	background := medianSorted(sortedFloatCopy(powers))
	if background <= 0 {
		background = meanFloat(powers)
	}
	if background <= 0 {
		background = 1
	}
	peaks := make([]SpectralPeak, 0, len(powers))
	bucketSeconds := float64(bucketMS) / 1000
	for index, power := range powers {
		if power <= 0 {
			continue
		}
		k := index + 1
		frequency := float64(k) / (float64(len(points)) * bucketSeconds)
		periodMS := uint64(math.Round((1 / frequency) * 1000))
		ratio := power / background
		peaks = append(peaks, SpectralPeak{
			Signal:           signalName,
			PeriodMS:         periodMS,
			FrequencyHz:      frequency,
			Power:            power,
			PeakToBackground: ratio,
			SpectralEntropy:  entropy,
			Confidence:       spectralConfidence(ratio, entropy),
		})
	}
	sort.Slice(peaks, func(i, j int) bool {
		if peaks[i].Confidence != peaks[j].Confidence {
			return peaks[i].Confidence > peaks[j].Confidence
		}
		return peaks[i].Power > peaks[j].Power
	})
	if len(peaks) > limit {
		peaks = peaks[:limit]
	}
	return peaks, entropy
}

func dftPowers(points []float64) []float64 {
	n := len(points)
	if n < 2 {
		return nil
	}
	powers := make([]float64, 0, n/2)
	for k := 1; k <= n/2; k++ {
		var realPart float64
		var imagPart float64
		for t, value := range points {
			angle := -2 * math.Pi * float64(k*t) / float64(n)
			realPart += value * math.Cos(angle)
			imagPart += value * math.Sin(angle)
		}
		powers = append(powers, realPart*realPart+imagPart*imagPart)
	}
	return powers
}

func detrendMean(points []float64) []float64 {
	mean := meanFloat(points)
	out := make([]float64, 0, len(points))
	for _, point := range points {
		out = append(out, point-mean)
	}
	return out
}

func hannWindow(points []float64) []float64 {
	if len(points) <= 1 {
		return append([]float64(nil), points...)
	}
	out := make([]float64, 0, len(points))
	for index, point := range points {
		weight := 0.5 * (1 - math.Cos(2*math.Pi*float64(index)/float64(len(points)-1)))
		out = append(out, point*weight)
	}
	return out
}

func spectralEntropy(powers []float64) float64 {
	var total float64
	for _, power := range powers {
		total += power
	}
	if total <= 0 || len(powers) <= 1 {
		return 1
	}
	var entropy float64
	for _, power := range powers {
		if power <= 0 {
			continue
		}
		p := power / total
		entropy -= p * math.Log2(p)
	}
	return entropy / math.Log2(float64(len(powers)))
}

func spectralConfidence(ratio float64, entropy float64) float64 {
	ratioScore := math.Min(1, math.Max(0, (ratio-2)/8))
	entropyScore := math.Min(1, math.Max(0, 1-entropy))
	return ratioScore*0.65 + entropyScore*0.35
}

func periodicSignalStatus(signal PeriodicSignal) string {
	if len(signal.Peaks) == 0 && signal.FirstSignificantLagMS == 0 {
		return "medium"
	}
	if topPeakConfidence(signal) >= 0.6 || signal.FirstSignificantLagMS > 0 {
		return "ok"
	}
	return "medium"
}

func periodicSignalSummary(signal PeriodicSignal) string {
	if signal.Status == "medium" && len(signal.Peaks) == 0 {
		return "Недостаточно выраженных периодических пиков: сигнал похож на шум или слишком короткий."
	}
	parts := []string{}
	if signal.FirstSignificantLagMS > 0 {
		parts = append(parts, fmt.Sprintf("первый значимый лаг %.1fs", seconds(signal.FirstSignificantLagMS)))
	}
	if len(signal.Peaks) > 0 {
		parts = append(parts, fmt.Sprintf("главный спектральный период %.1fs", seconds(signal.Peaks[0].PeriodMS)))
	}
	if signal.Approximated {
		parts = append(parts, "преобразование Фурье посчитано по ограниченной равномерной выборке")
	}
	return strings.Join(parts, "; ")
}

func periodicStatus(signals []PeriodicSignal) string {
	if len(signals) == 0 {
		return "medium"
	}
	for _, signal := range signals {
		if topPeakConfidence(signal) >= 0.6 || signal.FirstSignificantLagMS > 0 {
			return "ok"
		}
	}
	return "medium"
}

func periodicSummary(signals []PeriodicSignal) string {
	if len(signals) == 0 {
		return "Недостаточно данных для автокорреляции и преобразования Фурье."
	}
	peaks := 0
	for _, signal := range signals {
		peaks += len(signal.Peaks)
	}
	return fmt.Sprintf("Проанализировано %d сигналов: лаги автокорреляции, преобразование Фурье с окном Ханна, спектральная энтропия и %d спектральных пиков.", len(signals), peaks)
}

func periodicFindings(signals []PeriodicSignal) []Finding {
	if len(signals) == 0 {
		return []Finding{{
			Severity:       "medium",
			Title:          "Недостаточно данных для периодического анализа",
			Detail:         "Нужна запись хотя бы из нескольких десятков временных интервалов; короткий smoke-прогон почти не дает устойчивого спектра.",
			Recommendation: "Соберите более длинный ручной или длительный прогон для поиска периодических подтормаживаний UI или сетевых циклов.",
		}}
	}
	best := signals[0]
	if len(best.Peaks) > 0 && topPeakConfidence(best) >= 0.4 {
		return []Finding{{
			Severity:       "ok",
			Title:          "Найден периодический кандидат",
			Detail:         fmt.Sprintf("%s: главный период %.1fs, пик/фон %.2f, энтропия %.2f.", best.Signal, seconds(best.Peaks[0].PeriodMS), best.Peaks[0].PeakToBackground, best.SpectralEntropy),
			Recommendation: "Сопоставьте период с таймлайном, запросами конкретного маршрута, GC, диспетчером, исполнителем задач, gauge-метриками и сетевыми повторами.",
		}}
	}
	return []Finding{{
		Severity: "ok",
		Title:    "Сильной периодичности не найдено",
		Detail:   periodicSummary(signals),
	}}
}

func comparePeriodicStatus(baseline, candidate []PeriodicSignal) string {
	if len(baseline) == 0 || len(candidate) == 0 {
		return "medium"
	}
	return "ok"
}

func comparePeriodicSummary(baseline, candidate []PeriodicSignal) string {
	if len(baseline) == 0 || len(candidate) == 0 {
		return "Недостаточно периодических сигналов для честного сравнения."
	}
	return fmt.Sprintf("База: %d сигналов, кандидат: %d сигналов. Сетевые периоды дополнительно сопоставляются в разделе сетевых циклов.", len(baseline), len(candidate))
}

func comparePeriodicFindings(baseline, candidate []PeriodicSignal) []Finding {
	if len(baseline) == 0 || len(candidate) == 0 {
		return []Finding{{
			Severity:       "medium",
			Title:          "Недостаточно периодических сигналов для сравнения",
			Detail:         comparePeriodicSummary(baseline, candidate),
			Recommendation: "Соберите более длинные прогоны базы и кандидата.",
		}}
	}
	return []Finding{{
		Severity: "ok",
		Title:    "Периодический анализ построен для базы и кандидата",
		Detail:   comparePeriodicSummary(baseline, candidate),
	}}
}

func downsampleFloat(points []float64, limit int) []float64 {
	if len(points) <= limit {
		return append([]float64(nil), points...)
	}
	out := make([]float64, 0, limit)
	step := float64(len(points)-1) / float64(limit-1)
	for i := 0; i < limit; i++ {
		out = append(out, points[int(math.Round(float64(i)*step))])
	}
	return out
}

func centeredValues(points []float64) []float64 {
	mean := meanFloat(points)
	out := make([]float64, 0, len(points))
	for _, point := range points {
		out = append(out, point-mean)
	}
	return out
}

func meanFloat(points []float64) float64 {
	if len(points) == 0 {
		return 0
	}
	var total float64
	for _, point := range points {
		total += point
	}
	return total / float64(len(points))
}

func hasNonZeroFloat(points []float64) bool {
	for _, point := range points {
		if point != 0 {
			return true
		}
	}
	return false
}

func topPeakConfidence(signal PeriodicSignal) float64 {
	if len(signal.Peaks) == 0 {
		return 0
	}
	return signal.Peaks[0].Confidence
}

func periodicSignalRank(signal PeriodicSignal) int {
	if topPeakConfidence(signal) >= 0.6 || signal.FirstSignificantLagMS > 0 {
		return 2
	}
	if len(signal.Peaks) > 0 {
		return 1
	}
	return 0
}
