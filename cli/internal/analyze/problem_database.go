package analyze

import (
	"fmt"
	"math"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/datavalue"
)

func (b *problemBuilder) detectDatabaseCalls() {
	analysis := b.summary.DatabaseAnalysis
	if analysis == nil {
		return
	}
	for _, statement := range analysis.Statements {
		assessment := AssessDatabaseStatement(statement, b.cfg)
		if !assessment.IsProblem() {
			continue
		}
		callEstimate := DatabaseStatementCallEstimate(statement)
		callText := fmt.Sprint(statement.Overall.Calls)
		if callEstimate > statement.Overall.Calls {
			callText = fmt.Sprintf("≈ %d, детально сохранено %d", callEstimate, statement.Overall.Calls)
		}

		linkedToUI := assessment.MainThreadSlow && statement.MainCorrelation.UIWindowOverlaps > 0 &&
			statement.MainCorrelation.UIOverlapMaxDurationUS >= b.cfg.DatabaseMainThreadMS*1_000
		where := databaseProblemLocations(statement.Contexts, linkedToUI)
		owner := where[0].Owner
		p50MS := microsecondsToMillisecondsCeil(statement.Overall.P50DurationUS)
		p95MS := microsecondsToMillisecondsCeil(statement.Overall.P95DurationUS)
		maxMS := microsecondsToMillisecondsCeil(statement.Overall.MaxDurationUS)
		totalMS := microsecondsToMillisecondsCeil(statement.Overall.TotalDurationUS)
		confidence, reasons, limits := problemConfidence(b.summary, statement.Overall.Calls, 5, true)
		limits = append(limits,
			"SQL приводится к безопасному шаблону без значений параметров; динамически собранный текст может быть недоступен.",
			"Быстрые повторы указывают на возможное дублирование или N+1, но не доказывают его без связи с вызывающим сценарием.",
			"По журналу вызовов нельзя надёжно определить неиспользуемые строки и таблицы: для этого нужны снимки схемы, размеров и обращений за длительный период.",
		)
		if statement.Telemetry.PhaseMeasuredCalls == 0 {
			limits = append(limits, "Общую длительность нельзя разделить на ожидание соединения, блокировку, выполнение и чтение результата без измерений этих фаз адаптером приложения.")
		}
		if statement.Overall.QuantilesApproximated || statement.Main.QuantilesApproximated ||
			statement.Background.QuantilesApproximated {
			limits = append(
				limits,
				"После 128 выполнений квантили рассчитываются потоковым детерминированным алгоритмом с ограниченной памятью; максимум и количество остаются точными.",
			)
		}

		signals := make([]string, 0, 5)
		if assessment.MainThreadSlow {
			signals = append(signals, "SQL-вызов занимает бюджет кадра на главном потоке")
		}
		if assessment.BackgroundSlow {
			signals = append(signals, "SQL-вызов медленно выполняется в фоне")
		}
		if assessment.Storm {
			signals = append(signals, "зафиксирован всплеск частоты")
		}
		if assessment.Repeated {
			signals = append(signals, "SQL-вызов быстро повторяется")
		}
		if assessment.Failures {
			failureKinds := databaseFailureKindSummary(statement.Telemetry.FailureKinds)
			if failureKinds == "" {
				signals = append(signals, "есть неуспешные выполнения")
			} else {
				signals = append(signals, "есть неуспешные выполнения: "+failureKinds)
			}
		}

		evidence := []ProblemEvidence{
			{Name: "Число вызовов БД", Observed: callText, Unit: "events", Sample: u64ptr(statement.Overall.Calls), Source: "typed_database"},
			{Name: medianDurationLabel, Observed: fmt.Sprint(p50MS), Unit: "ms", Source: "typed_database"},
			{Name: upperFiveDurationLabel, Observed: fmt.Sprint(p95MS), Unit: "ms", ExpectedOrThreshold: fmt.Sprintf("< %d ms в фоне; < %d ms на главном потоке", b.cfg.DatabaseBackgroundMS, b.cfg.DatabaseMainThreadMS), Source: "typed_database"},
			{Name: "Максимальная длительность", Observed: fmt.Sprint(maxMS), Unit: "ms", Source: "typed_database"},
			{Name: "На главном потоке", Observed: fmt.Sprint(statement.Main.Calls), Unit: "events", Denominator: u64ptr(statement.Overall.Calls), Source: "typed_database"},
			{Name: "Пиковая частота", Observed: fmt.Sprint(statement.PeakCallsPerSecond), Unit: "events/s", ExpectedOrThreshold: fmt.Sprintf("< %d events/s", b.cfg.DatabaseStormRate), Source: "typed_database"},
			{Name: "Быстрые повторы", Observed: fmt.Sprint(statement.RapidRepeats), Unit: "events", ExpectedOrThreshold: fmt.Sprintf("< %d", b.cfg.DatabaseRapidRepeatCount), Source: "typed_database"},
			{Name: "Ошибки", Observed: fmt.Sprint(statement.Overall.Failures), Unit: "events", Denominator: u64ptr(statement.Overall.Calls), Source: "typed_database"},
		}
		if callEstimate > statement.Overall.Calls {
			evidence = append(evidence, ProblemEvidence{
				Name: "Оценка погрешности частоты", Observed: fmt.Sprint(statement.FrequencyEstimateError),
				Unit: "events", Source: "typed_database_count_min_sketch",
			})
		}
		if failureKinds := databaseFailureKindSummary(statement.Telemetry.FailureKinds); failureKinds != "" {
			evidence = append(evidence, ProblemEvidence{
				Name: "Классы ошибок", Observed: failureKinds, Sample: u64ptr(statement.Overall.Failures),
				Source: "typed_database_failure_taxonomy",
			})
		}
		if statement.Telemetry.ResultKnownCalls > 0 {
			evidence = append(evidence, ProblemEvidence{
				Name: "Вызовы с известным результатом", Observed: fmt.Sprintf("%d / %d", statement.Telemetry.ResultKnownCalls, statement.Overall.Calls),
				Sample: u64ptr(statement.Telemetry.ResultKnownCalls), Denominator: u64ptr(statement.Overall.Calls),
				Source: "typed_database_result",
			})
		}
		evidence = append(evidence, databasePhaseEvidence(statement.Telemetry)...)
		if assessment.MainThreadSlow {
			evidence = append(evidence, databaseThreadDurationEvidence(
				"На главном потоке", statement.Main, assessment.MainDurationUS,
				assessment.MainUsesMaximum, b.cfg.DatabaseMainThreadMS,
			))
		}
		if assessment.BackgroundSlow {
			evidence = append(evidence, databaseThreadDurationEvidence(
				"В фоне", statement.Background, assessment.BackgroundDurationUS,
				assessment.BackgroundUsesMaximum, b.cfg.DatabaseBackgroundMS,
			))
		}
		if linkedToUI {
			evidence = append(evidence, ProblemEvidence{
				Name:     "Пересечения SQL на главном потоке с проблемным окном UI",
				Observed: fmt.Sprint(statement.MainCorrelation.UIWindowOverlaps), Unit: "events",
				Numerator:   u64ptr(statement.MainCorrelation.UIJankyFrames),
				Denominator: u64ptr(statement.MainCorrelation.UIFrames), Source: "typed_database_ui_interval_join",
			})
		}
		if databaseHasRelatedCorrelation(statement.MainCorrelation) ||
			databaseHasRelatedCorrelation(statement.BackgroundCorrelation) {
			evidence = append(evidence, ProblemEvidence{
				Name: "Подсистемы, работавшие в тот же интервал",
				Observed: databaseRelatedThreadCorrelationSummary(
					statement.MainCorrelation,
					statement.BackgroundCorrelation,
				),
				Source: "typed_database_related_interval_join",
			})
			limits = append(limits, "Совпадения с HTTP, фоновыми задачами, файловыми операциями и GC показывают только одновременную работу в том же процессе и сценарии, но не доказывают причину задержки.")
		}
		impact := 18
		if assessment.MainThreadSlow {
			impact = 34
		}
		magnitude := 6
		if assessment.MainThreadSlow {
			mainMS := microsecondsToMillisecondsCeil(assessment.MainDurationUS)
			magnitude = max(magnitude, min(25, 8+int(mainMS/maxUint64(b.cfg.DatabaseMainThreadMS, 1))*4))
		}
		if assessment.BackgroundSlow {
			backgroundMS := microsecondsToMillisecondsCeil(assessment.BackgroundDurationUS)
			magnitude = max(magnitude, min(25, 8+int(backgroundMS/maxUint64(b.cfg.DatabaseBackgroundMS, 1))*4))
		}
		if assessment.Failures {
			magnitude = max(magnitude, min(25, 8+int(assessment.FailureRate/b.cfg.DatabaseFailureRate)*4))
		}
		exposure := min(20, 4+int(math.Log2(float64(callEstimate)+1))*3)
		if assessment.Storm {
			exposure = max(exposure, min(20, 8+int(statement.PeakCallsPerSecond/b.cfg.DatabaseStormRate)*3))
		}

		why := "Событие БД напрямую связывает шаблон SQL, место вызова, поток, длительность и результат выполнения."
		if linkedToUI {
			why = "Выполнение SQL на главном потоке пересеклось с проблемным окном UI в том же процессе, запуске приложения, операции и экране. SQL занимал главный поток во время наблюдаемого ухудшения."
		}
		b.add(ProblemFinding{
			DetectorID: "io.database_calls", DetectorVersion: b.cfg.Version,
			Category: ProblemCategoryIO, Subcategory: databaseFindingSubcategory(assessment, linkedToUI), Status: "observed",
			Confidence: confidence, ConfidenceReasons: reasons,
			Title:             fmt.Sprintf("Проблемный SQL-вызов в %s", displayUnknown(owner, "неизвестном месте")),
			WhatHappened:      fmt.Sprintf("%s: %s вызовов, граница верхних 5%% длительностей %d мс, максимум %d мс, %d вызовов на главном потоке, пик %d/с. %s.", displayUnknown(statement.Query, "SQL-текст не определён"), callText, p95MS, maxMS, statement.Main.Calls, statement.PeakCallsPerSecond, strings.Join(signals, "; ")),
			Where:             where,
			Why:               ProblemWhy{ClaimLevel: "linked", Summary: why},
			Impact:            []string{"Задержка интерфейса, лишняя нагрузка на хранилище и рост времени пользовательского сценария"},
			Evidence:          evidence,
			Frequency:         &ProblemFrequency{Count: callEstimate, RatePerSec: ratePerSecond(callEstimate, b.summary.DurationMS)},
			Cost:              &ProblemCost{WallTimeMS: nonZeroU64Ptr(totalMS)},
			PriorityBreakdown: priority(impact, magnitude, exposure, locationBreadth(where), boolScore(databaseAssessmentSignalCount(assessment) > 1, 5), "влияние БД на интерфейс и сценарий", "длительность и ошибки", "частота и повторы", "место SQL-вызова", "несколько независимых признаков"),
			Recommendations:   databaseRecommendations(statement, assessment),
			Limitations:       limits,
			Drilldowns:        []ProblemDrilldown{{Label: "База данных", Anchor: "database-analysis", Filter: firstKnown(statement.Query, owner)}},
		})
	}
}

