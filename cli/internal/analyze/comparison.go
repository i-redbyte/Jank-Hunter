package analyze

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/datavalue"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func Compare(baseline, candidate Summary) Comparison {
	comparison := Comparison{Baseline: baseline, Candidate: candidate}
	confidence := confidence(baseline, candidate)
	baselineLogSpam := totalLogSpam(baseline)
	candidateLogSpam := totalLogSpam(candidate)
	baselineProblemWindows := totalProblemWindows(baseline)
	candidateProblemWindows := totalProblemWindows(candidate)
	comparison.Deltas = append(comparison.Deltas,
		observedDelta("HTTP p95", baseline.HTTPP95MS, candidate.HTTPP95MS, "мс", true, uint64(baseline.HTTPCount), uint64(candidate.HTTPCount), "HTTP-запросы не зафиксированы"),
		observedDeltaFloat("HTTP failure rate", percentCount(baseline.HTTPFailed, baseline.HTTPCount), percentCount(candidate.HTTPFailed, candidate.HTTPCount), "п.п.", true, uint64(baseline.HTTPCount), uint64(candidate.HTTPCount), "HTTP-запросы не зафиксированы"),
		observedDeltaFloat("UI jank rate", baseline.UIJankPct, candidate.UIJankPct, "п.п.", true, baseline.UIFrames, candidate.UIFrames, "UI-кадры не зафиксированы"),
		observedDeltaFloat("UI avg FPS", baseline.UIAvgFPS, candidate.UIAvgFPS, "FPS", false, baseline.UIFPSMeasuredFrames, candidate.UIFPSMeasuredFrames, "недостаточно непрерывных UI-кадров для оценки FPS"),
		delta("Main-thread stall max", baseline.StallMaxMS, candidate.StallMaxMS, "мс", true, minUint64(uint64(baseline.StallCount), uint64(candidate.StallCount))),
		observedDelta("Max PSS", baseline.MemoryMaxKB, candidate.MemoryMaxKB, "КБ", true, uint64(baseline.MemoryCount), uint64(candidate.MemoryCount), "PSS не измерялся"),
		observedDelta("Min available memory", baseline.AvailMemoryMinKB, candidate.AvailMemoryMinKB, "КБ", false, uint64(baseline.ContextCount), uint64(candidate.ContextCount), "снимки контекста памяти отсутствуют"),
		observedDelta("UID RX delta", baseline.TrafficRxMax, candidate.TrafficRxMax, "байт", true, uint64(baseline.ContextCount), uint64(candidate.ContextCount), "снимки сетевого контекста отсутствуют"),
		observedDelta("UID TX delta", baseline.TrafficTxMax, candidate.TrafficTxMax, "байт", true, uint64(baseline.ContextCount), uint64(candidate.ContextCount), "снимки сетевого контекста отсутствуют"),
		delta("Retained objects", baseline.Retained, candidate.Retained, "шт", true, minUint64(baseline.Retained, candidate.Retained)),
		durationRateDelta("Log spam", baselineLogSpam, candidateLogSpam, baseline.DurationMS, candidate.DurationMS, minUint64(baselineLogSpam, candidateLogSpam)),
		durationRateDelta("Problem windows", baselineProblemWindows, candidateProblemWindows, baseline.DurationMS, candidate.DurationMS, minUint64(baselineProblemWindows, candidateProblemWindows)),
		mixDelta("Process mix", baseline.Processes, candidate.Processes, minUint64(uint64(baseline.LogCount), uint64(candidate.LogCount))),
		mixDelta("App version mix", baseline.AppVersions, candidate.AppVersions, minUint64(uint64(baseline.LogCount), uint64(candidate.LogCount))),
		mixDelta("SDK mix", baseline.SDKs, candidate.SDKs, minUint64(uint64(baseline.LogCount), uint64(candidate.LogCount))),
		mixDelta("Device mix", baseline.Devices, candidate.Devices, minUint64(uint64(baseline.LogCount), uint64(candidate.LogCount))),
		mixDelta("Network mix", baseline.Network, candidate.Network, minUint64(uint64(baseline.ContextCount), uint64(candidate.ContextCount))),
		mixDelta("Cohort mix", baseline.Cohorts, candidate.Cohorts, minUint64(uint64(baseline.EventCount), uint64(candidate.EventCount))),
	)
	for i := range comparison.Deltas {
		comparison.Deltas[i].Confidence = confidence
		comparison.Deltas[i].Severity = adjustedSeverity(
			comparison.Deltas[i].Severity,
			confidence,
			comparison.Deltas[i].SampleSize,
		)
	}
	comparison.CohortWarnings = cohortWarnings(baseline, candidate)
	comparison.QualityWarnings = comparisonQualityWarnings(baseline, candidate)
	comparison.ExposureWarnings = durationComparisonWarnings(baseline, candidate)
	comparison.Database = compareDatabaseAnalysis(baseline, candidate)
	comparison.AndroidComponents = compareAndroidComponentAnalysis(baseline, candidate)
	comparison.OperationDeltas = compareOperationAnalysis(baseline, candidate)
	androidPartial := baseline.AndroidComponents != nil && baseline.AndroidComponents.Partial ||
		candidate.AndroidComponents != nil && candidate.AndroidComponents.Partial
	if comparison.AndroidComponents.Note != "" && (androidPartial ||
		!comparison.AndroidComponents.Comparable) && (baseline.AndroidComponents != nil || candidate.AndroidComponents != nil) {
		comparison.QualityWarnings = append(comparison.QualityWarnings, "Android Components/IPC: "+comparison.AndroidComponents.Note)
	}
	comparison.Warnings = append(append(append([]string{}, comparison.CohortWarnings...), comparison.QualityWarnings...), comparison.ExposureWarnings...)
	comparison.ProblemComparison = CompareProblems(baseline, candidate, len(comparison.CohortWarnings) == 0)
	return comparison
}

