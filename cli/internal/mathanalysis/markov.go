package mathanalysis

import (
	"fmt"
	"math"
	"sort"
	"strings"
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
	badEpisodes := markovBadEpisodeCount(states)
	confidence, confidenceReason := markovConfidence(states, transitions, badEpisodes)
	model := MarkovModel{
		States:                  states,
		Transitions:             transitions,
		SampleCount:             len(states),
		TransitionEventCount:    markovTransitionEventCount(transitions),
		BadEpisodeCount:         badEpisodes,
		Confidence:              confidence,
		ConfidenceReason:        confidenceReason,
		HealthyToBadCount:       markovHealthyToBadCount(transitions),
		BadToHealthyProbability: markovBadToHealthyProbability(transitions),
		ExpectedRecoveryWindows: markovExpectedRecoveryWindows(states),
		ExpectedRecoveryMS:      markovExpectedRecoveryMS(states),
		TotalDurationMS:         markovTotalDurationMS(states),
		BadStateDurationMS:      markovBadStateDurationMS(states),
		BadStateExposure:        markovBadStateExposure(states),
		StateExposures:          markovStateExposures(states),
		StickyStates:            markovStickyStates(transitions),
		ContextStickyStates:     markovContextStickyStates(states),
	}
	return model
}

func classifyMarkovStates(timeline []TimelineBucket, loops []NetworkLoopFinding) []MarkovBucketState {
	pssFloor := minNonZeroPSS(timeline)
	states := make([]MarkovBucketState, 0, len(timeline))
	previousBad := false
	for _, bucket := range timeline {
		state, reason, contributors := classifyMarkovBucket(bucket, loops, pssFloor)
		if state == markovHealthy && previousBad {
			state = markovRecovering
			reason = "первое спокойное окно после деградации"
			contributors = []MarkovSymptomWeight{{
				State:  markovRecovering,
				Weight: 1,
				Reason: reason,
			}}
		}
		states = append(states, MarkovBucketState{
			TimeMS:       bucket.StartMS,
			DurationMS:   markovBucketDurationMS(bucket),
			State:        state,
			Reason:       reason,
			Contributors: contributors,
			Route:        bucket.RouteSample,
			Owner:        bucket.OwnerSample,
			Screen:       bucket.ScreenSample,
			Network:      bucket.NetworkSample,
		})
		previousBad = markovIsBadState(state)
	}
	return states
}

func classifyMarkovBucket(bucket TimelineBucket, loops []NetworkLoopFinding, pssFloor uint64) (string, string, []MarkovSymptomWeight) {
	contributors := markovBucketContributors(bucket, loops, pssFloor)
	if len(contributors) == 0 {
		return markovHealthy, "нет выраженной деградации", nil
	}
	state := markovDominantState(contributors)
	for _, contributor := range contributors {
		if contributor.State == state {
			return state, contributor.Reason, contributors
		}
	}
	return state, MarkovStateLabel(state), contributors
}

func markovNetworkLoopConfidence(bucket TimelineBucket, loops []NetworkLoopFinding) float64 {
	var confidence float64
	for _, loop := range loops {
		if loop.Confidence < 0.35 {
			continue
		}
		if bucket.StartMS >= loop.FirstMS && bucket.StartMS <= loop.LastMS {
			if loop.Confidence > confidence {
				confidence = loop.Confidence
			}
		}
	}
	return confidence
}

