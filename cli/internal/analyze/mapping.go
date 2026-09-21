package analyze

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"

	"github.com/i-redbyte/jank-hunter/cli/internal/retrace"
)

const (
	nameMappingMaxFileBytes = 1 << 30
	nameMappingMaxLineBytes = 4 << 20
)

type NameMapping struct {
	validation   *mappingValidation
	digest       string
	fieldResults map[fieldRetraceKey]retrace.FieldResult
	path         string
	classes      map[string]string
	fields       map[mappingFieldKey]string
}

// Shared by immutable mapping views, including heap views; never copy sync.Once itself.
type mappingValidation struct {
	once sync.Once
	err  error
}

func (m *NameMapping) validateSemantics() error {
	if m.validation == nil {
		return nil // Internal in-memory mappings have no external mapping file.
	}
	m.validation.once.Do(func() {
		backend, err := retrace.Discover()
		if err == nil {
			_, err = backend.Run(context.Background(), m.path, m.digest, nil)
		}
		m.validation.err = err
	})
	return m.validation.err
}

type mappingFieldKey struct {
	owner string
	name  string
}

type parsedMappingField struct {
	original   string
	obfuscated string
}

func LoadNameMapping(path string) (*NameMapping, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, nil
	}
	hash := sha256.New()
	input, err := openBoundedTextInput(path, "R8 mapping", nameMappingMaxFileBytes, 1024, nameMappingMaxLineBytes, hash)
	if err != nil {
		return nil, err
	}
	defer input.Close()

	mapping := &NameMapping{path: path, classes: map[string]string{}, validation: &mappingValidation{}}
	scanner := input.Scanner
	lineNumber := 0
	currentObfuscatedClass := ""
	pendingFields := make([]parsedMappingField, 0, 8)
	hasCaptureField := false
	flushFields := func() {
		if hasCaptureField {
			for _, field := range pendingFields {
				mapping.addField(currentObfuscatedClass, field.obfuscated, field.original)
			}
		}
		pendingFields = pendingFields[:0]
		hasCaptureField = false
	}
	for scanner.Scan() {
		lineNumber++
		raw := scanner.Text()
		if raw == "" {
			continue
		}
		if raw[0] == ' ' || raw[0] == '\t' {
			original, obfuscated, ok := parseFieldMapping(raw)
			if ok && currentObfuscatedClass != "" {
				pendingFields = append(pendingFields, parsedMappingField{original: original, obfuscated: obfuscated})
				hasCaptureField = hasCaptureField || isLambdaCaptureMappingField(original)
			}
			continue
		}
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "#") {
			continue
		}
		flushFields()
		currentObfuscatedClass = ""
		if !strings.HasSuffix(line, ":") || !strings.Contains(line, " -> ") {
			continue
		}
		line = strings.TrimSuffix(line, ":")
		originalValue, obfuscatedValue, ok := strings.Cut(line, " -> ")
		if !ok || strings.Contains(obfuscatedValue, " -> ") {
			return nil, fmt.Errorf("%s:%d: некорректная строка mapping", path, lineNumber)
		}
		original := normalizeMappingClass(originalValue)
		obfuscated := normalizeMappingClass(obfuscatedValue)
		if original == "" || obfuscated == "" {
			continue
		}
		mapping.classes[obfuscated] = original
		currentObfuscatedClass = obfuscated
	}
	flushFields()
	if err := input.Err(); err != nil {
		return nil, fmt.Errorf("read mapping %s: %w", path, err)
	}
	if len(mapping.classes) == 0 {
		return nil, fmt.Errorf("%s: mapping не содержит class mapping строк вида 'original.Name -> a.b:'", path)
	}
	mapping.digest = hex.EncodeToString(hash.Sum(nil))
	return mapping, nil
}

func isLambdaCaptureMappingField(name string) bool {
	if name == "this$0" || name == "$receiver" || strings.HasPrefix(name, "arg$") ||
		strings.HasPrefix(name, "f$") || strings.HasPrefix(name, "$") {
		return true
	}
	if len(name) < 3 || name[1] != '$' || strings.IndexByte("LIJFDZBCS", name[0]) < 0 {
		return false
	}
	for index := 2; index < len(name); index++ {
		if name[index] < '0' || name[index] > '9' {
			return false
		}
	}
	return true
}

