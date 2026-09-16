package mathanalysis

import (
	"errors"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func TestRawDurationGrowthReservesBothArraysBeforeAllocation(t *testing.T) {
	budget := newCollectionBudget(16 * 8)
	bucket := timelineBucketAgg{account: budget.account("durations")}
	for i := 0; i < 16; i++ {
		bucket.addDuration(uint64(i))
	}
	bucket.addDuration(100)
	if len(bucket.httpDurations) != 16 || cap(bucket.httpDurations) != 16 || budget.used != 128 || budget.err() == nil {
		t.Fatalf("failed growth changed duration storage: %+v %+v", bucket, budget)
	}
	for i, duration := range bucket.httpDurations {
		if duration != uint64(i) {
			t.Fatal("growth refusal changed an earlier observation")
		}
	}
}

func TestHistogramOutliersConsumeQuotaOnlyForDistinctValues(t *testing.T) {
	budget := newCollectionBudget(256 * 1024)
	set := robustSampleSet{account: budget.account("histogram")}
	for i := 0; i < robustSampleSetPromotionThreshold; i++ {
		set.add(7)
	}
	if !set.compacted() {
		t.Fatal("fixture did not reach exact histogram storage")
	}
	set.add(1000)
	used := budget.used
	for i := 0; i < 1000; i++ {
		set.add(1000)
	}
	if budget.used != used {
		t.Fatal("repeated outlier consumed additional storage")
	}
	budget.limit = used + mathMapEntryBytes + 64
	set.add(2000)
	seen := set.seen
	set.add(3000)
	if budget.err() == nil || budget.used != budget.limit || set.seen != seen || len(set.outlierCounts) != 2 {
		t.Fatal("refused distinct outlier changed histogram observations")
	}
}

func TestPendingStallStorageSharesQuotaAndReleasesAfterSessionEnd(t *testing.T) {
	paths := writeRotatedStallFixture(t, false)
	budget := newCollectionBudget(64 * 1024)
	lifecycle := mathStallLifecycle{account: budget.account("stalls")}
	symbols := newMathSymbolResolver(analyze.Options{})
	count := 0
	consume := func(event jhlog.Event, _ map[uint64]string) error {
		if event.Stall != nil {
			count++
		}
		return nil
	}
	if err := lifecycle.stream(paths[1], symbols, consume); err != nil {
		t.Fatal(err)
	}
	if budget.used == 0 || len(lifecycle.pending) != 1 || count != 0 {
		t.Fatal("ongoing payload was not held with a storage lease")
	}
	if err := lifecycle.finish(consume); err != nil {
		t.Fatal(err)
	}
	if budget.used != 0 || lifecycle.pending != nil || lifecycle.tracker.HasIncident(17) || count != 1 {
		t.Fatal("session end retained a payload, identity or quota")
	}

	// Leave enough space for identity and pending-map capacity, but not the retained payload.
	budget = newCollectionBudget(mathMapEntryBytes + 2*mathMapBaseBytes + uint64(jhlog.MaxPendingStallIncidents)*mathMapEntryBytes)
	lifecycle = mathStallLifecycle{account: budget.account("stalls")}
	err := lifecycle.stream(paths[1], symbols, consume)
	var limited *collectionBudgetError
	if !errors.As(err, &limited) || len(lifecycle.pending) != 0 || count != 1 || budget.used > budget.limit {
		t.Fatalf("payload quota was bypassed or partial stall delivered: %v", err)
	}
	lifecycle.account.close()
	if budget.used != 0 {
		t.Fatal("error path retained quota")
	}
}
