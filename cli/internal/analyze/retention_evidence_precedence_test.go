package analyze

import (
	"strings"
	"testing"
)

func TestAfterGCRetentionSurvivesWeakerObservationsAndHeapQuality(t *testing.T) {
	for _, test := range []struct {
		name    string
		timed   uint64
		heap    *HeapLeakEvidence
		quality retentionDataQuality
	}{
		{name: "after_gc_only"},
		{name: "mixed_runtime", timed: 1},
		{name: "no_heap_path", heap: &HeapLeakEvidence{ClassName: "example.Screen"}},
		{name: "incomplete_heap", timed: 1, heap: &HeapLeakEvidence{ClassName: "example.Screen"}, quality: retentionDataQuality{heapDegraded: true, heapNotes: []string{"partial heap"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			item := memoryLeakStats{className: "example.Screen", holder: "example.Owner", count: 1 + test.timed, timeOnlyCount: test.timed, afterExplicitGCCount: 1, maxAgeMs: 60000}
			got := memoryLeakSuspectFromStats(item, 0, 0, test.heap, test.quality)
			if got.EvidenceKind != RetentionEvidenceAfterExplicitGC || got.AfterExplicitGCCount != 1 {
				t.Fatalf("stronger runtime observation was lost: kind=%s afterGC=%d", got.EvidenceKind, got.AfterExplicitGCCount)
			}
			if got.EvidenceConfidence != "средняя" {
				t.Errorf("unrelated heap quality weakened runtime evidence: %s", got.EvidenceConfidence)
			}
			text := strings.Join(append(append([]string{got.Impact, got.Recommendation}, got.InvestigationSteps...), got.LeakChainActions...), " ")
			if strings.Contains(strings.ToLower(text), "исправление кода не требуется") {
				t.Errorf("unsupported exoneration: %s", text)
			}
		})
	}
}

func TestMissingHeapPathDoesNotProveCodeNeedsNoFix(t *testing.T) {
	got := memoryLeakSuspectFromStats(memoryLeakStats{className: "example.Screen", holder: "example.Owner", count: 1, timeOnlyCount: 1, maxAgeMs: 60000}, 0, 0,
		&HeapLeakEvidence{ClassName: "example.Screen"}, retentionDataQuality{heapDegraded: true, heapNotes: []string{"missing roots"}})
	if strings.Contains(strings.ToLower(got.Recommendation), "исправление кода не требуется") {
		t.Fatalf("unknown reachability became negative proof: %s", got.Recommendation)
	}
}

func TestRetentionSourcesPreserveIndependentPositiveNegativeAndUnknownStates(t *testing.T) {
	item := memoryLeakStats{className: "example.Screen", count: 2, timeOnlyCount: 1, afterExplicitGCCount: 1}
	for _, state := range []EvidenceState{EvidencePositive, EvidenceNegative, EvidenceUnknown} {
		got := memoryLeakSuspectFromStats(item, 0, 0, &HeapLeakEvidence{ClassName: item.className, Reachability: state}, retentionDataQuality{})
		if got.EvidenceSources.RuntimeAfterDelay != EvidencePositive || got.EvidenceSources.RuntimeAfterGC != EvidencePositive || got.EvidenceSources.HeapClassReachability != state {
			t.Errorf("independent evidence states lost: %+v", got.EvidenceSources)
		}
	}
}

func TestIncompleteHeapDoesNotPublishNegativeReachability(t *testing.T) {
	parser := newHprofParser("partial.hprof", map[string]struct{}{})
	parser.degrade("edges", "missing edges")
	evidence := &HeapEvidence{Leaks: []HeapLeakEvidence{{ClassName: "example.Screen", Reachability: EvidenceNegative, Confidence: "высокое: объект не достижим от распознанных корней GC"}}}
	parser.applyParseQuality(evidence)
	if evidence.Leaks[0].Reachability != EvidenceUnknown || strings.Contains(evidence.Leaks[0].Confidence, "не достижим") {
		t.Fatalf("incomplete graph published negative proof: %+v", evidence.Leaks[0])
	}
}

func TestLeakSummaryCountsIndependentRuntimeAndHeapObservations(t *testing.T) {
	item := memoryLeakStats{className: "example.Screen", count: 2, timeOnlyCount: 1, afterExplicitGCCount: 1}
	suspect := memoryLeakSuspectFromStats(item, 0, 0, &HeapLeakEvidence{ClassName: item.className}, retentionDataQuality{})
	stats := BuildLeakReport(Summary{MemoryLeaks: []MemoryLeakSuspect{suspect}}).Stats
	if stats.TimeOnly != 1 || stats.AfterExplicitGC != 1 || stats.UnconfirmedHPROF != 1 {
		t.Fatalf("summary erased an independent source: %+v", stats)
	}
}
