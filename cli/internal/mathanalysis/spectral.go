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
	minPeriodicPoints             = 12
	maxAutocorrLag                = 60
	minSpectralPeakRatio          = 3.0
	minSpectralPeakConfidence     = 0.35
	minDisplayedAutocorrelation   = 0.30
	minSignificantAutocorrelation = 0.60
)

type periodicDefinition struct {
	name    string
	unit    string
	points  []float64
	present []bool
}

func buildPeriodicAnalysisWithRouteDefinitions(timeline []TimelineBucket, scale timelineScale, routeDefinitions []periodicDefinition) ([]PeriodicSignal, []SpectralPeak) {
	return buildPeriodicAnalysisWithBudget(timeline, scale, routeDefinitions, nil)
}

func buildPeriodicAnalysisWithBudget(timeline []TimelineBucket, scale timelineScale, routeDefinitions []periodicDefinition, budget *collectionBudget) ([]PeriodicSignal, []SpectralPeak) {
	var definitionsAccount, resultsAccount *collectionAccount
	if budget != nil {
		definitionsAccount = budget.account("periodic definitions")
		resultsAccount = budget.account("periodic results")
	}
	defer definitionsAccount.close()
	if !budget.chargeSpectralWork(uint64(len(timeline))*8) || !definitionsAccount.reserveItems(len(timeline), 80) || !definitionsAccount.reserve(1024) {
		return nil, nil
	}

	definitions := timelinePeriodicDefinitions(timeline)
	definitions = append(definitions, routeDefinitions...)

	signals := make([]PeriodicSignal, 0, len(definitions))
	var peaks []SpectralPeak
	for _, definition := range definitions {
		points, observed := longestPeriodicRun(definition.points, definition.present)
		if !hasNonZeroFloat(points) {
			continue
		}
		signal := analyzePeriodicSignalWithBudget(definition.name, definition.unit, scale.bucketMSOrDefault(), points, budget)
		if budget != nil && budget.err() != nil {
			return nil, nil
		}
		signal.TotalBucketCount = len(definition.points)
		signal.ObservedBucketCount = observed
		signal.Summary = periodicSignalSummary(signal)
		// Returned signal/lag/peak storage remains charged across both compare sides.
		if !resultsAccount.reserve(4096) {
			return nil, nil
		}
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
		name    string
		unit    string
		value   func(TimelineBucket) float64
		present func(TimelineBucket) bool
	}{
		{name: "Доля подтормаживаний UI", unit: "%", value: func(b TimelineBucket) float64 { return jankRate(b.UIJankyFrames, b.UIFrames) }, present: func(b TimelineBucket) bool { return b.UIFrames > 0 }},
		{name: "HTTP запросы", unit: "шт", value: func(b TimelineBucket) float64 { return float64(b.HTTPCount) }, present: httpCountPresent},
		{name: "HTTP ошибки", unit: "шт", value: func(b TimelineBucket) float64 { return float64(b.HTTPFailed) }, present: httpCountPresent},
		{name: "DNS количество", unit: "шт", value: func(b TimelineBucket) float64 { return float64(b.DNSCount) }, present: httpCountPresent},
		{name: "DNS среднее", unit: "мс", value: func(b TimelineBucket) float64 { return float64(b.DNSDurationMS) }, present: func(b TimelineBucket) bool { return b.DNSCount > 0 }},
		{name: "Количество соединений", unit: "шт", value: func(b TimelineBucket) float64 { return float64(b.ConnectCount) }, present: httpCountPresent},
		{name: "Среднее время соединения", unit: "мс", value: func(b TimelineBucket) float64 { return float64(b.ConnectDurationMS) }, present: func(b TimelineBucket) bool { return b.ConnectCount > 0 }},
	}
	out := make([]periodicDefinition, 0, len(defs))
	for _, def := range defs {
		points := make([]float64, 0, len(timeline))
		present := make([]bool, 0, len(timeline))
		for _, bucket := range timeline {
			points = append(points, def.value(bucket))
			present = append(present, def.present == nil || def.present(bucket))
		}
		out = append(out, periodicDefinition{name: def.name, unit: def.unit, points: points, present: present})
	}
	return out
}

