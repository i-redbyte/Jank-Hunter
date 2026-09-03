package report

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
	"github.com/i-redbyte/jank-hunter/cli/internal/mathanalysis"
)

func TestSparklineDoesNotConnectMissingMeasurements(t *testing.T) {
	html := string(sparklineSVG(mathanalysis.Series{
		Name:    "PSS",
		Points:  []float64{100, 110, 0, 120, 130},
		Present: []bool{true, true, false, true, true},
	}))

	if got := strings.Count(html, `<polyline class="spark-line"`); got != 2 {
		t.Fatalf("sparkline segments = %d, want 2: %s", got, html)
	}
}

func TestUniqueCausalEdgesRemovesReverseDirection(t *testing.T) {
	edges := []mathanalysis.CausalEdge{
		{From: "owner:A", To: "state:Janky", Kind: "owner-state"},
		{From: "state:Janky", To: "owner:A", Kind: "owner-state"},
	}

	if unique := uniqueCausalEdges(edges); len(unique) != 1 {
		t.Fatalf("unique edges = %+v, want one undirected relation", unique)
	}
}

func TestCompareRowsDoNotCallOneSidedDimensionsRegressions(t *testing.T) {
	baseline := analyze.Summary{DurationMS: 60_000}
	candidate := analyze.Summary{
		DurationMS:     60_000,
		Routes:         []analyze.RouteStats{{Route: "GET /new", Count: 4, P95MS: 900}},
		Screens:        []analyze.ScreenStats{{Screen: "NewScreen", Frames: 240, JankRatePct: 12}},
		Owners:         []analyze.OwnerStats{{Owner: "NewOwner", Count: 4, MaxMS: 900}},
		SignalContexts: []analyze.SignalContextStats{{Operation: "new-operation", HTTPCount: 4, HTTPP95MS: 900}},
	}

	if row := routeCompareRows(baseline, candidate)[0]; row.Comparable || row.Severity != "ok" {
		t.Fatalf("candidate-only route became a regression: %+v", row)
	}
	if row := screenCompareRows(baseline, candidate)[0]; row.Comparable || row.Severity != "ok" {
		t.Fatalf("candidate-only screen became a regression: %+v", row)
	}
	if row := ownerCompareRows(baseline, candidate)[0]; row.Comparable || row.Severity != "ok" {
		t.Fatalf("candidate-only owner became a regression: %+v", row)
	}
	if row := signalContextCompareRows(baseline, candidate)[0]; row.Comparable || row.Severity != "ok" {
		t.Fatalf("candidate-only operation context became a regression: %+v", row)
	}
}

func TestScreenComparisonDoesNotTreatMissingFPSAsZero(t *testing.T) {
	baseline := analyze.Summary{Screens: []analyze.ScreenStats{{
		Screen: "Feed", Frames: 240, JankRatePct: 2, AvgFPS: 60,
	}}}
	candidate := analyze.Summary{Screens: []analyze.ScreenStats{{
		Screen: "Feed", Frames: 240, JankRatePct: 2, AvgFPS: 0, FPSStatus: "sparse_rendering",
	}}}

	row := screenCompareRows(baseline, candidate)[0]
	if row.DeltaFPS != 0 || row.FPSComparable || row.Severity != "ok" {
		t.Fatalf("missing candidate FPS became a regression: %+v", row)
	}
}

func TestInspectAnalysisCapsSmallSamplesAndAvoidsHealthyClaim(t *testing.T) {
	analysis := inspectAnalysis(analyze.Summary{
		LogCount:   1,
		EventCount: 10,
		HTTPCount:  1,
		HTTPP95MS:  2_000,
	}, "ru")
	if analysis.Severity != "medium" {
		t.Fatalf("small sample severity = %q", analysis.Severity)
	}
	if analysis.Status == "Все хорошо" || strings.Contains(analysis.Summary, "прогон здоров") {
		t.Fatalf("analysis made a categorical health claim: %+v", analysis)
	}
}