func (b *problemBuilder) detectDatabasePlanEvidence() {
	analysis := b.summary.DatabaseAnalysis
	if analysis == nil || analysis.Evidence == nil {
		return
	}
	for _, plan := range analysis.Evidence.Findings {
		where := []ProblemLocation{{Owner: plan.Table, Operation: plan.Operation}}
		if plan.Table == "" {
			where[0].Owner = "план SQL-запроса"
		}
		b.add(ProblemFinding{
			DetectorID: "io.database_plan_evidence", DetectorVersion: b.cfg.Version,
			Category: ProblemCategoryIO, Subcategory: "database_plan_" + plan.Kind,
			Status: "observed", Confidence: "medium",
			ConfidenceReasons: []string{
				"шаг плана SQL-запроса импортирован как заранее очищенный структурированный артефакт",
				"отпечаток и операция совпали с наблюдаемым SQL-шаблоном",
			},
			Title:        databasePlanFindingTitle(plan),
			WhatHappened: plan.Summary,
			Where:        where,
			Why: ProblemWhy{
				ClaimLevel: "linked",
				Summary:    "Вывод основан на явно импортированном шаге плана SQL-запроса, а не на оценке задержки по журналу.",
			},
			Impact: []string{"Лишняя работа SQLite возможна, если доступ или временная структура обрабатывают большой объём данных"},
			Evidence: []ProblemEvidence{{
				Name: "Шаг плана SQL-запроса", Observed: databasePlanFindingEvidence(plan),
				Source: "developer_database_evidence",
			}},
			Frequency: &ProblemFrequency{Count: 1},
			PriorityBreakdown: priority(
				18, 12, 6, locationBreadth(where), 2,
				"потенциальная стоимость доступа", "тип шага плана", "один импортированный план",
				"таблица и операция", "точное совпадение отпечатка",
			),
			Recommendations: []ProblemRecommendation{{
				Action:       plan.Action,
				Rationale:    "SCAN или временное B-дерево наблюдаемы в приложенном плане, но сами по себе не доказывают, что новый индекс будет быстрее с учётом селективности и стоимости записи.",
				Verification: "Повторно экспортируйте план и выполните тот же сценарий; сравните шаги плана, задержку БД и корректность результата на репрезентативных данных.",
			}},
			Limitations: []string{
				"Артефакт предоставлен разработчиком отдельно; анализатор проверяет формат и соответствие отпечатка, но не актуальность схемы или набора данных.",
				"Наличие SCAN не означает автоматически отсутствующий индекс или плохой план.",
			},
			Drilldowns: []ProblemDrilldown{{Label: "База данных", Anchor: "database-analysis", Filter: plan.Query}},
		})
	}
}

