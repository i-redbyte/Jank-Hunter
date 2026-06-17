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
	minNetworkLoopPoints      = 12
	minNetworkLoopBursts      = 3
	maxNetworkLoopFindings    = 8
	networkLoopPeriodEpsilon  = 0.30
	networkLoopBurnThreshold  = 4
	networkLoopConfidenceWarn = 0.45
)

type networkLoopSignal struct {
	name   string
	kind   string
	route  string
	owner  string
	points []float64
	tokens map[int]map[string]int
}

type networkLoopCollector struct {
	filter     analyze.Filter
	ownerMap   map[string]string
	bucketMS   uint64
	bucketSize int
	scale      timelineScale
	signals    map[string]*networkLoopSignal
}

type metricNetworkSignal struct {
	kind   string
	route  string
	owner  string
	tokens []string
	ok     bool
}

func detectNetworkLoops(paths []string, options analyze.Options, scale timelineScale) ([]NetworkLoopFinding, error) {
	if !scale.hasData {
		return nil, nil
	}
	collector := newNetworkLoopCollector(options, scale)
	for _, path := range paths {
		if err := jhlog.StreamFile(path, func(event jhlog.Event, dict map[uint64]string) error {
			collector.add(event, dict)
			return nil
		}); err != nil {
			return nil, err
		}
	}
	return selectNetworkLoops(collector.findings()), nil
}

func newNetworkLoopCollector(options analyze.Options, scale timelineScale) *networkLoopCollector {
	return &networkLoopCollector{
		filter:     normalizeTimelineFilter(options.Filter),
		ownerMap:   options.OwnerMap,
		bucketMS:   scale.bucketMS,
		bucketSize: scale.bucketCount,
		scale:      scale,
		signals:    map[string]*networkLoopSignal{},
	}
}

func networkLoopEventTimeMS(event jhlog.Event, dict map[uint64]string, filter analyze.Filter, ownerMap map[string]string) (uint64, bool) {
	switch {
	case event.HTTP != nil:
		route := jhlog.Resolve(dict, event.HTTP.RouteID)
		owner := resolveTimelineOwner(ownerMap, dict, event.HTTP.OwnerID)
		if !networkLoopPassesFilter(filter, route, owner) {
			return 0, false
		}
		return event.TimeMS, true
	case event.Metric != nil && (event.Type == jhlog.EventCounter || event.Type == jhlog.EventGauge):
		name := jhlog.Resolve(dict, event.Metric.MetricID)
		if name == "" {
			name = fmt.Sprintf("metric:%d", event.Metric.MetricID)
		}
		network := classifyNetworkMetric(name)
		if !network.ok || !networkLoopPassesFilter(filter, network.route, network.owner) {
			return 0, false
		}
		value := float64(event.Metric.Value)
		if value <= 0 {
			return 0, false
		}
		if event.Type == jhlog.EventGauge && strings.Contains(strings.ToLower(name), "attempts") && value <= 1 {
			return 0, false
		}
		return event.TimeMS, true
	default:
		return 0, false
	}
}

func (c *networkLoopCollector) add(event jhlog.Event, dict map[uint64]string) {
	switch {
	case event.HTTP != nil:
		c.addHTTP(event, dict)
	case event.Metric != nil && (event.Type == jhlog.EventCounter || event.Type == jhlog.EventGauge):
		c.addMetric(event, dict)
	}
}

func (c *networkLoopCollector) addHTTP(event jhlog.Event, dict map[uint64]string) {
	route := jhlog.Resolve(dict, event.HTTP.RouteID)
	owner := c.resolveOwner(dict, event.HTTP.OwnerID)
	if !c.passesFilter(route, owner) {
		return
	}
	indexValue, ok := c.scale.index(event.TimeMS)
	if !ok {
		return
	}
	index := int(indexValue)
	baseTokens := []string{}
	if route != "" {
		baseTokens = append(baseTokens, "route:"+route)
	}
	if owner != "" {
		baseTokens = append(baseTokens, "owner:"+owner)
	}

	c.addPoint("route:"+route, "Маршрут "+route+" запросы", "route", route, owner, index, 1, baseTokens...)
	if owner != "" {
		c.addPoint("owner:"+owner, "Источник "+owner+" запросы", "owner", route, owner, index, 1, baseTokens...)
	}
	if event.HTTP.DNSMS > 0 {
		tokens := append([]string{"dns_high"}, baseTokens...)
		c.addPoint("dns:global", "DNS всплески", "dns", route, owner, index, 1, tokens...)
		c.addPoint("dns:route:"+route, "DNS маршрут "+route, "dns", route, owner, index, 1, tokens...)
	}
	if event.HTTP.ConnectMS > 0 {
		tokens := append([]string{"connect_high"}, baseTokens...)
		c.addPoint("connect:global", "Connect всплески", "connect", route, owner, index, 1, tokens...)
		c.addPoint("connect:route:"+route, "Connect маршрут "+route, "connect", route, owner, index, 1, tokens...)
	}
	if event.Flags&uint64(jhlog.FlagHTTPFailed) != 0 || event.HTTP.Status == jhlog.Status5xx {
		tokens := append([]string{"http_failed"}, baseTokens...)
		if event.HTTP.Status == jhlog.Status5xx {
			tokens = append(tokens, "http_5xx")
		}
		c.addPoint("failure:global", "HTTP ошибки", "failure", route, owner, index, 1, tokens...)
		c.addPoint("failure:route:"+route, "HTTP ошибки маршрут "+route, "failure", route, owner, index, 1, tokens...)
	}
}

