package analyze

import (
	"fmt"
	"strings"
)

const (
	androidMetricServiceFailureRate            = "Service failure rate"
	androidMetricServiceTimeoutRate            = "Service timeout rate"
	androidMetricServiceSlowCallbackRate       = "Service slow callback rate"
	androidMetricReceiverFailureRate           = "Receiver failure rate"
	androidMetricReceiverAsyncDeadlineRiskRate = "Receiver async deadline risk rate"
	androidMetricReceiverSyncSlowRate          = "Receiver sync slow rate"
	androidMetricBinderClientP95               = "Binder client p95"
	androidMetricBinderSlowMainThreadRate      = "Binder slow main-thread rate"
	androidMetricBinderFailureRate             = "Binder failure rate"
	androidMetricBinderUnhandledRate           = "Binder unhandled rate"
	androidMetricBinderCorrelationCoverage     = "Binder correlation coverage"
	androidMetricHiddenForegroundServiceShare  = "Hidden foreground-service share"
)

func compareAndroidComponentAnalysis(baseline, candidate Summary) AndroidComponentComparison {
	before := baseline.AndroidComponents
	after := candidate.AndroidComponents
	if before == nil || after == nil || !before.Available || !after.Available {
		return AndroidComponentComparison{
			Note: "Структурированные события компонентов Android и IPC отсутствуют хотя бы в одном прогоне.",
		}
	}
	sameScope := baseline.CollectionQuality.ProcessScope != "" &&
		baseline.CollectionQuality.ProcessScope == candidate.CollectionQuality.ProcessScope
	result := AndroidComponentComparison{
		Comparable: sameScope,
		Partial:    before.Partial || after.Partial,
	}
	switch {
	case !sameScope:
		result.Note = fmt.Sprintf(
			"Набор процессов различается (%s и %s): метрики компонентов и IPC не сравниваются.",
			firstNonEmpty(baseline.CollectionQuality.ProcessScope, "не определён"),
			firstNonEmpty(candidate.CollectionQuality.ProcessScope, "не определён"),
		)
	case before.Partial || after.Partial:
		result.Note = "Разрешён частичный анализ: локальные метрики жизненного цикла и транзакций сравниваются, а полнота межпроцессной цепочки — нет. " +
			strings.Join(uniqueStrings(append(append([]string{}, before.PartialReasons...), after.PartialReasons...)), "; ")
	default:
		result.Note = "Сопоставлены одинаковые полные наборы процессов; цепочка Binder остаётся вероятной, а не точной связью."
	}

	beforeSyncReceivers := saturatingSub(before.Receivers.Completed, before.Receivers.AsyncCompleted)
	afterSyncReceivers := saturatingSub(after.Receivers.Completed, after.Receivers.AsyncCompleted)
	beforeBinderEvents := saturatingUint64Sum(before.Binder.ClientCalls, before.Binder.ServerCalls)
	afterBinderEvents := saturatingUint64Sum(after.Binder.ClientCalls, after.Binder.ServerCalls)
	result.Metrics = []Delta{
		androidRateDelta(androidMetricServiceFailureRate, before.Services.Failures, before.Services.Callbacks, after.Services.Failures, after.Services.Callbacks, "ошибки / завершённые методы Service"),
		androidRateDelta(androidMetricServiceTimeoutRate, before.Services.Timeouts, before.Services.Callbacks, after.Services.Timeouts, after.Services.Callbacks, "onTimeout / завершённые методы Service"),
		androidRateDelta(androidMetricServiceSlowCallbackRate, before.Services.SlowCallbacks, before.Services.Callbacks, after.Services.SlowCallbacks, after.Services.Callbacks, "методы ≥100 мс / завершённые методы Service"),
		androidRateDelta(androidMetricReceiverFailureRate, before.Receivers.Failures, before.Receivers.Completed, after.Receivers.Failures, after.Receivers.Completed, "ошибки / завершённые цепочки BroadcastReceiver"),
		androidRateDelta(androidMetricReceiverAsyncDeadlineRiskRate, before.Receivers.AsyncDeadlineRisks, before.Receivers.AsyncCompleted, after.Receivers.AsyncDeadlineRisks, after.Receivers.AsyncCompleted, "асинхронные цепочки ≥9 с / завершённые асинхронные цепочки"),
		androidRateDelta(androidMetricReceiverSyncSlowRate, before.Receivers.SyncSlowCallbacks, beforeSyncReceivers, after.Receivers.SyncSlowCallbacks, afterSyncReceivers, "синхронный onReceive ≥10 мс / завершённые синхронные вызовы"),
		androidDurationDelta(androidMetricBinderClientP95, before.Binder.P95ClientDurationUS, after.Binder.P95ClientDurationUS, before.Binder.ClientCalls, after.Binder.ClientCalls),
		androidRateDelta(androidMetricBinderSlowMainThreadRate, before.Binder.SlowMainThreadCalls, before.Binder.MainThreadClientCalls, after.Binder.SlowMainThreadCalls, after.Binder.MainThreadClientCalls, "клиентские вызовы на главном потоке ≥16 мс / все клиентские вызовы на главном потоке"),
		androidRateDelta(androidMetricBinderFailureRate, before.Binder.Failures, beforeBinderEvents, after.Binder.Failures, afterBinderEvents, "failure events / typed Binder events"),
		androidRateDelta(androidMetricBinderUnhandledRate, before.Binder.Unhandled, beforeBinderEvents, after.Binder.Unhandled, afterBinderEvents, "необработанные серверные события / структурированные события Binder"),
		androidQualityRateDelta(androidMetricBinderCorrelationCoverage, before.Binder.CorrelatedPairs, before.Binder.ClientCalls, after.Binder.CorrelatedPairs, after.Binder.ClientCalls, "однозначно связанные пары / клиентские вызовы"),
		androidInformationalRateDelta(androidMetricHiddenForegroundServiceShare, before.ProcessState.HiddenForegroundServiceSamples, before.ProcessState.Samples, after.ProcessState.HiddenForegroundServiceSamples, after.ProcessState.Samples, "FGS importance with hidden UI / process-state samples"),
	}
	confidence := confidence(baseline, candidate)
	for index := range result.Metrics {
		metric := &result.Metrics[index]
		metric.Confidence = confidence
		if !sameScope {
			*metric = markDeltaUnavailable(*metric, metric.SampleSize, metric.SampleSize, result.Note)
			metric.Confidence = "low"
			continue
		}
		if metric.Name == androidMetricBinderCorrelationCoverage && (before.Partial || after.Partial) {
			*metric = markDeltaUnavailable(*metric, before.Binder.ClientCalls, after.Binder.ClientCalls, "нужен полный одинаковый набор процессов в обоих прогонах")
			metric.Confidence = "low"
			continue
		}
		metric.Severity = adjustedSeverity(metric.Severity, confidence, metric.SampleSize)
	}
	return result
}

