package analyze

import (
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func TestInspectBuildsAsyncGCAndStartupAnalysisFromBoundedMetrics(t *testing.T) {
	const mib = uint64(1024 * 1024)
	dict := map[uint64]string{
		1:  "executor.render.wait_ms",
		2:  "executor.render.service_ms",
		3:  "executor.render.queue_depth",
		4:  "executor.render.active_count",
		5:  "executor.render.pool_size",
		6:  "executor.render.completed_task_count",
		7:  "executor.render.started.count",
		8:  "executor.render.failure.count",
		9:  "owner.FeedRepository.coroutine.duration_ms",
		10: "owner.FeedRepository.coroutine.failure.count",
		11: "gc.count.delta",
		12: "gc.time_ms.delta",
		13: "gc.blocking_count.delta",
		14: "gc.blocking_time_ms.delta",
		15: "gc.bytes_allocated.delta",
		16: "gc.bytes_freed.delta",
		17: "memory.allocation_rate_bytes_per_sec",
		18: "app.lifecycle.first_resume_ms",
		19: "screen.Checkout.lifecycle.time_to_resume_ms",
		20: "screen.transition.Feed.to.Checkout.count",
		21: "app.lifecycle.ui_visible.count",
		22: "app.lifecycle.ui_hidden.count",
	}
	metric := func(eventType jhlog.EventType, timeMS, ref, value, count, sum, maximum uint64) jhlog.Event {
		return jhlog.Event{Type: eventType, TimeMS: timeMS, Metric: &jhlog.MetricEvent{
			MetricRef: jhlog.LocalSymbol(ref), Value: value, Count: count, Sum: sum, Max: maximum,
		}}
	}
	events := []jhlog.Event{
		metric(jhlog.EventGauge, 100, 1, 80, 10, 800, 300),
		metric(jhlog.EventGauge, 100, 2, 150, 10, 1_500, 700),
		metric(jhlog.EventGauge, 100, 3, 3, 4, 10, 5),
		metric(jhlog.EventGauge, 100, 4, 3, 4, 12, 4),
		metric(jhlog.EventGauge, 100, 5, 4, 4, 16, 4),
		metric(jhlog.EventGauge, 100, 6, 8, 4, 20, 8),
		metric(jhlog.EventCounter, 100, 7, 10, 0, 0, 0),
		metric(jhlog.EventCounter, 100, 8, 2, 0, 0, 0),
		metric(jhlog.EventGauge, 200, 9, 500, 2, 1_000, 700),
		metric(jhlog.EventCounter, 200, 10, 1, 0, 0, 0),
		{Type: jhlog.EventUIWindow, TimeMS: 1_000, UIWindow: &jhlog.UIWindowEvent{
			WindowMS: 1_000, FrameCount: 60, JankCount: 10,
		}},
		metric(jhlog.EventCounter, 900, 11, 4, 0, 0, 0),
		metric(jhlog.EventCounter, 900, 12, 120, 0, 0, 0),
		metric(jhlog.EventCounter, 900, 13, 2, 0, 0, 0),
		metric(jhlog.EventCounter, 900, 14, 80, 0, 0, 0),
		metric(jhlog.EventCounter, 900, 15, 64*mib, 0, 0, 0),
		metric(jhlog.EventCounter, 900, 16, 32*mib, 0, 0, 0),
		metric(jhlog.EventGauge, 900, 17, 20*mib, 3, 60*mib, 30*mib),
		metric(jhlog.EventGauge, 1_100, 18, 1_800, 2, 3_600, 2_400),
		metric(jhlog.EventGauge, 1_200, 19, 700, 3, 2_100, 1_200),
		metric(jhlog.EventCounter, 1_300, 20, 3, 0, 0, 0),
		metric(jhlog.EventCounter, 1_300, 21, 2, 0, 0, 0),
		metric(jhlog.EventCounter, 1_300, 22, 1, 0, 0, 0),
	}

	summary := inspectLogsForTest("runtime", []jhlog.Log{{Dict: dict, Events: events}})

	async := summary.AsyncAnalysis
	if async == nil || len(async.Executors) != 1 || len(async.Tasks) != 1 {
		t.Fatalf("async analysis = %+v", async)
	}
	executor := async.Executors[0]
	if executor.Name != "render" || executor.Started != 10 || executor.Failures != 2 ||
		executor.WaitSamples != 10 || executor.AvgWaitMS != 80 || executor.MaxWaitMS != 300 ||
		executor.ServiceSamples != 10 || executor.AvgServiceMS != 150 || executor.MaxServiceMS != 700 ||
		executor.QueueSamples != 4 || executor.AvgQueueDepthX100 != 250 || executor.MaxQueueDepth != 5 ||
		executor.AvgActiveCountX100 != 300 || executor.MaxActiveCount != 4 || executor.MaxPoolSize != 4 ||
		executor.CompletedHighWatermark != 8 {
		t.Fatalf("executor analysis = %+v", executor)
	}
	task := async.Tasks[0]
	if task.Kind != "coroutine" || task.Owner != "FeedRepository" || task.DurationSamples != 2 ||
		task.AvgDurationMS != 500 || task.MaxDurationMS != 700 || task.Failures != 1 {
		t.Fatalf("async task analysis = %+v", task)
	}

	gc := summary.GCAnalysis
	if gc == nil || gc.CollectionCount != 4 || gc.TotalTimeMS != 120 || gc.BlockingCount != 2 ||
		gc.BlockingTimeMS != 80 || gc.BytesAllocated != 64*mib || gc.BytesFreed != 32*mib ||
		gc.AllocationRateSamples != 3 || gc.AvgAllocationRateBytesPerSec != 20*mib ||
		gc.MaxAllocationRateBytesPerSec != 30*mib || gc.CollectionWindows != 1 ||
		gc.JankyUIWindowsNearGC != 1 {
		t.Fatalf("GC analysis = %+v", gc)
	}

	startup := summary.StartupAnalysis
	if startup == nil || startup.ColdResumeSamples != 2 || startup.AvgColdResumeMS != 1_800 ||
		startup.MaxColdResumeMS != 2_400 || startup.UIVisibleCount != 2 || startup.UIHiddenCount != 1 ||
		len(startup.Screens) != 1 || len(startup.Transitions) != 1 {
		t.Fatalf("startup analysis = %+v", startup)
	}
	if screen := startup.Screens[0]; screen.Screen != "Checkout" || screen.ResumeSamples != 3 ||
		screen.AvgResumeMS != 700 || screen.MaxResumeMS != 1_200 {
		t.Fatalf("startup screen = %+v", screen)
	}
	if transition := startup.Transitions[0]; transition.Name != "Feed → Checkout" || transition.Value != 3 {
		t.Fatalf("startup transition = %+v", transition)
	}
}

func TestRuntimeAnalysisDoesNotInventModelsWithoutRelevantMetrics(t *testing.T) {
	summary := inspectLogsForTest("empty", []jhlog.Log{{Events: []jhlog.Event{{
		Type: jhlog.EventGauge, Metric: &jhlog.MetricEvent{Value: 42},
	}}}})
	if summary.AsyncAnalysis != nil || summary.GCAnalysis != nil || summary.StartupAnalysis != nil {
		t.Fatalf("unexpected runtime analysis: async=%+v gc=%+v startup=%+v", summary.AsyncAnalysis, summary.GCAnalysis, summary.StartupAnalysis)
	}
}

func TestProblemEngineSurfacesOnlyMaterialRuntimeSignals(t *testing.T) {
	summary := Summary{
		DurationMS:        10_000,
		CollectionQuality: CollectionQuality{Complete: true},
		AnalysisInputs:    AnalysisInputCompleteness{Complete: true, RuntimeEvidence: true},
		AsyncAnalysis: &AsyncAnalysis{Executors: []AsyncExecutorStats{{
			Name: "render", Started: 10, WaitSamples: 10, AvgWaitMS: 80, MaxWaitMS: 300,
			QueueSamples: 4, MaxQueueDepth: 5, MaxActiveCount: 4, MaxPoolSize: 4,
		}}},
		GCAnalysis: &GCAnalysis{
			CollectionCount: 4, TotalTimeMS: 120, BlockingCount: 2, BlockingTimeMS: 80,
			CollectionWindows: 1, JankyUIWindowsNearGC: 1,
		},
		StartupAnalysis: &StartupAnalysis{
			ColdResumeSamples: 2, AvgColdResumeMS: 1_800, MaxColdResumeMS: 2_400,
			Screens: []StartupScreenStats{{Screen: "Checkout", ResumeSamples: 3, AvgResumeMS: 700, MaxResumeMS: 1_200}},
		},
	}

	report, err := BuildProblemReport(summary)
	if err != nil {
		t.Fatal(err)
	}
	for _, detector := range []string{"cpu.async_queue", "memory.gc_blocking", "ui.startup_cold", "ui.screen_resume"} {
		finding := findingByDetector(report.Problems, detector)
		if finding == nil {
			t.Fatalf("%s finding missing: %+v", detector, report.Problems)
		}
		if len(finding.PriorityBreakdown) != 5 || len(finding.Drilldowns) != 1 {
			t.Fatalf("%s explanation is incomplete: %+v", detector, finding)
		}
		visible := strings.ToLower(problemFindingVisibleText(*finding))
		for _, forbidden := range []string{
			"unknown", "executor-wrapper", "samples", "max/average", "queue wait",
			"blocking gc", "stop-the-world", "tail latency", "allocation profile",
			"pressure", "allocator", "process counters", "metric event", "process-level",
			"create→resume", "lifecycle", "first draw", "fully drawn", "startup",
		} {
			if strings.Contains(visible, forbidden) {
				t.Fatalf("%s finding contains untranslated term %q: %s", detector, forbidden, visible)
			}
		}
	}
	gc := findingByDetector(report.Problems, "memory.gc_blocking")
	if gc.Why.ClaimLevel != "correlated" {
		t.Fatalf("GC causal boundary = %+v", gc.Why)
	}
}
