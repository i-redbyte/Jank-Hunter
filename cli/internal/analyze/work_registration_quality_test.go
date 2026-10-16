package analyze

import (
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
	"testing"
)

func TestRegistrationBudgetAndTimeoutCannotClaimCompleteObservations(t *testing.T) {
	for _, id := range []uint64{jhlog.QualityWorkRegistrationCapacity, jhlog.QualityWorkRegistrationTimeout,
		jhlog.QualityWorkChainDeclarationOmitted, jhlog.QualityWorkRequestBuilderOmitted} {
		q := crashCounterCollectionQuality(t, "unrelated", 0, map[uint64]uint64{id: 1})
		if q.DiagnosticCompletenessPercent != -1 || q.KnownLostEvents != 0 {
			t.Fatalf("registration observation loss became complete or an invented event count: %+v", q)
		}
	}
	q := crashCounterCollectionQuality(t, "unrelated", 0, map[uint64]uint64{jhlog.QualityWorkRegistrationClosed: 1})
	if q.KnownLostEvents != 0 {
		t.Fatal("stop means lost application work")
	}
}