func databasePlanFindingTitle(plan DatabasePlanFinding) string {
	switch plan.Kind {
	case "scan":
		return "План SQL-запроса содержит SCAN"
	case "temp_btree":
		return "План SQL-запроса использует временное B-дерево"
	case "automatic_index":
		return "План SQL-запроса использует автоматически созданный индекс"
	default:
		return "План SQL-запроса требует проверки"
	}
}

func databasePlanFindingEvidence(plan DatabasePlanFinding) string {
	parts := []string{plan.Kind}
	if plan.Table != "" {
		parts = append(parts, "table="+plan.Table)
	}
	if plan.Index != "" {
		parts = append(parts, "index="+plan.Index)
	}
	if plan.Purpose != "" {
		parts = append(parts, "purpose="+plan.Purpose)
	}
	return strings.Join(parts, ", ")
}

func (b *problemBuilder) detectDatabaseScenarios() {
	analysis := b.summary.DatabaseAnalysis
	if analysis == nil {
		return
	}
	for _, scenario := range analysis.Scenarios.Candidates {
		where := []ProblemLocation{{
			Screen: scenario.Screen, Operation: scenario.ContextOperation,
			Owner: scenario.Source, Method: scenario.Source,
		}}
		confidence, reasons, limits := problemConfidence(
			b.summary, scenario.AffectedScopes, 2, true,
		)
		limits = append(limits,
			"Нормализованный SQL-шаблон не содержит значений параметров: он подтверждает повтор формы запроса, но не отличает N+1 от полного дублирования.",
			"Пакетная обработка, объединение запросов и кэширование — варианты для проверки, а не автоматический рецепт без знания семантики данных.",
			fmt.Sprintf(
				"Число вызовов рассчитано приближённо; подробно сохранено %d замеров, максимальная погрешность оценки частоты — %s.",
				scenario.RetainedCalls,
				russianCountUint64(analysis.Scenarios.FrequencyEstimateError, "вызов", "вызова", "вызовов"),
			),
		)
		if scenario.CostLowerBound {
			limits = append(limits, "Часть ранних замеров не сохранена; число вызовов оценено приближённо, а показанное время является нижней границей.")
		}
		if analysis.Scenarios.ScopeCountsApproximated {
			limits = append(limits, "После превышения точного предела число границ сценария рассчитано приближённо.")
		}
		title := "Повторные SQL-вызовы внутри одного сценария"
		if scenario.Kind == "batch_candidate" {
			title = "Кандидат на пакетную запись БД"
		}
		claim := scenario.ClaimLevel
		if claim == "" {
			claim = "hypothesis"
		}
		impact := 18
		if scenario.MainThreadCalls > 0 {
			impact = 30
		}
		magnitude := min(25, 5+int(math.Log2(float64(scenario.MaxCallsPerScope)+1))*4)
		exposure := min(20, 4+int(math.Log2(float64(scenario.AffectedScopes)+1))*4)
		evidence := []ProblemEvidence{
			{Name: "Примерное число повторных вызовов", Observed: fmt.Sprint(scenario.EstimatedCalls), Unit: "calls", Sample: u64ptr(scenario.RetainedCalls), Source: "typed_database_scenario_count_min_sketch"},
			{Name: "Максимум вызовов в одной границе сценария", Observed: fmt.Sprint(scenario.MaxCallsPerScope), Unit: "calls", ExpectedOrThreshold: fmt.Sprintf("< %d", databaseScenarioRepeatCalls), Source: "typed_database_scenario"},
			{Name: "Границы сценария с повторами / все наблюдаемые", Observed: fmt.Sprintf("%d / %d", scenario.AffectedScopes, scenario.ObservedScopes), Numerator: u64ptr(scenario.AffectedScopes), Denominator: u64ptr(scenario.ObservedScopes), Source: "typed_database_scenario"},
			{Name: "Вызовов на одну наблюдаемую границу", Observed: fmt.Sprintf("%.2f", scenario.CallsPerObservedScope), Unit: "calls/boundary", Source: "typed_database_scenario"},
			{Name: "Суммарное время", Observed: fmt.Sprint(microsecondsToMillisecondsCeil(scenario.TotalDurationUS)), Unit: "ms", Source: "typed_database_scenario"},
			{Name: "На главном потоке", Observed: fmt.Sprint(scenario.MainThreadCalls), Unit: "events", Denominator: u64ptr(scenario.EstimatedCalls), Source: "typed_database_scenario"},
		}
		whatHappened, factors := databaseScenarioExplanation(scenario)
		b.add(ProblemFinding{
			DetectorID: "io.database_repeated_in_scope", DetectorVersion: b.cfg.Version,
			Category: ProblemCategoryIO, Subcategory: scenario.Kind, Status: "observed",
			Confidence: confidence, ConfidenceReasons: reasons,
			Title:        title,
			WhatHappened: whatHappened,
			Where:        where,
			Why: ProblemWhy{
				ClaimLevel: claim,
				Summary:    "Один нормализованный SQL-шаблон много раз вызван внутри одной границы сценария. Это подтверждает серию повторов, но без значений параметров нельзя отличить N+1 от допустимых повторов.",
				Factors:    factors,
			},
			Impact:    []string{"Дополнительные обращения к SQLite увеличивают время сценария; вызовы на главном потоке могут задерживать интерфейс"},
			Evidence:  evidence,
			Frequency: &ProblemFrequency{Count: scenario.EstimatedCalls},
			Cost:      &ProblemCost{WallTimeMS: nonZeroU64Ptr(microsecondsToMillisecondsCeil(scenario.TotalDurationUS))},
			PriorityBreakdown: priority(
				impact, magnitude, exposure, locationBreadth(where), boolScore(scenario.Failures > 0, 5),
				"стоимость повторов в сценарии", "число повторов в одной границе", "доля затронутых границ сценария",
				"источник и логическая операция", "ошибки среди повторов",
			),
			Recommendations: []ProblemRecommendation{{
				Action:       "Откройте указанное место вызова и проверьте, можно ли заменить повторы пакетной записью, одним SQL-запросом или кэшем.",
				Rationale:    "Форма SQL-вызова и его суммарное время известны, но значения параметров намеренно не записываются.",
				Verification: "Повторите тот же сценарий: результат должен сохраниться, а максимум вызовов в одной границе и суммарное время БД — уменьшиться.",
			}},
			Limitations: limits,
			Drilldowns: []ProblemDrilldown{{
				Label: "База данных", Anchor: "database-analysis",
				Filter: firstKnown(scenario.Query, scenario.Source),
			}},
		})
	}
}

