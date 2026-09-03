package report

import (
	"fmt"
	"sort"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/mathanalysis"
)

type heuristicCard struct {
	Severity string
	Title    string
	Detail   string
}

type heuristicSummary struct {
	Severity string
	Status   string
	Summary  string
	Cards    []heuristicCard
}

func inspectMathHeuristic(report mathanalysis.MathReport) heuristicSummary {
	summary := heuristicSummary{Severity: "ok", Status: "Математический профиль спокоен", Summary: "Критичных математических сигналов не найдено."}
	for _, section := range report.Sections {
		if severityRank(section.Status) > severityRank(summary.Severity) {
			summary.Severity = section.Status
		}
	}
	switch summary.Severity {
	case "high":
		summary.Status = "Требуется разбор"
		summary.Summary = "Есть сильные математические сигналы деградации. Начните с карточек ниже и проверьте связанные маршруты, источники и контекст."
	case "medium":
		summary.Status = "Есть сигналы для проверки"
		summary.Summary = "Обнаружены предупреждения. Их стоит подтвердить повторным прогоном и связать с конкретными владельцами работ."
	}
	if len(report.NetworkLoops) > 0 {
		loop := report.NetworkLoops[0]
		target := firstNonEmpty(loop.Route, loop.Owner, "сетевой сценарий")
		summary.Cards = append(summary.Cards, heuristicCard{Severity: networkLoopCardSeverity(loop.Confidence, loop.BurnScore), Title: "Признак сетевого цикла", Detail: fmt.Sprintf("Проверьте %s: предполагаемый период %.1f сек, уверенность %.2f, условная нагрузка %.1f. Это гипотеза, а не доказанная причина.", target, float64(loop.PeriodMS)/1000, loop.Confidence, loop.BurnScore)})
	}
	if len(report.CausalGraph.OwnerScores) > 0 {
		owner := report.CausalGraph.OwnerScores[0]
		ownerLabel := reportValue(owner.Owner, "место запуска не записано")
		detail := fmt.Sprintf("%s чаще других совпадало по времени с плохими состояниями и сетевыми циклами; условная оценка %.2f. Проверьте трассировку и код: совпадение не доказывает причину.", ownerLabel, owner.Score)
		if isUnknownReportValue(owner.Owner) {
			detail += " Чтобы восстановить место запуска, проверьте инструментирование сетевого клиента и включение пакета с кодом, который запускает запрос."
		}
		summary.Cards = append(summary.Cards, heuristicCard{Severity: "medium", Title: "Место запуска, чаще связанное с проблемами", Detail: detail})
	}
	if context, ok := topProblemSignalContext(report.Summary); ok {
		summary.Cards = append(summary.Cards, heuristicCard{Severity: signalContextCardSeverity(context), Title: "Операция со связанными сигналами", Detail: fmt.Sprintf("%s: проблем %d, лишних сообщений журнала %d, граница верхних 5%% HTTP-задержек %d мс, максимальная пауза %d мс.", signalContextLabel(context.Screen, context.Operation, context.Owner), context.ProblemCount, context.LogSpam, context.HTTPP95MS, context.StallMaxMS)})
	}
	if score, ok := topIntegralScore(report.IntegralScores); ok {
		summary.Cards = append(summary.Cards, heuristicCard{Severity: score.Severity, Title: score.Title, Detail: fmt.Sprintf("%.1f %s. %s", score.Value, score.Unit, score.Explanation)})
	}
	if len(summary.Cards) == 0 {
		summary.Cards = append(summary.Cards, heuristicCard{Severity: "ok", Title: "Что проверить первым", Detail: "Используйте отчёт как контрольную точку. При предупреждении начните с измеренного сигнала, затем проверьте временную шкалу, исходные события и граф связей."})
	}
	return summary
}

