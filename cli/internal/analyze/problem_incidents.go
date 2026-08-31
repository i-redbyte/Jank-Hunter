package analyze

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/datavalue"
)

// buildProblemIncidents keeps detector findings lossless while collapsing signals that belong to
// one UI investigation. The detailed and exported findings remain available separately.
func buildProblemIncidents(findings []ProblemFinding) []ProblemFinding {
	if len(findings) == 0 {
		return nil
	}
	groups := make(map[string][]ProblemFinding, len(findings))
	order := make([]string, 0, len(findings))
	for _, finding := range findings {
		key := problemIncidentKey(finding)
		if _, exists := groups[key]; !exists {
			order = append(order, key)
		}
		groups[key] = append(groups[key], finding)
	}
	incidents := make([]ProblemFinding, 0, len(groups))
	for _, key := range order {
		incidents = append(incidents, mergeProblemIncident(key, groups[key]))
	}
	sort.Slice(incidents, func(i, j int) bool {
		if problemSeverityRank(incidents[i].Severity) != problemSeverityRank(incidents[j].Severity) {
			return problemSeverityRank(incidents[i].Severity) > problemSeverityRank(incidents[j].Severity)
		}
		if incidents[i].InvestigationPriority != incidents[j].InvestigationPriority {
			return incidents[i].InvestigationPriority > incidents[j].InvestigationPriority
		}
		if problemConfidenceRank(incidents[i].Confidence) != problemConfidenceRank(incidents[j].Confidence) {
			return problemConfidenceRank(incidents[i].Confidence) > problemConfidenceRank(incidents[j].Confidence)
		}
		return incidents[i].Fingerprint < incidents[j].Fingerprint
	})
	return incidents
}

func problemIncidentKey(finding ProblemFinding) string {
	if isUIIncidentSignal(finding) {
		if screen := incidentScreen(finding.Where); screen != "" {
			return "ui-screen\x00" + strings.ToLower(screen)
		}
	}
	return "finding\x00" + finding.Fingerprint
}

func isUIIncidentSignal(finding ProblemFinding) bool {
	return finding.Category == ProblemCategoryUI ||
		finding.DetectorID == "stability.main_thread_stall" ||
		finding.DetectorID == "io.main_thread" ||
		finding.DetectorID == "io.room_main_thread" ||
		(finding.DetectorID == "io.database_calls" &&
			finding.Subcategory == "database_main_thread_ui_linked")
}

func incidentScreen(locations []ProblemLocation) string {
	for _, location := range locations {
		screen := strings.TrimSpace(location.Screen)
		if screen != "" && !datavalue.IsUnknown(screen) {
			return screen
		}
	}
	return ""
}

