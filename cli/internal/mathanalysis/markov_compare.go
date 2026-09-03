package mathanalysis

import (
	"fmt"
	"math"
	"sort"
)

func compareMarkovModels(baseline, candidate MarkovModel) []MarkovDelta {
	if normalizedRunCount(baseline.IndependentRunCount) > 1 || normalizedRunCount(candidate.IndependentRunCount) > 1 {
		return []MarkovDelta{{
			Metric:             "Сопоставимость последовательности состояний",
			Unit:               "прогонов",
			BaselineValue:      float64(normalizedRunCount(baseline.IndependentRunCount)),
			CandidateValue:     float64(normalizedRunCount(candidate.IndependentRunCount)),
			Delta:              float64(normalizedRunCount(candidate.IndependentRunCount) - normalizedRunCount(baseline.IndependentRunCount)),
			BaselineAvailable:  true,
			CandidateAvailable: true,
			Severity:           "medium",
			Summary:            "Марковские дельты не рассчитаны: хотя бы одна сторона объединяет независимые запуски, поэтому соседние агрегированные состояния не являются последовательностью одного прогона.",
		}}
	}
	deltas := []MarkovDelta{
		markovDeltaCount("Здоровые → плохие состояния", "шт", float64(baseline.HealthyToBadCount), float64(candidate.HealthyToBadCount), true),
		markovDeltaProbability("Доля плохих состояний", baseline.BadStateExposure, candidate.BadStateExposure, true),
		markovDeltaTransitionMatrixDivergence(baseline, candidate),
	}
	if baseline.HasRecoveryProbability && candidate.HasRecoveryProbability {
		deltas = append(deltas, markovDeltaProbability("Вероятность восстановления", baseline.BadToHealthyProbability, candidate.BadToHealthyProbability, false))
	} else if baseline.HasRecoveryProbability != candidate.HasRecoveryProbability {
		deltas = append(deltas, unavailableMarkovDelta("Вероятность восстановления", "%", baseline.BadToHealthyProbability*100, candidate.BadToHealthyProbability*100, baseline.HasRecoveryProbability, candidate.HasRecoveryProbability))
	}
	if baseline.HasExpectedRecovery && candidate.HasExpectedRecovery {
		deltas = append(deltas,
			markovDeltaCount("Ожидаемое восстановление", "интервалов", baseline.ExpectedRecoveryWindows, candidate.ExpectedRecoveryWindows, true),
			markovDeltaDuration("Ожидаемое восстановление по времени", baseline.ExpectedRecoveryMS, candidate.ExpectedRecoveryMS, true),
		)
	} else if baseline.HasExpectedRecovery != candidate.HasExpectedRecovery {
		deltas = append(deltas, unavailableMarkovDelta("Ожидаемое восстановление", "интервалов", baseline.ExpectedRecoveryWindows, candidate.ExpectedRecoveryWindows, baseline.HasExpectedRecovery, candidate.HasExpectedRecovery))
	}
	states := markovStickyStateUnion(baseline.StickyStates, candidate.StickyStates)
	for _, state := range states {
		base := stickyProbability(baseline.StickyStates, state)
		cand := stickyProbability(candidate.StickyStates, state)
		deltas = append(deltas, markovDeltaProbability("Липкость: "+MarkovStateLabel(state), base, cand, true))
	}
	return deltas
}

func unavailableMarkovDelta(metric, unit string, baseline, candidate float64, baselineAvailable, candidateAvailable bool) MarkovDelta {
	return MarkovDelta{
		Metric:             metric,
		Unit:               unit,
		BaselineValue:      baseline,
		CandidateValue:     candidate,
		Delta:              candidate - baseline,
		Comparable:         false,
		BaselineAvailable:  baselineAvailable,
		CandidateAvailable: candidateAvailable,
		Severity:           "medium",
		Summary:            fmt.Sprintf("%s не сравнивается: завершённый плохой эпизод наблюдался только на одной стороне.", metric),
	}
}

