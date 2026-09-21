package analyze

import (
	"fmt"
	"reflect"
	"testing"
)

var influenceBuilderAllocationSink *influenceViewBuilder

func TestInfluenceViewsSeparateProblemsAndRuntimeEvidence(t *testing.T) {
	nodes := []InfluenceNode{
		influenceViewTestNode("com.app.feature.A", 12, true),
		influenceViewTestNode("com.app.feature.B", 7, true),
		influenceViewTestNode("com.app.feature.Connector", 0, false),
		influenceViewTestNode("com.app.static.Only", 0, false),
	}
	edges := []InfluenceEdge{
		influenceViewTestEdge("com.app.feature.A", "com.app.feature.B", 5, 0),
		influenceViewTestEdge("com.app.feature.A", "com.app.feature.Connector", 0, 3),
		influenceViewTestEdge("com.app.feature.Connector", "com.app.feature.B", 0, 2),
		influenceViewTestEdge("com.app.static.Only", "com.app.feature.A", 0, 4),
	}
	builder := newInfluenceViewBuilder(nodes, edges)

	problems := builder.problemsView()
	if problems.Mode != "problems" || problems.TotalNodes != 4 || problems.TotalEdges != 4 {
		t.Fatalf("unexpected problems view: %+v", problems)
	}
	staticEdge := influenceGraphEdgeByEndpoints(t, problems.Edges, "com.app.feature.A", "com.app.feature.Connector")
	if staticEdge.RuntimeConfirmed || staticEdge.Evidence != "static" || staticEdge.RuntimeCount != 0 {
		t.Fatalf("static edge was converted to runtime evidence: %+v", staticEdge)
	}

	runtimeView := builder.runtimeView()
	if runtimeView.TotalNodes != 2 || runtimeView.TotalEdges != 1 {
		t.Fatalf("unexpected runtime view: %+v", runtimeView)
	}
	runtimeEdge := runtimeView.Edges[0]
	if !runtimeEdge.RuntimeConfirmed || runtimeEdge.Evidence != "runtime" || runtimeEdge.StaticCount != 0 {
		t.Fatalf("runtime evidence was not preserved: %+v", runtimeEdge)
	}
}

func TestPreparedInfluenceViewBuilderTakesOwnershipWithoutCopying(t *testing.T) {
	nodes := []InfluenceNode{influenceViewTestNode("com.app.A", 1, true)}
	edges := []InfluenceEdge{influenceViewTestEdge("com.app.A", "com.app.B", 1, 0)}
	builder := newPreparedInfluenceViewBuilder(nodes, edges)

	if &builder.nodes[0] != &nodes[0] || &builder.edges[0] != &edges[0] {
		t.Fatal("prepared influence builder copied its owned node or edge storage")
	}
}

func TestPreparedInfluenceViewBuilderAdjacencyAllocationBudget(t *testing.T) {
	const nodeCount = 4_096
	nodes := make([]InfluenceNode, nodeCount)
	edges := make([]InfluenceEdge, 0, nodeCount-1)
	for index := range nodes {
		nodes[index] = influenceViewTestNode(fmt.Sprintf("com.app.Node%04d", index), 1, true)
		if index > 0 {
			edges = append(edges, influenceViewTestEdge(nodes[index-1].ClassName, nodes[index].ClassName, 1, 0))
		}
	}

	allocations := testing.AllocsPerRun(5, func() {
		influenceBuilderAllocationSink = newPreparedInfluenceViewBuilder(nodes, edges)
	})
	if allocations > 64 {
		t.Fatalf("prepared influence adjacency allocates %.2f objects, want at most 64", allocations)
	}
}

