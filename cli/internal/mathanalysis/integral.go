package mathanalysis

import (
	"fmt"
	"math"
	"sort"
)

const (
	latencyPainTargetMS     = 300
	mainThreadStallTargetMS = 100
	lowMemoryTargetKB       = 256 * 1024
	memoryGrowthFloorKB     = 32 * 1024
)

type integralDefinition struct {
	id          string
	title       string
	formula     string
	explanation string
	unit        string
	value       func([]TimelineBucket, []NetworkLoopFinding) float64
}

func computeIntegralScores(timeline []TimelineBucket, loops []NetworkLoopFinding) []IntegralScore {
	return computeIntegralScoresForRuns(timeline, loops, 1)
}

func computeIntegralScoresForRuns(timeline []TimelineBucket, loops []NetworkLoopFinding, runCount int) []IntegralScore {
	if len(timeline) == 0 {
		return nil
	}
	runCount = normalizedRunCount(runCount)
	definitions := []integralDefinition{
		{
			id:          "jank_pressure_area",
			title:       "Площадь подтормаживаний UI",
			formula:     "Σ ((janky_frames / frames) * 100) * Δt",
			explanation: "Суммирует долю медленных UI-кадров по времени: короткий пик и длинная умеренная деградация получают разный вес.",
			unit:        "%*с",
			value:       jankPressureArea,
		},
		{
			id:          "latency_pain_area",
			title:       "Площадь сетевой задержки",
			formula:     "Σ max(0, HTTP p95 - 300ms) * Δt",
			explanation: "Интегрирует только хвост задержки выше инженерного целевого порога 300 мс.",
			unit:        "мс*с",
			value:       latencyPainArea,
		},
		{
			id:          "main_thread_stall_burden",
			title:       "Накопленное превышение пауз главного потока",
			formula:     "Σ max(0, max_пауза_в_интервале - 100ms)",
			explanation: "Суммирует превышение максимальной паузы над 100 мс в каждом временном интервале. Это не полная длительность всех пауз: в таймлайне хранится максимум интервала.",
			unit:        "мс",
			value:       mainThreadStallBurden,
		},
		{
			id:          "network_failure_burn",
			title:       "Накопленная нагрузка сетевых повторов",
			formula:     "Σ (HTTP_ошибки + 0.25*DNS + 0.25*соединение) * Δt + Σ нагрузка_цикла",
			explanation: "Объединяет ошибки, всплески DNS и соединения, а также условную стоимость найденных сетевых циклов. Значение не является временем, трафиком или расходом батареи.",
			unit:        "усл.ед.",
			value:       networkFailureBurn,
		},
		{
			id:          "memory_pressure_area",
			title:       "Площадь давления памяти",
			formula:     "Σ max(0, PSS - p10(PSS)) * Δt + Σ max(0, 256МБ - свободная_RAM) * Δt",
			explanation: "Показывает рост PSS относительно нижнего устойчивого уровня p10 и длительность низкого запаса RAM. p10 означает значение, ниже которого находится примерно 10% замеров PSS.",
			unit:        "МБ*с",
			value:       memoryPressureArea,
		},
		{
			id:          "recovery_debt",
			title:       "Долг восстановления",
			formula:     "Σ длительность_плохой_серии * Δt",
			explanation: "Чем дольше подряд идут плохие окна, тем быстрее растет долг восстановления.",
			unit:        "с^2",
			value:       recoveryDebt,
		},
	}

	scores := make([]IntegralScore, 0, len(definitions))
	durationMS := integralTimelineDurationMS(timeline)
	for _, definition := range definitions {
		value := definition.value(timeline, loops)
		if definition.id == "network_failure_burn" {
			value /= float64(runCount)
		}
		score := IntegralScore{
			ID:          definition.id,
			Title:       definition.title,
			Formula:     definition.formula,
			Explanation: definition.explanation,
			Unit:        definition.unit,
			Value:       value,
			DurationMS:  durationMS,
			RunCount:    runCount,
			Severity:    integralSeverity(definition.id, value),
		}
		if definition.id == "network_failure_burn" && runCount > 1 {
			score.Formula = "(Σ (HTTP_ошибки + 0.25*DNS + 0.25*соединение) * Δt + Σ нагрузка_цикла) / число_прогонов"
			score.Explanation += " При объединении независимых прогонов сумма делится на их количество, чтобы число повторов само по себе не увеличивало оценку."
		}
		score.Summary = fmt.Sprintf("%s: %.1f %s; независимых прогонов: %d. %s", score.Title, score.Value, score.Unit, score.RunCount, score.Explanation)
		scores = append(scores, score)
	}
	return scores
}

