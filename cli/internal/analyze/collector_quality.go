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
				"сегмент %s не содержит обязательный process scope; охват процессов подтвердить невозможно",
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
				"сегмент %s собран без EXACT admission; отсутствие потерь очереди нельзя гарантировать архитектурно",
				result.Source,
			))
		}
		if result.Header.RunID.IsZero() || result.Header.ProcessInstanceID.IsZero() || result.Header.SessionID.IsZero() {
			quality.ChainValid = false
			addReason("medium", fmt.Sprintf("identity сегмента %s неполна, поэтому принадлежность session не подтверждена", result.Source))
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
					"снимок активной сессии %s корректно прочитан до последнего зафиксированного чанка; FINAL seal появится после завершения runtime",
					result.Source,
				))
			default:
				segmentDamaged = true
				addReason("medium", fmt.Sprintf("сегмент %s не содержит FINAL seal (статус %s)", result.Source, result.Status))
			}
		}
		if result.LatestQuality == nil {
			quality.SegmentsWithoutQuality++
			addReason("medium", fmt.Sprintf("сегмент %s не содержит quality snapshot", result.Source))
		} else {
			quality.SegmentsWithQuality++
		}
		if result.SegmentEnd != nil {
			switch result.SegmentEnd.Reason {
			case jhlog.SegmentEndIOError:
				segmentDamaged = true
				addReason("low", fmt.Sprintf("сегмент %s завершен после ошибки ввода-вывода", result.Source))
			case jhlog.SegmentEndSizeLimit:
				segmentDamaged = true
				addReason("medium", sizeLimitCollectionReason(result.Source))
			case jhlog.SegmentEndStorageBudget:
				segmentDamaged = true
				addReason("medium", fmt.Sprintf(
					"сегмент %s запечатан с storage_budget_exhausted: активный запуск исчерпал общий бюджет .jhlog; последующие события не собирались",
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
			"входные сегменты относятся к %d разным запускам приложения; all-process roster нельзя объединять между запусками",
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
				addReason("low", "runtime не смог полностью объявить process roster из Android manifest")
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
					addNotice(fmt.Sprintf(
						"наблюдается %d процессов из %d указанных в области сбора; отсутствующие процессы могли не запускаться либо их сегменты не были переданы, поэтому это неопределённость охвата, а не доказанная потеря",
						len(observedProcesses),
						scope.expectedCount,
					))
				}
			}
			switch scope.scope {
			case jhlog.ProcessScopeMainOnly:
				quality.Notices = append(
					quality.Notices,
					"сбор намеренно ограничен main-процессом; полнота относится только к этому scope",
				)
			case jhlog.ProcessScopeAllowlist:
				quality.Notices = append(quality.Notices, fmt.Sprintf(
					"сбор намеренно ограничен allowlist из %d процессов; полнота относится только к этому scope",
					scope.allowedCount,
				))
			}
		}
	case 0:
		quality.ProcessScope = jhlog.ProcessScopeUnknown.String()
		quality.ProcessScopeConsistent = false
		addReason("low", "process scope отсутствует во всех входных сегментах")
	default:
		quality.ProcessScope = "mixed"
		quality.ProcessScopeConsistent = false
		addReason("low", "входные сегменты используют разные process scope или разные process allowlist")
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
			"циклическое хранение освободило %d байт: удалено %d завершённых запусков (%d сегментов); текущий run cohort сохранён целиком",
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
				"runtime-граф отмечен отключённым, но содержит input/emitted/decoded=%d/%d/%d; конфигурация и evidence противоречат друг другу",
				quality.RuntimeGraphInputEvents,
				quality.RuntimeGraphEmittedEvents,
				quality.DecodedRuntimeGraphCalls,
			))
		} else {
			quality.Notices = append(quality.Notices, fmt.Sprintf(
				"runtime-граф отключён конфигурацией в %d quality snapshot(s) и полностью исключён из индекса доверия",
				counters[jhlog.QualityRuntimeGraphDisabled],
			))
		}
	} else if quality.RuntimeGraphInputEvents > 0 {
		quality.RuntimeGraphCompletenessRatio =
			float64(quality.DecodedRuntimeGraphCalls) / float64(quality.RuntimeGraphInputEvents)
		if quality.RuntimeGraphEmittedEvents > quality.RuntimeGraphInputEvents {
			quality.CounterInvariantsValid = false
			addReason("low", fmt.Sprintf(
				"невозможное состояние runtime-графа: writer сообщает %d emitted при %d input",
				quality.RuntimeGraphEmittedEvents,
				quality.RuntimeGraphInputEvents,
			))
		}
		if quality.DecodedRuntimeGraphCalls > quality.RuntimeGraphInputEvents {
			quality.RuntimeGraphCompletenessRatio = 0
			quality.CounterInvariantsValid = false
			addReason("low", fmt.Sprintf(
				"невозможное состояние runtime-графа: декодировано %d логических вызовов при %d входных",
				quality.DecodedRuntimeGraphCalls,
				quality.RuntimeGraphInputEvents,
			))
		} else if quality.RuntimeGraphCompletenessRatio < 1 {
			level := "medium"
			if quality.RuntimeGraphCompletenessRatio < 0.99 {
				level = "low"
			}
			addReason(level, fmt.Sprintf(
				"полнота runtime-графа %.2f%% (%d из %d логических вызовов)",
				quality.RuntimeGraphCompletenessRatio*100,
				quality.DecodedRuntimeGraphCalls,
				quality.RuntimeGraphInputEvents,
			))
		}
	} else if quality.RuntimeGraphEmittedEvents > 0 || quality.DecodedRuntimeGraphCalls > 0 {
		quality.RuntimeGraphCompletenessRatio = 0
		quality.CounterInvariantsValid = false
		addReason("low", fmt.Sprintf(
			"невозможное состояние runtime-графа: reported=%d, decoded=%d при нулевом input counter",
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
			"невозможное состояние writer: записано %d событий при %d принятых",
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
			"reason-coded hook failures=%d превышают общий runtime_hook_failure_total=%d",
			classifiedRuntimeHookFailures,
			quality.RuntimeHookFailures,
		))
	}
	if quality.ExactAdmission && counters[jhlog.QualityWriterAdmissionContentionTotal] > 0 {
		quality.Notices = append(quality.Notices, fmt.Sprintf(
			"EXACT writer ожидал admission lock %d раз (%s суммарного backpressure); accepted/written=%d/%d, до admission потеряно %d событий",
			counters[jhlog.QualityWriterAdmissionContentionTotal],
			formatDurationNanos(quality.WriterBackpressureNanos),
			quality.AcceptedEvents,
			quality.WrittenEvents,
			quality.PreAdmissionLostEvents,
		))
	}
	if quality.RuntimeGraphBackpressureCount > 0 {
		quality.Notices = append(quality.Notices, fmt.Sprintf(
			"runtime-граф ожидал свободную producer page %d раз (%s суммарно по producer threads)",
			quality.RuntimeGraphBackpressureCount,
			formatDurationNanos(quality.RuntimeGraphBackpressureNanos),
		))
	}
	if quality.RuntimeEventBackpressureCount > 0 {
		quality.Notices = append(quality.Notices, fmt.Sprintf(
			"runtime method/log transport ожидал свободный buffer %d раз (%s суммарно по producer threads)",
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
		addReason(level, fmt.Sprintf("quality snapshots фиксируют потерю как минимум %d событий", quality.KnownLostEvents))
	}
	if c.summary.DataRecordCount > 0 && quality.AcceptedEvents == 0 && quality.WrittenEvents == 0 {
		quality.CounterInvariantsValid = false
		addReason("low", fmt.Sprintf(
			"%d data records присутствуют без accepted/written quality counters",
			c.summary.DataRecordCount,
		))
	}
	if quality.UnsealedSegments == 0 && quality.SegmentsWithoutQuality == 0 {
		if quality.RuntimeGraphEmittedEvents != quality.DecodedRuntimeGraphCalls {
			quality.CounterInvariantsValid = false
			addReason("low", fmt.Sprintf(
				"writer сообщает %d записанных runtime-вызовов, но декодировано %d",
				quality.RuntimeGraphEmittedEvents,
				quality.DecodedRuntimeGraphCalls,
			))
		}
		if quality.WrittenEvents != c.summary.DataRecordCount {
			quality.CounterInvariantsValid = false
			addReason("low", fmt.Sprintf(
				"writer сообщает %d записанных событий, но декодировано %d data records",
				quality.WrittenEvents,
				c.summary.DataRecordCount,
			))
		}
		if quality.ReportedCommittedChunks != quality.DecodedCommittedChunks {
			quality.CounterInvariantsValid = false
			addReason("low", fmt.Sprintf(
				"writer сообщает %d committed chunks, но декодировано %d",
				quality.ReportedCommittedChunks,
				quality.DecodedCommittedChunks,
			))
		}
	}
	if counters[jhlog.QualityWriterIOErrorTotal] > 0 || counters[jhlog.QualityFailedChunkTotal] > 0 {
		addReason("low", fmt.Sprintf(
			"writer сообщил ошибки I/O=%d и незаписанные чанки=%d",
			counters[jhlog.QualityWriterIOErrorTotal],
			counters[jhlog.QualityFailedChunkTotal],
		))
	}
	controlFailures := saturatingUint64Sum(
		counters[jhlog.QualityControlLaneFullTotal],
		counters[jhlog.QualityControlTimeoutTotal],
		counters[jhlog.QualityControlInterruptedTotal],
		counters[jhlog.QualityCloseTimeoutTotal],
	)
	quality.ControlFailures = controlFailures
	if controlFailures > 0 {
		addReason("medium", fmt.Sprintf("служебный канал writer сообщил %d сбоев или таймаутов", controlFailures))
	}
	quality.DictionaryOverflow = counters[jhlog.QualityDictionaryOverflowTotal]
	if uint64(c.dictionaryOverflow) > quality.DictionaryOverflow {
		quality.DictionaryOverflow = uint64(c.dictionaryOverflow)
	}
	quality.DictionaryTruncated = counters[jhlog.QualityDictionaryValueTruncated]
	if quality.DictionaryOverflow > 0 || quality.DictionaryTruncated > 0 {
		addReason("medium", fmt.Sprintf(
			"словарь деградировал: overflow=%d, truncated=%d",
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
		counters[jhlog.QualityMetricFlushTimeout],
	)
	quality.BoundedEvidenceLoss = boundedEvidenceLoss
	graphEvidenceLoss := saturatingUint64Sum(
		counters[jhlog.QualityRuntimeGraphShutdownLoss],
		counters[jhlog.QualityRuntimeGraphWriterRejectionLoss],
		counters[jhlog.QualityRuntimeGraphProducerCapacityLoss],
	)
	graphEvidenceLoss = saturatingUint64Sum(graphEvidenceLoss, quality.RuntimeGraphStackMismatches)
	if boundedEvidenceLoss > graphEvidenceLoss {
		quality.OtherEvidenceLoss = boundedEvidenceLoss - graphEvidenceLoss
	}
	if boundedEvidenceLoss > 0 {
		addReason("medium", fmt.Sprintf("runtime-подсистемы потеряли %d элементов evidence", boundedEvidenceLoss))
	}
	availabilityFailures := saturatingUint64Sum(
		counters[jhlog.QualityJankStatsDependencyMissing],
		counters[jhlog.QualityJankStatsInstallFailure],
	)
	if availabilityFailures > 0 {
		addNotice(fmt.Sprintf(
			"JankStats недоступен %d раз до активации; использован Choreographer fallback, транспорт событий не повреждён",
			availabilityFailures,
		))
	}
	if quality.CriticalRuntimeHookFailures > 0 {
		addReason("low", fmt.Sprintf(
			"fail-open границы runtime подавили %d сбоев, влияющих на evidence; причины: %s",
			quality.CriticalRuntimeHookFailures,
			runtimeHookFailureReasonSummary(quality.RuntimeHookFailureDetails, true),
		))
	}
	quality.ChainIssues = uniqueStrings(quality.ChainIssues)
	quality.Notices = uniqueStrings(quality.Notices)
	quality.Reasons = uniqueStrings(quality.Reasons)
	quality.DiagnosticCompletenessPercent, quality.DiagnosticCompletenessComponents =
		collectionDiagnosticCompleteness(quality)
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
