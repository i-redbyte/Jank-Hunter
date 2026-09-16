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
	finishOrder := i.depthFirstFinishOrder()
	componentByNode := make(map[string]uint32, len(finishOrder))
	cycles := make([]InfluenceCycle, 0, boundedResultCapacity(limit))
	var componentID uint32
	for orderIndex := len(finishOrder) - 1; orderIndex >= 0; orderIndex-- {
		root := finishOrder[orderIndex]
		if componentByNode[root] != 0 {
			continue
		}
		componentID++
		component := i.collectReverseComponent(root, componentID, componentByNode)
		if len(component) <= 1 {
			continue
		}
		sort.Strings(component)
		var weight uint64
		for _, node := range component {
			for _, edgeIndex := range i.outgoing[node] {
				edge := i.edges[edgeIndex]
				if componentByNode[edge.To] == componentID {
					weight = saturatingUint64Sum(weight, edge.Count)
				}
			}
		}
		cycles = retainTopInfluenceCycle(cycles, InfluenceCycle{Nodes: component, Weight: weight}, limit)
	}
	sortInfluenceCycles(cycles)
	return cycles
}

type graphDFSFrame struct {
	node          string
	nextEdgeIndex int
}

func (i *ClassGraphIndex) depthFirstFinishOrder() []string {
	visited := make(map[string]struct{}, len(i.outgoing)+len(i.incoming))
	finishOrder := make([]string, 0, len(i.outgoing)+len(i.incoming))
	stack := make([]graphDFSFrame, 0, 64)
	for root := range i.outgoing {
		if _, seen := visited[root]; seen {
			continue
		}
		visited[root] = struct{}{}
		stack = append(stack, graphDFSFrame{node: root})
		for len(stack) > 0 {
			frame := &stack[len(stack)-1]
			edges := i.outgoing[frame.node]
			if frame.nextEdgeIndex < len(edges) {
				next := i.edges[edges[frame.nextEdgeIndex]].To
				frame.nextEdgeIndex++
				if _, seen := visited[next]; !seen {
					visited[next] = struct{}{}
					stack = append(stack, graphDFSFrame{node: next})
				}
				continue
			}
			finishOrder = append(finishOrder, frame.node)
			stack = stack[:len(stack)-1]
		}
	}
	return finishOrder
}

func (i *ClassGraphIndex) collectReverseComponent(
	root string,
	componentID uint32,
	componentByNode map[string]uint32,
) []string {
	component := make([]string, 0, 4)
	stack := []string{root}
	componentByNode[root] = componentID
	for len(stack) > 0 {
		lastIndex := len(stack) - 1
		node := stack[lastIndex]
		stack = stack[:lastIndex]
		component = append(component, node)
		for _, edgeIndex := range i.incoming[node] {
			previous := i.edges[edgeIndex].From
			if componentByNode[previous] == 0 {
				componentByNode[previous] = componentID
				stack = append(stack, previous)
			}
		}
	}
	return component
}

func retainTopInfluenceCycle(cycles []InfluenceCycle, candidate InfluenceCycle, limit int) []InfluenceCycle {
	cycles = append(cycles, candidate)
	if limit < 0 || len(cycles) <= limit {
		return cycles
	}
	sortInfluenceCycles(cycles)
	cycles[limit] = InfluenceCycle{}
	return cycles[:limit]
}

