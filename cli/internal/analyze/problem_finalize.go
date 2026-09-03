package analyze

import (
	"fmt"
	"sort"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func (b *problemBuilder) add(f ProblemFinding) {
	f.InvestigationPriority = investigationPriority(f.PriorityBreakdown)
	f.Severity = severityForPriority(f.InvestigationPriority)
	f.Fingerprint = findingFingerprint(f)
	f.ID = "problem-" + f.Fingerprint[:16]
	if f.Status == "" {
		f.Status = "observed"
	}
	b.findings = append(b.findings, f)
}

func (b *problemBuilder) finishFindings() {
	b.findings = uniqueProblemFindings(b.findings)
	for index := range b.findings {
		b.findings[index].RelatedCategories = uniqueStrings(append(
			b.findings[index].RelatedCategories,
			b.findings[index].Category,
		))
		sort.Strings(b.findings[index].RelatedCategories)
	}
	sort.Slice(b.findings, func(i, j int) bool {
		if problemSeverityRank(b.findings[i].Severity) != problemSeverityRank(b.findings[j].Severity) {
			return problemSeverityRank(b.findings[i].Severity) > problemSeverityRank(b.findings[j].Severity)
		}
		if b.findings[i].InvestigationPriority != b.findings[j].InvestigationPriority {
			return b.findings[i].InvestigationPriority > b.findings[j].InvestigationPriority
		}
		if problemConfidenceRank(b.findings[i].Confidence) != problemConfidenceRank(b.findings[j].Confidence) {
			return problemConfidenceRank(b.findings[i].Confidence) > problemConfidenceRank(b.findings[j].Confidence)
		}
		return b.findings[i].Fingerprint < b.findings[j].Fingerprint
	})
}

// uniqueProblemFindings is the last safety boundary for the report. A detector target has one
// fingerprint by design, so overlapping instrumentation must never make the complete problem
// section disappear because two observations resolved to that same target.
func uniqueProblemFindings(findings []ProblemFinding) []ProblemFinding {
	if len(findings) == 0 {
		return findings
	}
	result := make([]ProblemFinding, 0, len(findings))
	positions := make(map[string]int, len(findings))
	for _, finding := range findings {
		index, exists := positions[finding.Fingerprint]
		if !exists {
			positions[finding.Fingerprint] = len(result)
			result = append(result, finding)
			continue
		}
		if preferProblemFinding(finding, result[index]) {
			result[index] = finding
		}
	}
	return result
}

func preferProblemFinding(candidate, current ProblemFinding) bool {
	if candidate.InvestigationPriority != current.InvestigationPriority {
		return candidate.InvestigationPriority > current.InvestigationPriority
	}
	if problemConfidenceRank(candidate.Confidence) != problemConfidenceRank(current.Confidence) {
		return problemConfidenceRank(candidate.Confidence) > problemConfidenceRank(current.Confidence)
	}
	if len(candidate.Evidence) != len(current.Evidence) {
		return len(candidate.Evidence) > len(current.Evidence)
	}
	return candidate.Title < current.Title
}

func (b *problemBuilder) coverage() []CategoryCoverage {
	type coverageDefinition struct {
		id, label  string
		required   []string
		configured bool
		sufficient bool
		available  []string
		action     string
	}
	definitions := []coverageDefinition{
		{ProblemCategoryStability, "Стабильность", []string{"паузы главного потока", "причины завершения процессов"}, collectorConfigured(b.summary, jhlog.CollectorMainThreadStalls) || collectorConfigured(b.summary, jhlog.CollectorProcessExit) || b.summary.StallCount > 0 || len(b.summary.ProcessExits) > 0, true, []string{"паузы главного потока", "причины завершения процессов"}, "Включить сбор пауз и завершений процессов, затем записать активный пользовательский сценарий длительностью не менее 30 секунд."},
		{ProblemCategoryOperations, "Операции приложения", []string{"начало, завершение, бюджет и итог операций"}, b.summary.OperationAnalysis != nil, b.summary.OperationAnalysis != nil && b.summary.OperationAnalysis.Completed >= b.cfg.OperationMinSample && !operationAggregationLimited(b.summary.OperationAnalysis), []string{"типизированный жизненный цикл операций"}, "Добавить измеряемые операции вокруг важных действий, экранов и фоновых работ, затем собрать не менее 20 завершений."},
		{ProblemCategoryUI, "Интерфейс и главный поток", []string{"интервалы наблюдения за интерфейсом", "целевое время и источник кадров", "работа Compose или жизненный цикл экранов"}, collectorConfigured(b.summary, jhlog.CollectorFPS) || collectorConfigured(b.summary, jhlog.CollectorCompose) || b.summary.UIFrames > 0 || b.summary.StartupAnalysis != nil, b.summary.UIFrames >= b.cfg.UIMinFrames || b.summary.StartupAnalysis != nil, []string{"интервалы наблюдения за интерфейсом, границы Compose и жизненный цикл экранов"}, "Включить сбор JankStats и частоты кадров, затем записать не менее 120 кадров."},
		{ProblemCategoryNetwork, "Сеть", []string{"жизненный цикл HTTP и WebSocket"}, b.summary.HTTPCount > 0 || b.summary.WebSocketAnalysis != nil, uint64(b.summary.HTTPCount) >= b.cfg.HTTPMinSample || (b.summary.WebSocketAnalysis != nil && b.summary.WebSocketAnalysis.Opened >= 3), []string{"завершённые HTTP-вызовы и WebSocket-соединения"}, "Подключить jankhunter-okhttp3 и повторить сетевой сценарий."},
		{ProblemCategoryMemory, "Память и сборка мусора", []string{"снимки памяти", "счётчики сборки мусора и выделения памяти ART", "удержания объектов или снимок кучи HPROF"}, collectorConfigured(b.summary, jhlog.CollectorSystemSampler) || collectorConfigured(b.summary, jhlog.CollectorRetainedObjects) || b.summary.MemoryCount > 0 || len(b.summary.MemoryLeaks) > 0 || b.summary.GCAnalysis != nil, b.summary.MemoryCount >= 3 || len(b.summary.MemoryLeaks) > 0 || b.summary.GCAnalysis != nil, []string{"события памяти, сборки мусора и удержаний"}, "Включить сбор памяти; подозрение на утечку проверить с помощью снимка кучи HPROF."},
		{ProblemCategoryIO, "Файлы и база данных", []string{"файловые операции, SQL-вызовы SQLite/Room и вызовы DAO с привязкой к коду"}, collectorConfigured(b.summary, jhlog.CollectorIOTracing) || collectorConfigured(b.summary, jhlog.CollectorRoom) || collectorConfigured(b.summary, jhlog.CollectorDatabase) || b.summary.IOAnalysis != nil || b.summary.DatabaseAnalysis != nil, b.summary.IOAnalysis != nil || b.summary.DatabaseAnalysis != nil || hasSemanticDomain(b.summary, SemanticDomainRoom), []string{"файловые операции, SQL-вызовы SQLite/Room и DAO с привязкой к коду"}, "Включить сбор файловых операций, базы данных и Room, затем выполнить сценарий с БД или хранилищем."},
		{ProblemCategoryCPU, "Процессор и фоновые задачи", []string{"нагрузка процессора", "очереди исполнителей", "выполнения фоновых задач с привязкой к коду"}, collectorConfigured(b.summary, jhlog.CollectorSystemSampler) || collectorConfigured(b.summary, jhlog.CollectorWorker) || hasNamedPrefix(b.summary.Gauges, "process.cpu.") || b.summary.AsyncAnalysis != nil, hasNamedPrefix(b.summary.Gauges, "process.cpu.") || hasSemanticDomain(b.summary, SemanticDomainWorker) || b.summary.AsyncAnalysis != nil, []string{"нагрузка процессора, очереди асинхронных и фоновых задач"}, "Обернуть критичные исполнители и включить системный сбор и сбор фоновых задач, затем выполнить фоновый сценарий."},
		{ProblemCategoryPower, "Энергия и нагрев", []string{"температура и расход батареи", "зарядка и активность приложения"}, collectorConfigured(b.summary, jhlog.CollectorSystemSampler) || hasNamedPrefix(b.summary.Gauges, "device.thermal.") || hasNamedPrefix(b.summary.Gauges, "battery."), hasNamedPrefix(b.summary.Gauges, "device.thermal.") || hasNamedPrefix(b.summary.Gauges, "battery."), []string{"температура и расход батареи"}, "Записать длинный сценарий без зарядки с включённым системным сборщиком."},
		{ProblemCategoryLogs, "Логи", []string{"число вызовов логирования"}, len(b.summary.LogSpam) > 0, len(b.summary.LogSpam) > 0, []string{"число вызовов логирования"}, "Включить запись частого логирования и выполнить соответствующий сценарий."},
		{ProblemCategoryAndroidComponents, "Компоненты Android и IPC", []string{"жизненный цикл Service и BroadcastReceiver", "данные клиента и сервера Binder", "видимость и важность процесса"}, b.summary.AndroidComponents != nil, b.summary.AndroidComponents != nil && b.summary.AndroidComponents.Available && !b.summary.AndroidComponents.Partial, []string{"структурированные события Service, BroadcastReceiver, Binder/AIDL и состояния процесса"}, "Включить сбор компонентов Android для всех процессов сценария и передать полный набор .jhlog одного запуска приложения."},
	}
	if (b.dependencyInjection != nil && b.dependencyInjection.Available) ||
		len(findingsForCoverageCategory(b.findings, ProblemCategoryDependencyInjection)) > 0 {
		definitions = append(definitions, coverageDefinition{
			id: ProblemCategoryDependencyInjection, label: "DI",
			required:   []string{"каталог DI-классов и связей выбранного варианта сборки"},
			configured: true,
			sufficient: true,
			available:  []string{"классы DI и однозначно распознанные связи зависимостей"},
		})
	}
	out := make([]CategoryCoverage, 0, len(definitions))
	for _, definition := range definitions {
		findings := findingsForCoverageCategory(b.findings, definition.id)
		status := "insufficient_data"
		explanation := "Источник данных доступен частично, но наблюдений пока недостаточно для оценки."
		if len(findings) > 0 {
			status, explanation = "problems_found", "Найдены проблемы. Ниже они отсортированы по приоритету расследования."
		} else if definition.id == ProblemCategoryDependencyInjection {
			status = "healthy"
			explanation = "DI-анализ включён; среди зарегистрированных проблем нет сигналов, привязанных к известным DI-классам. Это не доказывает корректность всего графа зависимостей."
		} else if !definition.configured {
			status, explanation = "not_measured", "Обязательный источник данных не подключён, поэтому состояние категории неизвестно."
		} else if collectionEvidenceDegraded(b.summary.CollectionQuality) {
			status, explanation = "collection_degraded", "Данных недостаточно, чтобы подтвердить отсутствие проблем в этой категории."
		} else if !definition.sufficient {
			status, explanation = "insufficient_data", "Сбор включён, но наблюдений пока недостаточно для оценки."
		} else if b.summary.DurationMS >= 30_000 {
			status, explanation = "healthy", "В записанном сценарии опасные пороги не превышены."
		}
		coverage := CategoryCoverage{Category: definition.id, Label: definition.label, Status: status, FindingCount: len(findings), RequiredEvidence: definition.required, Explanation: explanation}
		if definition.configured {
			coverage.AvailableEvidence = definition.available
		} else {
			coverage.MissingEvidence = definition.required
			coverage.NextAction = definition.action
		}
		if len(findings) > 0 {
			coverage.WorstSeverity = findings[0].Severity
		}
		out = append(out, coverage)
	}
	return out
}

func collectionEvidenceDegraded(quality CollectionQuality) bool {
	if quality.Complete {
		return false
	}
	if quality.SegmentsWithQuality == 0 &&
		quality.AcceptedEvents == 0 &&
		quality.WrittenEvents == 0 &&
		quality.DamagedSegments == 0 {
		return false
	}
	return !quality.ExactAdmission ||
		!quality.ChainValid ||
		!quality.CounterInvariantsValid ||
		!quality.QualityProgressionValid ||
		quality.DamagedSegments > 0 ||
		quality.KnownLostEvents > 0 ||
		quality.ControlFailures > 0 ||
		quality.BoundedEvidenceLoss > 0 ||
		quality.OtherEvidenceLoss > 0
}

func summarizeProblems(findings []ProblemFinding, coverage []CategoryCoverage) ProblemSummary {
	s := ProblemSummary{SchemaVersion: ProblemSchemaVersion, Total: len(findings), SignalTotal: len(findings)}
	for _, finding := range findings {
		switch finding.Severity {
		case "critical":
			s.Critical++
		case "high":
			s.High++
		case "medium":
			s.Medium++
		case "low":
			s.Low++
		default:
			s.Info++
		}
	}
	for _, category := range coverage {
		count := 0
		for _, finding := range findings {
			if problemFindingHasCategory(finding, category.Category) {
				count++
			}
		}
		s.ByCategory = append(s.ByCategory, ProblemCount{Category: category.Category, Count: count})
		if category.Status != "healthy" && category.Status != "problems_found" {
			s.Unchecked++
		}
	}
	if s.Total > 0 {
		s.Verdict = "problems_found"
	} else if s.Unchecked > 0 {
		s.Verdict = "incomplete"
	} else {
		s.Verdict = "clean"
	}
	s.Headline = problemSummaryHeadline(s)
	return s
}

func problemSummaryHeadline(summary ProblemSummary) string {
	if summary.Total > 0 {
		if summary.SignalTotal > summary.Total {
			return fmt.Sprintf(
				"Найдено инцидентов: %d; объединено связанных сигналов: %d. Максимальный приоритет расследования — %s.",
				summary.Total,
				summary.SignalTotal,
				problemSummaryWorstPriority(summary),
			)
		}
		return fmt.Sprintf("Найдено проблем: %d. Максимальный приоритет расследования — %s.", summary.Total, problemSummaryWorstPriority(summary))
	}
	if summary.Unchecked > 0 {
		return fmt.Sprintf("Проблем не найдено, но %d категорий не проверены полностью.", summary.Unchecked)
	}
	return "В записанном сценарии значимых проблем не обнаружено."
}

func problemSummaryWorstPriority(summary ProblemSummary) string {
	switch {
	case summary.Critical > 0:
		return "критичный"
	case summary.High > 0:
		return "высокий"
	case summary.Medium > 0:
		return "средний"
	case summary.Low > 0:
		return "низкий"
	default:
		return "информационный"
	}
}

func detectorRegistry(cfg ProblemDetectorConfig) []DetectorMetadata {
	return []DetectorMetadata{
		{ID: "stability.historical_process_exit", Version: cfg.Version, Category: ProblemCategoryStability, Title: "Историческое завершение процесса", MinimumSample: 1, RequiredSignals: []string{"ApplicationExitInfo"}},
		{ID: "operations.health", Version: cfg.Version, Category: ProblemCategoryOperations, Title: "Нарушение бюджета или неуспешный итог операции", MinimumSample: cfg.OperationMinSample, RequiredSignals: []string{"operation lifecycle"}, Thresholds: []DetectorThreshold{{"budget_breach_rate", cfg.OperationBudgetBreachRate * 100, "%"}, {"failure_rate", cfg.OperationFailureRate * 100, "%"}}},
		{ID: "stability.main_thread_stall", Version: cfg.Version, Category: ProblemCategoryStability, Title: "Остановка главного потока", MinimumSample: 1, RequiredSignals: []string{"main-thread stall"}, Thresholds: []DetectorThreshold{{"stall", float64(cfg.StallMS), "ms"}, {"high", float64(cfg.StallHighMS), "ms"}}},
		{ID: "ui.jank_tail", Version: cfg.Version, Category: ProblemCategoryUI, Title: "Jank и длинный tail кадров", MinimumSample: cfg.UIMinFrames, RequiredSignals: []string{"UI window"}, Thresholds: []DetectorThreshold{{"jank_rate", cfg.UIJankRate, "%"}, {"frame_tail", float64(cfg.UIFrameTailMS), "ms"}}},
		{ID: "ui.compose_work", Version: cfg.Version, Category: ProblemCategoryUI, Title: "Тяжёлая или частая Compose-работа", MinimumSample: 3, RequiredSignals: []string{"Compose boundary"}, Thresholds: []DetectorThreshold{{"frame_budget", float64(composeDefaultFrameBudgetMS), "ms"}, {"frequent", float64(composeFrequentMinCount), "events"}}},
		{ID: "ui.startup_cold", Version: cfg.Version, Category: ProblemCategoryUI, Title: "Медленный первый resume процесса", MinimumSample: 1, RequiredSignals: []string{"first Activity resume"}, Thresholds: []DetectorThreshold{{"first_resume", float64(cfg.StartupColdResumeMS), "ms"}}},
		{ID: "ui.screen_resume", Version: cfg.Version, Category: ProblemCategoryUI, Title: "Медленный create-to-resume экрана", MinimumSample: 1, RequiredSignals: []string{"Activity create/resume"}, Thresholds: []DetectorThreshold{{"screen_resume", float64(cfg.ScreenResumeMS), "ms"}}},
		{ID: "network.route_health", Version: cfg.Version, Category: ProblemCategoryNetwork, Title: "Медленный/ошибочный/частый route", MinimumSample: cfg.HTTPMinSample, RequiredSignals: []string{"typed HTTP"}, Thresholds: []DetectorThreshold{{"slow_p95", float64(cfg.HTTPSlowMS), "ms"}, {"failure_rate", cfg.HTTPFailureRate * 100, "%"}, {"storm_rate", cfg.HTTPStormRate, "requests/s"}}},
		{ID: "network.websocket_health", Version: cfg.Version, Category: ProblemCategoryNetwork, Title: "Нестабильный WebSocket", MinimumSample: 3, RequiredSignals: []string{"typed WebSocket lifecycle"}, Thresholds: []DetectorThreshold{{"failure_rate", 25, "%"}, {"reconnects", 3, "events"}, {"slow_connect_p95", 1500, "ms"}, {"rapid_churn_lifetime_p95", 5000, "ms"}}},
		{ID: "memory.retention", Version: cfg.Version, Category: ProblemCategoryMemory, Title: "Удержание памяти", MinimumSample: 1, RequiredSignals: []string{"retention or HPROF"}},
		{ID: "memory.pressure", Version: cfg.Version, Category: ProblemCategoryMemory, Title: "Дефицит памяти", MinimumSample: 3, RequiredSignals: []string{"memory/context samples"}},
		{ID: "memory.gc_blocking", Version: cfg.Version, Category: ProblemCategoryMemory, Title: "Блокирующий GC", MinimumSample: 1, RequiredSignals: []string{"ART GC counters"}, Thresholds: []DetectorThreshold{{"blocking_time", float64(cfg.GCBlockingTimeMS), "ms"}}},
		{ID: "io.main_thread", Version: cfg.Version, Category: ProblemCategoryIO, Title: "I/O на главном потоке", MinimumSample: 1, RequiredSignals: []string{"attributed I/O with source"}, Thresholds: []DetectorThreshold{{"main_thread_slow", float64(cfg.IOMainThreadMS), "ms"}, {"large_operation", float64(cfg.IOLargeMainBytes), "bytes"}}},
		{ID: "io.operation_pressure", Version: cfg.Version, Category: ProblemCategoryIO, Title: "Медленный, ошибочный или частый I/O", MinimumSample: 1, RequiredSignals: []string{"attributed I/O with source"}, Thresholds: []DetectorThreshold{{"background_slow", float64(cfg.IOBackgroundMS), "ms"}, {"storm_rate", cfg.IOStormRate, "events/s"}, {"failure_rate", cfg.IOFailureRate * 100, "%"}, {"large_operation", float64(cfg.IOLargeBackgroundBytes), "bytes"}}},
		{ID: "io.database_repeated_in_scope", Version: cfg.Version, Category: ProblemCategoryIO, Title: "Повтор SQL внутри одной границы сценария", MinimumSample: 2, RequiredSignals: []string{"нормализованный SQL-шаблон", "идентификатор операции или транзакции"}, Thresholds: []DetectorThreshold{{"repeated_calls_per_scope", databaseScenarioRepeatCalls, "events"}}},
		{ID: "io.database_plan_evidence", Version: cfg.Version, Category: ProblemCategoryIO, Title: "Подтверждённый шаг плана SQL-запроса", MinimumSample: 1, RequiredSignals: []string{"предоставленный разработчиком очищенный план SQL-запроса", "совпадающий нормализованный SQL-шаблон"}},
		{ID: "io.database_calls", Version: cfg.Version, Category: ProblemCategoryIO, Title: "Медленный, частый или ошибочный SQL-вызов", MinimumSample: 5, RequiredSignals: []string{"typed SQLite/Room invocation"}, Thresholds: []DetectorThreshold{{"main_thread_slow", float64(cfg.DatabaseMainThreadMS), "ms"}, {"background_slow", float64(cfg.DatabaseBackgroundMS), "ms"}, {"storm_rate", float64(cfg.DatabaseStormRate), "events/s"}, {"rapid_repeats", float64(cfg.DatabaseRapidRepeatCount), "events"}, {"failure_rate", cfg.DatabaseFailureRate * 100, "%"}}},
		{ID: "io.database_transaction", Version: cfg.Version, Category: ProblemCategoryIO, Title: "Длинная, неуспешная или незавершённая транзакция БД", MinimumSample: 1, RequiredSignals: []string{"typed database transaction lifecycle"}, Thresholds: []DetectorThreshold{{"main_thread_slow", float64(cfg.DatabaseTransactionMainMS), "ms"}, {"background_slow", float64(cfg.DatabaseTransactionBackgroundMS), "ms"}, {"many_statements", float64(cfg.DatabaseTransactionStatements), "events"}}},
		{ID: "io.room_main_thread", Version: cfg.Version, Category: ProblemCategoryIO, Title: "Room DAO на главном потоке", MinimumSample: 1, RequiredSignals: []string{"Room DAO boundary"}, Thresholds: []DetectorThreshold{{"main_thread_slow", float64(cfg.IOMainThreadMS), "ms"}}},
		{ID: "io.room_pressure", Version: cfg.Version, Category: ProblemCategoryIO, Title: "Медленный или частый Room DAO", MinimumSample: 1, RequiredSignals: []string{"Room DAO boundary"}, Thresholds: []DetectorThreshold{{"background_slow", float64(cfg.IOBackgroundMS), "ms"}, {"storm_rate", cfg.IOStormRate, "events/s"}}},
		{ID: "cpu.process_saturation", Version: cfg.Version, Category: ProblemCategoryCPU, Title: "Высокая загрузка CPU", MinimumSample: 4, RequiredSignals: []string{"process CPU"}, Thresholds: []DetectorThreshold{{"process_cpu", cfg.ProcessCPUPercent, "% core"}}},
		{ID: "cpu.worker_execution", Version: cfg.Version, Category: ProblemCategoryCPU, Title: "Долгая, повторная или неуспешная Worker-задача", MinimumSample: 1, RequiredSignals: []string{"Worker boundary"}, Thresholds: []DetectorThreshold{{"long_execution", float64(workerLongExecutionMS), "ms"}, {"repeated", float64(workerRepeatedMinCount), "events"}}},
		{ID: "cpu.async_queue", Version: cfg.Version, Category: ProblemCategoryCPU, Title: "Ожидание в очереди Executor", MinimumSample: cfg.AsyncQueueMinSamples, RequiredSignals: []string{"wrapped Executor metrics"}, Thresholds: []DetectorThreshold{{"queue_wait", float64(cfg.AsyncQueueWaitMS), "ms"}}},
		{ID: "power.thermal_pressure", Version: cfg.Version, Category: ProblemCategoryPower, Title: "Thermal pressure", MinimumSample: 3, RequiredSignals: []string{"thermal status"}, Thresholds: []DetectorThreshold{{"severe", float64(cfg.ThermalSevereStatus), "Android status"}}},
		{ID: "logs.spam", Version: cfg.Version, Category: ProblemCategoryLogs, Title: "Спам логами", MinimumSample: cfg.LogSpamMinCount, RequiredSignals: []string{"log hook"}, Thresholds: []DetectorThreshold{{"count", float64(cfg.LogSpamMinCount), "logs"}, {"rate", cfg.LogSpamRate, "logs/s"}}},
		{ID: "android.service.callback_failure", Version: cfg.Version, Category: ProblemCategoryAndroidComponents, Title: "Ошибка callback службы", MinimumSample: 1, RequiredSignals: []string{"typed Service callback"}},
		{ID: "android.service.timeout", Version: cfg.Version, Category: ProblemCategoryAndroidComponents, Title: "Таймаут службы", MinimumSample: 1, RequiredSignals: []string{"typed Service onTimeout"}},
		{ID: "android.service.callback_slow", Version: cfg.Version, Category: ProblemCategoryAndroidComponents, Title: "Долгий callback службы", MinimumSample: 1, RequiredSignals: []string{"typed Service callback"}, Thresholds: []DetectorThreshold{{"slow_callback", float64(serviceSlowCallbackUS / 1_000), "ms"}}},
		{ID: "android.receiver.async_deadline_risk", Version: cfg.Version, Category: ProblemCategoryAndroidComponents, Title: "BroadcastReceiver близок к системному дедлайну", MinimumSample: 1, RequiredSignals: []string{"typed BroadcastReceiver async lifecycle"}, Thresholds: []DetectorThreshold{{"deadline_risk", float64(receiverDeadlineRiskUS / 1_000), "ms"}}},
		{ID: "android.receiver.sync_slow", Version: cfg.Version, Category: ProblemCategoryAndroidComponents, Title: "Синхронная работа в BroadcastReceiver", MinimumSample: 1, RequiredSignals: []string{"typed BroadcastReceiver lifecycle"}, Thresholds: []DetectorThreshold{{"sync_work", float64(receiverSyncSlowUS / 1_000), "ms"}}},
		{ID: "android.receiver.failure", Version: cfg.Version, Category: ProblemCategoryAndroidComponents, Title: "Ошибка BroadcastReceiver", MinimumSample: 1, RequiredSignals: []string{"typed BroadcastReceiver lifecycle"}},
		{ID: "android.binder.main_thread_slow", Version: cfg.Version, Category: ProblemCategoryAndroidComponents, Title: "Медленный Binder-вызов на главном потоке", MinimumSample: 1, RequiredSignals: []string{"typed Binder client transaction"}, Thresholds: []DetectorThreshold{{"main_thread_slow", float64(binderMainThreadSlowUS / 1_000), "ms"}}},
		{ID: "android.binder.failure", Version: cfg.Version, Category: ProblemCategoryAndroidComponents, Title: "Ошибка Binder-транзакции", MinimumSample: 1, RequiredSignals: []string{"typed Binder transaction outcome"}},
		{ID: "android.binder.unhandled", Version: cfg.Version, Category: ProblemCategoryAndroidComponents, Title: "Необработанный transaction code", MinimumSample: 1, RequiredSignals: []string{"typed Binder transaction outcome"}},
	}
}

func validateProblemReport(report ProblemReport) error {
	categories := map[string]struct{}{
		ProblemCategoryStability: {}, ProblemCategoryOperations: {}, ProblemCategoryUI: {}, ProblemCategoryNetwork: {}, ProblemCategoryMemory: {},
		ProblemCategoryIO: {}, ProblemCategoryCPU: {}, ProblemCategoryPower: {}, ProblemCategoryLogs: {},
		ProblemCategoryAndroidComponents: {}, ProblemCategoryDependencyInjection: {},
	}
	detectors := map[string]struct{}{}
	for _, detector := range report.Registry {
		if _, ok := categories[detector.Category]; !ok {
			return fmt.Errorf("detector %q has unknown category %q", detector.ID, detector.Category)
		}
		if detector.ID == "" || detector.Version == "" {
			return fmt.Errorf("detector metadata is incomplete")
		}
		if _, exists := detectors[detector.ID]; exists {
			return fmt.Errorf("duplicate detector %q", detector.ID)
		}
		detectors[detector.ID] = struct{}{}
	}
	knownSeverity := map[string]struct{}{"info": {}, "low": {}, "medium": {}, "high": {}, "critical": {}}
	knownUnits := map[string]struct{}{"": {}, "requests": {}, "attempts": {}, "ms": {}, "%": {}, "requests/s": {}, "events": {}, "events/s": {}, "events/scope": {}, "calls": {}, "calls/boundary": {}, "bytes": {}, "KB": {}, "samples": {}, "% core": {}, "Android status": {}, "logs": {}}
	fingerprints := map[string]struct{}{}
	for _, finding := range report.Problems {
		if _, ok := detectors[finding.DetectorID]; !ok {
			return fmt.Errorf("finding %q uses unknown detector %q", finding.ID, finding.DetectorID)
		}
		if _, ok := categories[finding.Category]; !ok {
			return fmt.Errorf("finding %q has unknown category %q", finding.ID, finding.Category)
		}
		if _, ok := knownSeverity[finding.Severity]; !ok {
			return fmt.Errorf("finding %q has invalid severity %q", finding.ID, finding.Severity)
		}
		if finding.Confidence != "low" && finding.Confidence != "medium" && finding.Confidence != "high" {
			return fmt.Errorf("finding %q has invalid confidence %q", finding.ID, finding.Confidence)
		}
		if finding.Why.ClaimLevel != "linked" && finding.Why.ClaimLevel != "correlated" && finding.Why.ClaimLevel != "hypothesis" && finding.Why.ClaimLevel != "unknown" {
			return fmt.Errorf("finding %q has invalid claim level %q", finding.ID, finding.Why.ClaimLevel)
		}
		if err := validateInvestigationPriority(finding); err != nil {
			return fmt.Errorf("finding %q has invalid investigation priority: %w", finding.ID, err)
		}
		for _, evidence := range finding.Evidence {
			if _, ok := knownUnits[evidence.Unit]; !ok {
				return fmt.Errorf("finding %q has unknown unit %q", finding.ID, evidence.Unit)
			}
		}
		if _, exists := fingerprints[finding.Fingerprint]; exists {
			return fmt.Errorf("duplicate finding fingerprint %q", finding.Fingerprint)
		}
		fingerprints[finding.Fingerprint] = struct{}{}
	}
	return nil
}