func compareIntegralScores(baseline, candidate []IntegralScore) []IntegralDelta {
	baselineByID := integralByID(baseline)
	candidateByID := integralByID(candidate)
	ids := make([]string, 0, len(baselineByID)+len(candidateByID))
	seen := map[string]struct{}{}
	for _, score := range baseline {
		ids = append(ids, score.ID)
		seen[score.ID] = struct{}{}
	}
	for _, score := range candidate {
		if _, ok := seen[score.ID]; !ok {
			ids = append(ids, score.ID)
		}
	}
	deltas := make([]IntegralDelta, 0, len(ids))
	for _, id := range ids {
		base := baselineByID[id]
		cand := candidateByID[id]
		delta := cand.Value - base.Value
		deltaPct := percentDelta(base.Value, cand.Value)
		baseRunCount := normalizedRunCount(base.RunCount)
		candidateRunCount := normalizedRunCount(cand.RunCount)
		comparable := integralDurationsComparable(base.DurationMS, cand.DurationMS) && baseRunCount == candidateRunCount
		deltaPctAvailable := comparable && base.Value != 0
		severity := "medium"
		if comparable {
			severity = integralDeltaSeverity(id, delta, deltaPct)
		}
		deltas = append(deltas, IntegralDelta{
			ID:                  id,
			Title:               firstNonEmpty(cand.Title, base.Title, id),
			Formula:             firstNonEmpty(cand.Formula, base.Formula),
			Unit:                firstNonEmpty(cand.Unit, base.Unit),
			BaselineValue:       base.Value,
			CandidateValue:      cand.Value,
			Delta:               delta,
			DeltaPct:            deltaPct,
			DeltaPctAvailable:   deltaPctAvailable,
			Comparable:          comparable,
			BaselineDurationMS:  base.DurationMS,
			CandidateDurationMS: cand.DurationMS,
			BaselineRunCount:    baseRunCount,
			CandidateRunCount:   candidateRunCount,
			Severity:            severity,
			Summary:             integralDeltaSummary(firstNonEmpty(cand.Title, base.Title, id), base.Value, cand.Value, delta, deltaPct, firstNonEmpty(cand.Unit, base.Unit), comparable, base.DurationMS, cand.DurationMS, baseRunCount, candidateRunCount),
		})
	}
	return deltas
}

func normalizedRunCount(runCount int) int {
	if runCount < 1 {
		return 1
	}
	return runCount
}

func integralTimelineDurationMS(timeline []TimelineBucket) uint64 {
	var duration uint64
	for _, bucket := range timeline {
		if bucket.EndMS > bucket.StartMS {
			duration = saturatingAddUint64(duration, bucket.EndMS-bucket.StartMS)
		}
	}
	return duration
}

func integralDurationsComparable(baselineMS, candidateMS uint64) bool {
	if baselineMS == 0 && candidateMS == 0 {
		return true
	}
	if baselineMS == 0 || candidateMS == 0 {
		return false
	}
	shorter := baselineMS
	longer := candidateMS
	if shorter > longer {
		shorter, longer = longer, shorter
	}
	return float64(longer)/float64(shorter) <= 1.20
}

func jankPressureArea(timeline []TimelineBucket, _ []NetworkLoopFinding) float64 {
	var area float64
	for _, bucket := range timeline {
		if bucket.UIFrames == 0 {
			continue
		}
		area += jankRate(bucket.UIJankyFrames, bucket.UIFrames) * bucketDurationSeconds(bucket)
	}
	return area
}

func latencyPainArea(timeline []TimelineBucket, _ []NetworkLoopFinding) float64 {
	var area float64
	for _, bucket := range timeline {
		if bucket.HTTPCount == 0 || bucket.HTTPP95DurationMS <= latencyPainTargetMS {
			continue
		}
		area += float64(bucket.HTTPP95DurationMS-latencyPainTargetMS) * bucketDurationSeconds(bucket)
	}
	return area
}

func mainThreadStallBurden(timeline []TimelineBucket, _ []NetworkLoopFinding) float64 {
	var burden float64
	for _, bucket := range timeline {
		if bucket.StallCount == 0 || bucket.StallMaxMS <= mainThreadStallTargetMS {
			continue
		}
		burden += float64(bucket.StallMaxMS - mainThreadStallTargetMS)
	}
	return burden
}

func networkFailureBurn(timeline []TimelineBucket, loops []NetworkLoopFinding) float64 {
	var score float64
	for _, bucket := range timeline {
		bucketScore := float64(bucket.HTTPFailed) + float64(bucket.DNSCount)*0.25 + float64(bucket.ConnectCount)*0.25
		score += bucketScore * bucketDurationSeconds(bucket)
	}
	for _, loop := range loops {
		score += loop.BurnScore
	}
	return score
}

