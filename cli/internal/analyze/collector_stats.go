package analyze

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/datavalue"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

type retainedClassStats struct {
	count    uint64
	maxAgeMs uint64
}

type uint64SampleSet struct {
	values             []uint64
	denseCounts        []uint64
	denseOffset        uint64
	outlierCounts      map[uint64]uint64
	orderedFrequencies []uint64Frequency
	seen               int
	min                uint64
	max                uint64
	sorted             bool
	nextPromotionCheck int
}

func (s *uint64SampleSet) add(value uint64) {
	if s.seen == 0 {
		s.min = value
		s.nextPromotionCheck = uint64SampleSetPromotionThreshold
	}
	s.seen++
	if value < s.min {
		s.min = value
	}
	if value > s.max {
		s.max = value
	}
	if len(s.denseCounts) > 0 {
		if value >= s.denseOffset && value-s.denseOffset < uint64(len(s.denseCounts)) {
			s.denseCounts[value-s.denseOffset]++
		} else {
			if s.outlierCounts == nil {
				s.outlierCounts = make(map[uint64]uint64)
			}
			s.outlierCounts[value]++
		}
		s.sorted = false
		return
	}
	s.values = append(s.values, value)
	s.sorted = false
	if s.seen >= s.nextPromotionCheck {
		s.promoteDenseIfBeneficial()
		if s.nextPromotionCheck <= s.seen {
			if s.seen > int(^uint(0)>>1)/2 {
				s.nextPromotionCheck = int(^uint(0) >> 1)
			} else {
				s.nextPromotionCheck = s.seen * 2
			}
		}
	}
}

func (s *uint64SampleSet) percentile(p float64) uint64 {
	if s.seen == 0 {
		return 0
	}
	target := int(math.Ceil(float64(s.seen) * p))
	if target < 1 {
		target = 1
	}
	if target > s.seen {
		target = s.seen
	}
	if len(s.denseCounts) == 0 {
		if !s.sorted {
			sort.Slice(s.values, func(i, j int) bool { return s.values[i] < s.values[j] })
			s.sorted = true
		}
		return s.values[target-1]
	}
	s.prepareOrderedFrequencies()
	seen := uint64(0)
	for _, frequency := range s.orderedFrequencies {
		seen += frequency.count
		if seen >= uint64(target) {
			return frequency.value
		}
	}
	return s.max
}

func (s *uint64SampleSet) promoteDenseIfBeneficial() {
	if len(s.values) == 0 || s.max < s.min || s.max-s.min >= uint64SampleSetMaxDenseBins {
		return
	}
	span := int(s.max-s.min) + 1
	if span > s.seen/uint64SampleSetMinimumCompression {
		return
	}
	counts := make([]uint64, span)
	for _, value := range s.values {
		counts[value-s.min]++
	}
	s.denseCounts = counts
	s.denseOffset = s.min
	s.values = nil
	s.sorted = false
}

func (s *uint64SampleSet) prepareOrderedFrequencies() {
	if s.sorted {
		return
	}
	unique := len(s.outlierCounts)
	for _, count := range s.denseCounts {
		if count > 0 {
			unique++
		}
	}
	frequencies := make([]uint64Frequency, 0, unique)
	for index, count := range s.denseCounts {
		if count > 0 {
			frequencies = append(frequencies, uint64Frequency{
				value: s.denseOffset + uint64(index),
				count: count,
			})
		}
	}
	for value, count := range s.outlierCounts {
		frequencies = append(frequencies, uint64Frequency{value: value, count: count})
	}
	sort.Slice(frequencies, func(i, j int) bool { return frequencies[i].value < frequencies[j].value })
	s.orderedFrequencies = frequencies
	s.sorted = true
}

type uint64Frequency struct {
	value uint64
	count uint64
}

const (
	uint64SampleSetPromotionThreshold = 4_096
	uint64SampleSetMaxDenseBins       = 65_536
	uint64SampleSetMinimumCompression = 2
)

