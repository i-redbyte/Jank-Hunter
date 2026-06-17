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

	graph := &ClassGraph{Classes: map[string]ClassGraphClass{}}
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
			normalizeClassGraph(&full)
			return &full, nil
		}
		var record struct {
			Class string `json:"class"`
			Edges []struct {
				Caller       string `json:"caller"`
				CalleeClass  string `json:"calleeClass"`
				CalleeMethod string `json:"calleeMethod"`
				Count        uint64 `json:"count"`
			} `json:"edges"`
		}
		if err := json.Unmarshal([]byte(line), &record); err != nil {
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
	className string
	score     float64
	problems  uint64
	logSpam   uint64
	mainMS    uint64
	networkMS uint64
	memoryKB  uint64
	uiJank    uint64
	retained  uint64
	flows     map[string]struct{}
	screens   map[string]struct{}
	routes    map[string]struct{}
	reasons   map[string]struct{}
	runtime   bool
	static    bool
}

func (b *influenceBuilder) addRuntime(summary Summary) {
	for _, owner := range summary.Owners {
		className := classFromOwner(owner.Owner)
		if className == "" {
			continue
		}
		node := b.node(className)
		node.runtime = true
		switch owner.Kind {
		case "main_thread_stall":
			node.mainMS += owner.TotalMS
			node.problems += uint64(owner.Count)
			node.addReason("работа на главном потоке")
			node.score += scoreDuration(owner.TotalMS, 1800) + scoreDuration(owner.MaxMS, 500)
		case "http":
			node.networkMS += owner.TotalMS
			node.addReason("сетевые задержки")
			node.score += scoreDuration(owner.TotalMS, 2500) + scoreDuration(owner.MaxMS, 900)
		case "retained_object":
			node.retained += uint64(owner.Count)
			node.memoryKB += owner.MaxMS
			node.addReason("удержанные объекты")
			node.score += scoreCount(uint64(owner.Count), 8) + scoreDuration(owner.MaxMS, 60_000)
		default:
			node.score += scoreCount(uint64(owner.Count), 120)
		}
	}
	for _, flow := range summary.Flows {
		className := classFromOwner(flow.Owner)
		if className == "" {
			continue
		}
		node := b.node(className)
		node.runtime = true
		node.problems += flow.ProblemCount
		node.logSpam += flow.LogSpam
		node.mainMS += flow.StallMaxMS
		node.networkMS += flow.HTTPP95MS
		node.memoryKB += flow.MemoryMaxKB
		node.uiJank += flow.UIJank
		node.addFlow(flow.Flow)
		node.addScreen(flow.Screen)
		node.addRoute(flow.RouteSample)
		if flow.ProblemCount > 0 {
			node.addReason("проблемные окна")
		}
		if flow.LogSpam > 0 {
			node.addReason("спам логами")
		}
		if flow.HTTPP95MS > 0 {
			node.addReason("95-й процентиль HTTP")
		}
		if flow.StallMaxMS > 0 {
			node.addReason("паузы главного потока")
		}
		if flow.UIJank > 0 {
			node.addReason("UI-подтормаживания")
		}
		node.score += scoreCount(flow.ProblemCount, 10)
		node.score += scoreCount(flow.LogSpam, 180)
		node.score += scoreDuration(flow.HTTPP95MS, 900)
		node.score += scoreDuration(flow.StallMaxMS, 500)
		node.score += scoreCount(flow.UIJank, 50)
	}
	for _, spam := range summary.LogSpam {
		className := classFromOwner(spam.Owner)
		if className == "" {
			continue
		}
		node := b.node(className)
		node.runtime = true
		node.logSpam += spam.Count
		node.addFlow(spam.Flow)
		node.addScreen(spam.Screen)
		node.addReason("спам логами")
		node.score += scoreCount(spam.Count, 160)
	}
	for _, problem := range summary.ProblemWindows {
		className := classFromOwner(problem.Owner)
		if className == "" {
			continue
		}
		node := b.node(className)
		node.runtime = true
		node.problems += problem.Count
		node.mainMS += problem.MaxMS
		node.addFlow(problem.Flow)
		node.addScreen(problem.Screen)
		node.addReason(problemReason(problem.Kind))
		node.score += scoreCount(problem.Count, 8)
		node.score += scoreDuration(problem.MaxMS, 500)
	}
	for _, retained := range summary.RetainedClasses {
		className := normalizeClassName(retained.Name)
		if className == "" {
			continue
		}
		node := b.node(className)
		node.runtime = true
		node.retained += retained.Value
		node.addReason("удержанные объекты")
		node.score += scoreCount(retained.Value, 8)
	}
	for _, route := range summary.Routes {
		className := classFromOwner(route.OwnerSample)
		if className == "" {
			continue
		}
		node := b.node(className)
		node.runtime = true
		node.networkMS += route.P95MS
		node.addRoute(route.Route)
		node.addReason("сетевой маршрут")
		node.score += scoreDuration(route.P95MS, 900) + scoreCount(uint64(route.Failures), 3)
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
		caller.addFlow(call.Flow)
		caller.addScreen(call.Screen)
		callee.addFlow(call.Flow)
		callee.addScreen(call.Screen)
		caller.addReason("вызовы выполнения")
		callee.addReason("вызовы выполнения")
		caller.score += scoreCount(call.Count, 220) * 0.45
		callee.score += scoreCount(call.Count, 160) + scoreDuration(call.TotalMS, 2200) + scoreDuration(call.MaxMS, 500)
		caller.mainMS += call.TotalMS / 4
		callee.mainMS += call.TotalMS
		b.runtimeEdges = append(b.runtimeEdges, runtimeInfluenceEdge{
			from:    callerClass,
			to:      calleeClass,
			count:   call.Count,
			totalMS: call.TotalMS,
			maxMS:   call.MaxMS,
		})
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
}

func (b *influenceBuilder) finish(graph *ClassGraph) InfluenceSummary {
	runtimeNodes := 0
	staticNodes := 0
	for _, node := range b.nodes {
		if node.runtime {
			runtimeNodes++
			node.score += float64(len(node.flows)+len(node.screens)+len(node.routes)) * 0.35
		}
		if node.static {
			staticNodes++
		}
	}

	allNodes := make([]InfluenceNode, 0, len(b.nodes))
	for _, node := range b.nodes {
		if node.runtime || node.score > 0 {
			allNodes = append(allNodes, node.toNode())
		}
	}
	sort.Slice(allNodes, func(i, j int) bool {
		if allNodes[i].Score == allNodes[j].Score {
			return allNodes[i].ClassName < allNodes[j].ClassName
		}
		return allNodes[i].Score > allNodes[j].Score
	})
	if len(allNodes) > 60 {
		allNodes = allNodes[:60]
	}

	selected := map[string]struct{}{}
	for i, node := range allNodes {
		selected[node.ClassName] = struct{}{}
		if i >= 24 {
			break
		}
	}
	edges := b.influenceEdges(selected)
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].Influence == edges[j].Influence {
			return edges[i].Count > edges[j].Count
		}
		return edges[i].Influence > edges[j].Influence
	})
	if len(edges) > 120 {
		edges = edges[:120]
	}
	for _, edge := range edges {
		for _, endpoint := range []string{edge.From, edge.To} {
			if _, ok := selected[endpoint]; ok {
				continue
			}
			if len(allNodes) >= 80 {
				break
			}
			if node := b.nodes[endpoint]; node != nil {
				allNodes = append(allNodes, node.toNode())
				selected[endpoint] = struct{}{}
			}
		}
	}
	if len(allNodes) > 80 {
		allNodes = allNodes[:80]
	}

	out := InfluenceSummary{
		Available:        len(allNodes) > 0,
		HasClassGraph:    graph != nil && len(graph.Edges) > 0,
		HasRuntimeGraph:  len(b.runtimeEdges) > 0,
		RuntimeNodes:     runtimeNodes,
		RuntimeEdges:     len(b.runtimeEdges),
		StaticNodes:      staticNodes,
		StaticEdges:      len(b.edges),
		ShownNodes:       len(allNodes),
		ShownEdges:       len(edges),
		TopNodes:         allNodes,
		TopEdges:         edges,
		StandaloneReason: "Подробный граф вынесен отдельно, чтобы большой проект не превращал основной математический отчет в тяжелую страницу.",
	}
	out.Heuristic = influenceHeuristic(out)
	return out
}