func markovDeltaCount(metric, unit string, baseline, candidate float64, higherIsWorse bool) MarkovDelta {
	delta := candidate - baseline
	severity := markovDeltaCountSeverity(delta, higherIsWorse)
	return MarkovDelta{
		Metric:             metric,
		Unit:               unit,
		BaselineValue:      baseline,
		CandidateValue:     candidate,
		Delta:              delta,
		Comparable:         true,
		BaselineAvailable:  true,
		CandidateAvailable: true,
		Severity:           severity,
		Summary:            markovDeltaSummary(metric, baseline, candidate, delta, unit, higherIsWorse),
	}
}

func markovDeltaProbability(metric string, baseline, candidate float64, higherIsWorse bool) MarkovDelta {
	basePct := baseline * 100
	candPct := candidate * 100
	delta := candPct - basePct
	severity := markovDeltaProbabilitySeverity(delta, basePct, higherIsWorse)
	return MarkovDelta{
		Metric:             metric,
		Unit:               "%",
		BaselineValue:      basePct,
		CandidateValue:     candPct,
		Delta:              delta,
		Comparable:         true,
		BaselineAvailable:  true,
		CandidateAvailable: true,
		Severity:           severity,
		Summary:            markovDeltaSummary(metric, basePct, candPct, delta, "%", higherIsWorse),
	}
}

func markovDeltaDuration(metric string, baseline, candidate float64, higherIsWorse bool) MarkovDelta {
	delta := candidate - baseline
	severity := markovDeltaDurationSeverity(delta, baseline, higherIsWorse)
	return MarkovDelta{
		Metric:             metric,
		Unit:               "мс",
		BaselineValue:      baseline,
		CandidateValue:     candidate,
		Delta:              delta,
		Comparable:         true,
		BaselineAvailable:  true,
		CandidateAvailable: true,
		Severity:           severity,
		Summary:            markovDeltaSummary(metric, baseline, candidate, delta, "мс", higherIsWorse),
	}
}

func markovDeltaTransitionMatrixDivergence(baseline, candidate MarkovModel) MarkovDelta {
	divergence := markovTransitionMatrixDivergence(baseline, candidate)
	severity := markovMatrixDivergenceSeverity(divergence, baseline, candidate)
	summary := fmt.Sprintf("Матрица переходов изменилась на %.3f по расхождению Йенсена-Шеннона; показатель близкий к 1 означает сильное изменение сценария.", divergence)
	if severity == "ok" && divergence > 0 {
		summary += " Изменение не выглядит ухудшением по доле плохих состояний и восстановлению."
	}
	return MarkovDelta{
		Metric:             "Расхождение матрицы переходов",
		Unit:               "индекс",
		BaselineValue:      0,
		CandidateValue:     divergence,
		Delta:              divergence,
		Comparable:         true,
		BaselineAvailable:  true,
		CandidateAvailable: true,
		Severity:           severity,
		Summary:            summary,
	}
}

func markovDeltaCountSeverity(delta float64, higherIsWorse bool) string {
	worseDelta := delta
	if !higherIsWorse {
		worseDelta = -delta
	}
	switch {
	case worseDelta >= 3:
		return "high"
	case worseDelta >= 1:
		return "medium"
	default:
		return "ok"
	}
}

func markovDeltaProbabilitySeverity(delta, baseline float64, higherIsWorse bool) string {
	worseDelta := delta
	if !higherIsWorse {
		worseDelta = -delta
	}
	if worseDelta <= 0 {
		return "ok"
	}
	if worseDelta >= 25 || (baseline > 0 && worseDelta*100/baseline >= 50) {
		return "high"
	}
	if worseDelta >= 10 || (baseline > 0 && worseDelta*100/baseline >= 20) {
		return "medium"
	}
	return "ok"
}

