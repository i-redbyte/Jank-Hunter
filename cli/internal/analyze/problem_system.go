package analyze

import (
	"fmt"
	"math"
	"time"
)

func (b *problemBuilder) detectMemory() {
	for _, leak := range b.summary.MemoryLeaks {
		if leak.Count == 0 {
			continue
		}
		impact := 18
		if leak.HeapEvidence {
			impact = 32
		}
		if leak.EstimatedRetainedKB >= 16*1024 {
			impact = min(40, impact+6)
		}
		magnitude := min(25, 6+int(math.Log2(float64(leak.EstimatedRetainedKB/1024)+1))*4)
		exposure := min(20, 5+int(leak.Count)*3)
		confidence, reasons, limits := problemConfidence(b.summary, leak.Count, 2, true)
		claim := "hypothesis"
		why := "Объект удерживался дольше ожидаемого; без пути удержания в куче это сигнал, а не доказанная утечка."
		if leak.HeapEvidence {
			confidence, claim, why = "high", "linked", "HPROF подтвердил путь удержания до корня GC."
			reasons = append(reasons, "Есть путь удержания из HPROF и измерение удерживаемого размера.")
		}
		subcategory := "retained_object"
		evidenceSource := "runtime_estimate"
		if leak.HeapEvidence {
			subcategory = "confirmed_leak"
			evidenceSource = "hprof"
		}
		where := []ProblemLocation{{Screen: leak.Screen, Operation: leak.Operation, Owner: leak.Holder, Class: leak.ClassName}}
		b.add(ProblemFinding{
			DetectorID: "memory.retention", DetectorVersion: b.cfg.Version, Category: ProblemCategoryMemory, Subcategory: subcategory,
			Status: "observed", Confidence: confidence, ConfidenceReasons: uniqueStrings(reasons), Title: memoryTitle(leak),
			WhatHappened: fmt.Sprintf("%s удерживался до %d мс; оценка удерживаемого размера — %d КБ; наблюдений — %d.", leak.ClassName, leak.MaxAgeMS, leak.EstimatedRetainedKB, leak.Count), Where: where,
			Why:       ProblemWhy{ClaimLevel: claim, Summary: why, Factors: nonEmptyStrings(leak.GCRoot, leak.HolderField, leak.LeakPattern)},
			Impact:    []string{"Рост памяти, давление GC и риск OOM при накоплении"},
			Evidence:  []ProblemEvidence{{Name: "Возраст удержания", Observed: fmt.Sprint(leak.MaxAgeMS), Unit: "ms", Sample: u64ptr(leak.Count), Source: "retention"}, {Name: "Удерживаемый размер", Observed: fmt.Sprint(leak.EstimatedRetainedKB), Unit: "KB", Source: evidenceSource}},
			Frequency: &ProblemFrequency{Count: leak.Count}, Cost: &ProblemCost{MemoryKB: nonZeroU64Ptr(leak.EstimatedRetainedKB)},
			PriorityBreakdown: priority(impact, magnitude, exposure, locationBreadth(where), boolScore(leak.HeapEvidence && leak.EstimatedRetainedKB > 0, 5), "риск OOM", "удерживаемый размер", "повторяемость", "контекст", "путь удержания и размер"),
			Recommendations:   []ProblemRecommendation{{Action: firstNonEmpty(leak.Recommendation, "Разорвать путь удержания и ограничить время жизни владельца"), Rationale: "Устранение пути от корня GC освобождает всё поддерево зависимых объектов.", Verification: "Повторить сценарий, вызвать GC и подтвердить отсутствие объекта и пути удержания в новом HPROF."}},
			Limitations:       append(limits, leak.QualityWarnings...), Drilldowns: []ProblemDrilldown{{Label: "Память", Anchor: "memory-resources", Filter: leak.ClassName}},
		})
	}
	if b.summary.LowMemoryCount > 0 {
		count := uint64(b.summary.LowMemoryCount)
		confidence, reasons, limits := problemConfidence(b.summary, count, 3, true)
		b.add(ProblemFinding{DetectorID: "memory.pressure", DetectorVersion: b.cfg.Version, Category: ProblemCategoryMemory, Subcategory: "low_memory", Status: "observed", Confidence: confidence, ConfidenceReasons: reasons, Title: "Приложение работало при дефиците памяти", WhatHappened: fmt.Sprintf("Дефицит памяти отмечен в %d замерах; максимальный PSS — %d КБ, минимум доступной памяти — %d КБ.", count, b.summary.MemoryMaxKB, b.summary.AvailMemoryMinKB), Why: ProblemWhy{ClaimLevel: "correlated", Summary: "Дефицит памяти и PSS наблюдались в одном прогоне; код, создающий объекты, пока не локализован."}, Impact: []string{"Более частые GC, выгрузка компонентов и риск OOM"}, Evidence: []ProblemEvidence{{Name: "Замеры с дефицитом памяти", Observed: fmt.Sprint(count), Unit: "samples", Source: "memory_context"}, {Name: "Максимальный PSS", Observed: fmt.Sprint(b.summary.MemoryMaxKB), Unit: "KB", Source: "memory_sample"}}, Frequency: &ProblemFrequency{Count: count}, Cost: &ProblemCost{MemoryKB: nonZeroU64Ptr(b.summary.MemoryMaxKB)}, PriorityBreakdown: priority(26, 14, min(20, int(count)*4), 2, 3, "дефицит памяти", "PSS и запас доступной памяти", "число замеров", "весь прогон", "дефицит памяти и PSS"), Recommendations: []ProblemRecommendation{{Action: "Записать изменение размера кучи и выделения памяти во времени, затем сократить крупные кеши и буферы", Rationale: "Одного абсолютного значения PSS недостаточно, чтобы назвать участок кода.", Verification: "В длинном повторе сравнить динамику PSS и кучи, паузы GC и минимальный запас доступной памяти."}}, Limitations: append(limits, "Максимальный PSS не доказывает рост памяти; нужна динамика во времени.")})
	}
}

