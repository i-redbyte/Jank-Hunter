package analyze

import "testing"

func TestGenerationCapacityRejectionsReduceCompleteness(t *testing.T) {
	for _, id := range []uint64{0x203a, 0x203b} {
		counters := map[uint64]uint64{id: 7}
		if id == 0x203b {
			counters[0x201b] = 7
		}
		quality := crashCounterCollectionQuality(t, "unrelated", 0, counters)
		if quality.BoundedEvidenceLoss != 7 || quality.Complete || quality.DiagnosticCompletenessPercent >= 100 {
			t.Errorf("generation loss %x ignored: %+v", id, quality)
		}
	}
}

func TestSkippedGraphEntriesDoNotInventAnEdgeCount(t *testing.T) {
	quality := crashCounterCollectionQuality(t, "unrelated", 0, map[uint64]uint64{0x203c: 7})
	if quality.BoundedEvidenceLoss != 0 || quality.DiagnosticCompletenessPercent != -1 || quality.Complete {
		t.Errorf("skipped method entries are not a known number of lost edges: %+v", quality)
	}
}

func TestRuntimeGenerationARTFixtureKeepsUnknownCompletenessNumeric(t *testing.T) {
	summary, err := InspectFilesWithOptions("generation restart", []string{"../../../wire/testdata/runtime-generation-5.1.0.jhlog"}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	q := summary.CollectionQuality
	if q.BoundedEvidenceLoss != 3 || q.DiagnosticCompletenessPercent != -1 || q.Complete {
		t.Fatalf("generation quality=%+v", q)
	}
	if q.RuntimeGraphInputEvents != 2 || q.DecodedRuntimeGraphCalls != 1 || q.RuntimeGraphCompletenessRatio != 0.5 {
		t.Fatalf("generation graph accounting=%+v", q)
	}
}

func TestRejectedGenerationGraphEventsAreNotAlsoOtherEvidenceLoss(t *testing.T) {
	quality := crashCounterCollectionQuality(t, "unrelated", 0, map[uint64]uint64{0x203b: 7, 0x201b: 7})
	if quality.BoundedEvidenceLoss != 7 || quality.OtherEvidenceLoss != 0 || quality.RuntimeGraphCompletenessRatio != 0 {
		t.Fatalf("graph generation loss counted in another component: %+v", quality)
	}
}
