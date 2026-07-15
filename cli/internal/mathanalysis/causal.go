package mathanalysis

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

const maxFloydWarshallNodes = 60

type causalGraphBuilder struct {
	nodes map[string]CausalNode
	edges map[string]*causalEdgeAgg
}

type causalEdgeAgg struct {
	from        string
	to          string
	kind        string
	count       int
	strength    float64
	description string
}

func buildCausalGraph(timeline []TimelineBucket, loops []NetworkLoopFinding, markov MarkovModel) CausalGraph {
	builder := newCausalGraphBuilder()
	builder.addTimeline(timeline, markov)
	builder.addNetworkLoops(loops)
	nodes := builder.nodeList()
	edges := builder.edgeList()
	paths := causalShortestPaths(nodes, edges)
	allPairs := floydWarshallGraphPaths(nodes, edges, 6)
	ownerScores := causalOwnerScores(nodes, edges, loops)
	return CausalGraph{
		Nodes:       nodes,
		Edges:       edges,
		Paths:       paths,
		AllPairs:    allPairs,
		OwnerScores: ownerScores,
	}
}

func newCausalGraphBuilder() *causalGraphBuilder {
	return &causalGraphBuilder{
		nodes: map[string]CausalNode{},
		edges: map[string]*causalEdgeAgg{},
	}
}

func (b *causalGraphBuilder) addTimeline(timeline []TimelineBucket, markov MarkovModel) {
	for index, bucket := range timeline {
		state := markovHealthy
		if index < len(markov.States) {
			state = markov.States[index].State
		}
		stateID := causalNodeID("state", state)
		b.addNode(stateID, MarkovStateLabel(state), "state")
		strength := causalBucketStrength(state)
		if symptomID := causalSymptomForState(state); symptomID != "" {
			b.addUndirectedEdge(stateID, symptomID, "state-symptom", strength, "состояние связано с симптомом")
		}
		if bucket.ScreenSample != "" {
			screenID := causalNodeID("screen", bucket.ScreenSample)
			b.addNode(screenID, "экран: "+bucket.ScreenSample, "screen")
			b.addUndirectedEdge(screenID, stateID, "screen-state", strength, "экран наблюдался в этом состоянии")
		}
		if bucket.OwnerSample != "" {
			ownerID := causalNodeID("owner", bucket.OwnerSample)
			b.addNode(ownerID, "источник: "+bucket.OwnerSample, "owner")
			b.addUndirectedEdge(ownerID, stateID, "owner-state", strength, "источник активен рядом с состоянием")
			if bucket.ScreenSample != "" {
				b.addUndirectedEdge(causalNodeID("screen", bucket.ScreenSample), ownerID, "screen-owner", strength*0.8, "экран и источник совпали во временном интервале")
			}
		}
		if bucket.RouteSample != "" {
			routeID := causalNodeID("route", bucket.RouteSample)
			b.addNode(routeID, "маршрут: "+bucket.RouteSample, "route")
			b.addUndirectedEdge(routeID, stateID, "route-state", strength, "маршрут активен рядом с состоянием")
			if bucket.OwnerSample != "" {
				b.addUndirectedEdge(causalNodeID("owner", bucket.OwnerSample), routeID, "owner-route", strength+0.5, "источник вызвал маршрут")
			}
			b.addNetworkPhaseEdges(routeID, bucket, strength)
		}
		if bucket.NetworkSample != "" {
			networkID := causalNodeID("network", bucket.NetworkSample)
			b.addNode(networkID, "сеть: "+bucket.NetworkSample, "network")
			b.addUndirectedEdge(networkID, stateID, "сеть -> состояние", strength*0.7, "сетевая когорта совпала с состоянием")
		}
	}
}

