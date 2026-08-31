package analyze

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

const (
	workerMetricDeviceCPU      = "process.cpu.device_percent_x100"
	workerMetricCoreCPU        = "process.cpu.core_percent_x100"
	workerMetricAllocationRate = "memory.allocation_rate_bytes_per_sec"
	workerMetricGCCount        = "gc.count.delta"
	workerMetricGCTime         = "gc.time_ms.delta"
	workerMetricGCBlocking     = "gc.blocking_count.delta"
	workerMetricGCBlockingTime = "gc.blocking_time_ms.delta"
	workerMetricGCAllocated    = "gc.bytes_allocated.delta"
	workerMetricGCFreed        = "gc.bytes_freed.delta"
)

type workerRawEvent struct {
	logIndex   uint64
	sequence   uint64
	timeMS     uint64
	instanceID uint64
	durationMS uint64
	flags      uint64
	worker     string
	screen     string
	operation  string
	owner      string
	scope      string
	stage      jhlog.WorkerStage
	outcome    jhlog.WorkerOutcome
	runAttempt uint32
	generation uint32
	stopReason uint32
}

type workerCostInterval struct {
	logIndex uint64
	startMS  uint64
	endMS    uint64
	failures uint64
	duration uint64
	bytes    uint64
}

type workerPoint struct {
	logIndex uint64
	timeMS   uint64
	value    uint64
	count    uint64
	max      uint64
}

type workerMetricPoint struct {
	workerPoint
	name string
}

// workerCollectorState keeps the optional cross-event histories together. Its slices and identity
// map remain nil until Worker evidence enables correlation, so non-Worker logs pay only scalars.
type workerCollectorState struct {
	workerEvents         []workerRawEvent
	workerHTTP           []workerCostInterval
	workerIO             []workerCostInterval
	workerMemory         []workerPoint
	workerMetrics        []workerMetricPoint
	workerLogIdentities  map[uint64]string
	workerEventSequence  uint64
	workerSeenInLog      bool
	workerCorrelationOn  bool
	workerPriorMemory    workerPoint
	workerPriorMemorySet bool
}

func (s *workerCollectorState) startLog() {
	s.workerSeenInLog = false
	s.workerCorrelationOn = false
	s.workerPriorMemory = workerPoint{}
	s.workerPriorMemorySet = false
}

func (s *workerCollectorState) finishLog(logIndex uint64, result jhlog.StreamResult) {
	if !s.workerSeenInLog {
		return
	}
	if s.workerLogIdentities == nil {
		s.workerLogIdentities = make(map[uint64]string)
	}
	s.workerLogIdentities[logIndex] = workerStreamIdentity(result)
}

func (s *workerCollectorState) release() {
	s.workerEvents = nil
	s.workerHTTP = nil
	s.workerIO = nil
	s.workerMemory = nil
	s.workerMetrics = nil
	s.workerLogIdentities = nil
	s.workerSeenInLog = false
	s.workerCorrelationOn = false
	s.workerPriorMemory = workerPoint{}
	s.workerPriorMemorySet = false
}

type workerExecution struct {
	scope   string
	worker  string
	startMS uint64
	endMS   uint64
}

type workerScopedInterval struct {
	scope   string
	startMS uint64
	endMS   uint64
}

type workerStart struct {
	event   *workerRawEvent
	matched bool
}

type workerStartMatcher struct {
	first    workerStart
	hasFirst bool
	extra    []workerStart
	byKey    map[uint64][]int
	heads    map[uint64]int
}

type workerEnqueueQueue struct {
	first *workerRawEvent
	extra []*workerRawEvent
	head  int
}

func (m *workerStartMatcher) add(event *workerRawEvent) {
	index := m.len()
	if !m.hasFirst {
		m.first = workerStart{event: event}
		m.hasFirst = true
	} else {
		m.extra = append(m.extra, workerStart{event: event})
	}
	if m.byKey == nil && m.len() == 9 {
		m.byKey = make(map[uint64][]int, 4)
		m.heads = make(map[uint64]int, 4)
		for startIndex := range m.len() {
			start := m.at(startIndex).event
			key := workerAttemptKey(start.runAttempt, start.generation)
			m.byKey[key] = append(m.byKey[key], startIndex)
		}
		return
	}
	if m.byKey != nil {
		key := workerAttemptKey(event.runAttempt, event.generation)
		m.byKey[key] = append(m.byKey[key], index)
	}
}