type gaugeStats struct {
	count uint64
	total uint64
	max   uint64
	last  uint64
	mode  jhlog.MetricMode
}

type routeBurstBucket struct {
	logIndex uint64
	second   uint64
	count    uint64
}

type routeBurstAccumulator struct {
	buckets           []routeBurstBucket
	peak              uint64
	peakWindowStartMS uint64
	approximate       bool
}

func (s *routeBurstAccumulator) add(logIndex, timeMS uint64) {
	second := timeMS / 1_000
	for index := range s.buckets {
		bucket := &s.buckets[index]
		if bucket.logIndex != logIndex || bucket.second != second {
			continue
		}
		bucket.count++
		s.updatePeak(*bucket)
		return
	}
	bucket := routeBurstBucket{logIndex: logIndex, second: second, count: 1}
	if len(s.buckets) < routeBurstRetainedSeconds {
		s.buckets = append(s.buckets, bucket)
	} else {
		oldest := 0
		for index := 1; index < len(s.buckets); index++ {
			if routeBurstBucketBefore(s.buckets[index], s.buckets[oldest]) {
				oldest = index
			}
		}
		s.buckets[oldest] = bucket
		s.approximate = true
	}
	s.updatePeak(bucket)
}

func (s *routeBurstAccumulator) updatePeak(bucket routeBurstBucket) {
	if bucket.count > s.peak {
		s.peak = bucket.count
		s.peakWindowStartMS = bucket.second * 1_000
	}
}

func routeBurstBucketBefore(left, right routeBurstBucket) bool {
	if left.logIndex != right.logIndex {
		return left.logIndex < right.logIndex
	}
	return left.second < right.second
}

const routeBurstRetainedSeconds = 8

var httpPhaseNames = [...]string{"queue", "dns", "connect", "tls", "request", "ttfb", "response"}

func phaseAverage(stats *httpAggregate, index int) uint64 {
	seen := stats.phases[index].seen
	if seen == 0 {
		return 0
	}
	return stats.phaseTotals[index] / uint64(seen)
}

func httpPhaseStats(stats *httpAggregate) []HTTPPhaseStats {
	result := make([]HTTPPhaseStats, len(httpPhaseNames))
	for index, name := range httpPhaseNames {
		set := &stats.phases[index]
		result[index] = HTTPPhaseStats{
			Name:        name,
			SampleCount: set.seen,
			AvgMS:       phaseAverage(stats, index),
			P50MS:       set.percentile(0.50),
			P95MS:       set.percentile(0.95),
			MaxMS:       set.max,
		}
	}
	return result
}

func networkCallStats(key networkCallKey, stats *httpAggregate) NetworkCallStats {
	return NetworkCallStats{
		Route: key.route, Service: key.service, Initiator: key.initiator,
		Screen: key.screen, Operation: key.operation, Owner: key.owner,
		Count: stats.count, Failures: stats.failures,
		TransportFailures: stats.transportFailures, HTTP4xx: stats.http4xx, HTTP5xx: stats.http5xx,
		Canceled: stats.canceled, CacheHits: stats.cacheHits, ReusedConnections: stats.reusedConnections,
		KnownRequestBytes: stats.knownRequestBytes, KnownResponseBytes: stats.knownResponseBytes,
		Attempts: stats.attempts, DNSAttempts: stats.dnsAttempts,
		ConnectAttempts: stats.connectAttempts, TLSAttempts: stats.tlsAttempts,
		Retries: stats.retries, Redirects: stats.redirects,
		ConnectFailures: stats.connectFailures, TLSFailures: stats.tlsFailures,
		P50MS: stats.durations.percentile(0.50), P95MS: stats.durations.percentile(0.95),
		MaxMS: stats.durations.max, BytesRx: stats.bytesRx, BytesTx: stats.bytesTx,
		TotalDurationMS: stats.durationTotal,
		Phases:          httpPhaseStats(stats),
	}
}

