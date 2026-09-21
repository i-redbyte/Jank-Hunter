package analyze

import "testing"

var retentionBenchmarkSink MemoryLeakSuspect

func BenchmarkRetentionEvidenceConstruction(b *testing.B) {
	for _, partial := range []bool{false, true} {
		name := "runtime"
		var heap *HeapLeakEvidence
		quality := retentionDataQuality{}
		if partial {
			name = "partial_heap"
			heap = &HeapLeakEvidence{ClassName: "example.Screen", Confidence: "низкое: граф HPROF неполон"}
			quality = retentionDataQuality{heapDegraded: true, heapNotes: []string{"HPROF: граф HPROF неполон"}}
		}
		b.Run(name, func(b *testing.B) {
			item := memoryLeakStats{className: "example.Screen", holder: "example.Owner", count: 2, afterExplicitGCCount: 2, maxAgeMs: 60000}
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				retentionBenchmarkSink = memoryLeakSuspectFromStats(item, 0, 0, heap, quality)
			}
		})
	}
}