func (b *causalGraphBuilder) addNetworkPhaseEdges(routeID string, bucket TimelineBucket, strength float64) {
	if bucket.HTTPCount > 0 {
		phaseID := causalNodeID("phase", "HTTP")
		b.addNode(phaseID, "фаза: HTTP", "phase")
		b.addUndirectedEdge(routeID, phaseID, "route-phase", strength, "HTTP активность маршрута")
	}
	if bucket.DNSCount > 0 {
		phaseID := causalNodeID("phase", "DNS")
		b.addNode(phaseID, "фаза: DNS", "phase")
		b.addUndirectedEdge(routeID, phaseID, "route-phase", strength+float64(bucket.DNSCount)*0.3, "DNS активность маршрута")
		b.addUndirectedEdge(phaseID, causalNodeID("symptom", "network_slow"), "phase-symptom", strength+0.5, "DNS всплеск связан с сетевым симптомом")
	}
	if bucket.ConnectCount > 0 {
		phaseID := causalNodeID("phase", "connect")
		b.addNode(phaseID, "фаза: соединение", "phase")
		b.addUndirectedEdge(routeID, phaseID, "route-phase", strength+float64(bucket.ConnectCount)*0.3, "активность соединения маршрута")
		b.addUndirectedEdge(phaseID, causalNodeID("symptom", "network_slow"), "phase-symptom", strength+0.5, "всплеск соединения связан с сетевым симптомом")
	}
	if bucket.HTTPFailed > 0 {
		b.addUndirectedEdge(routeID, causalNodeID("symptom", "network_slow"), "route-symptom", strength+float64(bucket.HTTPFailed), "ошибки маршрута связаны с сетевым симптомом")
	}
}

func (b *causalGraphBuilder) addNetworkLoops(loops []NetworkLoopFinding) {
	for _, loop := range loops {
		strength := 1 + loop.Confidence*3 + math.Min(3, loop.BurnScore/10)
		symptomID := causalNodeID("symptom", "network_loop")
		b.addNode(symptomID, "симптом: сетевой цикл", "symptom")
		if loop.Owner != "" {
			ownerID := causalNodeID("owner", loop.Owner)
			b.addNode(ownerID, "источник: "+loop.Owner, "owner")
			b.addUndirectedEdge(symptomID, ownerID, "loop-owner", strength, "сетевой цикл связан с источником")
		}
		if loop.Route != "" {
			routeID := causalNodeID("route", loop.Route)
			b.addNode(routeID, "маршрут: "+loop.Route, "route")
			b.addUndirectedEdge(symptomID, routeID, "loop-route", strength, "сетевой цикл связан с маршрутом")
			if loop.Owner != "" {
				b.addUndirectedEdge(causalNodeID("owner", loop.Owner), routeID, "owner-route", strength, "источник связан с маршрутом сетевого цикла")
			}
		}
		for _, token := range loop.Motif {
			if token == "dns_high" {
				b.addUndirectedEdge(symptomID, causalNodeID("phase", "DNS"), "loop-phase", strength, "паттерн цикла содержит DNS")
			}
			if token == "connect_high" || token == "reconnect_high" || token == "websocket_reconnect" {
				b.addUndirectedEdge(symptomID, causalNodeID("phase", "connect"), "loop-phase", strength, "паттерн цикла содержит повторное соединение или соединение")
			}
		}
	}
}

func (b *causalGraphBuilder) addNode(id, label, kind string) {
	if id == "" {
		return
	}
	if _, ok := b.nodes[id]; ok {
		return
	}
	b.nodes[id] = CausalNode{ID: id, Label: label, Kind: kind}
}

func (b *causalGraphBuilder) addUndirectedEdge(from, to, kind string, strength float64, description string) {
	b.addEdge(from, to, kind, strength, description)
	b.addEdge(to, from, kind, strength, description)
}

func (b *causalGraphBuilder) addEdge(from, to, kind string, strength float64, description string) {
	if from == "" || to == "" || from == to {
		return
	}
	b.ensureKnownNode(from)
	b.ensureKnownNode(to)
	key := from + "\x00" + to + "\x00" + kind
	edge := b.edges[key]
	if edge == nil {
		edge = &causalEdgeAgg{from: from, to: to, kind: kind, description: description}
		b.edges[key] = edge
	}
	edge.count++
	edge.strength += math.Max(0.1, strength)
}

