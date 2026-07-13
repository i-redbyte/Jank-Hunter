package analyze

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/datavalue"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

const maxAggregateSamplesPerSignal = 20_000

type qualityCounterWarning struct {
	name  string
	label string
}

var runtimeQualityCounterWarnings = []qualityCounterWarning{
	{"jankhunter.events_dropped.count", "очередь writer отбросила события"},
	{"jankhunter.writer_io_error.count", "writer видел ошибки записи"},
	{"jankhunter.writer_event_lost_on_io.count", "writer потерял события после ошибки записи"},
	{"jankhunter.metric_aggregation.dropped.count", "агрегатор метрик отбросил ключи из-за лимита кардинальности"},
	{"jankhunter.log_spam.dropped_keys.count", "агрегатор спама логами отбросил ключи из-за лимита кардинальности"},
	{"jankhunter.runtime_call_graph.dropped.count", "runtime-граф вызовов отбросил ребра из-за лимита или рассинхронизации стека"},
	{"jankhunter.handler_wrapper.dropped_entries.count", "реестр Handler-оберток отбросил записи из-за лимита"},
	{"jankhunter.handler_wrapper.dropped_wrappers.count", "реестр Handler-оберток отбросил wrapper из-за лимита"},
	{"jankhunter.activity_tracker.unavailable.count", "Activity lifecycle tracker не подключился, поэтому screen мог остаться неизвестным"},
}

func InspectFilesWithOptions(title string, paths []string, options Options) (Summary, error) {
	collector := newCollector(title, len(paths), options)
	for _, path := range paths {
		collector.startLog()
		lastDictSize := 0
		warnings, err := jhlog.StreamFileWithWarnings(path, func(event jhlog.Event, dict map[uint64]string) error {
			if len(dict) > lastDictSize {
				collector.summary.Dictionary += len(dict) - lastDictSize
				lastDictSize = len(dict)
			}
			collector.add(dict, event)
			return nil
		})
		if err != nil {
			return Summary{}, err
		}
		collector.summary.Warnings = append(collector.summary.Warnings, warnings...)
		collector.finishLog()
	}
	return collector.finish(), nil
}

func LoadOwnerMap(path string) (map[string]string, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if ownerMap, ok, err := loadOwnerMapObject(path, data); ok || err != nil {
		return ownerMap, err
	}
	return loadOwnerMapJSONL(path, data)
}

type ownerMapRecord struct {
	Format  int               `json:"format"`
	Kind    string            `json:"kind"`
	Owners  map[string]string `json:"owners"`
	Entries []ownerMapEntry   `json:"entries"`
	ID      string            `json:"id"`
	Owner   string            `json:"owner"`
	Name    string            `json:"name"`
	Value   string            `json:"value"`
}

type ownerMapEntry struct {
	ID    string `json:"id"`
	Owner string `json:"owner"`
	Name  string `json:"name"`
	Value string `json:"value"`
}

func loadOwnerMapObject(path string, data []byte) (map[string]string, bool, error) {
	var raw ownerMapRecord
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, false, nil
	}
	if err := validateOwnerMapFormat(path, raw.Format); err != nil {
		return nil, true, err
	}
	out := map[string]string{}
	addOwnerMapRecord(out, raw)
	return out, true, nil
}

func loadOwnerMapJSONL(path string, data []byte) (map[string]string, error) {
	out := map[string]string{}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var raw ownerMapRecord
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			return nil, fmt.Errorf("parse owner map line %d: %w", lineNumber, err)
		}
		if err := validateOwnerMapFormat(path, raw.Format); err != nil {
			return nil, fmt.Errorf("parse owner map line %d: %w", lineNumber, err)
		}
		addOwnerMapRecord(out, raw)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func addOwnerMapRecord(out map[string]string, raw ownerMapRecord) {
	for id, owner := range raw.Owners {
		addOwnerMapEntry(out, ownerMapEntry{ID: id, Owner: owner})
	}
	for _, entry := range raw.Entries {
		addOwnerMapEntry(out, entry)
	}
	if raw.ID != "" {
		addOwnerMapEntry(out, ownerMapEntry{
			ID:    raw.ID,
			Owner: raw.Owner,
			Name:  raw.Name,
			Value: raw.Value,
		})
	}
}

func addOwnerMapEntry(out map[string]string, entry ownerMapEntry) {
	name := firstNonEmpty(entry.Owner, entry.Name, entry.Value)
	if entry.ID == "" || name == "" {
		return
	}
	out[entry.ID] = name
	out["jh:"+entry.ID] = name
}

func validateOwnerMapFormat(path string, got int) error {
	if got == 1 {
		return nil
	}
	return validateArtifactFormat(path, "owner map", got, OwnerMapFormat)
}

type collector struct {
	summary             Summary
	filter              Filter
	ownerMap            map[string]string
	nameMap             *NameMapping
	classGraph          *ClassGraph
	diagnostics         *InstrumentationDiagnostics
	heap                *HeapEvidence
	seenEvent           bool
	firstTime           uint64
	lastTime            uint64
	logSeen             bool
	logFirst            uint64
	logLast             uint64
	logsWithEvents      int
	totalLogDurationMS  uint64
	logTrafficSeen      bool
	logTrafficFirstRx   uint64
	logTrafficFirstTx   uint64
	logTrafficLastRx    uint64
	logTrafficLastTx    uint64
	totalTrafficRxBytes uint64
	totalTrafficTxBytes uint64
	dictionaryOverflow  int

	httpDurations  uint64SampleSet
	routeDurations map[string]*uint64SampleSet
	routeFailures  map[string]int
	routeRx        map[string]uint64
	routeTx        map[string]uint64
	routeTTFB      map[string]uint64
	routeTTFBCount map[string]uint64
	routeOwner     map[string]string

	screenStats        map[string]*ScreenStats
	ownerStats         map[ownerStatKey]*OwnerStats
	flowStats          map[string]*FlowStats
	flowHTTPDurations  map[string]*uint64SampleSet
	logSpamStats       map[string]*LogSpamStats
	problemStats       map[string]*ProblemWindowStats
	runtimeCallStats   map[string]*RuntimeCallStats
	counterValues      map[string]uint64
	gaugeValues        map[string]*gaugeStats
	appVersions        map[string]uint64
	builds             map[string]uint64
	devices            map[string]uint64
	sdks               map[string]uint64
	cohortSamples      map[string]uint64
	networkSamples     map[string]uint64
	processSamples     map[string]uint64
	retainedClasses    map[string]*retainedClassStats
	retainedAgeBuckets map[string]uint64
	memoryLeakStats    map[string]*memoryLeakStats

	currentAppVersion string
	currentBuild      string
	currentDevice     string
	currentSDK        string
	currentProcess    string
	currentNetwork    string
	currentAndroid    string
	currentPatch      string
	currentPrimaryABI string
	currentABIs       string
	currentMaker      string
	currentBrand      string
	currentHardware   string
	currentBoard      string
	currentProduct    string
	currentRootKnown  bool
	currentRooted     bool
	currentAttrScreen string
	currentAttrOwner  string
	currentAttrFlow   string
	currentAttrStep   string
}

func newCollector(title string, logCount int, options Options) *collector {
	return &collector{
		summary:            Summary{Title: title, LogCount: logCount},
		filter:             normalizeFilter(options.Filter),
		ownerMap:           options.OwnerMap,
		nameMap:            options.ObfuscationMap,
		classGraph:         DeobfuscateClassGraph(options.ClassGraph, options.ObfuscationMap),
		diagnostics:        options.InstrumentationDiagnostics,
		heap:               DeobfuscateHeapEvidence(options.HeapEvidence, options.ObfuscationMap),
		routeDurations:     map[string]*uint64SampleSet{},
		routeFailures:      map[string]int{},
		routeRx:            map[string]uint64{},
		routeTx:            map[string]uint64{},
		routeTTFB:          map[string]uint64{},
		routeTTFBCount:     map[string]uint64{},
		routeOwner:         map[string]string{},
		screenStats:        map[string]*ScreenStats{},
		ownerStats:         map[ownerStatKey]*OwnerStats{},
		flowStats:          map[string]*FlowStats{},
		flowHTTPDurations:  map[string]*uint64SampleSet{},
		logSpamStats:       map[string]*LogSpamStats{},
		problemStats:       map[string]*ProblemWindowStats{},
		runtimeCallStats:   map[string]*RuntimeCallStats{},
		counterValues:      map[string]uint64{},
		gaugeValues:        map[string]*gaugeStats{},
		appVersions:        map[string]uint64{},
		builds:             map[string]uint64{},
		devices:            map[string]uint64{},
		sdks:               map[string]uint64{},
		cohortSamples:      map[string]uint64{},
		networkSamples:     map[string]uint64{},
		processSamples:     map[string]uint64{},
		retainedClasses:    map[string]*retainedClassStats{},
		retainedAgeBuckets: map[string]uint64{},
		memoryLeakStats:    map[string]*memoryLeakStats{},
		currentAppVersion:  "unknown",
		currentBuild:       "unknown",
		currentDevice:      "unknown",
		currentSDK:         "unknown",
		currentProcess:     "unknown",
		currentNetwork:     "unknown",
		currentAndroid:     "unknown",
		currentPatch:       "unknown",
		currentPrimaryABI:  "unknown",
		currentABIs:        "unknown",
		currentMaker:       "unknown",
		currentBrand:       "unknown",
		currentHardware:    "unknown",
		currentBoard:       "unknown",
		currentProduct:     "unknown",
		currentAttrScreen:  "unknown",
		currentAttrOwner:   "unknown",
		currentAttrFlow:    "unknown",
		currentAttrStep:    "unknown",
	}
}