func databaseScenarioExplanation(scenario DatabaseScenarioStats) (string, []string) {
	boundary := "одной операции приложения"
	affected := fmt.Sprintf("%d операциях приложения", scenario.AffectedScopes)
	if scenario.ScopeKind == "transaction" {
		boundary = "одной транзакции"
		affected = fmt.Sprintf("%d транзакциях", scenario.AffectedScopes)
	}
	location := displayUnknown(scenario.Source, "месте вызова, которое не попало в журнал")
	query := strings.TrimSpace(scenario.Query)
	identity := fmt.Sprintf("Вызов БД из %s", location)
	factors := make([]string, 0, 1)
	if !datavalue.IsUnknown(query) {
		identity = fmt.Sprintf("Вызов БД с SQL-шаблоном «%s»", query)
	} else {
		factors = append(factors, fmt.Sprintf(
			"SQL-шаблон не записан. Проблема локализована по месту вызова %s; откройте этот DAO или метод и проверьте выполняемый им SQL.",
			location,
		))
	}
	return fmt.Sprintf(
		"%s повторялся до %d раз в %s; примерно %d вызовов в %s из %d наблюдаемых, суммарно не менее %d мс.",
		identity,
		scenario.MaxCallsPerScope,
		boundary,
		scenario.EstimatedCalls,
		affected,
		scenario.ObservedScopes,
		microsecondsToMillisecondsCeil(scenario.TotalDurationUS),
	), factors
}

