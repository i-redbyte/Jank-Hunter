package analyze

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/datavalue"
)

const (
	maxProblemIncidentLocations       = 8
	maxProblemIncidentEvidence        = 8
	maxProblemIncidentFactors         = 8
	maxProblemIncidentRecommendations = 4
)

type problemIncidentGroup struct {
	key      string
	findings []ProblemFinding
}

// buildProblemIncidents keeps detector findings lossless while collapsing signals that belong to
// one UI investigation or one retained class. Detailed and exported findings remain separate.
func buildProblemIncidents(findings []ProblemFinding) []ProblemFinding {
	if len(findings) == 0 {
		return nil
	}
	incidents := make([]ProblemFinding, 0, len(findings))
	groupIndexes := make(map[string]int)
	groups := make([]problemIncidentGroup, 0)
	for index := range findings {
		finding := findings[index]
		key, grouped := problemIncidentKey(finding)
		if !grouped {
			incidents = append(incidents, mergeProblemIncident("", findings[index:index+1]))
			continue
		}
		groupIndex, exists := groupIndexes[key]
		if !exists {
			groupIndex = len(groups)
			groupIndexes[key] = groupIndex
			groups = append(groups, problemIncidentGroup{key: key})
		}
		groups[groupIndex].findings = append(groups[groupIndex].findings, finding)
	}
	for _, group := range groups {
		incidents = append(incidents, mergeProblemIncident(group.key, group.findings))
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

func problemIncidentKey(finding ProblemFinding) (string, bool) {
	if isUIIncidentSignal(finding) {
		if screen := incidentScreen(finding.Where); screen != "" {
			return "ui-screen\x00" + strings.ToLower(screen), true
		}
	}
	if finding.DetectorID == "memory.retention" {
		className, _ := memoryIncidentTarget(finding.Where)
		if className != "" {
			key := "memory-retention\x00" + finding.Subcategory + "\x00" +
				strings.ToLower(className)
			if finding.Subcategory == "confirmed_leak" {
				key += "\x00" + strings.ToLower(strings.Join(finding.Why.Factors, "\x00"))
			}
			return key, true
		}
	}
	return "", false
}

func memoryIncidentTarget(locations []ProblemLocation) (string, string) {
	for _, location := range locations {
		className := strings.TrimSpace(location.Class)
		if className == "" || datavalue.IsUnknown(className) {
			continue
		}
		owner := strings.TrimSpace(location.Owner)
		if datavalue.IsUnknown(owner) {
			owner = ""
		}
		return className, owner
	}
	return "", ""
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
		categories := make([]string, 0, len(result.RelatedCategories)+1)
		categories = append(categories, result.RelatedCategories...)
		categories = append(categories, result.Category)
		result.RelatedCategories = uniqueStrings(categories)
		sort.Strings(result.RelatedCategories)
		if key != "" {
			result.Fingerprint = incidentFingerprint(key)
			result.ID = "incident-" + result.Fingerprint[:16]
		}
		return result
	}
	if strings.HasPrefix(key, "memory-retention\x00") {
		return mergeMemoryProblemIncident(key, findings)
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
			if len(result.Where) < maxProblemIncidentLocations {
				result.Where = appendUniqueLocation(result.Where, location)
			}
		}
		for _, evidence := range finding.Evidence {
			if len(result.Evidence) < maxProblemIncidentEvidence {
				result.Evidence = appendUniqueProblemEvidence(result.Evidence, evidence)
			}
		}
		if finding.ID != primary.ID && len(result.Recommendations) < maxProblemIncidentRecommendations {
			result.Recommendations = appendUniqueProblemRecommendations(result.Recommendations, finding.Recommendations...)
		}
		if len(factors) < maxProblemIncidentFactors {
			factors = append(factors, fmt.Sprintf("%s: %s", finding.Title, finding.WhatHappened))
		}
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
		displayUnknown(screen, "экран не определён"),
		russianCountUint64(uint64(len(findings)), "связанный сигнал", "связанных сигнала", "связанных сигналов"),
	)
	result.Why = ProblemWhy{
		ClaimLevel: "correlated",
		Summary:    incidentWhySummary(findings),
		Factors:    factors,
	}
	return result
}

