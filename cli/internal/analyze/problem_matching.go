package analyze

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"sort"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/datavalue"
)

const problemMatchCandidatesLimit = 32

type ProblemObservation string

const (
	ObservationBoth                   ProblemObservation = "both"
	ObservationNotObservedAfter       ProblemObservation = "not_observed_after"
	ObservationObservedOnlyAfter      ProblemObservation = "observed_only_after"
	ObservationTargetNotExercised     ProblemObservation = "target_not_exercised"
	ObservationMeasurementUnavailable ProblemObservation = "measurement_unavailable"
	ObservationAmbiguousMatch         ProblemObservation = "ambiguous_match"
)

type problemMatchGroup struct {
	key       string
	baseline  []ProblemFinding
	candidate []ProblemFinding
	ambiguous bool
}

func problemGroupFingerprint(group problemMatchGroup, before, after *ProblemFinding) string {
	if strings.HasPrefix(group.key, "exact\x00") {
		return strings.TrimPrefix(group.key, "exact\x00")
	}
	if strings.HasPrefix(group.key, "alias\x00") && after != nil {
		return after.Fingerprint
	}
	if group.key != "" {
		return group.key
	}
	if after != nil {
		return after.Fingerprint
	}
	if before != nil {
		return before.Fingerprint
	}
	return ""
}

func semanticProblemGroups(baseline, candidate []ProblemFinding) []problemMatchGroup {
	return semanticProblemGroupsWithAliases(baseline, candidate, nil)
}

func semanticProblemGroupsWithAliases(baseline, candidate []ProblemFinding, aliases *ProblemAliases) []problemMatchGroup {
	groups := make(map[string]*problemMatchGroup, len(baseline)+len(candidate))
	ambiguous := make([]problemMatchGroup, 0)
	baselineExact := make(map[string]int, len(baseline))
	candidateExact := make(map[string]int, len(candidate))
	for i := range baseline {
		if baseline[i].Fingerprint != "" {
			baselineExact[baseline[i].Fingerprint] = i
		}
	}
	for i := range candidate {
		if candidate[i].Fingerprint != "" {
			candidateExact[candidate[i].Fingerprint] = i
		}
	}
	baselineUsed := make([]bool, len(baseline))
	candidateUsed := make([]bool, len(candidate))
	if aliases != nil {
		baselineByFingerprint := make(map[string]int, len(baseline))
		candidateByFingerprint := make(map[string]int, len(candidate))
		for i := range baseline {
			if baseline[i].Fingerprint != "" {
				if _, exists := baselineByFingerprint[baseline[i].Fingerprint]; exists {
					baselineByFingerprint[baseline[i].Fingerprint] = -1
				} else {
					baselineByFingerprint[baseline[i].Fingerprint] = i
				}
			}
		}
		for i := range candidate {
			if candidate[i].Fingerprint != "" {
				if _, exists := candidateByFingerprint[candidate[i].Fingerprint]; exists {
					candidateByFingerprint[candidate[i].Fingerprint] = -1
				} else {
					candidateByFingerprint[candidate[i].Fingerprint] = i
				}
			}
		}
		for _, alias := range aliases.Entries {
			beforeIndex, beforeExists := baselineByFingerprint[alias.Baseline]
			afterIndex, afterExists := candidateByFingerprint[alias.Candidate]
			if !beforeExists || !afterExists || beforeIndex < 0 || afterIndex < 0 {
				continue
			}
			key := "alias\x00" + alias.Baseline + "\x00" + alias.Candidate
			groups[key] = &problemMatchGroup{key: key, baseline: []ProblemFinding{baseline[beforeIndex]}, candidate: []ProblemFinding{candidate[afterIndex]}}
			baselineUsed[beforeIndex], candidateUsed[afterIndex] = true, true
		}
	}
	for fingerprint, beforeIndex := range baselineExact {
		if baselineUsed[beforeIndex] {
			continue
		}
		afterIndex, exists := candidateExact[fingerprint]
		if !exists || candidateUsed[afterIndex] {
			continue
		}
		key := "exact\x00" + fingerprint
		groups[key] = &problemMatchGroup{key: key, baseline: []ProblemFinding{baseline[beforeIndex]}, candidate: []ProblemFinding{candidate[afterIndex]}}
		baselineUsed[beforeIndex], candidateUsed[afterIndex] = true, true
	}
	add := func(finding ProblemFinding, before bool) {
		key, valid := semanticProblemKey(finding)
		if !valid {
			group := problemMatchGroup{ambiguous: true}
			if before {
				group.baseline = []ProblemFinding{finding}
			} else {
				group.candidate = []ProblemFinding{finding}
			}
			ambiguous = append(ambiguous, group)
			return
		}
		group := groups[key]
		if group == nil {
			group = &problemMatchGroup{key: key}
			groups[key] = group
		}
		if before {
			group.baseline = append(group.baseline, finding)
		} else {
			group.candidate = append(group.candidate, finding)
		}
		if len(group.baseline) > problemMatchCandidatesLimit || len(group.candidate) > problemMatchCandidatesLimit {
			group.ambiguous = true
		}
	}
	for i, finding := range baseline {
		if !baselineUsed[i] {
			add(finding, true)
		}
	}
	for i, finding := range candidate {
		if !candidateUsed[i] {
			add(finding, false)
		}
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]problemMatchGroup, 0, len(keys)+len(ambiguous))
	for _, key := range keys {
		result = append(result, *groups[key])
	}
	result = append(result, ambiguous...)
	return result
}

