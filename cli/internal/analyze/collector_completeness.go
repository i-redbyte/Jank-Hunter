package analyze

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func collectionDiagnosticCompleteness(quality CollectionQuality) (float64, []DiagnosticCompletenessComponent) {
	components := make([]DiagnosticCompletenessComponent, 0, 4)
	activeWeight := 0.0
	earnedWeight := 0.0
	appendComponent := func(id, label string, weight, coverage float64, excluded bool, explanation string) {
		coverage = math.Max(0, math.Min(1, coverage))
		earned := 0.0
		missing := 0.0
		if !excluded {
			earned = weight * coverage
			missing = weight - earned
			activeWeight += weight
			earnedWeight += earned
		}
		components = append(components, DiagnosticCompletenessComponent{
			ID:              id,
			Label:           label,
			Weight:          weight,
			Excluded:        excluded,
			CoveragePercent: roundDiagnosticCompleteness(coverage * 100),
			EarnedPoints:    roundDiagnosticCompleteness(earned),
			MissingPoints:   roundDiagnosticCompleteness(missing),
			Explanation:     explanation,
		})
	}

	transportCoverage := 1.0
	transportExplanation := "Все принятые события записаны; известных пропусков журнала нет"
	if !quality.ExactAdmission {
		transportCoverage = 0
		transportExplanation = "Режим доставки не позволяет подтвердить полноту журнала; количественные оценки могут быть занижены"
	} else if quality.KnownLostEvents > 0 {
		transportTotal := saturatingUint64Sum(quality.WrittenEvents, quality.KnownLostEvents)
		if transportTotal == 0 {
			transportCoverage = 0
		} else {
			transportCoverage = float64(quality.WrittenEvents) / float64(transportTotal)
		}
		transportExplanation = "Часть событий журнала недоступна; количественные оценки могут быть занижены"
	}
	appendComponent("transport", "Доставка событий", 40, transportCoverage, false, transportExplanation)

	var runtimeCoverage float64
	runtimeDenominator := saturatingUint64Sum(
		quality.RuntimeGraphInputEvents,
		quality.RuntimeGraphStackMismatches,
	)
	runtimeExplanation := "Все записанные связи вызовов доступны"
	if !quality.RuntimeGraphEnabled {
		runtimeCoverage = 1
		runtimeExplanation = "Граф вызовов во время выполнения отключён настройками и исключён из расчёта индекса"
	} else if runtimeDenominator == 0 {
		runtimeCoverage = 1
		runtimeExplanation = "Сбор связей вызовов включён, но вызовов в этом сценарии не зарегистрировано"
	} else {
		runtimeCoverage = float64(quality.DecodedRuntimeGraphCalls) / float64(runtimeDenominator)
		if runtimeCoverage < 1 || quality.RuntimeGraphStackMismatches > 0 {
			runtimeExplanation = "Часть связей вызовов недоступна; цепочки вызовов и количество повторов могут быть неполными"
		}
	}
	appendComponent(
		"runtime_graph",
		"Граф вызовов во время выполнения",
		20,
		runtimeCoverage,
		!quality.RuntimeGraphEnabled,
		runtimeExplanation,
	)

	processCoverage := 0.0
	processExplanation := fmt.Sprintf(
		"записано %d из %d потенциальных процессов; AndroidManifest не доказывает, что остальные процессы запускались",
		quality.ObservedProcessCount,
		quality.ExpectedProcessCount,
	)
	if quality.ProcessRosterComplete {
		processCoverage = 1
		processExplanation = fmt.Sprintf(
			"подтверждены все %d ожидаемых процессов",
			quality.ExpectedProcessCount,
		)
	} else if quality.ProcessRosterDeclarationComplete && quality.RunCohortConsistent &&
		quality.ProcessScopeConsistent && quality.ExpectedProcessCount > 0 &&
		quality.ObservedProcessCount < quality.ExpectedProcessCount {
		processCoverage = float64(quality.ObservedProcessCount) / float64(quality.ExpectedProcessCount)
	} else if !quality.ProcessRosterDeclarationComplete {
		processExplanation = "AndroidManifest не позволил подтвердить полный список ожидаемых процессов"
	} else if !quality.RunCohortConsistent {
		processExplanation = "входные сегменты относятся к разным прогонам"
	} else if !quality.ProcessScopeConsistent {
		processExplanation = "настройки охвата процессов не согласованы между сегментами"
	} else {
		processExplanation = "количество или состав процессов не совпадает с объявленным списком"
	}
	appendComponent("process_roster", "Охват процессов", 20, processCoverage, false, processExplanation)

	integrityIssues := make([]string, 0, 9)
	if !quality.ChainValid {
		integrityIssues = append(integrityIssues, "цепочка сегментов")
	}
	if !quality.CounterInvariantsValid {
		integrityIssues = append(integrityIssues, "инварианты счётчиков")
	}
	if !quality.QualityProgressionValid {
		integrityIssues = append(integrityIssues, "последовательность показателей качества")
	}
	if quality.DamagedSegments > 0 {
		integrityIssues = append(integrityIssues, fmt.Sprintf("повреждённые/аварийные сегменты=%d", quality.DamagedSegments))
	}
	if quality.SegmentsWithoutQuality > 0 {
		integrityIssues = append(integrityIssues, "сегменты без сведений о качестве")
	}
	if quality.CriticalRuntimeHookFailures > 0 {
		integrityIssues = append(integrityIssues, "ошибки перехвата, способные скрыть часть событий")
	}
	if quality.DictionaryOverflow > 0 || quality.DictionaryTruncated > 0 {
		integrityIssues = append(integrityIssues, "неполный словарь имён")
	}
	if quality.ControlFailures > 0 {
		integrityIssues = append(integrityIssues, "ошибки служебных записей журнала")
	}
	if quality.OtherEvidenceLoss > 0 {
		integrityIssues = append(integrityIssues, "часть диагностических данных недоступна")
	}
	integrityCoverage := 1.0
	integrityExplanation := "цепочка сегментов, схема и счётчики согласованы"
	if len(integrityIssues) > 0 {
		integrityCoverage = 0
		integrityExplanation = "не доказаны: " + strings.Join(integrityIssues, "; ")
	}
	appendComponent("integrity", "Целостность доказательств", 20, integrityCoverage, false, integrityExplanation)
	if activeWeight == 0 {
		return 100, components
	}
	return roundDiagnosticCompleteness(earnedWeight * 100 / activeWeight), components
}