func TestTransientReportOutputMatchesDurableOutput(t *testing.T) {
	directory := t.TempDir()
	durablePath := filepath.Join(directory, "durable.html")
	transientPath := filepath.Join(directory, "transient.html")
	summary := analyze.Summary{
		Title:             "sample.jhlog",
		LogCount:          1,
		EventCount:        1,
		CollectionQuality: sampleCollectionQuality(),
	}
	options := ReportOptions{GeneratedAt: "2026-08-17T12:00:00+03:00"}
	if err := WriteInspectWithOptions(durablePath, summary, options); err != nil {
		t.Fatalf("write durable report: %v", err)
	}
	options.TransientOutput = true
	if err := WriteInspectWithOptions(transientPath, summary, options); err != nil {
		t.Fatalf("write transient report: %v", err)
	}
	durable, err := os.ReadFile(durablePath)
	if err != nil {
		t.Fatal(err)
	}
	transient, err := os.ReadFile(transientPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(transient, durable) {
		t.Fatal("transient report differs from the durable report")
	}
}

func TestInspectDoesNotPresentSparseFramesAsZeroFPS(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sparse-ui.html")
	summary := analyze.Summary{
		Title:       "sparse-ui",
		UIFrames:    30,
		UIFPSStatus: "sparse_rendering",
		UIAvgFPS:    0,
		UIMinFPS:    0,
		Screens: []analyze.ScreenStats{{
			Screen: "IdleActivity", Frames: 30, WindowCount: 1, WindowMS: 90_000,
			FPSStatus: "sparse_rendering",
		}},
	}
	if err := WriteInspectWithOptions(path, summary, ReportOptions{GeneratedAt: "2026-08-18T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	html := string(payload)
	if !strings.Contains(html, "не оценивается: редкая отрисовка") {
		t.Fatalf("sparse FPS explanation missing")
	}
	if strings.Contains(html, `<div class="value">0.0</div>`) || strings.Contains(html, `<td>0.0</td>`) {
		t.Fatalf("sparse frames were rendered as a measured zero FPS")
	}
}

func TestInspectUsesGroupedIncidentsOnMainPage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "incidents.html")
	raw := []analyze.ProblemFinding{
		{ID: "raw-stall", Title: "Сырая пауза", Category: analyze.ProblemCategoryStability},
		{ID: "raw-ui", Title: "Сырой UI-сигнал", Category: analyze.ProblemCategoryUI},
	}
	incident := analyze.ProblemFinding{
		ID: "incident-feed", Title: "Экран Feed: связанный инцидент", Category: analyze.ProblemCategoryUI,
		Severity: "high", Confidence: "high", Status: "observed",
		RelatedCategories: []string{analyze.ProblemCategoryStability, analyze.ProblemCategoryUI},
	}
	summary := analyze.Summary{
		ProblemSchemaVersion: analyze.ProblemSchemaVersion,
		ProblemSummary:       analyze.ProblemSummary{Verdict: "problems_found", Headline: "Один инцидент", Total: 1, SignalTotal: 2},
		Problems:             raw,
		ProblemIncidents:     []analyze.ProblemFinding{incident},
	}
	if err := WriteInspectWithOptions(path, summary, ReportOptions{GeneratedAt: "2026-08-18T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	html := string(payload)
	if !strings.Contains(html, "Экран Feed: связанный инцидент") || !strings.Contains(html, `data-category="stability ui_main_thread"`) {
		t.Fatalf("grouped incident or its categories are missing")
	}
	if strings.Contains(html, "Сырая пауза") || strings.Contains(html, "Сырой UI-сигнал") {
		t.Fatalf("raw duplicate findings leaked into the main problem cards")
	}
}

func TestInspectExplainsUICauseCandidatesWithoutInventingTemporalCausality(t *testing.T) {
	path := filepath.Join(t.TempDir(), "actionable-ui-incident.html")
	raw := []analyze.ProblemFinding{
		{
			ID: "stall-di", DetectorID: "stability.main_thread_stall", Title: "Главный поток остановился на 1364 мс",
			Category: analyze.ProblemCategoryStability, Where: []analyze.ProblemLocation{{
				Screen: "MainActivity", Owner: "ru.mail.im.app.di.components.ComponentFactoryImpl",
				Method: "ru.mail.im.app.di.components.ComponentFactoryImpl.create(ComponentFactoryImpl.kt:483)",
			}},
		},
		{
			ID: "stall-layout", DetectorID: "stability.main_thread_stall", Title: "Главный поток остановился на 1119 мс",
			Category: analyze.ProblemCategoryStability, Where: []analyze.ProblemLocation{{
				Screen: "MainActivity", Owner: "androidx.constraintlayout.core.ArrayLinkedVariables",
				Method: "androidx.constraintlayout.core.ArrayLinkedVariables.add(ArrayLinkedVariables.java:263)",
			}},
		},
		{ID: "jank", DetectorID: "ui.jank_tail", Title: "Подтормаживания интерфейса", Category: analyze.ProblemCategoryUI},
	}
	incident := analyze.ProblemFinding{
		ID: "incident-main", DetectorID: "ui.jank_tail", Title: "Экран MainActivity: зафиксированы подтормаживания и остановки главного потока",
		Category: analyze.ProblemCategoryUI, Severity: "high", Confidence: "low", Status: "observed",
		WhatHappened: "На одном экране собраны три сигнала; это само по себе не означает, что они совпали по времени.",
		Where: []analyze.ProblemLocation{
			{Screen: "MainActivity", Owner: "ru.mail.im.app.di.components.ComponentFactoryImpl", Method: "ru.mail.im.app.di.components.ComponentFactoryImpl.create(ComponentFactoryImpl.kt:483)"},
			{Screen: "MainActivity", Owner: "androidx.constraintlayout.core.ArrayLinkedVariables", Method: "androidx.constraintlayout.core.ArrayLinkedVariables.add(ArrayLinkedVariables.java:263)"},
		},
		RelatedFindings:   []string{"stall-di", "stall-layout", "jank"},
		RelatedCategories: []string{analyze.ProblemCategoryStability, analyze.ProblemCategoryUI},
		Why: analyze.ProblemWhy{
			ClaimLevel: "correlated",
			Summary:    "Группировка по экрану не доказывает совпадение по времени и причинную связь с медленными кадрами.",
		},
		Limitations: []string{"Нет идентификатора кадра, связывающего каждую паузу с медленным кадром."},
	}
	summary := analyze.Summary{
		ProblemSchemaVersion: analyze.ProblemSchemaVersion,
		ProblemSummary:       analyze.ProblemSummary{Verdict: "problems_found", Headline: "UI-инцидент", Total: 1, SignalTotal: 3},
		Problems:             raw,
		ProblemIncidents:     []analyze.ProblemFinding{incident},
		Screens: []analyze.ScreenStats{{
			Screen: "MainActivity", Frames: 1_720, JankyFrames: 39, JankRatePct: 2.3, FrameP95MS: 40, FrameP99MS: 250,
		}},
		SignalContexts: []analyze.SignalContextStats{
			{Screen: "MainActivity", Owner: "ru.mail.im.app.di.components.ComponentFactoryImpl", StallCount: 1, StallMaxMS: 1_364},
			{Screen: "MainActivity", Owner: "androidx.constraintlayout.core.ArrayLinkedVariables", StallCount: 1, StallMaxMS: 1_119},
		},
		Owners: []analyze.OwnerStats{
			{Owner: "ru.mail.im.app.di.components.ComponentFactoryImpl", Kind: "main_thread_stall", Count: 1, MaxMS: 1_364, StackHint: "ru.mail.im.app.di.components.ComponentFactoryImpl.create(ComponentFactoryImpl.kt:483)"},
			{Owner: "androidx.constraintlayout.core.ArrayLinkedVariables", Kind: "main_thread_stall", Count: 1, MaxMS: 1_119, StackHint: "androidx.constraintlayout.core.ArrayLinkedVariables.add(ArrayLinkedVariables.java:263)"},
		},
	}
	if err := WriteInspectWithOptions(path, summary, ReportOptions{GeneratedAt: "2026-08-29T04:00:00+03:00"}); err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	html := string(payload)
	for _, want := range []string{
		"Диагноз и план расследования",
		"Что доказано данными",
		"Цепочка влияния",
		"Кандидаты на первопричину",
		"Что пока не доказано",
		"ComponentFactoryImpl.create",
		"ArrayLinkedVariables.add",
		"не доказывает совпадение по времени",
		"Пошаговый план проверки",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("actionable UI diagnosis misses %q", want)
		}
	}
}

func TestWriteReports(t *testing.T) {
	summary := analyze.Summary{
		Title:       "sample.jhlog",
		LogCount:    1,
		EventCount:  27,
		HTTPCount:   3,
		HTTPFailed:  1,
		HTTPP95MS:   612,
		UIFrames:    1122,
		UIJank:      90,
		UIJankPct:   8.02,
		UIAvgFPS:    56.1,
		StallCount:  1,
		StallMaxMS:  1240,
		MemoryMaxKB: 188240,
		NetworkAnalysis: &analyze.NetworkAnalysis{
			MaxConcurrency: 4, PeakConcurrencyAtMS: 750,
			TransportFailures: 1, HTTP4xx: 1, HTTP5xx: 1, Canceled: 1,
			CacheHits: 1, ReusedConnections: 2, KnownRequestBytes: 2, KnownResponseBytes: 3,
			Attempts: 5, DNSAttempts: 2, ConnectAttempts: 3, TLSAttempts: 2,
			Retries: 1, Redirects: 1, ConnectFailures: 1, BytesRx: 8192, BytesTx: 1024,
			Phases:        []analyze.HTTPPhaseStats{{Name: "queue", SampleCount: 3, AvgMS: 80, P50MS: 50, P95MS: 150, MaxMS: 150}},
			StatusCodes:   []analyze.NamedValue{{Name: "200", Value: 1}, {Name: "429", Value: 1}, {Name: "503", Value: 1}},
			FailurePhases: []analyze.NamedValue{{Name: "response", Value: 1}, {Name: "cancelled", Value: 1}},
			FailureKinds:  []analyze.NamedValue{{Name: "timeout", Value: 1}, {Name: "cancelled", Value: 1}},
			Protocols:     []analyze.NamedValue{{Name: "http/2", Value: 3}},
			Calls: []analyze.NetworkCallStats{{
				Route: "GET /feed", Service: "feed-api", Initiator: "FeedRepository.refresh",
				Screen: "Feed", Operation: "feed.open", Owner: "FeedViewModel.load",
				Count: 3, Failures: 1, HTTP5xx: 1, CacheHits: 1, ReusedConnections: 2,
				Attempts: 5, Retries: 1, Redirects: 1, P50MS: 300, P95MS: 612, MaxMS: 900,
				Phases: []analyze.HTTPPhaseStats{{Name: "ttfb", SampleCount: 3, AvgMS: 400, P50MS: 380, P95MS: 550, MaxMS: 550}},
			}},
		},
		WebSocketAnalysis: &analyze.WebSocketAnalysis{
			Opened: 2, Closed: 1, Failures: 1, ActiveAtEnd: 0, Reconnects: 1,
			ConnectP50MS: 75, ConnectP95MS: 90, ConnectMaxMS: 90,
			LifetimeP50MS: 12_000, LifetimeP95MS: 20_000, LifetimeMaxMS: 20_000,
			TextMessages: 7, BinaryMessages: 3, ReceivedBytes: 4096,
			FailureKinds: []analyze.NamedValue{{Name: "timeout", Value: 1}},
			CloseCodes:   []analyze.NamedValue{{Name: "1000", Value: 1}},
			Connections: []analyze.WebSocketConnectionStats{{
				Route: "GET /socket", Screen: "Feed", Operation: "feed.open", Owner: "RealtimeRepository",
				Opened: 2, Closed: 1, Failures: 1, Reconnects: 1,
				ConnectP50MS: 75, ConnectP95MS: 90, ConnectMaxMS: 90,
				LifetimeP50MS: 12_000, LifetimeP95MS: 20_000, LifetimeMaxMS: 20_000,
				TextMessages: 7, BinaryMessages: 3, ReceivedBytes: 4096,
			}},
		},
		Environment: analyze.RunEnvironment{
			Title:    "Pixel 8",
			Subtitle: "Android 15 · 0.1.0-debug (100) · процесс main",
			Items: []analyze.InfoItem{
				{Label: "Батарея", Value: "82%", Detail: "заряжается · 32.0 C"},
				{Label: "Сеть", Value: "wifi", Detail: "валидирована да · лимитная нет · VPN нет"},
				{Label: "Рут-доступ", Value: "нет", Detail: "признаки рут-доступа не найдены"},
			},
		},
		CollectionQuality: sampleCollectionQuality(),
		Routes: []analyze.RouteStats{
			{Route: "GET /feed", ServiceSample: "feed-api", InitiatorSample: "FeedRepository.refresh", Count: 21_000, ContextCount: 1, Failures: 0, HTTP4xx: 1, HTTP5xx: 1, CacheHits: 1, ReusedConnections: 2, Attempts: 5, Retries: 1, Redirects: 1, ConnectFailures: 1, P95MS: 612, MaxMS: 612, MaxConcurrency: 4, Phases: []analyze.HTTPPhaseStats{{Name: "ttfb", SampleCount: 3, AvgMS: 400, P50MS: 380, P95MS: 550, MaxMS: 550}}, OwnerSample: "FeedRepository.refresh"},
		},
		Screens: []analyze.ScreenStats{
			{Screen: "Feed", Frames: 1122, JankyFrames: 90, JankRatePct: 8.02, AvgFPS: 56.1, WindowCount: 3, FrameP95MS: 24, FrameDistributionState: "mergeable_histogram_v2"},
		},
		Owners: []analyze.OwnerStats{
			{Owner: "FeedRepository.refresh", Kind: "http", Count: 2, MaxMS: 612},
		},
		SignalContexts: []analyze.SignalContextStats{
			{Screen: "Feed", Operation: "feed.open", Owner: "FeedRepository.refresh", HTTPCount: 21_000, HTTPP95MS: 612, UIFrames: 1122, UIJank: 90, UIJankPct: 8.02},
		},
		LogSpam: []analyze.LogSpamStats{
			{Screen: "Feed", Operation: "feed.open", Owner: "FeedPresenter.render", Source: "android.util.Log.w", Level: "warn", Count: 7},
		},
		ProblemWindows: []analyze.ProblemWindowStats{
			{Screen: "Feed", Operation: "feed.open", Owner: "FeedPresenter.render", Kind: "ui_jank", Windows: 1, Count: 90, TotalWindowMS: 10000, MaxMS: 24},
		},
		RuntimeCalls: []analyze.RuntimeCallStats{
			{Screen: "Feed", Operation: "feed.open", Caller: "FeedPresenter.render", Callee: "FeedAdapter.bind", Count: 12, TotalMS: 144, MaxMS: 24},
		},
		MemoryLeaks: []analyze.MemoryLeakSuspect{
			{
				ClassName:                "com.app.feed.FeedActivity",
				Holder:                   "FeedPresenter",
				Screen:                   "Feed",
				Operation:                "feed.open",
				Count:                    2,
				MaxAgeMS:                 30_000,
				EstimatedRetainedKB:      4096,
				RetainedSizeConfidence:   "среднее: есть возраст/повторяемость",
				RetainedSizeExplanation:  "Оценка по типу объекта, числу удержаний, возрасту и PSS процесса.",
				DominatorPath:            []string{"экран: Feed", "сценарий: feed.open", "держатель: FeedPresenter", "удержанный объект: com.app.feed.FeedActivity"},
				DominatorTreeConfidence:  "среднее: путь собран из контекста выполнения",
				DominatorTreeExplanation: "Схема показывает контекст обнаружения и не является цепочкой ссылок.",
				LeakChainConfidence:      "среднее: пользовательский держатель и контекст",
				LeakChainSummary:         "Удержан экран / Activity com.app.feed.FeedActivity. Вероятный пользовательский держатель: FeedPresenter.",
				LeakChainActions:         []string{"Проверьте FeedPresenter: какие поля, кеши, слушатели или обратные вызовы сохраняют com.app.feed.FeedActivity.", "Проверьте жизненный цикл: очистку ссылок в onDestroy/onDestroyView."},
				Score:                    9.5,
				Severity:                 "medium",
				ObjectKind:               "экран / Activity",
				HolderQuality:            "вероятный держатель из контекста",
				UserOwned:                true,
				Impact:                   "Удержано 2 объекта, максимальный возраст 30 сек.",
				Recommendation:           "Проверьте FeedPresenter: очистку слушателей и отмену фоновой работы.",
				Evidence:                 "кол-во=2 · макс. возраст=30 сек",
			},
		},
		Influence: sampleInfluence(),
	}
	summary.CodeProblems = analyze.BuildCodeProblemRegistry(summary)
	attachProblemReport(t, &summary)

	dir := t.TempDir()
	inspectPath := filepath.Join(dir, "inspect.html")
	if err := WriteInspectWithOptions(inspectPath, summary, ReportOptions{Links: ReportLinks{
		Math:      "inspect-math.html",
		Leaks:     "inspect-leaks.html",
		Influence: "inspect-influence.html",
	}}); err != nil {
		t.Fatalf("WriteInspect() error = %v", err)
	}
	assertCurrentReportStyle(t, inspectPath)
	assertHTMLContains(t, inspectPath, "Проблемы приложения", `id="problems"`, "data-problem-inbox", "data-problem-search", "data-problem-search-clear", "data-problem-search-results", "Поиск охватывает не только карточки проблем", "Поиск по всем данным страницы", "все классы и строки подробных разделов этой страницы, включая ещё не раскрытые", "Фильтры справа изменяют только карточки проблем", "Что делать", "Состав приоритета", "Контекст устройства", "Pixel 8", "Рут-доступ", "Все записанные сетевые маршруты", "Ниже показаны все маршруты, а не только худшие", "Связанные сигналы", "Спам логами", "Проблемные окна", "Связи вызовов", "Код и полные доказательства", "Открыть полный реестр кода и подтверждающие данные", "Удержания и возможные утечки памяти", "data-registry-category", "data-registry-severity", "span-all", "Шкала сигналов удержания", "Фильтр реестра утечек памяти", "FeedPresenter", "Быстрые проверки цепочки", "Вероятный пользовательский держатель", "Оценка удержанного размера", "Путь / контекст удержания", "leak-dominator", "4.0 МБ", "Фильтр по классу", "data-code-registry", "data-code-sort", "Как читать отчет", "Что исправлять", "jh-tooltip", "GET /feed", "Подтормаживания интерфейса", "Граф влияния кода", "influence-tile-body", "gauge-ring", `pathLength="100"`, "stroke-dasharray: var(--value) 100", "Диагностический индекс полноты: 80.00", "не оценка риска приложения", "Граф вызовов во время выполнения отключён", "Не хватает", "Подробный анализ", `href="inspect-math.html"`)
	assertHTMLContains(t, inspectPath, "Подробный сетевой анализ", "Максимальная одновременность", "Запросы по месту вызова и контексту", "feed-api", "FeedViewModel.load", "Фазы маршрутов", "TTFB", "Повторы без перенаправлений", "Точные HTTP-коды", "503", "network-route-table wide-analysis-table", "network-call-table wide-analysis-table", "min-width: 2440px", "word-break: keep-all", "WebSocket: соединения, сообщения и обрывы", "GET /socket", "RealtimeRepository", "websocket-connection-table wide-analysis-table", "тайм-аут")
	assertHTMLContains(t, inspectPath, "z-index: 2147483647", "word-break: keep-all", "table-scroll", "wrapTables", "table-cell-clip", "cell-toggle", "scheduleTableMeasure", "details.addEventListener('toggle'", "IntersectionObserver", "tooltipTarget", "ensureSelectOption", "setSelectFromChip", "viewportBox", "descriptiveBlock", ".problem-card[hidden]", "minmax(min(100%, 340px), 1fr)", "indexDeferredScript", "report-search-deferred", "revealDeferredSearchEntry", "details.parentElement?.closest('details')")
	assertHTMLContains(t, inspectPath, "Достаточность данных для выводов", "Проверка правил на эталонных данных", "нет эталонной проверки", "Задержка верхних 5% кадров", "Плавность интерфейса и возможные причины", "Итог анализа", "Все найденные проблемные элементы и факторы", "Уровень связи", "кандидат из кода", "Почему это показано", "Где смотреть код", "Как проверить версию", "Что происходило рядом", "С чего начать", "ui-cause-grid", "relation-context")
	assertHTMLNotContains(t, inspectPath, "Drill-down", "conic-gradient(var(--color)", "reservoir", "approx-badge", `<script type="application/octet-stream" data-code-problem-evidence-archive`, "Фильтр реестра проблем кода")

	mathInspectPath := filepath.Join(dir, "inspect-math.html")
	if err := WriteMathInspectWithOptions(mathInspectPath, sampleMathReport(summary), ReportOptions{Links: ReportLinks{
		Main:      "inspect.html",
		Influence: "inspect-influence.html",
	}}); err != nil {
		t.Fatalf("WriteMathInspect() error = %v", err)
	}
	assertCurrentReportStyle(t, mathInspectPath)
	assertHTMLContains(t, mathInspectPath, "Математический анализ", "Качество данных", "Сетевые циклы", "Атрибуция операций и источников", "Реестр проблем кода", `id="code-problems" class="fold code-registry-fold" open`, "Разбор утечек памяти", "Шкала математических оценок", "Шкала реестра кода", "registry-insights", "code-problem-details", "Доказательства и рекомендация", "FeedPresenter", "Шкала сигналов удержания", "Оценка удержанного размера", "Путь / контекст удержания", "overview-attribution-fold", "data-zero-scope", "closest('[data-zero-scope]')", "Пустые интервалы скрыты", "Вызовы выполнения", "Как читать оценки", "Критерии", "Накопленная нагрузка", "Детали раздела", "Сводка разделов", "Справка по методам", "Устойчивая статистика", "дельта Клиффа", "Граф связей и гипотез", "Уверенность", "Доля плохих состояний", "Повторение проблемных состояний", "Вклады симптомов", "Проекция внутри записанного прогона", "Методика и наблюдения", "Пропуск означает отсутствие замера памяти", "Самое большое реально наблюдавшееся значение", "Пустой интервал пропускается и разрывает последовательность", "измерено", "Поддержка модели: <strong>низкая</strong>", `data-markov-forecast="insufficient"`, `href="inspect.html"`, `href="inspect.html#runtime-calls"`, "не дублируется второй раз", "← Обзор", "periodic-analysis-table wide-analysis-table", "integral-score-table wide-analysis-table", "min-width: 2240px")

	comparePath := filepath.Join(dir, "compare.html")
	comparison := analyze.Compare(summary, summary)
	if err := WriteCompareReportWithOptions(
		comparePath,
		comparison,
		[]LogReport{{Name: "old/sample.jhlog", Anchor: "baseline-log-1", Summary: summary}},
		[]LogReport{{Name: "new/sample.jhlog", Anchor: "candidate-log-1", Summary: summary}},
		ReportOptions{Links: ReportLinks{Math: "compare-math.html", Leaks: "compare-leaks.html"}},
	); err != nil {
		t.Fatalf("WriteCompareReport() error = %v", err)
	}
	assertCurrentReportStyle(t, comparePath)
	assertHTMLContains(t, comparePath, "Изменения проблем", `id="problem-changes"`, `data-status="persistent"`, "data-problem-status", "Контекст сравнения", "Сеть и трафик", "Код и подтверждающие данные кандидата", "Открыть сравнительный реестр кода и подтверждающие данные", "Сравнение сигналов удержания памяти", "Шкала сравнения", "data-registry-category", "data-registry-severity", "Шкала сигналов удержания", "Оценка удержанного размера", "Путь / контекст удержания", "Фильтр сравнительного реестра утечек памяти", "data-code-registry", "data-code-sort", "дельта", "Где изменилось", "Сравнение связанных сигналов", "Как читать сравнение", "Контекст устройств", "Детали по каждому журналу", "Эвристический итог", "gauge-ring", `pathLength="100"`, "old/sample.jhlog", "new/sample.jhlog", "Диагностический индекс полноты: база 80.00%", "кандидат 80.00%", "не статистическая вероятность или оценка риска", "Граф вызовов во время выполнения отключён", "λ Анализ", `href="compare-math.html"`)
	assertHTMLNotContains(t, comparePath, `<script type="application/octet-stream" data-code-problem-evidence-archive`, "Фильтр сравнительного реестра проблем кода")

	assertHTMLContains(t, comparePath, "Достаточность данных кандидата", "Проверка правил на эталонных данных", "Верхние 5% кадров кандидата")
	mathComparePath := filepath.Join(dir, "compare-math.html")
	if err := WriteMathCompareWithOptions(mathComparePath, sampleCompareMathReport(comparison, summary), ReportOptions{Links: ReportLinks{Main: "compare.html"}}); err != nil {
		t.Fatalf("WriteMathCompare() error = %v", err)
	}
	assertCurrentReportStyle(t, mathComparePath)
	assertHTMLContains(t, mathComparePath, "Математический анализ сравнения", "Качество сравнения", "Сетевые циклы", "Сравнение операций и источников", "Реестр проблем кода кандидата", `id="code-problems" class="fold code-registry-fold" open`, "Сравнение сигналов удержания памяти", "Шкала сравнения", "Шкала реестра кода", "registry-insights", "code-problem-details", "Доказательства и рекомендация", "FeedPresenter", "Шкала сигналов удержания", "Оценка удержанного размера", "Путь / контекст удержания", "Фильтр сравнительного реестра утечек памяти", "Фильтр сравнительного реестра проблем кода", "data-code-registry", "data-code-sort", "Как читать сравнение", "Критерии", "Сводка разделов", "Справка по методам", "Марковская модель состояний", "Расхождение матрицы переходов", "Доля плохих состояний проверяемого прогона", "Граф связей и гипотез", "Базовый прогон · проекция внутри записи", "Проверяемый прогон · проекция внутри записи", "Сильно разные N могут означать", "Регрессия рассчитывается только для сопоставимой длительности", "не применимо", "измерено", `data-markov-forecast="insufficient"`, `href="compare.html"`, "← Обзор")

	influencePath := filepath.Join(dir, "inspect-influence.html")
	if err := WriteInfluenceWithOptions(influencePath, sampleInfluence(), "Граф влияния кода", ReportOptions{Links: ReportLinks{Main: "inspect.html"}}); err != nil {
		t.Fatalf("WriteInfluence() error = %v", err)
	}
	assertCurrentReportStyle(t, influencePath)
	assertHTMLContains(t, influencePath, "Граф влияния кода", "Карта влияния", "Классы для проверки", "Связи между классами", "Пути для расследования", "Методы для проверки", "Показать классы для проверки", "Показать связи", "influence-table-fold", "Оценка", "CheckoutRepository", "CheckoutPresenter", ".influence-node.high circle", "vector-effect: non-scaling-stroke", "data-influence-view=\"packages\"", "data-influence-highlight=\"tree\"", "data-influence-selection", "data-influence-viewport", "buildNeighborhood", "expandPackage", "RuntimeCount", "StaticCount", `href="inspect.html"`, "← Обзор")

	diagnosticsPath := filepath.Join(dir, "inspect-diagnostics.html")
	if err := WriteInstrumentationDiagnosticsWithOptions(diagnosticsPath, sampleInstrumentationDiagnostics(), ReportOptions{Links: ReportLinks{Main: "inspect.html"}}); err != nil {
		t.Fatalf("WriteInstrumentationDiagnostics() error = %v", err)
	}
	assertCurrentReportStyle(t, diagnosticsPath)
	assertHTMLContains(t, diagnosticsPath, "ASM диагностика", "Сводка ASM", "Сработавшие перехватчики", "Решения сопоставителя", "Области аннотаций", "okhttp3.bridge.v3", "FeedOwner", "invalid class metadata", "Иерархия broken.Parent разрешена частично", "instrumentation-diagnostics.jsonl", `href="inspect.html"`, "← Обзор")

	dependencyInjectionPath := filepath.Join(dir, "inspect-di.html")
	if err := WriteDependencyInjectionWithOptions(
		dependencyInjectionPath,
		sampleDependencyInjectionReport(),
		ReportOptions{Links: ReportLinks{Main: "inspect.html"}},
	); err != nil {
		t.Fatalf("WriteDependencyInjectionWithOptions() error = %v", err)
	}
	assertCurrentReportStyle(t, dependencyInjectionPath)
	assertHTMLContains(
		t,
		dependencyInjectionPath,
		"DI-каталог",
		"DI · ПРИ СБОРКЕ",
		"потребитель → зависимость",
		analyze.DependencyInjectionDisclaimer,
		"com.app.FeedViewModel",
		"com.app.FeedRepository",
		"подтверждено созданным кодом",
		"--di: #a78bfa",
		`href="inspect.html"`,
		"← Обзор",
	)
}

func TestLeakGraphSVGScopesMarkerAndGradientIDs(t *testing.T) {
	graph := analyze.LeakGraph{
		Title:    "Контекст обнаружения удержанного объекта",
		RootID:   "root",
		TargetID: "target",
		Nodes: []analyze.LeakGraphNode{
			{ID: "root", Label: "экран: Feed", Kind: "screen"},
			{ID: "target", Label: "удержанный объект: View", Kind: "target"},
		},
		Edges: []analyze.LeakGraphEdge{{
			From: "root", To: "target", Label: "наблюдался в этом контексте", Kind: "runtime",
		}},
	}
	first := string(leakGraphSVG("inspect-leak-1", graph))
	second := string(leakGraphSVG("inspect-leak-2", graph))

	if !strings.Contains(first, `id="leak-arrow-inspect-leak-1-target"`) ||
		!strings.Contains(second, `id="leak-arrow-inspect-leak-2-target"`) {
		t.Fatalf("scoped marker IDs missing:\nfirst=%s\nsecond=%s", first, second)
	}
	if strings.Contains(first, `id="leak-arrow-inspect-leak-2-target"`) ||
		strings.Contains(second, `id="leak-arrow-inspect-leak-1-target"`) {
		t.Fatal("marker IDs leaked between graph scopes")
	}
	if !strings.Contains(first, `class="leak-graph-edge-label-bg"`) ||
		!strings.Contains(first, `marker-end="url(#leak-arrow-inspect-leak-1-target)"`) {
		t.Fatalf("edge corridor or arrow marker missing: %s", first)
	}
}

func TestInspectPlacesCollectionQualityAtTheEndInCollapsedTechnicalSection(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "inspect.html")
	summary := analyze.Summary{
		Title:             "active.jhlog",
		LogCount:          1,
		CollectorSessions: 1,
		CollectorFlagsAny: uint64(jhlog.CollectorKnownMask &^ jhlog.CollectorIOTracing),
		CollectorFlagsAll: uint64(jhlog.CollectorKnownMask &^ jhlog.CollectorIOTracing),
		CollectionQuality: analyze.CollectionQuality{
			Level:   "high",
			Notices: []string{"снимок активной сессии прочитан корректно"},
		},
		Warnings: []string{"Качество сбора: тестовое техническое предупреждение."},
	}
	if err := WriteInspectWithOptions(path, summary, ReportOptions{}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	html := string(data)
	qualityIndex := strings.Index(html, `id="collection-quality"`)
	analysisIndex := strings.Index(html, `id="analysis"`)
	if qualityIndex < 0 || analysisIndex < 0 || qualityIndex < analysisIndex {
		t.Fatalf("technical quality is not at report end: analysis=%d quality=%d", analysisIndex, qualityIndex)
	}
	if strings.Contains(html, "Часть событий не попала в журнал") {
		t.Fatal("generic collection-loss warning leaked into the problem-oriented report")
	}
	if strings.Contains(html, "тестовое техническое предупреждение") {
		t.Fatal("internal collection warning leaked into the user-facing report")
	}
	qualitySection := html[qualityIndex:]
	if !strings.Contains(qualitySection, `<details class="fold">`) ||
		strings.Contains(qualitySection, `<details class="fold" open>`) {
		t.Fatal("technical quality details must be collapsed by default")
	}
	for _, expected := range []string{
		"Какие сборщики реально были включены",
		"Статусы прочитаны из самого журнала",
		"collector-capability-disabled",
		"Типизированные I/O операции",
		"В этом прогоне данные этого типа не собирались",
	} {
		if !strings.Contains(qualitySection, expected) {
			t.Fatalf("technical quality misses %q", expected)
		}
	}
}

func TestInspectRendersCollapsedLogGrowthSectionAfterCollectionQuality(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "inspect.html")
	summary := analyze.Summary{
		Title: "growth.jhlog",
		LogGrowth: analyze.LogGrowthSummary{
			Available:         true,
			HistoryGeneration: 7,
			CapturedAtMS:      1_800_000,
			Sessions: []jhlog.LogGrowthSession{{
				SessionID:            "session-7",
				DayKey:               20270115,
				StartedAtMS:          1_000_000,
				EndedAtMS:            1_600_000,
				ConfiguredLimitBytes: 1_048_576,
				MaximumRetainedBytes: 1_000_000,
				GeneratedBytes:       1_700_000,
				LimitReachedCount:    1,
				SegmentRotationCount: 4,
				ArchiveEvictedBytes:  700_000,
				Completed:            true,
			}},
			Days: []jhlog.LogGrowthDay{{
				DayKey:                20270115,
				SessionCount:          1,
				TotalDurationMS:       600_000,
				GeneratedBytes:        1_700_000,
				MaximumRetainedBytes:  1_000_000,
				MaximumFillPermille:   954,
				SessionsReachingLimit: 1,
				LimitReachedCount:     1,
				SegmentRotationCount:  4,
				ArchiveEvictedBytes:   700_000,
			}},
		},
	}
	if err := WriteInspectWithOptions(path, summary, ReportOptions{}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	html := string(data)
	qualityIndex := strings.Index(html, `id="collection-quality"`)
	growthIndex := strings.Index(html, `id="log-growth"`)
	if qualityIndex < 0 || growthIndex <= qualityIndex {
		t.Fatalf("log growth section must be last: quality=%d growth=%d", qualityIndex, growthIndex)
	}
	growthSection := html[growthIndex:]
	for _, expected := range []string{
		`<details class="fold">`,
		`data-log-growth-json`,
		`"history_generation":7`,
		`"limit_reached_count":1`,
		`data-growth-period="week"`,
		`data-growth-calculate`,
		`data-growth-session-chart`,
		`data-growth-day-chart`,
		`data-growth-period-insight`,
		`по горизонтали — дата начала сессии`,
		`сбор остановлен по лимиту`,
		`Общий лимит часто исчерпывается`,
		`const selected = days.filter`,
	} {
		if !strings.Contains(growthSection, expected) {
			t.Fatalf("log growth report misses %q", expected)
		}
	}
	if strings.Contains(growthSection, `<details class="fold" open>`) {
		t.Fatal("log growth details must be collapsed by default")
	}
}

func TestInspectMakesStaleGrowthProjectionProminent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stale-growth.html")
	summary := analyze.Summary{LogGrowth: analyze.LogGrowthSummary{
		Available:       true,
		FreshnessStatus: "stale",
		FreshnessReason: "lag=82507 мс, хвост=20609 байт",
	}}
	if err := WriteInspectWithOptions(path, summary, ReportOptions{}); err != nil {
		t.Fatal(err)
	}
	html, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"Снимок роста устарел", "отставание 82507 мс", "согласованный снимок выгрузки"} {
		if !strings.Contains(string(html), expected) {
			t.Fatalf("stale growth report misses %q", expected)
		}
	}
}