func (m *workerStartMatcher) match(attempt, generation uint32) *workerRawEvent {
	if m.byKey == nil {
		for index := range m.len() {
			start := m.at(index)
			if !start.matched && start.event.runAttempt == attempt && start.event.generation == generation {
				start.matched = true
				return start.event
			}
		}
		return nil
	}
	key := workerAttemptKey(attempt, generation)
	indices := m.byKey[key]
	head := m.heads[key]
	for head < len(indices) && m.at(indices[head]).matched {
		head++
	}
	if head == len(indices) {
		m.heads[key] = head
		return nil
	}
	index := indices[head]
	m.heads[key] = head + 1
	start := m.at(index)
	start.matched = true
	return start.event
}

func (m *workerStartMatcher) len() int {
	if !m.hasFirst {
		return 0
	}
	return len(m.extra) + 1
}

func (m *workerStartMatcher) at(index int) *workerStart {
	if index == 0 {
		return &m.first
	}
	return &m.extra[index-1]
}

func (m *workerStartMatcher) unmatched() int {
	count := 0
	for index := range m.len() {
		if !m.at(index).matched {
			count++
		}
	}
	return count
}

func (q *workerEnqueueQueue) add(event *workerRawEvent) {
	if q.first == nil {
		q.first = event
		return
	}
	q.extra = append(q.extra, event)
}

func (q *workerEnqueueQueue) pop() (*workerRawEvent, bool) {
	if q.first == nil || q.head > len(q.extra) {
		return nil, false
	}
	if q.head == 0 {
		q.head++
		return q.first, true
	}
	event := q.extra[q.head-1]
	q.head++
	return event, true
}

func (q *workerEnqueueQueue) remaining() int {
	total := len(q.extra)
	if q.first != nil {
		total++
	}
	return total - q.head
}

type workerAggregate struct {
	stats             WorkerStats
	wait              uint64SampleSet
	run               uint64SampleSet
	running           []workerScopedInterval
	deviceCPUSum      uint64
	coreCPUSum        uint64
	allocationRateSum uint64
	pssDeltaTotal     int64
}

type workerScopeSignals struct {
	http    workerCostIndex
	io      workerCostIndex
	memory  workerPointIndex
	metrics map[string]workerPointIndex
}

func (c *collector) recordWorker(dict map[uint64]string, event jhlog.Event) {
	c.workerSeenInLog = true
	c.enableWorkerCorrelation()
	worker := normalizeWorkerName(c.resolveOwnerRef(dict, event.Worker.WorkerRef))
	context := c.eventContext("", "")
	if !c.matchesFilters("", context, []string{worker}, worker, context.Owner) {
		return
	}
	c.databaseCorrelation.addWorker(databaseTimelineContext{
		screen: context.Screen, operation: context.Operation, operationID: c.currentOperationID,
	}, event)
	c.markCohort()
	c.workerEventSequence++
	c.workerEvents = append(c.workerEvents, workerRawEvent{
		logIndex: c.currentLogIndex, sequence: c.workerEventSequence, timeMS: event.TimeMS,
		instanceID: event.Worker.InstanceID, durationMS: event.Worker.DurationMS,
		flags: event.Flags, worker: worker, screen: context.Screen, operation: context.Operation,
		owner: context.Owner, stage: event.Worker.Stage,
		outcome: event.Worker.Outcome, runAttempt: event.Worker.RunAttempt,
		generation: event.Worker.Generation, stopReason: event.Worker.StopReason,
	})
}

func (c *collector) enableWorkerCorrelation() {
	if c.workerCorrelationOn {
		return
	}
	c.workerCorrelationOn = true
	if c.workerPriorMemorySet {
		c.workerMemory = append(c.workerMemory, c.workerPriorMemory)
		c.workerPriorMemory = workerPoint{}
		c.workerPriorMemorySet = false
	}
}

func (c *collector) recordWorkerHTTP(event jhlog.Event) {
	startMS := saturatingSub(event.TimeMS, event.HTTP.DurationMS)
	c.workerHTTP = append(c.workerHTTP, workerCostInterval{
		logIndex: c.currentLogIndex, startMS: startMS, endMS: event.TimeMS,
		failures: boolUint64(httpEventFailed(event.HTTP, event.Flags)),
		duration: event.HTTP.DurationMS,
		bytes:    saturatingUint64Sum(event.HTTP.RxBytes, event.HTTP.TxBytes),
	})
}

func (c *collector) recordWorkerIO(event jhlog.Event) {
	durationMS := event.IO.DurationUS / 1_000
	if event.IO.DurationUS%1_000 != 0 {
		durationMS++
	}
	c.workerIO = append(c.workerIO, workerCostInterval{
		logIndex: c.currentLogIndex, startMS: saturatingSub(event.TimeMS, durationMS),
		endMS: event.TimeMS, duration: event.IO.DurationUS, bytes: event.IO.Bytes,
	})
}

