package report

import (
	"fmt"
	"sort"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
)

const (
	defaultFrameDeadlineUS      = uint64(16_667)
	defaultFrameTailThresholdMS = uint64(32)
	maxUICauseInsights          = 4
)

type uiScreenInsight struct {
	Screen      string
	Severity    string
	Status      string
	Headline    string
	Observation string
	Diagnosis   string
	Nearby      string
	Where       string
	Action      string
	Causes      []uiCauseInsight
	Tooltip     string
}

type uiCauseInsight struct {
	Relation      string
	RelationClass string
	Title         string
	Evidence      string
	Explanation   string
	Where         string
	Action        string
	rank          int
	magnitude     uint64
	stableKey     string
}

func uiScreenInsights(summary analyze.Summary) []uiScreenInsight {
	insights := make([]uiScreenInsight, 0, len(summary.Screens))
	for _, screen := range summary.Screens {
		severity, status, headline := uiScreenVerdict(screen)
		observation := uiScreenObservation(screen)
		causes := uiCauseInsights(summary, screen)
		nearby, where, action := uiRelatedSignals(summary, screen.Screen)
		if len(causes) > 0 {
			where = causes[0].Where
			action = causes[0].Action
		}
		insights = append(insights, uiScreenInsight{
			Screen:      reportValue(screen.Screen, "экран не указан"),
			Severity:    severity,
			Status:      status,
			Headline:    headline,
			Observation: observation,
			Diagnosis:   uiDiagnosis(screen, causes),
			Nearby:      nearby,
			Where:       where,
			Action:      action,
			Causes:      causes,
			Tooltip:     "Карточка объединяет плавность экрана, работу главного потока, I/O, вызовы кода, сеть, память и логирование. Уровень связи показывает, что зафиксировано напрямую, а что ещё нужно проверить.",
		})
	}
	return insights
}

func uiScreenObservation(screen analyze.ScreenStats) string {
	if screen.JankyFrames == 0 && uiHasSlowFrameTail(screen) {
		return fmt.Sprintf(
			"Системный признак подтормаживания не сработал ни для одного из %d кадров, но верхние 5%% кадров занимали до %d мс, а отдельные худшие — до %d мс при целевом времени %d мс. Это подтверждает длинные кадры: значение 0%% здесь не означает норму.",
			screen.Frames,
			screen.FrameP95MS,
			screen.FrameP99MS,
			uiFrameDeadlineMS(screen),
		)
	}
	observation := fmt.Sprintf(
		"%s из %s были медленными (%.1f%%).",
		russianCount(screen.JankyFrames, "кадр", "кадра", "кадров"),
		russianCount(screen.Frames, "кадра", "кадров", "кадров"),
		screen.JankRatePct,
	)
	if screen.AvgFPS > 0 {
		observation += fmt.Sprintf(" Средняя скорость — %.1f FPS", screen.AvgFPS)
		if screen.MinFPS > 0 {
			observation += fmt.Sprintf(", минимальная — %.1f FPS", screen.MinFPS)
		}
		observation += "."
	}
	if screen.Frames >= 30 && screen.JankyFrames == 0 && screen.AvgFPS > 0 && screen.AvgFPS < 40 {
		observation += " Низкий FPS расходится с отсутствием медленных кадров: это бывает при редкой отрисовке или неполных данных о длительности кадров."
	}
	if screen.Frames < 30 {
		observation += " Кадров мало, поэтому повторите тот же сценарий перед окончательным выводом."
	}
	return observation
}

func uiScreenVerdict(screen analyze.ScreenStats) (string, string, string) {
	if screen.Frames < 30 {
		if screen.JankyFrames > 0 && screen.JankRatePct >= 10 {
			return "medium", "плохо, но мало данных", "Медленные кадры заметны, однако сценарий нужно повторить"
		}
		return "low", "мало данных", "Сигнал есть, но кадров недостаточно для устойчивой оценки"
	}
	if screen.JankyFrames == 0 && screen.AvgFPS > 0 && screen.AvgFPS < 40 {
		return "medium", "данные расходятся", "FPS низкий, но медленные кадры не зафиксированы"
	}
	if screen.JankRatePct >= 20 {
		return "critical", "критично", "Экран заметно зависает и требует разбора в первую очередь"
	}
	if screen.JankRatePct >= 10 {
		return "high", "плохо", "Подтормаживания, скорее всего, заметны пользователю"
	}
	if screen.JankRatePct >= 5 {
		return "medium", "нужно проверить", "Плавность ниже желаемой"
	}
	if uiHasSlowFrameTail(screen) {
		return "medium", "длинные кадры", "Часть кадров заметно выходит за целевое время"
	}
	if screen.JankyFrames > 0 {
		return "low", "редкие сбои", "Есть отдельные медленные кадры"
	}
	return "ok", "норма", "Зафиксированных подтормаживаний нет"
}

