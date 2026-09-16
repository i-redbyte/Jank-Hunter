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
	uiScreenCauseLimit          = 12
)

type uiScreenInsight struct {
	Screen        string
	Severity      string
	Status        string
	Headline      string
	Observation   string
	Diagnosis     string
	Nearby        string
	Where         string
	Action        string
	Causes        []uiCauseInsight
	CauseTotal    int
	OmittedCauses int
	Tooltip       string
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

// boundedCandidates keeps the cardinality of expensive presentation work independent from the
// cardinality of input telemetry while preserving the exact number of eligible source records.
type boundedCandidates[T any] struct {
	values []T
	total  int
	limit  int
}

func newBoundedCandidates[T any](limit int) boundedCandidates[T] {
	return boundedCandidates[T]{values: make([]T, 0, limit), limit: limit}
}

func (selection *boundedCandidates[T]) retain(candidate T, better func(T, T) bool) {
	selection.total++
	if selection.limit == 0 {
		return
	}
	if len(selection.values) < selection.limit {
		selection.values = append(selection.values, candidate)
		return
	}
	worst := 0
	for index := 1; index < len(selection.values); index++ {
		if better(selection.values[worst], selection.values[index]) {
			worst = index
		}
	}
	if better(candidate, selection.values[worst]) {
		selection.values[worst] = candidate
	}
}

func uiScreenInsights(summary analyze.Summary) []uiScreenInsight {
	insights := make([]uiScreenInsight, 0, len(summary.Screens))
	semanticWork := analyze.ActionableSemanticWork(summary)
	for _, screen := range summary.Screens {
		severity, status, headline := uiScreenVerdict(screen)
		observation := uiScreenObservation(screen)
		causes, causeTotal := uiCauseInsightsWithTotal(summary, semanticWork, screen)
		nearby, where, action := uiRelatedSignals(summary, screen.Screen)
		if len(causes) > 0 {
			where = causes[0].Where
			action = causes[0].Action
		}
		insights = append(insights, uiScreenInsight{
			Screen:        reportValue(screen.Screen, "экран не указан"),
			Severity:      severity,
			Status:        status,
			Headline:      headline,
			Observation:   observation,
			Diagnosis:     uiDiagnosis(screen, causes),
			Nearby:        nearby,
			Where:         where,
			Action:        action,
			Causes:        causes,
			CauseTotal:    causeTotal,
			OmittedCauses: causeTotal - len(causes),
			Tooltip:       "Карточка объединяет плавность экрана, работу главного потока, файловые операции, вызовы кода, сеть, память и журналирование. Уровень связи показывает, что зафиксировано напрямую, а что ещё нужно проверить.",
		})
	}
	sort.SliceStable(insights, func(i, j int) bool {
		left, right := severityRank(insights[i].Severity), severityRank(insights[j].Severity)
		if left != right {
			return left > right
		}
		return insights[i].Screen < insights[j].Screen
	})
	return insights
}

func uiProblemCount(insights []uiScreenInsight) int {
	count := 0
	for _, insight := range insights {
		if severityRank(insight.Severity) >= severityRank("medium") {
			count++
		}
	}
	return count
}

func uiScreenObservation(screen analyze.ScreenStats) string {
	if screen.JankyFrames == 0 && uiHasSlowFrameTail(screen) {
		return fmt.Sprintf(
			"Системный признак подтормаживания не сработал ни для одного из %d кадров, но верхние 5%% кадров занимали до %d мс, а отдельные худшие - до %d мс при целевом времени %d мс. Это подтверждает длинные кадры: значение 0%% здесь не означает норму.",
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
		observation += fmt.Sprintf(" Средняя скорость - %.1f FPS", screen.AvgFPS)
		if screen.MinFPS > 0 {
			observation += fmt.Sprintf(", минимальная - %.1f FPS", screen.MinFPS)
		}
		observation += "."
	}
	if screen.Frames >= 30 && screen.JankyFrames == 0 && screen.AvgFPS > 0 && screen.AvgFPS < 40 {
		observation += " Низкий FPS расходится с отсутствием медленных кадров: это бывает при редкой отрисовке или слишком короткой выборке кадров."
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

func uiCauseInsights(
	summary analyze.Summary,
	semanticWork []analyze.SemanticWorkStats,
	screen analyze.ScreenStats,
) []uiCauseInsight {
	causes, _ := uiCauseInsightsWithTotal(summary, semanticWork, screen)
	return causes
}

func uiCauseInsightsWithTotal(
	summary analyze.Summary,
	semanticWork []analyze.SemanticWorkStats,
	screen analyze.ScreenStats,
) ([]uiCauseInsight, int) {
	if !uiHasMeasuredSlowFrames(screen) {
		return nil, 0
	}
	deadlineUS := screen.FrameDeadlineUS
	if deadlineUS == 0 {
		deadlineUS = defaultFrameDeadlineUS
	}
	deadlineMS := max(uint64(1), microsecondsToMillisecondsCeilReport(deadlineUS))
	candidates := make([]uiCauseInsight, 0, 12)
	total := 0
	databaseCauses, databaseTotal := databaseUICausesWithTotal(summary.DatabaseAnalysis, screen.Screen)
	total = saturatingAddNonNegativeInt(total, databaseTotal)
	candidates = append(candidates, databaseCauses...)
	semanticCauses, semanticTotal := semanticUICausesWithTotal(semanticWork, screen.Screen, deadlineMS)
	total = saturatingAddNonNegativeInt(total, semanticTotal)
	candidates = append(candidates, semanticCauses...)
	ioCauses, ioTotal := mainThreadIOCausesWithTotal(summary, screen.Screen, deadlineUS)
	total = saturatingAddNonNegativeInt(total, ioTotal)
	candidates = append(candidates, ioCauses...)
	stallCauses, stallTotal := mainThreadStallCausesWithTotal(summary, screen.Screen)
	total = saturatingAddNonNegativeInt(total, stallTotal)
	candidates = append(candidates, stallCauses...)
	longTasks, longTaskTotal := longTaskCauses(summary, screen.Screen)
	total = saturatingAddNonNegativeInt(total, longTaskTotal)
	candidates = append(candidates, longTasks...)
	runtimeCauses, runtimeTotal := runtimeCallCausesWithTotal(summary, screen.Screen, deadlineMS)
	total = saturatingAddNonNegativeInt(total, runtimeTotal)
	candidates = append(candidates, runtimeCauses...)
	networkCauseSet, networkTotal := networkCausesWithTotal(summary, screen.Screen)
	total = saturatingAddNonNegativeInt(total, networkTotal)
	candidates = append(candidates, networkCauseSet...)
	logCauseSet, logTotal := logSpamCausesWithTotal(summary, screen.Screen)
	total = saturatingAddNonNegativeInt(total, logTotal)
	candidates = append(candidates, logCauseSet...)
	if cause, ok := memoryPressureCause(summary, screen.Screen); ok {
		candidates = append(candidates, cause)
		total = saturatingAddNonNegativeInt(total, 1)
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		return uiCauseBetter(candidates[i], candidates[j])
	})
	if len(candidates) <= uiScreenCauseLimit {
		return candidates, total
	}
	visible := make([]uiCauseInsight, uiScreenCauseLimit)
	copy(visible, candidates[:uiScreenCauseLimit])
	return visible, total
}

const databaseUICauseLimit = 8

func databaseUICauses(
	analysis *analyze.DatabaseAnalysis,
	screenName string,
) []uiCauseInsight {
	causes, _ := databaseUICausesWithTotal(analysis, screenName)
	return causes
}

type databaseUICandidate struct {
	statement  *analyze.DatabaseStatementStats
	context    *analyze.DatabaseStatementContextStats
	owner      string
	durationUS uint64
	mainThread bool
}

func databaseUICausesWithTotal(
	analysis *analyze.DatabaseAnalysis,
	screenName string,
) ([]uiCauseInsight, int) {
	if analysis == nil {
		return nil, 0
	}
	selected := newBoundedCandidates[databaseUICandidate](databaseUICauseLimit)
	cfg := analyze.DefaultProblemDetectorConfig()
	for statementIndex := range analysis.Statements {
		statement := &analysis.Statements[statementIndex]
		for contextIndex := range statement.Contexts {
			context := &statement.Contexts[contextIndex]
			if !sameKnownReportValue(context.Screen, screenName) {
				continue
			}
			owner := firstNonEmpty(context.Source, context.ContextOwner)
			if isUnknownReportValue(owner) {
				owner = "место SQL-вызова не определено"
			}
			switch {
			case context.MainCorrelation.UIWindowOverlaps > 0 &&
				context.MainCorrelation.UIOverlapMaxDurationUS >= cfg.DatabaseMainThreadMS*1_000:
				selected.retain(databaseUICandidate{
					statement: statement, context: context, owner: owner,
					durationUS: context.MainCorrelation.UIOverlapMaxDurationUS, mainThread: true,
				}, databaseUICandidateBetter)
			case context.BackgroundCorrelation.UIWindowOverlaps > 0 &&
				context.BackgroundCorrelation.UIOverlapMaxDurationUS >= cfg.DatabaseBackgroundMS*1_000:
				selected.retain(databaseUICandidate{
					statement: statement, context: context, owner: owner,
					durationUS: context.BackgroundCorrelation.UIOverlapMaxDurationUS,
				}, databaseUICandidateBetter)
			}
		}
	}
	sort.SliceStable(selected.values, func(i, j int) bool {
		return databaseUICandidateBetter(selected.values[i], selected.values[j])
	})
	causes := make([]uiCauseInsight, 0, len(selected.values))
	for _, candidate := range selected.values {
		statement, context := candidate.statement, candidate.context
		owner := reportValue(candidate.owner, "место SQL-вызова не определено")
		if candidate.mainThread {
			causes = append(causes, uiCauseInsight{
				Relation:      "пересечение интервалов подтверждено",
				RelationClass: "strong",
				Title:         "SQL-вызов выполнялся на главном потоке: " + owner,
				Evidence: fmt.Sprintf(
					"%s; максимум - %d мс; %s с проблемными UI-окнами (%d медленных кадров из %d в пересечённых окнах).",
					russianCount(context.Main.Calls, "вызов", "вызова", "вызовов"),
					microsecondsToMillisecondsCeilReport(candidate.durationUS),
					russianCount(context.MainCorrelation.UIWindowOverlaps, "пересечение", "пересечения", "пересечений"),
					context.MainCorrelation.UIJankyFrames,
					context.MainCorrelation.UIFrames,
				),
				Explanation: "Jank Hunter восстановил интервал SQL-вызова и подтвердил его пересечение с проблемным UI-окном того же процесса, запуска, операции и экрана. Для главного потока это прямая связь с занятым временем интерфейса, хотя вклад конкретного SQL-вызова в длительность отдельного кадра всё ещё следует проверить трассой.",
				Where:       uiLocation(context.ContextOperation, owner, statement.Query),
				Action:      "Перенесите этот SQL с главного потока, затем повторите ту же операцию и сравните число пересечений, максимум SQL и медленные кадры.",
				rank:        780,
				magnitude:   candidate.durationUS,
				stableKey:   "database-main\x00" + statement.Query + "\x00" + owner + "\x00" + context.SessionID,
			})
			continue
		}
		causes = append(causes, uiCauseInsight{
			Relation:      "совпало по времени",
			RelationClass: "related",
			Title:         "Фоновый SQL совпал с проблемным UI-окном: " + owner,
			Evidence: fmt.Sprintf(
				"%s; максимум - %d мс; %s с проблемными UI-окнами.",
				russianCount(context.Background.Calls, "фоновый вызов", "фоновых вызова", "фоновых вызовов"),
				microsecondsToMillisecondsCeilReport(candidate.durationUS),
				russianCount(context.BackgroundCorrelation.UIWindowOverlaps, "пересечение", "пересечения", "пересечений"),
			),
			Explanation: "Интервалы относятся к одному процессу, запуску, операции и экрану, но SQL выполнялся в фоне. Совпадение может указывать на конкуренцию за БД, CPU или I/O, однако само по себе не доказывает причину подтормаживания.",
			Where:       uiLocation(context.ContextOperation, owner, statement.Query),
			Action:      "Проверьте блокировки, план запроса и конкуренцию ресурсов в этом интервале; подтвердите влияние сравнением того же сценария после адресной оптимизации.",
			rank:        330,
			magnitude:   candidate.durationUS,
			stableKey:   "database-background\x00" + statement.Query + "\x00" + owner + "\x00" + context.SessionID,
		})
	}
	return causes, selected.total
}

func databaseUICandidateBetter(left, right databaseUICandidate) bool {
	if left.mainThread != right.mainThread {
		return left.mainThread
	}
	if left.durationUS != right.durationUS {
		return left.durationUS > right.durationUS
	}
	if left.statement.Query != right.statement.Query {
		return left.statement.Query < right.statement.Query
	}
	if left.owner != right.owner {
		return left.owner < right.owner
	}
	return left.context.SessionID < right.context.SessionID
}

func uiCauseBetter(left, right uiCauseInsight) bool {
	if left.rank != right.rank {
		return left.rank > right.rank
	}
	if left.magnitude != right.magnitude {
		return left.magnitude > right.magnitude
	}
	return left.stableKey < right.stableKey
}

func microsecondsToMillisecondsCeilReport(value uint64) uint64 {
	result := value / 1_000
	if value%1_000 != 0 {
		result++
	}
	return result
}

func semanticUICauses(work []analyze.SemanticWorkStats, screenName string, deadlineMS uint64) []uiCauseInsight {
	causes, _ := semanticUICausesWithTotal(work, screenName, deadlineMS)
	return causes
}

func semanticUICausesWithTotal(
	work []analyze.SemanticWorkStats,
	screenName string,
	deadlineMS uint64,
) ([]uiCauseInsight, int) {
	selected := newBoundedCandidates[analyze.SemanticWorkStats](uiScreenCauseLimit)
	for _, item := range work {
		if !item.MainThread || !sameKnownReportValue(item.Screen, screenName) || item.MaxMS < deadlineMS {
			continue
		}
		switch item.Domain {
		case analyze.SemanticDomainCompose, analyze.SemanticDomainRoom:
			selected.retain(item, semanticUICauseBetter)
		}
	}
	sort.SliceStable(selected.values, func(i, j int) bool {
		return semanticUICauseBetter(selected.values[i], selected.values[j])
	})
	causes := make([]uiCauseInsight, 0, len(selected.values))
	for _, item := range selected.values {
		switch item.Domain {
		case analyze.SemanticDomainCompose:
			phase, title, explanation, action := composeUICauseText(item.Operation)
			causes = append(causes, uiCauseInsight{
				Relation:      "главный поток измерен",
				RelationClass: "strong",
				Title:         title + ": " + reportValue(item.Owner, "Compose-функция"),
				Evidence: fmt.Sprintf(
					"%s выполнялась %s; максимум - %d мс при бюджете кадра %d мс.",
					phase,
					russianCount(item.Count, "раз", "раза", "раз"),
					item.MaxMS,
					deadlineMS,
				),
				Explanation: explanation + " Работа и медленные кадры относятся к одному экрану. Без идентификатора конкретного кадра это возможная причина, а не доказанный факт.",
				Where:       uiLocation(item.ContextOperation, item.Owner, ""),
				Action:      action,
				rank:        720,
				magnitude:   saturatingMulUint64(item.MaxMS, 1_000),
				stableKey:   "compose\x00" + item.Operation + "\x00" + item.Owner,
			})
		case analyze.SemanticDomainRoom:
			causes = append(causes, uiCauseInsight{
				Relation:      "главный поток измерен",
				RelationClass: "strong",
				Title:         "Метод доступа к данным Room выполнялся на главном потоке: " + reportValue(item.Owner, "метод не определён"),
				Evidence: fmt.Sprintf(
					"%s; максимум - %d мс при бюджете кадра %d мс.",
					russianCount(item.Count, "вызов", "вызова", "вызовов"),
					item.MaxMS,
					deadlineMS,
				),
				Explanation: "Jank Hunter напрямую измерил границу метода доступа к данным на главном потоке. Такой вызов способен заблокировать построение кадра; совпадение с конкретным медленным кадром следует подтвердить трассой.",
				Where:       uiLocation(item.ContextOperation, item.Owner, ""),
				Action:      "Перенесите запрос в асинхронное или фоновое выполнение, затем повторите экран и сравните максимальную длительность метода и число медленных кадров.",
				rank:        710,
				magnitude:   saturatingMulUint64(item.MaxMS, 1_000),
				stableKey:   "room\x00" + item.Owner,
			})
		}
	}
	return causes, selected.total
}

func semanticUICauseBetter(left, right analyze.SemanticWorkStats) bool {
	leftRank := 710
	if left.Domain == analyze.SemanticDomainCompose {
		leftRank = 720
	}
	rightRank := 710
	if right.Domain == analyze.SemanticDomainCompose {
		rightRank = 720
	}
	if leftRank != rightRank {
		return leftRank > rightRank
	}
	if left.MaxMS != right.MaxMS {
		return left.MaxMS > right.MaxMS
	}
	if left.Operation != right.Operation {
		return left.Operation < right.Operation
	}
	return left.Owner < right.Owner
}

func composeUICauseText(operation string) (phase, title, explanation, action string) {
	switch operation {
	case "measure":
		return "Измерение интерфейса Compose", "Измерение Compose превысило бюджет кадра", "Долгое измерение бывает связано с повторными проходами, внутренними размерами или сложным пользовательским расположением элементов.", "Проверьте число проходов измерения, внутренние размеры и сложность расположения элементов; уменьшите повторные измерения."
	case "layout":
		return "Размещение интерфейса Compose", "Размещение Compose превысило бюджет кадра", "Долгое размещение указывает на тяжёлую работу во время размещения или сложное дерево.", "Упростите дерево и уберите вычисления из фазы размещения, затем сравните длительность и медленные кадры."
	case "draw":
		return "Отрисовка Compose", "Отрисовка Compose превысила бюджет кадра", "Долгая отрисовка бывает связана со сложной геометрией, эффектами, выделением памяти или частыми запросами перерисовки.", "Проверьте Canvas/Path/Shader, создание объектов и причины повторной отрисовки; кэшируйте неизменяемые данные."
	default:
		return "Построение интерфейса", "Функция с @Composable превысила бюджет кадра", "Долгое построение интерфейса бывает связано с тяжёлыми вычислениями, нестабильными параметрами или слишком широкой областью чтения состояния.", "Вынесите вычисления из построения интерфейса, проверьте стабильность параметров и области чтения состояния в инспекторе компоновки Compose."
	}
}

func mainThreadIOCauses(summary analyze.Summary, screenName string, deadlineUS uint64) []uiCauseInsight {
	causes, _ := mainThreadIOCausesWithTotal(summary, screenName, deadlineUS)
	return causes
}

func mainThreadIOCausesWithTotal(
	summary analyze.Summary,
	screenName string,
	deadlineUS uint64,
) ([]uiCauseInsight, int) {
	selected := newBoundedCandidates[analyze.IOStats](uiScreenCauseLimit)
	if summary.IOAnalysis != nil {
		for _, operation := range summary.IOAnalysis.Calls {
			if !operation.MainThread || !sameKnownReportValue(operation.Screen, screenName) {
				continue
			}
			if operation.MaxDurationUS < deadlineUS &&
				operation.TotalDurationUS < saturatingMulUint64(deadlineUS, 2) {
				continue
			}
			selected.retain(operation, mainThreadIOCauseBetter)
		}
	}
	sort.SliceStable(selected.values, func(i, j int) bool {
		return mainThreadIOCauseBetter(selected.values[i], selected.values[j])
	})
	causes := make([]uiCauseInsight, 0, len(selected.values))
	for _, operation := range selected.values {
		label := reportIOOperationLabel(operation.Operation)
		where := uiLocation(operation.ContextOperation, operation.Owner, "")
		causes = append(causes, uiCauseInsight{
			Relation:      "главный поток подтверждён",
			RelationClass: "strong",
			Title:         label + " выполнялось на главном потоке",
			Evidence: fmt.Sprintf(
				"%s; максимум - %s, суммарно - %s.",
				russianCount(operation.Count, "операция", "операции", "операций"),
				humanMicroseconds(operation.MaxDurationUS),
				humanMicroseconds(operation.TotalDurationUS),
			),
			Explanation: "Журнал прямо пометил эту файловую операцию как выполненную на главном потоке. Она способна занять бюджет кадра; точное пересечение с конкретным медленным кадром ещё нужно подтвердить трассой.",
			Where:       where,
			Action:      "Перенесите операцию с главного потока или подготовьте данные заранее, затем повторите тот же экран и сравните медленные кадры.",
			rank:        700,
			magnitude:   operation.MaxDurationUS,
			stableKey:   "io\x00" + operation.Operation + "\x00" + operation.Owner,
		})
	}
	return causes, selected.total
}

func mainThreadIOCauseBetter(left, right analyze.IOStats) bool {
	if left.MaxDurationUS != right.MaxDurationUS {
		return left.MaxDurationUS > right.MaxDurationUS
	}
	if left.Operation != right.Operation {
		return left.Operation < right.Operation
	}
	return left.Owner < right.Owner
}

const mainThreadStallCauseLimit = 4

func mainThreadStallCauses(summary analyze.Summary, screenName string) []uiCauseInsight {
	causes, _ := mainThreadStallCausesWithTotal(summary, screenName)
	return causes
}

func mainThreadStallCausesWithTotal(summary analyze.Summary, screenName string) ([]uiCauseInsight, int) {
	type stallAggregate struct {
		count     int
		maxMS     uint64
		owner     string
		operation string
	}
	better := func(left, right stallAggregate) bool {
		if left.maxMS != right.maxMS {
			return left.maxMS > right.maxMS
		}
		if left.owner != right.owner {
			return left.owner < right.owner
		}
		return left.operation < right.operation
	}
	sameGroup := func(left stallAggregate, item analyze.SignalContextStats) bool {
		return left.owner == item.Owner && left.operation == item.Operation
	}
	aggregates := make([]stallAggregate, 0, mainThreadStallCauseLimit)
	total := 0
	for _, item := range summary.SignalContexts {
		if !sameKnownReportValue(item.Screen, screenName) || item.StallCount == 0 {
			continue
		}
		total = saturatingAddNonNegativeInt(total, 1)
		index := -1
		for candidateIndex := range aggregates {
			if sameGroup(aggregates[candidateIndex], item) {
				index = candidateIndex
				break
			}
		}
		if index < 0 && len(aggregates) < mainThreadStallCauseLimit {
			aggregates = append(aggregates, stallAggregate{
				owner: item.Owner, operation: item.Operation, maxMS: item.StallMaxMS,
			})
			continue
		}
		if index < 0 {
			worstIndex := 0
			for candidateIndex := 1; candidateIndex < len(aggregates); candidateIndex++ {
				if better(aggregates[worstIndex], aggregates[candidateIndex]) {
					worstIndex = candidateIndex
				}
			}
			candidate := stallAggregate{owner: item.Owner, operation: item.Operation, maxMS: item.StallMaxMS}
			if !better(candidate, aggregates[worstIndex]) {
				continue
			}
			aggregates[worstIndex] = candidate
			continue
		}
		if item.StallMaxMS > aggregates[index].maxMS {
			aggregates[index].maxMS = item.StallMaxMS
		}
	}
	for _, item := range summary.SignalContexts {
		if !sameKnownReportValue(item.Screen, screenName) || item.StallCount == 0 {
			continue
		}
		for index := range aggregates {
			if sameGroup(aggregates[index], item) {
				aggregates[index].count = saturatingAddNonNegativeInt(aggregates[index].count, item.StallCount)
				break
			}
		}
	}
	causes := make([]uiCauseInsight, 0, min(len(aggregates), mainThreadStallCauseLimit))
	for _, aggregate := range aggregates {
		stack := analyze.BestMainThreadStallStack(summary.Owners, aggregate.owner)
		where := uiLocation(aggregate.operation, aggregate.owner, stack)
		evidence := fmt.Sprintf("Главный поток останавливался %s; самая длинная пауза - %d мс.", russianCount(aggregate.count, "раз", "раза", "раз"), aggregate.maxMS)
		if stack != "" {
			evidence += " Во время паузы стек указывал на " + stack + "."
		}
		diagnosis := analyze.DiagnoseMainThreadStall(aggregate.owner, stack)
		causes = append(causes, uiCauseInsight{
			Relation:      "пауза подтверждена",
			RelationClass: "direct",
			Title:         diagnosis.Title,
			Evidence:      evidence,
			Explanation:   diagnosis.Explanation,
			Where:         where,
			Action:        diagnosis.Action,
			rank:          650,
			magnitude:     saturatingMulUint64(aggregate.maxMS, 1_000),
			stableKey:     "stall\x00" + aggregate.owner + "\x00" + stack,
		})
	}
	sort.SliceStable(causes, func(i, j int) bool {
		return uiCauseBetter(causes[i], causes[j])
	})
	return causes, total
}

func longTaskCauses(summary analyze.Summary, screenName string) ([]uiCauseInsight, int) {
	type candidate struct {
		window      analyze.ProblemWindowStats
		title       string
		relation    string
		explanation string
		action      string
		rank        int
	}
	better := func(left, right candidate) bool {
		if left.rank != right.rank {
			return left.rank > right.rank
		}
		if left.window.MaxMS != right.window.MaxMS {
			return left.window.MaxMS > right.window.MaxMS
		}
		if left.window.Kind != right.window.Kind {
			return left.window.Kind < right.window.Kind
		}
		return left.window.Owner < right.window.Owner
	}
	selected := newBoundedCandidates[candidate](uiScreenCauseLimit)
	for _, window := range summary.ProblemWindows {
		if !sameKnownReportValue(window.Screen, screenName) || window.MaxMS == 0 {
			continue
		}
		title, relation, explanation, action, rank := uiProblemWindowDescription(window.Kind)
		if title == "" {
			continue
		}
		current := candidate{
			window: window, title: title, relation: relation, explanation: explanation, action: action, rank: rank,
		}
		selected.retain(current, better)
	}
	sort.SliceStable(selected.values, func(i, j int) bool {
		return better(selected.values[i], selected.values[j])
	})
	causes := make([]uiCauseInsight, 0, len(selected.values))
	for _, current := range selected.values {
		window := current.window
		causes = append(causes, uiCauseInsight{
			Relation:      current.relation,
			RelationClass: "strong",
			Title:         current.title,
			Evidence:      fmt.Sprintf("Зафиксировано %s; максимум - %d мс.", russianCount(window.Count, "срабатывание", "срабатывания", "срабатываний"), window.MaxMS),
			Explanation:   current.explanation,
			Where:         uiLocation(window.Operation, window.Owner, ""),
			Action:        current.action,
			rank:          current.rank,
			magnitude:     saturatingMulUint64(window.MaxMS, 1_000),
			stableKey:     "task\x00" + window.Kind + "\x00" + window.Owner,
		})
	}
	return causes, selected.total
}

func runtimeCallCauses(summary analyze.Summary, screenName string, deadlineMS uint64) []uiCauseInsight {
	causes, _ := runtimeCallCausesWithTotal(summary, screenName, deadlineMS)
	return causes
}

func runtimeCallCausesWithTotal(
	summary analyze.Summary,
	screenName string,
	deadlineMS uint64,
) ([]uiCauseInsight, int) {
	const visibleCauseLimit = 2
	selectedSignatures, total := runtimeCallCauseSelection(
		summary.RuntimeCalls,
		screenName,
		deadlineMS,
		visibleCauseLimit,
	)
	if len(selectedSignatures) == 0 {
		return nil, 0
	}
	selected := make(map[runtimeCallSignature]struct{}, len(selectedSignatures))
	for _, signature := range selectedSignatures {
		selected[signature] = struct{}{}
	}
	groups := make(map[runtimeCallSignature]runtimeCallGroup, len(selectedSignatures))
	for _, call := range summary.RuntimeCalls {
		if !runtimeCallEligibleForScreen(call, screenName, deadlineMS) {
			continue
		}
		signature := runtimeCallSignature{
			Operation: call.Operation, Count: call.Count, TotalMS: call.TotalMS, MaxMS: call.MaxMS,
		}
		if _, retained := selected[signature]; !retained {
			continue
		}
		group, exists := groups[signature]
		if !exists {
			group.first = call
		} else {
			if group.calls == nil {
				group.calls = make([]analyze.RuntimeCallStats, 1, 4)
				group.calls[0] = group.first
			}
			group.calls = append(group.calls, call)
		}
		groups[signature] = group
	}

	causes := make([]uiCauseInsight, 0, visibleCauseLimit)
	for _, signature := range selectedSignatures {
		if len(causes) == visibleCauseLimit &&
			saturatingMulUint64(signature.MaxMS, 1_000) < causes[visibleCauseLimit-1].magnitude {
			break
		}
		group := groups[signature]
		if group.calls == nil {
			single := [...]analyze.RuntimeCallStats{group.first}
			retainRuntimeCallCause(&causes, runtimeCallCause(signature, single[:]), visibleCauseLimit)
			continue
		}
		for _, component := range selectConnectedRuntimeCallComponents(group.calls, visibleCauseLimit) {
			retainRuntimeCallCause(&causes, runtimeCallCause(signature, component), visibleCauseLimit)
		}
	}
	sort.SliceStable(causes, func(i, j int) bool {
		return uiCauseBetter(causes[i], causes[j])
	})
	return causes, total
}

func runtimeCallCauseSelection(
	calls []analyze.RuntimeCallStats,
	screenName string,
	deadlineMS uint64,
	limit int,
) ([]runtimeCallSignature, int) {
	if limit <= 0 {
		return nil, 0
	}
	top := make([]runtimeCallSignature, 0, limit)
	total := 0
	for _, call := range calls {
		if !runtimeCallEligibleForScreen(call, screenName, deadlineMS) {
			continue
		}
		total = saturatingAddNonNegativeInt(total, 1)
		signature := runtimeCallSignature{
			Operation: call.Operation, Count: call.Count, TotalMS: call.TotalMS, MaxMS: call.MaxMS,
		}
		duplicate := false
		for _, existing := range top {
			if existing == signature {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		if len(top) < limit {
			top = append(top, signature)
			for index := len(top) - 1; index > 0 && runtimeCallSignatureBetter(top[index], top[index-1]); index-- {
				top[index], top[index-1] = top[index-1], top[index]
			}
			continue
		}
		if runtimeCallSignatureBetter(signature, top[len(top)-1]) {
			top[len(top)-1] = signature
			for index := len(top) - 1; index > 0 && runtimeCallSignatureBetter(top[index], top[index-1]); index-- {
				top[index], top[index-1] = top[index-1], top[index]
			}
		}
	}
	if len(top) == 0 {
		return nil, 0
	}
	return top, total
}

func runtimeCallSignatureBetter(left, right runtimeCallSignature) bool {
	if left.MaxMS != right.MaxMS {
		return left.MaxMS > right.MaxMS
	}
	if left.Operation != right.Operation {
		return left.Operation < right.Operation
	}
	if left.TotalMS != right.TotalMS {
		return left.TotalMS > right.TotalMS
	}
	return left.Count > right.Count
}

func runtimeCallEligibleForScreen(call analyze.RuntimeCallStats, screenName string, deadlineMS uint64) bool {
	return !analyze.IsSemanticRuntimeCall(call.Caller) &&
		sameKnownReportValue(call.Screen, screenName) &&
		call.MaxMS >= deadlineMS &&
		!isUnknownReportValue(call.Callee)
}

type runtimeCallGroup struct {
	first analyze.RuntimeCallStats
	calls []analyze.RuntimeCallStats
}

func retainRuntimeCallCause(causes *[]uiCauseInsight, candidate uiCauseInsight, limit int) {
	if len(*causes) < limit {
		*causes = append(*causes, candidate)
		sort.SliceStable(*causes, func(i, j int) bool {
			return uiCauseBetter((*causes)[i], (*causes)[j])
		})
		return
	}
	if uiCauseBetter(candidate, (*causes)[limit-1]) {
		(*causes)[limit-1] = candidate
		sort.SliceStable(*causes, func(i, j int) bool {
			return uiCauseBetter((*causes)[i], (*causes)[j])
		})
	}
}

type runtimeCallSignature struct {
	Operation string
	Count     uint64
	TotalMS   uint64
	MaxMS     uint64
}

func selectConnectedRuntimeCallComponents(
	calls []analyze.RuntimeCallStats,
	limit int,
) [][]analyze.RuntimeCallStats {
	if len(calls) == 0 || limit <= 0 {
		return nil
	}
	parent := make([]int, len(calls))
	rank := make([]uint8, len(calls))
	firstEdgeByNode := make(map[string]int, len(calls))
	for index, call := range calls {
		parent[index] = index
		for _, node := range [...]string{call.Caller, call.Callee} {
			if previous, exists := firstEdgeByNode[node]; exists {
				unionRuntimeCallComponents(parent, rank, index, previous)
			} else {
				firstEdgeByNode[node] = index
			}
			if call.Caller == call.Callee {
				break
			}
		}
	}

	bestEdgeByRoot := make([]int, len(calls))
	for index := range bestEdgeByRoot {
		bestEdgeByRoot[index] = -1
	}
	for index := range calls {
		root := findRuntimeCallComponent(parent, index)
		best := bestEdgeByRoot[root]
		if best < 0 || runtimeCallEdgeBetter(calls[index], calls[best]) {
			bestEdgeByRoot[root] = index
		}
	}
	selectedRoots := make([]int, 0, min(limit, len(calls)))
	for root, best := range bestEdgeByRoot {
		if best < 0 {
			continue
		}
		insert := len(selectedRoots)
		for index, selectedRoot := range selectedRoots {
			if runtimeCallEdgeBetter(calls[best], calls[bestEdgeByRoot[selectedRoot]]) {
				insert = index
				break
			}
		}
		if len(selectedRoots) < limit {
			selectedRoots = append(selectedRoots, root)
			copy(selectedRoots[insert+1:], selectedRoots[insert:len(selectedRoots)-1])
			selectedRoots[insert] = root
		} else if insert < limit {
			copy(selectedRoots[insert+1:], selectedRoots[insert:limit-1])
			selectedRoots[insert] = root
		}
	}

	components := make([][]analyze.RuntimeCallStats, len(selectedRoots))
	for index := range calls {
		root := findRuntimeCallComponent(parent, index)
		for componentIndex, selectedRoot := range selectedRoots {
			if root == selectedRoot {
				components[componentIndex] = append(components[componentIndex], calls[index])
				break
			}
		}
	}
	for _, component := range components {
		sort.Slice(component, func(i, j int) bool {
			return runtimeCallEdgeBetter(component[i], component[j])
		})
	}
	return components
}

func findRuntimeCallComponent(parent []int, index int) int {
	root := index
	for parent[root] != root {
		root = parent[root]
	}
	for parent[index] != index {
		next := parent[index]
		parent[index] = root
		index = next
	}
	return root
}

func unionRuntimeCallComponents(parent []int, rank []uint8, left, right int) {
	leftRoot := findRuntimeCallComponent(parent, left)
	rightRoot := findRuntimeCallComponent(parent, right)
	if leftRoot == rightRoot {
		return
	}
	if rank[leftRoot] < rank[rightRoot] {
		leftRoot, rightRoot = rightRoot, leftRoot
	}
	parent[rightRoot] = leftRoot
	if rank[leftRoot] == rank[rightRoot] {
		rank[leftRoot]++
	}
}

func runtimeCallEdgeBetter(left, right analyze.RuntimeCallStats) bool {
	if left.Caller != right.Caller {
		return left.Caller < right.Caller
	}
	return left.Callee < right.Callee
}

func runtimeCallCause(signature runtimeCallSignature, calls []analyze.RuntimeCallStats) uiCauseInsight {
	path, extraEdges := runtimeCallPath(calls)
	title, explanation, action := runtimeCallNarrative(path)
	evidence := fmt.Sprintf(
		"%s; максимум - %d мс, суммарно - %d мс.",
		russianCount(signature.Count, "вызов", "вызова", "вызовов"),
		signature.MaxMS,
		signature.TotalMS,
	)
	if len(calls) > 1 {
		evidence = fmt.Sprintf(
			"Связанная цепочка из %s; %s, максимум - %d мс.",
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
		Relation:      "возможная причина в коде",
		RelationClass: "context",
		Title:         title,
		Evidence:      evidence,
		Explanation: explanation + " Граф вызовов не хранит признак главного потока и точное пересечение с кадром, " +
			"поэтому это место для проверки, а не доказанная причина.",
		Where:     "Цепочка кода: " + location + ".",
		Action:    action,
		rank:      450,
		magnitude: saturatingMulUint64(signature.MaxMS, 1_000),
		stableKey: "runtime\x00" + strings.Join(path, "\x00"),
	}
}

func runtimeCallPath(calls []analyze.RuntimeCallStats) ([]string, int) {
	if len(calls) == 0 {
		return nil, 0
	}
	if len(calls) == 1 {
		call := calls[0]
		if call.Caller == call.Callee {
			return []string{call.Caller}, 1
		}
		return []string{call.Caller, call.Callee}, 0
	}
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
			"Цепочка содержит onDraw/dispatchDraw - это участок построения кадра. Частые аллокации, сложная геометрия, Bitmap/Path и повторные вычисления здесь особенно подозрительны.",
			"Профилируйте onDraw/dispatchDraw: уберите создание объектов и тяжёлые вычисления, кэшируйте неизменяемые данные и проверьте частоту invalidate."
	case strings.Contains(joined, ".onmeasure"), strings.Contains(joined, ".onlayout"):
		return "Измерение или компоновка View выполнялись дольше бюджета кадра",
			"Цепочка содержит onMeasure/onLayout - это признак дорогой компоновки, повторных requestLayout или слишком сложной иерархии.",
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
	causes, _ := networkCausesWithTotal(summary, screenName)
	return causes
}

func networkCausesWithTotal(summary analyze.Summary, screenName string) ([]uiCauseInsight, int) {
	selected := newBoundedCandidates[analyze.SignalContextStats](uiScreenCauseLimit)
	for _, context := range summary.SignalContexts {
		if !sameKnownReportValue(context.Screen, screenName) || (context.HTTPFailed == 0 && context.HTTPP95MS < 700) {
			continue
		}
		selected.retain(context, networkCauseBetter)
	}
	sort.SliceStable(selected.values, func(i, j int) bool {
		return networkCauseBetter(selected.values[i], selected.values[j])
	})
	causes := make([]uiCauseInsight, 0, len(selected.values))
	for _, context := range selected.values {
		route := reportValue(context.RouteSample, "сетевой маршрут")
		title := route + " отвечал медленно"
		if context.HTTPFailed > 0 {
			title = route + " завершался с ошибкой или медленно"
		}
		causes = append(causes, uiCauseInsight{
			Relation:      "совпало в операции",
			RelationClass: "context",
			Title:         title,
			Evidence:      operationContextHTTPText(context),
			Explanation:   "Сетевой вызов записан в контексте той же операции и экрана. Журнал не показывает, ожидал ли его главный поток и пересёкся ли он с медленным кадром, поэтому сам по себе запрос не доказывает причину подтормаживаний.",
			Where:         uiLocation(context.Operation, context.Owner, context.RouteSample),
			Action:        "Проверьте, что запрос выполняется асинхронно, главный поток не ждёт результат, а разбор ответа и обновление UI не создают большую работу одним блоком.",
			rank:          350,
			magnitude:     saturatingMulUint64(context.HTTPP95MS, 1_000),
			stableKey:     "network\x00" + context.RouteSample + "\x00" + context.Owner,
		})
	}
	return causes, selected.total
}

func networkCauseBetter(left, right analyze.SignalContextStats) bool {
	if left.HTTPP95MS != right.HTTPP95MS {
		return left.HTTPP95MS > right.HTTPP95MS
	}
	if left.RouteSample != right.RouteSample {
		return left.RouteSample < right.RouteSample
	}
	return left.Owner < right.Owner
}

func logSpamCauses(summary analyze.Summary, screenName string) []uiCauseInsight {
	causes, _ := logSpamCausesWithTotal(summary, screenName)
	return causes
}

func logSpamCausesWithTotal(summary analyze.Summary, screenName string) ([]uiCauseInsight, int) {
	selected := newBoundedCandidates[analyze.LogSpamStats](uiScreenCauseLimit)
	for _, item := range summary.LogSpam {
		if !sameKnownReportValue(item.Screen, screenName) || item.Count < 50 {
			continue
		}
		selected.retain(item, logSpamCauseBetter)
	}
	sort.SliceStable(selected.values, func(i, j int) bool {
		return logSpamCauseBetter(selected.values[i], selected.values[j])
	})
	causes := make([]uiCauseInsight, 0, len(selected.values))
	for _, item := range selected.values {
		causes = append(causes, uiCauseInsight{
			Relation:      "может усиливать",
			RelationClass: "factor",
			Title:         "Частое логирование добавляло работу в этом экране",
			Evidence:      russianCount(item.Count, "запись", "записи", "записей") + " из " + reportValue(item.Source, "источника без названия") + ".",
			Explanation:   "Частое форматирование и вывод логов расходуют CPU и могут добавлять I/O. Совпадение экрана не доказывает, что именно логирование сорвало кадр.",
			Where:         uiLocation(item.Operation, item.Owner, item.Source),
			Action:        "Уберите повторяющиеся записи из горячего пути, затем повторите сценарий и сравните долю медленных кадров.",
			rank:          250,
			magnitude:     item.Count,
			stableKey:     "log\x00" + item.Owner + "\x00" + item.Source,
		})
	}
	return causes, selected.total
}

func logSpamCauseBetter(left, right analyze.LogSpamStats) bool {
	if left.Count != right.Count {
		return left.Count > right.Count
	}
	if left.Owner != right.Owner {
		return left.Owner < right.Owner
	}
	return left.Source < right.Source
}

func memoryPressureCause(summary analyze.Summary, screenName string) (uiCauseInsight, bool) {
	var maxKB uint64
	var owner, operation string
	for _, item := range summary.SignalContexts {
		if !sameKnownReportValue(item.Screen, screenName) || item.MemoryMaxKB < 256*1024 {
			continue
		}
		if item.MemoryMaxKB >= maxKB {
			maxKB = item.MemoryMaxKB
			owner, operation = item.Owner, item.Operation
		}
	}
	if maxKB == 0 {
		return uiCauseInsight{}, false
	}
	return uiCauseInsight{
		Relation:      "может усиливать",
		RelationClass: "factor",
		Title:         "Высокое потребление памяти могло усилить паузы сборки мусора",
		Evidence:      "PSS в этом контексте доходил до " + humanDataSizeKB(maxKB) + ".",
		Explanation:   "Большой объём памяти повышает риск частых или долгих сборок мусора, но один максимум занятой процессом памяти не доказывает такую паузу внутри медленного кадра.",
		Where:         uiLocation(operation, owner, ""),
		Action:        "Сопоставьте временную шкалу сборки мусора и выделений памяти с медленными кадрами; ищите массовое создание объектов при построении или обновлении интерфейса.",
		rank:          150,
		magnitude:     maxKB,
		stableKey:     "memory\x00" + owner,
	}, true
}

func uiProblemWindowDescription(kind string) (title, relation, explanation, action string, rank int) {
	switch kind {
	case "main_thread_dispatch":
		return "Обработка сообщения главного потока заняла слишком долго", "главный поток подтверждён", "Длительная обработка сообщения измерена непосредственно на главном потоке в этом контексте.", "Разбейте обработчик сообщения на короткие части и вынесите вычисления или файловые операции из главного потока.", 620
	case "wrapped_click":
		return "Обработчик нажатия выполнялся слишком долго", "долгая работа подтверждена", "Длительность обработчика пользовательского нажатия измерена напрямую; такой обработчик выполняется при построении интерфейса.", "Откройте обработчик нажатия и оставьте в нём только быстрое изменение состояния; файловые операции и тяжёлые вычисления перенесите из главного потока.", 600
	case "main_thread_io", "main_thread_disk_io", "disk_io_main_thread":
		return "Файловая операция блокировала главный поток", "главный поток подтверждён", "Тип проблемного окна прямо указывает на файловую операцию в главном потоке.", "Перенесите файловую операцию с главного потока и повторите сценарий с теми же входными данными.", 690
	case "wrapped_runnable", "wrapped_callable", "wrapped_coroutine", "wrapped_coroutine_active", "wrapped_executor":
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
		return "Подтормаживание подтверждено, но источник не найден. Повторите размеченный сценарий и запишите трассу главного потока во время самого длинного кадра."
	}
	return "Главный маршрут расследования: " + causes[0].Title + ". Ниже причины отделены от простых совпадений и отсортированы по силе доступных данных."
}

func uiHasMeasuredSlowFrames(screen analyze.ScreenStats) bool {
	return screen.JankyFrames > 0 || uiHasSlowFrameTail(screen)
}

func uiHasSlowFrameTail(screen analyze.ScreenStats) bool {
	tailThresholdMS := defaultFrameTailThresholdMS
	if screen.FrameDeadlineStatus == "consistent" && screen.FrameDeadlineUS > 0 {
		tailThresholdMS = max(
			uint64(1),
			microsecondsToMillisecondsCeilReport(saturatingMulUint64(screen.FrameDeadlineUS, 2)),
		)
	}
	return screen.FrameP95MS >= tailThresholdMS ||
		screen.FrameP99MS >= saturatingMulUint64(tailThresholdMS, 2)
}

func uiFrameDeadlineMS(screen analyze.ScreenStats) uint64 {
	deadlineUS := screen.FrameDeadlineUS
	if deadlineUS == 0 {
		deadlineUS = defaultFrameDeadlineUS
	}
	return max(uint64(1), microsecondsToMillisecondsCeilReport(deadlineUS))
}

func uiRelatedSignals(summary analyze.Summary, screenName string) (string, string, string) {
	var httpCount, httpFailed, stalls int
	var maxHTTP, maxStall, logSpam, problems uint64
	var detailedLogSpam, problemWindowSignals uint64
	contexts := make([]string, 0, 3)
	seenContexts := map[string]struct{}{}
	for _, contextStats := range summary.SignalContexts {
		if !sameKnownReportValue(contextStats.Screen, screenName) {
			continue
		}
		httpCount = saturatingAddNonNegativeInt(httpCount, contextStats.HTTPCount)
		httpFailed = saturatingAddNonNegativeInt(httpFailed, contextStats.HTTPFailed)
		stalls = saturatingAddNonNegativeInt(stalls, contextStats.StallCount)
		maxHTTP = max(maxHTTP, contextStats.HTTPP95MS)
		maxStall = max(maxStall, contextStats.StallMaxMS)
		logSpam = saturatingAddUint64(logSpam, contextStats.LogSpam)
		problems = saturatingAddUint64(problems, contextStats.ProblemCount)
		if len(contexts) < cap(contexts) {
			context := labelledOperationContext(contextStats.Operation, contextStats.Owner, contextStats.RouteSample)
			if _, exists := seenContexts[context]; context != "" && !exists {
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
		signals = append(signals, fmt.Sprintf("%s главного потока, самая длинная - %d мс", russianCount(stalls, "пауза", "паузы", "пауз"), maxStall))
	}
	if httpCount > 0 {
		network := fmt.Sprintf("%s, верхняя задержка - %d мс", russianCount(httpCount, "сетевой вызов", "сетевых вызова", "сетевых вызовов"), maxHTTP)
		if httpFailed > 0 {
			network += fmt.Sprintf(", с ошибкой - %d", httpFailed)
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
	where := "Точная операция или источник работ для этого экрана не записаны."
	if len(contexts) > 0 {
		where = "Начните проверку здесь: " + strings.Join(contexts, "; ") + "."
	}
	action := "Повторите экран с размеченной операцией и профилированием главного потока, затем найдите самый длинный кадр."
	switch {
	case stalls > 0:
		action = "Откройте трассу главного потока в указанной операции и уберите длинную синхронную работу из кадра."
	case httpCount > 0:
		action = "Проверьте, не ждёт ли экран сеть на главном потоке и нет ли повторных запросов при перерисовке."
	case logSpam > 0:
		action = "Сократите частое логирование в указанном источнике и повторно измерьте плавность экрана."
	}
	return nearby, where, action
}

func uiLocation(operation, owner, detail string) string {
	context := labelledOperationContext(operation, owner, detail)
	if context == "" {
		return "Точное место не размечено; повторите действие с именем операции и источника."
	}
	return "Проверьте: " + context + "."
}

func reportIOOperationLabel(operation string) string {
	labels := map[string]string{
		"file_read":     "Чтение файла",
		"file_write":    "Запись файла",
		"file_sync":     "Синхронизация файла",
		"content_read":  "Чтение ContentProvider",
		"content_write": "Запись в ContentProvider",
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

func labelledOperationContext(operation, owner, detail string) string {
	parts := make([]string, 0, 3)
	if !isUnknownReportValue(operation) {
		parts = append(parts, "операция "+operation)
	}
	if !isUnknownReportValue(owner) {
		parts = append(parts, "источник "+owner)
	}
	if !isUnknownReportValue(detail) {
		parts = append(parts, detail)
	}
	return strings.Join(parts, " · ")
}