func (c *collector) recordWorkerMetric(name string, event jhlog.Event) {
	if !isWorkerCorrelationMetric(name) {
		return
	}
	count := event.Metric.Count
	sum := event.Metric.Sum
	maximum := event.Metric.Max
	if count == 0 {
		count = 1
	}
	if sum == 0 {
		sum = event.Metric.Value
	}
	if maximum == 0 {
		maximum = event.Metric.Value
	}
	c.workerMetrics = append(c.workerMetrics, workerMetricPoint{
		workerPoint: workerPoint{
			logIndex: c.currentLogIndex, timeMS: event.TimeMS, value: sum,
			count: count, max: maximum,
		},
		name: name,
	})
}

func isWorkerCorrelationMetric(name string) bool {
	switch name {
	case workerMetricDeviceCPU, workerMetricCoreCPU, workerMetricAllocationRate,
		workerMetricGCCount, workerMetricGCTime, workerMetricGCBlocking,
		workerMetricGCBlockingTime, workerMetricGCAllocated, workerMetricGCFreed:
		return true
	default:
		return false
	}
}

func (c *collector) finalizeWorkerAnalysis() *WorkerAnalysis {
	if len(c.workerEvents) == 0 {
		return nil
	}
	for index := range c.workerEvents {
		c.workerEvents[index].scope = c.workerScope(c.workerEvents[index].logIndex)
	}
	sort.Slice(c.workerEvents, func(i, j int) bool {
		left, right := &c.workerEvents[i], &c.workerEvents[j]
		if left.scope != right.scope {
			return left.scope < right.scope
		}
		if left.instanceID != right.instanceID {
			return left.instanceID < right.instanceID
		}
		if left.timeMS != right.timeMS {
			return left.timeMS < right.timeMS
		}
		return left.sequence < right.sequence
	})

	analysis := &WorkerAnalysis{}
	aggregates := make(map[string]*workerAggregate)
	executions := make([]workerExecution, 0, len(c.workerEvents)/2)
	queued := make([]workerScopedInterval, 0, len(c.workerEvents)/3)
	running := make([]workerScopedInterval, 0, len(c.workerEvents)/2)
	var waitSamples uint64SampleSet
	var runSamples uint64SampleSet
	stopReasons := make(map[uint32]uint64)

	for first := 0; first < len(c.workerEvents); {
		last := first + 1
		for last < len(c.workerEvents) &&
			c.workerEvents[last].scope == c.workerEvents[first].scope &&
			c.workerEvents[last].instanceID == c.workerEvents[first].instanceID {
			last++
		}
		processWorkerInstance(
			c.workerEvents[first:last], analysis, aggregates, &waitSamples, &runSamples,
			&executions, &queued, &running, stopReasons,
		)
		first = last
	}

	analysis.WaitSamples = uint64(waitSamples.seen)
	analysis.WaitP50MS = waitSamples.percentile(0.50)
	analysis.WaitP95MS = waitSamples.percentile(0.95)
	analysis.WaitMaxMS = waitSamples.max
	analysis.RunSamples = uint64(runSamples.seen)
	analysis.RunP50MS = runSamples.percentile(0.50)
	analysis.RunP95MS = runSamples.percentile(0.95)
	analysis.RunMaxMS = runSamples.max
	analysis.MaxQueued, analysis.PeakQueuedAtMS = maxWorkerConcurrency(queued)
	analysis.MaxRunning, analysis.PeakRunningAtMS = maxWorkerConcurrency(running)
	analysis.Outcomes = workerOutcomeValues(analysis)
	for reason, count := range stopReasons {
		analysis.StopReasons = append(analysis.StopReasons, NamedValue{
			Name: workerStopReasonName(reason), Value: count, Extra: fmt.Sprintf("android_code=%d", reason),
		})
	}
	sortNamed(analysis.StopReasons)

	if len(executions) > 0 {
		signals := c.buildWorkerSignalIndexes()
		for _, execution := range executions {
			aggregate := aggregates[execution.worker]
			correlateWorkerExecution(&aggregate.stats, aggregate, signals[execution.scope], execution)
		}
	}
	for _, aggregate := range aggregates {
		finalizeWorkerAggregate(aggregate)
		analysis.Workers = append(analysis.Workers, aggregate.stats)
	}
	sort.Slice(analysis.Workers, func(i, j int) bool {
		left, right := &analysis.Workers[i], &analysis.Workers[j]
		leftBad := left.Failures + left.Retries + left.Cancelled + left.MissingStart + left.MissingFinish
		rightBad := right.Failures + right.Retries + right.Cancelled + right.MissingStart + right.MissingFinish
		if leftBad != rightBad {
			return leftBad > rightBad
		}
		if left.TotalRunMS != right.TotalRunMS {
			return left.TotalRunMS > right.TotalRunMS
		}
		return left.Worker < right.Worker
	})
	return analysis
}