func compareMathHeuristic(report mathanalysis.CompareMathReport) heuristicSummary {
	summary := heuristicSummary{Severity: "ok", Status: "Сравнение выглядит стабильным", Summary: "Сильных математических ухудшений между базой и кандидатом не найдено."}
	for _, section := range report.Sections {
		if severityRank(section.Status) > severityRank(summary.Severity) {
			summary.Severity = section.Status
		}
	}
	switch summary.Severity {
	case "high":
		summary.Status = "Проверяемый прогон требует расследования"
		summary.Summary = "Есть сильные математические дельты. Проверьте, совпадают ли они с изменениями маршрутов, экранов, памяти или контекста устройства."
	case "medium":
		summary.Status = "Есть предупреждения по кандидату"
		summary.Summary = "Найдены умеренные отличия. Подтвердите их повторным прогоном перед инженерным выводом."
	}
	if len(report.RobustDeltas) > 0 {
		for _, delta := range report.RobustDeltas {
			if delta.Severity == "high" || delta.Severity == "medium" {
				detail := delta.Summary
				if delta.Comparable && delta.DeltaPctAvailable {
					detail = fmt.Sprintf("%s / %s: p95 изменился на %+.1f %s (%+.1f%%), доверие %s.", delta.Dimension, delta.Metric, delta.P95Delta, delta.Unit, delta.P95DeltaPct, delta.Confidence)
				}
				summary.Cards = append(summary.Cards, heuristicCard{Severity: delta.Severity, Title: "Распределение изменилось", Detail: detail})
				break
			}
		}
	}
	if len(report.NetworkLoopDeltas) > 0 {
		delta := report.NetworkLoopDeltas[0]
		target := firstNonEmpty(delta.Route, delta.Owner, "сетевой цикл")
		summary.Cards = append(summary.Cards, heuristicCard{Severity: delta.Severity, Title: "Изменение кандидата сетевого цикла", Detail: fmt.Sprintf("%s: изменение условной нагрузки %+.1f, изменение уверенности %+.2f.", target, delta.BurnDelta, delta.ConfidenceDelta)})
	}
	if len(report.CausalDeltas) > 0 {
		delta := report.CausalDeltas[0]
		summary.Cards = append(summary.Cards, heuristicCard{Severity: delta.Severity, Title: "Граф связей изменился", Detail: delta.Summary})
	}
	if row, ok := topSignalContextDelta(report.Comparison.Baseline, report.Comparison.Candidate); ok && row.Severity != "ok" {
		summary.Cards = append(summary.Cards, heuristicCard{Severity: row.Severity, Title: "Связанные сигналы операции ухудшились", Detail: fmt.Sprintf("%s: изменение числа проблем %d, лишних сообщений журнала %d, границы верхних 5%% HTTP-задержек %d мс, подтормаживаний интерфейса %+.2f п.п.", signalContextLabel(row.Screen, row.Operation, row.Owner), row.DeltaProblems, row.DeltaLogSpam, row.DeltaHTTPP95MS, row.DeltaJankPct)})
	}
	if len(summary.Cards) == 0 {
		summary.Cards = append(summary.Cards, heuristicCard{Severity: "ok", Title: "Что проверить первым", Detail: "Сохраните сравнение как контрольную точку. При следующем ухудшении начните с распределений, временной шкалы и исходных событий, а граф связей используйте только как список гипотез."})
	}
	return summary
}

func topProblemSignalContext(summary analyze.Summary) (analyze.SignalContextStats, bool) {
	if len(summary.SignalContexts) == 0 {
		return analyze.SignalContextStats{}, false
	}
	best := summary.SignalContexts[0]
	for _, context := range summary.SignalContexts[1:] {
		if signalContextProblemScore(context) > signalContextProblemScore(best) {
			best = context
		}
	}
	if signalContextProblemScore(best) == 0 {
		return analyze.SignalContextStats{}, false
	}
	return best, true
}

