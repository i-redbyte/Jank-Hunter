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
	headline := "Evidence достаточно для проверенных фактов; ограничения перечислены по измерениям."
	switch overall {
	case EvidenceQualityInsufficient:
		headline = "Evidence недостаточно для общего clean-вывода; смотрите непроверенные категории и причины."
	case EvidenceQualityDegraded:
		headline = "Evidence пригодно с ограничениями; отдельные claims требуют повторного измерения или калибровки."
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
		result.Explanation = "BEST_EFFORT admission не позволяет доказать полноту событий до очереди."
	case !quality.ChainValid || !quality.CounterInvariantsValid || !quality.QualityProgressionValid || quality.DamagedSegments > 0:
		result.Status = EvidenceQualityInsufficient
		result.Explanation = "Digest chain, committed chunks или инварианты quality не подтверждены."
	case quality.KnownLostEvents > 0 || quality.ControlFailures > 0 || quality.BoundedEvidenceLoss > 0:
		result.Status = EvidenceQualityDegraded
		result.Explanation = "Сбор явно зарегистрировал потери payload или control/evidence."
	default:
		result.Explanation = "Committed prefix, chain и счётчики согласованы; известных потерь событий нет."
	}
	return result
}

func acquisitionEvidenceQuality(coverage []CategoryCoverage) EvidenceQualityDimension {
	result := EvidenceQualityDimension{ID: "acquisition", Label: "Покрытие collectors", Status: EvidenceQualityComplete}
	if len(coverage) == 0 {
		result.Status = EvidenceQualityInsufficient
		result.Explanation = "Detector coverage не рассчитан."
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
		result.Explanation = "Все категории имеют first-class источник и достаточную для detector выборку."
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
		result.Explanation = "Все объявленные процессы configured scope представлены в run."
	case quality.ObservedProcessCount > 0:
		result.Status = EvidenceQualityDegraded
		result.Explanation = formatInt64(quality.ObservedProcessCount) + " из " + formatInt64(quality.ExpectedProcessCount) + " объявленных процессов наблюдаются; отсутствие остальных не считается нулём."
	default:
		result.Status = EvidenceQualityInsufficient
		result.Explanation = "Process roster не подтверждён."
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
		result.Explanation = formatInt(quality.UnsealedSegments) + " сегментов не sealed; доступен только committed prefix без доказательства terminal completeness."
	case quality.SealedSegments > 0:
		result.Status = EvidenceQualityComplete
		result.Explanation = "Все анализируемые сегменты sealed и имеют terminal evidence."
	default:
		result.Status = EvidenceQualityInsufficient
		result.Explanation = "Terminal state сегментов не подтверждён."
	}
	return result
}

func semanticEvidenceQuality(summary Summary) EvidenceQualityDimension {
	result := EvidenceQualityDimension{ID: "sensor_semantics", Label: "Семантика UI sensor"}
	if summary.UIFrames == 0 {
		result.Status = EvidenceQualityNotMeasured
		result.Explanation = "UI frames не записаны; вывод о плавности невозможен."
		return result
	}
	if len(summary.Screens) == 0 {
		result.Status = EvidenceQualityDegraded
		result.Explanation = "UI frame totals есть, но screen-level source/deadline/distribution отсутствуют."
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
		result.Explanation = "JankStats source, frame deadline и mergeable frame-duration histogram явно записаны для всех UI-экранов."
		return result
	}
	result.Status = EvidenceQualityDegraded
	result.Explanation = "Часть UI evidence использует Choreographer/mixed source, смешанный deadline или неполную frame distribution; выводы помечены ограничениями."
	return result
}

func analysisEvidenceQuality(inputs AnalysisInputCompleteness) EvidenceQualityDimension {
	result := EvidenceQualityDimension{ID: "analysis_inputs", Label: "Аналитические входы"}
	switch {
	case inputs.Complete:
		result.Status = EvidenceQualityComplete
		result.Explanation = "Runtime evidence и обязательные build-time артефакты доступны и согласованы."
	case inputs.Status == "":
		result.Status = EvidenceQualityNotMeasured
		result.Explanation = "Полнота аналитических входов не рассчитана."
	case inputs.RuntimeEvidence:
		result.Status = EvidenceQualityDegraded
		result.Explanation = inputs.Explanation
	default:
		result.Status = EvidenceQualityInsufficient
		result.Explanation = inputs.Explanation
	}
	return result
}

func formatInt(value int) string {
	return fmt.Sprint(value)
}

func formatInt64(value uint64) string {
	return fmt.Sprint(value)
}