func newRouteSeriesCollector(options analyze.Options, scale timelineScale) *routeSeriesCollector {
	return &routeSeriesCollector{
		filter: normalizeTimelineFilter(options.Filter),
		scale:  scale,
		routes: map[string]*bucketSeries{},
	}
}

type routeSeriesCollector struct {
	timeline []TimelineBucket
	account  *collectionAccount
	results  *collectionAccount
	filter   analyze.Filter
	scale    timelineScale
	routes   map[string]*bucketSeries
}

func (c *routeSeriesCollector) add(event jhlog.Event, dict map[uint64]string, symbols *mathSymbolResolver) {
	if c.filter.Active() && !mathEventMatchesFilter(event, dict, c.filter, symbols) {
		return
	}
	if event.HTTP == nil || !c.scale.hasData || c.scale.bucketCount == 0 {
		return
	}
	route := symbols.resolveRaw(dict, event.HTTP.RouteRef)
	indexValue, ok := c.scale.index(event.TimeMS)
	if !ok {
		return
	}
	index := int(indexValue)
	points := c.routes[route]
	if points == nil {
		if !c.account.reserve(128 + mathMapEntryBytes + uint64(len(route))) {
			return
		}
		points = new(bucketSeries)
		c.routes[route] = points
	}
	points.add(index, 1, c.account)
}

func (c *routeSeriesCollector) definitions(limit int) []periodicDefinition {
	if limit <= 0 {
		return nil
	}
	scratch := c.account.scratch("route candidate selection")
	defer scratch.close()
	if !scratch.reserveItems(len(c.routes), 32) {
		return nil
	}
	type routeTotal struct {
		route string
		total float64
	}
	totals := make([]routeTotal, 0, len(c.routes))
	for route, points := range c.routes {
		totals = append(totals, routeTotal{route: route, total: points.total})
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
		if !c.results.reserveItems(c.scale.bucketCount, 9) || !c.results.reserve(128+uint64(len(item.route))) {
			return nil
		}
		points := make([]float64, c.scale.bucketCount)
		var present []bool
		if c.timeline != nil {
			present = make([]bool, c.scale.bucketCount)
			for i, b := range c.timeline {
				present[i] = httpCountPresent(b)
			}
		}
		c.routes[item.route].writeDense(points)
		out = append(out, periodicDefinition{
			present: present,
			name:    "Маршрут " + item.route + " запросы",
			unit:    "шт",
			points:  points,
		})
	}
	return out
}

func analyzePeriodicSignal(name string, unit string, bucketMS uint64, points []float64) PeriodicSignal {
	return analyzePeriodicSignalWithBudget(name, unit, bucketMS, points, nil)
}

