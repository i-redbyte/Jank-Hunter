package mathanalysis

import (
	"fmt"
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
	recoveryProbability, hasRecoveryProbability := markovBadToHealthyProbability(transitions)
	expectedRecoveryWindows, hasExpectedRecovery := markovExpectedRecoveryWindows(states)
	expectedRecoveryMS, _ := markovExpectedRecoveryMS(states)
	missingBucketCount := len(timeline) - len(states)
	coverage := markovObservationCoverage(len(timeline), len(states))
	if coverage < 0.8 && len(timeline) > 0 {
		confidence = "low"
		confidenceReason = fmt.Sprintf("измерено %d из %d временных интервалов: пропуски не считаются здоровыми и разрывают последовательность", len(states), len(timeline))
	} else if missingBucketCount > 0 && confidence == "high" {
		confidence = "medium"
		confidenceReason += fmt.Sprintf("; измерено %d из %d интервалов, переходы через пропуски исключены", len(states), len(timeline))
	}
	model := MarkovModel{
		States:                  states,
		Transitions:             transitions,
		SampleCount:             len(states),
		TimelineBucketCount:     len(timeline),
		MissingBucketCount:      missingBucketCount,
		ObservationCoverage:     coverage,
		TransitionEventCount:    markovTransitionEventCount(transitions),
		BadEpisodeCount:         badEpisodes,
		IndependentRunCount:     1,
		SequenceComparable:      true,
		Confidence:              confidence,
		ConfidenceReason:        confidenceReason,
		HealthyToBadCount:       markovHealthyToBadCount(transitions),
		BadToHealthyProbability: recoveryProbability,
		HasRecoveryProbability:  hasRecoveryProbability,
		ExpectedRecoveryWindows: expectedRecoveryWindows,
		ExpectedRecoveryMS:      expectedRecoveryMS,
		HasExpectedRecovery:     hasExpectedRecovery,
		TotalDurationMS:         markovTotalDurationMS(states),
		BadStateDurationMS:      markovBadStateDurationMS(states),
		BadStateExposure:        markovBadStateExposure(states),
		StateExposures:          markovStateExposures(states),
		StickyStates:            markovStickyStates(transitions),
		ContextStickyStates:     markovContextStickyStates(states),
	}
	model.Forecast = buildMarkovForecast(model)
	return model
}

func buildMarkovModelForRuns(timeline []TimelineBucket, loops []NetworkLoopFinding, runCount int) MarkovModel {
	model := buildMarkovModel(timeline, loops)
	model.IndependentRunCount = normalizedRunCount(runCount)
	if model.IndependentRunCount == 1 {
		return model
	}
	model.SequenceComparable = false
	model.Transitions = nil
	model.TransitionEventCount = 0
	model.BadEpisodeCount = 0
	model.HealthyToBadCount = 0
	model.BadToHealthyProbability = 0
	model.HasRecoveryProbability = false
	model.ExpectedRecoveryWindows = 0
	model.ExpectedRecoveryMS = 0
	model.HasExpectedRecovery = false
	model.StickyStates = nil
	model.ContextStickyStates = nil
	model.Confidence = "low"
	model.ConfidenceReason = fmt.Sprintf("объединены независимые прогоны: %d; состояния описывают позицию внутри общего сценария, а не хронологию одного запуска", model.IndependentRunCount)
	model.Forecast = MarkovForecast{
		Direction:        markovForecastInsufficient,
		Label:            "Прогноз для объединённых прогонов отключён",
		Severity:         "medium",
		Confidence:       "low",
		ConfidenceReason: model.ConfidenceReason,
		Summary:          "Состояния разных запусков совмещены по относительному времени. Из такой агрегированной последовательности нельзя честно предсказывать, что произойдёт дальше в одном запуске.",
	}
	return model
}

func classifyMarkovStates(timeline []TimelineBucket, loops []NetworkLoopFinding) []MarkovBucketState {
	pssFloor := minNonZeroPSS(timeline)
	states := make([]MarkovBucketState, 0, len(timeline))
	previousBad := false
	usesObservationFlags := timelineUsesObservationFlags(timeline)
	for _, bucket := range timeline {
		if usesObservationFlags && !bucket.HasObservation {
			previousBad = false
			continue
		}
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
		return markovHealthy, "в доступных сигналах нет выраженной деградации", nil
	}
	state := markovDominantState(contributors)
	for _, contributor := range contributors {
		if contributor.State == state {
			return state, contributor.Reason, contributors
		}
	}
	return state, MarkovStateLabel(state), contributors
}

func timelineUsesObservationFlags(timeline []TimelineBucket) bool {
	for _, bucket := range timeline {
		if bucket.HasObservation {
			return true
		}
	}
	return false
}

func markovObservationCoverage(total, observed int) float64 {
	if total <= 0 {
		return 0
	}
	return float64(observed) / float64(total)
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
		if !markovStatesAdjacent(states[i-1], states[i]) {
			continue
		}
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

func markovBadToHealthyProbability(transitions []MarkovTransition) (float64, bool) {
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
		return 0, false
	}
	return float64(recovered) / float64(badOutgoing), true
}

func markovExpectedRecoveryWindows(states []MarkovBucketState) (float64, bool) {
	var total float64
	var episodes int
	for index := 0; index < len(states); index++ {
		if !markovIsBadState(states[index].State) {
			continue
		}
		length := 0
		for {
			length++
			if index+1 >= len(states) || !markovStatesAdjacent(states[index], states[index+1]) || !markovIsBadState(states[index+1].State) {
				break
			}
			index++
		}
		if index+1 < len(states) && markovStatesAdjacent(states[index], states[index+1]) {
			total += float64(length)
			episodes++
		}
	}
	if episodes == 0 {
		return 0, false
	}
	return total / float64(episodes), true
}

func markovExpectedRecoveryMS(states []MarkovBucketState) (float64, bool) {
	var total float64
	var episodes int
	for index := 0; index < len(states); index++ {
		if !markovIsBadState(states[index].State) {
			continue
		}
		var duration uint64
		for {
			duration += markovStateDurationMS(states[index])
			if index+1 >= len(states) || !markovStatesAdjacent(states[index], states[index+1]) || !markovIsBadState(states[index+1].State) {
				break
			}
			index++
		}
		if index+1 < len(states) && markovStatesAdjacent(states[index], states[index+1]) {
			total += float64(duration)
			episodes++
		}
	}
	if episodes == 0 {
		return 0, false
	}
	return total / float64(episodes), true
}

func markovBadEpisodeCount(states []MarkovBucketState) int {
	var episodes int
	inBadEpisode := false
	for index, state := range states {
		if index > 0 && !markovStatesAdjacent(states[index-1], state) {
			inBadEpisode = false
		}
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

func markovStatesAdjacent(previous, current MarkovBucketState) bool {
	return saturatingAddUint64(previous.TimeMS, markovStateDurationMS(previous)) == current.TimeMS
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
		if !markovStatesAdjacent(previous, states[index]) {
			continue
		}
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
		parts = append(parts, "место запуска "+analysisOwnerLabel(state.Owner))
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
