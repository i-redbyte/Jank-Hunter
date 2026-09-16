package analyze

import (
	"bytes"
	"testing"
)

func TestHeapClassPathCannotConfirmWatchedObject(t *testing.T) {
	for _, holder := range []string{"unknown", "lifecycle.activity", "com.app.LiveOwner"} {
		t.Run(holder, func(t *testing.T) {
			item := memoryLeakStats{className: "com.app.Screen", holder: holder, screen: "old-screen", operation: "closed", count: 1, timeOnlyCount: 1, maxAgeMs: 16000}
			baseline := memoryLeakSuspectFromStats(item, 0, 0, nil, retentionDataQuality{})
			for _, source := range []string{"current.hprof", "other-process.hprof", "earlier-run.hprof", "later-dump.hprof"} {
				heap := &HeapEvidence{Leaks: []HeapLeakEvidence{{ClassName: item.className, Holder: "com.app.LiveOwner", GCRoot: "sticky class", Source: source, RetainedSizeKB: 32768,
					ReferencePath: []HeapPathElement{{ClassName: "GC root: sticky class", Kind: "gc_root"}, {ClassName: "com.app.LiveOwner", Kind: "root_object", ObjectID: "0x10"}, {ClassName: item.className, Kind: "field", ObjectID: "0x20", FieldName: "currentScreen"}},
				}}}
				candidate := bestHeapEvidence(item, heap)
				if candidate == nil {
					t.Fatal("fixture must find the class candidate")
				}
				got := memoryLeakSuspectFromStats(item, 0, 0, candidate, retentionDataQuality{})
				if got.HeapEvidence || got.EvidenceKind == RetentionEvidenceConfirmedHPROFPath || got.Score != baseline.Score || got.Severity != baseline.Severity || got.EstimatedRetainedKB != baseline.EstimatedRetainedKB || got.Holder != baseline.Holder {
					t.Errorf("%s: independent class instance replaced runtime evidence: heap=%v kind=%s score=%.1f/%.1f size=%d/%d holder=%s/%s", source, got.HeapEvidence, got.EvidenceKind, got.Score, baseline.Score, got.EstimatedRetainedKB, baseline.EstimatedRetainedKB, got.Holder, baseline.Holder)
				}
			}
		})
	}
}

func TestTwoHeapInstancesDoNotIdentifyRuntimeObject(t *testing.T) {
	b := newMiniHprof()
	b.loadClass(0x100, b.string("com.app.LiveOwner"))
	b.loadClass(0x200, b.string("com.app.Screen"))
	field := b.string("currentScreen")
	var dump bytes.Buffer
	dump.WriteByte(0x05)
	writeU4(&dump, 0x100)
	b.classDump(&dump, 0x100, 16, []miniStaticField{{nameID: field, valueID: 0x1002}}, nil)
	b.classDump(&dump, 0x200, 48, nil, nil)
	// Old screen is present but unreachable. Only the new screen is rooted.
	b.instanceDump(&dump, 0x1001, 0x200, nil)
	b.instanceDump(&dump, 0x1002, 0x200, nil)
	b.record(hprofTagHeapDump, dump.Bytes())
	heap, err := LoadHeapEvidenceFiles([]string{writeMiniHprof(t, b.bytes())}, []string{"com.app.Screen"})
	if err != nil {
		t.Fatal(err)
	}
	item := memoryLeakStats{className: "com.app.Screen", holder: "lifecycle.activity", count: 1, afterExplicitGCCount: 1, maxAgeMs: 16000}
	candidate := bestHeapEvidence(item, heap)
	if candidate == nil || !confirmedHeapReferencePath(candidate) {
		t.Fatal("fixture lost the real path to the new instance")
	}
	got := memoryLeakSuspectFromStats(item, 0, 0, candidate, retentionDataQuality{})
	if got.HeapEvidence || got.EvidenceKind != RetentionEvidenceAfterExplicitGC {
		t.Fatalf("new screen path was attributed to old watched screen: heap=%v kind=%s", got.HeapEvidence, got.EvidenceKind)
	}
}

func TestHeapCandidatePreservesIndependentGraphAndRuntimeComparison(t *testing.T) {
	item := memoryLeakStats{className: "com.app.Screen", holder: "lifecycle.activity", count: 2, afterExplicitGCCount: 2, maxAgeMs: 60000}
	heap := &HeapLeakEvidence{ClassName: item.className, GCRoot: "sticky class", RetainedSizeKB: 8192,
		ReferencePath:     []HeapPathElement{{Kind: "gc_root", ClassName: "GC root: sticky class"}, {Kind: "field", ClassName: item.className, FieldName: "liveScreen", ObjectID: "0x20"}},
		AlternativePaths:  [][]HeapPathElement{{{Kind: "gc_root", ClassName: "GC root: JNI global"}, {Kind: "field", ClassName: item.className, FieldName: "nativeScreen"}}},
		ReferenceMatchers: []string{"example matcher"}, DominatorTree: []string{"com.app.Child"},
	}
	before := memoryLeakSuspectFromStats(item, 0, 0, nil, retentionDataQuality{})
	after := memoryLeakSuspectFromStats(item, 0, 0, heap, retentionDataQuality{})
	if after.WatchedObjectAssociation != EvidenceUnknown || after.EvidenceSources.HeapClassReachability != EvidencePositive || after.HeapClassEvidence == nil {
		t.Fatal("independent states lost")
	}
	if len(after.ReferencePath) != 0 || after.HeapClassEvidence.RetainedSizeKB != 8192 {
		t.Fatal("heap payload mixed with runtime estimate")
	}
	graph := BuildLeakGraph(after)
	if !graph.HasHeapPath || graph.TargetID != "0x20" || graph.Title != "HPROF: путь к экземпляру класса" {
		t.Fatalf("class graph lost: %+v", graph)
	}
	comparison := Compare(Summary{MemoryLeaks: []MemoryLeakSuspect{before}}, Summary{MemoryLeaks: []MemoryLeakSuspect{after}})
	report := BuildLeakCompareReport(comparison)
	if len(report.Deltas) != 1 || report.Deltas[0].Status != LeakDeltaSame || report.Stats.HeapConfirmedAfter != 0 || report.Stats.HeapClassPathsAfter != 1 || report.Stats.HeapUnconfirmedAfter != 0 {
		t.Fatalf("candidate caused false comparison: %+v", report.Stats)
	}
	failures := evaluateLeakGate(comparison, LeakThreshold{RequireHeapForHigh: true})
	if len(failures) == 0 {
		t.Fatal("class path satisfied gate requiring heap evidence for watched high-severity object")
	}
	heap.ReferencePath[1].FieldName = "mutated"
	heap.AlternativePaths[0][1].FieldName = "mutated"
	heap.ReferenceMatchers[0] = "mutated"
	heap.DominatorTree[0] = "mutated"
	snapshot := after.HeapClassEvidence
	if snapshot.ReferencePath[1].FieldName != "liveScreen" || snapshot.AlternativePaths[0][1].FieldName != "nativeScreen" || snapshot.ReferenceMatchers[0] != "example matcher" || snapshot.DominatorTree[0] != "com.app.Child" {
		t.Fatal("snapshot aliases caller-owned heap inputs")
	}
}