func mixDelta(name string, baseline, candidate []NamedValue, sampleSize uint64) Delta {
	baselineTotal := namedValueTotal(baseline)
	candidateTotal := namedValueTotal(candidate)
	before := namedShareSummary(baseline)
	after := namedShareSummary(candidate)
	result := Delta{
		Name:       name,
		Baseline:   before,
		Candidate:  after,
		Change:     "без существенных изменений",
		Severity:   "ok",
		Comparable: true,
		SampleSize: sampleSize,
		Interval:   sampleNote(sampleSize),
	}
	if baselineTotal == 0 || candidateTotal == 0 {
		if baselineTotal == 0 {
			result.Baseline = "нет данных"
		}
		if candidateTotal == 0 {
			result.Candidate = "нет данных"
		}
		return markDeltaUnavailable(result, baselineTotal, candidateTotal, "категориальный состав отсутствует хотя бы в одном прогоне")
	}
	distance := namedDistributionDistance(baseline, candidate)
	result.ComparisonNote = fmt.Sprintf("сравниваются доли категорий, а не абсолютное число служебных событий; суммарное различие долей %.1f п.п.", distance*100)
	severity := "ok"
	if distance > 0.05 {
		severity = "medium"
		result.Change = "доли изменились"
	}
	result.Severity = severity
	return result
}

func observedDelta(name string, before, after uint64, unit string, higherIsWorse bool, baselineSamples, candidateSamples uint64, absence string) Delta {
	result := delta(name, before, after, unit, higherIsWorse, minUint64(baselineSamples, candidateSamples))
	if baselineSamples > 0 && candidateSamples > 0 {
		return result
	}
	if baselineSamples == 0 {
		result.Baseline = "нет данных"
	}
	if candidateSamples == 0 {
		result.Candidate = "нет данных"
	}
	return markDeltaUnavailable(result, baselineSamples, candidateSamples, absence+" хотя бы в одном прогоне")
}

func observedDeltaFloat(name string, before, after float64, unit string, higherIsWorse bool, baselineSamples, candidateSamples uint64, absence string) Delta {
	result := deltaFloat(name, before, after, unit, higherIsWorse, minUint64(baselineSamples, candidateSamples))
	if baselineSamples > 0 && candidateSamples > 0 {
		return result
	}
	if baselineSamples == 0 {
		result.Baseline = "нет данных"
	}
	if candidateSamples == 0 {
		result.Candidate = "нет данных"
	}
	return markDeltaUnavailable(result, baselineSamples, candidateSamples, absence+" хотя бы в одном прогоне")
}

func markDeltaUnavailable(result Delta, baselineSamples, candidateSamples uint64, reason string) Delta {
	result.Change = "не сравнивается"
	result.Severity = "ok"
	result.Interval = fmt.Sprintf("база=%d, кандидат=%d", baselineSamples, candidateSamples)
	result.Comparable = false
	result.ComparisonNote = reason
	result.ChangeAbs = 0
	result.ChangePct = 0
	result.RegressionAbs = 0
	result.RegressionPct = 0
	result.SampleSize = minUint64(baselineSamples, candidateSamples)
	return result
}

