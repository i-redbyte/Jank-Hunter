package report

import (
	"regexp"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
)

func TestLeakGraphSVGDoesNotOverlapRetainedSampleNodes(t *testing.T) {
	nodes := []analyze.LeakGraphNode{
		{ID: "root", Label: "GC root", Kind: "root"},
		{ID: "holder", Label: "com.app.Holder", Kind: "app"},
		{ID: "target", Label: "com.app.LeakedActivity", Kind: "target"},
	}
	for index := 0; index < 8; index++ {
		nodes = append(nodes, analyze.LeakGraphNode{
			ID:     "retained-" + string(rune('a'+index)),
			Label:  "retained class",
			Kind:   "retained",
			Detail: "object in retained subtree",
		})
	}
	graph := analyze.LeakGraph{
		Title:    "Reference chain",
		Nodes:    nodes,
		RootID:   "root",
		TargetID: "target",
	}

	html := string(leakGraphSVG("layout", graph))
	pattern := regexp.MustCompile(`<g class="leak-graph-node node-retained" transform="translate\(([^ ]+) ([^)]+)\)"`)
	matches := pattern.FindAllStringSubmatch(html, -1)
	if len(matches) != 8 {
		t.Fatalf("retained node transforms = %d, want 8: %s", len(matches), html)
	}
	positions := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		key := match[1] + "," + match[2]
		if _, exists := positions[key]; exists {
			t.Fatalf("retained graph nodes overlap at %s: %s", key, html)
		}
		positions[key] = struct{}{}
	}
}
