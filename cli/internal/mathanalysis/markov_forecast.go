package mathanalysis

import (
	"fmt"
	"math"
)

const (
	markovForecastDegrading    = "degrading"
	markovForecastStable       = "stable"
	markovForecastImproving    = "improving"
	markovForecastUncertain    = "uncertain"
	markovForecastInsufficient = "insufficient"
	markovForecastMinWindows   = 12
	markovForecastDriftFloor   = 0.08
	markovForecastSignalFloor  = 0.05
)

const (
	markovForecastHealthyGroup = iota
	markovForecastRecoveringGroup
	markovForecastBadGroup
	markovForecastGroupCount
)

func buildMarkovForecast(model MarkovModel) MarkovForecast {
	sampleCount := len(model.States)
	if model.TimelineBucketCount > 0 && model.ObservationCoverage < 0.8 {
		return MarkovForecast{
			Direction:        markovForecastInsufficient,
			Label:            "Недостаточно непрерывных измерений",
			Severity:         "medium",
			Confidence:       "low",
			ConfidenceReason: fmt.Sprintf("измерено %d из %d временных интервалов", sampleCount, model.TimelineBucketCount),
			Summary:          "Пропущенные интервалы не считаются нормальной работой и разрывают цепочку состояний. При текущем покрытии прогноз дальнейшей траектории не строится.",
		}
	}
	if sampleCount < markovForecastMinWindows {
		return MarkovForecast{
			Direction:        markovForecastInsufficient,
			Label:            "Недостаточно данных",
			Severity:         "medium",
			Confidence:       "low",
			ConfidenceReason: fmt.Sprintf("для прогноза нужно не менее %d временных интервалов, сейчас %d", markovForecastMinWindows, sampleCount),
			Summary:          "Текущая модель описывает только уже наблюдаемые состояния и не строит предположение о дальнейшей траектории.",
		}
	}

	segmentWindows := markovForecastSegmentWindows(sampleCount)
	earlyExposure := markovForecastBadExposure(model.States[:segmentWindows])
	recentStates := model.States[sampleCount-segmentWindows:]
	recentExposure := markovForecastBadExposure(recentStates)
	horizonWindows := markovForecastHorizonWindows(sampleCount)
	horizonMS := markovForecastHorizonMS(model.States, horizonWindows)
	projectedExposure := markovForecastProjection(model.States, recentStates, horizonWindows)
	observedDelta := recentExposure - earlyExposure
	forecastDelta := projectedExposure - earlyExposure
	direction := markovForecastDirection(observedDelta, forecastDelta)
	confidence, confidenceReason := markovForecastConfidence(model, direction, observedDelta, forecastDelta)

	forecast := MarkovForecast{
		Direction:               direction,
		Label:                   markovForecastLabel(model, direction, recentExposure, projectedExposure),
		Severity:                markovForecastSeverity(model, direction, observedDelta, projectedExposure),
		Confidence:              confidence,
		ConfidenceReason:        confidenceReason,
		SegmentWindows:          segmentWindows,
		HorizonWindows:          horizonWindows,
		HorizonMS:               horizonMS,
		EarlyBadExposure:        earlyExposure,
		RecentBadExposure:       recentExposure,
		ProjectedBadProbability: projectedExposure,
	}
	forecast.Summary = fmt.Sprintf(
		"In-sample проекция при сохранении эмпирической матрицы переходов на горизонте %d интервалов (около %.1f с) даёт модельную долю плохих состояний %.1f%%. Это не калиброванный прогноз будущего запуска. В первых %d интервалах плохие состояния занимали %.1f%% времени, в последних %d — %.1f%%.",
		horizonWindows,
		float64(horizonMS)/1000,
		projectedExposure*100,
		segmentWindows,
		earlyExposure*100,
		segmentWindows,
		recentExposure*100,
	)
	return forecast
}

func markovForecastSegmentWindows(sampleCount int) int {
	windows := sampleCount / 3
	if windows < 4 {
		return 4
	}
	if windows > 30 {
		return 30
	}
	return windows
}

func markovForecastHorizonWindows(sampleCount int) int {
	windows := sampleCount / 4
	if windows < 5 {
		return 5
	}
	if windows > 30 {
		return 30
	}
	return windows
}

func markovForecastHorizonMS(states []MarkovBucketState, horizonWindows int) uint64 {
	if len(states) == 0 || horizonWindows == 0 {
		return 0
	}
	return markovTotalDurationMS(states) / uint64(len(states)) * uint64(horizonWindows)
}

func markovForecastBadExposure(states []MarkovBucketState) float64 {
	return markovBadStateExposure(states)
}

func markovForecastProjection(states, recentStates []MarkovBucketState, horizonWindows int) float64 {
	matrix := markovForecastTransitionMatrix(states)
	distribution := markovForecastDistribution(recentStates)
	for range horizonWindows {
		var next [markovForecastGroupCount]float64
		for from := range markovForecastGroupCount {
			for to := range markovForecastGroupCount {
				next[to] += distribution[from] * matrix[from][to]
			}
		}
		distribution = next
	}
	return distribution[markovForecastBadGroup]
}

