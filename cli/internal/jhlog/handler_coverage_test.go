package jhlog

import "testing"

func TestHandlerCoverageCounterIsNamedWithoutInventingExecutionCounts(t *testing.T) {
	const id = 0x203d
	if !IsKnownQualityCounter(id) || QualityCounterName(id) != "handler_post_context_unavailable_total" {
		t.Fatalf("successful Handler post coverage must survive decoding: known=%t name=%q", IsKnownQualityCounter(id), QualityCounterName(id))
	}
}

func TestHandlerARTFixtureCountsPostsWithoutClaimingExecutionOrOwner(t *testing.T) {
	log, err := readLog("../../../wire/testdata/handler-coverage-5.1.0.jhlog")
	if err != nil {
		t.Fatal(err)
	}
	if !log.Result.Sealed || log.Result.Status != SegmentStatusClosedClean {
		t.Fatalf("unsealed fixture: %+v", log.Result)
	}
	quality := log.Result.LatestQuality
	if quality == nil || quality.Counters[QualityHandlerPostContextUnavailable] != 7 || quality.Counters[QualityQueueFullTotal] != 0 {
		t.Fatalf("successful submissions differ from ART scenario: %+v", quality)
	}
	for _, event := range log.Events {
		if event.RuntimeCall != nil {
			t.Fatal("fixture invented an enqueue-to-execution edge")
		}
	}
}