func (c *networkLoopCollector) addMetric(event jhlog.Event, dict map[uint64]string) {
	name := jhlog.Resolve(dict, event.Metric.MetricID)
	if name == "" {
		name = fmt.Sprintf("metric:%d", event.Metric.MetricID)
	}
	network := classifyNetworkMetric(name)
	if !network.ok || !c.passesFilter(network.route, network.owner) {
		return
	}
	value := float64(event.Metric.Value)
	if value <= 0 {
		return
	}
	if event.Type == jhlog.EventGauge && strings.Contains(strings.ToLower(name), "attempts") && value <= 1 {
		return
	}
	indexValue, ok := c.scale.index(event.TimeMS)
	if !ok {
		return
	}
	index := int(indexValue)
	tokens := append([]string(nil), network.tokens...)
	if network.route != "" {
		tokens = append(tokens, "route:"+network.route)
	}
	if network.owner != "" {
		tokens = append(tokens, "owner:"+network.owner)
	}
	key := "metric:" + network.kind + ":" + network.route + ":" + network.owner + ":" + name
	title := networkMetricTitle(network.kind, network.route, network.owner)
	c.addPoint(key, title, network.kind, network.route, network.owner, index, value, tokens...)
}

func (c *networkLoopCollector) addPoint(key, name, kind, route, owner string, index int, value float64, tokens ...string) {
	if index < 0 || value <= 0 {
		return
	}
	c.ensureBucket(index)
	signal := c.signals[key]
	if signal == nil {
		signal = &networkLoopSignal{
			name:   name,
			kind:   kind,
			route:  route,
			owner:  owner,
			points: make([]float64, c.bucketSize),
			tokens: map[int]map[string]int{},
		}
		c.signals[key] = signal
	}
	if signal.route == "" && route != "" {
		signal.route = route
	}
	if signal.owner == "" && owner != "" {
		signal.owner = owner
	}
	signal.points[index] += value
	for _, token := range tokens {
		if token == "" {
			continue
		}
		bucketTokens := signal.tokens[index]
		if bucketTokens == nil {
			bucketTokens = map[string]int{}
			signal.tokens[index] = bucketTokens
		}
		bucketTokens[token]++
	}
}

func (c *networkLoopCollector) ensureBucket(index int) {
	if index < c.bucketSize {
		return
	}
	c.bucketSize = index + 1
	for _, signal := range c.signals {
		if len(signal.points) < c.bucketSize {
			signal.points = append(signal.points, make([]float64, c.bucketSize-len(signal.points))...)
		}
	}
}

func (c *networkLoopCollector) findings() []NetworkLoopFinding {
	out := make([]NetworkLoopFinding, 0, len(c.signals))
	for _, signal := range c.signals {
		if finding, ok := analyzeNetworkLoopSignal(signal, c.bucketMS); ok {
			out = append(out, finding)
		}
	}
	return out
}

func (c *networkLoopCollector) passesFilter(route, owner string) bool {
	return networkLoopPassesFilter(c.filter, route, owner)
}

func networkLoopPassesFilter(filter analyze.Filter, route, owner string) bool {
	if filter.RouteContains != "" && !metricAwareContains(route, filter.RouteContains) {
		return false
	}
	if filter.OwnerContains != "" && !metricAwareContains(owner, filter.OwnerContains) {
		return false
	}
	return true
}

func (c *networkLoopCollector) resolveOwner(dict map[uint64]string, id uint64) string {
	owner := jhlog.Resolve(dict, id)
	if len(c.ownerMap) == 0 {
		return owner
	}
	if mapped, ok := c.ownerMap[owner]; ok {
		return mapped
	}
	if hash, ok := timelineOwnerHash(owner); ok {
		if mapped, ok := c.ownerMap[hash]; ok {
			return mapped
		}
		if mapped, ok := c.ownerMap["jh:"+hash]; ok {
			return mapped
		}
	}
	return owner
}

