package analyze

import (
	"fmt"
	"sort"
	"strings"
)

func betterHeapLeak(candidate, current HeapLeakEvidence) bool {
	if current.ClassName == "" {
		return true
	}
	if heapSizeDominates(candidate.RetainedSizeKB, current.RetainedSizeKB) {
		return true
	}
	if heapSizeDominates(current.RetainedSizeKB, candidate.RetainedSizeKB) {
		return false
	}
	candidateActionability := heapLeakActionabilityScore(candidate)
	currentActionability := heapLeakActionabilityScore(current)
	if candidateActionability != currentActionability {
		return candidateActionability > currentActionability
	}
	if candidate.RetainedSizeKB == current.RetainedSizeKB {
		if len(candidate.ReferencePath) == len(current.ReferencePath) {
			return candidate.Holder < current.Holder
		}
		return len(candidate.ReferencePath) < len(current.ReferencePath)
	}
	return candidate.RetainedSizeKB > current.RetainedSizeKB
}

func heapSizeDominates(left, right uint64) bool {
	if left == 0 || left <= right {
		return false
	}
	if right == 0 {
		return true
	}
	return left-right >= 4*1024 && left/right >= 2
}

func heapLeakActionabilityScore(leak HeapLeakEvidence) int {
	score := 0
	if isLikelyAppClass(leak.Holder) {
		score += 8
	}
	if leak.HolderField != "" {
		score += 5
		if isLikelyAppClass(leak.HolderField) {
			score += 2
		}
	}
	switch leak.GCRootCategory {
	case "class/static":
		score += 5
	case "thread":
		score += 4
	case "jni", "monitor":
		score += 2
	}
	if leak.LeakPattern != "" && leak.LeakPattern != "Сильная цепочка от корня GC удерживает объект" {
		score += 4
	}
	if len(leak.ReferenceMatchers) > 0 {
		score += 3
	}
	if heapPathContainsAppClass(leak.ReferencePath) {
		score += 3
	}
	if len(leak.AlternativePaths) > 0 {
		score += 1
	}
	if len(leak.ReferencePath) > 0 && len(leak.ReferencePath) <= 8 {
		score += 1
	}
	return score
}

func heapPathContainsAppClass(path []HeapPathElement) bool {
	for _, step := range path {
		if isLikelyAppClass(strings.TrimPrefix(step.ClassName, "GC root: ")) {
			return true
		}
	}
	return false
}

func retainedClassSample(classes map[string]uint64) []string {
	type row struct {
		name  string
		count uint64
	}
	rows := make([]row, 0, len(classes))
	for name, count := range classes {
		rows = append(rows, row{name: name, count: count})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].count == rows[j].count {
			return rows[i].name < rows[j].name
		}
		return rows[i].count > rows[j].count
	})
	if len(rows) > maxRetainedTreeSample {
		rows = rows[:maxRetainedTreeSample]
	}
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, fmt.Sprintf("%s × %d", row.name, row.count))
	}
	return out
}

func heapHolder(path []HeapPathElement, targetClass string) string {
	holder := ""
	for _, step := range path {
		className := strings.TrimPrefix(step.ClassName, "GC root: ")
		if className == targetClass || strings.HasPrefix(step.ClassName, "GC root: ") {
			continue
		}
		if isLikelyAppClass(className) {
			holder = className
		}
	}
	return holder
}

func heapHolderField(path []HeapPathElement, targetClass string) string {
	for i := len(path) - 1; i >= 0; i-- {
		step := path[i]
		if step.ClassName != targetClass || i == 0 {
			continue
		}
		prev := path[i-1]
		if prev.ClassName == "" || strings.HasPrefix(prev.ClassName, "GC root: ") || step.FieldName == "" {
			return ""
		}
		return prev.ClassName + "." + step.FieldName
	}
	return ""
}

func heapRootLabel(path []HeapPathElement) string {
	if len(path) == 0 {
		return ""
	}
	if strings.HasPrefix(path[0].ClassName, "GC root: ") {
		return strings.TrimPrefix(path[0].ClassName, "GC root: ")
	}
	return path[0].Kind
}

