package analyze

import (
	"fmt"
	"sort"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

type memoryLeakStats struct {
	className            string
	holder               string
	screen               string
	operation            string
	count                uint64
	maxAgeMs             uint64
	timeOnlyCount        uint64
	afterExplicitGCCount uint64
}

type memoryLeakKey struct {
	className string
	holder    string
	screen    string
	operation string
}

type signalContextKey struct {
	screen    string
	operation string
	owner     string
}

type processExitKey struct {
	reason  uint64
	process string
}

type ioStatsKey struct {
	context    signalContextKey
	source     string
	operation  string
	mainThread bool
}

type logSpamKey struct {
	context signalContextKey
	source  string
	level   string
}

type problemWindowKey struct {
	context signalContextKey
	kind    string
}

type runtimeCallKey struct {
	context signalContextKey
	caller  string
	callee  string
}

type retentionDataQuality struct {
	runtimeLoss            uint64
	runtimeMayBeIncomplete bool
	dictionaryDegraded     bool
	heapDegraded           bool
	runtimeNotes           []string
	dictionaryNotes        []string
	heapNotes              []string
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

func (c *collector) eventContext(screenOverride, ownerOverride string) SignalContextStats {
	return SignalContextStats{
		Screen:    attrValue(firstKnown(screenOverride, c.currentAttrScreen)),
		Operation: attrValue(c.operationAnalysis.activeName(c.currentOperationID)),
		Owner:     attrValue(firstKnown(ownerOverride, c.currentAttrOwner)),
	}
}

func (c *collector) matchesFilters(route string, context SignalContextStats, classCandidates []string, ownerCandidates ...string) bool {
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
	if event.Dictionary != nil {
		if event.Dictionary.Kind == jhlog.DictStableSymbol && event.Dictionary.Value != "" {
			if _, exists := c.stableSymbols.embedded[event.Dictionary.ID]; !exists {
				c.stableSymbols.embedded[event.Dictionary.ID] = event.Dictionary.Value
			}
		}
		if event.Dictionary.Value == "__jh_dictionary_overflow__" {
			c.dictionaryOverflow++
		}
		return
	}
	if !event.Type.IsSemanticData() {
		return
	}
	c.applyAttribution(dict, event.Attribution)
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
	if event.Operation != nil {
		c.operationAnalysis.recordLifecycle(dict, event, c.currentAttrScreen, c.filter)
	} else if event.Database == nil && event.DatabaseTransaction == nil {
		c.operationAnalysis.recordSignal(event, c.currentOperationID, c.currentAttrOwner)
	}
	switch {
	case event.Session != nil:
		c.summary.CollectorSessions++
		c.summary.CollectorFlagsAny |= event.Session.CollectorFlags
		if event.Session.CollectorFlags&uint64(jhlog.CollectorDatabase) != 0 {
			c.summary.DatabaseCoverage.RuntimeEnabledSessions++
		}
		if event.Session.CollectorFlags&uint64(jhlog.CollectorWorker) != 0 {
			c.enableWorkerCorrelation()
		}
		if c.summary.CollectorSessions == 1 {
			c.summary.CollectorFlagsAll = event.Session.CollectorFlags
		} else {
			c.summary.CollectorFlagsAll &= event.Session.CollectorFlags
		}
		c.currentAppVersion = jhlog.ResolveSymbol(dict, event.Session.AppVersionRef)
		c.currentBuild = jhlog.ResolveSymbol(dict, event.Session.BuildRef)
		c.currentDevice = jhlog.ResolveSymbol(dict, event.Session.DeviceRef)
		c.currentSDK = fmt.Sprintf("api-%d", event.Session.SDKInt)
		c.currentProcess = firstNonEmpty(event.Session.ProcessName, "unknown")
		c.currentAndroid = jhlog.ResolveSymbol(dict, event.Session.AndroidReleaseRef)
		c.currentPatch = jhlog.ResolveSymbol(dict, event.Session.SecurityPatchRef)
		c.currentPrimaryABI = jhlog.ResolveSymbol(dict, event.Session.PrimaryABIRef)
		c.currentABIs = jhlog.ResolveSymbol(dict, event.Session.SupportedABIsRef)
		c.currentMaker = jhlog.ResolveSymbol(dict, event.Session.ManufacturerRef)
		c.currentBrand = jhlog.ResolveSymbol(dict, event.Session.BrandRef)
		c.currentHardware = jhlog.ResolveSymbol(dict, event.Session.HardwareRef)
		c.currentBoard = jhlog.ResolveSymbol(dict, event.Session.BoardRef)
		c.currentProduct = jhlog.ResolveSymbol(dict, event.Session.ProductRef)
		c.currentRootKnown = true
		c.currentRooted = event.Session.DeviceRooted
		c.currentCohortDirty = true
		c.summary.DeviceRootKnown = true
		c.summary.DeviceRooted = event.Session.DeviceRooted
		c.appVersions[c.currentAppVersion]++
		c.builds[c.currentBuild]++
		c.devices[c.currentDevice]++
		c.sdks[c.currentSDK]++
		c.processSamples[c.currentProcess]++
	case event.HTTP != nil:
		route := attrValue(jhlog.ResolveSymbol(dict, event.HTTP.RouteRef))
		service := attrValue(jhlog.ResolveSymbol(dict, event.HTTP.ServiceRef))
		initiator := attrValue(c.resolveOwnerRef(dict, event.HTTP.InitiatorRef))
		owner := c.currentAttrOwner
		context := c.eventContext("", owner)
		if !c.matchesFilters(route, context, nil, owner, initiator) {
			return
		}
		c.databaseCorrelation.addHTTP(databaseTimelineContext{
			screen: context.Screen, operation: context.Operation, operationID: c.currentOperationID,
		}, event)
		c.markCohort()
		c.summary.HTTPCount++
		if c.workerCorrelationOn {
			c.recordWorkerHTTP(event)
		}
		c.networkTotals.add(event.HTTP, event.Flags, c.currentLogIndex, event.TimeMS, true)
		routeStats := c.networkRoutes[route]
		if routeStats == nil {
			routeStats = &httpAggregate{}
			c.networkRoutes[route] = routeStats
		}
		routeStats.add(event.HTTP, event.Flags, c.currentLogIndex, event.TimeMS, true)
		routeStats.burst.add(c.currentLogIndex, event.TimeMS)
		if routeStats.ownerSample == "" || routeStats.ownerSample == "unknown" {
			routeStats.ownerSample = firstKnown(initiator, owner)
		}
		if routeStats.serviceSample == "" || routeStats.serviceSample == "unknown" {
			routeStats.serviceSample = service
		}
		if routeStats.initiatorSample == "" || routeStats.initiatorSample == "unknown" {
			routeStats.initiatorSample = initiator
		}
		callKey := networkCallKey{
			route: route, service: service, initiator: initiator,
			screen: context.Screen, operation: context.Operation, owner: context.Owner,
		}
		callStats := c.networkCalls[callKey]
		if callStats == nil {
			callStats = &httpAggregate{}
			c.networkCalls[callKey] = callStats
		}
		callStats.add(event.HTTP, event.Flags, c.currentLogIndex, event.TimeMS, false)
		if event.HTTP.StatusCode != 0 {
			c.networkStatusCodes[event.HTTP.StatusCode]++
		}
		if event.HTTP.FailurePhase > jhlog.HTTPFailurePhaseUnknown && int(event.HTTP.FailurePhase) < len(c.networkFailurePhases) {
			c.networkFailurePhases[event.HTTP.FailurePhase]++
		}
		if event.HTTP.FailureKind > jhlog.HTTPFailureKindUnknown && int(event.HTTP.FailureKind) < len(c.networkFailureKinds) {
			c.networkFailureKinds[event.HTTP.FailureKind]++
		}
		if int(event.HTTP.Protocol) < len(c.networkProtocols) {
			c.networkProtocols[event.HTTP.Protocol]++
		}
		if httpEventFailed(event.HTTP, event.Flags) {
			c.summary.HTTPFailed++
		}
		addOwner(c.ownerStats, firstKnown(initiator, owner), "http", event.HTTP.DurationMS, "")
		contextKey := c.contextKey("", owner)
		contextStats := c.ensureSignalContext(contextKey)
		contextStats.HTTPCount++
		contextStats.RouteSample = firstNonEmpty(contextStats.RouteSample, route)
		c.sampleSet(c.signalContextHTTPDurations, contextKey).add(event.HTTP.DurationMS)
		if httpEventFailed(event.HTTP, event.Flags) {
			contextStats.HTTPFailed++
		}
		failed := httpEventFailed(event.HTTP, event.Flags)
		slow := event.Flags&uint64(jhlog.FlagHTTPSlow) != 0
		if failed || slow {
			c.addProblemWindow(context, "http_slow_or_failed", event.HTTP.DurationMS, 1, event.HTTP.DurationMS)
		}
	case event.WebSocket != nil:
		route := attrValue(jhlog.ResolveSymbol(dict, event.WebSocket.RouteRef))
		owner := c.currentAttrOwner
		context := c.eventContext("", owner)
		if !c.matchesFilters(route, context, nil, owner) {
			return
		}
		c.markCohort()
		c.webSocketTotals.add(event.WebSocket)
		key := webSocketKey{route: route, screen: context.Screen, operation: context.Operation, owner: context.Owner}
		stats := c.webSocketConnections[key]
		if stats == nil {
			stats = &webSocketAggregate{}
			c.webSocketConnections[key] = stats
		}
		stats.add(event.WebSocket)
		addOwner(c.ownerStats, owner, "websocket", event.WebSocket.DurationMS, "")
		if event.WebSocket.Stage == jhlog.WebSocketStageFailed {
			c.addProblemWindow(context, "websocket_failure", event.WebSocket.DurationMS, 1, event.WebSocket.DurationMS)
		}
	case event.Database != nil:
		query := attrValue(jhlog.ResolveSymbol(dict, event.Database.QueryRef))
		source := attrValue(c.resolveOwnerRef(dict, event.Database.SourceRef))
		context := c.eventContext("", "")
		if !c.matchesFilters("", context, []string{source}, source) {
			return
		}
		c.markCohort()
		c.databaseTotals.add(event.Database, event.Flags, c.currentLogIndex, event.TimeMS)
		c.databaseTelemetry.add(event.Database)
		framework := databaseFrameworkName(event.Database.Framework)
		operation := databaseOperationName(event.Database.Operation)
		statementKey := canonicalDatabaseStatementKey(query, source, framework, operation)
		contextKey := databaseContextKey{
			statement: statementKey, source: source, framework: framework,
			screen: context.Screen, contextOwner: context.Owner,
			contextOperation: context.Operation, operationID: c.currentOperationID,
			process: c.currentProcess, processInstanceID: c.currentProcessID,
			sessionID: c.currentSessionID,
		}
		estimatedCalls := c.databaseStatements.add(
			statementKey, contextKey, event.Database, event.Flags, c.currentLogIndex, event.TimeMS,
		)
		c.databaseScenarios.add(
			statementKey, contextKey, event.Database, event.Flags, c.currentLogIndex,
			databaseEventTimeUS(event),
		)
		c.databaseCorrelation.addDatabase(contextKey, databaseTimelineContext{
			screen: context.Screen, operation: context.Operation, operationID: c.currentOperationID,
		}, event, estimatedCalls)
		c.operationAnalysis.recordDatabaseStatement(
			c.currentOperationID, query, source, operation, event.Database, event.Flags,
		)
		addOwner(c.ownerStats, source, "database", event.Database.DurationUS/1_000, "")
		if event.Database.Outcome == jhlog.DatabaseOutcomeFailure ||
			(event.Flags&uint64(jhlog.FlagThreadMain) != 0 && event.Database.DurationUS >= 16_000) {
			c.addProblemWindow(context, "database_slow_or_failed", event.Database.DurationUS/1_000, 1, event.Database.DurationUS/1_000)
		}
	case event.DatabaseTransaction != nil:
		source := attrValue(c.resolveOwnerRef(dict, event.DatabaseTransaction.SourceRef))
		context := c.eventContext("", "")
		if !c.matchesFilters("", context, []string{source}, source) {
			return
		}
		c.markCohort()
		transactionKey := databaseTransactionScopeKey(
			c.currentProcessID, c.currentLogIndex, event.DatabaseTransaction.TransactionID,
		)
		c.databaseCorrelation.addTransaction(transactionKey, databaseTimelineContext{
			screen: context.Screen, operation: context.Operation, operationID: c.currentOperationID,
		}, event)
		c.databaseTransactions.add(
			event.DatabaseTransaction,
			event.Flags,
			c.currentLogIndex,
			source,
			context,
			c.currentProcess,
			c.currentProcessID,
			c.currentSessionID,
		)
		if event.DatabaseTransaction.Stage == jhlog.DatabaseTransactionTerminal {
			addOwner(c.ownerStats, source, "database_transaction", event.DatabaseTransaction.DurationUS/1_000, "")
		}
	case event.ProcessState != nil:
		c.androidAnalysis.addProcessState(event)
	case event.AndroidComponent != nil:
		component := attrValue(c.resolveOwnerRef(dict, event.AndroidComponent.ComponentRef))
		action := attrValue(jhlog.ResolveSymbol(dict, event.AndroidComponent.ActionRef))
		context := c.eventContext("", component)
		if c.matchesFilters("", context, []string{component}, component) {
			c.androidAnalysis.addComponent(component, action, event)
		}
	case event.BinderTransaction != nil:
		descriptor := ""
		if !event.BinderTransaction.DescriptorRef.IsUnknown() {
			descriptor = strings.TrimSpace(jhlog.ResolveSymbol(dict, event.BinderTransaction.DescriptorRef))
		}
		method := ""
		if !event.BinderTransaction.MethodRef.IsUnknown() {
			method = strings.TrimSpace(jhlog.ResolveSymbol(dict, event.BinderTransaction.MethodRef))
		}
		context := c.eventContext("", attrValue(descriptor))
		if c.matchesFilters("", context, []string{descriptor}, descriptor, method) {
			c.androidAnalysis.addBinder(descriptor, method, event)
		}
	case event.UIWindow != nil:
		c.runtimeAnalysis.gc.addUIWindow(event, c.currentLogIndex)
		screen := c.currentAttrScreen
		context := c.eventContext(screen, "")
		if !c.matchesFilters("", context, nil) {
			return
		}
		c.databaseCorrelation.addUIWindow(databaseTimelineContext{
			screen: context.Screen, operation: context.Operation, operationID: c.currentOperationID,
		}, event)
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
		if fpsWindowReliable(event.UIWindow) {
			windowFPS := fps(event.UIWindow.FrameCount, event.UIWindow.WindowMS)
			stats.FPSMeasuredFrames += event.UIWindow.FrameCount
			stats.FPSMeasuredWindowMS += event.UIWindow.WindowMS
			stats.FPSMeasuredWindowCount++
			if stats.MinFPS == 0 || windowFPS < stats.MinFPS {
				stats.MinFPS = windowFPS
			}
			c.summary.UIFPSMeasuredFrames += event.UIWindow.FrameCount
			c.summary.UIFPSMeasuredWindowMS += event.UIWindow.WindowMS
			c.summary.UIFPSMeasuredWindowCount++
			if c.summary.UIMinFPS == 0 || windowFPS < c.summary.UIMinFPS {
				c.summary.UIMinFPS = windowFPS
			}
		}
		mergeFrameWindow(stats, event.UIWindow)
		c.summary.UIFrames += event.UIWindow.FrameCount
		c.summary.UIJank += event.UIWindow.JankCount
		c.summary.UIWindowMS += event.UIWindow.WindowMS
		contextStats := c.ensureSignalContext(c.contextKey(screen, ""))
		contextStats.UIWindows++
		contextStats.UIFrames += event.UIWindow.FrameCount
		contextStats.UIJank += event.UIWindow.JankCount
		problem := event.Flags&uint64(jhlog.FlagUIProblem) != 0
		if problem {
			c.addProblemWindow(
				context,
				"ui_jank",
				event.UIWindow.WindowMS,
				maxUint64(event.UIWindow.JankCount, 1),
				maxUint64(event.UIWindow.P95MS, event.UIWindow.P99MS),
			)
		}
	case event.Stall != nil:
		owner := c.currentAttrOwner
		stack := jhlog.ResolveSymbol(dict, event.Stall.StackRef)
		if c.isHeapDumpStall(event.TimeMS, owner) {
			owner = "jankhunter.heap_dump"
		}
		context := c.eventContext("", owner)
		if !c.matchesFilters("", context, nil, owner) {
			return
		}
		c.databaseCorrelation.addStall(databaseTimelineContext{
			screen: context.Screen, operation: context.Operation, operationID: c.currentOperationID,
		}, event)
		c.markCohort()
		c.summary.StallCount++
		if event.Stall.DurationMS > c.summary.StallMaxMS {
			c.summary.StallMaxMS = event.Stall.DurationMS
		}
		addOwner(c.ownerStats, owner, "main_thread_stall", event.Stall.DurationMS, stack)
		contextStats := c.ensureSignalContext(signalContextKeyFromStats(context))
		contextStats.StallCount++
		if event.Stall.DurationMS > contextStats.StallMaxMS {
			contextStats.StallMaxMS = event.Stall.DurationMS
		}
		c.addProblemWindow(context, "main_thread_stall", event.Stall.DurationMS, 1, event.Stall.DurationMS)
	case event.Context != nil:
		c.summary.ContextCount++
		c.currentNetwork = jhlog.NetworkName(event.Context.Network)
		c.currentCohortDirty = true
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
		context := c.eventContext("", "")
		if !c.matchesFilters("", context, nil) {
			return
		}
		c.markCohort()
		point := workerPoint{
			logIndex: c.currentLogIndex, timeMS: event.TimeMS, value: event.Memory.PSSKB, count: 1,
		}
		if c.workerCorrelationOn {
			c.workerMemory = append(c.workerMemory, point)
		} else {
			c.workerPriorMemory = point
			c.workerPriorMemorySet = true
		}
		c.summary.MemoryCount++
		if event.Memory.PSSKB > c.summary.MemoryMaxKB {
			c.summary.MemoryMaxKB = event.Memory.PSSKB
		}
		contextStats := c.ensureSignalContext(c.contextKey("", ""))
		if event.Memory.PSSKB > contextStats.MemoryMaxKB {
			contextStats.MemoryMaxKB = event.Memory.PSSKB
		}
	case event.ProcessExit != nil:
		process := firstKnown(jhlog.ResolveSymbol(dict, event.ProcessExit.ProcessRef), c.currentProcess)
		key := processExitKey{reason: event.ProcessExit.Reason, process: process}
		stats := c.processExitStats[key]
		if stats == nil {
			label, _ := processExitReason(event.ProcessExit.Reason)
			stats = &ProcessExitStats{Reason: event.ProcessExit.Reason, ReasonLabel: label, Process: attrValue(process)}
			c.processExitStats[key] = stats
		}
		stats.Count++
		if event.ProcessExit.TimestampUnixMS >= stats.LatestTimestampUnixMS {
			stats.LatestTimestampUnixMS = event.ProcessExit.TimestampUnixMS
			stats.Importance = event.ProcessExit.Importance
		}
		stats.MaxPSSKB = maxUint64(stats.MaxPSSKB, event.ProcessExit.PSSKB)
		stats.MaxRSSKB = maxUint64(stats.MaxRSSKB, event.ProcessExit.RSSKB)
	case event.IO != nil:
		context := c.eventContext("", "")
		source := attrValue(c.resolveOwnerRef(dict, event.IO.SourceRef))
		if !c.matchesFilters("", context, nil, source, context.Owner) {
			return
		}
		c.databaseCorrelation.addIO(databaseTimelineContext{
			screen: context.Screen, operation: context.Operation, operationID: c.currentOperationID,
		}, event)
		c.markCohort()
		if c.workerCorrelationOn {
			c.recordWorkerIO(event)
		}
		operation := ioOperationName(event.IO.Operation)
		mainThread := event.Flags&uint64(jhlog.FlagThreadMain) != 0
		key := ioStatsKey{
			context: signalContextKeyFromStats(context), operation: operation,
			source: source, mainThread: mainThread,
		}
		stats := c.ioStats[key]
		if stats == nil {
			stats = &ioAggregate{stats: IOStats{
				Operation: operation, Source: source, MainThread: mainThread,
				Screen: context.Screen, ContextOperation: context.Operation, Owner: context.Owner,
			}}
			c.ioStats[key] = stats
		}
		stats.add(event.IO, event.Flags, c.currentLogIndex, event.TimeMS)
		c.ioAnalysis.add(
			event.IO,
			event.Flags,
			c.currentLogIndex,
			ioEventEndUS(event),
		)
	case event.Retained != nil:
		className := c.deobfuscate(jhlog.ResolveSymbol(dict, event.Retained.ClassRef))
		holder := c.resolveOwnerRef(dict, event.Retained.HolderRef)
		context := c.eventContext("", "")
		owner := context.Owner
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
		c.addMemoryLeakSuspect(
			className,
			holder,
			context,
			event.Retained.AgeMS,
			event.Retained.Count,
			event.Retained.Evidence,
			true,
		)
		addOwner(c.ownerStats, className, "retained_object", event.Retained.AgeMS, "")
		c.addProblemWindow(
			context,
			"retained_object",
			event.Retained.AgeMS,
			maxUint64(event.Retained.Count, 1),
			event.Retained.AgeMS,
		)
	case event.LogSpam != nil:
		key := c.contextKey("", "")
		context := key.stats()
		source := jhlog.ResolveSymbol(dict, event.LogSpam.SourceRef)
		if !c.matchesFilters("", context, []string{source}, context.Owner) {
			return
		}
		c.markCohort()
		level := logLevelName(event.LogSpam.Level)
		logKey := logSpamKey{context: key, source: source, level: level}
		stats := c.logSpamStats[logKey]
		if stats == nil {
			stats = &LogSpamStats{
				Screen: context.Screen, Operation: context.Operation, Owner: context.Owner,
				Source: source, Level: level,
			}
			c.logSpamStats[logKey] = stats
		}
		stats.Count += event.LogSpam.Count
		contextStats := c.ensureSignalContext(key)
		contextStats.LogSpam += event.LogSpam.Count
		if event.LogSpam.Count >= canonicalLogSpamCount {
			c.addProblemWindow(
				context,
				"log_spam",
				canonicalLogSpamWindowMS,
				event.LogSpam.Count,
				event.LogSpam.Count,
			)
		}
	case event.Problem != nil:
		key := c.contextKey("", "")
		context := key.stats()
		if !c.matchesFilters("", context, nil, context.Owner) {
			return
		}
		c.markCohort()
		kind := jhlog.ResolveSymbol(dict, event.Problem.KindRef)
		c.addProblemWindow(context, kind, event.Problem.WindowMS, event.Problem.Count, event.Problem.MaxMS)
	case event.Worker != nil:
		c.recordWorker(dict, event)
	case event.RuntimeCall != nil:
		caller := c.currentAttrOwner
		callee := c.resolveOwnerRef(dict, event.RuntimeCall.CalleeRef)
		key := c.contextKey("", "")
		context := key.stats()
		if !c.matchesFilters("", context, []string{caller, callee}, caller, callee) {
			return
		}
		c.markCohort()
		callKey := runtimeCallKey{context: key, caller: caller, callee: callee}
		stats := c.runtimeCallStats[callKey]
		if stats == nil {
			stats = &RuntimeCallStats{
				Screen: context.Screen, Operation: context.Operation,
				Caller: caller, Callee: callee,
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
		name := jhlog.ResolveSymbol(dict, event.Metric.MetricRef)
		if event.Type == jhlog.EventCounter && event.Metric.MetricRef.Stable {
			name = c.resolveOwnerRef(dict, event.Metric.MetricRef)
		}
		context := c.eventContext("", "")
		c.databaseCorrelation.addGC(databaseTimelineContext{
			screen: context.Screen, operation: context.Operation, operationID: c.currentOperationID,
		}, name, event)
		if event.Type == jhlog.EventCounter && name == "jankhunter.heap_dump.created.count" && event.Metric.Value > 0 {
			c.lastHeapDumpMS = event.TimeMS
		}
		if event.Type == jhlog.EventGauge {
			mode := event.Metric.Mode
			if mode == jhlog.MetricModeUnknown {
				mode = metricModeForGauge(name)
			}
			c.gauge(name).add(event.Metric.Value, event.Metric.Count, event.Metric.Sum, event.Metric.Max, mode)
		} else {
			c.counterValues[name] += event.Metric.Value
		}
		if c.workerCorrelationOn {
			c.recordWorkerMetric(name, event)
		}
		c.runtimeAnalysis.addMetric(name, event, c.currentLogIndex)
	}
}

const databaseStatementGroupLimit = 4_096

func (c *collector) isHeapDumpStall(eventTimeMS uint64, owner string) bool {
	if c.lastHeapDumpMS == 0 || eventTimeMS < c.lastHeapDumpMS || !isLikelySystemClass(owner) {
		return false
	}
	return eventTimeMS-c.lastHeapDumpMS <= heapDumpStallAttributionWindowMS
}

func (c *collector) markCohort() {
	if c.currentCohortDirty {
		c.currentCohortKey = fmt.Sprintf(
			"app=%s build=%s sdk=%s device=%s process=%s network=%s root=%s",
			c.currentAppVersion,
			c.currentBuild,
			c.currentSDK,
			c.currentDevice,
			c.currentProcess,
			c.currentNetwork,
			rootCohortValue(c.currentRootKnown, c.currentRooted),
		)
		c.currentCohortDirty = false
	}
	c.cohortSamples[c.currentCohortKey]++
}

func (c *collector) resolveOwnerRef(dict map[uint64]string, ref jhlog.SymbolRef) string {
	if !ref.Stable {
		return c.deobfuscate(jhlog.ResolveSymbol(dict, ref))
	}
	if embedded := c.stableSymbols.embedded[ref.ID]; embedded != "" {
		return c.deobfuscate(embedded)
	}
	canonical := jhlog.ResolveSymbol(dict, ref)
	c.stableSymbols.unresolved[canonical] = struct{}{}
	return canonical
}

func (c *collector) validateStableSymbols() error {
	if len(c.stableSymbols.unresolved) != 0 {
		ids := make([]string, 0, len(c.stableSymbols.unresolved))
		for id := range c.stableSymbols.unresolved {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		return fmt.Errorf(
			"log violates the self-contained stable-symbol contract: %d unresolved symbol(s), first is %s; collect a new log with the current Android SDK",
			len(ids), ids[0],
		)
	}
	return nil
}

func (c *collector) deobfuscate(value string) string {
	if c.nameMap == nil {
		return value
	}
	return c.nameMap.Deobfuscate(value)
}

func (c *collector) contextKey(screenOverride, ownerOverride string) signalContextKey {
	return signalContextKey{
		screen:    firstKnown(screenOverride, c.currentAttrScreen),
		operation: c.operationAnalysis.activeName(c.currentOperationID),
		owner:     firstKnown(ownerOverride, c.currentAttrOwner),
	}
}

func signalContextKeyFromStats(context SignalContextStats) signalContextKey {
	return signalContextKey{screen: context.Screen, operation: context.Operation, owner: context.Owner}
}

func (key signalContextKey) stats() SignalContextStats {
	return SignalContextStats{
		Screen:    attrValue(key.screen),
		Operation: attrValue(key.operation),
		Owner:     attrValue(key.owner),
	}
}

func (c *collector) ensureSignalContext(key signalContextKey) *SignalContextStats {
	stats := c.signalContextStats[key]
	if stats != nil {
		return stats
	}
	context := key.stats()
	stats = &SignalContextStats{
		Screen: context.Screen, Operation: context.Operation, Owner: context.Owner,
	}
	c.signalContextStats[key] = stats
	return stats
}

func (c *collector) addProblemWindow(context SignalContextStats, kind string, windowMS, count, maxMS uint64) {
	key := signalContextKeyFromStats(context)
	problemKey := problemWindowKey{context: key, kind: kind}
	stats := c.problemStats[problemKey]
	if stats == nil {
		stats = &ProblemWindowStats{
			Screen: context.Screen, Operation: context.Operation, Owner: context.Owner, Kind: kind,
		}
		c.problemStats[problemKey] = stats
	}
	stats.Windows++
	stats.Count += count
	stats.TotalWindowMS += windowMS
	stats.MaxMS = maxUint64(stats.MaxMS, maxMS)
	contextStats := c.ensureSignalContext(key)
	contextStats.ProblemCount += count
	contextStats.ProblemMaxMS = maxUint64(contextStats.ProblemMaxMS, maxMS)
}

func (c *collector) sampleSet(target map[signalContextKey]*uint64SampleSet, key signalContextKey) *uint64SampleSet {
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

func (c *collector) addMemoryLeakSuspect(
	className,
	holder string,
	context SignalContextStats,
	ageMs,
	count uint64,
	evidence jhlog.RetentionEvidence,
	runtimeSignal bool,
) {
	className = attrValue(className)
	holder = firstKnown(holder, context.Owner, className)
	key := memoryLeakKey{
		className: className,
		holder:    holder,
		screen:    context.Screen,
		operation: context.Operation,
	}
	stats := c.memoryLeakStats[key]
	if stats == nil {
		stats = &memoryLeakStats{
			className: className,
			holder:    holder,
			screen:    context.Screen,
			operation: context.Operation,
		}
		c.memoryLeakStats[key] = stats
	}
	stats.count += count
	if runtimeSignal {
		switch evidence.Effective() {
		case jhlog.RetentionEvidenceAfterExplicitGC:
			stats.afterExplicitGCCount += count
		default:
			stats.timeOnlyCount += count
		}
	}
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
		if !c.matchesFilters("", SignalContextStats{}, []string{className}, holder) {
			continue
		}
		c.addMemoryLeakSuspect(
			className,
			holder,
			SignalContextStats{},
			0,
			count,
			jhlog.RetentionEvidenceUnknown,
			false,
		)
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