func durationRateDelta(name string, before, after, baselineDurationMS, candidateDurationMS, sampleSize uint64) Delta {
	if baselineDurationMS == 0 || candidateDurationMS == 0 {
		baseline := "нет данных"
		candidate := "нет данных"
		if baselineDurationMS > 0 {
			baseline = fmt.Sprintf("%.2f шт/мин", float64(before)*60_000/float64(baselineDurationMS))
		}
		if candidateDurationMS > 0 {
			candidate = fmt.Sprintf("%.2f шт/мин", float64(after)*60_000/float64(candidateDurationMS))
		}
		return markDeltaUnavailable(Delta{Name: name, Baseline: baseline, Candidate: candidate, Unit: "шт/мин"}, baselineDurationMS, candidateDurationMS, "длительность хотя бы одного прогона неизвестна")
	}
	baselineRate := float64(before) * 60_000 / float64(baselineDurationMS)
	candidateRate := float64(after) * 60_000 / float64(candidateDurationMS)
	result := relativeDeltaFloat(name, baselineRate, candidateRate, "шт/мин", true, sampleSize)
	result.ComparisonNote = fmt.Sprintf("нормировано по длительности: %d и %d событий", before, after)
	return result
}

func relativeDeltaFloat(name string, before, after float64, unit string, higherIsWorse bool, sampleSize uint64) Delta {
	diff := after - before
	changePct := 0.0
	severity := "ok"
	regressionAbs := 0.0
	regressionPct := 0.0
	change := "0.0%"
	if before == 0 && after > 0 {
		change = "+new"
		if higherIsWorse {
			severity = "medium"
			regressionAbs = after
			regressionPct = 100
		}
	} else if before != 0 {
		changePct = diff * 100 / before
		change = fmt.Sprintf("%+.1f%%", changePct)
		if higherIsWorse && changePct > 0 {
			regressionAbs = diff
			regressionPct = changePct
		} else if !higherIsWorse && changePct < 0 {
			regressionAbs = math.Abs(diff)
			regressionPct = math.Abs(changePct)
		}
		if regressionPct >= 25 {
			severity = "high"
		} else if regressionPct >= 10 {
			severity = "medium"
		}
	}
	return Delta{
		Name:           name,
		Baseline:       fmt.Sprintf("%.2f %s", before, unit),
		Candidate:      fmt.Sprintf("%.2f %s", after, unit),
		Change:         change,
		Severity:       severity,
		Interval:       sampleNote(sampleSize),
		Comparable:     true,
		Unit:           unit,
		BaselineValue:  before,
		CandidateValue: after,
		ChangeAbs:      diff,
		ChangePct:      changePct,
		RegressionAbs:  regressionAbs,
		RegressionPct:  regressionPct,
		SampleSize:     sampleSize,
	}
}

func totalLogSpam(summary Summary) uint64 {
	var total uint64
	for _, item := range summary.LogSpam {
		total += item.Count
	}
	return total
}

func percentCount(part, total int) float64 {
	if total <= 0 {
		return 0
	}
	return float64(part) * 100 / float64(total)
}

func totalProblemWindows(summary Summary) uint64 {
	var total uint64
	for _, item := range summary.ProblemWindows {
		total += uint64(item.Windows)
	}
	return total
}

func cohortWarnings(baseline, candidate Summary) []string {
	checks := []struct {
		name      string
		baseline  []NamedValue
		candidate []NamedValue
	}{
		{name: "версий приложения", baseline: baseline.AppVersions, candidate: candidate.AppVersions},
		{name: "SDK", baseline: baseline.SDKs, candidate: candidate.SDKs},
		{name: "устройств", baseline: baseline.Devices, candidate: candidate.Devices},
		{name: "процессов", baseline: baseline.Processes, candidate: candidate.Processes},
		{name: "сетей", baseline: baseline.Network, candidate: candidate.Network},
		{name: "когорт", baseline: baseline.Cohorts, candidate: candidate.Cohorts},
	}
	var warnings []string
	for _, check := range checks {
		if namedValueTotal(check.baseline) == 0 || namedValueTotal(check.candidate) == 0 {
			continue
		}
		before := namedShareSummary(check.baseline)
		after := namedShareSummary(check.candidate)
		if namedDistributionDistance(check.baseline, check.candidate) > 0.05 {
			warnings = append(warnings, fmt.Sprintf("Состав %s отличается: база [%s], кандидат [%s].", check.name, before, after))
		}
	}
	return warnings
}