func uiCauseInsights(summary analyze.Summary, screen analyze.ScreenStats) []uiCauseInsight {
	if !uiHasMeasuredSlowFrames(screen) {
		return nil
	}
	deadlineUS := screen.FrameDeadlineUS
	if deadlineUS == 0 {
		deadlineUS = defaultFrameDeadlineUS
	}
	deadlineMS := max(uint64(1), deadlineUS/1_000)
	candidates := make([]uiCauseInsight, 0, 12)
	candidates = append(candidates, semanticUICauses(summary, screen.Screen, deadlineMS)...)
	candidates = append(candidates, mainThreadIOCauses(summary, screen.Screen, deadlineUS)...)
	if cause, ok := mainThreadStallCause(summary, screen.Screen); ok {
		candidates = append(candidates, cause)
	}
	candidates = append(candidates, longTaskCauses(summary, screen.Screen)...)
	candidates = append(candidates, runtimeCallCauses(summary, screen.Screen, deadlineMS)...)
	candidates = append(candidates, networkCauses(summary, screen.Screen)...)
	candidates = append(candidates, logSpamCauses(summary, screen.Screen)...)
	if cause, ok := memoryPressureCause(summary, screen.Screen); ok {
		candidates = append(candidates, cause)
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].rank != candidates[j].rank {
			return candidates[i].rank > candidates[j].rank
		}
		if candidates[i].magnitude != candidates[j].magnitude {
			return candidates[i].magnitude > candidates[j].magnitude
		}
		return candidates[i].stableKey < candidates[j].stableKey
	})
	if len(candidates) > maxUICauseInsights {
		candidates = candidates[:maxUICauseInsights]
	}
	return candidates
}

func semanticUICauses(summary analyze.Summary, screenName string, deadlineMS uint64) []uiCauseInsight {
	causes := make([]uiCauseInsight, 0, 3)
	for _, item := range analyze.ActionableSemanticWork(summary) {
		if !item.MainThread || !sameKnownReportValue(item.Screen, screenName) || item.MaxMS < deadlineMS {
			continue
		}
		switch item.Domain {
		case analyze.SemanticDomainCompose:
			phase, title, explanation, action := composeUICauseText(item.Operation)
			causes = append(causes, uiCauseInsight{
				Relation:      "главный поток измерен",
				RelationClass: "strong",
				Title:         title + ": " + reportValue(item.Owner, "Compose-функция"),
				Evidence: fmt.Sprintf(
					"%s выполнялась %s; максимум — %d мс при бюджете кадра %d мс.",
					phase,
					russianCount(item.Count, "раз", "раза", "раз"),
					item.MaxMS,
					deadlineMS,
				),
				Explanation: explanation + " Работа и медленные кадры относятся к одному экрану, но журнал пока не хранит идентификатор конкретного кадра, поэтому это сильный кандидат, а не доказанная первопричина.",
				Where:       uiLocation(item.Flow, item.Step, item.Owner, ""),
				Action:      action,
				rank:        720,
				magnitude:   item.MaxMS * 1_000,
				stableKey:   "compose\x00" + item.Operation + "\x00" + item.Owner,
			})
		case analyze.SemanticDomainRoom:
			causes = append(causes, uiCauseInsight{
				Relation:      "главный поток измерен",
				RelationClass: "strong",
				Title:         "Room DAO выполнялся на главном потоке: " + reportValue(item.Owner, "метод не определён"),
				Evidence: fmt.Sprintf(
					"%s; максимум — %d мс при бюджете кадра %d мс.",
					russianCount(item.Count, "вызов", "вызова", "вызовов"),
					item.MaxMS,
					deadlineMS,
				),
				Explanation: "SDK напрямую измерил границу DAO на главном потоке. Такой вызов способен заблокировать построение кадра; совпадение с конкретным медленным кадром следует подтвердить трассой.",
				Where:       uiLocation(item.Flow, item.Step, item.Owner, ""),
				Action:      "Перенесите запрос в асинхронное или фоновое выполнение, затем повторите экран и сравните максимальную длительность DAO и медленные кадры.",
				rank:        710,
				magnitude:   item.MaxMS * 1_000,
				stableKey:   "room\x00" + item.Owner,
			})
		}
	}
	return causes
}

