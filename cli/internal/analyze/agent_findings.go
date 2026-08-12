package analyze

import (
	"fmt"
	"sort"
	"strings"
)

type agentEvidenceMatch struct {
	gc            AgentInterval
	contention    AgentInterval
	stack         agentStackSample
	hasGC         bool
	hasContention bool
	hasStack      bool
}

func (a *agentAggregator) buildFindings(summary AgentSummary) []AgentFinding {
	symptoms := append([]agentSymptom(nil), a.symptoms...)
	sort.Slice(symptoms, func(i, j int) bool {
		return symptomWeight(symptoms[i]) > symptomWeight(symptoms[j])
	})

	findings := make([]AgentFinding, 0, maxAgentFindings)
	for index, symptom := range symptoms {
		if len(findings) >= maxAgentFindings {
			break
		}
		match := a.matchEvidence(symptom)
		if !match.hasAny() {
			continue
		}
		findings = append(findings, a.buildFinding(index, symptom, match, summary))
	}
	if len(findings) == 0 && summary.Contention.Count > 0 {
		findings = append(findings, fallbackContentionFinding(summary))
	}
	return findings
}

func (a *agentAggregator) matchEvidence(symptom agentSymptom) agentEvidenceMatch {
	match := agentEvidenceMatch{}
	match.gc, match.hasGC = bestOverlap(symptom, a.gc.Top)
	match.contention, match.hasContention = bestOverlap(symptom, a.contention.Top)
	match.stack, match.hasStack = a.closestStack(symptom)
	return match
}

func (match agentEvidenceMatch) hasAny() bool {
	return match.hasGC || match.hasContention || match.hasStack
}

func (a *agentAggregator) buildFinding(
	index int,
	symptom agentSymptom,
	match agentEvidenceMatch,
	summary AgentSummary,
) AgentFinding {
	finding := newAgentFinding(index, symptom)
	if match.hasGC {
		addGCEvidence(&finding, symptom, match.gc, summary)
	}
	if match.hasContention {
		addContentionEvidence(&finding, symptom, match.contention)
	}
	if match.hasStack {
		a.addStackEvidence(&finding, symptom, match.stack)
	}
	finalizeFinding(&finding, match, summary)
	return finding
}

func newAgentFinding(index int, symptom agentSymptom) AgentFinding {
	return AgentFinding{
		ID:              fmt.Sprintf("artti-%d", index+1),
		Symptom:         symptomDescription(symptom),
		Screen:          symptom.context.screen,
		Flow:            symptom.context.flow,
		Owner:           symptom.context.owner,
		ConfidenceScore: baseAgentConfidence,
		MissingOrCounterEvidence: []string{
			"Временное совпадение и снимок стека не доказывают, что наблюдаемый метод вызвал сборку мусора или ожидание блокировки и был единственной причиной задержки.",
		},
		AlternativeExplanations: []string{
			"Другая работа главного потока, ввод-вывод, задержка планировщика или отрисовка могли совпасть с тем же окном.",
			"Сборка мусора могла быть следствием большого числа выделений памяти, а не первичной причиной задержки.",
		},
		Actions: []string{
			"Повторить тот же сценарий с теми же экраном, сценарием и источником работы.",
			"Сравнить с прогоном без подозреваемой работы и проверить, повторяется ли временная связь.",
		},
		TimelineReference: fmt.Sprintf("%s@%d..%d ns", symptom.source, symptom.startNS, symptom.endNS),
	}
}

func addGCEvidence(
	finding *AgentFinding,
	symptom agentSymptom,
	interval AgentInterval,
	summary AgentSummary,
) {
	overlap := intervalOverlapNS(symptom, interval)
	finding.EvidenceChain = append(finding.EvidenceChain, AgentEvidenceStep{
		Level:       "DIRECT",
		Statement:   "Сборка мусора пересекла окно задержки",
		Measurement: fmt.Sprintf("пауза %.3f мс; пересечение %.3f мс", interval.DurationMS, float64(overlap)/1e6),
		EventIDs:    []uint64{interval.Sequence},
	})
	finding.ExactMeasurements = append(
		finding.ExactMeasurements,
		fmt.Sprintf("суммарная пауза сборки мусора %.3f мс, максимальная %.3f мс", summary.GC.TotalMS, summary.GC.MaxMS),
	)
	finding.PositiveEvidence = append(
		finding.PositiveEvidence,
		fmt.Sprintf("Измерено %.3f мс пересечения сборки мусора с проблемным окном.", float64(overlap)/1e6),
	)
	finding.ConfidenceScore += evidenceConfidenceIncrement
}