func TestInfluencePackageAggregationUsesMaximumScoreAndSeparateEvidenceCounts(t *testing.T) {
	nodes := []InfluenceNode{
		influenceViewTestNode("com.shop.checkout.alpha.A", 4, true),
		influenceViewTestNode("com.shop.checkout.beta.B", 12, false),
		influenceViewTestNode("com.shop.network.C", 8, true),
	}
	edges := []InfluenceEdge{
		influenceViewTestEdge("com.shop.checkout.alpha.A", "com.shop.network.C", 2, 0),
		influenceViewTestEdge("com.shop.checkout.beta.B", "com.shop.network.C", 0, 3),
	}
	view := newInfluenceViewBuilder(nodes, edges).packagesView(3)
	checkout := influenceGraphNodeByID(t, view.Nodes, "package:com.shop.checkout")
	if checkout.Score != 12 || checkout.ChildCount != 2 {
		t.Fatalf("package score or child count is not based on max child: %+v", checkout)
	}
	if checkout.RuntimeClassCount != 1 || checkout.StaticOnlyClassCount != 1 || checkout.ProblemClassCount != 2 {
		t.Fatalf("package class evidence counters are incorrect: %+v", checkout)
	}
	aggregated := influenceGraphEdgeByEndpoints(t, view.Edges, "package:com.shop.checkout", "package:com.shop.network")
	if aggregated.RuntimeCount != 2 || aggregated.StaticCount != 3 || aggregated.Evidence != "mixed" {
		t.Fatalf("aggregate edge evidence was collapsed: %+v", aggregated)
	}
	if !aggregated.Aggregate {
		t.Fatal("package edge is not marked as aggregate")
	}
}

func TestInfluencePackageViewAllocationBudget(t *testing.T) {
	const nodeCount = 4_096
	nodes := make([]InfluenceNode, nodeCount)
	for index := range nodes {
		nodes[index] = influenceViewTestNode(fmt.Sprintf("com.app.feature%04d.Node", index), float64(nodeCount-index), true)
	}
	builder := newInfluenceViewBuilder(nodes, nil)
	builder.packagesView(4)
	if raceDetectorEnabled {
		t.Skip("race instrumentation changes allocation accounting")
	}

	allocations := testing.AllocsPerRun(5, func() {
		builder.packagesView(4)
	})
	if allocations > 1_056 {
		t.Fatalf("package view allocates %.2f objects, want at most 1056", allocations)
	}
}

func TestInfluenceNeighborhoodDirectionDepthAndCycles(t *testing.T) {
	nodes := []InfluenceNode{
		influenceViewTestNode("graph.A", 10, true),
		influenceViewTestNode("graph.B", 8, true),
		influenceViewTestNode("graph.C", 6, true),
		influenceViewTestNode("graph.D", 4, false),
		influenceViewTestNode("graph.E", 2, false),
	}
	edges := []InfluenceEdge{
		influenceViewTestEdge("graph.A", "graph.B", 3, 0),
		influenceViewTestEdge("graph.B", "graph.C", 0, 2),
		influenceViewTestEdge("graph.C", "graph.A", 0, 2),
		influenceViewTestEdge("graph.D", "graph.B", 0, 2),
		influenceViewTestEdge("graph.C", "graph.E", 0, 2),
	}
	builder := newInfluenceViewBuilder(nodes, edges)
	cases := []struct {
		name      string
		direction string
		depth     int
		runtime   bool
		wantNodes int
	}{
		{name: "outgoing depth one", direction: "outgoing", depth: 1, wantNodes: 2},
		{name: "outgoing depth two", direction: "outgoing", depth: 2, wantNodes: 3},
		{name: "outgoing depth three cycle safe", direction: "outgoing", depth: 3, wantNodes: 4},
		{name: "incoming depth one", direction: "incoming", depth: 1, wantNodes: 2},
		{name: "incoming depth three", direction: "incoming", depth: 3, wantNodes: 4},
		{name: "both depth one", direction: "both", depth: 1, wantNodes: 3},
		{name: "both depth two", direction: "both", depth: 2, wantNodes: 5},
		{name: "runtime only", direction: "both", depth: 3, runtime: true, wantNodes: 2},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			view := builder.neighborhoodView("graph.A", test.direction, test.depth, test.runtime)
			if view.TotalNodes != test.wantNodes {
				t.Fatalf("nodes = %d, want %d: %+v", view.TotalNodes, test.wantNodes, view.Nodes)
			}
			second := builder.neighborhoodView("graph.A", test.direction, test.depth, test.runtime)
			if !reflect.DeepEqual(view, second) {
				t.Fatal("neighborhood ordering is not deterministic")
			}
		})
	}
}