func analyzePeriodicSignalWithBudget(name, unit string, bucketMS uint64, points []float64, budget *collectionBudget) PeriodicSignal {
	signal := PeriodicSignal{
		Signal:              name,
		Unit:                unit,
		BucketMS:            bucketMS,
		SampleCount:         len(points),
		TotalBucketCount:    len(points),
		ObservedBucketCount: len(points),
	}
	if len(points) < minPeriodicPoints {
		signal.Status = "medium"
		signal.Summary = fmt.Sprintf("Недостаточно данных: нужно хотя бы %d временных интервалов, сейчас %d.", minPeriodicPoints, len(points))
		return signal
	}
	// Constant input is rejected before FFT/autocorrelation; even an inexact decimal
	// constant must not become a tiny residual with apparently perfect regularity.
	if !budget.chargeSpectralWork(uint64(len(points))) {
		return signal
	}
	constant := true
	for _, value := range points[1:] {
		if value != points[0] {
			constant = false
			break
		}
	}
	if constant {
		signal.AnalyzedSampleCount = len(points)
		signal.AnalysisBucketMS = bucketMS
		signal.SpectralEntropy = 1
		signal.Status = periodicSignalStatus(signal)
		signal.Summary = periodicSignalSummary(signal)
		return signal
	}
	// Preserve every uniformly bucketed sample. Stride sampling aliases fast signals.
	analysisPoints, analysisBucketMS := points, bucketMS
	if !budget.chargeSpectralWork(spectralWorkRequirement(len(points))) {
		return signal
	}
	var scratch *collectionAccount
	if budget != nil {
		scratch = budget.account("spectral workspace")
	}
	defer scratch.close()
	if !scratch.reserve(spectralScratchBytes(len(points))) {
		return signal
	}
	signal.AnalyzedSampleCount = len(analysisPoints)
	signal.AnalysisBucketMS = analysisBucketMS
	lags := autocorrelationLags(analysisPoints, analysisBucketMS)
	signal.TopLags = topAutocorrelationLags(lags, 3)
	signal.FirstSignificantLagMS = firstSignificantLag(lags)
	signal.DecayHalfLifeMS = decayHalfLife(lags)
	signal.Peaks, signal.SpectralEntropy = spectralPeaks(name, analysisBucketMS, analysisPoints, 3)
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
	limit := len(centered) / 3
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
		if lag.Correlation >= minDisplayedAutocorrelation {
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
		if lag.Correlation >= minSignificantAutocorrelation {
			return lag.LagMS
		}
	}
	return 0
}

func decayHalfLife(lags []AutocorrelationLag) uint64 {
	if len(lags) == 0 || lags[0].Correlation <= 0.5 {
		return 0
	}
	for _, lag := range lags {
		if lag.Correlation <= 0.5 {
			return lag.LagMS
		}
	}
	return 0
}

func spectralPeaks(signalName string, bucketMS uint64, points []float64, limit int) ([]SpectralPeak, float64) {
	windowed := hannWindow(centeredValues(points))
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
	if limit <= 0 {
		return nil, entropy
	}
	peaks := make([]SpectralPeak, 0, min(limit, len(powers)))
	bucketSeconds := float64(bucketMS) / 1000
	for index, power := range powers {
		if power <= 0 {
			continue
		}
		if index > 0 && power < powers[index-1] {
			continue
		}
		if index+1 < len(powers) && power < powers[index+1] {
			continue
		}
		k := index + 1
		frequency := float64(k) / (float64(len(points)) * bucketSeconds)
		periodMS := uint64(math.Round((1 / frequency) * 1000))
		ratio := power / background
		confidence := spectralConfidence(ratio, entropy)
		if ratio < minSpectralPeakRatio || confidence < minSpectralPeakConfidence {
			continue
		}
		candidate := SpectralPeak{
			Signal:           signalName,
			PeriodMS:         periodMS,
			FrequencyHz:      frequency,
			Power:            power,
			PeakToBackground: ratio,
			SpectralEntropy:  entropy,
			Confidence:       confidence,
		}
		position := 0
		for position < len(peaks) && !strongerSpectralPeak(candidate, peaks[position]) {
			position++
		}
		if position >= limit {
			continue
		}
		if len(peaks) < limit {
			peaks = append(peaks, SpectralPeak{})
		}
		copy(peaks[position+1:], peaks[position:len(peaks)-1])
		peaks[position] = candidate
	}

	return peaks, entropy
}

func strongerSpectralPeak(left, right SpectralPeak) bool {
	if left.Confidence != right.Confidence {
		return left.Confidence > right.Confidence
	}
	if left.Power != right.Power {
		return left.Power > right.Power
	}
	return left.FrequencyHz < right.FrequencyHz
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
	if signal.SampleCount < minPeriodicPoints {
		return "medium"
	}
	if periodicHasCrediblePattern(signal) {
		return "medium"
	}
	return "ok"
}