func processWorkerInstance(
	events []workerRawEvent,
	analysis *WorkerAnalysis,
	aggregates map[string]*workerAggregate,
	waitSamples, runSamples *uint64SampleSet,
	executions *[]workerExecution,
	queued, running *[]workerScopedInterval,
	stopReasons map[uint32]uint64,
) {
	worker := "unknown"
	periodic := false
	for index := range events {
		if events[index].worker != "unknown" && events[index].worker != "" {
			worker = events[index].worker
		}
		periodic = periodic || events[index].flags&uint64(jhlog.FlagWorkerPeriodic) != 0
	}
	aggregate := aggregates[worker]
	if aggregate == nil {
		aggregate = &workerAggregate{stats: WorkerStats{Worker: worker}}
		aggregates[worker] = aggregate
	}
	analysis.Instances++
	aggregate.stats.Instances++
	if periodic {
		analysis.PeriodicInstances++
		aggregate.stats.PeriodicInstances++
	}

	var enqueues workerEnqueueQueue
	var starts workerStartMatcher
	for index := range events {
		event := &events[index]
		adoptWorkerContext(&aggregate.stats, event)
		switch event.stage {
		case jhlog.WorkerStageEnqueued:
			analysis.Enqueued++
			aggregate.stats.Enqueued++
			enqueues.add(event)
		case jhlog.WorkerStageStarted:
			analysis.Started++
			aggregate.stats.Started++
			if event.flags&uint64(jhlog.FlagThreadMain) != 0 {
				analysis.MainThreadStarts++
				aggregate.stats.MainThreadStarts++
			}
			if enqueue, ok := enqueues.pop(); ok {
				wait := saturatingSub(event.timeMS, enqueue.timeMS)
				waitSamples.add(wait)
				aggregate.wait.add(wait)
				analysis.TotalWaitMS = saturatingUint64Sum(analysis.TotalWaitMS, wait)
				aggregate.stats.TotalWaitMS = saturatingUint64Sum(aggregate.stats.TotalWaitMS, wait)
				if event.timeMS > enqueue.timeMS {
					*queued = append(*queued, workerScopedInterval{scope: event.scope, startMS: enqueue.timeMS, endMS: event.timeMS})
				}
			} else {
				analysis.MissingEnqueue++
				aggregate.stats.MissingEnqueue++
			}
			starts.add(event)
		case jhlog.WorkerStageFinished:
			analysis.Finished++
			aggregate.stats.Finished++
			recordWorkerOutcome(analysis, &aggregate.stats, event.outcome)
			if event.flags&uint64(jhlog.FlagWorkerStopReasonKnown) != 0 {
				stopReasons[event.stopReason]++
			}
			startMS := saturatingSub(event.timeMS, event.durationMS)
			if start := starts.match(event.runAttempt, event.generation); start != nil {
				startMS = start.timeMS
			} else {
				analysis.MissingStart++
				aggregate.stats.MissingStart++
				_, _ = enqueues.pop()
			}
			recordWorkerExecution(analysis, aggregate, runSamples, executions, running, event.scope, worker, startMS, event.timeMS, event.durationMS)
		}
	}
	if missingStarts := enqueues.remaining(); missingStarts > 0 {
		analysis.MissingStart += uint64(missingStarts)
		aggregate.stats.MissingStart += uint64(missingStarts)
	}
	if missingFinishes := starts.unmatched(); missingFinishes > 0 {
		analysis.MissingFinish += uint64(missingFinishes)
		aggregate.stats.MissingFinish += uint64(missingFinishes)
		analysis.Executions += uint64(missingFinishes)
		aggregate.stats.Executions += uint64(missingFinishes)
	}
}

