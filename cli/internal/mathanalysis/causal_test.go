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

func TestShortestGraphPathChoosesStableLexicographicEqualCostRoute(t *testing.T) {
	nodes := map[string]CausalNode{
		"symptom:jank": {ID: "symptom:jank", Label: "symptom", Kind: "symptom"},
		"state:alpha":  {ID: "state:alpha", Label: "alpha", Kind: "state"},
		"state:zeta":   {ID: "state:zeta", Label: "zeta", Kind: "state"},
		"owner:Feed":   {ID: "owner:Feed", Label: "owner", Kind: "owner"},
	}
	edges := []CausalEdge{
		{From: "symptom:jank", To: "state:zeta", Weight: 0.5},
		{From: "state:zeta", To: "owner:Feed", Weight: 1.5},
		{From: "symptom:jank", To: "state:alpha", Weight: 1},
		{From: "state:alpha", To: "owner:Feed", Weight: 1},
	}

	for iteration := range 200 {
		path, ok := shortestGraphPath(nodes, edges, "symptom:jank", "owner:Feed")
		if !ok {
			t.Fatalf("iteration %d: shortestGraphPath() returned no path", iteration)
		}
		if got, want := path.Nodes, []string{"symptom", "alpha", "owner"}; !equalStrings(got, want) {
			t.Fatalf("iteration %d: path.Nodes = %v, want %v", iteration, got, want)
		}
		assertFloat(t, path.Cost, 2)
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

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