func memoryPressureArea(timeline []TimelineBucket, _ []NetworkLoopFinding) float64 {
	pssFloor := minNonZeroPSS(timeline)
	var area float64
	for _, bucket := range timeline {
		seconds := bucketDurationSeconds(bucket)
		if pssFloor > 0 && bucket.MemoryPSSKB > pssFloor {
			area += float64(bucket.MemoryPSSKB-pssFloor) / 1024 * seconds
		}
		if bucket.AvailableMemoryKB > 0 && bucket.AvailableMemoryKB < lowMemoryTargetKB {
			area += float64(lowMemoryTargetKB-bucket.AvailableMemoryKB) / 1024 * seconds
		}
	}
	return area
}

func recoveryDebt(timeline []TimelineBucket, _ []NetworkLoopFinding) float64 {
	pssFloor := minNonZeroPSS(timeline)
	var debt float64
	var badStreak float64
	usesObservationFlags := timelineUsesObservationFlags(timeline)
	for _, bucket := range timeline {
		if usesObservationFlags && !bucket.HasObservation {
			badStreak = 0
			continue
		}
		seconds := bucketDurationSeconds(bucket)
		if integralBadBucket(bucket, pssFloor) {
			badStreak += seconds
			debt += badStreak * seconds
			continue
		}
		badStreak = 0
	}
	return debt
}

func integralBadBucket(bucket TimelineBucket, pssFloor uint64) bool {
	if bucket.UIFrames > 0 && jankRate(bucket.UIJankyFrames, bucket.UIFrames) >= 5 {
		return true
	}
	if bucket.HTTPCount > 0 && bucket.HTTPP95DurationMS >= 500 {
		return true
	}
	if bucket.HTTPFailed > 0 {
		return true
	}
	if bucket.StallCount > 0 {
		return true
	}
	if pssFloor > 0 && bucket.MemoryPSSKB >= pssFloor+memoryGrowthFloorKB {
		return true
	}
	return bucket.AvailableMemoryKB > 0 && bucket.AvailableMemoryKB < lowMemoryTargetKB
}

func bucketDurationSeconds(bucket TimelineBucket) float64 {
	if bucket.EndMS > bucket.StartMS {
		return float64(bucket.EndMS-bucket.StartMS) / 1000
	}
	return float64(DefaultBucketMS) / 1000
}

