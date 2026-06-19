package analyze

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

func buildMemoryLeakSuspects(items map[string]*memoryLeakStats, lowMemoryCount int, maxPSSKB uint64, heap *HeapEvidence) []MemoryLeakSuspect {
	out := make([]MemoryLeakSuspect, 0, len(items))
	for _, item := range items {
		if item == nil || item.count == 0 {
			continue
		}
		suspect := memoryLeakSuspectFromStats(*item, lowMemoryCount, maxPSSKB, bestHeapEvidence(*item, heap))
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

func memoryLeakSuspectFromStats(item memoryLeakStats, lowMemoryCount int, maxPSSKB uint64, heap *HeapLeakEvidence) MemoryLeakSuspect {
	className := item.className
	holder := item.holder
	if holder == "" || holder == "unknown" {
		holder = "не определен"
	}
	heapEvidence := heap != nil
	if heapEvidence {
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
	chainConfidence := retainedLeakChainConfidence(item, holderQuality, userOwned)
	chainSummary := retainedLeakChainSummary(item, holder, className, objectKind, chainConfidence)
	chainActions := retainedLeakChainActions(item, holder, className, objectKind, holderQuality)
	score := retainedScore(item, userOwned, systemRetained, lowMemoryCount, maxPSSKB)
	retainedObjectCount := uint64(0)
	heapSource := ""
	gcRoot := ""
	holderField := ""
	if heapEvidence {
		estimatedRetainedKB = firstPositive(heap.RetainedSizeKB, estimatedRetainedKB)
		sizeConfidence = firstNonEmpty(heap.Confidence, "высокое: рассчитано из heap dump")
		dominatorPath = heapDominatorPath(*heap, className)
		dominatorConfidence = "высокое: путь найден в heap dump до GC root"
		chainConfidence = "высокое: runtime retained-событие связано с heap-путем до GC root"
		chainSummary = retainedHeapChainSummary(*heap, holder, className, objectKind)
		chainActions = retainedHeapChainActions(*heap, chainActions)
		retainedObjectCount = heap.RetainedObjectCount
		heapSource = heap.Source
		gcRoot = heap.GCRoot
		holderField = heap.HolderField
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
	return MemoryLeakSuspect{
		ClassName:                className,
		Holder:                   holder,
		Screen:                   emptyUnknown(item.screen),
		Flow:                     emptyUnknown(item.flow),
		Step:                     emptyUnknown(item.step),
		Count:                    item.count,
		MaxAgeMS:                 item.maxAgeMs,
		EstimatedRetainedKB:      estimatedRetainedKB,
		HeapEvidence:             heapEvidence,
		HeapSource:               heapSource,
		GCRoot:                   gcRoot,
		HolderField:              holderField,
		RetainedObjectCount:      retainedObjectCount,
		ReferencePath:            cloneHeapPath(heapPath(heap)),
		RetainedClassSample:      append([]string(nil), heapDominatorTree(heap)...),
		RetainedSizeConfidence:   sizeConfidence,
		RetainedSizeExplanation:  retainedSizeExplanation(estimatedRetainedKB, sizeConfidence, objectKind, heap),
		DominatorPath:            dominatorPath,
		DominatorTreeConfidence:  dominatorConfidence,
		DominatorTreeExplanation: retainedDominatorExplanation(dominatorConfidence, heap),
		LeakChainConfidence:      chainConfidence,
		LeakChainSummary:         chainSummary,
		LeakChainActions:         chainActions,
		Score:                    math.Round(score*10) / 10,
		Severity:                 severity,
		ObjectKind:               objectKind,
		HolderQuality:            holderQuality,
		UserOwned:                userOwned,
		SystemRetained:           systemRetained,
		Impact:                   retainedImpact(className, item, lowMemoryCount, maxPSSKB, estimatedRetainedKB, heap),
		Recommendation:           retainedRecommendation(className, holder, holderQuality),
		Evidence:                 retainedEvidence(item, lowMemoryCount, maxPSSKB, estimatedRetainedKB, sizeConfidence, heap),
	}
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

func retainedImpact(className string, item memoryLeakStats, lowMemoryCount int, maxPSSKB, estimatedRetainedKB uint64, heap *HeapLeakEvidence) string {
	parts := []string{fmt.Sprintf("Удержано %d объект(ов), максимальный возраст %s.", item.count, formatDurationMS(item.maxAgeMs))}
	if heap != nil {
		if heap.RetainedObjectCount > 0 {
			parts = append(parts, fmt.Sprintf("В heap dump доминируемых объектов: %d.", heap.RetainedObjectCount))
		}
		if heap.GCRoot != "" {
			parts = append(parts, "GC root: "+heap.GCRoot+".")
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
		return "Проверьте владельцев ссылок на этот объект: singleton, static/cache, корутинные задачи, слушатели, обратные вызовы, adapter, ViewModel и DI scope. Для точного владельца добавьте ownerHint в watchObject или оберните участок в withOwner."
	}
	if isLikelySystemClass(className) {
		return fmt.Sprintf("Проверьте пользовательский держатель %s: не хранит ли он Context/View/Activity дольше жизненного цикла.", holder)
	}
	return fmt.Sprintf("Проверьте %s: очистку слушателей и обратных вызовов, отмену корутинных задач, слабые или сбрасываемые ссылки, scope DI и отмену фоновой работы при уходе экрана.", holder)
}

func retainedEvidence(item memoryLeakStats, lowMemoryCount int, maxPSSKB, estimatedRetainedKB uint64, sizeConfidence string, heap *HeapLeakEvidence) string {
	parts := []string{
		fmt.Sprintf("кол-во=%d", item.count),
		fmt.Sprintf("макс. возраст=%s", formatDurationMS(item.maxAgeMs)),
	}
	if heap != nil {
		parts = append(parts, "heap=есть")
		if heap.Source != "" {
			parts = append(parts, "heap source="+heap.Source)
		}
		if heap.GCRoot != "" {
			parts = append(parts, "GC root="+heap.GCRoot)
		}
		if heap.HolderField != "" {
			parts = append(parts, "поле="+heap.HolderField)
		}
		if heap.RetainedObjectCount > 0 {
			parts = append(parts, fmt.Sprintf("dominated objects=%d", heap.RetainedObjectCount))
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

func retainedSizeExplanation(sizeKB uint64, confidence, objectKind string, heap *HeapLeakEvidence) string {
	if sizeKB == 0 {
		return "Размер не рассчитан: в логе нет достаточных runtime-сигналов."
	}
	if heap != nil {
		source := heap.Source
		if source == "" {
			source = "heap dump"
		}
		return fmt.Sprintf("Размер взят из анализа %s: retained size=%s. Доверие: %s.", source, formatDataSize(sizeKB), confidence)
	}
	return fmt.Sprintf("Это не точный размер удержанной кучи из дампа памяти, а оценка по типу объекта %q, числу удержаний, возрасту и PSS процесса. Доверие: %s.", objectKind, confidence)
}

func retainedDominatorPath(item memoryLeakStats, holder, className string) []string {
	path := make([]string, 0, 6)
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
		return "среднее: известен lifecycle-контекст"
	}
	return "среднее: путь собран из контекста выполнения"
}

func retainedDominatorExplanation(confidence string, heap *HeapLeakEvidence) string {
	if heap != nil {
		details := []string{"Путь построен из heap dump и показывает цепочку ссылок от GC root до удержанного объекта."}
		if len(heap.DominatorTree) > 0 {
			details = append(details, "В мини-дереве доминирования: "+strings.Join(heap.DominatorTree, ", ")+".")
		}
		details = append(details, "Доверие: "+confidence+".")
		return strings.Join(details, " ")
	}
	return "Мини-дерево показывает вероятную цепочку доминирования по контексту выполнения. Это помогает быстрее найти владельца ссылки, но точный корень GC и размер удержанной кучи появятся только при анализе дампа памяти. Доверие: " + confidence + "."
}

func retainedLeakChainConfidence(item memoryLeakStats, holderQuality string, userOwned bool) string {
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
		return "ориентировочное: цепочка построена по runtime-сигналам"
	}
}

func retainedLeakChainSummary(item memoryLeakStats, holder, className, objectKind, confidence string) string {
	parts := []string{fmt.Sprintf("Удержан %s %s.", objectKind, className)}
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
		parts = append(parts, "Флоу: "+item.flow+".")
	}
	if item.step != "" && item.step != "unknown" {
		parts = append(parts, "Шаг: "+item.step+".")
	}
	parts = append(parts, "Доверие цепочки: "+confidence+".")
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
			actions = append(actions, fmt.Sprintf("Проверьте %s.%s: какие поля, кеши, listeners или callbacks сохраняют %s.", holderClass, holderMethod, className))
		} else {
			actions = append(actions, fmt.Sprintf("Проверьте поля, кеши, listeners и callbacks внутри %s.", holderClass))
		}
	}
	switch objectKind {
	case "экран / Activity", "Fragment", "Context":
		actions = append(actions, "Проверьте lifecycle: очистку ссылок в onDestroy/onDestroyView и отсутствие Activity/Context в singleton, static, DI singleton и долгих задачах.")
	case "View / binding":
		actions = append(actions, "Проверьте binding/View ссылки: сброс в onDestroyView, adapter/listener cleanup и отсутствие View в фоновых callbacks.")
	case "ресурс":
		actions = append(actions, "Проверьте закрытие Cursor/Stream/Closeable и отмену работы при уходе экрана.")
	case "системный объект":
		actions = append(actions, "Системный объект считайте симптомом: ищите пользовательский holder, который дольше жизненного цикла держит ссылку на него.")
	default:
		actions = append(actions, "Проверьте коллекции, кеши, coroutine Job, Flow subscription и observer/listener, которые живут дольше экрана.")
	}
	if item.maxAgeMs >= 30_000 {
		actions = append(actions, "Возраст удержания больше 30 секунд: проверьте долгие coroutine/executor задачи и отмену при закрытии флоу.")
	}
	if item.count >= 3 {
		actions = append(actions, "Удержание повторяется: ищите накопление в списках, кешах, подписках или повторной регистрации listener без снятия.")
	}
	return uniqueStrings(actions)
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
		score += int(minUint64(leak.RetainedSizeKB, 512*1024) / 1024)
		score += len(leak.ReferencePath)
		if score > bestScore {
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
	parts := []string{fmt.Sprintf("Удержан %s %s, цепочка подтверждена heap dump.", objectKind, className)}
	if heap.GCRoot != "" {
		parts = append(parts, "GC root: "+heap.GCRoot+".")
	}
	if heap.Holder != "" {
		parts = append(parts, "Пользовательский держатель: "+heap.Holder+".")
	} else if holder != "" && holder != "unknown" && holder != "не определен" {
		parts = append(parts, "Держатель из runtime-контекста: "+holder+".")
	}
	if heap.HolderField != "" {
		parts = append(parts, "Поле/ссылка: "+heap.HolderField+".")
	}
	if heap.RetainedSizeKB > 0 {
		parts = append(parts, "Retained size: "+formatDataSize(heap.RetainedSizeKB)+".")
	}
	if heap.RetainedObjectCount > 0 {
		parts = append(parts, fmt.Sprintf("Доминатор удерживает %d объект(ов).", heap.RetainedObjectCount))
	}
	return strings.Join(parts, " ")
}

func retainedHeapChainActions(heap HeapLeakEvidence, fallback []string) []string {
	actions := make([]string, 0, len(fallback)+2)
	if heap.HolderField != "" {
		actions = append(actions, "Начните с поля "+heap.HolderField+": очистите ссылку на lifecycle boundary или перенесите владельца в более короткий scope.")
	}
	if heap.GCRoot != "" {
		actions = append(actions, "Проверьте, почему цепочка живет от GC root "+heap.GCRoot+": static/singleton, thread local, активный поток, JNI или системный callback.")
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