func recordWorkerExecution(
	analysis *WorkerAnalysis,
	aggregate *workerAggregate,
	runSamples *uint64SampleSet,
	executions *[]workerExecution,
	running *[]workerScopedInterval,
	scope, worker string,
	startMS, endMS, durationMS uint64,
) {
	analysis.Executions++
	aggregate.stats.Executions++
	runSamples.add(durationMS)
	aggregate.run.add(durationMS)
	analysis.TotalRunMS = saturatingUint64Sum(analysis.TotalRunMS, durationMS)
	aggregate.stats.TotalRunMS = saturatingUint64Sum(aggregate.stats.TotalRunMS, durationMS)
	*executions = append(*executions, workerExecution{scope: scope, worker: worker, startMS: startMS, endMS: endMS})
	if endMS > startMS {
		interval := workerScopedInterval{scope: scope, startMS: startMS, endMS: endMS}
		*running = append(*running, interval)
		aggregate.running = append(aggregate.running, interval)
	}
}

func recordWorkerOutcome(analysis *WorkerAnalysis, stats *WorkerStats, outcome jhlog.WorkerOutcome) {
	switch outcome {
	case jhlog.WorkerOutcomeSuccess:
		analysis.Success++
		stats.Success++
	case jhlog.WorkerOutcomeFailure:
		analysis.Failures++
		stats.Failures++
	case jhlog.WorkerOutcomeRetry:
		analysis.Retries++
		stats.Retries++
	case jhlog.WorkerOutcomeCancelled:
		analysis.Cancelled++
		stats.Cancelled++
	}
}

func adoptWorkerContext(stats *WorkerStats, event *workerRawEvent) {
	if stats.Screen == "" || stats.Screen == "unknown" {
		stats.Screen = event.screen
	}
	if stats.Operation == "" || stats.Operation == "unknown" {
		stats.Operation = event.operation
	}
	if stats.Owner == "" || stats.Owner == "unknown" {
		stats.Owner = event.owner
	}
}

func finalizeWorkerAggregate(aggregate *workerAggregate) {
	stats := &aggregate.stats
	stats.WaitSamples = uint64(aggregate.wait.seen)
	stats.WaitP50MS = aggregate.wait.percentile(0.50)
	stats.WaitP95MS = aggregate.wait.percentile(0.95)
	stats.WaitMaxMS = aggregate.wait.max
	stats.RunSamples = uint64(aggregate.run.seen)
	stats.RunP50MS = aggregate.run.percentile(0.50)
	stats.RunP95MS = aggregate.run.percentile(0.95)
	stats.RunMaxMS = aggregate.run.max
	stats.MaxConcurrency, _ = maxWorkerConcurrency(aggregate.running)
	if stats.CPUSamples > 0 {
		stats.AvgDeviceCPUPercentX100 = aggregate.deviceCPUSum / stats.CPUSamples
	}
	if stats.CoreCPUSamples > 0 {
		stats.AvgCoreCPUPercentX100 = aggregate.coreCPUSum / stats.CoreCPUSamples
	}
	if stats.AllocationSamples > 0 {
		stats.AvgAllocationRateBytesPerSec = aggregate.allocationRateSum / stats.AllocationSamples
	}
	if stats.MemoryPairs > 0 {
		stats.AvgPSSDeltaKB = aggregate.pssDeltaTotal / int64(stats.MemoryPairs)
	}
}

func (c *collector) buildWorkerSignalIndexes() map[string]*workerScopeSignals {
	result := make(map[string]*workerScopeSignals)
	ensure := func(scope string) *workerScopeSignals {
		value := result[scope]
		if value == nil {
			value = &workerScopeSignals{metrics: make(map[string]workerPointIndex)}
			result[scope] = value
		}
		return value
	}
	for _, interval := range c.workerHTTP {
		scope := c.workerScope(interval.logIndex)
		ensure(scope).http.add(interval)
	}
	for _, interval := range c.workerIO {
		scope := c.workerScope(interval.logIndex)
		ensure(scope).io.add(interval)
	}
	for _, point := range c.workerMemory {
		scope := c.workerScope(point.logIndex)
		index := ensure(scope)
		index.memory.points = append(index.memory.points, point)
	}
	for _, point := range c.workerMetrics {
		scope := c.workerScope(point.logIndex)
		index := ensure(scope)
		metric := index.metrics[point.name]
		metric.points = append(metric.points, point.workerPoint)
		index.metrics[point.name] = metric
	}
	for _, signals := range result {
		signals.http.build()
		signals.io.build()
		signals.memory.build()
		for name, index := range signals.metrics {
			index.build()
			signals.metrics[name] = index
		}
	}
	return result
}

