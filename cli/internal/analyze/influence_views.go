package analyze

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

const (
	problemViewMaxNodes      = 48
	problemViewMaxEdges      = 96
	runtimeViewMaxNodes      = 48
	runtimeViewMaxEdges      = 96
	packageViewMaxNodes      = 140
	packageViewMaxEdges      = 64
	neighborhoodViewMaxNodes = 64
	neighborhoodViewMaxEdges = 120
	contextViewMaxNodes      = 16
	contextViewMaxEdges      = 48
	workspaceMaxNodes        = 64
	workspaceMaxEdges        = 128
	workspaceMaxContexts     = 8
	packageChildSample       = 6
)

type influenceViewBuilder struct {
	nodes          []InfluenceNode
	edges          []InfluenceEdge
	nodeByID       map[string]uint32
	outgoing       influenceAdjacency
	incoming       influenceAdjacency
	classNodes     map[string]influenceClassNodeProjection
	contextMarks   []uint8
	contextTouched []uint32
}

type influenceClassNodeProjection struct {
	packageName string
	breadcrumbs []string
	explanation string
}

type influenceClassCandidate struct {
	node      *InfluenceNode
	connector bool
}

type influencePackageAggregate struct {
	name         string
	children     [packageChildSample]uint32
	childSamples uint8
	childCount   int
	maxClass     string
	maxScore     float64
	maxSeverity  string
	runtime      int
	staticOnly   int
	problems     int
}

type influencePackageEdgeKey struct {
	from int
	to   int
}

type influencePackageEdgeAggregate struct {
	count        uint64
	runtimeCount uint64
	staticCount  uint64
	influence    float64
}

type influenceContextKind uint8

const (
	influenceContextOperation influenceContextKind = iota
	influenceContextRoute
	influenceContextScreen
)

type influenceContextKey struct {
	value string
	kind  influenceContextKind
}

type influenceContextCounts struct {
	runtimeNodes int
	problemNodes int
}

type influenceContextCandidate struct {
	key    influenceContextKey
	counts influenceContextCounts
}

type influenceContextSelection struct {
	items []InfluenceGraphContext
	total int
}

func buildInfluenceViews(nodes []InfluenceNode, edges []InfluenceEdge) ([]InfluenceGraphView, InfluenceGraphWorkspace) {
	builder := newPreparedInfluenceViewBuilder(nodes, edges)
	views := []InfluenceGraphView{
		builder.problemsView(),
		builder.runtimeView(),
	}
	for depth := 2; depth <= 5; depth++ {
		views = append(views, builder.packagesView(depth))
	}
	views = append(views, builder.neighborhoodView(builder.defaultNode(), "both", 1, false))
	contexts := builder.boundedContexts()
	for _, context := range contexts.items {
		views = append(views, builder.contextView(context))
	}
	return views, builder.workspace(contexts)
}

func newInfluenceViewBuilder(nodes []InfluenceNode, edges []InfluenceEdge) *influenceViewBuilder {
	nodeCopy := append([]InfluenceNode(nil), nodes...)
	edgeCopy := append([]InfluenceEdge(nil), edges...)
	sort.Slice(nodeCopy, func(i, j int) bool {
		return influenceNodeLess(nodeCopy[i], nodeCopy[j])
	})
	sortInfluenceEdges(edgeCopy)
	return newPreparedInfluenceViewBuilder(nodeCopy, edgeCopy)
}

// newPreparedInfluenceViewBuilder takes ownership of already sorted slices produced by
// influenceBuilder.finish. Keeping that contract explicit avoids duplicating the complete graph
// immediately before bounded views discard most of it.
func newPreparedInfluenceViewBuilder(nodes []InfluenceNode, edges []InfluenceEdge) *influenceViewBuilder {
	builder := &influenceViewBuilder{
		nodes:          nodes,
		edges:          edges,
		nodeByID:       make(map[string]uint32, len(nodes)),
		classNodes:     make(map[string]influenceClassNodeProjection),
		contextMarks:   make([]uint8, len(nodes)),
		contextTouched: make([]uint32, 0, min(len(nodes), 256)),
	}
	for index := range nodes {
		node := &nodes[index]
		builder.nodeByID[node.ClassName] = uint32(index) + 1
	}
	builder.outgoing, builder.incoming = newInfluenceAdjacency(builder.nodeByID, edges)
	return builder
}

