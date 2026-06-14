package mathanalysis

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

const DefaultBucketMS uint64 = 1000

type timelineCollector struct {
	filter   analyze.Filter
	ownerMap map[string]string
	bucketMS uint64
	buckets  map[uint64]*timelineBucketAgg
	maxIndex uint64
	hasData  bool
}

type timelineBucketAgg struct {
	bucket TimelineBucket

	httpDurations []uint64
	httpTotalMS   uint64
	ttfbTotalMS   uint64
	ttfbCount     uint64

	dnsTotalMS     uint64
	connectTotalMS uint64

	hasMemoryPSS       bool
	hasAvailableMemory bool

	routes   map[string]int
	owners   map[string]int
	screens  map[string]int
	networks map[string]int
}

type timelineStreamState struct {
	lastRx         uint64
	lastTx         uint64
	hasRxTx        bool
	currentNetwork string
}

func buildTimeline(paths []string, options analyze.Options) ([]TimelineBucket, []Series, error) {
	collector := &timelineCollector{
		filter:   normalizeTimelineFilter(options.Filter),
		ownerMap: options.OwnerMap,
		bucketMS: DefaultBucketMS,
		buckets:  map[uint64]*timelineBucketAgg{},
	}
	for _, path := range paths {
		state := &timelineStreamState{}
		if err := jhlog.StreamFile(path, func(event jhlog.Event, dict map[uint64]string) error {
			collector.add(event, dict, state)
			return nil
		}); err != nil {
			return nil, nil, err
		}
	}
	timeline := collector.finish()
	return timeline, timelineSeries(timeline, collector.bucketMS), nil
}

func normalizeTimelineFilter(filter analyze.Filter) analyze.Filter {
	return analyze.Filter{
		RouteContains:  strings.ToLower(filter.RouteContains),
		ScreenContains: strings.ToLower(filter.ScreenContains),
		OwnerContains:  strings.ToLower(filter.OwnerContains),
	}
}

func (c *timelineCollector) add(event jhlog.Event, dict map[uint64]string, state *timelineStreamState) {
	switch {
	case event.HTTP != nil:
		route := jhlog.Resolve(dict, event.HTTP.RouteID)
		owner := c.resolveOwner(dict, event.HTTP.OwnerID)
		if !timelineContainsFilter(route, c.filter.RouteContains) || !timelineContainsFilter(owner, c.filter.OwnerContains) {
			return
		}
		agg := c.bucket(event.TimeMS)
		agg.addSample(agg.routes, route)
		agg.addSample(agg.owners, owner)
		agg.bucket.HTTPCount++
		agg.httpDurations = append(agg.httpDurations, event.HTTP.DurationMS)
		agg.httpTotalMS += event.HTTP.DurationMS
		if event.Flags&uint64(jhlog.FlagHTTPFailed) != 0 || event.HTTP.Status == jhlog.Status5xx {
			agg.bucket.HTTPFailed++
		}
		if event.HTTP.DNSMS > 0 {
			agg.bucket.DNSCount++
			agg.dnsTotalMS += event.HTTP.DNSMS
		}
		if event.HTTP.ConnectMS > 0 {
			agg.bucket.ConnectCount++
			agg.connectTotalMS += event.HTTP.ConnectMS
		}
		if event.HTTP.TTFBMS > 0 {
			agg.ttfbTotalMS += event.HTTP.TTFBMS
			agg.ttfbCount++
		}
	case event.UIWindow != nil:
		screen := jhlog.Resolve(dict, event.UIWindow.ScreenID)
		if !timelineContainsFilter(screen, c.filter.ScreenContains) {
			return
		}
		agg := c.bucket(event.TimeMS)
		agg.addSample(agg.screens, screen)
		agg.bucket.UIFrames += event.UIWindow.FrameCount
		agg.bucket.UIJankyFrames += event.UIWindow.JankCount
	case event.Stall != nil:
		owner := c.resolveOwner(dict, event.Stall.OwnerID)
		if !timelineContainsFilter(owner, c.filter.OwnerContains) {
			return
		}
		agg := c.bucket(event.TimeMS)
		agg.addSample(agg.owners, owner)
		agg.bucket.StallCount++
		if event.Stall.DurationMS > agg.bucket.StallMaxMS {
			agg.bucket.StallMaxMS = event.Stall.DurationMS
		}
	case event.Memory != nil:
		agg := c.bucket(event.TimeMS)
		if !agg.hasMemoryPSS || event.Memory.PSSKB > agg.bucket.MemoryPSSKB {
			agg.bucket.MemoryPSSKB = event.Memory.PSSKB
			agg.hasMemoryPSS = true
		}
	case event.Context != nil:
		agg := c.bucket(event.TimeMS)
		state.currentNetwork = jhlog.NetworkName(event.Context.Network)
		agg.addSample(agg.networks, state.currentNetwork)
		if !agg.hasAvailableMemory || event.Context.AvailMemoryKB < agg.bucket.AvailableMemoryKB {
			agg.bucket.AvailableMemoryKB = event.Context.AvailMemoryKB
			agg.hasAvailableMemory = true
		}
		if state.hasRxTx {
			if event.Context.RxBytes >= state.lastRx {
				agg.bucket.TrafficRxBytes += event.Context.RxBytes - state.lastRx
			}
			if event.Context.TxBytes >= state.lastTx {
				agg.bucket.TrafficTxBytes += event.Context.TxBytes - state.lastTx
			}
		}
		state.lastRx = event.Context.RxBytes
		state.lastTx = event.Context.TxBytes
		state.hasRxTx = true
	}
}

