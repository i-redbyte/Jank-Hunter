package analyze

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
)

func LoadClassGraph(path string) (*ClassGraph, error) {
	if path == "" {
		return nil, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	graph := &ClassGraph{Format: ClassGraphFormat, Classes: map[string]ClassGraphClass{}}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
	lineCount := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		lineCount++
		if lineCount == 1 && strings.HasPrefix(line, "{") && strings.Contains(line, "\"classes\"") && strings.Contains(line, "\"edges\"") {
			var full ClassGraph
			if err := json.Unmarshal([]byte(line), &full); err != nil {
				return nil, err
			}
			if err := validateArtifactFormat(path, "class graph", full.Format, ClassGraphFormat); err != nil {
				return nil, err
			}
			normalizeClassGraph(&full)
			return &full, nil
		}
		var record struct {
			Format int    `json:"format"`
			Class  string `json:"class"`
			Edges  []struct {
				Caller       string `json:"caller"`
				CalleeClass  string `json:"calleeClass"`
				CalleeMethod string `json:"calleeMethod"`
				Count        uint64 `json:"count"`
			} `json:"edges"`
		}
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			return nil, fmt.Errorf("parse class graph line %d: %w", lineCount, err)
		}
		if err := validateArtifactFormat(path, "class graph", record.Format, ClassGraphFormat); err != nil {
			return nil, fmt.Errorf("parse class graph line %d: %w", lineCount, err)
		}
		from := normalizeClassName(record.Class)
		if from == "" {
			continue
		}
		graph.Classes[from] = ClassGraphClass{Name: from}
		for _, edge := range record.Edges {
			to := normalizeClassName(edge.CalleeClass)
			if to == "" || to == from {
				continue
			}
			count := edge.Count
			if count == 0 {
				count = 1
			}
			graph.Classes[to] = ClassGraphClass{Name: to}
			graph.Edges = append(graph.Edges, ClassGraphEdge{
				From:         from,
				To:           to,
				CallerMethod: edge.Caller,
				CalleeMethod: edge.CalleeMethod,
				Count:        count,
			})
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	normalizeClassGraph(graph)
	return graph, nil
}

func BuildInfluence(summary Summary, graph *ClassGraph) InfluenceSummary {
	builder := influenceBuilder{
		nodes: map[string]*influenceAccumulator{},
	}
	builder.addRuntime(summary)
	builder.addStatic(graph)
	return builder.finish(graph)
}

type influenceBuilder struct {
	nodes        map[string]*influenceAccumulator
	edges        []ClassGraphEdge
	staticIndex  *ClassGraphIndex
	methodIndex  *MethodGraphIndex
	runtimeEdges []runtimeInfluenceEdge
}

type runtimeInfluenceEdge struct {
	from    string
	to      string
	count   uint64
	totalMS uint64
	maxMS   uint64
}

type influenceAccumulator struct {
	className  string
	score      float64
	problems   uint64
	logSpam    uint64
	mainMS     uint64
	runtimeMS  uint64
	networkMS  uint64
	memoryKB   uint64
	uiJank     uint64
	retained   uint64
	heap       bool
	operations map[string]struct{}
	screens    map[string]struct{}
	routes     map[string]struct{}
	reasons    map[string]struct{}
	runtime    bool
	static     bool
}

func (b *influenceBuilder) addRuntime(summary Summary) {
	b.addDatabaseRuntime(summary.DatabaseAnalysis)
	for _, spam := range summary.LogSpam {
		className := classFromOwner(spam.Owner)
		if className == "" {
			continue
		}
		node := b.node(className)
		node.runtime = true
		node.logSpam += spam.Count
		node.addOperation(spam.Operation)
		node.addScreen(spam.Screen)
		node.addReason("спам логами")
		node.score += scoreContribution(spam.Count, 160)
	}
	for _, problem := range summary.ProblemWindows {
		if problem.Kind == "retained_object" || problem.Kind == "log_spam" {
			continue
		}
		className := classFromOwner(problem.Owner)
		if className == "" {
			continue
		}
		node := b.node(className)
		node.runtime = true
		node.problems += problem.Count
		if problemKindIsMainThread(problem.Kind) {
			node.mainMS += problem.TotalWindowMS
		}
		node.addOperation(problem.Operation)
		node.addScreen(problem.Screen)
		node.addReason(problemReason(problem.Kind))
		node.score += scoreContribution(problem.Count, 8)
		node.score += scoreContribution(problem.MaxMS, 500)
	}
	for _, leak := range summary.MemoryLeaks {
		target := leak.ClassName
		if isLikelyAppClass(leak.Holder) {
			target = leak.Holder
		}
		className := classFromOwner(target)
		if className == "" {
			className = normalizeClassName(target)
		}
		if className == "" {
			continue
		}
		node := b.node(className)
		node.runtime = leak.TimeOnlyCount+leak.AfterExplicitGCCount > 0
		node.retained += leak.Count
		node.memoryKB += leak.EstimatedRetainedKB
		node.heap = node.heap || leak.HeapEvidence
		node.addOperation(leak.Operation)
		node.addScreen(leak.Screen)
		node.addReason(leak.EvidenceLabel)
		node.score += leak.Score * 0.45
	}
	for _, call := range summary.RuntimeCalls {
		callerClass := classFromOwner(call.Caller)
		calleeClass := classFromOwner(call.Callee)
		if callerClass == "" || calleeClass == "" || callerClass == calleeClass {
			continue
		}
		caller := b.node(callerClass)
		callee := b.node(calleeClass)
		caller.runtime = true
		callee.runtime = true
		caller.addOperation(call.Operation)
		caller.addScreen(call.Screen)
		callee.addOperation(call.Operation)
		callee.addScreen(call.Screen)
		caller.addReason("вызов при выполнении")
		callee.addReason("вызов при выполнении")
		caller.score += scoreContribution(call.Count, 220) * 0.45
		callee.score += scoreContribution(call.Count, 160) + scoreContribution(call.TotalMS, 2200) + scoreContribution(call.MaxMS, 500)
		caller.runtimeMS += call.TotalMS / 4
		callee.runtimeMS += call.TotalMS
		b.runtimeEdges = append(b.runtimeEdges, runtimeInfluenceEdge{
			from:    callerClass,
			to:      calleeClass,
			count:   call.Count,
			totalMS: call.TotalMS,
			maxMS:   call.MaxMS,
		})
	}
	for _, context := range summary.SignalContexts {
		className := classFromOwner(context.Owner)
		if className == "" {
			continue
		}
		node := b.node(className)
		node.runtime = true
		node.addOperation(context.Operation)
		node.addScreen(context.Screen)
		node.addRoute(context.RouteSample)
		node.networkMS = maxUint64Value(node.networkMS, context.HTTPP95MS)
		node.uiJank += context.UIJank
	}
	for _, route := range summary.Routes {
		className := classFromOwner(route.OwnerSample)
		if className == "" {
			continue
		}
		node := b.node(className)
		node.runtime = true
		node.addRoute(route.Route)
		node.networkMS = maxUint64Value(node.networkMS, route.P95MS)
	}
}

func (b *influenceBuilder) addDatabaseRuntime(analysis *DatabaseAnalysis) {
	if analysis == nil {
		return
	}
	for statementIndex := range analysis.Statements {
		statement := &analysis.Statements[statementIndex]
		for contextIndex := range statement.Contexts {
			context := &statement.Contexts[contextIndex]
			className := classFromOwner(context.Source)
			if className == "" {
				continue
			}
			node := b.node(className)
			node.runtime = true
			node.problems = saturatingUint64Sum(node.problems, context.Overall.Failures)
			node.runtimeMS = saturatingUint64Sum(node.runtimeMS, context.Overall.TotalDurationUS/1_000)
			node.mainMS = saturatingUint64Sum(node.mainMS, context.Main.TotalDurationUS/1_000)
			node.addOperation(context.ContextOperation)
			node.addScreen(context.Screen)
			node.addReason("SQL-вызовы базы данных")
			node.score += scoreContribution(context.Overall.Calls, 64)
			node.score += scoreContribution(context.Overall.Failures, 4)
			node.score += scoreContribution(context.Overall.TotalDurationUS/1_000, 750)
			node.score += scoreContribution(context.Main.TotalDurationUS/1_000, 250)
		}
	}
	for candidateIndex := range analysis.Scenarios.Candidates {
		candidate := &analysis.Scenarios.Candidates[candidateIndex]
		className := classFromOwner(candidate.Source)
		if className == "" {
			continue
		}
		node := b.node(className)
		node.runtime = true
		node.addOperation(candidate.ContextOperation)
		node.addScreen(candidate.Screen)
		node.addReason("гипотеза о повторных SQL-вызовах в одном сценарии")
		node.score += scoreContribution(candidate.EstimatedCalls, 48)
		node.score += scoreContribution(candidate.MaxCallsPerScope, 12)
	}
	if analysis.Transactions == nil {
		return
	}
	for transactionIndex := range analysis.Transactions.Transactions {
		transaction := &analysis.Transactions.Transactions[transactionIndex]
		className := classFromOwner(transaction.Source)
		if className == "" {
			continue
		}
		node := b.node(className)
		node.runtime = true
		node.addOperation(transaction.ContextOperation)
		node.addScreen(transaction.Screen)
		node.addReason("транзакция базы данных")
		node.score += scoreContribution(transaction.StatementCount, 32)
		node.score += scoreContribution(transaction.DurationUS/1_000, 500)
	}
}

func (b *influenceBuilder) addStatic(graph *ClassGraph) {
	if graph == nil {
		return
	}
	for name := range graph.Classes {
		if name == "" {
			continue
		}
		b.node(name).static = true
	}
	for _, edge := range graph.Edges {
		from := normalizeClassName(edge.From)
		to := normalizeClassName(edge.To)
		if from == "" || to == "" || from == to {
			continue
		}
		count := edge.Count
		if count == 0 {
			count = 1
		}
		b.node(from).static = true
		b.node(to).static = true
		edge.From = from
		edge.To = to
		edge.Count = count
		b.edges = append(b.edges, edge)
	}
	b.staticIndex = NewClassGraphIndex(b.edges)
	b.methodIndex = NewMethodGraphIndex(b.edges)
}

func (b *influenceBuilder) finish(graph *ClassGraph) InfluenceSummary {
	runtimeNodes := 0
	staticNodes := 0
	for _, node := range b.nodes {
		if node.runtime {
			runtimeNodes++
			node.score += float64(len(node.operations)+len(node.screens)+len(node.routes)) * 0.35
		}
		if node.static {
			staticNodes++
		}
	}

	allNodes := make([]InfluenceNode, 0, len(b.nodes))
	for _, node := range b.nodes {
		if node.runtime || node.static || node.score > 0 {
			allNodes = append(allNodes, node.toNode())
		}
	}
	sort.Slice(allNodes, func(i, j int) bool {
		return influenceNodeLess(allNodes[i], allNodes[j])
	})
	edges := b.allInfluenceEdges()
	views, workspace := buildInfluenceViews(allNodes, edges)
	var defaultView InfluenceGraphView
	for _, view := range views {
		if view.ID == "problems" {
			defaultView = view
			break
		}
	}
	topNodes := make([]InfluenceNode, 0, len(defaultView.Nodes))
	for _, node := range defaultView.Nodes {
		topNodes = append(topNodes, node.InfluenceNode)
	}
	topEdges := make([]InfluenceEdge, 0, len(defaultView.Edges))
	for _, edge := range defaultView.Edges {
		topEdges = append(topEdges, edge.InfluenceEdge)
	}
	runtimeTargets := b.runtimeTargets()
	hotPaths := b.hotPaths(runtimeTargets)
	methodHotspots := b.methodHotspots(runtimeTargets)
	cycles := b.cycles(runtimeTargets)

	out := InfluenceSummary{
		Available:        len(allNodes) > 0,
		HasClassGraph:    graph != nil && len(graph.Edges) > 0,
		HasRuntimeGraph:  len(b.runtimeEdges) > 0,
		RuntimeNodes:     runtimeNodes,
		RuntimeEdges:     len(b.runtimeEdges),
		StaticNodes:      staticNodes,
		StaticEdges:      len(b.edges),
		TotalNodes:       len(allNodes),
		TotalEdges:       len(edges),
		ShownNodes:       defaultView.ShownNodes,
		ShownEdges:       defaultView.ShownEdges,
		TopNodes:         topNodes,
		TopEdges:         topEdges,
		Views:            views,
		Workspace:        workspace,
		HotPaths:         hotPaths,
		MethodHotspots:   methodHotspots,
		Cycles:           cycles,
		StandaloneReason: "В первую очередь показаны связи, которые ведут к проблемам; дополнительные представления помогают проверить полный контекст.",
	}
	out.Heuristic = influenceHeuristic(out)
	return out
}

func (b *influenceBuilder) runtimeTargets() map[string]struct{} {
	out := map[string]struct{}{}
	for className, node := range b.nodes {
		if node.runtime {
			out[className] = struct{}{}
		}
	}
	return out
}

func (b *influenceBuilder) scoreByClass() map[string]float64 {
	out := map[string]float64{}
	for className, node := range b.nodes {
		if node.score > 0 {
			out[className] = node.score
		}
	}
	return out
}

func (b *influenceBuilder) hotPaths(runtimeTargets map[string]struct{}) []InfluencePath {
	if b.staticIndex == nil {
		return nil
	}
	return b.staticIndex.HotPaths(b.scoreByClass(), runtimeTargets, 8)
}

func (b *influenceBuilder) methodHotspots(runtimeTargets map[string]struct{}) []InfluenceMethod {
	if b.methodIndex == nil {
		return nil
	}
	return b.methodIndex.HotMethods(b.scoreByClass(), runtimeTargets, 12)
}

func (b *influenceBuilder) cycles(runtimeTargets map[string]struct{}) []InfluenceCycle {
	if b.staticIndex == nil {
		return nil
	}
	cycles := b.staticIndex.StronglyConnectedComponents(8)
	for index := range cycles {
		for _, node := range cycles[index].Nodes {
			if _, ok := runtimeTargets[node]; ok {
				cycles[index].RuntimeTouched = true
				break
			}
		}
	}
	sort.Slice(cycles, func(i, j int) bool {
		if cycles[i].RuntimeTouched != cycles[j].RuntimeTouched {
			return cycles[i].RuntimeTouched
		}
		if cycles[i].Weight == cycles[j].Weight {
			return strings.Join(cycles[i].Nodes, ".") < strings.Join(cycles[j].Nodes, ".")
		}
		return cycles[i].Weight > cycles[j].Weight
	})
	return cycles
}

func (b *influenceBuilder) allInfluenceEdges() []InfluenceEdge {
	dedup := map[string]*InfluenceEdge{}
	for _, edge := range b.edges {
		fromNode := b.nodes[edge.From]
		toNode := b.nodes[edge.To]
		if fromNode == nil || toNode == nil {
			continue
		}
		key := edge.From + "\x00" + edge.To
		row := dedup[key]
		if row == nil {
			row = &InfluenceEdge{From: edge.From, To: edge.To}
			dedup[key] = row
		}
		row.Count += edge.Count
		row.StaticCount += edge.Count
		priority := math.Max(toNode.score, fromNode.score*0.35)
		row.Influence += math.Log1p(float64(edge.Count)) * (1 + priority)
	}
	for _, edge := range b.runtimeEdges {
		if b.nodes[edge.from] == nil || b.nodes[edge.to] == nil {
			continue
		}
		key := edge.from + "\x00" + edge.to
		row := dedup[key]
		if row == nil {
			row = &InfluenceEdge{From: edge.from, To: edge.to}
			dedup[key] = row
		}
		row.Count += edge.count
		row.RuntimeCount += edge.count
		row.Influence += float64(edge.count) + float64(edge.totalMS)/25 + float64(edge.maxMS)/5
	}
	out := make([]InfluenceEdge, 0, len(dedup))
	for _, edge := range dedup {
		normalized := normalizeInfluenceEvidence(*edge)
		normalized.Influence = roundedInfluence(normalized.Influence)
		out = append(out, normalized)
	}
	sortInfluenceEdges(out)
	return out
}

func (b *influenceBuilder) node(className string) *influenceAccumulator {
	className = normalizeClassName(className)
	node := b.nodes[className]
	if node != nil {
		return node
	}
	node = &influenceAccumulator{
		className:  className,
		operations: map[string]struct{}{},
		screens:    map[string]struct{}{},
		routes:     map[string]struct{}{},
		reasons:    map[string]struct{}{},
	}
	b.nodes[className] = node
	return node
}

func (n *influenceAccumulator) toNode() InfluenceNode {
	status := "runtime"
	if !n.runtime && n.static {
		status = "static_only"
	}
	score := math.Round(n.score*10) / 10
	severity := influenceSeverity(score)
	if !n.runtime && severity == "high" {
		severity = "medium"
	}
	return InfluenceNode{
		ClassName:       n.className,
		Label:           shortClassName(n.className),
		Score:           score,
		Severity:        severity,
		Status:          status,
		RuntimeEvidence: n.runtime,
		Problems:        n.problems,
		LogSpam:         n.logSpam,
		MainThreadMS:    n.mainMS,
		RuntimeWallMS:   n.runtimeMS,
		NetworkMS:       n.networkMS,
		MemoryPressure:  n.memoryKB,
		UIJank:          n.uiJank,
		Retained:        n.retained,
		HeapEvidence:    n.heap,
		Operations:      sortedSet(n.operations, 0),
		Screens:         sortedSet(n.screens, 0),
		Routes:          sortedSet(n.routes, 0),
		Reasons:         sortedSet(n.reasons, 0),
	}
}

func maxUint64Value(left uint64, right uint64) uint64 {
	if right > left {
		return right
	}
	return left
}

func (n *influenceAccumulator) addOperation(value string) {
	addNonEmpty(n.operations, value)
}

func (n *influenceAccumulator) addScreen(value string) {
	addNonEmpty(n.screens, value)
}

func (n *influenceAccumulator) addRoute(value string) {
	addNonEmpty(n.routes, value)
}

func (n *influenceAccumulator) addReason(value string) {
	addNonEmpty(n.reasons, value)
}

func addNonEmpty(target map[string]struct{}, value string) {
	value = strings.TrimSpace(value)
	if value == "" || value == "unknown" {
		return
	}
	target[value] = struct{}{}
}

func normalizeClassGraph(graph *ClassGraph) {
	if graph.Classes == nil {
		graph.Classes = map[string]ClassGraphClass{}
	}
	edgeCounts := map[string]ClassGraphEdge{}
	for name, class := range graph.Classes {
		normalized := normalizeClassName(firstNonEmpty(class.Name, name))
		if normalized == "" {
			continue
		}
		graph.Classes[normalized] = ClassGraphClass{Name: normalized}
		if normalized != name {
			delete(graph.Classes, name)
		}
	}
	for _, edge := range graph.Edges {
		from := normalizeClassName(edge.From)
		to := normalizeClassName(edge.To)
		if from == "" || to == "" || from == to {
			continue
		}
		count := edge.Count
		if count == 0 {
			count = 1
		}
		key := from + "\x00" + to + "\x00" + edge.CallerMethod + "\x00" + edge.CalleeMethod
		merged := edgeCounts[key]
		merged.From = from
		merged.To = to
		merged.CallerMethod = edge.CallerMethod
		merged.CalleeMethod = edge.CalleeMethod
		merged.Count += count
		edgeCounts[key] = merged
		graph.Classes[from] = ClassGraphClass{Name: from}
		graph.Classes[to] = ClassGraphClass{Name: to}
	}
	graph.Edges = graph.Edges[:0]
	for _, edge := range edgeCounts {
		graph.Edges = append(graph.Edges, edge)
	}
	sort.Slice(graph.Edges, func(i, j int) bool {
		if graph.Edges[i].From == graph.Edges[j].From {
			if graph.Edges[i].To == graph.Edges[j].To {
				return graph.Edges[i].Count > graph.Edges[j].Count
			}
			return graph.Edges[i].To < graph.Edges[j].To
		}
		return graph.Edges[i].From < graph.Edges[j].From
	})
}

func classFromOwner(owner string) string {
	owner = strings.TrimSpace(owner)
	if owner == "" || owner == "unknown" {
		return ""
	}
	owner = strings.TrimPrefix(owner, "owner.")
	if strings.HasPrefix(owner, "lifecycle.destroyed.") {
		className := normalizeClassName(strings.TrimPrefix(owner, "lifecycle.destroyed."))
		if isLikelyAppClass(className) {
			return className
		}
		return ""
	}
	if hashIndex := strings.LastIndex(owner, "#"); hashIndex > 0 {
		owner = owner[:hashIndex]
		if dot := strings.LastIndex(owner, "."); dot > 0 {
			return normalizeClassName(owner[:dot])
		}
	}
	normalized := normalizeClassName(owner)
	if dot := strings.LastIndex(normalized, "."); dot > 0 {
		candidate := normalized[:dot]
		simpleCandidate := candidate[strings.LastIndex(candidate, ".")+1:]
		if simpleCandidate != "" && (isUpperASCII(simpleCandidate[0]) || strings.Contains(simpleCandidate, "$")) {
			return candidate
		}
	}
	simple := normalized[strings.LastIndex(normalized, ".")+1:]
	if simple != "" && (isUpperASCII(simple[0]) || strings.Contains(simple, "$")) {
		return normalized
	}
	return ""
}

func normalizeClassName(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "/", "."))
	value = strings.TrimPrefix(value, "L")
	value = strings.TrimSuffix(value, ";")
	value = strings.Trim(value, ".")
	if value == "" || value == "unknown" || strings.Contains(value, " ") {
		return ""
	}
	return value
}