func (b *causalGraphBuilder) ensureKnownNode(id string) {
	if _, ok := b.nodes[id]; ok {
		return
	}
	kind, value := causalSplitNodeID(id)
	b.addNode(id, causalFallbackLabel(kind, value), kind)
}

func (b *causalGraphBuilder) nodeList() []CausalNode {
	nodes := make([]CausalNode, 0, len(b.nodes))
	for _, node := range b.nodes {
		nodes = append(nodes, node)
	}
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].Kind != nodes[j].Kind {
			return nodes[i].Kind < nodes[j].Kind
		}
		return nodes[i].Label < nodes[j].Label
	})
	return nodes
}

func (b *causalGraphBuilder) edgeList() []CausalEdge {
	edges := make([]CausalEdge, 0, len(b.edges))
	for _, edge := range b.edges {
		confidence := math.Min(1, edge.strength/6)
		if confidence <= 0 {
			confidence = 0.05
		}
		weight := 1 / confidence
		from := b.nodes[edge.from]
		to := b.nodes[edge.to]
		edges = append(edges, CausalEdge{
			From:        edge.from,
			To:          edge.to,
			FromLabel:   from.Label,
			ToLabel:     to.Label,
			Kind:        edge.kind,
			Count:       edge.count,
			Weight:      weight,
			Confidence:  confidence,
			Description: edge.description,
		})
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].Confidence != edges[j].Confidence {
			return edges[i].Confidence > edges[j].Confidence
		}
		if edges[i].Count != edges[j].Count {
			return edges[i].Count > edges[j].Count
		}
		return edges[i].FromLabel+"->"+edges[i].ToLabel < edges[j].FromLabel+"->"+edges[j].ToLabel
	})
	return edges
}

func causalShortestPaths(nodes []CausalNode, edges []CausalEdge) []GraphPath {
	nodeMap := causalNodeMap(nodes)
	adjacency := causalAdjacency(edges)
	var sources []string
	var targets []string
	for _, node := range nodes {
		if node.Kind == "symptom" {
			sources = append(sources, node.ID)
		}
		if node.Kind == "owner" || node.Kind == "route" {
			targets = append(targets, node.ID)
		}
	}
	var paths []GraphPath
	for _, source := range sources {
		for _, target := range targets {
			path, ok := shortestGraphPathWithAdjacency(nodeMap, adjacency, source, target)
			if ok && len(path.Nodes) > 1 {
				paths = append(paths, path)
			}
		}
	}
	sort.Slice(paths, func(i, j int) bool {
		if paths[i].Cost != paths[j].Cost {
			return paths[i].Cost < paths[j].Cost
		}
		return strings.Join(paths[i].Nodes, "->") < strings.Join(paths[j].Nodes, "->")
	})
	if len(paths) > 6 {
		paths = paths[:6]
	}
	return paths
}

func shortestGraphPath(nodes map[string]CausalNode, edges []CausalEdge, source, target string) (GraphPath, bool) {
	return shortestGraphPathWithAdjacency(nodes, causalAdjacency(edges), source, target)
}

func shortestGraphPathWithAdjacency(
	nodes map[string]CausalNode,
	adjacency map[string][]CausalEdge,
	source,
	target string,
) (GraphPath, bool) {
	if _, ok := nodes[source]; !ok {
		return GraphPath{}, false
	}
	if _, ok := nodes[target]; !ok {
		return GraphPath{}, false
	}
	dist := map[string]float64{}
	prev := map[string]string{}
	visited := map[string]bool{}
	for id := range nodes {
		dist[id] = math.Inf(1)
	}
	dist[source] = 0
	for {
		current := ""
		best := math.Inf(1)
		for id, value := range dist {
			if !visited[id] && (value < best || (value == best && (current == "" || id < current))) {
				current = id
				best = value
			}
		}
		if current == "" || current == target {
			break
		}
		visited[current] = true
		for _, edge := range adjacency[current] {
			next := edge.To
			candidate := dist[current] + edge.Weight
			if candidate < dist[next] || (candidate == dist[next] && (prev[next] == "" || current < prev[next])) {
				dist[next] = candidate
				prev[next] = current
			}
		}
	}
	if math.IsInf(dist[target], 1) {
		return GraphPath{}, false
	}
	ids := []string{target}
	for ids[len(ids)-1] != source {
		parent := prev[ids[len(ids)-1]]
		if parent == "" {
			return GraphPath{}, false
		}
		ids = append(ids, parent)
	}
	reverseStrings(ids)
	labels := make([]string, 0, len(ids))
	for _, id := range ids {
		labels = append(labels, nodes[id].Label)
	}
	return GraphPath{
		From:       nodes[source].Label,
		To:         nodes[target].Label,
		Nodes:      labels,
		Cost:       dist[target],
		Confidence: 1 / (1 + dist[target]),
	}, true
}