func (b *problemBuilder) detectDatabaseTransactions() {
	analysis := b.summary.DatabaseAnalysis
	if analysis == nil || analysis.Transactions == nil {
		return
	}
	for _, transaction := range analysis.Transactions.Transactions {
		assessment := AssessDatabaseTransaction(transaction, b.cfg)
		if !assessment.IsProblem() {
			continue
		}
		where := []ProblemLocation{{
			Process: transaction.Process, Screen: transaction.Screen,
			Operation: transaction.ContextOperation,
			Owner:     transaction.Source, Method: transaction.Source,
		}}
		confidence, reasons, limits := problemConfidence(b.summary, 1, 1, true)
		limits = append(limits,
			"Общая длительность описывает всю транзакцию; ожидание соединения, блокировку, выполнение и чтение результата нельзя разделить без измерений этих фаз интеграционным адаптером.",
		)
		outcome := databaseTransactionOutcomeExplanation(transaction)
		duration := "длительность не вычисляется: событие завершения отсутствует"
		if transaction.Complete {
			duration = fmt.Sprintf("длительность %d мс", microsecondsToMillisecondsCeil(transaction.DurationUS))
		}
		signals := databaseTransactionSignals(assessment)
		evidence := []ProblemEvidence{
			{Name: "Число SQL-вызовов", Observed: fmt.Sprint(transaction.StatementCount), Unit: "events", ExpectedOrThreshold: fmt.Sprintf("< %d", b.cfg.DatabaseTransactionStatements), Source: "typed_database_transaction"},
			{Name: "Чтения / записи", Observed: fmt.Sprintf("%d / %d", transaction.ReadCount, transaction.WriteCount), Unit: "events", Source: "typed_database_transaction"},
		}
		if transaction.Complete {
			evidence = append(evidence, ProblemEvidence{
				Name: "Длительность транзакции", Observed: fmt.Sprint(microsecondsToMillisecondsCeil(transaction.DurationUS)),
				Unit: "ms", ExpectedOrThreshold: fmt.Sprintf("< %d мс на главном потоке; < %d мс в фоне", b.cfg.DatabaseTransactionMainMS, b.cfg.DatabaseTransactionBackgroundMS),
				Source: "typed_database_transaction",
			})
		} else {
			evidence = append(evidence, ProblemEvidence{
				Name: "Полнота жизненного цикла", Observed: "начало без завершения", Source: "typed_database_transaction",
			})
		}
		if databaseHasRelatedCorrelation(transaction.Correlation) {
			evidence = append(evidence, ProblemEvidence{
				Name:     "Связанные подсистемы в границе транзакции",
				Observed: databaseRelatedCorrelationSummary(transaction.Correlation),
				Source:   "typed_database_transaction_related_interval_join",
			})
			limits = append(limits, "Пересечения с HTTP, фоновыми задачами, файловыми операциями и GC показывают только совпадение по времени и не доказывают конкуренцию за блокировку или причинность.")
		}
		impact := 20
		if assessment.MainThreadSlow || assessment.Failure {
			impact = 34
		} else if assessment.Rollback || assessment.Incomplete {
			impact = 26
		}
		magnitude := 8
		if assessment.MainThreadSlow {
			magnitude = min(25, 10+int(transaction.DurationUS/1_000/b.cfg.DatabaseTransactionMainMS)*3)
		} else if assessment.BackgroundSlow {
			magnitude = min(25, 8+int(transaction.DurationUS/1_000/b.cfg.DatabaseTransactionBackgroundMS)*3)
		}
		if assessment.ManyStatements {
			magnitude = max(magnitude, min(25, 8+int(transaction.StatementCount/b.cfg.DatabaseTransactionStatements)*3))
		}
		b.add(ProblemFinding{
			DetectorID: "io.database_transaction", DetectorVersion: b.cfg.Version,
			Category: ProblemCategoryIO, Subcategory: databaseTransactionSubcategory(assessment),
			Status: "observed", Confidence: confidence, ConfidenceReasons: reasons,
			Title: fmt.Sprintf("Проблемная транзакция БД в %s", displayUnknown(transaction.Source, "неизвестном месте")),
			WhatHappened: fmt.Sprintf(
				"Транзакция #%d: %s, %s, %d SQL-вызовов (чтение %d / запись %d); %s.",
				transaction.TransactionID, outcome, duration, transaction.StatementCount,
				transaction.ReadCount, transaction.WriteCount, strings.Join(signals, "; "),
			),
			Where: where,
			Why: ProblemWhy{
				ClaimLevel: "linked",
				Summary:    "События жизненного цикла транзакции напрямую связывают результат, длительность и число SQL-вызовов с этим местом в коде и контекстом. Без измерений отдельных фаз нельзя объяснить, из чего сложилась общая длительность.",
			},
			Impact:   []string{"Долгая или незавершённая атомарная работа увеличивает задержку сценария и дольше удерживает ресурсы БД"},
			Evidence: evidence, Frequency: &ProblemFrequency{Count: 1},
			Cost: &ProblemCost{WallTimeMS: transactionWallTimeMS(transaction)},
			PriorityBreakdown: priority(
				impact, magnitude, 8, locationBreadth(where),
				boolScore(databaseTransactionSignalCount(assessment) > 1, 5),
				"влияние транзакции на сценарий", "длительность, результат и число SQL-вызовов", "наблюдаемый экземпляр",
				"место в коде и контекст", "несколько независимых признаков",
			),
			Recommendations: databaseTransactionRecommendations(assessment, transaction),
			Limitations:     limits,
			Drilldowns:      []ProblemDrilldown{{Label: "Транзакции БД", Anchor: "database-analysis", Filter: transaction.Source}},
		})
	}
}

