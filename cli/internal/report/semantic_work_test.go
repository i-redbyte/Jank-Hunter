package report

import (
	"fmt"
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

	compose := semanticWorkOverviewFromItems(semanticWorkForDomain(summary, analyze.SemanticDomainCompose), analyze.SemanticDomainCompose)
	room := semanticWorkOverviewFromItems(semanticWorkForDomain(summary, analyze.SemanticDomainRoom), analyze.SemanticDomainRoom)
	worker := semanticWorkOverviewFromItems(semanticWorkForDomain(summary, analyze.SemanticDomainWorker), analyze.SemanticDomainWorker)
	if compose == nil || room == nil || worker == nil {
		t.Fatalf("domain overviews: compose=%+v room=%+v worker=%+v", compose, room, worker)
	}
	if compose.Title != "Интерфейс на Jetpack Compose" || compose.Suspicious != 1 {
		t.Fatalf("compose overview = %+v", compose)
	}
	composeRows := semanticWorkRowsForDomain(summary, analyze.SemanticDomainCompose)
	roomRows := roomWorkRows(summary)
	if len(composeRows) != 1 || len(roomRows) != 1 ||
		!strings.Contains(composeRows[0].StatusHelp, "главном потоке") && !strings.Contains(composeRows[0].StatusHelp, "бюджета кадра") {
		t.Fatalf("domain rows: compose=%+v room=%+v", composeRows, roomRows)
	}
	if !composeRows[0].NeedsAttention || composeRows[0].Action == "" {
		t.Fatalf("actionable Compose row is not presented as a problem: %+v", composeRows[0])
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
			Screen: "Feed", Caller: "jankhunter.semantic.v1.compose.composition.main",
			Callee: "com.app.FeedHeader", Count: 2, TotalMS: 8, MaxMS: 4,
		}, {
			Screen: "Feed", Caller: "jankhunter.semantic.v1.room.dao.main",
			Callee: "com.app.FeedDao_Impl.load", Count: 2, TotalMS: 64, MaxMS: 38,
		}, {
			Caller: "jankhunter.semantic.v1.worker.retry.background",
			Callee: "com.app.SyncWorker.doWork", Count: 1, TotalMS: 4_000, MaxMS: 4_000,
		}},
		DatabaseAnalysis: &analyze.DatabaseAnalysis{
			KnownSQLCalls:      3,
			Overall:            analyze.DatabaseExecutionStats{Calls: 3, P50DurationUS: 20_000, P95DurationUS: 38_000, MaxDurationUS: 40_000},
			Main:               analyze.DatabaseExecutionStats{Calls: 2, P95DurationUS: 38_000, MaxDurationUS: 40_000},
			Background:         analyze.DatabaseExecutionStats{Calls: 1, P95DurationUS: 20_000, MaxDurationUS: 20_000},
			PeakCallsPerSecond: 3,
			Statements: []analyze.DatabaseStatementStats{{
				Query: "SELECT * FROM feed WHERE account_id = ?", Operation: "чтение",
				Overall:    analyze.DatabaseExecutionStats{Calls: 3, P50DurationUS: 20_000, P95DurationUS: 38_000, MaxDurationUS: 40_000},
				Main:       analyze.DatabaseExecutionStats{Calls: 2, P95DurationUS: 38_000, MaxDurationUS: 40_000},
				Background: analyze.DatabaseExecutionStats{Calls: 1, P95DurationUS: 20_000, MaxDurationUS: 20_000},
				Contexts: []analyze.DatabaseStatementContextStats{{
					Source: "com.app.FeedDao_Impl.load", Framework: "Room", Screen: "Feed",
				}},
			}},
		},
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
		"Интерфейс на Jetpack Compose", "Проблемные элементы Compose",
		"Остальные измеренные функции и фазы Compose",
		"База данных: SQLite и Room", "Сначала проверьте проблемные SQL",
		"Метод доступа к данным Room выполнялся на главном потоке", "SELECT * FROM feed WHERE account_id = ?",
		"превышен бюджет кадра", "Что исправить", "FeedDao_Impl.load",
		"Полное место в коде: com.app.FeedCanvas.draw",
	} {
		if !strings.Contains(html, expected) {
			t.Fatalf("semantic report misses %q", expected)
		}
	}
	if strings.Contains(html, "jankhunter.semantic.v1") {
		t.Fatal("technical semantic root leaked into the human-readable report")
	}
	if strings.Contains(html, "Compose и Room") {
		t.Fatal("Compose and database analysis must not be combined")
	}
}