func (c *timelineCollector) bucket(timeMS uint64) *timelineBucketAgg {
	index := timeMS / c.bucketMS
	agg := c.buckets[index]
	if agg == nil {
		startMS := index * c.bucketMS
		agg = &timelineBucketAgg{
			bucket: TimelineBucket{
				StartMS: startMS,
				EndMS:   startMS + c.bucketMS,
			},
			routes:   map[string]int{},
			owners:   map[string]int{},
			screens:  map[string]int{},
			networks: map[string]int{},
		}
		c.buckets[index] = agg
	}
	if !c.hasData || index > c.maxIndex {
		c.maxIndex = index
	}
	c.hasData = true
	return agg
}

func (c *timelineCollector) finish() []TimelineBucket {
	if !c.hasData {
		return nil
	}
	out := make([]TimelineBucket, 0, c.maxIndex+1)
	for index := uint64(0); index <= c.maxIndex; index++ {
		startMS := index * c.bucketMS
		bucket := TimelineBucket{StartMS: startMS, EndMS: startMS + c.bucketMS}
		if agg := c.buckets[index]; agg != nil {
			bucket = agg.bucket
			if bucket.HTTPCount > 0 {
				bucket.HTTPAvgDurationMS = agg.httpTotalMS / uint64(bucket.HTTPCount)
				sort.Slice(agg.httpDurations, func(i, j int) bool { return agg.httpDurations[i] < agg.httpDurations[j] })
				bucket.HTTPP95DurationMS = percentileSorted(agg.httpDurations, 0.95)
			}
			if bucket.DNSCount > 0 {
				bucket.DNSDurationMS = agg.dnsTotalMS / uint64(bucket.DNSCount)
			}
			if bucket.ConnectCount > 0 {
				bucket.ConnectDurationMS = agg.connectTotalMS / uint64(bucket.ConnectCount)
			}
			if agg.ttfbCount > 0 {
				bucket.TTFBMS = agg.ttfbTotalMS / agg.ttfbCount
			}
			bucket.RouteSample = topSample(agg.routes)
			bucket.OwnerSample = topSample(agg.owners)
			bucket.ScreenSample = topSample(agg.screens)
			bucket.NetworkSample = topSample(agg.networks)
		}
		out = append(out, bucket)
	}
	return out
}

func (b *timelineBucketAgg) addSample(samples map[string]int, value string) {
	if value != "" {
		samples[value]++
	}
}

func topSample(samples map[string]int) string {
	var best string
	var bestCount int
	for value, count := range samples {
		if count > bestCount || (count == bestCount && value < best) {
			best = value
			bestCount = count
		}
	}
	return best
}

func timelineSeries(timeline []TimelineBucket, bucketMS uint64) []Series {
	definitions := []struct {
		name  string
		unit  string
		value func(TimelineBucket) float64
	}{
		{name: "HTTP запросы", unit: "шт", value: func(b TimelineBucket) float64 { return float64(b.HTTPCount) }},
		{name: "HTTP ошибки", unit: "шт", value: func(b TimelineBucket) float64 { return float64(b.HTTPFailed) }},
		{name: "HTTP p95", unit: "ms", value: func(b TimelineBucket) float64 { return float64(b.HTTPP95DurationMS) }},
		{name: "HTTP среднее", unit: "ms", value: func(b TimelineBucket) float64 { return float64(b.HTTPAvgDurationMS) }},
		{name: "DNS количество", unit: "шт", value: func(b TimelineBucket) float64 { return float64(b.DNSCount) }},
		{name: "DNS среднее", unit: "ms", value: func(b TimelineBucket) float64 { return float64(b.DNSDurationMS) }},
		{name: "Количество соединений", unit: "шт", value: func(b TimelineBucket) float64 { return float64(b.ConnectCount) }},
		{name: "Среднее время соединения", unit: "ms", value: func(b TimelineBucket) float64 { return float64(b.ConnectDurationMS) }},
		{name: "Средний TTFB", unit: "ms", value: func(b TimelineBucket) float64 { return float64(b.TTFBMS) }},
		{name: "Доля подтормаживаний UI", unit: "%", value: func(b TimelineBucket) float64 { return jankRate(b.UIJankyFrames, b.UIFrames) }},
		{name: "UI кадры", unit: "шт", value: func(b TimelineBucket) float64 { return float64(b.UIFrames) }},
		{name: "Паузы главного потока", unit: "шт", value: func(b TimelineBucket) float64 { return float64(b.StallCount) }},
		{name: "Макс. пауза", unit: "ms", value: func(b TimelineBucket) float64 { return float64(b.StallMaxMS) }},
		{name: "PSS", unit: "KB", value: func(b TimelineBucket) float64 { return float64(b.MemoryPSSKB) }},
		{name: "Свободная RAM", unit: "KB", value: func(b TimelineBucket) float64 { return float64(b.AvailableMemoryKB) }},
		{name: "Дельта RX трафика", unit: "байт", value: func(b TimelineBucket) float64 { return float64(b.TrafficRxBytes) }},
		{name: "Дельта TX трафика", unit: "байт", value: func(b TimelineBucket) float64 { return float64(b.TrafficTxBytes) }},
	}

	series := make([]Series, 0, len(definitions))
	for _, definition := range definitions {
		points := make([]float64, 0, len(timeline))
		hasSignal := false
		for _, bucket := range timeline {
			value := definition.value(bucket)
			if value != 0 {
				hasSignal = true
			}
			points = append(points, value)
		}
		if hasSignal {
			series = append(series, Series{
				Name:     definition.name,
				Unit:     definition.unit,
				BucketMS: bucketMS,
				Points:   points,
			})
		}
	}
	return series
}

