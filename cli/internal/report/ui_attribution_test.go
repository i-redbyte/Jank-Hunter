package report

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
)

var runtimeCallCauseAllocationSink []uiCauseInsight

func TestUIScreenInsightsRankActionableCauseCandidates(t *testing.T) {
	insights := uiScreenInsights(analyze.Summary{
		Screens: []analyze.ScreenStats{{
			Screen: "FeedActivity", Frames: 240, JankyFrames: 48, JankRatePct: 20,
			FrameDeadlineUS: 16_667,
		}},
		DatabaseAnalysis: &analyze.DatabaseAnalysis{Statements: []analyze.DatabaseStatementStats{{
			Query: "SELECT item FROM feed WHERE id = ?", Operation: "чтение",
			Overall: analyze.DatabaseExecutionStats{Calls: 2, TotalDurationUS: 70_000, MaxDurationUS: 48_000},
			Main:    analyze.DatabaseExecutionStats{Calls: 2, TotalDurationUS: 70_000, MaxDurationUS: 48_000},
			Contexts: []analyze.DatabaseStatementContextStats{{
				Source: "FeedStore.load", Screen: "FeedActivity", ContextOperation: "feed.open",
				Main: analyze.DatabaseExecutionStats{Calls: 2, TotalDurationUS: 70_000, MaxDurationUS: 48_000},
				MainCorrelation: analyze.DatabaseCorrelationStats{
					UIWindowOverlaps: 1, UIFrames: 10, UIJankyFrames: 2, UIOverlapMaxDurationUS: 48_000,
				},
			}},
		}}},
		SignalContexts: []analyze.SignalContextStats{{
			Screen: "FeedActivity", Operation: "feed.open", Owner: "FeedPresenter.render",
			StallCount: 1, StallMaxMS: 820, HTTPCount: 3, HTTPP95MS: 1_200,
			RouteSample: "GET /feed",
		}},
		Owners: []analyze.OwnerStats{{
			Owner: "FeedPresenter.render", Kind: "main_thread_stall", Count: 1, MaxMS: 820,
			StackHint: "com.app.FeedView.onDraw(FeedView.kt:84)",
		}},
		ProblemWindows: []analyze.ProblemWindowStats{{
			Screen: "FeedActivity", Operation: "feed.open", Owner: "FeedView.onClick",
			Kind: "wrapped_click", Count: 1, MaxMS: 410,
		}},
		RuntimeCalls: []analyze.RuntimeCallStats{{
			Screen: "FeedActivity", Operation: "feed.open", Caller: "FeedPresenter.render",
			Callee: "FeedAdapter.bind", Count: 12, TotalMS: 144, MaxMS: 24,
		}, {
			Screen: "FeedActivity", Operation: "feed.open", Caller: "FeedAdapter.bind",
			Callee: "FeedItemView.onDraw", Count: 12, TotalMS: 144, MaxMS: 24,
		}},
		LogSpam: []analyze.LogSpamStats{{
			Screen: "FeedActivity", Operation: "feed.open", Owner: "FeedPresenter.render",
			Source: "android.util.Log.d", Count: 100,
		}},
	})
	if len(insights) != 1 {
		t.Fatalf("uiScreenInsights() count = %d, want 1", len(insights))
	}
	causes := insights[0].Causes
	if len(causes) != 6 {
		t.Fatalf("some UI problem candidates are hidden: got %d, want 6: %+v", len(causes), causes)
	}
	if causes[0].Title != "SQL-вызов выполнялся на главном потоке: FeedStore.load" || causes[0].Relation != "пересечение интервалов подтверждено" {
		t.Fatalf("first cause = %+v", causes[0])
	}
	if causes[1].Relation != "пауза подтверждена" || !strings.Contains(causes[1].Evidence, "FeedView.onDraw") {
		t.Fatalf("stall cause = %+v", causes[1])
	}
	if causes[2].Title != "Обработчик нажатия выполнялся слишком долго" {
		t.Fatalf("click cause = %+v", causes[2])
	}
	if causes[3].Relation != "возможная причина в коде" || causes[3].Title != "Отрисовка пользовательского View выполнялась дольше бюджета кадра" || !strings.Contains(causes[3].Explanation, "не хранит признак главного потока") {
		t.Fatalf("runtime cause = %+v", causes[3])
	}
	if !strings.Contains(causes[3].Where, "FeedPresenter.render → FeedAdapter.bind → FeedItemView.onDraw") {
		t.Fatalf("runtime chain was not collapsed: %+v", causes[3])
	}
	if !strings.Contains(insights[0].Diagnosis, causes[0].Title) || insights[0].Action != causes[0].Action {
		t.Fatalf("diagnosis did not lead to the top cause: %+v", insights[0])
	}
}

