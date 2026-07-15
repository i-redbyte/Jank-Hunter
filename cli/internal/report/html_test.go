package report

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/mathanalysis"
)

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
		Environment: analyze.RunEnvironment{
			Title:    "Pixel 8",
			Subtitle: "Android 15 · 0.1.0-debug (100) · процесс main",
			Items: []analyze.InfoItem{
				{Label: "Батарея", Value: "82%", Detail: "заряжается · 32.0 C"},
				{Label: "Сеть", Value: "wifi", Detail: "валидирована да · лимитная нет · VPN нет"},
				{Label: "Рут-доступ", Value: "нет", Detail: "признаки рут-доступа не найдены"},
			},
		},
		Routes: []analyze.RouteStats{
			{Route: "GET /feed", Count: 21_000, Sampled: 20_000, Failures: 0, P95MS: 612, P95Approximate: true, MaxMS: 612, OwnerSample: "FeedRepository.refresh"},
		},
		Screens: []analyze.ScreenStats{
			{Screen: "Feed", Frames: 1122, JankyFrames: 90, JankRatePct: 8.02, AvgFPS: 56.1, P95MS: 24},
		},
		Owners: []analyze.OwnerStats{
			{Owner: "FeedRepository.refresh", Kind: "http", Count: 2, MaxMS: 612},
		},
		Flows: []analyze.FlowStats{
			{Screen: "Feed", Flow: "feed.open", Step: "network", Owner: "FeedRepository.refresh", HTTPCount: 21_000, HTTPP95MS: 612, HTTPP95Approximate: true, UIFrames: 1122, UIJank: 90, UIJankPct: 8.02},
		},
		LogSpam: []analyze.LogSpamStats{
			{Screen: "Feed", Flow: "feed.open", Step: "render", Owner: "FeedPresenter.render", Source: "android.util.Log.w", Level: "warn", Count: 7},
		},
		ProblemWindows: []analyze.ProblemWindowStats{
			{Screen: "Feed", Flow: "feed.open", Step: "render", Owner: "FeedPresenter.render", Kind: "ui_jank", Windows: 1, Count: 90, TotalWindowMS: 10000, MaxMS: 24},
		},
		RuntimeCalls: []analyze.RuntimeCallStats{
			{Screen: "Feed", Flow: "feed.open", Step: "render", Caller: "FeedPresenter.render", Callee: "FeedAdapter.bind", Count: 12, TotalMS: 144, MaxMS: 24},
		},
		MemoryLeaks: []analyze.MemoryLeakSuspect{
			{
				ClassName:                "com.app.feed.FeedActivity",
				Holder:                   "FeedPresenter",
				Screen:                   "Feed",
				Flow:                     "feed.open",
				Step:                     "render",
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

	dir := t.TempDir()
	inspectPath := filepath.Join(dir, "inspect.html")
	if err := WriteInspectWithOptions(inspectPath, summary, ReportOptions{Links: ReportLinks{
		Math:      "inspect-math.html",
		Leaks:     "inspect-leaks.html",
		Influence: "inspect-influence.html",
	}}); err != nil {
		t.Fatalf("WriteInspect() error = %v", err)
	}
	assertHTMLContains(t, inspectPath, "Отчет по сигналам выполнения", "Контекст устройства", "Pixel 8", "Рут-доступ", "Сетевые маршруты", "Сценарии и причины", "Спам логами", "Проблемные окна", "Вызовы выполнения", "Реестр проблем кода", "Удержания и возможные утечки памяти", "Шкала реестра кода", "Категории", "data-registry-category", "data-registry-severity", "code-problem-details", "Доказательства и рекомендация", "span-all", "Шкала сигналов удержания", "Фильтр реестра утечек памяти", "FeedPresenter", "Быстрые проверки цепочки", "Вероятный пользовательский держатель", "Оценка удержанного размера", "Путь / контекст удержания", "leak-dominator", "4.0 МБ", "Фильтр по классу", "data-code-registry", "data-code-sort", "Как читать отчет", "Что исправлять", "jh-tooltip", "GET /feed", "UI&#8209;подтормаживания", "Граф влияния кода", "influence-tile-body", "λ Анализ", `href="inspect-math.html"`, "approx-badge", "p95 рассчитан по reservoir-сэмплу: 20000 из 21000 запросов", "HTTP p95 сценария рассчитан по reservoir-сэмплу")
	assertHTMLContains(t, inspectPath, "z-index: 2147483647", "word-break: keep-all", "table-scroll", "wrapTables", "table-cell-clip", "cell-toggle", "scheduleTableMeasure", "details.addEventListener('toggle'", "ensureSelectOption", "setSelectFromChip", "viewportBox", "node.closest('.metric')")
	assertHTMLNotContains(t, inspectPath, "Drill-down")

	mathInspectPath := filepath.Join(dir, "inspect-math.html")
	if err := WriteMathInspectWithOptions(mathInspectPath, sampleMathReport(summary), ReportOptions{Links: ReportLinks{
		Main:      "inspect.html",
		Influence: "inspect-influence.html",
	}}); err != nil {
		t.Fatalf("WriteMathInspect() error = %v", err)
	}
	assertHTMLContains(t, mathInspectPath, "Математический анализ", "Качество данных", "Сетевые циклы", "Атрибуция сценариев и причин", "Реестр проблем кода", `id="code-problems" class="fold code-registry-fold" open`, "Разбор утечек памяти", "Шкала математических оценок", "Шкала реестра кода", "registry-insights", "code-problem-details", "Доказательства и рекомендация", "FeedPresenter", "Шкала сигналов удержания", "Оценка удержанного размера", "Путь / контекст удержания", "overview-attribution-fold", "data-zero-scope", "closest('[data-zero-scope]')", "Пустые интервалы скрыты", "Вызовы выполнения", "Как читать оценки", "Критерии", "Выгорание", "Детали раздела", "Сводка разделов", "Справка по методам", "Робастная статистика", "дельта Клиффа", "Граф причинности", "Уверенность", "Экспозиция плохих состояний", "Контекстная липкость", "Вклады симптомов", `href="inspect.html"`, "← Обзор")

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
	assertHTMLContains(t, comparePath, "Панель контроля регрессий", "Контекст сравнения", "Сеть и трафик", "Реестр проблем кода кандидата", "Сравнение сигналов удержания памяти", "Шкала сравнения", "Шкала реестра кода", "data-registry-category", "data-registry-severity", "code-problem-details", "Доказательства и рекомендация", "Шкала сигналов удержания", "Оценка удержанного размера", "Путь / контекст удержания", "Фильтр сравнительного реестра утечек памяти", "кандидат против базы", "Фильтр сравнительного реестра проблем кода", "data-code-registry", "data-code-sort", "дельта", "Где изменилось", "Сравнение сценариев и причин", "Как читать сравнение", "Контекст устройств", "Детали по каждому логу", "Эвристический итог", "old/sample.jhlog", "new/sample.jhlog", "λ Анализ", `href="compare-math.html"`)

	mathComparePath := filepath.Join(dir, "compare-math.html")
	if err := WriteMathCompareWithOptions(mathComparePath, sampleCompareMathReport(comparison, summary), ReportOptions{Links: ReportLinks{Main: "compare.html"}}); err != nil {
		t.Fatalf("WriteMathCompare() error = %v", err)
	}
	assertHTMLContains(t, mathComparePath, "Математический анализ сравнения", "Качество сравнения", "Сетевые циклы", "Сравнение сценариев и причин", "Реестр проблем кода кандидата", `id="code-problems" class="fold code-registry-fold" open`, "Сравнение сигналов удержания памяти", "Шкала сравнения", "Шкала реестра кода", "registry-insights", "code-problem-details", "Доказательства и рекомендация", "FeedPresenter", "Шкала сигналов удержания", "Оценка удержанного размера", "Путь / контекст удержания", "Фильтр сравнительного реестра утечек памяти", "Фильтр сравнительного реестра проблем кода", "data-code-registry", "data-code-sort", "Как читать сравнение", "Критерии", "Сводка разделов", "Справка по методам", "Марковская модель состояний", "Расхождение матрицы переходов", "Экспозиция плохих состояний кандидата", "Граф причинности", `href="compare.html"`, "← Обзор")

	influencePath := filepath.Join(dir, "inspect-influence.html")
	if err := WriteInfluenceWithOptions(influencePath, sampleInfluence(), "Граф влияния кода", ReportOptions{Links: ReportLinks{Main: "inspect.html"}}); err != nil {
		t.Fatalf("WriteInfluence() error = %v", err)
	}
	assertHTMLContains(t, influencePath, "Граф влияния кода", "Карта влияния", "Проблемные классы", "Связи влияния", "Горячие пути", "Горячие методы", "Показать проблемные классы", "Показать связи влияния", "influence-table-fold", "Оценка", "CheckoutRepository", "CheckoutPresenter", ".influence-node.high circle", "vector-effect: non-scaling-stroke", `id="influence-arrow-confirmed-report"`, `markerUnits="userSpaceOnUse"`, "<path class=\"influence-edge", `marker-end="url(#influence-arrow-confirmed-report)"`, "data-influence-mode=\"tree\"", "data-influence-selection", "data-node=", "walkPathsFrom", `href="inspect.html"`, "← Обзор")

	diagnosticsPath := filepath.Join(dir, "inspect-diagnostics.html")
	if err := WriteInstrumentationDiagnosticsWithOptions(diagnosticsPath, sampleInstrumentationDiagnostics(), ReportOptions{Links: ReportLinks{Main: "inspect.html"}}); err != nil {
		t.Fatalf("WriteInstrumentationDiagnostics() error = %v", err)
	}
	assertHTMLContains(t, diagnosticsPath, "ASM диагностика", "Сводка ASM", "Сработавшие перехватчики", "Решения сопоставителя", "Области аннотаций", "okhttp3.bridge.v3", "FeedOwner", "instrumentation-diagnostics.jsonl", `href="inspect.html"`, "← Обзор")

	dependencyInjectionPath := filepath.Join(dir, "inspect-di.html")
	if err := WriteDependencyInjectionWithOptions(
		dependencyInjectionPath,
		sampleDependencyInjectionReport(),
		ReportOptions{Links: ReportLinks{Main: "inspect.html"}},
	); err != nil {
		t.Fatalf("WriteDependencyInjectionWithOptions() error = %v", err)
	}
	assertHTMLContains(
		t,
		dependencyInjectionPath,
		"DI-каталог",
		"DI · BUILD TIME",
		"consumer → dependency",
		analyze.DependencyInjectionDisclaimer,
		"com.app.FeedViewModel",
		"com.app.FeedRepository",
		"generated_confirmed",
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
		Title:    "active.jhlog",
		LogCount: 1,
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
	warningIndex := strings.Index(html, "тестовое техническое предупреждение")
	if qualityIndex < 0 || analysisIndex < 0 || warningIndex < qualityIndex || qualityIndex < analysisIndex {
		t.Fatalf("technical quality is not at report end: analysis=%d quality=%d warning=%d", analysisIndex, qualityIndex, warningIndex)
	}
	qualitySection := html[qualityIndex:]
	if !strings.Contains(qualitySection, `<details class="fold">`) ||
		strings.Contains(qualitySection, `<details class="fold" open>`) {
		t.Fatal("technical quality details must be collapsed by default")
	}
}

func TestStandaloneLeakReportsLinkExplorerAndRegistry(t *testing.T) {
	summary := analyze.Summary{
		Title: "leaks.jhlog",
		MemoryLeaks: []analyze.MemoryLeakSuspect{{
			ClassName:           "com.app.checkout.SuperLongCheckoutActivityNameThatMustWrapInsideGraphNode",
			Holder:              "CheckoutPresenter",
			Screen:              "Checkout",
			Flow:                "checkout.pay",
			Step:                "destroyed",
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

func TestLeakReportsBoundExplorerAndKeepFullRegistry(t *testing.T) {
	const total = 30
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
			panelTargetTail: `data-leak-target="leak-30"`,
			expectedNotice:  "Интерактивные графы удержаний:</strong> показано 24 из 30",
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
			panelTargetTail: `data-leak-target="leak-delta-29"`,
			expectedNotice:  "Интерактивные графы дельт:</strong> показано 24 из 30",
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
			for marker, want := range map[string]int{
				`<button type="button" data-leak-select`:   24,
				`class="leak-graph-panel" data-leak-panel`: 24,
				`<svg class="leak-graph-svg"`:              24,
				`<tr data-code-problem-row`:                total,
				`<tr data-code-problem-row data-leak-row`:  24,
			} {
				if got := strings.Count(html, marker); got != want {
					t.Fatalf("%s count = %d, want %d", marker, got, want)
				}
			}
			if !strings.Contains(html, test.expectedNotice) {
				t.Fatalf("truncation notice missing: %q", test.expectedNotice)
			}
			if !strings.Contains(html, "com.app.LeakClass29") {
				t.Fatal("tail leak disappeared from the complete registry")
			}
			if strings.Contains(html, test.panelTargetTail) {
				t.Fatalf("tail registry row still links to a missing explorer panel: %s", test.panelTargetTail)
			}
		})
	}
}

func TestCompareReportBoundsLogGroupsAndHighCardinalityTables(t *testing.T) {
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
	if got := strings.Count(html, `class="log-card"`); got != 24 {
		t.Fatalf("log-card count = %d, want 24", got)
	}
	for _, want := range []string{
		"Логи базы:</strong> показано 12 из 15 исходных логов",
		"Логи кандидата:</strong> показано 12 из 14 исходных логов",
		"объединённые метрики сравнения выше учитывают всю группу",
		"Сравнение маршрутов:</strong> показано 64 из 70",
		"Сравнение экранов:</strong> показано 64 из 70",
		"Маршруты этого лога:</strong> показано 64 из 70",
		"Экраны этого лога:</strong> показано 64 из 70",
		"Gauge-метрики этого лога:</strong> показано 128 из 130",
		"baseline-11.jhlog",
		"candidate-11.jhlog",
		"GET /bounded/route-063",
		"BoundedScreen063",
		"bounded.gauge.127",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("bounded compare report does not contain %q", want)
		}
	}
	for _, unwanted := range []string{
		"baseline-12.jhlog",
		"candidate-12.jhlog",
		"GET /bounded/route-064",
		"BoundedScreen064",
		"bounded.gauge.128",
	} {
		if strings.Contains(html, unwanted) {
			t.Fatalf("bounded compare report unexpectedly contains %q", unwanted)
		}
	}
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

func TestFlowKeyLabelHidesUnknownParts(t *testing.T) {
	if got, want := flowKeyLabel("unknown", "unknown", "unknown", "unknown"), "контекст не задан"; got != want {
		t.Fatalf("flowKeyLabel(all unknown) = %q, want %q", got, want)
	}
	if got, want := flowKeyLabel("Feed", "unknown", "Feed", "FeedOwner"), "Feed / FeedOwner"; got != want {
		t.Fatalf("flowKeyLabel(deduplicated) = %q, want %q", got, want)
	}
	if got, want := reportValue("unknown unknown", "нет данных"), "нет данных"; got != want {
		t.Fatalf("reportValue(unknown unknown) = %q, want %q", got, want)
	}
	if got := string(contextValueHint("unknown", "screen")); !strings.Contains(got, "Activity lifecycle callbacks") {
		t.Fatalf("contextValueHint(screen) = %q, want Activity lifecycle hint", got)
	}
	if got := string(flowKeyLabelHint("unknown", "unknown", "unknown", "unknown")); !strings.Contains(got, "owner-map") {
		t.Fatalf("flowKeyLabelHint(all unknown) = %q, want attribution hint", got)
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
		Flows: []analyze.FlowStats{
			{Screen: "unknown", Flow: "unknown", Step: "unknown", Owner: "unknown", RouteSample: "unknown", ProblemCount: 1},
		},
		LogSpam: []analyze.LogSpamStats{
			{Screen: "unknown", Flow: "unknown", Step: "unknown", Owner: "unknown", Source: "unknown", Level: "warn", Count: 1},
		},
		ProblemWindows: []analyze.ProblemWindowStats{
			{Screen: "unknown", Flow: "unknown", Step: "unknown", Owner: "unknown", Kind: "ui_jank", Windows: 1, Count: 1, TotalWindowMS: 16, MaxMS: 16},
		},
		RuntimeCalls: []analyze.RuntimeCallStats{
			{Screen: "unknown", Flow: "unknown", Step: "unknown", Caller: "unknown", Callee: "unknown", Count: 1, TotalMS: 16, MaxMS: 16},
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
	assertHTMLContains(t, inspectPath, "неизвестное устройство", "контекст выполнения недоступен", "нет данных", "Activity lifecycle callbacks", "owner-map", "session-событием")
	assertHTMLNotContains(t, inspectPath, "unknown unknown", "unknown build", ">unknown<", "<code>unknown</code>")

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

	dir := t.TempDir()
	inspectPath := filepath.Join(dir, "inspect-ru.html")
	if err := WriteInspectWithOptions(inspectPath, summary, ReportOptions{Links: ReportLinks{Math: "inspect-ru-math.html"}}); err != nil {
		t.Fatalf("WriteInspect() error = %v", err)
	}
	assertHTMLContains(t, inspectPath, `<html lang="ru">`, "Отчет по сигналам выполнения", "Контекст устройства", "Батарея", "Сетевые маршруты", "Сценарии и причины", "Эвристический итог", "λ Анализ")

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
	assertHTMLContains(t, comparePath, "Панель контроля регрессий", "Матрица регрессий", "Где изменилось", "Сравнение сценариев и причин", "Детали по каждому логу", "Эвристический итог", "Логи базы", "λ Анализ")
}

func TestMathReportPath(t *testing.T) {
	tests := map[string]string{
		"report.html":               "report-math.html",
		"report-math.html":          "report-math.html",
		"/tmp/report.html":          "/tmp/report-math.html",
		"/tmp/report":               "/tmp/report-math.html",
		"/tmp/report.with.dots.htm": "/tmp/report.with.dots-math.htm",
	}
	for input, want := range tests {
		if got := MathReportPath(input); got != want {
			t.Fatalf("MathReportPath(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestInfluenceReportPath(t *testing.T) {
	tests := map[string]string{
		"report.html":                    "report-influence.html",
		"/tmp/report.html":               "/tmp/report-influence.html",
		"/tmp/report-math.html":          "/tmp/report-influence.html",
		"/tmp/report.with.dots.html":     "/tmp/report.with.dots-influence.html",
		"/tmp/report.with.dots-math.htm": "/tmp/report.with.dots-influence.htm",
		"/tmp/report.with.dots":          "/tmp/report.with-influence.dots",
		"/tmp/report-math":               "/tmp/report-influence.html",
	}
	for input, want := range tests {
		if got := InfluenceReportPath(input); got != want {
			t.Fatalf("InfluenceReportPath(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestLeakReportPath(t *testing.T) {
	tests := map[string]string{
		"report.html":                         "report-leaks.html",
		"/tmp/report.html":                    "/tmp/report-leaks.html",
		"/tmp/report-math.html":               "/tmp/report-leaks.html",
		"/tmp/report-influence.html":          "/tmp/report-leaks.html",
		"/tmp/report-diagnostics.html":        "/tmp/report-leaks.html",
		"/tmp/report.with.dots.html":          "/tmp/report.with.dots-leaks.html",
		"/tmp/report.with.dots-influence.htm": "/tmp/report.with.dots-leaks.htm",
		"/tmp/report-math":                    "/tmp/report-leaks.html",
	}
	for input, want := range tests {
		if got := LeakReportPath(input); got != want {
			t.Fatalf("LeakReportPath(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestDiagnosticsReportPath(t *testing.T) {
	tests := map[string]string{
		"report.html":                         "report-diagnostics.html",
		"/tmp/report.html":                    "/tmp/report-diagnostics.html",
		"/tmp/report-math.html":               "/tmp/report-diagnostics.html",
		"/tmp/report-influence.html":          "/tmp/report-diagnostics.html",
		"/tmp/report.with.dots.html":          "/tmp/report.with.dots-diagnostics.html",
		"/tmp/report.with.dots-influence.htm": "/tmp/report.with.dots-diagnostics.htm",
		"/tmp/report-math":                    "/tmp/report-diagnostics.html",
	}
	for input, want := range tests {
		if got := DiagnosticsReportPath(input); got != want {
			t.Fatalf("DiagnosticsReportPath(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestDependencyInjectionReportPath(t *testing.T) {
	tests := map[string]string{
		"report.html":                         "report-di.html",
		"/tmp/report.html":                    "/tmp/report-di.html",
		"/tmp/report-math.html":               "/tmp/report-di.html",
		"/tmp/report-influence.html":          "/tmp/report-di.html",
		"/tmp/report-diagnostics.html":        "/tmp/report-di.html",
		"/tmp/report-leaks.html":              "/tmp/report-di.html",
		"/tmp/report.with.dots-influence.htm": "/tmp/report.with.dots-di.htm",
	}
	for input, want := range tests {
		if got := DependencyInjectionReportPath(input); got != want {
			t.Fatalf("DependencyInjectionReportPath(%q) = %q, want %q", input, got, want)
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
			Flows:           []string{"checkout.open"},
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
		},
		Annotations: []analyze.InstrumentationAnnotationSummary{
			{Owner: "FeedOwner", Screen: "Feed", Flow: "feed.open", Trace: "refresh", Count: 1},
		},
		TopClasses: []analyze.InstrumentationClassDiagnostic{
			{
				ClassName:        "com.app.FeedRepository",
				Methods:          3,
				AnnotatedMethods: 1,
				HookCount:        2,
				Hooks: []analyze.InstrumentationHookSummary{
					{Intent: "okhttp.install_event_listener_factory", Signature: "okhttp3.builder.build.v3", Bridge: "okhttp3.bridge.v3", Method: "client()V", Count: 2},
				},
				Annotations: []analyze.InstrumentationAnnotationSummary{
					{Owner: "FeedOwner", Screen: "Feed", Flow: "feed.open", Trace: "refresh", Count: 1},
				},
			},
		},
	}
}

func sampleDependencyInjectionReport() analyze.DependencyInjectionReport {
	return analyze.DependencyInjectionReport{
		Available:       true,
		Source:          "di-catalog.jsonl",
		Variant:         "debug",
		Disclaimer:      analyze.DependencyInjectionDisclaimer,
		ClassCount:      2,
		EdgeCount:       1,
		ShownClassCount: 2,
		ShownEdgeCount:  1,
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

func sampleMathReport(summary analyze.Summary) mathanalysis.MathReport {
	return mathanalysis.MathReport{
		Title:       "sample.jhlog",
		SourcePaths: []string{"sample.jhlog"},
		Summary:     summary,
		Findings: []mathanalysis.Finding{{
			Severity: "ok",
			Title:    "Данных достаточно",
			Detail:   "Каркас математического отчета готов.",
		}},
		Sections: []mathanalysis.MathSection{
			{ID: "quality", Title: "Качество данных", Status: "ok", Summary: "Сводка качества данных."},
			{ID: "timeline", Title: "Таймлайн сигналов", Status: "ok", Summary: "Сводка таймлайна."},
			{ID: "network-loops", Title: "Сетевые циклы", Status: "pending", Summary: "Каркас детектора сетевых циклов."},
			{ID: "integral", Title: "Интегральная нагрузка", Status: "medium", Summary: "Каркас интегральных оценок."},
			{ID: "markov", Title: "Марковская модель состояний", Status: "medium", Summary: "Сводка марковских переходов."},
		},
		Timeline: []mathanalysis.TimelineBucket{
			{StartMS: 0, EndMS: 1000},
			{StartMS: 1000, EndMS: 2000, HTTPCount: 2, HTTPP95DurationMS: 612, UIFrames: 90, UIJankyFrames: 7},
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
			TransitionEventCount:    2,
			BadEpisodeCount:         1,
			Confidence:              "medium",
			ConfidenceReason:        "окон=3, плохих эпизодов=1: восстановление и липкость лучше подтвердить повтором",
			HealthyToBadCount:       1,
			BadToHealthyProbability: 1,
			ExpectedRecoveryWindows: 1,
			ExpectedRecoveryMS:      1000,
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
			Detail:   "Каркас математического сравнения готов.",
		}},
		Sections: []mathanalysis.MathSection{
			{ID: "quality", Title: "Качество сравнения", Status: "ok", Summary: "Сводка качества сравнения."},
			{ID: "network-loops", Title: "Сетевые циклы", Status: "pending", Summary: "Каркас compare-детектора сетевых циклов."},
			{ID: "integral", Title: "Интегральная нагрузка", Status: "medium", Summary: "Каркас интегральных дельт."},
			{ID: "markov", Title: "Марковская модель состояний", Status: "medium", Summary: "Каркас марковских дельт."},
		},
		IntegralDeltas: []mathanalysis.IntegralDelta{
			{
				ID:             "latency_pain_area",
				Title:          "Площадь сетевой задержки",
				Formula:        "Σ max(0, HTTP p95 - 300ms) * Δt",
				Unit:           "мс*с",
				BaselineValue:  100,
				CandidateValue: 620,
				Delta:          520,
				DeltaPct:       520,
				Severity:       "medium",
				Summary:        "Площадь сетевой задержки выросла.",
			},
		},
		MarkovDeltas: []mathanalysis.MarkovDelta{
			{
				Metric:         "Расхождение матрицы переходов",
				Unit:           "индекс",
				BaselineValue:  0,
				CandidateValue: 0.42,
				Delta:          0.42,
				Severity:       "medium",
				Summary:        "Матрица переходов изменилась на 0.420 по расхождению Йенсена-Шеннона.",
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
