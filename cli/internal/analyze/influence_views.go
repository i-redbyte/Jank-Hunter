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
	nodes      []InfluenceNode
	edges      []InfluenceEdge
	nodeByID   map[string]*InfluenceNode
	outgoing   map[string][]InfluenceEdge
	incoming   map[string][]InfluenceEdge
	classNodes map[string]influenceClassNodeProjection
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
	name        string
	children    []string
	childCount  int
	maxClass    string
	maxScore    float64
	maxSeverity string
	runtime     int
	staticOnly  int
	problems    int
}

type influencePackageEdgeKey struct {
	from *influencePackageAggregate
	to   *influencePackageAggregate
}

type influencePackageEdgeAggregate struct {
	count        uint64
	runtimeCount uint64
	staticCount  uint64
	influence    float64
}

func buildInfluenceViews(nodes []InfluenceNode, edges []InfluenceEdge) ([]InfluenceGraphView, InfluenceGraphWorkspace) {
	builder := newInfluenceViewBuilder(nodes, edges)
	views := []InfluenceGraphView{
		builder.problemsView(),
		builder.runtimeView(),
	}
	for depth := 2; depth <= 5; depth++ {
		views = append(views, builder.packagesView(depth))
	}
	views = append(views, builder.neighborhoodView(builder.defaultNode(), "both", 1, false))
	contexts := builder.contexts()
	for index, context := range contexts {
		if index >= workspaceMaxContexts {
			break
		}
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
	builder := &influenceViewBuilder{
		nodes:      nodeCopy,
		edges:      edgeCopy,
		nodeByID:   map[string]*InfluenceNode{},
		outgoing:   map[string][]InfluenceEdge{},
		incoming:   map[string][]InfluenceEdge{},
		classNodes: map[string]influenceClassNodeProjection{},
	}
	for index := range nodeCopy {
		node := &nodeCopy[index]
		builder.nodeByID[node.ClassName] = node
	}
	for _, edge := range edgeCopy {
		builder.outgoing[edge.From] = append(builder.outgoing[edge.From], edge)
		builder.incoming[edge.To] = append(builder.incoming[edge.To], edge)
	}
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
	candidateEdges := make([]InfluenceGraphEdge, 0)
	for _, edge := range b.edges {
		_, fromProblem := problemNodes[edge.From]
		_, toProblem := problemNodes[edge.To]
		if !fromProblem && !toProblem {
			continue
		}
		candidateEdges = append(candidateEdges, graphEdge(edge, false))
		for _, className := range []string{edge.From, edge.To} {
			if _, exists := candidates[className]; exists {
				continue
			}
			if _, ok := b.nodeByID[className]; ok {
				candidates[className] = true
			}
		}
	}
	return boundedInfluenceViewWithTotals(
		"problems",
		"problems",
		"Проблемы",
		"Классы с наибольшей оценкой риска и ближайшие связующие классы. Оценка задаёт порядок проверки и не доказывает причину.",
		InfluenceGraphFilters{},
		b.boundedClassNodes(candidates, problemViewMaxNodes),
		candidateEdges,
		len(candidates),
		len(candidateEdges),
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
	edges := make([]InfluenceGraphEdge, 0)
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
		edges = append(edges, graphEdge(edge, false))
	}
	return boundedInfluenceViewWithTotals(
		"runtime",
		"runtime",
		"Только выполнение",
		"Только классы и связи, реально записанные во время этого прогона. Изолированные классы с наблюдаемыми симптомами сохраняются.",
		InfluenceGraphFilters{RuntimeOnly: true},
		b.boundedClassNodes(candidates, runtimeViewMaxNodes),
		edges,
		len(candidates),
		len(edges),
		InfluenceGraphLimits{MaxNodes: runtimeViewMaxNodes, MaxEdges: runtimeViewMaxEdges},
	)
}

func (b *influenceViewBuilder) packagesView(depth int) InfluenceGraphView {
	aggregates := map[string]*influencePackageAggregate{}
	packageByClass := map[string]*influencePackageAggregate{}
	for _, node := range b.nodes {
		packageName := influencePackage(node.ClassName, depth)
		aggregate := aggregates[packageName]
		if aggregate == nil {
			aggregate = &influencePackageAggregate{name: packageName}
			aggregates[packageName] = aggregate
		}
		packageByClass[node.ClassName] = aggregate
		aggregate.childCount++
		aggregate.addChildSample(node)
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
		if isInfluenceProblem(node) {
			aggregate.problems++
		}
	}
	orderedAggregates := make([]*influencePackageAggregate, 0, len(aggregates))
	for _, aggregate := range aggregates {
		orderedAggregates = append(orderedAggregates, aggregate)
	}
	sort.Slice(orderedAggregates, func(i, j int) bool {
		left := orderedAggregates[i]
		right := orderedAggregates[j]
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
	selectedPackages := make(map[*influencePackageAggregate]struct{}, len(orderedAggregates))
	for _, aggregate := range orderedAggregates {
		packageID := "package:" + aggregate.name
		selectedPackages[aggregate] = struct{}{}
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
			Children:             append([]string(nil), aggregate.children...),
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
		fromPackage := packageByClass[edge.From]
		toPackage := packageByClass[edge.To]
		if fromPackage == nil || toPackage == nil || fromPackage == toPackage {
			continue
		}
		key := influencePackageEdgeKey{from: fromPackage, to: toPackage}
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
		if _, ok := selectedPackages[key.from]; !ok {
			continue
		}
		if _, ok := selectedPackages[key.to]; !ok {
			continue
		}
		edge := InfluenceEdge{
			From:         "package:" + key.from.name,
			To:           "package:" + key.to.name,
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

func (a *influencePackageAggregate) addChildSample(node InfluenceNode) {
	if len(a.children) < packageChildSample {
		a.children = append(a.children, node.ClassName)
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
		adjacent := make([]InfluenceEdge, 0)
		if direction == "outgoing" || direction == "both" {
			adjacent = append(adjacent, b.outgoing[current]...)
		}
		if direction == "incoming" || direction == "both" {
			adjacent = append(adjacent, b.incoming[current]...)
		}
		sortInfluenceEdges(adjacent)
		for _, edge := range adjacent {
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
	targets := map[string]struct{}{}
	for _, node := range b.nodes {
		if influenceNodeMatchesContext(node, context.Kind, context.Value) {
			targets[node.ClassName] = struct{}{}
		}
	}
	inFromTarget := map[string]bool{}
	outToTarget := map[string]bool{}
	for _, edge := range b.edges {
		if _, ok := targets[edge.From]; ok {
			inFromTarget[edge.To] = true
		}
		if _, ok := targets[edge.To]; ok {
			outToTarget[edge.From] = true
		}
	}
	connectors := map[string]struct{}{}
	for className := range inFromTarget {
		if outToTarget[className] {
			if _, target := targets[className]; !target {
				connectors[className] = struct{}{}
			}
		}
	}
	candidates := map[string]bool{}
	for className := range targets {
		if _, ok := b.nodeByID[className]; ok {
			candidates[className] = false
		}
	}
	for className := range connectors {
		if _, ok := b.nodeByID[className]; ok {
			candidates[className] = true
		}
	}
	edges := make([]InfluenceGraphEdge, 0)
	for _, edge := range b.edges {
		if _, fromOK := candidates[edge.From]; !fromOK {
			continue
		}
		if _, toOK := candidates[edge.To]; !toOK {
			continue
		}
		_, fromTarget := targets[edge.From]
		_, toTarget := targets[edge.To]
		if !fromTarget && !toTarget {
			continue
		}
		edges = append(edges, graphEdge(edge, false))
	}
	return boundedInfluenceViewWithTotals(
		context.ID,
		"context",
		"Экран / операция",
		"Классы с записанным контекстом и необходимые связующие классы на расстоянии до двух связей. Связующий класс не считается непосредственным участником экрана, операции или маршрута.",
		InfluenceGraphFilters{ContextKind: context.Kind, ContextValue: context.Value},
		b.boundedClassNodes(candidates, contextViewMaxNodes),
		edges,
		len(candidates),
		len(edges),
		InfluenceGraphLimits{MaxNodes: contextViewMaxNodes, MaxEdges: contextViewMaxEdges},
	)
}

func (b *influenceViewBuilder) contexts() []InfluenceGraphContext {
	byKey := map[string]*InfluenceGraphContext{}
	add := func(kind string, value string, problem bool) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		key := kind + "\x00" + value
		context := byKey[key]
		if context == nil {
			context = &InfluenceGraphContext{ID: "context:" + kind + ":" + value, Kind: kind, Value: value}
			byKey[key] = context
		}
		context.RuntimeNodes++
		if problem {
			context.ProblemNodes++
		}
	}
	for _, node := range b.nodes {
		if !node.RuntimeEvidence {
			continue
		}
		problem := isInfluenceProblem(node)
		for _, value := range node.Screens {
			add("screen", value, problem)
		}
		for _, value := range node.Operations {
			add("operation", value, problem)
		}
		for _, value := range node.Routes {
			add("route", value, problem)
		}
	}
	contexts := make([]InfluenceGraphContext, 0, len(byKey))
	for _, context := range byKey {
		contexts = append(contexts, *context)
	}
	sort.Slice(contexts, func(i, j int) bool {
		if contexts[i].ProblemNodes != contexts[j].ProblemNodes {
			return contexts[i].ProblemNodes > contexts[j].ProblemNodes
		}
		if contexts[i].RuntimeNodes != contexts[j].RuntimeNodes {
			return contexts[i].RuntimeNodes > contexts[j].RuntimeNodes
		}
		if contexts[i].Kind != contexts[j].Kind {
			return contexts[i].Kind < contexts[j].Kind
		}
		return contexts[i].Value < contexts[j].Value
	})
	return contexts
}

func (b *influenceViewBuilder) workspace(contexts []InfluenceGraphContext) InfluenceGraphWorkspace {
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
	shownContexts := contexts
	if len(shownContexts) > workspaceMaxContexts {
		shownContexts = shownContexts[:workspaceMaxContexts]
	}
	reasons := []string{}
	if len(nodes) < len(b.nodes) {
		reasons = append(reasons, fmt.Sprintf("область поиска ограничена %d наиболее приоритетными классами", workspaceMaxNodes))
	}
	if len(edges) < len(b.edges) {
		reasons = append(reasons, fmt.Sprintf("область поиска ограничена %d наиболее значимыми связями", workspaceMaxEdges))
	}
	if len(shownContexts) < len(contexts) {
		reasons = append(reasons, fmt.Sprintf("показаны %d из %d контекстов", len(shownContexts), len(contexts)))
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
		TotalContexts:   len(contexts),
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
		node := b.nodeByID[className]
		if node == nil {
			continue
		}
		ordered = append(ordered, influenceClassCandidate{node: node, connector: connector})
	}
	sort.Slice(ordered, func(i, j int) bool {
		left := ordered[i]
		right := ordered[j]
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
		incoming := make([]InfluenceEdge, 0)
		for _, edge := range b.incoming[current] {
			if edge.RuntimeCount > 0 {
				incoming = append(incoming, edge)
			}
		}
		if len(incoming) == 0 {
			break
		}
		sort.Slice(incoming, func(i, j int) bool {
			if incoming[i].RuntimeCount != incoming[j].RuntimeCount {
				return incoming[i].RuntimeCount > incoming[j].RuntimeCount
			}
			return incoming[i].From < incoming[j].From
		})
		next := ""
		for _, edge := range incoming {
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

func boundedInfluenceView(
	id string,
	mode string,
	title string,
	explanation string,
	filters InfluenceGraphFilters,
	nodes []InfluenceGraphNode,
	edges []InfluenceGraphEdge,
	limits InfluenceGraphLimits,
) InfluenceGraphView {
	return boundedInfluenceViewWithTotals(
		id,
		mode,
		title,
		explanation,
		filters,
		nodes,
		edges,
		len(nodes),
		len(edges),
		limits,
	)
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
	parts := strings.Split(strings.TrimSpace(className), ".")
	if len(parts) <= 1 {
		return "без пакета"
	}
	packageParts := parts[:len(parts)-1]
	if depth > 0 && len(packageParts) > depth {
		packageParts = packageParts[:depth]
	}
	return strings.Join(packageParts, ".")
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