func markovBucketContributors(bucket TimelineBucket, loops []NetworkLoopFinding, pssFloor uint64) []MarkovSymptomWeight {
	var contributors []MarkovSymptomWeight
	if confidence := markovNetworkLoopConfidence(bucket, loops); confidence >= 0.35 {
		contributors = append(contributors, MarkovSymptomWeight{
			State:  markovNetworkLoop,
			Weight: clampMarkovWeight(confidence),
			Reason: "временный интервал попадает в окно найденного сетевого цикла",
		})
	}
	if bucket.StallCount > 0 {
		weight := 0.65 + float64(bucket.StallCount)*0.1 + float64(bucket.StallMaxMS)/3000
		contributors = append(contributors, MarkovSymptomWeight{
			State:  markovStalled,
			Weight: clampMarkovWeight(weight),
			Reason: fmt.Sprintf("пауз главного потока: %d, максимум %d мс", bucket.StallCount, bucket.StallMaxMS),
		})
	}
	memoryContribution := markovMemoryPressureContribution(bucket, pssFloor)
	if memoryContribution.State != "" {
		contributors = append(contributors, memoryContribution)
	}
	if bucket.UIFrames > 0 {
		rate := jankRate(bucket.UIJankyFrames, bucket.UIFrames)
		if rate >= 5 {
			contributors = append(contributors, MarkovSymptomWeight{
				State:  markovJanky,
				Weight: clampMarkovWeight(rate / 20),
				Reason: fmt.Sprintf("доля подтормаживаний %.1f%%", rate),
			})
		}
	}
	networkContribution := markovNetworkSlowContribution(bucket)
	if networkContribution.State != "" {
		contributors = append(contributors, networkContribution)
	}
	sort.SliceStable(contributors, func(i, j int) bool {
		if markovDominanceRank(contributors[i].State) != markovDominanceRank(contributors[j].State) {
			return markovDominanceRank(contributors[i].State) < markovDominanceRank(contributors[j].State)
		}
		return contributors[i].Weight > contributors[j].Weight
	})
	return contributors
}

func markovMemoryPressureContribution(bucket TimelineBucket, pssFloor uint64) MarkovSymptomWeight {
	var best MarkovSymptomWeight
	if pssFloor > 0 && bucket.MemoryPSSKB >= pssFloor+memoryGrowthFloorKB {
		growthKB := bucket.MemoryPSSKB - pssFloor
		best = MarkovSymptomWeight{
			State:  markovMemoryPressure,
			Weight: clampMarkovWeight(float64(growthKB) / float64(128*1024)),
			Reason: fmt.Sprintf("PSS выше нижней полки на %.1f МБ", float64(growthKB)/1024),
		}
	}
	if bucket.AvailableMemoryKB > 0 && bucket.AvailableMemoryKB < lowMemoryTargetKB {
		pressureKB := lowMemoryTargetKB - bucket.AvailableMemoryKB
		candidate := MarkovSymptomWeight{
			State:  markovMemoryPressure,
			Weight: clampMarkovWeight(float64(pressureKB) / float64(lowMemoryTargetKB)),
			Reason: fmt.Sprintf("свободная память ниже 256 МБ: %.1f МБ", float64(bucket.AvailableMemoryKB)/1024),
		}
		if candidate.Weight > best.Weight {
			best = candidate
		}
	}
	return best
}

func markovNetworkSlowContribution(bucket TimelineBucket) MarkovSymptomWeight {
	var best MarkovSymptomWeight
	if bucket.HTTPFailed > 0 {
		best = MarkovSymptomWeight{
			State:  markovNetworkSlow,
			Weight: clampMarkovWeight(0.75 + float64(bucket.HTTPFailed)*0.1),
			Reason: fmt.Sprintf("HTTP ошибок: %d", bucket.HTTPFailed),
		}
	}
	if bucket.HTTPCount > 0 && bucket.HTTPP95DurationMS >= 500 {
		candidate := MarkovSymptomWeight{
			State:  markovNetworkSlow,
			Weight: clampMarkovWeight(float64(bucket.HTTPP95DurationMS-300) / 1000),
			Reason: fmt.Sprintf("HTTP p95 %d мс", bucket.HTTPP95DurationMS),
		}
		if candidate.Weight > best.Weight {
			best = candidate
		}
	}
	if bucket.DNSDurationMS >= 100 {
		candidate := MarkovSymptomWeight{
			State:  markovNetworkSlow,
			Weight: clampMarkovWeight(float64(bucket.DNSDurationMS-50) / 500),
			Reason: fmt.Sprintf("DNS среднее %d мс", bucket.DNSDurationMS),
		}
		if candidate.Weight > best.Weight {
			best = candidate
		}
	}
	if bucket.ConnectDurationMS >= 150 {
		candidate := MarkovSymptomWeight{
			State:  markovNetworkSlow,
			Weight: clampMarkovWeight(float64(bucket.ConnectDurationMS-100) / 700),
			Reason: fmt.Sprintf("среднее время соединения %d мс", bucket.ConnectDurationMS),
		}
		if candidate.Weight > best.Weight {
			best = candidate
		}
	}
	return best
}