func mergeMemoryProblemIncident(key string, findings []ProblemFinding) ProblemFinding {
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
	result.Recommendations = nil
	result.Limitations = nil
	result.RelatedFindings = make([]string, 0, len(findings))
	result.RelatedCategories = nil

	locationsSeen := make(map[ProblemLocation]struct{}, min(len(findings), maxProblemIncidentLocations))
	recommendationsSeen := make(map[string]struct{}, maxProblemIncidentRecommendations)
	factorsSeen := make(map[string]struct{}, maxProblemIncidentFactors)
	factors := make([]string, 0, min(len(findings), maxProblemIncidentFactors))
	uniqueLocationCount := 0
	var totalCount, maxAgeMS, maxMemoryKB uint64
	for _, finding := range findings {
		result.RelatedFindings = append(result.RelatedFindings, finding.ID)
		result.RelatedCategories = append(result.RelatedCategories, finding.Category)
		result.RelatedCategories = append(result.RelatedCategories, finding.RelatedCategories...)
		if finding.Confidence == result.Confidence {
			result.ConfidenceReasons = append(result.ConfidenceReasons, finding.ConfidenceReasons...)
		}
		result.Impact = append(result.Impact, finding.Impact...)
		result.Limitations = append(result.Limitations, finding.Limitations...)
		if finding.Frequency != nil {
			totalCount = saturatingUint64Sum(totalCount, finding.Frequency.Count)
		}
		if finding.Cost != nil && finding.Cost.MemoryKB != nil {
			maxMemoryKB = maxUint64(maxMemoryKB, *finding.Cost.MemoryKB)
		}
		maxAgeMS = maxUint64(maxAgeMS, retentionEvidenceAgeMS(finding.Evidence))
		for _, location := range finding.Where {
			if _, exists := locationsSeen[location]; exists {
				continue
			}
			locationsSeen[location] = struct{}{}
			uniqueLocationCount++
			if len(result.Where) < maxProblemIncidentLocations {
				result.Where = append(result.Where, location)
			}
		}
		for _, recommendation := range finding.Recommendations {
			if len(result.Recommendations) == maxProblemIncidentRecommendations {
				break
			}
			if _, exists := recommendationsSeen[recommendation.Action]; exists {
				continue
			}
			recommendationsSeen[recommendation.Action] = struct{}{}
			result.Recommendations = append(result.Recommendations, recommendation)
		}
		for _, factor := range finding.Why.Factors {
			if len(factors) == maxProblemIncidentFactors {
				break
			}
			if _, exists := factorsSeen[factor]; exists {
				continue
			}
			factorsSeen[factor] = struct{}{}
			factors = append(factors, factor)
		}
	}

	sort.Strings(result.RelatedFindings)
	result.RelatedCategories = uniqueStrings(result.RelatedCategories)
	sort.Strings(result.RelatedCategories)
	result.ConfidenceReasons = uniqueStrings(result.ConfidenceReasons)
	result.Impact = uniqueStrings(result.Impact)
	result.Limitations = uniqueStrings(append(
		result.Limitations,
		"Карточка объединяет один удержанный класс из разных контекстов и с разными вероятными держателями. Исходные сигналы остаются доступными отдельно.",
	))
	sort.SliceStable(result.Where, func(i, j int) bool {
		return locationHasApplicationSymbol(result.Where[i]) && !locationHasApplicationSymbol(result.Where[j])
	})
	result.Fingerprint = incidentFingerprint(key)
	result.ID = "incident-" + result.Fingerprint[:16]
	result.Frequency = &ProblemFrequency{Count: totalCount}
	result.Cost = &ProblemCost{MemoryKB: nonZeroU64Ptr(maxMemoryKB)}
	result.Evidence = memoryIncidentEvidence(totalCount, maxAgeMS, maxMemoryKB, uniqueLocationCount)
	result.Why.Factors = factors
	className, _ := memoryIncidentTarget(result.Where)
	result.WhatHappened = fmt.Sprintf(
		"Объект %s оставался достижимым до %s. Объединено %s из %s; наибольшая отдельная оценка памяти - %s.",
		displayUnknown(className, "неизвестного класса"),
		retentionIncidentDuration(maxAgeMS),
		russianCountUint64(totalCount, "наблюдение", "наблюдения", "наблюдений"),
		russianCountUint64(uint64(uniqueLocationCount), "контекст", "контекста", "контекстов"),
		formatDataSize(maxMemoryKB),
	)
	return result
}

func retentionEvidenceAgeMS(evidence []ProblemEvidence) uint64 {
	var maximum uint64
	for _, item := range evidence {
		if item.Name != "Возраст удержания" || item.Unit != "ms" {
			continue
		}
		value, err := strconv.ParseUint(item.Observed, 10, 64)
		if err == nil && value > maximum {
			maximum = value
		}
	}
	return maximum
}

func memoryIncidentEvidence(count, maxAgeMS, maxMemoryKB uint64, contexts int) []ProblemEvidence {
	evidence := make([]ProblemEvidence, 0, 4)
	evidence = append(evidence,
		ProblemEvidence{Name: "Наблюдения", Observed: strconv.FormatUint(count, 10), Unit: "events", Source: "aggregated_retention"},
		ProblemEvidence{Name: "Контексты", Observed: strconv.Itoa(contexts), Unit: "contexts", Source: "aggregated_retention"},
	)
	if maxAgeMS > 0 {
		evidence = append(evidence, ProblemEvidence{Name: "Максимальный возраст", Observed: strconv.FormatUint(maxAgeMS, 10), Unit: "ms", Source: "retention"})
	}
	if maxMemoryKB > 0 {
		evidence = append(evidence, ProblemEvidence{Name: "Наибольшая отдельная оценка", Observed: strconv.FormatUint(maxMemoryKB, 10), Unit: "KB", Source: "retention"})
	}
	return evidence
}

func retentionIncidentDuration(milliseconds uint64) string {
	if milliseconds < 1_000 {
		return fmt.Sprintf("%d мс", milliseconds)
	}
	if milliseconds%1_000 == 0 {
		return fmt.Sprintf("%d с", milliseconds/1_000)
	}
	return fmt.Sprintf("%.1f с", float64(milliseconds)/1_000)
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

var frameworkSymbolPrefixes = [...]string{
	"android.", "androidx.", "java.", "javax.", "kotlin.", "kotlinx.", "dalvik.",
	"libcore.", "sun.", "com.android.", "com.google.android.", "com.google.common.",
	"com.google.firebase.", "leakcanary.", "handler (android.",
}

func IsFrameworkSymbol(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || datavalue.IsUnknown(value) {
		return false
	}
	lower := strings.ToLower(value)
	for _, prefix := range frameworkSymbolPrefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

func isApplicationSymbol(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || datavalue.IsUnknown(value) || IsFrameworkSymbol(value) {
		return false
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
	screen = displayUnknown(screen, "экран не определён")
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
	for _, existing := range values {
		if existing.Name == value.Name && existing.Observed == value.Observed &&
			existing.Unit == value.Unit && existing.Source == value.Source {
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
