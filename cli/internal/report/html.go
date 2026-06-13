package report

import (
	"bytes"
	"fmt"
	"html/template"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/mathanalysis"
)

type LogReport struct {
	Name    string
	Anchor  string
	Summary analyze.Summary
}

func WriteInspect(path string, summary analyze.Summary) error {
	lang := reportLanguage()
	return execute(path, inspectTemplate, map[string]any{
		"GeneratedAt":    time.Now().Format(time.RFC3339),
		"Summary":        summary,
		"Analysis":       inspectAnalysis(summary, lang),
		"MathReportHref": MathReportHref(path),
	})
}

func WriteCompare(path string, comparison analyze.Comparison) error {
	return WriteCompareReport(path, comparison, nil, nil)
}

func WriteCompareReport(path string, comparison analyze.Comparison, baselineLogs, candidateLogs []LogReport) error {
	lang := reportLanguage()
	return execute(path, compareTemplate, map[string]any{
		"GeneratedAt":    time.Now().Format(time.RFC3339),
		"Comparison":     comparison,
		"BaselineLogs":   baselineLogs,
		"CandidateLogs":  candidateLogs,
		"Analysis":       compareAnalysis(comparison, lang),
		"MathReportHref": MathReportHref(path),
	})
}

func WriteMathInspect(path string, mathReport mathanalysis.MathReport) error {
	return execute(path, mathInspectTemplate, map[string]any{
		"GeneratedAt":      time.Now().Format(time.RFC3339),
		"Math":             mathReport,
		"MethodReferences": mathanalysis.MethodReferences(),
	})
}

func WriteMathCompare(path string, mathReport mathanalysis.CompareMathReport) error {
	return execute(path, mathCompareTemplate, map[string]any{
		"GeneratedAt":      time.Now().Format(time.RFC3339),
		"Math":             mathReport,
		"MethodReferences": mathanalysis.MethodReferences(),
	})
}

func MathReportPath(path string) string {
	ext := filepath.Ext(path)
	if ext == "" {
		return path + "-math.html"
	}
	return strings.TrimSuffix(path, ext) + "-math" + ext
}

func MathReportHref(path string) string {
	return filepath.Base(MathReportPath(path))
}

