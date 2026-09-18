package analyze

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/retrace"
)

type FieldRetraceEvidence struct {
	Status       string                   `json:"status"`
	RuntimeOwner string                   `json:"runtime_owner,omitempty"`
	RuntimeName  string                   `json:"runtime_name"`
	Alternatives []retrace.FieldCandidate `json:"alternatives,omitempty"`
}

type fieldRetraceKey struct{ owner, name, descriptor string }

func (m *NameMapping) prepareHeapRetrace(ctx context.Context, heap *HeapEvidence) (*NameMapping, error) {
	if m == nil || heap == nil {
		return m, nil
	}
	keys := make(map[fieldRetraceKey]struct{})
	visit := func(path []HeapPathElement) error {
		for _, item := range path {
			if item.Kind != "field" && item.Kind != "static" {
				continue
			}
			if item.DeclaringClass != "" && item.FieldName != "" {
				key := fieldRetraceKey{item.DeclaringClass, strings.TrimPrefix(item.FieldName, "static "), item.DeclaredType}
				if _, exists := keys[key]; !exists && len(keys) >= retrace.MaxRequests {
					return fmt.Errorf("heap field Retrace exceeds %d distinct fields", retrace.MaxRequests)
				}
				keys[key] = struct{}{}
			}
		}
		return nil
	}
	for _, leak := range heap.Leaks {
		if err := visit(leak.ReferencePath); err != nil {
			return nil, err
		}
		for _, path := range leak.AlternativePaths {
			if err := visit(path); err != nil {
				return nil, err
			}
		}
	}
	prepared := *m
	prepared.fieldResults = make(map[fieldRetraceKey]retrace.FieldResult, len(keys))
	if len(keys) == 0 {
		return &prepared, nil
	}
	if m.path == "" {
		return nil, fmt.Errorf("heap field Retrace requires a loaded mapping file")
	}
	ordered := make([]fieldRetraceKey, 0, len(keys))
	for key := range keys {
		ordered = append(ordered, key)
	}
	sort.Slice(ordered, func(i, j int) bool {
		a, b := ordered[i], ordered[j]
		if a.owner != b.owner {
			return a.owner < b.owner
		}
		if a.name != b.name {
			return a.name < b.name
		}
		return a.descriptor < b.descriptor
	})
	requests := make([]retrace.Request, len(ordered))
	for index, key := range ordered {
		requests[index] = retrace.FieldRequest{Owner: key.owner, Name: key.name, Descriptor: key.descriptor}
	}
	backend, err := retrace.Discover()
	if err != nil {
		return nil, err
	}
	results, err := backend.Run(ctx, m.path, m.digest, requests)
	if err != nil {
		return nil, err
	}
	for index, result := range results {
		prepared.fieldResults[ordered[index]] = result.(retrace.FieldResult)
	}
	return &prepared, nil
}

func (m *NameMapping) retraceHeapField(item *HeapPathElement) {
	if item.Kind != "field" && item.Kind != "static" {
		return
	}
	runtimeName := strings.TrimPrefix(item.FieldName, "static ")
	evidence := &FieldRetraceEvidence{Status: "unknown", RuntimeOwner: item.DeclaringClass, RuntimeName: runtimeName}
	item.FieldRetrace = evidence
	if item.DeclaringClass == "" {
		evidence.Status = "missing_declaring_owner"
		return
	}
	result, found := m.fieldResults[fieldRetraceKey{item.DeclaringClass, runtimeName, item.DeclaredType}]
	if !found {
		return
	}
	evidence.Alternatives = result.Alternatives
	if result.Ambiguous || len(result.Alternatives) > 1 {
		evidence.Status = "ambiguous"
		return
	}
	if len(result.Alternatives) != 1 || !result.Alternatives[0].Known {
		return
	}
	candidate := result.Alternatives[0]
	evidence.Status = "resolved"
	prefix := ""
	if strings.HasPrefix(item.FieldName, "static ") {
		prefix = "static "
	}
	item.FieldName = prefix + candidate.Name
	item.DeclaringClass = candidate.Owner
	item.DeclaredType = candidate.Descriptor
}

// FieldLabel keeps unresolved names visibly uncertain in every heap graph and path view.
func (item HeapPathElement) FieldLabel() string {
	if item.FieldRetrace == nil || item.FieldRetrace.Status == "resolved" {
		return item.FieldName
	}
	switch item.FieldRetrace.Status {
	case "ambiguous":
		return item.FieldName + " (mapping неоднозначен)"
	case "missing_declaring_owner":
		return item.FieldName + " (объявляющий класс неизвестен)"
	default:
		return item.FieldName + " (исходное имя не восстановлено)"
	}
}