func timelineStatus(timeline []TimelineBucket) string {
	if len(timeline) < 3 {
		return "medium"
	}
	return "ok"
}

func timelineSummary(timeline []TimelineBucket, series []Series) string {
	if len(timeline) < 3 {
		return "Недостаточно данных для надежного анализа: нужно хотя бы несколько временных интервалов одного сценария."
	}
	return fmt.Sprintf("Построено %d временных интервалов по %d ms и %d рядов сигналов.", len(timeline), DefaultBucketMS, len(series))
}

func timelineFindings(timeline []TimelineBucket) []Finding {
	if len(timeline) < 3 {
		return []Finding{{
			Severity:       "medium",
			Title:          "Недостаточно данных для надежного анализа",
			Detail:         fmt.Sprintf("В таймлайне только %d временных интервалов. Этого мало для устойчивых выводов по скользящим окнам, точкам изменения и спектру.", len(timeline)),
			Recommendation: "Соберите более длинный прогон или несколько повторов сценария.",
		}}
	}
	return []Finding{{
		Severity: "ok",
		Title:    "Таймлайн сигналов построен",
		Detail:   fmt.Sprintf("Временные интервалы по %d ms готовы для следующих этапов анализа: робастной статистики, точек изменения, автокорреляции и FFT.", DefaultBucketMS),
	}}
}

func compareTimelineStatus(baselineTimeline, candidateTimeline []TimelineBucket) string {
	if len(baselineTimeline) < 3 || len(candidateTimeline) < 3 {
		return "medium"
	}
	return "ok"
}

func compareTimelineSummary(baselineTimeline, candidateTimeline []TimelineBucket) string {
	if len(baselineTimeline) < 3 || len(candidateTimeline) < 3 {
		return "Недостаточно данных для надежного анализа: базе и кандидату нужны несколько временных интервалов."
	}
	return fmt.Sprintf("База: %d временных интервалов, кандидат: %d временных интервалов, размер интервала: %d ms.", len(baselineTimeline), len(candidateTimeline), DefaultBucketMS)
}

func compareTimelineFindings(baselineTimeline, candidateTimeline []TimelineBucket) []Finding {
	if len(baselineTimeline) < 3 || len(candidateTimeline) < 3 {
		return []Finding{{
			Severity:       "medium",
			Title:          "Недостаточно данных для надежного анализа",
			Detail:         fmt.Sprintf("База содержит %d временных интервалов, кандидат содержит %d временных интервалов. Этого мало для надежного сравнения формы таймлайна.", len(baselineTimeline), len(candidateTimeline)),
			Recommendation: "Соберите более длинные прогоны базы и кандидата или несколько повторов каждого сценария.",
		}}
	}
	return []Finding{{
		Severity: "ok",
		Title:    "Таймлайны базы и кандидата построены",
		Detail:   "Равномерные временные интервалы готовы для сравнения точек изменения, периодичности, сетевых циклов и интегральных оценок.",
	}}
}

func timelineContainsFilter(value string, needle string) bool {
	if needle == "" {
		return true
	}
	return strings.Contains(strings.ToLower(value), needle)
}

func (c *timelineCollector) resolveOwner(dict map[uint64]string, id uint64) string {
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

func timelineOwnerHash(owner string) (string, bool) {
	if owner == "" {
		return "", false
	}
	if strings.HasPrefix(owner, "jh:") {
		return strings.TrimPrefix(owner, "jh:"), true
	}
	hashIndex := strings.LastIndex(owner, "#")
	if hashIndex < 0 || hashIndex == len(owner)-1 {
		return "", false
	}
	return owner[hashIndex+1:], true
}

func percentileSorted(values []uint64, p float64) uint64 {
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

func jankRate(jankyFrames, frames uint64) float64 {
	if frames == 0 {
		return 0
	}
	return float64(jankyFrames) * 100 / float64(frames)
}
