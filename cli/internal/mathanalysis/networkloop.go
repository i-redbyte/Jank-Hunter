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
	name   string
	kind   string
	route  string
	owner  string
	points []float64
	tokens map[int]map[string]int
}

type networkLoopCollector struct {
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
	switch {
	case event.HTTP != nil:
		route := symbols.resolve(dict, event.HTTP.RouteRef)
		owner := symbols.resolve(dict, event.Attribution.Owner)
		if !networkLoopPassesFilter(filter, route, owner) {
			return 0, false
		}
		return event.TimeMS, true
	case event.Metric != nil && (event.Type == jhlog.EventCounter || event.Type == jhlog.EventGauge):
		name := symbols.resolve(dict, event.Metric.MetricRef)
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

func (c *networkLoopCollector) add(event jhlog.Event, dict map[uint64]string, symbols *mathSymbolResolver) {
	switch {
	case event.HTTP != nil:
		c.addHTTP(event, dict, symbols)
	case event.Metric != nil && (event.Type == jhlog.EventCounter || event.Type == jhlog.EventGauge):
		c.addMetric(event, dict, symbols)
	}
}

func (c *networkLoopCollector) addHTTP(event jhlog.Event, dict map[uint64]string, symbols *mathSymbolResolver) {
	route := symbols.resolve(dict, event.HTTP.RouteRef)
	owner := symbols.resolve(dict, event.Attribution.Owner)
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
		c.addPoint("dns:global", "DNS всплески", "dns", "", "", index, 1, tokens...)
		c.addPoint("dns:route:"+route, "DNS маршрут "+route, "dns", route, owner, index, 1, tokens...)
	}
	if event.HTTP.ConnectMS > 0 {
		tokens := append([]string{"connect_high"}, baseTokens...)
		c.addPoint("connect:global", "Всплески соединения", "connect", "", "", index, 1, tokens...)
		c.addPoint("connect:route:"+route, "Соединение маршрута "+route, "connect", route, owner, index, 1, tokens...)
	}
	if event.Flags&uint64(jhlog.FlagHTTPFailed) != 0 || event.HTTP.Status == jhlog.Status5xx {
		tokens := append([]string{"http_failed"}, baseTokens...)
		if event.HTTP.Status == jhlog.Status5xx {
			tokens = append(tokens, "http_5xx")
		}
		c.addPoint("failure:global", "HTTP ошибки", "failure", "", "", index, 1, tokens...)
		c.addPoint("failure:route:"+route, "HTTP ошибки маршрут "+route, "failure", route, owner, index, 1, tokens...)
	}
}

func (c *networkLoopCollector) addMetric(event jhlog.Event, dict map[uint64]string, symbols *mathSymbolResolver) {
	name := symbols.resolve(dict, event.Metric.MetricRef)
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
