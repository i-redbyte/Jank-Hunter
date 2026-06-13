package analyze

import (
	"fmt"
	"sort"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func Inspect(title string, logs []jhlog.Log) Summary {
	summary := Summary{Title: title, LogCount: len(logs)}

	routeDurations := map[string][]uint64{}
	routeFailures := map[string]int{}
	routeRx := map[string]uint64{}
	routeTx := map[string]uint64{}
	routeTTFB := map[string]uint64{}
	routeTTFBCount := map[string]uint64{}
	routeOwner := map[string]string{}

	screenStats := map[string]*ScreenStats{}
	ownerStats := map[string]*OwnerStats{}
	counterValues := map[string]uint64{}
	gaugeValues := map[string][]uint64{}
	networkSamples := map[string]uint64{}

	for _, log := range logs {
		summary.Dictionary += len(log.Dict)
		for _, event := range log.Events {
			summary.EventCount++
			if event.TimeMS > summary.DurationMS {
				summary.DurationMS = event.TimeMS
			}
			switch {
			case event.HTTP != nil:
				summary.HTTPCount++
				route := jhlog.Resolve(log.Dict, event.HTTP.RouteID)
				owner := jhlog.Resolve(log.Dict, event.HTTP.OwnerID)
				routeDurations[route] = append(routeDurations[route], event.HTTP.DurationMS)
				routeRx[route] += event.HTTP.RxBytes
				routeTx[route] += event.HTTP.TxBytes
				routeTTFB[route] += event.HTTP.TTFBMS
				routeTTFBCount[route]++
				if routeOwner[route] == "" {
					routeOwner[route] = owner
				}
				if event.Flags&uint64(jhlog.FlagHTTPFailed) != 0 || event.HTTP.Status == jhlog.Status5xx {
					summary.HTTPFailed++
					routeFailures[route]++
				}
				addOwner(ownerStats, owner, "http", event.HTTP.DurationMS, "")
			case event.UIWindow != nil:
				screen := jhlog.Resolve(log.Dict, event.UIWindow.ScreenID)
				stats := screenStats[screen]
				if stats == nil {
					stats = &ScreenStats{Screen: screen}
					screenStats[screen] = stats
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
				summary.UIFrames += event.UIWindow.FrameCount
				summary.UIJank += event.UIWindow.JankCount
				summary.UIWindowMS += event.UIWindow.WindowMS
				if summary.UIMinFPS == 0 || windowFPS < summary.UIMinFPS {
					summary.UIMinFPS = windowFPS
				}
			case event.Stall != nil:
				summary.StallCount++
				if event.Stall.DurationMS > summary.StallMaxMS {
					summary.StallMaxMS = event.Stall.DurationMS
				}
				owner := jhlog.Resolve(log.Dict, event.Stall.OwnerID)
				stack := jhlog.Resolve(log.Dict, event.Stall.StackID)
				addOwner(ownerStats, owner, "main_thread_stall", event.Stall.DurationMS, stack)
			case event.Context != nil:
				summary.ContextCount++
				summary.BatteryLastPct = event.Context.BatteryPct
				if summary.BatteryMinPct == 0 || event.Context.BatteryPct < summary.BatteryMinPct {
					summary.BatteryMinPct = event.Context.BatteryPct
				}
				if summary.AvailMemoryMinKB == 0 || event.Context.AvailMemoryKB < summary.AvailMemoryMinKB {
					summary.AvailMemoryMinKB = event.Context.AvailMemoryKB
				}
				if event.Context.LowMemory {
					summary.LowMemoryCount++
				}
				if event.Context.RxBytes > summary.TrafficRxMax {
					summary.TrafficRxMax = event.Context.RxBytes
				}
				if event.Context.TxBytes > summary.TrafficTxMax {
					summary.TrafficTxMax = event.Context.TxBytes
				}
				networkSamples[jhlog.NetworkName(event.Context.Network)]++
			case event.Memory != nil:
				if event.Memory.PSSKB > summary.MemoryMaxKB {
					summary.MemoryMaxKB = event.Memory.PSSKB
				}
			case event.Retained != nil:
				summary.Retained += event.Retained.Count
				className := jhlog.Resolve(log.Dict, event.Retained.ClassID)
				addOwner(ownerStats, className, "retained_object", event.Retained.AgeMS, "")
			case event.Metric != nil:
				name := jhlog.Resolve(log.Dict, event.Metric.MetricID)
				if event.Type == jhlog.EventGauge {
					gaugeValues[name] = append(gaugeValues[name], event.Metric.Value)
				} else {
					counterValues[name] += event.Metric.Value
				}
			}
		}
	}

	var allHTTPDurations []uint64
	for route, durations := range routeDurations {
		sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
		allHTTPDurations = append(allHTTPDurations, durations...)
		ttfbAvg := uint64(0)
		if routeTTFBCount[route] > 0 {
			ttfbAvg = routeTTFB[route] / routeTTFBCount[route]
		}
		summary.Routes = append(summary.Routes, RouteStats{
			Route:       route,
			Count:       len(durations),
			Failures:    routeFailures[route],
			P50MS:       percentileSorted(durations, 0.50),
			P95MS:       percentileSorted(durations, 0.95),
			MaxMS:       durations[len(durations)-1],
			AvgTTFBMS:   ttfbAvg,
			BytesRx:     routeRx[route],
			BytesTx:     routeTx[route],
			OwnerSample: routeOwner[route],
		})
	}
	sort.Slice(allHTTPDurations, func(i, j int) bool { return allHTTPDurations[i] < allHTTPDurations[j] })
	summary.HTTPP95MS = percentileSorted(allHTTPDurations, 0.95)

	for _, stats := range screenStats {
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

	for _, stats := range ownerStats {
		summary.Owners = append(summary.Owners, *stats)
	}
	for name, value := range counterValues {
		summary.Counters = append(summary.Counters, NamedValue{Name: name, Value: value})
	}
	for name, values := range gaugeValues {
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
	}

	for name, value := range networkSamples {
		summary.Network = append(summary.Network, NamedValue{Name: name, Value: value})
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
	sortNamed(summary.Network)
	sortNamed(summary.Counters)
	sortNamed(summary.Gauges)
	return summary
}

func Compare(baseline, candidate Summary) Comparison {
	comparison := Comparison{Baseline: baseline, Candidate: candidate}
	comparison.Deltas = append(comparison.Deltas,
		delta("HTTP p95", baseline.HTTPP95MS, candidate.HTTPP95MS, "ms", true),
		delta("HTTP failures", uint64(baseline.HTTPFailed), uint64(candidate.HTTPFailed), "count", true),
		deltaFloat("UI jank rate", baseline.UIJankPct, candidate.UIJankPct, "pp", true),
		deltaFloat("UI avg FPS", baseline.UIAvgFPS, candidate.UIAvgFPS, "fps", false),
		delta("Main-thread stall max", baseline.StallMaxMS, candidate.StallMaxMS, "ms", true),
		delta("Max PSS", baseline.MemoryMaxKB, candidate.MemoryMaxKB, "kb", true),
		delta("Min available memory", baseline.AvailMemoryMinKB, candidate.AvailMemoryMinKB, "kb", false),
		delta("UID RX max", baseline.TrafficRxMax, candidate.TrafficRxMax, "bytes", true),
		delta("UID TX max", baseline.TrafficTxMax, candidate.TrafficTxMax, "bytes", true),
		delta("Retained objects", baseline.Retained, candidate.Retained, "count", true),
	)
	return comparison
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

func delta(name string, before, after uint64, unit string, higherIsWorse bool) Delta {
	change := "0"
	severity := "ok"
	if before == 0 && after > 0 {
		change = "+new"
		severity = "medium"
	} else if before > 0 {
		diff := int64(after) - int64(before)
		pct := float64(diff) * 100 / float64(before)
		change = fmt.Sprintf("%+.1f%%", pct)
		if higherIsWorse {
			if pct >= 25 {
				severity = "high"
			} else if pct >= 10 {
				severity = "medium"
			}
		} else {
			if pct <= -25 {
				severity = "high"
			} else if pct <= -10 {
				severity = "medium"
			}
		}
	}
	return Delta{
		Name:      name,
		Baseline:  fmt.Sprintf("%d %s", before, unit),
		Candidate: fmt.Sprintf("%d %s", after, unit),
		Change:    change,
		Severity:  severity,
	}
}

func deltaFloat(name string, before, after float64, unit string, higherIsWorse bool) Delta {
	diff := after - before
	severity := "ok"
	if higherIsWorse {
		if diff >= 3.0 {
			severity = "high"
		} else if diff >= 1.0 {
			severity = "medium"
		}
	} else {
		if diff <= -5.0 {
			severity = "high"
		} else if diff <= -2.0 {
			severity = "medium"
		}
	}
	return Delta{
		Name:      name,
		Baseline:  fmt.Sprintf("%.2f %s", before, unit),
		Candidate: fmt.Sprintf("%.2f %s", after, unit),
		Change:    fmt.Sprintf("%+.2f %s", diff, unit),
		Severity:  severity,
	}
}

func formatMB(kb uint64) string {
	return fmt.Sprintf("%.1f MB", float64(kb)/1024)
}