func databaseTransactionOutcomeExplanation(transaction DatabaseTransactionStats) string {
	switch transaction.Outcome {
	case "failure":
		return "ошибка (" + databaseFailureDisplayName(transaction.FailureKind) + ")"
	case "rollback":
		return "откат"
	case "success":
		return "успешное завершение"
	case "incomplete":
		return "начало без завершения"
	default:
		return transaction.Outcome
	}
}

func databaseTransactionSignals(assessment DatabaseTransactionAssessment) []string {
	result := make([]string, 0, 7)
	if assessment.MainThreadSlow {
		result = append(result, "долгая работа на главном потоке")
	}
	if assessment.BackgroundSlow {
		result = append(result, "долгая фоновая работа")
	}
	if assessment.Failure {
		result = append(result, "неуспешное завершение")
	}
	if assessment.Rollback {
		result = append(result, "явный откат")
	}
	if assessment.ManyStatements {
		result = append(result, "много SQL-вызовов")
	}
	if assessment.Incomplete {
		result = append(result, "жизненный цикл не завершён")
	}
	if assessment.Nested {
		result = append(result, "есть родительская транзакция")
	}
	return result
}

func databaseTransactionSubcategory(assessment DatabaseTransactionAssessment) string {
	switch {
	case assessment.Failure:
		return "database_transaction_failure"
	case assessment.Incomplete:
		return "database_transaction_incomplete"
	case assessment.Rollback:
		return "database_transaction_rollback"
	case assessment.MainThreadSlow:
		return "database_transaction_main_thread"
	case assessment.ManyStatements:
		return "database_transaction_many_statements"
	case assessment.BackgroundSlow:
		return "database_transaction_long"
	default:
		return "database_transaction_nested"
	}
}

func databaseTransactionSignalCount(assessment DatabaseTransactionAssessment) int {
	return boolCount(
		assessment.MainThreadSlow, assessment.BackgroundSlow, assessment.Failure,
		assessment.Rollback, assessment.ManyStatements, assessment.Incomplete, assessment.Nested,
	)
}

