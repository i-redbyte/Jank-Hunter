package analyze

import "sort"

type ClassGraphIndex struct {
	Outgoing map[string][]ClassGraphEdge
	Incoming map[string][]ClassGraphEdge
}

func NewClassGraphIndex(edges []ClassGraphEdge) *ClassGraphIndex {
	index := &ClassGraphIndex{
		Outgoing: map[string][]ClassGraphEdge{},
		Incoming: map[string][]ClassGraphEdge{},
	}
	merged := map[string]ClassGraphEdge{}
	for _, edge := range edges {
		from := normalizeClassName(edge.From)
		to := normalizeClassName(edge.To)
		if from == "" || to == "" || from == to {
			continue
		}
		edge.From = from
		edge.To = to
		if edge.Count == 0 {
			edge.Count = 1
		}
		key := edgeKey(edge)
		existing := merged[key]
		if existing.From == "" {
			existing = edge
		} else {
			existing.Count += edge.Count
		}
		merged[key] = existing
	}
	for _, edge := range merged {
		index.Outgoing[edge.From] = append(index.Outgoing[edge.From], edge)
		index.Incoming[edge.To] = append(index.Incoming[edge.To], edge)
	}
	return index
}

func (i *ClassGraphIndex) RelevantEdges(selected map[string]struct{}, runtimeTargets map[string]struct{}) []ClassGraphEdge {
	if i == nil {
		return nil
	}
	edgesByKey := map[string]ClassGraphEdge{}
	for node := range selected {
		i.addEdges(edgesByKey, i.Outgoing[node])
		i.addEdges(edgesByKey, i.Incoming[node])
	}
	for node := range runtimeTargets {
		i.addEdges(edgesByKey, i.Incoming[node])
	}
	out := make([]ClassGraphEdge, 0, len(edgesByKey))
	for _, edge := range edgesByKey {
		out = append(out, edge)
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].From == out[b].From {
			if out[a].To == out[b].To {
				return out[a].Count > out[b].Count
			}
			return out[a].To < out[b].To
		}
		return out[a].From < out[b].From
	})
	return out
}

func (i *ClassGraphIndex) addEdges(edgesByKey map[string]ClassGraphEdge, edges []ClassGraphEdge) {
	for _, edge := range edges {
		key := edgeKey(edge)
		if _, ok := edgesByKey[key]; ok {
			continue
		}
		edgesByKey[key] = edge
	}
}

func edgeKey(edge ClassGraphEdge) string {
	return edge.From + "\x00" + edge.To + "\x00" + edge.CallerMethod + "\x00" + edge.CalleeMethod
}