func durationComparisonWarnings(baseline, candidate Summary) []string {
	if baseline.DurationMS == 0 || candidate.DurationMS == 0 {
		return []string{"Длительность хотя бы одного прогона неизвестна: частотные метрики не сравниваются."}
	}
	shorter := baseline.DurationMS
	longer := candidate.DurationMS
	if shorter > longer {
		shorter, longer = longer, shorter
	}
	if float64(longer-shorter)/float64(shorter) <= 0.2 {
		return nil
	}
	return []string{fmt.Sprintf(
		"Длительность прогонов отличается больше чем на 20%%: база %s, кандидат %s. Максимумы и редкие события могли получить разную экспозицию.",
		humanDurationMS(baseline.DurationMS),
		humanDurationMS(candidate.DurationMS),
	)}
}

func humanDurationMS(value uint64) string {
	if value < 1000 {
		return fmt.Sprintf("%d мс", value)
	}
	return fmt.Sprintf("%.1f с", float64(value)/1000)
}

func confidence(baseline, candidate Summary) string {
	sampleLevel := sampleConfidence(baseline, candidate)
	return lowerConfidenceLevel(
		lowerConfidenceLevel(
			lowerConfidenceLevel(sampleLevel, collectionConfidenceCap(baseline)),
			comparisonScopeConfidenceCap(baseline, candidate),
		),
		collectionConfidenceCap(candidate),
	)
}

func comparisonScopeConfidenceCap(baseline, candidate Summary) string {
	base := baseline.CollectionQuality
	next := candidate.CollectionQuality
	if base.ProcessScope == "" || next.ProcessScope == "" {
		return "high"
	}
	if base.ProcessScope != next.ProcessScope || base.AllowedProcessCount != next.AllowedProcessCount ||
		base.ProcessScopeFingerprint != next.ProcessScopeFingerprint ||
		base.ExpectedProcessCount != next.ExpectedProcessCount ||
		base.ExpectedProcessFingerprint != next.ExpectedProcessFingerprint {
		return "low"
	}
	return "high"
}

func sampleConfidence(baseline, candidate Summary) string {
	minLogs := baseline.LogCount
	if candidate.LogCount < minLogs {
		minLogs = candidate.LogCount
	}
	minEvents := baseline.EventCount
	if candidate.EventCount < minEvents {
		minEvents = candidate.EventCount
	}
	sampleLevel := "low"
	switch {
	case minLogs >= 5 && minEvents >= 500:
		sampleLevel = "high"
	case minLogs >= 2 && minEvents >= 80:
		sampleLevel = "medium"
	}
	return sampleLevel
}

func collectionConfidenceCap(summary Summary) string {
	if summary.CollectionQuality.Level == "" {
		return "high"
	}
	return summary.CollectionQuality.Level
}

func comparisonQualityWarnings(baseline, candidate Summary) []string {
	var warnings []string
	baseScope := baseline.CollectionQuality
	candidateScope := candidate.CollectionQuality
	if baseScope.ProcessScope != "" && candidateScope.ProcessScope != "" &&
		(baseScope.ProcessScope != candidateScope.ProcessScope ||
			baseScope.AllowedProcessCount != candidateScope.AllowedProcessCount ||
			baseScope.ProcessScopeFingerprint != candidateScope.ProcessScopeFingerprint ||
			baseScope.ExpectedProcessCount != candidateScope.ExpectedProcessCount ||
			baseScope.ExpectedProcessFingerprint != candidateScope.ExpectedProcessFingerprint) {
		warnings = append(warnings, fmt.Sprintf(
			"Process scope отличается: база %s (%d), кандидат %s (%d); сравнение ограничено низким доверием.",
			baseScope.ProcessScope,
			baseScope.AllowedProcessCount,
			candidateScope.ProcessScope,
			candidateScope.AllowedProcessCount,
		))
	}
	for _, item := range []struct {
		label   string
		summary Summary
	}{
		{label: "базы", summary: baseline},
		{label: "кандидата", summary: candidate},
	} {
		quality := item.summary.CollectionQuality
		if quality.Level == "" || quality.Level == "high" {
			continue
		}
		if len(quality.Reasons) == 0 {
			warnings = append(warnings, fmt.Sprintf("Качество данных %s ограничивает доверие уровнем %s.", item.label, quality.Level))
			continue
		}
		for _, reason := range quality.Reasons {
			warnings = append(warnings, fmt.Sprintf("Качество данных %s: %s.", item.label, reason))
		}
	}
	return uniqueStrings(warnings)
}

