package mathanalysis

import "testing"

func TestBucketPagesPreserveRepeatedOutOfOrderValuesAndPartialLastPage(t *testing.T) {
	var series bucketSeries
	for _, index := range []int{64, 32, 0, 31, 32, 64} {
		if !series.add(index, 2, nil) {
			t.Fatal("unbounded add refused")
		}
	}
	points := make([]float64, 65)
	series.writeDense(points)
	if series.positiveBuckets != 4 || series.total != 12 {
		t.Fatalf("observations changed: %+v", series)
	}
	for index, value := range points {
		want := float64(0)
		switch index {
		case 0, 31:
			want = 2
		case 32, 64:
			want = 4
		}
		if value != want {
			t.Fatalf("bucket %d = %g, want %g", index, value, want)
		}
	}
}

func TestBucketPageQuotaIsCheckedBeforeStorageAndObservationsChange(t *testing.T) {
	budget := newCollectionBudget(mathMapBaseBytes + mathMapEntryBytes + bucketPageSize*8)
	account := budget.account("pages")
	var series bucketSeries
	if !series.add(31, 1, account) || !series.add(0, 2, account) || !series.add(31, 3, account) {
		t.Fatal("existing page must fit")
	}
	if series.add(32, 9, account) || len(series.pages) != 1 || series.positiveBuckets != 2 || series.total != 6 {
		t.Fatal("refused page changed retained storage or observations")
	}
	if budget.err() == nil || budget.used != budget.limit {
		t.Fatal("page quota failure was not retained")
	}
	account.close()
	if budget.used != 0 {
		t.Fatal("page ownership was not released")
	}
}