func parseFieldMapping(raw string) (original string, obfuscated string, ok bool) {
	declaration, obfuscated, found := strings.Cut(strings.TrimSpace(raw), " -> ")
	if !found || strings.Contains(obfuscated, " -> ") || strings.ContainsRune(declaration, '(') {
		return "", "", false
	}
	separator := strings.LastIndexAny(declaration, " \t")
	if separator < 0 {
		return "", "", false
	}
	original = strings.TrimSpace(declaration[separator+1:])
	obfuscated = strings.TrimSpace(obfuscated)
	if original == "" || obfuscated == "" || strings.ContainsAny(obfuscated, " \t") {
		return "", "", false
	}
	return original, obfuscated, true
}

func (m *NameMapping) addField(owner, obfuscated, original string) {
	if m.fields == nil {
		m.fields = make(map[mappingFieldKey]string)
	}
	key := mappingFieldKey{owner: owner, name: obfuscated}
	existing, present := m.fields[key]
	if !present {
		m.fields[key] = original
		return
	}
	if existing != original {
		// R8 may reuse a short field name for different descriptors. HPROF does not retain
		// enough descriptor information in HeapPathElement, so guessing here would create
		// false exact correlations.
		m.fields[key] = ""
	}
}

// ExpandMappedClassAliases keeps original targets for unminified inputs and adds their
// obfuscated names so a raw release HPROF can be searched before its paths are deobfuscated.
func ExpandMappedClassAliases(classNames []string, mapping *NameMapping) []string {
	if len(classNames) == 0 {
		return nil
	}
	aliases := make([]string, 0, len(classNames))
	seen := make(map[string]struct{}, len(classNames))
	originalTargets := make(map[string]struct{}, len(classNames))
	for _, className := range classNames {
		className = normalizeMappingClass(className)
		if className == "" {
			continue
		}
		originalTargets[className] = struct{}{}
		if _, present := seen[className]; !present {
			seen[className] = struct{}{}
			aliases = append(aliases, className)
		}
	}
	if mapping == nil {
		return aliases
	}
	for obfuscated, original := range mapping.classes {
		if _, wanted := originalTargets[original]; !wanted {
			continue
		}
		if _, present := seen[obfuscated]; present {
			continue
		}
		seen[obfuscated] = struct{}{}
		aliases = append(aliases, obfuscated)
	}
	return aliases
}

func (m *NameMapping) Deobfuscate(value string) string {
	if m == nil || len(m.classes) == 0 {
		return value
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return value
	}
	if strings.HasPrefix(value, "GC root: ") {
		return "GC root: " + m.Deobfuscate(strings.TrimPrefix(value, "GC root: "))
	}
	suffix := ""
	for strings.HasSuffix(value, "[]") {
		value = strings.TrimSuffix(value, "[]")
		suffix += "[]"
	}
	normalized := normalizeMappingClass(value)
	obfuscated, original, found := m.mappedClassPrefix(normalized)
	if found {
		return original + normalized[len(obfuscated):] + suffix
	}
	return value + suffix
}

func (m *NameMapping) mappedClassPrefix(value string) (obfuscated string, original string, found bool) {
	if original, found = m.classes[value]; found {
		return value, original, true
	}
	for index := len(value) - 1; index > 0; index-- {
		if !mappingBoundary(value, index) {
			continue
		}
		if original, found = m.classes[value[:index]]; found {
			return value[:index], original, true
		}
	}
	return "", "", false
}

func (m *NameMapping) deobfuscateField(owner, field string) string {
	if m == nil || len(m.fields) == 0 {
		return field
	}
	prefix := ""
	name := strings.TrimSpace(field)
	if strings.HasPrefix(name, "static ") {
		prefix = "static "
		name = strings.TrimSpace(strings.TrimPrefix(name, prefix))
	}
	original, found := m.fields[mappingFieldKey{owner: normalizeMappingClass(owner), name: name}]
	if !found || original == "" {
		return field
	}
	return prefix + original
}

func (m *NameMapping) deobfuscateQualifiedField(value string) string {
	if m == nil {
		return value
	}
	normalized := normalizeMappingClass(value)
	for index := len(normalized) - 1; index > 0; index-- {
		if normalized[index] != '.' {
			continue
		}
		owner := normalized[:index]
		originalOwner, found := m.classes[owner]
		if !found {
			continue
		}
		field := normalized[index+1:]
		return originalOwner + "." + m.deobfuscateField(owner, field)
	}
	return m.Deobfuscate(value)
}

func mappingBoundary(value string, index int) bool {
	if index >= len(value) {
		return true
	}
	next := value[index]
	return next == '.' || next == '$' || next == '#' || next == ' ' || next == '\t' || next == '(' || next == '[' || next == ':'
}