func signalContextProblemScore(context analyze.SignalContextStats) uint64 {
	score := saturatingMulUint64(context.ProblemCount, 10_000)
	score = saturatingAddUint64(score, saturatingMulUint64(context.LogSpam, 10))
	score = saturatingAddUint64(score, saturatingMulUint64(nonNegativeInt(context.StallCount), 1_000))
	score = saturatingAddUint64(score, context.StallMaxMS)
	score = saturatingAddUint64(score, context.HTTPP95MS)
	return saturatingAddUint64(score, context.UIJank)
}

func signalContextCardSeverity(context analyze.SignalContextStats) string {
	if context.ProblemCount >= 10 || context.StallMaxMS >= 1000 || context.HTTPP95MS >= 1500 {
		return "high"
	}
	if context.ProblemCount > 0 || context.LogSpam >= 50 || context.StallMaxMS >= 250 || context.HTTPP95MS >= 500 {
		return "medium"
	}
	return "ok"
}

type operationContextInsight struct {
	Title       string
	Severity    string
	Status      string
	Context     string
	Summary     string
	Impact      string
	FirstCheck  string
	SignalCount int
	Tooltip     string
}

func operationContextInsights(summary analyze.Summary) []operationContextInsight {
	insights := make([]operationContextInsight, 0, len(summary.SignalContexts))
	for _, context := range summary.SignalContexts {
		if !signalContextNeedsAttention(context) {
			continue
		}
		severity, status := operationContextVerdict(context)
		signals := operationContextSignalSummary(context)
		insights = append(insights, operationContextInsight{
			Title:       operationContextTitle(context),
			Severity:    severity,
			Status:      status,
			Context:     operationContextDescription(context),
			Summary:     strings.Join(signals, " "),
			Impact:      operationContextImpact(context),
			FirstCheck:  operationContextFirstCheck(context),
			SignalCount: operationContextSignalCount(context),
			Tooltip:     "Карточка объединяет сигналы, записанные для одной операции и экрана, и предлагает первый практический шаг проверки.",
		})
	}
	sort.SliceStable(insights, func(i, j int) bool {
		left := severityOrder(insights[i].Severity)
		right := severityOrder(insights[j].Severity)
		if left != right {
			return left > right
		}
		if insights[i].SignalCount != insights[j].SignalCount {
			return insights[i].SignalCount > insights[j].SignalCount
		}
		return insights[i].Title < insights[j].Title
	})
	if len(insights) > 8 {
		return insights[:8]
	}
	return insights
}

func signalContextNeedsAttention(context analyze.SignalContextStats) bool {
	return context.ProblemCount > 0 || context.HTTPFailed > 0 || context.HTTPP95MS >= 700 ||
		context.StallCount > 0 || context.UIJank > 0 || context.LogSpam >= 10 || context.MemoryMaxKB >= 256*1024
}

func operationContextVerdict(context analyze.SignalContextStats) (string, string) {
	switch {
	case context.StallMaxMS >= 1500 || context.UIJankPct >= 20 || context.HTTPFailed >= 3:
		return "critical", "критично"
	case context.StallMaxMS >= 700 || context.UIJankPct >= 10 || context.HTTPP95MS >= 1500 || context.HTTPFailed > 0:
		return "high", "высокий риск"
	case context.StallCount > 0 || context.UIJankPct >= 5 || context.HTTPP95MS >= 700 || context.LogSpam >= 50 || context.ProblemCount > 0:
		return "medium", "нужно проверить"
	default:
		return "low", "наблюдение"
	}
}

func operationContextTitle(context analyze.SignalContextStats) string {
	parts := make([]string, 0, 2)
	if !isUnknownReportValue(context.Operation) {
		parts = append(parts, context.Operation)
	} else if !isUnknownReportValue(context.Screen) {
		parts = append(parts, context.Screen)
	} else if !isUnknownReportValue(context.Owner) {
		parts = append(parts, context.Owner)
	} else if !isUnknownReportValue(context.RouteSample) {
		parts = append(parts, context.RouteSample)
	} else {
		parts = append(parts, "Операция без названия")
	}
	return strings.Join(parts, " · ")
}