func TestInspectKeepsAnalysisInputCompletenessInTechnicalDetails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "analysis-inputs.html")
	summary := analyze.Summary{AnalysisInputs: analyze.AnalysisInputCompleteness{
		Status:          "runtime_only",
		RuntimeEvidence: true,
		Missing:         []string{"class-graph.jsonl", "instrumentation-diagnostics.jsonl"},
		Explanation:     "доступны только runtime evidence",
	}}
	if err := WriteInspectWithOptions(path, summary, ReportOptions{}); err != nil {
		t.Fatal(err)
	}
	html, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		`id="collection-quality"`,
		"Источники технического анализа",
		"Часть углублённого анализа недоступна",
		"class-graph.jsonl, instrumentation-diagnostics.jsonl",
		"События запуска",
	} {
		if !strings.Contains(string(html), expected) {
			t.Fatalf("analysis completeness report misses %q", expected)
		}
	}
}

func TestInspectRendersLogGrowthStateMatrix(t *testing.T) {
	const limit = uint64(1024 * 1024)
	tests := []struct {
		name    string
		session jhlog.LogGrowthSession
		current bool
	}{
		{
			name: "below limit",
			session: jhlog.LogGrowthSession{
				SessionID:            "below-limit",
				DayKey:               20260701,
				StartedAtMS:          1_700_000_000_000,
				EndedAtMS:            1_700_000_060_000,
				ConfiguredLimitBytes: limit,
				MaximumRetainedBytes: limit / 2,
				GeneratedBytes:       limit / 2,
				Completed:            true,
			},
		},
		{
			name: "archive limit reached",
			session: jhlog.LogGrowthSession{
				SessionID:            "archive-limit-reached",
				DayKey:               20260702,
				StartedAtMS:          1_700_086_400_000,
				EndedAtMS:            1_700_086_520_000,
				ConfiguredLimitBytes: limit,
				MaximumRetainedBytes: limit,
				GeneratedBytes:       4 * limit,
				LimitReachedCount:    1,
				SegmentRotationCount: 9,
				ArchiveEvictedBytes:  3 * limit,
				FirstLimitReachedMS:  1_700_086_430_000,
				LastLimitReachedMS:   1_700_086_510_000,
				Completed:            true,
			},
		},
		{
			name: "active session",
			session: jhlog.LogGrowthSession{
				SessionID:            "active-session",
				DayKey:               20260703,
				StartedAtMS:          1_700_172_800_000,
				EndedAtMS:            1_700_172_830_000,
				ConfiguredLimitBytes: limit,
				MaximumRetainedBytes: limit / 4,
				GeneratedBytes:       limit / 4,
			},
			current: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			day := growthDayFromSession(test.session)
			growth := analyze.LogGrowthSummary{
				Available:         true,
				HistoryGeneration: 11,
				CapturedAtMS:      test.session.EndedAtMS,
				Sessions:          []jhlog.LogGrowthSession{test.session},
				Days:              []jhlog.LogGrowthDay{day},
			}
			if test.current {
				current := test.session
				growth.CurrentSession = &current
			}

			path := filepath.Join(t.TempDir(), "inspect.html")
			if err := WriteInspectWithOptions(path, analyze.Summary{Title: test.name, LogGrowth: growth}, ReportOptions{}); err != nil {
				t.Fatal(err)
			}
			decoded := readEmbeddedLogGrowth(t, path)
			if len(decoded.Sessions) != 1 || decoded.Sessions[0] != test.session {
				t.Fatalf("sessions = %+v", decoded.Sessions)
			}
			if len(decoded.Days) != 1 || decoded.Days[0] != day {
				t.Fatalf("days = %+v", decoded.Days)
			}
			if (decoded.CurrentSession != nil) != test.current {
				t.Fatalf("current session = %+v, want present=%t", decoded.CurrentSession, test.current)
			}
		})
	}
}

