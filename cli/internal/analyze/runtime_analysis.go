package analyze

import (
	"math"
	"math/bits"
	"sort"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

const (
	asyncExecutorPrefix = "executor."
	asyncOwnerPrefix    = "owner."
	startupScreenPrefix = "screen."
)

type metricAggregate struct {
	count uint64
	sum   uint64
	max   uint64
}

func (a *metricAggregate) add(observation metricObservation) {
	a.count = saturatingUint64Sum(a.count, observation.count)
	a.sum = saturatingUint64Sum(a.sum, observation.sum)
	a.max = maxUint64(a.max, observation.maximum)
}

func (a metricAggregate) average() uint64 {
	if a.count == 0 {
		return 0
	}
	return a.sum / a.count
}

func (a metricAggregate) averageScaled(scale uint64) uint64 {
	if a.count == 0 || scale == 0 {
		return 0
	}
	high, low := bits.Mul64(a.sum, scale)
	if high >= a.count {
		return math.MaxUint64
	}
	quotient, _ := bits.Div64(high, low, a.count)
	return quotient
}

type metricObservation struct {
	count   uint64
	sum     uint64
	maximum uint64
}

func observationOf(event jhlog.Event) metricObservation {
	metric := event.Metric
	if metric == nil {
		return metricObservation{}
	}
	if event.Type == jhlog.EventCounter {
		return metricObservation{count: 1, sum: metric.Value, maximum: metric.Value}
	}
	count := metric.Count
	if count == 0 {
		count = 1
	}
	sum := metric.Sum
	if metric.Count == 0 {
		sum = metric.Value
	}
	maximum := metric.Max
	if metric.Count == 0 {
		maximum = metric.Value
	}
	return metricObservation{count: count, sum: sum, maximum: maximum}
}

type asyncExecutorAggregate struct {
	started   uint64
	failures  uint64
	wait      metricAggregate
	service   metricAggregate
	queue     metricAggregate
	active    metricAggregate
	pool      metricAggregate
	completed metricAggregate
}

type asyncTaskKey struct {
	kind  string
	owner string
}

type asyncTaskAggregate struct {
	duration metricAggregate
	failures uint64
}

type asyncAnalysisAccumulator struct {
	executors map[string]*asyncExecutorAggregate
	tasks     map[asyncTaskKey]*asyncTaskAggregate
}

func (a *asyncAnalysisAccumulator) add(name string, event jhlog.Event) {
	if executor, metric, ok := parseExecutorMetric(name); ok {
		if a.executors == nil {
			a.executors = make(map[string]*asyncExecutorAggregate)
		}
		aggregate := a.executors[executor]
		if aggregate == nil {
			aggregate = &asyncExecutorAggregate{}
			a.executors[executor] = aggregate
		}
		observation := observationOf(event)
		switch metric {
		case "started.count":
			aggregate.started = saturatingUint64Sum(aggregate.started, observation.sum)
		case "failure.count":
			aggregate.failures = saturatingUint64Sum(aggregate.failures, observation.sum)
		case "wait_ms":
			aggregate.wait.add(observation)
		case "service_ms":
			aggregate.service.add(observation)
		case "queue_depth":
			aggregate.queue.add(observation)
		case "active_count":
			aggregate.active.add(observation)
		case "pool_size":
			aggregate.pool.add(observation)
		case "completed_task_count":
			aggregate.completed.add(observation)
		}
		return
	}
	owner, kind, metric, ok := parseAsyncOwnerMetric(name)
	if !ok {
		return
	}
	if a.tasks == nil {
		a.tasks = make(map[asyncTaskKey]*asyncTaskAggregate)
	}
	key := asyncTaskKey{kind: kind, owner: owner}
	aggregate := a.tasks[key]
	if aggregate == nil {
		aggregate = &asyncTaskAggregate{}
		a.tasks[key] = aggregate
	}
	observation := observationOf(event)
	if metric == "duration_ms" {
		aggregate.duration.add(observation)
	} else {
		aggregate.failures = saturatingUint64Sum(aggregate.failures, observation.sum)
	}
}

func parseExecutorMetric(name string) (string, string, bool) {
	if !strings.HasPrefix(name, asyncExecutorPrefix) {
		return "", "", false
	}
	value := strings.TrimPrefix(name, asyncExecutorPrefix)
	for _, suffix := range []string{
		"completed_task_count", "started.count", "failure.count", "queue_depth",
		"active_count", "pool_size", "service_ms", "wait_ms",
	} {
		separator := "." + suffix
		if strings.HasSuffix(value, separator) && len(value) > len(separator) {
			return strings.TrimSuffix(value, separator), suffix, true
		}
	}
	return "", "", false
}

func parseAsyncOwnerMetric(name string) (string, string, string, bool) {
	if !strings.HasPrefix(name, asyncOwnerPrefix) {
		return "", "", "", false
	}
	value := strings.TrimPrefix(name, asyncOwnerPrefix)
	for _, kind := range []string{"handler_runnable", "coroutine"} {
		for _, metric := range []string{"duration_ms", "failure.count"} {
			suffix := "." + kind + "." + metric
			if strings.HasSuffix(value, suffix) && len(value) > len(suffix) {
				return strings.TrimSuffix(value, suffix), kind, metric, true
			}
		}
	}
	return "", "", "", false
}

func (a *asyncAnalysisAccumulator) finalize() *AsyncAnalysis {
	if len(a.executors) == 0 && len(a.tasks) == 0 {
		return nil
	}
	result := &AsyncAnalysis{
		Executors: make([]AsyncExecutorStats, 0, len(a.executors)),
		Tasks:     make([]AsyncTaskStats, 0, len(a.tasks)),
	}
	for name, aggregate := range a.executors {
		result.Executors = append(result.Executors, AsyncExecutorStats{
			Name: name, Started: aggregate.started, Failures: aggregate.failures,
			WaitSamples: aggregate.wait.count, AvgWaitMS: aggregate.wait.average(), MaxWaitMS: aggregate.wait.max,
			ServiceSamples: aggregate.service.count, AvgServiceMS: aggregate.service.average(), MaxServiceMS: aggregate.service.max,
			QueueSamples: aggregate.queue.count, AvgQueueDepthX100: aggregate.queue.averageScaled(100), MaxQueueDepth: aggregate.queue.max,
			ActiveSamples: aggregate.active.count, AvgActiveCountX100: aggregate.active.averageScaled(100), MaxActiveCount: aggregate.active.max,
			MaxPoolSize: aggregate.pool.max, CompletedHighWatermark: aggregate.completed.max,
		})
	}
	for key, aggregate := range a.tasks {
		result.Tasks = append(result.Tasks, AsyncTaskStats{
			Kind: key.kind, Owner: key.owner, DurationSamples: aggregate.duration.count,
			AvgDurationMS: aggregate.duration.average(), MaxDurationMS: aggregate.duration.max,
			Failures: aggregate.failures,
		})
	}
	sort.Slice(result.Executors, func(i, j int) bool { return result.Executors[i].Name < result.Executors[j].Name })
	sort.Slice(result.Tasks, func(i, j int) bool {
		if result.Tasks[i].Kind != result.Tasks[j].Kind {
			return result.Tasks[i].Kind < result.Tasks[j].Kind
		}
		return result.Tasks[i].Owner < result.Tasks[j].Owner
	})
	return result
}

type runtimeTimedPoint struct {
	logIndex uint64
	timeMS   uint64
}

type runtimeUIWindow struct {
	logIndex uint64
	startMS  uint64
	endMS    uint64
}

type gcAnalysisAccumulator struct {
	collectionCount uint64
	totalTimeMS     uint64
	blockingCount   uint64
	blockingTimeMS  uint64
	bytesAllocated  uint64
	bytesFreed      uint64
	allocationRate  metricAggregate
	collectionTimes []runtimeTimedPoint
	jankyUIWindows  []runtimeUIWindow
}

func (a *gcAnalysisAccumulator) addMetric(name string, event jhlog.Event, logIndex uint64) {
	observation := observationOf(event)
	switch name {
	case workerMetricGCCount:
		a.collectionCount = saturatingUint64Sum(a.collectionCount, observation.sum)
		if observation.sum > 0 {
			a.collectionTimes = append(a.collectionTimes, runtimeTimedPoint{logIndex: logIndex, timeMS: event.TimeMS})
		}
	case workerMetricGCTime:
		a.totalTimeMS = saturatingUint64Sum(a.totalTimeMS, observation.sum)
	case workerMetricGCBlocking:
		a.blockingCount = saturatingUint64Sum(a.blockingCount, observation.sum)
	case workerMetricGCBlockingTime:
		a.blockingTimeMS = saturatingUint64Sum(a.blockingTimeMS, observation.sum)
	case workerMetricGCAllocated:
		a.bytesAllocated = saturatingUint64Sum(a.bytesAllocated, observation.sum)
	case workerMetricGCFreed:
		a.bytesFreed = saturatingUint64Sum(a.bytesFreed, observation.sum)
	case workerMetricAllocationRate:
		a.allocationRate.add(observation)
	}
}

func (a *gcAnalysisAccumulator) addUIWindow(event jhlog.Event, logIndex uint64) {
	if event.UIWindow == nil || event.UIWindow.JankCount == 0 {
		return
	}
	a.jankyUIWindows = append(a.jankyUIWindows, runtimeUIWindow{
		logIndex: logIndex,
		startMS:  saturatingSub(event.TimeMS, event.UIWindow.WindowMS),
		endMS:    event.TimeMS,
	})
}

func (a *gcAnalysisAccumulator) finalize() *GCAnalysis {
	if a.collectionCount == 0 && a.totalTimeMS == 0 && a.blockingCount == 0 &&
		a.bytesAllocated == 0 && a.bytesFreed == 0 && a.allocationRate.count == 0 {
		return nil
	}
	return &GCAnalysis{
		CollectionCount: a.collectionCount, TotalTimeMS: a.totalTimeMS,
		BlockingCount: a.blockingCount, BlockingTimeMS: a.blockingTimeMS,
		BytesAllocated: a.bytesAllocated, BytesFreed: a.bytesFreed,
		AllocationRateSamples:        a.allocationRate.count,
		AvgAllocationRateBytesPerSec: a.allocationRate.average(),
		MaxAllocationRateBytesPerSec: a.allocationRate.max,
		CollectionWindows:            uint64(len(a.collectionTimes)),
		JankyUIWindowsNearGC:         correlatedRuntimeWindows(a.collectionTimes, a.jankyUIWindows),
	}
}

func correlatedRuntimeWindows(points []runtimeTimedPoint, windows []runtimeUIWindow) uint64 {
	if len(points) == 0 || len(windows) == 0 {
		return 0
	}
	sort.Slice(points, func(i, j int) bool {
		if points[i].logIndex != points[j].logIndex {
			return points[i].logIndex < points[j].logIndex
		}
		return points[i].timeMS < points[j].timeMS
	})
	sort.Slice(windows, func(i, j int) bool {
		if windows[i].logIndex != windows[j].logIndex {
			return windows[i].logIndex < windows[j].logIndex
		}
		return windows[i].startMS < windows[j].startMS
	})
	var correlated uint64
	pointIndex := 0
	for _, window := range windows {
		for pointIndex < len(points) && (points[pointIndex].logIndex < window.logIndex ||
			(points[pointIndex].logIndex == window.logIndex && points[pointIndex].timeMS < window.startMS)) {
			pointIndex++
		}
		if pointIndex < len(points) && points[pointIndex].logIndex == window.logIndex && points[pointIndex].timeMS <= window.endMS {
			correlated++
		}
	}
	return correlated
}

type startupAnalysisAccumulator struct {
	coldResume  metricAggregate
	uiVisible   uint64
	uiHidden    uint64
	screens     map[string]*metricAggregate
	transitions map[string]uint64
}

func (a *startupAnalysisAccumulator) add(name string, event jhlog.Event) {
	observation := observationOf(event)
	switch name {
	case "app.lifecycle.first_resume_ms":
		a.coldResume.add(observation)
	case "app.lifecycle.ui_visible.count":
		a.uiVisible = saturatingUint64Sum(a.uiVisible, observation.sum)
	case "app.lifecycle.ui_hidden.count":
		a.uiHidden = saturatingUint64Sum(a.uiHidden, observation.sum)
	default:
		if screen, ok := parseScreenResumeMetric(name); ok {
			if a.screens == nil {
				a.screens = make(map[string]*metricAggregate)
			}
			aggregate := a.screens[screen]
			if aggregate == nil {
				aggregate = &metricAggregate{}
				a.screens[screen] = aggregate
			}
			aggregate.add(observation)
			return
		}
		if transition, ok := parseTransitionMetric(name); ok {
			if a.transitions == nil {
				a.transitions = make(map[string]uint64)
			}
			a.transitions[transition] = saturatingUint64Sum(a.transitions[transition], observation.sum)
		}
	}
}

func parseScreenResumeMetric(name string) (string, bool) {
	const suffix = ".lifecycle.time_to_resume_ms"
	if !strings.HasPrefix(name, startupScreenPrefix) || !strings.HasSuffix(name, suffix) {
		return "", false
	}
	screen := strings.TrimSuffix(strings.TrimPrefix(name, startupScreenPrefix), suffix)
	return screen, screen != ""
}

func parseTransitionMetric(name string) (string, bool) {
	const prefix = "screen.transition."
	const suffix = ".count"
	if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
		return "", false
	}
	transition := strings.TrimSuffix(strings.TrimPrefix(name, prefix), suffix)
	if strings.HasPrefix(transition, "to.") || !strings.Contains(transition, ".to.") {
		return "", false
	}
	return strings.Replace(transition, ".to.", " → ", 1), true
}