func periodicSignalSummary(signal PeriodicSignal) string {
	coverage := periodicCoverageSummary(signal)
	if signal.SampleCount < minPeriodicPoints {
		return fmt.Sprintf("Недостаточно данных: нужен непрерывный участок не менее %d измеренных интервалов, сейчас %d.%s", minPeriodicPoints, signal.SampleCount, coverage)
	}
	if !periodicHasCrediblePattern(signal) {
		return "Повторяемый цикл не подтвержден: значимого лага и спектрального пика с достаточным отношением к фону нет." + coverage
	}
	parts := []string{}
	if signal.FirstSignificantLagMS > 0 {
		parts = append(parts, fmt.Sprintf("первый значимый сдвиг %.1f сек", seconds(signal.FirstSignificantLagMS)))
	}
	if len(signal.Peaks) > 0 {
		parts = append(parts, fmt.Sprintf("главный спектральный период %.1f сек", seconds(signal.Peaks[0].PeriodMS)))
	}
	if signal.Approximated {
		parts = append(parts, fmt.Sprintf("ряд сокращён до %d точек с шагом около %.1f сек без изменения масштаба времени", signal.AnalyzedSampleCount, seconds(signal.AnalysisBucketMS)))
	}
	return strings.Join(parts, "; ") + coverage
}

func periodicCoverageSummary(signal PeriodicSignal) string {
	if signal.TotalBucketCount <= 0 || signal.ObservedBucketCount >= signal.TotalBucketCount {
		return ""
	}
	return fmt.Sprintf(" Из %d интервалов измерение есть в %d; анализ использует самый длинный непрерывный участок из %d интервалов, чтобы не подменять пропуски нулями.", signal.TotalBucketCount, signal.ObservedBucketCount, signal.SampleCount)
}

func periodicStatus(signals []PeriodicSignal) string {
	if !periodicAnalysisAvailable(signals) {
		return "medium"
	}
	for _, signal := range signals {
		if periodicHasCrediblePattern(signal) {
			return "medium"
		}
	}
	return "ok"
}

func periodicSummary(signals []PeriodicSignal) string {
	if len(signals) == 0 {
		return "Недостаточно данных для автокорреляции и преобразования Фурье."
	}
	patterns := 0
	analyzed := 0
	for _, signal := range signals {
		if signal.SampleCount >= minPeriodicPoints {
			analyzed++
		}
		if periodicHasCrediblePattern(signal) {
			patterns++
		}
	}
	return fmt.Sprintf("Получено %d сигналов; непрерывного участка хватает для анализа у %d, повторяющаяся последовательность с достаточной поддержкой найдена у %d. Слабые спектральные пики, похожие на шум, скрыты.", len(signals), analyzed, patterns)
}

func periodicFindings(signals []PeriodicSignal) []Finding {
	if !periodicAnalysisAvailable(signals) {
		return []Finding{{
			Severity:       "medium",
			Title:          "Недостаточно данных для периодического анализа",
			Detail:         periodicSummary(signals),
			Recommendation: "Соберите более длинный ручной или длительный прогон для поиска периодических подтормаживаний UI или сетевых циклов.",
		}}
	}
	best := signals[0]
	if periodicHasCrediblePattern(best) {
		period := best.FirstSignificantLagMS
		if len(best.Peaks) > 0 {
			period = best.Peaks[0].PeriodMS
		}
		return []Finding{{
			Severity:       "medium",
			Title:          "Найдена повторяющаяся последовательность для проверки",
			Detail:         fmt.Sprintf("%s: предполагаемый период %.1f сек. Это статистическая повторяемость, а не доказанная проблема приложения.", best.Signal, seconds(period)),
			Recommendation: "Сопоставьте период с временной шкалой, запросами конкретного маршрута, GC, диспетчером, исполнителем задач, пользовательскими метриками и сетевыми повторами.",
		}}
	}
	return []Finding{{
		Severity: "ok",
		Title:    "Сильной периодичности не найдено",
		Detail:   periodicSummary(signals),
	}}
}