func analyzeNetworkLoopSignal(signal *networkLoopSignal, bucketMS uint64) (NetworkLoopFinding, bool) {
	if len(signal.points) < minNetworkLoopPoints || !hasNonZeroFloat(signal.points) {
		return NetworkLoopFinding{}, false
	}
	bursts := networkLoopBurstIndexes(signal.points)
	if len(bursts) < minNetworkLoopBursts {
		return NetworkLoopFinding{}, false
	}
	periodic := analyzePeriodicSignal(signal.name, "шт", bucketMS, signal.points)
	periodMS := networkLoopPeriod(periodic, bursts, bucketMS)
	if periodMS == 0 {
		return NetworkLoopFinding{}, false
	}
	regularity := networkLoopRegularity(bursts, periodMS, bucketMS)
	if regularity < 0.45 && topPeakConfidence(periodic) < 0.25 && periodic.FirstSignificantLagMS == 0 {
		return NetworkLoopFinding{}, false
	}
	motif := networkLoopMotif(signal, bursts)
	motifScore := networkLoopMotifScore(signal, bursts, motif)
	burstScore := math.Min(1, float64(len(bursts))/6)
	autocorrScore := networkLoopAutocorrScore(periodic, periodMS)
	spectralScore := topPeakConfidence(periodic)
	confidence := burstScore*0.20 + regularity*0.30 + autocorrScore*0.25 + spectralScore*0.15 + motifScore*0.10
	if confidence < 0.35 {
		return NetworkLoopFinding{}, false
	}
	route := signal.route
	owner := signal.owner
	if route == "" {
		route = motifValue(motif, "route:")
	}
	if owner == "" {
		owner = motifValue(motif, "owner:")
	}
	burnScore := networkLoopBurn(signal.points, bursts, confidence)
	return NetworkLoopFinding{
		Route:         route,
		Owner:         owner,
		PeriodMS:      periodMS,
		Confidence:    clamp01(confidence),
		Motif:         motif,
		FirstMS:       uint64(bursts[0]) * bucketMS,
		LastMS:        uint64(bursts[len(bursts)-1]) * bucketMS,
		BurnScore:     burnScore,
		ProbableCause: networkLoopProbableCause(signal.kind, route, owner),
		Path:          networkLoopPath(signal.kind, route, owner, motif, confidence),
	}, true
}

func networkLoopBurstIndexes(points []float64) []int {
	positive := make([]float64, 0, len(points))
	for _, value := range points {
		if value > 0 {
			positive = append(positive, value)
		}
	}
	if len(positive) == 0 {
		return nil
	}
	all := sortedFloatCopy(points)
	median := medianSorted(all)
	mad := medianAbsoluteDeviation(all, median)
	positiveMedian := medianSorted(sortedFloatCopy(positive))
	threshold := math.Max(1, median+3*mad)
	threshold = math.Max(threshold, median*2)
	if median == 0 && positiveMedian > 1 {
		threshold = math.Max(threshold, positiveMedian)
	}
	out := make([]int, 0, len(points))
	for index, value := range points {
		if value >= threshold && value > 0 {
			out = append(out, index)
		}
	}
	return out
}

func networkLoopPeriod(signal PeriodicSignal, bursts []int, bucketMS uint64) uint64 {
	burstPeriod := adjacentBurstPeriod(bursts, bucketMS)
	if signal.FirstSignificantLagMS > 0 && periodClose(signal.FirstSignificantLagMS, burstPeriod) {
		return signal.FirstSignificantLagMS
	}
	if signal.FirstSignificantLagMS > 0 && burstPeriod == 0 {
		return signal.FirstSignificantLagMS
	}
	if len(signal.Peaks) > 0 {
		peakPeriod := signal.Peaks[0].PeriodMS
		if burstPeriod == 0 || periodClose(peakPeriod, burstPeriod) || signal.Peaks[0].Confidence >= 0.45 {
			return peakPeriod
		}
	}
	return burstPeriod
}

func adjacentBurstPeriod(bursts []int, bucketMS uint64) uint64 {
	if len(bursts) < 2 {
		return 0
	}
	distances := make([]float64, 0, len(bursts)-1)
	for i := 1; i < len(bursts); i++ {
		distance := bursts[i] - bursts[i-1]
		if distance > 0 {
			distances = append(distances, float64(distance))
		}
	}
	if len(distances) == 0 {
		return 0
	}
	median := medianSorted(sortedFloatCopy(distances))
	return uint64(math.Round(median)) * bucketMS
}

func periodClose(a, b uint64) bool {
	if a == 0 || b == 0 {
		return false
	}
	delta := math.Abs(float64(a) - float64(b))
	return delta/math.Max(float64(a), float64(b)) <= networkLoopPeriodEpsilon
}

