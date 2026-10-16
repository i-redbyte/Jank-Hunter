package analyze

import (
	"fmt"
	"math"
	"sort"
)

const (
	composeDefaultFrameBudgetMS = uint64(16)
	composeFrequentMinCount     = uint64(120)
	composeFrequentRate         = 10.0
	composeFindingsLimit        = 12
	workerLongExecutionMS       = uint64(10_000)
	workerRepeatedMinCount      = uint64(10)
	workerRepeatedTotalMS       = uint64(30_000)
)

func (b *problemBuilder) detectSemanticWork() {
	work := ActionableSemanticWork(b.summary)
	b.detectComposeWork(work)
	b.detectRoomWork(work)
	if hasTypedWorkerLifecycle(b.summary) {
		b.detectWorkerLifecycle()
	} else {
		b.detectWorkerWork(work)
	}
}

func (b *problemBuilder) detectWorkerLifecycle() {
	for _, item := range b.summary.WorkerAnalysis.Workers {
		failures := item.Failures
		long := item.RunSamples > 0 && item.RunMaxMS >= workerLongExecutionMS
		repeated := item.RunSamples > 0 && item.Executions >= workerRepeatedMinCount && item.TotalRunMS >= workerRepeatedTotalMS
		mainThread := item.MainThreadStarts > 0
		if failures == 0 && !long && !repeated && !mainThread {
			continue
		}
		where := []ProblemLocation{{
			Screen: item.Screen, Operation: item.Operation,
			Owner: firstKnown(item.Worker, item.Owner), Method: item.Worker,
		}}
		title := displayUnknown(item.Worker, "Worker") + ": завершённая попытка достигла порога проверки 10 с"
		subcategory := "worker_long"
		if failures > 0 {
			title = displayUnknown(item.Worker, "Worker") + " имеет неуспешные завершения"
			subcategory = "worker_unsuccessful"
		} else if mainThread {
			title = displayUnknown(item.Worker, "Worker") + ": событие начала записано на главном потоке"
			subcategory = "worker_main_thread"
		} else if repeated {
			title = displayUnknown(item.Worker, "Worker") + ": выполнения класса накопили 30 с"
			subcategory = "worker_repeated"
		}
		confidence, reasons, limits := problemConfidence(b.summary, item.Executions, 1, true)
		if mainThread {
			limits = append(limits, "Флаг главного потока относится только к событию начала: где выполнялись этапы Worker после него, неизвестно.")
		}
		if long {
			limits = append(limits, "Порог 10 с - повод сопоставить завершённую попытку с ожидаемой длительностью задачи; сам по себе он не доказывает дефект или нагрузку на CPU.")
		}
		if repeated {
			limits = append(limits, "Числа объединяют разные UUID одного класса: они не доказывают повторное выполнение одного задания. Частота запусков зависит от расписания и условий, а накопленное время само по себе не доказывает лишнюю работу.")
		}
		if item.MissingStart > 0 || item.MissingFinish > 0 {
			limits = append(limits, fmt.Sprintf(
				"Неполнота сбора lifecycle: без start - %d, без finish - %d; это ограничение данных, а не доказательство ошибки приложения. Незакрытые интервалы не включены в длительность и корреляции.",
				item.MissingStart, item.MissingFinish,
			))
		}
		if workerHasCorrelations(item) {
			limits = append(limits, "CPU, аллокации, GC, память, сеть и I/O сопоставлены только по пересечению времени; это корреляция, а не доказанная причина нагрузки.")
		}
		evidence := []ProblemEvidence{
			{Name: "Число попыток Worker", Observed: fmt.Sprint(item.Executions), Unit: "events", Sample: u64ptr(item.Executions), Source: "worker_lifecycle"},
			{Name: "Завершения с ошибкой", Observed: fmt.Sprint(failures), Unit: "events", Sample: u64ptr(failures), Source: "worker_lifecycle"},
		}
		if item.Retries > 0 || item.Cancelled > 0 {
			evidence = append(evidence, ProblemEvidence{Name: "Повторы и отмены", Observed: fmt.Sprintf("повторов %d, отмен %d", item.Retries, item.Cancelled), Unit: "events", Source: "worker_lifecycle"})
		}
		if item.RunSamples > 0 {
			evidence = append(evidence, ProblemEvidence{Name: "Максимум завершённой попытки", Observed: fmt.Sprint(item.RunMaxMS), Unit: "ms", ExpectedOrThreshold: fmt.Sprintf("порог проверки %d ms", workerLongExecutionMS), Source: "worker_lifecycle"})
		}
		if mainThread {
			evidence = append(evidence, ProblemEvidence{Name: "События начала на главном потоке", Observed: fmt.Sprint(item.MainThreadStarts), Unit: "events", Sample: u64ptr(item.MainThreadStarts), Source: "worker_lifecycle"})
		}
		if item.WaitSamples > 0 {
			evidence = append(evidence, ProblemEvidence{Name: "Ожидание запуска p95", Observed: fmt.Sprint(item.WaitP95MS), Unit: "ms", Source: "worker_lifecycle"})
		}
		if item.CorrelatedHTTPCalls > 0 {
			evidence = append(evidence, ProblemEvidence{Name: "HTTP в окнах выполнения", Observed: fmt.Sprint(item.CorrelatedHTTPCalls), Unit: "events", Source: "worker_time_correlation"})
		}
		if item.CPUSamples > 0 {
			evidence = append(evidence, ProblemEvidence{Name: "Максимальная загрузка CPU устройства в окнах выполнения", Observed: fmt.Sprintf("%.2f", float64(item.MaxDeviceCPUPercentX100)/100), Unit: "%", Source: "worker_time_correlation"})
		}
		what := fmt.Sprintf("Зафиксировано %s в %s; завершено - %d, ошибок - %d, повторов - %d, отмен - %d, пик параллельных попыток - %d.",
			russianCountUint64(item.Executions, "попытка", "попытки", "попыток"),
			russianCountUint64(item.Instances, "экземпляре", "экземплярах", "экземплярах"),
			item.Finished, failures, item.Retries, item.Cancelled, item.MaxConcurrency)
		if item.RunSamples > 0 {
			what += fmt.Sprintf(" Максимум завершённой попытки - %d мс.", item.RunMaxMS)
		}
		if mainThread {
			what += fmt.Sprintf(" Событий начала на главном потоке - %d; это не измерение потока выполнения всей задачи.", item.MainThreadStarts)
		}
		action := "Сопоставить длительность и частоту запусков с ожидаемым сценарием; измерить отдельно дорогие этапы Worker."
		if failures > 0 {
			action = "Проверить причины завершения с ошибкой, условия запуска и ожидаемое поведение retry."
		} else if mainThread {
			action = "Проверить, выполнялись ли дорогие этапы Worker на главном потоке; если да, перенести их в фоновое выполнение."
		}
		b.add(ProblemFinding{
			DetectorID: "cpu.worker_execution", DetectorVersion: b.cfg.Version,
			Category: ProblemCategoryCPU, Subcategory: subcategory, Status: "observed",
			Confidence: confidence, ConfidenceReasons: reasons, Title: title,
			WhatHappened: what,
			Where:        where,
			Why: ProblemWhy{
				ClaimLevel: "linked",
				Summary:    "Типизированный lifecycle записал доступные события enqueue, start и finish; длительность известна только для завершённых попыток, а поток дальнейших этапов не установлен.",
			},
			Impact:    []string{"Возможна задержка фоновой задачи; влияние на интерфейс, CPU и батарею по этим событиям не установлено"},
			Evidence:  evidence,
			Frequency: &ProblemFrequency{Count: item.Executions, RatePerSec: ratePerSecond(item.Executions, b.summary.DurationMS)},
			Cost:      &ProblemCost{WallTimeMS: nonZeroU64Ptr(item.TotalRunMS)},
			PriorityBreakdown: priority(
				workerProblemImpact(failures > 0),
				boundedUint64RatioScore(item.RunMaxMS, workerLongExecutionMS, 7, 4, 25),
				min(20, 4+int(math.Log2(float64(item.Executions)+1))*3),
				locationBreadth(where), boolScore(failures > 0 && repeated, 5),
				"срыв фоновой работы", "длительность", "повторы", "контекст", "ошибка и повторы",
			),
			Recommendations: []ProblemRecommendation{{
				Action:       action,
				Rationale:    "События lifecycle и временные корреляции помогают выбрать участок для проверки, но не доказывают причину задержки или поток выполнения всей задачи.",
				Verification: "Повторить условия запуска и сравнить число попыток, результаты, ожидание, длительность, параллелизм и коррелирующие сигналы.",
			}},
			Limitations: uniqueStrings(limits),
			Drilldowns:  []ProblemDrilldown{{Label: "Worker", Anchor: "workers", Filter: item.Worker}},
		})
	}
}