func (b *influenceBuilder) influenceEdges(selected map[string]struct{}) []InfluenceEdge {
	dedup := map[string]*InfluenceEdge{}
	for _, edge := range b.edges {
		fromNode := b.nodes[edge.From]
		toNode := b.nodes[edge.To]
		if fromNode == nil || toNode == nil {
			continue
		}
		_, fromSelected := selected[edge.From]
		_, toSelected := selected[edge.To]
		if !fromSelected && !toSelected && !toNode.runtime {
			continue
		}
		key := edge.From + "\x00" + edge.To
		row := dedup[key]
		if row == nil {
			row = &InfluenceEdge{
				From:             edge.From,
				To:               edge.To,
				RuntimeConfirmed: fromNode.runtime || toNode.runtime,
			}
			dedup[key] = row
		}
		row.Count += edge.Count
		row.Influence += float64(edge.Count) * math.Max(toNode.score, fromNode.score*0.35)
		if toNode.runtime {
			row.Reason = "вызывает узел с проблемами выполнения"
		} else if fromNode.runtime {
			row.Reason = "сосед проблемного узла выполнения"
		} else {
			row.Reason = "статическая связь"
		}
	}
	for _, edge := range b.runtimeEdges {
		fromNode := b.nodes[edge.from]
		toNode := b.nodes[edge.to]
		if fromNode == nil || toNode == nil {
			continue
		}
		_, fromSelected := selected[edge.from]
		_, toSelected := selected[edge.to]
		if !fromSelected && !toSelected {
			continue
		}
		key := edge.from + "\x00" + edge.to
		row := dedup[key]
		if row == nil {
			row = &InfluenceEdge{
				From: edge.from,
				To:   edge.to,
			}
			dedup[key] = row
		}
		row.Count += edge.count
		row.Influence += float64(edge.count) + float64(edge.totalMS)/25 + float64(edge.maxMS)/5
		row.RuntimeConfirmed = true
		row.Reason = "вызов выполнения в этом прогоне"
	}
	out := make([]InfluenceEdge, 0, len(dedup))
	for _, edge := range dedup {
		out = append(out, *edge)
	}
	return out
}