func operationContextDescription(context analyze.SignalContextStats) string {
	parts := make([]string, 0, 4)
	if !isUnknownReportValue(context.Screen) {
		parts = append(parts, "экран "+context.Screen)
	}
	if !isUnknownReportValue(context.Owner) {
		parts = append(parts, "источник "+context.Owner)
	}
	if !isUnknownReportValue(context.RouteSample) {
		parts = append(parts, "маршрут "+context.RouteSample)
	}
	if len(parts) == 0 {
		return "Точное место не размечено; добавьте экран, операцию или источник работ."
	}
	return strings.Join(parts, " · ")
}

func operationContextSignalSummary(context analyze.SignalContextStats) []string {
	parts := make([]string, 0, 6)
	if context.UIFrames > 0 && context.UIJank > 0 {
		parts = append(parts, fmt.Sprintf("Медленными были %s из %s (%.1f%%).", russianCount(context.UIJank, "кадр", "кадра", "кадров"), russianCount(context.UIFrames, "кадра", "кадров", "кадров"), context.UIJankPct))
	}
	if context.StallCount > 0 {
		parts = append(parts, fmt.Sprintf("Главный поток останавливался %s; максимум — %d мс.", russianCount(context.StallCount, "раз", "раза", "раз"), context.StallMaxMS))
	}
	if context.HTTPCount > 0 {
		parts = append(parts, operationContextHTTPText(context))
	}
	if context.LogSpam > 0 {
		parts = append(parts, fmt.Sprintf("Лишних записей в лог — %d.", context.LogSpam))
	}
	if context.ProblemCount > 0 {
		parts = append(parts, fmt.Sprintf("Объединённых проблемных сигналов — %d.", context.ProblemCount))
	}
	if context.MemoryMaxKB > 0 {
		parts = append(parts, fmt.Sprintf("Память в этом контексте доходила до %s.", humanDataSizeKB(context.MemoryMaxKB)))
	}
	return parts
}

func operationContextHTTPText(context analyze.SignalContextStats) string {
	var text string
	switch {
	case context.HTTPCount == 1:
		text = fmt.Sprintf("Единственный сетевой вызов занял до %d мс", context.HTTPP95MS)
	case context.HTTPCount < 20:
		text = fmt.Sprintf("В небольшой выборке из %d сетевых вызовов верхняя задержка составила %d мс", context.HTTPCount, context.HTTPP95MS)
	default:
		text = fmt.Sprintf("У 95%% из %d сетевых вызовов длительность не превышала %d мс", context.HTTPCount, context.HTTPP95MS)
	}
	if context.HTTPFailed > 0 {
		text += fmt.Sprintf("; с ошибкой завершилось %d", context.HTTPFailed)
	}
	return text + "."
}

func operationContextSignalCount(context analyze.SignalContextStats) int {
	count := 0
	for _, present := range []bool{
		context.UIJank > 0,
		context.StallCount > 0,
		context.HTTPCount > 0,
		context.LogSpam > 0,
		context.ProblemCount > 0,
		context.MemoryMaxKB > 0,
	} {
		if present {
			count++
		}
	}
	return count
}

func operationContextImpact(context analyze.SignalContextStats) string {
	impacts := make([]string, 0, 3)
	if context.UIJank > 0 || context.StallCount > 0 {
		impacts = append(impacts, "рывки интерфейса и задержка реакции на действие")
	}
	if context.HTTPFailed > 0 {
		impacts = append(impacts, "ошибка или незавершённый пользовательский сценарий")
	} else if context.HTTPP95MS >= 700 {
		impacts = append(impacts, "долгое ожидание данных")
	}
	if context.MemoryMaxKB >= 256*1024 {
		impacts = append(impacts, "рост числа сборок мусора и риск нехватки памяти")
	}
	if context.LogSpam >= 50 {
		impacts = append(impacts, "лишняя нагрузка на процессор и хранилище из-за частого ведения журнала")
	}
	if len(impacts) == 0 {
		return "Влияние на пользователя по текущим данным невелико, но сигнал стоит перепроверить."
	}
	return "Для пользователя это может означать: " + strings.Join(impacts, "; ") + "."
}

