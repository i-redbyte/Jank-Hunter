package analyze

import "testing"

func BenchmarkHeapClassAssociation(b *testing.B) {
	item := memoryLeakStats{className: "com.app.Screen", holder: "lifecycle.activity", count: 2, afterExplicitGCCount: 2, maxAgeMs: 60000}
	heap := &HeapLeakEvidence{ClassName: item.className, Holder: "com.app.Owner", GCRoot: "sticky class", RetainedSizeKB: 8192,
		ReferencePath: []HeapPathElement{{Kind: "gc_root", ClassName: "GC root: sticky class"}, {Kind: "field", ClassName: item.className, FieldName: "screen", ObjectID: "0x20"}},
		DominatorTree: []string{"com.app.Child"},
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		retentionBenchmarkSink = memoryLeakSuspectFromStats(item, 0, 0, heap, retentionDataQuality{})
	}
}
