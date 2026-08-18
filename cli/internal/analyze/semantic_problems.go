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
	b.detectWorkerWork(work)
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
		where := []ProblemLocation{{Screen: item.Screen, Flow: item.Flow, Step: item.Step, Owner: item.Owner, Method: item.Owner}}
		phase := composePhaseLabel(item.Operation)
		title := fmt.Sprintf("%s занимает бюджет кадра", displayUnknown(item.Owner, "Compose-код"))
		if candidate.frequent && !candidate.slow {
			title = fmt.Sprintf("%s выполняется подозрительно часто", displayUnknown(item.Owner, "Compose-код"))
		} else if candidate.frequent {
			title = fmt.Sprintf("%s выполняется часто и надолго", displayUnknown(item.Owner, "Compose-код"))
		}
		what := fmt.Sprintf("%s: %s, самое долгое — %d мс", phase, russianCountUint64(item.Count, "выполнение", "выполнения", "выполнений"), item.MaxMS)
		if candidate.executionsPerFrame >= 0.1 {
			what += fmt.Sprintf(", в среднем %.1f на записанный кадр", candidate.executionsPerFrame)
		}
		what += "."
		claim, why := "linked", "SDK напрямую измерил выполнение этой Compose-функции на главном потоке."
		compound := 0
		if b.screenHasJank(item.Screen) {
			claim = "correlated"
			why = "Медленные кадры и эта Compose-работа записаны на одном экране. Без временного идентификатора кадра это сильный кандидат, но не доказанное пересечение."
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
			Frequency:       &ProblemFrequency{Count: item.Count, RatePerSec: ratePerSecond(item.Count, b.summary.DurationMS)},
			RankBreakdown:   risk(28, min(25, 7+int(item.MaxMS/maxUint64(candidate.budgetMS, 1))*4), min(20, 5+int(math.Log2(float64(item.Count)+1))*2), locationBreadth(where), compound, "риск задержки кадра", "длительность Compose-работы", "число выполнений", "контекст экрана", "совпадение с jank экрана"),
			Recommendations: []ProblemRecommendation{{Action: composeRecommendation(item.Operation, candidate.frequent), Rationale: "Проверка параметров и состояния рядом с указанной функцией локализует источник повторной или тяжёлой работы.", Verification: "Повторить тот же экран и сравнить число выполнений, максимум длительности и долю медленных кадров."}},
			Limitations:     uniqueStrings(limits), Drilldowns: []ProblemDrilldown{{Label: "UI и причины", Anchor: "stability-ui", Filter: item.Owner}},
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
		where := []ProblemLocation{{Screen: item.Screen, Flow: item.Flow, Step: item.Step, Owner: item.Owner, Method: item.Owner}}
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
		if item.MainThread {
			threshold = b.cfg.IOMainThreadMS
		}
		b.add(ProblemFinding{
			DetectorID: detector, DetectorVersion: b.cfg.Version, Category: ProblemCategoryIO, Subcategory: subcategory,
			Status: "observed", Confidence: confidence, ConfidenceReasons: reasons,
			Title:        title + ": " + displayUnknown(item.Owner, "метод не определён"),
			WhatHappened: fmt.Sprintf("DAO вызван %d раз; самое долгое выполнение — %d мс, суммарное время границ — %d мс.", item.Count, item.MaxMS, item.TotalMS),
			Where:        where, Why: ProblemWhy{ClaimLevel: "linked", Summary: map[bool]string{true: "SDK напрямую связал DAO, длительность и выполнение на главном потоке.", false: "SDK напрямую связал DAO, длительность и фоновый поток."}[item.MainThread]},
			Impact:          []string{map[bool]string{true: "Блокировка кадра и ввода, рост риска ANR при длинном запросе", false: "Задержка данных, конкуренция за БД и лишняя фоновая нагрузка"}[item.MainThread]},
			Evidence:        []ProblemEvidence{{Name: "Число вызовов DAO", Observed: fmt.Sprint(item.Count), Unit: "events", Sample: u64ptr(item.Count), Source: "room_dao_boundary"}, {Name: "Самый долгий вызов", Observed: fmt.Sprint(item.MaxMS), Unit: "ms", ExpectedOrThreshold: fmt.Sprintf("< %d ms", threshold), Source: "room_dao_boundary"}},
			Frequency:       &ProblemFrequency{Count: item.Count, RatePerSec: rate},
			RankBreakdown:   risk(impact, min(25, 7+int(item.MaxMS/maxUint64(threshold, 1))*4), min(20, 4+int(math.Log2(float64(item.Count)+1))*2), locationBreadth(where), boolScore(storm && slow, 4), "влияние БД", "длительность DAO", "частота", "контекст", "частота и задержка"),
			Recommendations: []ProblemRecommendation{{Action: action, Rationale: "Измерена конкретная граница DAO, поэтому проверку можно начать с указанного метода.", Verification: "Повторить сценарий и сравнить число вызовов, максимальную длительность и медленные кадры на том же экране."}},
			Limitations:     uniqueStrings(limits), Drilldowns: []ProblemDrilldown{{Label: "Сценарии", Anchor: "flows", Filter: item.Owner}},
		})
	}
}