func composeUICauseText(operation string) (phase, title, explanation, action string) {
	switch operation {
	case "measure":
		return "Измерение интерфейса Compose", "Измерение Compose превысило бюджет кадра", "Долгое измерение бывает связано с повторными проходами, внутренними размерами или сложным пользовательским Layout.", "Проверьте число проходов измерения, внутренние размеры и сложность пользовательского Layout; уменьшите повторные измерения."
	case "layout":
		return "Размещение интерфейса Compose", "Размещение Compose превысило бюджет кадра", "Долгое размещение указывает на тяжёлую работу во время размещения или сложное дерево.", "Упростите дерево и уберите вычисления из фазы размещения, затем сравните длительность и медленные кадры."
	case "draw":
		return "Отрисовка Compose", "Отрисовка Compose превысила бюджет кадра", "Долгая отрисовка бывает связана со сложной геометрией, эффектами, выделением памяти или частыми запросами перерисовки.", "Проверьте Canvas/Path/Shader, создание объектов и причины повторной отрисовки; кэшируйте неизменяемые данные."
	default:
		return "Композиция", "Функция с @Composable превысила бюджет кадра", "Долгое выполнение композиции бывает связано с тяжёлыми вычислениями, нестабильными параметрами или слишком широкой областью чтения состояния.", "Вынесите вычисления из композиции, проверьте стабильность параметров и области чтения состояния в Compose Layout Inspector."
	}
}

func mainThreadIOCauses(summary analyze.Summary, screenName string, deadlineUS uint64) []uiCauseInsight {
	causes := make([]uiCauseInsight, 0, 2)
	for _, operation := range summary.IOOperations {
		if !operation.MainThread || !sameKnownReportValue(operation.Screen, screenName) {
			continue
		}
		if operation.MaxDurationUS < deadlineUS && operation.TotalDurationUS < deadlineUS*2 {
			continue
		}
		label := reportIOOperationLabel(operation.Operation)
		where := uiLocation(operation.Flow, operation.Step, operation.Owner, "")
		causes = append(causes, uiCauseInsight{
			Relation:      "главный поток подтверждён",
			RelationClass: "strong",
			Title:         label + " выполнялось на главном потоке",
			Evidence: fmt.Sprintf(
				"%s; максимум — %s, суммарно — %s.",
				russianCount(operation.Count, "операция", "операции", "операций"),
				humanMicroseconds(operation.MaxDurationUS),
				humanMicroseconds(operation.TotalDurationUS),
			),
			Explanation: "Журнал прямо пометил эту I/O-работу как выполненную на главном потоке. Она способна занять бюджет кадра; точное пересечение с конкретным медленным кадром ещё нужно подтвердить трассой.",
			Where:       where,
			Action:      "Перенесите операцию с главного потока или подготовьте данные заранее, затем повторите тот же экран и сравните медленные кадры.",
			rank:        700,
			magnitude:   operation.MaxDurationUS,
			stableKey:   "io\x00" + operation.Operation + "\x00" + operation.Owner,
		})
	}
	return causes
}

func mainThreadStallCause(summary analyze.Summary, screenName string) (uiCauseInsight, bool) {
	var count int
	var maxMS uint64
	var owner, flow, step string
	for _, item := range summary.Flows {
		if !sameKnownReportValue(item.Screen, screenName) || item.StallCount == 0 {
			continue
		}
		count += item.StallCount
		if item.StallMaxMS >= maxMS {
			maxMS = item.StallMaxMS
			owner, flow, step = item.Owner, item.Flow, item.Step
		}
	}
	if count == 0 {
		return uiCauseInsight{}, false
	}
	stack := bestStallStack(summary.Owners, owner)
	where := uiLocation(flow, step, owner, stack)
	evidence := fmt.Sprintf("Главный поток останавливался %s; самая длинная пауза — %d мс.", russianCount(count, "раз", "раза", "раз"), maxMS)
	if stack != "" {
		evidence += " Во время паузы стек указывал на " + stack + "."
	}
	title, explanation, action := mainThreadStallNarrative(owner, stack)
	return uiCauseInsight{
		Relation:      "пауза подтверждена",
		RelationClass: "direct",
		Title:         title,
		Evidence:      evidence,
		Explanation:   explanation,
		Where:         where,
		Action:        action,
		rank:          650,
		magnitude:     maxMS * 1_000,
		stableKey:     "stall\x00" + owner + "\x00" + stack,
	}, true
}