type ownerStatKey struct {
	owner string
	kind  string
}

func addOwner(stats map[ownerStatKey]*OwnerStats, owner, kind string, duration uint64, stack string) {
	if owner == "" {
		owner = "unknown"
	}
	key := ownerStatKey{owner: owner, kind: kind}
	item := stats[key]
	if item == nil {
		item = &OwnerStats{Owner: owner, Kind: kind}
		stats[key] = item
	}
	item.Count++
	item.TotalMS += duration
	if duration > item.MaxMS {
		item.MaxMS = duration
	}
	if item.StackHint == "" {
		item.StackHint = stack
	}
}

func fps(frames uint64, windowMS uint64) float64 {
	if frames == 0 || windowMS == 0 {
		return 0
	}
	return float64(frames) * 1000 / float64(windowMS)
}

const (
	minimumReliableFPSFrames = 30
	fpsIdleTolerance         = 6
)

// FPS is meaningful only while the UI is continuously producing enough frames. A partial window
// can contain one quick frame and then stay open while the screen is idle; dividing that frame by
// the whole wall-clock interval produces a false near-zero FPS. Frame tails remain available for
// every sample, while FPS uses only windows whose cadence is compatible with recorded durations.
func fpsWindowReliable(window *jhlog.UIWindowEvent) bool {
	if window == nil || window.FrameCount < minimumReliableFPSFrames || window.WindowMS == 0 {
		return false
	}
	typicalFrameMS := maxUint64(window.P95MS, maxUint64((window.FrameDeadlineUS+999)/1000, 16))
	averageIntervalMS := float64(window.WindowMS) / float64(window.FrameCount)
	return averageIntervalMS <= float64(typicalFrameMS*fpsIdleTolerance)
}

func fpsMeasurementStatus(frames uint64, measuredWindows int) string {
	if measuredWindows > 0 {
		return "measured"
	}
	if frames < minimumReliableFPSFrames {
		return "insufficient_frames"
	}
	return "sparse_rendering"
}

func sortRoutes(routes []RouteStats) {
	sort.Slice(routes, func(i, j int) bool {
		if routes[i].P95MS == routes[j].P95MS {
			return routes[i].Count > routes[j].Count
		}
		return routes[i].P95MS > routes[j].P95MS
	})
}

func sortNetworkCalls(calls []NetworkCallStats) {
	sort.Slice(calls, func(i, j int) bool {
		if calls[i].Count != calls[j].Count {
			return calls[i].Count > calls[j].Count
		}
		if calls[i].P95MS != calls[j].P95MS {
			return calls[i].P95MS > calls[j].P95MS
		}
		if calls[i].Route != calls[j].Route {
			return calls[i].Route < calls[j].Route
		}
		if calls[i].Initiator != calls[j].Initiator {
			return calls[i].Initiator < calls[j].Initiator
		}
		return calls[i].Owner < calls[j].Owner
	})
}

func sortScreens(screens []ScreenStats) {
	sort.Slice(screens, func(i, j int) bool {
		if screens[i].JankRatePct == screens[j].JankRatePct {
			return screens[i].FrameP95MS > screens[j].FrameP95MS
		}
		return screens[i].JankRatePct > screens[j].JankRatePct
	})
}

func sortProcessExits(exits []ProcessExitStats) {
	sort.Slice(exits, func(i, j int) bool {
		if exits[i].LatestTimestampUnixMS != exits[j].LatestTimestampUnixMS {
			return exits[i].LatestTimestampUnixMS > exits[j].LatestTimestampUnixMS
		}
		if exits[i].Reason != exits[j].Reason {
			return exits[i].Reason < exits[j].Reason
		}
		return exits[i].Process < exits[j].Process
	})
}

func sortIOOperations(operations []IOStats) {
	sort.Slice(operations, func(i, j int) bool {
		if operations[i].MainThread != operations[j].MainThread {
			return operations[i].MainThread
		}
		if operations[i].TotalDurationUS != operations[j].TotalDurationUS {
			return operations[i].TotalDurationUS > operations[j].TotalDurationUS
		}
		if operations[i].Operation != operations[j].Operation {
			return operations[i].Operation < operations[j].Operation
		}
		if operations[i].Source != operations[j].Source {
			return operations[i].Source < operations[j].Source
		}
		if operations[i].Owner != operations[j].Owner {
			return operations[i].Owner < operations[j].Owner
		}
		if operations[i].Screen != operations[j].Screen {
			return operations[i].Screen < operations[j].Screen
		}
		return operations[i].ContextOperation < operations[j].ContextOperation
	})
}

