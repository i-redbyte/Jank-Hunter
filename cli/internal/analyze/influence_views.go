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
	nodes    []InfluenceNode
	edges    []InfluenceEdge
	nodeByID map[string]InfluenceNode
	outgoing map[string][]InfluenceEdge
	incoming map[string][]InfluenceEdge
}

type influencePackageAggregate struct {
	name       string
	children   []InfluenceNode
	maxNode    InfluenceNode
	runtime    int
	staticOnly int
	problems   int
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

func EnsureInfluenceViews(summary InfluenceSummary) InfluenceSummary {
	if len(summary.Views) > 0 {
		return summary
	}
	edges := append([]InfluenceEdge(nil), summary.TopEdges...)
	for index := range edges {
		if edges[index].RuntimeCount == 0 && edges[index].StaticCount == 0 {
			if edges[index].RuntimeConfirmed {
				edges[index].RuntimeCount = edges[index].Count
			} else {
				edges[index].StaticCount = edges[index].Count
			}
		}
		edges[index] = normalizeInfluenceEvidence(edges[index])
	}
	summary.Views, summary.Workspace = buildInfluenceViews(summary.TopNodes, edges)
	if summary.TotalNodes == 0 {
		summary.TotalNodes = len(summary.TopNodes)
	}
	if summary.TotalEdges == 0 {
		summary.TotalEdges = len(edges)
	}
	return summary
}

func newInfluenceViewBuilder(nodes []InfluenceNode, edges []InfluenceEdge) *influenceViewBuilder {
	nodeCopy := append([]InfluenceNode(nil), nodes...)
	edgeCopy := append([]InfluenceEdge(nil), edges...)
	sort.Slice(nodeCopy, func(i, j int) bool {
		return influenceNodeLess(nodeCopy[i], nodeCopy[j])
	})
	sortInfluenceEdges(edgeCopy)
	builder := &influenceViewBuilder{
		nodes:    nodeCopy,
		edges:    edgeCopy,
		nodeByID: map[string]InfluenceNode{},
		outgoing: map[string][]InfluenceEdge{},
		incoming: map[string][]InfluenceEdge{},
	}
	for _, node := range nodeCopy {
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
	candidates := map[string]InfluenceGraphNode{}
	for _, node := range b.nodes {
		if !isInfluenceProblem(node) {
			continue
		}
		problemNodes[node.ClassName] = struct{}{}
		candidates[node.ClassName] = b.classNode(node, false)
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
			if node, ok := b.nodeByID[className]; ok {
				candidates[className] = b.classNode(node, true)
			}
		}
	}
	return boundedInfluenceView(
		"problems",
		"problems",
		"Проблемы",
		"Классы с наибольшей оценкой риска и ближайшие связующие классы. Оценка задаёт порядок проверки и не доказывает причину.",
		InfluenceGraphFilters{},
		graphNodeValues(candidates),
		candidateEdges,
		InfluenceGraphLimits{MaxNodes: problemViewMaxNodes, MaxEdges: problemViewMaxEdges},
	)
}

func (b *influenceViewBuilder) runtimeView() InfluenceGraphView {
	candidates := map[string]InfluenceGraphNode{}
	for _, node := range b.nodes {
		if node.RuntimeEvidence {
			candidates[node.ClassName] = b.classNode(node, false)
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
	return boundedInfluenceView(
		"runtime",
		"runtime",
		"Только выполнение",
		"Только классы и связи, реально записанные во время этого прогона. Изолированные классы с наблюдаемыми симптомами сохраняются.",
		InfluenceGraphFilters{RuntimeOnly: true},
		graphNodeValues(candidates),
		edges,
		InfluenceGraphLimits{MaxNodes: runtimeViewMaxNodes, MaxEdges: runtimeViewMaxEdges},
	)
}

func (b *influenceViewBuilder) packagesView(depth int) InfluenceGraphView {
	aggregates := map[string]*influencePackageAggregate{}
	packageByClass := map[string]string{}
	for _, node := range b.nodes {
		packageName := influencePackage(node.ClassName, depth)
		packageByClass[node.ClassName] = packageName
		aggregate := aggregates[packageName]
		if aggregate == nil {
			aggregate = &influencePackageAggregate{name: packageName}
			aggregates[packageName] = aggregate
		}
		aggregate.children = append(aggregate.children, node)
		if aggregate.maxNode.ClassName == "" || influenceNodeLess(node, aggregate.maxNode) {
			aggregate.maxNode = node
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
	nodes := make([]InfluenceGraphNode, 0, len(aggregates))
	for _, aggregate := range aggregates {
		sort.Slice(aggregate.children, func(i, j int) bool {
			return influenceNodeLess(aggregate.children[i], aggregate.children[j])
		})
		children := make([]string, 0, minInt(len(aggregate.children), packageChildSample))
		for index, child := range aggregate.children {
			if index >= packageChildSample {
				break
			}
			children = append(children, child.ClassName)
		}
		maxNode := aggregate.maxNode
		nodes = append(nodes, InfluenceGraphNode{
			InfluenceNode: InfluenceNode{
				ClassName:       aggregate.name,
				Label:           aggregate.name,
				Score:           maxNode.Score,
				Severity:        maxNode.Severity,
				Status:          "aggregate",
				RuntimeEvidence: aggregate.runtime > 0,
			},
			ID:                   "package:" + aggregate.name,
			Kind:                 "package",
			Package:              aggregate.name,
			Breadcrumbs:          strings.Split(aggregate.name, "."),
			Aggregate:            true,
			ChildCount:           len(aggregate.children),
			RuntimeClassCount:    aggregate.runtime,
			StaticOnlyClassCount: aggregate.staticOnly,
			ProblemClassCount:    aggregate.problems,
			Children:             children,
			Explanation: fmt.Sprintf(
				"Пакет %s объединяет %d классов. Оценка %.1f равна максимальной оценке дочернего класса %s, а не сумме.",
				aggregate.name,
				len(aggregate.children),
				maxNode.Score,
				maxNode.ClassName,
			),
		})
	}
	edgeByKey := map[string]InfluenceEdge{}
	for _, edge := range b.edges {
		fromPackage := packageByClass[edge.From]
		toPackage := packageByClass[edge.To]
		if fromPackage == "" || toPackage == "" || fromPackage == toPackage {
			continue
		}
		key := fromPackage + "\x00" + toPackage
		aggregated := edgeByKey[key]
		aggregated.From = "package:" + fromPackage
		aggregated.To = "package:" + toPackage
		aggregated.RuntimeCount += edge.RuntimeCount
		aggregated.StaticCount += edge.StaticCount
		aggregated.Count += edge.Count
		aggregated.Influence += edge.Influence
		edgeByKey[key] = aggregated
	}
	edges := make([]InfluenceGraphEdge, 0, len(edgeByKey))
	for _, edge := range edgeByKey {
		edge = normalizeInfluenceEvidence(edge)
		edges = append(edges, graphEdge(edge, true))
	}
	return boundedInfluenceView(
		fmt.Sprintf("packages:%d", depth),
		"packages",
		"Пакеты",
		"Классы сгруппированы по пакету. Размер и уровень риска группы определяются максимальной оценкой дочернего класса; записанные и статические связи считаются отдельно.",
		InfluenceGraphFilters{PackageDepth: depth},
		nodes,
		edges,
		InfluenceGraphLimits{MaxNodes: packageViewMaxNodes, MaxEdges: packageViewMaxEdges},
	)
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
	nodes := make([]InfluenceGraphNode, 0, len(reached))
	for className := range reached {
		if node, ok := b.nodeByID[className]; ok {
			nodes = append(nodes, b.classNode(node, false))
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
	return boundedInfluenceView(
		"neighborhood",
		"neighborhood",
		"Окрестность",
		"Безопасный для циклов обход выбранного класса. Глубина и направление меняют только состав представления, но не оценку и исходные метрики.",
		InfluenceGraphFilters{SelectedNode: selected, Direction: direction, Depth: depth, RuntimeOnly: runtimeOnly},
		nodes,
		edges,
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
	candidates := map[string]InfluenceGraphNode{}
	for className := range targets {
		if node, ok := b.nodeByID[className]; ok {
			candidates[className] = b.classNode(node, false)
		}
	}
	for className := range connectors {
		if node, ok := b.nodeByID[className]; ok {
			candidates[className] = b.classNode(node, true)
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
	return boundedInfluenceView(
		context.ID,
		"context",
		"Экран / Flow",
		"Классы с записанным контекстом и необходимые связующие классы на расстоянии до двух шагов. Связующий класс не считается непосредственным участником экрана, сценария или маршрута.",
		InfluenceGraphFilters{ContextKind: context.Kind, ContextValue: context.Value},
		graphNodeValues(candidates),
		edges,
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
		for _, value := range node.Flows {
			add("flow", value, problem)
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
	nodes := make([]InfluenceGraphNode, 0, minInt(len(b.nodes), workspaceMaxNodes))
	selected := map[string]struct{}{}
	for _, node := range b.nodes {
		if len(nodes) >= workspaceMaxNodes {
			break
		}
		nodes = append(nodes, b.classNode(node, false))
		selected[node.ClassName] = struct{}{}
	}
	edges := make([]InfluenceGraphEdge, 0, minInt(len(b.edges), workspaceMaxEdges))
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

func (b *influenceViewBuilder) classNode(node InfluenceNode, connector bool) InfluenceGraphNode {
	kind := "class"
	if connector {
		kind = "connector"
	}
	packageName := influencePackage(node.ClassName, 0)
	return InfluenceGraphNode{
		InfluenceNode: node,
		ID:            node.ClassName,
		Kind:          kind,
		Package:       packageName,
		Breadcrumbs:   influenceBreadcrumbs(node.ClassName),
		Connector:     connector,
		Explanation:   b.nodeExplanation(node),
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
			label += " с HPROF evidence"
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
	sort.Slice(nodes, func(i, j int) bool {
		return graphNodeLess(nodes[i], nodes[j])
	})
	sortGraphEdges(edges)
	totalNodes := len(nodes)
	totalEdges := len(edges)
	if limits.MaxNodes > 0 && len(nodes) > limits.MaxNodes {
		nodes = append([]InfluenceGraphNode(nil), nodes[:limits.MaxNodes]...)
	}
	selected := map[string]struct{}{}
	for _, node := range nodes {
		selected[node.ID] = struct{}{}
	}
	visibleEdges := make([]InfluenceGraphEdge, 0, minInt(len(edges), limits.MaxEdges))
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
		edge.Reason = "runtime-вызов подтвержден; статический граф также содержит эту связь"
	case edge.RuntimeCount > 0:
		edge.Evidence = "runtime"
		edge.Reason = "вызов записан во время этого прогона"
	default:
		edge.Evidence = "static"
		edge.Reason = "связь известна только из статического графа"
	}
	return edge
}

func graphNodeValues(values map[string]InfluenceGraphNode) []InfluenceGraphNode {
	out := make([]InfluenceGraphNode, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	return out
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
	case "flow":
		return containsInfluenceValue(node.Flows, value)
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

func minInt(left int, right int) int {
	if left < right {
		return left
	}
	return right
}

func roundedInfluence(value float64) float64 {
	return math.Round(value*10) / 10
}