func (b *influenceBuilder) node(className string) *influenceAccumulator {
	className = normalizeClassName(className)
	node := b.nodes[className]
	if node != nil {
		return node
	}
	node = &influenceAccumulator{
		className: className,
		flows:     map[string]struct{}{},
		screens:   map[string]struct{}{},
		routes:    map[string]struct{}{},
		reasons:   map[string]struct{}{},
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
	return InfluenceNode{
		ClassName:       n.className,
		Label:           shortClassName(n.className),
		Score:           score,
		Severity:        influenceSeverity(score),
		Status:          status,
		RuntimeEvidence: n.runtime,
		Problems:        n.problems,
		LogSpam:         n.logSpam,
		MainThreadMS:    n.mainMS,
		NetworkMS:       n.networkMS,
		MemoryPressure:  n.memoryKB,
		UIJank:          n.uiJank,
		Retained:        n.retained,
		Flows:           sortedSet(n.flows, 4),
		Screens:         sortedSet(n.screens, 4),
		Routes:          sortedSet(n.routes, 4),
		Reasons:         sortedSet(n.reasons, 5),
	}
}

func (n *influenceAccumulator) addFlow(value string) {
	addNonEmpty(n.flows, value)
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

func scoreDuration(value uint64, pivot uint64) float64 {
	if value == 0 || pivot == 0 {
		return 0
	}
	return math.Min(8, math.Log1p(float64(value))/math.Log1p(float64(pivot))*3)
}

func scoreCount(value uint64, pivot uint64) float64 {
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
	case score >= 12:
		return "high"
	case score >= 6:
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
		return "медленный dispatch главного потока"
	case "main_thread_io", "main_thread_disk_io", "disk_io_main_thread":
		return "IO на главном потоке"
	case "ui_jank":
		return "UI-подтормаживания"
	case "log_spam":
		return "спам логами"
	case "retained_object", "memory_retained":
		return "удержанные объекты"
	case "wrapped_runnable":
		return "долгая Runnable-задача"
	case "wrapped_handler_runnable":
		return "долгая Handler-задача"
	case "wrapped_callable":
		return "долгая Callable-задача"
	case "wrapped_coroutine":
		return "долгая coroutine-задача"
	case "wrapped_executor":
		return "долгая executor-задача"
	case "wrapped_click":
		return "долгий click-handler"
	case "gc_pressure", "gc_count", "gc_time":
		return "давление GC"
	default:
		if kind == "" {
			return "проблемные окна"
		}
		return kind
	}
}

func influenceHeuristic(summary InfluenceSummary) []InfluenceFinding {
	if !summary.Available {
		return nil
	}
	out := []InfluenceFinding{}
	if len(summary.TopNodes) > 0 {
		top := summary.TopNodes[0]
		out = append(out, InfluenceFinding{
			Severity: top.Severity,
			Title:    "Главный узел влияния",
			Detail:   fmt.Sprintf("%s: оценка %.1f, причины: %s.", top.ClassName, top.Score, strings.Join(top.Reasons, ", ")),
		})
	}
	for _, edge := range summary.TopEdges {
		if edge.RuntimeConfirmed {
			out = append(out, InfluenceFinding{
				Severity: "medium",
				Title:    "Связь с доказательством выполнения",
				Detail:   fmt.Sprintf("%s → %s, вес %.1f, вызовов %d.", edge.From, edge.To, edge.Influence, edge.Count),
			})
			break
		}
	}
	if !summary.HasClassGraph {
		out = append(out, InfluenceFinding{
			Severity: "medium",
			Title:    "Нет статического графа",
			Detail:   "CLI построил влияние только по событиям выполнения. Передайте --class-graph, чтобы увидеть связи между классами.",
		})
	}
	return out
}

func isUpperASCII(value byte) bool {
	return value >= 'A' && value <= 'Z'
}