func execute(path, source string, data any) error {
	tmpl, err := template.New("report").Funcs(template.FuncMap{
		"pctWidth": func(value float64) template.CSS {
			return template.CSS(fmt.Sprintf("width:%.2f%%", clampPct(value)))
		},
		"msWidth": func(value uint64) template.CSS {
			width := float64(value) * 100 / 2000
			if width > 100 {
				width = 100
			}
			if width < 1 && value > 0 {
				width = 1
			}
			return template.CSS(fmt.Sprintf("width:%.2f%%", width))
		},
		"deltaWidth": func(value float64) template.CSS {
			width := math.Abs(value)
			if width > 100 {
				width = 100
			}
			if width < 1 && value != 0 {
				width = 1
			}
			return template.CSS(fmt.Sprintf("width:%.2f%%", width))
		},
		"scoreWidth": func(value float64) template.CSS {
			width := value * 12.5
			if width > 100 {
				width = 100
			}
			if width < 1 && value > 0 {
				width = 1
			}
			return template.CSS(fmt.Sprintf("width:%.2f%%", width))
		},
		"ringStyle": func(value float64) template.CSS {
			return template.CSS(fmt.Sprintf("--value:%.2f", clampPct(value)))
		},
		"rate": func(part int, total int) float64 {
			if total <= 0 {
				return 0
			}
			return float64(part) * 100 / float64(total)
		},
		"fpsScore": func(value float64) float64 {
			return clampPct(value * 100 / 60)
		},
		"severityClass": func(value string) string {
			switch value {
			case "high":
				return "sev-high"
			case "medium":
				return "sev-medium"
			default:
				return "sev-ok"
			}
		},
		"statusLabel": func(value string) string {
			switch value {
			case "high":
				return "критично"
			case "medium":
				return "предупреждение"
			case "ok":
				return "готово"
			case "pending":
				return "ожидает данных"
			default:
				return "каркас"
			}
		},
		"sparkline": func(series mathanalysis.Series) template.HTML {
			return sparklineSVG(series)
		},
		"seriesMax": func(series mathanalysis.Series) float64 {
			return seriesMax(series)
		},
		"seriesLast": func(series mathanalysis.Series) float64 {
			return seriesLast(series)
		},
		"bucketRange": func(bucket mathanalysis.TimelineBucket) string {
			return fmt.Sprintf("%.1f-%.1fs", float64(bucket.StartMS)/1000, float64(bucket.EndMS)/1000)
		},
		"seconds": func(ms uint64) float64 {
			return float64(ms) / 1000
		},
		"jankPct": func(jankyFrames uint64, frames uint64) float64 {
			if frames == 0 {
				return 0
			}
			return float64(jankyFrames) * 100 / float64(frames)
		},
		"motifText": func(tokens []string) string {
			return mathanalysis.NetworkLoopMotifText(tokens)
		},
		"pathText": func(path mathanalysis.GraphPath) string {
			if len(path.Nodes) == 0 {
				return ""
			}
			return strings.Join(path.Nodes, " -> ")
		},
		"markovState": func(state string) string {
			return mathanalysis.MarkovStateLabel(state)
		},
		"causalKind": func(kind string) string {
			return mathanalysis.CausalKindLabel(kind)
		},
		"percent01": func(value float64) float64 {
			return value * 100
		},
		"notOK": func(value string) bool {
			return value != "" && value != "ok"
		},
		"fallback": func(value string, fallback string) string {
			if value == "" {
				return fallback
			}
			return value
		},
	}).Parse(source)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return err
	}
	html := buf.String()
	if reportLanguage() == "ru" {
		html = localizeRussianHTML(html)
	}
	return os.WriteFile(path, []byte(html), 0o644)
}

func clampPct(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}

func sparklineSVG(series mathanalysis.Series) template.HTML {
	const (
		width  = 360.0
		height = 86.0
		pad    = 5.0
	)
	if len(series.Points) == 0 {
		return template.HTML(`<svg class="sparkline" viewBox="0 0 360 86" role="img" aria-label="нет данных"></svg>`)
	}
	maxValue := seriesMax(series)
	minValue := series.Points[0]
	for _, point := range series.Points {
		if point < minValue {
			minValue = point
		}
	}
	if maxValue == minValue {
		minValue = 0
	}
	if maxValue == minValue {
		maxValue = minValue + 1
	}
	scaleY := func(value float64) float64 {
		return height - pad - ((value - minValue) * (height - 2*pad) / (maxValue - minValue))
	}
	step := width - 2*pad
	if len(series.Points) > 1 {
		step = (width - 2*pad) / float64(len(series.Points)-1)
	}
	barWidth := step * 0.62
	if barWidth < 1.2 {
		barWidth = 1.2
	}
	if barWidth > 11 {
		barWidth = 11
	}

	var bars strings.Builder
	var line strings.Builder
	for i, point := range series.Points {
		x := pad
		if len(series.Points) > 1 {
			x += float64(i) * step
		}
		y := scaleY(point)
		barHeight := height - pad - y
		if barHeight < 1 && point > 0 {
			barHeight = 1
		}
		if point > 0 {
			fmt.Fprintf(&bars, `<rect x="%.2f" y="%.2f" width="%.2f" height="%.2f" rx="1.4"></rect>`, x-barWidth/2, height-pad-barHeight, barWidth, barHeight)
		}
		if i > 0 {
			line.WriteByte(' ')
		}
		fmt.Fprintf(&line, "%.2f,%.2f", x, y)
	}

	var out strings.Builder
	fmt.Fprintf(&out, `<svg class="sparkline" viewBox="0 0 %.0f %.0f" role="img" aria-label="%s">`, width, height, template.HTMLEscapeString(series.Name))
	out.WriteString(`<line class="spark-axis" x1="5" y1="81" x2="355" y2="81"></line>`)
	out.WriteString(`<g class="spark-bars">`)
	out.WriteString(bars.String())
	out.WriteString(`</g><polyline class="spark-line" points="`)
	out.WriteString(line.String())
	out.WriteString(`"></polyline></svg>`)
	return template.HTML(out.String())
}