func (c *collector) startLog() {
	c.resetAttribution()
	c.logSeen = false
	c.logFirst = 0
	c.logLast = 0
	c.logTrafficSeen = false
	c.logTrafficFirstRx = 0
	c.logTrafficFirstTx = 0
	c.logTrafficLastRx = 0
	c.logTrafficLastTx = 0
}

func (c *collector) finishLog() {
	if !c.logSeen {
		return
	}
	c.logsWithEvents++
	if c.logLast >= c.logFirst {
		c.totalLogDurationMS += c.logLast - c.logFirst
	}
	if c.logTrafficSeen {
		c.totalTrafficRxBytes += counterDelta(c.logTrafficFirstRx, c.logTrafficLastRx)
		c.totalTrafficTxBytes += counterDelta(c.logTrafficFirstTx, c.logTrafficLastTx)
	}
}

func (c *collector) recordTraffic(rxBytes, txBytes uint64) {
	if !c.logTrafficSeen {
		c.logTrafficSeen = true
		c.logTrafficFirstRx = rxBytes
		c.logTrafficFirstTx = txBytes
	}
	c.logTrafficLastRx = rxBytes
	c.logTrafficLastTx = txBytes
}

func (c *collector) resetAttribution() {
	c.currentAttrScreen = "unknown"
	c.currentAttrOwner = "unknown"
	c.currentAttrFlow = "unknown"
	c.currentAttrStep = "unknown"
}

type retainedClassStats struct {
	count    uint64
	maxAgeMs uint64
}

type uint64SampleSet struct {
	values       []uint64
	seen         int
	max          uint64
	approximated bool
}

func (s *uint64SampleSet) add(value uint64) {
	s.seen++
	if value > s.max {
		s.max = value
	}
	if len(s.values) < maxAggregateSamplesPerSignal {
		s.values = append(s.values, value)
		return
	}
	s.approximated = true
	index := deterministicAggregateReservoirIndex(s.seen)
	if index < maxAggregateSamplesPerSignal {
		s.values[index] = value
	}
}

func (s *uint64SampleSet) sortedValues() []uint64 {
	if len(s.values) == 0 {
		return nil
	}
	values := append([]uint64(nil), s.values...)
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	return values
}

func (s *uint64SampleSet) sampled() int {
	return len(s.values)
}

func (s *uint64SampleSet) isApproximated() bool {
	return s.approximated || s.seen > len(s.values)
}

type gaugeStats struct {
	count uint64
	total uint64
	max   uint64
	last  uint64
	mode  jhlog.MetricMode
}

func (s *gaugeStats) add(value, count, sum, max uint64, mode jhlog.MetricMode) {
	if count == 0 {
		count = 1
	}
	if sum == 0 {
		sum = value
	}
	if max == 0 {
		max = value
	}
	if mode != jhlog.MetricModeUnknown {
		s.mode = mode
	}
	if s.mode == jhlog.MetricModeUnknown {
		s.mode = jhlog.MetricModeAverage
	}
	s.count++
	s.count += count - 1
	s.last = value
	switch s.mode {
	case jhlog.MetricModeLast, jhlog.MetricModeState:
		s.total = value
		s.max = max
	case jhlog.MetricModeBooleanRate:
		s.total += sum
		if max > s.max {
			s.max = max
		}
	default:
		s.total += sum
		if max > s.max {
			s.max = max
		}
	}
}

func (s *gaugeStats) value() uint64 {
	if s.count == 0 {
		return 0
	}
	switch s.mode {
	case jhlog.MetricModeLast, jhlog.MetricModeState:
		return s.last
	case jhlog.MetricModeBooleanRate:
		return (s.total * 100) / s.count
	}
	return s.total / s.count
}

func (s *gaugeStats) extra() string {
	switch s.mode {
	case jhlog.MetricModeLast:
		return fmt.Sprintf("last=%d samples=%d", s.last, s.count)
	case jhlog.MetricModeState:
		return fmt.Sprintf("state=%d samples=%d", s.last, s.count)
	case jhlog.MetricModeBooleanRate:
		return fmt.Sprintf("true_pct=%d true=%d samples=%d", s.value(), s.total, s.count)
	default:
		return fmt.Sprintf("avg=%d max=%d samples=%d", s.value(), s.max, s.count)
	}
}

func metricModeForGauge(name string) jhlog.MetricMode {
	metric := strings.ToLower(strings.TrimSpace(name))
	switch metric {
	case "battery.status",
		"battery.plugged",
		"battery.health",
		"device.thermal.status",
		"process.exit.last.reason",
		"process.exit.last.importance",
		"memory.trim.last_level":
		return jhlog.MetricModeState
	case "battery.charging",
		"device.power_save_mode",
		"device.interactive",
		"device.idle_mode",
		"network.request.connection_released":
		return jhlog.MetricModeBooleanRate
	}
	if strings.HasPrefix(metric, "process.exit.last.reason_") && strings.HasSuffix(metric, ".count") {
		return jhlog.MetricModeLast
	}
	if strings.HasSuffix(metric, ".last_id") ||
		strings.Contains(metric, ".last.") ||
		strings.HasSuffix(metric, ".last_level") ||
		strings.HasSuffix(metric, ".core_count") ||
		strings.HasSuffix(metric, ".max_kb") {
		return jhlog.MetricModeLast
	}
	return jhlog.MetricModeAverage
}

type memoryLeakStats struct {
	className string
	holder    string
	screen    string
	flow      string
	step      string
	count     uint64
	maxAgeMs  uint64
}

func deterministicAggregateReservoirIndex(seen int) int {
	x := uint64(seen)*2862933555777941757 + 3037000493
	return int(x % uint64(seen))
}

func normalizeFilter(filter Filter) Filter {
	return Filter{
		RouteContains:  strings.ToLower(filter.RouteContains),
		ScreenContains: strings.ToLower(filter.ScreenContains),
		OwnerContains:  strings.ToLower(filter.OwnerContains),
		ClassContains:  strings.ToLower(filter.ClassContains),
	}
}

func filterActive(filter Filter) bool {
	return filter.RouteContains != "" ||
		filter.ScreenContains != "" ||
		filter.OwnerContains != "" ||
		filter.ClassContains != ""
}

func containsFilter(value string, needle string) bool {
	if needle == "" {
		return true
	}
	return strings.Contains(strings.ToLower(value), needle)
}

func containsAnyFilter(needle string, values ...string) bool {
	if needle == "" {
		return true
	}
	for _, value := range values {
		if containsFilter(value, needle) {
			return true
		}
	}
	return false
}

func (c *collector) eventContext(screenOverride, ownerOverride, flowOverride, stepOverride string) FlowStats {
	return c.flowContextFromKey(c.contextKey(screenOverride, ownerOverride, flowOverride, stepOverride))
}

func (c *collector) matchesFilters(route string, context FlowStats, classCandidates []string, ownerCandidates ...string) bool {
	if !containsFilter(route, c.filter.RouteContains) {
		return false
	}
	if !containsFilter(context.Screen, c.filter.ScreenContains) {
		return false
	}
	if c.filter.ClassContains != "" && !containsAnyFilter(c.filter.ClassContains, classCandidates...) {
		return false
	}
	if c.filter.OwnerContains != "" {
		candidates := append([]string{context.Owner}, ownerCandidates...)
		if !containsAnyFilter(c.filter.OwnerContains, candidates...) {
			return false
		}
	}
	return true
}