func TestInfluenceContextViewMarksConnectorWithoutChangingScore(t *testing.T) {
	left := influenceViewTestNode("com.app.checkout.ScreenPresenter", 11, true)
	left.Screens = []string{"Checkout"}
	left.Operations = []string{"checkout.pay"}
	right := influenceViewTestNode("com.app.checkout.Repository", 9, true)
	right.Screens = []string{"Checkout"}
	right.Operations = []string{"checkout.pay"}
	connector := influenceViewTestNode("com.app.shared.Dispatcher", 3, false)
	builder := newInfluenceViewBuilder(
		[]InfluenceNode{left, right, connector},
		[]InfluenceEdge{
			influenceViewTestEdge(left.ClassName, connector.ClassName, 0, 2),
			influenceViewTestEdge(connector.ClassName, right.ClassName, 0, 2),
		},
	)
	view := builder.contextView(InfluenceGraphContext{ID: "context:operation:checkout.pay", Kind: "operation", Value: "checkout.pay"})
	connectorNode := influenceGraphNodeByID(t, view.Nodes, connector.ClassName)
	if !connectorNode.Connector || connectorNode.Kind != "connector" {
		t.Fatalf("connector node is not explicit: %+v", connectorNode)
	}
	if connectorNode.Score != connector.Score {
		t.Fatalf("context view changed source score: got %.1f want %.1f", connectorNode.Score, connector.Score)
	}
	if view.TotalNodes != 3 || view.TotalEdges != 2 {
		t.Fatalf("context connector path is incomplete: %+v", view)
	}
}

func TestInfluenceContextViewAllocationsStayBounded(t *testing.T) {
	const nodeCount = 4_096
	nodes := make([]InfluenceNode, nodeCount)
	for index := range nodes {
		nodes[index] = influenceViewTestNode(fmt.Sprintf("com.app.feature.Node%04d", index), float64(nodeCount-index), true)
		nodes[index].Operations = []string{"shared.operation"}
	}
	builder := newInfluenceViewBuilder(nodes, nil)
	context := InfluenceGraphContext{ID: "context:operation:shared.operation", Kind: "operation", Value: "shared.operation"}
	builder.contextView(context)

	allocations := testing.AllocsPerRun(25, func() {
		builder.contextView(context)
	})
	if allocations > 32 {
		t.Fatalf("bounded context view allocates %.2f objects, want at most 32", allocations)
	}
}

func TestInfluenceContextsBoundMaterializationAndKeepExactTotal(t *testing.T) {
	const contextCount = 100
	nodes := make([]InfluenceNode, contextCount)
	for index := range nodes {
		nodes[index] = influenceViewTestNode(fmt.Sprintf("com.app.Node%03d", index), 1, true)
		nodes[index].Operations = []string{fmt.Sprintf("operation-%03d", index)}
	}
	builder := newInfluenceViewBuilder(nodes, nil)

	selection := builder.boundedContexts()
	if selection.total != contextCount || len(selection.items) != workspaceMaxContexts {
		t.Fatalf("context selection = total:%d shown:%d, want %d/%d", selection.total, len(selection.items), contextCount, workspaceMaxContexts)
	}
	for index, context := range selection.items {
		want := fmt.Sprintf("operation-%03d", index)
		if context.Kind != "operation" || context.Value != want || context.ID != "context:operation:"+want {
			t.Fatalf("context %d = %+v, want operation %q", index, context, want)
		}
	}
}

func TestInfluenceContextSelectionAllocationBudget(t *testing.T) {
	const contextCount = 4_096
	nodes := make([]InfluenceNode, contextCount)
	for index := range nodes {
		nodes[index] = influenceViewTestNode(fmt.Sprintf("com.app.Node%04d", index), 1, true)
		nodes[index].Operations = []string{fmt.Sprintf("operation-%04d", index)}
	}
	builder := newInfluenceViewBuilder(nodes, nil)
	builder.boundedContexts()

	allocations := testing.AllocsPerRun(10, func() {
		builder.boundedContexts()
	})
	if allocations > 256 {
		t.Fatalf("bounded context selection allocates %.2f objects, want at most 256", allocations)
	}
}