func floydWarshallGraphPaths(nodes []CausalNode, edges []CausalEdge, limit int) []GraphPath {
	if len(nodes) == 0 || len(nodes) > maxFloydWarshallNodes {
		return nil
	}
	indexByID := map[string]int{}
	nodeMap := map[string]CausalNode{}
	for index, node := range nodes {
		indexByID[node.ID] = index
		nodeMap[node.ID] = node
	}
	n := len(nodes)
	dist := make([][]float64, n)
	next := make([][]int, n)
	for i := range dist {
		dist[i] = make([]float64, n)
		next[i] = make([]int, n)
		for j := range dist[i] {
			if i == j {
				dist[i][j] = 0
			} else {
				dist[i][j] = math.Inf(1)
			}
			next[i][j] = -1
		}
	}
	for _, edge := range edges {
		i, iOK := indexByID[edge.From]
		j, jOK := indexByID[edge.To]
		if !iOK || !jOK || edge.Weight >= dist[i][j] {
			continue
		}
		dist[i][j] = edge.Weight
		next[i][j] = j
	}
	for k := 0; k < n; k++ {
		for i := 0; i < n; i++ {
			for j := 0; j < n; j++ {
				if dist[i][k]+dist[k][j] < dist[i][j] {
					dist[i][j] = dist[i][k] + dist[k][j]
					next[i][j] = next[i][k]
				}
			}
		}
	}
	var paths []GraphPath
	for i := 0; i < n; i++ {
		for j := 0; j < n; j++ {
			if i == j || math.IsInf(dist[i][j], 1) {
				continue
			}
			if nodes[i].Kind == nodes[j].Kind {
				continue
			}
			pathIDs := floydPathIDs(nodes, next, i, j)
			if len(pathIDs) < 2 {
				continue
			}
			labels := make([]string, 0, len(pathIDs))
			for _, id := range pathIDs {
				labels = append(labels, nodeMap[id].Label)
			}
			paths = append(paths, GraphPath{
				From:       nodes[i].Label,
				To:         nodes[j].Label,
				Nodes:      labels,
				Cost:       dist[i][j],
				Confidence: 1 / (1 + dist[i][j]),
			})
		}
	}
	sort.Slice(paths, func(i, j int) bool {
		if paths[i].Cost != paths[j].Cost {
			return paths[i].Cost < paths[j].Cost
		}
		return strings.Join(paths[i].Nodes, "->") < strings.Join(paths[j].Nodes, "->")
	})
	if len(paths) > limit {
		paths = paths[:limit]
	}
	return paths
}

func floydPathIDs(nodes []CausalNode, next [][]int, i, j int) []string {
	if next[i][j] < 0 {
		return nil
	}
	path := []string{nodes[i].ID}
	for i != j {
		i = next[i][j]
		if i < 0 {
			return nil
		}
		path = append(path, nodes[i].ID)
	}
	return path
}