func (c *collector) add(dict map[uint64]string, event jhlog.Event) {
	c.summary.EventCount++
	if !c.seenEvent {
		c.seenEvent = true
		c.firstTime = event.TimeMS
		c.lastTime = event.TimeMS
	} else {
		if event.TimeMS < c.firstTime {
			c.firstTime = event.TimeMS
		}
		if event.TimeMS > c.lastTime {
			c.lastTime = event.TimeMS
		}
	}
	if !c.logSeen {
		c.logSeen = true
		c.logFirst = event.TimeMS
		c.logLast = event.TimeMS
	} else {
		if event.TimeMS < c.logFirst {
			c.logFirst = event.TimeMS
		}
		if event.TimeMS > c.logLast {
			c.logLast = event.TimeMS
		}
	}
	switch {
	case event.Dictionary != nil:
		if event.Dictionary.Value == "__jh_dictionary_overflow__" {
			c.dictionaryOverflow++
		}
	case event.Session != nil:
		c.resetAttribution()
		c.currentAppVersion = jhlog.Resolve(dict, event.Session.AppVersionID)
		c.currentBuild = jhlog.Resolve(dict, event.Session.BuildID)
		c.currentDevice = jhlog.Resolve(dict, event.Session.DeviceID)
		c.currentSDK = fmt.Sprintf("api-%d", event.Session.SDKInt)
		c.currentProcess = jhlog.Resolve(dict, event.Session.ProcessID)
		c.currentAndroid = jhlog.Resolve(dict, event.Session.AndroidReleaseID)
		c.currentPatch = jhlog.Resolve(dict, event.Session.SecurityPatchID)
		c.currentPrimaryABI = jhlog.Resolve(dict, event.Session.PrimaryABIID)
		c.currentABIs = jhlog.Resolve(dict, event.Session.SupportedABIsID)
		c.currentMaker = jhlog.Resolve(dict, event.Session.ManufacturerID)
		c.currentBrand = jhlog.Resolve(dict, event.Session.BrandID)
		c.currentHardware = jhlog.Resolve(dict, event.Session.HardwareID)
		c.currentBoard = jhlog.Resolve(dict, event.Session.BoardID)
		c.currentProduct = jhlog.Resolve(dict, event.Session.ProductID)
		c.currentRootKnown = true
		c.currentRooted = event.Session.DeviceRooted
		c.summary.DeviceRootKnown = true
		c.summary.DeviceRooted = event.Session.DeviceRooted
		c.appVersions[c.currentAppVersion]++
		c.builds[c.currentBuild]++
		c.devices[c.currentDevice]++
		c.sdks[c.currentSDK]++
		c.processSamples[c.currentProcess]++
	case event.Flow != nil:
		c.currentAttrScreen = attrValue(jhlog.Resolve(dict, event.Flow.ScreenID))
		c.currentAttrOwner = attrValue(c.resolveOwner(dict, event.Flow.OwnerID))
		c.currentAttrFlow = attrValue(jhlog.Resolve(dict, event.Flow.FlowID))
		c.currentAttrStep = attrValue(jhlog.Resolve(dict, event.Flow.StepID))
	case event.HTTP != nil:
		route := jhlog.Resolve(dict, event.HTTP.RouteID)
		owner := c.resolveOwner(dict, event.HTTP.OwnerID)
		context := c.eventContext("", owner, "", "")
		if !c.matchesFilters(route, context, nil, owner) {
			return
		}
		c.markCohort()
		c.summary.HTTPCount++
		c.httpDurations.add(event.HTTP.DurationMS)
		c.sampleSet(c.routeDurations, route).add(event.HTTP.DurationMS)
		c.routeRx[route] += event.HTTP.RxBytes
		c.routeTx[route] += event.HTTP.TxBytes
		c.routeTTFB[route] += event.HTTP.TTFBMS
		c.routeTTFBCount[route]++
		if c.routeOwner[route] == "" {
			c.routeOwner[route] = owner
		}
		if event.Flags&uint64(jhlog.FlagHTTPFailed) != 0 || event.HTTP.Status == jhlog.Status5xx {
			c.summary.HTTPFailed++
			c.routeFailures[route]++
		}
		addOwner(c.ownerStats, owner, "http", event.HTTP.DurationMS, "")
		flowKey := c.flowKey("", owner)
		flow := c.ensureFlow(flowKey)
		flow.HTTPCount++
		flow.RouteSample = firstNonEmpty(flow.RouteSample, route)
		c.sampleSet(c.flowHTTPDurations, flowKey).add(event.HTTP.DurationMS)
		if event.Flags&uint64(jhlog.FlagHTTPFailed) != 0 || event.HTTP.Status == jhlog.Status5xx {
			flow.HTTPFailed++
		}
	case event.UIWindow != nil:
		screen := jhlog.Resolve(dict, event.UIWindow.ScreenID)
		context := c.eventContext(screen, "", "", "")
		if !c.matchesFilters("", context, nil) {
			return
		}
		c.markCohort()
		stats := c.screenStats[screen]
		if stats == nil {
			stats = &ScreenStats{Screen: screen}
			c.screenStats[screen] = stats
		}
		stats.WindowCount++
		stats.WindowMS += event.UIWindow.WindowMS
		stats.Frames += event.UIWindow.FrameCount
		stats.JankyFrames += event.UIWindow.JankCount
		windowFPS := fps(event.UIWindow.FrameCount, event.UIWindow.WindowMS)
		if stats.MinFPS == 0 || windowFPS < stats.MinFPS {
			stats.MinFPS = windowFPS
		}
		if event.UIWindow.P95MS > stats.P95MS {
			stats.P95MS = event.UIWindow.P95MS
		}
		if event.UIWindow.P99MS > stats.MaxP99MS {
			stats.MaxP99MS = event.UIWindow.P99MS
		}
		c.summary.UIFrames += event.UIWindow.FrameCount
		c.summary.UIJank += event.UIWindow.JankCount
		c.summary.UIWindowMS += event.UIWindow.WindowMS
		if c.summary.UIMinFPS == 0 || windowFPS < c.summary.UIMinFPS {
			c.summary.UIMinFPS = windowFPS
		}
		flowKey := c.flowKey(screen, "")
		flow := c.ensureFlow(flowKey)
		flow.UIWindows++
		flow.UIFrames += event.UIWindow.FrameCount
		flow.UIJank += event.UIWindow.JankCount
	case event.Stall != nil:
		owner := c.resolveOwner(dict, event.Stall.OwnerID)
		stack := jhlog.Resolve(dict, event.Stall.StackID)
		context := c.eventContext("", owner, "", "")
		if !c.matchesFilters("", context, nil, owner) {
			return
		}
		c.markCohort()
		c.summary.StallCount++
		if event.Stall.DurationMS > c.summary.StallMaxMS {
			c.summary.StallMaxMS = event.Stall.DurationMS
		}
		addOwner(c.ownerStats, owner, "main_thread_stall", event.Stall.DurationMS, stack)
		flowKey := c.flowKey("", owner)
		flow := c.ensureFlow(flowKey)
		flow.StallCount++
		if event.Stall.DurationMS > flow.StallMaxMS {
			flow.StallMaxMS = event.Stall.DurationMS
		}
	case event.Context != nil:
		c.summary.ContextCount++
		c.currentNetwork = jhlog.NetworkName(event.Context.Network)
		c.markCohort()
		c.summary.BatteryLastPct = event.Context.BatteryPct
		c.summary.BatteryStateLast = event.Context.BatteryState
		c.summary.BatteryTempDeciC = event.Context.BatteryTempDeciC
		c.summary.AvailMemoryLastKB = event.Context.AvailMemoryKB
		c.summary.TotalMemoryKB = event.Context.TotalMemoryKB
		c.summary.FreeStorageKB = event.Context.FreeStorageKB
		c.summary.TotalStorageKB = event.Context.TotalStorageKB
		c.summary.NetworkMetered = event.Context.NetworkMetered
		c.summary.NetworkValidated = event.Context.NetworkValidated
		c.summary.NetworkVPN = event.Context.NetworkVPN
		if c.summary.BatteryMinPct == 0 || event.Context.BatteryPct < c.summary.BatteryMinPct {
			c.summary.BatteryMinPct = event.Context.BatteryPct
		}
		if c.summary.AvailMemoryMinKB == 0 || event.Context.AvailMemoryKB < c.summary.AvailMemoryMinKB {
			c.summary.AvailMemoryMinKB = event.Context.AvailMemoryKB
		}
		if event.Context.LowMemory {
			c.summary.LowMemoryCount++
		}
		c.recordTraffic(event.Context.RxBytes, event.Context.TxBytes)
		c.networkSamples[c.currentNetwork]++
	case event.Memory != nil:
		context := c.eventContext("", "", "", "")
		if !c.matchesFilters("", context, nil) {
			return
		}
		c.markCohort()
		c.summary.MemoryCount++
		if event.Memory.PSSKB > c.summary.MemoryMaxKB {
			c.summary.MemoryMaxKB = event.Memory.PSSKB
		}
		flow := c.ensureFlow(c.flowKey("", ""))
		if event.Memory.PSSKB > flow.MemoryMaxKB {
			flow.MemoryMaxKB = event.Memory.PSSKB
		}
	case event.Retained != nil:
		className := c.deobfuscate(jhlog.Resolve(dict, event.Retained.ClassID))
		holder := c.resolveOwner(dict, event.Retained.HolderID)
		owner := c.resolveOwner(dict, event.Retained.OwnerID)
		context := c.eventContext(
			jhlog.Resolve(dict, event.Retained.ScreenID),
			owner,
			jhlog.Resolve(dict, event.Retained.FlowID),
			jhlog.Resolve(dict, event.Retained.StepID),
		)
		holder = firstKnown(holder, context.Owner)
		if !c.matchesFilters("", context, []string{className}, holder, owner) {
			return
		}
		c.markCohort()
		c.summary.Retained += event.Retained.Count
		stats := c.retainedClasses[className]
		if stats == nil {
			stats = &retainedClassStats{}
			c.retainedClasses[className] = stats
		}
		stats.count += event.Retained.Count
		if event.Retained.AgeMS > stats.maxAgeMs {
			stats.maxAgeMs = event.Retained.AgeMS
		}
		c.retainedAgeBuckets[retainedAgeBucket(event.Retained.AgeMS)] += event.Retained.Count
		c.addMemoryLeakSuspect(className, holder, context, event.Retained.AgeMS, event.Retained.Count)
		addOwner(c.ownerStats, className, "retained_object", event.Retained.AgeMS, "")
	case event.LogSpam != nil:
		key := c.contextKey(
			jhlog.Resolve(dict, event.LogSpam.ScreenID),
			c.resolveOwner(dict, event.LogSpam.OwnerID),
			jhlog.Resolve(dict, event.LogSpam.FlowID),
			jhlog.Resolve(dict, event.LogSpam.StepID),
		)
		context := c.flowContextFromKey(key)
		source := jhlog.Resolve(dict, event.LogSpam.SourceID)
		if !c.matchesFilters("", context, []string{source}, context.Owner) {
			return
		}
		c.markCohort()
		level := logLevelName(event.LogSpam.Level)
		logKey := key + "\x00" + source + "\x00" + level
		stats := c.logSpamStats[logKey]
		if stats == nil {
			stats = &LogSpamStats{
				Screen: context.Screen,
				Flow:   context.Flow,
				Step:   context.Step,
				Owner:  context.Owner,
				Source: source,
				Level:  level,
			}
			c.logSpamStats[logKey] = stats
		}
		stats.Count += event.LogSpam.Count
		flow := c.ensureFlow(key)
		flow.LogSpam += event.LogSpam.Count
	case event.Problem != nil:
		key := c.contextKey(
			jhlog.Resolve(dict, event.Problem.ScreenID),
			c.resolveOwner(dict, event.Problem.OwnerID),
			jhlog.Resolve(dict, event.Problem.FlowID),
			jhlog.Resolve(dict, event.Problem.StepID),
		)
		context := c.flowContextFromKey(key)
		if !c.matchesFilters("", context, nil, context.Owner) {
			return
		}
		c.markCohort()
		kind := jhlog.Resolve(dict, event.Problem.KindID)
		problemKey := key + "\x00" + kind
		stats := c.problemStats[problemKey]
		if stats == nil {
			stats = &ProblemWindowStats{
				Screen: context.Screen,
				Flow:   context.Flow,
				Step:   context.Step,
				Owner:  context.Owner,
				Kind:   kind,
			}
			c.problemStats[problemKey] = stats
		}
		stats.Windows++
		stats.Count += event.Problem.Count
		stats.TotalWindowMS += event.Problem.WindowMS
		if event.Problem.MaxMS > stats.MaxMS {
			stats.MaxMS = event.Problem.MaxMS
		}
		flow := c.ensureFlow(key)
		flow.ProblemCount += event.Problem.Count
		if event.Problem.MaxMS > flow.ProblemMaxMS {
			flow.ProblemMaxMS = event.Problem.MaxMS
		}
	case event.RuntimeCall != nil:
		caller := c.resolveOwner(dict, event.RuntimeCall.CallerID)
		callee := c.resolveOwner(dict, event.RuntimeCall.CalleeID)
		key := c.contextKey(
			jhlog.Resolve(dict, event.RuntimeCall.ScreenID),
			caller,
			jhlog.Resolve(dict, event.RuntimeCall.FlowID),
			jhlog.Resolve(dict, event.RuntimeCall.StepID),
		)
		context := c.flowContextFromKey(key)
		if !c.matchesFilters("", context, []string{caller, callee}, caller, callee) {
			return
		}
		c.markCohort()
		callKey := key + "\x00" + caller + "\x00" + callee
		stats := c.runtimeCallStats[callKey]
		if stats == nil {
			stats = &RuntimeCallStats{
				Screen: context.Screen,
				Flow:   context.Flow,
				Step:   context.Step,
				Caller: caller,
				Callee: callee,
			}
			c.runtimeCallStats[callKey] = stats
		}
		stats.Count += event.RuntimeCall.Count
		stats.TotalMS += event.RuntimeCall.TotalMS
		if event.RuntimeCall.MaxMS > stats.MaxMS {
			stats.MaxMS = event.RuntimeCall.MaxMS
		}
	case event.Metric != nil:
		c.markCohort()
		name := jhlog.Resolve(dict, event.Metric.MetricID)
		if event.Type == jhlog.EventGauge {
			mode := event.Metric.Mode
			if mode == jhlog.MetricModeUnknown {
				mode = metricModeForGauge(name)
			}
			c.gauge(name).add(event.Metric.Value, event.Metric.Count, event.Metric.Sum, event.Metric.Max, mode)
		} else {
			c.counterValues[name] += event.Metric.Value
		}
	}
}