func seriesMax(series mathanalysis.Series) float64 {
	var maxValue float64
	for _, point := range series.Points {
		if point > maxValue {
			maxValue = point
		}
	}
	return maxValue
}

func seriesLast(series mathanalysis.Series) float64 {
	if len(series.Points) == 0 {
		return 0
	}
	return series.Points[len(series.Points)-1]
}

func reportLanguage() string {
	lang := firstNonEmpty(
		os.Getenv("JH_LANG"),
		detectAppleLanguage(),
	)
	if lang == "" {
		lang = firstNonEmpty(os.Getenv("LC_ALL"), os.Getenv("LC_MESSAGES"), os.Getenv("LANG"))
	}
	lang = strings.ToLower(lang)
	if strings.HasPrefix(lang, "ru") {
		return "ru"
	}
	return "en"
}

func detectAppleLanguage() string {
	output, err := exec.Command("defaults", "read", "-g", "AppleLanguages").Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(output), "\n") {
		start := strings.Index(line, `"`)
		if start < 0 {
			continue
		}
		rest := line[start+1:]
		end := strings.Index(rest, `"`)
		if end < 0 {
			continue
		}
		return rest[:end]
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func localizeRussianHTML(html string) string {
	replacer := strings.NewReplacer(
		`<html lang="en">`, `<html lang="ru">`,
		`<title>Jank Hunter Inspect</title>`, `<title>Jank Hunter: отчет</title>`,
		`<title>Jank Hunter Compare</title>`, `<title>Jank Hunter: сравнение</title>`,
		`Runtime Signal Report`, `Отчет по runtime-сигналам`,
		`Regression Control Deck`, `Панель контроля регрессий`,
		`Candidate Device Context`, `Контекст устройства candidate`,
		`Device Context`, `Контекст устройства`,
		`runtime context unavailable`, `контекст runtime недоступен`,
		`unknown device`, `неизвестное устройство`,
		`No session/context metadata.`, `Нет session/context metadata.`,
		`generated `, `создан `,
		`standalone offline HTML`, `автономный offline HTML`,
		`compare first, then drill into every baseline and candidate log`, `сначала сравнение, затем детальный просмотр каждого baseline и candidate лога`,
		`>Logs <strong>`, `>Логи <strong>`,
		`>Events <strong>`, `>События <strong>`,
		`>Duration <strong>`, `>Длительность <strong>`,
		`>Baseline logs <strong>`, `>Baseline-логи <strong>`,
		`>Candidate logs <strong>`, `>Candidate-логи <strong>`,
		`>Deltas <strong>`, `>Изменения <strong>`,
		`>Overview<`, `>Обзор<`,
		`>Network<`, `>Сеть<`,
		`>Owners<`, `>Источники<`,
		`>Memory<`, `>Память<`,
		`>Metrics<`, `>Метрики<`,
		`>Context<`, `>Контекст<`,
		`>Verdict<`, `>Итог<`,
		`>Comparison<`, `>Сравнение<`,
		`>Regressions<`, `>Регрессии<`,
		`>Candidate Detail<`, `>Детали candidate<`,
		`>Per-log Drill-down<`, `>Детали по каждому логу<`,
		`>Cohorts<`, `>Когорты<`,
		`Executive Signal Matrix`, `Матрица ключевых сигналов`,
		`Fast read of the run: latency, smoothness, stalls, memory and traffic.`, `Быстрый срез прогона: latency, плавность, stalls, память и трафик.`,
		`offline report`, `offline-отчет`,
		`Comparative Scoreboard`, `Сводная панель сравнения`,
		`Baseline vs candidate across latency, smoothness, memory, traffic, retention and cohort mix.`, `Baseline против candidate по latency, плавности, памяти, трафику, retained objects и составу когорты.`,
		`standalone HTML`, `автономный HTML`,
		`Regression Matrix`, `Матрица регрессий`,
		`Severity is adjusted for confidence and sample size. Bars show regression magnitude capped at 100%.`, `Серьезность учитывает доверие и размер выборки. Полосы показывают величину регрессии с ограничением в 100%.`,
		`Worst Regression Cards`, `Карточки худших регрессий`,
		`Candidate Deep Summary`, `Подробная сводка candidate`,
		`The aggregate candidate profile after all filters.`, `Агрегированный профиль candidate после всех фильтров.`,
		`Per-log Drill-down`, `Детали по каждому логу`,
		`Open any source log to inspect its own network, UI, memory, metrics and attribution profile.`, `Раскрой любой исходный лог, чтобы посмотреть его сеть, UI, память, метрики и attribution profile.`,
		`Baseline Logs`, `Baseline-логи`,
		`Candidate Logs`, `Candidate-логи`,
		`Cohort Breakdown`, `Разбивка по когортам`,
		`Use this to check whether the comparison is fair across app version, SDK, device, process and network.`, `Используй это, чтобы проверить честность сравнения по версии приложения, SDK, устройству, процессу и сети.`,
		`Process Mix`, `Состав процессов`,
		`Network Routes`, `Сетевые маршруты`,
		`Slowest routes by p95 latency, failures, bytes and owner attribution.`, `Самые медленные маршруты по p95 latency, ошибкам, байтам и owner attribution.`,
		`Route Table`, `Таблица маршрутов`,
		`UI Smoothness`, `Плавность UI`,
		`Screens ranked by jank rate and frame latency.`, `Экраны, отсортированные по jank rate и latency кадров.`,
		`Screen Table`, `Таблица экранов`,
		`Attribution Hotspots`, `Горячие точки attribution`,
		`Owners, classes and stack hints with the largest measured impact.`, `Owners, классы и stack hints с наибольшим измеренным влиянием.`,
		`Memory And Retention`, `Память и удержанные объекты`,
		`PSS, available memory, low-memory samples and retained object age buckets.`, `PSS, доступная память, low-memory samples и age buckets удержанных объектов.`,
		`Custom Metrics`, `Пользовательские метрики`,
		`Counters, gauges and AndroidX JankStats bridge metrics when available.`, `Счетчики, gauge-метрики и AndroidX JankStats bridge, если они доступны.`,
		`Run Context`, `Контекст прогона`,
		`Cohorts keep comparisons honest: app, build, SDK, device, process and network.`, `Когорты помогают честно сравнивать app, build, SDK, device, process и network.`,
		`Health Gauges`, `Индикаторы здоровья`,
		`Signal Rings`, `Кольцевые индикаторы`,
		`>Battery<`, `>Батарея<`,
		`>Free RAM<`, `>Свободная RAM<`,
		`>Free storage<`, `>Свободное хранилище<`,
		`>Android<`, `>Android<`,
		`>CPU ABI<`, `>CPU ABI<`,
		`>Hardware<`, `>Железо<`,
		`>Brand<`, `>Бренд<`,
		`Route Details`, `Детали маршрутов`,
		`Screen Details`, `Детали экранов`,
		`Owner Details`, `Детали owners`,
		`Memory Details`, `Детали памяти`,
		`Metric Details`, `Детали метрик`,
		`Context Details`, `Детали контекста`,
		`Candidate Route, Screen And Owner Details`, `Детали маршрутов, экранов и owners candidate`,
		`Cohort Details`, `Детали когорт`,
		`Heuristic Verdict`, `Эвристический итог`,
		`Rule-based triage over all collected signals. Treat it as a review checklist, not as a mathematical proof.`, `Эвристический разбор всех собранных сигналов. Это проверочный список для ревью, а не математическое доказательство.`,
		`Rule-based triage over all comparison deltas and cohort warnings. Treat it as a review checklist, not as a mathematical proof.`, `Эвристический разбор изменений и предупреждений по когортам. Это проверочный список для ревью, а не математическое доказательство.`,
		`Overall status`, `Общий статус`,
		`Findings`, `Находки`,
		`Recommendations`, `Рекомендации`,
		`No heuristic findings.`, `Нет эвристических находок.`,
		`No extra recommendations.`, `Нет дополнительных рекомендаций.`,
		`>Routes<`, `>Маршруты<`,
		`>Route<`, `>Маршрут<`,
		`>Count<`, `>Количество<`,
		`>Failures<`, `>Ошибки<`,
		`>Avg TTFB<`, `>Средний TTFB<`,
		`>Owner / Class<`, `>Owner / класс<`,
		`>Owner<`, `>Owner<`,
		`>Screens<`, `>Экраны<`,
		`>Screen<`, `>Экран<`,
		`>Windows<`, `>Окна<`,
		`>Frames<`, `>Кадры<`,
		`>Janky<`, `>Janky<`,
		`>Jank rate<`, `>Jank rate<`,
		`>Avg FPS<`, `>Средний FPS<`,
		`>Min FPS<`, `>Мин. FPS<`,
		`>p95 frame<`, `>p95 кадра<`,
		`>max p99<`, `>макс. p99<`,
		`>Kind<`, `>Тип<`,
		`>Total<`, `>Итого<`,
		`>Stack hint<`, `>Stack hint<`,
		`>Value<`, `>Значение<`,
		`>Details<`, `>Детали<`,
		`>Class / Owner<`, `>Класс / owner<`,
		`>Age<`, `>Возраст<`,
		`>Name<`, `>Имя<`,
		`>Average<`, `>Среднее<`,
		`>Metric<`, `>Метрика<`,
		`>App Versions<`, `>Версии приложения<`,
		`>Devices<`, `>Устройства<`,
		`>Process Breakdown<`, `>Разбивка по процессам<`,
		`>Process<`, `>Процесс<`,
		`>Sessions<`, `>Сессии<`,
		`>Network Samples<`, `>Сэмплы сети<`,
		`>Combined Cohorts<`, `>Объединенные когорты<`,
		`>Counters<`, `>Счетчики<`,
		`>Gauges<`, `>Gauge-метрики<`,
		`>Memory And Metrics<`, `>Память и метрики<`,
		`>Signal<`, `>Сигнал<`,
		`>Cohort<`, `>Когорта<`,
		`>Baseline process<`, `>Baseline процесс<`,
		`>Candidate process<`, `>Candidate процесс<`,
		`>Change<`, `>Изменение<`,
		`>Regression<`, `>Регрессия<`,
		`>Severity<`, `>Серьезность<`,
		`>Confidence<`, `>Доверие<`,
		`>Sample<`, `>Выборка<`,
		`>Interval<`, `>Интервал<`,
		`HTTP p95`, `HTTP p95`,
		`HTTP failures`, `HTTP ошибки`,
		`UI jank rate`, `UI jank rate`,
		`UI avg FPS`, `Средний UI FPS`,
		`Main-thread stall max`, `Макс. main-thread stall`,
		`Max PSS`, `Макс. PSS`,
		`Min available memory`, `Мин. доступная память`,
		`UID RX max`, `Макс. UID RX`,
		`UID TX max`, `Макс. UID TX`,
		`Retained objects`, `Удержанные объекты`,
		`Process mix`, `Состав процессов`,
		`App version mix`, `Состав версий приложения`,
		`SDK mix`, `Состав SDK`,
		`Device mix`, `Состав устройств`,
		`Network mix`, `Состав сетей`,
		`Cohort mix`, `Состав когорт`,
		`<div class="label">Average FPS</div>`, `<div class="label">Средний FPS</div>`,
		`<div class="label">Max stall</div>`, `<div class="label">Макс. stall</div>`,
		`<div class="label">UID RX max</div>`, `<div class="label">Макс. UID RX</div>`,
		` requests, `, ` запросов, `,
		` requests<`, ` запросов<`,
		` failed`, ` ошибок`,
		` frames`, ` кадров`,
		`min free`, `мин. свободно`,
		`min `, `мин. `,
		` stall events`, ` stall-событий`,
		`retained `, `retained `,
		`TX max `, `макс. TX `,
		`validated yes`, `проверена: да`,
		`validated no`, `проверена: нет`,
		`metered yes`, `лимитная: да`,
		`metered no`, `лимитная: нет`,
		`VPN yes`, `VPN да`,
		`VPN no`, `VPN нет`,
		`not charging`, `не заряжается`,
		`charging`, `заряжается`,
		`discharging`, `разряжается`,
		`full`, `полная`,
		`total`, `всего`,
		`supported`, `поддерживаются`,
		`security patch unknown`, `патч безопасности неизвестен`,
		`security patch`, `патч безопасности`,
		`app data partition`, `раздел данных приложения`,
		`board `, `плата `,
		`product `, `продукт `,
		`brand `, `бренд `,
		`process `, `процесс `,
		`avg FPS`, `средний FPS`,
		`candidate jank`, `candidate jank`,
		`candidate fail`, `candidate ошибки`,
		`candidate FPS`, `candidate FPS`,
		`avg FPS`, `средний FPS`,
		`No HTTP events.`, `Нет HTTP-событий.`,
		`No UI window events.`, `Нет UI-window событий.`,
		`No owner attribution yet.`, `Пока нет owner attribution.`,
		`No memory events.`, `Нет событий памяти.`,
		`No retained-object events.`, `Нет событий retained objects.`,
		`No counters.`, `Нет counters.`,
		`No gauges.`, `Нет gauges.`,
		`No JankStats metrics.`, `Нет метрик JankStats.`,
		`No process metadata.`, `Нет metadata процессов.`,
		`No context events.`, `Нет context events.`,
		`No cohort metadata.`, `Нет metadata когорт.`,
		`No per-log baseline details were embedded.`, `Детали baseline-логов не встроены.`,
		`No per-log candidate details were embedded.`, `Детали candidate-логов не встроены.`,
		`No owners.`, `Нет owners.`,
		`content: "open";`, `content: "открыть";`,
		`content: "close";`, `content: "закрыть";`,
		`<td class="sev-high">high</td>`, `<td class="sev-high">высокая</td>`,
		`<td class="sev-medium">medium</td>`, `<td class="sev-medium">средняя</td>`,
		`<td class="sev-ok">ok</td>`, `<td class="sev-ok">норма</td>`,
		`<td>high</td>`, `<td>высокая</td>`,
		`<td>medium</td>`, `<td>средняя</td>`,
		`<td>low</td>`, `<td>низкая</td>`,
		`<td>same</td>`, `<td>без изменений</td>`,
		`changed`, `изменено`,
		`app version mix differs: baseline`, `состав версий приложения отличается: baseline`,
		`SDK mix differs: baseline`, `состав SDK отличается: baseline`,
		`device mix differs: baseline`, `состав устройств отличается: baseline`,
		`process mix differs: baseline`, `состав процессов отличается: baseline`,
		`network mix differs: baseline`, `состав сетей отличается: baseline`,
		`cohort mix differs: baseline`, `состав когорт отличается: baseline`,
		`, candidate`, `, candidate`,
	)
	return replacer.Replace(html)
}