func correlateWorkerExecution(stats *WorkerStats, aggregate *workerAggregate, signals *workerScopeSignals, execution workerExecution) {
	if signals == nil {
		return
	}
	http := signals.http.query(execution.startMS, execution.endMS)
	stats.CorrelatedHTTPCalls = saturatingUint64Sum(stats.CorrelatedHTTPCalls, http.count)
	stats.CorrelatedHTTPFailures = saturatingUint64Sum(stats.CorrelatedHTTPFailures, http.failures)
	stats.CorrelatedHTTPDurationMS = saturatingUint64Sum(stats.CorrelatedHTTPDurationMS, http.duration)
	io := signals.io.query(execution.startMS, execution.endMS)
	stats.CorrelatedIOOperations = saturatingUint64Sum(stats.CorrelatedIOOperations, io.count)
	stats.CorrelatedIODurationUS = saturatingUint64Sum(stats.CorrelatedIODurationUS, io.duration)
	stats.CorrelatedIOBytes = saturatingUint64Sum(stats.CorrelatedIOBytes, io.bytes)
	applyWorkerMetric(stats, aggregate, signals.metrics[workerMetricDeviceCPU], execution, workerMetricDeviceCPU)
	applyWorkerMetric(stats, aggregate, signals.metrics[workerMetricCoreCPU], execution, workerMetricCoreCPU)
	applyWorkerMetric(stats, aggregate, signals.metrics[workerMetricAllocationRate], execution, workerMetricAllocationRate)
	applyWorkerMetric(stats, aggregate, signals.metrics[workerMetricGCCount], execution, workerMetricGCCount)
	applyWorkerMetric(stats, aggregate, signals.metrics[workerMetricGCTime], execution, workerMetricGCTime)
	applyWorkerMetric(stats, aggregate, signals.metrics[workerMetricGCBlocking], execution, workerMetricGCBlocking)
	applyWorkerMetric(stats, aggregate, signals.metrics[workerMetricGCBlockingTime], execution, workerMetricGCBlockingTime)
	applyWorkerMetric(stats, aggregate, signals.metrics[workerMetricGCAllocated], execution, workerMetricGCAllocated)
	applyWorkerMetric(stats, aggregate, signals.metrics[workerMetricGCFreed], execution, workerMetricGCFreed)

	before, beforeOK := signals.memory.atOrBefore(execution.startMS)
	after, afterOK := signals.memory.atOrAfter(execution.endMS)
	if beforeOK && afterOK {
		stats.MemoryPairs++
		aggregate.pssDeltaTotal = saturatingInt64Sum(aggregate.pssDeltaTotal, boundedUint64Delta(after, before))
	}
	if memory := signals.memory.query(execution.startMS, execution.endMS); memory.count > 0 {
		stats.MaxPSSDuringKB = maxUint64(stats.MaxPSSDuringKB, memory.maximum)
	}
}

func applyWorkerMetric(stats *WorkerStats, aggregate *workerAggregate, index workerPointIndex, execution workerExecution, name string) {
	value := index.query(execution.startMS, execution.endMS)
	if value.count == 0 {
		return
	}
	switch name {
	case workerMetricDeviceCPU:
		stats.CPUSamples = saturatingUint64Sum(stats.CPUSamples, value.count)
		aggregate.deviceCPUSum = saturatingUint64Sum(aggregate.deviceCPUSum, value.sum)
		stats.MaxDeviceCPUPercentX100 = maxUint64(stats.MaxDeviceCPUPercentX100, value.maximum)
	case workerMetricCoreCPU:
		stats.CoreCPUSamples = saturatingUint64Sum(stats.CoreCPUSamples, value.count)
		aggregate.coreCPUSum = saturatingUint64Sum(aggregate.coreCPUSum, value.sum)
		stats.MaxCoreCPUPercentX100 = maxUint64(stats.MaxCoreCPUPercentX100, value.maximum)
	case workerMetricAllocationRate:
		stats.AllocationSamples = saturatingUint64Sum(stats.AllocationSamples, value.count)
		aggregate.allocationRateSum = saturatingUint64Sum(aggregate.allocationRateSum, value.sum)
		stats.MaxAllocationRateBytesPerSec = maxUint64(stats.MaxAllocationRateBytesPerSec, value.maximum)
	case workerMetricGCCount:
		stats.GCCount = saturatingUint64Sum(stats.GCCount, value.sum)
	case workerMetricGCTime:
		stats.GCTimeMS = saturatingUint64Sum(stats.GCTimeMS, value.sum)
	case workerMetricGCBlocking:
		stats.GCBlockingCount = saturatingUint64Sum(stats.GCBlockingCount, value.sum)
	case workerMetricGCBlockingTime:
		stats.GCBlockingTimeMS = saturatingUint64Sum(stats.GCBlockingTimeMS, value.sum)
	case workerMetricGCAllocated:
		stats.GCBytesAllocated = saturatingUint64Sum(stats.GCBytesAllocated, value.sum)
	case workerMetricGCFreed:
		stats.GCBytesFreed = saturatingUint64Sum(stats.GCBytesFreed, value.sum)
	}
}

