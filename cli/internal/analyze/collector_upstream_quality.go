package analyze

import (
	"fmt"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

// addUpstreamIncompleteness reports gaps whose size cannot be inferred from transport counters.
func (c *collector) addUpstreamIncompleteness(counters map[uint64]uint64, addReason func(string, string)) bool {
	upstreamDrainIncomplete := counters[jhlog.QualityMetricFlushTimeout] > 0
	if upstreamDrainIncomplete {
		addReason("medium", "сброс объединённых метрик не завершён; объём несохранённых данных неизвестен")
	}
	for _, stage := range []struct {
		name  string
		label string
	}{
		{"metrics", "объединённых метрик"},
		{"hooks", "событий методов и логов"},
		{"graph", "графа вызовов"},
		{"writer", "буфера записи"},
		{"concurrent", "повторного одновременного исключения"},
	} {
		if attempts := c.counterValues["jankhunter.runtime.crash_flush.incomplete."+stage.name+".count"]; attempts > 0 {
			upstreamDrainIncomplete = true
			addReason("medium", fmt.Sprintf(
				"аварийное сохранение %s не завершено (попыток: %d); объём несохранённых данных неизвестен",
				stage.label, attempts,
			))
		}
	}
	if skipped := counters[jhlog.QualityRuntimeGraphGenerationSkippedEntryTotal]; skipped > 0 {
		upstreamDrainIncomplete = true
		addReason("medium", fmt.Sprintf("сбор графа временно приостановлен: пропущено входов в методы %d; число отсутствующих связей неизвестно", skipped))
	}
	if skipped := counters[jhlog.QualityRuntimeGraphStorageSkippedEntryTotal]; skipped > 0 {
		upstreamDrainIncomplete = true
		addReason("medium", fmt.Sprintf("исчерпан бюджет памяти сборщиков графа: пропущено входов в методы %d; число отсутствующих связей неизвестно", skipped))
	}
	for _, item := range []struct {
		id    uint64
		label string
	}{
		{jhlog.QualityAsyncCompletionInvalid, "недостоверные завершения"},
		{jhlog.QualityAsyncTokenCapacityRejected, "таблица операций заполнена"},
		{jhlog.QualityAsyncTokenIdExhausted, "идентификаторы операций исчерпаны"},
	} {
		if rejected := counters[item.id]; rejected > 0 {
			upstreamDrainIncomplete = true
			addReason("medium", fmt.Sprintf("полный учёт асинхронных операций не подтверждён: %s (%d); число отсутствующих событий неизвестно", item.label, rejected))
		}
	}
	if completing := counters[jhlog.QualityAsyncCompletionInProgressAtStop]; completing > 0 {
		upstreamDrainIncomplete = true
		addReason("medium", fmt.Sprintf("при финальном снимке ещё публиковались асинхронные завершения: %d; итог их доставки этим снимком не подтверждён", completing))
	}
	return upstreamDrainIncomplete
}