func (b *influenceViewBuilder) problemsView() InfluenceGraphView {
	problemNodes := map[string]struct{}{}
	candidates := map[string]bool{}
	for _, node := range b.nodes {
		if !isInfluenceProblem(node) {
			continue
		}
		problemNodes[node.ClassName] = struct{}{}
		candidates[node.ClassName] = false
	}
	totalEdges := 0
	for _, edge := range b.edges {
		_, fromProblem := problemNodes[edge.From]
		_, toProblem := problemNodes[edge.To]
		if !fromProblem && !toProblem {
			continue
		}
		totalEdges++
		for _, className := range []string{edge.From, edge.To} {
			if _, exists := candidates[className]; exists {
				continue
			}
			if _, ok := b.nodeByID[className]; ok {
				candidates[className] = true
			}
		}
	}
	nodes := b.boundedClassNodes(candidates, problemViewMaxNodes)
	selected := influenceGraphNodeIDs(nodes)
	edges := make([]InfluenceGraphEdge, 0, min(totalEdges, problemViewMaxEdges))
	for _, edge := range b.edges {
		_, fromProblem := problemNodes[edge.From]
		_, toProblem := problemNodes[edge.To]
		if !fromProblem && !toProblem {
			continue
		}
		if _, ok := selected[edge.From]; !ok {
			continue
		}
		if _, ok := selected[edge.To]; !ok {
			continue
		}
		edges = append(edges, graphEdge(edge, false))
		if len(edges) == problemViewMaxEdges {
			break
		}
	}
	return boundedInfluenceViewWithTotals(
		"problems",
		"problems",
		"Проблемы",
		"Классы с наибольшей оценкой риска и ближайшие связующие классы. Оценка задаёт порядок проверки и не доказывает причину.",
		InfluenceGraphFilters{},
		nodes,
		edges,
		len(candidates),
		totalEdges,
		InfluenceGraphLimits{MaxNodes: problemViewMaxNodes, MaxEdges: problemViewMaxEdges},
	)
}

func (b *influenceViewBuilder) runtimeView() InfluenceGraphView {
	candidates := map[string]bool{}
	for _, node := range b.nodes {
		if node.RuntimeEvidence {
			candidates[node.ClassName] = false
		}
	}
	totalEdges := 0
	for _, edge := range b.edges {
		if edge.RuntimeCount == 0 {
			continue
		}
		if _, fromOK := candidates[edge.From]; !fromOK {
			continue
		}
		if _, toOK := candidates[edge.To]; !toOK {
			continue
		}
		totalEdges++
	}
	nodes := b.boundedClassNodes(candidates, runtimeViewMaxNodes)
	selected := influenceGraphNodeIDs(nodes)
	edges := make([]InfluenceGraphEdge, 0, min(totalEdges, runtimeViewMaxEdges))
	for _, edge := range b.edges {
		if edge.RuntimeCount == 0 {
			continue
		}
		if _, ok := selected[edge.From]; !ok {
			continue
		}
		if _, ok := selected[edge.To]; !ok {
			continue
		}
		edges = append(edges, graphEdge(edge, false))
		if len(edges) == runtimeViewMaxEdges {
			break
		}
	}
	return boundedInfluenceViewWithTotals(
		"runtime",
		"runtime",
		"Только выполнение",
		"Только классы и связи, реально записанные во время этого прогона. Изолированные классы с наблюдаемыми симптомами сохраняются.",
		InfluenceGraphFilters{RuntimeOnly: true},
		nodes,
		edges,
		len(candidates),
		totalEdges,
		InfluenceGraphLimits{MaxNodes: runtimeViewMaxNodes, MaxEdges: runtimeViewMaxEdges},
	)
}