type workerCostValue struct {
	count    uint64
	failures uint64
	duration uint64
	bytes    uint64
}

type workerCostSample struct {
	timeMS uint64
	workerCostValue
}

type workerCostIndex struct {
	starts []workerCostSample
	ends   []workerCostSample
}

func (i *workerCostIndex) add(interval workerCostInterval) {
	value := workerCostValue{count: 1, failures: interval.failures, duration: interval.duration, bytes: interval.bytes}
	i.starts = append(i.starts, workerCostSample{timeMS: interval.startMS, workerCostValue: value})
	i.ends = append(i.ends, workerCostSample{timeMS: interval.endMS, workerCostValue: value})
}

func (i *workerCostIndex) build() {
	workerCostPrefix(i.starts)
	workerCostPrefix(i.ends)
}

func workerCostPrefix(values []workerCostSample) {
	sort.Slice(values, func(i, j int) bool { return values[i].timeMS < values[j].timeMS })
	for index := 1; index < len(values); index++ {
		values[index].count = saturatingUint64Sum(values[index].count, values[index-1].count)
		values[index].failures = saturatingUint64Sum(values[index].failures, values[index-1].failures)
		values[index].duration = saturatingUint64Sum(values[index].duration, values[index-1].duration)
		values[index].bytes = saturatingUint64Sum(values[index].bytes, values[index-1].bytes)
	}
}

func (i workerCostIndex) query(startMS, endMS uint64) workerCostValue {
	started := sort.Search(len(i.starts), func(index int) bool { return i.starts[index].timeMS >= endMS })
	ended := sort.Search(len(i.ends), func(index int) bool { return i.ends[index].timeMS > startMS })
	return subtractWorkerCost(workerCostPrefixAt(i.starts, started), workerCostPrefixAt(i.ends, ended))
}

func workerCostPrefixAt(values []workerCostSample, length int) workerCostValue {
	if length == 0 {
		return workerCostValue{}
	}
	return values[length-1].workerCostValue
}

func subtractWorkerCost(left, right workerCostValue) workerCostValue {
	return workerCostValue{
		count: saturatingSub(left.count, right.count), failures: saturatingSub(left.failures, right.failures),
		duration: saturatingSub(left.duration, right.duration), bytes: saturatingSub(left.bytes, right.bytes),
	}
}

type workerPointValue struct {
	count   uint64
	sum     uint64
	maximum uint64
}

type workerPointIndex struct {
	points    []workerPoint
	prefixSum []uint64
	prefixN   []uint64
	maxTree   []uint64
	treeBase  int
}

func (i *workerPointIndex) build() {
	if len(i.points) == 0 {
		return
	}
	sort.Slice(i.points, func(left, right int) bool { return i.points[left].timeMS < i.points[right].timeMS })
	i.prefixSum = make([]uint64, len(i.points)+1)
	i.prefixN = make([]uint64, len(i.points)+1)
	for index, point := range i.points {
		i.prefixSum[index+1] = saturatingUint64Sum(i.prefixSum[index], point.value)
		i.prefixN[index+1] = saturatingUint64Sum(i.prefixN[index], point.count)
	}
	i.treeBase = 1
	for i.treeBase < len(i.points) {
		i.treeBase *= 2
	}
	i.maxTree = make([]uint64, i.treeBase*2)
	for index, point := range i.points {
		i.maxTree[i.treeBase+index] = maxUint64(point.max, point.value/maxUint64(point.count, 1))
	}
	for index := i.treeBase - 1; index > 0; index-- {
		i.maxTree[index] = maxUint64(i.maxTree[index*2], i.maxTree[index*2+1])
	}
}

func (i workerPointIndex) query(startMS, endMS uint64) workerPointValue {
	left := sort.Search(len(i.points), func(index int) bool { return i.points[index].timeMS >= startMS })
	right := sort.Search(len(i.points), func(index int) bool { return i.points[index].timeMS > endMS })
	if left >= right {
		return workerPointValue{}
	}
	return workerPointValue{
		count:   saturatingSub(i.prefixN[right], i.prefixN[left]),
		sum:     saturatingSub(i.prefixSum[right], i.prefixSum[left]),
		maximum: i.rangeMax(left, right),
	}
}