func sortOwners(owners []OwnerStats) {
	sort.Slice(owners, func(i, j int) bool {
		if owners[i].MaxMS == owners[j].MaxMS {
			return owners[i].TotalMS > owners[j].TotalMS
		}
		return owners[i].MaxMS > owners[j].MaxMS
	})
}

func sortSignalContexts(contexts []SignalContextStats) {
	sort.Slice(contexts, func(i, j int) bool {
		left := signalContextSeverityScore(contexts[i])
		right := signalContextSeverityScore(contexts[j])
		if left == right {
			return contexts[i].Operation < contexts[j].Operation
		}
		return left > right
	})
}

func signalContextSeverityScore(context SignalContextStats) uint64 {
	return context.ProblemCount*10_000 +
		uint64(context.StallCount)*5_000 +
		context.UIJank*100 +
		context.LogSpam*10 +
		uint64(context.HTTPFailed)*500 +
		context.HTTPP95MS +
		context.ProblemMaxMS
}

func sortLogSpam(items []LogSpamStats) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Count == items[j].Count {
			return items[i].Source < items[j].Source
		}
		return items[i].Count > items[j].Count
	})
}

func sortProblems(items []ProblemWindowStats) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].MaxMS == items[j].MaxMS {
			return items[i].Count > items[j].Count
		}
		return items[i].MaxMS > items[j].MaxMS
	})
}

func sortRuntimeCalls(items []RuntimeCallStats) {
	sort.Slice(items, func(i, j int) bool {
		left := items[i].TotalMS + items[i].MaxMS*10 + items[i].Count
		right := items[j].TotalMS + items[j].MaxMS*10 + items[j].Count
		if left == right {
			if items[i].Caller == items[j].Caller {
				return items[i].Callee < items[j].Callee
			}
			return items[i].Caller < items[j].Caller
		}
		return left > right
	})
}

func sortMemoryLeaks(items []MemoryLeakSuspect) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Score == items[j].Score {
			if items[i].MaxAgeMS == items[j].MaxAgeMS {
				return items[i].ClassName < items[j].ClassName
			}
			return items[i].MaxAgeMS > items[j].MaxAgeMS
		}
		return items[i].Score > items[j].Score
	})
}

func sortNamed(values []NamedValue) {
	sort.Slice(values, func(i, j int) bool {
		if values[i].Value == values[j].Value {
			return values[i].Name < values[j].Name
		}
		return values[i].Value > values[j].Value
	})
}

func namedValueTotal(values []NamedValue) uint64 {
	var total uint64
	for _, value := range values {
		total += value.Value
	}
	return total
}

func namedShareSummary(values []NamedValue) string {
	total := namedValueTotal(values)
	if total == 0 {
		return "нет данных"
	}
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, fmt.Sprintf("%s:%.1f%% (n=%d)", humanSummaryName(value.Name), float64(value.Value)*100/float64(total), value.Value))
	}
	if len(parts) == 0 {
		return "нет данных"
	}
	return strings.Join(parts, ",")
}

func namedDistributionDistance(baseline, candidate []NamedValue) float64 {
	baselineTotal := namedValueTotal(baseline)
	candidateTotal := namedValueTotal(candidate)
	if baselineTotal == 0 || candidateTotal == 0 {
		return 0
	}
	shares := map[string][2]float64{}
	for _, value := range baseline {
		pair := shares[value.Name]
		pair[0] += float64(value.Value) / float64(baselineTotal)
		shares[value.Name] = pair
	}
	for _, value := range candidate {
		pair := shares[value.Name]
		pair[1] += float64(value.Value) / float64(candidateTotal)
		shares[value.Name] = pair
	}
	distance := 0.0
	for _, pair := range shares {
		distance += math.Abs(pair[0] - pair[1])
	}
	return distance / 2
}

func humanSummaryName(value string) string {
	value = datavalue.HumanUnknown(value, "неизвестно")
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return "неизвестно"
	}
	for i, field := range fields {
		key, raw, ok := strings.Cut(field, "=")
		if !ok {
			fields[i] = datavalue.HumanUnknown(field, "неизвестно")
			continue
		}
		fields[i] = key + "=" + datavalue.HumanUnknown(raw, "неизвестно")
	}
	return strings.Join(fields, " ")
}