func (b *influenceViewBuilder) packagesView(depth int) InfluenceGraphView {
	aggregateIndexes := make(map[string]int)
	packageByNode := make([]uint32, len(b.nodes))
	for index := range b.nodes {
		node := &b.nodes[index]
		packageName := influencePackage(node.ClassName, depth)
		slot := aggregateIndexes[packageName]
		if slot == 0 {
			slot = len(aggregateIndexes) + 1
			aggregateIndexes[packageName] = slot
		}
		packageByNode[index] = uint32(slot)
	}
	aggregates := make([]influencePackageAggregate, len(aggregateIndexes))
	for packageName, slot := range aggregateIndexes {
		aggregates[slot-1].name = packageName
	}
	for nodeIndex := range b.nodes {
		node := &b.nodes[nodeIndex]
		aggregate := &aggregates[packageByNode[nodeIndex]-1]
		aggregate.childCount++
		aggregate.addChildSample(nodeIndex)
		if aggregate.maxClass == "" {
			aggregate.maxClass = node.ClassName
			aggregate.maxScore = node.Score
			aggregate.maxSeverity = node.Severity
		}
		if node.RuntimeEvidence {
			aggregate.runtime++
		} else {
			aggregate.staticOnly++
		}
		if isInfluenceProblem(*node) {
			aggregate.problems++
		}
	}
	orderedAggregates := make([]int, len(aggregates))
	for index := range orderedAggregates {
		orderedAggregates[index] = index
	}
	sort.Slice(orderedAggregates, func(i, j int) bool {
		left := &aggregates[orderedAggregates[i]]
		right := &aggregates[orderedAggregates[j]]
		if left.maxScore != right.maxScore {
			return left.maxScore > right.maxScore
		}
		if left.problems != right.problems {
			return left.problems > right.problems
		}
		if (left.runtime > 0) != (right.runtime > 0) {
			return left.runtime > 0
		}
		return left.name < right.name
	})
	totalNodes := len(orderedAggregates)
	if len(orderedAggregates) > packageViewMaxNodes {
		orderedAggregates = orderedAggregates[:packageViewMaxNodes]
	}
	nodes := make([]InfluenceGraphNode, 0, len(orderedAggregates))
	selectedPackages := make([]bool, len(aggregates))
	for _, aggregateIndex := range orderedAggregates {
		aggregate := &aggregates[aggregateIndex]
		packageID := "package:" + aggregate.name
		selectedPackages[aggregateIndex] = true
		children := make([]string, aggregate.childSamples)
		for index := range children {
			children[index] = b.nodes[aggregate.children[index]].ClassName
		}
		nodes = append(nodes, InfluenceGraphNode{
			InfluenceNode: InfluenceNode{
				ClassName:       aggregate.name,
				Label:           aggregate.name,
				Score:           aggregate.maxScore,
				Severity:        aggregate.maxSeverity,
				Status:          "aggregate",
				RuntimeEvidence: aggregate.runtime > 0,
			},
			ID:                   packageID,
			Kind:                 "package",
			Package:              aggregate.name,
			Breadcrumbs:          strings.Split(aggregate.name, "."),
			Aggregate:            true,
			ChildCount:           aggregate.childCount,
			RuntimeClassCount:    aggregate.runtime,
			StaticOnlyClassCount: aggregate.staticOnly,
			ProblemClassCount:    aggregate.problems,
			Children:             children,
			Explanation: fmt.Sprintf(
				"Пакет %s объединяет %d классов. Оценка %.1f равна максимальной оценке дочернего класса %s, а не сумме.",
				aggregate.name,
				aggregate.childCount,
				aggregate.maxScore,
				aggregate.maxClass,
			),
		})
	}
	edgeByKey := map[influencePackageEdgeKey]influencePackageEdgeAggregate{}
	for _, edge := range b.edges {
		fromNodeSlot := b.nodeByID[edge.From]
		toNodeSlot := b.nodeByID[edge.To]
		if fromNodeSlot == 0 || toNodeSlot == 0 {
			continue
		}
		fromSlot := int(packageByNode[fromNodeSlot-1])
		toSlot := int(packageByNode[toNodeSlot-1])
		if fromSlot == 0 || toSlot == 0 || fromSlot == toSlot {
			continue
		}
		key := influencePackageEdgeKey{from: fromSlot - 1, to: toSlot - 1}
		aggregated := edgeByKey[key]
		aggregated.runtimeCount += edge.RuntimeCount
		aggregated.staticCount += edge.StaticCount
		aggregated.count += edge.Count
		aggregated.influence += edge.Influence
		edgeByKey[key] = aggregated
	}
	totalEdges := len(edgeByKey)
	edges := make([]InfluenceGraphEdge, 0, min(totalEdges, packageViewMaxEdges))
	for key, aggregate := range edgeByKey {
		if !selectedPackages[key.from] {
			continue
		}
		if !selectedPackages[key.to] {
			continue
		}
		fromPackage := &aggregates[key.from]
		toPackage := &aggregates[key.to]
		edge := InfluenceEdge{
			From:         "package:" + fromPackage.name,
			To:           "package:" + toPackage.name,
			Count:        aggregate.count,
			RuntimeCount: aggregate.runtimeCount,
			StaticCount:  aggregate.staticCount,
			Influence:    aggregate.influence,
		}
		edge = normalizeInfluenceEvidence(edge)
		edges = append(edges, graphEdge(edge, true))
	}
	return boundedInfluenceViewWithTotals(
		fmt.Sprintf("packages:%d", depth),
		"packages",
		"Пакеты",
		"Классы сгруппированы по пакету. Размер и уровень риска группы определяются максимальной оценкой дочернего класса; записанные и статические связи считаются отдельно.",
		InfluenceGraphFilters{PackageDepth: depth},
		nodes,
		edges,
		totalNodes,
		totalEdges,
		InfluenceGraphLimits{MaxNodes: packageViewMaxNodes, MaxEdges: packageViewMaxEdges},
	)
}

func (a *influencePackageAggregate) addChildSample(nodeIndex int) {
	if a.childSamples < packageChildSample {
		a.children[a.childSamples] = uint32(nodeIndex)
		a.childSamples++
	}
}