func (b *problemBuilder) detectProcessExit() {
	for _, exit := range b.summary.ProcessExits {
		name, severe := processExitReason(exit.Reason)
		if !severe {
			continue
		}
		when := time.UnixMilli(int64(exit.LatestTimestampUnixMS)).UTC().Format(time.RFC3339)
		confidence := "medium"
		reasons := []string{"ApplicationExitInfo содержит типизированные причину, время, процесс и показатели памяти.", "Событие историческое: оно относится к предыдущему экземпляру процесса."}
		where := []ProblemLocation{{Process: exit.Process}}
		b.add(ProblemFinding{
			DetectorID: "stability.historical_process_exit", DetectorVersion: b.cfg.Version,
			Category: ProblemCategoryStability, Subcategory: fmt.Sprintf("historical_exit_%d", exit.Reason), Status: "observed",
			Confidence: confidence, ConfidenceReasons: reasons,
			Title:        "Android подтвердил предыдущее завершение: " + name,
			WhatHappened: fmt.Sprintf("%s завершился с причиной %s; последнее событие — %s, наблюдений — %d.", displayUnknown(exit.Process, "Процесс"), name, when, exit.Count),
			Where:        where,
			Why:          ProblemWhy{ClaimLevel: "linked", Summary: "Android связал системную причину с конкретным историческим процессом; связь с текущим пользовательским действием не доказана."},
			Impact:       []string{"Потеря пользовательского сценария; для ANR/crash/OOM возможна потеря несохранённых данных"},
			Evidence: []ProblemEvidence{
				{Name: "Причина завершения", Observed: name, Sample: u64ptr(exit.Count), Source: "typed_application_exit_info"},
				{Name: "Время последнего завершения", Observed: when, Source: "typed_application_exit_info"},
				{Name: "PSS при завершении", Observed: fmt.Sprint(exit.MaxPSSKB), Unit: "KB", Source: "typed_application_exit_info"},
				{Name: "RSS при завершении", Observed: fmt.Sprint(exit.MaxRSSKB), Unit: "KB", Source: "typed_application_exit_info"},
			},
			Frequency: &ProblemFrequency{Count: exit.Count}, Cost: &ProblemCost{MemoryKB: nonZeroU64Ptr(exit.MaxPSSKB)},
			PriorityBreakdown: priority(40, 20, min(20, 6+int(exit.Count)*3), locationBreadth(where), 3, "аварийное завершение, ANR или OOM", "системная причина", "исторические события", "процесс", "причина, время и память"),
			Recommendations:   []ProblemRecommendation{{Action: "Сопоставить время с трассой сбоя или ANR и воспроизвести соответствующую операцию", Rationale: "Типизированное завершение подтверждает класс сбоя, но не называет текущую строку кода.", Verification: "Проверить отсутствие новой записи той же причины после исправления и повторной операции."}},
			Limitations:       []string{"ApplicationExitInfo описывает предыдущий процесс и не доказывает, что сбой произошёл в анализируемом прогоне."},
		})
	}
}