func addContentionEvidence(
	finding *AgentFinding,
	symptom agentSymptom,
	interval AgentInterval,
) {
	overlap := intervalOverlapNS(symptom, interval)
	finding.ThreadToken = interval.ThreadToken
	finding.EvidenceChain = append(finding.EvidenceChain, AgentEvidenceStep{
		Level:       "DIRECT",
		Statement:   "Ожидание блокировки пересекло окно задержки",
		Measurement: fmt.Sprintf("ожидание %.3f мс; пересечение %.3f мс", interval.DurationMS, float64(overlap)/1e6),
		EventIDs:    []uint64{interval.Sequence},
	})
	finding.PositiveEvidence = append(
		finding.PositiveEvidence,
		"Зафиксирован завершённый интервал ожидания блокировки в том же временном окне.",
	)
	finding.ConfidenceScore += evidenceConfidenceIncrement
}

func (a *agentAggregator) addStackEvidence(
	finding *AgentFinding,
	symptom agentSymptom,
	sample agentStackSample,
) {
	methods := a.stackMethods(sample.fingerprint)
	method := firstInterestingMethod(methods)
	finding.ThreadToken = firstNonZero(finding.ThreadToken, sample.thread)
	finding.Method = method

	statement := "Снимок стека зафиксирован рядом с задержкой"
	if method != "" {
		statement += "; наблюдаемый метод " + method
	}
	stackEvidence := []AgentEvidenceStep{
		{
			Level:       "DIRECT",
			Statement:   symptomDescription(symptom),
			Measurement: fmt.Sprintf("временное окно %d..%d нс", symptom.startNS, symptom.endNS),
		},
		{
			Level:       "DIRECT",
			Statement:   statement,
			Measurement: fmt.Sprintf("отпечаток стека 0x%016x", sample.fingerprint),
			EventIDs:    []uint64{sample.sequence},
		},
	}
	finding.EvidenceChain = append(stackEvidence, finding.EvidenceChain...)
	finding.PositiveEvidence = append(
		finding.PositiveEvidence,
		"Снимок стека и задержка относятся к одному источнику данных и ограниченному временному окну.",
	)
	finding.ConfidenceScore += evidenceConfidenceIncrement

	if containsImageDecode(methods) {
		finding.SuspectedCause = "Декодирование или подготовка изображения на наблюдаемом потоке вместе с интенсивным выделением памяти и сборкой мусора"
		finding.Actions = append(
			[]string{"Перенести декодирование и изменение размера изображения с главного потока, использовать заранее подготовленные миниатюры и повторить сценарий."},
			finding.Actions...,
		)
	}
}

func finalizeFinding(finding *AgentFinding, match agentEvidenceMatch, summary AgentSummary) {
	if finding.SuspectedCause == "" {
		switch {
		case match.hasContention:
			finding.SuspectedCause = "Ожидание блокировки в потоке, пересёкшее окно задержки"
		case match.hasGC:
			finding.SuspectedCause = "Пауза сборки мусора и интенсивное выделение памяти, совпавшие с окном задержки"
		default:
			finding.SuspectedCause = "Работа из снимка стека рядом с задержкой"
		}
	}

	if match.hasStack && (match.hasGC || match.hasContention) {
		finding.EvidenceLevel = "STRONG_ASSOCIATION"
	} else {
		finding.EvidenceLevel = "TEMPORAL_CORRELATION"
	}
	if !match.hasStack {
		finding.MissingOrCounterEvidence = append(
			finding.MissingOrCounterEvidence,
			"В ограниченном временном окне нет снимка стека.",
		)
	}
	if !match.hasGC {
		finding.MissingOrCounterEvidence = append(
			finding.MissingOrCounterEvidence,
			"Пересечение со сборкой мусора не зафиксировано.",
		)
	}

	penalty := qualityPenalty(summary)
	finding.ConfidenceScore -= penalty
	if penalty > 0 {
		finding.DataQualityPenalties = append(
			finding.DataQualityPenalties,
			strings.Join(summary.DataGaps, " "),
		)
	}
	finding.ConfidenceScore = clamp(finding.ConfidenceScore, minAgentConfidence, maxAgentConfidence)
	finding.Confidence = confidenceLabel(finding.ConfidenceScore)
}