func (c *collector) markCohort() {
	c.cohortSamples[fmt.Sprintf(
		"app=%s build=%s sdk=%s device=%s process=%s network=%s root=%s",
		c.currentAppVersion,
		c.currentBuild,
		c.currentSDK,
		c.currentDevice,
		c.currentProcess,
		c.currentNetwork,
		rootCohortValue(c.currentRootKnown, c.currentRooted),
	)]++
}

func (c *collector) resolveOwner(dict map[uint64]string, id uint64) string {
	return c.deobfuscate(ResolveOwnerAlias(c.ownerMap, jhlog.Resolve(dict, id)))
}

func (c *collector) deobfuscate(value string) string {
	if c.nameMap == nil {
		return value
	}
	return c.nameMap.Deobfuscate(value)
}

func (c *collector) flowKey(screenOverride, ownerOverride string) string {
	return c.contextKey(screenOverride, ownerOverride, "", "")
}

func (c *collector) contextKey(screenOverride, ownerOverride, flowOverride, stepOverride string) string {
	return strings.Join([]string{
		firstKnown(screenOverride, c.currentAttrScreen),
		firstKnown(flowOverride, c.currentAttrFlow),
		firstKnown(stepOverride, c.currentAttrStep),
		firstKnown(ownerOverride, c.currentAttrOwner),
	}, "\x00")
}

func (c *collector) flowContextFromKey(key string) FlowStats {
	parts := strings.Split(key, "\x00")
	for len(parts) < 4 {
		parts = append(parts, "unknown")
	}
	return FlowStats{
		Screen: attrValue(parts[0]),
		Flow:   attrValue(parts[1]),
		Step:   attrValue(parts[2]),
		Owner:  attrValue(parts[3]),
	}
}

func (c *collector) ensureFlow(key string) *FlowStats {
	stats := c.flowStats[key]
	if stats != nil {
		return stats
	}
	context := c.flowContextFromKey(key)
	stats = &FlowStats{
		Screen: context.Screen,
		Flow:   context.Flow,
		Step:   context.Step,
		Owner:  context.Owner,
	}
	c.flowStats[key] = stats
	return stats
}

func (c *collector) sampleSet(target map[string]*uint64SampleSet, key string) *uint64SampleSet {
	set := target[key]
	if set == nil {
		set = &uint64SampleSet{}
		target[key] = set
	}
	return set
}

func (c *collector) gauge(name string) *gaugeStats {
	stats := c.gaugeValues[name]
	if stats == nil {
		stats = &gaugeStats{}
		c.gaugeValues[name] = stats
	}
	return stats
}

func (c *collector) addMemoryLeakSuspect(className, holder string, context FlowStats, ageMs, count uint64) {
	className = attrValue(className)
	holder = firstKnown(holder, context.Owner, className)
	key := strings.Join([]string{className, holder, context.Screen, context.Flow, context.Step}, "\x00")
	stats := c.memoryLeakStats[key]
	if stats == nil {
		stats = &memoryLeakStats{
			className: className,
			holder:    holder,
			screen:    context.Screen,
			flow:      context.Flow,
			step:      context.Step,
		}
		c.memoryLeakStats[key] = stats
	}
	stats.count += count
	if ageMs > stats.maxAgeMs {
		stats.maxAgeMs = ageMs
	}
}

func (c *collector) addHeapOnlyMemoryLeaks() {
	if c.heap == nil {
		return
	}
	for _, leak := range c.heap.Leaks {
		className := attrValue(c.deobfuscate(leak.ClassName))
		if className == "unknown" || c.hasMemoryLeakClass(className) {
			continue
		}
		count := leak.RetainedObjectCount
		if count == 0 {
			count = 1
		}
		holder := c.deobfuscate(firstKnown(leak.Holder, leak.HolderField))
		if !c.matchesFilters("", FlowStats{}, []string{className}, holder) {
			continue
		}
		c.addMemoryLeakSuspect(className, holder, FlowStats{}, 0, count)
		c.summary.Retained += count
		stats := c.retainedClasses[className]
		if stats == nil {
			stats = &retainedClassStats{}
			c.retainedClasses[className] = stats
		}
		stats.count += count
	}
}

func (c *collector) hasMemoryLeakClass(className string) bool {
	for _, stats := range c.memoryLeakStats {
		if stats != nil && stats.className == className {
			return true
		}
	}
	return false
}

func firstKnown(values ...string) string {
	for _, value := range values {
		value = attrValue(value)
		if value != "unknown" {
			return value
		}
	}
	return "unknown"
}

func attrValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || value == "id:0" {
		return "unknown"
	}
	return value
}