func (c *collector) finalizeNetworkAnalysis(result NetworkAnalysis) NetworkAnalysis {
	stats := &c.networkTotals
	result.MaxConcurrency, result.PeakConcurrencyAtMS = maxHTTPConcurrency(stats.intervals)
	result.TransportFailures = stats.transportFailures
	result.HTTP4xx = stats.http4xx
	result.HTTP5xx = stats.http5xx
	result.Canceled = stats.canceled
	result.CacheHits = stats.cacheHits
	result.ReusedConnections = stats.reusedConnections
	result.KnownRequestBytes = stats.knownRequestBytes
	result.KnownResponseBytes = stats.knownResponseBytes
	result.Attempts = stats.attempts
	result.DNSAttempts = stats.dnsAttempts
	result.ConnectAttempts = stats.connectAttempts
	result.TLSAttempts = stats.tlsAttempts
	result.Retries = stats.retries
	result.Redirects = stats.redirects
	result.ConnectFailures = stats.connectFailures
	result.TLSFailures = stats.tlsFailures
	result.BytesRx = stats.bytesRx
	result.BytesTx = stats.bytesTx
	result.TotalDurationMS = stats.durationTotal
	result.Phases = httpPhaseStats(stats)
	for code, count := range c.networkStatusCodes {
		result.StatusCodes = append(result.StatusCodes, NamedValue{Name: fmt.Sprint(code), Value: count})
	}
	sort.Slice(result.StatusCodes, func(i, j int) bool { return result.StatusCodes[i].Name < result.StatusCodes[j].Name })
	for phase, count := range c.networkFailurePhases {
		if count > 0 {
			result.FailurePhases = append(result.FailurePhases, NamedValue{Name: httpFailurePhaseName(jhlog.HTTPFailurePhase(phase)), Value: count})
		}
	}
	for kind, count := range c.networkFailureKinds {
		if count > 0 {
			result.FailureKinds = append(result.FailureKinds, NamedValue{Name: httpFailureKindName(jhlog.HTTPFailureKind(kind)), Value: count})
		}
	}
	for protocol, count := range c.networkProtocols {
		if count > 0 {
			result.Protocols = append(result.Protocols, NamedValue{Name: httpProtocolName(jhlog.HTTPProtocol(protocol)), Value: count})
		}
	}
	return result
}

func (c *collector) finalizeWebSocketAnalysis() *WebSocketAnalysis {
	totals := &c.webSocketTotals
	result := &WebSocketAnalysis{
		Opened: totals.opened, Closed: totals.closed, Failures: totals.failures,
		ActiveAtEnd: totals.activeAtEnd(), Reconnects: totals.reconnects,
		ConnectP50MS: totals.connect.percentile(0.50), ConnectP95MS: totals.connect.percentile(0.95),
		ConnectMaxMS: totals.connect.max, LifetimeP50MS: totals.lifetime.percentile(0.50),
		LifetimeP95MS: totals.lifetime.percentile(0.95), LifetimeMaxMS: totals.lifetime.max,
		TextMessages: totals.textMessages, BinaryMessages: totals.binaryMessages, ReceivedBytes: totals.receivedBytes,
	}
	for kind, count := range totals.failureKinds {
		if count > 0 {
			result.FailureKinds = append(result.FailureKinds, NamedValue{
				Name: webSocketFailureKindName(jhlog.WebSocketFailureKind(kind)), Value: count,
			})
		}
	}
	for code, count := range totals.closeCodes {
		result.CloseCodes = append(result.CloseCodes, NamedValue{Name: fmt.Sprint(code), Value: count})
	}
	sortNamed(result.FailureKinds)
	sortNamed(result.CloseCodes)
	for key, stats := range c.webSocketConnections {
		result.Connections = append(result.Connections, WebSocketConnectionStats{
			Route: key.route, Screen: key.screen, Operation: key.operation, Owner: key.owner,
			Opened: stats.opened, Closed: stats.closed, Failures: stats.failures,
			ActiveAtEnd: stats.activeAtEnd(), Reconnects: stats.reconnects,
			ConnectP50MS: stats.connect.percentile(0.50), ConnectP95MS: stats.connect.percentile(0.95),
			ConnectMaxMS: stats.connect.max, LifetimeP50MS: stats.lifetime.percentile(0.50),
			LifetimeP95MS: stats.lifetime.percentile(0.95), LifetimeMaxMS: stats.lifetime.max,
			TextMessages: stats.textMessages, BinaryMessages: stats.binaryMessages, ReceivedBytes: stats.receivedBytes,
		})
	}
	sort.Slice(result.Connections, func(i, j int) bool {
		left, right := result.Connections[i], result.Connections[j]
		if left.Failures != right.Failures {
			return left.Failures > right.Failures
		}
		if left.Opened != right.Opened {
			return left.Opened > right.Opened
		}
		if left.Route != right.Route {
			return left.Route < right.Route
		}
		return left.Owner < right.Owner
	})
	return result
}