func mainThreadStallNarrative(owner, stack string) (title, explanation, action string) {
	location := strings.ToLower(owner + " " + stack)
	switch {
	case strings.Contains(location, ".ondraw"), strings.Contains(location, ".dispatchdraw"):
		return "Пользовательский View блокировал отрисовку в onDraw",
			"Пауза главного потока и участок onDraw/dispatchDraw связаны напрямую. Тяжёлые вычисления, создание объектов или сложная геометрия в этом методе задерживают построение кадра.",
			"Откройте указанный onDraw/dispatchDraw: вынесите вычисления, не создавайте объекты на каждом кадре, кэшируйте Path/Bitmap/Shader и проверьте частоту invalidate."
	case strings.Contains(location, ".onmeasure"), strings.Contains(location, ".onlayout"):
		return "Измерение или размещение View блокировало главный поток",
			"Пауза главного потока напрямую связана с onMeasure/onLayout. Обычно это означает повторные requestLayout, тяжёлое измерение или слишком сложную иерархию View.",
			"Проверьте число onMeasure/onLayout за кадр, сократите вложенность и не вызывайте requestLayout без изменения размеров."
	default:
		return "Главный поток не обрабатывал кадры",
			"Это прямое подтверждение подвисания главного потока. Строка стека показывает место, где поток находился при снимке, но не заменяет полную трассу вызовов.",
			"Откройте указанный метод и его вызывающую цепочку: ищите синхронный I/O, ожидание блокировки, тяжёлое вычисление или большую работу измерения, размещения и отрисовки."
	}
}

func longTaskCauses(summary analyze.Summary, screenName string) []uiCauseInsight {
	causes := make([]uiCauseInsight, 0, 3)
	for _, window := range summary.ProblemWindows {
		if !sameKnownReportValue(window.Screen, screenName) || window.MaxMS == 0 {
			continue
		}
		title, relation, explanation, action, rank := uiProblemWindowDescription(window.Kind)
		if title == "" {
			continue
		}
		causes = append(causes, uiCauseInsight{
			Relation:      relation,
			RelationClass: "strong",
			Title:         title,
			Evidence:      fmt.Sprintf("Зафиксировано %s; максимум — %d мс.", russianCount(window.Count, "срабатывание", "срабатывания", "срабатываний"), window.MaxMS),
			Explanation:   explanation,
			Where:         uiLocation(window.Flow, window.Step, window.Owner, ""),
			Action:        action,
			rank:          rank,
			magnitude:     window.MaxMS * 1_000,
			stableKey:     "task\x00" + window.Kind + "\x00" + window.Owner,
		})
	}
	return causes
}

func runtimeCallCauses(summary analyze.Summary, screenName string, deadlineMS uint64) []uiCauseInsight {
	groups := map[runtimeCallSignature][]analyze.RuntimeCallStats{}
	for _, call := range summary.RuntimeCalls {
		if analyze.IsSemanticRuntimeCall(call.Caller) || !sameKnownReportValue(call.Screen, screenName) || call.MaxMS < deadlineMS || isUnknownReportValue(call.Callee) {
			continue
		}
		signature := runtimeCallSignature{
			Flow: call.Flow, Step: call.Step, Count: call.Count, TotalMS: call.TotalMS, MaxMS: call.MaxMS,
		}
		groups[signature] = append(groups[signature], call)
	}

	causes := make([]uiCauseInsight, 0, len(groups))
	for signature, calls := range groups {
		for _, component := range connectedRuntimeCallComponents(calls) {
			causes = append(causes, runtimeCallCause(signature, component))
		}
	}
	sort.SliceStable(causes, func(i, j int) bool {
		if causes[i].magnitude != causes[j].magnitude {
			return causes[i].magnitude > causes[j].magnitude
		}
		return causes[i].stableKey < causes[j].stableKey
	})
	if len(causes) > 2 {
		causes = causes[:2]
	}
	return causes
}

type runtimeCallSignature struct {
	Flow    string
	Step    string
	Count   uint64
	TotalMS uint64
	MaxMS   uint64
}

func connectedRuntimeCallComponents(calls []analyze.RuntimeCallStats) [][]analyze.RuntimeCallStats {
	byNode := map[string][]int{}
	for index, call := range calls {
		byNode[call.Caller] = append(byNode[call.Caller], index)
		byNode[call.Callee] = append(byNode[call.Callee], index)
	}
	seen := make([]bool, len(calls))
	components := make([][]analyze.RuntimeCallStats, 0, len(calls))
	for start := range calls {
		if seen[start] {
			continue
		}
		seen[start] = true
		queue := []int{start}
		component := make([]analyze.RuntimeCallStats, 0, 2)
		for len(queue) > 0 {
			index := queue[0]
			queue = queue[1:]
			call := calls[index]
			component = append(component, call)
			for _, node := range []string{call.Caller, call.Callee} {
				for _, neighbour := range byNode[node] {
					if !seen[neighbour] {
						seen[neighbour] = true
						queue = append(queue, neighbour)
					}
				}
			}
		}
		sort.SliceStable(component, func(i, j int) bool {
			if component[i].Caller != component[j].Caller {
				return component[i].Caller < component[j].Caller
			}
			return component[i].Callee < component[j].Callee
		})
		components = append(components, component)
	}
	return components
}