func operationContextFirstCheck(context analyze.SignalContextStats) string {
	place := "в этой операции"
	if !isUnknownReportValue(context.Owner) {
		place = "в " + context.Owner
	}
	switch {
	case context.StallCount > 0:
		return "Сначала откройте трассу главного потока " + place + " и найдите синхронную работу вокруг самой длинной паузы."
	case context.HTTPFailed > 0:
		return "Сначала проверьте ошибки, повторные вызовы и обработку ответа маршрута " + reportValue(context.RouteSample, "в этой операции") + "."
	case context.HTTPP95MS >= 700:
		return "Сначала проверьте длительный и повторный сетевой вызов маршрута " + reportValue(context.RouteSample, "в этой операции") + "."
	case context.UIJank > 0:
		return "Сначала профилируйте отрисовку экрана и работу " + place + " во время медленных кадров."
	case context.LogSpam >= 10:
		return "Сначала сократите частое логирование " + place + " и повторите операцию."
	default:
		return "Повторите операцию с теми же входными данными и сравните длительность, память и плавность."
	}
}

func severityOrder(value string) int {
	switch value {
	case "critical":
		return 4
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	default:
		return 0
	}
}

func primaryCategoryCoverage(items []analyze.CategoryCoverage) []analyze.CategoryCoverage {
	visible := make([]analyze.CategoryCoverage, 0, len(items))
	for _, item := range items {
		if item.FindingCount > 0 || item.Status == "healthy" {
			visible = append(visible, item)
		}
	}
	return visible
}

func findingCategoryCoverage(items []analyze.CategoryCoverage) []analyze.CategoryCoverage {
	visible := make([]analyze.CategoryCoverage, 0, len(items))
	for _, item := range items {
		if item.FindingCount > 0 || item.Category == analyze.ProblemCategoryDependencyInjection {
			visible = append(visible, item)
		}
	}
	return visible
}

func hiddenCoverageSummary(items []analyze.CategoryCoverage) string {
	labels := make([]string, 0, len(items))
	for _, item := range items {
		if item.FindingCount == 0 && item.Status != "healthy" {
			labels = append(labels, item.Label)
		}
	}
	if len(labels) == 0 {
		return ""
	}
	return "Не проверено из-за отсутствия или малого объёма данных: " + strings.Join(labels, ", ") + ". Эти категории не показаны как нулевые, потому что ноль проблем здесь не доказан."
}

func collectionWindowNotice(summary analyze.Summary) string {
	reason := ""
	const prefix = "jankhunter.runtime.enabled.reason."
	const suffix = ".count"
	for _, item := range summary.Counters {
		if item.Value == 0 || !strings.HasPrefix(item.Name, prefix) || !strings.HasSuffix(item.Name, suffix) {
			continue
		}
		reason = strings.TrimSuffix(strings.TrimPrefix(item.Name, prefix), suffix)
		break
	}
	if reason == "" {
		return ""
	}
	inactiveBeforeStartMS := uint64(0)
	for _, item := range summary.Gauges {
		if item.Name == "jankhunter.runtime.collection_inactive_before_start_ms" {
			inactiveBeforeStartMS = item.Value
			break
		}
	}
	message := "Сбор был включён переключателем «" + reason + "» уже во время работы приложения. В отчёт вошли только события за " + humanDuration(summary.DurationMS) + " после включения; более ранние действия отсутствуют в журнале."
	if inactiveBeforeStartMS > 0 {
		message = "До включения сбора приложение работало " + humanDuration(inactiveBeforeStartMS) + ". Затем переключатель «" + reason + "» включил Jank Hunter, и в отчёт вошли события за следующие " + humanDuration(summary.DurationMS) + "."
	}
	return message
}