func TestUIScreenInsightsRoundFrameDeadlineUpForMillisecondMetrics(t *testing.T) {
	insights := uiScreenInsights(analyze.Summary{
		Screens: []analyze.ScreenStats{{
			Screen: "Feed", Frames: 120, JankyFrames: 12, JankRatePct: 10,
			FrameDeadlineUS: 16_667,
		}},
		RuntimeCalls: []analyze.RuntimeCallStats{{
			Screen: "Feed", Caller: "jankhunter.semantic.v1.compose.draw.main", Callee: "FeedScreen",
			Count: 1, TotalMS: 16, MaxMS: 16,
		}, {
			Screen: "Feed", Caller: "FeedScreen.render", Callee: "FeedRow.bind",
			Count: 1, TotalMS: 16, MaxMS: 16,
		}},
	})

	if len(insights) != 1 {
		t.Fatalf("uiScreenInsights() count = %d, want 1", len(insights))
	}
	if len(insights[0].Causes) != 0 {
		t.Fatalf("sub-deadline millisecond metrics were presented as causes: %+v", insights[0].Causes)
	}
}

func TestUIScreenInsightsPutProblematicScreensFirst(t *testing.T) {
	insights := uiScreenInsights(analyze.Summary{Screens: []analyze.ScreenStats{
		{Screen: "Healthy", Frames: 180, JankyFrames: 0, JankRatePct: 0},
		{Screen: "Critical", Frames: 180, JankyFrames: 40, JankRatePct: 22.2},
		{Screen: "Warning", Frames: 180, JankyFrames: 12, JankRatePct: 6.7},
	}})
	if len(insights) != 3 || insights[0].Screen != "Critical" || insights[1].Screen != "Warning" || insights[2].Screen != "Healthy" {
		t.Fatalf("UI screens are not ordered problem-first: %+v", insights)
	}
	if got := uiProblemCount(insights); got != 2 {
		t.Fatalf("uiProblemCount() = %d, want 2", got)
	}
}

func TestUIScreenInsightsKeepDistinctMainThreadStallStacks(t *testing.T) {
	insights := uiScreenInsights(analyze.Summary{
		Screens: []analyze.ScreenStats{{
			Screen: "MainActivity", Frames: 1_720, JankyFrames: 39, JankRatePct: 2.3,
		}},
		SignalContexts: []analyze.SignalContextStats{
			{Screen: "MainActivity", Owner: "ru.mail.im.app.di.components.ComponentFactoryImpl", StallCount: 1, StallMaxMS: 1_364},
			{Screen: "MainActivity", Owner: "androidx.constraintlayout.core.ArrayLinkedVariables", StallCount: 1, StallMaxMS: 1_119},
		},
		Owners: []analyze.OwnerStats{
			{
				Owner: "ru.mail.im.app.di.components.ComponentFactoryImpl", Kind: "main_thread_stall", Count: 1, MaxMS: 1_364,
				StackHint: "ru.mail.im.app.di.components.ComponentFactoryImpl.create(ComponentFactoryImpl.kt:483)",
			},
			{
				Owner: "androidx.constraintlayout.core.ArrayLinkedVariables", Kind: "main_thread_stall", Count: 1, MaxMS: 1_119,
				StackHint: "androidx.constraintlayout.core.ArrayLinkedVariables.add(ArrayLinkedVariables.java:263)",
			},
		},
	})
	if len(insights) != 1 || len(insights[0].Causes) != 2 {
		t.Fatalf("distinct stall candidates were collapsed: %+v", insights)
	}
	joined := insights[0].Causes[0].Evidence + "\n" + insights[0].Causes[1].Evidence
	for _, stack := range []string{"ComponentFactoryImpl.create", "ArrayLinkedVariables.add"} {
		if !strings.Contains(joined, stack) {
			t.Fatalf("stall stack %q is missing: %+v", stack, insights[0].Causes)
		}
	}
}

func TestMainThreadStallCandidatesStayBoundedAndKeepLargestPauses(t *testing.T) {
	contexts := make([]analyze.SignalContextStats, 6)
	for index := range contexts {
		contexts[index] = analyze.SignalContextStats{
			Screen: "MainActivity", Owner: fmt.Sprintf("Owner%d", index),
			StallCount: 1, StallMaxMS: uint64((index + 1) * 100),
		}
	}
	causes := mainThreadStallCauses(analyze.Summary{SignalContexts: contexts}, "MainActivity")
	if len(causes) != mainThreadStallCauseLimit {
		t.Fatalf("stall candidates = %d, want %d: %+v", len(causes), mainThreadStallCauseLimit, causes)
	}
	joined := causes[0].Evidence + causes[1].Evidence + causes[2].Evidence + causes[3].Evidence
	for _, retained := range []string{"600 мс", "500 мс", "400 мс", "300 мс"} {
		if !strings.Contains(joined, retained) {
			t.Fatalf("top stall %q was not retained: %+v", retained, causes)
		}
	}
}

