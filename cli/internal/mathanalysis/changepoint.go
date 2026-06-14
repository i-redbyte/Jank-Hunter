package mathanalysis

import (
	"fmt"
	"math"
	"sort"
)

const (
	changeWindowBuckets  = 3
	changePointTolerance = DefaultBucketMS * 2
)

type changeSignal struct {
	name       string
	unit       string
	minDelta   float64
	noiseFloor float64
	badWhenUp  bool
	value      func(TimelineBucket) (float64, bool)
}

type changePointCandidate struct {
	point ChangePoint
	index int
}

func detectChangePoints(timeline []TimelineBucket) []ChangePoint {
	signals := []changeSignal{
		{
			name:       "HTTP p95",
			unit:       "ms",
			minDelta:   100,
			noiseFloor: 50,
			badWhenUp:  true,
			value: func(bucket TimelineBucket) (float64, bool) {
				return float64(bucket.HTTPP95DurationMS), bucket.HTTPCount > 0 && bucket.HTTPP95DurationMS > 0
			},
		},
		{
			name:       "Доля подтормаживаний UI",
			unit:       "%",
			minDelta:   5,
			noiseFloor: 2,
			badWhenUp:  true,
			value: func(bucket TimelineBucket) (float64, bool) {
				return jankRate(bucket.UIJankyFrames, bucket.UIFrames), bucket.UIFrames > 0
			},
		},
		{
			name:       "PSS памяти",
			unit:       "KB",
			minDelta:   10 * 1024,
			noiseFloor: 4 * 1024,
			badWhenUp:  true,
			value: func(bucket TimelineBucket) (float64, bool) {
				return float64(bucket.MemoryPSSKB), bucket.MemoryPSSKB > 0
			},
		},
		{
			name:       "HTTP ошибки",
			unit:       "шт",
			minDelta:   2,
			noiseFloor: 1,
			badWhenUp:  true,
			value: func(bucket TimelineBucket) (float64, bool) {
				return float64(bucket.HTTPFailed), true
			},
		},
	}

	var out []ChangePoint
	for _, signal := range signals {
		out = append(out, detectSignalChangePoints(timeline, signal)...)
	}
	sort.Slice(out, func(i, j int) bool {
		if severityRank(out[i].Severity) != severityRank(out[j].Severity) {
			return severityRank(out[i].Severity) > severityRank(out[j].Severity)
		}
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].TimeMS < out[j].TimeMS
	})
	return out
}

func detectSignalChangePoints(timeline []TimelineBucket, signal changeSignal) []ChangePoint {
	points := signalPoints(timeline, signal)
	if len(points) < changeWindowBuckets*2 {
		return nil
	}
	var candidates []changePointCandidate
	for split := changeWindowBuckets; split <= len(points)-changeWindowBuckets; split++ {
		before := pointValues(points[split-changeWindowBuckets : split])
		after := pointValues(points[split : split+changeWindowBuckets])
		beforeMedian := medianSorted(sortedFloatCopy(before))
		afterMedian := medianSorted(sortedFloatCopy(after))
		beforeMAD := medianAbsoluteDeviation(sortedFloatCopy(before), beforeMedian)
		afterMAD := medianAbsoluteDeviation(sortedFloatCopy(after), afterMedian)
		delta := afterMedian - beforeMedian
		if math.Abs(delta) < signal.minDelta {
			continue
		}
		scale := math.Max(signal.noiseFloor, (beforeMAD+afterMAD)/2)
		score := math.Abs(delta) / scale
		if score < 3 {
			continue
		}
		point := points[split]
		change := ChangePoint{
			Signal:         signal.name,
			Unit:           signal.unit,
			TimeMS:         point.bucket.StartMS,
			BeforeMedian:   beforeMedian,
			AfterMedian:    afterMedian,
			BeforeMAD:      beforeMAD,
			AfterMAD:       afterMAD,
			Delta:          delta,
			DeltaPct:       percentDelta(beforeMedian, afterMedian),
			Score:          score,
			Direction:      changeDirection(delta),
			Severity:       changePointSeverity(signal, delta, score),
			NearbyRoute:    point.bucket.RouteSample,
			NearbyOwner:    point.bucket.OwnerSample,
			NearbyScreen:   point.bucket.ScreenSample,
			NearbyNetwork:  point.bucket.NetworkSample,
			Recommendation: changePointRecommendation(signal, delta),
		}
		candidates = append(candidates, changePointCandidate{point: change, index: point.index})
	}
	return selectChangeCandidates(candidates)
}

