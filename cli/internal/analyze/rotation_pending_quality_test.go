package analyze

import (
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
	"testing"
)

func TestRotationSnapshotDoesNotInventPostAdmissionLossForFutureQueuedEvents(t *testing.T) {
	c := newCollector("rotation frontier", 1, Options{})
	c.add(map[uint64]string{1: "fixture"}, jhlog.Event{Type: jhlog.EventCounter, Metric: &jhlog.MetricEvent{MetricRef: jhlog.LocalSymbol(1), Value: 1}})
	snapshot := jhlog.QualitySnapshot{Sequence: 1, Counters: map[uint64]uint64{jhlog.QualityAcceptedEventTotal: 2, jhlog.QualityWrittenEventTotal: 1}}
	c.addStreamResult(jhlog.StreamResult{Source: "rotation.jhlog", Header: collectionTestHeader(91, 0),
		Status: jhlog.SegmentStatusClosedClean, Sealed: true, LatestQuality: &snapshot,
		SegmentEnd: &jhlog.SegmentEndEvent{Reason: jhlog.SegmentEndRotation}, DataRecords: 1})
	if err := c.validateSegmentIdentityConsistency(); err != nil {
		t.Fatal(err)
	}
	c.finalizeCollectionQuality()
	q := c.summary.CollectionQuality
	if q.PostAdmissionLostEvents != 0 || q.KnownLostEvents != 0 {
		t.Fatalf("queue frontier invented a lost event: %+v", q)
	}
	if q.UnobservedAcceptedEvents != 1 {
		t.Fatalf("missing unknown frontier count: %+v", q)
	}
	if q.Complete || q.DiagnosticCompletenessPercent != -1 {
		t.Fatalf("unobserved accepted event must remain unknown: %+v", q)
	}
}