func semanticProblemKey(finding ProblemFinding) (string, bool) {
	family := problemDetectorFamily(finding)
	var targets [4]map[string]struct{}
	for _, location := range finding.Where {
		key := semanticProblemLocation(finding, location)
		if key == "" {
			continue
		}
		rank := semanticProblemLocationRank(key)
		if targets[rank] == nil {
			targets[rank] = make(map[string]struct{}, 1)
		}
		targets[rank][key] = struct{}{}
	}
	target := ""
	for rank := 0; rank < len(targets); rank++ {
		if len(targets[rank]) == 0 {
			continue
		}
		if len(targets[rank]) != 1 {
			return "", false
		}
		for key := range targets[rank] {
			target = key
		}
		break
	}
	if target == "" {
		if len(finding.Where) > 0 {
			return "", false
		}
		target = "global\x00" + strings.ToLower(finding.Subcategory)
	}
	digest := sha256.Sum256([]byte(family + "\x00" + target))
	return hex.EncodeToString(digest[:]), true
}

func semanticProblemLocationRank(key string) int {
	switch {
	case strings.HasPrefix(key, "operation\x00"):
		return 0
	case strings.HasPrefix(key, "route\x00"):
		return 1
	case strings.HasPrefix(key, "screen\x00"):
		return 2
	default:
		return 3
	}
}

func problemDetectorFamily(finding ProblemFinding) string {
	if isUIIncidentSignal(finding) && len(finding.RelatedFindings) > 1 {
		return "ui-incident"
	}
	if finding.DetectorID == "stability.historical_process_exit" || strings.HasPrefix(finding.DetectorID, "io.") {
		return finding.DetectorID + "\x00" + finding.Subcategory
	}
	return finding.DetectorID
}

func semanticProblemLocation(finding ProblemFinding, location ProblemLocation) string {
	process := semanticProblemText(location.Process)
	screen := semanticProblemText(location.Screen)
	operation := semanticProblemText(location.Operation)
	route := semanticProblemText(location.Route)
	if operation != "" {
		if finding.Category != ProblemCategoryNetwork {
			route = ""
		}
		return strings.Join([]string{"operation", process, screen, operation, route}, "\x00")
	}
	if route != "" {
		return strings.Join([]string{"route", process, screen, route}, "\x00")
	}
	if screen != "" && (finding.Category == ProblemCategoryUI || isUIIncidentSignal(finding)) {
		return strings.Join([]string{"screen", process, screen}, "\x00")
	}
	className, method, owner := semanticProblemText(location.Class), semanticProblemText(location.Method), semanticProblemText(location.Owner)
	if className != "" || method != "" || owner != "" {
		return strings.Join([]string{"code", process, className, method, owner}, "\x00")
	}
	return ""
}

func semanticProblemText(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" || datavalue.IsUnknown(value) {
		return ""
	}
	return value
}

func problemGroupPrimary(values []ProblemFinding) *ProblemFinding {
	if len(values) == 0 {
		return nil
	}
	index := 0
	for i := 1; i < len(values); i++ {
		if preferProblemFinding(values[i], values[index]) {
			index = i
		}
	}
	copy := values[index]
	return &copy
}

func problemGroupDetectorCompatible(before, after []ProblemFinding) bool {
	version := ""
	for _, values := range [][]ProblemFinding{before, after} {
		for _, finding := range values {
			if finding.DetectorVersion == "" {
				return false
			}
			if version == "" {
				version = finding.DetectorVersion
			} else if version != finding.DetectorVersion {
				return false
			}
		}
	}
	return version != ""
}

func problemCategoryMeasured(summary Summary, category string) bool {
	for _, coverage := range summary.CategoryCoverage {
		if coverage.Category == category {
			return coverage.Status == "healthy" || coverage.Status == "problems_found"
		}
	}
	return false
}

func problemTargetExercised(finding ProblemFinding, baseline, candidate Summary, scope ComparisonScope) bool {
	if scope.Comparability != ScenarioFull && scope.Comparability != ScenarioPartial {
		return false
	}
	if len(finding.Where) == 0 {
		return scope.Comparability == ScenarioFull
	}
	for _, location := range finding.Where {
		if matchedLocationOnBothSides(location, baseline.OperationAnalysis, candidate.OperationAnalysis, scope.matches) {
			return true
		}
	}
	return false
}

func matchedLocationOnBothSides(location ProblemLocation, baseline, candidate *OperationAnalysis, matches []scenarioMatch) bool {
	if baseline == nil || candidate == nil {
		return false
	}
	for _, match := range matches {
		before := profileIndicesMatchLocation(baseline.Profiles, match.Baseline, location)
		after := profileIndicesMatchLocation(candidate.Profiles, match.Candidate, location)
		if before && after {
			return true
		}
	}
	return false
}