func webSocketFailureKindName(kind jhlog.WebSocketFailureKind) string {
	switch kind {
	case jhlog.WebSocketFailureTimeout:
		return "timeout"
	case jhlog.WebSocketFailureConnection:
		return "connection"
	case jhlog.WebSocketFailureTLS:
		return "tls"
	case jhlog.WebSocketFailureProtocol:
		return "protocol"
	case jhlog.WebSocketFailureIO:
		return "io"
	case jhlog.WebSocketFailureOther:
		return "other"
	default:
		return "unknown"
	}
}

func (c *collector) finalizeDatabaseAnalysis() *DatabaseAnalysis {
	totals := &c.databaseTotals
	store := &c.databaseStatements
	correlations := c.databaseCorrelation.finalize()
	result := &DatabaseAnalysis{
		KnownSQLCalls: totals.knownSQL, Overall: databaseExecutionStats(&totals.overall),
		Main: databaseExecutionStats(&totals.main), Background: databaseExecutionStats(&totals.background),
		Telemetry:       databaseTelemetryStats(&c.databaseTelemetry),
		Transactions:    c.databaseTransactions.finalizeWithCorrelations(correlations.transactions),
		Scenarios:       c.databaseScenarios.finalize(),
		MainCorrelation: correlations.total.Main, BackgroundCorrelation: correlations.total.Background,
		PeakCallsPerSecond: totals.burst.peak,
		PeakWindowStartMS:  totals.burst.peakWindowStartMS, RapidRepeats: totals.rapidRepeats,
		DroppedStatementEvents:      store.droppedStatementEvents,
		DroppedContextEvents:        store.droppedContextEvents,
		EvictedStatementGroups:      store.evictedStatements,
		EvictedContextGroups:        store.evictedContexts,
		FrequencyEstimateError:      store.statementFrequency.estimatedError(),
		DroppedDBIntervals:          c.databaseCorrelation.database.dropped,
		EvictedDBIntervals:          c.databaseCorrelation.database.evicted,
		DroppedTransactionIntervals: c.databaseCorrelation.transactions.dropped,
		EvictedTransactionIntervals: c.databaseCorrelation.transactions.evicted,
		DroppedUIWindows:            c.databaseCorrelation.ui.dropped,
		EvictedUIWindows:            c.databaseCorrelation.ui.evicted,
		DroppedStallIntervals:       c.databaseCorrelation.stalls.dropped,
		EvictedStallIntervals:       c.databaseCorrelation.stalls.evicted,
		DroppedRelatedIntervals: saturatingUint64Sum(
			saturatingUint64Sum(c.databaseCorrelation.http.dropped, c.databaseCorrelation.workers.dropped),
			saturatingUint64Sum(c.databaseCorrelation.fileIO.dropped, c.databaseCorrelation.gc.dropped),
		),
		EvictedRelatedIntervals: saturatingUint64Sum(
			saturatingUint64Sum(c.databaseCorrelation.http.evicted, c.databaseCorrelation.workers.evicted),
			saturatingUint64Sum(c.databaseCorrelation.fileIO.evicted, c.databaseCorrelation.gc.evicted),
		),
	}
	for key, entry := range store.entries {
		stats := &entry.stats
		statementFingerprint := entry.statementFingerprint
		if statementFingerprint == 0 {
			statementFingerprint = databaseStatementFingerprint(key.query)
		}
		statement := DatabaseStatementStats{
			Query: key.query, Operation: key.operation,
			OperationCode:        databaseEvidenceOperationCode(key.operation),
			StatementFingerprint: statementFingerprint,
			Overall:              databaseExecutionStats(&stats.overall), Main: databaseExecutionStats(&stats.main),
			Background:         databaseExecutionStats(&stats.background),
			Telemetry:          databaseTelemetryStats(&entry.telemetry),
			PeakCallsPerSecond: stats.burst.peak, PeakWindowStartMS: stats.burst.peakWindowStartMS,
			RapidRepeats: stats.rapidRepeats, EstimatedCalls: entry.estimatedCalls,
			FrequencyEstimateError: entry.frequencyEstimateError,
		}
		statement.Contexts = make([]DatabaseStatementContextStats, 0, len(entry.contexts))
		for index := range entry.contexts {
			contextEntry := &entry.contexts[index]
			contextKey := contextEntry.key
			contextStats := &contextEntry.stats
			correlation := correlations.contexts[contextKey]
			context := DatabaseStatementContextStats{
				Source: contextKey.source, Framework: contextKey.framework, Screen: contextKey.screen,
				ContextOwner: contextKey.contextOwner, ContextOperation: contextKey.contextOperation,
				OperationID: contextKey.operationID, Process: contextKey.process,
				ProcessInstanceID: databaseIdentity(contextKey.processInstanceID),
				SessionID:         databaseIdentity(contextKey.sessionID),
				Overall:           databaseExecutionStats(&contextStats.overall), Main: databaseExecutionStats(&contextStats.main),
				Background:      databaseExecutionStats(&contextStats.background),
				MainCorrelation: correlation.Main, BackgroundCorrelation: correlation.Background,
				PeakCallsPerSecond: contextStats.burst.peak, PeakWindowStartMS: contextStats.burst.peakWindowStartMS,
				RapidRepeats: contextStats.rapidRepeats, EstimatedCalls: contextEntry.estimatedCalls,
				FrequencyEstimateError: contextEntry.frequencyEstimateError,
			}
			statement.Contexts = append(statement.Contexts, context)
			mergeDatabaseCorrelation(&statement.MainCorrelation, correlation.Main)
			mergeDatabaseCorrelation(&statement.BackgroundCorrelation, correlation.Background)
		}
		sortDatabaseContexts(statement.Contexts)
		result.Statements = append(result.Statements, statement)
	}
	sort.Slice(result.Statements, func(i, j int) bool {
		left, right := result.Statements[i], result.Statements[j]
		if left.Main.Calls != right.Main.Calls {
			return left.Main.Calls > right.Main.Calls
		}
		if left.Overall.P95DurationUS != right.Overall.P95DurationUS {
			return left.Overall.P95DurationUS > right.Overall.P95DurationUS
		}
		if left.Overall.Calls != right.Overall.Calls {
			return left.Overall.Calls > right.Overall.Calls
		}
		if left.Query != right.Query {
			return left.Query < right.Query
		}
		return left.Operation < right.Operation
	})
	c.databaseTotals = databaseAggregate{}
	c.databaseTelemetry = databaseTelemetryAggregate{}
	c.databaseStatements = databaseStatementStore{}
	c.databaseTransactions = databaseTransactionAccumulator{}
	c.databaseScenarios = databaseScenarioAccumulator{}
	c.databaseCorrelation = databaseCorrelationAccumulator{}
	applyDatabaseEvidence(result, c.databaseEvidence)
	return result
}