func shortClassName(value string) string {
	value = normalizeClassName(value)
	if value == "" {
		return ""
	}
	parts := strings.Split(value, ".")
	if len(parts) <= 2 {
		return value
	}
	return strings.Join(parts[len(parts)-2:], ".")
}

func scoreContribution(value uint64, pivot uint64) float64 {
	if value == 0 || pivot == 0 {
		return 0
	}
	return math.Min(8, math.Log1p(float64(value))/math.Log1p(float64(pivot))*3)
}

func sortedSet(values map[string]struct{}, limit int) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	if limit > 0 && len(out) > limit {
		return out[:limit]
	}
	return out
}

func influenceSeverity(score float64) string {
	switch {
	case score >= 15:
		return "high"
	case score >= 5:
		return "medium"
	default:
		return "ok"
	}
}

func problemReason(kind string) string {
	switch kind {
	case "http", "http_slow", "http_slow_or_failed":
		return "медленный или ошибочный HTTP"
	case "main_thread_stall":
		return "паузы главного потока"
	case "main_thread_dispatch":
		return "медленная обработка сообщения главного потока"
	case "main_thread_io", "main_thread_disk_io", "disk_io_main_thread":
		return "файловая операция на главном потоке"
	case "ui_jank":
		return "Подтормаживания интерфейса"
	case "log_spam":
		return "спам логами"
	case "retained_object", "memory_retained":
		return "удержанные объекты"
	case "wrapped_runnable":
		return "долгая задача Runnable"
	case "wrapped_handler_runnable":
		return "долгая задача обработчика Handler"
	case "wrapped_callable":
		return "долгая вычислительная задача Callable"
	case "wrapped_coroutine":
		return "долгая задача корутины"
	case "wrapped_executor":
		return "долгая задача исполнителя"
	case "wrapped_click":
		return "долгий обработчик нажатия"
	case "gc_pressure", "gc_count", "gc_time":
		return "давление сборки мусора"
	default:
		if kind == "" {
			return "проблемные окна"
		}
		return kind
	}
}