type signalPoint struct {
	index  int
	value  float64
	bucket TimelineBucket
}

func signalPoints(timeline []TimelineBucket, signal changeSignal) []signalPoint {
	points := make([]signalPoint, 0, len(timeline))
	for index, bucket := range timeline {
		value, ok := signal.value(bucket)
		if ok {
			points = append(points, signalPoint{index: index, value: value, bucket: bucket})
		}
	}
	return points
}

func pointValues(points []signalPoint) []float64 {
	values := make([]float64, 0, len(points))
	for _, point := range points {
		values = append(values, point.value)
	}
	return values
}

func selectChangeCandidates(candidates []changePointCandidate) []ChangePoint {
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].point.Score != candidates[j].point.Score {
			return candidates[i].point.Score > candidates[j].point.Score
		}
		return candidates[i].point.TimeMS < candidates[j].point.TimeMS
	})
	selected := make([]changePointCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		tooClose := false
		for _, existing := range selected {
			if absInt(candidate.index-existing.index) <= changeWindowBuckets {
				tooClose = true
				break
			}
		}
		if !tooClose {
			selected = append(selected, candidate)
		}
		if len(selected) >= 5 {
			break
		}
	}
	out := make([]ChangePoint, 0, len(selected))
	for _, candidate := range selected {
		out = append(out, candidate.point)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TimeMS < out[j].TimeMS })
	return out
}

func compareChangePoints(baseline, candidate []ChangePoint) []ChangePointDelta {
	matchedBaseline := make([]bool, len(baseline))
	var deltas []ChangePointDelta
	for _, candidatePoint := range candidate {
		index := nearestChangePoint(baseline, candidatePoint)
		if index < 0 {
			deltas = append(deltas, appearedChangeDelta(candidatePoint))
			continue
		}
		matchedBaseline[index] = true
		baselinePoint := baseline[index]
		if candidatePoint.Score-baselinePoint.Score >= 2 {
			deltas = append(deltas, strengthenedChangeDelta(baselinePoint, candidatePoint))
		}
	}
	for index, baselinePoint := range baseline {
		if !matchedBaseline[index] {
			deltas = append(deltas, disappearedChangeDelta(baselinePoint))
		}
	}
	sort.Slice(deltas, func(i, j int) bool {
		if severityRank(deltas[i].Severity) != severityRank(deltas[j].Severity) {
			return severityRank(deltas[i].Severity) > severityRank(deltas[j].Severity)
		}
		if math.Abs(deltas[i].CandidateScore-deltas[i].BaselineScore) != math.Abs(deltas[j].CandidateScore-deltas[j].BaselineScore) {
			return math.Abs(deltas[i].CandidateScore-deltas[i].BaselineScore) > math.Abs(deltas[j].CandidateScore-deltas[j].BaselineScore)
		}
		return deltas[i].TimeMS < deltas[j].TimeMS
	})
	return deltas
}

func nearestChangePoint(points []ChangePoint, target ChangePoint) int {
	best := -1
	bestDistance := ^uint64(0)
	for index, point := range points {
		if point.Signal != target.Signal {
			continue
		}
		distance := timeDistance(point.TimeMS, target.TimeMS)
		if distance <= changePointTolerance && distance < bestDistance {
			best = index
			bestDistance = distance
		}
	}
	return best
}