func TestMainThreadStallCausesPreserveAllSamplesForLateTopGroup(t *testing.T) {
	summary := analyze.Summary{SignalContexts: []analyze.SignalContextStats{
		{Screen: "Feed", Owner: "LateTop", Operation: "load", StallCount: 2, StallMaxMS: 1},
		{Screen: "Feed", Owner: "A", Operation: "load", StallCount: 1, StallMaxMS: 50},
		{Screen: "Feed", Owner: "B", Operation: "load", StallCount: 1, StallMaxMS: 40},
		{Screen: "Feed", Owner: "C", Operation: "load", StallCount: 1, StallMaxMS: 30},
		{Screen: "Feed", Owner: "D", Operation: "load", StallCount: 1, StallMaxMS: 20},
		{Screen: "Feed", Owner: "LateTop", Operation: "load", StallCount: 3, StallMaxMS: 60},
	}}

	causes, total := mainThreadStallCausesWithTotal(summary, "Feed")

	if total != 6 {
		t.Fatalf("total = %d, want 6", total)
	}
	if len(causes) != mainThreadStallCauseLimit {
		t.Fatalf("visible causes = %d, want %d", len(causes), mainThreadStallCauseLimit)
	}
	if got := causes[0].Evidence; !strings.Contains(got, "5 раз") {
		t.Fatalf("late top group lost earlier samples: %q", got)
	}
}

func TestUIScreenInsightsDiscloseOmittedMainThreadStallSignals(t *testing.T) {
	contexts := make([]analyze.SignalContextStats, 6)
	for index := range contexts {
		contexts[index] = analyze.SignalContextStats{
			Screen: "MainActivity", Owner: fmt.Sprintf("Owner%d", index),
			StallCount: 1, StallMaxMS: uint64((index + 1) * 100),
		}
	}
	insights := uiScreenInsights(analyze.Summary{
		Screens:        []analyze.ScreenStats{{Screen: "MainActivity", Frames: 120, JankyFrames: 12}},
		SignalContexts: contexts,
	})

	if len(insights) != 1 || insights[0].CauseTotal != 6 || insights[0].OmittedCauses != 2 {
		t.Fatalf("stall signal totals = %+v, want total=6 omitted=2", insights)
	}
}

func TestUIScreenInsightsDoNotPresentNetworkCoincidenceAsJankCause(t *testing.T) {
	insights := uiScreenInsights(analyze.Summary{
		Screens: []analyze.ScreenStats{{Screen: "RegistrationActivity", Frames: 180, JankyFrames: 30, JankRatePct: 16.7}},
		SignalContexts: []analyze.SignalContextStats{{
			Screen: "RegistrationActivity", Operation: "registration", Owner: "RegistrationController.load",
			RouteSample: "GET /config", HTTPCount: 2, HTTPP95MS: 1_500,
		}},
	})
	if len(insights) != 1 || len(insights[0].Causes) != 1 {
		t.Fatalf("unexpected insights: %+v", insights)
	}
	cause := insights[0].Causes[0]
	if cause.Relation != "совпало в операции" {
		t.Fatalf("network relation = %q", cause.Relation)
	}
	for _, phrase := range []string{"не показывает, ожидал ли его главный поток", "не доказывает причину подтормаживаний"} {
		if !strings.Contains(cause.Explanation, phrase) {
			t.Fatalf("network explanation misses %q: %q", phrase, cause.Explanation)
		}
	}
}

func TestUIScreenInsightsExplainLinkedMainThreadSQLBeforeWeakerCandidates(t *testing.T) {
	insights := uiScreenInsights(analyze.Summary{
		Screens: []analyze.ScreenStats{{
			Screen: "Feed", Frames: 180, JankyFrames: 30, JankRatePct: 16.7,
			FrameDeadlineUS: 16_667,
		}},
		DatabaseAnalysis: &analyze.DatabaseAnalysis{Statements: []analyze.DatabaseStatementStats{{
			Query: "SELECT item FROM feed WHERE id = ?", Operation: "чтение",
			Overall: analyze.DatabaseExecutionStats{Calls: 3, MaxDurationUS: 48_000},
			Main:    analyze.DatabaseExecutionStats{Calls: 3, MaxDurationUS: 48_000},
			MainCorrelation: analyze.DatabaseCorrelationStats{
				UIWindowOverlaps: 2, UIFrames: 120, UIJankyFrames: 18, UIOverlapMaxDurationUS: 48_000,
			},
			Contexts: []analyze.DatabaseStatementContextStats{{
				Source: "FeedDao.load", Screen: "Feed", ContextOperation: "feed.open",
				Main: analyze.DatabaseExecutionStats{Calls: 3, MaxDurationUS: 48_000},
				MainCorrelation: analyze.DatabaseCorrelationStats{
					UIWindowOverlaps: 2, UIFrames: 120, UIJankyFrames: 18, UIOverlapMaxDurationUS: 48_000,
				},
			}},
		}}},
	})
	if len(insights) != 1 || len(insights[0].Causes) != 1 {
		t.Fatalf("UI database causes = %+v", insights)
	}
	cause := insights[0].Causes[0]
	if cause.Relation != "пересечение интервалов подтверждено" || cause.RelationClass != "strong" ||
		!strings.Contains(cause.Title, "FeedDao.load") ||
		!strings.Contains(cause.Evidence, "2") || !strings.Contains(cause.Evidence, "48 мс") {
		t.Fatalf("linked main-thread SQL cause = %+v", cause)
	}
}