func mergeProblemIncident(key string, findings []ProblemFinding) ProblemFinding {
	if len(findings) == 1 {
		result := findings[0]
		result.RelatedFindings = []string{result.ID}
		result.RelatedCategories = uniqueStrings(append(result.RelatedCategories, result.Category))
		sort.Strings(result.RelatedCategories)
		if strings.HasPrefix(key, "ui-screen\x00") {
			result.Fingerprint = incidentFingerprint(key)
			result.ID = "incident-" + result.Fingerprint[:16]
		}
		return result
	}
	primary := findings[0]
	highestPriority := findings[0]
	for _, finding := range findings[1:] {
		if preferIncidentPrimary(finding, primary) {
			primary = finding
		}
		if preferProblemFinding(finding, highestPriority) {
			highestPriority = finding
		}
	}
	result := primary
	result.InvestigationPriority = highestPriority.InvestigationPriority
	result.Severity = highestPriority.Severity
	result.PriorityBreakdown = append([]ProblemPriorityComponent(nil), highestPriority.PriorityBreakdown...)
	result.Confidence = bestIncidentConfidence(findings)
	result.ConfidenceReasons = nil
	result.Where = nil
	result.Evidence = nil
	result.Impact = nil
	result.Recommendations = append([]ProblemRecommendation(nil), primary.Recommendations...)
	result.Limitations = nil
	result.RelatedFindings = nil
	result.RelatedCategories = nil

	ids := make([]string, 0, len(findings))
	factors := make([]string, 0, len(findings))
	for _, finding := range findings {
		ids = append(ids, finding.ID)
		result.RelatedCategories = append(result.RelatedCategories, finding.Category)
		result.RelatedCategories = append(result.RelatedCategories, finding.RelatedCategories...)
		if finding.Confidence == result.Confidence {
			result.ConfidenceReasons = append(result.ConfidenceReasons, finding.ConfidenceReasons...)
		}
		result.Impact = append(result.Impact, finding.Impact...)
		result.Limitations = append(result.Limitations, finding.Limitations...)
		for _, location := range finding.Where {
			result.Where = appendUniqueLocation(result.Where, location)
		}
		for _, evidence := range finding.Evidence {
			result.Evidence = appendUniqueProblemEvidence(result.Evidence, evidence)
		}
		if finding.ID != primary.ID {
			result.Recommendations = appendUniqueProblemRecommendations(result.Recommendations, finding.Recommendations...)
		}
		factors = append(factors, fmt.Sprintf("%s: %s", finding.Title, finding.WhatHappened))
	}
	sort.Strings(ids)
	result.RelatedFindings = ids
	result.RelatedCategories = uniqueStrings(result.RelatedCategories)
	sort.Strings(result.RelatedCategories)
	result.ConfidenceReasons = uniqueStrings(result.ConfidenceReasons)
	result.Impact = uniqueStrings(result.Impact)
	result.Limitations = uniqueStrings(append(
		result.Limitations,
		"Сигналы объединены по экрану; это не доказывает совпадение по времени или причинную связь между отдельными сигналами.",
	))
	sort.SliceStable(result.Where, func(i, j int) bool {
		return locationHasApplicationSymbol(result.Where[i]) && !locationHasApplicationSymbol(result.Where[j])
	})
	result.Fingerprint = incidentFingerprint(key)
	result.ID = "incident-" + result.Fingerprint[:16]
	screen := incidentScreen(result.Where)
	result.Title = incidentTitle(screen, findings)
	result.WhatHappened = fmt.Sprintf(
		"На экране %s объединено %s. Группировка сохраняет все измерения и места в коде, но сама по себе не означает, что события совпали по времени.",
		displayUnknown(screen, "без атрибуции"),
		russianCountUint64(uint64(len(findings)), "связанный сигнал", "связанных сигнала", "связанных сигналов"),
	)
	result.Why = ProblemWhy{
		ClaimLevel: "correlated",
		Summary:    incidentWhySummary(findings),
		Factors:    factors,
	}
	return result
}

func incidentWhySummary(findings []ProblemFinding) string {
	for _, finding := range findings {
		if finding.DetectorID == "stability.main_thread_stall" {
			return "Остановки главного потока подтверждены прямыми измерениями, а снимки стека показывают, где поток находился во время каждой паузы. Остальные сигналы относятся к тому же экрану, но это не доказывает совпадение по времени и не устанавливает единую первопричину подтормаживаний."
		}
	}
	return "Сигналы измерены на одном экране и собраны в общий маршрут расследования. Без общего интервала или идентификатора события это не доказывает совпадение по времени и не устанавливает единую первопричину."
}

func preferIncidentPrimary(candidate, current ProblemFinding) bool {
	candidateApp := findingHasApplicationLocation(candidate)
	currentApp := findingHasApplicationLocation(current)
	if candidateApp != currentApp {
		return candidateApp
	}
	if problemClaimRank(candidate.Why.ClaimLevel) != problemClaimRank(current.Why.ClaimLevel) {
		return problemClaimRank(candidate.Why.ClaimLevel) > problemClaimRank(current.Why.ClaimLevel)
	}
	return preferProblemFinding(candidate, current)
}

