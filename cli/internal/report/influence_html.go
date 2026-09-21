package report

import (
	"encoding/json"
	"fmt"
	"html/template"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
)

type influenceHTMLData struct {
	Views          []influenceHTMLView
	ViewNodes      []influenceHTMLNode
	ViewEdges      []influenceHTMLEdge
	Workspace      influenceHTMLWorkspace
	HotPaths       []analyze.InfluencePath
	MethodHotspots []analyze.InfluenceMethod
}

type influenceHTMLView struct {
	ID              string
	Mode            string
	Title           string
	Explanation     string
	Filters         analyze.InfluenceGraphFilters
	NodeIDs         []string
	EdgeIDs         []string
	TotalNodes      int
	TotalEdges      int
	ShownNodes      int
	ShownEdges      int
	OmittedNodes    int
	OmittedEdges    int
	OmissionReasons []string
	Limits          analyze.InfluenceGraphLimits
	Legend          []analyze.InfluenceGraphLegend
}

type influenceHTMLWorkspace struct {
	Nodes           []influenceHTMLNode
	Edges           []influenceHTMLEdge
	TotalNodes      int
	TotalEdges      int
	ShownNodes      int
	ShownEdges      int
	OmittedNodes    int
	OmittedEdges    int
	Contexts        []analyze.InfluenceGraphContext
	TotalContexts   int
	ShownContexts   int
	OmissionReasons []string
}

type influenceHTMLNode struct {
	ViewKey              string   `json:"_Key,omitempty"`
	ClassName            string   `json:",omitempty"`
	Label                string   `json:",omitempty"`
	Score                float64  `json:",omitempty"`
	Severity             string   `json:",omitempty"`
	Status               string   `json:",omitempty"`
	RuntimeEvidence      bool     `json:",omitempty"`
	Problems             uint64   `json:",omitempty"`
	LogSpam              uint64   `json:",omitempty"`
	MainThreadMS         uint64   `json:",omitempty"`
	RuntimeWallMS        uint64   `json:",omitempty"`
	NetworkMS            uint64   `json:",omitempty"`
	MemoryPressure       uint64   `json:",omitempty"`
	UIJank               uint64   `json:",omitempty"`
	Retained             uint64   `json:",omitempty"`
	HeapEvidence         bool     `json:",omitempty"`
	Operations           []string `json:",omitempty"`
	Screens              []string `json:",omitempty"`
	Routes               []string `json:",omitempty"`
	Reasons              []string `json:",omitempty"`
	ID                   string   `json:",omitempty"`
	Kind                 string   `json:",omitempty"`
	Package              string   `json:",omitempty"`
	Breadcrumbs          []string `json:",omitempty"`
	Aggregate            bool     `json:",omitempty"`
	Connector            bool     `json:",omitempty"`
	ChildCount           int      `json:",omitempty"`
	RuntimeClassCount    int      `json:",omitempty"`
	StaticOnlyClassCount int      `json:",omitempty"`
	ProblemClassCount    int      `json:",omitempty"`
	Children             []string `json:",omitempty"`
	Explanation          string   `json:",omitempty"`
}

type influenceHTMLEdge struct {
	ViewKey      string  `json:"_Key,omitempty"`
	From         string  `json:",omitempty"`
	To           string  `json:",omitempty"`
	RuntimeCount uint64  `json:",omitempty"`
	StaticCount  uint64  `json:",omitempty"`
	Influence    float64 `json:",omitempty"`
	Evidence     string  `json:",omitempty"`
	Aggregate    bool    `json:",omitempty"`
}

func influenceGraphData(influence analyze.InfluenceSummary) template.JS {
	payload, err := json.Marshal(buildInfluenceHTMLData(influence))
	if err != nil {
		return template.JS(`{}`)
	}
	return template.JS(payload)
}

