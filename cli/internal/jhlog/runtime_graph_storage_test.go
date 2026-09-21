package jhlog

import "testing"

func TestRuntimeGraphStorageARTFixtureUsesKnownQualityAndNeverInventsSkippedEdges(t *testing.T) {
	var count uint64
	result, err := StreamFileWithResult("../../../wire/testdata/runtime-graph-storage-5.1.0.jhlog", func(event Event, _ map[uint64]string) error {
		if edge := event.RuntimeCall; edge != nil {
			count += edge.Count
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Sealed || result.Status != SegmentStatusClosedClean || result.LatestQuality == nil {
		t.Fatalf("fixture is not sealed: %+v", result)
	}
	if got := result.LatestQuality.Counters[QualityRuntimeGraphStorageSkippedEntryTotal]; got != 7 || count != 255 {
		t.Fatalf("omitted entries=%d, captured calls=%d", got, count)
	}
	if QualityCounterName(QualityRuntimeGraphStorageSkippedEntryTotal) != "runtime_graph_storage_skipped_entry_total" {
		t.Fatal("counter name changed")
	}
}