func (b *problemBuilder) detectCPU() {
	value, ok := namedValue(b.summary.Gauges, "process.cpu.core_percent_x100")
	if !ok {
		return
	}
	percent := float64(value) / 100
	if percent < b.cfg.ProcessCPUPercent {
		return
	}
	confidence, reasons, limits := problemConfidence(b.summary, 1, 4, true)
	b.add(ProblemFinding{DetectorID: "cpu.process_saturation", DetectorVersion: b.cfg.Version, Category: ProblemCategoryCPU, Subcategory: "process_cpu", Status: "observed", Confidence: confidence, ConfidenceReasons: reasons, Title: "Процесс длительно нагружает CPU", WhatHappened: fmt.Sprintf("Агрегированная загрузка процесса — %.1f%% одного ядра.", percent), Why: ProblemWhy{ClaimLevel: "unknown", Summary: "Общая загрузка CPU процесса не связывает нагрузку с конкретным методом или задачей."}, Impact: []string{"Конкуренция за CPU, задержки UI, нагрев и расход батареи"}, Evidence: []ProblemEvidence{{Name: "Загрузка CPU процесса", Observed: fmt.Sprintf("%.1f", percent), Unit: "% core", ExpectedOrThreshold: fmt.Sprintf("< %.0f%%", b.cfg.ProcessCPUPercent), Source: "system_sampler_gauge"}}, PriorityBreakdown: priority(22, min(25, int(percent/10)+8), 10, 2, 0, "конкуренция за процессор", "уровень загрузки CPU", "агрегированные замеры", "весь прогон", "нет привязки к задаче"), Recommendations: []ProblemRecommendation{{Action: "Связать всплеск CPU с трассой выполняемых задач и методов", Rationale: "Сигнал всего процесса не называет виновный код.", Verification: "Повторить с записью выполняемых задач и методов и проверить CPU вместе с верхними 5% задержек UI."}}, Limitations: append(limits, "Измерение агрегировано: длительность нагрузки и привязка к отдельным потокам недоступны.")})
}

func (b *problemBuilder) detectRuntimeAnalysis() {
	b.detectAsyncQueues()
	b.detectGCBlocking()
	b.detectStartupLatency()
}