func causalOwnerScores(nodes []CausalNode, edges []CausalEdge, loops []NetworkLoopFinding) []OwnerBlameScore {
	scores := map[string]float64{}
	for _, edge := range edges {
		if strings.HasPrefix(edge.From, "owner:") {
			owner := strings.TrimPrefix(edge.From, "owner:")
			scores[owner] += edge.Confidence
		}
	}
	for _, loop := range loops {
		if loop.Owner != "" {
			scores[loop.Owner] += loop.Confidence*2 + math.Min(3, loop.BurnScore/10)
		}
	}
	out := make([]OwnerBlameScore, 0, len(scores))
	for owner, score := range scores {
		out = append(out, OwnerBlameScore{Owner: owner, Score: score})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].Owner < out[j].Owner
	})
	if len(out) > 8 {
		out = out[:8]
	}
	for index := range out {
		out[index].Rank = index + 1
	}
	return out
}

func compareCausalGraphs(baseline, candidate CausalGraph) []CausalDelta {
	var deltas []CausalDelta
	baselineEdges := causalEdgeMap(baseline.Edges)
	candidateEdges := causalEdgeMap(candidate.Edges)
	for key, edge := range candidateEdges {
		base, ok := baselineEdges[key]
		if !ok {
			if edge.Confidence >= 0.35 {
				deltas = append(deltas, CausalDelta{
					Kind:           "новое ребро",
					Severity:       causalDeltaSeverity(edge.Confidence, 0),
					Summary:        fmt.Sprintf("Новое ребро: %s -> %s, доверие %.2f, наблюдений %d.", edge.FromLabel, edge.ToLabel, edge.Confidence, edge.Count),
					CandidateValue: edge.Confidence,
					Delta:          edge.Confidence,
				})
			}
			continue
		}
		delta := edge.Confidence - base.Confidence
		if delta >= 0.25 {
			deltas = append(deltas, CausalDelta{
				Kind:           "усилилось ребро",
				Severity:       causalDeltaSeverity(delta, base.Confidence),
				Summary:        fmt.Sprintf("Ребро усилилось: %s -> %s, доверие %.2f -> %.2f.", edge.FromLabel, edge.ToLabel, base.Confidence, edge.Confidence),
				BaselineValue:  base.Confidence,
				CandidateValue: edge.Confidence,
				Delta:          delta,
			})
		}
	}
	if changedPathDelta, ok := causalChangedPathDelta(baseline.Paths, candidate.Paths); ok {
		deltas = append(deltas, changedPathDelta)
	}
	deltas = append(deltas, causalOwnerScoreDeltas(baseline.OwnerScores, candidate.OwnerScores)...)
	sort.Slice(deltas, func(i, j int) bool {
		if severityRank(deltas[i].Severity) != severityRank(deltas[j].Severity) {
			return severityRank(deltas[i].Severity) > severityRank(deltas[j].Severity)
		}
		if math.Abs(deltas[i].Delta) != math.Abs(deltas[j].Delta) {
			return math.Abs(deltas[i].Delta) > math.Abs(deltas[j].Delta)
		}
		return deltas[i].Summary < deltas[j].Summary
	})
	if len(deltas) > 12 {
		deltas = deltas[:12]
	}
	return deltas
}

func causalChangedPathDelta(baseline, candidate []GraphPath) (CausalDelta, bool) {
	if len(candidate) == 0 {
		return CausalDelta{}, false
	}
	candidatePath := strings.Join(candidate[0].Nodes, " -> ")
	baselinePath := ""
	baselineCost := 0.0
	if len(baseline) > 0 {
		baselinePath = strings.Join(baseline[0].Nodes, " -> ")
		baselineCost = baseline[0].Cost
	}
	if candidatePath == baselinePath {
		return CausalDelta{}, false
	}
	return CausalDelta{
		Kind:           "изменился путь",
		Severity:       "medium",
		Summary:        fmt.Sprintf("Главный кратчайший путь изменился: было `%s`, стало `%s`.", fallbackPathText(baselinePath), candidatePath),
		BaselineValue:  baselineCost,
		CandidateValue: candidate[0].Cost,
		Delta:          candidate[0].Cost - baselineCost,
	}, true
}

