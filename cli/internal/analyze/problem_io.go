package analyze

import (
	"fmt"
	"math"
)

func (b *problemBuilder) detectCriticalIO(operation IOStats) {
	maxMS := microsecondsToMillisecondsCeil(operation.MaxDurationUS)
	totalMS := microsecondsToMillisecondsCeil(operation.TotalDurationUS)
	classification := classifyIO(operation, b.cfg)
	if !classification.IsProblem() {
		return
	}

	subcategory := "slow_background_io_" + operation.Operation
	title := fmt.Sprintf("%s выполняется слишком долго", ioOperationLabel(operation.Operation))
	impact := 16
	switch classification.Kind {
	case IOClassificationMainThreadSync:
		subcategory = "main_thread_sync_" + operation.Operation
		title = "Синхронизация файла выполняется на главном потоке"
		impact = 32
	case IOClassificationMainThreadLarge:
		subcategory = "main_thread_large_" + operation.Operation
		title = fmt.Sprintf("%s читает или пишет большой объём на главном потоке", ioOperationLabel(operation.Operation))
		impact = 32
	case IOClassificationMainThreadSlow:
		subcategory = "main_thread_io_" + operation.Operation
		title = fmt.Sprintf("%s блокирует главный поток", ioOperationLabel(operation.Operation))
		impact = 32
	case IOClassificationRepeatedFailures:
		subcategory = "io_failures_" + operation.Operation
		title = fmt.Sprintf("%s часто завершается ошибкой", ioOperationLabel(operation.Operation))
		impact = 24
	case IOClassificationBackgroundLarge:
		subcategory = "large_background_io_" + operation.Operation
		title = fmt.Sprintf("%s обрабатывает большой объём за одну операцию", ioOperationLabel(operation.Operation))
		impact = 20
	case IOClassificationSmallOperationStorm:
		subcategory = "small_io_storm_" + operation.Operation
		title = fmt.Sprintf("%s создаёт поток мелких I/O операций", ioOperationLabel(operation.Operation))
		impact = 20
	}

	where := []ProblemLocation{{
		Screen: operation.Screen, Operation: operation.ContextOperation,
		Owner: firstKnown(operation.Source, operation.Owner),
	}}
	if !isUnknownAnalysisValue(operation.Owner) && operation.Owner != operation.Source {
		where = appendUniqueLocation(where, ProblemLocation{
			Screen: operation.Screen, Operation: operation.ContextOperation, Owner: operation.Owner,
		})
	}
	confidence, reasons, limits := problemConfidence(b.summary, operation.Count, 3, true)
	if classification.byteCoverage < 1 {
		limits = append(limits, fmt.Sprintf(
			"Размер известен для %d из %d операций; объём и throughput описывают только покрытую часть.",
			operation.KnownByteOperations,
			operation.Count,
		))
	}
	evidence := []ProblemEvidence{
		{Name: "Максимальная длительность", Observed: fmt.Sprint(maxMS), Unit: "ms", Sample: u64ptr(operation.Count), Source: "typed_io"},
		{Name: "Суммарная длительность", Observed: fmt.Sprint(totalMS), Unit: "ms", Source: "typed_io"},
		{Name: "Неуспешные операции", Observed: fmt.Sprint(operation.Failures), Unit: "events", Denominator: u64ptr(operation.Count), Source: "typed_io"},
		{Name: "Максимальный известный объём", Observed: fmt.Sprint(operation.MaxBytes), Unit: "bytes", Denominator: u64ptr(operation.KnownByteOperations), Source: "typed_io"},
		{Name: "Пик за скользящую секунду", Observed: fmt.Sprint(operation.PeakOperationsPerSecond), Unit: "events/s", Source: "typed_io"},
	}
	thresholdMS := b.cfg.IOBackgroundMS
	if operation.MainThread {
		thresholdMS = b.cfg.IOMainThreadMS
	}
	evidence[0].ExpectedOrThreshold = fmt.Sprintf("< %d ms", thresholdMS)
	peakRate := float64(operation.PeakOperationsPerSecond)
	magnitude := min(25, 6+int(maxMS/maxUint64(thresholdMS, 1))*4)
	if classification.has(ioSignalRepeatedFailures) {
		magnitude = max(magnitude, min(25, 8+int(classification.failureRate/b.cfg.IOFailureRate)*4))
	}
	largeThreshold := b.cfg.IOLargeBackgroundBytes
	if operation.MainThread {
		largeThreshold = b.cfg.IOLargeMainBytes
	}
	if operation.MaxBytes >= largeThreshold {
		magnitude = max(magnitude, min(25, 8+int(operation.MaxBytes/maxUint64(largeThreshold, 1))*3))
	}
	exposure := min(20, 5+int(math.Log2(float64(operation.Count)+1))*3)
	if classification.has(ioSignalSmallOperationStorm) {
		exposure = max(exposure, min(20, 10+int(peakRate/b.cfg.IOStormRate)*2))
	}
	compound := boolScore(classification.signalCount() > 1, 5)
	whatHappened := fmt.Sprintf(
		"%s из %s: %d операций, %d ошибок, граница верхних 5%% %s, максимум %s, пик %d операций/с; известно %d из %d размеров (%d байт).",
		ioOperationLabel(operation.Operation),
		displayUnknown(operation.Source, operation.Owner),
		operation.Count,
		operation.Failures,
		formatMicroseconds(operation.P95DurationUS),
		formatMicroseconds(operation.MaxDurationUS),
		operation.PeakOperationsPerSecond,
		operation.KnownByteOperations,
		operation.Count,
		operation.Bytes,
	)
	detectorID := "io.operation_pressure"
	impactSummary := "Рост задержки, нагрузки на хранилище и конкуренция за I/O"
	action := "Сократить критический путь, объединить мелкие обращения и ограничить объём одной операции"
	var mainThreadBlockedMS *uint64
	if operation.MainThread {
		detectorID = "io.main_thread"
		impactSummary = "Блокировка ввода, пропуски кадров, крупная аллокация и рост риска ANR"
		action = "Перенести целостную операцию с главного потока; чтение/запись выполнять порциями только внутри фоновой задачи"
		mainThreadBlockedMS = nonZeroU64Ptr(totalMS)
	}
	b.add(ProblemFinding{
		DetectorID:      detectorID,
		DetectorVersion: b.cfg.Version, Category: ProblemCategoryIO, Subcategory: subcategory,
		Status: "observed", Confidence: confidence, ConfidenceReasons: reasons, Title: title,
		WhatHappened: whatHappened, Where: where,
		Why: ProblemWhy{
			ClaimLevel: "linked",
			Summary:    "Типизированное событие атомарно связывает целостную I/O-операцию, поток, стабильное место вызова и контекст приложения.",
		},
		Impact:    []string{impactSummary},
		Evidence:  evidence,
		Frequency: &ProblemFrequency{Count: operation.Count, RatePerSec: &peakRate},
		Cost: &ProblemCost{
			MainThreadBlockedMS: mainThreadBlockedMS,
			Bytes:               nonZeroU64Ptr(operation.Bytes),
		},
		PriorityBreakdown: priority(
			impact, magnitude, exposure, locationBreadth(where), compound,
			"задержка и блокировка файловых операций", "длительность, ошибки и объём", "число и пиковая частота", "место вызова и контекст", "сочетание сигналов",
		),
		Recommendations: []ProblemRecommendation{{
			Action:       action,
			Rationale:    "Стабильное место вызова уже локализовано; снижение числа, объёма и длительности обращений уменьшает измеренную нагрузку на хранилище.",
			Verification: "Повторить тот же сценарий и сравнить границу верхних 5%, максимум, ошибки, известный объём и точный пик операций за скользящую секунду.",
		}},
		Limitations: limits,
		Drilldowns:  []ProblemDrilldown{{Label: "Подробно о файловых операциях", Anchor: "io-analysis", Filter: operation.Source}},
	})
}