func minNonZeroPSS(timeline []TimelineBucket) uint64 {
	var values []uint64
	for _, bucket := range timeline {
		if bucket.MemoryPSSKB > 0 {
			values = append(values, bucket.MemoryPSSKB)
		}
	}
	if len(values) == 0 {
		return 0
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	return percentileSorted(values, 0.10)
}

func integralByID(scores []IntegralScore) map[string]IntegralScore {
	out := make(map[string]IntegralScore, len(scores))
	for _, score := range scores {
		out[score.ID] = score
	}
	return out
}

func integralSeverity(id string, value float64) string {
	medium, high := integralThresholds(id)
	switch {
	case value >= high:
		return "high"
	case value >= medium:
		return "medium"
	default:
		return "ok"
	}
}

func integralDeltaSeverity(id string, delta, deltaPct float64) string {
	if delta <= 0 {
		return "ok"
	}
	medium, high := integralThresholds(id)
	switch {
	case delta >= high*0.35 || deltaPct >= 50:
		return "high"
	case delta >= medium*0.35 || deltaPct >= 15:
		return "medium"
	default:
		return "ok"
	}
}

func integralThresholds(id string) (medium, high float64) {
	switch id {
	case "jank_pressure_area":
		return 60, 180
	case "latency_pain_area":
		return 500, 2000
	case "main_thread_stall_burden":
		return 500, 2000
	case "network_failure_burn":
		return 5, 20
	case "memory_pressure_area":
		return 128, 1024
	case "recovery_debt":
		return 8, 30
	default:
		return math.Inf(1), math.Inf(1)
	}
}

func integralStatus(scores []IntegralScore) string {
	if len(scores) == 0 {
		return "medium"
	}
	status := "ok"
	for _, score := range scores {
		if score.Severity == "high" {
			return "high"
		}
		if score.Severity == "medium" {
			status = "medium"
		}
	}
	return status
}

func integralSummary(scores []IntegralScore) string {
	if len(scores) == 0 {
		return "Недостаточно временных интервалов для интегральной оценки."
	}
	return fmt.Sprintf("Посчитано %d накопленных оценок: подтормаживания UI, сетевая задержка, паузы главного потока, сетевые повторы, память и долг восстановления.", len(scores))
}

func integralFindings(scores []IntegralScore) []Finding {
	if len(scores) == 0 {
		return []Finding{{
			Severity:       "medium",
			Title:          "Недостаточно данных для интегральной оценки",
			Detail:         "Нужен хотя бы один временной интервал с UI, HTTP, памятью или контекстными сигналами.",
			Recommendation: "Соберите более длинный прогон сценария.",
		}}
	}
	worst := worstIntegralScore(scores)
	if worst.Severity == "ok" {
		return []Finding{{
			Severity: "ok",
			Title:    "Интегральные оценки в норме",
			Detail:   integralSummary(scores),
		}}
	}
	return []Finding{{
		Severity:       worst.Severity,
		Title:          "Накопленная нагрузка заметна",
		Detail:         worst.Summary,
		Recommendation: "Посмотрите соседние разделы: таймлайн показывает, где накопилась нагрузка, а распределения, точки изменения и сетевые циклы помогают сузить контекст проверки, но сами не доказывают причину.",
	}}
}

func compareIntegralSummary(deltas []IntegralDelta) string {
	if len(deltas) == 0 {
		return "Недостаточно интегральных оценок для сравнения."
	}
	var worse int
	var incomparable int
	for _, delta := range deltas {
		if !delta.Comparable {
			incomparable++
			continue
		}
		if delta.Delta > 0 {
			worse++
		}
	}
	if incomparable > 0 {
		return fmt.Sprintf("Сопоставимо %d из %d накопленных оценок; для %d оценок длительность различается больше чем на 20%% или число независимых прогонов не совпадает. Среди сопоставимых выросло %d.", len(deltas)-incomparable, len(deltas), incomparable, worse)
	}
	if worse == 0 {
		return "Кандидат не увеличил интегральную нагрузку относительно базы."
	}
	return fmt.Sprintf("Кандидат увеличил %d из %d интегральных оценок нагрузки.", worse, len(deltas))
}

func compareIntegralFindings(deltas []IntegralDelta) []Finding {
	if len(deltas) == 0 {
		return []Finding{{
			Severity: "medium",
			Title:    "Интегральные дельты недоступны",
			Detail:   "Один из прогонов не дал временных интервалов.",
		}}
	}
	var findings []Finding
	for _, delta := range deltas {
		if !delta.Comparable {
			findings = append(findings, Finding{
				Severity:       "medium",
				Title:          "Часть накопленных оценок несопоставима",
				Detail:         delta.Summary,
				Recommendation: "Повторите одинаковый сценарий с тем же числом повторов и сопоставимой длительностью или сравнивайте робастные распределения, которые не суммируют нагрузку по всему времени прогона.",
			})
			break
		}
	}
	var worst *IntegralDelta
	for _, delta := range deltas {
		if !delta.Comparable || delta.Severity == "ok" {
			continue
		}
		if worst == nil || severityRank(delta.Severity) > severityRank(worst.Severity) || (delta.Severity == worst.Severity && delta.Delta > worst.Delta) {
			item := delta
			worst = &item
		}
	}
	if worst != nil {
		findings = append(findings, Finding{
			Severity:       worst.Severity,
			Title:          "Интегральная нагрузка выросла",
			Detail:         worst.Summary,
			Recommendation: "Сравните эту оценку с таймлайном, точками изменения и сетевыми циклами: накопленная оценка показывает масштаб эффекта, а соседние разделы помогают выбрать следующую проверку.",
		})
	}
	if len(findings) > 0 {
		return findings
	}
	return []Finding{{
		Severity: "ok",
		Title:    "Интегральная нагрузка не выросла",
		Detail:   compareIntegralSummary(deltas),
	}}
}

func worstIntegralScore(scores []IntegralScore) IntegralScore {
	worst := scores[0]
	for _, score := range scores[1:] {
		if severityRank(score.Severity) > severityRank(worst.Severity) {
			worst = score
			continue
		}
		if score.Severity == worst.Severity && score.Value > worst.Value {
			worst = score
		}
	}
	return worst
}

func integralDeltaSummary(title string, baseline, candidate, delta, deltaPct float64, unit string, comparable bool, baselineDurationMS, candidateDurationMS uint64, baselineRunCount, candidateRunCount int) string {
	if !comparable {
		return fmt.Sprintf("%s: база %.1f %s за %.1f с (независимых прогонов: %d), кандидат %.1f %s за %.1f с (независимых прогонов: %d). Длительность различается больше чем на 20%% или число прогонов не совпадает, поэтому изменение не считается регрессией автоматически.", title, baseline, unit, seconds(baselineDurationMS), baselineRunCount, candidate, unit, seconds(candidateDurationMS), candidateRunCount)
	}
	if delta <= 0 {
		return fmt.Sprintf("%s улучшилась или не выросла: %.1f -> %.1f %s.", title, baseline, candidate, unit)
	}
	if baseline == 0 {
		return fmt.Sprintf("%s появилась у кандидата: в базе 0, у кандидата %.1f %s. Процент не рассчитывается, потому что база равна нулю.", title, candidate, unit)
	}
	return fmt.Sprintf("%s выросла: %.1f -> %.1f %s, Δ %.1f %s (%+.1f%%).", title, baseline, candidate, unit, delta, unit, deltaPct)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