func retainedAgeBucket(ageMs uint64) string {
	switch {
	case ageMs < 10_000:
		return "<10s"
	case ageMs < 30_000:
		return "10s-30s"
	case ageMs < 60_000:
		return "30s-60s"
	default:
		return ">=60s"
	}
}

func delta(name string, before, after uint64, unit string, higherIsWorse bool, sampleSize uint64) Delta {
	change := "0"
	severity := "ok"
	changePct := 0.0
	changeAbs := signedUint64DeltaFloat(before, after)
	regressionAbs := 0.0
	regressionPct := 0.0
	if before == 0 && after > 0 {
		change = "+new"
		if higherIsWorse {
			severity = "medium"
			regressionAbs = float64(after)
			regressionPct = 100
		}
	} else if before > 0 {
		diff := signedUint64DeltaFloat(before, after)
		changePct = diff * 100 / float64(before)
		change = fmt.Sprintf("%+.1f%%", changePct)
		if higherIsWorse {
			if changePct > 0 {
				regressionAbs = diff
				regressionPct = changePct
			}
			if changePct >= 25 {
				severity = "high"
			} else if changePct >= 10 {
				severity = "medium"
			}
		} else {
			if changePct < 0 {
				regressionAbs = math.Abs(diff)
				regressionPct = math.Abs(changePct)
			}
			if changePct <= -25 {
				severity = "high"
			} else if changePct <= -10 {
				severity = "medium"
			}
		}
	}
	return Delta{
		Name:           name,
		Baseline:       fmt.Sprintf("%d %s", before, unit),
		Candidate:      fmt.Sprintf("%d %s", after, unit),
		Change:         change,
		Severity:       severity,
		Interval:       sampleNote(sampleSize),
		Comparable:     true,
		Unit:           unit,
		BaselineValue:  float64(before),
		CandidateValue: float64(after),
		ChangeAbs:      changeAbs,
		ChangePct:      changePct,
		RegressionAbs:  regressionAbs,
		RegressionPct:  regressionPct,
		SampleSize:     sampleSize,
	}
}

func signedUint64DeltaFloat(before, after uint64) float64 {
	if after >= before {
		return float64(after - before)
	}
	return -float64(before - after)
}

func deltaFloat(name string, before, after float64, unit string, higherIsWorse bool, sampleSize uint64) Delta {
	diff := after - before
	severity := "ok"
	regressionAbs := 0.0
	regressionPct := 0.0
	changePct := 0.0
	if before != 0 {
		changePct = diff * 100 / before
	}
	if higherIsWorse {
		if diff > 0 {
			regressionAbs = diff
			if before == 0 {
				regressionPct = 100
			} else {
				regressionPct = math.Abs(changePct)
			}
		}
		if diff >= 3.0 {
			severity = "high"
		} else if diff >= 1.0 {
			severity = "medium"
		}
	} else {
		if diff < 0 {
			regressionAbs = math.Abs(diff)
			if before == 0 {
				regressionPct = 100
			} else {
				regressionPct = math.Abs(changePct)
			}
		}
		if diff <= -5.0 {
			severity = "high"
		} else if diff <= -2.0 {
			severity = "medium"
		}
	}
	return Delta{
		Name:           name,
		Baseline:       fmt.Sprintf("%.2f %s", before, unit),
		Candidate:      fmt.Sprintf("%.2f %s", after, unit),
		Change:         fmt.Sprintf("%+.2f %s", diff, unit),
		Severity:       severity,
		Interval:       sampleNote(sampleSize),
		Comparable:     true,
		Unit:           unit,
		BaselineValue:  before,
		CandidateValue: after,
		ChangeAbs:      diff,
		ChangePct:      changePct,
		RegressionAbs:  regressionAbs,
		RegressionPct:  regressionPct,
		SampleSize:     sampleSize,
	}
}

func adjustedSeverity(effectSeverity, confidence string, sampleSize uint64) string {
	if effectSeverity == "ok" {
		return "ok"
	}
	if confidence == "low" || sampleSize < 3 {
		if effectSeverity == "high" {
			return "medium"
		}
	}
	return effectSeverity
}

func sampleNote(sampleSize uint64) string {
	return fmt.Sprintf("выборка=%d", sampleSize)
}

func minUint64(a, b uint64) uint64 {
	if a < b {
		return a
	}
	return b
}

func formatMB(kb uint64) string {
	return fmt.Sprintf("%.1f МБ", float64(kb)/1024)
}