func (b *problemBuilder) detectAsyncQueues() {
	if b.summary.AsyncAnalysis == nil {
		return
	}
	for _, executor := range b.summary.AsyncAnalysis.Executors {
		if executor.WaitSamples < b.cfg.AsyncQueueMinSamples || executor.MaxWaitMS < b.cfg.AsyncQueueWaitMS {
			continue
		}
		saturated := executor.MaxPoolSize > 0 && executor.MaxActiveCount >= executor.MaxPoolSize && executor.MaxQueueDepth > 0
		confidence, reasons, limits := problemConfidence(b.summary, executor.WaitSamples, b.cfg.AsyncQueueMinSamples, true)
		where := []ProblemLocation{{Owner: executor.Name}}
		compound := boolScore(saturated, 4)
		var saturationFactor string
		if saturated {
			saturationFactor = "В тот же период active count достиг размера пула при непустой очереди."
		}
		b.add(ProblemFinding{
			DetectorID: "cpu.async_queue", DetectorVersion: b.cfg.Version,
			Category: ProblemCategoryCPU, Subcategory: "executor_queue_wait", Status: "observed",
			Confidence: confidence, ConfidenceReasons: reasons,
			Title:        fmt.Sprintf("Очередь пула %s задерживает запуск задач", displayUnknown(executor.Name, "неизвестный пул")),
			WhatHappened: fmt.Sprintf("Измерено %d запусков: среднее ожидание %d мс, максимум %d мс; глубина очереди доходила до %d.", executor.WaitSamples, executor.AvgWaitMS, executor.MaxWaitMS, executor.MaxQueueDepth),
			Where:        where,
			Why:          ProblemWhy{ClaimLevel: "linked", Summary: "Обёртка пула напрямую измерила время между постановкой задачи и началом её выполнения.", Factors: nonEmptyStrings(saturationFactor)},
			Impact:       []string{"Задержка фоновых результатов и рост latency зависимых UI/сетевых сценариев"},
			Evidence: []ProblemEvidence{
				{Name: "Максимальное ожидание", Observed: fmt.Sprint(executor.MaxWaitMS), Unit: "ms", ExpectedOrThreshold: fmt.Sprintf("< %d ms", b.cfg.AsyncQueueWaitMS), Sample: u64ptr(executor.WaitSamples), Source: "executor_wrapper_metric"},
				{Name: "Среднее ожидание", Observed: fmt.Sprint(executor.AvgWaitMS), Unit: "ms", Source: "executor_wrapper_metric"},
				{Name: "Максимальная глубина очереди", Observed: fmt.Sprint(executor.MaxQueueDepth), Unit: "events", Source: "executor_wrapper_metric"},
			},
			Frequency:         &ProblemFrequency{Count: executor.WaitSamples},
			PriorityBreakdown: priority(22, min(25, 8+int(executor.MaxWaitMS/b.cfg.AsyncQueueWaitMS)*4), min(20, 5+int(math.Log2(float64(executor.WaitSamples)+1))*3), locationBreadth(where), compound, "задержка результата", "время ожидания в очереди", "число запусков", "пул потоков", "насыщение пула"),
			Recommendations:   []ProblemRecommendation{{Action: "Проверить размер пула, блокирующие задачи и приоритет его очереди", Rationale: "Измерение локализует ожидание до конкретного пула, но не выбирает виновную задачу.", Verification: "Повторить сценарий и подтвердить снижение максимального и среднего ожидания и глубины очереди."}},
			Limitations:       append(limits, "Метрики агрегированы по окнам: максимум точен, среднее взвешено по числу замеров, распределение внутри окна не восстанавливается."),
			Drilldowns:        []ProblemDrilldown{{Label: "Асинхронные очереди", Anchor: "async-analysis", Filter: executor.Name}},
		})
	}
}