func (b *influenceViewBuilder) neighborhoodView(selected string, direction string, depth int, runtimeOnly bool) InfluenceGraphView {
	if depth < 1 {
		depth = 1
	}
	if depth > 3 {
		depth = 3
	}
	if direction != "incoming" && direction != "outgoing" {
		direction = "both"
	}
	reached := map[string]int{}
	if _, ok := b.nodeByID[selected]; ok {
		reached[selected] = 0
	}
	queue := []string{}
	if selected != "" && len(reached) > 0 {
		queue = append(queue, selected)
	}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		currentDepth := reached[current]
		if currentDepth >= depth {
			continue
		}
		currentSlot := b.nodeByID[current]
		outgoing := b.outgoing.edgesForSlot(currentSlot)
		incoming := b.incoming.edgesForSlot(currentSlot)
		adjacent := make([]uint32, 0, len(outgoing)+len(incoming))
		if direction == "outgoing" || direction == "both" {
			adjacent = append(adjacent, outgoing...)
		}
		if direction == "incoming" || direction == "both" {
			adjacent = append(adjacent, incoming...)
		}
		sort.Slice(adjacent, func(i, j int) bool {
			return influenceEdgeLess(b.edges[adjacent[i]], b.edges[adjacent[j]])
		})
		for _, edgeIndex := range adjacent {
			edge := b.edges[edgeIndex]
			if runtimeOnly && edge.RuntimeCount == 0 {
				continue
			}
			next := edge.To
			if next == current {
				next = edge.From
			}
			if previous, seen := reached[next]; seen && previous <= currentDepth+1 {
				continue
			}
			reached[next] = currentDepth + 1
			queue = append(queue, next)
		}
	}
	candidates := make(map[string]bool, len(reached))
	for className := range reached {
		if _, ok := b.nodeByID[className]; ok {
			candidates[className] = false
		}
	}
	edges := make([]InfluenceGraphEdge, 0)
	for _, edge := range b.edges {
		if runtimeOnly && edge.RuntimeCount == 0 {
			continue
		}
		if _, fromOK := reached[edge.From]; !fromOK {
			continue
		}
		if _, toOK := reached[edge.To]; !toOK {
			continue
		}
		edges = append(edges, graphEdge(edge, false))
	}
	return boundedInfluenceViewWithTotals(
		"neighborhood",
		"neighborhood",
		"Окрестность",
		"Безопасный для циклов обход выбранного класса. Глубина и направление меняют только состав представления, но не оценку и исходные метрики.",
		InfluenceGraphFilters{SelectedNode: selected, Direction: direction, Depth: depth, RuntimeOnly: runtimeOnly},
		b.boundedClassNodes(candidates, neighborhoodViewMaxNodes),
		edges,
		len(candidates),
		len(edges),
		InfluenceGraphLimits{MaxNodes: neighborhoodViewMaxNodes, MaxEdges: neighborhoodViewMaxEdges},
	)
}

func (b *influenceViewBuilder) contextView(context InfluenceGraphContext) InfluenceGraphView {
	const (
		contextTargetMark uint8 = 1 << iota
		contextFromTargetMark
		contextToTargetMark
		contextSelectedMark
	)
	for index := range b.nodes {
		node := &b.nodes[index]
		if influenceNodeMatchesContext(*node, context.Kind, context.Value) {
			b.markContextNode(uint32(index), contextTargetMark)
		}
	}
	for index := range b.edges {
		edge := &b.edges[index]
		fromSlot := b.nodeByID[edge.From]
		toSlot := b.nodeByID[edge.To]
		if fromSlot == 0 || toSlot == 0 {
			continue
		}
		fromIndex := fromSlot - 1
		toIndex := toSlot - 1
		if b.contextMarks[fromIndex]&contextTargetMark != 0 {
			b.markContextNode(toIndex, contextFromTargetMark)
		}
		if b.contextMarks[toIndex]&contextTargetMark != 0 {
			b.markContextNode(fromIndex, contextToTargetMark)
		}
	}
	nodes, totalNodes := b.boundedContextNodes(contextViewMaxNodes)
	for index := range nodes {
		if slot := b.nodeByID[nodes[index].ID]; slot != 0 {
			b.contextMarks[slot-1] |= contextSelectedMark
		}
	}
	edges := make([]InfluenceGraphEdge, 0, contextViewMaxEdges)
	totalEdges := 0
	for index := range b.edges {
		edge := b.edges[index]
		fromSlot := b.nodeByID[edge.From]
		toSlot := b.nodeByID[edge.To]
		if fromSlot == 0 || toSlot == 0 {
			continue
		}
		fromMark := b.contextMarks[fromSlot-1]
		toMark := b.contextMarks[toSlot-1]
		fromCandidate := fromMark&contextTargetMark != 0 || fromMark&(contextFromTargetMark|contextToTargetMark) == contextFromTargetMark|contextToTargetMark
		toCandidate := toMark&contextTargetMark != 0 || toMark&(contextFromTargetMark|contextToTargetMark) == contextFromTargetMark|contextToTargetMark
		if !fromCandidate || !toCandidate || fromMark&contextTargetMark == 0 && toMark&contextTargetMark == 0 {
			continue
		}
		totalEdges++
		if fromMark&contextSelectedMark == 0 || toMark&contextSelectedMark == 0 {
			continue
		}
		edges = append(edges, graphEdge(edge, false))
	}
	b.resetContextMarks()
	return boundedInfluenceViewWithTotals(
		context.ID,
		"context",
		"Экран / операция",
		"Классы с записанным контекстом и необходимые связующие классы на расстоянии до двух связей. Связующий класс не считается непосредственным участником экрана, операции или маршрута.",
		InfluenceGraphFilters{ContextKind: context.Kind, ContextValue: context.Value},
		nodes,
		edges,
		totalNodes,
		totalEdges,
		InfluenceGraphLimits{MaxNodes: contextViewMaxNodes, MaxEdges: contextViewMaxEdges},
	)
}

