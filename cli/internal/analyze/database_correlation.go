package analyze

import (
	"sort"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

const (
	databaseIntervalLimit            = 8_192
	databaseTransactionIntervalLimit = 2_048
	databaseUIWindowLimit            = 4_096
	databaseStallLimit               = 4_096
	databaseRelatedIntervalLimit     = 2_048
)

type databaseTimelineIdentity struct {
	process  jhlog.ID128
	session  jhlog.ID128
	fallback uint64
}

type databaseTimelineContext struct {
	screen      string
	operation   string
	operationID uint64
}

type databaseTimelineKey struct {
	identity    databaseTimelineIdentity
	screen      string
	operation   string
	operationID uint64
}

type databaseTimedInterval struct {
	contextKey     databaseContextKey
	transactionKey databaseTransactionKey
	timeline       databaseTimelineKey
	startUS        uint64
	endUS          uint64
	main           bool
}

type databaseCorrelationWindow struct {
	timeline databaseTimelineKey
	startUS  uint64
	endUS    uint64
	frames   uint64
	jank     uint64
	weight   uint64
}

type databaseTimelineRank struct {
	class     uint8
	primary   uint64
	secondary uint64
	hash      uint64
}

// databaseTimelineRanking keeps heap metadata separate from typed payloads. This avoids the heap
// boxing that Go's generic implementation would otherwise perform for every retained value.
type databaseTimelineRanking struct {
	capacity int
	ranks    []databaseTimelineRank
	heap     []int
	dropped  uint64
	evicted  uint64
}

func (s *databaseTimelineRanking) drop() {
	s.dropped = saturatingUint64Sum(s.dropped, 1)
}

func (s *databaseTimelineRanking) retain(rank databaseTimelineRank) (int, bool, bool) {
	if s.capacity <= 0 {
		s.dropped = saturatingUint64Sum(s.dropped, 1)
		return 0, false, false
	}
	if s.ranks == nil {
		s.ranks = make([]databaseTimelineRank, 0, s.capacity)
		s.heap = make([]int, 0, s.capacity)
	}
	if len(s.ranks) < s.capacity {
		slot := len(s.ranks)
		s.ranks = append(s.ranks, rank)
		s.heap = append(s.heap, slot)
		s.up(len(s.heap) - 1)
		return slot, true, true
	}
	if !databaseTimelineRankLess(s.ranks[s.heap[0]], rank) {
		s.dropped = saturatingUint64Sum(s.dropped, 1)
		return 0, false, false
	}
	slot := s.heap[0]
	s.ranks[slot] = rank
	s.evicted = saturatingUint64Sum(s.evicted, 1)
	s.dropped = saturatingUint64Sum(s.dropped, 1)
	s.down(0)
	return slot, false, true
}

func (s *databaseTimelineRanking) up(index int) {
	for index > 0 {
		parent := (index - 1) / 2
		if !s.less(index, parent) {
			return
		}
		s.heap[index], s.heap[parent] = s.heap[parent], s.heap[index]
		index = parent
	}
}

func (s *databaseTimelineRanking) down(index int) {
	for {
		left := index*2 + 1
		if left >= len(s.heap) {
			return
		}
		smallest := left
		right := left + 1
		if right < len(s.heap) && s.less(right, left) {
			smallest = right
		}
		if !s.less(smallest, index) {
			return
		}
		s.heap[index], s.heap[smallest] = s.heap[smallest], s.heap[index]
		index = smallest
	}
}

func (s *databaseTimelineRanking) less(left, right int) bool {
	return databaseTimelineRankLess(s.ranks[s.heap[left]], s.ranks[s.heap[right]])
}

type databaseIntervalTimeline struct {
	databaseTimelineRanking
	values []databaseTimedInterval
}

func newDatabaseIntervalTimeline(capacity int) databaseIntervalTimeline {
	return databaseIntervalTimeline{databaseTimelineRanking: databaseTimelineRanking{capacity: capacity}}
}

func (s *databaseIntervalTimeline) add(value databaseTimedInterval, rank databaseTimelineRank) {
	slot, appendValue, retained := s.retain(rank)
	if !retained {
		return
	}
	if appendValue {
		if s.values == nil {
			s.values = make([]databaseTimedInterval, 0, s.capacity)
		}
		s.values = append(s.values, value)
		return
	}
	s.values[slot] = value
}

type databaseWindowTimeline struct {
	databaseTimelineRanking
	values []databaseCorrelationWindow
}

func newDatabaseWindowTimeline(capacity int) databaseWindowTimeline {
	return databaseWindowTimeline{databaseTimelineRanking: databaseTimelineRanking{capacity: capacity}}
}

func (s *databaseWindowTimeline) add(value databaseCorrelationWindow, rank databaseTimelineRank) {
	slot, appendValue, retained := s.retain(rank)
	if !retained {
		return
	}
	if appendValue {
		if s.values == nil {
			s.values = make([]databaseCorrelationWindow, 0, s.capacity)
		}
		s.values = append(s.values, value)
		return
	}
	s.values[slot] = value
}

func databaseTimelineRankLess(left, right databaseTimelineRank) bool {
	if left.class != right.class {
		return left.class < right.class
	}
	if left.primary != right.primary {
		return left.primary < right.primary
	}
	if left.secondary != right.secondary {
		return left.secondary < right.secondary
	}
	return left.hash < right.hash
}

type databaseCorrelationAccumulator struct {
	identity     databaseTimelineIdentity
	database     databaseIntervalTimeline
	transactions databaseIntervalTimeline
	ui           databaseWindowTimeline
	stalls       databaseWindowTimeline
	http         databaseWindowTimeline
	workers      databaseWindowTimeline
	fileIO       databaseWindowTimeline
	gc           databaseWindowTimeline
}

func (a *databaseCorrelationAccumulator) ensureInitialized() {
	if a.database.capacity == 0 {
		a.database = newDatabaseIntervalTimeline(databaseIntervalLimit)
		a.transactions = newDatabaseIntervalTimeline(databaseTransactionIntervalLimit)
		a.ui = newDatabaseWindowTimeline(databaseUIWindowLimit)
		a.stalls = newDatabaseWindowTimeline(databaseStallLimit)
		a.http = newDatabaseWindowTimeline(databaseRelatedIntervalLimit)
		a.workers = newDatabaseWindowTimeline(databaseRelatedIntervalLimit)
		a.fileIO = newDatabaseWindowTimeline(databaseRelatedIntervalLimit)
		a.gc = newDatabaseWindowTimeline(databaseRelatedIntervalLimit)
	}
}

func (a *databaseCorrelationAccumulator) startLog(header jhlog.SegmentHeader, logIndex uint64) {
	a.ensureInitialized()
	a.identity = databaseTimelineIdentity{process: header.ProcessInstanceID, session: header.SessionID}
	if header.ProcessInstanceID.IsZero() || header.SessionID.IsZero() {
		a.identity.fallback = logIndex
	}
}

func (a *databaseCorrelationAccumulator) timelineKey(context databaseTimelineContext) (databaseTimelineKey, bool) {
	screen := attrValue(context.screen)
	if isUnknownAnalysisValue(screen) {
		return databaseTimelineKey{}, false
	}
	key := databaseTimelineKey{
		identity: a.identity, screen: screen, operationID: context.operationID,
	}
	if key.operationID == 0 {
		operation := attrValue(context.operation)
		if !isUnknownAnalysisValue(operation) {
			key.operation = operation
		}
	}
	return key, true
}

func (a *databaseCorrelationAccumulator) addDatabase(
	contextKey databaseContextKey,
	context databaseTimelineContext,
	event jhlog.Event,
	estimatedCalls uint64,
) {
	if event.Database == nil || event.Database.DurationUS == 0 {
		return
	}
	timeline, ok := a.timelineKey(context)
	if !ok {
		a.database.drop()
		return
	}
	endUS := databaseEventTimeUS(event)
	startUS := saturatingUint64Sub(endUS, event.Database.DurationUS)
	if startUS >= endUS {
		return
	}
	retentionClass := databaseEventRetentionClass(event.Database, event.Flags)
	hash := databaseTimelineValueHash(timeline, startUS, endUS)
	frequency := uint64(1)
	frequency = maxUint64(frequency, estimatedCalls)
	primary, secondary := event.Database.DurationUS, frequency
	if retentionClass == 0 {
		primary, secondary = frequency, event.Database.DurationUS
	}
	a.database.add(databaseTimedInterval{
		contextKey: contextKey, timeline: timeline, startUS: startUS, endUS: endUS,
		main: event.Flags&uint64(jhlog.FlagThreadMain) != 0,
	}, databaseTimelineRank{
		class: retentionClass, primary: primary, secondary: secondary, hash: hash,
	})
}

func (a *databaseCorrelationAccumulator) addTransaction(
	key databaseTransactionKey,
	context databaseTimelineContext,
	event jhlog.Event,
) {
	transaction := event.DatabaseTransaction
	if transaction == nil || transaction.Stage != jhlog.DatabaseTransactionTerminal ||
		transaction.DurationUS == 0 {
		return
	}
	timeline, ok := a.timelineKey(context)
	if !ok {
		a.transactions.drop()
		return
	}
	endUS := databaseEventTimeUS(event)
	startUS := saturatingUint64Sub(endUS, transaction.DurationUS)
	if startUS >= endUS {
		return
	}
	hash := databaseHashUint64(
		databaseTimelineValueHash(timeline, startUS, endUS),
		databaseTransactionKeyHashValue(key),
	)
	retentionClass := uint8(1)
	if event.Flags&uint64(jhlog.FlagThreadMain) != 0 ||
		transaction.Outcome != jhlog.DatabaseTransactionSuccess {
		retentionClass = 2
	}
	a.transactions.add(databaseTimedInterval{
		transactionKey: key, timeline: timeline, startUS: startUS, endUS: endUS,
		main: event.Flags&uint64(jhlog.FlagThreadMain) != 0,
	}, databaseTimelineRank{
		class: retentionClass, primary: transaction.DurationUS,
		secondary: transaction.StatementCount, hash: hash,
	})
}

func (a *databaseCorrelationAccumulator) addUIWindow(context databaseTimelineContext, event jhlog.Event) {
	if event.UIWindow == nil || event.UIWindow.WindowMS == 0 || !databaseProblemUIWindow(event) {
		return
	}
	timeline, ok := a.timelineKey(context)
	if !ok {
		a.ui.drop()
		return
	}
	endUS := databaseEventTimeUS(event)
	startUS := saturatingUint64Sub(endUS, saturatingUint64Product(event.UIWindow.WindowMS, 1_000))
	if startUS >= endUS {
		return
	}
	hash := databaseTimelineValueHash(timeline, startUS, endUS)
	a.ui.add(databaseCorrelationWindow{
		timeline: timeline, startUS: startUS, endUS: endUS,
		frames: event.UIWindow.FrameCount, jank: event.UIWindow.JankCount,
	}, databaseTimelineRank{
		class: 1, primary: event.UIWindow.JankCount,
		secondary: maxUint64(event.UIWindow.P95MS, event.UIWindow.P99MS), hash: hash,
	})
}

func (a *databaseCorrelationAccumulator) addStall(context databaseTimelineContext, event jhlog.Event) {
	if event.Stall == nil || event.Stall.DurationMS == 0 {
		return
	}
	timeline, ok := a.timelineKey(context)
	if !ok {
		a.stalls.drop()
		return
	}
	endUS := databaseEventTimeUS(event)
	durationUS := saturatingUint64Product(event.Stall.DurationMS, 1_000)
	startUS := saturatingUint64Sub(endUS, durationUS)
	if startUS >= endUS {
		return
	}
	hash := databaseTimelineValueHash(timeline, startUS, endUS)
	a.stalls.add(databaseCorrelationWindow{
		timeline: timeline, startUS: startUS, endUS: endUS,
	}, databaseTimelineRank{class: 1, primary: durationUS, hash: hash})
}

func (a *databaseCorrelationAccumulator) addHTTP(context databaseTimelineContext, event jhlog.Event) {
	if event.HTTP == nil || event.HTTP.DurationMS == 0 {
		return
	}
	durationUS := saturatingUint64Product(event.HTTP.DurationMS, 1_000)
	a.addRelatedInterval(&a.http, context, event, durationUS, 1, durationUS)
}

func (a *databaseCorrelationAccumulator) addIO(context databaseTimelineContext, event jhlog.Event) {
	if event.IO == nil || event.IO.DurationUS == 0 {
		return
	}
	a.addRelatedInterval(&a.fileIO, context, event, event.IO.DurationUS, 1, event.IO.DurationUS)
}

func (a *databaseCorrelationAccumulator) addWorker(context databaseTimelineContext, event jhlog.Event) {
	if event.Worker == nil || event.Worker.Stage != jhlog.WorkerStageFinished ||
		event.Worker.DurationMS == 0 {
		return
	}
	durationUS := saturatingUint64Product(event.Worker.DurationMS, 1_000)
	a.addRelatedInterval(&a.workers, context, event, durationUS, 1, durationUS)
}

func (a *databaseCorrelationAccumulator) addGC(
	context databaseTimelineContext,
	name string,
	event jhlog.Event,
) {
	if name != workerMetricGCCount || event.Metric == nil {
		return
	}
	count := observationOf(event).sum
	if count == 0 {
		return
	}
	timeline, ok := a.timelineKey(context)
	if !ok {
		a.gc.drop()
		return
	}
	startUS := databaseEventTimeUS(event)
	endUS := saturatingUint64Sum(startUS, 1)
	if startUS >= endUS {
		return
	}
	hash := databaseTimelineValueHash(timeline, startUS, endUS)
	a.gc.add(databaseCorrelationWindow{
		timeline: timeline, startUS: startUS, endUS: endUS, weight: count,
	}, databaseTimelineRank{class: 1, primary: count, hash: hash})
}

func (a *databaseCorrelationAccumulator) addRelatedInterval(
	store *databaseWindowTimeline,
	context databaseTimelineContext,
	event jhlog.Event,
	durationUS, weight, priority uint64,
) {
	timeline, ok := a.timelineKey(context)
	if !ok {
		store.drop()
		return
	}
	endUS := databaseEventTimeUS(event)
	startUS := saturatingUint64Sub(endUS, durationUS)
	if startUS >= endUS {
		return
	}
	hash := databaseTimelineValueHash(timeline, startUS, endUS)
	store.add(databaseCorrelationWindow{
		timeline: timeline, startUS: startUS, endUS: endUS, weight: weight,
	}, databaseTimelineRank{class: 1, primary: priority, hash: hash})
}

func databaseEventTimeUS(event jhlog.Event) uint64 {
	if event.TimeUS != 0 {
		return event.TimeUS
	}
	return saturatingUint64Product(event.TimeMS, 1_000)
}

func databaseProblemUIWindow(event jhlog.Event) bool {
	if event.Flags&uint64(jhlog.FlagUIProblem) != 0 || event.UIWindow.JankCount > 0 {
		return true
	}
	thresholdMS := uint64(32)
	if event.UIWindow.FrameDeadlineUS > 0 {
		thresholdMS = maxUint64(1, (event.UIWindow.FrameDeadlineUS*2+999)/1_000)
	}
	return event.UIWindow.P95MS >= thresholdMS || event.UIWindow.P99MS >= thresholdMS*2
}

func saturatingUint64Product(left, right uint64) uint64 {
	if left == 0 || right == 0 {
		return 0
	}
	maximum := ^uint64(0)
	if left > maximum/right {
		return maximum
	}
	return left * right
}

func databaseTimelineValueHash(key databaseTimelineKey, startUS, endUS uint64) uint64 {
	hash := databaseHashOffset
	for _, value := range key.identity.process {
		hash = databaseHashByte(hash, value)
	}
	for _, value := range key.identity.session {
		hash = databaseHashByte(hash, value)
	}
	hash = databaseHashUint64(hash, key.identity.fallback)
	hash = databaseHashString(hash, key.screen)
	hash = databaseHashString(hash, key.operation)
	hash = databaseHashUint64(hash, key.operationID)
	hash = databaseHashUint64(hash, startUS)
	return databaseHashUint64(hash, endUS)
}

type databaseThreadCorrelation struct {
	Main       DatabaseCorrelationStats
	Background DatabaseCorrelationStats
}

type databaseCorrelationResult struct {
	total        databaseThreadCorrelation
	contexts     map[databaseContextKey]databaseThreadCorrelation
	transactions map[databaseTransactionKey]DatabaseCorrelationStats
}

type databaseIntervalPrefix struct {
	time     uint64
	overlaps uint64
	frames   uint64
	jank     uint64
}

type databaseWindowIndex struct {
	byStart []databaseIntervalPrefix
	byEnd   []databaseIntervalPrefix
}

func (a *databaseCorrelationAccumulator) finalize() databaseCorrelationResult {
	result := databaseCorrelationResult{}
	if len(a.database.values) == 0 && len(a.transactions.values) == 0 {
		return result
	}
	if len(a.ui.values) == 0 && len(a.stalls.values) == 0 && len(a.http.values) == 0 &&
		len(a.workers.values) == 0 && len(a.fileIO.values) == 0 && len(a.gc.values) == 0 {
		return result
	}
	windowCount := len(a.ui.values) + len(a.stalls.values) + len(a.http.values) +
		len(a.workers.values) + len(a.fileIO.values) + len(a.gc.values)
	contextHint := min(len(a.database.values), windowCount)
	contextHint = min(contextHint, 256)
	if len(a.database.values) > 0 {
		result.contexts = make(map[databaseContextKey]databaseThreadCorrelation, contextHint)
	}
	if len(a.transactions.values) > 0 {
		result.transactions = make(
			map[databaseTransactionKey]DatabaseCorrelationStats,
			min(len(a.transactions.values), 256),
		)
	}
	ui := databaseWindowIndexes(a.ui.values)
	stalls := databaseWindowIndexes(a.stalls.values)
	http := databaseWindowIndexes(a.http.values)
	workers := databaseWindowIndexes(a.workers.values)
	fileIO := databaseWindowIndexes(a.fileIO.values)
	gc := databaseWindowIndexes(a.gc.values)
	for index := range a.database.values {
		interval := a.database.values[index]
		stats := databaseIntervalCorrelationStats(interval, ui, stalls, http, workers, fileIO, gc)
		if stats == (DatabaseCorrelationStats{}) {
			continue
		}
		contextStats := result.contexts[interval.contextKey]
		if interval.main {
			mergeDatabaseCorrelation(&contextStats.Main, stats)
			mergeDatabaseCorrelation(&result.total.Main, stats)
		} else {
			mergeDatabaseCorrelation(&contextStats.Background, stats)
			mergeDatabaseCorrelation(&result.total.Background, stats)
		}
		result.contexts[interval.contextKey] = contextStats
	}
	for index := range a.transactions.values {
		interval := a.transactions.values[index]
		stats := databaseIntervalCorrelationStats(interval, ui, stalls, http, workers, fileIO, gc)
		if stats == (DatabaseCorrelationStats{}) {
			continue
		}
		current := result.transactions[interval.transactionKey]
		mergeDatabaseCorrelation(&current, stats)
		result.transactions[interval.transactionKey] = current
	}
	return result
}

func databaseIntervalCorrelationStats(
	interval databaseTimedInterval,
	ui, stalls, http, workers, fileIO, gc map[databaseTimelineKey]*databaseWindowIndex,
) DatabaseCorrelationStats {
	stats := DatabaseCorrelationStats{}
	durationUS := interval.endUS - interval.startUS
	if windows := ui[interval.timeline]; windows != nil {
		stats.UIWindowOverlaps, stats.UIFrames, stats.UIJankyFrames = windows.overlap(
			interval.startUS, interval.endUS,
		)
		if stats.UIWindowOverlaps > 0 {
			stats.UIOverlapMaxDurationUS = durationUS
		}
	}
	if windows := stalls[interval.timeline]; windows != nil {
		stats.StallOverlaps, _, _ = windows.overlap(interval.startUS, interval.endUS)
		if stats.StallOverlaps > 0 {
			stats.StallOverlapMaxDurationUS = durationUS
		}
	}
	if windows := http[interval.timeline]; windows != nil {
		stats.HTTPOverlaps, _, _ = windows.overlap(interval.startUS, interval.endUS)
		if stats.HTTPOverlaps > 0 {
			stats.HTTPOverlapMaxDurationUS = durationUS
		}
	}
	if windows := workers[interval.timeline]; windows != nil {
		stats.WorkerOverlaps, _, _ = windows.overlap(interval.startUS, interval.endUS)
		if stats.WorkerOverlaps > 0 {
			stats.WorkerOverlapMaxDurationUS = durationUS
		}
	}
	if windows := fileIO[interval.timeline]; windows != nil {
		stats.FileIOOverlaps, _, _ = windows.overlap(interval.startUS, interval.endUS)
		if stats.FileIOOverlaps > 0 {
			stats.FileIOOverlapMaxDurationUS = durationUS
		}
	}
	if windows := gc[interval.timeline]; windows != nil {
		stats.GCOverlaps, _, _ = windows.overlap(interval.startUS, interval.endUS)
		if stats.GCOverlaps > 0 {
			stats.GCOverlapMaxDurationUS = durationUS
		}
	}
	return stats
}

func databaseWindowIndexes(
	values []databaseCorrelationWindow,
) map[databaseTimelineKey]*databaseWindowIndex {
	if len(values) == 0 {
		return nil
	}
	counts := make(map[databaseTimelineKey]int, min(len(values), 256))
	for index := range values {
		counts[values[index].timeline]++
	}
	result := make(map[databaseTimelineKey]*databaseWindowIndex, len(counts))
	for key, count := range counts {
		result[key] = &databaseWindowIndex{
			byStart: make([]databaseIntervalPrefix, 0, count),
			byEnd:   make([]databaseIntervalPrefix, 0, count),
		}
	}
	for index := range values {
		window := values[index]
		timelineIndex := result[window.timeline]
		weight := window.weight
		if weight == 0 {
			weight = 1
		}
		timelineIndex.byStart = append(timelineIndex.byStart, databaseIntervalPrefix{
			time: window.startUS, overlaps: weight, frames: window.frames, jank: window.jank,
		})
		timelineIndex.byEnd = append(timelineIndex.byEnd, databaseIntervalPrefix{
			time: window.endUS, overlaps: weight, frames: window.frames, jank: window.jank,
		})
	}
	for _, timelineIndex := range result {
		prepareDatabaseIntervalPrefix(timelineIndex.byStart)
		prepareDatabaseIntervalPrefix(timelineIndex.byEnd)
	}
	return result
}

func prepareDatabaseIntervalPrefix(values []databaseIntervalPrefix) {
	sort.Slice(values, func(i, j int) bool { return values[i].time < values[j].time })
	for index := 1; index < len(values); index++ {
		values[index].overlaps = saturatingUint64Sum(values[index-1].overlaps, values[index].overlaps)
		values[index].frames = saturatingUint64Sum(values[index-1].frames, values[index].frames)
		values[index].jank = saturatingUint64Sum(values[index-1].jank, values[index].jank)
	}
}

func (i *databaseWindowIndex) overlap(startUS, endUS uint64) (uint64, uint64, uint64) {
	started := sort.Search(len(i.byStart), func(index int) bool { return i.byStart[index].time >= endUS })
	ended := sort.Search(len(i.byEnd), func(index int) bool { return i.byEnd[index].time > startUS })
	startedOverlaps, startedFrames, startedJank := databasePrefixValues(i.byStart, started)
	endedOverlaps, endedFrames, endedJank := databasePrefixValues(i.byEnd, ended)
	return saturatingUint64Sub(startedOverlaps, endedOverlaps),
		saturatingUint64Sub(startedFrames, endedFrames),
		saturatingUint64Sub(startedJank, endedJank)
}

func databasePrefixValues(values []databaseIntervalPrefix, count int) (uint64, uint64, uint64) {
	if count == 0 {
		return 0, 0, 0
	}
	value := values[count-1]
	return value.overlaps, value.frames, value.jank
}

func mergeDatabaseCorrelation(target *DatabaseCorrelationStats, value DatabaseCorrelationStats) {
	target.UIWindowOverlaps = saturatingUint64Sum(target.UIWindowOverlaps, value.UIWindowOverlaps)
	target.UIFrames = saturatingUint64Sum(target.UIFrames, value.UIFrames)
	target.UIJankyFrames = saturatingUint64Sum(target.UIJankyFrames, value.UIJankyFrames)
	target.UIOverlapMaxDurationUS = maxUint64(target.UIOverlapMaxDurationUS, value.UIOverlapMaxDurationUS)
	target.StallOverlaps = saturatingUint64Sum(target.StallOverlaps, value.StallOverlaps)
	target.StallOverlapMaxDurationUS = maxUint64(target.StallOverlapMaxDurationUS, value.StallOverlapMaxDurationUS)
	target.HTTPOverlaps = saturatingUint64Sum(target.HTTPOverlaps, value.HTTPOverlaps)
	target.HTTPOverlapMaxDurationUS = maxUint64(target.HTTPOverlapMaxDurationUS, value.HTTPOverlapMaxDurationUS)
	target.WorkerOverlaps = saturatingUint64Sum(target.WorkerOverlaps, value.WorkerOverlaps)
	target.WorkerOverlapMaxDurationUS = maxUint64(target.WorkerOverlapMaxDurationUS, value.WorkerOverlapMaxDurationUS)
	target.FileIOOverlaps = saturatingUint64Sum(target.FileIOOverlaps, value.FileIOOverlaps)
	target.FileIOOverlapMaxDurationUS = maxUint64(target.FileIOOverlapMaxDurationUS, value.FileIOOverlapMaxDurationUS)
	target.GCOverlaps = saturatingUint64Sum(target.GCOverlaps, value.GCOverlaps)
	target.GCOverlapMaxDurationUS = maxUint64(target.GCOverlapMaxDurationUS, value.GCOverlapMaxDurationUS)
}