func workerHasCorrelations(item WorkerStats) bool {
	return item.CorrelatedHTTPCalls > 0 || item.CorrelatedIOOperations > 0 || item.CPUSamples > 0 ||
		item.CoreCPUSamples > 0 || item.AllocationSamples > 0 || item.GCCount > 0 || item.MemoryPairs > 0
}

func (b *problemBuilder) detectComposeWork(work []SemanticWorkStats) {
	type candidate struct {
		work               SemanticWorkStats
		budgetMS           uint64
		executionsPerFrame float64
		slow               bool
		frequent           bool
	}
	candidates := make([]candidate, 0)
	for _, item := range work {
		if item.Domain != SemanticDomainCompose || !item.MainThread || item.Count == 0 {
			continue
		}
		frames, budgetMS := b.composeScreenContext(item.Screen)
		executionsPerFrame := 0.0
		if frames > 0 {
			executionsPerFrame = float64(item.Count) / float64(frames)
		}
		rate := ratePerSecond(item.Count, b.summary.DurationMS)
		frequent := item.Count >= composeFrequentMinCount && item.TotalMS >= 100 &&
			(executionsPerFrame >= 2 || rate != nil && *rate >= composeFrequentRate)
		slow := item.MaxMS >= budgetMS
		if slow || frequent {
			candidates = append(candidates, candidate{item, budgetMS, executionsPerFrame, slow, frequent})
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].slow != candidates[j].slow {
			return candidates[i].slow
		}
		if candidates[i].work.MaxMS != candidates[j].work.MaxMS {
			return candidates[i].work.MaxMS > candidates[j].work.MaxMS
		}
		if candidates[i].work.Count != candidates[j].work.Count {
			return candidates[i].work.Count > candidates[j].work.Count
		}
		return semanticWorkKey(candidates[i].work) < semanticWorkKey(candidates[j].work)
	})
	if len(candidates) > composeFindingsLimit {
		candidates = candidates[:composeFindingsLimit]
	}
	for _, candidate := range candidates {
		item := candidate.work
		where := []ProblemLocation{{Screen: item.Screen, Operation: item.ContextOperation, Owner: item.Owner, Method: item.Owner}}
		phase := composePhaseLabel(item.Operation)
		title := fmt.Sprintf("%s занимает бюджет кадра", displayUnknown(item.Owner, "Compose-код"))
		if candidate.frequent && !candidate.slow {
			title = fmt.Sprintf("%s выполняется подозрительно часто", displayUnknown(item.Owner, "Compose-код"))
		} else if candidate.frequent {
			title = fmt.Sprintf("%s выполняется часто и надолго", displayUnknown(item.Owner, "Compose-код"))
		}
		what := fmt.Sprintf("%s: %s, самое долгое - %d мс", phase, russianCountUint64(item.Count, "выполнение", "выполнения", "выполнений"), item.MaxMS)
		if candidate.executionsPerFrame >= 0.1 {
			what += fmt.Sprintf(", в среднем %.1f на записанный кадр", candidate.executionsPerFrame)
		}
		what += "."
		claim, why := "linked", "SDK напрямую измерил выполнение этой Compose-функции на главном потоке."
		compound := 0
		if b.screenHasJank(item.Screen) {
			claim = "correlated"
			why = "Медленные кадры и эта Compose-работа записаны на одном экране. Без ID кадра связь возможна, но не доказана."
			compound = 4
		}
		confidence, reasons, limits := problemConfidence(b.summary, item.Count, 3, true)
		limits = append(limits, "Выполнение @Composable не всегда является лишней рекомпозицией; пропущенные Compose-группы не выполняются и в этот счётчик не входят.")
		b.add(ProblemFinding{
			DetectorID: "ui.compose_work", DetectorVersion: b.cfg.Version, Category: ProblemCategoryUI,
			Subcategory: "compose_" + item.Operation, Status: "observed", Confidence: confidence,
			ConfidenceReasons: reasons, Title: title, WhatHappened: what, Where: where,
			Why:    ProblemWhy{ClaimLevel: claim, Summary: why, Factors: composeFactors(candidate.slow, candidate.frequent)},
			Impact: []string{"Долгая композиция, измерение или отрисовка может пропустить кадр; частые выполнения расходуют время главного потока"},
			Evidence: []ProblemEvidence{
				{Name: "Число выполнений", Observed: fmt.Sprint(item.Count), Unit: "events", Sample: u64ptr(item.Count), Source: "compose_boundary"},
				{Name: "Самое долгое выполнение", Observed: fmt.Sprint(item.MaxMS), Unit: "ms", ExpectedOrThreshold: fmt.Sprintf("< %d ms", candidate.budgetMS), Source: "compose_boundary"},
			},
			Frequency:         &ProblemFrequency{Count: item.Count, RatePerSec: ratePerSecond(item.Count, b.summary.DurationMS)},
			PriorityBreakdown: priority(28, boundedUint64RatioScore(item.MaxMS, maxUint64(candidate.budgetMS, 1), 7, 4, 25), min(20, 5+int(math.Log2(float64(item.Count)+1))*2), locationBreadth(where), compound, "риск задержки кадра", "длительность Compose-работы", "число выполнений", "контекст экрана", "совпадение с jank экрана"),
			Recommendations:   []ProblemRecommendation{{Action: composeRecommendation(item.Operation, candidate.frequent), Rationale: "Проверка параметров и состояния рядом с указанной функцией поможет найти источник повторной или тяжёлой работы.", Verification: "Повторить тот же экран и сравнить число выполнений, максимум длительности и долю медленных кадров."}},
			Limitations:       uniqueStrings(limits), Drilldowns: []ProblemDrilldown{{Label: "UI и причины", Anchor: "stability-ui", Filter: item.Owner}},
		})
	}
}