func sortDatabaseContexts(contexts []DatabaseStatementContextStats) {
	sort.Slice(contexts, func(i, j int) bool {
		left, right := contexts[i], contexts[j]
		if left.Main.Calls != right.Main.Calls {
			return left.Main.Calls > right.Main.Calls
		}
		if left.Overall.P95DurationUS != right.Overall.P95DurationUS {
			return left.Overall.P95DurationUS > right.Overall.P95DurationUS
		}
		if left.Overall.Calls != right.Overall.Calls {
			return left.Overall.Calls > right.Overall.Calls
		}
		if left.Source != right.Source {
			return left.Source < right.Source
		}
		if left.ContextOperation != right.ContextOperation {
			return left.ContextOperation < right.ContextOperation
		}
		if left.ProcessInstanceID != right.ProcessInstanceID {
			return left.ProcessInstanceID < right.ProcessInstanceID
		}
		if left.SessionID != right.SessionID {
			return left.SessionID < right.SessionID
		}
		return left.OperationID < right.OperationID
	})
}

func databaseIdentity(id jhlog.ID128) string {
	if id.IsZero() {
		return "unknown"
	}
	return fmt.Sprintf("%x", id[:])
}

func databaseExecutionStats(stats *databaseExecutionAggregate) DatabaseExecutionStats {
	return DatabaseExecutionStats{
		Calls: stats.calls, Failures: stats.failures,
		P50DurationUS: stats.durations.percentile(0.50),
		P95DurationUS: stats.durations.percentile(0.95),
		MaxDurationUS: stats.durations.max, TotalDurationUS: stats.totalDuration,
		QuantilesApproximated: stats.durations.approximated(),
	}
}