func markovDominantState(contributors []MarkovSymptomWeight) string {
	state := markovHealthy
	rank := markovDominanceRank(markovHealthy)
	for _, contributor := range contributors {
		if contributor.State == "" {
			continue
		}
		contributorRank := markovDominanceRank(contributor.State)
		if state == markovHealthy || contributorRank < rank {
			state = contributor.State
			rank = contributorRank
		}
	}
	return state
}

func markovDominanceRank(state string) int {
	switch state {
	case markovNetworkLoop:
		return 0
	case markovStalled:
		return 1
	case markovMemoryPressure:
		return 2
	case markovJanky:
		return 3
	case markovNetworkSlow:
		return 4
	case markovRecovering:
		return 5
	case markovHealthy:
		return 6
	default:
		return 7
	}
}

func clampMarkovWeight(weight float64) float64 {
	switch {
	case weight < 0.05:
		return 0.05
	case weight > 1:
		return 1
	default:
		return weight
	}
}

func markovBucketDurationMS(bucket TimelineBucket) uint64 {
	if bucket.EndMS > bucket.StartMS {
		return bucket.EndMS - bucket.StartMS
	}
	return DefaultBucketMS
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

func markovExpectedRecoveryMS(states []MarkovBucketState) float64 {
	var total float64
	var episodes int
	for index := 0; index < len(states); {
		if !markovIsBadState(states[index].State) {
			index++
			continue
		}
		var duration uint64
		for index < len(states) && markovIsBadState(states[index].State) {
			duration += markovStateDurationMS(states[index])
			index++
		}
		if index < len(states) {
			total += float64(duration)
			episodes++
		}
	}
	if episodes == 0 {
		return 0
	}
	return total / float64(episodes)
}

func markovBadEpisodeCount(states []MarkovBucketState) int {
	var episodes int
	inBadEpisode := false
	for _, state := range states {
		if markovIsBadState(state.State) {
			if !inBadEpisode {
				episodes++
				inBadEpisode = true
			}
			continue
		}
		inBadEpisode = false
	}
	return episodes
}

func markovTransitionEventCount(transitions []MarkovTransition) int {
	var count int
	for _, transition := range transitions {
		count += transition.Count
	}
	return count
}

func markovTotalDurationMS(states []MarkovBucketState) uint64 {
	var duration uint64
	for _, state := range states {
		duration += markovStateDurationMS(state)
	}
	return duration
}

func markovBadStateDurationMS(states []MarkovBucketState) uint64 {
	var duration uint64
	for _, state := range states {
		if markovIsBadState(state.State) {
			duration += markovStateDurationMS(state)
		}
	}
	return duration
}

func markovBadStateExposure(states []MarkovBucketState) float64 {
	total := markovTotalDurationMS(states)
	if total == 0 {
		return 0
	}
	return float64(markovBadStateDurationMS(states)) / float64(total)
}

func markovStateExposures(states []MarkovBucketState) []MarkovStateExposure {
	total := markovTotalDurationMS(states)
	if total == 0 {
		return nil
	}
	byState := map[string]*MarkovStateExposure{}
	for _, state := range states {
		if !markovIsBadState(state.State) {
			continue
		}
		exposure := byState[state.State]
		if exposure == nil {
			exposure = &MarkovStateExposure{State: state.State}
			byState[state.State] = exposure
		}
		exposure.Windows++
		exposure.DurationMS += markovStateDurationMS(state)
	}
	out := make([]MarkovStateExposure, 0, len(byState))
	for _, exposure := range byState {
		exposure.Exposure = float64(exposure.DurationMS) / float64(total)
		out = append(out, *exposure)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Exposure != out[j].Exposure {
			return out[i].Exposure > out[j].Exposure
		}
		return markovStateRank(out[i].State) < markovStateRank(out[j].State)
	})
	return out
}

