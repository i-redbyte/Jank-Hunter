package main

import (
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
)

func TestHeapCandidateRootMetadataHasSeparateCSVColumns(t *testing.T) {
	table := leakSuspectsTable([]analyze.MemoryLeakSuspect{{ClassName: "app.Target", HeapClassEvidence: &analyze.HeapLeakEvidence{GCRoot: "JNI global", GCRootCategory: "jni", GCRootObjectID: "0x123", ReferencePathState: analyze.HeapPathTruncated}}, {ClassName: "app.NoCandidate"}})
	want := map[string]string{"heap_class_gc_root": "JNI global", "heap_class_gc_root_category": "jni", "heap_class_gc_root_object_id": "0x123", "heap_class_reference_path_state": "truncated", "gc_root": "", "gc_root_category": ""}
	for key, value := range want {
		index := -1
		for i, header := range table.header {
			if header == key {
				index = i
				break
			}
		}
		if index < 0 {
			t.Errorf("missing separate CSV column %s", key)
			continue
		}
		if table.rows[0][index] != value {
			t.Errorf("%s=%s, want%s", key, table.rows[0][index], value)
		}
		if table.rows[1][index] != "" {
			t.Errorf("no candidate invented %s", key)
		}
	}
}