func TestInfluenceClassNodeCacheKeepsConnectorProjectionIndependent(t *testing.T) {
	node := influenceViewTestNode("com.app.shared.Dispatcher", 3, true)
	builder := newInfluenceViewBuilder([]InfluenceNode{node}, nil)

	classNode := builder.classNode(node, false)
	connectorNode := builder.classNode(node, true)
	if classNode.Kind != "class" || classNode.Connector {
		t.Fatalf("class projection changed: %+v", classNode)
	}
	if connectorNode.Kind != "connector" || !connectorNode.Connector {
		t.Fatalf("connector projection changed: %+v", connectorNode)
	}
	if classNode.Score != connectorNode.Score || classNode.Explanation != connectorNode.Explanation {
		t.Fatal("cache changed source metrics or explanation between projections")
	}
}

func TestInfluenceViewTotalsAndOmissionsAreCalculatedBeforeLimits(t *testing.T) {
	nodes := make([]InfluenceNode, 0, problemViewMaxNodes+20)
	edges := make([]InfluenceEdge, 0, problemViewMaxNodes+19)
	for index := 0; index < problemViewMaxNodes+20; index++ {
		name := fmt.Sprintf("com.app.problem.Node%03d", index)
		nodes = append(nodes, influenceViewTestNode(name, float64(problemViewMaxNodes+20-index), true))
		if index > 0 {
			edges = append(edges, influenceViewTestEdge(fmt.Sprintf("com.app.problem.Node%03d", index-1), name, 1, 0))
		}
	}
	view := newInfluenceViewBuilder(nodes, edges).problemsView()
	if view.TotalNodes != len(nodes) || view.ShownNodes != problemViewMaxNodes || view.OmittedNodes != 20 {
		t.Fatalf("node totals do not describe pre-limit data: %+v", view)
	}
	if view.TotalEdges != len(edges) || view.OmittedEdges == 0 || len(view.OmissionReasons) == 0 {
		t.Fatalf("edge omissions are not explicit: %+v", view)
	}
}

func TestInfluenceProblemViewAllocationBudget(t *testing.T) {
	const nodeCount = 4_096
	nodes := make([]InfluenceNode, nodeCount)
	edges := make([]InfluenceEdge, 0, nodeCount-1)
	for index := range nodes {
		name := fmt.Sprintf("com.app.problem.Node%04d", index)
		nodes[index] = influenceViewTestNode(name, float64(nodeCount-index), true)
		if index > 0 {
			edges = append(edges, influenceViewTestEdge(nodes[index-1].ClassName, name, 1, 0))
		}
	}
	builder := newInfluenceViewBuilder(nodes, edges)
	builder.problemsView()

	allocations := testing.AllocsPerRun(5, func() {
		builder.problemsView()
	})
	if allocations > 512 {
		t.Fatalf("bounded problem view allocates %.2f objects, want at most 512", allocations)
	}
}

func TestInfluenceViewsPreserveSourceScoresAcrossModes(t *testing.T) {
	node := influenceViewTestNode("com.app.checkout.Repository", 13.7, true)
	node.Operations = []string{"checkout.pay"}
	other := influenceViewTestNode("com.app.checkout.Api", 4.2, true)
	other.Operations = []string{"checkout.pay"}
	builder := newInfluenceViewBuilder(
		[]InfluenceNode{node, other},
		[]InfluenceEdge{influenceViewTestEdge(node.ClassName, other.ClassName, 2, 1)},
	)
	views := []InfluenceGraphView{
		builder.problemsView(),
		builder.runtimeView(),
		builder.neighborhoodView(node.ClassName, "both", 3, false),
		builder.contextView(InfluenceGraphContext{ID: "context:operation:checkout.pay", Kind: "operation", Value: "checkout.pay"}),
	}
	for _, view := range views {
		got := influenceGraphNodeByID(t, view.Nodes, node.ClassName)
		if got.Score != node.Score {
			t.Fatalf("view %s changed score from %.1f to %.1f", view.ID, node.Score, got.Score)
		}
	}
	packageNode := influenceGraphNodeByID(t, builder.packagesView(3).Nodes, "package:com.app.checkout")
	if packageNode.Score != node.Score {
		t.Fatalf("package max score = %.1f, want %.1f", packageNode.Score, node.Score)
	}
}