func runtimeCallCause(signature runtimeCallSignature, calls []analyze.RuntimeCallStats) uiCauseInsight {
	path, extraEdges := runtimeCallPath(calls)
	title, explanation, action := runtimeCallNarrative(path)
	evidence := fmt.Sprintf(
		"%s; максимум — %d мс, суммарно — %d мс.",
		russianCount(signature.Count, "вызов", "вызова", "вызовов"),
		signature.MaxMS,
		signature.TotalMS,
	)
	if len(calls) > 1 {
		evidence = fmt.Sprintf(
			"Связанная цепочка из %s; %s, максимум — %d мс.",
			russianCount(len(calls), "перехода", "переходов", "переходов"),
			russianCount(signature.Count, "наблюдение", "наблюдения", "наблюдений"),
			signature.MaxMS,
		)
	}
	location := strings.Join(path, " → ")
	if extraEdges > 0 {
		location += fmt.Sprintf(" · ещё %s в этой связанной цепочке", russianCount(extraEdges, "переход", "перехода", "переходов"))
	}
	return uiCauseInsight{
		Relation:      "кандидат из кода",
		RelationClass: "context",
		Title:         title,
		Evidence:      evidence,
		Explanation: explanation + " Граф вызовов не хранит признак главного потока и точное пересечение с кадром, " +
			"поэтому это место для проверки, а не доказанная причина.",
		Where:     "Цепочка кода: " + location + ".",
		Action:    action,
		rank:      450,
		magnitude: signature.MaxMS * 1_000,
		stableKey: "runtime\x00" + strings.Join(path, "\x00"),
	}
}

func runtimeCallPath(calls []analyze.RuntimeCallStats) ([]string, int) {
	outgoing := map[string][]string{}
	incoming := map[string]bool{}
	nodes := map[string]bool{}
	for _, call := range calls {
		outgoing[call.Caller] = append(outgoing[call.Caller], call.Callee)
		incoming[call.Callee] = true
		nodes[call.Caller] = true
		nodes[call.Callee] = true
	}
	for caller := range outgoing {
		sort.Strings(outgoing[caller])
	}
	roots := make([]string, 0, len(nodes))
	for node := range nodes {
		if !incoming[node] {
			roots = append(roots, node)
		}
	}
	sort.Strings(roots)
	start := calls[0].Caller
	if len(roots) > 0 {
		start = roots[0]
	}
	path := []string{start}
	visitedNodes := map[string]bool{start: true}
	usedEdges := 0
	for {
		var next string
		for _, candidate := range outgoing[path[len(path)-1]] {
			if !visitedNodes[candidate] {
				next = candidate
				break
			}
		}
		if next == "" {
			break
		}
		path = append(path, next)
		visitedNodes[next] = true
		usedEdges++
	}
	return path, max(0, len(calls)-usedEdges)
}

func runtimeCallNarrative(path []string) (title, explanation, action string) {
	joined := strings.ToLower(strings.Join(path, " "))
	switch {
	case strings.Contains(joined, ".ondraw"), strings.Contains(joined, ".dispatchdraw"):
		return "Отрисовка пользовательского View выполнялась дольше бюджета кадра",
			"Цепочка содержит onDraw/dispatchDraw — это участок построения кадра. Частые аллокации, сложная геометрия, Bitmap/Path и повторные вычисления здесь особенно подозрительны.",
			"Профилируйте onDraw/dispatchDraw: уберите создание объектов и тяжёлые вычисления, кэшируйте неизменяемые данные и проверьте частоту invalidate."
	case strings.Contains(joined, ".onmeasure"), strings.Contains(joined, ".onlayout"):
		return "Измерение или компоновка View выполнялись дольше бюджета кадра",
			"Цепочка содержит onMeasure/onLayout — это признак дорогой компоновки, повторных requestLayout или слишком сложной иерархии.",
			"Проверьте число повторных измерений и размещений за кадр, уменьшите вложенность и не запускайте requestLayout без изменения размеров."
	case strings.Contains(joined, ".oncreate"), strings.Contains(joined, ".oncustomcreate"):
		return "Цепочка создания экрана выполнялась дольше бюджета кадра",
			"Несколько связанных методов создания экрана имеют одно и то же измерение и объединены в один путь, а не показаны как независимые проблемы.",
			"Разделите инициализацию экрана: отложите некритичные части, не выполняйте синхронный I/O и создавайте только UI, необходимый для первого кадра."
	default:
		endpoint := "Вызов метода"
		if len(path) > 0 && !isUnknownReportValue(path[len(path)-1]) {
			endpoint = path[len(path)-1]
		}
		return endpoint + " выполнялся дольше бюджета кадра",
			"Граф вызовов измерил долгий вызов в том же экранном контексте.",
			"Профилируйте эту цепочку во время воспроизведения и разделите вычисления, создание объектов и обновление UI, которые не помещаются в один кадр."
	}
}

