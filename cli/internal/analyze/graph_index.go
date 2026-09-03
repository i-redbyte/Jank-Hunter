package analyze

import (
	"math"
	"sort"
)

type ClassGraphIndex struct {
	edges    []ClassGraphEdge
	outgoing map[string][]uint32
	incoming map[string][]uint32
}

type MethodGraphIndex struct {
	edges    []ClassGraphEdge
	outgoing map[methodGraphNodeKey][]uint32
	incoming map[methodGraphNodeKey][]uint32
}

type classGraphEdgeKey struct {
	from, to                   string
	callerMethod, calleeMethod string
}

type methodGraphNodeKey struct {
	className  string
	methodName string
}

func NewClassGraphIndex(edges []ClassGraphEdge) *ClassGraphIndex {
	return newClassGraphIndex(canonicalClassGraphEdges(edges))
}

func newClassGraphIndex(edges []ClassGraphEdge) *ClassGraphIndex {
	index := &ClassGraphIndex{
		edges:    edges,
		outgoing: make(map[string][]uint32),
		incoming: make(map[string][]uint32),
	}
	for edgeIndex := range edges {
		edge := &edges[edgeIndex]
		index.outgoing[edge.From] = append(index.outgoing[edge.From], uint32(edgeIndex))
		index.incoming[edge.To] = append(index.incoming[edge.To], uint32(edgeIndex))
	}
	index.sort()
	return index
}

func canonicalClassGraphEdges(edges []ClassGraphEdge) []ClassGraphEdge {
	merged := make(map[classGraphEdgeKey]ClassGraphEdge, len(edges))
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
		key := classGraphEdgeKey{
			from: edge.From, to: edge.To,
			callerMethod: edge.CallerMethod, calleeMethod: edge.CalleeMethod,
		}
		existing := merged[key]
		if existing.From == "" {
			existing = edge
		} else {
			existing.Count = saturatingUint64Sum(existing.Count, edge.Count)
		}
		merged[key] = existing
	}
	canonical := make([]ClassGraphEdge, 0, len(merged))
	for _, edge := range merged {
		canonical = append(canonical, edge)
	}
	return canonical
}

func NewMethodGraphIndex(edges []ClassGraphEdge) *MethodGraphIndex {
	return newMethodGraphIndex(canonicalClassGraphEdges(edges))
}

func newMethodGraphIndex(edges []ClassGraphEdge) *MethodGraphIndex {
	index := &MethodGraphIndex{
		edges:    edges,
		outgoing: make(map[methodGraphNodeKey][]uint32),
		incoming: make(map[methodGraphNodeKey][]uint32),
	}
	for edgeIndex := range edges {
		edge := &edges[edgeIndex]
		from := methodGraphNodeKey{className: edge.From, methodName: normalizeGraphMethodName(edge.CallerMethod)}
		to := methodGraphNodeKey{className: edge.To, methodName: normalizeGraphMethodName(edge.CalleeMethod)}
		index.outgoing[from] = append(index.outgoing[from], uint32(edgeIndex))
		index.incoming[to] = append(index.incoming[to], uint32(edgeIndex))
	}
	index.sort()
	return index
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

		for _, edgeIndex := range i.outgoing[node] {
			edge := i.edges[edgeIndex]
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
				for _, edgeIndex := range i.outgoing[item] {
					edge := i.edges[edgeIndex]
					if _, inside := componentSet[edge.To]; inside {
						weight = saturatingUint64Sum(weight, edge.Count)
					}
				}
			}
			cycles = append(cycles, InfluenceCycle{Nodes: component, Weight: weight})
		}
	}

	for node := range i.outgoing {
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
	type source struct {
		className string
		score     float64
	}
	sources := make([]source, 0, len(scores))
	for className, score := range scores {
		if score <= 0 {
			continue
		}
		if _, ok := i.outgoing[className]; !ok {
			continue
		}
		sources = append(sources, source{className: className, score: score})
	}
	sort.Slice(sources, func(a, b int) bool {
		if sources[a].score == sources[b].score {
			return sources[a].className < sources[b].className
		}
		return sources[a].score > sources[b].score
	})
	type candidate struct {
		nodes         []string
		weight        float64
		runtimeTarget bool
	}
	type state struct {
		node  string
		edges []ClassGraphEdge
	}
	candidates := []candidate{}
	for _, src := range sources {
		queue := []state{{node: src.className}}
		seenDepth := map[string]int{src.className: 0}
		for len(queue) > 0 {
			current := queue[0]
			queue = queue[1:]
			if len(current.edges) >= 4 {
				continue
			}
			for _, edgeIndex := range i.outgoing[current.node] {
				edge := i.edges[edgeIndex]
				if edge.To == src.className {
					continue
				}
				nextEdges := append(append([]ClassGraphEdge{}, current.edges...), edge)
				depth := len(nextEdges)
				if previousDepth, seen := seenDepth[edge.To]; seen && previousDepth <= depth {
					continue
				}
				seenDepth[edge.To] = depth
				_, runtimeTarget := runtimeTargets[edge.To]
				targetScore := scores[edge.To]
				if runtimeTarget || targetScore > 0 {
					candidates = append(candidates, candidate{
						nodes:         pathNodes(nextEdges),
						weight:        hotPathWeight(nextEdges, src.score, targetScore, runtimeTarget),
						runtimeTarget: runtimeTarget,
					})
				}
				if depth < 4 {
					queue = append(queue, state{node: edge.To, edges: nextEdges})
				}
			}
		}
	}
	sort.Slice(candidates, func(a, b int) bool {
		if candidates[a].weight == candidates[b].weight {
			return stringsKey(candidates[a].nodes) < stringsKey(candidates[b].nodes)
		}
		return candidates[a].weight > candidates[b].weight
	})
	seen := map[string]struct{}{}
	out := []InfluencePath{}
	for _, candidate := range candidates {
		key := stringsKey(candidate.nodes)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, InfluencePath{
			Nodes:         candidate.nodes,
			Weight:        math.Round(candidate.weight*10) / 10,
			RuntimeTarget: candidate.runtimeTarget,
			Reason:        hotPathReason(candidate.runtimeTarget),
		})
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func (i *MethodGraphIndex) HotMethods(scores map[string]float64, runtimeTargets map[string]struct{}, limit int) []InfluenceMethod {
	if i == nil || limit == 0 {
		return nil
	}
	type aggregate struct {
		item InfluenceMethod
	}
	aggregates := map[string]*aggregate{}
	add := func(className string, method string, role string, count uint64, score float64, runtimeTouched bool) {
		if method == "" || method == "<unknown>" {
			return
		}
		key := className + "\x00" + method + "\x00" + role
		row := aggregates[key]
		if row == nil {
			row = &aggregate{item: InfluenceMethod{
				ClassName: className,
				Method:    method,
				Role:      role,
			}}
			aggregates[key] = row
		}
		row.item.Count = saturatingUint64Sum(row.item.Count, count)
		row.item.Weight += math.Log1p(float64(count)) * (1 + score)
		row.item.RuntimeTouched = row.item.RuntimeTouched || runtimeTouched
	}
	for _, edgeIndexes := range i.outgoing {
		for _, edgeIndex := range edgeIndexes {
			edge := i.edges[edgeIndex]
			fromScore := scores[edge.From]
			toScore := scores[edge.To]
			_, fromRuntime := runtimeTargets[edge.From]
			_, toRuntime := runtimeTargets[edge.To]
			if fromScore == 0 && toScore == 0 && !fromRuntime && !toRuntime {
				continue
			}
			add(edge.From, normalizeGraphMethodName(edge.CallerMethod), "caller", edge.Count, fromScore, fromRuntime || toRuntime)
			add(edge.To, normalizeGraphMethodName(edge.CalleeMethod), "callee", edge.Count, toScore, fromRuntime || toRuntime)
		}
	}
	out := make([]InfluenceMethod, 0, len(aggregates))
	for _, row := range aggregates {
		row.item.Weight = math.Round(row.item.Weight*10) / 10
		out = append(out, row.item)
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].Weight == out[b].Weight {
			if out[a].ClassName == out[b].ClassName {
				if out[a].Method == out[b].Method {
					return out[a].Role < out[b].Role
				}
				return out[a].Method < out[b].Method
			}
			return out[a].ClassName < out[b].ClassName
		}
		return out[a].Weight > out[b].Weight
	})
	if limit > 0 && len(out) > limit {
		return out[:limit]
	}
	return out
}