func TestInfluenceViewsHandleEmptyRuntimeOnlyAndStaticOnlyGraphs(t *testing.T) {
	empty := BuildInfluence(Summary{}, nil)
	if empty.Available || len(empty.Views) == 0 || empty.Views[0].TotalNodes != 0 {
		t.Fatalf("empty graph is not represented safely: %+v", empty)
	}

	runtime := BuildInfluence(Summary{RuntimeCalls: []RuntimeCallStats{{Caller: "com.app.A.run", Callee: "com.app.B.load", Count: 2}}}, nil)
	runtimeView := influenceViewByID(t, runtime.Views, "runtime")
	if runtimeView.TotalNodes != 2 || runtimeView.TotalEdges != 1 || runtimeView.Edges[0].Evidence != "runtime" {
		t.Fatalf("runtime-only graph is incorrect: %+v", runtimeView)
	}

	staticGraph := &ClassGraph{
		Classes: map[string]ClassGraphClass{"com.app.A": {Name: "com.app.A"}, "com.app.B": {Name: "com.app.B"}},
		Edges:   []ClassGraphEdge{{From: "com.app.A", To: "com.app.B", Count: 3}},
	}
	staticOnly := BuildInfluence(Summary{}, staticGraph)
	if !staticOnly.Available || staticOnly.HasRuntimeGraph {
		t.Fatalf("static-only graph availability is incorrect: %+v", staticOnly)
	}
	if influenceViewByID(t, staticOnly.Views, "runtime").TotalNodes != 0 {
		t.Fatal("static-only classes leaked into runtime view")
	}
	if staticOnly.TotalEdges != 1 || staticOnly.Workspace.Edges[0].Evidence != "static" {
		t.Fatalf("static evidence is incorrect: %+v", staticOnly.Workspace.Edges)
	}
}

func TestInfluenceLargeStaticGraphKeepsFullTotalsAndBoundedWorkspace(t *testing.T) {
	const nodeCount = 5_000
	graph := &ClassGraph{Classes: map[string]ClassGraphClass{}}
	for index := 0; index < nodeCount; index++ {
		name := fmt.Sprintf("com.large.feature%04d.component.Class%04d", index, index)
		graph.Classes[name] = ClassGraphClass{Name: name}
		for step := 1; step <= 3 && index+step < nodeCount; step++ {
			graph.Edges = append(graph.Edges, ClassGraphEdge{
				From:  name,
				To:    fmt.Sprintf("com.large.feature%04d.component.Class%04d", index+step, index+step),
				Count: uint64(4 - step),
			})
		}
	}
	influence := BuildInfluence(Summary{}, graph)
	if influence.TotalNodes != nodeCount || influence.TotalEdges != len(graph.Edges) {
		t.Fatalf("full graph totals were lost: nodes %d/%d edges %d/%d", influence.TotalNodes, nodeCount, influence.TotalEdges, len(graph.Edges))
	}
	if influence.Workspace.ShownNodes != workspaceMaxNodes || influence.Workspace.OmittedNodes != nodeCount-workspaceMaxNodes {
		t.Fatalf("workspace node budget is not explicit: %+v", influence.Workspace)
	}
	if influence.Workspace.ShownEdges > workspaceMaxEdges || influence.Workspace.OmittedEdges == 0 || len(influence.Workspace.OmissionReasons) == 0 {
		t.Fatalf("workspace edge budget is not explicit: %+v", influence.Workspace)
	}
	packageView := influenceViewByID(t, influence.Views, "packages:3")
	if packageView.TotalNodes != nodeCount || packageView.ShownNodes != packageViewMaxNodes || packageView.OmittedNodes == 0 {
		t.Fatalf("large package view does not expose truncation: %+v", packageView)
	}
}

