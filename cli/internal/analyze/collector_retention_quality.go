package analyze

import (
	"fmt"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func (c *collector) retentionDataQuality() retentionDataQuality {
	quality := c.latestQualityTotals()
	result := retentionDataQuality{}
	for _, reason := range []jhlog.QualityLossReason{
		jhlog.QualityLossQueueFull,
		jhlog.QualityLossNotAccepting,
		jhlog.QualityLossIOLost,
		jhlog.QualityLossOversized,
		jhlog.QualityLossSizeLimit,
		jhlog.QualityLossAdmissionContention,
		jhlog.QualityLossStorageBudget,
	} {
		result.runtimeLoss += quality[jhlog.EventQualityCounterID(jhlog.EventRetained, reason)]
	}
	if result.runtimeLoss > 0 {
		result.runtimeMayBeIncomplete = true
		result.runtimeNotes = append(result.runtimeNotes, fmt.Sprintf("потеряно retained-событий: %d", result.runtimeLoss))
	}
	if watcherLoss := quality[jhlog.QualityObjectWatcherLimit]; watcherLoss > 0 {
		result.runtimeLoss += watcherLoss
		result.runtimeMayBeIncomplete = true
		result.runtimeNotes = append(
			result.runtimeNotes,
			fmt.Sprintf("наблюдатель удержания отклонил объектов из-за лимита: %d", watcherLoss),
		)
	}
	if lifecycleLoss := quality[jhlog.QualityLifecycleRegistryLimit]; lifecycleLoss > 0 {
		result.runtimeMayBeIncomplete = true
		result.runtimeNotes = append(
			result.runtimeNotes,
			fmt.Sprintf("реестр lifecycle-наблюдения достиг лимита: %d", lifecycleLoss),
		)
	}
	if dropped := c.counterValues["jankhunter.events_dropped.count"]; dropped > 0 {
		result.runtimeMayBeIncomplete = true
		result.runtimeNotes = append(result.runtimeNotes, fmt.Sprintf("writer отбросил события неизвестных типов: %d", dropped))
	}
	for _, segment := range c.summary.CollectionSegments {
		if segment.Status == string(jhlog.SegmentStatusOpenWithTail) ||
			segment.Status == string(jhlog.SegmentStatusCorrupt) {
			result.runtimeMayBeIncomplete = true
			result.runtimeNotes = append(
				result.runtimeNotes,
				fmt.Sprintf("сегмент %s имеет статус %s и хвост %d байт", segment.Source, segment.Status, segment.TailBytes),
			)
		}
	}
	dictionaryLoss := quality[jhlog.QualityDictionaryOverflowTotal]
	if dictionaryLoss > 0 || c.dictionaryOverflow > 0 {
		result.dictionaryDegraded = true
		result.dictionaryNotes = append(
			result.dictionaryNotes,
			"имена retained-класса или держателя могли быть заменены overflow-ссылкой",
		)
	}
	if c.heap != nil && len(c.heap.Warnings) > 0 {
		result.heapDegraded = true
		for _, warning := range c.heap.Warnings {
			result.heapNotes = append(result.heapNotes, "HPROF: "+warning)
		}
	}
	result.runtimeNotes = uniqueStrings(result.runtimeNotes)
	result.dictionaryNotes = uniqueStrings(result.dictionaryNotes)
	result.heapNotes = uniqueStrings(result.heapNotes)
	return result
}

func qualityCounterWarnings(counters map[uint64]uint64, exactAdmission bool) []string {
	items := []struct {
		id    uint64
		label string
	}{
		{jhlog.QualityQueueFullTotal, "очередь событий была заполнена"},
		{jhlog.QualityNotAcceptingTotal, "события пришли после остановки приёма"},
		{jhlog.QualityControlLaneFullTotal, "служебная очередь writer была заполнена"},
		{jhlog.QualityControlTimeoutTotal, "служебные команды writer завершились по таймауту"},
		{jhlog.QualityControlInterruptedTotal, "служебные команды writer были прерваны"},
		{jhlog.QualityWriterIOErrorTotal, "writer встретил ошибки ввода-вывода"},
		{jhlog.QualityEventLostAfterIOTotal, "события потеряны после ошибки записи"},
		{jhlog.QualityEventLostAfterSizeLimitTotal, "события потеряны после достижения лимита session-файла"},
		{jhlog.QualityEventLostAfterStorageBudget, "storage_budget_exhausted: активный запуск исчерпал общий бюджет .jhlog"},
		{jhlog.QualityDictionaryValueTruncated, "значения словаря были усечены"},
		{jhlog.QualityOversizedRecordTotal, "слишком крупные записи не поместились в чанк"},
		{jhlog.QualityFailedChunkTotal, "чанки не удалось зафиксировать"},
		{jhlog.QualityRecoveryTotal, "writer выполнял восстановление после ошибки"},
		{jhlog.QualityCloseTimeoutTotal, "закрытие writer завершилось по таймауту"},
		{jhlog.QualityMetricCardinalityLoss, "метрики потеряны из-за лимита кардинальности"},
		{jhlog.QualityInvalidMetric, "некорректные метрики отклонены"},
		{jhlog.QualityRuntimeGraphShutdownLoss, "runtime-граф не успел завершить drain при shutdown"},
		{jhlog.QualityRuntimeGraphWriterRejectionLoss, "writer не принял логические вызовы runtime-графа"},
		{jhlog.QualityRuntimeGraphProducerCapacityLoss, "потеряно логических вызовов runtime-графа после deadline ожидания свободной producer page"},
		{jhlog.QualityRuntimeStackMismatch, "runtime-стек вызовов рассинхронизировался"},
		{jhlog.QualityHandlerContentionBypass, "Handler instrumentation была обойдена из-за конкуренции registry"},
		{jhlog.QualityRuntimeGraphDisabled, "runtime-граф явно отключён конфигурацией"},
		{jhlog.QualityRuntimeEventBufferCapacityLoss, "producer buffer method/log events был заполнен"},
		{jhlog.QualityRuntimeEventRegistryCapacityLoss, "реестр producer buffers method/log events был заполнен"},
		{jhlog.QualityMethodCounterCardinalityLoss, "method counters достигли лимита кардинальности"},
		{jhlog.QualityRuntimeEventWriterRejectionLoss, "writer отклонил batch method/log events"},
		{jhlog.QualityLogSpamCardinalityLoss, "агрегатор логов достиг лимита кардинальности"},
		{jhlog.QualityHandlerEntryLimit, "реестр Handler достиг лимита записей"},
		{jhlog.QualityHandlerWrapperLimit, "реестр Handler достиг лимита wrapper-объектов"},
		{jhlog.QualityLifecycleRegistryLimit, "реестр lifecycle-наблюдения достиг лимита объектов"},
		{jhlog.QualityObjectWatcherLimit, "наблюдатель удержания достиг лимита объектов"},
		{jhlog.QualityJankStatsHandleLimit, "реестр JankStats достиг лимита активных окон"},
		{jhlog.QualityMetricFlushTimeout, "агрегированные метрики не успели попасть в writer до таймаута"},
		{jhlog.QualityPreparedStatementRegistryEviction, "реестр prepared statement вытеснил активные записи из-за лимита ёмкости"},
		{jhlog.QualityPreparedStatementResolutionMiss, "execute-вызовы потеряли SQL-шаблон после вытеснения из реестра prepared statement"},
		{jhlog.QualityReceiverAsyncRegistryEviction, "реестр BroadcastReceiver.goAsync вытеснил незавершённые PendingResult из-за лимита ёмкости"},
		{jhlog.QualityReceiverAsyncResolutionMiss, "PendingResult.finish не удалось сопоставить с goAsync после вытеснения из реестра"},
	}
	if !exactAdmission {
		items = append(items, struct {
			id    uint64
			label string
		}{jhlog.QualityWriterAdmissionContentionTotal, "BEST_EFFORT writer обошёл admission из-за конкуренции producers"})
	}
	warnings := make([]string, 0, len(items)+1)
	for _, item := range items {
		if value := counters[item.id]; value > 0 {
			warnings = append(warnings, fmt.Sprintf("Качество сбора: %s: %d.", item.label, value))
		}
	}
	accepted := counters[jhlog.QualityAcceptedEventTotal]
	written := counters[jhlog.QualityWrittenEventTotal]
	if accepted > written {
		warnings = append(warnings, fmt.Sprintf("Качество сбора: принято %d событий, но зафиксировано %d; разница: %d.", accepted, written, accepted-written))
	}
	return warnings
}

func runtimeHookFailureDetails(counters map[uint64]uint64) ([]RuntimeHookFailureDetail, uint64, uint64) {
	descriptors := []struct {
		id          uint64
		reason      string
		impact      string
		explanation string
	}{
		{jhlog.QualityRuntimeHookInstrumentationFailure, "instrumentation_hook", "evidence_loss", "инжектированный hook завершился через fail-open"},
		{jhlog.QualityRuntimeHookAsyncWrapperFailure, "async_wrapper", "evidence_loss", "обёртка Runnable, Callable или coroutine не записала evidence"},
		{jhlog.QualityRuntimeHookLifecycleFailure, "runtime_lifecycle", "evidence_loss", "операция запуска, остановки или flush runtime завершилась ошибкой"},
		{jhlog.QualityRuntimeHookCollectorFailure, "collector", "evidence_loss", "runtime collector подавил внутреннюю ошибку"},
		{jhlog.QualityRuntimeHookContextFailure, "context", "evidence_loss", "контекст экрана, операции или источника мог быть неполным"},
		{jhlog.QualityRuntimeHookSchedulerFailure, "scheduler", "evidence_loss", "служебная задача runtime не была выполнена штатно"},
		{jhlog.QualityJankStatsDependencyMissing, "jankstats_dependency_missing", "fallback", "AndroidX Metrics отсутствовал; использован Choreographer fallback"},
		{jhlog.QualityJankStatsInstallFailure, "jankstats_install", "fallback", "JankStats не установился; использован Choreographer fallback"},
		{jhlog.QualityJankStatsFrameFailure, "jankstats_frame", "evidence_loss", "активный JankStats не смог декодировать frame evidence"},
		{jhlog.QualityJankStatsControlFailure, "jankstats_control", "evidence_loss", "не удалось переключить состояние активного JankStats"},
		{jhlog.QualityRuntimeHookUnclassifiedFailure, "unclassified", "evidence_loss", "источник fail-open ошибки не был классифицирован"},
	}
	details := make([]RuntimeHookFailureDetail, 0, len(descriptors)+1)
	classified := uint64(0)
	critical := uint64(0)
	for _, descriptor := range descriptors {
		count := counters[descriptor.id]
		if count == 0 {
			continue
		}
		classified = saturatingUint64Sum(classified, count)
		if descriptor.impact == "evidence_loss" {
			critical = saturatingUint64Sum(critical, count)
		}
		details = append(details, RuntimeHookFailureDetail{
			Reason: descriptor.reason, Count: count, Impact: descriptor.impact, Explanation: descriptor.explanation,
		})
	}
	total := counters[jhlog.QualityRuntimeHookFailureTotal]
	if total > classified {
		unclassified := total - classified
		critical = saturatingUint64Sum(critical, unclassified)
		details = append(details, RuntimeHookFailureDetail{
			Reason: "unclassified", Count: unclassified, Impact: "evidence_loss",
			Explanation: "quality snapshot не содержит reason-coded разбивку для этой части ошибок",
		})
	}
	return details, critical, classified
}

func runtimeHookFailureReasonSummary(details []RuntimeHookFailureDetail, criticalOnly bool) string {
	parts := make([]string, 0, len(details))
	for _, detail := range details {
		if criticalOnly && detail.Impact != "evidence_loss" {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s=%d", detail.Reason, detail.Count))
	}
	if len(parts) == 0 {
		return "нет"
	}
	return strings.Join(parts, ", ")
}

func formatDurationNanos(value uint64) string {
	if value < 1_000 {
		return fmt.Sprintf("%d нс", value)
	}
	if value < 1_000_000 {
		return fmt.Sprintf("%.3f мкс", float64(value)/1_000)
	}
	return fmt.Sprintf("%.3f мс", float64(value)/1_000_000)
}
