package analyze

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

const (
	RetentionEvidenceTimeOnly           = "time_only"
	RetentionEvidenceAfterExplicitGC    = "after_explicit_gc"
	RetentionEvidenceConfirmedHPROFPath = "confirmed_hprof/path"
	RetentionEvidenceUnconfirmedHPROF   = "unconfirmed_hprof"
)

func buildMemoryLeakSuspects(
	items map[string]*memoryLeakStats,
	lowMemoryCount int,
	maxPSSKB uint64,
	heap *HeapEvidence,
	quality retentionDataQuality,
) []MemoryLeakSuspect {
	out := make([]MemoryLeakSuspect, 0, len(items))
	for _, item := range items {
		if item == nil || item.count == 0 {
			continue
		}
		suspect := memoryLeakSuspectFromStats(
			*item,
			lowMemoryCount,
			maxPSSKB,
			bestHeapEvidence(*item, heap),
			quality,
		)
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

func memoryLeakSuspectFromStats(
	item memoryLeakStats,
	lowMemoryCount int,
	maxPSSKB uint64,
	heap *HeapLeakEvidence,
	quality retentionDataQuality,
) MemoryLeakSuspect {
	className := item.className
	holder := item.holder
	if holder == "" || holder == "unknown" {
		holder = "не определен"
	}
	heapCandidate := heap != nil
	heapEvidence := confirmedHeapReferencePath(heap)
	var confirmedHeap *HeapLeakEvidence
	if heapEvidence {
		confirmedHeap = heap
	}
	evidenceKind := retainedEvidenceKind(item, heap, heapEvidence)
	evidenceLabel := retainedEvidenceLabel(evidenceKind)
	qualityWarnings := retainedQualityWarnings(item, heapCandidate, quality)
	dataQuality := "complete"
	if len(qualityWarnings) > 0 {
		dataQuality = "degraded"
	}
	if heapCandidate {
		if heap.Holder != "" {
			holder = heap.Holder
		} else if heap.HolderField != "" {
			holder = heap.HolderField
		}
	}
	userOwned := isLikelyAppClass(className) || isLikelyAppClass(holder)
	systemRetained := isLikelySystemClass(className)
	objectKind := retainedObjectKind(className)
	holderQuality := retainedHolderQuality(holder)
	estimatedRetainedKB := estimatedRetainedSizeKB(item, objectKind, maxPSSKB)
	sizeConfidence := retainedSizeConfidence(item, holderQuality, maxPSSKB)
	dominatorPath := retainedDominatorPath(item, holder, className)
	dominatorConfidence := retainedDominatorConfidence(holderQuality, dominatorPath)
	chainConfidence := retainedAttributionConfidence(item, holderQuality, userOwned)
	chainSummary := retainedLeakChainSummary(item, holder, className, objectKind, chainConfidence, evidenceKind)
	chainActions := retainedLeakChainActions(item, holder, className, objectKind, holderQuality)
	score := retainedScore(item, userOwned, systemRetained, lowMemoryCount, maxPSSKB)
	score *= retainedRuntimeEvidenceWeight(evidenceKind)
	retainedObjectCount := uint64(0)
	heapSource := ""
	gcRoot := ""
	gcRootCategory := ""
	holderField := ""
	chainFingerprint := ""
	leakPattern := ""
	var referenceMatchers []string
	var alternativePaths [][]HeapPathElement
	if heapCandidate {
		estimatedRetainedKB = firstPositive(heap.RetainedSizeKB, estimatedRetainedKB)
		sizeConfidence = firstNonEmpty(heap.Confidence, "среднее: значение получено из дампа памяти")
		retainedObjectCount = heap.RetainedObjectCount
		heapSource = heap.Source
		if heapEvidence {
			gcRoot = heap.GCRoot
			gcRootCategory = heap.GCRootCategory
			holderField = heap.HolderField
			chainFingerprint = heap.ChainFingerprint
			leakPattern = heap.LeakPattern
			referenceMatchers = append([]string(nil), heap.ReferenceMatchers...)
			alternativePaths = cloneHeapPaths(heap.AlternativePaths)
		}
	}
	if heapEvidence {
		sizeConfidence = firstNonEmpty(heap.Confidence, "высокое: рассчитано из дампа памяти")
		dominatorPath = heapDominatorPath(*heap, className)
		dominatorConfidence = "высокое: путь найден в дампе памяти до корня GC"
		chainConfidence = "высокое: в HPROF найден путь от распознанного корня GC до объекта"
		chainSummary = retainedHeapChainSummary(*heap, holder, className, objectKind)
		chainActions = retainedHeapChainActions(*heap, chainActions)
		score += 6
		if estimatedRetainedKB >= 16*1024 {
			score += 4
		} else if estimatedRetainedKB >= 4*1024 {
			score += 2
		}
	}
	severity := "ok"
	switch {
	case score >= 16 || item.maxAgeMs >= 60_000 || item.count >= 10 || estimatedRetainedKB >= 16*1024:
		severity = "high"
	case score >= 7 || item.maxAgeMs >= 15_000 || item.count >= 2 || estimatedRetainedKB >= 4*1024:
		severity = "medium"
	}
	if (evidenceKind == RetentionEvidenceTimeOnly || evidenceKind == RetentionEvidenceUnconfirmedHPROF) && severity == "high" {
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
		EvidenceKind:             evidenceKind,
		EvidenceLabel:            evidenceLabel,
		EvidenceConfidence:       retainedEvidenceConfidence(evidenceKind, dataQuality),
		TimeOnlyCount:            item.timeOnlyCount,
		AfterExplicitGCCount:     item.afterExplicitGCCount,
		DataQuality:              dataQuality,
		QualityWarnings:          qualityWarnings,
		EstimatedRetainedKB:      estimatedRetainedKB,
		HeapEvidence:             heapEvidence,
		HeapCandidate:            heapCandidate,
		HeapSource:               heapSource,
		GCRoot:                   gcRoot,
		GCRootCategory:           gcRootCategory,
		ChainFingerprint:         firstNonEmpty(chainFingerprint, runtimeLeakFingerprint(className, holder, item)),
		HolderField:              holderField,
		RetainedObjectCount:      retainedObjectCount,
		ReferencePath:            cloneHeapPath(heapPath(confirmedHeap)),
		AlternativePaths:         alternativePaths,
		AlternativePathSummaries: heapPathSummaries(alternativePaths),
		RetainedClassSample:      append([]string(nil), heapDominatorTree(confirmedHeap)...),
		LeakPattern:              leakPattern,
		ReferenceMatchers:        referenceMatchers,
		RetainedSizeConfidence:   sizeConfidence,
		RetainedSizeExplanation:  retainedSizeExplanation(estimatedRetainedKB, sizeConfidence, objectKind, heap),
		DominatorPath:            dominatorPath,
		DominatorTreeConfidence:  dominatorConfidence,
		DominatorTreeExplanation: retainedDominatorExplanation(dominatorConfidence, confirmedHeap),
		LeakChainConfidence:      chainConfidence,
		LeakChainSummary:         chainSummary,
		LeakChainActions:         chainActions,
		InvestigationSteps:       retainedInvestigationSteps(item, holder, className, objectKind, confirmedHeap),
		FixExamples:              retainedFixExamples(holder, objectKind, confirmedHeap),
		VerificationSteps:        retainedVerificationSteps(heapEvidence),
		Score:                    math.Round(score*10) / 10,
		Severity:                 severity,
		ObjectKind:               objectKind,
		HolderQuality:            holderQuality,
		UserOwned:                userOwned,
		SystemRetained:           systemRetained,
		Impact:                   retainedImpact(className, item, lowMemoryCount, maxPSSKB, estimatedRetainedKB, heap, evidenceKind),
		Recommendation:           retainedRecommendation(className, holder, holderQuality),
		Evidence: retainedEvidence(
			item,
			lowMemoryCount,
			maxPSSKB,
			estimatedRetainedKB,
			sizeConfidence,
			heap,
			evidenceKind,
			qualityWarnings,
		),
	}
}

func confirmedHeapReferencePath(heap *HeapLeakEvidence) bool {
	if heap == nil || len(heap.ReferencePath) < 2 {
		return false
	}
	first := heap.ReferencePath[0]
	return heap.GCRoot != "" || first.Kind == "gc_root" || strings.HasPrefix(first.ClassName, "GC root: ")
}

func retainedEvidenceKind(item memoryLeakStats, heap *HeapLeakEvidence, confirmedPath bool) string {
	if confirmedPath {
		return RetentionEvidenceConfirmedHPROFPath
	}
	if item.timeOnlyCount > 0 {
		return RetentionEvidenceTimeOnly
	}
	if item.afterExplicitGCCount > 0 {
		return RetentionEvidenceAfterExplicitGC
	}
	if heap != nil {
		return RetentionEvidenceUnconfirmedHPROF
	}
	return RetentionEvidenceTimeOnly
}

func retainedEvidenceLabel(kind string) string {
	switch kind {
	case RetentionEvidenceConfirmedHPROFPath:
		return "подтвержденный путь HPROF"
	case RetentionEvidenceAfterExplicitGC:
		return "объект остался достижим после запрошенного GC"
	case RetentionEvidenceUnconfirmedHPROF:
		return "данные HPROF без подтвержденного пути"
	default:
		return "объект жив после временной задержки"
	}
}

func retainedRuntimeEvidenceWeight(kind string) float64 {
	switch kind {
	case RetentionEvidenceConfirmedHPROFPath:
		return 1
	case RetentionEvidenceUnconfirmedHPROF:
		return 0.6
	case RetentionEvidenceAfterExplicitGC:
		return 0.75
	default:
		return 0.45
	}
}

func retainedEvidenceConfidence(kind, dataQuality string) string {
	confidence := "низкое"
	switch kind {
	case RetentionEvidenceConfirmedHPROFPath:
		confidence = "высокое"
	case RetentionEvidenceAfterExplicitGC, RetentionEvidenceUnconfirmedHPROF:
		confidence = "среднее"
	}
	if dataQuality == "degraded" {
		switch confidence {
		case "высокое":
			confidence = "среднее"
		case "среднее":
			confidence = "низкое"
		}
		return confidence + ": качество входных данных снижено"
	}
	return confidence + ": " + retainedEvidenceLabel(kind)
}

func retainedQualityWarnings(item memoryLeakStats, heapCandidate bool, quality retentionDataQuality) []string {
	var warnings []string
	if item.timeOnlyCount+item.afterExplicitGCCount > 0 && quality.runtimeMayBeIncomplete {
		warnings = append(warnings, quality.runtimeNotes...)
	}
	if item.timeOnlyCount+item.afterExplicitGCCount > 0 && quality.dictionaryDegraded {
		warnings = append(warnings, quality.dictionaryNotes...)
	}
	if heapCandidate && quality.heapDegraded {
		warnings = append(warnings, quality.heapNotes...)
	}
	return uniqueStrings(warnings)
}

func heapPath(heap *HeapLeakEvidence) []HeapPathElement {
	if heap == nil {
		return nil
	}
	return heap.ReferencePath
}

func heapDominatorTree(heap *HeapLeakEvidence) []string {
	if heap == nil {
		return nil
	}
	return heap.DominatorTree
}

func cloneHeapPath(path []HeapPathElement) []HeapPathElement {
	if len(path) == 0 {
		return nil
	}
	return append([]HeapPathElement(nil), path...)
}

func cloneHeapPaths(paths [][]HeapPathElement) [][]HeapPathElement {
	if len(paths) == 0 {
		return nil
	}
	out := make([][]HeapPathElement, 0, len(paths))
	for _, path := range paths {
		out = append(out, cloneHeapPath(path))
	}
	return out
}

func heapPathSummaries(paths [][]HeapPathElement) []string {
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		labels := make([]string, 0, len(path))
		for _, step := range path {
			label := strings.TrimPrefix(step.ClassName, "GC root: ")
			if label == "" {
				label = step.Kind
			}
			if step.FieldName != "" {
				label = step.FieldName + " → " + label
			}
			if label != "" {
				labels = append(labels, label)
			}
		}
		if len(labels) > 0 {
			out = append(out, strings.Join(labels, " → "))
		}
	}
	return uniqueStrings(out)
}

func runtimeLeakFingerprint(className, holder string, item memoryLeakStats) string {
	return strings.Join([]string{
		normalizeLeakToken(className),
		normalizeLeakToken(holder),
		normalizeLeakToken(item.screen),
		normalizeLeakToken(item.flow),
		normalizeLeakToken(item.step),
	}, "|")
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
	if isHighRiskRetainedClass(item.className) {
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
	case strings.Contains(lower, "viewmodel"):
		return "ViewModel"
	case strings.Contains(lower, "service"):
		return "Service"
	case strings.Contains(lower, "dialog"):
		return "Dialog"
	case strings.Contains(lower, "viewholder"):
		return "RecyclerView ViewHolder"
	case strings.Contains(lower, "adapter"):
		return "adapter"
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

func isHighRiskRetainedClass(className string) bool {
	lower := strings.ToLower(className)
	return strings.Contains(lower, "activity") ||
		strings.Contains(lower, "fragment") ||
		strings.Contains(lower, "context") ||
		strings.Contains(lower, "viewmodel") ||
		strings.Contains(lower, "service") ||
		strings.Contains(lower, "dialog") ||
		strings.Contains(lower, "viewholder") ||
		strings.Contains(lower, "adapter") ||
		strings.Contains(lower, "binding") ||
		strings.Contains(lower, "view")
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

func retainedImpact(
	className string,
	item memoryLeakStats,
	lowMemoryCount int,
	maxPSSKB,
	estimatedRetainedKB uint64,
	heap *HeapLeakEvidence,
	evidenceKind string,
) string {
	parts := []string{
		fmt.Sprintf("Наблюдалось %d достижимых объект(ов), максимальный возраст %s.", item.count, formatDurationMS(item.maxAgeMs)),
		retainedEvidenceMeaning(evidenceKind),
	}
	if heap != nil && evidenceKind == RetentionEvidenceConfirmedHPROFPath {
		if heap.LeakPattern != "" {
			parts = append(parts, "Паттерн: "+heap.LeakPattern+".")
		}
		if heap.RetainedObjectCount > 0 {
			parts = append(parts, fmt.Sprintf("В дампе памяти доминируемых объектов: %d.", heap.RetainedObjectCount))
		}
		if heap.GCRoot != "" {
			parts = append(parts, "Корень GC: "+heap.GCRoot+".")
		}
	}
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
		return "Проверьте владельцев ссылок на этот объект: одиночки, статические поля и кеши, корутинные задачи, слушатели, обратные вызовы, adapter, ViewModel и область DI. Для точного владельца добавьте ownerHint в watchObject или оберните участок в withOwner."
	}
	if isLikelySystemClass(className) {
		return fmt.Sprintf("Проверьте пользовательский держатель %s: не хранит ли он Context/View/Activity дольше жизненного цикла.", holder)
	}
	return fmt.Sprintf("Проверьте %s: очистку слушателей и обратных вызовов, отмену корутинных задач, слабые или сбрасываемые ссылки, область DI и отмену фоновой работы при уходе экрана.", holder)
}

func retainedEvidence(
	item memoryLeakStats,
	lowMemoryCount int,
	maxPSSKB,
	estimatedRetainedKB uint64,
	sizeConfidence string,
	heap *HeapLeakEvidence,
	evidenceKind string,
	qualityWarnings []string,
) string {
	parts := []string{
		"уровень=" + evidenceKind,
		"смысл=" + retainedEvidenceMeaning(evidenceKind),
		fmt.Sprintf("кол-во=%d", item.count),
		fmt.Sprintf("макс. возраст=%s", formatDurationMS(item.maxAgeMs)),
		fmt.Sprintf("time_only=%d", item.timeOnlyCount),
		fmt.Sprintf("after_explicit_gc=%d", item.afterExplicitGCCount),
	}
	if heap != nil {
		parts = append(parts, "дамп=есть", fmt.Sprintf("подтвержденный путь=%t", confirmedHeapReferencePath(heap)))
		if heap.Source != "" {
			parts = append(parts, "источник дампа="+heap.Source)
		}
		if heap.GCRoot != "" {
			parts = append(parts, "корень GC="+heap.GCRoot)
		}
		if heap.HolderField != "" {
			parts = append(parts, "поле="+heap.HolderField)
		}
		if heap.LeakPattern != "" {
			parts = append(parts, "паттерн="+heap.LeakPattern)
		}
		if len(heap.ReferenceMatchers) > 0 {
			parts = append(parts, "совпавшие правила ссылок="+strings.Join(heap.ReferenceMatchers, ","))
		}
		if heap.RetainedObjectCount > 0 {
			parts = append(parts, fmt.Sprintf("доминируемые объекты=%d", heap.RetainedObjectCount))
		}
	}
	if estimatedRetainedKB > 0 {
		parts = append(parts, fmt.Sprintf("оценка удержанного размера=%s", formatDataSize(estimatedRetainedKB)))
		parts = append(parts, "доверие размера="+sizeConfidence)
	}
	if item.screen != "" && item.screen != "unknown" {
		parts = append(parts, "экран="+item.screen)
	}
	if item.flow != "" && item.flow != "unknown" {
		parts = append(parts, "сценарий="+item.flow)
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
	if len(qualityWarnings) > 0 {
		parts = append(parts, "ограничения качества="+strings.Join(qualityWarnings, "; "))
	}
	return strings.Join(parts, " · ")
}

func retainedEvidenceMeaning(kind string) string {
	switch kind {
	case RetentionEvidenceConfirmedHPROFPath:
		return "HPROF подтвердил путь от распознанного корня GC; это доказательство удержания в момент дампа, но не автоматический диагноз утечки"
	case RetentionEvidenceAfterExplicitGC:
		return "объект остался достижим после запрошенного явного GC; это сильное подозрение, но без пути ссылок не доказательство утечки"
	case RetentionEvidenceUnconfirmedHPROF:
		return "HPROF содержит сведения об объекте, но путь от корня GC не подтвержден; утечка не доказана"
	default:
		return "объект оставался жив только после временной задержки; GC и путь ссылок не подтверждены, это не доказательство утечки"
	}
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
	case "ViewModel":
		return 512
	case "Service":
		return 1024
	case "Dialog":
		return 768
	case "RecyclerView ViewHolder":
		return 512
	case "adapter":
		return 768
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

func retainedSizeExplanation(sizeKB uint64, confidence, objectKind string, heap *HeapLeakEvidence) string {
	if sizeKB == 0 {
		return "Размер не рассчитан: в логе нет достаточных сигналов выполнения."
	}
	if heap != nil {
		source := heap.Source
		if source == "" {
			source = "дамп памяти"
		}
		return fmt.Sprintf("Размер взят из анализа %s: удержанный размер=%s. Доверие: %s.", source, formatDataSize(sizeKB), confidence)
	}
	return fmt.Sprintf("Это не точный размер удержанной кучи из дампа памяти, а оценка по типу объекта %q, числу удержаний, возрасту и PSS процесса. Доверие: %s.", objectKind, confidence)
}

func retainedDominatorPath(item memoryLeakStats, holder, className string) []string {
	path := make([]string, 0, 6)
	if item.screen != "" && item.screen != "unknown" {
		path = append(path, "экран: "+item.screen)
	}
	if item.flow != "" && item.flow != "unknown" {
		path = append(path, "сценарий: "+item.flow)
	}
	if item.step != "" && item.step != "unknown" {
		path = append(path, "шаг: "+item.step)
	}
	if holder != "" && holder != "unknown" && holder != "не определен" {
		holderClass, holderMethod := splitHolderReference(holder)
		if holderClass != "" {
			path = append(path, "держатель: "+holderClass)
		} else {
			path = append(path, "держатель: "+holder)
		}
		if holderMethod != "" {
			path = append(path, "метод: "+holderMethod)
		}
	}
	path = append(path, "удержанный объект: "+className)
	return path
}

func retainedDominatorConfidence(holderQuality string, path []string) string {
	if holderQuality == "держатель не определен" || len(path) <= 1 {
		return "низкое: нужен дамп кучи или ownerHint"
	}
	if strings.HasPrefix(holderQuality, "автоматическая проверка") {
		return "среднее: известен контекст жизненного цикла"
	}
	return "среднее: путь собран из контекста выполнения"
}

func retainedDominatorExplanation(confidence string, heap *HeapLeakEvidence) string {
	if heap != nil {
		details := []string{"Путь построен из дампа памяти и показывает цепочку ссылок от корня GC до удержанного объекта."}
		if len(heap.DominatorTree) > 0 {
			details = append(details, "В мини-дереве доминирования: "+strings.Join(heap.DominatorTree, ", ")+".")
		}
		details = append(details, "Доверие: "+confidence+".")
		return strings.Join(details, " ")
	}
	return "Схема показывает контекст, в котором объект оставался жив после lifecycle-события. Она помогает найти вероятного владельца и место наблюдения, но не является цепочкой ссылок; точный корень GC и удержанный размер доступны только из дампа памяти. Доверие: " + confidence + "."
}

func retainedAttributionConfidence(item memoryLeakStats, holderQuality string, userOwned bool) string {
	hasContext := knownContextCount(item) > 0
	switch {
	case holderQuality == "держатель не определен" && !hasContext:
		return "низкое: нет держателя и контекста"
	case holderQuality == "держатель не определен":
		return "низкое: есть контекст, но нет владельца ссылки"
	case userOwned && hasContext && item.count >= 2:
		return "среднее+: пользовательский держатель, контекст и повторяемость"
	case userOwned && hasContext:
		return "среднее: пользовательский держатель и контекст"
	case userOwned:
		return "среднее: есть пользовательский держатель"
	default:
		return "ориентировочное: контекст собран по сигналам выполнения"
	}
}

func retainedLeakChainSummary(item memoryLeakStats, holder, className, objectKind, confidence, evidenceKind string) string {
	parts := []string{
		fmt.Sprintf("Наблюдается достижимый %s %s.", objectKind, className),
		retainedEvidenceMeaning(evidenceKind) + ".",
	}
	holderClass, holderMethod := splitHolderReference(holder)
	if holderClass != "" {
		if holderMethod != "" {
			parts = append(parts, fmt.Sprintf("Вероятный пользовательский держатель: %s, метод %s.", holderClass, holderMethod))
		} else {
			parts = append(parts, fmt.Sprintf("Вероятный пользовательский держатель: %s.", holderClass))
		}
	} else if holder != "" && holder != "unknown" && holder != "не определен" {
		parts = append(parts, fmt.Sprintf("Вероятный держатель: %s.", holder))
	} else {
		parts = append(parts, "Вероятный держатель ссылки не определен.")
	}
	if item.screen != "" && item.screen != "unknown" {
		parts = append(parts, "Экран: "+item.screen+".")
	}
	if item.flow != "" && item.flow != "unknown" {
		parts = append(parts, "Сценарий: "+item.flow+".")
	}
	if item.step != "" && item.step != "unknown" {
		parts = append(parts, "Шаг: "+item.step+".")
	}
	parts = append(parts, "Доверие runtime-атрибуции: "+confidence+".")
	return strings.Join(parts, " ")
}

func retainedLeakChainActions(item memoryLeakStats, holder, className, objectKind, holderQuality string) []string {
	actions := []string{}
	holderClass, holderMethod := splitHolderReference(holder)
	switch {
	case holderQuality == "держатель не определен":
		actions = append(actions, "Добавьте ownerHint в watchObject/watchActivity или оберните подозрительный участок в withOwner.")
	case holderClass != "":
		if holderMethod != "" {
			actions = append(actions, fmt.Sprintf("Проверьте %s.%s: какие поля, кеши, слушатели или обратные вызовы сохраняют %s.", holderClass, holderMethod, className))
		} else {
			actions = append(actions, fmt.Sprintf("Проверьте поля, кеши, слушатели и обратные вызовы внутри %s.", holderClass))
		}
	}
	switch objectKind {
	case "экран / Activity", "Fragment", "Context":
		actions = append(actions, "Проверьте жизненный цикл: очистку ссылок в onDestroy/onDestroyView и отсутствие Activity/Context в одиночках, статических полях, одиночках DI и долгих задачах.")
	case "ViewModel":
		actions = append(actions, "Проверьте onCleared: отмену корутинных задач и Flow-подписок, очистку наблюдателей LiveData и обратных вызовов, отсутствие ссылок на View/Activity.")
	case "Service":
		actions = append(actions, "Проверьте onDestroy сервиса: отмену передней и фоновой работы, unregisterReceiver, обратные вызовы и освобождение долгоживущих ресурсов.")
	case "Dialog":
		actions = append(actions, "Проверьте dismiss/onStop/onDestroy: ссылки слушателя, окна и decorView должны очищаться, а обратный вызов не должен держать Activity.")
	case "RecyclerView ViewHolder", "adapter":
		actions = append(actions, "Проверьте очистку RecyclerView: onViewRecycled/onViewDetachedFromWindow/onDetachedFromRecyclerView должны очищать привязку view, слушателей, наблюдателей и обратные вызовы адаптера.")
	case "View / binding":
		actions = append(actions, "Проверьте ссылки binding/View: сброс в onDestroyView, очистку адаптера и слушателя, отсутствие View в фоновых обратных вызовах.")
	case "ресурс":
		actions = append(actions, "Проверьте закрытие Cursor/Stream/Closeable и отмену работы при уходе экрана.")
	case "системный объект":
		actions = append(actions, "Системный объект считайте симптомом: ищите пользовательский держатель, который дольше жизненного цикла держит ссылку на него.")
	default:
		actions = append(actions, "Проверьте коллекции, кеши, корутинные задачи, Flow-подписки, наблюдателей и слушателей, которые живут дольше экрана.")
	}
	if item.maxAgeMs >= 30_000 {
		actions = append(actions, "Возраст удержания больше 30 секунд: проверьте долгие корутинные задачи, задачи исполнителя и отмену при закрытии сценария.")
	}
	if item.count >= 3 {
		actions = append(actions, "Удержание повторяется: ищите накопление в списках, кешах, подписках или повторной регистрации слушателя без снятия.")
	}
	return uniqueStrings(actions)
}

func retainedInvestigationSteps(item memoryLeakStats, holder, className, objectKind string, heap *HeapLeakEvidence) []string {
	steps := []string{
		"Откройте проводник утечек и начните с первого класса приложения после корня GC или владельца из событий выполнения.",
	}
	if heap != nil {
		if heap.HolderField != "" {
			steps = append(steps, "Найдите поле "+heap.HolderField+" в коде и проверьте, где ссылка присваивается и где должна очищаться.")
		}
		if heap.LeakPattern != "" {
			steps = append(steps, "Проверьте конкретный паттерн: "+heap.LeakPattern+".")
		}
		if heap.GCRootCategory != "" {
			steps = append(steps, "Проверьте категорию корня GC: "+heap.GCRootCategory+"; она подсказывает, искать статическое поле, одиночку, поток, JNI или системный обратный вызов.")
		}
		if len(heap.AlternativePaths) > 0 {
			steps = append(steps, "Посмотрите альтернативные пути: если их несколько, исправляйте самого долгоживущего владельца, а не только первое поле.")
		}
	} else if holder == "" || holder == "unknown" || holder == "не определен" {
		steps = append(steps, "Повторите сценарий с ownerHint или withOwner, чтобы легкий режим связал объект с владельцем ссылки.")
	}
	if item.screen != "" && item.screen != "unknown" {
		steps = append(steps, "Воспроизведите экран "+item.screen+" и проверьте, что объект исчезает после закрытия экрана и задержки удержания.")
	}
	switch objectKind {
	case "экран / Activity", "Fragment", "Context":
		steps = append(steps, "Проверьте границу жизненного цикла: onDestroy/onDestroyView, отписку наблюдателей и слушателей, отмену фоновой работы.")
	case "ViewModel":
		steps = append(steps, "Проверьте ViewModel.onCleared: все корутинные области, Flow/LiveData подписки и обратные вызовы должны завершаться или отвязываться.")
	case "Service":
		steps = append(steps, "Проверьте путь Service.onDestroy и stopSelf/stopForeground: не остается ли активный поток, receiver, binder-обратный вызов или ссылка из одиночки.")
	case "Dialog":
		steps = append(steps, "Проверьте dismiss/onStop/onDestroy: ссылки окна, decorView и слушателей не должны переживать закрытие диалога.")
	case "RecyclerView ViewHolder", "adapter":
		steps = append(steps, "Проверьте жизненный цикл RecyclerView: binding, слушатель и наблюдатель очищаются при recycle/detach, адаптер не держит экран после detach.")
	case "View / binding":
		steps = append(steps, "Проверьте, что binding/View не хранится после onDestroyView и не попадает в адаптер, слушатель или кеш.")
	case "ресурс":
		steps = append(steps, "Проверьте close/cancel/dispose для ресурса и его владельца.")
	default:
		if strings.Contains(strings.ToLower(className), "listener") || strings.Contains(strings.ToLower(holder), "listener") {
			steps = append(steps, "Проверьте регистрацию слушателя или обратного вызова: у каждой регистрации должна быть симметричная отписка.")
		} else {
			steps = append(steps, "Проверьте коллекции, кеши, одиночки, области DI и долгоживущие обратные вызовы.")
		}
	}
	return uniqueStrings(steps)
}

func retainedFixExamples(holder, objectKind string, heap *HeapLeakEvidence) []string {
	examples := []string{}
	if heap != nil && heap.HolderField != "" {
		examples = append(examples, "Очистите "+heap.HolderField+" на границе жизненного цикла или замените сильную ссылку на WeakReference, если владелец обязан жить дольше.")
	}
	if heap != nil && heap.LeakPattern != "" {
		examples = append(examples, "Зафиксируйте паттерн \""+heap.LeakPattern+"\" регрессионным сценарием: объект должен исчезнуть после задержки удержания и повторного дампа памяти.")
	}
	switch objectKind {
	case "экран / Activity", "Context":
		examples = append(examples, "Не храните Activity/Context в одиночке или статическом поле; передавайте applicationContext только для зависимостей всего приложения.")
		examples = append(examples, "Отменяйте корутинную работу и задачи исполнителя в onDestroy или области владельца экрана.")
	case "Fragment":
		examples = append(examples, "Очищайте view binding в onDestroyView и отписывайте наблюдателей, привязанных к жизненному циклу view.")
	case "ViewModel":
		examples = append(examples, "В onCleared отменяйте задачи и области, очищайте обратные вызовы; не храните Activity/View/Fragment в ViewModel.")
	case "Service":
		examples = append(examples, "В onDestroy снимайте receivers и слушателей, останавливайте переднюю работу и закрывайте binder/native ресурсы.")
	case "Dialog":
		examples = append(examples, "Перед dismiss/onDestroy сбрасывайте слушателей и обратные вызовы, которые держат Activity или decorView.")
	case "RecyclerView ViewHolder", "adapter":
		examples = append(examples, "В onViewRecycled/onViewDetachedFromWindow очищайте binding и слушателей; при detach адаптера очищайте обратные вызовы на экран.")
	case "View / binding":
		examples = append(examples, "Сбрасывайте обратные вызовы адаптера и слушателей, которые держат View или binding после закрытия экрана.")
	case "ресурс":
		examples = append(examples, "Используйте use/try-finally и закрывайте Cursor/Stream/Closeable при отмене сценария.")
	default:
		examples = append(examples, "Для кеша или слушателя держите remove/clear рядом с add/register и покрывайте это тестом жизненного цикла.")
	}
	if holder != "" && holder != "unknown" && holder != "не определен" {
		examples = append(examples, "Добавьте тест или отладочную проверку, что "+holder+" после очистки не содержит ссылку на удержанный объект.")
	}
	return uniqueStrings(examples)
}

func retainedVerificationSteps(heapEvidence bool) []string {
	steps := []string{
		"Повторите тот же пользовательский сценарий после фикса.",
		"Сравните базовый и проверяемый прогоны во вкладке «Утечки памяти» файла compare.html и убедитесь, что отпечаток стал слабее или исчез.",
	}
	if heapEvidence {
		steps = append(steps, "Снимите новый HPROF и проверьте, что путь до корня GC исчез или удержанный размер заметно упал.")
	} else {
		steps = append(steps, "Если легкий режим всё ещё показывает удержание, повторите прогон с --heap-dump или --heap-evidence.")
	}
	return steps
}

func bestHeapEvidence(item memoryLeakStats, heap *HeapEvidence) *HeapLeakEvidence {
	if heap == nil {
		return nil
	}
	bestIndex := -1
	bestScore := -1
	for i := range heap.Leaks {
		leak := heap.Leaks[i]
		if leak.ClassName != item.className {
			continue
		}
		score := 0
		if leak.Holder != "" && item.holder != "" && strings.Contains(strings.ToLower(leak.Holder), strings.ToLower(item.holder)) {
			score += 1000
		}
		if leak.HolderField != "" && item.holder != "" && strings.Contains(strings.ToLower(leak.HolderField), strings.ToLower(item.holder)) {
			score += 800
		}
		score += heapLeakActionabilityScore(leak) * 100
		score += int(minUint64(leak.RetainedSizeKB, 512*1024) / 1024)
		if len(leak.ReferencePath) > 0 && len(leak.ReferencePath) < maxHprofPathElements {
			score += maxHprofPathElements - len(leak.ReferencePath)
		}
		if score > bestScore || (score == bestScore && (bestIndex < 0 || betterHeapLeak(leak, heap.Leaks[bestIndex]))) {
			bestScore = score
			bestIndex = i
		}
	}
	if bestIndex < 0 {
		return nil
	}
	return &heap.Leaks[bestIndex]
}

func heapDominatorPath(heap HeapLeakEvidence, className string) []string {
	if len(heap.ReferencePath) == 0 {
		if len(heap.DominatorTree) > 0 {
			return heap.DominatorTree
		}
		return []string{"удержанный объект: " + className}
	}
	out := make([]string, 0, len(heap.ReferencePath))
	for i, step := range heap.ReferencePath {
		if i == 0 && strings.HasPrefix(step.ClassName, "GC root: ") {
			out = append(out, step.ClassName)
			continue
		}
		label := step.ClassName
		if step.FieldName != "" {
			label = step.FieldName + " -> " + label
		}
		if step.Kind == "static" && !strings.HasPrefix(label, "static ") {
			label = "static " + label
		}
		out = append(out, label)
	}
	if len(out) == 0 {
		out = append(out, "удержанный объект: "+className)
	}
	return out
}

func retainedHeapChainSummary(heap HeapLeakEvidence, holder, className, objectKind string) string {
	parts := []string{fmt.Sprintf("Удержан %s %s, цепочка подтверждена дампом памяти.", objectKind, className)}
	if heap.GCRoot != "" {
		parts = append(parts, "Корень GC: "+heap.GCRoot+".")
	}
	if heap.GCRootCategory != "" {
		parts = append(parts, "Категория корня: "+heap.GCRootCategory+".")
	}
	if heap.Holder != "" {
		parts = append(parts, "Пользовательский держатель: "+heap.Holder+".")
	} else if holder != "" && holder != "unknown" && holder != "не определен" {
		parts = append(parts, "Держатель из контекста выполнения: "+holder+".")
	}
	if heap.HolderField != "" {
		parts = append(parts, "Поле/ссылка: "+heap.HolderField+".")
	}
	if heap.RetainedSizeKB > 0 {
		parts = append(parts, "Удержанный размер: "+formatDataSize(heap.RetainedSizeKB)+".")
	}
	if heap.RetainedObjectCount > 0 {
		parts = append(parts, fmt.Sprintf("Доминатор удерживает %d объект(ов).", heap.RetainedObjectCount))
	}
	if len(heap.AlternativePaths) > 0 {
		parts = append(parts, fmt.Sprintf("Найдено альтернативных цепочек: %d.", len(heap.AlternativePaths)))
	}
	return strings.Join(parts, " ")
}

func retainedHeapChainActions(heap HeapLeakEvidence, fallback []string) []string {
	actions := make([]string, 0, len(fallback)+2)
	if heap.HolderField != "" {
		actions = append(actions, "Начните с поля "+heap.HolderField+": очистите ссылку на границе жизненного цикла или перенесите владельца в более короткую область.")
	}
	if heap.GCRoot != "" {
		actions = append(actions, "Проверьте, почему цепочка живет от корня GC "+heap.GCRoot+": статическое поле, одиночка, thread-local, активный поток, JNI или системный обратный вызов.")
	}
	if heap.GCRootCategory != "" {
		switch heap.GCRootCategory {
		case "class/static":
			actions = append(actions, "Категория class/static: ищите companion object, object singleton, статическое поле, одиночку DI или глобальный кеш.")
		case "thread":
			actions = append(actions, "Категория thread: проверьте активные Runnable/Coroutine/Executor/Handler задачи и ThreadLocal.")
		case "jni":
			actions = append(actions, "Категория JNI: проверьте native/global references и обратные вызовы сторонних SDK.")
		case "monitor":
			actions = append(actions, "Категория monitor: проверьте синхронизацию и объекты, удерживаемые заблокированными потоками.")
		}
	}
	if len(heap.ReferenceMatchers) > 0 {
		actions = append(actions, "Правила совпадения ссылок подсказали область риска: "+strings.Join(heap.ReferenceMatchers, ", ")+". Проверьте соответствующую очистку фреймворка, слушателя или контекста.")
	}
	actions = append(actions, fallback...)
	return uniqueStrings(actions)
}

func firstPositive(values ...uint64) uint64 {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func splitHolderReference(holder string) (string, string) {
	holder = strings.TrimSpace(holder)
	if holder == "" || holder == "unknown" || holder == "не определен" || strings.HasPrefix(holder, "lifecycle.") {
		return "", ""
	}
	holder = strings.TrimPrefix(holder, "owner.")
	hashIndex := strings.Index(holder, "#")
	if hashIndex >= 0 {
		holder = holder[:hashIndex]
	}
	dot := strings.LastIndex(holder, ".")
	if dot <= 0 || dot >= len(holder)-1 {
		return holder, ""
	}
	return holder[:dot], holder[dot+1:]
}

func knownContextCount(item memoryLeakStats) int {
	count := 0
	for _, value := range []string{item.screen, item.flow, item.step} {
		if value != "" && value != "unknown" {
			count++
		}
	}
	return count
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
