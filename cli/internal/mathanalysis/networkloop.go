package mathanalysis

import (
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
	bucketOffset  int
	name          string
	kind          string
	points        []float64
	sparse        bucketSeries
	tokens        map[int]map[string]int
	maxTokenBytes int
}

type networkLoopCollector struct {
	timeline   []TimelineBucket
	budget     *collectionBudget
	account    *collectionAccount
	results    *collectionAccount
	filter     analyze.Filter
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

func newNetworkLoopCollector(options analyze.Options, scale timelineScale) *networkLoopCollector {
	return &networkLoopCollector{
		filter:     normalizeTimelineFilter(options.Filter),
		bucketMS:   scale.bucketMS,
		bucketSize: scale.bucketCount,
		scale:      scale,
		signals:    map[string]*networkLoopSignal{},
	}
}

func networkLoopEventTimeMS(event jhlog.Event, dict map[uint64]string, filter analyze.Filter, symbols *mathSymbolResolver) (uint64, bool) {
	if filter.Active() && !mathEventMatchesFilter(event, dict, filter, symbols) {
		return 0, false
	}
	switch {
	case event.HTTP != nil:
		return event.TimeMS, true
	case event.Metric != nil && (event.Type == jhlog.EventCounter || event.Type == jhlog.EventGauge):
		name := symbols.resolveRaw(dict, event.Metric.MetricRef)
		network := classifyNetworkMetric(name)
		if !network.ok {
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

func (c *networkLoopCollector) add(event jhlog.Event, dict map[uint64]string, symbols *mathSymbolResolver) {
	switch {
	case event.HTTP != nil:
		c.addHTTP(event, dict, symbols)
	case event.Metric != nil && (event.Type == jhlog.EventCounter || event.Type == jhlog.EventGauge):
		c.addMetric(event, dict, symbols)
	}
}

func (c *networkLoopCollector) addHTTP(event jhlog.Event, dict map[uint64]string, symbols *mathSymbolResolver) {
	if c.filter.Active() && !mathEventMatchesFilter(event, dict, c.filter, symbols) {
		return
	}
	route := symbols.resolveRaw(dict, event.HTTP.RouteRef)
	owner := symbols.resolve(dict, event.Attribution.Owner)
	indexValue, ok := c.scale.index(event.TimeMS)
	if !ok {
		return
	}
	index := int(indexValue)
	baseTokens := []string{"route:" + route, "owner:" + owner}

	c.addPoint("route:"+route, "Маршрут "+route+" запросы", "route", index, 1, baseTokens...)
	if analysisOwnerIsKnown(owner) {
		c.addPoint("owner:"+owner, "Источник "+owner+" запросы", "owner", index, 1, baseTokens...)
	}
	if event.HTTP.DNSMS > 0 {
		tokens := append([]string{"dns_high"}, baseTokens...)
		c.addPoint("dns:global", "DNS всплески", "dns", index, 1, tokens...)
		c.addPoint("dns:route:"+route, "DNS маршрут "+route, "dns", index, 1, tokens...)
	}
	if event.HTTP.ConnectMS > 0 {
		tokens := append([]string{"connect_high"}, baseTokens...)
		c.addPoint("connect:global", "Всплески соединения", "connect", index, 1, tokens...)
		c.addPoint("connect:route:"+route, "Соединение маршрута "+route, "connect", index, 1, tokens...)
	}
	if event.Flags&uint64(jhlog.FlagHTTPFailed) != 0 || event.HTTP.Status == jhlog.Status5xx {
		tokens := append([]string{"http_failed"}, baseTokens...)
		if event.HTTP.Status == jhlog.Status5xx {
			tokens = append(tokens, "http_5xx")
		}
		c.addPoint("failure:global", "HTTP ошибки", "failure", index, 1, tokens...)
		c.addPoint("failure:route:"+route, "HTTP ошибки маршрут "+route, "failure", index, 1, tokens...)
	}
}

func (c *networkLoopCollector) addMetric(event jhlog.Event, dict map[uint64]string, symbols *mathSymbolResolver) {
	name := symbols.resolveRaw(dict, event.Metric.MetricRef)
	network := classifyNetworkMetric(name)
	if !network.ok {
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
	tokens = append(tokens, "route:"+network.route, "owner:"+network.owner)
	key := "metric:" + network.kind + ":" + network.route + ":" + network.owner + ":" + name
	title := networkMetricTitle(network.kind, network.route, network.owner)
	c.addPoint(key, title, network.kind, index, value, tokens...)
}

func (c *networkLoopCollector) addPoint(key, name, kind string, index int, value float64, tokens ...string) {
	if index < 0 || value <= 0 {
		return
	}
	c.ensureBucket(index)
	signal := c.signals[key]
	if signal == nil {
		if !c.account.reserve(2*mathMapBaseBytes + mathMapEntryBytes + uint64(len(key)+len(name))) {
			return
		}
		signal = &networkLoopSignal{
			name:   name,
			kind:   kind,
			tokens: map[int]map[string]int{},
		}
		c.signals[key] = signal
	}
	if !signal.sparse.add(index, value, c.account) {
		return
	}
	bucketTokens := signal.tokens[index]
	for _, token := range tokens {
		if token == "" {
			continue
		}
		if len(token) > signal.maxTokenBytes {
			signal.maxTokenBytes = len(token)
		}
		if bucketTokens == nil {
			if !c.account.reserve(mathMapBaseBytes + mathMapEntryBytes) {
				return
			}
			bucketTokens = map[string]int{}
			signal.tokens[index] = bucketTokens
		}
		// Include space for motif counting/sorting as well as the retained token map.
		count, exists := bucketTokens[token]
		if !exists && !c.account.reserve(2*mathMapEntryBytes+uint64(len(token))) {
			return
		}
		bucketTokens[token] = count + 1
	}
}

func (c *networkLoopCollector) ensureBucket(index int) {
	if index < c.bucketSize {
		return
	}
	c.bucketSize = index + 1
}
