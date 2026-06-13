package analyze

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func Inspect(title string, logs []jhlog.Log) Summary {
	return InspectWithFilter(title, logs, Filter{})
}

func InspectWithFilter(title string, logs []jhlog.Log, filter Filter) Summary {
	collector := newCollector(title, len(logs), Options{Filter: filter})
	for _, log := range logs {
		collector.summary.Dictionary += len(log.Dict)
		for _, event := range log.Events {
			collector.add(log.Dict, event)
		}
	}
	return collector.finish()
}

func InspectFiles(title string, paths []string) (Summary, error) {
	return InspectFilesWithFilter(title, paths, Filter{})
}

func InspectFilesWithFilter(title string, paths []string, filter Filter) (Summary, error) {
	return InspectFilesWithOptions(title, paths, Options{Filter: filter})
}

func InspectFilesWithOptions(title string, paths []string, options Options) (Summary, error) {
	collector := newCollector(title, len(paths), options)
	for _, path := range paths {
		lastDictSize := 0
		err := jhlog.StreamFile(path, func(event jhlog.Event, dict map[uint64]string) error {
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
	var raw struct {
		Owners  map[string]string `json:"owners"`
		Entries []struct {
			ID    string `json:"id"`
			Owner string `json:"owner"`
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	out := make(map[string]string, len(raw.Owners)+len(raw.Entries))
	for id, owner := range raw.Owners {
		if id == "" || owner == "" {
			continue
		}
		out[id] = owner
		out["jh:"+id] = owner
	}
	for _, entry := range raw.Entries {
		name := firstNonEmpty(entry.Owner, entry.Name, entry.Value)
		if entry.ID == "" || name == "" {
			continue
		}
		out[entry.ID] = name
		out["jh:"+entry.ID] = name
	}
	return out, nil
}

type collector struct {
	summary  Summary
	filter   Filter
	ownerMap map[string]string

	routeDurations []namedDuration
	routeFailures  map[string]int
	routeRx        map[string]uint64
	routeTx        map[string]uint64
	routeTTFB      map[string]uint64
	routeTTFBCount map[string]uint64
	routeOwner     map[string]string

	screenStats        map[string]*ScreenStats
	ownerStats         map[string]*OwnerStats
	counterValues      map[string]uint64
	gaugeValues        map[string][]uint64
	appVersions        map[string]uint64
	builds             map[string]uint64
	devices            map[string]uint64
	sdks               map[string]uint64
	cohortSamples      map[string]uint64
	networkSamples     map[string]uint64
	processSamples     map[string]uint64
	retainedClasses    map[string]*retainedClassStats
	retainedAgeBuckets map[string]uint64

	currentAppVersion string
	currentBuild      string
	currentDevice     string
	currentSDK        string
	currentProcess    string
	currentNetwork    string
}

type namedDuration struct {
	name     string
	duration uint64
}

func newCollector(title string, logCount int, options Options) *collector {
	return &collector{
		summary:            Summary{Title: title, LogCount: logCount},
		filter:             normalizeFilter(options.Filter),
		ownerMap:           options.OwnerMap,
		routeFailures:      map[string]int{},
		routeRx:            map[string]uint64{},
		routeTx:            map[string]uint64{},
		routeTTFB:          map[string]uint64{},
		routeTTFBCount:     map[string]uint64{},
		routeOwner:         map[string]string{},
		screenStats:        map[string]*ScreenStats{},
		ownerStats:         map[string]*OwnerStats{},
		counterValues:      map[string]uint64{},
		gaugeValues:        map[string][]uint64{},
		appVersions:        map[string]uint64{},
		builds:             map[string]uint64{},
		devices:            map[string]uint64{},
		sdks:               map[string]uint64{},
		cohortSamples:      map[string]uint64{},
		networkSamples:     map[string]uint64{},
		processSamples:     map[string]uint64{},
		retainedClasses:    map[string]*retainedClassStats{},
		retainedAgeBuckets: map[string]uint64{},
		currentAppVersion:  "unknown",
		currentBuild:       "unknown",
		currentDevice:      "unknown",
		currentSDK:         "unknown",
		currentProcess:     "unknown",
		currentNetwork:     "unknown",
	}
}

type retainedClassStats struct {
	count    uint64
	maxAgeMs uint64
}

func normalizeFilter(filter Filter) Filter {
	return Filter{
		RouteContains:  strings.ToLower(filter.RouteContains),
		ScreenContains: strings.ToLower(filter.ScreenContains),
		OwnerContains:  strings.ToLower(filter.OwnerContains),
	}
}

func containsFilter(value string, needle string) bool {
	if needle == "" {
		return true
	}
	return strings.Contains(strings.ToLower(value), needle)
}

func (c *collector) add(dict map[uint64]string, event jhlog.Event) {
	c.summary.EventCount++
	if event.TimeMS > c.summary.DurationMS {
		c.summary.DurationMS = event.TimeMS
	}
	switch {
	case event.Session != nil:
		c.currentAppVersion = jhlog.Resolve(dict, event.Session.AppVersionID)
		c.currentBuild = jhlog.Resolve(dict, event.Session.BuildID)
		c.currentDevice = jhlog.Resolve(dict, event.Session.DeviceID)
		c.currentSDK = fmt.Sprintf("api-%d", event.Session.SDKInt)
		c.currentProcess = jhlog.Resolve(dict, event.Session.ProcessID)
		c.appVersions[c.currentAppVersion]++
		c.builds[c.currentBuild]++
		c.devices[c.currentDevice]++
		c.sdks[c.currentSDK]++
		c.processSamples[c.currentProcess]++
	case event.HTTP != nil:
		c.markCohort()
		route := jhlog.Resolve(dict, event.HTTP.RouteID)
		owner := c.resolveOwner(dict, event.HTTP.OwnerID)
		if !containsFilter(route, c.filter.RouteContains) || !containsFilter(owner, c.filter.OwnerContains) {
			return
		}
		c.summary.HTTPCount++
		c.routeDurations = append(c.routeDurations, namedDuration{route, event.HTTP.DurationMS})
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
	case event.UIWindow != nil:
		c.markCohort()
		screen := jhlog.Resolve(dict, event.UIWindow.ScreenID)
		if !containsFilter(screen, c.filter.ScreenContains) {
			return
		}
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
	case event.Stall != nil:
		c.markCohort()
		owner := c.resolveOwner(dict, event.Stall.OwnerID)
		stack := jhlog.Resolve(dict, event.Stall.StackID)
		if !containsFilter(owner, c.filter.OwnerContains) {
			return
		}
		c.summary.StallCount++
		if event.Stall.DurationMS > c.summary.StallMaxMS {
			c.summary.StallMaxMS = event.Stall.DurationMS
		}
		addOwner(c.ownerStats, owner, "main_thread_stall", event.Stall.DurationMS, stack)
	case event.Context != nil:
		c.summary.ContextCount++
		c.currentNetwork = jhlog.NetworkName(event.Context.Network)
		c.markCohort()
		c.summary.BatteryLastPct = event.Context.BatteryPct
		if c.summary.BatteryMinPct == 0 || event.Context.BatteryPct < c.summary.BatteryMinPct {
			c.summary.BatteryMinPct = event.Context.BatteryPct
		}
		if c.summary.AvailMemoryMinKB == 0 || event.Context.AvailMemoryKB < c.summary.AvailMemoryMinKB {
			c.summary.AvailMemoryMinKB = event.Context.AvailMemoryKB
		}
		if event.Context.LowMemory {
			c.summary.LowMemoryCount++
		}
		if event.Context.RxBytes > c.summary.TrafficRxMax {
			c.summary.TrafficRxMax = event.Context.RxBytes
		}
		if event.Context.TxBytes > c.summary.TrafficTxMax {
			c.summary.TrafficTxMax = event.Context.TxBytes
		}
		c.networkSamples[c.currentNetwork]++
	case event.Memory != nil:
		c.markCohort()
		if event.Memory.PSSKB > c.summary.MemoryMaxKB {
			c.summary.MemoryMaxKB = event.Memory.PSSKB
		}
	case event.Retained != nil:
		c.markCohort()
		className := jhlog.Resolve(dict, event.Retained.ClassID)
		if !containsFilter(className, c.filter.OwnerContains) {
			return
		}
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
		addOwner(c.ownerStats, className, "retained_object", event.Retained.AgeMS, "")
	case event.Metric != nil:
		c.markCohort()
		name := jhlog.Resolve(dict, event.Metric.MetricID)
		if event.Type == jhlog.EventGauge {
			c.gaugeValues[name] = append(c.gaugeValues[name], event.Metric.Value)
		} else {
			c.counterValues[name] += event.Metric.Value
		}
	}
}

func (c *collector) markCohort() {
	c.cohortSamples[fmt.Sprintf(
		"app=%s build=%s sdk=%s device=%s process=%s network=%s",
		c.currentAppVersion,
		c.currentBuild,
		c.currentSDK,
		c.currentDevice,
		c.currentProcess,
		c.currentNetwork,
	)]++
}

func (c *collector) resolveOwner(dict map[uint64]string, id uint64) string {
	owner := jhlog.Resolve(dict, id)
	if len(c.ownerMap) == 0 {
		return owner
	}
	if mapped, ok := c.ownerMap[owner]; ok {
		return mapped
	}
	if hash, ok := ownerHash(owner); ok {
		if mapped, ok := c.ownerMap[hash]; ok {
			return mapped
		}
		if mapped, ok := c.ownerMap["jh:"+hash]; ok {
			return mapped
		}
	}
	return owner
}

func ownerHash(owner string) (string, bool) {
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func (c *collector) finish() Summary {
	summary := c.summary
	routeDurations := map[string][]uint64{}
	for _, item := range c.routeDurations {
		routeDurations[item.name] = append(routeDurations[item.name], item.duration)
	}

	var allHTTPDurations []uint64
	for route, durations := range routeDurations {
		sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
		allHTTPDurations = append(allHTTPDurations, durations...)
		ttfbAvg := uint64(0)
		if c.routeTTFBCount[route] > 0 {
			ttfbAvg = c.routeTTFB[route] / c.routeTTFBCount[route]
		}
		summary.Routes = append(summary.Routes, RouteStats{
			Route:       route,
			Count:       len(durations),
			Failures:    c.routeFailures[route],
			P50MS:       percentileSorted(durations, 0.50),
			P95MS:       percentileSorted(durations, 0.95),
			MaxMS:       durations[len(durations)-1],
			AvgTTFBMS:   ttfbAvg,
			BytesRx:     c.routeRx[route],
			BytesTx:     c.routeTx[route],
			OwnerSample: c.routeOwner[route],
		})
	}
	sort.Slice(allHTTPDurations, func(i, j int) bool { return allHTTPDurations[i] < allHTTPDurations[j] })
	summary.HTTPP95MS = percentileSorted(allHTTPDurations, 0.95)

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
	for name, value := range c.counterValues {
		summary.Counters = append(summary.Counters, NamedValue{Name: name, Value: value})
		if strings.HasPrefix(name, "jankstats.") {
			summary.JankStats = append(summary.JankStats, NamedValue{Name: name, Value: value})
		}
	}
	for name, values := range c.gaugeValues {
		var total uint64
		var max uint64
		for _, value := range values {
			total += value
			if value > max {
				max = value
			}
		}
		avg := uint64(0)
		if len(values) > 0 {
			avg = total / uint64(len(values))
		}
		summary.Gauges = append(summary.Gauges, NamedValue{Name: name, Value: avg, Extra: fmt.Sprintf("avg=%d max=%d samples=%d", avg, max, len(values))})
		if strings.HasPrefix(name, "jankstats.") {
			summary.JankStats = append(summary.JankStats, NamedValue{Name: name, Value: avg, Extra: fmt.Sprintf("avg=%d max=%d samples=%d", avg, max, len(values))})
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
	summary.Memory = append(summary.Memory, NamedValue{Name: "max_pss_kb", Value: summary.MemoryMaxKB, Extra: formatMB(summary.MemoryMaxKB)})
	if summary.AvailMemoryMinKB > 0 {
		summary.Memory = append(summary.Memory, NamedValue{Name: "min_available_kb", Value: summary.AvailMemoryMinKB, Extra: formatMB(summary.AvailMemoryMinKB)})
	}
	if summary.ContextCount > 0 {
		summary.Memory = append(summary.Memory, NamedValue{Name: "low_memory_samples", Value: uint64(summary.LowMemoryCount)})
	}

	sortRoutes(summary.Routes)
	sortScreens(summary.Screens)
	sortOwners(summary.Owners)
	sortNamed(summary.AppVersions)
	sortNamed(summary.Builds)
	sortNamed(summary.Devices)
	sortNamed(summary.SDKs)
	sortNamed(summary.Cohorts)
	sortNamed(summary.Processes)
	sortNamed(summary.Network)
	sortNamed(summary.RetainedClasses)
	sortNamed(summary.RetainedAgeBuckets)
	sortNamed(summary.JankStats)
	sortNamed(summary.Counters)
	sortNamed(summary.Gauges)
	return summary
}

func Compare(baseline, candidate Summary) Comparison {
	comparison := Comparison{Baseline: baseline, Candidate: candidate}
	confidence := confidence(baseline, candidate)
	comparison.Deltas = append(comparison.Deltas,
		delta("HTTP p95", baseline.HTTPP95MS, candidate.HTTPP95MS, "ms", true, minUint64(uint64(baseline.HTTPCount), uint64(candidate.HTTPCount))),
		delta("HTTP failures", uint64(baseline.HTTPFailed), uint64(candidate.HTTPFailed), "count", true, minUint64(uint64(baseline.HTTPCount), uint64(candidate.HTTPCount))),
		deltaFloat("UI jank rate", baseline.UIJankPct, candidate.UIJankPct, "pp", true, minUint64(baseline.UIFrames, candidate.UIFrames)),
		deltaFloat("UI avg FPS", baseline.UIAvgFPS, candidate.UIAvgFPS, "fps", false, minUint64(baseline.UIFrames, candidate.UIFrames)),
		delta("Main-thread stall max", baseline.StallMaxMS, candidate.StallMaxMS, "ms", true, minUint64(uint64(baseline.StallCount), uint64(candidate.StallCount))),
		delta("Max PSS", baseline.MemoryMaxKB, candidate.MemoryMaxKB, "kb", true, minUint64(uint64(baseline.ContextCount), uint64(candidate.ContextCount))),
		delta("Min available memory", baseline.AvailMemoryMinKB, candidate.AvailMemoryMinKB, "kb", false, minUint64(uint64(baseline.ContextCount), uint64(candidate.ContextCount))),
		delta("UID RX max", baseline.TrafficRxMax, candidate.TrafficRxMax, "bytes", true, minUint64(uint64(baseline.ContextCount), uint64(candidate.ContextCount))),
		delta("UID TX max", baseline.TrafficTxMax, candidate.TrafficTxMax, "bytes", true, minUint64(uint64(baseline.ContextCount), uint64(candidate.ContextCount))),
		delta("Retained objects", baseline.Retained, candidate.Retained, "count", true, minUint64(baseline.Retained, candidate.Retained)),
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
	change := "same"
	if before != after {
		severity = "medium"
		change = "changed"
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

func cohortWarnings(baseline, candidate Summary) []string {
	checks := []struct {
		name      string
		baseline  []NamedValue
		candidate []NamedValue
	}{
		{name: "app version", baseline: baseline.AppVersions, candidate: candidate.AppVersions},
		{name: "SDK", baseline: baseline.SDKs, candidate: candidate.SDKs},
		{name: "device", baseline: baseline.Devices, candidate: candidate.Devices},
		{name: "process", baseline: baseline.Processes, candidate: candidate.Processes},
		{name: "network", baseline: baseline.Network, candidate: candidate.Network},
		{name: "cohort", baseline: baseline.Cohorts, candidate: candidate.Cohorts},
	}
	var warnings []string
	for _, check := range checks {
		before := namedSummary(check.baseline)
		after := namedSummary(check.candidate)
		if before != after {
			warnings = append(warnings, fmt.Sprintf("%s mix differs: baseline [%s], candidate [%s]", check.name, before, after))
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

func addOwner(stats map[string]*OwnerStats, owner, kind string, duration uint64, stack string) {
	if owner == "" {
		owner = "unknown"
	}
	item := stats[owner]
	if item == nil {
		item = &OwnerStats{Owner: owner, Kind: kind}
		stats[owner] = item
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
	index := int(float64(len(values)-1) * p)
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
		return "unknown"
	}
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, fmt.Sprintf("%s:%d", value.Name, value.Value))
	}
	return strings.Join(parts, ",")
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
		Interval:       interval(float64(after), unit, sampleSize),
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
		Interval:       interval(after, unit, sampleSize),
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

func interval(value float64, unit string, sampleSize uint64) string {
	if sampleSize < 2 || value <= 0 {
		return fmt.Sprintf("n=%d", sampleSize)
	}
	margin := value / math.Sqrt(float64(sampleSize))
	low := value - margin
	if low < 0 {
		low = 0
	}
	high := value + margin
	return fmt.Sprintf("approx %.2f..%.2f %s, n=%d", low, high, unit, sampleSize)
}

func minUint64(a, b uint64) uint64 {
	if a < b {
		return a
	}
	return b
}

func formatMB(kb uint64) string {
	return fmt.Sprintf("%.1f MB", float64(kb)/1024)
}
