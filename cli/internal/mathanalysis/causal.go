package mathanalysis

import (
	"container/heap"
	"fmt"
	"math"
	"sort"
	"strings"
	"unsafe"
)

const maxFloydWarshallNodes = 60

type causalGraphBuilder struct {
	nodes         map[string]CausalNode
	edges         map[causalEdgeKey]*causalEdgeAgg
	work, results *collectionAccount
	httpWindow    int
	httpNodes     map[causalContextKey]string
}

type causalEdgeKey struct{ from, to, kind string }
type causalContextKey struct{ kind, value string }

type causalEdgeAgg struct {
	from        string
	to          string
	kind        string
	count       int
	httpWindow  int
	strength    float64
	description string
}

func buildCausalGraph(timeline []TimelineBucket, loops []NetworkLoopFinding, markov MarkovModel) CausalGraph {
	return buildCausalGraphWithBudget(timeline, loops, markov, nil)
}

func newCausalGraphBuilder() *causalGraphBuilder {
	return newCausalGraphBuilderWithBudget(nil)
}

func (b *causalGraphBuilder) addTimeline(timeline []TimelineBucket, markov MarkovModel) {
	states := b.work.scratch("causal state index")
	defer states.close()
	if !states.reserve(mathMapBaseBytes) || !states.reserveItems(len(markov.States), mathMapEntryBytes) {
		return
	}
	stateByTime := make(map[uint64]string, len(markov.States))
	for _, state := range markov.States {
		stateByTime[state.TimeMS] = state.State
	}
	for _, bucket := range timeline {
		if !b.work.canReserve(0) {
			return
		}
		state, ok := stateByTime[bucket.StartMS]
		b.addHTTPObservations(bucket.HTTPRouteObservations, state == markovNetworkSlow)
		if !ok {
			continue
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
			b.addNode(ownerID, "место запуска: "+analysisOwnerLabel(bucket.OwnerSample), "owner")
			b.addUndirectedEdge(ownerID, stateID, "owner-state", strength, "источник активен рядом с состоянием")
			if bucket.ScreenSample != "" {
				b.addUndirectedEdge(causalNodeID("screen", bucket.ScreenSample), ownerID, "screen-owner", strength*0.8, "экран и источник совпали во временном интервале")
			}
		}
		if bucket.RouteSample != "" {
			routeID := causalNodeID("route", bucket.RouteSample)
			b.addNode(routeID, "маршрут: "+bucket.RouteSample, "route")
			b.addUndirectedEdge(routeID, stateID, "route-state", strength, "маршрут активен рядом с состоянием")
		}
		if bucket.NetworkSample != "" {
			networkID := causalNodeID("network", bucket.NetworkSample)
			b.addNode(networkID, "сеть: "+bucket.NetworkSample, "network")
			b.addUndirectedEdge(networkID, stateID, "сеть → состояние", strength*0.7, "группа сетевых событий совпала с состоянием")
		}
	}
}

