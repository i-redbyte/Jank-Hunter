package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
)

func TestSemanticWorkPresentationSeparatesSpecializedAndOrdinaryCalls(t *testing.T) {
	summary := analyze.Summary{RuntimeCalls: []analyze.RuntimeCallStats{{
		Caller: "jankhunter.semantic.v1.compose.composition.main", Callee: "com.app.Feed",
		Screen: "Feed", Count: 120, TotalMS: 400, MaxMS: 28,
	}, {
		Caller: "jankhunter.semantic.v1.room.dao.main", Callee: "com.app.FeedDao.load",
		Screen: "Feed", Count: 3, TotalMS: 55, MaxMS: 30,
	}, {
		Caller: "jankhunter.semantic.v1.worker.success.background", Callee: "com.app.SyncWorker",
		Count: 2, TotalMS: 2_000, MaxMS: 1_200,
	}, {
		Caller: "com.app.FeedPresenter.render", Callee: "com.app.FeedView.bind",
		Screen: "Feed", Count: 2, TotalMS: 20, MaxMS: 10,
	}}}

	overviews := semanticWorkOverviews(summary)
	if len(overviews) != 3 {
		t.Fatalf("overview count = %d, want 3: %+v", len(overviews), overviews)
	}
	if overviews[0].Title != "Jetpack Compose" || overviews[0].Suspicious != 1 {
		t.Fatalf("compose overview = %+v", overviews[0])
	}
	rows := semanticWorkRows(summary)
	if len(rows) != 3 || !strings.Contains(rows[0].StatusHelp, "главном потоке") && !strings.Contains(rows[0].StatusHelp, "бюджета кадра") {
		t.Fatalf("semantic rows = %+v", rows)
	}
	ordinary := ordinaryRuntimeCalls(summary)
	if len(ordinary) != 1 || ordinary[0].Caller != "com.app.FeedPresenter.render" {
		t.Fatalf("ordinary runtime calls = %+v", ordinary)
	}
}

func TestInspectRendersSemanticWorkAsHumanReadableAnalysis(t *testing.T) {
	path := filepath.Join(t.TempDir(), "semantic.html")
	summary := analyze.Summary{
		Title: "semantic.jhlog",
		Screens: []analyze.ScreenStats{{
			Screen: "Feed", Frames: 180, JankyFrames: 20, JankRatePct: 11.1, FrameDeadlineUS: 16_667,
		}},
		RuntimeCalls: []analyze.RuntimeCallStats{{
			Screen: "Feed", Caller: "jankhunter.semantic.v1.compose.draw.main",
			Callee: "com.app.FeedCanvas.draw", Count: 30, TotalMS: 300, MaxMS: 42,
		}, {
			Screen: "Feed", Caller: "jankhunter.semantic.v1.room.dao.main",
			Callee: "com.app.FeedDao_Impl.load", Count: 2, TotalMS: 64, MaxMS: 38,
		}, {
			Caller: "jankhunter.semantic.v1.worker.retry.background",
			Callee: "com.app.SyncWorker.doWork", Count: 1, TotalMS: 4_000, MaxMS: 4_000,
		}},
	}
	problemReport, err := analyze.BuildProblemReport(summary)
	if err != nil {
		t.Fatal(err)
	}
	summary.ProblemSchemaVersion = analyze.ProblemSchemaVersion
	summary.ProblemSummary = problemReport.Summary
	summary.Problems = problemReport.Problems
	summary.CategoryCoverage = problemReport.Coverage
	summary.Detectors = problemReport.Registry
	if err := WriteInspectWithOptions(path, summary, ReportOptions{}); err != nil {
		t.Fatalf("WriteInspectWithOptions() error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	html := string(data)
	for _, expected := range []string{
		"Compose, Room и фоновые задачи", "Отрисовка Compose превысила бюджет кадра",
		"Room DAO выполнялся на главном потоке", "Worker и фоновые задачи",
		"превышен бюджет кадра", "FeedDao_Impl.load", "Полное место в коде: com.app.FeedDao_Impl.load",
	} {
		if !strings.Contains(html, expected) {
			t.Fatalf("semantic report misses %q", expected)
		}
	}
	if strings.Contains(html, "jankhunter.semantic.v1") {
		t.Fatal("technical semantic root leaked into the human-readable report")
	}
}