func TestUIScreenInsightsDescribeBackgroundSQLOverlapAsCorrelationNotCause(t *testing.T) {
	insights := uiScreenInsights(analyze.Summary{
		Screens: []analyze.ScreenStats{{Screen: "Feed", Frames: 180, JankyFrames: 30, JankRatePct: 16.7}},
		DatabaseAnalysis: &analyze.DatabaseAnalysis{Statements: []analyze.DatabaseStatementStats{{
			Query: "SELECT item FROM feed", Operation: "чтение",
			Overall:    analyze.DatabaseExecutionStats{Calls: 2, MaxDurationUS: 180_000},
			Background: analyze.DatabaseExecutionStats{Calls: 2, MaxDurationUS: 180_000},
			Contexts: []analyze.DatabaseStatementContextStats{{
				Source: "FeedDao.prefetch", Screen: "Feed", ContextOperation: "feed.open",
				Background:            analyze.DatabaseExecutionStats{Calls: 2, MaxDurationUS: 180_000},
				BackgroundCorrelation: analyze.DatabaseCorrelationStats{UIWindowOverlaps: 1, UIOverlapMaxDurationUS: 180_000},
			}},
		}}},
	})
	if len(insights) != 1 || len(insights[0].Causes) != 1 {
		t.Fatalf("background database causes = %+v", insights)
	}
	cause := insights[0].Causes[0]
	if cause.Relation != "совпало по времени" || cause.RelationClass == "strong" ||
		!strings.Contains(cause.Explanation, "не доказывает") {
		t.Fatalf("background SQL relation overclaims causality: %+v", cause)
	}
}

func TestUIScreenInsightsDoNotPromoteShortOverlappingSQLToCause(t *testing.T) {
	insights := uiScreenInsights(analyze.Summary{
		Screens: []analyze.ScreenStats{{Screen: "Feed", Frames: 180, JankyFrames: 30, JankRatePct: 16.7}},
		DatabaseAnalysis: &analyze.DatabaseAnalysis{Statements: []analyze.DatabaseStatementStats{{
			Query: "SELECT 1", Main: analyze.DatabaseExecutionStats{Calls: 1, MaxDurationUS: 1_000},
			Contexts: []analyze.DatabaseStatementContextStats{{
				Source: "FeedDao.ping", Screen: "Feed", Main: analyze.DatabaseExecutionStats{Calls: 1, MaxDurationUS: 1_000},
				MainCorrelation: analyze.DatabaseCorrelationStats{UIWindowOverlaps: 1, UIOverlapMaxDurationUS: 1_000},
			}},
		}}},
	})
	if len(insights) != 1 || len(insights[0].Causes) != 0 {
		t.Fatalf("short SQL was promoted to a jank cause: %+v", insights)
	}
}

func TestUIScreenInsightsDoNotJoinUnknownContextToConcreteScreen(t *testing.T) {
	insights := uiScreenInsights(analyze.Summary{
		Screens: []analyze.ScreenStats{{Screen: "FeedActivity", Frames: 120, JankyFrames: 12, JankRatePct: 10}},
		IOAnalysis: &analyze.IOAnalysis{Operations: 1, MainThreadOperations: 1, Calls: []analyze.IOStats{{
			Operation: "file_read", MainThread: true, Count: 1, TotalDurationUS: 50_000,
			MaxDurationUS: 50_000, Screen: "unknown", Owner: "Cache.load",
		}}},
	})
	if len(insights) != 1 || len(insights[0].Causes) != 0 {
		t.Fatalf("unknown context was joined to concrete screen: %+v", insights)
	}
}