func buildInfluenceHTMLData(influence analyze.InfluenceSummary) influenceHTMLData {
	views := make([]influenceHTMLView, 0, len(influence.Views))
	viewNodes := make([]influenceHTMLNode, 0)
	viewEdges := make([]influenceHTMLEdge, 0)
	seenNodes := map[string]string{}
	seenEdges := map[string]string{}
	for _, view := range influence.Views {
		nodeIDs := make([]string, 0, len(view.Nodes))
		for _, node := range view.Nodes {
			key := node.ID
			if node.Connector {
				key = "connector:" + key
			}
			if existing, exists := seenNodes[key]; exists {
				nodeIDs = append(nodeIDs, existing)
				continue
			}
			payloadKey := fmt.Sprintf("n%d", len(viewNodes))
			seenNodes[key] = payloadKey
			nodeIDs = append(nodeIDs, payloadKey)
			compact := compactInfluenceHTMLNode(node, node.Aggregate)
			compact.ViewKey = payloadKey
			viewNodes = append(viewNodes, compact)
		}
		edgeIDs := make([]string, 0, len(view.Edges))
		for _, edge := range view.Edges {
			key := fmt.Sprintf("%s:%t:%d:%d", edge.ID, edge.Aggregate, edge.RuntimeCount, edge.StaticCount)
			if existing, exists := seenEdges[key]; exists {
				edgeIDs = append(edgeIDs, existing)
				continue
			}
			payloadKey := fmt.Sprintf("e%d", len(viewEdges))
			seenEdges[key] = payloadKey
			edgeIDs = append(edgeIDs, payloadKey)
			compact := compactInfluenceHTMLEdge(edge)
			compact.ViewKey = payloadKey
			viewEdges = append(viewEdges, compact)
		}
		views = append(views, influenceHTMLView{
			ID:              view.ID,
			Mode:            view.Mode,
			Title:           view.Title,
			Explanation:     view.Explanation,
			Filters:         view.Filters,
			NodeIDs:         nodeIDs,
			EdgeIDs:         edgeIDs,
			TotalNodes:      view.TotalNodes,
			TotalEdges:      view.TotalEdges,
			ShownNodes:      view.ShownNodes,
			ShownEdges:      view.ShownEdges,
			OmittedNodes:    view.OmittedNodes,
			OmittedEdges:    view.OmittedEdges,
			OmissionReasons: view.OmissionReasons,
			Limits:          view.Limits,
			Legend:          view.Legend,
		})
	}
	workspaceNodes := make([]influenceHTMLNode, 0, len(influence.Workspace.Nodes))
	for _, node := range influence.Workspace.Nodes {
		workspaceNodes = append(workspaceNodes, compactInfluenceHTMLNode(node, true))
	}
	workspace := influence.Workspace
	return influenceHTMLData{
		Views:     views,
		ViewNodes: viewNodes,
		ViewEdges: viewEdges,
		Workspace: influenceHTMLWorkspace{
			Nodes:           workspaceNodes,
			Edges:           influenceHTMLEdges(workspace.Edges),
			TotalNodes:      workspace.TotalNodes,
			TotalEdges:      workspace.TotalEdges,
			ShownNodes:      workspace.ShownNodes,
			ShownEdges:      workspace.ShownEdges,
			OmittedNodes:    workspace.OmittedNodes,
			OmittedEdges:    workspace.OmittedEdges,
			Contexts:        workspace.Contexts,
			TotalContexts:   workspace.TotalContexts,
			ShownContexts:   workspace.ShownContexts,
			OmissionReasons: workspace.OmissionReasons,
		},
		HotPaths:       influence.HotPaths,
		MethodHotspots: influence.MethodHotspots,
	}
}

func compactInfluenceHTMLNode(node analyze.InfluenceGraphNode, detailed bool) influenceHTMLNode {
	result := influenceHTMLNode{
		Score:           node.Score,
		Severity:        node.Severity,
		RuntimeEvidence: node.RuntimeEvidence,
		HeapEvidence:    node.HeapEvidence,
		ID:              node.ID,
		Aggregate:       node.Aggregate,
		Connector:       node.Connector,
	}
	if len(node.Reasons) > 0 {
		limit := len(node.Reasons)
		if !detailed && limit > 2 {
			limit = 2
		}
		result.Reasons = node.Reasons[:limit]
	}
	if node.Aggregate || detailed {
		result.Package = node.Package
	}
	if node.Connector {
		result.Kind = node.Kind
	}
	if !detailed {
		return result
	}
	result.Problems = node.Problems
	result.LogSpam = node.LogSpam
	result.MainThreadMS = node.MainThreadMS
	result.RuntimeWallMS = node.RuntimeWallMS
	result.NetworkMS = node.NetworkMS
	result.MemoryPressure = node.MemoryPressure
	result.UIJank = node.UIJank
	result.Retained = node.Retained
	result.Operations = node.Operations
	result.Screens = node.Screens
	result.Routes = node.Routes
	result.ChildCount = node.ChildCount
	result.RuntimeClassCount = node.RuntimeClassCount
	result.StaticOnlyClassCount = node.StaticOnlyClassCount
	result.ProblemClassCount = node.ProblemClassCount
	return result
}

func influenceHTMLEdges(edges []analyze.InfluenceGraphEdge) []influenceHTMLEdge {
	result := make([]influenceHTMLEdge, 0, len(edges))
	for _, edge := range edges {
		result = append(result, compactInfluenceHTMLEdge(edge))
	}
	return result
}

func compactInfluenceHTMLEdge(edge analyze.InfluenceGraphEdge) influenceHTMLEdge {
	return influenceHTMLEdge{
		From:         edge.From,
		To:           edge.To,
		RuntimeCount: edge.RuntimeCount,
		StaticCount:  edge.StaticCount,
		Influence:    edge.Influence,
		Evidence:     edge.Evidence,
		Aggregate:    edge.Aggregate,
	}
}
