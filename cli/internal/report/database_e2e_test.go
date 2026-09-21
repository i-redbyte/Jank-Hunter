package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func TestDatabaseJHLogAnalyzeReportEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	logPath := filepath.Join(tempDir, "database-scenario.jhlog")
	writeDatabaseScenarioLog(t, logPath)

	summary, err := analyze.InspectFilesWithOptions("database-e2e", []string{logPath}, analyze.Options{})
	if err != nil {
		t.Fatalf("InspectFilesWithOptions() error = %v", err)
	}
	analysis := summary.DatabaseAnalysis
	if analysis == nil || analysis.Overall.Calls != 7 || analysis.Main.Calls != 7 {
		t.Fatalf("database analysis = %+v", analysis)
	}
	if len(analysis.Scenarios.Candidates) == 0 || analysis.Scenarios.Candidates[0].MaxCallsPerScope != 6 {
		t.Fatalf("database scenarios = %+v", analysis.Scenarios)
	}
	if analysis.Transactions == nil || analysis.Transactions.Completed != 1 || analysis.Transactions.MaxStatementCount != 1 {
		t.Fatalf("database transactions = %+v", analysis.Transactions)
	}
	if analysis.Telemetry.PhaseMeasuredCalls != 6 || analysis.Telemetry.TransactionLinkedCalls != 1 {
		t.Fatalf("database telemetry = %+v", analysis.Telemetry)
	}

	reportPath := filepath.Join(tempDir, "database-scenario.html")
	if err := WriteInspectWithOptions(reportPath, summary, ReportOptions{GeneratedAt: "2026-08-27T12:00:00+03:00"}); err != nil {
		t.Fatalf("WriteInspectWithOptions() error = %v", err)
	}
	raw, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	html := string(raw)
	for _, expected := range []string{
		`class="database-problem-list"`,
		`<pre class="database-sql-template"><code>SELECT label FROM sample_database_record WHERE id = ?`,
		"Сценарные гипотезы",
		"возможный N&#43;1 или повторный запрос",
		"Проблемные транзакции",
		"SampleStore.replace",
		"выполнение<strong>6",
		"чтение результата<strong>6",
	} {
		if !strings.Contains(html, expected) {
			t.Fatalf("database E2E report does not contain %q", expected)
		}
	}
}