func networkLoopRegularity(bursts []int, periodMS, bucketMS uint64) float64 {
	if len(bursts) < 2 || periodMS == 0 || bucketMS == 0 {
		return 0
	}
	expected := int(math.Round(float64(periodMS) / float64(bucketMS)))
	if expected <= 0 {
		return 0
	}
	var matched int
	for i := 1; i < len(bursts); i++ {
		if absInt((bursts[i]-bursts[i-1])-expected) <= 1 {
			matched++
		}
	}
	return float64(matched) / float64(len(bursts)-1)
}

func networkLoopAutocorrScore(signal PeriodicSignal, periodMS uint64) float64 {
	if signal.FirstSignificantLagMS > 0 && periodClose(signal.FirstSignificantLagMS, periodMS) {
		return 1
	}
	var best float64
	for _, lag := range signal.TopLags {
		if periodClose(lag.LagMS, periodMS) && lag.Correlation > best {
			best = lag.Correlation
		}
	}
	return clamp01(best)
}

func networkLoopMotif(signal *networkLoopSignal, bursts []int) []string {
	type tokenCount struct {
		token   string
		buckets int
		total   int
	}
	counts := map[string]*tokenCount{}
	for _, index := range bursts {
		for token, total := range signal.tokens[index] {
			item := counts[token]
			if item == nil {
				item = &tokenCount{token: token}
				counts[token] = item
			}
			item.buckets++
			item.total += total
		}
	}
	if _, ok := counts[networkLoopKindToken(signal.kind)]; !ok {
		token := networkLoopKindToken(signal.kind)
		if token != "" {
			counts[token] = &tokenCount{token: token, buckets: len(bursts), total: len(bursts)}
		}
	}
	items := make([]tokenCount, 0, len(counts))
	for _, item := range counts {
		if item.buckets >= 2 || strings.HasPrefix(item.token, "route:") || strings.HasPrefix(item.token, "owner:") {
			items = append(items, *item)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if tokenPriority(items[i].token) != tokenPriority(items[j].token) {
			return tokenPriority(items[i].token) < tokenPriority(items[j].token)
		}
		if items[i].buckets != items[j].buckets {
			return items[i].buckets > items[j].buckets
		}
		if items[i].total != items[j].total {
			return items[i].total > items[j].total
		}
		return items[i].token < items[j].token
	})
	if len(items) > 5 {
		items = items[:5]
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.token)
	}
	return out
}

func networkLoopMotifScore(signal *networkLoopSignal, bursts []int, motif []string) float64 {
	if len(bursts) == 0 || len(motif) == 0 {
		return 0
	}
	var repeated int
	for _, token := range motif {
		var buckets int
		for _, index := range bursts {
			if signal.tokens[index][token] > 0 {
				buckets++
			}
		}
		if buckets >= 2 {
			repeated++
		}
	}
	return math.Min(1, float64(repeated)/3)
}

func networkLoopBurn(points []float64, bursts []int, confidence float64) float64 {
	var total float64
	for _, index := range bursts {
		total += points[index]
	}
	return total * (1 + confidence)
}

func selectNetworkLoops(candidates []NetworkLoopFinding) []NetworkLoopFinding {
	sort.Slice(candidates, func(i, j int) bool {
		if severityRank(networkLoopFindingSeverity(candidates[i])) != severityRank(networkLoopFindingSeverity(candidates[j])) {
			return severityRank(networkLoopFindingSeverity(candidates[i])) > severityRank(networkLoopFindingSeverity(candidates[j]))
		}
		if candidates[i].Confidence != candidates[j].Confidence {
			return candidates[i].Confidence > candidates[j].Confidence
		}
		if networkLoopSpecificity(candidates[i]) != networkLoopSpecificity(candidates[j]) {
			return networkLoopSpecificity(candidates[i]) > networkLoopSpecificity(candidates[j])
		}
		if candidates[i].BurnScore != candidates[j].BurnScore {
			return candidates[i].BurnScore > candidates[j].BurnScore
		}
		return networkLoopKey(candidates[i]) < networkLoopKey(candidates[j])
	})
	seen := map[string]struct{}{}
	out := make([]NetworkLoopFinding, 0, maxNetworkLoopFindings)
	for _, candidate := range candidates {
		key := networkLoopKey(candidate)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, candidate)
		if len(out) >= maxNetworkLoopFindings {
			break
		}
	}
	return out
}