func heapRootCategory(root string) string {
	lower := strings.ToLower(strings.TrimSpace(root))
	switch {
	case lower == "":
		return ""
	case strings.Contains(lower, "sticky class"):
		return "class/static"
	case strings.Contains(lower, "jni"):
		return "jni"
	case strings.Contains(lower, "thread") || strings.Contains(lower, "java frame") || strings.Contains(lower, "native stack"):
		return "thread"
	case strings.Contains(lower, "monitor"):
		return "monitor"
	case strings.Contains(lower, "reference") || strings.Contains(lower, "finalizing"):
		return "reference"
	case strings.Contains(lower, "vm") || strings.Contains(lower, "debugger"):
		return "vm/internal"
	default:
		return "unknown"
	}
}

func ignoredReferenceField(nodeClass, fieldOwner, fieldName string) bool {
	lowerField := strings.ToLower(strings.TrimSpace(fieldName))
	if lowerField != "referent" {
		return false
	}
	for _, owner := range []string{nodeClass, fieldOwner} {
		lowerOwner := strings.ToLower(strings.TrimSpace(owner))
		if lowerOwner == "java.lang.ref.reference" ||
			strings.HasPrefix(lowerOwner, "java.lang.ref.weakreference") ||
			strings.HasPrefix(lowerOwner, "java.lang.ref.softreference") ||
			strings.HasPrefix(lowerOwner, "java.lang.ref.phantomreference") {
			return true
		}
	}
	return false
}

func heapEvidenceConfidence(exact bool, matchers []string) string {
	parts := []string{"высокое: путь найден в HPROF по сильным ссылкам"}
	if !exact {
		parts[0] = "среднее+: путь найден в HPROF по сильным ссылкам, удержанный размер ограничен безопасным лимитом"
	}
	if len(matchers) > 0 {
		parts = append(parts, "совпавшие правила ссылок: "+strings.Join(matchers, ", "))
	}
	return strings.Join(parts, "; ")
}

func heapReferenceMatchers(path []HeapPathElement) []string {
	var out []string
	for _, step := range path {
		className := strings.ToLower(strings.TrimPrefix(step.ClassName, "GC root: "))
		fieldName := strings.ToLower(step.FieldName)
		if strings.Contains(className, "inputmethodmanager") {
			out = append(out, "android.input_method_manager")
		}
		if strings.Contains(className, "viewmodelstore") {
			out = append(out, "androidx.viewmodel_store")
		}
		if strings.Contains(className, "livedata") {
			out = append(out, "androidx.livedata_observer")
		}
		if strings.Contains(className, "recyclerview") {
			out = append(out, "androidx.recyclerview")
		}
		if strings.Contains(className, "compose") {
			out = append(out, "androidx.compose")
		}
		if strings.Contains(className, "kotlinx.coroutines") || strings.Contains(className, "job") ||
			strings.Contains(fieldName, "continuation") {
			out = append(out, "kotlin.coroutines")
		}
		if strings.Contains(className, "textline") {
			out = append(out, "android.text_line_pool")
		}
		if strings.Contains(className, "choreographer") || strings.Contains(className, "handler") || strings.Contains(className, "looper") {
			out = append(out, "android.main_thread_queue")
		}
		if strings.Contains(fieldName, "listener") || strings.Contains(fieldName, "callback") || strings.Contains(fieldName, "observer") {
			out = append(out, "listener_or_callback")
		}
		if strings.Contains(fieldName, "adapter") {
			out = append(out, "adapter_reference")
		}
		if strings.Contains(fieldName, "binding") {
			out = append(out, "view_binding_reference")
		}
		if strings.Contains(fieldName, "mcontext") || strings.HasSuffix(fieldName, ".context") {
			out = append(out, "context_reference")
		}
	}
	return uniqueStrings(out)
}