func TestInspectEmbedsCompleteCalendarMonthForOnDemandCalculation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inspect.html")
	if err := WriteInspectWithOptions(
		path,
		analyze.Summary{Title: "calendar-month", LogGrowth: calendarMonthLogGrowthSummary()},
		ReportOptions{},
	); err != nil {
		t.Fatal(err)
	}

	decoded := readEmbeddedLogGrowth(t, path)
	if len(decoded.Sessions) != 31 || len(decoded.Days) != 31 {
		t.Fatalf("month detail = %d sessions/%d days, want 31/31", len(decoded.Sessions), len(decoded.Days))
	}
	if decoded.Days[0].DayKey != 20260701 || decoded.Days[30].DayKey != 20260731 {
		t.Fatalf("month bounds = %d..%d", decoded.Days[0].DayKey, decoded.Days[30].DayKey)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	html := string(data)
	for _, expected := range []string{
		`data-growth-period="current-month"`,
		`data-growth-period="previous-month"`,
		`data-growth-from`,
		`data-growth-to`,
		`data-growth-calculate`,
	} {
		if !strings.Contains(html, expected) {
			t.Fatalf("calendar-month report misses %q", expected)
		}
	}
}

func TestReportDatesUseDayMonthYearDisplayFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inspect.html")
	summary := analyze.Summary{
		Title: "dates",
		Environment: analyze.RunEnvironment{
			Items: []analyze.InfoItem{{
				Label:  "Android",
				Value:  "15",
				Detail: "API 35 · патч безопасности 2026-07-09",
			}},
		},
		LogGrowth: calendarMonthLogGrowthSummary(),
	}
	if err := WriteInspectWithOptions(path, summary, ReportOptions{GeneratedAt: "2026-08-08T14:05:06+03:00"}); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	html := string(data)
	for _, expected := range []string{
		"создан 08.08.2026, 14:05:06",
		"патч безопасности 09.07.2026",
	} {
		if !strings.Contains(html, expected) {
			t.Fatalf("report misses %q", expected)
		}
	}
	for _, forbidden := range []string{
		"создан 2026-08-08",
		"патч безопасности 2026-07-09",
	} {
		if strings.Contains(html, forbidden) {
			t.Fatalf("report contains internal date form %q", forbidden)
		}
	}
}

func TestWriteLogGrowthVisualFixture(t *testing.T) {
	path := os.Getenv("JH_GROWTH_VISUAL_OUT")
	if path == "" {
		path = filepath.Join(t.TempDir(), "inspect.html")
	}
	if err := WriteInspectWithOptions(
		path,
		analyze.Summary{Title: "calendar-month", LogGrowth: calendarMonthLogGrowthSummary()},
		ReportOptions{},
	); err != nil {
		t.Fatal(err)
	}
}

func TestWriteLogGrowthPaginationFixture(t *testing.T) {
	path := os.Getenv("JH_GROWTH_PAGINATION_OUT")
	if path == "" {
		path = filepath.Join(t.TempDir(), "inspect-pagination.html")
	}
	if err := WriteInspectWithOptions(
		path,
		analyze.Summary{Title: "log-growth-pagination", LogGrowth: largeLogGrowthSummary(120)},
		ReportOptions{},
	); err != nil {
		t.Fatal(err)
	}
}

func largeLogGrowthSummary(count int) analyze.LogGrowthSummary {
	const limit = uint64(1024 * 1024)
	const minuteMS = uint64(60_000)
	startedAt := time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC)
	sessions := make([]jhlog.LogGrowthSession, 0, count)
	days := make([]jhlog.LogGrowthDay, 0, count)
	for index := range count {
		started := startedAt.AddDate(0, 0, index)
		startedMS := uint64(started.UnixMilli())
		session := jhlog.LogGrowthSession{
			SessionID:            fmt.Sprintf("pagination-%03d", index),
			DayKey:               uint32(started.Year()*10_000 + int(started.Month())*100 + started.Day()),
			StartedAtMS:          startedMS,
			EndedAtMS:            startedMS + minuteMS,
			ConfiguredLimitBytes: limit,
			MaximumRetainedBytes: limit / 2,
			GeneratedBytes:       limit / 2,
			Completed:            true,
		}
		sessions = append(sessions, session)
		days = append(days, growthDayFromSession(session))
	}
	return analyze.LogGrowthSummary{
		Available:         true,
		HistoryGeneration: uint64(count),
		CapturedAtMS:      sessions[len(sessions)-1].EndedAtMS,
		Sessions:          sessions,
		Days:              days,
	}
}

func calendarMonthLogGrowthSummary() analyze.LogGrowthSummary {
	const limit = uint64(1024 * 1024)
	const minuteMS = uint64(60_000)
	sessions := make([]jhlog.LogGrowthSession, 0, 31)
	days := make([]jhlog.LogGrowthDay, 0, 31)
	for day := 1; day <= 31; day++ {
		started := uint64(time.Date(2026, time.July, day, 12, 0, 0, 0, time.UTC).UnixMilli())
		limitReached := uint64(0)
		maximum := limit / 2
		generated := maximum
		if day%5 == 0 {
			limitReached = uint64(day / 5)
			maximum = limit
			generated = (limitReached + 1) * limit
		}
		session := jhlog.LogGrowthSession{
			SessionID:            fmt.Sprintf("july-%02d", day),
			DayKey:               uint32(20260700 + day),
			StartedAtMS:          started,
			EndedAtMS:            started + minuteMS,
			ConfiguredLimitBytes: limit,
			MaximumRetainedBytes: maximum,
			GeneratedBytes:       generated,
			LimitReachedCount:    limitReached,
			SegmentRotationCount: limitReached * 2,
			ArchiveEvictedBytes:  limitReached * limit,
			Completed:            true,
		}
		if limitReached > 0 {
			session.FirstLimitReachedMS = started + 10_000
			session.LastLimitReachedMS = started + 50_000
		}
		sessions = append(sessions, session)
		days = append(days, growthDayFromSession(session))
	}
	return analyze.LogGrowthSummary{
		Available:         true,
		HistoryGeneration: 31,
		CapturedAtMS:      sessions[len(sessions)-1].EndedAtMS,
		Sessions:          sessions,
		Days:              days,
	}
}

func growthDayFromSession(session jhlog.LogGrowthSession) jhlog.LogGrowthDay {
	duration := uint64(0)
	if session.EndedAtMS >= session.StartedAtMS {
		duration = session.EndedAtMS - session.StartedAtMS
	}
	fill := uint64(0)
	if session.ConfiguredLimitBytes > 0 {
		fill = session.MaximumRetainedBytes * 1_000 / session.ConfiguredLimitBytes
	}
	reached := uint64(0)
	if session.LimitReachedCount > 0 {
		reached = 1
	}
	return jhlog.LogGrowthDay{
		DayKey:                session.DayKey,
		SessionCount:          1,
		TotalDurationMS:       duration,
		GeneratedBytes:        session.GeneratedBytes,
		MaximumRetainedBytes:  session.MaximumRetainedBytes,
		MaximumFillPermille:   fill,
		SessionsReachingLimit: reached,
		LimitReachedCount:     session.LimitReachedCount,
		SegmentRotationCount:  session.SegmentRotationCount,
		ArchiveEvictedBytes:   session.ArchiveEvictedBytes,
	}
}

func readEmbeddedLogGrowth(t *testing.T, path string) analyze.LogGrowthSummary {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	const marker = `<script type="application/json" data-log-growth-json>`
	start := strings.Index(string(data), marker)
	if start < 0 {
		t.Fatal("embedded log-growth JSON is missing")
	}
	start += len(marker)
	end := strings.Index(string(data[start:]), `</script>`)
	if end < 0 {
		t.Fatal("embedded log-growth JSON is not terminated")
	}
	var summary analyze.LogGrowthSummary
	if err := json.Unmarshal(data[start:start+end], &summary); err != nil {
		t.Fatalf("decode embedded log-growth JSON: %v", err)
	}
	return summary
}

func TestStandaloneLeakReportsLinkExplorerAndRegistry(t *testing.T) {
	summary := analyze.Summary{
		Title: "leaks.jhlog",
		MemoryLeaks: []analyze.MemoryLeakSuspect{{
			ClassName:           "com.app.checkout.SuperLongCheckoutActivityNameThatMustWrapInsideGraphNode",
			Holder:              "CheckoutPresenter",
			Screen:              "Checkout",
			Operation:           "checkout.pay",
			Count:               1,
			MaxAgeMS:            45_000,
			EstimatedRetainedKB: 8192,
			DominatorPath: []string{
				"экран: Checkout",
				"сценарий: checkout.pay",
				"держатель: CheckoutPresenter",
				"удержанный объект: com.app.checkout.SuperLongCheckoutActivityNameThatMustWrapInsideGraphNode",
			},
			LeakChainSummary: "Удержан экран Checkout после destroy.",
			Severity:         "high",
			ObjectKind:       "экран / Activity",
			HolderQuality:    "вероятный держатель из контекста",
			Impact:           "Удержана Activity.",
			Recommendation:   "Очистите ссылки presenter-а на Activity.",
			Evidence:         "кол-во=1 · макс. возраст=45 сек",
		}},
	}

	dir := t.TempDir()
	inspectPath := filepath.Join(dir, "report-leaks.html")
	if err := WriteLeakInspectWithOptions(inspectPath, analyze.BuildLeakReport(summary), ReportOptions{Links: ReportLinks{Main: "report.html"}}); err != nil {
		t.Fatalf("WriteLeakInspectWithOptions() error = %v", err)
	}
	assertCurrentReportStyle(t, inspectPath)
	assertHTMLContains(
		t,
		inspectPath,
		`<details id="explorer" class="fold leak-report-fold" open>`,
		`<details id="registry" class="fold leak-report-fold" open>`,
		`data-leak-target="leak-1"`,
		`data-leak-row`,
		`role="tab"`,
		`linkedRows`,
		`scrollIntoView`,
		`tipCache`,
		`prepareLeakCardTables`,
		`cell.dataset.leakField = leakFieldForHeader`,
		`.leak-card-table .leak-card-row`,
		`.table-scroll.leak-card-scroll`,
		`.table-scroll.leak-card-scroll > table.leak-card-table`,
		`.leak-graph-panel[hidden]`,
		`class="node-title"`,
		"Контекст обнаружения удержанного объекта",
		"leak-graph-scroll",
		"leak-arrow-inspect-leak-1",
		`href="report.html"`,
		"← Обзор",
	)

	comparePath := filepath.Join(dir, "compare-leaks.html")
	if err := WriteLeakCompareWithOptions(comparePath, analyze.BuildLeakCompareReport(analyze.Compare(analyze.Summary{}, summary)), ReportOptions{Links: ReportLinks{Main: "compare.html"}}); err != nil {
		t.Fatalf("WriteLeakCompareWithOptions() error = %v", err)
	}
	assertCurrentReportStyle(t, comparePath)
	assertHTMLContains(
		t,
		comparePath,
		`<details id="explorer" class="fold leak-report-fold" open>`,
		`<details id="registry" class="fold leak-report-fold" open>`,
		`data-leak-target="leak-delta-0"`,
		`data-leak-row`,
		`href="compare.html"`,
		"← Обзор",
	)
}