func (b *influenceViewBuilder) markContextNode(index uint32, mark uint8) {
	if b.contextMarks[index] == 0 {
		b.contextTouched = append(b.contextTouched, index)
	}
	b.contextMarks[index] |= mark
}

func (b *influenceViewBuilder) resetContextMarks() {
	for _, index := range b.contextTouched {
		b.contextMarks[index] = 0
	}
	b.contextTouched = b.contextTouched[:0]
}

func (b *influenceViewBuilder) boundedContextNodes(limit int) ([]InfluenceGraphNode, int) {
	const connectorMarks = uint8(1<<1 | 1<<2)
	ordered := make([]influenceClassCandidate, 0, limit)
	total := 0
	for _, index := range b.contextTouched {
		mark := b.contextMarks[index]
		target := mark&1 != 0
		connector := !target && mark&connectorMarks == connectorMarks
		if !target && !connector {
			continue
		}
		total++
		candidate := influenceClassCandidate{node: &b.nodes[index], connector: connector}
		if len(ordered) < limit {
			ordered = append(ordered, candidate)
		} else if !influenceClassCandidateLess(candidate, ordered[len(ordered)-1]) {
			continue
		} else {
			ordered[len(ordered)-1] = candidate
		}
		for position := len(ordered) - 1; position > 0 && influenceClassCandidateLess(ordered[position], ordered[position-1]); position-- {
			ordered[position], ordered[position-1] = ordered[position-1], ordered[position]
		}
	}
	nodes := make([]InfluenceGraphNode, len(ordered))
	for index := range ordered {
		nodes[index] = b.classNode(*ordered[index].node, ordered[index].connector)
	}
	return nodes, total
}

func (b *influenceViewBuilder) boundedContexts() influenceContextSelection {
	byKey := make(map[influenceContextKey]influenceContextCounts)
	for _, node := range b.nodes {
		if !node.RuntimeEvidence {
			continue
		}
		problem := isInfluenceProblem(node)
		for _, value := range node.Screens {
			addInfluenceContext(byKey, influenceContextScreen, value, problem)
		}
		for _, value := range node.Operations {
			addInfluenceContext(byKey, influenceContextOperation, value, problem)
		}
		for _, value := range node.Routes {
			addInfluenceContext(byKey, influenceContextRoute, value, problem)
		}
	}
	ordered := make([]influenceContextCandidate, 0, min(len(byKey), workspaceMaxContexts))
	for key, counts := range byKey {
		candidate := influenceContextCandidate{key: key, counts: counts}
		if len(ordered) < workspaceMaxContexts {
			ordered = append(ordered, candidate)
		} else if !influenceContextCandidateLess(candidate, ordered[len(ordered)-1]) {
			continue
		} else {
			ordered[len(ordered)-1] = candidate
		}
		for position := len(ordered) - 1; position > 0 && influenceContextCandidateLess(ordered[position], ordered[position-1]); position-- {
			ordered[position], ordered[position-1] = ordered[position-1], ordered[position]
		}
	}
	contexts := make([]InfluenceGraphContext, len(ordered))
	for index, candidate := range ordered {
		kind := candidate.key.kind.name()
		contexts[index] = InfluenceGraphContext{
			ID:           "context:" + kind + ":" + candidate.key.value,
			Kind:         kind,
			Value:        candidate.key.value,
			RuntimeNodes: candidate.counts.runtimeNodes,
			ProblemNodes: candidate.counts.problemNodes,
		}
	}
	return influenceContextSelection{items: contexts, total: len(byKey)}
}

func addInfluenceContext(byKey map[influenceContextKey]influenceContextCounts, kind influenceContextKind, value string, problem bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	key := influenceContextKey{value: value, kind: kind}
	counts := byKey[key]
	counts.runtimeNodes++
	if problem {
		counts.problemNodes++
	}
	byKey[key] = counts
}

func influenceContextCandidateLess(left, right influenceContextCandidate) bool {
	if left.counts.problemNodes != right.counts.problemNodes {
		return left.counts.problemNodes > right.counts.problemNodes
	}
	if left.counts.runtimeNodes != right.counts.runtimeNodes {
		return left.counts.runtimeNodes > right.counts.runtimeNodes
	}
	if left.key.kind != right.key.kind {
		return left.key.kind < right.key.kind
	}
	return left.key.value < right.key.value
}

func (kind influenceContextKind) name() string {
	switch kind {
	case influenceContextScreen:
		return "screen"
	case influenceContextOperation:
		return "operation"
	case influenceContextRoute:
		return "route"
	default:
		return "unknown"
	}
}

