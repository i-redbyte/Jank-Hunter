package analyze

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/datavalue"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func priority(impact, magnitude, exposure, breadth, compounding int, impactWhy, magnitudeWhy, exposureWhy, breadthWhy, compoundWhy string) []ProblemPriorityComponent {
	return []ProblemPriorityComponent{
		{Component: "impact", Score: clamp(impact, 0, 40), Maximum: 40, Explanation: impactWhy},
		{Component: "magnitude", Score: clamp(magnitude, 0, 25), Maximum: 25, Explanation: magnitudeWhy},
		{Component: "exposure", Score: clamp(exposure, 0, 20), Maximum: 20, Explanation: exposureWhy},
		{Component: "breadth", Score: clamp(breadth, 0, 10), Maximum: 10, Explanation: breadthWhy},
		{Component: "compounding", Score: clamp(compounding, 0, 5), Maximum: 5, Explanation: compoundWhy},
	}
}

func investigationPriority(parts []ProblemPriorityComponent) int {
	total := 0
	for _, part := range parts {
		total += part.Score
	}
	return clamp(total, 0, 100)
}

func validateInvestigationPriority(finding ProblemFinding) error {
	if finding.InvestigationPriority < 0 || finding.InvestigationPriority > 100 || len(finding.PriorityBreakdown) != 5 {
		return fmt.Errorf("expected a value in 0..100 and five components")
	}
	expected := [...]struct {
		name    string
		maximum int
	}{{"impact", 40}, {"magnitude", 25}, {"exposure", 20}, {"breadth", 10}, {"compounding", 5}}
	for index, component := range finding.PriorityBreakdown {
		if component.Component != expected[index].name || component.Maximum != expected[index].maximum ||
			component.Score < 0 || component.Score > component.Maximum {
			return fmt.Errorf("component %d does not match %s 0..%d", index, expected[index].name, expected[index].maximum)
		}
	}
	if calculated := investigationPriority(finding.PriorityBreakdown); finding.InvestigationPriority != calculated {
		return fmt.Errorf("value %d does not equal component sum %d", finding.InvestigationPriority, calculated)
	}
	return nil
}

func severityForPriority(score int) string {
	switch {
	case score >= 80:
		return "critical"
	case score >= 60:
		return "high"
	case score >= 35:
		return "medium"
	case score > 0:
		return "low"
	default:
		return "info"
	}
}
func problemSeverityRank(value string) int {
	switch value {
	case "low":
		return 1
	case "medium":
		return 2
	case "high":
		return 3
	case "critical":
		return 4
	default:
		return 0
	}
}
func problemConfidenceRank(value string) int {
	switch value {
	case "low":
		return 1
	case "medium":
		return 2
	case "high":
		return 3
	default:
		return 0
	}
}

func capProblemConfidence(value, maximum string) string {
	if problemConfidenceRank(value) > problemConfidenceRank(maximum) {
		return maximum
	}
	return value
}

func problemConfidence(summary Summary, sample, minimum uint64, direct bool) (string, []string, []string) {
	confidence := "high"
	reasons := []string{}
	limits := []string{}
	if direct {
		reasons = append(reasons, "Симптом записан типизированным событием во время выполнения приложения.")
	}
	if sample < minimum {
		confidence = "low"
		reasons = append(reasons, fmt.Sprintf("Выборка %d меньше минимума %d.", sample, minimum))
		limits = append(limits, "Малая выборка: пока нельзя надёжно оценить типичную задержку и частоту вызовов.")
	} else {
		reasons = append(reasons, fmt.Sprintf("Выборка %d достигает минимума %d.", sample, minimum))
	}
	if collectionEvidenceDegraded(summary.CollectionQuality) {
		confidence = "low"
		reasons = append(reasons, "Охват измерений неполный, поэтому абсолютные количества считаются нижней оценкой.")
		limits = append(limits, "Используйте сохранённые места в коде для локализации, а частоту проблемы подтвердите повторным прогоном.")
	} else if confidence == "high" && !summary.AnalysisInputs.Complete {
		confidence = "medium"
		reasons = append(reasons, "Не все дополнительные материалы для анализа доступны.")
	}
	return confidence, uniqueStrings(reasons), uniqueStrings(limits)
}

func findingFingerprint(f ProblemFinding) string {
	// Fingerprints identify the underlying detector target. Subcategory and detector version are
	// deliberately excluded so a slow route can become a compound storm without looking "new".
	parts := []string{f.DetectorID, f.Category}
	if f.DetectorID == "stability.historical_process_exit" || strings.HasPrefix(f.DetectorID, "io.") {
		parts = append(parts, f.Subcategory)
	}
	locationKeys := make([]string, len(f.Where))
	for index, location := range f.Where {
		locationKeys[index] = locationKey(location)
	}
	sort.Strings(locationKeys)
	parts = append(parts, locationKeys...)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}

