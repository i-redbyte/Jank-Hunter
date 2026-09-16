package analyze

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"unsafe"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func TestHprofReferenceRootSurvivesTruncation(t *testing.T) {
	for _, kind := range []string{"JNI global", "Java frame", "sticky class", "monitor"} {
		for _, nodes := range []int{1, 47, 48, 49, 60, 1000} {
			t.Run(fmt.Sprintf("%s/%d", kind, nodes), func(t *testing.T) {
				p := heapPathChain(nodes, kind)
				e := p.evidence()
				leak := e.Leaks[0]
				if leak.GCRoot != kind || leak.GCRootCategory != heapRootCategory(kind) {
					t.Errorf("truncation changed actual root: got %q/%q, want %q", leak.GCRoot, leak.GCRootCategory, kind)
				}
				if len(leak.ReferencePath) == 0 || leak.ReferencePath[0].Kind != "gc_root" || leak.ReferencePath[0].ObjectID != "0x1" {
					t.Errorf("actual root identity lost: %+v", leak.ReferencePath)
				}
				truncated := nodes > maxHprofPathElements
				if p.hasDegradation("reference-path-depth") != truncated {
					t.Errorf("path boundary %d: truncation diagnostic=%v want %v", nodes, p.hasDegradation("reference-path-depth"), truncated)
				}
				gap := false
				for _, step := range leak.ReferencePath {
					gap = gap || step.Kind == "truncated"
				}
				if gap != truncated {
					t.Errorf("path does not explicitly mark omitted edges: gap=%v want %v", gap, truncated)
				}
				if leak.Reachability != EvidencePositive || leak.RetainedSizeState != HeapSizeExact || leak.RetainedSizeBytes != 64 {
					t.Errorf("display truncation changed computed reachability/size: %+v", leak)
				}
				if len(leak.ReferencePath) > maxHprofPathElements+1 {
					t.Fatal("unbounded display path")
				}
			})
		}
	}
}

func TestHeapRootLabelRejectsFieldOnlyFragments(t *testing.T) {
	for _, kind := range []string{"field", "static", "root_object", "truncated"} {
		if got := heapRootLabel([]HeapPathElement{{ClassName: "app.Owner", Kind: kind}}); got != "" {
			t.Errorf("fragment kind %s became GC root %q", kind, got)
		}
	}
}

func TestHprofMissingRootCannotInventPath(t *testing.T) {
	p := heapPathChain(60, "JNI global")
	p.roots = nil
	leak := p.evidence().Leaks[0]
	if leak.GCRoot != "" || len(leak.ReferencePath) != 0 || leak.Reachability != EvidenceUnknown {
		t.Fatalf("missing root invented positive path: %+v", leak)
	}
}

func TestHprofPathMetadataSurvivesJSON(t *testing.T) {
	for _, nodes := range []int{1, 48, 49, 60} {
		heap := heapPathChain(nodes, "JNI global").evidence()
		for _, mapping := range []*NameMapping{nil, {}} {
			mapped := DeobfuscateHeapEvidence(heap, mapping)
			data, err := json.Marshal(mapped.Leaks[0])
			if err != nil {
				t.Fatal(err)
			}
			var raw map[string]any
			if err := json.Unmarshal(data, &raw); err != nil {
				t.Fatal(err)
			}
			want := "complete"
			if nodes > 48 {
				want = "truncated"
			}
			if raw["reference_path_state"] != want || raw["gc_root_object_id"] != "0x1" {
				t.Errorf("nodes=%d: path state/root identity lost: state=%v rootID=%v", nodes, raw["reference_path_state"], raw["gc_root_object_id"])
			}
		}
	}
}

func TestHprofImportedFragmentDoesNotProveReachability(t *testing.T) {
	var leak HeapLeakEvidence
	data := []byte(`{"class_name":"app.Target","gc_root":"JNI global","reference_path":[{"class_name":"GC root: JNI global","kind":"gc_root"},{"class_name":"…","kind":"truncated"},{"class_name":"app.Target","kind":"field"}]}`)
	if err := json.Unmarshal(data, &leak); err != nil {
		t.Fatal(err)
	}
	normalizeHeapLeak(&leak)
	if leak.Reachability != EvidenceUnknown {
		t.Fatalf("fragment alone invented reachability: %s", leak.Reachability)
	}
	leak.Reachability = EvidencePositive
	normalizeHeapLeak(&leak)
	if leak.Reachability != EvidencePositive {
		t.Fatal("explicit independently computed reachability erased")
	}
}

func TestHprofMappingCannotRenameGCRootKind(t *testing.T) {
	heap := heapPathChain(60, "monitor").evidence()
	mapped := DeobfuscateHeapEvidence(heap, &NameMapping{classes: map[string]string{"monitor": "app.Wrong", "app.Owner": "app.RealOwner", "…": "app.WrongGap"}})
	leak := mapped.Leaks[0]
	if leak.GCRoot != "monitor" || leak.GCRootCategory != "monitor" || leak.GCRootObjectID != "0x1" {
		t.Fatalf("mapping renamed root metadata: %q/%q/%q", leak.GCRoot, leak.GCRootCategory, leak.GCRootObjectID)
	}
	if leak.ReferencePath[1].ClassName != "app.RealOwner" || leak.ReferencePath[2].ClassName != "…" || leak.ReferencePath[2].Kind != "truncated" {
		t.Fatal("mapping lost class name or gap marker")
	}
}

