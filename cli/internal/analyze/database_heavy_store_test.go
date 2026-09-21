package analyze

import (
	"fmt"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func TestDatabaseHeavyStoreBoundsContextsAndRetainsCriticalLocation(t *testing.T) {
	store := newDatabaseStatementStore(1)
	statementKey := databaseStatementKey{query: "SELECT value FROM samples", operation: "чтение"}
	ordinary := jhlog.DatabaseEvent{
		Framework: jhlog.DatabaseFrameworkSQLite, Operation: jhlog.DatabaseOperationQuery,
		Outcome: jhlog.DatabaseOutcomeSuccess, DurationUS: 1_000,
	}
	for index := 0; index < databaseContextPerStatement; index++ {
		contextKey := databaseContextKey{statement: statementKey, source: fmt.Sprintf("Dao.load%d", index)}
		store.add(statementKey, contextKey, &ordinary, 0, 1, uint64(index))
	}
	criticalKey := databaseContextKey{statement: statementKey, source: "CriticalDao.load"}
	critical := ordinary
	critical.Outcome = jhlog.DatabaseOutcomeFailure
	critical.DurationUS = 500_000
	store.add(statementKey, criticalKey, &critical, uint64(jhlog.FlagThreadMain), 1, 100)

	entry := store.entries[statementKey]
	if entry == nil || len(entry.contexts) != databaseContextPerStatement ||
		store.evictedContexts != 1 || store.droppedContextEvents != 1 {
		t.Fatalf("bounded contexts: entry=%+v evicted=%d dropped=%d", entry, store.evictedContexts, store.droppedContextEvents)
	}
	for _, context := range entry.contexts {
		if context.key == criticalKey {
			if context.stats.main.calls != 1 || context.stats.overall.failures != 1 {
				t.Fatalf("critical context = %+v", context)
			}
			return
		}
	}
	t.Fatal("late critical context was not retained")
}

func TestDatabaseHeavyStoreMemoryRemainsBoundedUnderHighCardinality(t *testing.T) {
	store := newDatabaseStatementStore(64)
	event := jhlog.DatabaseEvent{
		Framework: jhlog.DatabaseFrameworkSQLite, Operation: jhlog.DatabaseOperationQuery,
		Outcome: jhlog.DatabaseOutcomeSuccess, DurationUS: 1_000,
	}
	for index := 0; index < 100_000; index++ {
		statementKey := databaseStatementKey{query: fmt.Sprintf("SELECT value_%d FROM samples", index), operation: "чтение"}
		contextKey := databaseContextKey{statement: statementKey, source: "SamplesDao.load"}
		store.add(statementKey, contextKey, &event, 0, 1, uint64(index))
	}

	if len(store.entries) != 64 || len(store.ranking) != 64 || cap(store.ranking) != 64 {
		t.Fatalf("statement store grew: entries=%d heap=%d/%d", len(store.entries), len(store.ranking), cap(store.ranking))
	}
	if len(store.statementFrequency.counters) != databaseFrequencySketchWidth*databaseFrequencySketchDepth ||
		len(store.contextFrequency.counters) != databaseFrequencySketchWidth*databaseFrequencySketchDepth {
		t.Fatalf("frequency sketches grew: statement=%d context=%d", len(store.statementFrequency.counters), len(store.contextFrequency.counters))
	}
	for key, entry := range store.entries {
		if len(entry.contexts) > databaseContextPerStatement {
			t.Fatalf("contexts for %+v grew to %d", key, len(entry.contexts))
		}
		if entry.heapIndex < 0 || entry.heapIndex >= len(store.ranking) || store.ranking[entry.heapIndex] != entry {
			t.Fatalf("heap index is inconsistent for %+v", key)
		}
	}
}

func BenchmarkDatabaseHeavyStoreHighCardinality(b *testing.B) {
	event := jhlog.DatabaseEvent{
		Framework: jhlog.DatabaseFrameworkSQLite, Operation: jhlog.DatabaseOperationQuery,
		Outcome: jhlog.DatabaseOutcomeSuccess, DurationUS: 1_000,
	}
	statementKeys := make([]databaseStatementKey, 100_000)
	contextKeys := make([]databaseContextKey, len(statementKeys))
	for index := range statementKeys {
		statementKeys[index] = databaseStatementKey{
			query: fmt.Sprintf("SELECT value_%d FROM samples", index), operation: "чтение",
		}
		contextKeys[index] = databaseContextKey{statement: statementKeys[index], source: "SamplesDao.load"}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		store := newDatabaseStatementStore(databaseStatementGroupLimit)
		for index := range statementKeys {
			store.add(statementKeys[index], contextKeys[index], &event, 0, 1, uint64(index))
		}
	}
}

func BenchmarkDatabaseHeavyStoreSteadyState(b *testing.B) {
	event := jhlog.DatabaseEvent{
		Framework: jhlog.DatabaseFrameworkSQLite, Operation: jhlog.DatabaseOperationQuery,
		Outcome: jhlog.DatabaseOutcomeSuccess, DurationUS: 1_000,
	}
	statementKey := databaseStatementKey{query: "SELECT value FROM samples", operation: "чтение"}
	contextKey := databaseContextKey{statement: statementKey, source: "SamplesDao.load"}
	store := newDatabaseStatementStore(databaseStatementGroupLimit)
	store.add(statementKey, contextKey, &event, 0, 1, 1)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		store.add(statementKey, contextKey, &event, 0, 1, uint64(index+2))
	}
}