func locationKey(v ProblemLocation) string {
	return strings.ToLower(strings.Join([]string{v.Process, v.Screen, v.Operation, v.Route, v.Owner, v.Class, v.Method}, "\x1f"))
}
func locationBreadth(where []ProblemLocation) int {
	seen := map[string]struct{}{}
	for _, item := range where {
		seen[locationKey(item)] = struct{}{}
	}
	return min(10, max(1, len(seen)*2))
}
func appendUniqueLocation(values []ProblemLocation, value ProblemLocation) []ProblemLocation {
	key := locationKey(value)
	for _, item := range values {
		if locationKey(item) == key {
			return values
		}
	}
	return append(values, value)
}

func networkProblemLocations(summary Summary) map[string][]ProblemLocation {
	locations := make(map[string][]ProblemLocation, len(summary.Routes))
	for _, route := range summary.Routes {
		locations[route.Route] = []ProblemLocation{{Route: route.Route, Owner: route.OwnerSample}}
	}
	if summary.NetworkAnalysis != nil {
		for _, call := range summary.NetworkAnalysis.Calls {
			locations[call.Route] = appendUniqueLocation(locations[call.Route], ProblemLocation{
				Screen: call.Screen, Operation: call.Operation,
				Route: call.Route, Owner: firstKnown(call.Initiator, call.Owner),
			})
		}
	}
	for _, context := range summary.SignalContexts {
		if context.RouteSample == "" {
			continue
		}
		locations[context.RouteSample] = appendUniqueLocation(locations[context.RouteSample], ProblemLocation{
			Screen: context.Screen, Operation: context.Operation,
			Route: context.RouteSample, Owner: context.Owner,
		})
	}
	return locations
}
func findingsForCoverageCategory(values []ProblemFinding, category string) []ProblemFinding {
	out := make([]ProblemFinding, 0)
	for _, value := range values {
		if problemFindingHasCategory(value, category) {
			out = append(out, value)
		}
	}
	return out
}

func problemFindingHasCategory(finding ProblemFinding, category string) bool {
	if finding.Category == category {
		return true
	}
	for _, related := range finding.RelatedCategories {
		if related == category {
			return true
		}
	}
	return false
}
func ratePerSecond(count, durationMS uint64) *float64 {
	if durationMS == 0 {
		return nil
	}
	value := float64(count) * 1000 / float64(durationMS)
	return &value
}
func problemRatio(a, b uint64) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b)
}
func u64ptr(v uint64) *uint64 { return &v }
func nonZeroU64Ptr(v uint64) *uint64 {
	if v == 0 {
		return nil
	}
	return &v
}
func formatPercent(v float64) string { return fmt.Sprintf("%.1f", v) }
func formatOptionalRate(rate *float64) string {
	if rate == nil {
		return ""
	}
	return fmt.Sprintf(" (%.2f/с)", *rate)
}
func displayUnknown(v, fallback string) string {
	return datavalue.HumanUnknown(v, fallback)
}
func boolCount(values ...bool) int {
	total := 0
	for _, value := range values {
		if value {
			total++
		}
	}
	return total
}
func boolScore(value bool, score int) int {
	if value {
		return score
	}
	return 0
}
func clamp(v, low, high int) int { return min(high, max(low, v)) }
func nonEmptyStrings(values ...string) []string {
	out := []string{}
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, value)
		}
	}
	return out
}
func namedValue(values []NamedValue, name string) (uint64, bool) {
	for _, value := range values {
		if value.Name == name {
			return value.Value, true
		}
	}
	return 0, false
}
func hasNamedPrefix(values []NamedValue, prefix string) bool {
	for _, value := range values {
		if strings.HasPrefix(value.Name, prefix) {
			return true
		}
	}
	return false
}

func collectorConfigured(summary Summary, flag jhlog.CollectorFlag) bool {
	return summary.CollectorSessions > 0 && summary.CollectorFlagsAll&uint64(flag) != 0
}

func microsecondsToMillisecondsCeil(value uint64) uint64 {
	return value/1_000 + boolToUint64(value%1_000 != 0)
}

func formatMicroseconds(value uint64) string {
	if value%1_000 == 0 {
		return fmt.Sprintf("%d ms", value/1_000)
	}
	return fmt.Sprintf("%.2f ms", float64(value)/1_000)
}

func boolToUint64(value bool) uint64 {
	if value {
		return 1
	}
	return 0
}

func formatFrameDeadline(valueUS uint64) string {
	if valueUS == 0 {
		return "неоднородное или не записано"
	}
	return fmt.Sprintf("%.3f", float64(valueUS)/1_000)
}

func problemFrameSourceLabel(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "mixed":
		return "смешанный"
	case "choreographer":
		return "Choreographer"
	case "jankstats":
		return "JankStats"
	case "", "unknown":
		return "не записан"
	default:
		return value
	}
}