func profileIndicesMatchLocation(profiles []OperationProfile, indices []int, location ProblemLocation) bool {
	if semanticProblemText(location.Operation) == "" && semanticProblemText(location.Screen) == "" {
		return false
	}
	for _, index := range indices {
		if index < 0 || index >= len(profiles) {
			continue
		}
		stats := profiles[index].Stats
		operationMatches := semanticProblemText(location.Operation) == "" || strings.EqualFold(location.Operation, stats.Operation)
		screenMatches := semanticProblemText(location.Screen) == "" || strings.EqualFold(location.Screen, stats.Screen)
		if operationMatches && screenMatches {
			return true
		}
	}
	return false
}

func problemEffect(before, after *ProblemFinding) comparisonEffect {
	if before == nil || after == nil {
		return comparisonEffect{Change: ChangeInsufficientData}
	}
	left, right, band, ok := comparableProblemMeasurement(*before, *after)
	if !ok {
		return comparisonEffect{Change: ChangeInsufficientData}
	}
	return compareMeasurement(left, right, band, true, true)
}

// With zero candidate events, three divided by exposure is a conservative
// approximation of the one-sided 95% Poisson upper rate bound (-ln(0.05)/N).
// Improvement is descriptive and still carries the run-level confidence.
func problemAbsencePassesUpperBound(finding ProblemFinding, baseline, candidate Summary, scope ComparisonScope) bool {
	if finding.Frequency == nil || finding.Frequency.Count == 0 || finding.Frequency.Count > maxExactMetricInteger {
		return false
	}
	for _, location := range finding.Where {
		beforeExposure, afterExposure, ok := matchedLocationExposure(location, baseline.OperationAnalysis, candidate.OperationAnalysis, scope.matches)
		if !ok || beforeExposure == 0 || afterExposure == 0 || beforeExposure > maxExactMetricInteger || afterExposure > maxExactMetricInteger {
			continue
		}
		candidateUpperRate, upperKnown := zeroEventUpperRate95(afterExposure)
		if !upperKnown {
			continue
		}
		baselineRate := float64(finding.Frequency.Count) / float64(beforeExposure)
		if candidateUpperRate < baselineRate {
			return true
		}
	}
	return false
}

func matchedLocationExposure(location ProblemLocation, baseline, candidate *OperationAnalysis, matches []scenarioMatch) (uint64, uint64, bool) {
	if baseline == nil || candidate == nil {
		return 0, 0, false
	}
	var before, after uint64
	for _, match := range matches {
		if !match.Exact {
			continue
		}
		left, leftOK := profileLocationExposure(baseline.Profiles, match.Baseline, location)
		right, rightOK := profileLocationExposure(candidate.Profiles, match.Candidate, location)
		if !leftOK || !rightOK || math.MaxUint64-before < left || math.MaxUint64-after < right {
			return 0, 0, false
		}
		before, after = before+left, after+right
	}
	return before, after, before > 0 && after > 0
}

func profileLocationExposure(profiles []OperationProfile, indices []int, location ProblemLocation) (uint64, bool) {
	var exposure uint64
	for _, index := range indices {
		if index < 0 || index >= len(profiles) {
			return 0, false
		}
		stats := profiles[index].Stats
		operationMatches := semanticProblemText(location.Operation) == "" || strings.EqualFold(location.Operation, stats.Operation)
		screenMatches := semanticProblemText(location.Screen) == "" || strings.EqualFold(location.Screen, stats.Screen)
		if !operationMatches || !screenMatches {
			continue
		}
		if math.MaxUint64-exposure < stats.Count {
			return 0, false
		}
		exposure += stats.Count
	}
	return exposure, true
}

func comparableProblemMeasurement(before, after ProblemFinding) (comparisonMeasurement, comparisonMeasurement, comparisonBand, bool) {
	if before.Frequency != nil && after.Frequency != nil && before.Frequency.RatePerSec != nil && after.Frequency.RatePerSec != nil {
		return comparisonMeasurement{Value: *before.Frequency.RatePerSec, Samples: max(1, before.Frequency.Count), State: measurementObserved}, comparisonMeasurement{Value: *after.Frequency.RatePerSec, Samples: max(1, after.Frequency.Count), State: measurementObserved}, comparisonBand{Absolute: .01, Relative: .05}, true
	}
	for _, left := range before.Evidence {
		if left.Numerator == nil || left.Denominator == nil || *left.Denominator == 0 {
			continue
		}
		for _, right := range after.Evidence {
			if left.Name != right.Name || right.Numerator == nil || right.Denominator == nil || *right.Denominator == 0 {
				continue
			}
			return comparisonMeasurement{Value: float64(*left.Numerator) * 100 / float64(*left.Denominator), Samples: *left.Denominator, State: measurementObserved}, comparisonMeasurement{Value: float64(*right.Numerator) * 100 / float64(*right.Denominator), Samples: *right.Denominator, State: measurementObserved}, comparisonBand{Absolute: 1}, true
		}
	}
	return comparisonMeasurement{}, comparisonMeasurement{}, comparisonBand{}, false
}
