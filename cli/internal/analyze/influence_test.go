package analyze

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadClassGraphJSONLAndBuildInfluence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "class-graph.jsonl")
	data := `{"format":1,"class":"com.app.feature.CheckoutPresenter","edges":[{"caller":"open()V","calleeClass":"com.app.data.CheckoutRepository","calleeMethod":"load","count":3}]}` + "\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	graph, err := LoadClassGraph(path)
	if err != nil {
		t.Fatalf("LoadClassGraph() error = %v", err)
	}
	if len(graph.Edges) != 1 {
		t.Fatalf("edges = %d, want 1", len(graph.Edges))
	}

	summary := Summary{
		Owners: []OwnerStats{{
			Owner:   "com.app.data.CheckoutRepository.load#abc",
			Count:   3,
			TotalMS: 1400,
			MaxMS:   900,
			Kind:    "http",
		}},
		Flows: []FlowStats{{
			Screen:       "Checkout",
			Flow:         "checkout.open",
			Step:         "network",
			Owner:        "com.app.data.CheckoutRepository.load#abc",
			RouteSample:  "GET /checkout",
			HTTPP95MS:    900,
			ProblemCount: 2,
		}},
	}
	influence := BuildInfluence(summary, graph)
	if !influence.Available || !influence.HasClassGraph {
		t.Fatalf("unexpected influence availability: %+v", influence)
	}
	if len(influence.TopNodes) == 0 || influence.TopNodes[0].ClassName != "com.app.data.CheckoutRepository" {
		t.Fatalf("unexpected top nodes: %+v", influence.TopNodes)
	}
	if len(influence.TopEdges) == 0 || influence.TopEdges[0].To != "com.app.data.CheckoutRepository" {
		t.Fatalf("unexpected top edges: %+v", influence.TopEdges)
	}
}

func TestBuildInfluenceWorksWithoutClassGraph(t *testing.T) {
	influence := BuildInfluence(Summary{
		LogSpam: []LogSpamStats{{
			Owner: "com.app.feature.FeedPresenter.render#abc",
			Count: 240,
		}},
	}, nil)

	if !influence.Available {
		t.Fatalf("expected runtime-only influence")
	}
	if influence.HasClassGraph {
		t.Fatalf("did not expect class graph")
	}
	if len(influence.Heuristic) == 0 {
		t.Fatalf("expected heuristic")
	}
}