func BenchmarkBuildInfluenceLargeGraph(b *testing.B) {
	const nodeCount = 20_000
	graph := &ClassGraph{Classes: map[string]ClassGraphClass{}}
	for index := 0; index < nodeCount; index++ {
		name := fmt.Sprintf("com.benchmark.feature%05d.Class%05d", index, index)
		graph.Classes[name] = ClassGraphClass{Name: name}
		for step := 1; step <= 3 && index+step < nodeCount; step++ {
			graph.Edges = append(graph.Edges, ClassGraphEdge{From: name, To: fmt.Sprintf("com.benchmark.feature%05d.Class%05d", index+step, index+step), Count: uint64(step)})
		}
	}
	summary := Summary{}
	for index := 0; index < 200; index++ {
		summary.RuntimeCalls = append(summary.RuntimeCalls, RuntimeCallStats{
			Caller:  fmt.Sprintf("com.benchmark.feature%05d.Class%05d.run", index, index),
			Callee:  fmt.Sprintf("com.benchmark.feature%05d.Class%05d.load", index+1, index+1),
			Count:   4,
			TotalMS: 80,
			MaxMS:   30,
		})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		influence := BuildInfluence(summary, graph)
		if influence.TotalNodes != nodeCount {
			b.Fatalf("nodes = %d, want %d", influence.TotalNodes, nodeCount)
		}
	}
}

var benchmarkInfluenceString string
var benchmarkInfluenceIndex uint32

func TestInfluenceClassNameProjectionsDoNotAllocate(t *testing.T) {
	allocations := testing.AllocsPerRun(1_000, func() {
		benchmarkInfluenceString = influencePackage("com.example.feature.FeedPresenter", 3)
		benchmarkInfluenceString = shortClassName("com.example.feature.FeedPresenter")
	})
	if allocations != 0 {
		t.Fatalf("class-name projection allocates %.2f objects, want zero", allocations)
	}
}

func TestInfluencePackageChildSampleDoesNotAllocate(t *testing.T) {
	allocations := testing.AllocsPerRun(1_000, func() {
		var aggregate influencePackageAggregate
		aggregate.addChildSample(42)
		benchmarkInfluenceIndex = aggregate.children[0]
	})
	if allocations != 0 {
		t.Fatalf("package child sample allocates %.2f objects, want zero", allocations)
	}
}

func influenceViewTestNode(className string, score float64, runtime bool) InfluenceNode {
	severity := "ok"
	if score >= 15 {
		severity = "high"
	} else if score >= 5 {
		severity = "medium"
	}
	status := "static_only"
	if runtime {
		status = "runtime"
	}
	return InfluenceNode{
		ClassName:       className,
		Label:           shortClassName(className),
		Score:           score,
		Severity:        severity,
		Status:          status,
		RuntimeEvidence: runtime,
		Problems:        uint64(score),
		Reasons:         []string{"test signal"},
	}
}

func influenceViewTestEdge(from string, to string, runtime uint64, static uint64) InfluenceEdge {
	return normalizeInfluenceEvidence(InfluenceEdge{
		From:         from,
		To:           to,
		Count:        runtime + static,
		RuntimeCount: runtime,
		StaticCount:  static,
		Influence:    float64(runtime*2 + static),
	})
}

func influenceViewByID(t *testing.T, views []InfluenceGraphView, id string) InfluenceGraphView {
	t.Helper()
	for _, view := range views {
		if view.ID == id {
			return view
		}
	}
	t.Fatalf("view %q not found", id)
	return InfluenceGraphView{}
}

func influenceGraphNodeByID(t *testing.T, nodes []InfluenceGraphNode, id string) InfluenceGraphNode {
	t.Helper()
	for _, node := range nodes {
		if node.ID == id {
			return node
		}
	}
	t.Fatalf("node %q not found in %+v", id, nodes)
	return InfluenceGraphNode{}
}

func influenceGraphEdgeByEndpoints(t *testing.T, edges []InfluenceGraphEdge, from string, to string) InfluenceGraphEdge {
	t.Helper()
	for _, edge := range edges {
		if edge.From == from && edge.To == to {
			return edge
		}
	}
	t.Fatalf("edge %s -> %s not found in %+v", from, to, edges)
	return InfluenceGraphEdge{}
}