func databaseTransactionRecommendations(
	assessment DatabaseTransactionAssessment,
	transaction DatabaseTransactionStats,
) []ProblemRecommendation {
	result := make([]ProblemRecommendation, 0, 4)
	if assessment.MainThreadSlow {
		result = append(result, ProblemRecommendation{
			Action:       "Перенести транзакцию с главного потока и сократить её синхронную границу",
			Rationale:    "Событие завершения подтверждает превышение бюджета кадра именно на главном потоке.",
			Verification: "Повторить операцию: долгих транзакций на главном потоке не должно остаться; длительность и самые медленные кадры UI оцениваются отдельно.",
		})
	}
	if assessment.Failure || assessment.Rollback {
		result = append(result, ProblemRecommendation{
			Action:       "Проверить ветку завершения и правила повторов или отката в указанном месте кода",
			Rationale:    "Жизненный цикл точно фиксирует результат и безопасный класс ошибки, но не хранит текст ошибки и значения параметров SQL.",
			Verification: "Повторить те же входные условия и подтвердить успешное завершение без роста повторов.",
		})
	}
	if assessment.ManyStatements {
		result = append(result, ProblemRecommendation{
			Action:       "Проверить повторные вызовы внутри транзакции и возможность пакетной записи, вставки с обновлением или объединённого запроса",
			Rationale:    fmt.Sprintf("В одной транзакции выполнено %d SQL-вызовов; это наблюдаемый объём, но не автоматический диагноз N+1.", transaction.StatementCount),
			Verification: "В том же сценарии уменьшились число SQL-вызовов, операций чтения и записи и длительность при неизменном результате.",
		})
	}
	if assessment.Incomplete {
		result = append(result, ProblemRecommendation{
			Action:       "Проверить все пути выхода и запись события завершения",
			Rationale:    "Начало зафиксировано, но длительность и результат нельзя восстановить без события завершения.",
			Verification: "В повторном прогоне у каждой начатой транзакции есть событие завершения.",
		})
	}
	if assessment.Nested {
		result = append(result, ProblemRecommendation{
			Action:       "Проверить необходимость родительской и дочерней транзакций и фактическую семантику вложенности адаптера",
			Rationale:    "Идентификатор родителя подтверждает вложенный жизненный цикл, но сам по себе не доказывает ошибку.",
			Verification: "Сопоставить число и результаты родительских и дочерних транзакций; после упрощения вложенность исчезает без нарушения атомарности.",
		})
	}
	if len(result) == 0 && assessment.BackgroundSlow {
		result = append(result, ProblemRecommendation{
			Action:       "Сократить объём работы внутри транзакции и измерить её фазы через доверенный адаптер",
			Rationale:    "Общая длительность превышает фоновый порог, но без фаз нельзя выбрать причину задержки.",
			Verification: "Повторить сценарий и сравнить длительность и число SQL-вызовов; выводы по фазам делать только при наличии их измерений.",
		})
	}
	return result
}

func transactionWallTimeMS(transaction DatabaseTransactionStats) *uint64 {
	if !transaction.Complete {
		return nil
	}
	return nonZeroU64Ptr(microsecondsToMillisecondsCeil(transaction.DurationUS))
}

func databaseProblemLocations(contexts []DatabaseStatementContextStats, preferLinkedUI bool) []ProblemLocation {
	if len(contexts) == 0 {
		return []ProblemLocation{{Owner: "unknown", Method: "unknown"}}
	}
	locations := make([]ProblemLocation, 0, min(len(contexts), databaseProblemLocationLimit))
	for pass := 0; pass < 2 && len(locations) < databaseProblemLocationLimit; pass++ {
		for _, context := range contexts {
			linked := context.MainCorrelation.UIWindowOverlaps > 0
			if preferLinkedUI && linked != (pass == 0) || !preferLinkedUI && pass > 0 {
				continue
			}
			locations = appendUniqueLocation(locations, ProblemLocation{
				Process: context.Process, Screen: context.Screen, Operation: context.ContextOperation,
				Owner: firstKnown(context.Source, context.ContextOwner), Method: context.Source,
			})
			if len(locations) == databaseProblemLocationLimit {
				break
			}
		}
	}
	return locations
}

const databaseProblemLocationLimit = 8

func databaseThreadDurationEvidence(
	thread string,
	stats DatabaseExecutionStats,
	durationUS uint64,
	usesMaximum bool,
	thresholdMS uint64,
) ProblemEvidence {
	metric := "верхние 5%"
	if usesMaximum {
		metric = "максимум (малая выборка)"
	}
	return ProblemEvidence{
		Name:     thread + ": " + metric,
		Observed: fmt.Sprint(microsecondsToMillisecondsCeil(durationUS)), Unit: "ms",
		ExpectedOrThreshold: fmt.Sprintf("< %d ms", thresholdMS), Sample: u64ptr(stats.Calls),
		Source: "typed_database",
	}
}

func databaseFindingSubcategory(assessment DatabaseStatementAssessment, linkedToUI bool) string {
	switch {
	case assessment.MainThreadSlow && linkedToUI:
		return "database_main_thread_ui_linked"
	case assessment.MainThreadSlow:
		return "database_main_thread_slow"
	case assessment.BackgroundSlow:
		return "database_background_slow"
	case assessment.Failures:
		return "database_failures"
	case assessment.Storm:
		return "database_storm"
	case assessment.Repeated:
		return "database_repeated"
	default:
		return "database_call"
	}
}

func databaseAssessmentSignalCount(assessment DatabaseStatementAssessment) int {
	return boolCount(
		assessment.MainThreadSlow,
		assessment.BackgroundSlow,
		assessment.Storm,
		assessment.Repeated,
		assessment.Failures,
	)
}