func formatDataSize(kb uint64) string {
	if kb == 0 {
		return "неизвестно"
	}
	if kb >= 1024*1024 {
		return fmt.Sprintf("%.1f ГБ", float64(kb)/(1024*1024))
	}
	return fmt.Sprintf("%.1f МБ", float64(kb)/1024)
}

func unknownIfEmpty(value string) string {
	if value == "" {
		return "unknown"
	}
	return value
}

func osValue(release string, sdk string) string {
	release = unknownIfEmpty(release)
	sdk = unknownIfEmpty(sdk)
	switch {
	case release == "unknown" && sdk == "unknown":
		return "Android неизвестен"
	case release == "unknown":
		return fmt.Sprintf("Android API %s", apiNumber(sdk))
	case sdk == "unknown":
		return fmt.Sprintf("Android %s", release)
	default:
		return fmt.Sprintf("Android %s", release)
	}
}

func appBuildValue(app string, build string) string {
	if app == "unknown" && build == "unknown" {
		return "версия приложения неизвестна"
	}
	if build == "unknown" {
		return app
	}
	if app == "unknown" {
		return fmt.Sprintf("версия неизвестна (%s)", build)
	}
	return fmt.Sprintf("%s (%s)", app, build)
}

func batteryValue(pct uint64) string {
	if pct == 0 {
		return "неизвестно"
	}
	return fmt.Sprintf("%d%%", pct)
}

func batteryDetail(summary Summary) string {
	parts := []string{batteryStateName(summary.BatteryStateLast)}
	if summary.BatteryTempDeciC != 0 {
		parts = append(parts, fmt.Sprintf("%.1f °C", float64(summary.BatteryTempDeciC)/10))
	}
	if summary.BatteryMinPct > 0 {
		parts = append(parts, fmt.Sprintf("мин. %d%%", summary.BatteryMinPct))
	}
	return strings.Join(parts, " · ")
}

func batteryStateName(state uint64) string {
	switch state {
	case 2:
		return "заряжается"
	case 3:
		return "разряжается"
	case 4:
		return "не заряжается"
	case 5:
		return "полный заряд"
	default:
		return "неизвестно"
	}
}

func networkDetail(summary Summary) string {
	return fmt.Sprintf(
		"валидирована %s · лимитная %s · VPN %s",
		yesNoRU(summary.NetworkValidated),
		yesNoRU(summary.NetworkMetered),
		yesNoRU(summary.NetworkVPN),
	)
}

func memoryDetail(summary Summary) string {
	parts := []string{}
	if summary.TotalMemoryKB > 0 {
		parts = append(parts, fmt.Sprintf("всего %s", formatDataSize(summary.TotalMemoryKB)))
	}
	if summary.AvailMemoryMinKB > 0 {
		parts = append(parts, fmt.Sprintf("мин. свободно %s", formatDataSize(summary.AvailMemoryMinKB)))
	}
	if summary.LowMemoryCount > 0 {
		parts = append(parts, fmt.Sprintf("сигналы low-memory %d", summary.LowMemoryCount))
	}
	if len(parts) == 0 {
		return "нет контекста памяти"
	}
	return strings.Join(parts, " · ")
}

func storageDetail(summary Summary) string {
	if summary.TotalStorageKB == 0 {
		return "раздел данных приложения"
	}
	return fmt.Sprintf("из %s раздел данных приложения", formatDataSize(summary.TotalStorageKB))
}

func androidDetail(sdk string, patch string) string {
	patch = unknownIfEmpty(patch)
	sdk = unknownIfEmpty(sdk)
	if patch == "unknown" {
		return fmt.Sprintf("API %s · патч безопасности неизвестен", apiNumber(sdk))
	}
	return fmt.Sprintf("API %s · патч безопасности %s", apiNumber(sdk), patch)
}

func apiNumber(sdk string) string {
	if sdk == "unknown" {
		return "неизвестен"
	}
	return strings.TrimPrefix(sdk, "api-")
}

func yesNoRU(value bool) string {
	if value {
		return "да"
	}
	return "нет"
}

func rootCohortValue(known bool, rooted bool) string {
	if !known {
		return "unknown"
	}
	if rooted {
		return "yes"
	}
	return "no"
}

func rootValue(known bool, rooted bool) string {
	if !known {
		return "неизвестно"
	}
	if rooted {
		return "да"
	}
	return "нет"
}

func rootDetail(known bool, rooted bool) string {
	if !known {
		return "нет сигнала о рут-доступе в метаданных сессии"
	}
	if rooted {
		return "обнаружены признаки рут-доступа"
	}
	return "признаки рут-доступа не найдены"
}