func (b *problemBuilder) detectGCBlocking() {
	gc := b.summary.GCAnalysis
	if gc == nil || gc.BlockingCount == 0 || gc.BlockingTimeMS < b.cfg.GCBlockingTimeMS {
		return
	}
	confidence, reasons, limits := problemConfidence(b.summary, gc.BlockingCount, 1, true)
	b.add(ProblemFinding{
		DetectorID: "memory.gc_blocking", DetectorVersion: b.cfg.Version,
		Category: ProblemCategoryMemory, Subcategory: "blocking_gc", Status: "observed",
		Confidence: confidence, ConfidenceReasons: reasons,
		Title:        "ART зафиксировал заметное время блокирующих GC",
		WhatHappened: fmt.Sprintf("Зафиксировано %d блокирующих GC суммарной длительностью %d мс; всего GC — %d за %d мс.", gc.BlockingCount, gc.BlockingTimeMS, gc.CollectionCount, gc.TotalTimeMS),
		Why:          ProblemWhy{ClaimLevel: "correlated", Summary: fmt.Sprintf("Счётчики ART подтверждают нагрузку от GC в процессе. %d UI-окон с рывками пересеклись по времени с окном измерения GC, но это не доказывает причинность.", gc.JankyUIWindowsNearGC)},
		Impact:       []string{"Паузы всего процесса во время GC могут увеличивать задержку самых медленных кадров и фоновых задач"},
		Evidence: []ProblemEvidence{
			{Name: "Время блокирующих GC", Observed: fmt.Sprint(gc.BlockingTimeMS), Unit: "ms", ExpectedOrThreshold: fmt.Sprintf("< %d ms", b.cfg.GCBlockingTimeMS), Sample: u64ptr(gc.BlockingCount), Source: "art_runtime_stats"},
			{Name: "Блокирующие GC", Observed: fmt.Sprint(gc.BlockingCount), Unit: "events", Source: "art_runtime_stats"},
			{Name: "UI-окна с рывками рядом", Observed: fmt.Sprint(gc.JankyUIWindowsNearGC), Unit: "events", Source: "temporal_window_join"},
		},
		Frequency:         &ProblemFrequency{Count: gc.BlockingCount},
		PriorityBreakdown: priority(28, min(25, 8+int(gc.BlockingTimeMS/b.cfg.GCBlockingTimeMS)*5), min(20, 4+int(gc.BlockingCount)*3), 1, boolScore(gc.JankyUIWindowsNearGC > 0, 4), "паузы процесса", "время блокирующих GC", "число GC", "весь процесс", "совпавшие UI-окна"),
		Recommendations:   []ProblemRecommendation{{Action: "Записать выделения памяти вокруг сценария и сократить часто создаваемые временные объекты", Rationale: "ART показывает нагрузку от GC, но счётчики всего процесса не называют участок кода и тип объектов.", Verification: "Повторить сценарий и сравнить число и время блокирующих GC, скорость выделения памяти и верхние 5% задержек UI."}},
		Limitations:       append(limits, "Время события может соответствовать окончанию окна агрегации; временное совпадение с UI не является доказательством причины."),
		Drilldowns:        []ProblemDrilldown{{Label: "GC и аллокации", Anchor: "gc-analysis"}},
	})
}

