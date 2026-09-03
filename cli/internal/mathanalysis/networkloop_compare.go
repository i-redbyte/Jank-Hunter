package mathanalysis

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

func compareNetworkLoops(baseline, candidate []NetworkLoopFinding) []NetworkLoopDelta {
	baselineByKey := map[string]NetworkLoopFinding{}
	for _, loop := range baseline {
		baselineByKey[networkLoopKey(loop)] = loop
	}
	matched := map[string]struct{}{}
	var deltas []NetworkLoopDelta
	for _, candidateLoop := range candidate {
		key := networkLoopKey(candidateLoop)
		baselineLoop, ok := baselineByKey[key]
		if !ok {
			deltas = append(deltas, appearedNetworkLoopDelta(candidateLoop))
			continue
		}
		matched[key] = struct{}{}
		if delta, ok := changedNetworkLoopDelta(baselineLoop, candidateLoop); ok {
			deltas = append(deltas, delta)
		}
	}
	for _, baselineLoop := range baseline {
		key := networkLoopKey(baselineLoop)
		if _, ok := matched[key]; ok {
			continue
		}
		if _, stillExists := baselineByKey[key]; stillExists {
			deltas = append(deltas, disappearedNetworkLoopDelta(baselineLoop))
		}
	}
	sort.Slice(deltas, func(i, j int) bool {
		if severityRank(deltas[i].Severity) != severityRank(deltas[j].Severity) {
			return severityRank(deltas[i].Severity) > severityRank(deltas[j].Severity)
		}
		if math.Abs(deltas[i].BurnDelta) != math.Abs(deltas[j].BurnDelta) {
			return math.Abs(deltas[i].BurnDelta) > math.Abs(deltas[j].BurnDelta)
		}
		return deltas[i].Summary < deltas[j].Summary
	})
	return deltas
}

func appearedNetworkLoopDelta(loop NetworkLoopFinding) NetworkLoopDelta {
	return NetworkLoopDelta{
		Route:             loop.Route,
		Owner:             loop.Owner,
		Status:            "появился",
		CandidatePeriodMS: loop.PeriodMS,
		CandidateBurn:     loop.BurnScore,
		BurnDelta:         loop.BurnScore,
		ConfidenceDelta:   loop.Confidence,
		Severity:          networkLoopFindingSeverity(loop),
		Summary:           fmt.Sprintf("В проверяемом прогоне появился признак сетевого цикла: период %.1f сек, доверие %.2f, условная нагрузка %.1f. %s", seconds(loop.PeriodMS), loop.Confidence, loop.BurnScore, loop.ProbableCause),
	}
}

func disappearedNetworkLoopDelta(loop NetworkLoopFinding) NetworkLoopDelta {
	return NetworkLoopDelta{
		Route:            loop.Route,
		Owner:            loop.Owner,
		Status:           "исчез",
		BaselinePeriodMS: loop.PeriodMS,
		BaselineBurn:     loop.BurnScore,
		BurnDelta:        -loop.BurnScore,
		ConfidenceDelta:  -loop.Confidence,
		Severity:         "ok",
		Summary:          fmt.Sprintf("В проверяемом прогоне исчез признак сетевого цикла из базового: период %.1f сек, доверие %.2f, условная нагрузка %.1f.", seconds(loop.PeriodMS), loop.Confidence, loop.BurnScore),
	}
}

func changedNetworkLoopDelta(baseline, candidate NetworkLoopFinding) (NetworkLoopDelta, bool) {
	burnDelta := candidate.BurnScore - baseline.BurnScore
	confidenceDelta := candidate.Confidence - baseline.Confidence
	periodChanged := !periodClose(baseline.PeriodMS, candidate.PeriodMS)
	burnChanged := math.Abs(burnDelta) >= 3 || math.Abs(percentDelta(baseline.BurnScore, candidate.BurnScore)) >= 35
	confidenceChanged := math.Abs(confidenceDelta) >= 0.15
	if !periodChanged && !burnChanged && !confidenceChanged {
		return NetworkLoopDelta{}, false
	}
	status := "изменился"
	severity := "medium"
	if burnDelta > 0 || confidenceDelta > 0.15 {
		status = "усилился"
		severity = networkLoopFindingSeverity(candidate)
		if severity == "ok" {
			severity = "medium"
		}
	}
	if burnDelta < -3 && confidenceDelta <= 0 {
		status = "ослаб"
		severity = "ok"
	}
	return NetworkLoopDelta{
		Route:             candidate.Route,
		Owner:             candidate.Owner,
		Status:            status,
		BaselinePeriodMS:  baseline.PeriodMS,
		CandidatePeriodMS: candidate.PeriodMS,
		BaselineBurn:      baseline.BurnScore,
		CandidateBurn:     candidate.BurnScore,
		BurnDelta:         burnDelta,
		ConfidenceDelta:   confidenceDelta,
		Severity:          severity,
		Summary:           fmt.Sprintf("Признак сетевого цикла %s: период %.1f сек -> %.1f сек, условная нагрузка %.1f -> %.1f, доверие %.2f -> %.2f.", status, seconds(baseline.PeriodMS), seconds(candidate.PeriodMS), baseline.BurnScore, candidate.BurnScore, baseline.Confidence, candidate.Confidence),
	}, true
}

