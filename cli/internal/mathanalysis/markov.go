package mathanalysis

import (
	"fmt"
	"sort"
)

const (
	markovHealthy        = "Healthy"
	markovNetworkLoop    = "NetworkLoop"
	markovNetworkSlow    = "NetworkSlow"
	markovJanky          = "Janky"
	markovStalled        = "Stalled"
	markovMemoryPressure = "MemoryPressure"
	markovRecovering     = "Recovering"
)

var markovStateOrder = []string{
	markovHealthy,
	markovNetworkLoop,
	markovNetworkSlow,
	markovJanky,
	markovStalled,
	markovMemoryPressure,
	markovRecovering,
}

func buildMarkovModel(timeline []TimelineBucket, loops []NetworkLoopFinding) MarkovModel {
	states := classifyMarkovStates(timeline, loops)
	transitions := buildMarkovTransitions(states)
	model := MarkovModel{
		States:                  states,
		Transitions:             transitions,
		HealthyToBadCount:       markovHealthyToBadCount(transitions),
		BadToHealthyProbability: markovBadToHealthyProbability(transitions),
		ExpectedRecoveryWindows: markovExpectedRecoveryWindows(states),
		StickyStates:            markovStickyStates(transitions),
	}
	return model
}

func classifyMarkovStates(timeline []TimelineBucket, loops []NetworkLoopFinding) []MarkovBucketState {
	pssFloor := minNonZeroPSS(timeline)
	states := make([]MarkovBucketState, 0, len(timeline))
	previousBad := false
	for _, bucket := range timeline {
		state, reason := classifyMarkovBucket(bucket, loops, pssFloor)
		if state == markovHealthy && previousBad {
			state = markovRecovering
			reason = "первое спокойное окно после деградации"
		}
		states = append(states, MarkovBucketState{
			TimeMS:  bucket.StartMS,
			State:   state,
			Reason:  reason,
			Route:   bucket.RouteSample,
			Owner:   bucket.OwnerSample,
			Screen:  bucket.ScreenSample,
			Network: bucket.NetworkSample,
		})
		previousBad = markovIsBadState(state)
	}
	return states
}

func classifyMarkovBucket(bucket TimelineBucket, loops []NetworkLoopFinding, pssFloor uint64) (string, string) {
	switch {
	case bucketInsideNetworkLoop(bucket, loops):
		return markovNetworkLoop, "временный интервал попадает в окно найденного сетевого цикла"
	case bucket.StallCount > 0:
		return markovStalled, fmt.Sprintf("пауз главного потока: %d, максимум %d ms", bucket.StallCount, bucket.StallMaxMS)
	case pssFloor > 0 && bucket.MemoryPSSKB >= pssFloor+memoryGrowthFloorKB:
		return markovMemoryPressure, fmt.Sprintf("PSS выше нижней полки на %.1f MB", float64(bucket.MemoryPSSKB-pssFloor)/1024)
	case bucket.AvailableMemoryKB > 0 && bucket.AvailableMemoryKB < lowMemoryTargetKB:
		return markovMemoryPressure, fmt.Sprintf("свободная память ниже 256 MB: %.1f MB", float64(bucket.AvailableMemoryKB)/1024)
	case bucket.UIFrames > 0 && jankRate(bucket.UIJankyFrames, bucket.UIFrames) >= 5:
		return markovJanky, fmt.Sprintf("доля подтормаживаний %.1f%%", jankRate(bucket.UIJankyFrames, bucket.UIFrames))
	case bucket.HTTPFailed > 0:
		return markovNetworkSlow, fmt.Sprintf("HTTP ошибок: %d", bucket.HTTPFailed)
	case bucket.HTTPCount > 0 && bucket.HTTPP95DurationMS >= 500:
		return markovNetworkSlow, fmt.Sprintf("HTTP p95 %d ms", bucket.HTTPP95DurationMS)
	case bucket.DNSDurationMS >= 100:
		return markovNetworkSlow, fmt.Sprintf("DNS среднее %d ms", bucket.DNSDurationMS)
	case bucket.ConnectDurationMS >= 150:
		return markovNetworkSlow, fmt.Sprintf("среднее время соединения %d ms", bucket.ConnectDurationMS)
	default:
		return markovHealthy, "нет выраженной деградации"
	}
}

func bucketInsideNetworkLoop(bucket TimelineBucket, loops []NetworkLoopFinding) bool {
	for _, loop := range loops {
		if loop.Confidence < 0.35 {
			continue
		}
		if bucket.StartMS >= loop.FirstMS && bucket.StartMS <= loop.LastMS {
			return true
		}
	}
	return false
}