func logLevelName(level uint64) string {
	switch level {
	case 2:
		return "verbose"
	case 3:
		return "debug"
	case 4:
		return "info"
	case 5:
		return "warn"
	case 6:
		return "error"
	case 7:
		return "assert"
	default:
		return fmt.Sprintf("level-%d", level)
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func counterDelta(first, last uint64) uint64 {
	if last >= first {
		return last - first
	}
	return last
}

func (c *collector) finish() Summary {
	summary := c.summary
	if c.logsWithEvents > 0 {
		summary.DurationMS = c.totalLogDurationMS
	} else if c.seenEvent && c.lastTime >= c.firstTime {
		summary.DurationMS = c.lastTime - c.firstTime
	}
	if summary.LogCount > 1 && c.logsWithEvents > 1 {
		summary.Warnings = append(
			summary.Warnings,
			"Несколько логов считаются независимыми прогонами: длительность в обзоре равна сумме длительностей логов, а math timeline накладывает события по относительному времени.",
		)
	}
	summary.TrafficRxMax = c.totalTrafficRxBytes
	summary.TrafficTxMax = c.totalTrafficTxBytes

	for route, set := range c.routeDurations {
		durations := set.sortedValues()
		ttfbAvg := uint64(0)
		if c.routeTTFBCount[route] > 0 {
			ttfbAvg = c.routeTTFB[route] / c.routeTTFBCount[route]
		}
		summary.Routes = append(summary.Routes, RouteStats{
			Route:          route,
			Count:          set.seen,
			Sampled:        set.sampled(),
			Failures:       c.routeFailures[route],
			P50MS:          percentileSorted(durations, 0.50),
			P95MS:          percentileSorted(durations, 0.95),
			P95Approximate: set.isApproximated(),
			MaxMS:          set.max,
			AvgTTFBMS:      ttfbAvg,
			BytesRx:        c.routeRx[route],
			BytesTx:        c.routeTx[route],
			OwnerSample:    c.routeOwner[route],
		})
	}
	summary.HTTPP95MS = percentileSorted(c.httpDurations.sortedValues(), 0.95)
	summary.HTTPP95Approximate = c.httpDurations.isApproximated()

	for _, stats := range c.screenStats {
		if stats.Frames > 0 {
			stats.JankRatePct = float64(stats.JankyFrames) * 100 / float64(stats.Frames)
		}
		stats.AvgFPS = fps(stats.Frames, stats.WindowMS)
		summary.Screens = append(summary.Screens, *stats)
	}
	if summary.UIFrames > 0 {
		summary.UIJankPct = float64(summary.UIJank) * 100 / float64(summary.UIFrames)
	}
	summary.UIAvgFPS = fps(summary.UIFrames, summary.UIWindowMS)

	for _, stats := range c.ownerStats {
		summary.Owners = append(summary.Owners, *stats)
	}
	for key, stats := range c.flowStats {
		if durations := c.flowHTTPDurations[key]; durations != nil {
			stats.HTTPP95MS = percentileSorted(durations.sortedValues(), 0.95)
			stats.HTTPP95Approximate = durations.isApproximated()
		}
		if stats.UIFrames > 0 {
			stats.UIJankPct = float64(stats.UIJank) * 100 / float64(stats.UIFrames)
		}
		summary.Flows = append(summary.Flows, *stats)
	}
	for _, stats := range c.logSpamStats {
		summary.LogSpam = append(summary.LogSpam, *stats)
	}
	for _, stats := range c.problemStats {
		summary.ProblemWindows = append(summary.ProblemWindows, *stats)
	}
	for _, stats := range c.runtimeCallStats {
		summary.RuntimeCalls = append(summary.RuntimeCalls, *stats)
	}
	for name, value := range c.counterValues {
		summary.Counters = append(summary.Counters, NamedValue{Name: name, Value: value})
		if strings.HasPrefix(name, "jankstats.") {
			summary.JankStats = append(summary.JankStats, NamedValue{Name: name, Value: value})
		}
	}
	for name, values := range c.gaugeValues {
		value := values.value()
		extra := values.extra()
		summary.Gauges = append(summary.Gauges, NamedValue{Name: name, Value: value, Extra: extra})
		if strings.HasPrefix(name, "jankstats.") {
			summary.JankStats = append(summary.JankStats, NamedValue{Name: name, Value: value, Extra: extra})
		}
	}

	for name, value := range c.networkSamples {
		summary.Network = append(summary.Network, NamedValue{Name: name, Value: value})
	}
	for name, value := range c.appVersions {
		summary.AppVersions = append(summary.AppVersions, NamedValue{Name: name, Value: value})
	}
	for name, value := range c.builds {
		summary.Builds = append(summary.Builds, NamedValue{Name: name, Value: value})
	}
	for name, value := range c.devices {
		summary.Devices = append(summary.Devices, NamedValue{Name: name, Value: value})
	}
	for name, value := range c.sdks {
		summary.SDKs = append(summary.SDKs, NamedValue{Name: name, Value: value})
	}
	for name, value := range c.cohortSamples {
		summary.Cohorts = append(summary.Cohorts, NamedValue{Name: name, Value: value})
	}
	for name, value := range c.processSamples {
		summary.Processes = append(summary.Processes, NamedValue{Name: name, Value: value})
	}
	c.addHeapOnlyMemoryLeaks()
	for name, stats := range c.retainedClasses {
		summary.RetainedClasses = append(summary.RetainedClasses, NamedValue{
			Name:  name,
			Value: stats.count,
			Extra: fmt.Sprintf("max_age_ms=%d", stats.maxAgeMs),
		})
	}
	for bucket, value := range c.retainedAgeBuckets {
		summary.RetainedAgeBuckets = append(summary.RetainedAgeBuckets, NamedValue{Name: bucket, Value: value})
	}
	summary.MemoryLeaks = buildMemoryLeakSuspects(c.memoryLeakStats, summary.LowMemoryCount, summary.MemoryMaxKB, c.heap)
	if c.heap != nil {
		summary.Warnings = append(summary.Warnings, c.heap.Warnings...)
	}
	summary.Memory = append(summary.Memory, NamedValue{Name: "max_pss_kb", Value: summary.MemoryMaxKB, Extra: formatMB(summary.MemoryMaxKB)})
	if summary.AvailMemoryMinKB > 0 {
		summary.Memory = append(summary.Memory, NamedValue{Name: "min_available_kb", Value: summary.AvailMemoryMinKB, Extra: formatMB(summary.AvailMemoryMinKB)})
	}
	if summary.ContextCount > 0 {
		summary.Memory = append(summary.Memory, NamedValue{Name: "low_memory_samples", Value: uint64(summary.LowMemoryCount)})
	}
	summary.Environment = c.runEnvironment(summary)
	summary.Warnings = append(summary.Warnings, c.telemetryHealthWarnings(summary)...)
	summary.Warnings = append(summary.Warnings, c.sampleWarnings(summary)...)
	summary.Warnings = append(summary.Warnings, c.filterWarnings(summary)...)

	sortRoutes(summary.Routes)
	sortScreens(summary.Screens)
	sortOwners(summary.Owners)
	sortFlows(summary.Flows)
	sortLogSpam(summary.LogSpam)
	sortProblems(summary.ProblemWindows)
	sortRuntimeCalls(summary.RuntimeCalls)
	sortNamed(summary.AppVersions)
	sortNamed(summary.Builds)
	sortNamed(summary.Devices)
	sortNamed(summary.SDKs)
	sortNamed(summary.Cohorts)
	sortNamed(summary.Processes)
	sortNamed(summary.Network)
	sortNamed(summary.RetainedClasses)
	sortNamed(summary.RetainedAgeBuckets)
	sortMemoryLeaks(summary.MemoryLeaks)
	sortNamed(summary.JankStats)
	sortNamed(summary.Counters)
	sortNamed(summary.Gauges)
	summary.Influence = BuildInfluence(summary, c.classGraph)
	summary.CodeProblems = BuildCodeProblemRegistry(summary)
	return summary
}

func (c *collector) telemetryHealthWarnings(summary Summary) []string {
	warnings := c.runtimeQualityWarnings()
	warnings = append(warnings, c.instrumentationQualityWarnings()...)
	warnings = append(warnings, c.attributionQualityWarnings(summary)...)
	return warnings
}

func (c *collector) runtimeQualityWarnings() []string {
	var warnings []string
	for _, item := range runtimeQualityCounterWarnings {
		if value := c.counterValues[item.name]; value > 0 {
			warnings = append(warnings, fmt.Sprintf("Качество сбора: %s: %d.", item.label, value))
		}
	}
	if c.dictionaryOverflow > 0 {
		warnings = append(warnings, fmt.Sprintf("Качество сбора: словарь .jhlog переполнен, overflow-записей: %d; часть имен стала неразличимой.", c.dictionaryOverflow))
	}
	return warnings
}

func (c *collector) instrumentationQualityWarnings() []string {
	var warnings []string
	diagnostics := c.diagnostics
	if diagnostics == nil || !diagnostics.Available {
		warnings = append(warnings, "Качество сбора: ASM-диагностика не передана; нельзя подтвердить, какие bytecode hooks реально сработали.")
	} else {
		if diagnostics.ClassCount == 0 {
			warnings = append(warnings, "Качество сбора: ASM-диагностика пустая, значит instrument matcher не увидел классы или артефакт не был собран.")
		}
		if diagnostics.ClassCount > 0 && diagnostics.HookCount == 0 && diagnostics.AnnotatedMethodCount == 0 {
			warnings = append(warnings, "Качество сбора: ASM прошел по классам, но не нашел hooks или аннотации; проверьте include/exclude, версии библиотек и включенные bridge-флаги.")
		}
		if unsupported := unsupportedDecisionCount(diagnostics); unsupported > 0 {
			warnings = append(warnings, fmt.Sprintf("Качество сбора: ASM встретил неподдержанные сигнатуры hooks: %d; часть телеметрии могла не попасть в лог.", unsupported))
		}
	}
	return warnings
}

func (c *collector) attributionQualityWarnings(summary Summary) []string {
	var warnings []string
	if totalProblemWindows(summary) > 0 && unknownProblemOwnerRate(summary.ProblemWindows) >= 0.8 {
		warnings = append(warnings, "Качество сбора: большинство проблемных окон не имеют понятного owner; добавьте ownerHint/withOwner или проверьте owner-map.")
	}
	if len(summary.Flows) > 0 && unknownFlowContextRate(summary.Flows) >= 0.8 {
		warnings = append(warnings, "Качество сбора: большинство сценариев не имеют screen/flow/step/owner; проверьте автотрекинг Activity, @JankHunterTrace/withFlow/withOwner и ASM owner-map.")
	}
	if summary.EventCount > 0 && datavalue.IsUnknown(c.currentDevice) {
		warnings = append(warnings, "Качество сбора: модель устройства не записана в session-событие; проверьте JankHunter init и device snapshot при старте runtime.")
	}
	if summary.EventCount > 0 && datavalue.IsUnknown(c.currentAppVersion) && datavalue.IsUnknown(c.currentBuild) {
		warnings = append(warnings, "Качество сбора: версия приложения не записана в session-событие; проверьте PackageInfo/versionName/versionCode на старте runtime.")
	}
	if summary.EventCount > 0 && len(summary.Processes) == 1 && datavalue.IsUnknown(summary.Processes[0].Name) {
		warnings = append(warnings, "Качество сбора: процесс неизвестен; проверьте session-события и mainProcessOnly/allowedProcesses.")
	}
	return warnings
}

func unsupportedDecisionCount(diagnostics *InstrumentationDiagnostics) uint64 {
	var total uint64
	if diagnostics == nil {
		return 0
	}
	for _, decision := range diagnostics.Decisions {
		if decision.Kind == "unsupported" || decision.Reason == "unsupported_signature" {
			total += decision.Count
		}
	}
	return total
}

func unknownProblemOwnerRate(problems []ProblemWindowStats) float64 {
	var total uint64
	var unknown uint64
	for _, problem := range problems {
		if problem.Count == 0 {
			continue
		}
		total += problem.Count
		if datavalue.IsUnknown(problem.Owner) {
			unknown += problem.Count
		}
	}
	if total == 0 {
		return 0
	}
	return float64(unknown) / float64(total)
}

func unknownFlowContextRate(flows []FlowStats) float64 {
	var total uint64
	var unknown uint64
	for _, flow := range flows {
		count := uint64(flow.HTTPCount) + uint64(flow.StallCount) + flow.LogSpam + flow.ProblemCount + uint64(flow.UIWindows)
		if count == 0 {
			count = 1
		}
		total += count
		if datavalue.IsUnknown(flow.Screen) &&
			datavalue.IsUnknown(flow.Flow) &&
			datavalue.IsUnknown(flow.Step) &&
			datavalue.IsUnknown(flow.Owner) {
			unknown += count
		}
	}
	if total == 0 {
		return 0
	}
	return float64(unknown) / float64(total)
}

func (c *collector) sampleWarnings(summary Summary) []string {
	var warnings []string
	if summary.HTTPP95Approximate {
		warnings = append(warnings, fmt.Sprintf("HTTP p95 рассчитан по reservoir-сэмплу: использовано %d из %d запросов.", c.httpDurations.sampled(), c.httpDurations.seen))
	}
	var approximateRoutes int
	var totalRoutes int
	for _, route := range summary.Routes {
		if route.P95Approximate {
			approximateRoutes++
			totalRoutes += route.Count
		}
	}
	if approximateRoutes > 0 {
		warnings = append(warnings, fmt.Sprintf("P95 маршрутов приблизительный для %d маршрутов; суммарно %d запросов ограничены reservoir-сэмплингом.", approximateRoutes, totalRoutes))
	}
	return warnings
}

func (c *collector) filterWarnings(summary Summary) []string {
	if !filterActive(c.filter) {
		return nil
	}
	var globalSignals []string
	if summary.ContextCount > 0 {
		globalSignals = append(globalSignals, "контекст устройства")
	}
	if len(summary.Counters) > 0 || len(summary.Gauges) > 0 {
		globalSignals = append(globalSignals, "custom metrics")
	}
	if len(globalSignals) == 0 {
		return nil
	}
	return []string{
		fmt.Sprintf(
			"Фильтр применен к событиям с маршрутом, экраном, источником или классом; %s не несут полного контекста выполнения и показаны глобально.",
			strings.Join(globalSignals, " и "),
		),
	}
}

func (c *collector) runEnvironment(summary Summary) RunEnvironment {
	device := unknownIfEmpty(c.currentDevice)
	manufacturer := unknownIfEmpty(c.currentMaker)
	brand := unknownIfEmpty(c.currentBrand)
	hardware := unknownIfEmpty(c.currentHardware)
	board := unknownIfEmpty(c.currentBoard)
	product := unknownIfEmpty(c.currentProduct)
	abi := unknownIfEmpty(c.currentPrimaryABI)
	abis := unknownIfEmpty(c.currentABIs)
	network := unknownIfEmpty(c.currentNetwork)
	app := unknownIfEmpty(c.currentAppVersion)
	build := unknownIfEmpty(c.currentBuild)
	process := unknownIfEmpty(c.currentProcess)

	return RunEnvironment{
		Title:    datavalue.HumanUnknown(device, "неизвестное устройство"),
		Subtitle: fmt.Sprintf("%s · %s · процесс %s", osValue(c.currentAndroid, c.currentSDK), appBuildValue(app, build), datavalue.HumanUnknown(process, "неизвестен")),
		Items: []InfoItem{
			{Label: "Батарея", Value: batteryValue(summary.BatteryLastPct), Detail: batteryDetail(summary)},
			{Label: "Сеть", Value: datavalue.HumanUnknown(network, "неизвестно"), Detail: networkDetail(summary)},
			{Label: "Свободная RAM", Value: formatDataSize(summary.AvailMemoryLastKB), Detail: memoryDetail(summary)},
			{Label: "Свободное хранилище", Value: formatDataSize(summary.FreeStorageKB), Detail: storageDetail(summary)},
			{Label: "Android", Value: osValue(c.currentAndroid, c.currentSDK), Detail: androidDetail(c.currentSDK, c.currentPatch)},
			{Label: "Рут-доступ", Value: rootValue(summary.DeviceRootKnown, summary.DeviceRooted), Detail: rootDetail(summary.DeviceRootKnown, summary.DeviceRooted)},
			{Label: "CPU ABI", Value: datavalue.HumanUnknown(abi, "неизвестно"), Detail: fmt.Sprintf("поддерживаются %s", datavalue.HumanUnknown(abis, "неизвестно"))},
			{Label: "Железо", Value: datavalue.HumanUnknown(hardware, "неизвестно"), Detail: fmt.Sprintf("плата %s · продукт %s", datavalue.HumanUnknown(board, "неизвестна"), datavalue.HumanUnknown(product, "неизвестен"))},
			{Label: "Бренд", Value: datavalue.HumanUnknown(manufacturer, "неизвестно"), Detail: fmt.Sprintf("бренд %s", datavalue.HumanUnknown(brand, "неизвестен"))},
		},
	}
}

func Compare(baseline, candidate Summary) Comparison {
	comparison := Comparison{Baseline: baseline, Candidate: candidate}
	confidence := confidence(baseline, candidate)
	comparison.Deltas = append(comparison.Deltas,
		delta("HTTP p95", baseline.HTTPP95MS, candidate.HTTPP95MS, "мс", true, minUint64(uint64(baseline.HTTPCount), uint64(candidate.HTTPCount))),
		delta("HTTP failures", uint64(baseline.HTTPFailed), uint64(candidate.HTTPFailed), "шт", true, minUint64(uint64(baseline.HTTPCount), uint64(candidate.HTTPCount))),
		deltaFloat("UI jank rate", baseline.UIJankPct, candidate.UIJankPct, "п.п.", true, minUint64(baseline.UIFrames, candidate.UIFrames)),
		deltaFloat("UI avg FPS", baseline.UIAvgFPS, candidate.UIAvgFPS, "FPS", false, minUint64(baseline.UIFrames, candidate.UIFrames)),
		delta("Main-thread stall max", baseline.StallMaxMS, candidate.StallMaxMS, "мс", true, minUint64(uint64(baseline.StallCount), uint64(candidate.StallCount))),
		delta("Max PSS", baseline.MemoryMaxKB, candidate.MemoryMaxKB, "КБ", true, minUint64(uint64(baseline.MemoryCount), uint64(candidate.MemoryCount))),
		delta("Min available memory", baseline.AvailMemoryMinKB, candidate.AvailMemoryMinKB, "КБ", false, minUint64(uint64(baseline.ContextCount), uint64(candidate.ContextCount))),
		delta("UID RX delta", baseline.TrafficRxMax, candidate.TrafficRxMax, "байт", true, minUint64(uint64(baseline.ContextCount), uint64(candidate.ContextCount))),
		delta("UID TX delta", baseline.TrafficTxMax, candidate.TrafficTxMax, "байт", true, minUint64(uint64(baseline.ContextCount), uint64(candidate.ContextCount))),
		delta("Retained objects", baseline.Retained, candidate.Retained, "шт", true, minUint64(baseline.Retained, candidate.Retained)),
		delta("Log spam", totalLogSpam(baseline), totalLogSpam(candidate), "шт", true, minUint64(uint64(len(baseline.LogSpam)), uint64(len(candidate.LogSpam)))),
		delta("Problem windows", totalProblemWindows(baseline), totalProblemWindows(candidate), "шт", true, minUint64(uint64(len(baseline.ProblemWindows)), uint64(len(candidate.ProblemWindows)))),
		mixDelta("Process mix", baseline.Processes, candidate.Processes, minUint64(uint64(baseline.LogCount), uint64(candidate.LogCount))),
		mixDelta("App version mix", baseline.AppVersions, candidate.AppVersions, minUint64(uint64(baseline.LogCount), uint64(candidate.LogCount))),
		mixDelta("SDK mix", baseline.SDKs, candidate.SDKs, minUint64(uint64(baseline.LogCount), uint64(candidate.LogCount))),
		mixDelta("Device mix", baseline.Devices, candidate.Devices, minUint64(uint64(baseline.LogCount), uint64(candidate.LogCount))),
		mixDelta("Network mix", baseline.Network, candidate.Network, minUint64(uint64(baseline.ContextCount), uint64(candidate.ContextCount))),
		mixDelta("Cohort mix", baseline.Cohorts, candidate.Cohorts, minUint64(uint64(baseline.EventCount), uint64(candidate.EventCount))),
	)
	for i := range comparison.Deltas {
		comparison.Deltas[i].Confidence = confidence
		comparison.Deltas[i].Severity = adjustedSeverity(
			comparison.Deltas[i].Severity,
			confidence,
			comparison.Deltas[i].SampleSize,
		)
	}
	comparison.Warnings = cohortWarnings(baseline, candidate)
	return comparison
}

func mixDelta(name string, baseline, candidate []NamedValue, sampleSize uint64) Delta {
	before := namedSummary(baseline)
	after := namedSummary(candidate)
	severity := "ok"
	change := "без изменений"
	if before != after {
		severity = "medium"
		change = "изменилось"
	}
	return Delta{
		Name:       name,
		Baseline:   before,
		Candidate:  after,
		Change:     change,
		Severity:   severity,
		SampleSize: sampleSize,
	}
}

func totalLogSpam(summary Summary) uint64 {
	var total uint64
	for _, item := range summary.LogSpam {
		total += item.Count
	}
	return total
}

func totalProblemWindows(summary Summary) uint64 {
	var total uint64
	for _, item := range summary.ProblemWindows {
		total += uint64(item.Windows)
	}
	return total
}

func cohortWarnings(baseline, candidate Summary) []string {
	checks := []struct {
		name      string
		baseline  []NamedValue
		candidate []NamedValue
	}{
		{name: "версий приложения", baseline: baseline.AppVersions, candidate: candidate.AppVersions},
		{name: "SDK", baseline: baseline.SDKs, candidate: candidate.SDKs},
		{name: "устройств", baseline: baseline.Devices, candidate: candidate.Devices},
		{name: "процессов", baseline: baseline.Processes, candidate: candidate.Processes},
		{name: "сетей", baseline: baseline.Network, candidate: candidate.Network},
		{name: "когорт", baseline: baseline.Cohorts, candidate: candidate.Cohorts},
	}
	var warnings []string
	for _, check := range checks {
		before := namedSummary(check.baseline)
		after := namedSummary(check.candidate)
		if before != after {
			warnings = append(warnings, fmt.Sprintf("Состав %s отличается: база [%s], кандидат [%s].", check.name, before, after))
		}
	}
	return warnings
}

func confidence(baseline, candidate Summary) string {
	minLogs := baseline.LogCount
	if candidate.LogCount < minLogs {
		minLogs = candidate.LogCount
	}
	minEvents := baseline.EventCount
	if candidate.EventCount < minEvents {
		minEvents = candidate.EventCount
	}
	switch {
	case minLogs >= 5 && minEvents >= 500:
		return "high"
	case minLogs >= 2 && minEvents >= 80:
		return "medium"
	default:
		return "low"
	}
}

type ownerStatKey struct {
	owner string
	kind  string
}

func addOwner(stats map[ownerStatKey]*OwnerStats, owner, kind string, duration uint64, stack string) {
	if owner == "" {
		owner = "unknown"
	}
	key := ownerStatKey{owner: owner, kind: kind}
	item := stats[key]
	if item == nil {
		item = &OwnerStats{Owner: owner, Kind: kind}
		stats[key] = item
	}
	item.Count++
	item.TotalMS += duration
	if duration > item.MaxMS {
		item.MaxMS = duration
	}
	if item.StackHint == "" {
		item.StackHint = stack
	}
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

func fps(frames uint64, windowMS uint64) float64 {
	if frames == 0 || windowMS == 0 {
		return 0
	}
	return float64(frames) * 1000 / float64(windowMS)
}

func sortRoutes(routes []RouteStats) {
	sort.Slice(routes, func(i, j int) bool {
		if routes[i].P95MS == routes[j].P95MS {
			return routes[i].Count > routes[j].Count
		}
		return routes[i].P95MS > routes[j].P95MS
	})
}

func sortScreens(screens []ScreenStats) {
	sort.Slice(screens, func(i, j int) bool {
		if screens[i].JankRatePct == screens[j].JankRatePct {
			return screens[i].P95MS > screens[j].P95MS
		}
		return screens[i].JankRatePct > screens[j].JankRatePct
	})
}

func sortOwners(owners []OwnerStats) {
	sort.Slice(owners, func(i, j int) bool {
		if owners[i].MaxMS == owners[j].MaxMS {
			return owners[i].TotalMS > owners[j].TotalMS
		}
		return owners[i].MaxMS > owners[j].MaxMS
	})
}

func sortFlows(flows []FlowStats) {
	sort.Slice(flows, func(i, j int) bool {
		left := flowSeverityScore(flows[i])
		right := flowSeverityScore(flows[j])
		if left == right {
			return flows[i].Flow < flows[j].Flow
		}
		return left > right
	})
}

func flowSeverityScore(flow FlowStats) uint64 {
	return flow.ProblemCount*10_000 +
		uint64(flow.StallCount)*5_000 +
		flow.UIJank*100 +
		flow.LogSpam*10 +
		uint64(flow.HTTPFailed)*500 +
		flow.HTTPP95MS +
		flow.ProblemMaxMS
}

func sortLogSpam(items []LogSpamStats) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Count == items[j].Count {
			return items[i].Source < items[j].Source
		}
		return items[i].Count > items[j].Count
	})
}