func appearedChangeDelta(point ChangePoint) ChangePointDelta {
	return ChangePointDelta{
		Signal:         point.Signal,
		Status:         "появилась",
		TimeMS:         point.TimeMS,
		CandidateTime:  point.TimeMS,
		CandidateScore: point.Score,
		Severity:       point.Severity,
		Summary:        fmt.Sprintf("У кандидата появилась точка изменения %s на %.1fs: %s %.1f %s, оценка %.2f.", point.Signal, seconds(point.TimeMS), point.Direction, point.Delta, point.Unit, point.Score),
	}
}

func disappearedChangeDelta(point ChangePoint) ChangePointDelta {
	return ChangePointDelta{
		Signal:        point.Signal,
		Status:        "исчезла",
		TimeMS:        point.TimeMS,
		BaselineTime:  point.TimeMS,
		BaselineScore: point.Score,
		Severity:      "ok",
		Summary:       fmt.Sprintf("У кандидата исчезла точка изменения %s, которая была в базе на %.1fs с оценкой %.2f.", point.Signal, seconds(point.TimeMS), point.Score),
	}
}

func strengthenedChangeDelta(baseline, candidate ChangePoint) ChangePointDelta {
	severity := candidate.Severity
	if severity == "ok" {
		severity = "medium"
	}
	return ChangePointDelta{
		Signal:         candidate.Signal,
		Status:         "усилилась",
		TimeMS:         candidate.TimeMS,
		BaselineTime:   baseline.TimeMS,
		CandidateTime:  candidate.TimeMS,
		BaselineScore:  baseline.Score,
		CandidateScore: candidate.Score,
		Severity:       severity,
		Summary:        fmt.Sprintf("Точка изменения %s усилилась: оценка %.2f -> %.2f, время %.1fs -> %.1fs.", candidate.Signal, baseline.Score, candidate.Score, seconds(baseline.TimeMS), seconds(candidate.TimeMS)),
	}
}

func changePointStatus(timeline []TimelineBucket, points []ChangePoint) string {
	if len(timeline) < changeWindowBuckets*2 {
		return "medium"
	}
	for _, point := range points {
		if point.Severity == "high" {
			return "high"
		}
		if point.Severity == "medium" {
			return "medium"
		}
	}
	return "ok"
}

func changePointSummary(timeline []TimelineBucket, points []ChangePoint) string {
	if len(timeline) < changeWindowBuckets*2 {
		return fmt.Sprintf("Недостаточно данных: нужно хотя бы %d временных интервалов, сейчас %d.", changeWindowBuckets*2, len(timeline))
	}
	if len(points) == 0 {
		return "Сильных сдвигов распределения по HTTP p95, подтормаживаниям UI, памяти и сетевым ошибкам не найдено."
	}
	return fmt.Sprintf("Найдено %d точек изменения по скользящей медиане/MAD для задержек, подтормаживаний UI, памяти и сетевых ошибок.", len(points))
}

func changePointFindings(timeline []TimelineBucket, points []ChangePoint) []Finding {
	if len(timeline) < changeWindowBuckets*2 {
		return []Finding{{
			Severity:       "medium",
			Title:          "Недостаточно данных для точек изменения",
			Detail:         changePointSummary(timeline, points),
			Recommendation: "Соберите более длинный прогон: детектор сравнивает окна до и после потенциального сдвига.",
		}}
	}
	if len(points) == 0 {
		return []Finding{{
			Severity: "ok",
			Title:    "Сильных точек изменения не найдено",
			Detail:   changePointSummary(timeline, points),
		}}
	}
	worst := points[0]
	return []Finding{{
		Severity:       worst.Severity,
		Title:          "Найдена точка изменения",
		Detail:         fmt.Sprintf("%s на %.1fs: медиана %.1f -> %.1f %s, оценка %.2f.", worst.Signal, seconds(worst.TimeMS), worst.BeforeMedian, worst.AfterMedian, worst.Unit, worst.Score),
		Recommendation: worst.Recommendation,
		Evidence:       changePointEvidence(worst),
	}}
}

