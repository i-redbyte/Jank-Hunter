package analyze

import "fmt"

const EvidenceQualitySchemaVersion = "jankhunter.evidence-quality/v1"

const (
	EvidenceQualityComplete      = "complete"
	EvidenceQualityDegraded      = "degraded"
	EvidenceQualityInsufficient  = "insufficient"
	EvidenceQualityNotMeasured   = "not_measured"
	EvidenceQualityNotCalibrated = "not_calibrated"
)

// BuildEvidenceQualityVector keeps independent evidence limitations visible. It deliberately does
// not collapse transport, acquisition, process, temporal, semantic and estimator quality into a
// probability-looking percentage.
func BuildEvidenceQualityVector(summary Summary) EvidenceQualityVector {
	dimensions := []EvidenceQualityDimension{
		transportEvidenceQuality(summary.CollectionQuality),
		acquisitionEvidenceQuality(summary.CategoryCoverage),
		processEvidenceQuality(summary.CollectionQuality),
		temporalEvidenceQuality(summary.CollectionQuality, summary.LogCount),
		semanticEvidenceQuality(summary),
		analysisEvidenceQuality(summary.AnalysisInputs),
		{
			ID:          "estimator_calibration",
			Label:       "Проверка правил на эталонных данных",
			Status:      EvidenceQualityNotCalibrated,
			Explanation: "Приоритеты рассчитаны по открытым инженерным правилам. Пока нет независимого набора размеченных прогонов, на котором можно проверить точность этих правил.",
		},
	}

	overall := EvidenceQualityComplete
	for _, dimension := range dimensions {
		if dimension.Status == EvidenceQualityInsufficient {
			overall = EvidenceQualityInsufficient
			break
		}
		if dimension.Status != EvidenceQualityComplete {
			overall = EvidenceQualityDegraded
		}
	}
	headline := "Диагностических данных достаточно для проверенных фактов; ограничения перечислены по измерениям."
	switch overall {
	case EvidenceQualityInsufficient:
		headline = "Диагностических данных недостаточно для общего вывода; смотрите непроверенные категории и причины."
	case EvidenceQualityDegraded:
		headline = "Диагностические данные пригодны с ограничениями; отдельные выводы требуют повторного измерения или проверки правил."
	}
	return EvidenceQualityVector{
		SchemaVersion: EvidenceQualitySchemaVersion,
		Overall:       overall,
		Headline:      headline,
		Dimensions:    dimensions,
	}
}

func transportEvidenceQuality(quality CollectionQuality) EvidenceQualityDimension {
	result := EvidenceQualityDimension{ID: "transport", Label: "Транспорт и целостность", Status: EvidenceQualityComplete}
	switch {
	case !quality.ExactAdmission:
		result.Status = EvidenceQualityInsufficient
		result.Explanation = "Выбранный режим доставки не позволяет подтвердить полноту событий до очереди."
	case !quality.ChainValid || !quality.CounterInvariantsValid || !quality.QualityProgressionValid || quality.DamagedSegments > 0:
		result.Status = EvidenceQualityInsufficient
		result.Explanation = "Цепочка сегментов, зафиксированные блоки или согласованность счётчиков не подтверждены."
	case quality.KnownLostEvents > 0 || quality.ControlFailures > 0 || quality.BoundedEvidenceLoss > 0:
		result.Status = EvidenceQualityDegraded
		result.Explanation = "Часть диагностических данных недоступна; количественные оценки могут быть занижены."
	default:
		result.Explanation = "Зафиксированная часть журнала, цепочка сегментов и счётчики согласованы; известных пропусков событий нет."
	}
	return result
}

func acquisitionEvidenceQuality(coverage []CategoryCoverage) EvidenceQualityDimension {
	result := EvidenceQualityDimension{ID: "acquisition", Label: "Покрытие категорий", Status: EvidenceQualityComplete}
	if len(coverage) == 0 {
		result.Status = EvidenceQualityInsufficient
		result.Explanation = "Покрытие категорий анализа не рассчитано."
		return result
	}
	missing, degraded := 0, 0
	for _, item := range coverage {
		switch item.Status {
		case "not_measured", "insufficient_data":
			missing++
		case "collection_degraded":
			degraded++
		}
	}
	switch {
	case missing > 0:
		result.Status = EvidenceQualityInsufficient
		result.Explanation = evidenceCategoryCount(missing, "категорий не измерены или имеют недостаточную выборку")
	case degraded > 0:
		result.Status = EvidenceQualityDegraded
		result.Explanation = evidenceCategoryCount(degraded, "категорий ограничены качеством сбора")
	default:
		result.Explanation = "Все категории имеют прямой источник данных и достаточную выборку."
	}
	return result
}