func databaseRecommendations(
	statement DatabaseStatementStats,
	assessment DatabaseStatementAssessment,
) []ProblemRecommendation {
	recommendations := make([]ProblemRecommendation, 0, 3)
	if assessment.MainThreadSlow {
		recommendations = append(recommendations, ProblemRecommendation{
			Action:       "Перенести вызов БД с главного потока и повторить тот же сценарий",
			Rationale:    "Thread-specific измерение подтверждает превышение бюджета кадра именно на главном потоке.",
			Verification: "На главном потоке остаётся 0 медленных вызовов; фоновые вызовы оцениваются отдельно.",
		})
	}
	if assessment.BackgroundSlow {
		if databaseOperationIsRead(statement.Operation) {
			recommendations = append(recommendations, ProblemRecommendation{
				Action:       "Снять EXPLAIN QUERY PLAN для этого шаблона на репрезентативной схеме",
				Rationale:    "Фоновое чтение превышает порог, но журнал без плана не различает полное сканирование, блокировку и стоимость чтения результата.",
				Verification: "После адресного изменения повторить сценарий и сравнить границу верхних 5% фоновых длительностей при том же числе вызовов.",
			})
		} else {
			recommendations = append(recommendations, ProblemRecommendation{
				Action:       "Проверить размер пакета, границы транзакции и конкуренцию записей в этом сценарии",
				Rationale:    "Фоновая запись превышает порог; план SQL-запроса и объём выборки не объясняют стоимость записи без дополнительных данных.",
				Verification: "Повторить тот же сценарий и сравнить верхние 5% и максимум фоновых длительностей, число вызовов на транзакцию и измеренные фазы ожидания.",
			})
		}
	}
	if assessment.Storm || assessment.Repeated {
		recommendations = append(recommendations, ProblemRecommendation{
			Action:       "Проверить вызывающий сценарий на дублирование, N+1 и возможность пакетной обработки или кэширования",
			Rationale:    "Наблюдается высокая частота или серия близких одинаковых шаблонов; это гипотеза, а не доказанный N+1.",
			Verification: "Сравнить число вызовов, быстрых повторов и пик/с на той же операции.",
		})
	}
	if assessment.Failures {
		failureKinds := databaseFailureKindSummary(statement.Telemetry.FailureKinds)
		rationale := "События БД подтверждают ошибки; текст ошибки, стек и значения параметров SQL намеренно не сохраняются."
		if failureKinds != "" {
			rationale = "События БД подтверждают безопасные классы ошибок: " + failureKinds + ". Текст ошибки, стек и значения параметров SQL не сохраняются."
		}
		recommendations = append(recommendations, ProblemRecommendation{
			Action:       "Локализовать неуспешное завершение в указанном месте кода и воспроизвести входные условия",
			Rationale:    rationale,
			Verification: "Повторный прогон не содержит ошибок этого SQL-шаблона.",
		})
	}
	return recommendations
}

func databaseOperationIsRead(operation string) bool {
	switch strings.ToLower(strings.TrimSpace(operation)) {
	case "query", "read", "select", "чтение":
		return true
	default:
		return false
	}
}

func databaseFailureKindSummary(values []NamedValue) string {
	if len(values) == 0 {
		return ""
	}
	var result strings.Builder
	for index, value := range values {
		if index > 0 {
			result.WriteString(", ")
		}
		result.WriteString(databaseFailureDisplayName(value.Name))
		result.WriteString(": ")
		result.WriteString(fmt.Sprint(value.Value))
	}
	return result.String()
}

func databaseHasRelatedCorrelation(stats DatabaseCorrelationStats) bool {
	return stats.HTTPOverlaps > 0 || stats.WorkerOverlaps > 0 ||
		stats.FileIOOverlaps > 0 || stats.GCOverlaps > 0
}

func databaseRelatedCorrelationSummary(stats DatabaseCorrelationStats) string {
	return fmt.Sprintf(
		"HTTP %d; фоновые задачи %d; файловые операции %d; GC %d",
		stats.HTTPOverlaps,
		stats.WorkerOverlaps,
		stats.FileIOOverlaps,
		stats.GCOverlaps,
	)
}

func databaseRelatedThreadCorrelationSummary(
	main, background DatabaseCorrelationStats,
) string {
	return fmt.Sprintf(
		"главный поток: %s; фон: %s",
		databaseRelatedCorrelationSummary(main),
		databaseRelatedCorrelationSummary(background),
	)
}

func databasePhaseEvidence(telemetry DatabaseTelemetryStats) []ProblemEvidence {
	result := make([]ProblemEvidence, 0, 4)
	for _, phase := range [...]struct {
		name  string
		stats DatabasePhaseStats
	}{
		{"Ожидание соединения: верхние 5%", telemetry.PoolWait},
		{"Ожидание блокировки: верхние 5%", telemetry.LockWait},
		{"Выполнение: верхние 5%", telemetry.Execute},
		{"Чтение результата: верхние 5%", telemetry.Materialize},
	} {
		if phase.stats.Samples == 0 {
			continue
		}
		result = append(result, ProblemEvidence{
			Name: phase.name, Observed: fmt.Sprint(microsecondsToMillisecondsCeil(phase.stats.P95DurationUS)),
			Unit: "ms", Sample: u64ptr(phase.stats.Samples), Source: "typed_database_phase",
		})
	}
	return result
}
