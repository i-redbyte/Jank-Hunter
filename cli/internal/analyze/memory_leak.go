package analyze

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

func buildMemoryLeakSuspects(items map[string]*memoryLeakStats, lowMemoryCount int, maxPSSKB uint64) []MemoryLeakSuspect {
	out := make([]MemoryLeakSuspect, 0, len(items))
	for _, item := range items {
		if item == nil || item.count == 0 {
			continue
		}
		suspect := memoryLeakSuspectFromStats(*item, lowMemoryCount, maxPSSKB)
		if suspect.Score <= 0 {
			continue
		}
		out = append(out, suspect)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			if out[i].MaxAgeMS == out[j].MaxAgeMS {
				return out[i].ClassName < out[j].ClassName
			}
			return out[i].MaxAgeMS > out[j].MaxAgeMS
		}
		return out[i].Score > out[j].Score
	})
	if len(out) > 80 {
		out = out[:80]
	}
	return out
}

func memoryLeakSuspectFromStats(item memoryLeakStats, lowMemoryCount int, maxPSSKB uint64) MemoryLeakSuspect {
	className := item.className
	holder := item.holder
	if holder == "" || holder == "unknown" {
		holder = "не определен"
	}
	userOwned := isLikelyAppClass(className) || isLikelyAppClass(holder)
	systemRetained := isLikelySystemClass(className)
	objectKind := retainedObjectKind(className)
	holderQuality := retainedHolderQuality(holder)
	estimatedRetainedKB := estimatedRetainedSizeKB(item, objectKind, maxPSSKB)
	sizeConfidence := retainedSizeConfidence(item, holderQuality, maxPSSKB)
	dominatorPath := retainedDominatorPath(item, holder, className)
	dominatorConfidence := retainedDominatorConfidence(holderQuality, dominatorPath)
	score := retainedScore(item, userOwned, systemRetained, lowMemoryCount, maxPSSKB)
	severity := "ok"
	switch {
	case score >= 16 || item.maxAgeMs >= 60_000 || item.count >= 10 || estimatedRetainedKB >= 16*1024:
		severity = "high"
	case score >= 7 || item.maxAgeMs >= 15_000 || item.count >= 2 || estimatedRetainedKB >= 4*1024:
		severity = "medium"
	}
	return MemoryLeakSuspect{
		ClassName:                className,
		Holder:                   holder,
		Screen:                   emptyUnknown(item.screen),
		Flow:                     emptyUnknown(item.flow),
		Step:                     emptyUnknown(item.step),
		Count:                    item.count,
		MaxAgeMS:                 item.maxAgeMs,
		EstimatedRetainedKB:      estimatedRetainedKB,
		RetainedSizeConfidence:   sizeConfidence,
		RetainedSizeExplanation:  retainedSizeExplanation(estimatedRetainedKB, sizeConfidence, objectKind),
		DominatorPath:            dominatorPath,
		DominatorTreeConfidence:  dominatorConfidence,
		DominatorTreeExplanation: retainedDominatorExplanation(dominatorConfidence),
		Score:                    math.Round(score*10) / 10,
		Severity:                 severity,
		ObjectKind:               objectKind,
		HolderQuality:            holderQuality,
		UserOwned:                userOwned,
		SystemRetained:           systemRetained,
		Impact:                   retainedImpact(className, item, lowMemoryCount, maxPSSKB, estimatedRetainedKB),
		Recommendation:           retainedRecommendation(className, holder, holderQuality),
		Evidence:                 retainedEvidence(item, lowMemoryCount, maxPSSKB, estimatedRetainedKB, sizeConfidence),
	}
}

func retainedScore(item memoryLeakStats, userOwned, systemRetained bool, lowMemoryCount int, maxPSSKB uint64) float64 {
	score := float64(item.count)*2 + float64(item.maxAgeMs)/10_000
	if item.maxAgeMs >= 60_000 {
		score += 8
	} else if item.maxAgeMs >= 30_000 {
		score += 4
	}
	if userOwned {
		score += 4
	}
	if systemRetained {
		score += 3
	}
	if strings.Contains(strings.ToLower(item.className), "activity") ||
		strings.Contains(strings.ToLower(item.className), "fragment") ||
		strings.Contains(strings.ToLower(item.className), "context") {
		score += 5
	}
	if lowMemoryCount > 0 {
		score += 2
	}
	if maxPSSKB >= 512*1024 {
		score += 2
	}
	if estimated := estimatedRetainedSizeKB(item, retainedObjectKind(item.className), maxPSSKB); estimated >= 16*1024 {
		score += 5
	} else if estimated >= 4*1024 {
		score += 2
	}
	return score
}