func TestHprofAlternativePathsKeepTheirOwnRoots(t *testing.T) {
	p := heapPathChain(3, "JNI global")
	p.ensureNode(10, "app.OtherRoot", 8)
	p.addEdge(p.nodeByID(10), 2, "other", "field")
	p.roots = append(p.roots, heapRoot{id: 10, kind: "Java frame"})
	e := p.evidence()
	leak := e.Leaks[0]
	if leak.GCRoot != "JNI global" || leak.GCRootObjectID != "0x1" {
		t.Fatal("primary root changed")
	}
	if len(leak.AlternativePaths) == 0 {
		t.Fatal("alternative path missing")
	}
	path := leak.AlternativePaths[0]
	if heapRootLabel(path) != "Java frame" || path[0].ObjectID != "0xa" {
		t.Fatalf("alternative path inherited primary root: %+v", path)
	}
}

func TestHprofLegacyPathDisplayStateIsUnknown(t *testing.T) {
	for _, state := range []HeapPathState{"", "future", HeapPathUnknown} {
		leak := HeapLeakEvidence{ClassName: "app.Target", ReferencePathState: state, ReferencePath: []HeapPathElement{{ClassName: "GC root: JNI global", ObjectID: "0x1", Kind: "gc_root"}, {ClassName: "app.Target", ObjectID: "0x2", Kind: "field"}}}
		normalizeHeapLeak(&leak)
		if leak.ReferencePathState != HeapPathUnknown || leak.GCRootObjectID != "0x1" {
			t.Fatalf("legacy display claimed complete: %+v", leak)
		}
	}
}

func TestHeapRootProvenanceRecordsStayCompact(t *testing.T) {
	if unsafe.Sizeof(heapParent{}) != 16 {
		t.Fatalf("parent=%d bytes, want16", unsafe.Sizeof(heapParent{}))
	}
}

func TestHprofDisplayGraphDoesNotInventEdgesAcrossGap(t *testing.T) {
	for _, nodes := range []int{3, 60} {
		leak := heapPathChain(nodes, "JNI global").evidence().Leaks[0]
		graph := BuildLeakGraph(MemoryLeakSuspect{HeapClassEvidence: &leak})
		gaps := map[string]bool{}
		for _, node := range graph.Nodes {
			if node.Label == "…" {
				gaps[node.ID] = true
			}
		}
		for _, edge := range graph.Edges {
			if edge.From == edge.To {
				t.Errorf("synthetic GC-root/object pair invented self-reference %s", edge.From)
			}
			if gaps[edge.From] || gaps[edge.To] {
				t.Errorf("display gap became object reference: %+v", edge)
			}
		}
		if nodes == 60 {
			if graph.Title != "HPROF: фрагмент пути к экземпляру класса" {
				t.Errorf("fragment title claims complete displayed path: %s", graph.Title)
			}
			// Only explicitly observed links between the retained suffix nodes remain.
			for _, edge := range graph.Edges {
				if edge.From == "0x1" && edge.To != "0x1" {
					t.Errorf("root linked directly over omitted nodes: %+v", edge)
				}
			}
		}
	}
}

func TestHprofPathRootEndToEnd(t *testing.T) {
	dir := os.Getenv("JH_GM11_FIXTURES")
	if dir == "" {
		dir = t.TempDir()
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(dir, "sample.jhlog")
	if err := jhlog.WriteSample(input); err != nil {
		t.Fatal(err)
	}
	const targetClass = "com.app.checkout.CheckoutActivity"
	for _, nodes := range []int{48, 49, 60} {
		b := newMiniHprof()
		b.loadClass(0x301, b.string("com.app.checkout.CheckoutPresenter"))
		b.loadClass(0x302, b.string(targetClass))
		field := b.string("next")
		var heap bytes.Buffer
		heap.WriteByte(0x01)
		writeU4(&heap, 1)
		writeU4(&heap, 0)
		b.classDump(&heap, 0x301, 64, nil, []miniField{{nameID: field, typ: hprofTypeObject}})
		b.classDump(&heap, 0x302, 64, nil, nil)
		for i := 1; i <= nodes; i++ {
			if i == nodes {
				b.instanceDump(&heap, uint32(i), 0x302, nil)
			} else {
				b.instanceDump(&heap, uint32(i), 0x301, []uint32{uint32(i + 1)})
			}
		}
		b.record(hprofTagHeapDump, heap.Bytes())
		path := filepath.Join(dir, fmt.Sprintf("chain-%d.hprof", nodes))
		if err := os.WriteFile(path, b.bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		e, err := LoadHeapEvidenceFiles([]string{path}, []string{targetClass})
		if err != nil {
			t.Fatal(err)
		}
		summary, err := InspectFilesWithOptions("path", []string{input}, Options{HeapEvidence: e})
		if err != nil {
			t.Fatal(err)
		}
		leak, ok := memoryLeakByClass(summary.MemoryLeaks, targetClass)
		if !ok || leak.HeapClassEvidence == nil {
			for _, h := range e.Leaks {
				t.Logf("heap class=%s root=%s", h.ClassName, h.GCRoot)
			}
			for _, s := range summary.MemoryLeaks {
				t.Logf("summary class=%s candidate=%v", s.ClassName, s.HeapClassEvidence != nil)
			}
			t.Fatalf("class candidate lost: matched=%v", ok)
		}
		candidate := leak.HeapClassEvidence
		want := HeapPathComplete
		if nodes > 48 {
			want = HeapPathTruncated
		}
		if candidate.ReferencePathState != want || candidate.GCRoot != "JNI global" || candidate.GCRootObjectID != "0x1" || candidate.Reachability != EvidencePositive {
			t.Fatalf("HPROF→CLI lost root/state: %+v", candidate)
		}
	}
}