func buildMarkovTransitions(states []MarkovBucketState) []MarkovTransition {
	if len(states) < 2 {
		return nil
	}
	counts := map[string]map[string]int{}
	totals := map[string]int{}
	for i := 1; i < len(states); i++ {
		from := states[i-1].State
		to := states[i].State
		if counts[from] == nil {
			counts[from] = map[string]int{}
		}
		counts[from][to]++
		totals[from]++
	}
	transitions := make([]MarkovTransition, 0)
	for from, row := range counts {
		for to, count := range row {
			transitions = append(transitions, MarkovTransition{
				From:        from,
				To:          to,
				Count:       count,
				Probability: float64(count) / float64(totals[from]),
			})
		}
	}
	sort.Slice(transitions, func(i, j int) bool {
		if markovStateRank(transitions[i].From) != markovStateRank(transitions[j].From) {
			return markovStateRank(transitions[i].From) < markovStateRank(transitions[j].From)
		}
		if transitions[i].Probability != transitions[j].Probability {
			return transitions[i].Probability > transitions[j].Probability
		}
		return markovStateRank(transitions[i].To) < markovStateRank(transitions[j].To)
	})
	return transitions
}

func markovHealthyToBadCount(transitions []MarkovTransition) int {
	var count int
	for _, transition := range transitions {
		if transition.From == markovHealthy && markovIsBadState(transition.To) {
			count += transition.Count
		}
	}
	return count
}

func markovBadToHealthyProbability(transitions []MarkovTransition) float64 {
	var badOutgoing int
	var recovered int
	for _, transition := range transitions {
		if !markovIsBadState(transition.From) {
			continue
		}
		badOutgoing += transition.Count
		if transition.To == markovHealthy || transition.To == markovRecovering {
			recovered += transition.Count
		}
	}
	if badOutgoing == 0 {
		return 1
	}
	return float64(recovered) / float64(badOutgoing)
}

func markovExpectedRecoveryWindows(states []MarkovBucketState) float64 {
	var total float64
	var episodes int
	for index := 0; index < len(states); {
		if !markovIsBadState(states[index].State) {
			index++
			continue
		}
		length := 0
		for index < len(states) && markovIsBadState(states[index].State) {
			length++
			index++
		}
		if index < len(states) {
			total += float64(length)
			episodes++
		}
	}
	if episodes == 0 {
		return 0
	}
	return total / float64(episodes)
}

func markovStickyStates(transitions []MarkovTransition) []MarkovStickyState {
	sticky := make([]MarkovStickyState, 0)
	for _, transition := range transitions {
		if transition.From != transition.To || transition.Count == 0 {
			continue
		}
		sticky = append(sticky, MarkovStickyState{
			State:       transition.From,
			Count:       transition.Count,
			Probability: transition.Probability,
		})
	}
	sort.Slice(sticky, func(i, j int) bool {
		if sticky[i].Probability != sticky[j].Probability {
			return sticky[i].Probability > sticky[j].Probability
		}
		return sticky[i].Count > sticky[j].Count
	})
	if len(sticky) > 3 {
		sticky = sticky[:3]
	}
	return sticky
}

func compareMarkovModels(baseline, candidate MarkovModel) []MarkovDelta {
	deltas := []MarkovDelta{
		markovDeltaCount("Здоровые -> плохие состояния", "шт", float64(baseline.HealthyToBadCount), float64(candidate.HealthyToBadCount), true),
		markovDeltaProbability("Вероятность восстановления", baseline.BadToHealthyProbability, candidate.BadToHealthyProbability, false),
		markovDeltaCount("Ожидаемое восстановление", "интервалов", baseline.ExpectedRecoveryWindows, candidate.ExpectedRecoveryWindows, true),
	}
	states := markovStickyStateUnion(baseline.StickyStates, candidate.StickyStates)
	for _, state := range states {
		base := stickyProbability(baseline.StickyStates, state)
		cand := stickyProbability(candidate.StickyStates, state)
		deltas = append(deltas, markovDeltaProbability("Липкость: "+MarkovStateLabel(state), base, cand, true))
	}
	return deltas
}

func markovDeltaCount(metric, unit string, baseline, candidate float64, higherIsWorse bool) MarkovDelta {
	delta := candidate - baseline
	severity := markovDeltaCountSeverity(delta, higherIsWorse)
	return MarkovDelta{
		Metric:         metric,
		Unit:           unit,
		BaselineValue:  baseline,
		CandidateValue: candidate,
		Delta:          delta,
		Severity:       severity,
		Summary:        markovDeltaSummary(metric, baseline, candidate, delta, unit, higherIsWorse),
	}
}

