package analyze

import (
	"math"
	"sort"
)

type ClassGraphIndex struct {
	Outgoing map[string][]ClassGraphEdge
	Incoming map[string][]ClassGraphEdge
}

type MethodGraphIndex struct {
	Outgoing map[string][]MethodGraphEdge
	Incoming map[string][]MethodGraphEdge
}

type MethodGraphEdge struct {
	FromClass  string
	FromMethod string
	ToClass    string
	ToMethod   string
	Count      uint64
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
	index.sort()
	return index
}

func NewMethodGraphIndex(edges []ClassGraphEdge) *MethodGraphIndex {
	index := &MethodGraphIndex{
		Outgoing: map[string][]MethodGraphEdge{},
		Incoming: map[string][]MethodGraphEdge{},
	}
	merged := map[string]MethodGraphEdge{}
	for _, edge := range edges {
		fromClass := normalizeClassName(edge.From)
		toClass := normalizeClassName(edge.To)
		if fromClass == "" || toClass == "" || fromClass == toClass {
			continue
		}
		methodEdge := MethodGraphEdge{
			FromClass:  fromClass,
			FromMethod: normalizeGraphMethodName(edge.CallerMethod),
			ToClass:    toClass,
			ToMethod:   normalizeGraphMethodName(edge.CalleeMethod),
			Count:      edge.Count,
		}
		if methodEdge.Count == 0 {
			methodEdge.Count = 1
		}
		key := methodNodeKey(methodEdge.FromClass, methodEdge.FromMethod) + "\x00" + methodNodeKey(methodEdge.ToClass, methodEdge.ToMethod)
		existing := merged[key]
		if existing.FromClass == "" {
			existing = methodEdge
		} else {
			existing.Count += methodEdge.Count
		}
		merged[key] = existing
	}
	for _, edge := range merged {
		from := methodNodeKey(edge.FromClass, edge.FromMethod)
		to := methodNodeKey(edge.ToClass, edge.ToMethod)
		index.Outgoing[from] = append(index.Outgoing[from], edge)
		index.Incoming[to] = append(index.Incoming[to], edge)
	}
	index.sort()
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

func (i *ClassGraphIndex) ShortestPath(from string, to string, maxDepth int) []ClassGraphEdge {
	if i == nil || maxDepth <= 0 {
		return nil
	}
	from = normalizeClassName(from)
	to = normalizeClassName(to)
	if from == "" || to == "" || from == to {
		return nil
	}
	type pathState struct {
		node  string
		edges []ClassGraphEdge
	}
	seen := map[string]struct{}{from: {}}
	queue := []pathState{{node: from}}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if len(current.edges) >= maxDepth {
			continue
		}
		for _, edge := range i.Outgoing[current.node] {
			nextEdges := append(append([]ClassGraphEdge{}, current.edges...), edge)
			if edge.To == to {
				return nextEdges
			}
			if _, ok := seen[edge.To]; ok {
				continue
			}
			seen[edge.To] = struct{}{}
			queue = append(queue, pathState{node: edge.To, edges: nextEdges})
		}
	}
	return nil
}

func (i *ClassGraphIndex) StronglyConnectedComponents(limit int) []InfluenceCycle {
	if i == nil || limit == 0 {
		return nil
	}
	index := 0
	stack := []string{}
	onStack := map[string]bool{}
	indexes := map[string]int{}
	lowLinks := map[string]int{}
	cycles := []InfluenceCycle{}

	var visit func(string)
	visit = func(node string) {
		indexes[node] = index
		lowLinks[node] = index
		index++
		stack = append(stack, node)
		onStack[node] = true

		for _, edge := range i.Outgoing[node] {
			next := edge.To
			if _, seen := indexes[next]; !seen {
				visit(next)
				if lowLinks[next] < lowLinks[node] {
					lowLinks[node] = lowLinks[next]
				}
			} else if onStack[next] && indexes[next] < lowLinks[node] {
				lowLinks[node] = indexes[next]
			}
		}

		if lowLinks[node] != indexes[node] {
			return
		}
		component := []string{}
		for {
			last := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			onStack[last] = false
			component = append(component, last)
			if last == node {
				break
			}
		}
		if len(component) > 1 {
			sort.Strings(component)
			componentSet := map[string]struct{}{}
			for _, item := range component {
				componentSet[item] = struct{}{}
			}
			var weight uint64
			for _, item := range component {
				for _, edge := range i.Outgoing[item] {
					if _, inside := componentSet[edge.To]; inside {
						weight += edge.Count
					}
				}
			}
			cycles = append(cycles, InfluenceCycle{Nodes: component, Weight: weight})
		}
	}

	for node := range i.Outgoing {
		if _, seen := indexes[node]; !seen {
			visit(node)
		}
	}
	sort.Slice(cycles, func(a, b int) bool {
		if cycles[a].Weight == cycles[b].Weight {
			return stringsKey(cycles[a].Nodes) < stringsKey(cycles[b].Nodes)
		}
		return cycles[a].Weight > cycles[b].Weight
	})
	if limit > 0 && len(cycles) > limit {
		return cycles[:limit]
	}
	return cycles
}