func TestLeakReportsDeferRegistryRowsWithoutTruncatingData(t *testing.T) {
	const total = 300
	graph := analyze.LeakGraph{
		Title:    "Контекст удержания",
		RootID:   "target",
		TargetID: "target",
		Nodes: []analyze.LeakGraphNode{{
			ID: "target", Label: "удержанный объект", Detail: "проверить", Kind: "target",
		}},
	}
	items := make([]analyze.LeakReportItem, 0, total)
	deltas := make([]analyze.LeakDelta, 0, total)
	for index := range total {
		suspect := analyze.MemoryLeakSuspect{
			ClassName:      fmt.Sprintf("com.app.LeakClass%02d", index),
			Holder:         fmt.Sprintf("com.app.Holder%02d", index),
			Count:          1,
			Score:          float64(total - index),
			Severity:       "medium",
			ObjectKind:     "object",
			Recommendation: "Проверить время жизни объекта.",
		}
		items = append(items, analyze.LeakReportItem{
			Rank: index + 1, Suspect: suspect, Graph: graph,
		})
		deltas = append(deltas, analyze.LeakDelta{
			Status:         analyze.LeakDeltaSame,
			StatusLabel:    "Без изменений",
			Severity:       "medium",
			HasCandidate:   true,
			Candidate:      suspect,
			Graph:          graph,
			ScoreAfter:     suspect.Score,
			Recommendation: suspect.Recommendation,
		})
	}

	dir := t.TempDir()
	tests := []struct {
		name            string
		path            string
		write           func(string) error
		panelTargetTail string
		expectedNotice  string
	}{
		{
			name: "inspect",
			path: filepath.Join(dir, "inspect-leaks.html"),
			write: func(path string) error {
				return WriteLeakInspectWithOptions(
					path,
					analyze.LeakReport{Items: items},
					ReportOptions{},
				)
			},
			panelTargetTail: `data-leak-target="leak-300"`,
			expectedNotice:  "Интерактивные графы удержаний:</strong> показано 24 из 300",
		},
		{
			name: "compare",
			path: filepath.Join(dir, "compare-leaks.html"),
			write: func(path string) error {
				return WriteLeakCompareWithOptions(
					path,
					analyze.LeakCompareReport{Deltas: deltas},
					ReportOptions{},
				)
			},
			panelTargetTail: `data-leak-target="leak-delta-299"`,
			expectedNotice:  "Интерактивные графы дельт:</strong> показано 24 из 300",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.write(test.path); err != nil {
				t.Fatalf("write leak report: %v", err)
			}
			data, err := os.ReadFile(test.path)
			if err != nil {
				t.Fatal(err)
			}
			html := string(data)
			payloadIndex := strings.Index(html, `<script type="application/json" data-table-chunk`)
			if payloadIndex < 0 {
				t.Fatal("deferred registry payload is missing")
			}
			initialHTML := html[:payloadIndex]
			for marker, want := range map[string]int{
				`<button type="button" data-leak-select`:   24,
				`class="leak-graph-panel" data-leak-panel`: 24,
				`<svg class="leak-graph-svg"`:              24,
				`<tr data-code-problem-row`:                300,
				`<tr data-code-problem-row data-leak-row`:  24,
			} {
				if got := strings.Count(html, marker); got != want {
					t.Fatalf("%s count = %d, want %d", marker, got, want)
				}
			}
			if !strings.Contains(html, test.expectedNotice) {
				t.Fatalf("truncation notice missing: %q", test.expectedNotice)
			}
			if got := strings.Count(initialHTML, `<tr data-code-problem-row`); got != 50 {
				t.Fatalf("initial registry rows = %d, want 50", got)
			}
			for _, marker := range []string{`data-deferred-total="300"`, "Показать ещё 50", "Осталось строк: 250"} {
				if !strings.Contains(html, marker) {
					t.Fatalf("deferred registry control missing %q", marker)
				}
			}
			if !strings.Contains(html, "com.app.LeakClass299") {
				t.Fatal("tail leak disappeared from the deferred payload")
			}
			if strings.Contains(html, test.panelTargetTail) {
				t.Fatalf("tail registry row still links to a missing explorer panel: %s", test.panelTargetTail)
			}
		})
	}
}