func heapLeakPattern(className, holder, holderField, rootCategory string, path []HeapPathElement) string {
	lowerClass := strings.ToLower(className)
	lowerHolder := strings.ToLower(holder)
	lowerField := strings.ToLower(holderField)
	switch {
	case strings.Contains(lowerClass, "activity") && strings.Contains(lowerField, "mcontext"):
		return "View/Context цепочка удерживает Activity"
	case strings.Contains(lowerClass, "activity") && rootCategory == "class/static":
		return "Activity удерживается цепочкой статического поля или одиночки"
	case strings.Contains(lowerClass, "fragment") && rootCategory == "class/static":
		return "Fragment удерживается цепочкой статического поля или одиночки"
	case strings.Contains(lowerClass, "viewmodel"):
		return "ViewModel живет после onCleared"
	case strings.Contains(lowerClass, "service"):
		return "Service удерживается после onDestroy"
	case strings.Contains(lowerClass, "dialog"):
		return "Dialog/window цепочка живет после dismiss или onStop"
	case strings.Contains(lowerClass, "viewholder"):
		return "RecyclerView ViewHolder удерживает view/binding после recycle"
	case strings.Contains(lowerClass, "adapter"):
		return "RecyclerView adapter удерживает экран после detach"
	case strings.Contains(lowerClass, "view") || strings.Contains(lowerClass, "binding"):
		return "View/binding живет после onDestroyView или detach"
	case rootCategory == "thread":
		return "Активный поток или очередь удерживает объект жизненного цикла"
	case strings.Contains(lowerHolder, "listener") || strings.Contains(lowerField, "listener") ||
		strings.Contains(lowerHolder, "callback") || strings.Contains(lowerField, "callback"):
		return "Слушатель или обратный вызов удерживает объект после очистки жизненного цикла"
	case pathContainsClass(path, "kotlinx.coroutines") || pathContainsClass(path, "java.util.concurrent"):
		return "Корутина или задача исполнителя удерживает объект"
	default:
		return "Сильная цепочка от корня GC удерживает объект"
	}
}

func pathContainsClass(path []HeapPathElement, needle string) bool {
	needle = strings.ToLower(needle)
	for _, step := range path {
		if strings.Contains(strings.ToLower(step.ClassName), needle) {
			return true
		}
	}
	return false
}

func heapChainFingerprint(className, holder, holderField, rootCategory string, path []HeapPathElement) string {
	parts := []string{
		normalizeLeakToken(className),
		normalizeLeakToken(holder),
		normalizeLeakToken(holderField),
		normalizeLeakToken(rootCategory),
	}
	for _, step := range path {
		classToken := normalizeLeakToken(strings.TrimPrefix(step.ClassName, "GC root: "))
		fieldToken := normalizeLeakToken(step.FieldName)
		kindToken := normalizeLeakToken(step.Kind)
		if classToken == "" && fieldToken == "" && kindToken == "" {
			continue
		}
		parts = append(parts, classToken+"."+fieldToken+"."+kindToken)
	}
	return strings.Join(parts, "|")
}

func pathFingerprint(path []HeapPathElement) string {
	if len(path) == 0 {
		return ""
	}
	return heapChainFingerprint("", "", "", heapRootCategory(heapRootLabel(path)), path)
}

func normalizeLeakToken(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.TrimPrefix(value, "gc root: ")
	value = strings.ReplaceAll(value, "/", ".")
	value = strings.ReplaceAll(value, "$", ".")
	fields := strings.FieldsFunc(value, func(r rune) bool {
		switch r {
		case '\x00', '\x01', '\t', '\n', '\r':
			return true
		default:
			return false
		}
	})
	return strings.Join(fields, " ")
}

func emptyFieldName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "<field>"
	}
	return name
}

func primitiveArrayName(typ byte) string {
	switch typ {
	case hprofTypeBoolean:
		return "boolean[]"
	case hprofTypeChar:
		return "char[]"
	case hprofTypeFloat:
		return "float[]"
	case hprofTypeDouble:
		return "double[]"
	case hprofTypeByte:
		return "byte[]"
	case hprofTypeShort:
		return "short[]"
	case hprofTypeInt:
		return "int[]"
	case hprofTypeLong:
		return "long[]"
	default:
		return "primitive[]"
	}
}