func sortProblems(items []ProblemWindowStats) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].MaxMS == items[j].MaxMS {
			return items[i].Count > items[j].Count
		}
		return items[i].MaxMS > items[j].MaxMS
	})
}

func sortRuntimeCalls(items []RuntimeCallStats) {
	sort.Slice(items, func(i, j int) bool {
		left := items[i].TotalMS + items[i].MaxMS*10 + items[i].Count
		right := items[j].TotalMS + items[j].MaxMS*10 + items[j].Count
		if left == right {
			if items[i].Caller == items[j].Caller {
				return items[i].Callee < items[j].Callee
			}
			return items[i].Caller < items[j].Caller
		}
		return left > right
	})
}

func sortMemoryLeaks(items []MemoryLeakSuspect) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Score == items[j].Score {
			if items[i].MaxAgeMS == items[j].MaxAgeMS {
				return items[i].ClassName < items[j].ClassName
			}
			return items[i].MaxAgeMS > items[j].MaxAgeMS
		}
		return items[i].Score > items[j].Score
	})
}

func sortNamed(values []NamedValue) {
	sort.Slice(values, func(i, j int) bool {
		if values[i].Value == values[j].Value {
			return values[i].Name < values[j].Name
		}
		return values[i].Value > values[j].Value
	})
}