func retainedObjectKind(className string) string {
	lower := strings.ToLower(className)
	switch {
	case strings.Contains(lower, "activity"):
		return "экран / Activity"
	case strings.Contains(lower, "fragment"):
		return "Fragment"
	case strings.Contains(lower, "context"):
		return "Context"
	case strings.Contains(lower, "view") || strings.Contains(lower, "binding"):
		return "View / binding"
	case strings.Contains(lower, "closeable") || strings.Contains(lower, "stream") || strings.Contains(lower, "cursor"):
		return "ресурс"
	case isLikelySystemClass(className):
		return "системный объект"
	default:
		return "пользовательский объект"
	}
}

func retainedHolderQuality(holder string) string {
	switch {
	case holder == "" || holder == "unknown" || holder == "не определен":
		return "держатель не определен"
	case strings.HasPrefix(holder, "lifecycle.destroyed."):
		return "автоматическая проверка после destroy, точный держатель не определен"
	default:
		return "вероятный держатель из контекста"
	}
}

func retainedImpact(className string, item memoryLeakStats, lowMemoryCount int, maxPSSKB, estimatedRetainedKB uint64) string {
	parts := []string{fmt.Sprintf("Удержано %d объект(ов), максимальный возраст %s.", item.count, formatDurationMS(item.maxAgeMs))}
	if estimatedRetainedKB > 0 {
		parts = append(parts, fmt.Sprintf("Оценка удержанного размера: %s.", formatDataSize(estimatedRetainedKB)))
	}
	if isLikelySystemClass(className) {
		parts = append(parts, "Системный объект в строке важен только вместе с пользовательским держателем или экраном.")
	}
	if lowMemoryCount > 0 {
		parts = append(parts, fmt.Sprintf("В прогоне были сигналы низкой памяти: %d.", lowMemoryCount))
	}
	if maxPSSKB > 0 {
		parts = append(parts, fmt.Sprintf("Макс. PSS процесса: %s.", formatDataSize(maxPSSKB)))
	}
	return strings.Join(parts, " ")
}

func retainedRecommendation(className, holder, holderQuality string) string {
	if holderQuality == "держатель не определен" || strings.HasPrefix(holderQuality, "автоматическая проверка") {
		return "Проверьте владельцев ссылок на этот объект: singleton, static/cache, корутинные задачи, слушатели, обратные вызовы, adapter, ViewModel и DI scope. Для точного владельца добавьте ownerHint в watchObject или оберните участок в withOwner."
	}
	if isLikelySystemClass(className) {
		return fmt.Sprintf("Проверьте пользовательский держатель %s: не хранит ли он Context/View/Activity дольше жизненного цикла.", holder)
	}
	return fmt.Sprintf("Проверьте %s: очистку слушателей и обратных вызовов, отмену корутинных задач, слабые или сбрасываемые ссылки, scope DI и отмену фоновой работы при уходе экрана.", holder)
}

func retainedEvidence(item memoryLeakStats, lowMemoryCount int, maxPSSKB, estimatedRetainedKB uint64, sizeConfidence string) string {
	parts := []string{
		fmt.Sprintf("кол-во=%d", item.count),
		fmt.Sprintf("макс. возраст=%s", formatDurationMS(item.maxAgeMs)),
	}
	if estimatedRetainedKB > 0 {
		parts = append(parts, fmt.Sprintf("оценка удержанного размера=%s", formatDataSize(estimatedRetainedKB)))
		parts = append(parts, "доверие размера="+sizeConfidence)
	}
	if item.screen != "" && item.screen != "unknown" {
		parts = append(parts, "экран="+item.screen)
	}
	if item.flow != "" && item.flow != "unknown" {
		parts = append(parts, "флоу="+item.flow)
	}
	if item.step != "" && item.step != "unknown" {
		parts = append(parts, "шаг="+item.step)
	}
	if lowMemoryCount > 0 {
		parts = append(parts, fmt.Sprintf("низкая память=%d", lowMemoryCount))
	}
	if maxPSSKB > 0 {
		parts = append(parts, fmt.Sprintf("макс. PSS=%s", formatDataSize(maxPSSKB)))
	}
	return strings.Join(parts, " · ")
}