func markovDeltaDurationSeverity(delta, baseline float64, higherIsWorse bool) string {
	worseDelta := delta
	if !higherIsWorse {
		worseDelta = -delta
	}
	if worseDelta <= 0 {
		return "ok"
	}
	if worseDelta >= 5000 || (baseline > 0 && worseDelta*100/baseline >= 50) {
		return "high"
	}
	if worseDelta >= 1000 || (baseline > 0 && worseDelta*100/baseline >= 20) {
		return "medium"
	}
	return "ok"
}

func markovDeltaSummary(metric string, baseline, candidate, delta float64, unit string, higherIsWorse bool) string {
	trend := "изменение считается улучшением"
	if (higherIsWorse && delta > 0) || (!higherIsWorse && delta < 0) {
		trend = "изменение считается ухудшением"
	} else if delta == 0 {
		trend = "без изменения"
	}
	return fmt.Sprintf("%s: %s; %.1f → %.1f %s, Δ %+.1f %s.", metric, trend, baseline, candidate, unit, delta, unit)
}

func markovTransitionMatrixDivergence(baseline, candidate MarkovModel) float64 {
	baseDistribution, baseTotal := markovTransitionPairDistribution(baseline.Transitions)
	candidateDistribution, candidateTotal := markovTransitionPairDistribution(candidate.Transitions)
	switch {
	case baseTotal == 0 && candidateTotal == 0:
		return 0
	case baseTotal == 0 || candidateTotal == 0:
		return 1
	}
	keys := map[string]struct{}{}
	for key := range baseDistribution {
		keys[key] = struct{}{}
	}
	for key := range candidateDistribution {
		keys[key] = struct{}{}
	}
	var divergence float64
	for key := range keys {
		p := baseDistribution[key]
		q := candidateDistribution[key]
		mid := (p + q) / 2
		if p > 0 {
			divergence += 0.5 * p * math.Log2(p/mid)
		}
		if q > 0 {
			divergence += 0.5 * q * math.Log2(q/mid)
		}
	}
	return divergence
}

func markovTransitionPairDistribution(transitions []MarkovTransition) (map[string]float64, int) {
	var total int
	for _, transition := range transitions {
		total += transition.Count
	}
	distribution := map[string]float64{}
	if total == 0 {
		return distribution, total
	}
	for _, transition := range transitions {
		key := transition.From + "\x00" + transition.To
		distribution[key] = float64(transition.Count) / float64(total)
	}
	return distribution, total
}

func markovMatrixDivergenceSeverity(divergence float64, baseline, candidate MarkovModel) string {
	if divergence < 0.12 || !markovCandidateLooksWorse(baseline, candidate) {
		return "ok"
	}
	if divergence >= 0.30 {
		return "high"
	}
	return "medium"
}

func markovCandidateLooksWorse(baseline, candidate MarkovModel) bool {
	if candidate.BadStateExposure > baseline.BadStateExposure+0.05 {
		return true
	}
	if candidate.HealthyToBadCount > baseline.HealthyToBadCount {
		return true
	}
	if baseline.HasRecoveryProbability && candidate.HasRecoveryProbability && candidate.BadToHealthyProbability < baseline.BadToHealthyProbability-0.10 {
		return true
	}
	if !baseline.HasExpectedRecovery || !candidate.HasExpectedRecovery {
		return false
	}
	return candidate.ExpectedRecoveryMS > baseline.ExpectedRecoveryMS*1.20
}

func markovStickyStateUnion(baseline, candidate []MarkovStickyState) []string {
	seen := map[string]struct{}{}
	var states []string
	for _, item := range baseline {
		if _, ok := seen[item.State]; !ok {
			seen[item.State] = struct{}{}
			states = append(states, item.State)
		}
	}
	for _, item := range candidate {
		if _, ok := seen[item.State]; !ok {
			seen[item.State] = struct{}{}
			states = append(states, item.State)
		}
	}
	sort.Slice(states, func(i, j int) bool { return markovStateRank(states[i]) < markovStateRank(states[j]) })
	return states
}

func stickyProbability(states []MarkovStickyState, state string) float64 {
	for _, item := range states {
		if item.State == state {
			return item.Probability
		}
	}
	return 0
}