func sortInfluenceCycles(cycles []InfluenceCycle) {
	sort.Slice(cycles, func(a, b int) bool {
		if cycles[a].Weight == cycles[b].Weight {
			return compareStringSlices(cycles[a].Nodes, cycles[b].Nodes) < 0
		}
		return cycles[a].Weight > cycles[b].Weight
	})
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
		key           string
		weight        float64
		runtimeTarget bool
	}
	type state struct {
		node        string
		edgeIndexes [maxHotPathDepth]uint32
		depth       uint8
	}
	candidates := make([]candidate, 0, boundedResultCapacity(limit))
	candidateComesBefore := func(left candidate, right candidate) bool {
		if left.weight == right.weight {
			return left.key < right.key
		}
		return left.weight > right.weight
	}
	worstCandidateWeight := func() float64 {
		worst := math.Inf(1)
		for _, item := range candidates {
			if item.weight < worst {
				worst = item.weight
			}
		}
		return worst
	}
	retainCandidate := func(next candidate) {
		if limit < 0 {
			candidates = append(candidates, next)
			return
		}
		for index := range candidates {
			if candidates[index].key == next.key {
				if candidateComesBefore(next, candidates[index]) {
					candidates[index] = next
				}
				return
			}
		}
		if len(candidates) < limit {
			candidates = append(candidates, next)
			return
		}
		worstIndex := 0
		for index := 1; index < len(candidates); index++ {
			if candidateComesBefore(candidates[worstIndex], candidates[index]) {
				worstIndex = index
			}
		}
		if candidateComesBefore(next, candidates[worstIndex]) {
			candidates[worstIndex] = next
		}
	}
	for _, src := range sources {
		queue := []state{{node: src.className}}
		queueCursor := 0
		seenDepth := map[string]int{src.className: 0}
		for queueCursor < len(queue) {
			current := queue[queueCursor]
			queueCursor++
			if current.depth >= maxHotPathDepth {
				continue
			}
			for _, edgeIndex := range i.outgoing[current.node] {
				edge := i.edges[edgeIndex]
				if edge.To == src.className {
					continue
				}
				next := current
				next.node = edge.To
				next.edgeIndexes[current.depth] = edgeIndex
				next.depth++
				depth := int(next.depth)
				if previousDepth, seen := seenDepth[edge.To]; seen && previousDepth <= depth {
					continue
				}
				seenDepth[edge.To] = depth
				_, runtimeTarget := runtimeTargets[edge.To]
				targetScore := scores[edge.To]
				if runtimeTarget || targetScore > 0 {
					weight := hotPathWeight(i.edges, next.edgeIndexes[:next.depth], src.score, targetScore, runtimeTarget)
					if limit < 0 || len(candidates) < limit || weight >= worstCandidateWeight() {
						nodes := make([]string, depth+1)
						nodes[0] = src.className
						for pathIndex, pathEdgeIndex := range next.edgeIndexes[:next.depth] {
							nodes[pathIndex+1] = i.edges[pathEdgeIndex].To
						}
						retainCandidate(candidate{
							nodes:         nodes,
							key:           stringsKey(nodes),
							weight:        weight,
							runtimeTarget: runtimeTarget,
						})
					}
				}
				if depth < maxHotPathDepth {
					queue = append(queue, next)
				}
			}
		}
	}
	sort.Slice(candidates, func(a, b int) bool {
		return candidateComesBefore(candidates[a], candidates[b])
	})
	seen := map[string]struct{}{}
	out := []InfluencePath{}
	for _, candidate := range candidates {
		if _, ok := seen[candidate.key]; ok {
			continue
		}
		seen[candidate.key] = struct{}{}
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

const maxHotPathDepth = 4
const maxPreallocatedGraphResults = 64

func boundedResultCapacity(limit int) int {
	if limit > 0 {
		return min(limit, maxPreallocatedGraphResults)
	}
	return 0
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

func stringsKey(values []string) string {
	if len(values) == 0 {
		return ""
	}
	length := len(values) - 1
	for _, value := range values {
		length += len(value)
	}
	buffer := make([]byte, 0, length)
	for index, value := range values {
		if index > 0 {
			buffer = append(buffer, 0)
		}
		buffer = append(buffer, value...)
	}
	return string(buffer)
}

func compareStringSlices(left []string, right []string) int {
	commonLength := min(len(left), len(right))
	for index := 0; index < commonLength; index++ {
		if left[index] < right[index] {
			return -1
		}
		if left[index] > right[index] {
			return 1
		}
	}
	return len(left) - len(right)
}

func hotPathReason(runtimeTarget bool) string {
	if runtimeTarget {
		return "ведет к классу с симптомами выполнения"
	}
	return "сильная статическая связь рядом с проблемной зоной"
}

func hotPathWeight(
	edges []ClassGraphEdge,
	edgeIndexes []uint32,
	sourceScore float64,
	targetScore float64,
	runtimeTarget bool,
) float64 {
	var edgeWeight float64
	for _, edgeIndex := range edgeIndexes {
		edgeWeight += math.Log1p(float64(edges[edgeIndex].Count))
	}
	if edgeWeight == 0 {
		edgeWeight = 1
	}
	depthPenalty := 1 / math.Sqrt(float64(len(edgeIndexes)))
	weight := edgeWeight * depthPenalty * (1 + sourceScore*0.25 + targetScore*0.75)
	if runtimeTarget {
		weight *= 1.35
	}
	return weight
}