func TestUIScreenInsightsDoNotSuggestCausesWithoutJankyFrames(t *testing.T) {
	insights := uiScreenInsights(analyze.Summary{
		Screens: []analyze.ScreenStats{{Screen: "FeedActivity", Frames: 120, JankyFrames: 0, JankRatePct: 0}},
		RuntimeCalls: []analyze.RuntimeCallStats{{
			Screen: "FeedActivity", Caller: "FeedPresenter.render", Callee: "FeedItemView.onDraw",
			Count: 10, TotalMS: 500, MaxMS: 50,
		}},
	})
	if len(insights) != 1 || len(insights[0].Causes) != 0 {
		t.Fatalf("causes were suggested without a janky frame: %+v", insights)
	}
}

func TestUIScreenInsightsUseLongFrameTailWhenSystemJankFlagIsZero(t *testing.T) {
	insights := uiScreenInsights(analyze.Summary{
		Screens: []analyze.ScreenStats{{
			Screen: "CustomViewLab", Frames: 214, JankyFrames: 0, JankRatePct: 0,
			FrameP95MS: 100, FrameP99MS: 250, FrameDeadlineUS: 32_000,
		}},
		SignalContexts: []analyze.SignalContextStats{{
			Screen: "CustomViewLab", Operation: "custom_view.heavy_on_draw",
			Owner: "sample.CustomView.onDraw", StallCount: 5, StallMaxMS: 247,
		}},
	})
	if len(insights) != 1 || len(insights[0].Causes) == 0 {
		t.Fatalf("long frame tail did not enable cause analysis: %+v", insights)
	}
	if insights[0].Status != "длинные кадры" || !strings.Contains(insights[0].Observation, "0% здесь не означает норму") {
		t.Fatalf("tail-only observation is misleading: %+v", insights[0])
	}
	if !strings.Contains(insights[0].Causes[0].Title, "onDraw") || !strings.Contains(insights[0].Causes[0].Action, "Path/Bitmap/Shader") {
		t.Fatalf("custom View cause is not actionable: %+v", insights[0].Causes[0])
	}
}

func TestUIScreenInsightsExplainComposeAndRoomMainThreadWork(t *testing.T) {
	insights := uiScreenInsights(analyze.Summary{
		Screens: []analyze.ScreenStats{{
			Screen: "ComposeFeed", Frames: 180, JankyFrames: 24, JankRatePct: 13.3,
			FrameDeadlineUS: 16_667,
		}},
		RuntimeCalls: []analyze.RuntimeCallStats{{
			Screen: "ComposeFeed", Caller: "jankhunter.semantic.v1.compose.draw.main",
			Callee: "com.app.FeedCanvas", Count: 30, TotalMS: 260, MaxMS: 42,
		}, {
			Screen: "ComposeFeed", Caller: "jankhunter.semantic.v1.room.dao.main",
			Callee: "com.app.FeedDao_Impl.load", Count: 2, TotalMS: 64, MaxMS: 38,
		}},
	})
	if len(insights) != 1 || len(insights[0].Causes) != 2 {
		t.Fatalf("unexpected semantic causes: %+v", insights)
	}
	if !strings.Contains(insights[0].Causes[0].Title, "Отрисовка Compose") || insights[0].Causes[0].Relation != "главный поток измерен" {
		t.Fatalf("compose cause = %+v", insights[0].Causes[0])
	}
	if !strings.Contains(insights[0].Causes[1].Title, "Метод доступа к данным Room") || !strings.Contains(insights[0].Causes[1].Explanation, "на главном потоке") {
		t.Fatalf("room cause = %+v", insights[0].Causes[1])
	}
}

func TestRuntimeCallCausesBoundMaterializationToVisibleCandidates(t *testing.T) {
	const callCount = 4_096
	calls := make([]analyze.RuntimeCallStats, callCount)
	for index := range calls {
		calls[index] = analyze.RuntimeCallStats{
			Screen:    "FeedActivity",
			Operation: fmt.Sprintf("feed.operation.%04d", index),
			Caller:    fmt.Sprintf("com.app.FeedCaller%04d.run", index),
			Callee:    fmt.Sprintf("com.app.FeedTarget%04d.render", index),
			Count:     1,
			TotalMS:   uint64(index + 17),
			MaxMS:     uint64(index + 17),
		}
	}
	summary := analyze.Summary{RuntimeCalls: calls}

	causes := runtimeCallCauses(summary, "FeedActivity", 16)
	if len(causes) != 2 {
		t.Fatalf("runtime call causes = %d, want 2", len(causes))
	}
	for _, endpoint := range []string{"FeedTarget4095.render", "FeedTarget4094.render"} {
		if !strings.Contains(causes[0].Title+causes[1].Title, endpoint) {
			t.Fatalf("top runtime endpoint %q is missing: %+v", endpoint, causes)
		}
	}
	if raceDetectorEnabled {
		t.Skip("race instrumentation changes allocation accounting")
	}

	runtimeCallCauses(summary, "FeedActivity", 16)
	allocations := testing.AllocsPerRun(10, func() {
		runtimeCallCauses(summary, "FeedActivity", 16)
	})
	if allocations > 64 {
		t.Fatalf("bounded runtime causes allocate %.2f objects, want at most 64", allocations)
	}
	benchmark := testing.Benchmark(func(b *testing.B) {
		for range b.N {
			runtimeCallCauseAllocationSink = runtimeCallCauses(summary, "FeedActivity", 16)
		}
	})
	if bytes := benchmark.AllocedBytesPerOp(); bytes > 512*1024 {
		t.Fatalf("bounded runtime causes allocate %d bytes/op, want at most 512 KiB", bytes)
	}
}

