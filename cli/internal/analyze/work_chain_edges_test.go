package analyze

import (
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func TestDeclaredWorkChainEdgeRetainsScopeWithoutInventingAcceptedJob(t *testing.T) {
	c := &collector{}
	c.workRegistry.scope = "run/process/session"
	c.currentProcess = "app"
	c.recordWorkChainEdge(jhlog.Event{Type: jhlog.EventWorkChainEdge, TimeMS: 80,
		DiagnosticEvent: &jhlog.DiagnosticEvent{WorkChainEdge: &jhlog.WorkChainEdgeEvent{
			Version: 1, ParentLeast: 1, ChildLeast: 2, ObservedElapsedNanos: 900, CallerSource: "app#chain",
		}}})
	got := c.workChainEdges.finalize()
	if got == nil || len(got.Edges) != 1 || got.Edges[0].Scope != "run/process/session" || got.Edges[0].Process != "app" {
		t.Fatalf("edge scope lost: %#v", got)
	}
	if c.workRegistry.finalize() != nil {
		t.Fatal("a declared chain edge cannot invent a registered job")
	}
}

func TestComparisonSnapshotBoundsWorkChainHistory(t *testing.T) {
	source := &WorkChainAnalysis{CapturedEdges: 2, CapturedRoots: 1,
		Edges: []WorkChainEdge{{ParentUUID: "a", ChildUUID: "b"}}, Roots: []WorkChainRoot{{UUID: "a"}}}
	compact := comparisonSnapshotSummary(Summary{WorkChainEdges: source}).WorkChainEdges
	if compact == nil || !compact.HistoryOmitted || compact.CapturedEdges != 2 || compact.CapturedRoots != 1 || compact.Edges != nil || compact.Roots != nil {
		t.Fatalf("unbounded comparison chain: %+v", compact)
	}
	if len(source.Edges) != 1 {
		t.Fatal("source summary mutated")
	}
}

func TestUniqueChainRootIsDeclarationNotSubmission(t *testing.T) {
	c := &collector{}
	c.workRegistry.scope = "session-a"
	c.recordWorkChainRoot(jhlog.Event{Type: jhlog.EventWorkChainRoot, TimeMS: 50,
		DiagnosticEvent: &jhlog.DiagnosticEvent{WorkChainRoot: &jhlog.WorkSubmissionEvent{
			Version: 2, UUIDLeast: 7, ObservedElapsedNanos: 500, HasUnique: true,
			UniqueName: "sync", Policy: jhlog.WorkSubmissionKeep, CallerSource: "app#root",
		}}})
	result := c.workChainEdges.finalize()
	if result == nil || result.CapturedRoots != 1 || len(result.Roots) != 1 ||
		result.Roots[0].Scope != "session-a" || result.Roots[0].Root.UniqueName != "sync" {
		t.Fatalf("unique root lost: %+v", result)
	}
	if c.workSubmissions.finalize() != nil {
		t.Fatal("graph construction cannot become an enqueue observation")
	}
}
