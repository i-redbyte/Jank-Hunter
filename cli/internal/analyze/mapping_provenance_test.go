package analyze

import (
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
	"testing"
)

func TestOriginalArtifactsAreNotRetracedAgain(t *testing.T) {
	// An original application name can also be another class's obfuscated spelling.
	mapping := &NameMapping{classes: map[string]string{"a": "original.Other"}}
	graph := &ClassGraph{Classes: map[string]ClassGraphClass{"a": {Name: "a"}}}
	captures := &LambdaCaptureCatalog{Captures: []LambdaCapture{{Owner: "a", Implementation: "a", Values: []LambdaCapturedValue{{Type: "a"}}}}}
	collector := newCollector("provenance", 1, Options{ObfuscationMap: mapping, ClassGraph: graph, LambdaCaptures: captures})
	if collector.classGraph.Classes["a"].Name != "a" {
		t.Errorf("rewrote pre-R8 class graph: %+v", collector.classGraph)
	}
	if got := collector.lambdaCaptures.Captures[0]; got.Owner != "a" || got.Implementation != "a" || got.Values[0].Type != "a" {
		t.Errorf("rewrote pre-R8 capture: %+v", got)
	}
	if got := collector.resolveOwnerRef(map[uint64]string{7: "a"}, jhlog.SymbolRef{ID: 7, Origin: jhlog.SymbolOriginRuntimeClass}); got != "original.Other" {
		t.Errorf("runtime class was not mapped: %q", got)
	}
}
