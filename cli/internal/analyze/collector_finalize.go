package analyze

import (
	"fmt"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func (c *collector) finish() Summary {
	c.finalizeCollectionQuality()
	summary := c.summary
	if c.logsWithEvents > 0 {
		summary.DurationMS = c.totalLogDurationMS
	} else if c.seenEvent && c.lastTime >= c.firstTime {
		summary.DurationMS = c.lastTime - c.firstTime
	}
	if c.logsWithEvents > 1 {
		summary.Warnings = append(
			summary.Warnings,
			"Несколько независимых сессий объединены в отчёте: длительность в обзоре равна сумме длительностей сессий, а математическая временная шкала накладывает события по относительному времени.",
		)
	}
	summary.TrafficRxMax = c.totalTrafficRxBytes
	summary.TrafficTxMax = c.totalTrafficTxBytes

	networkAnalysis := NetworkAnalysis{}
	contextCounts := make(map[string]int, len(c.networkRoutes))
	for key, stats := range c.networkCalls {
		contextCounts[key.route]++
		networkAnalysis.Calls = append(networkAnalysis.Calls, networkCallStats(key, stats))
	}
	for route, stats := range c.networkRoutes {
		burstStatus := "exact_rolling_second"
		if stats.burst.approximate {
			burstStatus = "bounded_approximation"
		}
		maxConcurrency, peakConcurrencyAtMS := maxHTTPConcurrency(stats.intervals)
		row := RouteStats{
			Route:                 route,
			ServiceSample:         stats.serviceSample,
			InitiatorSample:       stats.initiatorSample,
			Count:                 stats.count,
			ContextCount:          contextCounts[route],
			Failures:              stats.failures,
			TransportFailures:     stats.transportFailures,
			HTTP4xx:               stats.http4xx,
			HTTP5xx:               stats.http5xx,
			Canceled:              stats.canceled,
			CacheHits:             stats.cacheHits,
			ReusedConnections:     stats.reusedConnections,
			KnownRequestBytes:     stats.knownRequestBytes,
			KnownResponseBytes:    stats.knownResponseBytes,
			Attempts:              stats.attempts,
			DNSAttempts:           stats.dnsAttempts,
			ConnectAttempts:       stats.connectAttempts,
			TLSAttempts:           stats.tlsAttempts,
			Retries:               stats.retries,
			Redirects:             stats.redirects,
			ConnectFailures:       stats.connectFailures,
			TLSFailures:           stats.tlsFailures,
			P50MS:                 stats.durations.percentile(0.50),
			P95MS:                 stats.durations.percentile(0.95),
			MaxMS:                 stats.durations.max,
			TotalDurationMS:       stats.durationTotal,
			AvgTTFBMS:             phaseAverage(stats, 5),
			MaxConcurrency:        maxConcurrency,
			PeakConcurrencyAtMS:   peakConcurrencyAtMS,
			Phases:                httpPhaseStats(stats),
			BytesRx:               stats.bytesRx,
			BytesTx:               stats.bytesTx,
			OwnerSample:           stats.ownerSample,
			BurstEstimateStatus:   burstStatus,
			PeakRequestsPerSecond: stats.burst.peak,
			PeakWindowStartMS:     stats.burst.peakWindowStartMS,
		}
		summary.Routes = append(summary.Routes, row)
	}
	summary.HTTPP95MS = c.networkTotals.durations.percentile(0.95)
	if c.networkTotals.count > 0 {
		networkAnalysis = c.finalizeNetworkAnalysis(networkAnalysis)
		summary.NetworkAnalysis = &networkAnalysis
	}
	if c.webSocketTotals.opened > 0 || c.webSocketTotals.closed > 0 || c.webSocketTotals.failures > 0 {
		summary.WebSocketAnalysis = c.finalizeWebSocketAnalysis()
	}
	if c.databaseTotals.overall.calls > 0 || c.databaseTransactions.events > 0 || c.databaseEvidence != nil {
		summary.DatabaseAnalysis = c.finalizeDatabaseAnalysis()
	}
	summary.DatabaseCoverage = buildDatabaseCoverage(summary, c.diagnostics)
	if workerAnalysis := c.finalizeWorkerAnalysis(); workerAnalysis != nil {
		summary.WorkerAnalysis = workerAnalysis
	}

	for _, stats := range c.screenStats {
		if stats.Frames > 0 {
			stats.JankRatePct = float64(stats.JankyFrames) * 100 / float64(stats.Frames)
		}
		stats.AvgFPS = fps(stats.FPSMeasuredFrames, stats.FPSMeasuredWindowMS)
		stats.FPSStatus = fpsMeasurementStatus(stats.Frames, stats.FPSMeasuredWindowCount)
		stats.FrameP50MS = jhlog.UIFrameHistogramQuantileMS(stats.FrameDurationBuckets, 50)
		stats.FrameP95MS = jhlog.UIFrameHistogramQuantileMS(stats.FrameDurationBuckets, 95)
		stats.FrameP99MS = jhlog.UIFrameHistogramQuantileMS(stats.FrameDurationBuckets, 99)
		summary.Screens = append(summary.Screens, *stats)
	}
	if summary.UIFrames > 0 {
		summary.UIJankPct = float64(summary.UIJank) * 100 / float64(summary.UIFrames)
	}
	summary.UIAvgFPS = fps(summary.UIFPSMeasuredFrames, summary.UIFPSMeasuredWindowMS)
	summary.UIFPSStatus = fpsMeasurementStatus(summary.UIFrames, summary.UIFPSMeasuredWindowCount)

	for _, stats := range c.ownerStats {
		summary.Owners = append(summary.Owners, *stats)
	}
	for key, stats := range c.signalContextStats {
		if durations := c.signalContextHTTPDurations[key]; durations != nil {
			stats.HTTPP95MS = durations.percentile(0.95)
		}
		if stats.UIFrames > 0 {
			stats.UIJankPct = float64(stats.UIJank) * 100 / float64(stats.UIFrames)
		}
		summary.SignalContexts = append(summary.SignalContexts, *stats)
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
	for _, stats := range c.processExitStats {
		summary.ProcessExits = append(summary.ProcessExits, *stats)
	}
	criticalIOCalls := make([]IOStats, 0, len(c.ioStats))
	for _, aggregate := range c.ioStats {
		criticalIOCalls = append(criticalIOCalls, aggregate.finalize())
	}
	sortIOOperations(criticalIOCalls)
	summary.IOAnalysis = c.ioAnalysis.finalize(criticalIOCalls)
	summary.AsyncAnalysis = c.runtimeAnalysis.async.finalize()
	summary.GCAnalysis = c.runtimeAnalysis.gc.finalize()
	summary.StartupAnalysis = c.runtimeAnalysis.startup.finalize()
	summary.OperationAnalysis = c.operationAnalysis.finalize()
	if androidAnalysis := c.androidAnalysis.finalize(summary.CollectionQuality); androidAnalysis.Available ||
		androidAnalysis.Coverage.CatalogAvailable {
		summary.AndroidComponents = androidAnalysis
		if androidAnalysis.Partial {
			summary.Warnings = append(
				summary.Warnings,
				"Анализ компонентов Android и IPC частичный: "+strings.Join(androidAnalysis.PartialReasons, "; ")+".",
			)
		}
	}
	if operations := summary.OperationAnalysis; operations != nil {
		if operations.MissingFinish > 0 || operations.MissingStart > 0 || operations.DuplicateStart > 0 ||
			operations.InconsistentLifecycle > 0 {
			summary.Warnings = append(summary.Warnings, fmt.Sprintf(
				"Жизненный цикл операций неполон: без завершения=%d, без начала=%d, повторных начал=%d, противоречий=%d.",
				operations.MissingFinish,
				operations.MissingStart,
				operations.DuplicateStart,
				operations.InconsistentLifecycle,
			))
		}
	}
	for name, value := range c.counterValues {
		summary.Counters = append(summary.Counters, NamedValue{Name: name, Value: value})
		if isJankStatsMetric(name) {
			summary.JankStats = append(summary.JankStats, NamedValue{Name: name, Value: value})
		}
	}
	for name, values := range c.gaugeValues {
		value := values.value()
		extra := values.extra()
		summary.Gauges = append(summary.Gauges, NamedValue{Name: name, Value: value, Extra: extra})
		if isJankStatsMetric(name) {
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
	summary.MemoryLeaks = buildMemoryLeakSuspects(
		c.memoryLeakStats,
		summary.LowMemoryCount,
		summary.MemoryMaxKB,
		c.heap,
		c.retentionDataQuality(),
	)
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
	summary.Warnings = append(summary.Warnings, c.filterWarnings(summary)...)

	sortRoutes(summary.Routes)
	if summary.NetworkAnalysis != nil {
		sortNetworkCalls(summary.NetworkAnalysis.Calls)
	}
	sortScreens(summary.Screens)
	sortOwners(summary.Owners)
	sortSignalContexts(summary.SignalContexts)
	sortLogSpam(summary.LogSpam)
	sortProblems(summary.ProblemWindows)
	sortRuntimeCalls(summary.RuntimeCalls)
	sortProcessExits(summary.ProcessExits)
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
	summary.LogGrowth = buildLogGrowthSummary(c.streamResults)
	dependencyInjection := c.dependencyInjection
	// Every base aggregate has been copied into Summary. Drop the mutable collection maps before
	// materializing influence views and the code-problem registry so both representations do not
	// coexist at peak heap usage on large applications.
	c.releaseAggregationState()
	summary.Influence = BuildInfluence(summary, c.classGraph)
	summary.CodeProblems = BuildCodeProblemRegistry(summary)
	summary.AnalysisInputs = c.analysisInputCompleteness(summary)
	problemReport, problemErr := buildProblemReportWithCatalog(
		summary,
		DefaultProblemDetectorConfig(),
		dependencyInjection,
	)
	if problemErr != nil {
		summary.Warnings = append(summary.Warnings, "problem engine: "+problemErr.Error())
	} else {
		summary.ProblemSchemaVersion = ProblemSchemaVersion
		summary.ProblemSummary = problemReport.Summary
		summary.Problems = problemReport.Problems
		summary.ProblemIncidents = problemReport.Incidents
		summary.CategoryCoverage = problemReport.Coverage
		summary.Detectors = problemReport.Registry
	}
	summary.EvidenceQuality = BuildEvidenceQualityVector(summary)
	return summary
}

func (c *collector) releaseAggregationState() {
	c.nameMap = nil
	c.dependencyInjection = nil
	c.networkTotals = httpAggregate{}
	c.networkRoutes = nil
	c.networkCalls = nil
	c.networkStatusCodes = nil
	c.databaseTotals = databaseAggregate{}
	c.databaseStatements = databaseStatementStore{}
	c.workerCollectorState.release()
	c.runtimeAnalysis = runtimeAnalysisAccumulator{}
	c.operationAnalysis.release()
	c.androidAnalysis = nil
	c.databaseCorrelation = databaseCorrelationAccumulator{}
	c.screenStats = nil
	c.processExitStats = nil
	c.ioStats = nil
	c.ownerStats = nil
	c.signalContextStats = nil
	c.signalContextHTTPDurations = nil
	c.logSpamStats = nil
	c.problemStats = nil
	c.runtimeCallStats = nil
	c.counterValues = nil
	c.gaugeValues = nil
	c.appVersions = nil
	c.builds = nil
	c.devices = nil
	c.sdks = nil
	c.cohortSamples = nil
	c.networkSamples = nil
	c.processSamples = nil
	c.retainedClasses = nil
	c.retainedAgeBuckets = nil
	c.memoryLeakStats = nil
	c.qualitySnapshots = nil
	c.streamResults = nil
	c.stableSymbols.embedded = nil
}