func networkCauses(summary analyze.Summary, screenName string) []uiCauseInsight {
	causes := make([]uiCauseInsight, 0, 2)
	for _, flow := range summary.Flows {
		if !sameKnownReportValue(flow.Screen, screenName) || (flow.HTTPFailed == 0 && flow.HTTPP95MS < 700) {
			continue
		}
		route := reportValue(flow.RouteSample, "сетевой маршрут")
		title := route + " отвечал медленно"
		if flow.HTTPFailed > 0 {
			title = route + " завершался с ошибкой или медленно"
		}
		causes = append(causes, uiCauseInsight{
			Relation:      "совпало в сценарии",
			RelationClass: "context",
			Title:         title,
			Evidence:      scenarioHTTPText(flow),
			Explanation:   "Сетевой вызов записан в том же экранном и сценарном контексте. Журнал не показывает, ожидал ли его главный поток и пересёкся ли он с медленным кадром, поэтому сам по себе запрос не доказывает причину подтормаживаний.",
			Where:         uiLocation(flow.Flow, flow.Step, flow.Owner, flow.RouteSample),
			Action:        "Проверьте, что запрос выполняется асинхронно, главный поток не ждёт результат, а разбор ответа и обновление UI не создают большую работу одним блоком.",
			rank:          350,
			magnitude:     flow.HTTPP95MS * 1_000,
			stableKey:     "network\x00" + flow.RouteSample + "\x00" + flow.Owner,
		})
	}
	return causes
}

func logSpamCauses(summary analyze.Summary, screenName string) []uiCauseInsight {
	causes := make([]uiCauseInsight, 0, 2)
	for _, item := range summary.LogSpam {
		if !sameKnownReportValue(item.Screen, screenName) || item.Count < 50 {
			continue
		}
		causes = append(causes, uiCauseInsight{
			Relation:      "может усиливать",
			RelationClass: "factor",
			Title:         "Частое логирование добавляло работу в этом экране",
			Evidence:      russianCount(item.Count, "запись", "записи", "записей") + " из " + reportValue(item.Source, "источника без названия") + ".",
			Explanation:   "Частое форматирование и вывод логов расходуют CPU и могут добавлять I/O. Совпадение экрана не доказывает, что именно логирование сорвало кадр.",
			Where:         uiLocation(item.Flow, item.Step, item.Owner, item.Source),
			Action:        "Уберите повторяющиеся записи из горячего пути, затем повторите сценарий и сравните долю медленных кадров.",
			rank:          250,
			magnitude:     item.Count,
			stableKey:     "log\x00" + item.Owner + "\x00" + item.Source,
		})
	}
	return causes
}

func memoryPressureCause(summary analyze.Summary, screenName string) (uiCauseInsight, bool) {
	var maxKB uint64
	var owner, flow, step string
	for _, item := range summary.Flows {
		if !sameKnownReportValue(item.Screen, screenName) || item.MemoryMaxKB < 256*1024 {
			continue
		}
		if item.MemoryMaxKB >= maxKB {
			maxKB = item.MemoryMaxKB
			owner, flow, step = item.Owner, item.Flow, item.Step
		}
	}
	if maxKB == 0 {
		return uiCauseInsight{}, false
	}
	return uiCauseInsight{
		Relation:      "может усиливать",
		RelationClass: "factor",
		Title:         "Высокое потребление памяти могло усилить паузы GC",
		Evidence:      "PSS в этом контексте доходил до " + humanDataSizeKB(maxKB) + ".",
		Explanation:   "Большой объём памяти повышает риск частых или долгих сборок мусора, но один максимум PSS не доказывает GC-паузу внутри медленного кадра.",
		Where:         uiLocation(flow, step, owner, ""),
		Action:        "Сопоставьте временную шкалу GC и выделений с медленными кадрами; ищите массовое создание объектов при построении или обновлении UI.",
		rank:          150,
		magnitude:     maxKB,
		stableKey:     "memory\x00" + owner,
	}, true
}

