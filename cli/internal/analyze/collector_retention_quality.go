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
		result.runtimeLoss = saturatingUint64Sum(
			result.runtimeLoss,
			quality[jhlog.EventQualityCounterID(jhlog.EventRetained, reason)],
		)
	}
	if result.runtimeLoss > 0 {
		result.runtimeMayBeIncomplete = true
		result.runtimeNotes = append(result.runtimeNotes, fmt.Sprintf("потеряно событий удержания: %d", result.runtimeLoss))
	}
	if watcherLoss := quality[jhlog.QualityObjectWatcherLimit]; watcherLoss > 0 {
		result.runtimeLoss = saturatingUint64Sum(result.runtimeLoss, watcherLoss)
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
		result.runtimeNotes = append(result.runtimeNotes, fmt.Sprintf("модуль записи отбросил события неизвестных типов: %d", dropped))
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
			"имена удержанного класса или держателя могли быть заменены служебной ссылкой из-за переполнения словаря",
		)
	}
	if c.heap != nil {
		result.heapNotesBySource = make(map[string][]string)
	}
	for _, diagnostic := range c.heap.effectiveDiagnostics() {
		if diagnostic.affectsGraphEvidence() {
			result.heapDegraded = true
			note := "HPROF: " + diagnostic.Message
			result.heapNotes = append(result.heapNotes, note)
			result.heapNotesBySource[diagnostic.Source] = append(result.heapNotesBySource[diagnostic.Source], note)
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
		{jhlog.QualityControlLaneFullTotal, "служебная очередь записи была заполнена"},
		{jhlog.QualityControlTimeoutTotal, "служебные команды записи завершились по таймауту"},
		{jhlog.QualityControlInterruptedTotal, "служебные команды записи были прерваны"},
		{jhlog.QualityWriterIOErrorTotal, "при записи возникли ошибки ввода-вывода"},
		{jhlog.QualityEventLostAfterIOTotal, "события потеряны после ошибки записи"},
		{jhlog.QualityEventLostAfterSizeLimitTotal, "события потеряны после достижения лимита файла сессии"},
		{jhlog.QualityEventLostAfterStorageBudget, "активный запуск исчерпал общий лимит .jhlog"},
		{jhlog.QualityDictionaryValueTruncated, "значения словаря были усечены"},
		{jhlog.QualityOversizedRecordTotal, "слишком крупные записи не поместились в блок"},
		{jhlog.QualityFailedChunkTotal, "блоки не удалось сохранить"},
		{jhlog.QualityRecoveryTotal, "модуль записи восстанавливался после ошибки"},
		{jhlog.QualityCloseTimeoutTotal, "закрытие записи завершилось по таймауту"},
		{jhlog.QualityMetricCardinalityLoss, "метрики потеряны из-за ограничения по числу разных ключей"},
		{jhlog.QualityInvalidMetric, "некорректные метрики отклонены"},
		{jhlog.QualityRuntimeGraphShutdownLoss, "граф вызовов не успел сохранить очередь при остановке"},
		{jhlog.QualityRuntimeGraphWriterRejectionLoss, "модуль записи не принял вызовы для графа"},
		{jhlog.QualityRuntimeGraphProducerCapacityLoss, "вызовы графа не приняты: исчерпана ёмкость буферов или время ожидания"},
		{jhlog.QualityRuntimeGraphStorageSkippedEntryTotal, "исчерпан бюджет памяти сборщиков графа; пропущено входов в методы, число отсутствующих связей неизвестно"},
		{jhlog.QualityRuntimeStackMismatch, "стек вызовов во время выполнения рассинхронизировался"},
		{jhlog.QualityHandlerContentionBypass, "ASM-хук Handler пропущен из-за одновременного доступа к реестру"},
		{jhlog.QualityRuntimeGraphDisabled, "граф вызовов явно отключён в настройках"},
		{jhlog.QualityRuntimeEventBufferCapacityLoss, "буфер событий методов и логов был заполнен"},
		{jhlog.QualityRuntimeEventRegistryCapacityLoss, "реестр буферов событий методов и логов был заполнен"},
		{jhlog.QualityMethodCounterCardinalityLoss, "счётчики методов достигли ограничения по числу разных ключей"},
		{jhlog.QualityRuntimeEventWriterRejectionLoss, "модуль записи отклонил batch событий методов и логов"},
		{jhlog.QualityLogSpamCardinalityLoss, "сборщик логов достиг ограничения по числу разных ключей"},
		{jhlog.QualityHandlerEntryLimit, "реестр Handler достиг лимита записей"},
		{jhlog.QualityHandlerWrapperLimit, "реестр Handler достиг лимита объектов-обёрток"},
		{jhlog.QualityHandlerPostContextUnavailable, "Handler: связь отправки с выполнением неизвестна; успешные post могут быть отменены; число таких post"},
		{jhlog.QualityAsyncCompletionStale, "отклонены завершения из предыдущей сессии; повторная потеря данных новой сессии не подразумевается"},
		{jhlog.QualityAsyncCompletionDuplicate, "отклонены повторные завершения уже учтённых операций"},
		{jhlog.QualityAsyncCompletionInvalid, "отклонены завершения без достоверного токена начала"},
		{jhlog.QualityAsyncTokenCapacityRejected, "не начат учёт асинхронных операций из-за заполнения таблицы"},
		{jhlog.QualityAsyncTokenIdExhausted, "не начат учёт асинхронных операций из-за исчерпания идентификаторов"},
		{jhlog.QualityAsyncCompletionFeatureDisabled, "завершения не записаны после отключения источника"},
		{jhlog.QualityAsyncUnfinishedHTTP, "HTTP-запросы не завершились к границе сессии; это наблюдение границы, а не число потерянных событий"},
		{jhlog.QualityAsyncUnfinishedDatabase, "SQL-вызовы не завершились к границе сессии; это наблюдение границы, а не число потерянных событий"},
		{jhlog.QualityAsyncUnfinishedWorker, "Worker не завершились к границе сессии; это наблюдение границы, а не число потерянных событий"},
		{jhlog.QualityAsyncUnfinishedDatabaseTransaction, "транзакции не завершились к границе сессии; это наблюдение границы, а не число потерянных событий"},
		{jhlog.QualityAsyncCompletionInProgressAtStop, "на границе сессии завершение уже обрабатывалось; результат записи не подтверждён в итоговом снимке"},
		{jhlog.QualityHTTPLegacyContextCompletion, "HTTP завершены с общим контекстом без регистрации начала; незавершённые запросы этого API не отслеживаются"},
		{jhlog.QualityLifecycleRegistryLimit, "реестр lifecycle-наблюдения достиг лимита объектов"},
		{jhlog.QualityObjectWatcherLimit, "наблюдатель удержания достиг лимита объектов"},
		{jhlog.QualityJankStatsHandleLimit, "реестр JankStats достиг лимита активных окон"},
		{jhlog.QualityMetricFlushTimeout, "объединённые метрики не успели сохраниться до таймаута"},
		{jhlog.QualityPreparedStatementRegistryEviction, "реестр prepared statement вытеснил активные записи из-за лимита ёмкости"},
		{jhlog.QualityPreparedStatementResolutionMiss, "execute-вызовы потеряли SQL-шаблон после вытеснения из реестра prepared statement"},
		{jhlog.QualityReceiverAsyncRegistryEviction, "реестр BroadcastReceiver.goAsync вытеснил незавершённые PendingResult из-за лимита ёмкости"},
		{jhlog.QualityReceiverAsyncResolutionMiss, "PendingResult.finish не удалось сопоставить с goAsync после вытеснения из реестра"},
	}
	if !exactAdmission {
		items = append(items, struct {
			id    uint64
			label string
		}{jhlog.QualityWriterAdmissionContentionTotal, "при записи с возможной потерей событие пропущено из-за конкуренции рабочих потоков"})
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
		{jhlog.QualityRuntimeHookInstrumentationFailure, "instrumentation_hook", "evidence_loss", "добавленный ASM-хук завершился внутренней ошибкой"},
		{jhlog.QualityRuntimeHookAsyncWrapperFailure, "async_wrapper", "evidence_loss", "обёртка Runnable, Callable или coroutine не записала подтверждающие данные"},
		{jhlog.QualityRuntimeHookLifecycleFailure, "runtime_lifecycle", "evidence_loss", "запуск, остановка или сброс буфера завершились ошибкой"},
		{jhlog.QualityRuntimeHookCollectorFailure, "collector", "evidence_loss", "сборщик подавил внутреннюю ошибку"},
		{jhlog.QualityRuntimeHookContextFailure, "context", "evidence_loss", "контекст экрана, операции или источника мог быть неполным"},
		{jhlog.QualityRuntimeHookSchedulerFailure, "scheduler", "evidence_loss", "служебная задача сбора не была выполнена штатно"},
		{jhlog.QualityJankStatsDependencyMissing, "jankstats_dependency_missing", "fallback", "AndroidX Metrics отсутствовал; использован резервный Choreographer"},
		{jhlog.QualityJankStatsInstallFailure, "jankstats_install", "fallback", "JankStats не запустился; использован резервный Choreographer"},
		{jhlog.QualityJankStatsFrameFailure, "jankstats_frame", "evidence_loss", "активный JankStats не смог прочитать данные кадра"},
		{jhlog.QualityJankStatsControlFailure, "jankstats_control", "evidence_loss", "не удалось переключить состояние активного JankStats"},
		{jhlog.QualityRuntimeHookUnclassifiedFailure, "unclassified", "evidence_loss", "источник внутренней ошибки определить не удалось"},
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
			Explanation: "снимок качества сбора не содержит разбивку этой части ошибок по причинам",
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