func canonicalDatabaseStatementKey(query, source, framework, operation string) databaseStatementKey {
	key := databaseStatementKey{query: query, operation: operation}
	if datavalue.IsUnknown(query) {
		key.fallbackSource = source
		key.fallbackFramework = framework
	}
	return key
}

func databaseFrameworkName(value jhlog.DatabaseFramework) string {
	switch value {
	case jhlog.DatabaseFrameworkSQLite:
		return "SQLite"
	case jhlog.DatabaseFrameworkSupportSQLite:
		return "SupportSQLite"
	case jhlog.DatabaseFrameworkRoom:
		return "Room"
	case jhlog.DatabaseFrameworkCustom:
		return "Custom adapter"
	default:
		return "unknown"
	}
}

func databaseOperationName(value jhlog.DatabaseOperation) string {
	switch value {
	case jhlog.DatabaseOperationQuery:
		return "чтение"
	case jhlog.DatabaseOperationInsert:
		return "вставка"
	case jhlog.DatabaseOperationUpdate:
		return "обновление"
	case jhlog.DatabaseOperationDelete:
		return "удаление"
	case jhlog.DatabaseOperationExecute:
		return "выполнение"
	case jhlog.DatabaseOperationStatement:
		return "подготовка"
	default:
		return "неизвестно"
	}
}

func maxHTTPConcurrency(intervals []httpInterval) (uint64, uint64) {
	if len(intervals) == 0 {
		return 0, 0
	}
	sort.Slice(intervals, func(i, j int) bool {
		if intervals[i].logIndex != intervals[j].logIndex {
			return intervals[i].logIndex < intervals[j].logIndex
		}
		if intervals[i].startMS != intervals[j].startMS {
			return intervals[i].startMS < intervals[j].startMS
		}
		return intervals[i].endMS < intervals[j].endMS
	})
	ends := make([]uint64, 0, min(len(intervals), 64))
	currentLog := intervals[0].logIndex
	var peak uint64
	var peakAtMS uint64
	for _, interval := range intervals {
		if interval.logIndex != currentLog {
			ends = ends[:0]
			currentLog = interval.logIndex
		}
		for len(ends) > 0 && ends[0] <= interval.startMS {
			ends = popUint64MinHeap(ends)
		}
		ends = pushUint64MinHeap(ends, interval.endMS)
		if uint64(len(ends)) > peak {
			peak = uint64(len(ends))
			peakAtMS = interval.startMS
		}
	}
	return peak, peakAtMS
}

func pushUint64MinHeap(values []uint64, value uint64) []uint64 {
	values = append(values, value)
	index := len(values) - 1
	for index > 0 {
		parent := (index - 1) / 2
		if values[parent] <= value {
			break
		}
		values[index] = values[parent]
		index = parent
	}
	values[index] = value
	return values
}

