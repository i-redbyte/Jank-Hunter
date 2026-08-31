package analyze

import (
	"math/rand"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

var benchmarkWorkerAnalysis *WorkerAnalysis

func TestWorkerCorrelationCollectionStaysDormantWithoutWorkerEvidence(t *testing.T) {
	collector := newCollector("no workers", 1, Options{})
	collector.startLog(jhlog.SegmentHeader{})
	dict := map[uint64]string{1: workerMetricDeviceCPU}
	collector.add(dict, jhlog.Event{Type: jhlog.EventMemory, TimeMS: 10, Memory: &jhlog.MemoryEvent{PSSKB: 1_000}})
	collector.add(dict, jhlog.Event{Type: jhlog.EventHTTP, TimeMS: 20, HTTP: &jhlog.HTTPEvent{DurationMS: 5}})
	collector.add(dict, jhlog.Event{Type: jhlog.EventIO, TimeMS: 30, IO: &jhlog.IOEvent{
		Operation: jhlog.IOOperationFileRead, Outcome: jhlog.IOOutcomeSuccess, DurationUS: 1_000,
	}})
	collector.add(dict, jhlog.Event{Type: jhlog.EventGauge, TimeMS: 40, Metric: &jhlog.MetricEvent{
		MetricRef: jhlog.LocalSymbol(1), Value: 2_000,
	}})

	if len(collector.workerHTTP) != 0 || len(collector.workerIO) != 0 ||
		len(collector.workerMemory) != 0 || len(collector.workerMetrics) != 0 {
		t.Fatalf(
			"worker correlation retained without worker evidence: http=%d io=%d memory=%d metrics=%d",
			len(collector.workerHTTP), len(collector.workerIO), len(collector.workerMemory), len(collector.workerMetrics),
		)
	}
}

func TestWorkerCollectorFlagPreservesSignalsBeforeIncompleteLifecycle(t *testing.T) {
	dict := map[uint64]string{1: "com.app.IncompleteWorker"}
	events := []jhlog.Event{
		{Type: jhlog.EventSession, TimeMS: 1, Session: &jhlog.SessionEvent{CollectorFlags: uint64(jhlog.CollectorWorker)}},
		{Type: jhlog.EventHTTP, TimeMS: 15, HTTP: &jhlog.HTTPEvent{DurationMS: 5}},
		{Type: jhlog.EventWorker, TimeMS: 20, Worker: &jhlog.WorkerEvent{
			WorkerRef: jhlog.LocalSymbol(1), InstanceID: 1, Stage: jhlog.WorkerStageFinished,
			Outcome: jhlog.WorkerOutcomeSuccess, DurationMS: 10,
		}},
	}

	analysis := inspectLogsForTest("incomplete worker", []jhlog.Log{{Dict: dict, Events: events}}).WorkerAnalysis
	if analysis == nil || len(analysis.Workers) != 1 || analysis.Workers[0].CorrelatedHTTPCalls != 1 {
		t.Fatalf("collector flag did not preserve pre-finish correlation: %+v", analysis)
	}
}

func TestWorkerAnalysisJoinsLifecycleAndCorrelatesRuntimeSignals(t *testing.T) {
	dict := map[uint64]string{
		1: "com.app.SyncWorker.doWork",
		2: "com.app.UploadWorker.doWork",
		3: "process.cpu.device_percent_x100",
		4: "memory.allocation_rate_bytes_per_sec",
		5: "gc.count.delta",
	}
	worker := func(timeMS, instance uint64, stage jhlog.WorkerStage, outcome jhlog.WorkerOutcome, duration uint64, ref uint64, flags uint64) jhlog.Event {
		return jhlog.Event{Type: jhlog.EventWorker, TimeMS: timeMS, Flags: flags, Worker: &jhlog.WorkerEvent{
			WorkerRef: jhlog.LocalSymbol(ref), InstanceID: instance, Stage: stage,
			Outcome: outcome, DurationMS: duration,
		}}
	}
	events := []jhlog.Event{
		{Type: jhlog.EventMemory, TimeMS: 50, Memory: &jhlog.MemoryEvent{PSSKB: 1_000}},
		worker(100, 1, jhlog.WorkerStageEnqueued, jhlog.WorkerOutcomeUnknown, 0, 1, 0),
		worker(200, 2, jhlog.WorkerStageEnqueued, jhlog.WorkerOutcomeUnknown, 0, 2, uint64(jhlog.FlagWorkerPeriodic)),
		{Type: jhlog.EventMemory, TimeMS: 250, Memory: &jhlog.MemoryEvent{PSSKB: 1_100}},
		worker(300, 1, jhlog.WorkerStageStarted, jhlog.WorkerOutcomeUnknown, 0, 1, 0),
		{Type: jhlog.EventGauge, TimeMS: 400, Metric: &jhlog.MetricEvent{MetricRef: jhlog.LocalSymbol(3), Count: 2, Sum: 12_000, Max: 7_000, Mode: jhlog.MetricModeAverage}},
		{Type: jhlog.EventHTTP, TimeMS: 425, HTTP: &jhlog.HTTPEvent{DurationMS: 25, StatusCode: 503}},
		worker(450, 2, jhlog.WorkerStageStarted, jhlog.WorkerOutcomeUnknown, 0, 2, uint64(jhlog.FlagWorkerPeriodic|jhlog.FlagThreadMain)),
		{Type: jhlog.EventGauge, TimeMS: 500, Metric: &jhlog.MetricEvent{MetricRef: jhlog.LocalSymbol(4), Value: 4_096}},
		{Type: jhlog.EventCounter, TimeMS: 700, Metric: &jhlog.MetricEvent{MetricRef: jhlog.LocalSymbol(5), Value: 2}},
		{Type: jhlog.EventMemory, TimeMS: 800, Memory: &jhlog.MemoryEvent{PSSKB: 1_400}},
		worker(800, 1, jhlog.WorkerStageFinished, jhlog.WorkerOutcomeSuccess, 500, 1, 0),
		{Type: jhlog.EventIO, TimeMS: 825, Flags: uint64(jhlog.FlagIOBytesKnown), IO: &jhlog.IOEvent{Operation: jhlog.IOOperationFileWrite, Outcome: jhlog.IOOutcomeSuccess, DurationUS: 20_000, Bytes: 2_048}},
		worker(850, 2, jhlog.WorkerStageFinished, jhlog.WorkerOutcomeRetry, 400, 2, uint64(jhlog.FlagWorkerPeriodic)),
		worker(900, 3, jhlog.WorkerStageStarted, jhlog.WorkerOutcomeUnknown, 0, 1, 0),
		{Type: jhlog.EventMemory, TimeMS: 950, Memory: &jhlog.MemoryEvent{PSSKB: 1_500}},
		worker(1_000, 4, jhlog.WorkerStageFinished, jhlog.WorkerOutcomeFailure, 10, 2, 0),
	}

	summary := inspectLogsForTest("workers", []jhlog.Log{{Dict: dict, Events: events}})
	analysis := summary.WorkerAnalysis
	if analysis == nil {
		t.Fatal("WorkerAnalysis is nil")
	}
	if analysis.Instances != 4 || analysis.Executions != 4 || analysis.Enqueued != 2 || analysis.Started != 3 || analysis.Finished != 3 {
		t.Fatalf("lifecycle totals = %+v", analysis)
	}
	if analysis.Success != 1 || analysis.Failures != 1 || analysis.Retries != 1 || analysis.Cancelled != 0 {
		t.Fatalf("outcomes = %+v", analysis)
	}
	if analysis.WaitSamples != 2 || analysis.WaitP50MS != 200 || analysis.WaitP95MS != 250 || analysis.WaitMaxMS != 250 || analysis.TotalWaitMS != 450 {
		t.Fatalf("wait stats = %+v", analysis)
	}
	if analysis.RunSamples != 3 || analysis.RunP50MS != 400 || analysis.RunP95MS != 500 || analysis.RunMaxMS != 500 || analysis.TotalRunMS != 910 {
		t.Fatalf("run stats = %+v", analysis)
	}
	if analysis.MaxRunning != 2 || analysis.MaxQueued != 2 || analysis.MainThreadStarts != 1 || analysis.PeriodicInstances != 1 {
		t.Fatalf("load stats = %+v", analysis)
	}
	if analysis.MissingEnqueue != 1 || analysis.MissingStart != 1 || analysis.MissingFinish != 1 {
		t.Fatalf("incomplete lifecycle = %+v", analysis)
	}

	workers := workerStatsByName(analysis.Workers)
	sync := workers["com.app.SyncWorker"]
	if sync.Instances != 2 || sync.Executions != 2 || sync.Success != 1 || sync.MissingFinish != 1 {
		t.Fatalf("sync worker = %+v", sync)
	}
	if sync.CorrelatedHTTPCalls != 1 || sync.CorrelatedHTTPFailures != 1 || sync.CPUSamples != 2 || sync.AvgDeviceCPUPercentX100 != 6_000 || sync.MaxDeviceCPUPercentX100 != 7_000 {
		t.Fatalf("sync correlations = %+v", sync)
	}
	if sync.AllocationSamples != 1 || sync.AvgAllocationRateBytesPerSec != 4_096 || sync.GCCount != 2 {
		t.Fatalf("sync sampled load = %+v", sync)
	}
	if sync.MemoryPairs != 1 || sync.AvgPSSDeltaKB != 300 || sync.MaxPSSDuringKB != 1_400 {
		t.Fatalf("sync memory correlation = %+v", sync)
	}

	upload := workers["com.app.UploadWorker"]
	if upload.PeriodicInstances != 1 || upload.MainThreadStarts != 1 || upload.Retries != 1 || upload.Failures != 1 {
		t.Fatalf("upload worker = %+v", upload)
	}
	if upload.CorrelatedIOOperations != 1 || upload.CorrelatedIOBytes != 2_048 || upload.CorrelatedIODurationUS != 20_000 {
		t.Fatalf("upload I/O correlation = %+v", upload)
	}
}

func TestWorkerAnalysisNeverJoinsLifecycleAcrossIndependentLogs(t *testing.T) {
	const instance = 42
	logs := []jhlog.Log{{
		Dict: map[uint64]string{1: "com.app.SyncWorker"},
		Events: []jhlog.Event{{Type: jhlog.EventWorker, TimeMS: 100, Worker: &jhlog.WorkerEvent{
			WorkerRef: jhlog.LocalSymbol(1), InstanceID: instance, Stage: jhlog.WorkerStageStarted,
		}}},
	}, {
		Dict: map[uint64]string{1: "com.app.SyncWorker"},
		Events: []jhlog.Event{{Type: jhlog.EventWorker, TimeMS: 200, Worker: &jhlog.WorkerEvent{
			WorkerRef: jhlog.LocalSymbol(1), InstanceID: instance, Stage: jhlog.WorkerStageFinished,
			Outcome: jhlog.WorkerOutcomeSuccess, DurationMS: 100,
		}}},
	}}

	analysis := inspectLogsForTest("independent", logs).WorkerAnalysis
	if analysis == nil || analysis.Executions != 2 || analysis.MissingStart != 1 || analysis.MissingFinish != 1 {
		t.Fatalf("independent lifecycle was joined: %+v", analysis)
	}
}

func TestWorkerAnalysisJoinsLifecycleAcrossRotatedSegmentsOfSameProcess(t *testing.T) {
	collector := newCollector("rotation", 2, Options{})
	dict := map[uint64]string{1: "com.app.SyncWorker"}
	collector.startLog(jhlog.SegmentHeader{})
	collector.add(dict, jhlog.Event{Type: jhlog.EventWorker, TimeMS: 100, Worker: &jhlog.WorkerEvent{
		WorkerRef: jhlog.LocalSymbol(1), InstanceID: 42, Stage: jhlog.WorkerStageStarted,
	}})
	collector.addStreamResult(jhlog.StreamResult{Source: "segment-0", Header: collectionTestHeader(7, 0)})
	collector.finishLog()
	collector.startLog(jhlog.SegmentHeader{})
	collector.add(dict, jhlog.Event{Type: jhlog.EventWorker, TimeMS: 600, Worker: &jhlog.WorkerEvent{
		WorkerRef: jhlog.LocalSymbol(1), InstanceID: 42, Stage: jhlog.WorkerStageFinished,
		Outcome: jhlog.WorkerOutcomeSuccess, DurationMS: 500,
	}})
	collector.addStreamResult(jhlog.StreamResult{Source: "segment-1", Header: collectionTestHeader(7, 1)})
	collector.finishLog()

	analysis := collector.finish().WorkerAnalysis
	if analysis == nil || analysis.Executions != 1 || analysis.MissingStart != 0 || analysis.MissingFinish != 0 || analysis.RunMaxMS != 500 {
		t.Fatalf("rotated lifecycle was not joined: %+v", analysis)
	}
}

func TestWorkerAnalysisReportsEnqueueWithoutStartWithoutInventingWait(t *testing.T) {
	analysis := inspectLogsForTest("enqueue only", []jhlog.Log{{
		Dict: map[uint64]string{1: "com.app.SyncWorker"},
		Events: []jhlog.Event{{Type: jhlog.EventWorker, TimeMS: 100, Worker: &jhlog.WorkerEvent{
			WorkerRef: jhlog.LocalSymbol(1), InstanceID: 42, Stage: jhlog.WorkerStageEnqueued,
		}}},
	}}).WorkerAnalysis
	if analysis == nil || analysis.MissingStart != 1 || analysis.WaitSamples != 0 || analysis.Executions != 0 {
		t.Fatalf("enqueue-only lifecycle = %+v", analysis)
	}
}

func TestWorkerAnalysisMatchesRepeatedPeriodicAttemptAfterCompactMatcherPromotion(t *testing.T) {
	events := make([]jhlog.Event, 0, 21)
	for cycle := range 10 {
		start := uint64(cycle * 100)
		events = append(events,
			jhlog.Event{Type: jhlog.EventWorker, TimeMS: start, Flags: uint64(jhlog.FlagWorkerPeriodic), Worker: &jhlog.WorkerEvent{
				WorkerRef: jhlog.LocalSymbol(1), InstanceID: 42, Stage: jhlog.WorkerStageStarted,
			}},
			jhlog.Event{Type: jhlog.EventWorker, TimeMS: start + 50, Flags: uint64(jhlog.FlagWorkerPeriodic), Worker: &jhlog.WorkerEvent{
				WorkerRef: jhlog.LocalSymbol(1), InstanceID: 42, Stage: jhlog.WorkerStageFinished,
				Outcome: jhlog.WorkerOutcomeSuccess, DurationMS: 50,
			}},
		)
	}
	analysis := inspectLogsForTest("periodic", []jhlog.Log{{
		Dict: map[uint64]string{1: "com.app.PeriodicWorker"}, Events: events,
	}}).WorkerAnalysis
	if analysis == nil || analysis.Executions != 10 || analysis.Success != 10 || analysis.MissingStart != 0 || analysis.MissingFinish != 0 {
		t.Fatalf("periodic lifecycle = %+v", analysis)
	}
}

func TestTypedWorkerLifecycleSuppressesSemanticMirrorFinding(t *testing.T) {
	summary := inspectLogsForTest("worker problem", []jhlog.Log{{
		Dict: map[uint64]string{1: "com.app.SyncWorker", 2: "jankhunter.semantic.v1.worker.failure.background"},
		Events: []jhlog.Event{
			{Type: jhlog.EventWorker, TimeMS: 100, Worker: &jhlog.WorkerEvent{WorkerRef: jhlog.LocalSymbol(1), InstanceID: 1, Stage: jhlog.WorkerStageStarted}},
			{Type: jhlog.EventWorker, TimeMS: 20_100, Worker: &jhlog.WorkerEvent{WorkerRef: jhlog.LocalSymbol(1), InstanceID: 1, Stage: jhlog.WorkerStageFinished, Outcome: jhlog.WorkerOutcomeFailure, DurationMS: 20_000}},
			{Type: jhlog.EventRuntimeCall, TimeMS: 20_100, Attribution: jhlog.AttributionContext{Present: true, Owner: jhlog.LocalSymbol(2)}, RuntimeCall: &jhlog.RuntimeCallEvent{CalleeRef: jhlog.LocalSymbol(1), Count: 1, TotalMS: 20_000, MaxMS: 20_000}},
		},
	}})
	report, err := BuildProblemReport(summary)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	typedEvidence := false
	for _, finding := range report.Problems {
		if finding.DetectorID == "cpu.worker_execution" {
			count++
			for _, evidence := range finding.Evidence {
				typedEvidence = typedEvidence || evidence.Source == "worker_lifecycle"
			}
		}
	}
	if count != 1 || !typedEvidence {
		t.Fatalf("typed worker finding count = %d evidence=%t, want one lifecycle finding: %+v", count, typedEvidence, report.Problems)
	}
}

func workerStatsByName(values []WorkerStats) map[string]WorkerStats {
	result := make(map[string]WorkerStats, len(values))
	for _, value := range values {
		result[value.Worker] = value
	}
	return result
}

func TestWorkerCostIndexMatchesIntervalScan(t *testing.T) {
	random := rand.New(rand.NewSource(32_621))
	intervals := make([]workerCostInterval, 0, 2_000)
	var index workerCostIndex
	for range 2_000 {
		start := uint64(random.Intn(20_000))
		interval := workerCostInterval{
			startMS: start, endMS: start + uint64(random.Intn(500)),
			failures: uint64(random.Intn(2)), duration: uint64(random.Intn(10_000)),
			bytes: uint64(random.Intn(1_000_000)),
		}
		intervals = append(intervals, interval)
		index.add(interval)
	}
	index.build()
	for range 500 {
		start := uint64(random.Intn(20_000))
		end := start + uint64(random.Intn(1_000))
		var want workerCostValue
		for _, interval := range intervals {
			if interval.startMS >= end || interval.endMS <= start {
				continue
			}
			want.count++
			want.failures += interval.failures
			want.duration += interval.duration
			want.bytes += interval.bytes
		}
		if got := index.query(start, end); got != want {
			t.Fatalf("query [%d,%d) = %+v, want %+v", start, end, got, want)
		}
	}
}

func TestWorkerPointIndexMatchesRangeScan(t *testing.T) {
	random := rand.New(rand.NewSource(1_032_621))
	var index workerPointIndex
	for range 2_000 {
		count := uint64(random.Intn(8) + 1)
		average := uint64(random.Intn(10_000))
		index.points = append(index.points, workerPoint{
			timeMS: uint64(random.Intn(20_000)), value: average * count,
			count: count, max: average + uint64(random.Intn(500)),
		})
	}
	index.build()
	for range 500 {
		start := uint64(random.Intn(20_000))
		end := start + uint64(random.Intn(1_000))
		var want workerPointValue
		for _, point := range index.points {
			if point.timeMS < start || point.timeMS > end {
				continue
			}
			want.count += point.count
			want.sum += point.value
			want.maximum = maxUint64(want.maximum, point.max)
		}
		if got := index.query(start, end); got != want {
			t.Fatalf("query [%d,%d] = %+v, want %+v", start, end, got, want)
		}
	}
}

func BenchmarkFinalizeWorkerAnalysisRepresentativeTwentyThousandExecutions(b *testing.B) {
	const executionCount = 20_000
	var workerNames [16]string
	for index := range workerNames {
		workerNames[index] = "com.app.Worker" + string(rune('A'+index))
	}
	for range b.N {
		collector := newCollector("benchmark", 1, Options{})
		collector.currentLogIndex = 1
		collector.workerEvents = make([]workerRawEvent, 0, executionCount*2)
		collector.workerHTTP = make([]workerCostInterval, 0, executionCount)
		collector.workerMetrics = make([]workerMetricPoint, 0, executionCount)
		for index := range executionCount {
			start := uint64(index * 10)
			instance := uint64(index + 1)
			worker := workerNames[index%len(workerNames)]
			collector.workerEvents = append(collector.workerEvents,
				workerRawEvent{logIndex: 1, sequence: uint64(index*2 + 1), timeMS: start, instanceID: instance, worker: worker, stage: jhlog.WorkerStageStarted},
				workerRawEvent{logIndex: 1, sequence: uint64(index*2 + 2), timeMS: start + 100, instanceID: instance, worker: worker, stage: jhlog.WorkerStageFinished, outcome: jhlog.WorkerOutcomeSuccess, durationMS: 100},
			)
			collector.workerHTTP = append(collector.workerHTTP, workerCostInterval{logIndex: 1, startMS: start + 20, endMS: start + 40, duration: 20})
			collector.workerMetrics = append(collector.workerMetrics, workerMetricPoint{
				workerPoint: workerPoint{logIndex: 1, timeMS: start + 50, value: 5_000, count: 1, max: 5_000},
				name:        workerMetricDeviceCPU,
			})
		}
		benchmarkWorkerAnalysis = collector.finalizeWorkerAnalysis()
	}
}