func markovStateDurationMS(state MarkovBucketState) uint64 {
	if state.DurationMS > 0 {
		return state.DurationMS
	}
	return DefaultBucketMS
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

func markovContextStickyStates(states []MarkovBucketState) []MarkovContextStickyState {
	if len(states) < 2 {
		return nil
	}
	type contextCounts struct {
		state  string
		label  string
		total  int
		sticky int
	}
	counts := map[string]*contextCounts{}
	for index := 1; index < len(states); index++ {
		previous := states[index-1]
		if !markovIsBadState(previous.State) {
			continue
		}
		context := markovContextLabel(previous)
		if context == "" {
			continue
		}
		key := previous.State + "\x00" + context
		item := counts[key]
		if item == nil {
			item = &contextCounts{state: previous.State, label: context}
			counts[key] = item
		}
		item.total++
		current := states[index]
		if current.State == previous.State && markovContextLabel(current) == context {
			item.sticky++
		}
	}
	out := make([]MarkovContextStickyState, 0, len(counts))
	for _, item := range counts {
		if item.total == 0 || item.sticky == 0 {
			continue
		}
		out = append(out, MarkovContextStickyState{
			State:       item.state,
			Context:     item.label,
			Count:       item.sticky,
			Probability: float64(item.sticky) / float64(item.total),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Probability != out[j].Probability {
			return out[i].Probability > out[j].Probability
		}
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return markovStateRank(out[i].State) < markovStateRank(out[j].State)
	})
	if len(out) > 5 {
		out = out[:5]
	}
	return out
}

func markovContextLabel(state MarkovBucketState) string {
	var parts []string
	if state.Owner != "" {
		parts = append(parts, "источник "+state.Owner)
	}
	if state.Route != "" {
		parts = append(parts, "маршрут "+state.Route)
	}
	if state.Screen != "" {
		parts = append(parts, "экран "+state.Screen)
	}
	if state.Network != "" {
		parts = append(parts, "сеть "+state.Network)
	}
	return strings.Join(parts, " · ")
}

func compareMarkovModels(baseline, candidate MarkovModel) []MarkovDelta {
	deltas := []MarkovDelta{
		markovDeltaCount("Здоровые -> плохие состояния", "шт", float64(baseline.HealthyToBadCount), float64(candidate.HealthyToBadCount), true),
		markovDeltaProbability("Вероятность восстановления", baseline.BadToHealthyProbability, candidate.BadToHealthyProbability, false),
		markovDeltaCount("Ожидаемое восстановление", "интервалов", baseline.ExpectedRecoveryWindows, candidate.ExpectedRecoveryWindows, true),
		markovDeltaDuration("Ожидаемое восстановление по времени", baseline.ExpectedRecoveryMS, candidate.ExpectedRecoveryMS, true),
		markovDeltaProbability("Доля плохих состояний", baseline.BadStateExposure, candidate.BadStateExposure, true),
		markovDeltaTransitionMatrixDivergence(baseline, candidate),
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

func markovDeltaDuration(metric string, baseline, candidate float64, higherIsWorse bool) MarkovDelta {
	delta := candidate - baseline
	severity := markovDeltaDurationSeverity(delta, baseline, higherIsWorse)
	return MarkovDelta{
		Metric:         metric,
		Unit:           "мс",
		BaselineValue:  baseline,
		CandidateValue: candidate,
		Delta:          delta,
		Severity:       severity,
		Summary:        markovDeltaSummary(metric, baseline, candidate, delta, "мс", higherIsWorse),
	}
}

func markovDeltaTransitionMatrixDivergence(baseline, candidate MarkovModel) MarkovDelta {
	divergence := markovTransitionMatrixDivergence(baseline, candidate)
	severity := markovMatrixDivergenceSeverity(divergence, baseline, candidate)
	summary := fmt.Sprintf("Матрица переходов изменилась на %.3f по расхождению Йенсена-Шеннона; показатель близкий к 1 означает сильное изменение сценария.", divergence)
	if severity == "ok" && divergence > 0 {
		summary += " Изменение не выглядит регрессией по экспозиции и восстановлению."
	}
	return MarkovDelta{
		Metric:         "Расхождение матрицы переходов",
		Unit:           "индекс",
		BaselineValue:  0,
		CandidateValue: divergence,
		Delta:          divergence,
		Severity:       severity,
		Summary:        summary,
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
	trend := "улучшилась"
	if (higherIsWorse && delta > 0) || (!higherIsWorse && delta < 0) {
		trend = "ухудшилась"
	}
	return fmt.Sprintf("%s %s: %.1f -> %.1f %s, Δ %+.1f %s.", metric, trend, baseline, candidate, unit, delta, unit)
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
	if candidate.BadToHealthyProbability < baseline.BadToHealthyProbability-0.10 {
		return true
	}
	if baseline.ExpectedRecoveryMS == 0 {
		return candidate.ExpectedRecoveryMS >= 1000
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

func markovConfidence(states []MarkovBucketState, transitions []MarkovTransition, badEpisodes int) (string, string) {
	sampleCount := len(states)
	transitionCount := markovTransitionEventCount(transitions)
	switch {
	case sampleCount < 2:
		return "low", "меньше двух временных интервалов: матрица переходов не строится"
	case sampleCount < 4 || transitionCount < 3:
		return "low", fmt.Sprintf("окон=%d, переходов=%d: вероятности будут скачкообразными", sampleCount, transitionCount)
	case badEpisodes == 0 && sampleCount >= 10:
		return "high", fmt.Sprintf("окон=%d, плохих эпизодов нет: вывод об отсутствии деградации устойчивее", sampleCount)
	case badEpisodes == 0:
		return "medium", fmt.Sprintf("окон=%d, плохих эпизодов нет: для спокойного сценария данных достаточно, для метрик восстановления нет", sampleCount)
	case sampleCount < 10 || badEpisodes < 2:
		return "medium", fmt.Sprintf("окон=%d, плохих эпизодов=%d: восстановление и липкость лучше подтвердить повтором", sampleCount, badEpisodes)
	default:
		return "high", fmt.Sprintf("окон=%d, плохих эпизодов=%d, переходов=%d: выборка достаточна для марковских выводов", sampleCount, badEpisodes, transitionCount)
	}
}

func markovConfidenceLabel(value string) string {
	switch value {
	case "high":
		return "высокая"
	case "medium":
		return "средняя"
	case "low":
		return "низкая"
	default:
		return "неизвестная"
	}
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
	if model.Confidence == "low" {
		return "medium"
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
	return fmt.Sprintf("Состояний=%d, переходов=%d, уверенность=%s, плохая экспозиция %.1f%%, здоровые -> плохие=%d, восстановление %.1f%%, ожидание восстановления %.1f интервалов / %.0f мс.", len(model.States), len(model.Transitions), markovConfidenceLabel(model.Confidence), model.BadStateExposure*100, model.HealthyToBadCount, model.BadToHealthyProbability*100, model.ExpectedRecoveryWindows, model.ExpectedRecoveryMS)
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
	if model.Confidence == "low" {
		return []Finding{{
			Severity:       "medium",
			Title:          "Низкая уверенность марковской модели",
			Detail:         model.ConfidenceReason + ". " + markovSummary(model),
			Recommendation: "Соберите более длинный прогон или несколько повторов того же сценария, чтобы стабилизировать вероятности переходов.",
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
				Recommendation: "Посмотрите соседние временные интервалы и контекст источника/маршрута: липкость обычно означает повторяющуюся работу или отсутствие задержки повторов.",
			}}
		}
	}
	for _, sticky := range model.ContextStickyStates {
		if markovIsBadState(sticky.State) && sticky.Probability >= 0.5 {
			return []Finding{{
				Severity:       markovStatus(model),
				Title:          "Липкое плохое состояние привязано к контексту",
				Detail:         fmt.Sprintf("%s повторяется в контексте %s с вероятностью %.1f%%.", MarkovStateLabel(sticky.State), sticky.Context, sticky.Probability*100),
				Recommendation: "Проверьте источник, маршрут и экран рядом с этим контекстом: такая липкость часто указывает на повторяющуюся работу без задержки повторов или очистки.",
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