func fallbackContentionFinding(summary AgentSummary) AgentFinding {
	top := summary.Contention.Top[0]
	score := clamp(fallbackContentionConfidence-qualityPenalty(summary), minAgentConfidence, maxAgentConfidence)
	return AgentFinding{
		ID:              "artti-contention-1",
		EvidenceLevel:   "DIRECT",
		Symptom:         "Длительное ожидание блокировки",
		SuspectedCause:  "Конкуренция за блокировку",
		ThreadToken:     top.ThreadToken,
		ConfidenceScore: score,
		Confidence:      confidenceLabel(score),
		ExactMeasurements: []string{
			fmt.Sprintf("максимум %.3f мс; суммарно %.3f мс", summary.Contention.MaxMS, summary.Contention.TotalMS),
		},
		PositiveEvidence:         []string{"Агент зафиксировал завершённый интервал ожидания блокировки."},
		MissingOrCounterEvidence: []string{"Нет связанной паузы главного потока или проблемного окна интерфейса."},
		AlternativeExplanations:  []string{"Ожидание могло происходить в фоновом потоке и не влиять на интерфейс."},
		Actions:                  []string{"Найти владельца блокировки и убрать длительную работу из синхронизированного участка."},
		TimelineReference:        fmt.Sprintf("%s, событие %d", top.Source, top.Sequence),
	}
}

func (a *agentAggregator) closestStack(symptom agentSymptom) (agentStackSample, bool) {
	var best agentStackSample
	distance := ^uint64(0)
	found := false
	for _, sample := range a.stackSamples {
		if sample.source != symptom.source {
			continue
		}
		current := distanceToWindow(sample.timeNS, symptom.startNS, symptom.endNS)
		if current <= agentTemporalWindowNS && current < distance {
			best = sample
			distance = current
			found = true
		}
	}
	return best, found
}

func (a *agentAggregator) stackMethods(fingerprint uint64) []string {
	state := a.stacks[fingerprint]
	if state == nil {
		return nil
	}
	methods := make([]string, 0, len(state.methodIDs))
	for _, id := range state.methodIDs {
		if method := a.methods[id]; method != "" {
			methods = append(methods, method)
		}
	}
	return methods
}

func bestOverlap(symptom agentSymptom, intervals []AgentInterval) (AgentInterval, bool) {
	var best AgentInterval
	var overlap uint64
	for _, interval := range intervals {
		if interval.Source != symptom.source {
			continue
		}
		current := intervalOverlapNS(symptom, interval)
		if current > overlap {
			best = interval
			overlap = current
		}
	}
	return best, overlap > 0
}

func intervalOverlapNS(symptom agentSymptom, interval AgentInterval) uint64 {
	end := saturatingAdd(interval.StartNS, interval.DurationNS)
	start := maxUint64(symptom.startNS, interval.StartNS)
	stop := minUint64(symptom.endNS, end)
	if stop <= start {
		return 0
	}
	return stop - start
}

func distanceToWindow(value, start, end uint64) uint64 {
	switch {
	case value < start:
		return start - value
	case value > end:
		return value - end
	default:
		return 0
	}
}

func symptomWeight(value agentSymptom) uint64 {
	return saturatingAdd(value.durationMS, value.jankFrames*16)
}

func symptomDescription(value agentSymptom) string {
	if value.kind == "main_thread_stall" {
		return fmt.Sprintf("Пауза главного потока %d мс в %s", value.durationMS, value.context.label())
	}
	return fmt.Sprintf("Проблемное окно интерфейса: медленных кадров %d в %s", value.jankFrames, value.context.label())
}

func qualityPenalty(summary AgentSummary) int {
	penalty := 0
	if summary.Quality.SequenceGaps > 0 {
		penalty += 15
	}
	if summary.Quality.QueueFullTotal+
		summary.Quality.AdmissionContentionTotal+
		summary.Quality.OtherNativeLossTotal > 0 {
		penalty += 15
	}
	if summary.Stacks.MissingDefinitions > 0 {
		penalty += 10
	}
	if !summary.Quality.CompatibleClockCalibration {
		penalty += 10
	}
	if summary.Capabilities.Missing != 0 {
		penalty += 10
	}
	return min(penalty, maxQualityPenalty)
}

func confidenceLabel(score int) string {
	switch {
	case score >= 80:
		return "HIGH"
	case score >= 55:
		return "MEDIUM"
	default:
		return "LOW"
	}
}

func containsImageDecode(methods []string) bool {
	for _, method := range methods {
		lower := strings.ToLower(method)
		if strings.Contains(lower, "bitmapfactory") ||
			(strings.Contains(lower, "decode") && strings.Contains(lower, "image")) {
			return true
		}
	}
	return false
}

func firstInterestingMethod(methods []string) string {
	for _, method := range methods {
		if containsImageDecode([]string{method}) {
			return method
		}
	}
	if len(methods) > 0 {
		return methods[0]
	}
	return ""
}

const (
	maxAgentFindings             = 8
	baseAgentConfidence          = 45
	evidenceConfidenceIncrement  = 20
	fallbackContentionConfidence = 70
	minAgentConfidence           = 5
	maxAgentConfidence           = 95
	maxQualityPenalty            = 45
)