func (b *problemBuilder) detectStartupLatency() {
	startup := b.summary.StartupAnalysis
	if startup == nil {
		return
	}
	if startup.ColdResumeSamples > 0 && startup.MaxColdResumeMS >= b.cfg.StartupColdResumeMS {
		confidence, reasons, limits := problemConfidence(b.summary, startup.ColdResumeSamples, 1, true)
		b.add(ProblemFinding{
			DetectorID: "ui.startup_cold", DetectorVersion: b.cfg.Version,
			Category: ProblemCategoryUI, Subcategory: "cold_first_resume", Status: "observed",
			Confidence: confidence, ConfidenceReasons: reasons,
			Title:             "Первое открытие экрана после запуска процесса занимает слишком много времени",
			WhatHappened:      fmt.Sprintf("Первое открытие экрана после запуска процесса: среднее %d мс, максимум %d мс по %d запускам.", startup.AvgColdResumeMS, startup.MaxColdResumeMS, startup.ColdResumeSamples),
			Why:               ProblemWhy{ClaimLevel: "linked", Summary: "Жизненный цикл экрана напрямую измеряет время от запуска наблюдения в процессе до первого вызова onActivityResumed."},
			Impact:            []string{"Пользователь дольше ждёт первого доступного экрана"},
			Evidence:          []ProblemEvidence{{Name: "Максимум первого открытия", Observed: fmt.Sprint(startup.MaxColdResumeMS), Unit: "ms", ExpectedOrThreshold: fmt.Sprintf("< %d ms", b.cfg.StartupColdResumeMS), Sample: u64ptr(startup.ColdResumeSamples), Source: "activity_lifecycle"}},
			Frequency:         &ProblemFrequency{Count: startup.ColdResumeSamples},
			PriorityBreakdown: priority(30, min(25, 10+int(startup.MaxColdResumeMS/b.cfg.StartupColdResumeMS)*5), min(20, 5+int(startup.ColdResumeSamples)*3), 1, 0, "первый экран", "длительность первого открытия", "число запусков", "весь процесс", "один сигнал жизненного цикла"),
			Recommendations:   []ProblemRecommendation{{Action: "Разделить критический путь запуска приложения и первого экрана, затем отложить необязательную инициализацию", Rationale: "Метрика отделяет первое открытие экрана после запуска процесса от последующих переходов.", Verification: "Сравнить время первого открытия на одинаковом устройстве после полного перезапуска процесса."}},
			Limitations:       append(limits, "Это не полное измерение холодного запуска Android и не время до полной отрисовки: отсчёт начинается при запуске наблюдения Jank Hunter."),
			Drilldowns:        []ProblemDrilldown{{Label: "Запуск приложения", Anchor: "startup-analysis"}},
		})
	}
	for _, screen := range startup.Screens {
		if screen.ResumeSamples == 0 || screen.MaxResumeMS < b.cfg.ScreenResumeMS {
			continue
		}
		confidence, reasons, limits := problemConfidence(b.summary, screen.ResumeSamples, 1, true)
		where := []ProblemLocation{{Screen: screen.Screen}}
		b.add(ProblemFinding{
			DetectorID: "ui.screen_resume", DetectorVersion: b.cfg.Version,
			Category: ProblemCategoryUI, Subcategory: "screen_time_to_resume", Status: "observed",
			Confidence: confidence, ConfidenceReasons: reasons,
			Title:             fmt.Sprintf("Экран %s долго переходит от создания к активному состоянию", displayUnknown(screen.Screen, "без атрибуции")),
			WhatHappened:      fmt.Sprintf("Среднее время от создания до активного состояния — %d мс, максимум %d мс по %d наблюдениям.", screen.AvgResumeMS, screen.MaxResumeMS, screen.ResumeSamples),
			Where:             where,
			Why:               ProblemWhy{ClaimLevel: "linked", Summary: "События жизненного цикла Activity напрямую связали создание и переход в активное состояние одного экземпляра экрана."},
			Impact:            []string{"Задержка появления или интерактивности экрана при навигации"},
			Evidence:          []ProblemEvidence{{Name: "Максимум от создания до активного состояния", Observed: fmt.Sprint(screen.MaxResumeMS), Unit: "ms", ExpectedOrThreshold: fmt.Sprintf("< %d ms", b.cfg.ScreenResumeMS), Sample: u64ptr(screen.ResumeSamples), Source: "activity_lifecycle"}},
			Frequency:         &ProblemFrequency{Count: screen.ResumeSamples},
			PriorityBreakdown: priority(24, min(25, 8+int(screen.MaxResumeMS/b.cfg.ScreenResumeMS)*5), min(20, 4+int(screen.ResumeSamples)*3), locationBreadth(where), 0, "переход экрана", "время до активного состояния", "число переходов", "экран", "один сигнал жизненного цикла"),
			Recommendations:   []ProblemRecommendation{{Action: "Проверить синхронную работу между onCreate, onStart и onResume Activity и перенести необязательную подготовку", Rationale: "Метрика локализована до класса экрана.", Verification: "Повторить переход и сравнить среднее и максимальное время до активного состояния."}},
			Limitations:       append(limits, "Интервал включает жизненный цикл и планирование главного потока, но не измеряет время до первого или полного отображения кадра."),
			Drilldowns:        []ProblemDrilldown{{Label: "Запуск и переходы", Anchor: "startup-analysis", Filter: screen.Screen}},
		})
	}
}