func problemKindIsMainThread(kind string) bool {
	switch kind {
	case "main_thread_stall",
		"main_thread_dispatch",
		"main_thread_io",
		"main_thread_disk_io",
		"disk_io_main_thread",
		"wrapped_click":
		return true
	default:
		return false
	}
}

func influenceHeuristic(summary InfluenceSummary) []InfluenceFinding {
	if !summary.Available {
		return nil
	}
	out := []InfluenceFinding{}
	if len(summary.TopNodes) > 0 {
		top := summary.TopNodes[0]
		detail := fmt.Sprintf("%s: оценка приоритета %.1f, основания: %s.", top.ClassName, top.Score, strings.Join(top.Reasons, ", "))
		if top.Status != "runtime" {
			detail += " Для узла нет подтверждения выполнения в этом прогоне; статическая связь не доказывает влияние на производительность."
		}
		out = append(out, InfluenceFinding{
			Severity: top.Severity,
			Title:    "Первый узел для проверки",
			Detail:   detail,
		})
	}
	for _, edge := range summary.TopEdges {
		if edge.RuntimeConfirmed {
			out = append(out, InfluenceFinding{
				Severity: "ok",
				Title:    "Связь наблюдалась во время выполнения",
				Detail:   fmt.Sprintf("%s → %s: зафиксировано вызовов %d, вес ранжирования %.1f. Наличие вызова не доказывает, что он вызвал соседний симптом.", edge.From, edge.To, edge.Count, edge.Influence),
			})
			break
		}
	}
	if !summary.HasClassGraph {
		out = append(out, InfluenceFinding{
			Severity: "ok",
			Title:    "Статический граф не передан",
			Detail:   "Показаны только наблюдения выполнения. Параметр --class-graph добавит структуру связей между классами, но сам по себе не подтвердит их выполнение.",
		})
	}
	return out
}

func isUpperASCII(value byte) bool {
	return value >= 'A' && value <= 'Z'
}
