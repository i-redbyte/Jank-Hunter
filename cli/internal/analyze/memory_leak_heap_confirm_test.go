package analyze

import "testing"

func TestHeapConfirmsWatchedRetentionWithOwnerHint(t *testing.T) {
	item := memoryLeakStats{
		className:            "io.jankhunter.sample.RetainedCheckoutScreen",
		holder:               "sample.auto.retention.activity_registry",
		count:                1,
		afterExplicitGCCount: 1,
		maxAgeMs:             20_000,
	}
	heap := &HeapLeakEvidence{
		ClassName: item.className,
		GCRoot:    "sticky class",
		ReferencePath: []HeapPathElement{
			{Kind: "gc_root", ClassName: "GC root: sticky class"},
			{Kind: "field", ClassName: "io.jankhunter.sample.SampleApplication", FieldName: "retainedObjects"},
			{Kind: "field", ClassName: item.className, FieldName: "activity_registry"},
		},
	}
	got := memoryLeakSuspectFromStats(item, 0, 0, heap, retentionDataQuality{})
	if got.EvidenceKind != RetentionEvidenceConfirmedHPROFPath || !got.HeapEvidence || len(got.ReferencePath) < 2 {
		t.Fatalf("expected confirmed heap path: kind=%s heap=%v path=%d", got.EvidenceKind, got.HeapEvidence, len(got.ReferencePath))
	}
}

func TestLifecycleHolderDoesNotConfirmHeapPath(t *testing.T) {
	item := memoryLeakStats{
		className:            "com.app.Screen",
		holder:               "lifecycle.activity",
		count:                1,
		afterExplicitGCCount: 1,
	}
	heap := &HeapLeakEvidence{
		ClassName: item.className,
		GCRoot:    "sticky class",
		ReferencePath: []HeapPathElement{
			{Kind: "gc_root", ClassName: "GC root: sticky class"},
			{Kind: "field", ClassName: item.className, FieldName: "liveScreen"},
		},
	}
	got := memoryLeakSuspectFromStats(item, 0, 0, heap, retentionDataQuality{})
	if got.EvidenceKind != RetentionEvidenceAfterExplicitGC || got.HeapEvidence || len(got.ReferencePath) != 0 {
		t.Fatalf("lifecycle holder must not promote heap evidence: kind=%s heap=%v path=%d", got.EvidenceKind, got.HeapEvidence, len(got.ReferencePath))
	}
}