func TestRuntimeCallCausesRemainBoundedWhenDurationsTie(t *testing.T) {
	const callCount = 4_096
	calls := make([]analyze.RuntimeCallStats, callCount)
	for index := range calls {
		calls[index] = analyze.RuntimeCallStats{
			Screen:    "FeedActivity",
			Operation: fmt.Sprintf("feed.operation.%04d", index),
			Caller:    fmt.Sprintf("com.app.FeedCaller%04d.run", index),
			Callee:    fmt.Sprintf("com.app.FeedTarget%04d.render", index),
			Count:     1,
			TotalMS:   100,
			MaxMS:     100,
		}
	}
	summary := analyze.Summary{RuntimeCalls: calls}

	if causes := runtimeCallCauses(summary, "FeedActivity", 16); len(causes) != 2 {
		t.Fatalf("runtime call causes = %d, want 2", len(causes))
	}
	if raceDetectorEnabled {
		t.Skip("race instrumentation changes allocation accounting")
	}
	allocations := testing.AllocsPerRun(10, func() {
		runtimeCallCauses(summary, "FeedActivity", 16)
	})
	if allocations > 64 {
		t.Fatalf("equal-duration runtime causes allocate %.2f objects, want at most 64", allocations)
	}
}

func TestRuntimeCallCausesReuseTraversalBuffersForDisconnectedTiedCalls(t *testing.T) {
	const callCount = 4_096
	calls := make([]analyze.RuntimeCallStats, callCount)
	for index := range calls {
		calls[index] = analyze.RuntimeCallStats{
			Screen: "FeedActivity", Operation: "feed.operation",
			Caller: fmt.Sprintf("com.app.FeedCaller%04d.run", index),
			Callee: fmt.Sprintf("com.app.FeedTarget%04d.render", index),
			Count:  1, TotalMS: 100, MaxMS: 100,
		}
	}
	summary := analyze.Summary{RuntimeCalls: calls}

	if causes := runtimeCallCauses(summary, "FeedActivity", 16); len(causes) != 2 {
		t.Fatalf("runtime call causes = %d, want 2", len(causes))
	}
	if raceDetectorEnabled {
		t.Skip("race instrumentation changes allocation accounting")
	}
	allocations := testing.AllocsPerRun(10, func() {
		runtimeCallCauses(summary, "FeedActivity", 16)
	})
	if allocations > 128 {
		t.Fatalf("disconnected tied runtime causes allocate %.2f objects, want at most 128", allocations)
	}
}

func TestUIScreenInsightsDiscloseOmittedRuntimeCallSignals(t *testing.T) {
	calls := make([]analyze.RuntimeCallStats, 10)
	for index := range calls {
		calls[index] = analyze.RuntimeCallStats{
			Screen: "FeedActivity", Caller: fmt.Sprintf("Caller%d.run", index),
			Callee: fmt.Sprintf("Target%d.render", index), Count: 1,
			TotalMS: uint64(index + 20), MaxMS: uint64(index + 20),
		}
	}
	insights := uiScreenInsights(analyze.Summary{
		Screens:      []analyze.ScreenStats{{Screen: "FeedActivity", Frames: 120, JankyFrames: 12}},
		RuntimeCalls: calls,
	})

	if len(insights) != 1 || insights[0].CauseTotal != 10 || insights[0].OmittedCauses != 8 {
		t.Fatalf("runtime-call signal totals = %+v, want total=10 omitted=8", insights)
	}
}

func TestRuntimeCallCausesDoNotWrapDurationMagnitude(t *testing.T) {
	overflowingMS := uint64(math.MaxUint64/1_000 + 1)
	causes := runtimeCallCauses(analyze.Summary{RuntimeCalls: []analyze.RuntimeCallStats{
		{
			Screen: "FeedActivity", Caller: "com.app.Slow.run", Callee: "com.app.VerySlow.render",
			Count: 1, TotalMS: overflowingMS, MaxMS: overflowingMS,
		},
		{
			Screen: "FeedActivity", Caller: "com.app.Fast.run", Callee: "com.app.LessSlow.render",
			Count: 1, TotalMS: 1_000, MaxMS: 1_000,
		},
	}}, "FeedActivity", 16)

	if len(causes) != 2 || !strings.Contains(causes[0].Title, "VerySlow.render") {
		t.Fatalf("wrapped duration changed runtime cause order: %+v", causes)
	}
}