func uiProblemWindowDescription(kind string) (title, relation, explanation, action string, rank int) {
	switch kind {
	case "main_thread_dispatch":
		return "Обработка сообщения главного потока заняла слишком долго", "главный поток подтверждён", "Длительный dispatch измерен непосредственно на главном потоке в этом контексте.", "Разбейте обработчик сообщения на короткие части и вынесите вычисления или I/O из главного потока.", 620
	case "wrapped_click":
		return "Обработчик нажатия выполнялся слишком долго", "долгая работа подтверждена", "Длительность обработчика пользовательского нажатия измерена напрямую; такой обработчик выполняется в UI-пути.", "Откройте обработчик нажатия и оставьте в нём только быстрое изменение состояния; I/O и тяжёлые вычисления перенесите из UI-пути.", 600
	case "main_thread_io", "main_thread_disk_io", "disk_io_main_thread":
		return "I/O блокировал главный поток", "главный поток подтверждён", "Тип проблемного окна прямо указывает на I/O в главном потоке.", "Перенесите I/O с главного потока и повторите сценарий с теми же входными данными.", 690
	case "wrapped_runnable", "wrapped_callable", "wrapped_coroutine", "wrapped_executor":
		return "Долгая задача совпала с проблемным экраном", "совпало в сценарии", "Обёртка измерила длительную задачу в том же контексте, но тип события не доказывает её выполнение внутри конкретного UI-кадра.", "Проверьте поток выполнения задачи и отделите подготовку данных от короткого применения результата в UI.", 420
	default:
		return "", "", "", "", 0
	}
}

func uiDiagnosis(screen analyze.ScreenStats, causes []uiCauseInsight) string {
	if !uiHasMeasuredSlowFrames(screen) {
		return "Причину подвисания искать рано: медленные кадры на этом экране не зафиксированы. Сначала подтвердите симптом повторным прогоном."
	}
	if len(causes) == 0 {
		return "Подтормаживание подтверждено, но доступные события не локализуют источник. Нужен повтор с размеченным сценарием и трассой главного потока во время самого длинного кадра."
	}
	return "Главный маршрут расследования: " + causes[0].Title + ". Ниже причины отделены от простых совпадений и отсортированы по силе доступных данных."
}

func uiHasMeasuredSlowFrames(screen analyze.ScreenStats) bool {
	return screen.JankyFrames > 0 || uiHasSlowFrameTail(screen)
}

func uiHasSlowFrameTail(screen analyze.ScreenStats) bool {
	tailThresholdMS := defaultFrameTailThresholdMS
	if screen.FrameDeadlineStatus == "consistent" && screen.FrameDeadlineUS > 0 {
		tailThresholdMS = max(uint64(1), (screen.FrameDeadlineUS*2+999)/1_000)
	}
	return screen.FrameP95MS >= tailThresholdMS || screen.FrameP99MS >= tailThresholdMS*2
}

func uiFrameDeadlineMS(screen analyze.ScreenStats) uint64 {
	deadlineUS := screen.FrameDeadlineUS
	if deadlineUS == 0 {
		deadlineUS = defaultFrameDeadlineUS
	}
	return max(uint64(1), (deadlineUS+999)/1_000)
}

