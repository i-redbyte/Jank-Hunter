package report

import (
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
				DominatorTreeExplanation: "Мини-дерево показывает вероятную цепочку доминирования по контексту выполнения.",
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
	if err := WriteInspectWithOptions(inspectPath, summary, ReportOptions{}); err != nil {
		t.Fatalf("WriteInspect() error = %v", err)
	}
	assertHTMLContains(t, inspectPath, "Отчет по сигналам выполнения", "Контекст устройства", "Pixel 8", "Рут-доступ", "Сетевые маршруты", "Сценарии и причины", "Спам логами", "Проблемные окна", "Вызовы выполнения", "Реестр проблем кода", "Разбор утечек памяти", "Шкала реестра кода", "Категории", "data-registry-category", "data-registry-severity", "code-problem-details", "Доказательства и рекомендация", "span-all", "Шкала утечек памяти", "Фильтр реестра утечек памяти", "FeedPresenter", "Быстрые проверки цепочки", "Вероятный пользовательский держатель", "Оценка удержанного размера", "Мини-дерево доминирования", "leak-dominator", "4.0 МБ", "Фильтр по классу", "data-code-registry", "data-code-sort", "Как читать отчет", "Что исправлять", "jh-tooltip", "GET /feed", "UI&#8209;подтормаживания", "Граф влияния кода", "influence-tile-body", "λ Анализ", `href="inspect-math.html"`, "approx-badge", "p95 рассчитан по reservoir-сэмплу: 20000 из 21000 запросов", "HTTP p95 сценария рассчитан по reservoir-сэмплу")
	assertHTMLContains(t, inspectPath, "z-index: 2147483647", "word-break: keep-all", "table-scroll", "wrapTables", "table-cell-clip", "cell-toggle", "scheduleTableMeasure", "details.addEventListener('toggle'", "ensureSelectOption", "setSelectFromChip", "viewportBox", "node.closest('.metric')")
	assertHTMLNotContains(t, inspectPath, "Drill-down")

	mathInspectPath := filepath.Join(dir, "inspect-math.html")
	if err := WriteMathInspectWithOptions(mathInspectPath, sampleMathReport(summary), ReportOptions{}); err != nil {
		t.Fatalf("WriteMathInspect() error = %v", err)
	}
	assertHTMLContains(t, mathInspectPath, "Математический анализ", "Качество данных", "Сетевые циклы", "Атрибуция сценариев и причин", "Реестр проблем кода", `id="code-problems" class="fold code-registry-fold" open`, "Разбор утечек памяти", "Шкала математических оценок", "Шкала реестра кода", "registry-insights", "code-problem-details", "Доказательства и рекомендация", "FeedPresenter", "Шкала утечек памяти", "Оценка удержанного размера", "Мини-дерево доминирования", "overview-attribution-fold", "data-zero-scope", "closest('[data-zero-scope]')", "Пустые интервалы скрыты", "Вызовы выполнения", "Как читать оценки", "Критерии", "Выгорание", "Детали раздела", "Сводка разделов", "Справка по методам", "Робастная статистика", "дельта Клиффа", "Граф причинности", "Уверенность", "Экспозиция плохих состояний", "Контекстная липкость", "Вклады симптомов")

	comparePath := filepath.Join(dir, "compare.html")
	comparison := analyze.Compare(summary, summary)
	if err := WriteCompareReportWithOptions(
		comparePath,
		comparison,
		[]LogReport{{Name: "old/sample.jhlog", Anchor: "baseline-log-1", Summary: summary}},
		[]LogReport{{Name: "new/sample.jhlog", Anchor: "candidate-log-1", Summary: summary}},
		ReportOptions{},
	); err != nil {
		t.Fatalf("WriteCompareReport() error = %v", err)
	}
	assertHTMLContains(t, comparePath, "Панель контроля регрессий", "Контекст сравнения", "Сеть и трафик", "Реестр проблем кода кандидата", "Сравнение утечек памяти", "Шкала сравнения", "Шкала реестра кода", "data-registry-category", "data-registry-severity", "code-problem-details", "Доказательства и рекомендация", "Шкала утечек памяти", "Оценка удержанного размера", "Мини-дерево доминирования", "Фильтр сравнительного реестра утечек памяти", "кандидат против базы", "Фильтр сравнительного реестра проблем кода", "data-code-registry", "data-code-sort", "дельта", "Где изменилось", "Сравнение сценариев и причин", "Как читать сравнение", "Контекст устройств", "Детали по каждому логу", "Эвристический итог", "old/sample.jhlog", "new/sample.jhlog", "λ Анализ", `href="compare-math.html"`)

	mathComparePath := filepath.Join(dir, "compare-math.html")
	if err := WriteMathCompareWithOptions(mathComparePath, sampleCompareMathReport(comparison, summary), ReportOptions{}); err != nil {
		t.Fatalf("WriteMathCompare() error = %v", err)
	}
	assertHTMLContains(t, mathComparePath, "Математический анализ сравнения", "Качество сравнения", "Сетевые циклы", "Сравнение сценариев и причин", "Реестр проблем кода кандидата", `id="code-problems" class="fold code-registry-fold" open`, "Сравнение утечек памяти", "Шкала сравнения", "Шкала реестра кода", "registry-insights", "code-problem-details", "Доказательства и рекомендация", "FeedPresenter", "Шкала утечек памяти", "Оценка удержанного размера", "Мини-дерево доминирования", "Фильтр сравнительного реестра утечек памяти", "Фильтр сравнительного реестра проблем кода", "data-code-registry", "data-code-sort", "Как читать сравнение", "Критерии", "Сводка разделов", "Справка по методам", "Марковская модель состояний", "Расхождение матрицы переходов", "Экспозиция плохих состояний кандидата", "Граф причинности")

	influencePath := filepath.Join(dir, "inspect-influence.html")
	if err := WriteInfluenceWithOptions(influencePath, sampleInfluence(), "Граф влияния кода", ReportOptions{}); err != nil {
		t.Fatalf("WriteInfluence() error = %v", err)
	}
	assertHTMLContains(t, influencePath, "Граф влияния кода", "Карта влияния", "Проблемные классы", "Связи влияния", "Горячие пути", "Горячие методы", "Показать проблемные классы", "Показать связи влияния", "influence-table-fold", "Оценка", "CheckoutRepository", "CheckoutPresenter", ".influence-node.high circle", "vector-effect: non-scaling-stroke", `id="influence-arrow-confirmed"`, `markerUnits="userSpaceOnUse"`, "<path class=\"influence-edge", `marker-end="url(#influence-arrow-confirmed)"`, "data-influence-mode=\"tree\"", "data-influence-selection", "data-node=", "walkPathsFrom")

	diagnosticsPath := filepath.Join(dir, "inspect-diagnostics.html")
	if err := WriteInstrumentationDiagnosticsWithOptions(diagnosticsPath, sampleInstrumentationDiagnostics(), ReportOptions{}); err != nil {
		t.Fatalf("WriteInstrumentationDiagnostics() error = %v", err)
	}
	assertHTMLContains(t, diagnosticsPath, "ASM диагностика", "Сводка ASM", "Сработавшие перехватчики", "Решения сопоставителя", "Области аннотаций", "okhttp3.bridge.v3", "FeedOwner", "instrumentation-diagnostics.jsonl")
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

func TestWriteReportsCanHideMathLink(t *testing.T) {
	t.Setenv("JH_LANG", "ru")
	dir := t.TempDir()
	summary := analyze.Summary{Title: "sample.jhlog", LogCount: 1, EventCount: 1}

	inspectPath := filepath.Join(dir, "inspect.html")
	if err := WriteInspectWithOptions(inspectPath, summary, ReportOptions{DisableMathLink: true}); err != nil {
		t.Fatalf("WriteInspectWithOptions() error = %v", err)
	}
	assertHTMLNotContains(t, inspectPath, "λ Анализ", `href="inspect-math.html"`)

	comparePath := filepath.Join(dir, "compare.html")
	if err := WriteCompareReportWithOptions(comparePath, analyze.Compare(summary, summary), nil, nil, ReportOptions{DisableMathLink: true}); err != nil {
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
	if err := WriteInspectWithOptions(inspectPath, summary, ReportOptions{}); err != nil {
		t.Fatalf("WriteInspect() error = %v", err)
	}
	assertHTMLContains(t, inspectPath, `<html lang="ru">`, "Отчет по сигналам выполнения", "Контекст устройства", "Батарея", "Сетевые маршруты", "Сценарии и причины", "Эвристический итог", "λ Анализ")

	comparePath := filepath.Join(dir, "compare-ru.html")
	if err := WriteCompareReportWithOptions(
		comparePath,
		analyze.Compare(summary, summary),
		[]LogReport{{Name: "old/sample.jhlog", Anchor: "baseline-log-1", Summary: summary}},
		[]LogReport{{Name: "new/sample.jhlog", Anchor: "candidate-log-1", Summary: summary}},
		ReportOptions{},
	); err != nil {
		t.Fatalf("WriteCompareReport() error = %v", err)
	}
	assertHTMLContains(t, comparePath, "Панель контроля регрессий", "Матрица регрессий", "Где изменилось", "Сравнение сценариев и причин", "Детали по каждому логу", "Эвристический итог", "Логи базы", "λ Анализ")
}

func TestMathReportPath(t *testing.T) {
	tests := map[string]string{
		"report.html":               "report-math.html",
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
		if strings.Contains(html, needle) {
			t.Fatalf("%s unexpectedly contains %q", path, needle)
		}
	}
}
