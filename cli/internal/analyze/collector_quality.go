package analyze

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func (c *collector) finalizeCollectionQuality() {
	if len(c.streamResults) == 0 {
		return
	}
	quality := CollectionQuality{
		Level:                   "high",
		Complete:                true,
		ChainValid:              true,
		ExactAdmission:          true,
		ProcessScopeConsistent:  true,
		RunCohortConsistent:     true,
		CounterInvariantsValid:  true,
		QualityProgressionValid: true,
		RuntimeGraphEnabled:     true,
		ChainIssues:             append([]string(nil), c.chainIssues...),
	}
	type processScopeConfig struct {
		scope                     jhlog.ProcessScope
		allowedCount              uint64
		fingerprint               string
		expectedCount             uint64
		expectedFingerprint       string
		rosterDeclarationComplete bool
	}
	processScopes := map[processScopeConfig]struct{}{}
	observedProcesses := map[string]struct{}{}
	runCohorts := map[jhlog.ID128]struct{}{}
	addReason := func(level, reason string) {
		quality.Complete = false
		quality.Level = lowerConfidenceLevel(quality.Level, level)
		quality.Reasons = append(quality.Reasons, reason)
	}
	addNotice := func(notice string) {
		quality.Complete = false
		quality.Notices = append(quality.Notices, notice)
	}

	for _, result := range c.streamResults {
		segmentDamaged := false
		if !result.Header.RunID.IsZero() {
			runCohorts[result.Header.RunID] = struct{}{}
		}
		if processName := strings.TrimSpace(result.Header.ProcessName); processName != "" {
			observedProcesses[processName] = struct{}{}
		}
		if result.Header.RequiredFeatures&jhlog.FeatureProcessScope == 0 {
			processScopes[processScopeConfig{}] = struct{}{}
			addReason("low", fmt.Sprintf(
				"сегмент %s не указывает область процессов для сбора; полноту охвата подтвердить невозможно",
				result.Source,
			))
		} else {
			processScopes[processScopeConfig{
				scope:                     result.Header.ProcessScope,
				allowedCount:              result.Header.AllowedProcessCount,
				fingerprint:               hex.EncodeToString(result.Header.ProcessScopeFingerprint),
				expectedCount:             result.Header.ExpectedProcessCount,
				expectedFingerprint:       hex.EncodeToString(result.Header.ExpectedProcessFingerprint),
				rosterDeclarationComplete: result.Header.ProcessRosterDeclarationComplete,
			}] = struct{}{}
		}
		if result.Header.RequiredFeatures&jhlog.FeatureExactEventAdmission == 0 {
			quality.ExactAdmission = false
			addReason("medium", fmt.Sprintf(
				"сегмент %s собран без гарантированной записи принятых событий; при конкуренции потоков возможны потери",
				result.Source,
			))
		}
		if result.Header.RunID.IsZero() || result.Header.ProcessInstanceID.IsZero() || result.Header.SessionID.IsZero() {
			quality.ChainValid = false
			addReason("medium", fmt.Sprintf("идентификаторы сегмента %s неполны, поэтому принадлежность к сессии не подтверждена", result.Source))
		}
		if result.Sealed && result.Status == jhlog.SegmentStatusClosedClean {
			quality.SealedSegments++
		} else {
			quality.UnsealedSegments++
			switch result.Status {
			case jhlog.SegmentStatusOpenWithTail, jhlog.SegmentStatusCorrupt:
				segmentDamaged = true
				addReason("low", fmt.Sprintf("сегмент %s не запечатан и имеет статус %s (хвост %d байт)", result.Source, result.Status, result.TailBytes))
			case jhlog.SegmentStatusOpenClean:
				addNotice(fmt.Sprintf(
					"активная сессия %s прочитана до последнего сохранённого блока; итоговая отметка появится после завершения записи",
					result.Source,
				))
			default:
				segmentDamaged = true
				addReason("medium", fmt.Sprintf("сегмент %s не содержит итоговую отметку завершения (статус %s)", result.Source, result.Status))
			}
		}
		if result.LatestQuality == nil {
			quality.SegmentsWithoutQuality++
			addReason("medium", fmt.Sprintf("сегмент %s не содержит снимок качества сбора", result.Source))
		} else {
			quality.SegmentsWithQuality++
		}
		if result.SegmentEnd != nil {
			switch result.SegmentEnd.Reason {
			case jhlog.SegmentEndIOError:
				segmentDamaged = true
				addReason("low", fmt.Sprintf("сегмент %s завершён из-за ошибки ввода-вывода", result.Source))
			case jhlog.SegmentEndSizeLimit:
				segmentDamaged = true
				addReason("medium", sizeLimitCollectionReason(result.Source))
			case jhlog.SegmentEndStorageBudget:
				segmentDamaged = true
				addReason("medium", fmt.Sprintf(
					"сегмент %s закрыт после исчерпания общего лимита .jhlog; последующие события не собирались",
					result.Source,
				))
			case jhlog.SegmentEndNormal, jhlog.SegmentEndShutdown, jhlog.SegmentEndRotation:
			default:
				segmentDamaged = true
				addReason("low", fmt.Sprintf("сегмент %s завершен с неизвестной причиной %d", result.Source, uint64(result.SegmentEnd.Reason)))
			}
		}
		if segmentDamaged {
			quality.DamagedSegments++
		}
	}
	quality.RunCohortCount = uint64(len(runCohorts))
	if len(runCohorts) != 1 {
		quality.RunCohortConsistent = false
		addReason("low", fmt.Sprintf(
			"входные сегменты относятся к %d разным запускам приложения; список процессов нельзя объединять между запусками",
			len(runCohorts),
		))
	}

	switch len(processScopes) {
	case 1:
		for scope := range processScopes {
			quality.ProcessScope = scope.scope.String()
			quality.AllowedProcessCount = scope.allowedCount
			quality.ProcessScopeFingerprint = scope.fingerprint
			quality.ExpectedProcessCount = scope.expectedCount
			quality.ExpectedProcessFingerprint = scope.expectedFingerprint
			quality.ProcessRosterDeclarationComplete = scope.rosterDeclarationComplete
			quality.ObservedProcessCount = uint64(len(observedProcesses))
			if scope.scope == jhlog.ProcessScopeUnknown {
				quality.ProcessScopeConsistent = false
			}
			quality.AllProcessesConfigured = scope.scope == jhlog.ProcessScopeAll
			if !scope.rosterDeclarationComplete {
				addReason("low", "во время работы не удалось получить полный список процессов из AndroidManifest")
			} else {
				observedNames := make([]string, 0, len(observedProcesses))
				for processName := range observedProcesses {
					observedNames = append(observedNames, processName)
				}
				observedFingerprint := hex.EncodeToString(jhlog.ProcessRosterFingerprint(observedNames))
				quality.ProcessRosterComplete = quality.RunCohortConsistent &&
					uint64(len(observedProcesses)) == scope.expectedCount &&
					observedFingerprint == scope.expectedFingerprint
				if !quality.RunCohortConsistent {
					addReason("low", "состав процессов не доказан: процессы принадлежат разным группам запусков")
				} else if !quality.ProcessRosterComplete {
					observedCount := uint64(len(observedProcesses))
					verb := "наблюдаются"
					if observedCount%10 == 1 && observedCount%100 != 11 {
						verb = "наблюдается"
					}
					addNotice(fmt.Sprintf(
						"%s %s из %d указанных в области сбора; отсутствующие процессы могли не запускаться либо их сегменты не были переданы, поэтому это неопределённость охвата, а не доказанная потеря",
						verb,
						russianCountUint64(observedCount, "процесс", "процесса", "процессов"),
						scope.expectedCount,
					))
				}
			}
			switch scope.scope {
			case jhlog.ProcessScopeMainOnly:
				quality.Notices = append(
					quality.Notices,
					"сбор намеренно ограничен главным процессом; полнота относится только к нему",
				)
			case jhlog.ProcessScopeAllowlist:
				quality.Notices = append(quality.Notices, fmt.Sprintf(
					"сбор намеренно ограничен списком из %d процессов; полнота относится только к ним",
					scope.allowedCount,
				))
			}
		}
	case 0:
		quality.ProcessScope = jhlog.ProcessScopeUnknown.String()
		quality.ProcessScopeConsistent = false
		addReason("low", "во входных сегментах не указана область процессов для сбора")
	default:
		quality.ProcessScope = "mixed"
		quality.ProcessScopeConsistent = false
		addReason("low", "входные сегменты используют разные области процессов или разные списки разрешённых процессов")
	}

	if len(c.chainIssues) > 0 {
		quality.ChainValid = false
		for _, issue := range c.chainIssues {
			addReason("low", issue)
		}
	}
	for _, issue := range qualityProgressionIssues(c.streamResults) {
		quality.ChainValid = false
		quality.QualityProgressionValid = false
		quality.ChainIssues = append(quality.ChainIssues, issue)
		addReason("low", issue)
	}

	counters := c.latestQualityTotals()
	quality.AsyncLifecycle = asyncLifecycleQuality(counters)
	if posts := counters[jhlog.QualityHandlerPostContextUnavailable]; posts > 0 {
		quality.AsyncAttribution = &AsyncAttributionQuality{Status: "unknown", HandlerPostsWithoutContext: posts}
	}
	quality.AcceptedEvents = counters[jhlog.QualityAcceptedEventTotal]
	quality.WrittenEvents = counters[jhlog.QualityWrittenEventTotal]
	quality.ReportedCommittedChunks = counters[jhlog.QualityCommittedChunkTotal]
	for _, result := range c.streamResults {
		quality.DecodedCommittedChunks = saturatingUint64Sum(
			quality.DecodedCommittedChunks,
			uint64(result.CommittedChunks),
		)
		quality.DecodedRuntimeGraphCalls = saturatingUint64Sum(
			quality.DecodedRuntimeGraphCalls,
			result.RuntimeGraphLogicalCalls,
		)
	}
	quality.RuntimeGraphInputEvents = counters[jhlog.QualityRuntimeGraphInputTotal]
	quality.RuntimeGraphEmittedEvents = counters[jhlog.QualityRuntimeGraphEmittedTotal]
	quality.RuntimeGraphStackMismatches = counters[jhlog.QualityRuntimeStackMismatch]
	quality.RuntimeGraphEnabled = counters[jhlog.QualityRuntimeGraphDisabled] == 0
	quality.ArchiveEvictedRuns = counters[jhlog.QualityArchiveEvictedRunTotal]
	quality.ArchiveEvictedSegments = counters[jhlog.QualityArchiveEvictedSegmentTotal]
	quality.ArchiveEvictedBytes = counters[jhlog.QualityArchiveEvictedBytesTotal]
	if quality.ArchiveEvictedRuns > 0 {
		addNotice(fmt.Sprintf(
			"циклическое хранение освободило %d байт: удалено %d завершённых запусков (%d сегментов); текущий запуск сохранён целиком",
			quality.ArchiveEvictedBytes,
			quality.ArchiveEvictedRuns,
			quality.ArchiveEvictedSegments,
		))
	}
	quality.RuntimeGraphCompletenessRatio = 1
	if !quality.RuntimeGraphEnabled {
		quality.RuntimeGraphCompletenessRatio = 0
		if quality.RuntimeGraphInputEvents > 0 || quality.RuntimeGraphEmittedEvents > 0 || quality.DecodedRuntimeGraphCalls > 0 {
			addReason("low", fmt.Sprintf(
				"граф вызовов отмечен отключённым, но счётчики полученных, переданных и прочитанных вызовов равны %d/%d/%d; конфигурация противоречит данным",
				quality.RuntimeGraphInputEvents,
				quality.RuntimeGraphEmittedEvents,
				quality.DecodedRuntimeGraphCalls,
			))
		} else {
			quality.Notices = append(quality.Notices, fmt.Sprintf(
				"граф вызовов отключён в %d снимках качества сбора и не влияет на оценку надёжности",
				counters[jhlog.QualityRuntimeGraphDisabled],
			))
		}
	} else if quality.RuntimeGraphInputEvents > 0 {
		quality.RuntimeGraphCompletenessRatio =
			float64(quality.DecodedRuntimeGraphCalls) / float64(quality.RuntimeGraphInputEvents)
		if quality.RuntimeGraphEmittedEvents > quality.RuntimeGraphInputEvents {
			quality.CounterInvariantsValid = false
			addReason("low", fmt.Sprintf(
				"ошибка счётчиков графа вызовов: передано %d вызовов при %d полученных",
				quality.RuntimeGraphEmittedEvents,
				quality.RuntimeGraphInputEvents,
			))
		}
		if quality.DecodedRuntimeGraphCalls > quality.RuntimeGraphInputEvents {
			quality.RuntimeGraphCompletenessRatio = 0
			quality.CounterInvariantsValid = false
			addReason("low", fmt.Sprintf(
				"ошибка счётчиков графа вызовов: прочитано %d логических вызовов при %d полученных",
				quality.DecodedRuntimeGraphCalls,
				quality.RuntimeGraphInputEvents,
			))
		} else if quality.RuntimeGraphCompletenessRatio < 1 {
			level := "medium"
			if quality.RuntimeGraphCompletenessRatio < 0.99 {
				level = "low"
			}
			addReason(level, fmt.Sprintf(
				"полнота графа вызовов %.2f%% (%d из %d логических вызовов)",
				quality.RuntimeGraphCompletenessRatio*100,
				quality.DecodedRuntimeGraphCalls,
				quality.RuntimeGraphInputEvents,
			))
		}
	} else if quality.RuntimeGraphEmittedEvents > 0 || quality.DecodedRuntimeGraphCalls > 0 {
		quality.RuntimeGraphCompletenessRatio = 0
		quality.CounterInvariantsValid = false
		addReason("low", fmt.Sprintf(
			"ошибка счётчиков графа вызовов: передано %d и прочитано %d вызовов, хотя получено 0",
			quality.RuntimeGraphEmittedEvents,
			quality.DecodedRuntimeGraphCalls,
		))
	}
	queueFullLoss := transportEventLoss(counters, jhlog.QualityLossQueueFull)
	notAcceptingLoss := transportEventLoss(counters, jhlog.QualityLossNotAccepting)
	quality.AdmissionContentionLostEvents = transportEventLoss(
		counters,
		jhlog.QualityLossAdmissionContention,
	)
	quality.PreAdmissionLostEvents = saturatingUint64Sum(
		queueFullLoss,
		notAcceptingLoss,
		quality.AdmissionContentionLostEvents,
	)
	postAdmissionCounters := transportEventLoss(
		counters,
		jhlog.QualityLossIOLost,
		jhlog.QualityLossOversized,
		jhlog.QualityLossSizeLimit,
		jhlog.QualityLossStorageBudget,
	)
	acceptedGap := uint64(0)
	if quality.AcceptedEvents > quality.WrittenEvents {
		acceptedGap = quality.AcceptedEvents - quality.WrittenEvents
	} else if quality.WrittenEvents > quality.AcceptedEvents {
		quality.CounterInvariantsValid = false
		addReason("low", fmt.Sprintf(
			"ошибка счётчиков записи: записано %d событий при %d принятых",
			quality.WrittenEvents,
			quality.AcceptedEvents,
		))
	}
	if acceptedGap > postAdmissionCounters {
		postAdmissionCounters = acceptedGap
	}
	quality.PostAdmissionLostEvents = postAdmissionCounters
	quality.KnownLostEvents = saturatingUint64Sum(
		quality.PreAdmissionLostEvents,
		quality.PostAdmissionLostEvents,
	)
	quality.WriterBackpressureCount = counters[jhlog.QualityWriterBackpressureCount]
	quality.WriterBackpressureNanos = counters[jhlog.QualityWriterBackpressureNanos]
	quality.RuntimeGraphBackpressureCount = counters[jhlog.QualityRuntimeGraphBackpressureCount]
	quality.RuntimeGraphBackpressureNanos = counters[jhlog.QualityRuntimeGraphBackpressureNanos]
	quality.RuntimeGraphProducerCapacityLoss = counters[jhlog.QualityRuntimeGraphProducerCapacityLoss]
	quality.RuntimeEventBackpressureCount = counters[jhlog.QualityRuntimeEventBackpressureCount]
	quality.RuntimeEventBackpressureNanos = counters[jhlog.QualityRuntimeEventBackpressureNanos]
	quality.RuntimeHookFailures = counters[jhlog.QualityRuntimeHookFailureTotal]
	var classifiedRuntimeHookFailures uint64
	quality.RuntimeHookFailureDetails, quality.CriticalRuntimeHookFailures, classifiedRuntimeHookFailures =
		runtimeHookFailureDetails(counters)
	if classifiedRuntimeHookFailures > quality.RuntimeHookFailures {
		quality.CounterInvariantsValid = false
		addReason("low", fmt.Sprintf(
			"сумма ошибок ASM-хуков по причинам (%d) превышает общий счётчик ошибок (%d)",
			classifiedRuntimeHookFailures,
			quality.RuntimeHookFailures,
		))
	}
	if quality.ExactAdmission && counters[jhlog.QualityWriterAdmissionContentionTotal] > 0 {
		quality.Notices = append(quality.Notices, fmt.Sprintf(
			"при строгой записи поток ожидал доступ к очереди %d раз (%s суммарно); принято %d, записано %d, до принятия потеряно %s",
			counters[jhlog.QualityWriterAdmissionContentionTotal],
			formatDurationNanos(quality.WriterBackpressureNanos),
			quality.AcceptedEvents,
			quality.WrittenEvents,
			russianCountUint64(quality.PreAdmissionLostEvents, "событие", "события", "событий"),
		))
	}
	if quality.RuntimeGraphBackpressureCount > 0 {
		quality.Notices = append(quality.Notices, fmt.Sprintf(
			"граф вызовов ожидал свободную страницу буфера %d раз (%s суммарно по рабочим потокам)",
			quality.RuntimeGraphBackpressureCount,
			formatDurationNanos(quality.RuntimeGraphBackpressureNanos),
		))
	}
	if quality.RuntimeEventBackpressureCount > 0 {
		quality.Notices = append(quality.Notices, fmt.Sprintf(
			"очередь методов и логов ожидала свободный буфер %d раз (%s суммарно по рабочим потокам)",
			quality.RuntimeEventBackpressureCount,
			formatDurationNanos(quality.RuntimeEventBackpressureNanos),
		))
	}
	if quality.KnownLostEvents > 0 {
		level := "medium"
		denominator := saturatingUint64Sum(quality.WrittenEvents, quality.KnownLostEvents)
		if denominator == 0 || float64(quality.KnownLostEvents)/float64(denominator) >= 0.01 {
			level = "low"
		}
		addReason(level, "снимки качества сбора подтверждают потерю как минимум "+
			russianCountUint64(quality.KnownLostEvents, "событие", "события", "событий"))
	}
	if c.summary.DataRecordCount > 0 && quality.AcceptedEvents == 0 && quality.WrittenEvents == 0 {
		quality.CounterInvariantsValid = false
		addReason("low", fmt.Sprintf(
			"прочитано %d записей данных, но счётчиков принятых и записанных событий нет",
			c.summary.DataRecordCount,
		))
	}
	if quality.UnsealedSegments == 0 && quality.SegmentsWithoutQuality == 0 {
		if quality.RuntimeGraphEmittedEvents != quality.DecodedRuntimeGraphCalls {
			quality.CounterInvariantsValid = false
			addReason("low", fmt.Sprintf(
				"модуль записи сообщает %d сохранённых вызовов, но прочитано %d",
				quality.RuntimeGraphEmittedEvents,
				quality.DecodedRuntimeGraphCalls,
			))
		}
		if quality.WrittenEvents != c.summary.DataRecordCount {
			quality.CounterInvariantsValid = false
			addReason("low", fmt.Sprintf(
				"модуль записи сообщает %d сохранённых событий, но прочитано %d записей данных",
				quality.WrittenEvents,
				c.summary.DataRecordCount,
			))
		}
		if quality.ReportedCommittedChunks != quality.DecodedCommittedChunks {
			quality.CounterInvariantsValid = false
			addReason("low", fmt.Sprintf(
				"модуль записи сообщает %d подтверждённых блоков, но прочитано %d",
				quality.ReportedCommittedChunks,
				quality.DecodedCommittedChunks,
			))
		}
	}
	if counters[jhlog.QualityWriterIOErrorTotal] > 0 || counters[jhlog.QualityFailedChunkTotal] > 0 {
		parts := make([]string, 0, 2)
		if ioErrors := counters[jhlog.QualityWriterIOErrorTotal]; ioErrors > 0 {
			parts = append(parts, russianCountUint64(ioErrors, "ошибка ввода-вывода", "ошибки ввода-вывода", "ошибок ввода-вывода"))
		}
		if failedChunks := counters[jhlog.QualityFailedChunkTotal]; failedChunks > 0 {
			parts = append(parts, russianCountUint64(failedChunks, "несохранённый блок", "несохранённых блока", "несохранённых блоков"))
		}
		addReason("low", "при записи зафиксировано: "+strings.Join(parts, ", "))
	}
	controlFailures := saturatingUint64Sum(
		counters[jhlog.QualityControlLaneFullTotal],
		counters[jhlog.QualityControlTimeoutTotal],
		counters[jhlog.QualityControlInterruptedTotal],
		counters[jhlog.QualityCloseTimeoutTotal],
	)
	quality.ControlFailures = controlFailures
	if controlFailures > 0 {
		addReason("medium", "служебный канал записи: "+
			russianCountUint64(controlFailures, "сбой или таймаут", "сбоя или таймаута", "сбоев или таймаутов"))
	}
	quality.DictionaryOverflow = counters[jhlog.QualityDictionaryOverflowTotal]
	if uint64(c.dictionaryOverflow) > quality.DictionaryOverflow {
		quality.DictionaryOverflow = uint64(c.dictionaryOverflow)
	}
	quality.DictionaryTruncated = counters[jhlog.QualityDictionaryValueTruncated]
	if quality.DictionaryOverflow > 0 || quality.DictionaryTruncated > 0 {
		addReason("medium", fmt.Sprintf(
			"словарь строк переполнен: заменено служебной ссылкой %d значений, обрезано %d значений",
			quality.DictionaryOverflow,
			quality.DictionaryTruncated,
		))
	}
	boundedEvidenceLoss := saturatingUint64Sum(
		counters[jhlog.QualityMetricCardinalityLoss],
		counters[jhlog.QualityRuntimeGraphShutdownLoss],
		counters[jhlog.QualityRuntimeGraphWriterRejectionLoss],
		counters[jhlog.QualityRuntimeGraphProducerCapacityLoss],
		counters[jhlog.QualityRuntimeStackMismatch],
		counters[jhlog.QualityHandlerContentionBypass],
		counters[jhlog.QualityRuntimeEventGenerationCapacityLoss],
		counters[jhlog.QualityRuntimeGraphGenerationCapacityLoss],
		counters[jhlog.QualityRuntimeEventBufferCapacityLoss],
		counters[jhlog.QualityRuntimeEventRegistryCapacityLoss],
		counters[jhlog.QualityMethodCounterCardinalityLoss],
		counters[jhlog.QualityRuntimeEventWriterRejectionLoss],
		counters[jhlog.QualityLogSpamCardinalityLoss],
		counters[jhlog.QualityHandlerEntryLimit],
		counters[jhlog.QualityHandlerWrapperLimit],
		counters[jhlog.QualityLifecycleRegistryLimit],
		counters[jhlog.QualityObjectWatcherLimit],
		counters[jhlog.QualityJankStatsHandleLimit],
	)
	quality.BoundedEvidenceLoss = boundedEvidenceLoss
	graphEvidenceLoss := saturatingUint64Sum(
		counters[jhlog.QualityRuntimeGraphShutdownLoss],
		counters[jhlog.QualityRuntimeGraphWriterRejectionLoss],
		counters[jhlog.QualityRuntimeGraphProducerCapacityLoss],
		counters[jhlog.QualityRuntimeGraphGenerationCapacityLoss],
	)
	graphEvidenceLoss = saturatingUint64Sum(graphEvidenceLoss, quality.RuntimeGraphStackMismatches)
	if boundedEvidenceLoss > graphEvidenceLoss {
		quality.OtherEvidenceLoss = boundedEvidenceLoss - graphEvidenceLoss
	}
	if boundedEvidenceLoss > 0 {
		addReason("medium", "сборщики во время выполнения не сохранили "+
			russianCountUint64(boundedEvidenceLoss, "подтверждающую запись", "подтверждающие записи", "подтверждающих записей"))
	}
	availabilityFailures := saturatingUint64Sum(
		counters[jhlog.QualityJankStatsDependencyMissing],
		counters[jhlog.QualityJankStatsInstallFailure],
	)
	if availabilityFailures > 0 {
		addNotice(fmt.Sprintf(
			"JankStats был недоступен %d раз до запуска; использован резервный Choreographer, события не потеряны",
			availabilityFailures,
		))
	}
	if quality.CriticalRuntimeHookFailures > 0 {
		addReason("low", fmt.Sprintf(
			"защитные границы сбора подавили %d внутренних сбоев; часть подтверждающих данных могла потеряться. Причины: %s",
			quality.CriticalRuntimeHookFailures,
			runtimeHookFailureReasonSummary(quality.RuntimeHookFailureDetails, true),
		))
	}
	upstreamDrainIncomplete := c.addUpstreamIncompleteness(counters, addReason)
	quality.ChainIssues = uniqueStrings(quality.ChainIssues)
	quality.Notices = uniqueStrings(quality.Notices)
	quality.Reasons = uniqueStrings(quality.Reasons)
	quality.DiagnosticCompletenessPercent, quality.DiagnosticCompletenessComponents =
		collectionDiagnosticCompleteness(quality)
	if upstreamDrainIncomplete {
		quality.DiagnosticCompletenessPercent = UnknownDiagnosticCompleteness
		quality.DiagnosticCompletenessComponents = nil
	}
	quality.DiagnosticCompletenessModel = diagnosticCompletenessModel
	quality.DiagnosticCompletenessLevel, quality.DiagnosticCompletenessExplanation =
		describeDiagnosticCompleteness(
			quality.DiagnosticCompletenessPercent,
			quality.DiagnosticCompletenessComponents,
		)
	c.summary.CollectionQuality = quality
	for _, reason := range quality.Reasons {
		c.summary.Warnings = append(c.summary.Warnings, "Качество сбора: "+reason+".")
	}
}