func markovForecastTransitionMatrix(states []MarkovBucketState) [markovForecastGroupCount][markovForecastGroupCount]float64 {
	var counts [markovForecastGroupCount][markovForecastGroupCount]float64
	var matrix [markovForecastGroupCount][markovForecastGroupCount]float64
	for index := 1; index < len(states); index++ {
		if !markovStatesAdjacent(states[index-1], states[index]) {
			continue
		}
		from := markovForecastGroup(states[index-1].State)
		to := markovForecastGroup(states[index].State)
		counts[from][to]++
	}
	for from := range markovForecastGroupCount {
		var total float64
		for to := range markovForecastGroupCount {
			total += counts[from][to]
		}
		if total == 0 {
			matrix[from][from] = 1
			continue
		}
		for to := range markovForecastGroupCount {
			matrix[from][to] = counts[from][to] / total
		}
	}
	return matrix
}

func markovForecastDistribution(states []MarkovBucketState) [markovForecastGroupCount]float64 {
	var distribution [markovForecastGroupCount]float64
	var total float64
	for _, state := range states {
		duration := float64(markovStateDurationMS(state))
		distribution[markovForecastGroup(state.State)] += duration
		total += duration
	}
	if total == 0 {
		distribution[markovForecastHealthyGroup] = 1
		return distribution
	}
	for index := range distribution {
		distribution[index] /= total
	}
	return distribution
}

func markovForecastGroup(state string) int {
	switch {
	case markovIsBadState(state):
		return markovForecastBadGroup
	case state == markovRecovering:
		return markovForecastRecoveringGroup
	default:
		return markovForecastHealthyGroup
	}
}

func markovForecastDirection(observedDelta, forecastDelta float64) string {
	conflictingSignals := observedDelta*forecastDelta < 0 && math.Abs(observedDelta) >= markovForecastSignalFloor && math.Abs(forecastDelta) >= markovForecastSignalFloor
	if conflictingSignals {
		return markovForecastUncertain
	}
	combinedDelta := (observedDelta + forecastDelta) / 2
	if observedDelta >= markovForecastSignalFloor && forecastDelta >= markovForecastSignalFloor && combinedDelta >= markovForecastDriftFloor {
		return markovForecastDegrading
	}
	if observedDelta <= -markovForecastSignalFloor && forecastDelta <= -markovForecastSignalFloor && combinedDelta <= -markovForecastDriftFloor {
		return markovForecastImproving
	}
	return markovForecastStable
}

func markovForecastConfidence(model MarkovModel, direction string, observedDelta, forecastDelta float64) (string, string) {
	if model.Confidence == "low" || direction == markovForecastUncertain {
		return "low", "наблюдаемая динамика и прогноз матрицы переходов недостаточно согласованы"
	}
	strongStableSignal := direction == markovForecastStable && math.Abs(observedDelta) < markovForecastSignalFloor && math.Abs(forecastDelta) < markovForecastSignalFloor
	strongDirectionalSignal := direction != markovForecastStable && math.Abs(observedDelta) >= 0.20 && math.Abs(forecastDelta) >= 0.20
	if model.Confidence == "high" && (strongStableSignal || strongDirectionalSignal) {
		return "high", "объем выборки достаточен, наблюдаемая динамика согласуется с матрицей переходов"
	}
	if direction == markovForecastStable {
		return "medium", "устойчивое направление не подтверждено; оценку нужно проверить более длинным одиночным прогоном"
	}
	return "medium", "направление различимо, но его нужно подтвердить более длинным прогоном или повтором сценария"
}

func markovForecastLabel(model MarkovModel, direction string, recentExposure, projectedExposure float64) string {
	switch direction {
	case markovForecastDegrading:
		return "Есть сигнал возможной деградации"
	case markovForecastImproving:
		return "Есть сигнал возможного улучшения"
	case markovForecastUncertain:
		return "Траектория приложения неоднозначна"
	case markovForecastStable:
		if markovStatus(model) == "ok" && recentExposure <= 0.10 && projectedExposure <= 0.10 {
			return "Деградация не подтверждена, состояние в норме"
		}
		return "Устойчивое улучшение или деградация не подтверждены"
	default:
		return "Недостаточно данных"
	}
}

func markovForecastSeverity(model MarkovModel, direction string, observedDelta, projectedExposure float64) string {
	switch direction {
	case markovForecastDegrading:
		if observedDelta >= 0.25 || projectedExposure >= 0.30 {
			return "high"
		}
		return "medium"
	case markovForecastImproving:
		if projectedExposure >= 0.25 {
			return "medium"
		}
		return "ok"
	case markovForecastUncertain:
		return "medium"
	case markovForecastStable:
		return markovStatus(model)
	default:
		return "medium"
	}
}