func causalOwnerScoreDeltas(baseline, candidate []OwnerBlameScore) []CausalDelta {
	base := ownerScoreMap(baseline)
	cand := ownerScoreMap(candidate)
	var deltas []CausalDelta
	for owner, candidateScore := range cand {
		baselineScore := base[owner]
		delta := candidateScore - baselineScore
		if delta < 1 {
			continue
		}
		deltas = append(deltas, CausalDelta{
			Kind:           "вклад источника",
			Severity:       causalDeltaSeverity(delta, baselineScore),
			Summary:        fmt.Sprintf("Вклад источника `%s` вырос: %.1f -> %.1f.", owner, baselineScore, candidateScore),
			BaselineValue:  baselineScore,
			CandidateValue: candidateScore,
			Delta:          delta,
		})
	}
	return deltas
}

func causalDeltaSeverity(delta, baseline float64) string {
	if delta >= 0.6 || (baseline > 0 && delta/baseline >= 0.6) {
		return "high"
	}
	return "medium"
}

func causalGraphStatus(graph CausalGraph) string {
	if len(graph.Nodes) == 0 || len(graph.Edges) == 0 {
		return "medium"
	}
	for _, score := range graph.OwnerScores {
		if score.Score >= 6 {
			return "high"
		}
	}
	if len(graph.Paths) > 0 {
		return "ok"
	}
	return "medium"
}

func causalGraphSummary(graph CausalGraph) string {
	if len(graph.Nodes) == 0 || len(graph.Edges) == 0 {
		return "Недостаточно агрегированных узлов и ребер для графа причинности."
	}
	return fmt.Sprintf("Построено %d узлов, %d ребер, %d кратчайших объясняющих путей и %d оценок вклада источников.", len(graph.Nodes), len(graph.Edges), len(graph.Paths), len(graph.OwnerScores))
}

func causalGraphFindings(graph CausalGraph) []Finding {
	if len(graph.Nodes) == 0 || len(graph.Edges) == 0 {
		return []Finding{{
			Severity:       "medium",
			Title:          "Граф причинности пуст",
			Detail:         causalGraphSummary(graph),
			Recommendation: "Нужны временные интервалы с контекстом маршрута, источника, экрана или состояния.",
		}}
	}
	if len(graph.Paths) > 0 {
		best := graph.Paths[0]
		return []Finding{{
			Severity:       "ok",
			Title:          "Найден объясняющий путь",
			Detail:         fmt.Sprintf("%s; стоимость %.2f, доверие %.2f.", strings.Join(best.Nodes, " -> "), best.Cost, best.Confidence),
			Recommendation: "Используйте путь как гипотезу: проверьте источник, маршрут и состояние рядом с соответствующим симптомом.",
		}}
	}
	return []Finding{{
		Severity: "medium",
		Title:    "Кратчайший путь не найден",
		Detail:   causalGraphSummary(graph),
	}}
}

func compareCausalGraphStatus(deltas []CausalDelta) string {
	if len(deltas) == 0 {
		return "ok"
	}
	status := "ok"
	for _, delta := range deltas {
		if delta.Severity == "high" {
			return "high"
		}
		if delta.Severity == "medium" {
			status = "medium"
		}
	}
	return status
}

func compareCausalGraphSummary(deltas []CausalDelta) string {
	if len(deltas) == 0 {
		return "Новых или заметно усилившихся причинных ребер не найдено."
	}
	return fmt.Sprintf("Найдено %d изменений графа: новые/усиленные ребра, измененные пути или рост вклада источников.", len(deltas))
}

func compareCausalGraphFindings(deltas []CausalDelta) []Finding {
	for _, delta := range deltas {
		if delta.Severity == "high" || delta.Severity == "medium" {
			return []Finding{{
				Severity:       delta.Severity,
				Title:          "Изменился граф причинности",
				Detail:         delta.Summary,
				Recommendation: "Сравните изменившееся ребро или путь с марковскими состояниями, сетевыми циклами и вкладом источников.",
			}}
		}
	}
	return []Finding{{
		Severity: "ok",
		Title:    "Граф причинности стабилен",
		Detail:   compareCausalGraphSummary(deltas),
	}}
}