func (b *problemBuilder) detectRoomWork(work []SemanticWorkStats) {
	for _, item := range work {
		if item.Domain != SemanticDomainRoom || item.Count == 0 {
			continue
		}
		rate := ratePerSecond(item.Count, b.summary.DurationMS)
		storm := item.Count >= b.cfg.IOStormMinCount && rate != nil && *rate >= b.cfg.IOStormRate
		slow := item.MainThread && item.MaxMS >= b.cfg.IOMainThreadMS || !item.MainThread && item.MaxMS >= b.cfg.IOBackgroundMS
		if !slow && !storm {
			continue
		}
		where := []ProblemLocation{{Screen: item.Screen, Operation: item.ContextOperation, Owner: item.Owner, Method: item.Owner}}
		detector, subcategory, title, impact, action := "io.room_pressure", "room_background", "Room DAO выполняется слишком долго", 18, "Проверить план запроса, индексы, размер результата и число обращений к DAO"
		if item.MainThread {
			detector, subcategory, title, impact = "io.room_main_thread", "room_main_thread", "Room DAO блокирует главный поток", 34
			action = "Запретить выполнение запроса на главном потоке и перенести его в асинхронное или фоновое выполнение"
		} else if storm {
			title, subcategory = "Room DAO вызывается слишком часто", "room_frequent"
		}
		confidence, reasons, limits := problemConfidence(b.summary, item.Count, 3, true)
		limits = append(limits, "Автоматическая граница DAO точно измеряет синхронный вызов; полную длительность асинхронного DAO следует дополнить traceIO.")
		threshold := b.cfg.IOBackgroundMS
		whySummary := "SDK напрямую связал DAO, длительность и фоновый поток."
		impactSummary := "Задержка данных, конкуренция за БД и лишняя фоновая нагрузка"
		if item.MainThread {
			threshold = b.cfg.IOMainThreadMS
			whySummary = "SDK напрямую связал DAO, длительность и выполнение на главном потоке."
			impactSummary = "Блокировка кадра и ввода, рост риска ANR при длинном запросе"
		}
		b.add(ProblemFinding{
			DetectorID: detector, DetectorVersion: b.cfg.Version, Category: ProblemCategoryIO, Subcategory: subcategory,
			Status: "observed", Confidence: confidence, ConfidenceReasons: reasons,
			Title:             title + ": " + displayUnknown(item.Owner, "метод не определён"),
			WhatHappened:      fmt.Sprintf("DAO вызван %d раз; самое долгое выполнение - %d мс, суммарное время границ - %d мс.", item.Count, item.MaxMS, item.TotalMS),
			Where:             where,
			Why:               ProblemWhy{ClaimLevel: "linked", Summary: whySummary},
			Impact:            []string{impactSummary},
			Evidence:          []ProblemEvidence{{Name: "Число вызовов DAO", Observed: fmt.Sprint(item.Count), Unit: "events", Sample: u64ptr(item.Count), Source: "room_dao_boundary"}, {Name: "Самый долгий вызов", Observed: fmt.Sprint(item.MaxMS), Unit: "ms", ExpectedOrThreshold: fmt.Sprintf("< %d ms", threshold), Source: "room_dao_boundary"}},
			Frequency:         &ProblemFrequency{Count: item.Count, RatePerSec: rate},
			PriorityBreakdown: priority(impact, boundedUint64RatioScore(item.MaxMS, maxUint64(threshold, 1), 7, 4, 25), min(20, 4+int(math.Log2(float64(item.Count)+1))*2), locationBreadth(where), boolScore(storm && slow, 4), "влияние БД", "длительность DAO", "частота", "контекст", "частота и задержка"),
			Recommendations:   []ProblemRecommendation{{Action: action, Rationale: "Измерена конкретная граница DAO, поэтому проверку можно начать с указанного метода.", Verification: "Повторить сценарий и сравнить число вызовов, максимальную длительность и медленные кадры на том же экране."}},
			Limitations:       uniqueStrings(limits), Drilldowns: []ProblemDrilldown{{Label: "Связанные сигналы", Anchor: "signal-contexts", Filter: item.Owner}},
		})
	}
}