func TestCompareReportPreservesAllLogGroupsAndDefersCompleteHighCardinalityTables(t *testing.T) {
	const (
		routeTotal  = 70
		screenTotal = 70
		gaugeTotal  = 130
	)
	summary := analyze.Summary{}
	for index := range routeTotal {
		summary.Routes = append(summary.Routes, analyze.RouteStats{
			Route: fmt.Sprintf("GET /bounded/route-%03d", index),
			P95MS: uint64(routeTotal - index),
		})
	}
	for index := range screenTotal {
		summary.Screens = append(summary.Screens, analyze.ScreenStats{
			Screen:      fmt.Sprintf("BoundedScreen%03d", index),
			JankRatePct: float64(screenTotal - index),
		})
	}
	for index := range gaugeTotal {
		summary.Gauges = append(summary.Gauges, analyze.NamedValue{
			Name:  fmt.Sprintf("bounded.gauge.%03d", index),
			Value: uint64(gaugeTotal - index),
		})
	}
	logs := func(prefix string, count int) []LogReport {
		out := make([]LogReport, 0, count)
		for index := range count {
			logSummary := analyze.Summary{}
			if index == 0 {
				logSummary = summary
			}
			out = append(out, LogReport{
				Name:    fmt.Sprintf("%s-%02d.jhlog", prefix, index),
				Anchor:  fmt.Sprintf("%s-log-%02d", prefix, index),
				Summary: logSummary,
			})
		}
		return out
	}

	path := filepath.Join(t.TempDir(), "compare.html")
	if err := WriteCompareReportWithOptions(
		path,
		analyze.Comparison{Baseline: summary, Candidate: summary},
		logs("baseline", 15),
		logs("candidate", 14),
		ReportOptions{},
	); err != nil {
		t.Fatalf("WriteCompareReportWithOptions() error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	html := string(data)
	if got := strings.Count(html, `class="log-card"`); got != 29 {
		t.Fatalf("log-card count = %d, want 29", got)
	}
	for _, want := range []string{
		`data-deferred-total="70"`,
		`data-deferred-total="131"`,
		"Показать ещё 20",
		"baseline-14.jhlog",
		"candidate-13.jhlog",
		"GET /bounded/route-069",
		"BoundedScreen069",
		"bounded.gauge.129",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("bounded compare report does not contain %q", want)
		}
	}
	for _, unwanted := range []string{
		"исходных логов; объединённые метрики",
		"Сравнение маршрутов:</strong> показано 64 из 70",
		"Сравнение экранов:</strong> показано 64 из 70",
		"Gauge-метрики этого лога:</strong> показано 128 из 130",
	} {
		if strings.Contains(html, unwanted) {
			t.Fatalf("bounded compare report unexpectedly contains %q", unwanted)
		}
	}
}

func TestCodeProblemReportsPreserveEverySignalAndDrillDown(t *testing.T) {
	problem := analyze.CodeProblemStats{
		ClassName:      "com.app.CompleteEvidence",
		Score:          10,
		Severity:       "high",
		Categories:     []string{"performance"},
		Recommendation: "Inspect every item.",
	}
	for index := range 7 {
		problem.Signals = append(problem.Signals, analyze.CodeProblemSignal{
			Name:     fmt.Sprintf("complete-signal-%d", index),
			Category: "performance",
			Severity: "high",
			Detail:   fmt.Sprintf("complete signal detail %d", index),
		})
	}
	for index := range 6 {
		problem.DrillDown = append(problem.DrillDown, analyze.CodeProblemDrillDown{
			ClassName:      problem.ClassName,
			Operation:      fmt.Sprintf("complete-operation-%d", index),
			Evidence:       fmt.Sprintf("complete evidence %d", index),
			Recommendation: "Inspect this flow.",
		})
	}
	summary := analyze.Summary{CodeProblems: []analyze.CodeProblemStats{problem}}
	comparison := analyze.Compare(summary, summary)
	dir := t.TempDir()
	tests := []struct {
		name  string
		write func(string) error
	}{
		{name: "inspect", write: func(path string) error {
			return WriteInspectWithOptions(path, summary, ReportOptions{})
		}},
		{name: "compare", write: func(path string) error {
			return WriteCompareReportWithOptions(path, comparison, nil, nil, ReportOptions{})
		}},
		{name: "math-inspect", write: func(path string) error {
			return WriteMathInspectWithOptions(path, sampleMathReport(summary), ReportOptions{})
		}},
		{name: "math-compare", write: func(path string) error {
			return WriteMathCompareWithOptions(path, sampleCompareMathReport(comparison, summary), ReportOptions{})
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(dir, test.name+".html")
			if err := test.write(path); err != nil {
				t.Fatalf("write report: %v", err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			html := string(data)
			archived := decodeCodeProblemEvidenceArchive(t, html)
			if len(archived) != 1 {
				t.Fatalf("archived code problems = %d, want 1", len(archived))
			}
			encodedEvidence, err := json.Marshal(archived[0])
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"complete-signal-6", "complete-operation-5", "complete evidence 5"} {
				if !strings.Contains(string(encodedEvidence), want) {
					t.Fatalf("complete report does not contain tail evidence %q", want)
				}
			}
			for _, unwanted := range []string{"Сценарии этой строки:</strong> показано", "Сигналы этой строки:</strong> показано"} {
				if strings.Contains(html, unwanted) {
					t.Fatalf("complete report unexpectedly truncates evidence with %q", unwanted)
				}
			}
		})
	}
}

func TestCodeProblemReportKeepsHighCardinalityRegistryInCompressedArchive(t *testing.T) {
	const total = 75
	problems := make([]analyze.CodeProblemStats, 0, total)
	for index := range total {
		problems = append(problems, analyze.CodeProblemStats{
			ClassName:      fmt.Sprintf("com.app.ArchivedProblem%03d", index),
			Score:          float64(total - index),
			Severity:       "medium",
			Categories:     []string{"performance"},
			Evidence:       fmt.Sprintf("archived evidence %03d", index),
			Recommendation: "Inspect the archived evidence.",
		})
	}
	path := os.Getenv("JH_DEFERRED_SEARCH_OUT")
	if path == "" {
		path = filepath.Join(t.TempDir(), "inspect.html")
	}
	if err := WriteInspectWithOptions(path, analyze.Summary{CodeProblems: problems}, ReportOptions{}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	document := string(data)
	if got := strings.Count(document, `<tr data-code-problem-row `); got != 50 {
		t.Fatalf("initial code problem rows = %d, want 50", got)
	}
	for _, marker := range []string{`data-code-problem-loader`, `data-deferred-total="75"`, `data-deferred-loaded="50"`} {
		if !strings.Contains(document, marker) {
			t.Fatalf("high-cardinality registry is missing %q", marker)
		}
	}
	for _, marker := range []string{
		"indexDeferredScript",
		"archivedCodeSearchEntries",
		"Ищу по всем строкам подробных разделов",
		"dataset.reportSearchId",
		"revealDeferredSearchEntry",
	} {
		if !strings.Contains(document, marker) {
			t.Fatalf("full-report search is missing deferred-row support %q", marker)
		}
	}
	archived := decodeCodeProblemEvidenceArchive(t, document)
	tail := ""
	if len(archived) > 0 {
		tail = archived[len(archived)-1].Evidence
	}
	if len(archived) != total || tail != "archived evidence 074" {
		t.Fatalf("archived registry is incomplete: count=%d tail=%q", len(archived), tail)
	}
}

func decodeCodeProblemEvidenceArchive(t *testing.T, document string) []analyze.CodeProblemStats {
	t.Helper()
	const marker = `data-code-problem-evidence-archive data-encoding="gzip-base64url">`
	start := strings.Index(document, marker)
	if start < 0 {
		t.Fatal("code problem evidence archive is missing")
	}
	start += len(marker)
	end := strings.Index(document[start:], "</script>")
	if end < 0 {
		t.Fatal("code problem evidence archive is unterminated")
	}
	encoded := strings.TrimSpace(document[start : start+end])
	if strings.HasPrefix(encoded, `"`) {
		if err := json.Unmarshal([]byte(encoded), &encoded); err != nil {
			t.Fatalf("parse encoded code problem archive: %v", err)
		}
	}
	decoded := base64.NewDecoder(base64.RawURLEncoding, strings.NewReader(encoded))
	compressed, err := gzip.NewReader(decoded)
	if err != nil {
		t.Fatalf("open code problem evidence archive: %v (prefix %q)", err, encoded[:min(len(encoded), 96)])
	}
	payload, readErr := io.ReadAll(compressed)
	closeErr := compressed.Close()
	if readErr != nil || closeErr != nil {
		t.Fatalf("decode code problem evidence archive: read=%v close=%v (prefix %q)", readErr, closeErr, encoded[:min(len(encoded), 96)])
	}
	var archive codeProblemEvidencePayload
	if err := json.Unmarshal(payload, &archive); err != nil {
		t.Fatalf("parse code problem evidence archive: %v", err)
	}
	if archive.Mode == "compare" {
		problems := make([]analyze.CodeProblemStats, 0, len(archive.Rows))
		for _, row := range archive.Rows {
			problems = append(problems, row.Candidate)
		}
		return problems
	}
	return archive.Problems
}

func TestCodeProblemCategoryOptions(t *testing.T) {
	html := string(codeProblemCategoryOptions([]analyze.CodeProblemStats{
		{Categories: []string{"Новая категория"}},
		{Categories: []string{"Сеть"}},
	}))
	if got, want := strings.Count(html, "<option "), len(codeProblemCategoryFilterOptions)+1; got != want {
		t.Fatalf("option count = %d, want %d", got, want)
	}
	for _, category := range codeProblemCategoryFilterOptions {
		want := `<option value="` + category + `">` + category + `</option>`
		if !strings.Contains(html, want) {
			t.Fatalf("missing category option %q in %s", category, html)
		}
	}
	if !strings.Contains(html, `<option value="Новая категория">Новая категория</option>`) {
		t.Fatalf("missing dynamic category option in %s", html)
	}
}

func TestLeakObjectKindOptions(t *testing.T) {
	options := leakObjectKindOptions()
	if len(options) != len(leakObjectKindFilterOptions) {
		t.Fatalf("option count = %d, want %d", len(options), len(leakObjectKindFilterOptions))
	}
	if got := leakObjectKindLabel("экран / Activity"); got != "Экран / Activity" {
		t.Fatalf("leakObjectKindLabel() = %q", got)
	}
}

func TestSignalContextLabelHidesUnknownParts(t *testing.T) {
	if got, want := signalContextLabel("unknown", "unknown", "unknown"), "контекст не задан"; got != want {
		t.Fatalf("signalContextLabel(all unknown) = %q, want %q", got, want)
	}
	if got, want := signalContextLabel("Feed", "Feed", "FeedOwner"), "Feed / FeedOwner"; got != want {
		t.Fatalf("signalContextLabel(deduplicated) = %q, want %q", got, want)
	}
	if got, want := reportValue("unknown unknown", "нет данных"), "нет данных"; got != want {
		t.Fatalf("reportValue(unknown unknown) = %q, want %q", got, want)
	}
	if got := string(contextValueHint("unknown", "screen")); !strings.Contains(got, "жизненного цикла Activity") {
		t.Fatalf("contextValueHint(screen) = %q, want Activity lifecycle hint", got)
	}
	if got := string(signalContextLabelHint("unknown", "unknown", "unknown")); !strings.Contains(got, "@JankHunterOperation") {
		t.Fatalf("signalContextLabelHint(all unknown) = %q, want attribution hint", got)
	}
}

func TestProblemLocationTextHidesUnknownAndMergesRepeatedContext(t *testing.T) {
	locations := []analyze.ProblemLocation{
		{Route: "POST /omicron", Owner: "unknown"},
		{
			Screen:    "ru.mail.im.registration.ui.RegistrationActivity",
			Operation: "unknown",
			Route:     "POST /omicron",
			Owner:     "unknown",
		},
	}

	got := problemLocationText(locations)
	want := "экран ru.mail.im.registration.ui.RegistrationActivity · маршрут POST /omicron"
	if got != want {
		t.Fatalf("problemLocationText() = %q, want %q", got, want)
	}
	if strings.Contains(got, "unknown") || strings.Contains(got, "→") {
		t.Fatalf("problemLocationText() contains a placeholder or a misleading chain: %q", got)
	}
}

func TestProblemLocationTextPreservesDistinctKnownLocations(t *testing.T) {
	locations := []analyze.ProblemLocation{
		{Screen: "FeedActivity", Route: "GET /feed"},
		{Screen: "SearchActivity", Route: "GET /feed"},
	}

	got := problemLocationText(locations)
	want := "экран FeedActivity · маршрут GET /feed; экран SearchActivity · маршрут GET /feed"
	if got != want {
		t.Fatalf("problemLocationText() = %q, want %q", got, want)
	}
	if got := problemLocationText([]analyze.ProblemLocation{{Screen: "unknown"}}); got != "точное место не определено" {
		t.Fatalf("problemLocationText(all unknown) = %q", got)
	}
}

func TestWriteReportsHideUnknownPlaceholders(t *testing.T) {
	summary := analyze.Summary{
		Title:      "sample.jhlog",
		LogCount:   1,
		EventCount: 1,
		Environment: analyze.RunEnvironment{
			Title:    "unknown",
			Subtitle: "unknown build",
			Items: []analyze.InfoItem{
				{Label: "Устройство", Value: "unknown", Detail: "unknown unknown"},
			},
		},
		SignalContexts: []analyze.SignalContextStats{
			{Screen: "unknown", Operation: "unknown", Owner: "unknown", RouteSample: "unknown", ProblemCount: 1},
		},
		LogSpam: []analyze.LogSpamStats{
			{Screen: "unknown", Operation: "unknown", Owner: "unknown", Source: "unknown", Level: "warn", Count: 1},
		},
		ProblemWindows: []analyze.ProblemWindowStats{
			{Screen: "unknown", Operation: "unknown", Owner: "unknown", Kind: "ui_jank", Windows: 1, Count: 1, TotalWindowMS: 16, MaxMS: 16},
		},
		RuntimeCalls: []analyze.RuntimeCallStats{
			{Screen: "unknown", Operation: "unknown", Caller: "unknown", Callee: "unknown", Count: 1, TotalMS: 16, MaxMS: 16},
		},
		Owners: []analyze.OwnerStats{
			{Owner: "unknown", Kind: "handler", Count: 1, MaxMS: 16, StackHint: "unknown unknown"},
		},
		AppVersions: []analyze.NamedValue{{Name: "unknown", Value: 1}},
		Builds:      []analyze.NamedValue{{Name: "unknown build", Value: 1}},
		Devices:     []analyze.NamedValue{{Name: "unknown unknown", Value: 1}},
		SDKs:        []analyze.NamedValue{{Name: "unknown", Value: 1}},
		Processes:   []analyze.NamedValue{{Name: "unknown", Value: 1}},
		Network:     []analyze.NamedValue{{Name: "unknown", Value: 1}},
		Cohorts:     []analyze.NamedValue{{Name: "device=unknown app=unknown build=unknown", Value: 1}},
	}

	dir := t.TempDir()
	inspectPath := filepath.Join(dir, "inspect.html")
	if err := WriteInspectWithOptions(inspectPath, summary, ReportOptions{}); err != nil {
		t.Fatalf("WriteInspectWithOptions() error = %v", err)
	}
	assertHTMLContains(t, inspectPath, "неизвестное устройство", "контекст выполнения недоступен", "нет данных", "обратным вызовам жизненного цикла Activity", "@JankHunterOperation", "начале запуска")
	assertHTMLNotContains(t, inspectPath, "unknown unknown", "unknown build", ">unknown<", "<code>unknown</code>", `data-tip="unknown"`, "session-событием", "runtimeCallGraph instrumentation", "bytecode hook", "ограниченные runtime-реестры", "элементов evidence")

	comparePath := filepath.Join(dir, "compare.html")
	if err := WriteCompareReportWithOptions(
		comparePath,
		analyze.Compare(summary, summary),
		[]LogReport{{Name: "base.jhlog", Anchor: "baseline-log-1", Summary: summary}},
		[]LogReport{{Name: "candidate.jhlog", Anchor: "candidate-log-1", Summary: summary}},
		ReportOptions{},
	); err != nil {
		t.Fatalf("WriteCompareReportWithOptions() error = %v", err)
	}
	assertHTMLContains(t, comparePath, "неизвестная база", "неизвестный кандидат", "контекст недоступен", "нет данных")
	assertHTMLNotContains(t, comparePath, "unknown unknown", "unknown build", ">unknown<", "<code>unknown</code>")
}

func TestWriteReportsOnlyLinkGeneratedCompanions(t *testing.T) {
	t.Setenv("JH_LANG", "ru")
	dir := t.TempDir()
	summary := analyze.Summary{Title: "sample.jhlog", LogCount: 1, EventCount: 1}

	inspectPath := filepath.Join(dir, "inspect.html")
	if err := WriteInspectWithOptions(inspectPath, summary, ReportOptions{}); err != nil {
		t.Fatalf("WriteInspectWithOptions() error = %v", err)
	}
	assertHTMLNotContains(t, inspectPath, "λ Анализ", `href="inspect-math.html"`)

	comparePath := filepath.Join(dir, "compare.html")
	if err := WriteCompareReportWithOptions(comparePath, analyze.Compare(summary, summary), nil, nil, ReportOptions{}); err != nil {
		t.Fatalf("WriteCompareReportWithOptions() error = %v", err)
	}
	assertHTMLNotContains(t, comparePath, "λ Анализ", `href="compare-math.html"`)
}

func TestWriteReportsRussian(t *testing.T) {
	summary := analyze.Summary{
		Title:      "sample.jhlog",
		LogCount:   1,
		EventCount: 27,
		HTTPCount:  3,
		HTTPFailed: 1,
		HTTPP95MS:  612,
		UIFrames:   1122,
		UIJankPct:  8.02,
		UIAvgFPS:   56.1,
		Environment: analyze.RunEnvironment{
			Title:    "Pixel 8",
			Subtitle: "Android 15 · 0.1.0-debug (100) · процесс main",
			Items: []analyze.InfoItem{
				{Label: "Батарея", Value: "82%", Detail: "заряжается · 32.0 C"},
			},
		},
		Routes: []analyze.RouteStats{
			{Route: "GET /feed", Count: 2, P95MS: 612},
		},
	}
	attachProblemReport(t, &summary)

	dir := t.TempDir()
	inspectPath := filepath.Join(dir, "inspect-ru.html")
	if err := WriteInspectWithOptions(inspectPath, summary, ReportOptions{Links: ReportLinks{Math: "inspect-ru-math.html"}}); err != nil {
		t.Fatalf("WriteInspect() error = %v", err)
	}
	assertHTMLContains(t, inspectPath, `<html lang="ru">`, "Проблемы приложения", "Контекст устройства", "Батарея", "3 запроса, 1 ошибка", "Все записанные сетевые маршруты", "Эвристический итог", "Подробный анализ")
	assertHTMLNotContains(t, inspectPath, "Связанные сигналы")

	comparePath := filepath.Join(dir, "compare-ru.html")
	if err := WriteCompareReportWithOptions(
		comparePath,
		analyze.Compare(summary, summary),
		[]LogReport{{Name: "old/sample.jhlog", Anchor: "baseline-log-1", Summary: summary}},
		[]LogReport{{Name: "new/sample.jhlog", Anchor: "candidate-log-1", Summary: summary}},
		ReportOptions{Links: ReportLinks{Math: "compare-ru-math.html"}},
	); err != nil {
		t.Fatalf("WriteCompareReport() error = %v", err)
	}
	assertHTMLContains(t, comparePath, "Изменения проблем", "Матрица регрессий", "Где изменилось", "Сравнение связанных сигналов", "Детали по каждому журналу", "Эвристический итог", "Логи базы", "λ Анализ")
}

func attachProblemReport(t *testing.T, summary *analyze.Summary) {
	t.Helper()
	report, err := analyze.BuildProblemReport(*summary)
	if err != nil {
		t.Fatalf("BuildProblemReport() error = %v", err)
	}
	summary.ProblemSchemaVersion = analyze.ProblemSchemaVersion
	summary.ProblemSummary = report.Summary
	summary.Problems = report.Problems
	summary.ProblemIncidents = report.Incidents
	summary.CategoryCoverage = report.Coverage
	summary.Detectors = report.Registry
	summary.EvidenceQuality = analyze.BuildEvidenceQualityVector(*summary)
}

func TestCollectionWindowNoticeExplainsLateRuntimeEnablement(t *testing.T) {
	summary := analyze.Summary{
		DurationMS: 61_344,
		Counters: []analyze.NamedValue{{
			Name:  "jankhunter.runtime.enabled.reason.omicron.count",
			Value: 1,
		}},
		Gauges: []analyze.NamedValue{{
			Name:  "jankhunter.runtime.collection_inactive_before_start_ms",
			Value: 9 * 60_000,
		}},
	}
	got := collectionWindowNotice(summary)
	for _, expected := range []string{"9 мин", "omicron", "1 мин 1 сек"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("collectionWindowNotice() = %q, missing %q", got, expected)
		}
	}
}

func TestPrimaryCategoryCoverageHidesUnknownZeroes(t *testing.T) {
	items := []analyze.CategoryCoverage{
		{Category: analyze.ProblemCategoryNetwork, Label: "Сеть", Status: "problems_found", FindingCount: 2},
		{Category: analyze.ProblemCategoryUI, Label: "UI", Status: "healthy"},
		{Category: analyze.ProblemCategoryIO, Label: "I/O", Status: "not_measured"},
		{Category: analyze.ProblemCategoryDependencyInjection, Label: "DI", Status: "healthy"},
	}
	if got := primaryCategoryCoverage(items); len(got) != 3 {
		t.Fatalf("primaryCategoryCoverage() = %+v, want problems and healthy categories", got)
	}
	if got := findingCategoryCoverage(items); len(got) != 2 || got[1].Category != analyze.ProblemCategoryDependencyInjection {
		t.Fatalf("findingCategoryCoverage() = %+v, want DI whenever analysis is enabled", got)
	}
	if got := hiddenCoverageSummary(items); !strings.Contains(got, "I/O") || strings.Contains(got, "Сеть") {
		t.Fatalf("hiddenCoverageSummary() = %q", got)
	}
}

func TestInspectMathHeuristicExplainsMissingOwnerInsteadOfShowingUnknown(t *testing.T) {
	summary := inspectMathHeuristic(mathanalysis.MathReport{
		CausalGraph: mathanalysis.CausalGraph{OwnerScores: []mathanalysis.OwnerBlameScore{{
			Owner: "unknown",
			Score: 3.5,
		}}},
	})

	if len(summary.Cards) == 0 {
		t.Fatal("inspectMathHeuristic() returned no cards")
	}
	detail := strings.ToLower(summary.Cards[0].Detail)
	if strings.Contains(detail, "unknown") {
		t.Fatalf("missing owner leaked as unknown: %s", detail)
	}
	for _, expected := range []string{"место запуска не записано", "инструментирован", "пакет"} {
		if !strings.Contains(detail, expected) {
			t.Fatalf("missing owner explanation lacks %q: %s", expected, detail)
		}
	}
}

func TestPathsForKeepsPrimarySuffixOpaque(t *testing.T) {
	paths := PathsFor("/tmp/report-math.html")
	if paths.Main != "/tmp/report-math.html" ||
		paths.Math != "/tmp/report-math-math.html" ||
		paths.Leaks != "/tmp/report-math-leaks.html" ||
		paths.Influence != "/tmp/report-math-influence.html" {
		t.Fatalf("PathsFor() = %+v", paths)
	}
	if got := paths.MainLink().Main; got != "report-math.html" {
		t.Fatalf("MainLink() = %q", got)
	}
}

func TestCachedReportTemplateReturnsSameParsedInstance(t *testing.T) {
	first, err := cachedInspectTemplate.parsed()
	if err != nil {
		t.Fatal(err)
	}
	second, err := cachedInspectTemplate.parsed()
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("inspect template was parsed more than once")
	}
}

func TestSaturatingSignedDeltaCoversFullUint64Domain(t *testing.T) {
	maxUint64 := ^uint64(0)
	tests := []struct {
		name          string
		after         uint64
		before        uint64
		want          int64
		wantMagnitude uint64
	}{
		{name: "positive", after: 9, before: 4, want: 5, wantMagnitude: 5},
		{name: "negative", after: 4, before: 9, want: -5, wantMagnitude: 5},
		{name: "positive saturation", after: maxUint64, want: maxSignedInt64, wantMagnitude: uint64(maxSignedInt64)},
		{name: "negative saturation", before: maxUint64, want: minSignedInt64, wantMagnitude: uint64(maxSignedInt64) + 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := saturatingSignedDelta(test.after, test.before)
			if got != test.want {
				t.Fatalf("saturatingSignedDelta(%d, %d) = %d, want %d", test.after, test.before, got, test.want)
			}
			if magnitude := int64Magnitude(got); magnitude != test.wantMagnitude {
				t.Fatalf("int64Magnitude(%d) = %d, want %d", got, magnitude, test.wantMagnitude)
			}
		})
	}
}