func writeDatabaseScenarioLog(t *testing.T, path string) {
	t.Helper()
	closer, writer, err := jhlog.Create(path)
	if err != nil {
		t.Fatalf("jhlog.Create() error = %v", err)
	}
	events := databaseScenarioEvents()
	for index := range events {
		if err := writer.WriteEvent(events[index]); err != nil {
			t.Fatalf("WriteEvent(%d) error = %v", index, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Writer.Close() error = %v", err)
	}
	if err := closer.Close(); err != nil {
		t.Fatalf("log Close() error = %v", err)
	}
}

func databaseScenarioEvents() []jhlog.Event {
	events := []jhlog.Event{
		dictionaryEvent(jhlog.DictGeneric, 1, "SELECT label FROM sample_database_record WHERE id = ?"),
		dictionaryEvent(jhlog.DictScreen, 2, "sample.compose.database"),
		dictionaryEvent(jhlog.DictOwner, 3, "SampleDatabaseDao.load"),
		dictionaryEvent(jhlog.DictOperation, 4, "sample.auto.database.repeated_reads"),
		dictionaryEvent(jhlog.DictGeneric, 5, "DELETE FROM sample_database_record"),
		dictionaryEvent(jhlog.DictStableSymbol, 0x1001, "SampleDatabaseDao.load"),
		dictionaryEvent(jhlog.DictStableSymbol, 0x1002, "SampleStore.replace"),
		{
			Type:   jhlog.EventSession,
			TimeUS: 1,
			Session: &jhlog.SessionEvent{
				SDKInt: 35, CollectorFlags: uint64(jhlog.CollectorDatabase), ProcessName: "sample",
			},
		},
		{
			Type:        jhlog.EventOperation,
			TimeUS:      1_000_000,
			Attribution: databaseScenarioAttribution(),
			Operation: &jhlog.OperationEvent{
				NameRef: jhlog.LocalSymbol(4), ID: 42, Phase: jhlog.OperationPhaseStarted,
				Kind: jhlog.OperationKindUser,
			},
		},
	}
	for index := uint64(0); index < 6; index++ {
		events = append(events, jhlog.Event{
			Type: jhlog.EventDatabase, TimeUS: 1_020_000 + index*21_000,
			Flags: uint64(jhlog.FlagThreadMain), Attribution: databaseScenarioAttribution(),
			Database: &jhlog.DatabaseEvent{
				QueryRef: jhlog.LocalSymbol(1), SourceRef: jhlog.StableSymbol(0x1001),
				StatementFingerprint: 0x9a13, Framework: jhlog.DatabaseFrameworkRoom,
				Operation: jhlog.DatabaseOperationQuery, Outcome: jhlog.DatabaseOutcomeSuccess,
				Boundary: jhlog.DatabaseBoundaryMaterialize, ResultKnown: true,
				ResultKind: jhlog.DatabaseResultRows, ResultCountBucket: jhlog.DatabaseCountOne,
				PhaseMask: jhlog.DatabasePhaseExecute | jhlog.DatabasePhaseMaterialize,
				ExecuteUS: 15_000, MaterializeUS: 5_000, DurationUS: 20_000,
			},
		})
	}
	events = append(events,
		jhlog.Event{
			Type: jhlog.EventOperation, TimeUS: 1_160_000,
			Attribution: databaseScenarioAttribution(),
			Operation: &jhlog.OperationEvent{
				NameRef: jhlog.LocalSymbol(4), ID: 42, Phase: jhlog.OperationPhaseFinished,
				Kind: jhlog.OperationKindUser, Outcome: jhlog.OperationOutcomeSuccess, DurationUS: 160_000,
			},
		},
		jhlog.Event{
			Type: jhlog.EventDatabaseTransaction, TimeUS: 2_000_000,
			Flags: uint64(jhlog.FlagThreadMain), Attribution: databaseScenarioAttribution(),
			DatabaseTransaction: &jhlog.DatabaseTransactionEvent{
				SourceRef: jhlog.StableSymbol(0x1002), TransactionID: 99,
				Stage: jhlog.DatabaseTransactionBegin, Mode: jhlog.DatabaseTransactionImmediate,
			},
		},
		jhlog.Event{
			Type: jhlog.EventDatabase, TimeUS: 2_020_000,
			Flags: uint64(jhlog.FlagThreadMain), Attribution: databaseScenarioAttribution(),
			Database: &jhlog.DatabaseEvent{
				QueryRef: jhlog.LocalSymbol(5), SourceRef: jhlog.StableSymbol(0x1002),
				StatementFingerprint: 0x7b22, Framework: jhlog.DatabaseFrameworkSupportSQLite,
				Operation: jhlog.DatabaseOperationDelete, Outcome: jhlog.DatabaseOutcomeSuccess,
				Boundary: jhlog.DatabaseBoundaryExecute, TransactionID: 99, DurationUS: 20_000,
			},
		},
		jhlog.Event{
			Type: jhlog.EventDatabaseTransaction, TimeUS: 2_080_000,
			Flags: uint64(jhlog.FlagThreadMain), Attribution: databaseScenarioAttribution(),
			DatabaseTransaction: &jhlog.DatabaseTransactionEvent{
				SourceRef: jhlog.StableSymbol(0x1002), TransactionID: 99,
				Stage: jhlog.DatabaseTransactionTerminal, Outcome: jhlog.DatabaseTransactionSuccess,
				DurationUS: 80_000, StatementCount: 1, WriteCount: 1,
			},
		},
	)
	return events
}

func databaseScenarioAttribution() jhlog.AttributionContext {
	return jhlog.AttributionContext{
		Present: true, Screen: jhlog.LocalSymbol(2), Owner: jhlog.LocalSymbol(3), OperationID: 42,
	}
}

func dictionaryEvent(kind jhlog.DictKind, id uint64, value string) jhlog.Event {
	return jhlog.Event{
		Type:       jhlog.EventDictionary,
		Dictionary: &jhlog.DictionaryEntry{Kind: kind, ID: id, Value: value},
	}
}