func (b *problemBuilder) detectWorkerWork(work []SemanticWorkStats) {
	for _, item := range work {
		if item.Domain != SemanticDomainWorker || item.Count == 0 {
			continue
		}
		badOutcome := item.Outcome == "failure"
		long := item.MaxMS >= workerLongExecutionMS
		repeated := item.Count >= workerRepeatedMinCount && item.TotalMS >= workerRepeatedTotalMS
		if !badOutcome && !long && !repeated {
			continue
		}
		where := []ProblemLocation{{Screen: item.Screen, Operation: item.ContextOperation, Owner: item.Owner, Method: item.Owner}}
		title := displayUnknown(item.Owner, "Worker") + ": граница выполнения достигла порога проверки 10 с"
		if badOutcome {
			title = displayUnknown(item.Owner, "Worker") + " завершился: " + workerOutcomeLabel(item.Outcome)
		} else if repeated {
			title = displayUnknown(item.Owner, "Worker") + ": выполнения границы накопили 30 с"
		}
		confidence, reasons, limits := problemConfidence(b.summary, item.Count, 1, true)
		if long {
			limits = append(limits, "Порог 10 с требует сопоставления с ожидаемой длительностью задачи; сам по себе не доказывает дефект или нагрузку на CPU.")
		}
		if repeated {
			limits = append(limits, "Общие числа для этой границы не доказывают повторное выполнение одного UUID или лишнюю работу.")
		}
		if item.Outcome == "unknown" {
			limits = append(limits, "Результат Worker не определён. Синхронный Worker распознаётся автоматически; для CoroutineWorker оберните полное выполнение в traceSuspendingWorker.")
		}
		b.add(ProblemFinding{
			DetectorID: "cpu.worker_execution", DetectorVersion: b.cfg.Version, Category: ProblemCategoryCPU, Subcategory: "worker_" + item.Outcome,
			Status: "observed", Confidence: confidence, ConfidenceReasons: reasons, Title: title,
			WhatHappened: fmt.Sprintf("Зафиксировано %s; самое долгое - %d мс, суммарно - %d мс, итог - %s.", russianCountUint64(item.Count, "выполнение", "выполнения", "выполнений"), item.MaxMS, item.TotalMS, workerOutcomeLabel(item.Outcome)),
			Where:        where, Why: ProblemWhy{ClaimLevel: "linked", Summary: "Типизированная граница связала имя Worker, длительность и результат; поток события не доказывает поток выполнения всей задачи."},
			Impact:    []string{"Возможна задержка фоновой задачи; влияние на интерфейс, CPU и батарею по этой границе не установлено"},
			Evidence:  []ProblemEvidence{{Name: "Число выполнений", Observed: fmt.Sprint(item.Count), Unit: "events", Sample: u64ptr(item.Count), Source: "worker_boundary"}, {Name: "Самое долгое выполнение", Observed: fmt.Sprint(item.MaxMS), Unit: "ms", ExpectedOrThreshold: fmt.Sprintf("порог проверки %d ms", workerLongExecutionMS), Source: "worker_boundary"}},
			Frequency: &ProblemFrequency{Count: item.Count, RatePerSec: ratePerSecond(item.Count, b.summary.DurationMS)}, Cost: &ProblemCost{WallTimeMS: nonZeroU64Ptr(item.TotalMS)},
			PriorityBreakdown: priority(workerProblemImpact(badOutcome), boundedUint64RatioScore(item.MaxMS, workerLongExecutionMS, 7, 4, 25), min(20, 4+int(math.Log2(float64(item.Count)+1))*3), locationBreadth(where), boolScore(badOutcome && repeated, 5), "срыв фоновой работы", "длительность", "повторы", "контекст", "ошибка и повторы"),
			Recommendations:   []ProblemRecommendation{{Action: "Сопоставить длительность и частоту запусков с ожидаемым сценарием; измерить отдельно дорогие этапы Worker.", Rationale: "Граница даёт измеренную длительность, но не доказывает причину задержки или необходимость повторов.", Verification: "Повторить условия запуска и сравнить число выполнений, итог, максимальную и суммарную длительность."}},
			Limitations:       uniqueStrings(limits), Drilldowns: []ProblemDrilldown{{Label: "Связанные сигналы", Anchor: "signal-contexts", Filter: item.Owner}},
		})
	}
}