func (a *startupAnalysisAccumulator) finalize() *StartupAnalysis {
	if a.coldResume.count == 0 && a.uiVisible == 0 && a.uiHidden == 0 &&
		len(a.screens) == 0 && len(a.transitions) == 0 {
		return nil
	}
	result := &StartupAnalysis{
		ColdResumeSamples: a.coldResume.count,
		AvgColdResumeMS:   a.coldResume.average(), MaxColdResumeMS: a.coldResume.max,
		UIVisibleCount: a.uiVisible, UIHiddenCount: a.uiHidden,
		Screens:     make([]StartupScreenStats, 0, len(a.screens)),
		Transitions: make([]NamedValue, 0, len(a.transitions)),
	}
	for screen, aggregate := range a.screens {
		result.Screens = append(result.Screens, StartupScreenStats{
			Screen: screen, ResumeSamples: aggregate.count,
			AvgResumeMS: aggregate.average(), MaxResumeMS: aggregate.max,
		})
	}
	for transition, count := range a.transitions {
		result.Transitions = append(result.Transitions, NamedValue{Name: transition, Value: count})
	}
	sort.Slice(result.Screens, func(i, j int) bool { return result.Screens[i].Screen < result.Screens[j].Screen })
	sortNamed(result.Transitions)
	return result
}

type runtimeAnalysisAccumulator struct {
	async   asyncAnalysisAccumulator
	gc      gcAnalysisAccumulator
	startup startupAnalysisAccumulator
}

func (a *runtimeAnalysisAccumulator) addMetric(name string, event jhlog.Event, logIndex uint64) {
	a.async.add(name, event)
	a.gc.addMetric(name, event, logIndex)
	a.startup.add(name, event)
}
