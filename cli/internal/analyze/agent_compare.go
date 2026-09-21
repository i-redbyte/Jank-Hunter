package analyze

import (
	"fmt"
	"sort"
	"strings"
)

func agentSignalSamples(summary AgentSummary, capability uint64) uint64 {
	if summary.EventCount == 0 || summary.Capabilities.Active&capability == 0 {
		return 0
	}
	return summary.EventCount
}

func compareAgent(baseline, candidate AgentSummary) AgentComparison {
	result := AgentComparison{}
	if baseline.EventCount == 0 || candidate.EventCount == 0 {
		result.Warnings = append(result.Warnings, "Данные JVM TI отсутствуют хотя бы в одном прогоне; показатели среды выполнения нельзя полностью сопоставить.")
		return result
	}
	result.ConfigMismatch = baseline.ConfigHash != candidate.ConfigHash || baseline.EffectivePreset != candidate.EffectivePreset
	result.CapabilityMismatch = baseline.Capabilities.Active != candidate.Capabilities.Active
	if result.ConfigMismatch {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Настройки JVM TI различаются: %s/%s → %s/%s.", baseline.EffectivePreset, baseline.ConfigHash, candidate.EffectivePreset, candidate.ConfigHash))
	}
	if result.CapabilityMismatch {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Активные возможности JVM TI различаются: 0x%x → 0x%x.", baseline.Capabilities.Active, candidate.Capabilities.Active))
	}
	if len(baseline.DataGaps) > 0 || len(candidate.DataGaps) > 0 {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Качество данных JVM TI различается: ограничений в базе %d, у кандидата %d; выводы требуют ручной проверки.", len(baseline.DataGaps), len(candidate.DataGaps)))
	}
	baseStacks := agentMethodSuspects(baseline.Stacks)
	candidateStacks := agentMethodSuspects(candidate.Stacks)
	result.NewStackSuspects, result.ResolvedSuspects = setDifference(candidateStacks, baseStacks), setDifference(baseStacks, candidateStacks)
	basePaths := agentFindingKeys(baseline.Findings)
	candidatePaths := agentFindingKeys(candidate.Findings)
	for _, key := range setDifference(candidatePaths, basePaths) {
		result.CausalChanges = append(result.CausalChanges, "появилась связь: "+key)
	}
	for _, key := range setDifference(basePaths, candidatePaths) {
		result.CausalChanges = append(result.CausalChanges, "исчезла связь: "+key)
	}
	return result
}

func agentMethodSuspects(summary AgentStackSummary) []string {
	set := map[string]struct{}{}
	for _, hotspot := range summary.Hotspots {
		for _, method := range hotspot.Methods {
			set[method] = struct{}{}
		}
	}
	return sortedStringSet(set)
}

func agentFindingKeys(findings []AgentFinding) []string {
	set := map[string]struct{}{}
	for _, finding := range findings {
		set[strings.Join([]string{finding.SuspectedCause, finding.Screen, finding.Flow, finding.Owner}, " | ")] = struct{}{}
	}
	return sortedStringSet(set)
}

func sortedStringSet(set map[string]struct{}) []string {
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func setDifference(left, right []string) []string {
	seen := map[string]struct{}{}
	for _, value := range right {
		seen[value] = struct{}{}
	}
	result := make([]string, 0)
	for _, value := range left {
		if _, ok := seen[value]; !ok {
			result = append(result, value)
		}
	}
	return result
}