func TestUIRelatedSignalsSaturateIntegerCounters(t *testing.T) {
	maximum := int(^uint(0) >> 1)
	nearby, _, _ := uiRelatedSignals(analyze.Summary{SignalContexts: []analyze.SignalContextStats{
		{Screen: "Feed", HTTPCount: maximum, HTTPFailed: maximum, StallCount: maximum},
		{Screen: "Feed", HTTPCount: 1, HTTPFailed: 1, StallCount: 1},
	}}, "Feed")
	if !strings.Contains(nearby, fmt.Sprint(maximum)) || strings.Contains(nearby, "-1") {
		t.Fatalf("UI related signal counters wrapped: %q", nearby)
	}
}

func TestUIScreenInsightsBoundVisibleCauseCardsAndReportOmissions(t *testing.T) {
	const causeCount = 100
	windows := make([]analyze.ProblemWindowStats, causeCount)
	for index := range windows {
		windows[index] = analyze.ProblemWindowStats{
			Screen: "FeedActivity", Kind: "wrapped_click", Owner: fmt.Sprintf("com.app.Click%03d.run", index),
			Count: 1, MaxMS: uint64(index + 20),
		}
	}
	insights := uiScreenInsights(analyze.Summary{
		Screens:        []analyze.ScreenStats{{Screen: "FeedActivity", Frames: 180, JankyFrames: 30, JankRatePct: 16.7}},
		ProblemWindows: windows,
	})

	if len(insights) != 1 || len(insights[0].Causes) != 12 {
		t.Fatalf("visible UI causes = %+v, want 12 bounded cards", insights)
	}
	if insights[0].CauseTotal != causeCount || insights[0].OmittedCauses != causeCount-12 {
		t.Fatalf("UI cause totals = %d/%d, want %d/%d", insights[0].CauseTotal, insights[0].OmittedCauses, causeCount, causeCount-12)
	}
	if !strings.Contains(insights[0].Causes[0].Where, "Click099.run") {
		t.Fatalf("strongest UI cause was not retained: %+v", insights[0].Causes[0])
	}
	if raceDetectorEnabled {
		t.Skip("race instrumentation changes allocation accounting")
	}
	allocations := testing.AllocsPerRun(10, func() {
		uiScreenInsights(analyze.Summary{
			Screens:        []analyze.ScreenStats{{Screen: "FeedActivity", Frames: 180, JankyFrames: 30, JankRatePct: 16.7}},
			ProblemWindows: windows,
		})
	})
	if allocations > 112 {
		t.Fatalf("bounded UI cause cards allocate %.2f objects, want at most 112", allocations)
	}
}

func TestUIScreenInsightsBoundHighCardinalitySourceMaterialization(t *testing.T) {
	const sourceCount = 1_024
	ioCalls := make([]analyze.IOStats, sourceCount)
	contexts := make([]analyze.SignalContextStats, sourceCount)
	logSpam := make([]analyze.LogSpamStats, sourceCount)
	for index := 0; index < sourceCount; index++ {
		owner := fmt.Sprintf("com.app.Owner%04d.run", index)
		ioCalls[index] = analyze.IOStats{
			Screen: "FeedActivity", Owner: owner, Operation: "file_read", MainThread: true,
			Count: 1, MaxDurationUS: uint64(100_000 + index), TotalDurationUS: uint64(100_000 + index),
		}
		contexts[index] = analyze.SignalContextStats{
			Screen: "FeedActivity", Owner: owner, RouteSample: fmt.Sprintf("GET /feed/%04d", index),
			HTTPCount: 1, HTTPP95MS: uint64(1_000 + index),
		}
		logSpam[index] = analyze.LogSpamStats{
			Screen: "FeedActivity", Owner: owner, Source: "android.util.Log.d", Count: uint64(100 + index),
		}
	}
	summary := analyze.Summary{
		Screens:        []analyze.ScreenStats{{Screen: "FeedActivity", Frames: 180, JankyFrames: 30, JankRatePct: 16.7}},
		IOAnalysis:     &analyze.IOAnalysis{Calls: ioCalls},
		SignalContexts: contexts,
		LogSpam:        logSpam,
	}

	insights := uiScreenInsights(summary)
	if len(insights) != 1 || len(insights[0].Causes) != uiScreenCauseLimit {
		t.Fatalf("visible UI causes = %+v, want %d bounded cards", insights, uiScreenCauseLimit)
	}
	wantTotal := sourceCount * 3
	if insights[0].CauseTotal != wantTotal || insights[0].OmittedCauses != wantTotal-uiScreenCauseLimit {
		t.Fatalf("UI cause totals = %d/%d, want %d/%d", insights[0].CauseTotal, insights[0].OmittedCauses, wantTotal, wantTotal-uiScreenCauseLimit)
	}
	if raceDetectorEnabled {
		t.Skip("race instrumentation changes allocation accounting")
	}
	allocations := testing.AllocsPerRun(5, func() {
		uiScreenInsights(summary)
	})
	if allocations > 512 {
		t.Fatalf("high-cardinality UI sources allocate %.2f objects, want at most 512", allocations)
	}
}