func namedSummary(values []NamedValue) string {
	if len(values) == 0 {
		return "неизвестно"
	}
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, fmt.Sprintf("%s:%d", humanSummaryName(value.Name), value.Value))
	}
	return strings.Join(parts, ",")
}

func humanSummaryName(value string) string {
	value = datavalue.HumanUnknown(value, "неизвестно")
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return "неизвестно"
	}
	for i, field := range fields {
		key, raw, ok := strings.Cut(field, "=")
		if !ok {
			fields[i] = datavalue.HumanUnknown(field, "неизвестно")
			continue
		}
		fields[i] = key + "=" + datavalue.HumanUnknown(raw, "неизвестно")
	}
	return strings.Join(fields, " ")
}

func retainedAgeBucket(ageMs uint64) string {
	switch {
	case ageMs < 10_000:
		return "<10s"
	case ageMs < 30_000:
		return "10s-30s"
	case ageMs < 60_000:
		return "30s-60s"
	default:
		return ">=60s"
	}
}

func delta(name string, before, after uint64, unit string, higherIsWorse bool, sampleSize uint64) Delta {
	change := "0"
	severity := "ok"
	changePct := 0.0
	changeAbs := float64(int64(after) - int64(before))
	regressionAbs := 0.0
	regressionPct := 0.0
	if before == 0 && after > 0 {
		change = "+new"
		if higherIsWorse {
			severity = "medium"
			regressionAbs = float64(after)
			regressionPct = 100
		}
	} else if before > 0 {
		diff := int64(after) - int64(before)
		changePct = float64(diff) * 100 / float64(before)
		change = fmt.Sprintf("%+.1f%%", changePct)
		if higherIsWorse {
			if changePct > 0 {
				regressionAbs = float64(diff)
				regressionPct = changePct
			}
			if changePct >= 25 {
				severity = "high"
			} else if changePct >= 10 {
				severity = "medium"
			}
		} else {
			if changePct < 0 {
				regressionAbs = math.Abs(float64(diff))
				regressionPct = math.Abs(changePct)
			}
			if changePct <= -25 {
				severity = "high"
			} else if changePct <= -10 {
				severity = "medium"
			}
		}
	}
	return Delta{
		Name:           name,
		Baseline:       fmt.Sprintf("%d %s", before, unit),
		Candidate:      fmt.Sprintf("%d %s", after, unit),
		Change:         change,
		Severity:       severity,
		Interval:       sampleNote(sampleSize),
		Unit:           unit,
		BaselineValue:  float64(before),
		CandidateValue: float64(after),
		ChangeAbs:      changeAbs,
		ChangePct:      changePct,
		RegressionAbs:  regressionAbs,
		RegressionPct:  regressionPct,
		SampleSize:     sampleSize,
	}
}