func describeDiagnosticCompleteness(score float64, components []DiagnosticCompletenessComponent) (string, string) {
	level, explanation := diagnosticCompletenessTier(score)
	missing := make([]string, 0, len(components))
	excluded := make([]string, 0, len(components))
	for _, component := range components {
		if component.Excluded {
			excluded = append(excluded, component.Label)
			continue
		}
		if component.MissingPoints > 0 {
			missing = append(missing, fmt.Sprintf(
				"%s: −%.2f из %.0f (%s)",
				component.Label,
				component.MissingPoints,
				component.Weight,
				component.Explanation,
			))
		}
	}
	if len(missing) == 0 {
		explanation += " Недостающих баллов нет."
	} else {
		explanation += " Почему не 100%: " + strings.Join(missing, "; ") + "."
	}
	if len(excluded) > 0 {
		explanation += " Отключены настройками и не входят в расчёт: " + strings.Join(excluded, ", ") + "."
	}
	return level, explanation
}

func diagnosticCompletenessTier(score float64) (string, string) {
	switch {
	case score >= 95:
		return "excellent", "Максимальная полнота: индекс 95–100%; все активные источники диагностических данных практически полностью подтверждены."
	case score >= 85:
		return "high", "Высокая полнота: индекс 85–94,99%; основные диагностические данные подтверждены, оставшиеся ограничения явно перечислены."
	case score >= 65:
		return "sufficient", "Достаточная полнота: индекс 65–84,99%; выводы применимы с учётом перечисленных ограничений."
	case score >= 40:
		return "limited", "Ограниченная полнота: индекс 40–64,99%; существенная часть активных диагностических данных не подтверждена."
	default:
		return "low", "Низкая полнота: индекс ниже 40%; отчёт нельзя использовать для уверенных выводов без повторного сбора."
	}
}

func roundDiagnosticCompleteness(value float64) float64 {
	return math.Round(value*100) / 100
}

func qualityProgressionIssues(results []jhlog.StreamResult) []string {
	chains := map[string][]jhlog.StreamResult{}
	for _, result := range results {
		chains[qualityIdentityKey(result)] = append(chains[qualityIdentityKey(result)], result)
	}
	var issues []string
	for _, chain := range chains {
		sort.Slice(chain, func(i, j int) bool {
			return chain[i].Header.SegmentIndex < chain[j].Header.SegmentIndex
		})
		for index := 1; index < len(chain); index++ {
			previous := chain[index-1]
			current := chain[index]
			if previous.LatestQuality == nil || current.LatestQuality == nil {
				continue
			}
			if err := jhlog.ValidateQualityProgression(*previous.LatestQuality, *current.LatestQuality); err != nil {
				issues = append(issues, fmt.Sprintf(
					"session %x имеет немонотонные quality snapshots между segment %d и %d: %v",
					current.Header.SessionID,
					previous.Header.SegmentIndex,
					current.Header.SegmentIndex,
					err,
				))
			}
		}
	}
	sort.Strings(issues)
	return uniqueStrings(issues)
}

func lowerConfidenceLevel(current, candidate string) string {
	if confidenceRank(candidate) < confidenceRank(current) {
		return candidate
	}
	return current
}

func saturatingUint64Sum(values ...uint64) uint64 {
	total := uint64(0)
	for _, value := range values {
		if math.MaxUint64-total < value {
			return math.MaxUint64
		}
		total += value
	}
	return total
}

// Runtime-call batches use logical invocation counts while accepted/written transport counters
// use encoded event rows. Excluding EventRuntimeCall here prevents the same graph loss from being
// charged once to transport and again to the dedicated runtime-graph completeness component.
func transportEventLoss(counters map[uint64]uint64, reasons ...jhlog.QualityLossReason) uint64 {
	var total uint64
	for eventType := jhlog.EventSession; eventType <= jhlog.EventBinderTransaction; eventType++ {
		if eventType == jhlog.EventRuntimeCall || !eventType.IsSemanticData() {
			continue
		}
		for _, reason := range reasons {
			total = saturatingUint64Sum(total, counters[jhlog.EventQualityCounterID(eventType, reason)])
		}
	}
	return total
}