func compareChangePointStatus(baselineTimeline, candidateTimeline []TimelineBucket, deltas []ChangePointDelta) string {
	if len(baselineTimeline) < changeWindowBuckets*2 || len(candidateTimeline) < changeWindowBuckets*2 {
		return "medium"
	}
	if len(deltas) == 0 {
		return "ok"
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

func compareChangePointSummary(baselineTimeline, candidateTimeline []TimelineBucket, deltas []ChangePointDelta) string {
	if len(baselineTimeline) < changeWindowBuckets*2 || len(candidateTimeline) < changeWindowBuckets*2 {
		return "Недостаточно данных для сравнения точек изменения."
	}
	if len(deltas) == 0 {
		return "Новых, исчезнувших или заметно усилившихся точек изменения у кандидата не найдено."
	}
	return fmt.Sprintf("Найдено %d изменений в карте точек изменения кандидата относительно базы.", len(deltas))
}

func compareChangePointFindings(deltas []ChangePointDelta) []Finding {
	for _, delta := range deltas {
		if delta.Severity == "high" || delta.Severity == "medium" {
			return []Finding{{
				Severity:       delta.Severity,
				Title:          "Изменилась точка изменения",
				Detail:         delta.Summary,
				Recommendation: "Сопоставьте этот момент с таймлайном, маршрутом, источником и lifecycle-событиями рядом с точкой.",
			}}
		}
	}
	return []Finding{{
		Severity: "ok",
		Title:    "Регрессий по точкам изменения не найдено",
		Detail:   "Кандидат не добавил новых сильных сдвигов распределения относительно базы.",
	}}
}

func changePointEvidence(point ChangePoint) []string {
	var evidence []string
	if point.NearbyScreen != "" {
		evidence = append(evidence, "экран: "+point.NearbyScreen)
	}
	if point.NearbyRoute != "" {
		evidence = append(evidence, "маршрут: "+point.NearbyRoute)
	}
	if point.NearbyOwner != "" {
		evidence = append(evidence, "источник: "+point.NearbyOwner)
	}
	if point.NearbyNetwork != "" {
		evidence = append(evidence, "сеть: "+point.NearbyNetwork)
	}
	return evidence
}

func changePointSeverity(signal changeSignal, delta float64, score float64) string {
	if delta <= 0 || !signal.badWhenUp {
		return "ok"
	}
	if score >= 6 {
		return "high"
	}
	return "medium"
}

func changePointRecommendation(signal changeSignal, delta float64) string {
	if delta <= 0 {
		return "Сдвиг выглядит как улучшение; проверьте, совпадает ли он с окончанием загрузки экрана или восстановлением сети."
	}
	switch signal.name {
	case "HTTP p95":
		return "Проверьте маршруты и источники рядом с точкой: возможен переход сети в медленный режим или всплеск серверной задержки."
	case "Доля подтормаживаний UI":
		return "Проверьте переходы экранов, работу главного потока и метрики executor/coroutine рядом с этим временем."
	case "PSS памяти":
		return "Проверьте удержанные объекты, давление GC/heap и lifecycle-события рядом с ростом базового уровня памяти."
	case "HTTP ошибки":
		return "Проверьте DNS, соединения, retry/reconnect метрики и ошибки конкретного маршрута вокруг этого окна."
	default:
		return "Проверьте соседние события и источники работ вокруг этой точки изменения."
	}
}

func changeDirection(delta float64) string {
	if delta >= 0 {
		return "рост"
	}
	return "снижение"
}

func percentDelta(before, after float64) float64 {
	if before == 0 {
		return 0
	}
	return (after - before) * 100 / before
}

func timeDistance(a, b uint64) uint64 {
	if a > b {
		return a - b
	}
	return b - a
}

func seconds(ms uint64) float64 {
	return float64(ms) / 1000
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
