package report

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
)

func TestInspectReportRendersUniversalOperationAnalysis(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "operations.html")
	summary := analyze.Summary{
		Title: "operations",
		OperationAnalysis: &analyze.OperationAnalysis{
			Started: 8, Completed: 7, MissingFinish: 1, UnmatchedSignalEvents: 3,
			CompletedContextEvictions: 5,
			Operations: []analyze.OperationStats{{
				Operation: "content.open", Kind: "screen", Screen: "ArticleActivity", Count: 7,
				Failures: 1, P50MS: 180, P95MS: 900, QuantilesApproximated: true, MaxMS: 1200,
				Budgeted: 7, BudgetBreaches: 2, BudgetBreachRatePct: 28.57,
				CorrelatedHTTP: 4, CorrelatedHTTPFailures: 1, CorrelatedHTTPDurationMS: 845,
				CorrelatedWebSocket: 2, CorrelatedWebSocketErrors: 1,
				CorrelatedDatabase: 11, CorrelatedDatabaseErrors: 1, CorrelatedDatabaseMain: 3, CorrelatedDatabaseUS: 72_000,
				WorstDatabaseStatements: []analyze.OperationDatabaseStatementStats{{
					Query: "SELECT article FROM articles WHERE id = ?", Source: "ArticleDao.load",
					Operation: "чтение", Calls: 7, Failures: 1, MainThreadCalls: 2,
					TotalDurationUS: 61_000, MaxDurationUS: 19_000,
				}},
				CorrelatedCompose: 30, CorrelatedComposeMS: 54,
				CorrelatedWorkers: 2, CorrelatedWorkerFailures: 1, CorrelatedWorkerMS: 1_200,
				CorrelatedStalls: 2, CorrelatedStallMaxMS: 350,
				CorrelatedUIFrames: 100, CorrelatedUIJank: 7, CorrelatedUIJankRatePct: 7,
				CorrelatedIO: 5, CorrelatedIODurationUS: 24_000, CorrelatedIOBytes: 512,
				CorrelatedProblems: 2, CorrelatedLogRecords: 17,
				CorrelatedRuntimeCalls: 23, CorrelatedRuntimeTotalMS: 46,
				CorrelatedMetricEvents: 6, CorrelatedRetainedObjects: 2, MaxPSSKB: 204_800,
			}},
			TimeSlots: []analyze.OperationTimeSlot{{
				Label: "2026-08-20 15:00 (UTC+03:00)", Operation: "content.open", Kind: "screen",
				Screen: "ArticleActivity", Count: 5, P50MS: 200, P95MS: 950, MaxMS: 1200,
				CorrelatedHTTP: 3, CorrelatedHTTPFailures: 1, CorrelatedHTTPDurationMS: 612,
				CorrelatedWebSocket: 1, CorrelatedDatabase: 8, CorrelatedDatabaseMain: 2, CorrelatedDatabaseUS: 51_000,
				CorrelatedCompose: 20, CorrelatedComposeMS: 34, CorrelatedWorkers: 1, CorrelatedWorkerMS: 800,
				CorrelatedStalls: 1, CorrelatedStallMaxMS: 300,
				CorrelatedUIFrames: 80, CorrelatedUIJank: 6, CorrelatedUIJankRatePct: 7.5,
				CorrelatedIO: 4, CorrelatedIODurationUS: 12_000, CorrelatedIOBytes: 256,
				CorrelatedProblems: 1, CorrelatedLogRecords: 12,
				CorrelatedRuntimeCalls: 18, CorrelatedRuntimeTotalMS: 32,
				CorrelatedMetricEvents: 5, CorrelatedRetainedObjects: 1, MaxPSSKB: 200_704,
			}},
			Dimensions: []analyze.OperationDimensionStats{{
				Operation: "content.open", Kind: "screen", Screen: "ArticleActivity",
				Key: "source", Value: "push", Count: 4, P95MS: 950, MaxMS: 1200,
			}},
			Stages: []analyze.OperationStageStats{{
				ParentOperation: "content.open", ParentKind: "screen", Screen: "ArticleActivity",
				Stage: "database.read", Count: 7, P50MS: 80, P95MS: 700, MaxMS: 900,
				TotalMS: 2500, ParentTotalMS: 4000, SharePct: 62.5,
			}},
			WorstIncidents: []analyze.OperationIncident{{
				StartUnixMS: 1_777_000_000_000, Operation: "content.open", Kind: "screen",
				Screen: "ArticleActivity", Outcome: "failure", DurationMS: 1200,
				BudgetMS: 500, BudgetExceededMS: 700,
				CorrelatedHTTP: 2, HTTPFailures: 1, HTTPDurationMS: 410,
				CorrelatedStalls: 1, StallMaxMS: 350,
				CorrelatedUIFrames: 20, CorrelatedUIJank: 3,
				CorrelatedIO: 2, IODurationUS: 8_000, IOBytes: 128,
				CorrelatedProblems: 1, CorrelatedLogRecords: 8,
				CorrelatedRuntimeCalls: 9, CorrelatedRuntimeTotalMS: 21,
				CorrelatedMetricEvents: 2, CorrelatedRetainedObjects: 1, MaxPSSKB: 204_800,
			}},
		},
	}

	if err := WriteInspectWithOptions(path, summary, ReportOptions{GeneratedAt: "2026-08-21T12:00:00+03:00"}); err != nil {
		t.Fatalf("WriteInspectWithOptions() error = %v", err)
	}

	assertHTMLContains(t, path,
		`href="#operations"`, `id="operations"`, "Операции приложения", "Изменение по времени",
		"2026-08-20 15:00 (UTC+03:00)", "В каких условиях меняется результат",
		"Из каких этапов складывается время", "Самые тяжёлые отдельные случаи",
		"открытие экрана", "content.open", "database.read", "Часть измерений неполна", "≈900 мс",
		"Ограниченная история завершённых операций", "вытеснено: 5",
		"Сеть / ошибки / время", "Ввод-вывод / время / объём",
		"Вызовы кода / журнал / время", "Удержания / память / свои метрики",
		"4 / 1 / 845 мс", "5 / 24.00 мс / 512 Б", "23 / 17 / 46 мс", "2 / 200.0 МБ / 6",
		"Взаимное влияние подсистем внутри сценария", "Как взаимное влияние менялось по времени",
		"11 · ошибок 1", "на главном потоке 3 · 72.00 мс", "30 выполнений", "2 завершений · неуспешных 1",
		"Худшие SQL-шаблоны операции", "ArticleDao.load", "7 вызовов · ошибок 1 · главный поток 2 · 61.00 мс",
	)
}