func TestDatabaseUICausesBoundHighCardinalityMaterialization(t *testing.T) {
	const contextCount = 1_024
	contexts := make([]analyze.DatabaseStatementContextStats, contextCount)
	for index := range contexts {
		contexts[index] = analyze.DatabaseStatementContextStats{
			Source: fmt.Sprintf("com.app.FeedDao%04d.load", index), Screen: "FeedActivity",
			Main: analyze.DatabaseExecutionStats{Calls: 1, MaxDurationUS: uint64(40_000 + index)},
			MainCorrelation: analyze.DatabaseCorrelationStats{
				UIWindowOverlaps: 1, UIOverlapMaxDurationUS: uint64(40_000 + index),
			},
		}
	}
	analysis := &analyze.DatabaseAnalysis{Statements: []analyze.DatabaseStatementStats{{
		Query: "SELECT item FROM feed WHERE id = ?", Contexts: contexts,
	}}}

	causes := databaseUICauses(analysis, "FeedActivity")
	if len(causes) != databaseUICauseLimit {
		t.Fatalf("database UI causes = %d, want %d", len(causes), databaseUICauseLimit)
	}
	insights := uiScreenInsights(analyze.Summary{
		Screens:          []analyze.ScreenStats{{Screen: "FeedActivity", Frames: 180, JankyFrames: 30, JankRatePct: 16.7}},
		DatabaseAnalysis: analysis,
	})
	if len(insights) != 1 || len(insights[0].Causes) != databaseUICauseLimit ||
		insights[0].CauseTotal != contextCount || insights[0].OmittedCauses != contextCount-databaseUICauseLimit {
		t.Fatalf("database UI insight did not preserve bounded/total counts: %+v", insights)
	}
	if raceDetectorEnabled {
		t.Skip("race instrumentation changes allocation accounting")
	}
	allocations := testing.AllocsPerRun(5, func() {
		databaseUICauses(analysis, "FeedActivity")
	})
	if allocations > 160 {
		t.Fatalf("high-cardinality database UI causes allocate %.2f objects, want at most 160", allocations)
	}
}

func TestMainThreadIOCauseThresholdDoesNotWrapDeadline(t *testing.T) {
	deadlineUS := uint64(math.MaxUint64/2 + 1)
	causes := mainThreadIOCauses(analyze.Summary{IOAnalysis: &analyze.IOAnalysis{Calls: []analyze.IOStats{{
		Screen: "FeedActivity", Operation: "file_read", MainThread: true,
		MaxDurationUS: deadlineUS - 1, TotalDurationUS: deadlineUS,
	}}}}, "FeedActivity", deadlineUS)
	if len(causes) != 0 {
		t.Fatalf("sub-threshold I/O was included after deadline overflow: %+v", causes)
	}
}

func TestUIFrameDeadlineMillisecondsDoesNotWrap(t *testing.T) {
	want := uint64(math.MaxUint64/1_000 + 1)
	if got := uiFrameDeadlineMS(analyze.ScreenStats{FrameDeadlineUS: math.MaxUint64}); got != want {
		t.Fatalf("uiFrameDeadlineMS(MaxUint64) = %d, want %d", got, want)
	}
}

func TestUIHasSlowFrameTailDoesNotWrapDeadline(t *testing.T) {
	screen := analyze.ScreenStats{
		FrameDeadlineStatus: "consistent", FrameDeadlineUS: math.MaxUint64,
		FrameP95MS: 1, FrameP99MS: 1,
	}
	if uiHasSlowFrameTail(screen) {
		t.Fatal("tiny frame tail was classified as slow after deadline overflow")
	}
}

func BenchmarkConnectedRuntimeCallComponentsStar(b *testing.B) {
	const edgeCount = 4_096
	calls := make([]analyze.RuntimeCallStats, edgeCount)
	for index := range calls {
		calls[index] = analyze.RuntimeCallStats{
			Caller: "com.app.Root.dispatch",
			Callee: fmt.Sprintf("com.app.Leaf%04d.run", index),
		}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		components := selectConnectedRuntimeCallComponents(calls, 2)
		if len(components) != 1 || len(components[0]) != edgeCount {
			b.Fatalf("components = %d/%d, want 1/%d", len(components), len(components[0]), edgeCount)
		}
	}
}