type customMetricInsight struct {
	Title    string
	Severity string
	Count    int
	Names    string
	Relation string
	Action   string
	Tooltip  string
}

func customMetricInsights(summary analyze.Summary) []customMetricInsight {
	type metricGroup struct {
		id    string
		title string
		names []string
	}
	groups := []metricGroup{
		{id: "memory", title: "Память и сборка мусора"},
		{id: "network", title: "Сеть"},
		{id: "ui", title: "Интерфейс и отрисовка"},
		{id: "tasks", title: "Очереди и задачи"},
		{id: "io", title: "Файлы и база данных"},
		{id: "other", title: "Остальные показатели"},
	}
	add := func(item analyze.NamedValue) {
		id := customMetricGroupID(item.Name)
		for index := range groups {
			if groups[index].id == id {
				groups[index].names = append(groups[index].names, item.Name)
				return
			}
		}
	}
	for _, item := range summary.Counters {
		add(item)
	}
	for _, item := range summary.Gauges {
		add(item)
	}
	for _, item := range summary.JankStats {
		add(item)
	}

	insights := make([]customMetricInsight, 0, len(groups))
	for _, group := range groups {
		if len(group.names) == 0 {
			continue
		}
		names := group.names
		if len(names) > 4 {
			names = names[:4]
		}
		nameSummary := strings.Join(names, ", ")
		if omitted := len(group.names) - len(names); omitted > 0 {
			nameSummary += fmt.Sprintf(" и ещё %d", omitted)
		}
		severity, relation, action := customMetricRelation(group.id, summary)
		insights = append(insights, customMetricInsight{
			Title:    group.title,
			Severity: severity,
			Count:    len(group.names),
			Names:    nameSummary,
			Relation: relation,
			Action:   action,
			Tooltip:  "Группа объединяет пользовательские показатели по смыслу и подсказывает, с какими основными сигналами отчёта их проверять вместе.",
		})
	}
	return insights
}

func customMetricGroupID(name string) string {
	value := strings.ToLower(name)
	value = strings.TrimPrefix(value, "jankhunter.")
	switch {
	case strings.Contains(value, "gc"), strings.Contains(value, "alloc"), strings.Contains(value, "heap"), strings.Contains(value, "memory"), strings.Contains(value, "pss"), strings.Contains(value, "object_watcher"), strings.Contains(value, "retained"), strings.Contains(value, "leak"):
		return "memory"
	case strings.Contains(value, "http"), strings.Contains(value, "network"), strings.Contains(value, "request"), strings.Contains(value, "socket"):
		return "network"
	case strings.Contains(value, "frame"), strings.Contains(value, "jank"), strings.Contains(value, "fps"), strings.Contains(value, "render"), strings.Contains(value, "layout"):
		return "ui"
	case strings.Contains(value, "queue"), strings.Contains(value, "thread"), strings.Contains(value, "task"), strings.Contains(value, "cpu"), strings.Contains(value, "dispatcher"):
		return "tasks"
	case strings.Contains(value, "storage"), strings.Contains(value, "disk"), strings.Contains(value, "file"), strings.Contains(value, "database"), strings.Contains(value, "sqlite"), strings.Contains(value, "io."):
		return "io"
	default:
		return "other"
	}
}