func estimatedRetainedSizeKB(item memoryLeakStats, objectKind string, maxPSSKB uint64) uint64 {
	baseKB := retainedObjectBaseSizeKB(objectKind)
	ageMultiplier := uint64(1)
	switch {
	case item.maxAgeMs >= 60_000:
		ageMultiplier = 3
	case item.maxAgeMs >= 30_000:
		ageMultiplier = 2
	}
	estimate := baseKB * maxUint64(item.count, 1) * ageMultiplier
	if maxPSSKB > 0 {
		capKB := maxPSSKB / 3
		if capKB < baseKB {
			capKB = baseKB
		}
		if estimate > capKB {
			estimate = capKB
		}
	}
	return estimate
}

func retainedObjectBaseSizeKB(objectKind string) uint64 {
	switch objectKind {
	case "экран / Activity":
		return 2 * 1024
	case "Fragment":
		return 1024
	case "Context":
		return 768
	case "View / binding":
		return 768
	case "ресурс":
		return 256
	case "системный объект":
		return 128
	default:
		return 256
	}
}

func retainedSizeConfidence(item memoryLeakStats, holderQuality string, maxPSSKB uint64) string {
	switch {
	case maxPSSKB == 0:
		return "низкое: нет PSS в прогоне"
	case holderQuality == "держатель не определен":
		return "низкое: нет держателя"
	case item.count >= 3 || item.maxAgeMs >= 30_000:
		return "среднее: есть возраст/повторяемость"
	default:
		return "ориентировочное: мало повторов"
	}
}

func retainedSizeExplanation(sizeKB uint64, confidence, objectKind string) string {
	if sizeKB == 0 {
		return "Размер не рассчитан: в логе нет достаточных runtime-сигналов."
	}
	return fmt.Sprintf("Это не точный размер удержанной кучи из дампа памяти, а оценка по типу объекта %q, числу удержаний, возрасту и PSS процесса. Доверие: %s.", objectKind, confidence)
}

func retainedDominatorPath(item memoryLeakStats, holder, className string) []string {
	path := make([]string, 0, 5)
	if item.screen != "" && item.screen != "unknown" {
		path = append(path, "экран: "+item.screen)
	}
	if item.flow != "" && item.flow != "unknown" {
		path = append(path, "флоу: "+item.flow)
	}
	if item.step != "" && item.step != "unknown" {
		path = append(path, "шаг: "+item.step)
	}
	if holder != "" && holder != "unknown" && holder != "не определен" {
		path = append(path, "держатель: "+holder)
	}
	path = append(path, "удержанный объект: "+className)
	return path
}

func retainedDominatorConfidence(holderQuality string, path []string) string {
	if holderQuality == "держатель не определен" || len(path) <= 1 {
		return "низкое: нужен дамп кучи или ownerHint"
	}
	if strings.HasPrefix(holderQuality, "автоматическая проверка") {
		return "среднее: известен lifecycle-контекст"
	}
	return "среднее: путь собран из контекста выполнения"
}

func retainedDominatorExplanation(confidence string) string {
	return "Мини-дерево показывает вероятную цепочку доминирования по контексту выполнения. Это помогает быстрее найти владельца ссылки, но точный корень GC и размер удержанной кучи появятся только при анализе дампа памяти. Доверие: " + confidence + "."
}

func isLikelyAppClass(value string) bool {
	if value == "" || value == "unknown" || value == "не определен" || strings.HasPrefix(value, "lifecycle.") {
		return false
	}
	return strings.Contains(value, ".") && !isLikelySystemClass(value)
}

func emptyUnknown(value string) string {
	value = strings.TrimSpace(value)
	if value == "unknown" {
		return ""
	}
	return value
}

func isLikelySystemClass(value string) bool {
	lower := strings.ToLower(value)
	prefixes := []string{
		"android.",
		"androidx.",
		"java.",
		"javax.",
		"kotlin.",
		"kotlinx.",
		"com.android.",
		"dalvik.",
		"libcore.",
		"sun.",
		"io.jankhunter.",
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

func formatDurationMS(ms uint64) string {
	switch {
	case ms >= 86_400_000 && ms%86_400_000 == 0:
		return fmt.Sprintf("%d д", ms/86_400_000)
	case ms >= 3_600_000 && ms%3_600_000 == 0:
		return fmt.Sprintf("%d ч", ms/3_600_000)
	case ms >= 60_000 && ms%60_000 == 0:
		return fmt.Sprintf("%d мин", ms/60_000)
	case ms >= 1_000:
		if ms%1_000 == 0 {
			return fmt.Sprintf("%d сек", ms/1_000)
		}
		return fmt.Sprintf("%.1f сек", float64(ms)/1_000)
	default:
		return fmt.Sprintf("%d мс", ms)
	}
}