func workerProblemImpact(high bool) int {
	if high {
		return 28
	}
	return 18
}

func (b *problemBuilder) composeScreenContext(screen string) (uint64, uint64) {
	for _, item := range b.summary.Screens {
		if item.Screen != screen {
			continue
		}
		budget := composeDefaultFrameBudgetMS
		if item.FrameDeadlineUS > 0 {
			budget = maxUint64(1, microsecondsToMillisecondsCeil(item.FrameDeadlineUS))
		}
		return item.Frames, budget
	}
	return 0, composeDefaultFrameBudgetMS
}

func (b *problemBuilder) screenHasJank(screen string) bool {
	if screen == "" {
		return false
	}
	for _, item := range b.summary.Screens {
		if item.Screen != screen {
			continue
		}
		if item.JankyFrames > 0 {
			return true
		}
		budgetMS := composeDefaultFrameBudgetMS
		if item.FrameDeadlineUS > 0 {
			budgetMS = maxUint64(1, microsecondsToMillisecondsCeil(item.FrameDeadlineUS))
		}
		return item.FrameP95MS >= saturatingMultiply(budgetMS, 2) ||
			item.FrameP99MS >= saturatingMultiply(budgetMS, 4)
	}
	return false
}

func composePhaseLabel(value string) string {
	switch value {
	case "composition":
		return "Композиция"
	case "measure":
		return "Измерение"
	case "layout":
		return "Размещение"
	case "draw":
		return "Отрисовка"
	default:
		return ""
	}
}