func sampleInfluence() analyze.InfluenceSummary {
	return analyze.InfluenceSummary{
		Available:       true,
		HasClassGraph:   true,
		HasRuntimeGraph: true,
		RuntimeNodes:    1,
		RuntimeEdges:    1,
		StaticNodes:     2,
		StaticEdges:     1,
		ShownNodes:      2,
		ShownEdges:      1,
		TopNodes: []analyze.InfluenceNode{{
			ClassName:       "com.app.data.CheckoutRepository",
			Label:           "data.CheckoutRepository",
			Score:           14.2,
			Severity:        "high",
			Status:          "runtime",
			RuntimeEvidence: true,
			Problems:        2,
			NetworkMS:       900,
			Reasons:         []string{"сетевые задержки", "проблемные окна"},
			Operations:      []string{"checkout.open"},
		}},
		TopEdges: []analyze.InfluenceEdge{{
			From:             "com.app.feature.CheckoutPresenter",
			To:               "com.app.data.CheckoutRepository",
			Count:            3,
			Influence:        42,
			RuntimeConfirmed: true,
			Reason:           "вызывает узел с проблемами выполнения",
		}},
		HotPaths: []analyze.InfluencePath{{
			Nodes:         []string{"com.app.feature.CheckoutPresenter", "com.app.data.CheckoutRepository"},
			Weight:        10.4,
			RuntimeTarget: true,
			Reason:        "ведет к классу с симптомами выполнения",
		}},
		MethodHotspots: []analyze.InfluenceMethod{{
			ClassName:      "com.app.data.CheckoutRepository",
			Method:         "load",
			Role:           "callee",
			Count:          3,
			Weight:         9.5,
			RuntimeTouched: true,
		}},
		Heuristic: []analyze.InfluenceFinding{{
			Severity: "high",
			Title:    "Главный узел влияния",
			Detail:   "CheckoutRepository.",
		}},
	}
}

func sampleInstrumentationDiagnostics() analyze.InstrumentationDiagnostics {
	return analyze.InstrumentationDiagnostics{
		Available:            true,
		Source:               "instrumentation-diagnostics.jsonl",
		ClassCount:           2,
		MethodCount:          5,
		IgnoredMethodCount:   1,
		AnnotatedMethodCount: 1,
		HookCount:            3,
		SkippedMethods: []analyze.InstrumentationSkippedSummary{
			{Reason: "constructor", Count: 2},
		},
		Hooks: []analyze.InstrumentationHookSummary{
			{Intent: "okhttp.install_event_listener_factory", Signature: "okhttp3.builder.build.v3", Bridge: "okhttp3.bridge.v3", Method: "client()V", Count: 2},
			{Intent: "logspam.android.util.Log.d", Signature: "logspam.android.util.Log.d", Method: "load()V", Count: 1},
		},
		Decisions: []analyze.InstrumentationDecisionSummary{
			{Kind: "unsupported", Module: "okhttp", Family: "okhttp", Reason: "unsupported_signature", Method: "client()V", Count: 2},
			{Kind: "warning", Module: "class_hierarchy", Family: "metadata", Reason: "metadata_load_failed", Method: "broken.Parent", Detail: "java.lang.IllegalStateException: invalid class metadata", Count: 1},
		},
		Annotations: []analyze.InstrumentationAnnotationSummary{
			{Owner: "FeedOwner", Screen: "Feed", Operation: "feed.open", OperationKind: "navigation", OperationBudgetMS: 800, Count: 1},
		},
		Classes: []analyze.InstrumentationClassDiagnostic{
			{
				ClassName:        "com.app.FeedRepository",
				Methods:          3,
				AnnotatedMethods: 1,
				HookCount:        2,
				Hooks: []analyze.InstrumentationHookSummary{
					{Intent: "okhttp.install_event_listener_factory", Signature: "okhttp3.builder.build.v3", Bridge: "okhttp3.bridge.v3", Method: "client()V", Count: 2},
				},
				Annotations: []analyze.InstrumentationAnnotationSummary{
					{Owner: "FeedOwner", Screen: "Feed", Operation: "feed.open", OperationKind: "navigation", OperationBudgetMS: 800, Count: 1},
				},
			},
		},
		Warnings: []string{
			"Иерархия broken.Parent разрешена частично: AGP не смог прочитать метаданные класса.",
		},
	}
}

func TestDependencyInjectionReportDefersPresentationRows(t *testing.T) {
	report := sampleDependencyInjectionReport()
	report.Classes = make([]analyze.DependencyInjectionReportClass, 300)
	for index := range report.Classes {
		report.Classes[index].DependencyInjectionClass = analyze.DependencyInjectionClass{
			Name:      fmt.Sprintf("com.app.Class%03d", index),
			Framework: "hilt",
		}
	}
	report.Edges = make([]analyze.DependencyInjectionReportEdge, 600)
	for index := range report.Edges {
		report.Edges[index].DependencyInjectionEdge = analyze.DependencyInjectionEdge{
			Consumer:   fmt.Sprintf("com.app.Consumer%03d", index),
			Dependency: fmt.Sprintf("com.app.Dependency%03d", index),
			Framework:  "hilt",
		}
	}

	path := filepath.Join(t.TempDir(), "inspect-di.html")
	if err := WriteDependencyInjectionWithOptions(path, report, ReportOptions{}); err != nil {
		t.Fatalf("WriteDependencyInjectionWithOptions() error = %v", err)
	}
	renderedBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	rendered := string(renderedBytes)
	if got := strings.Count(rendered, `class="di-class-row"`); got != 50 {
		t.Fatalf("initial DI classes = %d, want 50", got)
	}
	if got := strings.Count(rendered, `class="di-edge-row"`); got != 50 {
		t.Fatalf("initial DI edges = %d, want 50", got)
	}
	assertHTMLContains(
		t,
		path,
		`data-deferred-total="300"`,
		`data-deferred-total="600"`,
		"Показать ещё 50",
		"com.app.Class299",
		"com.app.Dependency599",
	)
	if strings.Contains(rendered, "--json") {
		t.Fatal("DI truncation notice must not claim that inspect --json contains the source catalog")
	}
}

func sampleDependencyInjectionReport() analyze.DependencyInjectionReport {
	return analyze.DependencyInjectionReport{
		Available:  true,
		Source:     "di-catalog.jsonl",
		Variant:    "debug",
		Disclaimer: analyze.DependencyInjectionDisclaimer,
		ClassCount: 2,
		EdgeCount:  1,
		Frameworks: []analyze.DependencyInjectionFrameworkSummary{
			{Name: "hilt", Classes: 2, Edges: 1},
		},
		Classes: []analyze.DependencyInjectionReportClass{
			{
				DependencyInjectionClass: analyze.DependencyInjectionClass{
					Name:       "com.app.FeedViewModel",
					Framework:  "hilt",
					Roles:      []string{"consumer"},
					Components: []string{"dagger.hilt.components.SingletonComponent"},
				},
				Observed: []string{"есть отдельный runtime-сигнал"},
			},
			{
				DependencyInjectionClass: analyze.DependencyInjectionClass{
					Name:      "com.app.FeedViewModel_Factory",
					Framework: "hilt",
					Roles:     []string{"factory"},
					Generated: true,
				},
			},
		},
		Edges: []analyze.DependencyInjectionReportEdge{
			{
				DependencyInjectionEdge: analyze.DependencyInjectionEdge{
					Consumer:      "com.app.FeedViewModel",
					Dependency:    "com.app.FeedRepository",
					Framework:     "hilt",
					InjectionKind: "generated_factory",
					Site:          "com.app.FeedViewModel_Factory#newInstance(Lcom/app/FeedRepository;)Lcom/app/FeedViewModel;",
					Resolution:    "generated_confirmed",
				},
				ConsumerObserved: true,
			},
		},
	}
}

func sampleCollectionQuality() analyze.CollectionQuality {
	return analyze.CollectionQuality{
		Level:                             "medium",
		DiagnosticCompletenessPercent:     80,
		DiagnosticCompletenessModel:       "diagnostic-completeness-v1:active-components-normalized;transport=40,runtime_graph=20,process_roster=20,integrity=20",
		DiagnosticCompletenessLevel:       "sufficient",
		DiagnosticCompletenessExplanation: "Достаточная полнота: основные evidence подтверждены с ограничениями.",
		RuntimeGraphEnabled:               false,
		DiagnosticCompletenessComponents: []analyze.DiagnosticCompletenessComponent{
			{ID: "transport", Label: "Доставка событий", Weight: 40, CoveragePercent: 100, EarnedPoints: 40, Explanation: "известных потерь нет"},
			{ID: "runtime_graph", Label: "Граф вызовов во время выполнения", Weight: 20, Excluded: true, Explanation: "Граф вызовов во время выполнения отключён и не входит в индекс"},
			{ID: "process_roster", Label: "Охват процессов", Weight: 20, CoveragePercent: 100, EarnedPoints: 20, Explanation: "configured scope подтверждён"},
			{ID: "integrity", Label: "Целостность доказательств", Weight: 20, CoveragePercent: 100, EarnedPoints: 20, Explanation: "инварианты согласованы"},
		},
	}
}