func DeobfuscateHeapEvidence(heap *HeapEvidence, mapping *NameMapping) *HeapEvidence {
	if heap == nil || mapping == nil {
		return heap
	}
	out := &HeapEvidence{
		DiagnosticsVersion: heap.DiagnosticsVersion,
		Diagnostics:        append([]HeapDiagnostic(nil), heap.Diagnostics...),
		Sources:            append([]string{}, heap.Sources...),
		Leaks:              make([]HeapLeakEvidence, 0, len(heap.Leaks)),
		Warnings:           append([]string{}, heap.Warnings...),
	}
	for _, leak := range heap.Leaks {
		leak.ClassName = mapping.Deobfuscate(leak.ClassName)
		leak.Holder = mapping.Deobfuscate(leak.Holder)
		leak.HolderField = mapping.deobfuscateQualifiedField(leak.HolderField)
		leak.ReferencePath = deobfuscateHeapPath(leak.ReferencePath, mapping)
		if mapping.fieldResults != nil && len(leak.ReferencePath) > 0 {
			leak.HolderField = heapHolderField(leak.ReferencePath, leak.ClassName)
		}
		if len(leak.AlternativePaths) > 0 {
			alternativePaths := make([][]HeapPathElement, len(leak.AlternativePaths))
			for i, path := range leak.AlternativePaths {
				alternativePaths[i] = deobfuscateHeapPath(path, mapping)
			}
			leak.AlternativePaths = alternativePaths
		}
		if leak.LeakPattern == "" || leak.LeakPattern == defaultHeapLeakPattern {
			leak.LeakPattern = heapLeakPattern(
				leak.ClassName,
				leak.Holder,
				leak.HolderField,
				leak.GCRootCategory,
				leak.ReferencePath,
			)
		}
		leak.DominatorTree = append([]string(nil), leak.DominatorTree...)
		for i := range leak.DominatorTree {
			leak.DominatorTree[i] = mapping.Deobfuscate(leak.DominatorTree[i])
		}
		var derivedMatchers []string
		if len(leak.ReferencePath) > 0 {
			derivedMatchers = heapReferenceMatchers(leak.ReferencePath)
		}
		if len(leak.ReferenceMatchers)+len(derivedMatchers) > 0 {
			referenceMatchers := make(
				[]string,
				len(leak.ReferenceMatchers),
				len(leak.ReferenceMatchers)+len(derivedMatchers),
			)
			copy(referenceMatchers, leak.ReferenceMatchers)
			leak.ReferenceMatchers = referenceMatchers
			for i := range leak.ReferenceMatchers {
				leak.ReferenceMatchers[i] = strings.ToLower(mapping.Deobfuscate(leak.ReferenceMatchers[i]))
			}
			leak.ReferenceMatchers = append(leak.ReferenceMatchers, derivedMatchers...)
		}
		leak.ChainFingerprint = ""
		normalizeHeapLeak(&leak)
		out.Leaks = append(out.Leaks, leak)
	}
	return out
}

func deobfuscateHeapPath(path []HeapPathElement, mapping *NameMapping) []HeapPathElement {
	if len(path) == 0 {
		return nil
	}
	out := make([]HeapPathElement, len(path))
	fieldOwner := ""
	for i, item := range path {
		if item.Kind == "truncated" || item.Kind == "gc_root" || strings.HasPrefix(item.ClassName, "GC root: ") {
			fieldOwner = ""
			out[i] = item
			continue
		}
		if mapping.fieldResults != nil {
			mapping.retraceHeapField(&item)
		} else {
			owner := item.DeclaringClass
			if owner == "" {
				owner = fieldOwner
			}
			item.FieldName = mapping.deobfuscateField(owner, item.FieldName)
		}
		if item.FieldRetrace == nil || item.FieldRetrace.Status != "resolved" {
			item.DeclaringClass = mapping.Deobfuscate(item.DeclaringClass)
		}
		fieldOwner = strings.TrimPrefix(item.ClassName, "GC root: ")
		item.ClassName = mapping.Deobfuscate(item.ClassName)
		out[i] = item
	}
	return out
}

func normalizeMappingClass(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "class ")
	value = strings.TrimPrefix(value, "interface ")
	if strings.HasPrefix(value, "L") && strings.HasSuffix(value, ";") {
		value = strings.TrimPrefix(strings.TrimSuffix(value, ";"), "L")
	}
	value = strings.ReplaceAll(value, "/", ".")
	return value
}