func TestOperationTablesDeferRowsBeyondInitialPage(t *testing.T) {
	t.Parallel()
	operations := make([]analyze.OperationStats, 75)
	deltas := make([]analyze.OperationDelta, 75)
	for index := range operations {
		name := fmt.Sprintf("operation.%02d", index)
		operations[index] = analyze.OperationStats{Operation: name, Kind: "background", Count: 20}
		deltas[index] = analyze.OperationDelta{
			Operation: name, Kind: "background", BaselineCount: 20, CandidateCount: 20,
			Comparable: true, Status: "stable", Severity: "ok", Confidence: "medium",
		}
	}

	inspectPath := filepath.Join(t.TempDir(), "operations-large.html")
	if err := WriteInspectWithOptions(inspectPath, analyze.Summary{
		OperationAnalysis: &analyze.OperationAnalysis{Completed: 1_500, Operations: operations},
	}, ReportOptions{}); err != nil {
		t.Fatal(err)
	}
	assertHTMLContains(t, inspectPath, `data-deferred-total="75"`, "Показать ещё 25", "в таблице операции приложения")

	comparePath := filepath.Join(t.TempDir(), "operations-compare-large.html")
	if err := WriteCompareReportWithOptions(comparePath, analyze.Comparison{OperationDeltas: deltas}, nil, nil, ReportOptions{}); err != nil {
		t.Fatal(err)
	}
	assertHTMLContains(t, comparePath, `data-deferred-total="75"`, "Показать ещё 25", "в таблице сравнение операций")
}

func TestOperationTablesEscapeUntrustedValues(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "operations-escaped.html")
	unsafe := `<script>alert("operation")</script>`
	if err := WriteInspectWithOptions(path, analyze.Summary{
		OperationAnalysis: &analyze.OperationAnalysis{
			Completed: 1,
			Operations: []analyze.OperationStats{{
				Operation: unsafe,
				Kind:      "action",
				Screen:    `<img src=x onerror=alert("screen")>`,
				Count:     1,
			}},
		},
	}, ReportOptions{}); err != nil {
		t.Fatal(err)
	}

	assertHTMLContains(t, path, `&lt;script&gt;alert(&#34;operation&#34;)&lt;/script&gt;`, `&lt;img src=x onerror=alert(&#34;screen&#34;)&gt;`)
	assertHTMLNotContains(t, path, unsafe, `<img src=x onerror=alert("screen")>`)
}

func TestCompareReportRendersOperationDeltasAndComparisonBoundary(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "operation-compare.html")
	comparison := analyze.Comparison{OperationDeltas: []analyze.OperationDelta{
		{
			Operation: "content.open", Kind: "screen", Screen: "ArticleActivity",
			BaselineCount: 30, CandidateCount: 32, BaselineP95MS: 300, CandidateP95MS: 900,
			BaselineQuantilesApproximated: true, CandidateQuantilesApproximated: true,
			P95ChangeMS: 600, P95ChangePct: 200, Comparable: true,
			Status: "regressed", Severity: "high", Confidence: "medium",
			Note: "Описательное сравнение одинаковой операции.",
		},
		{
			Operation: "sync.refresh", Kind: "background", CandidateCount: 3,
			Status: "new", Severity: "ok", Confidence: "low",
			Note: "Операция отсутствует в базовом наборе.",
		},
	}}

	if err := WriteCompareReportWithOptions(path, comparison, nil, nil, ReportOptions{GeneratedAt: "2026-08-21T12:00:00+03:00"}); err != nil {
		t.Fatalf("WriteCompareReportWithOptions() error = %v", err)
	}

	assertHTMLContains(t, path,
		`href="#operations"`, `id="operations"`, "Изменение операций приложения",
		"не менее 20 завершений", "ухудшение", "есть только в кандидате",
		"content.open", "sync.refresh", "Операция отсутствует в базовом наборе", "≈300 мс", "≈900 мс",
	)
}

func TestCompareReportDistinguishesMissingOperationBudget(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "operation-budget-compare.html")
	comparison := analyze.Comparison{OperationDeltas: []analyze.OperationDelta{{
		Operation: "content.open", Kind: "screen",
		BaselineCount: 100, CandidateCount: 100,
		BaselineBudgeted: 0, CandidateBudgeted: 100,
		CandidateBudgetBreachPct: 25,
		Comparable:               true, Status: "stable", Severity: "ok", Confidence: "high",
	}}}

	if err := WriteCompareReportWithOptions(path, comparison, nil, nil, ReportOptions{}); err != nil {
		t.Fatal(err)
	}

	assertHTMLContains(t, path, "бюджет не задан", "25.00%")
}