func sampleMathReport(summary analyze.Summary) mathanalysis.MathReport {
	return mathanalysis.MathReport{
		Title:       "sample.jhlog",
		SourcePaths: []string{"sample.jhlog"},
		Summary:     summary,
		Findings: []mathanalysis.Finding{{
			Severity: "ok",
			Title:    "Данных достаточно",
			Detail:   "Математический отчет рассчитан по доступным событиям.",
		}},
		Sections: []mathanalysis.MathSection{
			{ID: "quality", Title: "Качество данных", Status: "ok", Summary: "Сводка качества данных."},
			{ID: "timeline", Title: "Таймлайн сигналов", Status: "ok", Summary: "Сводка таймлайна."},
			{ID: "robust", Title: "Робастная статистика", Status: "ok", Summary: "Сводка распределений."},
			{ID: "change-points", Title: "Точки изменения", Status: "ok", Summary: "Сводка сдвигов."},
			{ID: "periodic", Title: "Периодические сигналы", Status: "ok", Summary: "Подтвержденных повторяемых паттернов нет."},
			{ID: "network-loops", Title: "Сетевые циклы", Status: "pending", Summary: "Каркас детектора сетевых циклов."},
			{ID: "integral", Title: "Интегральная нагрузка", Status: "medium", Summary: "Накопленные оценки рассчитаны по доступным интервалам."},
			{ID: "markov", Title: "Марковская модель состояний", Status: "medium", Summary: "Сводка марковских переходов."},
			{ID: "graph", Title: "Граф связей и гипотез", Status: "ok", Summary: "Сводка статистических связей."},
		},
		Timeline: []mathanalysis.TimelineBucket{
			{StartMS: 0, EndMS: 1000},
			{StartMS: 1000, EndMS: 2000, HTTPCount: 2, HTTPP95DurationMS: 612, UIFrames: 90, UIJankyFrames: 7},
		},
		RobustStats: []mathanalysis.RobustStat{{
			Dimension:             "Маршрут",
			Name:                  "GET /feed",
			Metric:                "HTTP задержка",
			Unit:                  "мс",
			Count:                 10,
			Median:                180,
			P90:                   420,
			P95:                   520,
			P99:                   610,
			MAD:                   45,
			TrimmedMean:           230,
			Min:                   90,
			Max:                   640,
			SampleQuality:         "достаточная",
			SampleQualitySeverity: "ok",
		}},
		Periodic: []mathanalysis.PeriodicSignal{
			{Signal: "HTTP запросы", Unit: "шт", BucketMS: 1_000, SampleCount: 12, TotalBucketCount: 16, ObservedBucketCount: 14, AnalyzedSampleCount: 12, AnalysisBucketMS: 1_000, Status: "ok", Summary: "Повторяемый цикл не подтвержден."},
		},
		IntegralScores: []mathanalysis.IntegralScore{
			{
				ID:          "latency_pain_area",
				Title:       "Площадь сетевой задержки",
				Formula:     "Σ max(0, HTTP p95 - 300ms) * Δt",
				Explanation: "Интегрирует хвост задержки выше порога.",
				Unit:        "мс*с",
				Value:       620,
				Severity:    "medium",
				Summary:     "Площадь сетевой задержки: 620 мс*с.",
			},
		},
		Markov: mathanalysis.MarkovModel{
			SampleCount:             3,
			TimelineBucketCount:     3,
			ObservationCoverage:     1,
			TransitionEventCount:    2,
			BadEpisodeCount:         1,
			SequenceComparable:      true,
			Confidence:              "medium",
			ConfidenceReason:        "окон=3, плохих эпизодов=1: восстановление и липкость лучше подтвердить повтором",
			HealthyToBadCount:       1,
			BadToHealthyProbability: 1,
			HasRecoveryProbability:  true,
			ExpectedRecoveryWindows: 1,
			ExpectedRecoveryMS:      1000,
			HasExpectedRecovery:     true,
			TotalDurationMS:         3000,
			BadStateDurationMS:      1000,
			BadStateExposure:        1.0 / 3.0,
			States: []mathanalysis.MarkovBucketState{
				{TimeMS: 0, DurationMS: 1000, State: "Healthy", Reason: "нет выраженной деградации"},
				{
					TimeMS:     1000,
					DurationMS: 1000,
					State:      "NetworkSlow",
					Reason:     "HTTP p95 612 мс",
					Contributors: []mathanalysis.MarkovSymptomWeight{
						{State: "NetworkSlow", Weight: 0.6, Reason: "HTTP p95 612 мс"},
						{State: "Janky", Weight: 0.35, Reason: "доля подтормаживаний 7.8%"},
					},
					Route: "GET /feed",
					Owner: "FeedRepository.refresh",
				},
				{TimeMS: 2000, DurationMS: 1000, State: "Recovering", Reason: "первое спокойное окно после деградации"},
			},
			Transitions: []mathanalysis.MarkovTransition{
				{From: "Healthy", To: "NetworkSlow", Count: 1, Probability: 1},
				{From: "NetworkSlow", To: "Recovering", Count: 1, Probability: 1},
			},
			StateExposures: []mathanalysis.MarkovStateExposure{
				{State: "NetworkSlow", Windows: 1, DurationMS: 1000, Exposure: 1.0 / 3.0},
			},
			StickyStates: []mathanalysis.MarkovStickyState{
				{State: "NetworkSlow", Count: 1, Probability: 0.5},
			},
			ContextStickyStates: []mathanalysis.MarkovContextStickyState{
				{State: "NetworkSlow", Context: "источник FeedRepository.refresh · маршрут GET /feed", Count: 1, Probability: 0.5},
			},
			Forecast: mathanalysis.MarkovForecast{
				Direction:        "insufficient",
				Label:            "Недостаточно данных",
				Severity:         "medium",
				Confidence:       "low",
				ConfidenceReason: "для прогноза нужно не менее 12 временных интервалов, сейчас 3",
				Summary:          "Текущая модель описывает только уже наблюдаемые состояния и не строит предположение о дальнейшей траектории.",
			},
		},
	}
}

func sampleCompareMathReport(comparison analyze.Comparison, summary analyze.Summary) mathanalysis.CompareMathReport {
	inspectMath := sampleMathReport(summary)
	return mathanalysis.CompareMathReport{
		Title:      "база против кандидата",
		Baseline:   inspectMath,
		Candidate:  inspectMath,
		Comparison: comparison,
		Findings: []mathanalysis.Finding{{
			Severity: "ok",
			Title:    "Сравнение готово",
			Detail:   "Математическое сравнение рассчитано по доступным событиям.",
		}},
		Sections: []mathanalysis.MathSection{
			{ID: "quality", Title: "Качество сравнения", Status: "ok", Summary: "Сводка качества сравнения."},
			{ID: "robust", Title: "Робастная статистика", Status: "medium", Summary: "Один сигнал есть только у кандидата."},
			{ID: "periodic", Title: "Периодические сигналы", Status: "ok", Summary: "Подтвержденных повторяемых паттернов нет."},
			{ID: "network-loops", Title: "Сетевые циклы", Status: "pending", Summary: "Каркас compare-детектора сетевых циклов."},
			{ID: "integral", Title: "Интегральная нагрузка", Status: "medium", Summary: "Накопленные дельты рассчитаны по доступным интервалам."},
			{ID: "markov", Title: "Марковская модель состояний", Status: "medium", Summary: "Каркас марковских дельт."},
		},
		RobustDeltas: []mathanalysis.RobustDelta{
			{Dimension: "Маршрут", Name: "GET /new", Metric: "HTTP задержка", Unit: "мс", CandidateCount: 12, CandidateP95: 420, P95Delta: 420, EffectSize: "не применимо", Confidence: "не применимо: сигнал есть только в одном прогоне", Severity: "medium", Summary: "Сигнал есть только у кандидата."},
		},
		IntegralDeltas: []mathanalysis.IntegralDelta{
			{
				ID:                  "latency_pain_area",
				Title:               "Площадь сетевой задержки",
				Formula:             "Σ max(0, HTTP p95 - 300ms) * Δt",
				Unit:                "мс*с",
				BaselineValue:       100,
				CandidateValue:      620,
				Delta:               520,
				DeltaPct:            520,
				DeltaPctAvailable:   true,
				Comparable:          true,
				BaselineDurationMS:  10_000,
				CandidateDurationMS: 10_000,
				Severity:            "medium",
				Summary:             "Площадь сетевой задержки выросла.",
			},
		},
		MarkovDeltas: []mathanalysis.MarkovDelta{
			{
				Metric:             "Расхождение матрицы переходов",
				Unit:               "индекс",
				BaselineValue:      0,
				CandidateValue:     0.42,
				Delta:              0.42,
				Comparable:         true,
				BaselineAvailable:  true,
				CandidateAvailable: true,
				Severity:           "medium",
				Summary:            "Матрица переходов изменилась на 0.420 по расхождению Йенсена-Шеннона.",
			},
		},
	}
}

func TestLimitRowsKeepsRankedPrefixWithoutMutatingSource(t *testing.T) {
	rows := []int{9, 7, 5, 3}
	limited, ok := limitRows(rows, 2).([]int)
	if !ok {
		t.Fatalf("limitRows() type = %T, want []int", limitRows(rows, 2))
	}
	if got, want := len(limited), 2; got != want || limited[0] != 9 || limited[1] != 7 {
		t.Fatalf("limitRows() = %v, want ranked prefix [9 7]", limited)
	}
	limited[0] = 11
	if rows[0] != 11 {
		t.Fatalf("limitRows() copied backing data; source = %v", rows)
	}
	empty := limitRows(rows, -1).([]int)
	if len(empty) != 0 {
		t.Fatalf("limitRows(rows, -1) = %v, want empty prefix", empty)
	}
	if got := limitRows("not a slice", 1); got != "not a slice" {
		t.Fatalf("limitRows(non-slice) = %v", got)
	}
}

func TestRowLimitNoteIsExplicitAndEscapesLabel(t *testing.T) {
	note := string(rowLimitNote(`<runtime>`, 12_925, 256))
	for _, want := range []string{
		"&lt;runtime&gt;",
		"показано 256 из 12925",
		"еще 12669 учтены",
		"--json",
	} {
		if !strings.Contains(note, want) {
			t.Fatalf("rowLimitNote() = %q, want %q", note, want)
		}
	}
	if note := rowLimitNote("small", 4, 4); note != "" {
		t.Fatalf("rowLimitNote() for an unbounded set = %q, want empty", note)
	}
}

func TestRussianCountUsesCorrectForms(t *testing.T) {
	tests := map[int]string{
		0:   "0 сигналов",
		1:   "1 сигнал",
		2:   "2 сигнала",
		4:   "4 сигнала",
		5:   "5 сигналов",
		11:  "11 сигналов",
		14:  "14 сигналов",
		21:  "21 сигнал",
		23:  "23 сигнала",
		100: "100 сигналов",
		101: "101 сигнал",
	}
	for value, want := range tests {
		if got := russianCount(value, "сигнал", "сигнала", "сигналов"); got != want {
			t.Errorf("russianCount(%d) = %q, want %q", value, got, want)
		}
	}
}

func TestPerMinuteNormalizesByRunDuration(t *testing.T) {
	if got := perMinute(60, 120_000); got != 30 {
		t.Fatalf("perMinute(60, 120000) = %.2f, want 30", got)
	}
	if got := perMinute(60, 0); got != 0 {
		t.Fatalf("perMinute(60, 0) = %.2f, want 0", got)
	}
}

func TestOperationContextInsightsExplainSingleNetworkRequestWithoutRateOrPercentile(t *testing.T) {
	insights := operationContextInsights(analyze.Summary{SignalContexts: []analyze.SignalContextStats{{
		Screen:      "RegistrationActivity",
		Operation:   "registration",
		Owner:       "RegistrationController.loadConfig",
		RouteSample: "GET /myteam-config.json",
		HTTPCount:   1,
		HTTPP95MS:   1662,
	}}})
	if len(insights) != 1 {
		t.Fatalf("operationContextInsights() count = %d, want 1", len(insights))
	}
	if !strings.Contains(insights[0].Summary, "Единственный сетевой вызов занял до 1662 мс") {
		t.Fatalf("scenario summary = %q", insights[0].Summary)
	}
	for _, confusing := range []string{"0.00/с", "p95"} {
		if strings.Contains(insights[0].Summary, confusing) {
			t.Fatalf("scenario summary contains %q: %q", confusing, insights[0].Summary)
		}
	}
}

func TestProblemEvidenceDisplayUsesRussianCountForm(t *testing.T) {
	if got := problemEvidenceDisplay(analyze.ProblemEvidence{Observed: "1", Unit: "requests"}); got != "1 запрос" {
		t.Fatalf("problemEvidenceDisplay() = %q, want %q", got, "1 запрос")
	}
	if got := problemEvidenceDisplay(analyze.ProblemEvidence{Observed: "2", Unit: "requests"}); got != "2 запроса" {
		t.Fatalf("problemEvidenceDisplay() = %q, want %q", got, "2 запроса")
	}
}

func TestUIScreenInsightsConnectNearbyScenarioSignals(t *testing.T) {
	insights := uiScreenInsights(analyze.Summary{
		Screens:        []analyze.ScreenStats{{Screen: "FeedActivity", Frames: 100, JankyFrames: 18, JankRatePct: 18, AvgFPS: 42, MinFPS: 28}},
		SignalContexts: []analyze.SignalContextStats{{Screen: "FeedActivity", Operation: "feed", Owner: "FeedPresenter.render", StallCount: 2, StallMaxMS: 820}},
	})
	if len(insights) != 1 {
		t.Fatalf("uiScreenInsights() count = %d, want 1", len(insights))
	}
	if insights[0].Status != "плохо" || !strings.Contains(insights[0].Nearby, "2 паузы") || !strings.Contains(insights[0].Where, "FeedPresenter.render") {
		t.Fatalf("unexpected UI insight: %+v", insights[0])
	}
}

func TestUIScreenInsightsDoNotCallSparseRenderingAJankFailure(t *testing.T) {
	insights := uiScreenInsights(analyze.Summary{Screens: []analyze.ScreenStats{{
		Screen: "RegistrationActivity", Frames: 248, JankyFrames: 0, JankRatePct: 0, AvgFPS: 16.9, MinFPS: 2,
	}}})
	if len(insights) != 1 || insights[0].Status != "данные расходятся" {
		t.Fatalf("unexpected sparse-rendering verdict: %+v", insights)
	}
	if strings.Contains(insights[0].Headline, "зависает") {
		t.Fatalf("sparse rendering was mislabeled as a freeze: %+v", insights[0])
	}
}

func TestCustomMetricInsightsGroupMetricsAndRelateThemToMainSignals(t *testing.T) {
	insights := customMetricInsights(analyze.Summary{
		HTTPCount: 3,
		HTTPP95MS: 1200,
		Counters:  []analyze.NamedValue{{Name: "network.retry.count", Value: 2}, {Name: "gc.bytes_allocated.delta", Value: 4096}},
	})
	if len(insights) != 2 {
		t.Fatalf("customMetricInsights() count = %d, want 2", len(insights))
	}
	if insights[0].Title != "Память и сборка мусора" || insights[1].Title != "Сеть" {
		t.Fatalf("custom metric groups = %+v", insights)
	}
	if !strings.Contains(insights[1].Relation, "3 сетевых вызова") {
		t.Fatalf("network relation = %q", insights[1].Relation)
	}
}

func assertHTMLContains(t *testing.T, path string, needles ...string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	html := string(data)
	if strings.Contains(html, "ZgotmplZ") {
		t.Fatalf("%s contains escaped unsafe template CSS", path)
	}
	for _, needle := range needles {
		if !strings.Contains(html, needle) {
			t.Fatalf("%s does not contain %q", path, needle)
		}
	}
}

func assertHTMLNotContains(t *testing.T, path string, needles ...string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	html := string(data)
	for _, needle := range needles {
		if index := strings.Index(html, needle); index >= 0 {
			start := max(0, index-120)
			end := min(len(html), index+len(needle)+120)
			t.Fatalf("%s unexpectedly contains %q near %q", path, needle, html[start:end])
		}
	}
}