func (b *influenceViewBuilder) workspace(contexts influenceContextSelection) InfluenceGraphWorkspace {
	nodes := make([]InfluenceGraphNode, 0, min(len(b.nodes), workspaceMaxNodes))
	selected := map[string]struct{}{}
	for _, node := range b.nodes {
		if len(nodes) >= workspaceMaxNodes {
			break
		}
		nodes = append(nodes, b.classNode(node, false))
		selected[node.ClassName] = struct{}{}
	}
	edges := make([]InfluenceGraphEdge, 0, min(len(b.edges), workspaceMaxEdges))
	for _, edge := range b.edges {
		if len(edges) >= workspaceMaxEdges {
			break
		}
		if _, fromOK := selected[edge.From]; !fromOK {
			continue
		}
		if _, toOK := selected[edge.To]; !toOK {
			continue
		}
		edges = append(edges, graphEdge(edge, false))
	}
	shownContexts := contexts.items
	reasons := []string{}
	if len(nodes) < len(b.nodes) {
		reasons = append(reasons, fmt.Sprintf("область поиска ограничена %d наиболее приоритетными классами", workspaceMaxNodes))
	}
	if len(edges) < len(b.edges) {
		reasons = append(reasons, fmt.Sprintf("область поиска ограничена %d наиболее значимыми связями", workspaceMaxEdges))
	}
	if len(shownContexts) < contexts.total {
		reasons = append(reasons, fmt.Sprintf("показаны %d из %d контекстов", len(shownContexts), contexts.total))
	}
	return InfluenceGraphWorkspace{
		Nodes:           nodes,
		Edges:           edges,
		TotalNodes:      len(b.nodes),
		TotalEdges:      len(b.edges),
		ShownNodes:      len(nodes),
		ShownEdges:      len(edges),
		OmittedNodes:    len(b.nodes) - len(nodes),
		OmittedEdges:    len(b.edges) - len(edges),
		Contexts:        append([]InfluenceGraphContext(nil), shownContexts...),
		TotalContexts:   contexts.total,
		ShownContexts:   len(shownContexts),
		OmissionReasons: reasons,
	}
}

// boundedClassNodes ranks lightweight references before materializing breadcrumbs, explanations
// and runtime paths. Views expose only a small bounded subset, so projecting every candidate first
// wastes tens of megabytes on large graphs and then immediately discards almost all of it.
func (b *influenceViewBuilder) boundedClassNodes(candidates map[string]bool, limit int) []InfluenceGraphNode {
	ordered := make([]influenceClassCandidate, 0, len(candidates))
	for className, connector := range candidates {
		slot := b.nodeByID[className]
		if slot == 0 {
			continue
		}
		ordered = append(ordered, influenceClassCandidate{node: &b.nodes[slot-1], connector: connector})
	}
	sort.Slice(ordered, func(i, j int) bool {
		return influenceClassCandidateLess(ordered[i], ordered[j])
	})
	if limit > 0 && len(ordered) > limit {
		ordered = ordered[:limit]
	}
	nodes := make([]InfluenceGraphNode, 0, len(ordered))
	for _, candidate := range ordered {
		nodes = append(nodes, b.classNode(*candidate.node, candidate.connector))
	}
	return nodes
}

func influenceGraphNodeIDs(nodes []InfluenceGraphNode) map[string]struct{} {
	selected := make(map[string]struct{}, len(nodes))
	for index := range nodes {
		selected[nodes[index].ID] = struct{}{}
	}
	return selected
}

func influenceClassCandidateLess(left, right influenceClassCandidate) bool {
	if left.node.Score != right.node.Score {
		return left.node.Score > right.node.Score
	}
	if left.node.RuntimeEvidence != right.node.RuntimeEvidence {
		return left.node.RuntimeEvidence
	}
	if left.connector != right.connector {
		return !left.connector
	}
	return left.node.ClassName < right.node.ClassName
}

func (b *influenceViewBuilder) classNode(node InfluenceNode, connector bool) InfluenceGraphNode {
	kind := "class"
	if connector {
		kind = "connector"
	}
	projection, ok := b.classNodes[node.ClassName]
	if !ok {
		projection = influenceClassNodeProjection{
			packageName: influencePackage(node.ClassName, 0),
			breadcrumbs: influenceBreadcrumbs(node.ClassName),
			explanation: b.nodeExplanation(node),
		}
		b.classNodes[node.ClassName] = projection
	}
	return InfluenceGraphNode{
		InfluenceNode: node,
		ID:            node.ClassName,
		Kind:          kind,
		Package:       projection.packageName,
		Breadcrumbs:   projection.breadcrumbs,
		Connector:     connector,
		Explanation:   projection.explanation,
	}
}