func (b *problemBuilder) detectPower() {
	status, ok := namedValue(b.summary.Gauges, "device.thermal.status")
	if !ok || status < b.cfg.ThermalSevereStatus {
		return
	}
	confidence, reasons, limits := problemConfidence(b.summary, 1, 3, true)
	b.add(ProblemFinding{DetectorID: "power.thermal_pressure", DetectorVersion: b.cfg.Version, Category: ProblemCategoryPower, Subcategory: "thermal", Status: "observed", Confidence: confidence, ConfidenceReasons: reasons, Title: "Устройство работало при сильном нагреве", WhatHappened: fmt.Sprintf("Состояние нагрева Android достигло %d (порог сильного нагрева: %d).", status, b.cfg.ThermalSevereStatus), Why: ProblemWhy{ClaimLevel: "unknown", Summary: "Состояние нагрева — внешний фактор; оно не доказывает, что приложение вызвало нагрев."}, Impact: []string{"Снижение частот процессора из-за нагрева может усиливать просадки CPU и интерфейса"}, Evidence: []ProblemEvidence{{Name: "Состояние нагрева", Observed: fmt.Sprint(status), Unit: "Android status", ExpectedOrThreshold: fmt.Sprintf("< %d", b.cfg.ThermalSevereStatus), Source: "system_sampler_gauge"}}, PriorityBreakdown: priority(18, min(25, int(status)*4), 8, 2, 0, "ограничение частот", "состояние нагрева", "снимок запуска", "уровень устройства", "причина не связана"), Recommendations: []ProblemRecommendation{{Action: "Повторить операцию на холодном устройстве и сопоставить загрузку процессора и плавность интерфейса", Rationale: "Так отделяется дефект приложения от внешнего влияния нагрева.", Verification: "Сравнить одинаковую операцию при нормальном и сильном нагреве."}}, Limitations: append(limits, "Короткий снимок нагрева не доказывает длительность или источник нагрева.")})
}

func (b *problemBuilder) detectLogSpam() {
	for _, row := range b.summary.LogSpam {
		rate := ratePerSecond(row.Count, b.summary.DurationMS)
		if row.Count < b.cfg.LogSpamMinCount && (rate == nil || *rate < b.cfg.LogSpamRate) {
			continue
		}
		confidence, reasons, limits := problemConfidence(b.summary, row.Count, b.cfg.LogSpamMinCount, true)
		where := []ProblemLocation{{Screen: row.Screen, Operation: row.Operation, Owner: row.Owner}}
		b.add(ProblemFinding{DetectorID: "logs.spam", DetectorVersion: b.cfg.Version, Category: ProblemCategoryLogs, Subcategory: "log_spam", Status: "observed", Confidence: confidence, ConfidenceReasons: reasons, Title: fmt.Sprintf("%s создаёт поток повторяющихся записей журнала", displayUnknown(row.Owner, row.Source)), WhatHappened: fmt.Sprintf("%s.%s записан %d раз%s.", row.Source, row.Level, row.Count, formatOptionalRate(rate)), Where: where, Why: ProblemWhy{ClaimLevel: "linked", Summary: "Автоматический перехват напрямую связал вызовы журналирования с этим источником и контекстом."}, Impact: []string{"Лишние выделения памяти, форматирование и ввод-вывод; полезные сообщения теряются в шуме"}, Evidence: []ProblemEvidence{{Name: "Число сообщений", Observed: fmt.Sprint(row.Count), Unit: "logs", ExpectedOrThreshold: fmt.Sprintf("< %d", b.cfg.LogSpamMinCount), Sample: u64ptr(row.Count), Source: "log_hook"}}, Frequency: &ProblemFrequency{Count: row.Count, RatePerSec: rate}, PriorityBreakdown: priority(12, min(25, 6+int(math.Log2(float64(row.Count)))), min(20, 6+int(math.Log2(float64(row.Count)))), locationBreadth(where), 0, "накладные расходы диагностики", "число", "частота/число", "контекст", "один симптом"), Recommendations: []ProblemRecommendation{{Action: "Удалить запись журнала из часто выполняемого участка или добавить ограничение частоты и объединение", Rationale: "Сокращает накладные расходы и повышает диагностическую ценность журнала.", Verification: "Повторить операцию и проверить число и частоту записей этого источника."}}, Limitations: limits})
	}
}