func CausalKindLabel(kind string) string {
	switch kind {
	case "state-symptom":
		return "состояние -> симптом"
	case "screen-state":
		return "экран -> состояние"
	case "owner-state":
		return "источник -> состояние"
	case "screen-owner":
		return "экран -> источник"
	case "route-state":
		return "маршрут -> состояние"
	case "owner-route":
		return "источник -> маршрут"
	case "route-phase":
		return "маршрут -> фаза"
	case "phase-symptom":
		return "фаза -> симптом"
	case "route-symptom":
		return "маршрут -> симптом"
	case "loop-owner":
		return "цикл -> источник"
	case "loop-route":
		return "цикл -> маршрут"
	case "loop-phase":
		return "цикл -> фаза"
	case "state":
		return "состояние"
	case "symptom":
		return "симптом"
	case "network":
		return "сеть"
	case "phase":
		return "фаза"
	case "loop":
		return "цикл"
	case "route":
		return "маршрут"
	case "owner":
		return "источник"
	case "screen":
		return "экран"
	default:
		return kind
	}
}

func causalEdgeMap(edges []CausalEdge) map[string]CausalEdge {
	out := make(map[string]CausalEdge, len(edges))
	for _, edge := range edges {
		out[edge.From+"\x00"+edge.To+"\x00"+edge.Kind] = edge
	}
	return out
}

func ownerScoreMap(scores []OwnerBlameScore) map[string]float64 {
	out := make(map[string]float64, len(scores))
	for _, score := range scores {
		out[score.Owner] = score.Score
	}
	return out
}

func causalAdjacency(edges []CausalEdge) map[string][]CausalEdge {
	adjacency := map[string][]CausalEdge{}
	for _, edge := range edges {
		adjacency[edge.From] = append(adjacency[edge.From], edge)
	}
	return adjacency
}

func causalNodeMap(nodes []CausalNode) map[string]CausalNode {
	out := make(map[string]CausalNode, len(nodes))
	for _, node := range nodes {
		out[node.ID] = node
	}
	return out
}

func causalSymptomForState(state string) string {
	switch state {
	case markovNetworkLoop:
		return causalNodeID("symptom", "network_loop")
	case markovNetworkSlow:
		return causalNodeID("symptom", "network_slow")
	case markovJanky:
		return causalNodeID("symptom", "jank")
	case markovStalled:
		return causalNodeID("symptom", "stall")
	case markovMemoryPressure:
		return causalNodeID("symptom", "memory_pressure")
	default:
		return ""
	}
}

func causalBucketStrength(state string) float64 {
	if markovIsBadState(state) {
		return 2
	}
	if state == markovRecovering {
		return 1
	}
	return 0.5
}

func causalNodeID(kind, value string) string {
	if value == "" {
		return ""
	}
	return kind + ":" + value
}

func causalSplitNodeID(id string) (string, string) {
	parts := strings.SplitN(id, ":", 2)
	if len(parts) != 2 {
		return "unknown", id
	}
	return parts[0], parts[1]
}

func causalFallbackLabel(kind, value string) string {
	switch kind {
	case "symptom":
		switch value {
		case "network_loop":
			return "симптом: сетевой цикл"
		case "network_slow":
			return "симптом: медленная сеть"
		case "jank":
			return "симптом: подтормаживание UI"
		case "stall":
			return "симптом: пауза главного потока"
		case "memory_pressure":
			return "симптом: давление памяти"
		}
		return "симптом: " + value
	case "phase":
		return "фаза: " + value
	case "state":
		return MarkovStateLabel(value)
	case "owner":
		return "источник: " + value
	case "route":
		return "маршрут: " + value
	case "screen":
		return "экран: " + value
	case "network":
		return "сеть: " + value
	default:
		return value
	}
}

func fallbackPathText(path string) string {
	if path == "" {
		return "пути не было"
	}
	return path
}

func reverseStrings(values []string) {
	for left, right := 0, len(values)-1; left < right; left, right = left+1, right-1 {
		values[left], values[right] = values[right], values[left]
	}
}
