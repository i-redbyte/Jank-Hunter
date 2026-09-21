package analyze

import (
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func TestSignalContextKeyDoesNotAllocate(t *testing.T) {
	collector := newCollector("allocation test", 1, Options{})
	collector.ensureSignalContext(collector.contextKey("Screen", "Owner"))

	allocations := testing.AllocsPerRun(1_000, func() {
		collector.ensureSignalContext(collector.contextKey("Screen", "Owner"))
	})
	if allocations != 0 {
		t.Fatalf("signal-context lookup allocates %.2f objects, want zero", allocations)
	}
}

func TestSignalContextKeyCannotCollideOnFieldSeparator(t *testing.T) {
	collector := newCollector("collision test", 1, Options{})
	collector.currentAttrScreen = "screen\x00unknown"
	collector.currentAttrOwner = "owner"
	first := collector.contextKey("", "")

	collector.currentAttrScreen = "screen"
	collector.currentAttrOwner = "unknown\x00owner"
	second := collector.contextKey("", "")

	if first == second {
		t.Fatal("different signal contexts produced the same aggregation key")
	}
}

func TestMemoryLeakKeyCannotCollideOnFieldSeparator(t *testing.T) {
	collector := newCollector("collision test", 1, Options{})
	collector.addMemoryLeakSuspect(
		"class\x00holder", "screen",
		SignalContextStats{Screen: "operation", Operation: "owner"},
		1, 1, jhlog.RetentionEvidenceTimeOnly, true,
	)
	collector.addMemoryLeakSuspect(
		"class", "holder",
		SignalContextStats{Screen: "screen", Operation: "operation\x00owner"},
		1, 1, jhlog.RetentionEvidenceTimeOnly, true,
	)

	if got := len(collector.memoryLeakStats); got != 2 {
		t.Fatalf("memory-leak aggregation has %d entries, want two distinct contexts", got)
	}
}

func TestOwnerFilterHotPathDoesNotAllocate(t *testing.T) {
	collector := newCollector("owner filter allocation test", 1, Options{Filter: Filter{OwnerContains: "presenter"}})
	context := SignalContextStats{Owner: "com.example.FeedPresenter"}
	candidates := []string{"com.example.FallbackOwner"}
	if !collector.matchesFilters("", context, nil, candidates...) {
		t.Fatal("owner filter test setup does not match")
	}

	allocations := testing.AllocsPerRun(1_000, func() {
		if !collector.matchesFilters("", context, nil, candidates...) {
			t.Fatal("owner filter unexpectedly stopped matching")
		}
	})
	if allocations != 0 {
		t.Fatalf("owner filter allocates %.2f objects per event, want zero", allocations)
	}
}

func TestMemoryLeakCountersSaturateInsteadOfWrapping(t *testing.T) {
	collector := newCollector("retention overflow test", 1, Options{})
	context := SignalContextStats{Screen: "Feed", Operation: "open"}
	collector.addMemoryLeakSuspect(
		"com.example.LeakedActivity", "com.example.AppCache", context,
		30_000, ^uint64(0), jhlog.RetentionEvidenceAfterExplicitGC, true,
	)
	collector.addMemoryLeakSuspect(
		"com.example.LeakedActivity", "com.example.AppCache", context,
		30_000, 1, jhlog.RetentionEvidenceAfterExplicitGC, true,
	)

	for _, stats := range collector.memoryLeakStats {
		if stats.count != ^uint64(0) || stats.afterExplicitGCCount != ^uint64(0) {
			t.Fatalf("retention counters wrapped: %+v", stats)
		}
		return
	}
	t.Fatal("retention statistics were not recorded")
}

func TestCollectorTelemetryCountersSaturateInsteadOfWrapping(t *testing.T) {
	collector := newCollector("counter overflow test", 1, Options{})
	dict := map[uint64]string{
		1: "android.util.Log", 2: "com.example.Target.call", 3: "main_thread_io", 4: "custom.counter",
	}
	max := ^uint64(0)
	events := []jhlog.Event{
		{Type: jhlog.EventUIWindow, UIWindow: &jhlog.UIWindowEvent{WindowMS: max, FrameCount: max, JankCount: max}},
		{Type: jhlog.EventUIWindow, UIWindow: &jhlog.UIWindowEvent{WindowMS: 1, FrameCount: 1, JankCount: 1}},
		{Type: jhlog.EventLogSpam, LogSpam: &jhlog.LogSpamEvent{SourceRef: jhlog.LocalSymbol(1), Count: max}},
		{Type: jhlog.EventLogSpam, LogSpam: &jhlog.LogSpamEvent{SourceRef: jhlog.LocalSymbol(1), Count: 1}},
		{Type: jhlog.EventRuntimeCall, RuntimeCall: &jhlog.RuntimeCallEvent{CalleeRef: jhlog.LocalSymbol(2), Count: max, TotalMS: max}},
		{Type: jhlog.EventRuntimeCall, RuntimeCall: &jhlog.RuntimeCallEvent{CalleeRef: jhlog.LocalSymbol(2), Count: 1, TotalMS: 1}},
		{Type: jhlog.EventProblem, Problem: &jhlog.ProblemEvent{KindRef: jhlog.LocalSymbol(3), Count: max, WindowMS: max}},
		{Type: jhlog.EventProblem, Problem: &jhlog.ProblemEvent{KindRef: jhlog.LocalSymbol(3), Count: 1, WindowMS: 1}},
		{Type: jhlog.EventCounter, Metric: &jhlog.MetricEvent{MetricRef: jhlog.LocalSymbol(4), Value: max}},
		{Type: jhlog.EventCounter, Metric: &jhlog.MetricEvent{MetricRef: jhlog.LocalSymbol(4), Value: 1}},
	}
	for _, event := range events {
		collector.add(dict, event)
	}

	if stats := collector.screenStats["unknown"]; stats.WindowMS != max || stats.Frames != max || stats.JankyFrames != max {
		t.Fatalf("UI counters wrapped: %+v", stats)
	}
	for _, stats := range collector.logSpamStats {
		if stats.Count != max {
			t.Fatalf("log counter wrapped: %+v", stats)
		}
	}
	for _, stats := range collector.runtimeCallStats {
		if stats.Count != max || stats.TotalMS != max {
			t.Fatalf("runtime call counters wrapped: %+v", stats)
		}
	}
	for _, stats := range collector.problemStats {
		if stats.Kind == "main_thread_io" && (stats.Count != max || stats.TotalWindowMS != max) {
			t.Fatalf("problem counters wrapped: %+v", stats)
		}
	}
	if collector.counterValues["custom.counter"] != max {
		t.Fatalf("custom counter wrapped: %d", collector.counterValues["custom.counter"])
	}
}

func TestGaugeStatsSaturateCountAndPreserveWideTotal(t *testing.T) {
	stats := gaugeStats{count: ^uint64(0), total: ^uint64(0), mode: jhlog.MetricModeAverage}
	stats.add(1, 1, 1, 1, jhlog.MetricModeAverage)
	if stats.count != ^uint64(0) || stats.total != 0 || stats.totalHigh != 1 {
		t.Fatalf("gauge counters wrapped: %+v", stats)
	}
}

func TestBooleanGaugeRateDoesNotOverflow(t *testing.T) {
	stats := gaugeStats{count: ^uint64(0), total: ^uint64(0), mode: jhlog.MetricModeBooleanRate}
	if got := stats.value(); got != 100 {
		t.Fatalf("boolean gauge rate = %d, want 100", got)
	}
}
