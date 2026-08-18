package report

import (
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
)

func TestUIScreenInsightsRankActionableCauseCandidates(t *testing.T) {
	insights := uiScreenInsights(analyze.Summary{
		Screens: []analyze.ScreenStats{{
			Screen: "FeedActivity", Frames: 240, JankyFrames: 48, JankRatePct: 20,
			FrameDeadlineUS: 16_667,
		}},
		IOOperations: []analyze.IOStats{{
			Operation: "database_read", MainThread: true, Count: 2, TotalDurationUS: 70_000,
			MaxDurationUS: 48_000, Screen: "FeedActivity", Flow: "feed.open", Step: "render",
			Owner: "FeedStore.load",
		}},
		Flows: []analyze.FlowStats{{
			Screen: "FeedActivity", Flow: "feed.open", Step: "render", Owner: "FeedPresenter.render",
			StallCount: 1, StallMaxMS: 820, HTTPCount: 3, HTTPP95MS: 1_200,
			RouteSample: "GET /feed",
		}},
		Owners: []analyze.OwnerStats{{
			Owner: "FeedPresenter.render", Kind: "main_thread_stall", Count: 1, MaxMS: 820,
			StackHint: "com.app.FeedView.onDraw(FeedView.kt:84)",
		}},
		ProblemWindows: []analyze.ProblemWindowStats{{
			Screen: "FeedActivity", Flow: "feed.open", Step: "tap", Owner: "FeedView.onClick",
			Kind: "wrapped_click", Count: 1, MaxMS: 410,
		}},
		RuntimeCalls: []analyze.RuntimeCallStats{{
			Screen: "FeedActivity", Flow: "feed.open", Step: "render", Caller: "FeedPresenter.render",
			Callee: "FeedAdapter.bind", Count: 12, TotalMS: 144, MaxMS: 24,
		}, {
			Screen: "FeedActivity", Flow: "feed.open", Step: "render", Caller: "FeedAdapter.bind",
			Callee: "FeedItemView.onDraw", Count: 12, TotalMS: 144, MaxMS: 24,
		}},
		LogSpam: []analyze.LogSpamStats{{
			Screen: "FeedActivity", Flow: "feed.open", Owner: "FeedPresenter.render",
			Source: "android.util.Log.d", Count: 100,
		}},
	})
	if len(insights) != 1 {
		t.Fatalf("uiScreenInsights() count = %d, want 1", len(insights))
	}
	causes := insights[0].Causes
	if len(causes) != maxUICauseInsights {
		t.Fatalf("cause count = %d, want %d: %+v", len(causes), maxUICauseInsights, causes)
	}
	if causes[0].Title != "Чтение базы данных выполнялось на главном потоке" || causes[0].Relation != "главный поток подтверждён" {
		t.Fatalf("first cause = %+v", causes[0])
	}
	if causes[1].Relation != "пауза подтверждена" || !strings.Contains(causes[1].Evidence, "FeedView.onDraw") {
		t.Fatalf("stall cause = %+v", causes[1])
	}
	if causes[2].Title != "Обработчик нажатия выполнялся слишком долго" {
		t.Fatalf("click cause = %+v", causes[2])
	}
	if causes[3].Relation != "кандидат из кода" || causes[3].Title != "Отрисовка пользовательского View выполнялась дольше бюджета кадра" || !strings.Contains(causes[3].Explanation, "не хранит признак главного потока") {
		t.Fatalf("runtime cause = %+v", causes[3])
	}
	if !strings.Contains(causes[3].Where, "FeedPresenter.render → FeedAdapter.bind → FeedItemView.onDraw") {
		t.Fatalf("runtime chain was not collapsed: %+v", causes[3])
	}
	if !strings.Contains(insights[0].Diagnosis, causes[0].Title) || insights[0].Action != causes[0].Action {
		t.Fatalf("diagnosis did not lead to the top cause: %+v", insights[0])
	}
}

func TestUIScreenInsightsDoNotPresentNetworkCoincidenceAsJankCause(t *testing.T) {
	insights := uiScreenInsights(analyze.Summary{
		Screens: []analyze.ScreenStats{{Screen: "RegistrationActivity", Frames: 180, JankyFrames: 30, JankRatePct: 16.7}},
		Flows: []analyze.FlowStats{{
			Screen: "RegistrationActivity", Flow: "registration", Owner: "RegistrationController.load",
			RouteSample: "GET /config", HTTPCount: 2, HTTPP95MS: 1_500,
		}},
	})
	if len(insights) != 1 || len(insights[0].Causes) != 1 {
		t.Fatalf("unexpected insights: %+v", insights)
	}
	cause := insights[0].Causes[0]
	if cause.Relation != "совпало в сценарии" {
		t.Fatalf("network relation = %q", cause.Relation)
	}
	for _, phrase := range []string{"не показывает, ожидал ли его главный поток", "не доказывает причину подтормаживаний"} {
		if !strings.Contains(cause.Explanation, phrase) {
			t.Fatalf("network explanation misses %q: %q", phrase, cause.Explanation)
		}
	}
}

func TestUIScreenInsightsDoNotJoinUnknownContextToConcreteScreen(t *testing.T) {
	insights := uiScreenInsights(analyze.Summary{
		Screens: []analyze.ScreenStats{{Screen: "FeedActivity", Frames: 120, JankyFrames: 12, JankRatePct: 10}},
		IOOperations: []analyze.IOStats{{
			Operation: "file_read", MainThread: true, Count: 1, TotalDurationUS: 50_000,
			MaxDurationUS: 50_000, Screen: "unknown", Owner: "Cache.load",
		}},
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
		Flows: []analyze.FlowStats{{
			Screen: "CustomViewLab", Flow: "custom_view", Step: "heavy_on_draw",
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
	if !strings.Contains(insights[0].Causes[1].Title, "Room DAO") || !strings.Contains(insights[0].Causes[1].Explanation, "на главном потоке") {
		t.Fatalf("room cause = %+v", insights[0].Causes[1])
	}
}