func uiRelatedSignals(summary analyze.Summary, screenName string) (string, string, string) {
	var httpCount, httpFailed, stalls int
	var maxHTTP, maxStall, logSpam, problems uint64
	var detailedLogSpam, problemWindowSignals uint64
	contexts := make([]string, 0, 3)
	seenContexts := map[string]struct{}{}
	for _, flow := range summary.Flows {
		if !sameKnownReportValue(flow.Screen, screenName) {
			continue
		}
		httpCount += flow.HTTPCount
		httpFailed += flow.HTTPFailed
		stalls += flow.StallCount
		maxHTTP = max(maxHTTP, flow.HTTPP95MS)
		maxStall = max(maxStall, flow.StallMaxMS)
		logSpam = saturatingAddUint64(logSpam, flow.LogSpam)
		problems = saturatingAddUint64(problems, flow.ProblemCount)
		context := labelledFlowContext(flow.Flow, flow.Step, flow.Owner, flow.RouteSample)
		if context != "" {
			if _, exists := seenContexts[context]; !exists && len(contexts) < 3 {
				seenContexts[context] = struct{}{}
				contexts = append(contexts, context)
			}
		}
	}
	for _, item := range summary.ProblemWindows {
		if sameKnownReportValue(item.Screen, screenName) {
			problemWindowSignals = saturatingAddUint64(problemWindowSignals, item.Count)
		}
	}
	for _, item := range summary.LogSpam {
		if sameKnownReportValue(item.Screen, screenName) {
			detailedLogSpam = saturatingAddUint64(detailedLogSpam, item.Count)
		}
	}
	problems = max(problems, problemWindowSignals)
	logSpam = max(logSpam, detailedLogSpam)

	signals := make([]string, 0, 4)
	if stalls > 0 {
		signals = append(signals, fmt.Sprintf("%s главного потока, самая длинная — %d мс", russianCount(stalls, "пауза", "паузы", "пауз"), maxStall))
	}
	if httpCount > 0 {
		network := fmt.Sprintf("%s, верхняя задержка — %d мс", russianCount(httpCount, "сетевой вызов", "сетевых вызова", "сетевых вызовов"), maxHTTP)
		if httpFailed > 0 {
			network += fmt.Sprintf(", с ошибкой — %d", httpFailed)
		}
		signals = append(signals, network)
	}
	if logSpam > 0 {
		signals = append(signals, russianCount(logSpam, "лишняя запись в лог", "лишние записи в лог", "лишних записей в лог"))
	}
	if problems > 0 {
		signals = append(signals, russianCount(problems, "проблемный сигнал", "проблемных сигнала", "проблемных сигналов"))
	}

	nearby := "В том же экранном контексте дополнительные проблемные сигналы не записаны."
	if len(signals) > 0 {
		nearby = "В том же экранном контексте записаны: " + strings.Join(signals, "; ") + "."
	}
	where := "Точный сценарий или источник работ для этого экрана не записан."
	if len(contexts) > 0 {
		where = "Начните проверку здесь: " + strings.Join(contexts, "; ") + "."
	}
	action := "Повторите экран с размеченным сценарием и профилированием главного потока, затем найдите самый длинный кадр."
	switch {
	case stalls > 0:
		action = "Откройте трассу главного потока в указанном сценарии и уберите длинную синхронную работу из кадра."
	case httpCount > 0:
		action = "Проверьте, не ждёт ли экран сеть на главном потоке и нет ли повторных запросов при перерисовке."
	case logSpam > 0:
		action = "Сократите частое логирование в указанном источнике и повторно измерьте плавность экрана."
	}
	return nearby, where, action
}

func bestStallStack(owners []analyze.OwnerStats, owner string) string {
	var stack string
	var maxMS uint64
	for _, item := range owners {
		if item.Kind != "main_thread_stall" || !sameKnownReportValue(item.Owner, owner) || item.StackHint == "" {
			continue
		}
		if item.MaxMS >= maxMS {
			maxMS = item.MaxMS
			stack = item.StackHint
		}
	}
	return stack
}

func uiLocation(flow, step, owner, detail string) string {
	context := labelledFlowContext(flow, step, owner, detail)
	if context == "" {
		return "Точное место не размечено; повторите сценарий с owner/flow/step."
	}
	return "Проверьте: " + context + "."
}

func reportIOOperationLabel(operation string) string {
	labels := map[string]string{
		"file_read":      "Чтение файла",
		"file_write":     "Запись файла",
		"file_sync":      "Синхронизация файла",
		"database_read":  "Чтение базы данных",
		"database_write": "Запись в базу данных",
		"content_read":   "Чтение ContentProvider",
		"content_write":  "Запись в ContentProvider",
	}
	if label := labels[operation]; label != "" {
		return label
	}
	return reportValue(strings.ReplaceAll(operation, "_", " "), "I/O-операция")
}

func sameKnownReportValue(left, right string) bool {
	return !isUnknownReportValue(left) && !isUnknownReportValue(right) && sameReportValue(left, right)
}

func sameReportValue(left, right string) bool {
	return strings.EqualFold(strings.TrimSpace(left), strings.TrimSpace(right))
}

func labelledFlowContext(flow, step, owner, detail string) string {
	parts := make([]string, 0, 4)
	if !isUnknownReportValue(flow) {
		parts = append(parts, "сценарий "+flow)
	}
	if !isUnknownReportValue(step) {
		parts = append(parts, "шаг "+step)
	}
	if !isUnknownReportValue(owner) {
		parts = append(parts, "источник "+owner)
	}
	if !isUnknownReportValue(detail) {
		parts = append(parts, detail)
	}
	return strings.Join(parts, " · ")
}