func androidDurationDelta(name string, before, after, beforeSamples, afterSamples uint64) Delta {
	result := observedDelta(
		name,
		before,
		after,
		"мкс",
		true,
		beforeSamples,
		afterSamples,
		"Клиентские вызовы Binder не зафиксированы",
	)
	result.ComparisonNote = appendAndroidComparisonNote(
		result.ComparisonNote,
		"полная длительность структурированного клиентского вызова Binder; время процессора сервера отдельно не измеряется",
	)
	return result
}

func androidRateDelta(name string, beforePart, beforeTotal, afterPart, afterTotal uint64, note string) Delta {
	result := observedDeltaFloat(
		name,
		androidPercent(beforePart, beforeTotal),
		androidPercent(afterPart, afterTotal),
		"п.п.",
		true,
		beforeTotal,
		afterTotal,
		"знаменатель метрики отсутствует",
	)
	result.ComparisonNote = appendAndroidComparisonNote(result.ComparisonNote, note)
	return result
}

func androidQualityRateDelta(name string, beforePart, beforeTotal, afterPart, afterTotal uint64, note string) Delta {
	result := observedDeltaFloat(
		name,
		androidPercent(beforePart, beforeTotal),
		androidPercent(afterPart, afterTotal),
		"%",
		false,
		beforeTotal,
		afterTotal,
		"клиентские вызовы отсутствуют",
	)
	result.ComparisonNote = appendAndroidComparisonNote(result.ComparisonNote, note)
	return result
}

func androidInformationalRateDelta(name string, beforePart, beforeTotal, afterPart, afterTotal uint64, note string) Delta {
	result := observedDeltaFloat(
		name,
		androidPercent(beforePart, beforeTotal),
		androidPercent(afterPart, afterTotal),
		"%",
		true,
		beforeTotal,
		afterTotal,
		"process-state samples отсутствуют",
	)
	result.Severity = "ok"
	result.RegressionAbs = 0
	result.RegressionPct = 0
	result.ComparisonNote = appendAndroidComparisonNote(
		result.ComparisonNote,
		note+"; показатель описывает сценарий и сам по себе не является регрессией",
	)
	return result
}

func appendAndroidComparisonNote(existing, detail string) string {
	if existing == "" {
		return detail
	}
	return existing + "; " + detail
}

func androidPercent(part, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return float64(part) * 100 / float64(total)
}
