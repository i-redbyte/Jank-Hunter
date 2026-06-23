package analyze

import (
	"math"
	"sort"
)

const (
	defaultMaxGraphIndexEdges          = 150_000
	defaultMaxRelevantGraphEdges       = 2_500
	defaultMaxStrongComponentNodes     = 10_000
	defaultMaxHotPathSources           = 80
	defaultMaxHotPathExploredPerSource = 2_048
)

type GraphIndexBudget struct {
	MaxEdges           int
	MaxRelevantEdges   int
	MaxSCCNodes        int
	MaxHotPathSources  int
	MaxHotPathExplored int
}

type ClassGraphIndex struct {
	Outgoing map[string][]ClassGraphEdge
	Incoming map[string][]ClassGraphEdge
	budget   GraphIndexBudget
}

type MethodGraphIndex struct {
	Outgoing map[string][]MethodGraphEdge
	Incoming map[string][]MethodGraphEdge
	budget   GraphIndexBudget
}

type MethodGraphEdge struct {
	FromClass  string
	FromMethod string
	ToClass    string
	ToMethod   string
	Count      uint64
}

func NewClassGraphIndex(edges []ClassGraphEdge) *ClassGraphIndex {
	return NewClassGraphIndexWithBudget(edges, GraphIndexBudget{})
}

func NewClassGraphIndexWithBudget(edges []ClassGraphEdge, budget GraphIndexBudget) *ClassGraphIndex {
	budget = normalizeGraphIndexBudget(budget)
	index := &ClassGraphIndex{
		Outgoing: map[string][]ClassGraphEdge{},
		Incoming: map[string][]ClassGraphEdge{},
		budget:   budget,
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
	mergedEdges := make([]ClassGraphEdge, 0, len(merged))
	for _, edge := range merged {
		mergedEdges = append(mergedEdges, edge)
	}
	mergedEdges = limitClassGraphEdgesByWeight(mergedEdges, budget.MaxEdges)
	for _, edge := range mergedEdges {
		index.Outgoing[edge.From] = append(index.Outgoing[edge.From], edge)
		index.Incoming[edge.To] = append(index.Incoming[edge.To], edge)
	}
	index.sort()
	return index
}

func NewMethodGraphIndex(edges []ClassGraphEdge) *MethodGraphIndex {
	return NewMethodGraphIndexWithBudget(edges, GraphIndexBudget{})
}

func NewMethodGraphIndexWithBudget(edges []ClassGraphEdge, budget GraphIndexBudget) *MethodGraphIndex {
	budget = normalizeGraphIndexBudget(budget)
	index := &MethodGraphIndex{
		Outgoing: map[string][]MethodGraphEdge{},
		Incoming: map[string][]MethodGraphEdge{},
		budget:   budget,
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
	mergedEdges := make([]MethodGraphEdge, 0, len(merged))
	for _, edge := range merged {
		mergedEdges = append(mergedEdges, edge)
	}
	mergedEdges = limitMethodGraphEdgesByWeight(mergedEdges, budget.MaxEdges)
	for _, edge := range mergedEdges {
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
	out = limitClassGraphEdgesByWeight(out, i.budget.MaxRelevantEdges)
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

func (i *ClassGraphIndex) StronglyConnectedComponents(limit int) []InfluenceCycle {
	if i == nil || limit == 0 {
		return nil
	}
	if i.budget.MaxSCCNodes > 0 && len(i.Outgoing) > i.budget.MaxSCCNodes {
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
	type source struct {
		className string
		score     float64
	}
	sources := make([]source, 0, len(scores))
	for className, score := range scores {
		if score <= 0 {
			continue
		}
		if _, ok := i.Outgoing[className]; !ok {
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
	if i.budget.MaxHotPathSources > 0 && len(sources) > i.budget.MaxHotPathSources {
		sources = sources[:i.budget.MaxHotPathSources]
	}

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
		explored := 0
		for len(queue) > 0 && explored < i.budget.MaxHotPathExplored {
			current := queue[0]
			queue = queue[1:]
			explored++
			if len(current.edges) >= 4 {
				continue
			}
			for _, edge := range i.Outgoing[current.node] {
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
		row.item.Count += count
		row.item.Weight += math.Log1p(float64(count)) * (1 + score)
		row.item.RuntimeTouched = row.item.RuntimeTouched || runtimeTouched
	}
	for _, edges := range i.Outgoing {
		for _, edge := range edges {
			fromScore := scores[edge.FromClass]
			toScore := scores[edge.ToClass]
			_, fromRuntime := runtimeTargets[edge.FromClass]
			_, toRuntime := runtimeTargets[edge.ToClass]
			if fromScore == 0 && toScore == 0 && !fromRuntime && !toRuntime {
				continue
			}
			add(edge.FromClass, edge.FromMethod, "caller", edge.Count, fromScore, fromRuntime || toRuntime)
			add(edge.ToClass, edge.ToMethod, "callee", edge.Count, toScore, fromRuntime || toRuntime)
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

func normalizeGraphIndexBudget(budget GraphIndexBudget) GraphIndexBudget {
	if budget.MaxEdges <= 0 {
		budget.MaxEdges = defaultMaxGraphIndexEdges
	}
	if budget.MaxRelevantEdges <= 0 {
		budget.MaxRelevantEdges = defaultMaxRelevantGraphEdges
	}
	if budget.MaxSCCNodes <= 0 {
		budget.MaxSCCNodes = defaultMaxStrongComponentNodes
	}
	if budget.MaxHotPathSources <= 0 {
		budget.MaxHotPathSources = defaultMaxHotPathSources
	}
	if budget.MaxHotPathExplored <= 0 {
		budget.MaxHotPathExplored = defaultMaxHotPathExploredPerSource
	}
	return budget
}

func limitClassGraphEdgesByWeight(edges []ClassGraphEdge, limit int) []ClassGraphEdge {
	if limit <= 0 || len(edges) <= limit {
		return edges
	}
	sort.Slice(edges, func(a, b int) bool {
		if edges[a].Count != edges[b].Count {
			return edges[a].Count > edges[b].Count
		}
		if edges[a].From != edges[b].From {
			return edges[a].From < edges[b].From
		}
		if edges[a].To != edges[b].To {
			return edges[a].To < edges[b].To
		}
		if edges[a].CallerMethod != edges[b].CallerMethod {
			return edges[a].CallerMethod < edges[b].CallerMethod
		}
		return edges[a].CalleeMethod < edges[b].CalleeMethod
	})
	out := make([]ClassGraphEdge, limit)
	copy(out, edges[:limit])
	return out
}

func limitMethodGraphEdgesByWeight(edges []MethodGraphEdge, limit int) []MethodGraphEdge {
	if limit <= 0 || len(edges) <= limit {
		return edges
	}
	sort.Slice(edges, func(a, b int) bool {
		if edges[a].Count != edges[b].Count {
			return edges[a].Count > edges[b].Count
		}
		leftFrom := methodNodeKey(edges[a].FromClass, edges[a].FromMethod)
		rightFrom := methodNodeKey(edges[b].FromClass, edges[b].FromMethod)
		if leftFrom != rightFrom {
			return leftFrom < rightFrom
		}
		leftTo := methodNodeKey(edges[a].ToClass, edges[a].ToMethod)
		rightTo := methodNodeKey(edges[b].ToClass, edges[b].ToMethod)
		return leftTo < rightTo
	})
	out := make([]MethodGraphEdge, limit)
	copy(out, edges[:limit])
	return out
}