func (b *causalGraphBuilder) addNetworkLoops(loops []NetworkLoopFinding) {
	for _, loop := range loops {
		if !b.work.canReserve(0) {
			return
		}
		strength := 1 + loop.Confidence*3 + math.Min(3, loop.BurnScore/10)
		symptomID := causalNodeID("symptom", "network_loop")
		b.addNode(symptomID, "симптом: сетевой цикл", "symptom")
		if loop.Owner != "" {
			ownerID := causalNodeID("owner", loop.Owner)
			b.addNode(ownerID, "место запуска: "+analysisOwnerLabel(loop.Owner), "owner")
			b.addUndirectedEdge(symptomID, ownerID, "loop-owner", strength, "сетевой цикл связан с источником")
		}
		if loop.Route != "" {
			routeID := causalNodeID("route", loop.Route)
			b.addNode(routeID, "маршрут: "+loop.Route, "route")
			b.addUndirectedEdge(symptomID, routeID, "loop-route", strength, "сетевой цикл связан с маршрутом")
			if loop.Owner != "" {
				b.addUndirectedEdge(causalNodeID("owner", loop.Owner), routeID, "loop-owner-route", strength, "источник связан с маршрутом сетевого цикла")
			}
		}
		for _, token := range loop.Motif {
			if token == "dns_high" {
				b.addUndirectedEdge(symptomID, causalNodeID("phase", "DNS"), "loop-phase", strength, "повторяемая последовательность содержит DNS")
			}
			if token == "connect_high" || token == "reconnect_high" || token == "websocket_reconnect" {
				b.addUndirectedEdge(symptomID, causalNodeID("phase", "connect"), "loop-phase", strength, "повторяемая последовательность содержит соединение или переподключение")
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
	if !b.work.reserve(mathMapEntryBytes+uint64(unsafe.Sizeof(CausalNode{}))) ||
		!b.results.reserve(uint64(len(id)+len(label)+len(kind))) {
		return
	}
	b.nodes[id] = CausalNode{ID: id, Label: label, Kind: kind}
}

func (b *causalGraphBuilder) addUndirectedEdge(from, to, kind string, strength float64, description string) {
	b.addEdge(from, to, kind, strength, description)
	b.addEdge(to, from, kind, strength, description)
}

func (b *causalGraphBuilder) addEdge(from, to, kind string, strength float64, description string) {
	b.addEdgeCount(from, to, kind, strength, description, 1)
}

func (b *causalGraphBuilder) addEdgeCount(from, to, kind string, strength float64, description string, count int) {
	edge := b.edge(from, to, kind, description)
	if edge == nil {
		return
	}
	edge.count += count
	edge.strength += math.Max(0.1, strength)
}

func (b *causalGraphBuilder) edge(from, to, kind, description string) *causalEdgeAgg {
	if from == "" || to == "" || from == to {
		return nil
	}
	b.ensureKnownNode(from)
	b.ensureKnownNode(to)
	if !b.work.canReserve(0) {
		return nil
	}
	// Reuse canonical node IDs instead of retaining newly formatted copies per edge.
	from, to = b.nodes[from].ID, b.nodes[to].ID
	key := causalEdgeKey{from, to, kind}
	edge := b.edges[key]
	if edge == nil {
		if !b.work.reserve(mathMapEntryBytes+uint64(unsafe.Sizeof(causalEdgeAgg{}))+uint64(unsafe.Sizeof(key))) ||
			!b.results.reserve(uint64(len(kind)+len(description))) {
			return nil
		}
		edge = &causalEdgeAgg{from: from, to: to, kind: kind, description: description}
		b.edges[key] = edge
	}
	return edge
}

func (b *causalGraphBuilder) ensureKnownNode(id string) {
	if _, ok := b.nodes[id]; ok {
		return
	}
	kind, value := causalSplitNodeID(id)
	b.addNode(id, causalFallbackLabel(kind, value), kind)
}

func (b *causalGraphBuilder) nodeList() []CausalNode {
	if !b.results.reserveItems(len(b.nodes), uint64(unsafe.Sizeof(CausalNode{}))) {
		return nil
	}
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
	if !b.results.reserveItems(len(b.edges), uint64(unsafe.Sizeof(CausalEdge{}))) {
		return nil
	}
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
		if edges[i].From != edges[j].From {
			return edges[i].From < edges[j].From
		}
		if edges[i].To != edges[j].To {
			return edges[i].To < edges[j].To
		}
		return edges[i].Kind < edges[j].Kind
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
	paths := make([]GraphPath, 0, 6)
	for _, source := range sources {
		distances, previous := causalShortestPathTree(adjacency, source)
		bestTargets := make([]causalPathTarget, 0, 6)
		for _, target := range targets {
			cost, reachable := distances[target]
			if !reachable {
				continue
			}
			bestTargets = retainCausalPathTarget(bestTargets, causalPathTarget{id: target, cost: cost}, 6)
		}
		for _, target := range bestTargets {
			path, ok := causalGraphPath(nodeMap, distances, previous, source, target.id)
			if ok && len(path.Nodes) > 1 {
				paths = retainGraphPath(paths, path, 6)
			}
		}
	}
	return paths
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
	distances, previous := causalShortestPathTree(adjacency, source)
	return causalGraphPath(nodes, distances, previous, source, target)
}

type causalPathQueueItem struct {
	id   string
	cost float64
}

type causalPathQueue []causalPathQueueItem

func (queue causalPathQueue) Len() int { return len(queue) }

func (queue causalPathQueue) Less(i, j int) bool {
	if queue[i].cost != queue[j].cost {
		return queue[i].cost < queue[j].cost
	}
	return queue[i].id < queue[j].id
}

func (queue causalPathQueue) Swap(i, j int) { queue[i], queue[j] = queue[j], queue[i] }

func (queue *causalPathQueue) Push(value any) {
	*queue = append(*queue, value.(causalPathQueueItem))
}

func (queue *causalPathQueue) Pop() any {
	previous := *queue
	last := len(previous) - 1
	value := previous[last]
	*queue = previous[:last]
	return value
}

func causalShortestPathTree(
	adjacency map[string][]CausalEdge,
	source string,
) (map[string]float64, map[string]string) {
	distances := map[string]float64{source: 0}
	previous := make(map[string]string)
	visited := make(map[string]struct{})
	queue := causalPathQueue{{id: source, cost: 0}}
	for queue.Len() > 0 {
		current := heap.Pop(&queue).(causalPathQueueItem)
		if _, done := visited[current.id]; done {
			continue
		}
		best, currentReachable := distances[current.id]
		if !currentReachable || current.cost != best {
			continue
		}
		visited[current.id] = struct{}{}
		for _, edge := range adjacency[current.id] {
			candidate := current.cost + edge.Weight
			known, reachable := distances[edge.To]
			if reachable && candidate > known {
				continue
			}
			if reachable && candidate == known && previous[edge.To] != "" && current.id >= previous[edge.To] {
				continue
			}
			distances[edge.To] = candidate
			previous[edge.To] = current.id
			heap.Push(&queue, causalPathQueueItem{id: edge.To, cost: candidate})
		}
	}
	return distances, previous
}

func causalGraphPath(
	nodes map[string]CausalNode,
	distances map[string]float64,
	previous map[string]string,
	source,
	target string,
) (GraphPath, bool) {
	cost, reachable := distances[target]
	if !reachable {
		return GraphPath{}, false
	}
	ids := []string{target}
	for ids[len(ids)-1] != source {
		parent := previous[ids[len(ids)-1]]
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
		Cost:       cost,
		Confidence: 1 / (1 + cost),
	}, true
}

type causalPathTarget struct {
	id   string
	cost float64
}

func retainCausalPathTarget(values []causalPathTarget, candidate causalPathTarget, limit int) []causalPathTarget {
	insert := len(values)
	for index, current := range values {
		if candidate.cost < current.cost || (candidate.cost == current.cost && candidate.id < current.id) {
			insert = index
			break
		}
	}
	if len(values) < limit {
		values = append(values, candidate)
		copy(values[insert+1:], values[insert:len(values)-1])
		values[insert] = candidate
	} else if insert < limit {
		copy(values[insert+1:], values[insert:limit-1])
		values[insert] = candidate
	}
	return values
}

func retainGraphPath(paths []GraphPath, candidate GraphPath, limit int) []GraphPath {
	insert := len(paths)
	for index, current := range paths {
		if graphPathBetter(candidate, current) {
			insert = index
			break
		}
	}
	if len(paths) < limit {
		paths = append(paths, candidate)
		copy(paths[insert+1:], paths[insert:len(paths)-1])
		paths[insert] = candidate
	} else if insert < limit {
		copy(paths[insert+1:], paths[insert:limit-1])
		paths[insert] = candidate
	}
	return paths
}

func graphPathBetter(left, right GraphPath) bool {
	if left.Cost != right.Cost {
		return left.Cost < right.Cost
	}
	common := min(len(left.Nodes), len(right.Nodes))
	for index := range common {
		if left.Nodes[index] != right.Nodes[index] {
			return left.Nodes[index] < right.Nodes[index]
		}
	}
	return len(left.Nodes) < len(right.Nodes)
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
	paths := make([]GraphPath, 0, limit)
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
			paths = retainGraphPath(paths, GraphPath{
				From:       nodes[i].Label,
				To:         nodes[j].Label,
				Nodes:      labels,
				Cost:       dist[i][j],
				Confidence: 1 / (1 + dist[i][j]),
			}, limit)
		}
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
		if edge.Kind != "owner-state" || !strings.HasPrefix(edge.From, "owner:") || !strings.HasPrefix(edge.To, "state:") {
			continue
		}
		state := strings.TrimPrefix(edge.To, "state:")
		if !markovIsBadState(state) {
			continue
		}
		owner := strings.TrimPrefix(edge.From, "owner:")
		if !analysisOwnerIsKnown(owner) {
			continue
		}
		scores[owner] += edge.Confidence
	}
	for _, loop := range loops {
		if analysisOwnerIsKnown(loop.Owner) {
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
	deltas := make([]CausalDelta, 0, 12)
	baselineEdges := causalEdgeMap(baseline.Edges)
	candidateEdges := causalEdgeMap(candidate.Edges)
	for key, edge := range candidateEdges {
		base, ok := baselineEdges[key]
		if !ok {
			if edge.Confidence >= 0.35 {
				deltas = retainCausalDelta(deltas, CausalDelta{
					Kind:           "новая связь",
					Severity:       causalDeltaSeverity(edge.Confidence, 0),
					Summary:        fmt.Sprintf("Новая статистическая связь: %s ↔ %s, надёжность %.2f, совместных наблюдений %d. Она не доказывает направление причины.", edge.FromLabel, edge.ToLabel, edge.Confidence, edge.Count),
					CandidateValue: edge.Confidence,
					Delta:          edge.Confidence,
				})
			}
			continue
		}
		delta := edge.Confidence - base.Confidence
		if delta >= 0.25 {
			deltas = retainCausalDelta(deltas, CausalDelta{
				Kind:           "усилилась связь",
				Severity:       causalDeltaSeverity(delta, base.Confidence),
				Summary:        fmt.Sprintf("Статистическая связь усилилась: %s ↔ %s, надёжность %.2f → %.2f. Это приоритет проверки, а не доказанная причина.", edge.FromLabel, edge.ToLabel, base.Confidence, edge.Confidence),
				BaselineValue:  base.Confidence,
				CandidateValue: edge.Confidence,
				Delta:          delta,
			})
		}
	}
	if changedPathDelta, ok := causalChangedPathDelta(baseline.Paths, candidate.Paths); ok {
		deltas = retainCausalDelta(deltas, changedPathDelta)
	}
	for _, delta := range causalOwnerScoreDeltas(baseline.OwnerScores, candidate.OwnerScores) {
		deltas = retainCausalDelta(deltas, delta)
	}
	return deltas
}

func retainCausalDelta(deltas []CausalDelta, candidate CausalDelta) []CausalDelta {
	insert := len(deltas)
	for i, current := range deltas {
		if causalDeltaLess(candidate, current) {
			insert = i
			break
		}
	}
	if len(deltas) < 12 {
		deltas = append(deltas, candidate)
	} else if insert == 12 {
		return deltas
	}
	copy(deltas[insert+1:], deltas[insert:len(deltas)-1])
	deltas[insert] = candidate
	return deltas
}

func causalDeltaLess(a, b CausalDelta) bool {
	if severityRank(a.Severity) != severityRank(b.Severity) {
		return severityRank(a.Severity) > severityRank(b.Severity)
	}
	if math.Abs(a.Delta) != math.Abs(b.Delta) {
		return math.Abs(a.Delta) > math.Abs(b.Delta)
	}
	return a.Summary < b.Summary
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
	baselineConfidence := 0.0
	if len(baseline) > 0 {
		baselineConfidence = baseline[0].Confidence
	}
	if len(baseline) > 0 && candidate[0].Confidence-baselineConfidence < 0.15 {
		return CausalDelta{}, false
	}
	return CausalDelta{
		Kind:           "усилилась цепочка связей",
		Severity:       "medium",
		Summary:        fmt.Sprintf("Самая сильная цепочка связей изменилась: было `%s`, стало `%s`; надёжность %.2f → %.2f. Направление причины не установлено.", fallbackPathText(baselinePath), candidatePath, baselineConfidence, candidate[0].Confidence),
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
	return "medium"
}

func causalGraphStatus(graph CausalGraph) string {
	if len(graph.Nodes) == 0 || len(graph.Edges) == 0 {
		return "medium"
	}
	if len(graph.Paths) > 0 {
		return "medium"
	}
	return "ok"
}

func causalGraphSummary(graph CausalGraph) string {
	if len(graph.Nodes) == 0 || len(graph.Edges) == 0 {
		return "Недостаточно наблюдаемых связей для графа гипотез."
	}
	return fmt.Sprintf("Построено %d узлов, %d направлений для %d пар связей, %d цепочек от симптомов и %d оценок связи источников с плохими состояниями. Граф показывает совместные наблюдения, а не доказанные причины.", len(graph.Nodes), len(graph.Edges), len(graph.Edges)/2, len(graph.Paths), len(graph.OwnerScores))
}

func causalGraphFindings(graph CausalGraph) []Finding {
	if len(graph.Nodes) == 0 || len(graph.Edges) == 0 {
		return []Finding{{
			Severity:       "medium",
			Title:          "Для графа гипотез недостаточно данных",
			Detail:         causalGraphSummary(graph),
			Recommendation: "Нужны временные интервалы с контекстом маршрута, источника, экрана или состояния.",
		}}
	}
	if len(graph.Paths) > 0 {
		best := graph.Paths[0]
		return []Finding{{
			Severity:       "medium",
			Title:          "Есть цепочка связей для проверки",
			Detail:         fmt.Sprintf("%s; условная стоимость %.2f, надёжность %.2f. Цепочка не устанавливает направление причины.", strings.Join(best.Nodes, " ↔ "), best.Cost, best.Confidence),
			Recommendation: "Проверьте эту гипотезу по сырым событиям, трассировке и коду источника. Не считайте источник виновником только по графу.",
		}}
	}
	return []Finding{{
		Severity: "medium",
		Title:    "Кратчайший путь не найден",
		Detail:   causalGraphSummary(graph),
	}}
}

func compareCausalGraphSummary(deltas []CausalDelta) string {
	if len(deltas) == 0 {
		return "Новых или заметно усилившихся статистических связей не найдено."
	}
	return fmt.Sprintf("Найдено %d изменений графа гипотез: новые или усиленные связи, более сильные цепочки либо рост связи источника с плохими состояниями.", len(deltas))
}

func compareCausalGraphFindings(deltas []CausalDelta) []Finding {
	for _, delta := range deltas {
		if delta.Severity == "high" || delta.Severity == "medium" {
			return []Finding{{
				Severity:       delta.Severity,
				Title:          "Изменился граф связей и гипотез",
				Detail:         delta.Summary,
				Recommendation: "Сравните изменившееся ребро или путь с марковскими состояниями, сетевыми циклами и вкладом источников.",
			}}
		}
	}
	return []Finding{{
		Severity: "ok",
		Title:    "Граф связей заметно не изменился",
		Detail:   compareCausalGraphSummary(deltas),
	}}
}

func CausalKindLabel(kind string) string {
	switch kind {
	case "state-symptom":
		return "состояние ↔ симптом"
	case "screen-state":
		return "экран ↔ состояние"
	case "owner-state":
		return "источник ↔ состояние"
	case "screen-owner":
		return "экран ↔ источник"
	case "route-state":
		return "маршрут ↔ состояние"
	case "owner-route":
		return "контекст HTTP ↔ маршрут"
	case "loop-owner-route":
		return "источник ↔ маршрут цикла"
	case "route-phase":
		return "маршрут ↔ фаза"
	case "phase-symptom":
		return "фаза ↔ симптом"
	case "route-symptom":
		return "маршрут ↔ симптом"
	case "loop-owner":
		return "цикл ↔ источник"
	case "loop-route":
		return "цикл ↔ маршрут"
	case "loop-phase":
		return "цикл ↔ фаза"
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

func causalEdgeMap(edges []CausalEdge) map[causalEdgeKey]CausalEdge {
	out := make(map[causalEdgeKey]CausalEdge, len(edges))
	for _, edge := range edges {
		left := edge.From
		right := edge.To
		if left > right {
			left, right = right, left
		}
		key := causalEdgeKey{left, right, edge.Kind}
		if _, ok := out[key]; ok {
			continue
		}
		out[key] = edge
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
		return "место запуска: " + analysisOwnerLabel(value)
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
