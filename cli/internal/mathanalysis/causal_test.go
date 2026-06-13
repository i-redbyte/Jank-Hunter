package mathanalysis

import "testing"

func TestShortestGraphPathUsesLowerCostRoute(t *testing.T) {
	nodes := map[string]CausalNode{
		"symptom:jank": {ID: "symptom:jank", Label: "симптом: jank", Kind: "symptom"},
		"state:Janky":  {ID: "state:Janky", Label: "Jank", Kind: "state"},
		"owner:Feed":   {ID: "owner:Feed", Label: "источник: Feed", Kind: "owner"},
	}
	edges := []CausalEdge{
		{From: "symptom:jank", To: "owner:Feed", Weight: 5, Confidence: 0.2},
		{From: "symptom:jank", To: "state:Janky", Weight: 1, Confidence: 0.8},
		{From: "state:Janky", To: "owner:Feed", Weight: 1, Confidence: 0.8},
	}

	path, ok := shortestGraphPath(nodes, edges, "symptom:jank", "owner:Feed")
	if !ok {
		t.Fatalf("shortestGraphPath() returned no path")
	}
	assertFloat(t, path.Cost, 2)
	if got, want := len(path.Nodes), 3; got != want {
		t.Fatalf("len(path.Nodes) = %d, want %d: %+v", got, want, path.Nodes)
	}
	if path.Nodes[1] != "Jank" {
		t.Fatalf("path.Nodes[1] = %q, want Jank: %+v", path.Nodes[1], path.Nodes)
	}
}

func TestFloydWarshallGraphPathsFindsAllPairsPath(t *testing.T) {
	nodes := []CausalNode{
		{ID: "symptom:network_slow", Label: "симптом: медленная сеть", Kind: "symptom"},
		{ID: "route:GET /config", Label: "маршрут: GET /config", Kind: "route"},
		{ID: "owner:Config", Label: "источник: Config", Kind: "owner"},
	}
	edges := []CausalEdge{
		{From: "symptom:network_slow", To: "route:GET /config", Weight: 1, Confidence: 0.8},
		{From: "route:GET /config", To: "owner:Config", Weight: 2, Confidence: 0.6},
	}

	paths := floydWarshallGraphPaths(nodes, edges, 10)
	for _, path := range paths {
		if path.From == "симптом: медленная сеть" && path.To == "источник: Config" {
			assertFloat(t, path.Cost, 3)
			return
		}
	}
	t.Fatalf("Floyd-Warshall path symptom -> owner not found: %+v", paths)
}

func TestBuildCausalGraphConnectsSymptomToOwner(t *testing.T) {
	timeline := []TimelineBucket{{
		StartMS:           0,
		EndMS:             1000,
		HTTPCount:         1,
		HTTPFailed:        1,
		HTTPP95DurationMS: 800,
		RouteSample:       "GET /config",
		OwnerSample:       "ConfigRepository.refresh",
	}}
	markov := buildMarkovModel(timeline, nil)

	graph := buildCausalGraph(timeline, nil, markov)
	for _, path := range graph.Paths {
		if path.From == "симптом: медленная сеть" && path.To == "источник: ConfigRepository.refresh" {
			return
		}
	}
	t.Fatalf("causal graph did not connect network symptom to owner: %+v", graph.Paths)
}