func markovDeltaProbability(metric string, baseline, candidate float64, higherIsWorse bool) MarkovDelta {
	basePct := baseline * 100
	candPct := candidate * 100
	delta := candPct - basePct
	severity := markovDeltaProbabilitySeverity(delta, basePct, higherIsWorse)
	return MarkovDelta{
		Metric:         metric,
		Unit:           "%",
		BaselineValue:  basePct,
		CandidateValue: candPct,
		Delta:          delta,
		Severity:       severity,
		Summary:        markovDeltaSummary(metric, basePct, candPct, delta, "%", higherIsWorse),
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

func markovDeltaSummary(metric string, baseline, candidate, delta float64, unit string, higherIsWorse bool) string {
	trend := "улучшилась"
	if (higherIsWorse && delta > 0) || (!higherIsWorse && delta < 0) {
		trend = "ухудшилась"
	}
	return fmt.Sprintf("%s %s: %.1f -> %.1f %s, Δ %+.1f %s.", metric, trend, baseline, candidate, unit, delta, unit)
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

func markovStatus(model MarkovModel) string {
	if len(model.States) < 2 {
		return "medium"
	}
	if model.HealthyToBadCount > 0 && model.BadToHealthyProbability < 0.35 {
		return "high"
	}
	for _, sticky := range model.StickyStates {
		if markovIsBadState(sticky.State) && sticky.Probability >= 0.70 {
			return "high"
		}
	}
	if model.HealthyToBadCount > 0 || model.ExpectedRecoveryWindows >= 3 {
		return "medium"
	}
	return "ok"
}

func markovSummary(model MarkovModel) string {
	if len(model.States) < 2 {
		return "Недостаточно временных интервалов для матрицы переходов состояний."
	}
	return fmt.Sprintf("Состояний=%d, переходов=%d, здоровые -> плохие=%d, восстановление %.1f%%, ожидание восстановления %.1f временных интервалов.", len(model.States), len(model.Transitions), model.HealthyToBadCount, model.BadToHealthyProbability*100, model.ExpectedRecoveryWindows)
}

func markovFindings(model MarkovModel) []Finding {
	if len(model.States) < 2 {
		return []Finding{{
			Severity:       "medium",
			Title:          "Недостаточно данных для марковской модели",
			Detail:         markovSummary(model),
			Recommendation: "Нужны хотя бы два временных интервала сценария.",
		}}
	}
	if model.HealthyToBadCount > 0 && model.BadToHealthyProbability < 0.5 {
		return []Finding{{
			Severity:       markovStatus(model),
			Title:          "Слабое восстановление после плохих состояний",
			Detail:         markovSummary(model),
			Recommendation: "Проверьте причины плохих окон: сетевые циклы, точки изменения, интегральную нагрузку и источники работ.",
		}}
	}
	for _, sticky := range model.StickyStates {
		if markovIsBadState(sticky.State) && sticky.Probability >= 0.5 {
			return []Finding{{
				Severity:       markovStatus(model),
				Title:          "Найдено липкое плохое состояние",
				Detail:         fmt.Sprintf("%s повторяется само в себя с вероятностью %.1f%%.", MarkovStateLabel(sticky.State), sticky.Probability*100),
				Recommendation: "Посмотрите соседние временные интервалы и контекст источника/маршрута: липкость обычно означает повторяющуюся работу или отсутствие backoff.",
			}}
		}
	}
	return []Finding{{
		Severity: "ok",
		Title:    "Марковская модель не показывает опасных переходов",
		Detail:   markovSummary(model),
	}}
}

func compareMarkovStatus(deltas []MarkovDelta) string {
	if len(deltas) == 0 {
		return "medium"
	}
	status := "ok"
	for _, delta := range deltas {
		if delta.Severity == "high" {
			return "high"
		}
		if delta.Severity == "medium" {
			status = "medium"
		}
	}
	return status
}

func compareMarkovSummary(deltas []MarkovDelta) string {
	if len(deltas) == 0 {
		return "Недостаточно марковских метрик для сравнения."
	}
	var worse int
	for _, delta := range deltas {
		if delta.Severity == "high" || delta.Severity == "medium" {
			worse++
		}
	}
	if worse == 0 {
		return "Кандидат не ухудшил переходы между состояниями."
	}
	return fmt.Sprintf("Кандидат ухудшил %d марковских метрик из %d.", worse, len(deltas))
}

func compareMarkovFindings(deltas []MarkovDelta) []Finding {
	if len(deltas) == 0 {
		return []Finding{{
			Severity: "medium",
			Title:    "Марковское сравнение недоступно",
			Detail:   compareMarkovSummary(deltas),
		}}
	}
	for _, delta := range deltas {
		if delta.Severity == "high" || delta.Severity == "medium" {
			return []Finding{{
				Severity:       delta.Severity,
				Title:          "Изменились переходы состояний",
				Detail:         delta.Summary,
				Recommendation: "Сравните последовательность состояний кандидата с таймлайном, сетевыми циклами и интегральной нагрузкой.",
			}}
		}
	}
	return []Finding{{
		Severity: "ok",
		Title:    "Регрессий марковских переходов не найдено",
		Detail:   compareMarkovSummary(deltas),
	}}
}

func MarkovStateLabel(state string) string {
	switch state {
	case markovHealthy:
		return "Здоровое"
	case markovNetworkLoop:
		return "Сетевой цикл"
	case markovNetworkSlow:
		return "Медленная сеть"
	case markovJanky:
		return "Подтормаживание UI"
	case markovStalled:
		return "Пауза главного потока"
	case markovMemoryPressure:
		return "Давление памяти"
	case markovRecovering:
		return "Восстановление"
	default:
		return state
	}
}

func markovIsBadState(state string) bool {
	switch state {
	case markovNetworkLoop, markovNetworkSlow, markovJanky, markovStalled, markovMemoryPressure:
		return true
	default:
		return false
	}
}

func markovStateRank(state string) int {
	for index, item := range markovStateOrder {
		if item == state {
			return index
		}
	}
	return len(markovStateOrder)
}