func (b *problemBuilder) detectWorkerWork(work []SemanticWorkStats) {
	for _, item := range work {
		if item.Domain != SemanticDomainWorker || item.Count == 0 {
			continue
		}
		badOutcome := item.Outcome == "failure" || item.Outcome == "retry" || item.Outcome == "cancelled"
		long := item.MaxMS >= workerLongExecutionMS
		repeated := item.Count >= workerRepeatedMinCount && item.TotalMS >= workerRepeatedTotalMS
		if !badOutcome && !long && !repeated {
			continue
		}
		where := []ProblemLocation{{Screen: item.Screen, Flow: item.Flow, Step: item.Step, Owner: item.Owner, Method: item.Owner}}
		title := displayUnknown(item.Owner, "Worker") + " выполняется слишком долго"
		if badOutcome {
			title = displayUnknown(item.Owner, "Worker") + " завершился: " + workerOutcomeLabel(item.Outcome)
		} else if repeated {
			title = displayUnknown(item.Owner, "Worker") + " часто повторяет долгую работу"
		}
		confidence, reasons, limits := problemConfidence(b.summary, item.Count, 1, true)
		if item.Outcome == "unknown" {
			limits = append(limits, "Результат Worker не определён. Синхронный Worker распознаётся автоматически; для CoroutineWorker оберните полное выполнение в traceSuspendingWorker.")
		}
		b.add(ProblemFinding{
			DetectorID: "cpu.worker_execution", DetectorVersion: b.cfg.Version, Category: ProblemCategoryCPU, Subcategory: "worker_" + item.Outcome,
			Status: "observed", Confidence: confidence, ConfidenceReasons: reasons, Title: title,
			WhatHappened: fmt.Sprintf("Зафиксировано %s; самое долгое — %d мс, суммарно — %d мс, итог — %s.", russianCountUint64(item.Count, "выполнение", "выполнения", "выполнений"), item.MaxMS, item.TotalMS, workerOutcomeLabel(item.Outcome)),
			Where:        where, Why: ProblemWhy{ClaimLevel: "linked", Summary: "Типизированная граница связала имя Worker, длительность, поток и результат выполнения."},
			Impact:    []string{"Задержка фоновой синхронизации, повторная нагрузка на CPU/сеть/БД и расход батареи"},
			Evidence:  []ProblemEvidence{{Name: "Число выполнений", Observed: fmt.Sprint(item.Count), Unit: "events", Sample: u64ptr(item.Count), Source: "worker_boundary"}, {Name: "Самое долгое выполнение", Observed: fmt.Sprint(item.MaxMS), Unit: "ms", ExpectedOrThreshold: fmt.Sprintf("< %d ms", workerLongExecutionMS), Source: "worker_boundary"}},
			Frequency: &ProblemFrequency{Count: item.Count, RatePerSec: ratePerSecond(item.Count, b.summary.DurationMS)}, Cost: &ProblemCost{WallTimeMS: nonZeroU64Ptr(item.TotalMS)},
			RankBreakdown:   risk(map[bool]int{true: 28, false: 18}[badOutcome], min(25, 7+int(item.MaxMS/workerLongExecutionMS)*4), min(20, 4+int(math.Log2(float64(item.Count)+1))*3), locationBreadth(where), boolScore(badOutcome && repeated, 5), "срыв фоновой работы", "длительность", "повторы", "контекст", "ошибка и повторы"),
			Recommendations: []ProblemRecommendation{{Action: "Проверить ограничения запуска, задержку между повторами, идемпотентность и содержимое doWork; тяжёлые этапы разбить и измерить отдельно", Rationale: "Так устраняются бесконтрольные повторы и локализуется дорогой этап.", Verification: "Повторить условия запуска и сравнить число выполнений, итог, максимальную и суммарную длительность."}},
			Limitations:     uniqueStrings(limits), Drilldowns: []ProblemDrilldown{{Label: "Сценарии", Anchor: "flows", Filter: item.Owner}},
		})
	}
}

func (b *problemBuilder) composeScreenContext(screen string) (uint64, uint64) {
	for _, item := range b.summary.Screens {
		if item.Screen != screen {
			continue
		}
		budget := composeDefaultFrameBudgetMS
		if item.FrameDeadlineUS > 0 {
			budget = maxUint64(1, (item.FrameDeadlineUS+999)/1_000)
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
			budgetMS = maxUint64(1, (item.FrameDeadlineUS+999)/1_000)
		}
		return item.FrameP95MS >= budgetMS*2 || item.FrameP99MS >= budgetMS*4
	}
	return false
}

func composePhaseLabel(value string) string {
	return map[string]string{"composition": "Композиция", "measure": "Измерение", "layout": "Размещение", "draw": "Отрисовка"}[value]
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
	return map[string]string{"success": "успешно", "failure": "ошибка", "retry": "повтор", "cancelled": "отменено", "unknown": "неизвестен"}[value]
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