func compareNetworkLoops(baseline, candidate []NetworkLoopFinding) []NetworkLoopDelta {
	baselineByKey := map[string]NetworkLoopFinding{}
	for _, loop := range baseline {
		baselineByKey[networkLoopKey(loop)] = loop
	}
	matched := map[string]struct{}{}
	var deltas []NetworkLoopDelta
	for _, candidateLoop := range candidate {
		key := networkLoopKey(candidateLoop)
		baselineLoop, ok := baselineByKey[key]
		if !ok {
			deltas = append(deltas, appearedNetworkLoopDelta(candidateLoop))
			continue
		}
		matched[key] = struct{}{}
		if delta, ok := changedNetworkLoopDelta(baselineLoop, candidateLoop); ok {
			deltas = append(deltas, delta)
		}
	}
	for _, baselineLoop := range baseline {
		key := networkLoopKey(baselineLoop)
		if _, ok := matched[key]; ok {
			continue
		}
		if _, stillExists := baselineByKey[key]; stillExists {
			deltas = append(deltas, disappearedNetworkLoopDelta(baselineLoop))
		}
	}
	sort.Slice(deltas, func(i, j int) bool {
		if severityRank(deltas[i].Severity) != severityRank(deltas[j].Severity) {
			return severityRank(deltas[i].Severity) > severityRank(deltas[j].Severity)
		}
		if math.Abs(deltas[i].BurnDelta) != math.Abs(deltas[j].BurnDelta) {
			return math.Abs(deltas[i].BurnDelta) > math.Abs(deltas[j].BurnDelta)
		}
		return deltas[i].Summary < deltas[j].Summary
	})
	return deltas
}

func appearedNetworkLoopDelta(loop NetworkLoopFinding) NetworkLoopDelta {
	return NetworkLoopDelta{
		Route:             loop.Route,
		Owner:             loop.Owner,
		Status:            "появился",
		CandidatePeriodMS: loop.PeriodMS,
		CandidateBurn:     loop.BurnScore,
		BurnDelta:         loop.BurnScore,
		ConfidenceDelta:   loop.Confidence,
		Severity:          networkLoopFindingSeverity(loop),
		Summary:           fmt.Sprintf("У кандидата появился сетевой цикл: период %.1fs, доверие %.2f, выгорание %.1f. %s", seconds(loop.PeriodMS), loop.Confidence, loop.BurnScore, loop.ProbableCause),
	}
}

func disappearedNetworkLoopDelta(loop NetworkLoopFinding) NetworkLoopDelta {
	return NetworkLoopDelta{
		Route:            loop.Route,
		Owner:            loop.Owner,
		Status:           "исчез",
		BaselinePeriodMS: loop.PeriodMS,
		BaselineBurn:     loop.BurnScore,
		BurnDelta:        -loop.BurnScore,
		ConfidenceDelta:  -loop.Confidence,
		Severity:         "ok",
		Summary:          fmt.Sprintf("У кандидата исчез сетевой цикл из базы: период %.1fs, доверие %.2f, выгорание %.1f.", seconds(loop.PeriodMS), loop.Confidence, loop.BurnScore),
	}
}

func changedNetworkLoopDelta(baseline, candidate NetworkLoopFinding) (NetworkLoopDelta, bool) {
	burnDelta := candidate.BurnScore - baseline.BurnScore
	confidenceDelta := candidate.Confidence - baseline.Confidence
	periodChanged := !periodClose(baseline.PeriodMS, candidate.PeriodMS)
	burnChanged := math.Abs(burnDelta) >= 3 || math.Abs(percentDelta(baseline.BurnScore, candidate.BurnScore)) >= 35
	confidenceChanged := math.Abs(confidenceDelta) >= 0.15
	if !periodChanged && !burnChanged && !confidenceChanged {
		return NetworkLoopDelta{}, false
	}
	status := "изменился"
	severity := "medium"
	if burnDelta > 0 || confidenceDelta > 0.15 {
		status = "усилился"
		severity = networkLoopFindingSeverity(candidate)
		if severity == "ok" {
			severity = "medium"
		}
	}
	if burnDelta < -3 && confidenceDelta <= 0 {
		status = "ослаб"
		severity = "ok"
	}
	return NetworkLoopDelta{
		Route:             candidate.Route,
		Owner:             candidate.Owner,
		Status:            status,
		BaselinePeriodMS:  baseline.PeriodMS,
		CandidatePeriodMS: candidate.PeriodMS,
		BaselineBurn:      baseline.BurnScore,
		CandidateBurn:     candidate.BurnScore,
		BurnDelta:         burnDelta,
		ConfidenceDelta:   confidenceDelta,
		Severity:          severity,
		Summary:           fmt.Sprintf("Сетевой цикл %s: период %.1fs -> %.1fs, выгорание %.1f -> %.1f, доверие %.2f -> %.2f.", status, seconds(baseline.PeriodMS), seconds(candidate.PeriodMS), baseline.BurnScore, candidate.BurnScore, baseline.Confidence, candidate.Confidence),
	}, true
}

