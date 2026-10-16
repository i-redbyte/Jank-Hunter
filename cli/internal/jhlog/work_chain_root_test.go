package jhlog

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestWorkChainRootUsesSubmissionFieldsWithoutClaimingEnqueue(t *testing.T) {
	want := &WorkSubmissionEvent{Version: 2, UUIDMost: 1, UUIDLeast: 2, ObservedElapsedNanos: 100,
		HasUnique: true, UniqueName: "sync", Policy: WorkSubmissionKeep, CallerSource: "app#beginUniqueWork"}
	path := filepath.Join(t.TempDir(), "root.jhlog")
	writeClosedEvents(t, path, []Event{{Type: EventWorkChainRoot, DiagnosticEvent: &DiagnosticEvent{WorkChainRoot: want}}})
	log, err := readLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(log.Events) != 1 || log.Events[0].Type != EventWorkChainRoot ||
		!reflect.DeepEqual(log.Events[0].WorkChainRoot, want) || log.Events[0].WorkSubmission != nil {
		t.Fatalf("root mixed with accepted submission: %#v", log.Events)
	}
}

func TestMinifiedAndroidUniqueRootIsSeparateFromDirectSubmission(t *testing.T) {
	log, err := readLog("../../../wire/testdata/work-chain-root-r8-5.1.0.jhlog")
	if err != nil {
		t.Fatal(err)
	}
	var roots, submissions int
	for _, event := range log.Events {
		if event.DiagnosticEvent == nil {
			continue
		}
		if event.WorkChainRoot != nil {
			roots++
			if event.WorkChainRoot.UniqueName != "registry-probe" || event.WorkChainRoot.Policy != WorkSubmissionReplace {
				t.Fatalf("invalid minified root: %+v", event.WorkChainRoot)
			}
		}
		if event.WorkSubmission != nil {
			submissions++
		}
	}
	if roots != 1 || submissions != 16 {
		t.Fatalf("root mixed with direct submissions: %d roots, %d submissions", roots, submissions)
	}
}

func TestMinifiedAndroidCombinePreservesBothObservedPrerequisites(t *testing.T) {
	log, err := readLog("../../../wire/testdata/work-chain-combine-r8-5.1.0.jhlog")
	if err != nil {
		t.Fatal(err)
	}
	var roots, edges, submissions int
	anonymousRoots := make(map[string]bool)
	children := make(map[string]map[string]bool)
	for _, event := range log.Events {
		if event.DiagnosticEvent == nil {
			continue
		}
		if root := event.WorkChainRoot; root != nil {
			roots++
			if !root.HasUnique {
				anonymousRoots[root.UUID()] = true
			}
		}
		if edge := event.WorkChainEdge; edge != nil {
			edges++
			if children[edge.ChildUUID()] == nil {
				children[edge.ChildUUID()] = make(map[string]bool)
			}
			children[edge.ChildUUID()][edge.ParentUUID()] = true
		}
		if event.WorkSubmission != nil {
			submissions++
		}
	}
	if roots != 3 || edges != 3 || submissions != 16 || len(anonymousRoots) != 2 {
		t.Fatalf("unexpected minified graph: roots=%d edges=%d submissions=%d anonymous=%d",
			roots, edges, submissions, len(anonymousRoots))
	}
	found := false
	for _, parents := range children {
		if len(parents) == 2 {
			found = true
			for parent := range parents {
				if !anonymousRoots[parent] {
					t.Fatalf("combined edge has an unobserved parent: %s", parent)
				}
			}
		}
	}
	if !found {
		t.Fatal("combined child has no pair of independently observed prerequisites")
	}
}
