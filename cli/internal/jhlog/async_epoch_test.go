package jhlog

import "testing"

func TestAsyncEpochARTFixturesKeepCompletionsAndTransactionLinksInTheirOwnSession(t *testing.T) {
	for _, name := range []string{"old", "new"} {
		log, err := readLog("../../../wire/testdata/async-epoch-" + name + "-5.1.0.jhlog")
		if err != nil {
			t.Fatal(err)
		}
		if !log.Result.Sealed || log.Result.Status != SegmentStatusClosedClean {
			t.Fatalf("%s fixture was not cleanly sealed: status=%v quality=%+v", name, log.Result.Status, log.Result.LatestQuality)
		}
		q := log.Result.LatestQuality.Counters
		if q[QualityAcceptedEventTotal] != q[QualityWrittenEventTotal] || q[QualityWriterIOErrorTotal] != 0 {
			t.Fatalf("%s fixture lost admitted events: %+v", name, q)
		}
		workers, queries, requests, transactions := 0, 0, 0, 0
		queryTransactions := make(map[uint64]uint64)
		terminalStatements := make(map[uint64]uint64)
		for _, event := range log.Events {
			if w := event.Worker; w != nil {
				workers++
				if name == "old" && (w.Stage != WorkerStageStarted || w.InstanceID != 11) {
					t.Fatalf("old worker: %+v", w)
				}
				if name == "new" && w.InstanceID != 22 {
					t.Fatalf("old worker leaked into replacement: %+v", w)
				}
			}
			if db := event.Database; db != nil {
				queries++
				queryTransactions[db.TransactionID]++
			}
			if event.HTTP != nil {
				requests++
			}
			if tx := event.DatabaseTransaction; tx != nil {
				transactions++
				if name == "new" && tx.ParentID != 0 {
					t.Fatalf("old parent leaked into replacement: %+v", tx)
				}
				if tx.Stage == DatabaseTransactionStage(2) {
					terminalStatements[tx.TransactionID] = tx.StatementCount
				}
			}
		}
		if name == "old" {
			if workers != 1 || queries != 0 || requests != 0 || transactions != 2 {
				t.Fatalf("old: worker=%d sql=%d http=%d tx=%d", workers, queries, requests, transactions)
			}
			if q[QualityAsyncUnfinishedHTTP] != 1 || q[QualityAsyncUnfinishedDatabase] != 2 || q[QualityAsyncUnfinishedWorker] != 1 || q[QualityAsyncUnfinishedDatabaseTransaction] != 2 {
				t.Fatalf("old epoch boundary is absent from its sealed snapshot: %+v", q)
			}
		} else {
			if workers != 2 || queries != 3 || requests != 1 || transactions != 4 {
				t.Fatalf("new: worker=%d sql=%d http=%d tx=%d", workers, queries, requests, transactions)
			}
			if q[QualityAsyncCompletionStale] != 5 || q[QualityAsyncCompletionDuplicate] != 3 {
				t.Fatalf("new rejections: %+v", q)
			}
			if len(queryTransactions) != 2 || len(terminalStatements) != 2 || queryTransactions[0] != 0 {
				t.Fatalf("missing transaction associations: %+v / %+v", queryTransactions, terminalStatements)
			}
			for id, count := range queryTransactions {
				if terminalStatements[id] != count {
					t.Fatalf("transaction %d queries=%d terminal=%d", id, count, terminalStatements[id])
				}
			}
		}
	}
}

func TestAsyncEpochQualityCounterNamesSurviveDecoding(t *testing.T) {
	names := []string{"async_completion_stale_total", "async_completion_duplicate_total", "async_completion_invalid_total",
		"async_token_capacity_rejected_total", "async_token_id_exhausted_total", "async_completion_feature_disabled_total",
		"async_unfinished_http_total", "async_unfinished_database_total", "async_unfinished_worker_total",
		"async_unfinished_database_transaction_total", "async_completion_in_progress_at_stop_total", "http_legacy_context_completion_total"}
	for index, name := range names {
		id := uint64(0x203e + index)
		if !IsKnownQualityCounter(id) || QualityCounterName(id) != name {
			t.Errorf("counter %x: known=%t name=%q, want %q", id, IsKnownQualityCounter(id), QualityCounterName(id), name)
		}
	}
}