func popUint64MinHeap(values []uint64) []uint64 {
	last := values[len(values)-1]
	values = values[:len(values)-1]
	if len(values) == 0 {
		return values
	}
	index := 0
	for {
		left := index*2 + 1
		if left >= len(values) {
			break
		}
		right := left + 1
		child := left
		if right < len(values) && values[right] < values[left] {
			child = right
		}
		if values[child] >= last {
			break
		}
		values[index] = values[child]
		index = child
	}
	values[index] = last
	return values
}

func httpFailurePhaseName(phase jhlog.HTTPFailurePhase) string {
	return [...]string{"unknown", "call", "queue", "dns", "connect", "tls", "request", "response", "cancelled"}[phase]
}

func httpFailureKindName(kind jhlog.HTTPFailureKind) string {
	return [...]string{"unknown", "dns", "timeout", "connection", "tls", "protocol", "cancelled", "io", "other"}[kind]
}

func httpProtocolName(protocol jhlog.HTTPProtocol) string {
	return [...]string{"unknown", "http/1.0", "http/1.1", "http/2", "http/3"}[protocol]
}

func mergeFrameWindow(stats *ScreenStats, window *jhlog.UIWindowEvent) {
	if len(stats.FrameDurationBuckets) == 0 {
		stats.FrameDurationBuckets = make([]uint64, jhlog.UIFrameHistogramBucketCount)
		stats.FrameSource = uiFrameSourceName(window.Source)
		stats.FrameDeadlineUS = window.FrameDeadlineUS
		stats.FrameDeadlineStatus = "consistent"
	} else {
		if stats.FrameSource != uiFrameSourceName(window.Source) {
			stats.FrameSource = "mixed"
		}
		if stats.FrameDeadlineUS != window.FrameDeadlineUS {
			stats.FrameDeadlineUS = 0
			stats.FrameDeadlineStatus = "mixed"
		}
	}
	for index, count := range window.FrameDurationBuckets {
		stats.FrameDurationBuckets[index] = saturatingUint64Sum(stats.FrameDurationBuckets[index], count)
	}
	stats.FrameDistributionState = "mergeable_histogram_v2"
}

func uiFrameSourceName(source jhlog.UIFrameSource) string {
	switch source {
	case jhlog.UIFrameSourceJankStats:
		return "jankstats"
	case jhlog.UIFrameSourceChoreographer:
		return "choreographer"
	default:
		return "unknown"
	}
}

func ioOperationName(operation jhlog.IOOperationKind) string {
	switch operation {
	case jhlog.IOOperationFileRead:
		return "file_read"
	case jhlog.IOOperationFileWrite:
		return "file_write"
	case jhlog.IOOperationFileSync:
		return "file_sync"
	case jhlog.IOOperationContentRead:
		return "content_read"
	case jhlog.IOOperationContentWrite:
		return "content_write"
	default:
		return "unknown"
	}
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
		return fmt.Sprintf("последнее=%d наблюдений=%d", s.last, s.count)
	case jhlog.MetricModeState:
		return fmt.Sprintf("состояние=%d наблюдений=%d", s.last, s.count)
	case jhlog.MetricModeBooleanRate:
		return fmt.Sprintf("доля включённого состояния=%d включено=%d наблюдений=%d", s.value(), s.total, s.count)
	default:
		return fmt.Sprintf("среднее=%d максимум=%d наблюдений=%d", s.value(), s.max, s.count)
	}
}

func metricModeForGauge(name string) jhlog.MetricMode {
	metric := strings.ToLower(strings.TrimSpace(name))
	switch metric {
	case "battery.status",
		"battery.plugged",
		"battery.health",
		"device.thermal.status",
		"memory.trim.last_level":
		return jhlog.MetricModeState
	case "battery.charging",
		"device.power_save_mode",
		"device.interactive",
		"device.idle_mode",
		"network.request.connection_released":
		return jhlog.MetricModeBooleanRate
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