func customMetricRelation(group string, summary analyze.Summary) (string, string, string) {
	switch group {
	case "memory":
		if summary.Retained > 0 || summary.MemoryMaxKB > 0 {
			return "medium", fmt.Sprintf("В этом же прогоне: удержанных объектов — %d, максимум занятой процессом памяти — %s. Смотрите эти значения вместе с выделением памяти и частотой сборки мусора.", summary.Retained, humanDataSizeKB(summary.MemoryMaxKB)), "Повторите сценарий и проверьте, возвращаются ли память и число удержаний к исходному уровню."
		}
		return "low", "Основных событий памяти рядом не записано.", "Используйте эти показатели для сравнения одинаковых сценариев между прогонами."
	case "network":
		if summary.HTTPCount > 0 {
			return "medium", fmt.Sprintf("В этом же прогоне записано %s; задержка верхней части выборки — %d мс.", russianCount(summary.HTTPCount, "сетевой вызов", "сетевых вызова", "сетевых вызовов"), summary.HTTPP95MS), "Сопоставьте пользовательские показатели сети с маршрутом и сценарием в разделе проблем."
		}
		return "low", "Основных сетевых событий рядом не записано.", "Проверьте, записывает ли сценарий маршрут и источник сетевой работы."
	case "ui":
		if summary.UIFrames > 0 {
			return "medium", fmt.Sprintf("Основной сборщик интерфейса увидел %.1f%% медленных кадров из %d.", summary.UIJankPct, summary.UIFrames), "Сверьте название показателя с экраном и причиной в разделе «Плавность интерфейса»."
		}
		return "low", "Основные события интерфейса в этом прогоне не записаны.", "Для связи с экраном повторите сценарий со включённым сбором кадров."
	case "tasks":
		if summary.StallCount > 0 {
			return "medium", fmt.Sprintf("В этом же прогоне главный поток останавливался %s; максимум — %d мс.", russianCount(summary.StallCount, "раз", "раза", "раз"), summary.StallMaxMS), "Ищите рост очереди рядом с длинными задачами главного потока."
		}
		return "low", "Длинные паузы главного потока в этом прогоне не записаны.", "Сравнивайте размер очереди в одинаковых сценариях и на одинаковом устройстве."
	case "io":
		if count := totalTypedIOOperations(summary); count > 0 {
			return "medium", fmt.Sprintf("Типизированных файловых операций в прогоне — %d.", count), "Сопоставьте рост показателя с операцией, потоком и источником в подробном анализе хранилища."
		}
		return "low", "Типизированные файловые операции в этом прогоне не записаны.", "Для точной привязки добавьте запись операции с потоком и источником."
	default:
		return "low", "Эти показатели пока не относятся к известной группе Jank Hunter.", "Добавьте понятное имя и описание единицы измерения, затем сравнивайте одинаковые сценарии."
	}
}

func topSignalContextDelta(baseline, candidate analyze.Summary) (signalContextCompareRow, bool) {
	rows := signalContextCompareRows(baseline, candidate)
	if len(rows) == 0 {
		return signalContextCompareRow{}, false
	}
	return rows[0], true
}

func networkLoopCardSeverity(confidence, burn float64) string {
	if confidence >= 0.70 && burn >= 8 {
		return "high"
	}
	if confidence >= 0.45 || burn >= 4 {
		return "medium"
	}
	return "ok"
}

func topIntegralScore(scores []mathanalysis.IntegralScore) (mathanalysis.IntegralScore, bool) {
	if len(scores) == 0 {
		return mathanalysis.IntegralScore{}, false
	}
	best := scores[0]
	for _, score := range scores[1:] {
		if severityRank(score.Severity) > severityRank(best.Severity) || (score.Severity == best.Severity && score.Value > best.Value) {
			best = score
		}
	}
	return best, true
}

func leakModeLabel(mode string) string {
	switch mode {
	case analyze.LeakModeHeap:
		return "режим дампа кучи"
	default:
		return "легкий режим"
	}
}

func leakDeltaStatusClass(status string) string {
	switch status {
	case analyze.LeakDeltaNew, analyze.LeakDeltaWorse, analyze.LeakDeltaRegressed:
		return "sev-high"
	case analyze.LeakDeltaBetter, analyze.LeakDeltaResolved, analyze.LeakDeltaImproved:
		return "sev-ok"
	default:
		return "sev-medium"
	}
}
