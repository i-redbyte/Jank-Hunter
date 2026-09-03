package analyze

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

func TestAggregationAndComparisonStaySeparated(t *testing.T) {
	assertSourceLineLimit(t, "aggregate.go", 1_100)
	file, err := parser.ParseFile(token.NewFileSet(), "comparison.go", nil, 0)
	if err != nil {
		t.Fatalf("parse comparison.go: %v", err)
	}
	found := false
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && function.Name.Name == "Compare" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("comparison.go does not own Compare")
	}
	assertFileOwnsFunction(t, "collector_finalize.go", "finish")
	assertFileOwnsFunction(t, "collector_quality.go", "finalizeCollectionQuality")
	assertFileOwnsFunction(t, "collector_stats.go", "networkCallStats")
	assertFileOwnsFunction(t, "collector_events.go", "add")
}

func TestCollectorFinalizationHasQualityOwners(t *testing.T) {
	assertSourceLineLimit(t, "collector_finalize.go", 400)
	assertSourceLineLimit(t, "collector_quality.go", 550)
	assertFileOwnsFunction(t, "collector_inputs.go", "analysisInputCompleteness")
	assertFileOwnsFunction(t, "collector_quality.go", "finalizeCollectionQuality")
	assertFileOwnsFunction(t, "collector_completeness.go", "collectionDiagnosticCompleteness")
	assertFileOwnsFunction(t, "collector_retention_quality.go", "retentionDataQuality")
	assertFileOwnsFunction(t, "collector_diagnostics.go", "instrumentationQualityWarnings")
}

func TestProblemAnalysisHasDomainOwners(t *testing.T) {
	assertSourceLineLimit(t, "problems.go", 500)
	owners := map[string]string{
		"problem_operations.go": "detectOperations",
		"problem_network.go":    "detectNetwork",
		"problem_ui.go":         "detectUI",
		"problem_database.go":   "detectDatabaseCalls",
		"problem_io.go":         "detectCriticalIO",
		"problem_system.go":     "detectMemory",
		"problem_finalize.go":   "finishFindings",
		"problem_helpers.go":    "problemConfidence",
	}
	for path, function := range owners {
		assertFileOwnsFunction(t, path, function)
	}
}

func TestHeapAnalysisHasParserAndGraphOwners(t *testing.T) {
	assertSourceLineLimit(t, "heap.go", 400)
	owners := map[string]string{
		"heap_parser.go":           "parse",
		"heap_dump_parser.go":      "parseHeapDump",
		"heap_evidence.go":         "evidence",
		"heap_graph.go":            "rootBFS",
		"heap_evidence_helpers.go": "betterHeapLeak",
		"heap_reader.go":           "remaining",
	}
	for path, function := range owners {
		assertFileOwnsFunction(t, path, function)
	}
}

func TestOperationAnalysisHasLifecycleAndAlgorithmOwners(t *testing.T) {
	assertSourceLineLimit(t, "operation_analysis.go", 140)
	owners := map[string]string{
		"operation_store.go":     "operationInstanceHash",
		"operation_quantiles.go": "percentile",
		"operation_lifecycle.go": "recordLifecycle",
		"operation_signals.go":   "recordSignal",
		"operation_finalize.go":  "finalize",
		"operation_incidents.go": "retainIncident",
		"operation_time.go":      "operationSlot",
		"operation_compare.go":   "compareOperationAnalysis",
	}
	for path, function := range owners {
		assertFileOwnsFunction(t, path, function)
	}
}

func assertFileOwnsFunction(t *testing.T, path, name string) {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && function.Name.Name == name {
			return
		}
	}
	t.Fatalf("%s does not own %s", path, name)
}

func assertSourceLineLimit(t *testing.T, path string, limit int) {
	t.Helper()
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	lines := 1 + strings.Count(string(payload), "\n")
	if lines > limit {
		t.Fatalf("%s has %d lines, want <= %d", path, lines, limit)
	}
}