func networkLoopStatus(loops []NetworkLoopFinding) string {
	status := "ok"
	for _, loop := range loops {
		severity := networkLoopFindingSeverity(loop)
		if severity == "high" {
			return "high"
		}
		if severity == "medium" {
			status = "medium"
		}
	}
	return status
}

func networkLoopSummary(loops []NetworkLoopFinding) string {
	if len(loops) == 0 {
		return "Сетевых циклов по DNS, connect, reconnect, websocket и всплескам маршрутов не найдено."
	}
	return fmt.Sprintf("Найдено %d кандидатов сетевых циклов: скользящие MAD-всплески подтверждены автокорреляцией, DFT и повторяющимся паттерном.", len(loops))
}

func networkLoopFindings(loops []NetworkLoopFinding) []Finding {
	if len(loops) == 0 {
		return []Finding{{
			Severity: "ok",
			Title:    "Сетевые циклы не найдены",
			Detail:   "Повторяющихся DNS/connect/reconnect или всплесков маршрута/источника с достаточным доверием нет.",
		}}
	}
	worst := loops[0]
	return []Finding{{
		Severity:       networkLoopFindingSeverity(worst),
		Title:          "Найден сетевой цикл",
		Detail:         fmt.Sprintf("Период %.1fs, доверие %.2f, выгорание %.1f. Паттерн: %s.", seconds(worst.PeriodMS), worst.Confidence, worst.BurnScore, NetworkLoopMotifText(worst.Motif)),
		Recommendation: worst.ProbableCause,
		Evidence:       networkLoopEvidence(worst),
	}}
}