func deltaFloat(name string, before, after float64, unit string, higherIsWorse bool, sampleSize uint64) Delta {
	diff := after - before
	severity := "ok"
	regressionAbs := 0.0
	regressionPct := 0.0
	changePct := 0.0
	if before != 0 {
		changePct = diff * 100 / before
	}
	if higherIsWorse {
		if diff > 0 {
			regressionAbs = diff
			regressionPct = math.Abs(changePct)
		}
		if diff >= 3.0 {
			severity = "high"
		} else if diff >= 1.0 {
			severity = "medium"
		}
	} else {
		if diff < 0 {
			regressionAbs = math.Abs(diff)
			regressionPct = math.Abs(changePct)
		}
		if diff <= -5.0 {
			severity = "high"
		} else if diff <= -2.0 {
			severity = "medium"
		}
	}
	return Delta{
		Name:           name,
		Baseline:       fmt.Sprintf("%.2f %s", before, unit),
		Candidate:      fmt.Sprintf("%.2f %s", after, unit),
		Change:         fmt.Sprintf("%+.2f %s", diff, unit),
		Severity:       severity,
		Interval:       sampleNote(sampleSize),
		Unit:           unit,
		BaselineValue:  before,
		CandidateValue: after,
		ChangeAbs:      diff,
		ChangePct:      changePct,
		RegressionAbs:  regressionAbs,
		RegressionPct:  regressionPct,
		SampleSize:     sampleSize,
	}
}

func adjustedSeverity(effectSeverity, confidence string, sampleSize uint64) string {
	if effectSeverity == "ok" {
		return "ok"
	}
	if confidence == "low" || sampleSize < 3 {
		if effectSeverity == "high" {
			return "medium"
		}
	}
	return effectSeverity
}

func sampleNote(sampleSize uint64) string {
	return fmt.Sprintf("выборка=%d", sampleSize)
}

func minUint64(a, b uint64) uint64 {
	if a < b {
		return a
	}
	return b
}

func formatMB(kb uint64) string {
	return fmt.Sprintf("%.1f МБ", float64(kb)/1024)
}

func formatDataSize(kb uint64) string {
	if kb == 0 {
		return "неизвестно"
	}
	if kb >= 1024*1024 {
		return fmt.Sprintf("%.1f ГБ", float64(kb)/(1024*1024))
	}
	return fmt.Sprintf("%.1f МБ", float64(kb)/1024)
}

func unknownIfEmpty(value string) string {
	if value == "" {
		return "unknown"
	}
	return value
}

func osValue(release string, sdk string) string {
	release = unknownIfEmpty(release)
	sdk = unknownIfEmpty(sdk)
	switch {
	case release == "unknown" && sdk == "unknown":
		return "Android неизвестен"
	case release == "unknown":
		return fmt.Sprintf("Android API %s", apiNumber(sdk))
	case sdk == "unknown":
		return fmt.Sprintf("Android %s", release)
	default:
		return fmt.Sprintf("Android %s", release)
	}
}

func appBuildValue(app string, build string) string {
	if app == "unknown" && build == "unknown" {
		return "версия приложения неизвестна"
	}
	if build == "unknown" {
		return app
	}
	if app == "unknown" {
		return fmt.Sprintf("версия неизвестна (%s)", build)
	}
	return fmt.Sprintf("%s (%s)", app, build)
}

func batteryValue(pct uint64) string {
	if pct == 0 {
		return "неизвестно"
	}
	return fmt.Sprintf("%d%%", pct)
}

func batteryDetail(summary Summary) string {
	parts := []string{batteryStateName(summary.BatteryStateLast)}
	if summary.BatteryTempDeciC != 0 {
		parts = append(parts, fmt.Sprintf("%.1f °C", float64(summary.BatteryTempDeciC)/10))
	}
	if summary.BatteryMinPct > 0 {
		parts = append(parts, fmt.Sprintf("мин. %d%%", summary.BatteryMinPct))
	}
	return strings.Join(parts, " · ")
}

func batteryStateName(state uint64) string {
	switch state {
	case 2:
		return "заряжается"
	case 3:
		return "разряжается"
	case 4:
		return "не заряжается"
	case 5:
		return "полный заряд"
	default:
		return "неизвестно"
	}
}

func networkDetail(summary Summary) string {
	return fmt.Sprintf(
		"валидирована %s · лимитная %s · VPN %s",
		yesNoRU(summary.NetworkValidated),
		yesNoRU(summary.NetworkMetered),
		yesNoRU(summary.NetworkVPN),
	)
}

func memoryDetail(summary Summary) string {
	parts := []string{}
	if summary.TotalMemoryKB > 0 {
		parts = append(parts, fmt.Sprintf("всего %s", formatDataSize(summary.TotalMemoryKB)))
	}
	if summary.AvailMemoryMinKB > 0 {
		parts = append(parts, fmt.Sprintf("мин. свободно %s", formatDataSize(summary.AvailMemoryMinKB)))
	}
	if summary.LowMemoryCount > 0 {
		parts = append(parts, fmt.Sprintf("сигналы low-memory %d", summary.LowMemoryCount))
	}
	if len(parts) == 0 {
		return "нет контекста памяти"
	}
	return strings.Join(parts, " · ")
}

func storageDetail(summary Summary) string {
	if summary.TotalStorageKB == 0 {
		return "раздел данных приложения"
	}
	return fmt.Sprintf("из %s раздел данных приложения", formatDataSize(summary.TotalStorageKB))
}

func androidDetail(sdk string, patch string) string {
	patch = unknownIfEmpty(patch)
	sdk = unknownIfEmpty(sdk)
	if patch == "unknown" {
		return fmt.Sprintf("API %s · патч безопасности неизвестен", apiNumber(sdk))
	}
	return fmt.Sprintf("API %s · патч безопасности %s", apiNumber(sdk), patch)
}

func apiNumber(sdk string) string {
	if sdk == "unknown" {
		return "неизвестен"
	}
	return strings.TrimPrefix(sdk, "api-")
}

func yesNoRU(value bool) string {
	if value {
		return "да"
	}
	return "нет"
}

func rootCohortValue(known bool, rooted bool) string {
	if !known {
		return "unknown"
	}
	if rooted {
		return "yes"
	}
	return "no"
}

func rootValue(known bool, rooted bool) string {
	if !known {
		return "неизвестно"
	}
	if rooted {
		return "да"
	}
	return "нет"
}

func rootDetail(known bool, rooted bool) string {
	if !known {
		return "нет сигнала о рут-доступе в метаданных сессии"
	}
	if rooted {
		return "обнаружены признаки рут-доступа"
	}
	return "признаки рут-доступа не найдены"
}
