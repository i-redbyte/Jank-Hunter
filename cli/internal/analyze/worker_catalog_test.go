package analyze

import (
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
	"testing"
)

func TestWorkerInventoryDoesNotRequireExecutionAndPreservesMissingCatalog(t *testing.T) {
	events := []jhlog.Event{{Type: jhlog.EventWorkerCatalog, TimeMS: 10, DiagnosticEvent: &jhlog.DiagnosticEvent{WorkerCatalog: &jhlog.WorkerCatalogEvent{Schema: 1, Status: "available", Classes: []string{"app.NeverRun"}}}}}
	summary := inspectLogsForTest("catalog", []jhlog.Log{{Events: events}})
	if summary.WorkerCatalog == nil || len(summary.WorkerCatalog.Observations) != 1 || summary.WorkerCatalog.Observations[0].Catalog.Classes[0] != "app.NeverRun" {
		t.Fatalf("lost catalog: %+v", summary.WorkerCatalog)
	}
	if summary.WorkerAnalysis != nil {
		t.Fatal("class inventory invented execution")
	}
	if inspectLogsForTest("old", []jhlog.Log{{}}).WorkerCatalog != nil {
		t.Fatal("old log invented empty inventory")
	}
}

func TestWorkerInventoryHistoryBoundReportsOmittedDocuments(t *testing.T) {
	events := make([]jhlog.Event, 33)
	for i := range events {
		events[i] = jhlog.Event{Type: jhlog.EventWorkerCatalog, DiagnosticEvent: &jhlog.DiagnosticEvent{WorkerCatalog: &jhlog.WorkerCatalogEvent{Schema: 1, Status: "missing"}}}
	}
	result := inspectLogsForTest("catalog", []jhlog.Log{{Events: events}}).WorkerCatalog
	if result == nil || len(result.Observations) != 32 || result.DroppedDocuments != 1 {
		t.Fatalf("unbounded history: %+v", result)
	}
}

func TestWorkerInventoryReleasedWithCollectorState(t *testing.T) {
	state := workerCollectorState{workerCatalog: &WorkerCatalogAnalysis{}}
	state.release()
	if state.workerCatalog != nil {
		t.Fatal("retained catalog after collector release")
	}
}