func (i *ClassGraphIndex) sort() {
	for node := range i.outgoing {
		edges := i.outgoing[node]
		sort.Slice(edges, func(a, b int) bool {
			left := i.edges[edges[a]]
			right := i.edges[edges[b]]
			if left.Count == right.Count {
				return left.To < right.To
			}
			return left.Count > right.Count
		})
	}
	for node := range i.incoming {
		edges := i.incoming[node]
		sort.Slice(edges, func(a, b int) bool {
			left := i.edges[edges[a]]
			right := i.edges[edges[b]]
			if left.Count == right.Count {
				return left.From < right.From
			}
			return left.Count > right.Count
		})
	}
}

func (i *MethodGraphIndex) sort() {
	for node := range i.outgoing {
		edges := i.outgoing[node]
		sort.Slice(edges, func(a, b int) bool {
			left := i.edges[edges[a]]
			right := i.edges[edges[b]]
			if left.Count == right.Count {
				if left.To != right.To {
					return left.To < right.To
				}
				return normalizeGraphMethodName(left.CalleeMethod) < normalizeGraphMethodName(right.CalleeMethod)
			}
			return left.Count > right.Count
		})
	}
	for node := range i.incoming {
		edges := i.incoming[node]
		sort.Slice(edges, func(a, b int) bool {
			left := i.edges[edges[a]]
			right := i.edges[edges[b]]
			if left.Count == right.Count {
				if left.From != right.From {
					return left.From < right.From
				}
				return normalizeGraphMethodName(left.CallerMethod) < normalizeGraphMethodName(right.CallerMethod)
			}
			return left.Count > right.Count
		})
	}
}

func normalizeGraphMethodName(value string) string {
	if value == "" {
		return "<unknown>"
	}
	return value
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
		return "ведет к классу с симптомами выполнения"
	}
	return "сильная статическая связь рядом с проблемной зоной"
}

func hotPathWeight(edges []ClassGraphEdge, sourceScore float64, targetScore float64, runtimeTarget bool) float64 {
	var edgeWeight float64
	for _, edge := range edges {
		edgeWeight += math.Log1p(float64(edge.Count))
	}
	if edgeWeight == 0 {
		edgeWeight = 1
	}
	depthPenalty := 1 / math.Sqrt(float64(len(edges)))
	weight := edgeWeight * depthPenalty * (1 + sourceScore*0.25 + targetScore*0.75)
	if runtimeTarget {
		weight *= 1.35
	}
	return weight
}