func composeFactors(slow, frequent bool) []string {
	values := make([]string, 0, 2)
	if slow {
		values = append(values, "одно выполнение превысило бюджет кадра")
	}
	if frequent {
		values = append(values, "много выполнений относительно кадров или длительности прогона")
	}
	return values
}

func composeRecommendation(operation string, frequent bool) string {
	if frequent {
		return "Проверить стабильность параметров, области чтения состояния, ключи и разбиение функций с @Composable; затем исключить ненужные повторные выполнения"
	}
	switch operation {
	case "measure", "layout":
		return "Проверить сложность дерева, повторные измерения, внутренние размеры и пользовательский Layout"
	case "draw":
		return "Проверить пользовательскую отрисовку, выделение памяти, сложные Path/Shader и ненужные запросы перерисовки"
	default:
		return "Вынести тяжёлые вычисления из композиции, мемоизировать результат и проверить стабильность параметров"
	}
}

func workerOutcomeLabel(value string) string {
	switch value {
	case "success":
		return "успешно"
	case "failure":
		return "ошибка"
	case "retry":
		return "повтор"
	case "cancelled":
		return "отменено"
	case "unknown":
		return "неизвестен"
	default:
		return ""
	}
}

func russianCountUint64(value uint64, singular, paucal, plural string) string {
	lastTwo := value % 100
	last := value % 10
	form := plural
	if lastTwo < 11 || lastTwo > 14 {
		switch {
		case last == 1:
			form = singular
		case last >= 2 && last <= 4:
			form = paucal
		}
	}
	return fmt.Sprintf("%d %s", value, form)
}