func problemClaimRank(value string) int {
	switch value {
	case "hypothesis":
		return 1
	case "correlated":
		return 2
	case "linked":
		return 3
	default:
		return 0
	}
}

func findingHasApplicationLocation(finding ProblemFinding) bool {
	for _, location := range finding.Where {
		for _, value := range []string{location.Class, location.Method, location.Owner} {
			if isApplicationSymbol(value) {
				return true
			}
		}
	}
	return false
}

func locationHasApplicationSymbol(location ProblemLocation) bool {
	return isApplicationSymbol(location.Class) || isApplicationSymbol(location.Method) || isApplicationSymbol(location.Owner)
}

func isApplicationSymbol(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || datavalue.IsUnknown(value) {
		return false
	}
	lower := strings.ToLower(value)
	frameworkPrefixes := []string{
		"android.", "androidx.", "java.", "javax.", "kotlin.", "kotlinx.", "dalvik.",
		"libcore.", "sun.", "com.android.", "com.google.android.", "com.google.common.",
		"com.google.firebase.", "leakcanary.", "handler (android.",
	}
	for _, prefix := range frameworkPrefixes {
		if strings.HasPrefix(lower, prefix) {
			return false
		}
	}
	return strings.Contains(value, ".")
}

func bestIncidentConfidence(findings []ProblemFinding) string {
	best := "low"
	for _, finding := range findings {
		if problemConfidenceRank(finding.Confidence) > problemConfidenceRank(best) {
			best = finding.Confidence
		}
	}
	return best
}

func incidentFingerprint(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

func problemIncidentsOrFindings(summary Summary) []ProblemFinding {
	if len(summary.ProblemIncidents) > 0 {
		return summary.ProblemIncidents
	}
	return summary.Problems
}

func incidentTitle(screen string, findings []ProblemFinding) string {
	hasJank, hasCompose, hasStall := false, false, false
	for _, finding := range findings {
		hasJank = hasJank || finding.DetectorID == "ui.jank_tail"
		hasCompose = hasCompose || finding.DetectorID == "ui.compose_work"
		hasStall = hasStall || finding.DetectorID == "stability.main_thread_stall"
	}
	screen = displayUnknown(screen, "без атрибуции")
	switch {
	case hasJank && hasStall:
		return fmt.Sprintf("Экран %s: зафиксированы подтормаживания и остановки главного потока", screen)
	case hasJank && hasCompose:
		return fmt.Sprintf("Экран %s: зафиксированы подтормаживания и тяжёлая Compose-работа", screen)
	case hasCompose && hasStall:
		return fmt.Sprintf("Экран %s: зафиксированы тяжёлая UI-работа и остановки главного потока", screen)
	case hasJank:
		return fmt.Sprintf("Экран %s: несколько связанных причин подтормаживаний", screen)
	case hasStall:
		return fmt.Sprintf("Экран %s: главный поток останавливался в нескольких местах", screen)
	default:
		return fmt.Sprintf("Экран %s: связанные UI-сигналы", screen)
	}
}

func appendUniqueProblemEvidence(values []ProblemEvidence, value ProblemEvidence) []ProblemEvidence {
	key := strings.Join([]string{value.Name, value.Observed, value.Unit, value.Source}, "\x00")
	for _, existing := range values {
		if strings.Join([]string{existing.Name, existing.Observed, existing.Unit, existing.Source}, "\x00") == key {
			return values
		}
	}
	return append(values, value)
}

func appendUniqueProblemRecommendations(values []ProblemRecommendation, candidates ...ProblemRecommendation) []ProblemRecommendation {
	for _, candidate := range candidates {
		duplicate := false
		for _, existing := range values {
			if existing.Action == candidate.Action {
				duplicate = true
				break
			}
		}
		if !duplicate {
			values = append(values, candidate)
		}
	}
	return values
}
