package jhlog

import "testing"

func TestDeepSQLiteARTFixturePreservesEveryTransactionAndStatement(t *testing.T) {
	log, err := readLog("../../../wire/testdata/database-depth-5.1.0.jhlog")
	if err != nil {
		t.Fatal(err)
	}
	if !log.Result.Sealed || log.Result.Status != SegmentStatusClosedClean {
		t.Fatalf("fixture not finalized: %+v", log.Result)
	}
	if quality := log.Result.LatestQuality; quality == nil || quality.Counters[QualityQueueFullTotal] != 0 {
		t.Fatalf("paced fixture lost events at admission: %+v", quality)
	}
	stack := make([]DatabaseTransactionEvent, 0, 65)
	seen := make(map[uint64]bool)
	statements := make(map[uint64]uint64)
	begins, terminals, sql, maximumDepth := 0, 0, 0, 0
	for _, event := range log.Events {
		if transaction := event.DatabaseTransaction; transaction != nil {
			if transaction.Stage == DatabaseTransactionBegin {
				parent := uint64(0)
				if len(stack) > 0 {
					parent = stack[len(stack)-1].TransactionID
				}
				if transaction.TransactionID == 0 || seen[transaction.TransactionID] || transaction.ParentID != parent {
					t.Fatalf("invalid deep transaction lineage at depth %d: %+v", len(stack)+1, transaction)
				}
				seen[transaction.TransactionID] = true
				stack = append(stack, *transaction)
				begins++
				maximumDepth = max(maximumDepth, len(stack))
				continue
			}
			if len(stack) == 0 {
				t.Fatalf("terminal without begin: %+v", transaction)
			}
			begin := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if transaction.TransactionID != begin.TransactionID || transaction.ParentID != begin.ParentID || transaction.SourceRef != begin.SourceRef {
				t.Fatalf("terminal mismatched its active level: %+v, begin=%+v", transaction, begin)
			}
			want := DatabaseTransactionSuccess
			if begins > 65 {
				want = DatabaseTransactionRollback
			}
			if transaction.Outcome != want || transaction.StatementCount != 1 || transaction.ReadCount != 0 || transaction.WriteCount != 1 || statements[transaction.TransactionID] != 1 {
				t.Fatalf("deep completion disagrees with SQLite or SQL events: %+v", transaction)
			}
			terminals++
		}
		if statement := event.Database; statement != nil {
			if len(stack) == 0 || statement.TransactionID != stack[len(stack)-1].TransactionID {
				t.Fatalf("SQL event attached to another level: %+v, depth=%d", statement, len(stack))
			}
			statements[statement.TransactionID]++
			sql++
		}
	}
	if begins != 130 || terminals != 130 || sql != 130 || maximumDepth != 65 || len(stack) != 0 {
		t.Fatalf("incomplete deep fixture: begin=%d terminal=%d sql=%d maxDepth=%d remaining=%d", begins, terminals, sql, maximumDepth, len(stack))
	}
}
