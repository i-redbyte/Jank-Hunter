package mathanalysis

import (
	"math"
	"sort"
	"strings"
)

func analyzeNetworkLoopSignal(signal *networkLoopSignal, bucketMS uint64) (NetworkLoopFinding, bool) {
	if len(signal.points) < minNetworkLoopPoints || !hasNonZeroFloat(signal.points) {
		return NetworkLoopFinding{}, false
	}
	bursts := networkLoopBurstIndexes(signal.points)
	if len(bursts) < minNetworkLoopBursts {
		return NetworkLoopFinding{}, false
	}
	periodic := analyzePeriodicSignal(signal.name, "шт", bucketMS, signal.points)
	periodMS := networkLoopPeriod(periodic, bursts, bucketMS)
	if periodMS == 0 {
		return NetworkLoopFinding{}, false
	}
	regularity := networkLoopRegularity(bursts, periodMS, bucketMS)
	if regularity < 0.45 && topPeakConfidence(periodic) < 0.25 && periodic.FirstSignificantLagMS == 0 {
		return NetworkLoopFinding{}, false
	}
	motif := networkLoopMotif(signal, bursts)
	motifScore := networkLoopMotifScore(signal, bursts, motif)
	burstScore := math.Min(1, float64(len(bursts))/6)
	autocorrScore := networkLoopAutocorrScore(periodic, periodMS)
	spectralScore := topPeakConfidence(periodic)
	confidence := burstScore*0.20 + regularity*0.30 + autocorrScore*0.25 + spectralScore*0.15 + motifScore*0.10
	if confidence < 0.35 {
		return NetworkLoopFinding{}, false
	}
	route := signal.route
	owner := signal.owner
	if route == "" {
		route = uniqueMotifValue(motif, "route:")
	}
	if owner == "" {
		owner = uniqueMotifValue(motif, "owner:")
	}
	burnScore := networkLoopBurn(signal.points, bursts, confidence)
	return NetworkLoopFinding{
		Route:         route,
		Owner:         owner,
		PeriodMS:      periodMS,
		Confidence:    clamp01(confidence),
		Motif:         motif,
		FirstMS:       uint64(bursts[0]) * bucketMS,
		LastMS:        uint64(bursts[len(bursts)-1]) * bucketMS,
		BurnScore:     burnScore,
		ProbableCause: networkLoopProbableCause(signal.kind, route, owner),
		Path:          networkLoopPath(signal.kind, route, owner, motif, confidence),
	}, true
}

func networkLoopBurstIndexes(points []float64) []int {
	positive := make([]float64, 0, len(points))
	for _, value := range points {
		if value > 0 {
			positive = append(positive, value)
		}
	}
	if len(positive) == 0 {
		return nil
	}
	all := sortedFloatCopy(points)
	median := medianSorted(all)
	mad := medianAbsoluteDeviation(all, median)
	positiveMedian := medianSorted(sortedFloatCopy(positive))
	threshold := math.Max(1, median+3*mad)
	threshold = math.Max(threshold, median*2)
	if median == 0 && positiveMedian > 1 {
		threshold = math.Max(threshold, positiveMedian)
	}
	out := make([]int, 0, len(points))
	for index, value := range points {
		if value >= threshold && value > 0 {
			out = append(out, index)
		}
	}
	return out
}

func networkLoopPeriod(signal PeriodicSignal, bursts []int, bucketMS uint64) uint64 {
	burstPeriod := adjacentBurstPeriod(bursts, bucketMS)
	if signal.FirstSignificantLagMS > 0 && periodClose(signal.FirstSignificantLagMS, burstPeriod) {
		return signal.FirstSignificantLagMS
	}
	if signal.FirstSignificantLagMS > 0 && burstPeriod == 0 {
		return signal.FirstSignificantLagMS
	}
	if len(signal.Peaks) > 0 {
		peakPeriod := signal.Peaks[0].PeriodMS
		if burstPeriod == 0 || periodClose(peakPeriod, burstPeriod) || signal.Peaks[0].Confidence >= 0.45 {
			return peakPeriod
		}
	}
	return burstPeriod
}

func adjacentBurstPeriod(bursts []int, bucketMS uint64) uint64 {
	if len(bursts) < 2 {
		return 0
	}
	distances := make([]float64, 0, len(bursts)-1)
	for i := 1; i < len(bursts); i++ {
		distance := bursts[i] - bursts[i-1]
		if distance > 0 {
			distances = append(distances, float64(distance))
		}
	}
	if len(distances) == 0 {
		return 0
	}
	median := medianSorted(sortedFloatCopy(distances))
	return uint64(math.Round(median)) * bucketMS
}