func ioOperationLabel(operation string) string {
	label := map[string]string{
		"file_read": "Чтение файла", "file_write": "Запись файла", "file_sync": "Синхронизация файла",
		"content_read": "Чтение ContentProvider", "content_write": "Запись ContentProvider",
	}[operation]
	return firstNonEmpty(label, operation, "I/O операция")
}
func processExitReason(reason uint64) (string, bool) {
	switch reason {
	case 3:
		return "low memory / OOM pressure", true
	case 4:
		return "Java crash", true
	case 5:
		return "native crash", true
	case 6:
		return "ANR", true
	case 7:
		return "initialization failure", true
	case 9:
		return "excessive resource usage", true
	default:
		return fmt.Sprintf("reason %d", reason), false
	}
}
func memoryTitle(leak MemoryLeakSuspect) string {
	if leak.HeapEvidence {
		return fmt.Sprintf("%s подтверждённо удерживается до GC root", displayUnknown(leak.ClassName, "Объект"))
	}
	return fmt.Sprintf("%s подозрительно долго удерживается", displayUnknown(leak.ClassName, "Объект"))
}
func networkSubcategory(slow, failed, storm bool) string {
	if storm && slow {
		return "slow_storm"
	}
	if storm {
		return "request_storm"
	}
	if failed {
		return "failure_rate"
	}
	return "slow_route"
}
func networkFactors(route RouteStats, slow, failed, storm bool) []string {
	out := []string{}
	if slow {
		out = append(out, "Высокая задержка среди самых медленных вызовов")
	}
	if failed {
		out = append(out, "Высокая доля ошибок")
	}
	if storm {
		out = append(out, "Высокий секундный пик завершений")
	}
	if phase, ok := dominantHTTPPhase(route.Phases); ok {
		out = append(out, fmt.Sprintf("Доминирующая измеренная фаза — %s, граница верхних 5%% %d мс", httpPhaseProblemLabel(phase.Name), phase.P95MS))
	}
	if route.Retries > 0 {
		out = append(out, fmt.Sprintf("%d дополнительных попыток запроса без учёта перенаправлений", route.Retries))
	}
	if route.ConnectFailures > 0 || route.TLSFailures > 0 {
		out = append(out, fmt.Sprintf("Ошибки попыток соединения: обычное соединение — %d, защищённое TLS-соединение — %d", route.ConnectFailures, route.TLSFailures))
	}
	return out
}
func networkWhat(route RouteStats, failures, count uint64, slow, storm bool, minimumSample uint64) string {
	parts := []string{fmt.Sprintf("Запрос выполнился %s", russianTimes(count))}
	if slow {
		switch {
		case count == 1:
			parts = append(parts, fmt.Sprintf("единственный вызов занял %d мс", route.P95MS))
		case count < minimumSample:
			parts = append(parts, fmt.Sprintf("задержка в верхней части небольшой выборки достигла %d мс", route.P95MS))
		default:
			parts = append(parts, fmt.Sprintf("95%% вызовов завершились не дольше %d мс", route.P95MS))
		}
	}
	if failures > 0 {
		parts = append(parts, fmt.Sprintf("с ошибкой завершилось %s", russianRequestCount(failures)))
	}
	if storm {
		parts = append(parts, fmt.Sprintf("в пике завершалось %d запросов в секунду — это выше допустимого уровня", route.PeakRequestsPerSecond))
	}
	if phase, ok := dominantHTTPPhase(route.Phases); ok {
		parts = append(parts, fmt.Sprintf("самая длинная фаза среди верхних 5%% задержек — %s (%d мс)", httpPhaseProblemLabel(phase.Name), phase.P95MS))
	}
	if route.Retries > 0 {
		parts = append(parts, fmt.Sprintf("зафиксировано %d повторных попыток без учета редиректов", route.Retries))
	}
	return strings.Join(parts, "; ") + "."
}

func dominantHTTPPhase(phases []HTTPPhaseStats) (HTTPPhaseStats, bool) {
	var dominant HTTPPhaseStats
	found := false
	for _, phase := range phases {
		if phase.SampleCount == 0 || (found && phase.P95MS <= dominant.P95MS) {
			continue
		}
		dominant = phase
		found = true
	}
	return dominant, found
}

func httpPhaseProblemLabel(name string) string {
	switch name {
	case "queue":
		return "очередь"
	case "dns":
		return "DNS"
	case "connect":
		return "соединение"
	case "tls":
		return "TLS"
	case "request":
		return "отправка запроса"
	case "ttfb":
		return "TTFB"
	case "response":
		return "получение ответа"
	default:
		return name
	}
}

func russianTimes(count uint64) string {
	if count%10 == 1 && count%100 != 11 {
		return fmt.Sprintf("%d раз", count)
	}
	if count%10 >= 2 && count%10 <= 4 && (count%100 < 12 || count%100 > 14) {
		return fmt.Sprintf("%d раза", count)
	}
	return fmt.Sprintf("%d раз", count)
}

func russianRequestCount(count uint64) string {
	word := "запросов"
	if count%10 == 1 && count%100 != 11 {
		word = "запрос"
	} else if count%10 >= 2 && count%10 <= 4 && (count%100 < 12 || count%100 > 14) {
		word = "запроса"
	}
	return fmt.Sprintf("%d %s", count, word)
}
