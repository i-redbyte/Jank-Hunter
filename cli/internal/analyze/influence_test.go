package analyze

import (
	"fmt"
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
		RuntimeCalls: []RuntimeCallStats{{
			Screen:  "Checkout",
			Flow:    "checkout.open",
			Step:    "network",
			Caller:  "com.app.feature.CheckoutPresenter.open#def",
			Callee:  "com.app.data.CheckoutRepository.load#abc",
			Count:   3,
			TotalMS: 600,
			MaxMS:   240,
		}},
	}
	influence := BuildInfluence(summary, graph)
	if !influence.Available || !influence.HasClassGraph || !influence.HasRuntimeGraph {
		t.Fatalf("unexpected influence availability: %+v", influence)
	}
	if len(influence.TopNodes) == 0 || influence.TopNodes[0].ClassName != "com.app.data.CheckoutRepository" {
		t.Fatalf("unexpected top nodes: %+v", influence.TopNodes)
	}
	if len(influence.TopEdges) == 0 || influence.TopEdges[0].To != "com.app.data.CheckoutRepository" {
		t.Fatalf("unexpected top edges: %+v", influence.TopEdges)
	}
	if len(influence.HotPaths) == 0 || influence.HotPaths[0].Nodes[len(influence.HotPaths[0].Nodes)-1] != "com.app.data.CheckoutRepository" {
		t.Fatalf("unexpected hot paths: %+v", influence.HotPaths)
	}
	if len(influence.MethodHotspots) == 0 || influence.MethodHotspots[0].Method == "" {
		t.Fatalf("unexpected method hotspots: %+v", influence.MethodHotspots)
	}
}

func TestLoadClassGraphRequiresSupportedFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "class-graph.jsonl")
	data := `{"class":"com.app.Feature","edges":[{"caller":"open()V","calleeClass":"com.app.Repository","calleeMethod":"load","count":1}]}` + "\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if _, err := LoadClassGraph(path); err == nil {
		t.Fatal("LoadClassGraph() accepted graph record without format")
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

func TestBuildInfluenceSeparatesRuntimeWallTimeAndRetainedMemory(t *testing.T) {
	influence := BuildInfluence(Summary{
		Owners: []OwnerStats{{
			Owner: "com.app.FeedActivity",
			Kind:  "retained_object",
			Count: 1,
			MaxMS: 60_000,
		}},
		RuntimeCalls: []RuntimeCallStats{{
			Caller:  "com.app.FeedPresenter.open",
			Callee:  "com.app.FeedRepository.load",
			Count:   4,
			TotalMS: 800,
			MaxMS:   300,
		}},
		ProblemWindows: []ProblemWindowStats{{
			Owner:         "com.app.FeedPresenter.open",
			Kind:          "http_slow_or_failed",
			Windows:       1,
			Count:         3,
			TotalWindowMS: 1_500,
			MaxMS:         900,
		}},
		MemoryLeaks: []MemoryLeakSuspect{{
			ClassName:           "com.app.BigBitmapHolder",
			Holder:              "com.app.FeedActivity",
			Count:               1,
			EstimatedRetainedKB: 4_096,
			Severity:            "medium",
			Score:               8,
		}},
	}, nil)

	presenter := influenceNodeByClass(influence.TopNodes, "com.app.FeedPresenter")
	if presenter.MainThreadMS != 0 {
		t.Fatalf("non-main-thread problem contributed to main thread: %+v", presenter)
	}
	if presenter.RuntimeWallMS != 200 {
		t.Fatalf("caller runtime wall = %d, want 200: %+v", presenter.RuntimeWallMS, presenter)
	}
	repository := influenceNodeByClass(influence.TopNodes, "com.app.FeedRepository")
	if repository.MainThreadMS != 0 || repository.RuntimeWallMS != 800 {
		t.Fatalf("callee runtime metrics = %+v", repository)
	}
	activity := influenceNodeByClass(influence.TopNodes, "com.app.FeedActivity")
	if activity.MemoryPressure != 4_096 {
		t.Fatalf("heap retained size missing or age used as memory: %+v", activity)
	}
}