func (b *influenceViewBuilder) nodeExplanation(node InfluenceNode) string {
	if !node.RuntimeEvidence {
		return fmt.Sprintf("%s известен только из статического графа. Выполнение класса в этом прогоне не подтверждено; оценка %.1f используется только для приоритизации.", node.Label, node.Score)
	}
	reasons := make([]string, 0, 6)
	if node.Problems > 0 {
		reasons = append(reasons, fmt.Sprintf("%d проблемных сигналов", node.Problems))
	}
	if node.LogSpam > 0 {
		reasons = append(reasons, fmt.Sprintf("%d вызовов шумных логов", node.LogSpam))
	}
	if node.RuntimeWallMS > 0 {
		reasons = append(reasons, fmt.Sprintf("%d мс времени выполнения", node.RuntimeWallMS))
	}
	if node.MainThreadMS > 0 {
		reasons = append(reasons, fmt.Sprintf("%d мс работы главного потока", node.MainThreadMS))
	}
	if node.NetworkMS > 0 {
		reasons = append(reasons, fmt.Sprintf("%d мс сетевого p95", node.NetworkMS))
	}
	if node.Retained > 0 {
		label := fmt.Sprintf("%d сигналов удержания", node.Retained)
		if node.HeapEvidence {
			label += " с данными HPROF"
		}
		reasons = append(reasons, label)
	}
	detail := "наблюдения выполнения класса"
	if len(reasons) > 0 {
		detail = strings.Join(reasons, ", ")
	}
	explanation := fmt.Sprintf("%s получил оценку риска %.1f из-за фактических данных: %s. Оценка является эвристикой приоритизации.", node.Label, node.Score, detail)
	path := b.confirmedRuntimePath(node.ClassName)
	if len(path) > 1 {
		explanation += " Подтверждённый путь выполнения: " + strings.Join(path, " → ") + "."
	}
	return explanation
}

func (b *influenceViewBuilder) confirmedRuntimePath(target string) []string {
	path := []string{target}
	seen := map[string]struct{}{target: {}}
	current := target
	for len(path) < 5 {
		incomingEdges := b.incoming.edgesForSlot(b.nodeByID[current])
		incoming := make([]uint32, 0, len(incomingEdges))
		for _, edgeIndex := range incomingEdges {
			edge := b.edges[edgeIndex]
			if edge.RuntimeCount > 0 {
				incoming = append(incoming, edgeIndex)
			}
		}
		if len(incoming) == 0 {
			break
		}
		sort.Slice(incoming, func(i, j int) bool {
			left := b.edges[incoming[i]]
			right := b.edges[incoming[j]]
			if left.RuntimeCount != right.RuntimeCount {
				return left.RuntimeCount > right.RuntimeCount
			}
			return left.From < right.From
		})
		next := ""
		for _, edgeIndex := range incoming {
			edge := b.edges[edgeIndex]
			if _, duplicate := seen[edge.From]; !duplicate {
				next = edge.From
				break
			}
		}
		if next == "" {
			break
		}
		seen[next] = struct{}{}
		path = append(path, next)
		current = next
	}
	for left, right := 0, len(path)-1; left < right; left, right = left+1, right-1 {
		path[left], path[right] = path[right], path[left]
	}
	return path
}

func (b *influenceViewBuilder) defaultNode() string {
	for _, node := range b.nodes {
		if isInfluenceProblem(node) {
			return node.ClassName
		}
	}
	if len(b.nodes) > 0 {
		return b.nodes[0].ClassName
	}
	return ""
}

func boundedInfluenceViewWithTotals(
	id string,
	mode string,
	title string,
	explanation string,
	filters InfluenceGraphFilters,
	nodes []InfluenceGraphNode,
	edges []InfluenceGraphEdge,
	totalNodes int,
	totalEdges int,
	limits InfluenceGraphLimits,
) InfluenceGraphView {
	sort.Slice(nodes, func(i, j int) bool {
		return graphNodeLess(nodes[i], nodes[j])
	})
	sortGraphEdges(edges)
	if limits.MaxNodes > 0 && len(nodes) > limits.MaxNodes {
		nodes = append([]InfluenceGraphNode(nil), nodes[:limits.MaxNodes]...)
	}
	selected := map[string]struct{}{}
	for _, node := range nodes {
		selected[node.ID] = struct{}{}
	}
	visibleEdges := make([]InfluenceGraphEdge, 0, min(len(edges), limits.MaxEdges))
	for _, edge := range edges {
		if _, fromOK := selected[edge.From]; !fromOK {
			continue
		}
		if _, toOK := selected[edge.To]; !toOK {
			continue
		}
		if limits.MaxEdges > 0 && len(visibleEdges) >= limits.MaxEdges {
			break
		}
		visibleEdges = append(visibleEdges, edge)
	}
	reasons := []string{}
	if len(nodes) < totalNodes {
		reasons = append(reasons, fmt.Sprintf("лимит представления: %d узлов", limits.MaxNodes))
	}
	if len(visibleEdges) < totalEdges {
		reasons = append(reasons, fmt.Sprintf("часть связей исключена лимитом %d или вместе с исключенными узлами", limits.MaxEdges))
	}
	return InfluenceGraphView{
		ID:              id,
		Mode:            mode,
		Title:           title,
		Explanation:     explanation,
		Filters:         filters,
		Nodes:           append([]InfluenceGraphNode(nil), nodes...),
		Edges:           visibleEdges,
		TotalNodes:      totalNodes,
		TotalEdges:      totalEdges,
		ShownNodes:      len(nodes),
		ShownEdges:      len(visibleEdges),
		OmittedNodes:    totalNodes - len(nodes),
		OmittedEdges:    totalEdges - len(visibleEdges),
		OmissionReasons: reasons,
		Limits:          limits,
		Legend:          influenceGraphLegend(),
	}
}