func compareNetworkLoopStatus(deltas []NetworkLoopDelta) string {
	if len(deltas) == 0 {
		return "ok"
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

func compareNetworkLoopSummary(deltas []NetworkLoopDelta) string {
	if len(deltas) == 0 {
		return "Новых, исчезнувших или заметно усилившихся сетевых циклов не найдено."
	}
	return fmt.Sprintf("Найдено %d изменений сетевых циклов: появление/исчезновение, смена периода, оценка выгорания и доверие.", len(deltas))
}

func compareNetworkLoopFindings(deltas []NetworkLoopDelta) []Finding {
	for _, delta := range deltas {
		if delta.Severity == "high" || delta.Severity == "medium" {
			return []Finding{{
				Severity:       delta.Severity,
				Title:          "Изменился сетевой цикл",
				Detail:         delta.Summary,
				Recommendation: "Проверьте маршрут, источник, DNS/connect/retry и WebSocket-события с тем же периодом; для Android смотрите OkHttp EventListener и владельца coroutine/refresh.",
			}}
		}
	}
	return []Finding{{
		Severity: "ok",
		Title:    "Регрессий сетевых циклов не найдено",
		Detail:   compareNetworkLoopSummary(deltas),
	}}
}

func networkLoopFindingSeverity(loop NetworkLoopFinding) string {
	if loop.Confidence >= 0.70 && loop.BurnScore >= 8 {
		return "high"
	}
	if loop.Confidence >= networkLoopConfidenceWarn || loop.BurnScore >= networkLoopBurnThreshold {
		return "medium"
	}
	return "ok"
}

func networkLoopEvidence(loop NetworkLoopFinding) []string {
	var evidence []string
	if loop.Route != "" {
		evidence = append(evidence, "маршрут: "+loop.Route)
	}
	if loop.Owner != "" {
		evidence = append(evidence, "источник: "+loop.Owner)
	}
	evidence = append(evidence, fmt.Sprintf("окно: %.1fs..%.1fs", seconds(loop.FirstMS), seconds(loop.LastMS)))
	if len(loop.Path.Nodes) > 0 {
		evidence = append(evidence, "путь: "+strings.Join(loop.Path.Nodes, " -> "))
	}
	return evidence
}

func classifyNetworkMetric(name string) metricNetworkSignal {
	lower := strings.ToLower(name)
	if !strings.HasPrefix(lower, "network.") && !strings.HasPrefix(lower, "websocket.") {
		return metricNetworkSignal{}
	}
	route := networkMetricRoute(lower)
	owner := ""
	if strings.HasPrefix(lower, "websocket.") {
		prefix := websocketMetricPrefix(lower)
		if prefix != "" {
			if looksLikeMetricRoute(prefix) {
				route = prefix
			} else {
				owner = prefix
			}
		}
		switch {
		case strings.Contains(lower, "reconnect.count"):
			return metricNetworkSignal{kind: "websocket", route: route, owner: owner, tokens: []string{"websocket_reconnect", "reconnect_high"}, ok: true}
		case strings.Contains(lower, "failure"):
			return metricNetworkSignal{kind: "websocket", route: route, owner: owner, tokens: []string{"websocket_failure", "http_failed"}, ok: true}
		default:
			return metricNetworkSignal{}
		}
	}
	switch {
	case strings.Contains(lower, "retry_or_reconnect"):
		return metricNetworkSignal{kind: "retry", route: route, tokens: []string{"reconnect_high"}, ok: true}
	case strings.Contains(lower, "dns.lookup") || strings.Contains(lower, "dns_attempts"):
		return metricNetworkSignal{kind: "dns", route: route, tokens: []string{"dns_high"}, ok: true}
	case strings.Contains(lower, "connect.attempt") || strings.Contains(lower, "connect_attempts") || strings.Contains(lower, "phase.connect.failure"):
		return metricNetworkSignal{kind: "connect", route: route, tokens: []string{"connect_high"}, ok: true}
	case strings.Contains(lower, "failed.count") || strings.Contains(lower, "failure.count") || strings.Contains(lower, ".failure."):
		tokens := []string{"http_failed"}
		if strings.Contains(lower, "5xx") {
			tokens = append(tokens, "http_5xx")
		}
		return metricNetworkSignal{kind: "failure", route: route, tokens: tokens, ok: true}
	case route != "" && (strings.Contains(lower, ".started.count") || strings.Contains(lower, ".finished.count")):
		return metricNetworkSignal{kind: "route", route: route, tokens: []string{"route:" + route}, ok: true}
	default:
		return metricNetworkSignal{}
	}
}

func networkMetricRoute(name string) string {
	const prefix = "network.route."
	if !strings.HasPrefix(name, prefix) {
		return ""
	}
	rest := strings.TrimPrefix(name, prefix)
	suffixes := []string{
		".dns.lookup.count",
		".connect.attempt.count",
		".retry_or_reconnect.count",
		".failed.count",
		".started.count",
		".finished.count",
		".reused_connection.count",
		".phase.",
	}
	for _, suffix := range suffixes {
		if index := strings.Index(rest, suffix); index >= 0 {
			return rest[:index]
		}
	}
	if index := strings.IndexByte(rest, '.'); index >= 0 {
		return rest[:index]
	}
	return rest
}

func websocketMetricPrefix(name string) string {
	rest := strings.TrimPrefix(name, "websocket.")
	parts := strings.Split(rest, ".")
	if len(parts) < 2 {
		return ""
	}
	if isWebsocketEventPart(parts[0]) {
		return ""
	}
	return parts[0]
}

func isWebsocketEventPart(part string) bool {
	switch part {
	case "open", "response_code", "reconnect", "message", "close_code", "closing", "closed", "failure", "lifetime_ms":
		return true
	default:
		return false
	}
}

func looksLikeMetricRoute(value string) bool {
	for _, prefix := range []string{"get_", "post_", "put_", "patch_", "delete_", "head_", "options_"} {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}

func networkMetricTitle(kind, route, owner string) string {
	target := ""
	if route != "" {
		target = " " + route
	} else if owner != "" {
		target = " " + owner
	}
	switch kind {
	case "dns":
		return "DNS-метрика" + target
	case "connect":
		return "Метрика соединения" + target
	case "retry":
		return "Метрика retry/reconnect" + target
	case "websocket":
		return "WebSocket-метрика" + target
	case "failure":
		return "Метрика сетевой ошибки" + target
	case "route":
		return "Метрика маршрута" + target
	default:
		return "Сетевая метрика" + target
	}
}

func networkLoopKindToken(kind string) string {
	switch kind {
	case "dns":
		return "dns_high"
	case "connect":
		return "connect_high"
	case "retry":
		return "reconnect_high"
	case "websocket":
		return "websocket_reconnect"
	case "failure":
		return "http_failed"
	default:
		return ""
	}
}

func networkLoopProbableCause(kind, route, owner string) string {
	target := networkLoopTarget(route, owner)
	switch kind {
	case "dns":
		return "Вероятная причина: периодический DNS resolve или потеря DNS cache" + target + ". Проверьте TTL/cache, OkHttp DNS и сетевой слой."
	case "connect":
		return "Вероятная причина: повторные connect/TLS попытки" + target + ". Проверьте connection pool, прокси/VPN, TLS и reachability."
	case "retry":
		return "Вероятная причина: retry/reconnect контур" + target + ". Проверьте backoff, cancellation и владельца refresh."
	case "websocket":
		return "Вероятная причина: шторм WebSocket-переподключений" + target + ". Проверьте lifecycle, heartbeat и backoff переподключения."
	case "failure":
		return "Вероятная причина: повторяющиеся сетевые ошибки" + target + ". Проверьте статус backend, обработку IOException и retry policy."
	case "owner":
		return "Вероятная причина: источник регулярно запускает сетевую работу" + target + ". Проверьте coroutine/job scheduling и debounce."
	case "route":
		return "Вероятная причина: периодический polling или шквал запросов" + target + ". Проверьте таймеры, refresh и cache policy."
	default:
		return "Вероятная причина: повторяющийся сетевой паттерн" + target + "."
	}
}

func networkLoopTarget(route, owner string) string {
	parts := []string{}
	if route != "" {
		parts = append(parts, "маршрута "+route)
	}
	if owner != "" {
		parts = append(parts, "источника "+owner)
	}
	if len(parts) == 0 {
		return ""
	}
	return " для " + strings.Join(parts, " и ")
}

func networkLoopPath(kind, route, owner string, motif []string, confidence float64) GraphPath {
	nodes := []string{"симптом: сетевой цикл"}
	if owner != "" {
		nodes = append(nodes, "источник: "+owner)
	}
	if route != "" {
		nodes = append(nodes, "маршрут: "+route)
	}
	nodes = append(nodes, networkLoopKindLabel(kind))
	for _, token := range motif {
		label := networkLoopTokenLabel(token)
		if label != "" && !stringSliceContains(nodes, label) {
			nodes = append(nodes, label)
		}
	}
	to := nodes[len(nodes)-1]
	return GraphPath{
		From:       nodes[0],
		To:         to,
		Nodes:      nodes,
		Cost:       1 - clamp01(confidence),
		Confidence: clamp01(confidence),
	}
}

func networkLoopKey(loop NetworkLoopFinding) string {
	return loop.Route + "|" + loop.Owner + "|" + motifClass(loop.Motif)
}

func motifClass(motif []string) string {
	for _, token := range motif {
		switch token {
		case "dns_high", "connect_high", "reconnect_high", "websocket_reconnect", "websocket_failure", "http_5xx", "http_failed":
			return token
		}
	}
	if len(motif) > 0 {
		return motif[0]
	}
	return "unknown"
}

func networkLoopSpecificity(loop NetworkLoopFinding) int {
	score := 0
	if loop.Route != "" {
		score++
	}
	if loop.Owner != "" {
		score++
	}
	return score
}

func motifValue(motif []string, prefix string) string {
	for _, token := range motif {
		if strings.HasPrefix(token, prefix) {
			return strings.TrimPrefix(token, prefix)
		}
	}
	return ""
}

func tokenPriority(token string) int {
	switch {
	case token == "dns_high":
		return 0
	case token == "connect_high":
		return 1
	case token == "reconnect_high" || token == "websocket_reconnect":
		return 2
	case token == "websocket_failure" || token == "http_5xx" || token == "http_failed":
		return 3
	case strings.HasPrefix(token, "route:"):
		return 4
	case strings.HasPrefix(token, "owner:"):
		return 5
	default:
		return 6
	}
}

func metricAwareContains(value, filter string) bool {
	if timelineContainsFilter(value, filter) {
		return true
	}
	normalized := normalizeMetricFragment(filter)
	return normalized != "" && strings.Contains(strings.ToLower(value), normalized)
}

func normalizeMetricFragment(value string) string {
	var b strings.Builder
	lastUnderscore := false
	for _, r := range strings.ToLower(value) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore && b.Len() > 0 {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	return strings.Trim(b.String(), "_")
}

func clamp01(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func stringSliceContains(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func NetworkLoopMotifText(tokens []string) string {
	if len(tokens) == 0 {
		return "повторяющийся паттерн не выделен"
	}
	labels := make([]string, 0, len(tokens))
	for _, token := range tokens {
		labels = append(labels, networkLoopTokenLabel(token))
	}
	return strings.Join(labels, ", ")
}

func networkLoopTokenLabel(token string) string {
	switch token {
	case "dns_high":
		return "DNS-всплеск"
	case "connect_high":
		return "connect-всплеск"
	case "reconnect_high":
		return "retry/reconnect-всплеск"
	case "websocket_reconnect":
		return "websocket-переподключение"
	case "websocket_failure":
		return "websocket-ошибка"
	case "http_5xx":
		return "HTTP 5xx"
	case "http_failed":
		return "HTTP-ошибка"
	}
	if strings.HasPrefix(token, "route:") {
		return "маршрут: " + strings.TrimPrefix(token, "route:")
	}
	if strings.HasPrefix(token, "owner:") {
		return "источник: " + strings.TrimPrefix(token, "owner:")
	}
	return token
}

func networkLoopKindLabel(kind string) string {
	switch kind {
	case "dns":
		return "фаза: DNS"
	case "connect":
		return "фаза: connect"
	case "retry":
		return "фаза: retry/reconnect"
	case "websocket":
		return "фаза: websocket"
	case "failure":
		return "фаза: сетевые ошибки"
	case "owner":
		return "фаза: всплеск источника"
	case "route":
		return "фаза: шквал запросов"
	default:
		return "фаза: сеть"
	}
}