func TestInfluenceSeverityUsesPublishedBandsAndCapsStaticNodes(t *testing.T) {
	if influenceSeverity(4.9) != "ok" || influenceSeverity(5) != "medium" || influenceSeverity(15) != "high" {
		t.Fatalf("influence score bands do not match the report guide")
	}
	node := (&influenceAccumulator{className: "com.app.StaticOnly", score: 30, static: true, flows: map[string]struct{}{}, screens: map[string]struct{}{}, routes: map[string]struct{}{}, reasons: map[string]struct{}{}}).toNode()
	if node.Severity != "medium" || node.RuntimeEvidence {
		t.Fatalf("static-only node was presented as runtime critical: %+v", node)
	}
}

func TestInfluenceNodeKeepsEveryContextAndReason(t *testing.T) {
	node := &influenceAccumulator{
		className: "com.app.CompleteEvidence",
		runtime:   true,
		flows:     map[string]struct{}{},
		screens:   map[string]struct{}{},
		routes:    map[string]struct{}{},
		reasons:   map[string]struct{}{},
	}
	for index := range 12 {
		node.addFlow(fmt.Sprintf("flow-%02d", index))
		node.addScreen(fmt.Sprintf("screen-%02d", index))
		node.addRoute(fmt.Sprintf("route-%02d", index))
		node.addReason(fmt.Sprintf("reason-%02d", index))
	}

	got := node.toNode()
	if len(got.Flows) != 12 || len(got.Screens) != 12 || len(got.Routes) != 12 || len(got.Reasons) != 12 {
		t.Fatalf("influence evidence was silently truncated: %+v", got)
	}
}

func TestClassFromOwnerResolvesDestroyedLifecycleClass(t *testing.T) {
	tests := []struct {
		owner string
		want  string
	}{
		{
			owner: "lifecycle.destroyed.com.app.feature.FeedActivity",
			want:  "com.app.feature.FeedActivity",
		},
		{
			owner: "owner.lifecycle.destroyed.com.app.feature.FeedActivity",
			want:  "com.app.feature.FeedActivity",
		},
		{
			owner: "lifecycle.destroyed.unknown",
			want:  "",
		},
	}

	for _, test := range tests {
		if got := classFromOwner(test.owner); got != test.want {
			t.Fatalf("classFromOwner(%q) = %q, want %q", test.owner, got, test.want)
		}
	}
}

func TestProblemReasonMapsRuntimeKinds(t *testing.T) {
	cases := map[string]string{
		"http_slow_or_failed":      "медленный или ошибочный HTTP",
		"main_thread_stall":        "паузы главного потока",
		"main_thread_dispatch":     "медленный dispatch главного потока",
		"main_thread_io":           "IO на главном потоке",
		"ui_jank":                  "UI-подтормаживания",
		"log_spam":                 "спам логами",
		"retained_object":          "удержанные объекты",
		"wrapped_runnable":         "долгая Runnable-задача",
		"wrapped_handler_runnable": "долгая Handler-задача",
		"wrapped_callable":         "долгая Callable-задача",
		"wrapped_coroutine":        "долгая coroutine-задача",
		"wrapped_executor":         "долгая executor-задача",
		"wrapped_click":            "долгий click-handler",
		"gc_pressure":              "давление GC",
		"":                         "проблемные окна",
	}

	for kind, want := range cases {
		if got := problemReason(kind); got != want {
			t.Fatalf("problemReason(%q) = %q, want %q", kind, got, want)
		}
	}
}

func influenceNodeByClass(nodes []InfluenceNode, className string) InfluenceNode {
	for _, node := range nodes {
		if node.ClassName == className {
			return node
		}
	}
	return InfluenceNode{}
}
