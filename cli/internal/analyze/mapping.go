package analyze

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strings"
)

type NameMapping struct {
	classes map[string]string
	keys    []string
}

func LoadNameMapping(path string) (*NameMapping, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	mapping := &NameMapping{classes: map[string]string{}}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1024), 4*1024*1024)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		raw := scanner.Text()
		if raw == "" || raw[0] == ' ' || raw[0] == '\t' {
			continue
		}
		line := strings.TrimSpace(raw)
		if !strings.HasSuffix(line, ":") || !strings.Contains(line, " -> ") {
			continue
		}
		line = strings.TrimSuffix(line, ":")
		parts := strings.Split(line, " -> ")
		if len(parts) != 2 {
			return nil, fmt.Errorf("%s:%d: некорректная строка mapping", path, lineNumber)
		}
		original := normalizeMappingClass(parts[0])
		obfuscated := normalizeMappingClass(parts[1])
		if original == "" || obfuscated == "" {
			continue
		}
		mapping.classes[obfuscated] = original
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read mapping %s: %w", path, err)
	}
	if len(mapping.classes) == 0 {
		return nil, fmt.Errorf("%s: mapping не содержит class mapping строк вида 'original.Name -> a.b:'", path)
	}
	for key := range mapping.classes {
		mapping.keys = append(mapping.keys, key)
	}
	sort.Slice(mapping.keys, func(i, j int) bool {
		if len(mapping.keys[i]) == len(mapping.keys[j]) {
			return mapping.keys[i] < mapping.keys[j]
		}
		return len(mapping.keys[i]) > len(mapping.keys[j])
	})
	return mapping, nil
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
	if original, ok := m.classes[normalized]; ok {
		return original + suffix
	}
	for _, key := range m.keys {
		if strings.HasPrefix(normalized, key) && mappingBoundary(normalized, len(key)) {
			return m.classes[key] + normalized[len(key):] + suffix
		}
	}
	return value + suffix
}

func mappingBoundary(value string, index int) bool {
	if index >= len(value) {
		return true
	}
	next := value[index]
	return next == '.' || next == '$' || next == '#' || next == ' ' || next == '\t' || next == '(' || next == '[' || next == ':'
}

func DeobfuscateClassGraph(graph *ClassGraph, mapping *NameMapping) *ClassGraph {
	if graph == nil || mapping == nil {
		return graph
	}
	out := &ClassGraph{
		Format:  graph.Format,
		Classes: map[string]ClassGraphClass{},
		Edges:   make([]ClassGraphEdge, 0, len(graph.Edges)),
	}
	for key, class := range graph.Classes {
		name := mapping.Deobfuscate(firstNonEmpty(class.Name, key))
		out.Classes[name] = ClassGraphClass{Name: name}
	}
	for _, edge := range graph.Edges {
		edge.From = mapping.Deobfuscate(edge.From)
		edge.To = mapping.Deobfuscate(edge.To)
		out.Edges = append(out.Edges, edge)
		if _, ok := out.Classes[edge.From]; !ok && edge.From != "" {
			out.Classes[edge.From] = ClassGraphClass{Name: edge.From}
		}
		if _, ok := out.Classes[edge.To]; !ok && edge.To != "" {
			out.Classes[edge.To] = ClassGraphClass{Name: edge.To}
		}
	}
	return out
}

func DeobfuscateHeapEvidence(heap *HeapEvidence, mapping *NameMapping) *HeapEvidence {
	if heap == nil || mapping == nil {
		return heap
	}
	out := &HeapEvidence{
		Sources:  append([]string{}, heap.Sources...),
		Leaks:    make([]HeapLeakEvidence, 0, len(heap.Leaks)),
		Warnings: append([]string{}, heap.Warnings...),
	}
	for _, leak := range heap.Leaks {
		leak.ClassName = mapping.Deobfuscate(leak.ClassName)
		leak.Holder = mapping.Deobfuscate(leak.Holder)
		leak.HolderField = mapping.Deobfuscate(leak.HolderField)
		leak.GCRoot = mapping.Deobfuscate(leak.GCRoot)
		leak.ReferencePath = deobfuscateHeapPath(leak.ReferencePath, mapping)
		for i := range leak.AlternativePaths {
			leak.AlternativePaths[i] = deobfuscateHeapPath(leak.AlternativePaths[i], mapping)
		}
		for i := range leak.DominatorTree {
			leak.DominatorTree[i] = mapping.Deobfuscate(leak.DominatorTree[i])
		}
		for i := range leak.ReferenceMatchers {
			leak.ReferenceMatchers[i] = strings.ToLower(mapping.Deobfuscate(leak.ReferenceMatchers[i]))
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
	for i, item := range path {
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