func networkLoopStatus(loops []NetworkLoopFinding) string {
	status := "ok"
	for _, loop := range loops {
		severity := networkLoopFindingSeverity(loop)
		if severity == "high" {
			return "high"
		}
		if severity == "medium" {
			status = "medium"
		}
	}
	return status
}

func networkLoopSummary(loops []NetworkLoopFinding) string {
	if len(loops) == 0 {
		return "Сетевых циклов по DNS, соединениям, переподключениям, WebSocket и всплескам маршрутов не найдено."
	}
	return fmt.Sprintf("Найдено %d признаков сетевых циклов. Каждый прошёл порог по повторяемости и совокупной уверенности; отдельные методы могут давать разную силу подтверждения.", len(loops))
}

func networkLoopFindings(loops []NetworkLoopFinding) []Finding {
	if len(loops) == 0 {
		return []Finding{{
			Severity: "ok",
			Title:    "Сетевые циклы не найдены",
			Detail:   "Повторяющихся DNS-запросов, соединений, переподключений или всплесков маршрута/источника с достаточным доверием нет.",
		}}
	}
	worst := loops[0]
	return []Finding{{
		Severity:       networkLoopFindingSeverity(worst),
		Title:          "Найден признак сетевого цикла",
		Detail:         fmt.Sprintf("Предполагаемый период %.1fs, доверие %.2f, условная нагрузка %.1f. Повторяющаяся последовательность: %s. Это гипотеза, а не доказанная причина.", seconds(worst.PeriodMS), worst.Confidence, worst.BurnScore, NetworkLoopMotifText(worst.Motif)),
		Recommendation: worst.ProbableCause,
		Evidence:       networkLoopEvidence(worst),
	}}
}

func compareNetworkLoopSummary(deltas []NetworkLoopDelta) string {
	if len(deltas) == 0 {
		return "Новых, исчезнувших или заметно усилившихся сетевых циклов не найдено."
	}
	return fmt.Sprintf("Найдено %d изменений признаков сетевых циклов: появление или исчезновение, смена периода, условной нагрузки либо поддержки данными.", len(deltas))
}

func compareNetworkLoopFindings(deltas []NetworkLoopDelta) []Finding {
	for _, delta := range deltas {
		if delta.Severity == "high" || delta.Severity == "medium" {
			return []Finding{{
				Severity:       delta.Severity,
				Title:          "Изменился признак сетевого цикла",
				Detail:         delta.Summary,
				Recommendation: "Проверьте маршрут, источник, DNS, соединения, повторы и WebSocket-события с тем же периодом; для Android смотрите OkHttp EventListener и владельца корутины или обновления.",
			}}
		}
	}
	return []Finding{{
		Severity: "ok",
		Title:    "Регрессий сетевых циклов не найдено",
		Detail:   compareNetworkLoopSummary(deltas),
	}}
}

func networkLoopFindingSeverity(loop NetworkLoopFinding) string {
	if loop.Confidence >= 0.70 && loop.BurnScore >= 8 {
		return "high"
	}
	if loop.Confidence >= networkLoopConfidenceWarn || loop.BurnScore >= networkLoopBurnThreshold {
		return "medium"
	}
	return "ok"
}

func networkLoopEvidence(loop NetworkLoopFinding) []string {
	var evidence []string
	if loop.Route != "" {
		evidence = append(evidence, "маршрут: "+loop.Route)
	}
	if loop.Owner != "" {
		evidence = append(evidence, "место запуска: "+analysisOwnerLabel(loop.Owner))
		if !analysisOwnerIsKnown(loop.Owner) {
			evidence = append(evidence, missingOwnerAction())
		}
	}
	evidence = append(evidence, fmt.Sprintf("интервал: %.1f сек..%.1f сек", seconds(loop.FirstMS), seconds(loop.LastMS)))
	if len(loop.Path.Nodes) > 0 {
		evidence = append(evidence, "путь: "+strings.Join(loop.Path.Nodes, " -> "))
	}
	return evidence
}