func comparePeriodicStatus(baseline, candidate []PeriodicSignal) string {
	if !periodicAnalysisAvailable(baseline) || !periodicAnalysisAvailable(candidate) {
		return "medium"
	}
	if periodicPatternCount(candidate) > periodicPatternCount(baseline) {
		return "medium"
	}
	return "ok"
}

func comparePeriodicSummary(baseline, candidate []PeriodicSignal) string {
	if !periodicAnalysisAvailable(baseline) || !periodicAnalysisAvailable(candidate) {
		return "Недостаточно периодических сигналов для честного сравнения."
	}
	return fmt.Sprintf("Базовый прогон: %d подтверждённых последовательностей из %d сигналов; проверяемый прогон: %d из %d. Наличие периода само по себе не доказывает ухудшение.", periodicPatternCount(baseline), len(baseline), periodicPatternCount(candidate), len(candidate))
}

func comparePeriodicFindings(baseline, candidate []PeriodicSignal) []Finding {
	if !periodicAnalysisAvailable(baseline) || !periodicAnalysisAvailable(candidate) {
		return []Finding{{
			Severity:       "medium",
			Title:          "Недостаточно периодических сигналов для сравнения",
			Detail:         comparePeriodicSummary(baseline, candidate),
			Recommendation: "Соберите более длинные базовый и проверяемый прогоны.",
		}}
	}
	if periodicPatternCount(candidate) > periodicPatternCount(baseline) {
		return []Finding{{
			Severity:       "medium",
			Title:          "В проверяемом прогоне больше повторяющихся последовательностей",
			Detail:         comparePeriodicSummary(baseline, candidate),
			Recommendation: "Проверьте, совпадает ли новый период с таймером, периодическим опросом, повторами, GC или пользовательским действием. Если совпадения нет, не считайте период регрессией.",
		}}
	}
	return []Finding{{
		Severity: "ok",
		Title:    "Периодический анализ построен для обоих прогонов",
		Detail:   comparePeriodicSummary(baseline, candidate),
	}}
}

func longestPeriodicRun(points []float64, present []bool) ([]float64, int) {
	if len(present) != len(points) {
		return append([]float64(nil), points...), len(points)
	}
	bestStart := 0
	bestLength := 0
	currentStart := 0
	currentLength := 0
	observed := 0
	for index, available := range present {
		if available {
			observed++
			if currentLength == 0 {
				currentStart = index
			}
			currentLength++
			if currentLength > bestLength {
				bestStart = currentStart
				bestLength = currentLength
			}
			continue
		}
		currentLength = 0
	}
	return append([]float64(nil), points[bestStart:bestStart+bestLength]...), observed
}

func periodicHasCrediblePattern(signal PeriodicSignal) bool {
	return signal.FirstSignificantLagMS > 0 || topPeakConfidence(signal) >= minSpectralPeakConfidence
}

func periodicAnalysisAvailable(signals []PeriodicSignal) bool {
	for _, signal := range signals {
		if signal.SampleCount >= minPeriodicPoints {
			return true
		}
	}
	return false
}

func periodicPatternCount(signals []PeriodicSignal) int {
	count := 0
	for _, signal := range signals {
		if periodicHasCrediblePattern(signal) {
			count++
		}
	}
	return count
}

func centeredValues(points []float64) []float64 {
	out := make([]float64, len(points))
	if len(points) == 0 {
		return out
	}
	// Shift first, then compensate the sum. This preserves variation on a large DC
	// offset and makes constant input exactly zero, without a scale-dependent epsilon.
	origin := points[0]
	sum, correction := 0.0, 0.0
	for _, point := range points {
		delta := (point - origin) - correction
		next := sum + delta
		correction = (next - sum) - delta
		sum = next
	}
	mean := sum / float64(len(points))
	for i, point := range points {
		out[i] = (point - origin) - mean
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
