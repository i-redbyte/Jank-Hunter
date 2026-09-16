package mathanalysis

import (
	"fmt"
	"math/rand"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func writeManyJointHTTPFixture(t testing.TB, count int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "many.jhlog")
	file, writer, err := jhlog.CreateWithHeader(path, jhlog.DefaultSegmentHeader())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < count; i++ {
		for _, entry := range []jhlog.DictionaryEntry{
			{Kind: jhlog.DictOwner, ID: uint64(2*i + 1), Value: fmt.Sprintf("Owner%d", i/32)},
			{Kind: jhlog.DictRoute, ID: uint64(2*i + 2), Value: fmt.Sprintf("GET /%d", i%32)},
		} {
			if err := writer.WriteEvent(jhlog.Event{Type: jhlog.EventDictionary, Dictionary: &entry}); err != nil {
				t.Fatal(err)
			}
		}
		event := jhlog.Event{Type: jhlog.EventHTTP, TimeMS: 100, HTTP: &jhlog.HTTPEvent{RouteRef: jhlog.LocalSymbol(uint64(2*i + 2)), DurationMS: 100, Status: jhlog.Status2xx}}
		event.Attribution.Present = true
		event.Attribution.Owner = jhlog.LocalSymbol(uint64(2*i + 1))
		if err := writer.WriteEvent(event); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCausalGraphCannotEscapeSharedCollectionBudget(t *testing.T) {
	paths := []string{writeManyJointHTTPFixture(t, 512)}
	for _, compare := range []bool{false, true} {
		budget := newCollectionBudget(0)
		if _, err := analyzeMathInputsWithBudget(paths, analyze.Options{}, budget); err != nil {
			t.Fatal(err)
		}
		if compare {
			if _, err := analyzeMathInputsWithBudget(paths, analyze.Options{}, budget); err != nil {
				t.Fatal(err)
			}
		}
		options := analyze.Options{MathMemoryLimitBytes: budget.peak + 64*1024}
		var limits []CollectionLimit
		if compare {
			report, err := AnalyzeCompareWithSummaries(paths, paths, options, analyze.Summary{}, analyze.Summary{})
			if err != nil {
				t.Fatal(err)
			}
			limits = report.CollectionLimits
			if len(report.CausalDeltas) != 0 {
				t.Fatal("partial comparison escaped exhausted graph budget")
			}
		} else {
			report, err := AnalyzeInspectWithSummary(paths, options, analyze.Summary{})
			if err != nil {
				t.Fatal(err)
			}
			limits = report.CollectionLimits
			if len(report.CausalGraph.Nodes) != 0 {
				t.Error("partial graph escaped exhausted budget")
			}
		}
		if len(limits) != 1 || !strings.HasPrefix(limits[0].Component, "causal") {
			t.Errorf("compare=%v: inputs fit but graph allocations were not charged: %+v (input peak=%d)", compare, limits, budget.peak)
		}
	}
}

func writeJointHTTPFixture(t testing.TB, process byte) string {
	t.Helper()
	header := jhlog.DefaultSegmentHeader()
	header.RunID = jhlog.ID128{1}
	header.ProcessInstanceID = jhlog.ID128{process}
	header.SessionID = jhlog.ID128{process}
	path := filepath.Join(t.TempDir(), fmt.Sprintf("joint-%d.jhlog", process))
	file, writer, err := jhlog.CreateWithHeader(path, header)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range []jhlog.DictionaryEntry{{Kind: jhlog.DictOwner, ID: 1, Value: "OwnerA"}, {Kind: jhlog.DictOwner, ID: 2, Value: "OwnerB"},
		{Kind: jhlog.DictRoute, ID: 3, Value: "GET /a"}, {Kind: jhlog.DictRoute, ID: 4, Value: "GET /b"}, {Kind: jhlog.DictRoute, ID: 5, Value: "GET /c"}} {
		if err := writer.WriteEvent(jhlog.Event{Type: jhlog.EventDictionary, Dictionary: &entry}); err != nil {
			t.Fatal(err)
		}
	}
	count := 5
	if process == 1 {
		count = 6
	}
	for index := 0; index < count; index++ {
		route := uint64(3)
		if process == 1 {
			route = 4 + uint64(index%2)
		}
		event := jhlog.Event{Type: jhlog.EventHTTP, TimeMS: 100 + uint64(index)*10, HTTP: &jhlog.HTTPEvent{RouteRef: jhlog.LocalSymbol(route), DurationMS: 100, Status: jhlog.Status2xx}}
		event.Attribution.Present = true
		event.Attribution.Owner = jhlog.LocalSymbol(uint64(process))
		if process == 1 {
			event.HTTP.DNSMS = 5
			event.HTTP.ConnectMS = 7
			event.Flags = uint64(jhlog.FlagHTTPFailed)
			event.HTTP.Status = jhlog.Status5xx
		}
		if err := writer.WriteEvent(event); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func jointFixtureGraph(t *testing.T, paths []string, withState bool) CausalGraph {
	t.Helper()
	inputs, err := analyzeMathInputs(paths, analyze.Options{})
	if err != nil {
		t.Fatal(err)
	}
	model := MarkovModel{}
	if withState {
		for _, bucket := range inputs.Timeline {
			model.States = append(model.States, MarkovBucketState{TimeMS: bucket.StartMS, State: markovNetworkSlow})
		}
	}
	return buildCausalGraph(inputs.Timeline, nil, model)
}

func jointEdge(graph CausalGraph, from, to, kind string) (CausalEdge, bool) {
	for _, edge := range graph.Edges {
		if edge.From == from && edge.To == to && edge.Kind == kind {
			return edge, true
		}
	}
	return CausalEdge{}, false
}

func TestCausalOwnerRouteUsesJointEventsAcrossProcesses(t *testing.T) {
	paths := []string{writeJointHTTPFixture(t, 1), writeJointHTTPFixture(t, 2)}
	for _, withState := range []bool{true, false} {
		for reverse := 0; reverse < 2; reverse++ {
			if reverse == 1 {
				paths[0], paths[1] = paths[1], paths[0]
			}
			graph := jointFixtureGraph(t, paths, withState)
			if edge, ok := jointEdge(graph, "owner:OwnerA", "route:GET /a", "owner-route"); ok {
				t.Errorf("independent marginal modes fabricated a caller pair: %+v", edge)
			}
			for _, pair := range []struct {
				owner, route string
				count        int
			}{{"OwnerA", "GET /b", 3}, {"OwnerA", "GET /c", 3}, {"OwnerB", "GET /a", 5}} {
				edge, ok := jointEdge(graph, "owner:"+pair.owner, "route:"+pair.route, "owner-route")
				if !ok || edge.Count != pair.count {
					t.Errorf("state=%v reverse=%d actual pair %s/%s: edge=%+v exists=%v want count%d", withState, reverse, pair.owner, pair.route, edge, ok, pair.count)
				}
			}
		}
	}
}

func TestCausalNetworkPhasesBelongToTheirObservedRoute(t *testing.T) {
	graph := jointFixtureGraph(t, []string{writeJointHTTPFixture(t, 1), writeJointHTTPFixture(t, 2)}, true)
	for _, phase := range []string{"DNS", "connect"} {
		if edge, ok := jointEdge(graph, "route:GET /a", "phase:"+phase, "route-phase"); ok {
			t.Errorf("another route's phase assigned to RouteA: %+v", edge)
		}
		for _, route := range []string{"GET /b", "GET /c"} {
			if _, ok := jointEdge(graph, "route:"+route, "phase:"+phase, "route-phase"); !ok {
				t.Errorf("observed phase absent: %s/%s", route, phase)
			}
		}
	}
	if edge, ok := jointEdge(graph, "route:GET /a", "symptom:network_slow", "route-symptom"); ok {
		t.Errorf("another route's failure assigned to RouteA: %+v", edge)
	}
}

func TestCausalRoutePhaseConfidenceDoesNotDependOnOwnerPartition(t *testing.T) {
	graphs := []CausalGraph{
		buildCausalGraph([]TimelineBucket{{HTTPRouteObservations: []HTTPRouteObservation{{Route: "/a", Owner: "A", Count: 4, DNSCount: 4}}}}, nil, MarkovModel{}),
		buildCausalGraph([]TimelineBucket{{HTTPRouteObservations: []HTTPRouteObservation{{Route: "/a", Owner: "A", Count: 2, DNSCount: 2}, {Route: "/a", Owner: "B", Count: 2, DNSCount: 2}}}}, nil, MarkovModel{}),
	}
	for _, phase := range []string{"HTTP", "DNS"} {
		a, okA := jointEdge(graphs[0], "route:/a", "phase:"+phase, "route-phase")
		b, okB := jointEdge(graphs[1], "route:/a", "phase:"+phase, "route-phase")
		if !okA || !okB || a.Count != 4 || b.Count != 4 || a.Confidence != b.Confidence {
			t.Errorf("owner partition changed route observations: %+v / %+v", a, b)
		}
	}
}

func TestCausalJointObservationsRespectMappingAndFilters(t *testing.T) {
	for _, mapped := range []bool{false, true} {
		for _, stable := range []bool{false, true} {
			path, mapping := writeFilterParityFixture(t, mapped, stable)
			inputs, err := analyzeMathInputs([]string{path}, analyze.Options{ObfuscationMap: mapping, Filter: analyze.Filter{OwnerContains: "FeedOwner", RouteContains: "/feed"}})
			if err != nil {
				t.Fatal(err)
			}
			graph := buildCausalGraph(inputs.Timeline, nil, MarkovModel{})
			edge, ok := jointEdge(graph, "owner:example.FeedOwner", "route:GET /feed", "owner-route")
			if !ok || edge.Count != 3 {
				t.Fatalf("mapped=%v stable=%v: %+v", mapped, stable, edge)
			}
			for _, edge := range graph.Edges {
				if strings.Contains(edge.From, "Other") || strings.Contains(edge.To, "other") {
					t.Fatal("excluded event entered joint graph")
				}
			}
		}
	}
}

func TestCausalUnknownOwnerDoesNotInventCallerOrSlowNetwork(t *testing.T) {
	graph := buildCausalGraph([]TimelineBucket{{HTTPRouteObservations: []HTTPRouteObservation{{Route: "/a", Count: 3, DNSCount: 3}}}}, nil, MarkovModel{})
	for _, edge := range graph.Edges {
		if edge.Kind == "owner-route" || edge.Kind == "phase-symptom" {
			t.Fatalf("invented context: %+v", edge)
		}
	}
	if edge, ok := jointEdge(graph, "route:/a", "phase:DNS", "route-phase"); !ok || edge.Count != 3 {
		t.Fatal("unknown owner discarded measured route phase")
	}
}

func TestCausalGraphStorageDependsOnUniqueEdgesAndReleasesScratch(t *testing.T) {
	observation := HTTPRouteObservation{Owner: "A", Route: "/a", Count: 2, DNSCount: 1}
	var retained uint64
	for _, count := range []int{1, 10000} {
		timeline := make([]TimelineBucket, count)
		for i := range timeline {
			timeline[i].HTTPRouteObservations = []HTTPRouteObservation{observation}
		}
		budget := newCollectionBudget(128 * 1024)
		graph := buildCausalGraphWithBudget(timeline, nil, MarkovModel{}, budget)
		if budget.err() != nil {
			t.Fatal(budget.err())
		}
		edge, ok := jointEdge(graph, "owner:A", "route:/a", "owner-route")
		if !ok || edge.Count != 2*count {
			t.Fatalf("lost repeated observations: %+v", edge)
		}
		if budget.used == 0 || budget.used >= budget.peak {
			t.Fatalf("results or released scratch unaccounted: %+v", budget)
		}
		if retained != 0 && retained != budget.used {
			t.Fatalf("per-event graph retention: %d -> %d", retained, budget.used)
		}
		retained = budget.used
	}
}

func TestCausalComparisonOwnsResultsAndRejectsInsufficientBudget(t *testing.T) {
	graph := buildCausalGraph([]TimelineBucket{{HTTPRouteObservations: []HTTPRouteObservation{{Owner: "A", Route: "/a", Count: 2}}}, {HTTPRouteObservations: []HTTPRouteObservation{{Owner: "A", Route: "/a", Count: 3}}}, {HTTPRouteObservations: []HTTPRouteObservation{{Owner: "A", Route: "/a", Count: 4}}}}, nil, MarkovModel{})
	budget := newCollectionBudget(128 * 1024)
	result := compareCausalGraphsWithBudget(CausalGraph{}, graph, budget)
	if !reflect.DeepEqual(result, compareCausalGraphs(CausalGraph{}, graph)) || len(result) == 0 || budget.used == 0 || budget.used >= budget.peak {
		t.Fatalf("comparison ownership: %+v", budget)
	}
	limited := newCollectionBudget(budget.peak - 1)
	if got := compareCausalGraphsWithBudget(CausalGraph{}, graph, limited); len(got) != 0 || limited.err() == nil || limited.used != 0 {
		t.Fatal("partial deltas or reservations survived exhausted comparison")
	}
}

func TestCausalRepeatedKnownHTTPContextsDoNotAllocate(t *testing.T) {
	b := newCausalGraphBuilder()
	observations := []HTTPRouteObservation{{Owner: "com.example.FeaturePresenter.load", Route: "GET /api/feed/items", Count: 2, DNSCount: 1, ConnectCount: 1}}
	b.addHTTPObservations(observations, true)
	if allocs := testing.AllocsPerRun(100, func() { b.addHTTPObservations(observations, true) }); allocs != 0 {
		t.Fatalf("repeated known nodes and edges allocate %.0f times per observation", allocs)
	}
}

func TestCausalFloydTopPathsMatchIndependentBreadthFirstDistances(t *testing.T) {
	for seed := int64(0); seed < 32; seed++ {
		rng := rand.New(rand.NewSource(seed))
		nodes := make([]CausalNode, 16)
		adjacency := make([][]int, len(nodes))
		var edges []CausalEdge
		for i := range nodes {
			nodes[i] = CausalNode{ID: fmt.Sprint(i), Label: fmt.Sprint(i), Kind: fmt.Sprint(i % 2)}
		}
		for i := range nodes {
			for j := range nodes {
				if i != j && rng.Intn(4) == 0 {
					adjacency[i] = append(adjacency[i], j)
					edges = append(edges, CausalEdge{From: nodes[i].ID, To: nodes[j].ID, Weight: 1})
				}
			}
		}
		var costs []int
		for source := range nodes {
			distance := make([]int, len(nodes))
			for i := range distance {
				distance[i] = -1
			}
			distance[source] = 0
			queue := []int{source}
			for head := 0; head < len(queue); head++ {
				for _, target := range adjacency[queue[head]] {
					if distance[target] < 0 {
						distance[target] = distance[queue[head]] + 1
						queue = append(queue, target)
					}
				}
			}
			for target, distance := range distance {
				if nodes[source].Kind != nodes[target].Kind && distance > 0 {
					costs = append(costs, distance)
				}
			}
		}
		sort.Ints(costs)
		paths := floydWarshallGraphPaths(nodes, edges, 6)
		if len(paths) != min(6, len(costs)) || cap(paths) > 6 {
			t.Fatalf("seed%d: incomplete or oversized top paths", seed)
		}
		for i, path := range paths {
			if path.Cost != float64(costs[i]) || len(path.Nodes)-1 != costs[i] {
				t.Fatalf("seed%d: path%d=%+v want%d", seed, i, path, costs[i])
			}
		}
	}
}