func graphEdge(edge InfluenceEdge, aggregate bool) InfluenceGraphEdge {
	edge = normalizeInfluenceEvidence(edge)
	return InfluenceGraphEdge{
		InfluenceEdge: edge,
		ID:            edge.From + "→" + edge.To,
		Aggregate:     aggregate,
	}
}

func normalizeInfluenceEvidence(edge InfluenceEdge) InfluenceEdge {
	edge.RuntimeConfirmed = edge.RuntimeCount > 0
	switch {
	case edge.RuntimeCount > 0 && edge.StaticCount > 0:
		edge.Evidence = "mixed"
		edge.Reason = "вызов при выполнении подтверждён; статический граф также содержит эту связь"
	case edge.RuntimeCount > 0:
		edge.Evidence = "runtime"
		edge.Reason = "вызов записан во время этого прогона"
	default:
		edge.Evidence = "static"
		edge.Reason = "связь известна только из статического графа"
	}
	return edge
}

func graphNodeLess(left InfluenceGraphNode, right InfluenceGraphNode) bool {
	if left.Score != right.Score {
		return left.Score > right.Score
	}
	if left.ProblemClassCount != right.ProblemClassCount {
		return left.ProblemClassCount > right.ProblemClassCount
	}
	if left.RuntimeEvidence != right.RuntimeEvidence {
		return left.RuntimeEvidence
	}
	if left.Connector != right.Connector {
		return !left.Connector
	}
	return left.ID < right.ID
}

func influenceNodeLess(left InfluenceNode, right InfluenceNode) bool {
	if left.Score != right.Score {
		return left.Score > right.Score
	}
	if left.RuntimeEvidence != right.RuntimeEvidence {
		return left.RuntimeEvidence
	}
	return left.ClassName < right.ClassName
}

func sortInfluenceEdges(edges []InfluenceEdge) {
	sort.Slice(edges, func(i, j int) bool {
		return influenceEdgeLess(edges[i], edges[j])
	})
}

func influenceEdgeLess(left, right InfluenceEdge) bool {
	if left.Influence != right.Influence {
		return left.Influence > right.Influence
	}
	if left.RuntimeCount != right.RuntimeCount {
		return left.RuntimeCount > right.RuntimeCount
	}
	if left.From != right.From {
		return left.From < right.From
	}
	return left.To < right.To
}

func sortGraphEdges(edges []InfluenceGraphEdge) {
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].Influence != edges[j].Influence {
			return edges[i].Influence > edges[j].Influence
		}
		if edges[i].RuntimeCount != edges[j].RuntimeCount {
			return edges[i].RuntimeCount > edges[j].RuntimeCount
		}
		if edges[i].From != edges[j].From {
			return edges[i].From < edges[j].From
		}
		return edges[i].To < edges[j].To
	})
}

func influencePackage(className string, depth int) string {
	className = strings.TrimSpace(className)
	packageEnd := strings.LastIndexByte(className, '.')
	if packageEnd < 0 {
		return "без пакета"
	}
	if depth > 0 {
		separators := 0
		for index := 0; index < packageEnd; index++ {
			if className[index] != '.' {
				continue
			}
			separators++
			if separators == depth {
				packageEnd = index
				break
			}
		}
	}
	return className[:packageEnd]
}

func influenceBreadcrumbs(className string) []string {
	packageName := influencePackage(className, 0)
	label := shortClassName(className)
	if packageName == "без пакета" {
		return []string{label}
	}
	return []string{packageName, label}
}

func isInfluenceProblem(node InfluenceNode) bool {
	return node.Score > 0 || node.Problems > 0 || node.LogSpam > 0 || node.MainThreadMS > 0 || node.NetworkMS > 0 || node.UIJank > 0 || node.Retained > 0
}

func influenceNodeMatchesContext(node InfluenceNode, kind string, value string) bool {
	switch kind {
	case "screen":
		return containsInfluenceValue(node.Screens, value)
	case "operation":
		return containsInfluenceValue(node.Operations, value)
	case "route":
		return containsInfluenceValue(node.Routes, value)
	default:
		return false
	}
}

func containsInfluenceValue(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func influenceGraphLegend() []InfluenceGraphLegend {
	return []InfluenceGraphLegend{
		{Kind: "runtime", Label: "Выполнение", Help: "Вызов записан во время анализируемого прогона."},
		{Kind: "static", Label: "Только статика", Help: "Связь возможна по байткоду, но её выполнение не записано."},
		{Kind: "mixed", Label: "Выполнение + статика", Help: "Выполнение подтверждает часть агрегированных или совпавших связей; счётчики показаны отдельно."},
		{Kind: "connector", Label: "Связующий класс", Help: "Класс соединяет контекстные узлы, но не считается прямым участником экрана или сценария."},
		{Kind: "aggregate", Label: "Пакет", Help: "Группа классов; оценка равна максимуму дочернего класса."},
		{Kind: "hprof", Label: "HPROF-дамп", Help: "Есть отдельное подтверждение пути удержания из дампа памяти."},
	}
}

func roundedInfluence(value float64) float64 {
	return math.Round(value*10) / 10
}