func (i workerPointIndex) rangeMax(left, right int) uint64 {
	left += i.treeBase
	right += i.treeBase
	var result uint64
	for left < right {
		if left&1 != 0 {
			result = maxUint64(result, i.maxTree[left])
			left++
		}
		if right&1 != 0 {
			right--
			result = maxUint64(result, i.maxTree[right])
		}
		left /= 2
		right /= 2
	}
	return result
}

func (i workerPointIndex) atOrBefore(timeMS uint64) (uint64, bool) {
	index := sort.Search(len(i.points), func(index int) bool { return i.points[index].timeMS > timeMS })
	if index == 0 {
		return 0, false
	}
	return i.points[index-1].value, true
}

func (i workerPointIndex) atOrAfter(timeMS uint64) (uint64, bool) {
	index := sort.Search(len(i.points), func(index int) bool { return i.points[index].timeMS >= timeMS })
	if index == len(i.points) {
		return 0, false
	}
	return i.points[index].value, true
}

func maxWorkerConcurrency(intervals []workerScopedInterval) (uint64, uint64) {
	if len(intervals) == 0 {
		return 0, 0
	}
	sort.Slice(intervals, func(i, j int) bool {
		if intervals[i].scope != intervals[j].scope {
			return intervals[i].scope < intervals[j].scope
		}
		if intervals[i].startMS != intervals[j].startMS {
			return intervals[i].startMS < intervals[j].startMS
		}
		return intervals[i].endMS < intervals[j].endMS
	})
	ends := make([]uint64, 0, min(len(intervals), 64))
	currentScope := intervals[0].scope
	var peak uint64
	var peakAtMS uint64
	for _, interval := range intervals {
		if interval.scope != currentScope {
			ends = ends[:0]
			currentScope = interval.scope
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

func (c *collector) workerScope(logIndex uint64) string {
	if identity := c.workerLogIdentities[logIndex]; identity != "" {
		return identity
	}
	identity := "log:" + strconv.FormatUint(logIndex, 10)
	if c.workerLogIdentities == nil {
		c.workerLogIdentities = make(map[uint64]string)
	}
	c.workerLogIdentities[logIndex] = identity
	return identity
}

func workerStreamIdentity(result jhlog.StreamResult) string {
	var identity [48]byte
	copy(identity[0:16], result.Header.RunID[:])
	copy(identity[16:32], result.Header.ProcessInstanceID[:])
	copy(identity[32:48], result.Header.SessionID[:])
	for _, value := range identity {
		if value != 0 {
			return string(identity[:])
		}
	}
	return "source:" + result.Source
}

func workerAttemptKey(attempt, generation uint32) uint64 {
	return uint64(attempt)<<32 | uint64(generation)
}

func normalizeWorkerName(value string) string {
	value = strings.TrimSpace(value)
	for _, suffix := range []string{".doWork", ".startWork"} {
		value = strings.TrimSuffix(value, suffix)
	}
	return attrValue(value)
}

func workerOutcomeValues(analysis *WorkerAnalysis) []NamedValue {
	values := []NamedValue{
		{Name: "success", Value: analysis.Success},
		{Name: "failure", Value: analysis.Failures},
		{Name: "retry", Value: analysis.Retries},
		{Name: "cancelled", Value: analysis.Cancelled},
	}
	result := values[:0]
	for _, value := range values {
		if value.Value > 0 {
			result = append(result, value)
		}
	}
	return result
}

func workerStopReasonName(reason uint32) string {
	if reason == 0 {
		return "unknown"
	}
	return "reason-" + strconv.FormatUint(uint64(reason), 10)
}

func boolUint64(value bool) uint64 {
	if value {
		return 1
	}
	return 0
}

func saturatingSub(left, right uint64) uint64 {
	if left >= right {
		return left - right
	}
	return 0
}

func boundedUint64Delta(left, right uint64) int64 {
	const maxInt64 = int64(^uint64(0) >> 1)
	if left >= right {
		delta := left - right
		if delta > uint64(maxInt64) {
			return maxInt64
		}
		return int64(delta)
	}
	delta := right - left
	if delta > uint64(maxInt64) {
		return -maxInt64
	}
	return -int64(delta)
}

func saturatingInt64Sum(left, right int64) int64 {
	const maxInt64 = int64(^uint64(0) >> 1)
	const minInt64 = -maxInt64 - 1
	if right > 0 && left > maxInt64-right {
		return maxInt64
	}
	if right < 0 && left < minInt64-right {
		return minInt64
	}
	return left + right
}