func (i *ClassGraphIndex) HotPaths(scores map[string]float64, runtimeTargets map[string]struct{}, limit int) []InfluencePath {
	if i == nil || limit == 0 {
		return nil
	}
	type candidate struct {
		from   string
		to     string
		weight float64
	}
	candidates := []candidate{}
	for from, edges := range i.Outgoing {
		fromScore := scores[from]
		if fromScore == 0 {
			fromScore = 0.25
		}
		for _, edge := range edges {
			toScore := scores[edge.To]
			_, runtimeTarget := runtimeTargets[edge.To]
			if toScore == 0 && !runtimeTarget {
				continue
			}
			weight := math.Log1p(float64(edge.Count)) * (1 + fromScore*0.2 + toScore*0.8)
			if runtimeTarget {
				weight *= 1.35
			}
			candidates = append(candidates, candidate{from: from, to: edge.To, weight: weight})
		}
	}
	sort.Slice(candidates, func(a, b int) bool {
		if candidates[a].weight == candidates[b].weight {
			if candidates[a].from == candidates[b].from {
				return candidates[a].to < candidates[b].to
			}
			return candidates[a].from < candidates[b].from
		}
		return candidates[a].weight > candidates[b].weight
	})
	seen := map[string]struct{}{}
	out := []InfluencePath{}
	for _, candidate := range candidates {
		pathEdges := i.ShortestPath(candidate.from, candidate.to, 4)
		if len(pathEdges) == 0 {
			pathEdges = []ClassGraphEdge{{From: candidate.from, To: candidate.to}}
		}
		nodes := pathNodes(pathEdges)
		key := stringsKey(nodes)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		_, runtimeTarget := runtimeTargets[candidate.to]
		out = append(out, InfluencePath{
			Nodes:         nodes,
			Weight:        math.Round(candidate.weight*10) / 10,
			RuntimeTarget: runtimeTarget,
			Reason:        hotPathReason(runtimeTarget),
		})
		if limit > 0 && len(out) >= limit {
			break
		}
	}
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

func (i *ClassGraphIndex) sort() {
	for node := range i.Outgoing {
		sort.Slice(i.Outgoing[node], func(a, b int) bool {
			if i.Outgoing[node][a].Count == i.Outgoing[node][b].Count {
				return i.Outgoing[node][a].To < i.Outgoing[node][b].To
			}
			return i.Outgoing[node][a].Count > i.Outgoing[node][b].Count
		})
	}
	for node := range i.Incoming {
		sort.Slice(i.Incoming[node], func(a, b int) bool {
			if i.Incoming[node][a].Count == i.Incoming[node][b].Count {
				return i.Incoming[node][a].From < i.Incoming[node][b].From
			}
			return i.Incoming[node][a].Count > i.Incoming[node][b].Count
		})
	}
}

func (i *MethodGraphIndex) sort() {
	for node := range i.Outgoing {
		sort.Slice(i.Outgoing[node], func(a, b int) bool {
			if i.Outgoing[node][a].Count == i.Outgoing[node][b].Count {
				return methodNodeKey(i.Outgoing[node][a].ToClass, i.Outgoing[node][a].ToMethod) <
					methodNodeKey(i.Outgoing[node][b].ToClass, i.Outgoing[node][b].ToMethod)
			}
			return i.Outgoing[node][a].Count > i.Outgoing[node][b].Count
		})
	}
	for node := range i.Incoming {
		sort.Slice(i.Incoming[node], func(a, b int) bool {
			if i.Incoming[node][a].Count == i.Incoming[node][b].Count {
				return methodNodeKey(i.Incoming[node][a].FromClass, i.Incoming[node][a].FromMethod) <
					methodNodeKey(i.Incoming[node][b].FromClass, i.Incoming[node][b].FromMethod)
			}
			return i.Incoming[node][a].Count > i.Incoming[node][b].Count
		})
	}
}

func edgeKey(edge ClassGraphEdge) string {
	return edge.From + "\x00" + edge.To + "\x00" + edge.CallerMethod + "\x00" + edge.CalleeMethod
}

func normalizeGraphMethodName(value string) string {
	if value == "" {
		return "<unknown>"
	}
	return value
}

func methodNodeKey(className string, methodName string) string {
	methodName = normalizeGraphMethodName(methodName)
	if methodName == "" || methodName == "<unknown>" {
		return className
	}
	return className + "#" + methodName
}

func pathNodes(edges []ClassGraphEdge) []string {
	if len(edges) == 0 {
		return nil
	}
	nodes := []string{edges[0].From}
	for _, edge := range edges {
		if len(nodes) == 0 || nodes[len(nodes)-1] != edge.To {
			nodes = append(nodes, edge.To)
		}
	}
	return nodes
}

func stringsKey(values []string) string {
	key := ""
	for index, value := range values {
		if index > 0 {
			key += "\x00"
		}
		key += value
	}
	return key
}

func hotPathReason(runtimeTarget bool) string {
	if runtimeTarget {
		return "ведет к классу с runtime-симптомами"
	}
	return "сильная статическая связь рядом с проблемной зоной"
}