func evidenceCategoryCount(count int, explanation string) string {
	return formatInt(count) + " " + explanation + "."
}

func processEvidenceQuality(quality CollectionQuality) EvidenceQualityDimension {
	result := EvidenceQualityDimension{ID: "process_coverage", Label: "Охват процессов"}
	switch {
	case quality.ProcessRosterComplete:
		result.Status = EvidenceQualityComplete
		result.Explanation = "Все объявленные процессы представлены в прогоне."
	case quality.ObservedProcessCount > 0:
		result.Status = EvidenceQualityDegraded
		result.Explanation = formatInt64(quality.ObservedProcessCount) + " из " + formatInt64(quality.ExpectedProcessCount) + " объявленных процессов наблюдаются; отсутствие остальных не считается нулём."
	default:
		result.Status = EvidenceQualityInsufficient
		result.Explanation = "Список процессов не подтверждён."
	}
	return result
}

func temporalEvidenceQuality(quality CollectionQuality, logCount int) EvidenceQualityDimension {
	result := EvidenceQualityDimension{ID: "temporal_coverage", Label: "Завершённость интервала"}
	switch {
	case logCount == 0:
		result.Status = EvidenceQualityInsufficient
		result.Explanation = "Нет входных сегментов."
	case quality.UnsealedSegments > 0:
		result.Status = EvidenceQualityDegraded
		result.Explanation = formatInt(quality.UnsealedSegments) + " сегментов не завершены; доступна только зафиксированная часть без подтверждения конца интервала."
	case quality.SealedSegments > 0:
		result.Status = EvidenceQualityComplete
		result.Explanation = "Все анализируемые сегменты завершены и содержат запись о конце интервала."
	default:
		result.Status = EvidenceQualityInsufficient
		result.Explanation = "Завершённость сегментов не подтверждена."
	}
	return result
}

func semanticEvidenceQuality(summary Summary) EvidenceQualityDimension {
	result := EvidenceQualityDimension{ID: "sensor_semantics", Label: "Качество данных UI"}
	if summary.UIFrames == 0 {
		result.Status = EvidenceQualityNotMeasured
		result.Explanation = "UI-кадры не записаны; вывод о плавности невозможен."
		return result
	}
	if len(summary.Screens) == 0 {
		result.Status = EvidenceQualityDegraded
		result.Explanation = "Общее число UI-кадров известно, но нет разбивки по экранам, целевого времени кадра или распределения длительности."
		return result
	}
	allJankStats := true
	allMergeable := true
	allDeadlines := true
	for _, screen := range summary.Screens {
		allJankStats = allJankStats && screen.FrameSource == "jankstats"
		allMergeable = allMergeable && screen.FrameDistributionState == "mergeable_histogram_v2"
		allDeadlines = allDeadlines && screen.FrameDeadlineStatus == "consistent" && screen.FrameDeadlineUS > 0
	}
	if allJankStats && allMergeable && allDeadlines {
		result.Status = EvidenceQualityComplete
		result.Explanation = "Источник JankStats, целевое время кадра и распределение длительности записаны для всех UI-экранов."
		return result
	}
	result.Status = EvidenceQualityDegraded
	result.Explanation = "Для части UI-кадров используется Choreographer, смешаны источники или неполно распределение длительности; выводы помечены ограничениями."
	return result
}

func analysisEvidenceQuality(inputs AnalysisInputCompleteness) EvidenceQualityDimension {
	result := EvidenceQualityDimension{ID: "analysis_inputs", Label: "Аналитические входы"}
	switch {
	case inputs.Complete:
		result.Status = EvidenceQualityComplete
		result.Explanation = "Данные выполнения и обязательные артефакты сборки доступны и согласованы."
	case inputs.Status == "":
		result.Status = EvidenceQualityNotMeasured
		result.Explanation = "Полнота аналитических входов не рассчитана."
	case inputs.RuntimeEvidence:
		result.Status = EvidenceQualityDegraded
		result.Explanation = "Данные выполнения доступны, но отсутствует часть артефактов сборки; статические пути и диагностика инструментирования могут быть неполными."
	default:
		result.Status = EvidenceQualityInsufficient
		result.Explanation = "Нет обязательных данных выполнения или артефактов сборки; часть причин нельзя локализовать до кода."
	}
	return result
}

func formatInt(value int) string {
	return fmt.Sprint(value)
}

func formatInt64(value uint64) string {
	return fmt.Sprint(value)
}