func periodClose(a, b uint64) bool {
	if a == 0 || b == 0 {
		return false
	}
	delta := math.Abs(float64(a) - float64(b))
	return delta/math.Max(float64(a), float64(b)) <= networkLoopPeriodEpsilon
}

func networkLoopRegularity(bursts []int, periodMS, bucketMS uint64) float64 {
	if len(bursts) < 2 || periodMS == 0 || bucketMS == 0 {
		return 0
	}
	expected := int(math.Round(float64(periodMS) / float64(bucketMS)))
	if expected <= 0 {
		return 0
	}
	var matched int
	for i := 1; i < len(bursts); i++ {
		if absInt((bursts[i]-bursts[i-1])-expected) <= 1 {
			matched++
		}
	}
	return float64(matched) / float64(len(bursts)-1)
}

func networkLoopAutocorrScore(signal PeriodicSignal, periodMS uint64) float64 {
	if signal.FirstSignificantLagMS > 0 && periodClose(signal.FirstSignificantLagMS, periodMS) {
		return 1
	}
	var best float64
	for _, lag := range signal.TopLags {
		if periodClose(lag.LagMS, periodMS) && lag.Correlation > best {
			best = lag.Correlation
		}
	}
	return clamp01(best)
}

func networkLoopMotif(signal *networkLoopSignal, bursts []int) []string {
	type tokenCount struct {
		token   string
		buckets int
		total   int
	}
	counts := map[string]*tokenCount{}
	for _, index := range bursts {
		for token, total := range signal.tokens[index] {
			item := counts[token]
			if item == nil {
				item = &tokenCount{token: token}
				counts[token] = item
			}
			item.buckets++
			item.total += total
		}
	}
	if _, ok := counts[networkLoopKindToken(signal.kind)]; !ok {
		token := networkLoopKindToken(signal.kind)
		if token != "" {
			counts[token] = &tokenCount{token: token, buckets: len(bursts), total: len(bursts)}
		}
	}
	items := make([]tokenCount, 0, len(counts))
	for _, item := range counts {
		if item.buckets >= 2 || strings.HasPrefix(item.token, "route:") || strings.HasPrefix(item.token, "owner:") {
			items = append(items, *item)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if tokenPriority(items[i].token) != tokenPriority(items[j].token) {
			return tokenPriority(items[i].token) < tokenPriority(items[j].token)
		}
		if items[i].buckets != items[j].buckets {
			return items[i].buckets > items[j].buckets
		}
		if items[i].total != items[j].total {
			return items[i].total > items[j].total
		}
		return items[i].token < items[j].token
	})
	if len(items) > 5 {
		items = items[:5]
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.token)
	}
	return out
}

func networkLoopMotifScore(signal *networkLoopSignal, bursts []int, motif []string) float64 {
	if len(bursts) == 0 || len(motif) == 0 {
		return 0
	}
	var repeated int
	for _, token := range motif {
		var buckets int
		for _, index := range bursts {
			if signal.tokens[index][token] > 0 {
				buckets++
			}
		}
		if buckets >= 2 {
			repeated++
		}
	}
	return math.Min(1, float64(repeated)/3)
}

func networkLoopBurn(points []float64, bursts []int, confidence float64) float64 {
	var total float64
	for _, index := range bursts {
		total += points[index]
	}
	return total * (1 + confidence)
}

func selectNetworkLoops(candidates []NetworkLoopFinding) []NetworkLoopFinding {
	sort.Slice(candidates, func(i, j int) bool {
		if severityRank(networkLoopFindingSeverity(candidates[i])) != severityRank(networkLoopFindingSeverity(candidates[j])) {
			return severityRank(networkLoopFindingSeverity(candidates[i])) > severityRank(networkLoopFindingSeverity(candidates[j]))
		}
		if candidates[i].Confidence != candidates[j].Confidence {
			return candidates[i].Confidence > candidates[j].Confidence
		}
		if networkLoopSpecificity(candidates[i]) != networkLoopSpecificity(candidates[j]) {
			return networkLoopSpecificity(candidates[i]) > networkLoopSpecificity(candidates[j])
		}
		if candidates[i].BurnScore != candidates[j].BurnScore {
			return candidates[i].BurnScore > candidates[j].BurnScore
		}
		return networkLoopKey(candidates[i]) < networkLoopKey(candidates[j])
	})
	seen := map[string]struct{}{}
	out := make([]NetworkLoopFinding, 0, maxNetworkLoopFindings)
	for _, candidate := range candidates {
		key := networkLoopKey(candidate)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, candidate)
		if len(out) >= maxNetworkLoopFindings {
			break
		}
	}
	return out
}
