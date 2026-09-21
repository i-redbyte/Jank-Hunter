package analyze

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestHeapTypedNonGraphDiagnosticsDoNotInventLostGraph(t *testing.T) {
	for _, impact := range []string{"none", "display", "retained_size"} {
		var heap HeapEvidence
		raw := fmt.Sprintf(`{"warnings":["arbitrary text"],"diagnostics_version":1,"diagnostics":[{"code":"test","severity":"info","impact":%q,"source":"complete.hprof","message":"arbitrary text"}]}`, impact)
		if err := json.Unmarshal([]byte(raw), &heap); err != nil {
			t.Fatal(err)
		}
		c := newCollector("quality", 0, Options{HeapEvidence: &heap})
		if q := c.retentionDataQuality(); q.heapDegraded {
			t.Errorf("impact%s invented lost graph: %+v", impact, q)
		}
	}
}

func TestHeapDiagnosticMergeMappingAndJSONPreserveImpact(t *testing.T) {
	info := &HeapEvidence{}
	info.AddDiagnostic(HeapDiagnostic{Code: "auto_discovery", Severity: HeapDiagnosticInfo, Impact: HeapImpactNone, Source: "first.hprof", Message: "arbitrary"})
	partial := heapPathChain(2, "JNI global")
	partial.path = "second.hprof"
	partial.degrade("edges", "same message")
	heap := MergeHeapEvidence(info, partial.evidence(), info)
	if len(heap.Diagnostics) != 2 {
		t.Fatalf("merge duplicated or lost metadata: %+v", heap.Diagnostics)
	}
	mapped := DeobfuscateHeapEvidence(heap, &NameMapping{})
	if !reflect.DeepEqual(mapped.Diagnostics, heap.Diagnostics) {
		t.Fatal("mapping changed provenance")
	}
	data, err := json.Marshal(mapped)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "heap.json")
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadHeapEvidenceFiles([]string{path}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded.Diagnostics, heap.Diagnostics) {
		t.Fatal("JSON round trip changed impact")
	}
	mapped.Diagnostics[0].Message = "mutated"
	if heap.Diagnostics[0].Message == "mutated" {
		t.Fatal("mapping retained caller-owned diagnostics slice")
	}
}

func TestHeapDiagnosticLegacyAndUnknownRemainConservative(t *testing.T) {
	for _, raw := range []string{
		`{"warnings":["legacy"]}`,
		`{"diagnostics_version":99,"diagnostics":[{"message":"future"}]}`,
		`{"diagnostics_version":1,"diagnostics":[{"code":"future","severity":"warning","impact":"new_kind","message":"unknown"}]}`,
	} {
		var heap HeapEvidence
		if err := json.Unmarshal([]byte(raw), &heap); err != nil {
			t.Fatal(err)
		}
		c := newCollector("legacy", 0, Options{HeapEvidence: &heap})
		if !c.retentionDataQuality().heapDegraded {
			t.Fatalf("unknown impact ignored: %s", raw)
		}
		merged := MergeHeapEvidence(&heap)
		c = newCollector("merged", 0, Options{HeapEvidence: merged})
		if !c.retentionDataQuality().heapDegraded {
			t.Fatal("merge upgraded unknown impact")
		}
	}
}

func TestHeapParserImpactClassificationIsIndependentOfMessage(t *testing.T) {
	for _, test := range []struct {
		code     string
		degraded bool
	}{{"edges", true}, {"roots", true}, {"nodes", true}, {"reference-path-depth", false}, {"retained-sample", false}, {"unresolved-instance-sizes", false}, {"retained-traversal", false}, {"analysis-work", true}} {
		p := heapPathChain(2, "JNI global")
		p.degrade(test.code, "same message")
		c := newCollector("impact", 0, Options{HeapEvidence: p.evidence()})
		if c.retentionDataQuality().heapDegraded != test.degraded {
			t.Errorf("%s classification depends on text", test.code)
		}
	}
}

func TestHprofDisplayTruncationDoesNotDegradeGraphQuality(t *testing.T) {
	heap := heapPathChain(60, "JNI global").evidence()
	c := newCollector("fragment", 0, Options{HeapEvidence: heap})
	if c.retentionDataQuality().heapDegraded {
		t.Fatal("complete graph degraded because display is truncated")
	}
}

func TestHprofParserEmitsTypedDiagnosticProvenance(t *testing.T) {
	p := heapPathChain(2, "JNI global")
	p.path = "partial.hprof"
	p.degrade("edges", "same arbitrary message")
	heap := p.evidence()
	data, err := json.Marshal(heap)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err = json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	items, ok := raw["diagnostics"].([]any)
	if !ok || len(items) == 0 {
		t.Fatalf("missing typed diagnostics: %s", data)
	}
	d := items[0].(map[string]any)
	if d["code"] != "edges" || d["impact"] != "graph_incomplete" || d["severity"] != "warning" || d["source"] != "partial.hprof" {
		t.Fatalf("wrong provenance: %+v", d)
	}
}

func TestHeapDiagnosticDoesNotContaminateAnotherSource(t *testing.T) {
	var heap HeapEvidence
	if err := json.Unmarshal([]byte(`{"warnings":["partial second dump"],"diagnostics_version":1,"diagnostics":[{"code":"edges","severity":"warning","impact":"graph_incomplete","source":"partial.hprof","message":"partial second dump"}]}`), &heap); err != nil {
		t.Fatal(err)
	}
	c := newCollector("separate sources", 0, Options{HeapEvidence: &heap})
	q := c.retentionDataQuality()
	item := memoryLeakStats{className: "app.Target", count: 1, afterExplicitGCCount: 1, maxAgeMs: 60000}
	got := memoryLeakSuspectFromStats(item, 0, 0, &HeapLeakEvidence{ClassName: "app.Target", Source: "complete.hprof", Reachability: EvidencePositive}, q)
	if got.DataQuality != "complete" {
		t.Fatalf("another source contaminated candidate: %+v", got.QualityWarnings)
	}
}

func TestLegacyHeapWarningCannotInferSourceFromContainer(t *testing.T) {
	heap := &HeapEvidence{Sources: []string{"evidence.json"}, Warnings: []string{"legacy incomplete input"}}
	c := newCollector("legacy source", 0, Options{HeapEvidence: heap})
	got := memoryLeakSuspectFromStats(memoryLeakStats{className: "app.Target", count: 1, afterExplicitGCCount: 1}, 0, 0, &HeapLeakEvidence{ClassName: "app.Target", Source: "actual.hprof"}, c.retentionDataQuality())
	if got.DataQuality != "degraded" {
		t.Fatal("container filename incorrectly excluded legacy warning")
	}
}

func TestMergedUnknownDiagnosticRetainsLegacyWarningProjection(t *testing.T) {
	heap := MergeHeapEvidence(&HeapEvidence{DiagnosticsVersion: 99, Diagnostics: []HeapDiagnostic{{Message: "future"}}})
	if heap == nil || len(heap.Warnings) == 0 {
		t.Fatal("converted unknown diagnostics disappeared for legacy JSON readers")
	}
}