func TestComposeReportShowsEveryProblemBeforeOrdinaryObservations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "compose-problems.html")
	calls := make([]analyze.RuntimeCallStats, 0, 7)
	for index := 0; index < 6; index++ {
		calls = append(calls, analyze.RuntimeCallStats{
			Caller: "jankhunter.semantic.v1.compose.composition.main",
			Callee: fmt.Sprintf("com.app.Problem%d", index),
			Screen: "Feed", Count: 1, TotalMS: 20, MaxMS: 20,
		})
	}
	calls = append(calls, analyze.RuntimeCallStats{
		Caller: "jankhunter.semantic.v1.compose.composition.main",
		Callee: "com.app.Ordinary", Screen: "Feed", Count: 1, TotalMS: 4, MaxMS: 4,
	})
	if err := WriteInspectWithOptions(path, analyze.Summary{RuntimeCalls: calls}, ReportOptions{}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	html := string(data)
	problemStart := strings.Index(html, "Проблемные элементы Compose")
	ordinaryStart := strings.Index(html, "Остальные измеренные функции и фазы Compose")
	if problemStart < 0 || ordinaryStart < 0 || problemStart >= ordinaryStart {
		t.Fatalf("Compose problem-first order is missing: problem=%d ordinary=%d", problemStart, ordinaryStart)
	}
	for index := 0; index < 6; index++ {
		if !strings.Contains(html[problemStart:ordinaryStart], fmt.Sprintf("com.app.Problem%d", index)) {
			t.Fatalf("problem %d is hidden from the immediate Compose list", index)
		}
	}
	if strings.Contains(html[problemStart:ordinaryStart], "com.app.Ordinary") {
		t.Fatal("ordinary Compose observation leaked into the problem list")
	}
}

func TestUnmarkedContextHasPlainRussianExplanation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "unmarked-context.html")
	summary := analyze.Summary{RuntimeCalls: []analyze.RuntimeCallStats{{
		Caller: "jankhunter.semantic.v1.compose.composition.main",
		Callee: "com.app.Unattributed", Count: 1, TotalMS: 20, MaxMS: 20,
	}}}
	if err := WriteInspectWithOptions(path, summary, ReportOptions{}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	html := string(data)
	for _, expected := range []string{
		"Контекст не указан",
		"не было имени экрана, пользовательской операции или владельца участка кода",
		`class="explain"`,
	} {
		if !strings.Contains(html, expected) {
			t.Fatalf("unmarked context explanation misses %q", expected)
		}
	}
}

func TestRoomOnlyReportExplainsMissingTypedSQL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "room-only.html")
	summary := analyze.Summary{RuntimeCalls: []analyze.RuntimeCallStats{{
		Caller: "jankhunter.semantic.v1.room.dao.main", Callee: "com.app.FeedDao_Impl.load",
		Count: 1, TotalMS: 12, MaxMS: 12,
	}}}
	if err := WriteInspectWithOptions(path, summary, ReportOptions{}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	html := string(data)
	for _, expected := range []string{
		"SQL-метрики недоступны",
		"Диагностика охвата не сформирована",
		"Ноль событий не доказывает отсутствие работы с БД",
	} {
		if !strings.Contains(html, expected) {
			t.Fatalf("Room-only report misses %q", expected)
		}
	}
}
