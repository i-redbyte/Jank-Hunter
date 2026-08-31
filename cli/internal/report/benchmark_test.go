package report_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/benchfixture"
	"github.com/i-redbyte/jank-hunter/cli/internal/report"
)

func BenchmarkWriteInspectRepresentative(b *testing.B) {
	profile, err := benchfixture.ProfileByName("representative")
	if err != nil {
		b.Fatal(err)
	}
	directory := b.TempDir()
	logPath := filepath.Join(directory, "representative.jhlog")
	if _, err := benchfixture.Write(logPath, profile); err != nil {
		b.Fatal(err)
	}
	summary, err := analyze.InspectFilesWithOptions("benchmark", []string{logPath}, analyze.Options{})
	if err != nil {
		b.Fatal(err)
	}
	reportPath := filepath.Join(directory, "inspect.html")

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := report.WriteInspectWithOptions(reportPath, summary, report.ReportOptions{}); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	stat, err := os.Stat(reportPath)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportMetric(float64(stat.Size()), "html-bytes/op")
}

func BenchmarkWriteOperationTablesHighCardinality(b *testing.B) {
	const (
		operationRows = 2_048
		timeSlotRows  = 8_192
		dimensionRows = 8_192
		stageRows     = 4_096
	)
	analysis := &analyze.OperationAnalysis{
		Completed:  100_000,
		Operations: make([]analyze.OperationStats, operationRows),
		TimeSlots:  make([]analyze.OperationTimeSlot, timeSlotRows),
		Dimensions: make([]analyze.OperationDimensionStats, dimensionRows),
		Stages:     make([]analyze.OperationStageStats, stageRows),
	}
	for index := range analysis.Operations {
		analysis.Operations[index] = analyze.OperationStats{
			Operation: fmt.Sprintf("operation.%04d", index), Kind: "background", Screen: "BenchmarkScreen",
			Count: 100, P50MS: 200, P90MS: 400, P95MS: 600, MaxMS: 1_000,
		}
	}
	for index := range analysis.TimeSlots {
		analysis.TimeSlots[index] = analyze.OperationTimeSlot{
			Label:     fmt.Sprintf("2026-08-%02d %02d:00 (UTC+03:00)", index%28+1, index%24),
			Operation: fmt.Sprintf("operation.%04d", index%operationRows), Kind: "background",
			Screen: "BenchmarkScreen", Count: 20, P50MS: 200, P90MS: 400, P95MS: 600, MaxMS: 1_000,
		}
	}
	for index := range analysis.Dimensions {
		analysis.Dimensions[index] = analyze.OperationDimensionStats{
			Operation: fmt.Sprintf("operation.%04d", index%operationRows), Kind: "background",
			Screen: "BenchmarkScreen", Key: "source", Value: fmt.Sprintf("group-%04d", index),
			Count: 20, P90MS: 400, P95MS: 600, MaxMS: 1_000,
		}
	}
	for index := range analysis.Stages {
		analysis.Stages[index] = analyze.OperationStageStats{
			ParentOperation: fmt.Sprintf("operation.%04d", index%operationRows), ParentKind: "background",
			Screen: "BenchmarkScreen", Stage: fmt.Sprintf("stage.%04d", index),
			Count: 20, P50MS: 100, P90MS: 200, P95MS: 300, MaxMS: 500,
		}
	}

	directory := b.TempDir()
	reportPath := filepath.Join(directory, "operations.html")
	summary := analyze.Summary{Title: "operation benchmark", OperationAnalysis: analysis}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := report.WriteInspectWithOptions(reportPath, summary, report.ReportOptions{}); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	stat, err := os.Stat(reportPath)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportMetric(float64(stat.Size()), "html-bytes/op")
}

func BenchmarkWriteDatabaseHighCardinality(b *testing.B) {
	const statementCount = 4_096
	statements := make([]analyze.DatabaseStatementStats, statementCount)
	for index := range statements {
		query := fmt.Sprintf(
			"SELECT value_%04d FROM benchmark_record WHERE group_key = ? ORDER BY sequence DESC",
			index,
		)
		statistics := analyze.DatabaseExecutionStats{
			Calls: 100, P50DurationUS: 1_000, P95DurationUS: 20_000,
			MaxDurationUS: 50_000, TotalDurationUS: 500_000,
		}
		statements[index] = analyze.DatabaseStatementStats{
			Query: query, Operation: "чтение", OperationCode: "query",
			StatementFingerprint: uint64(index + 1), Overall: statistics,
			Main: statistics, PeakCallsPerSecond: 20, RapidRepeats: 10,
			Contexts: []analyze.DatabaseStatementContextStats{{
				Source: fmt.Sprintf("BenchmarkDao.load%04d", index), Framework: "Room",
				Screen: "DatabaseBenchmark", ContextOperation: "database.benchmark",
				Overall: statistics, Main: statistics,
			}},
		}
	}
	analysis := &analyze.DatabaseAnalysis{
		KnownSQLCalls: statementCount * 100,
		Overall: analyze.DatabaseExecutionStats{
			Calls: statementCount * 100, P50DurationUS: 1_000, P95DurationUS: 20_000,
			MaxDurationUS: 50_000, TotalDurationUS: statementCount * 500_000,
		},
		Main: analyze.DatabaseExecutionStats{
			Calls: statementCount * 100, P50DurationUS: 1_000, P95DurationUS: 20_000,
			MaxDurationUS: 50_000, TotalDurationUS: statementCount * 500_000,
		},
		PeakCallsPerSecond: 20,
		RapidRepeats:       statementCount * 10,
		Statements:         statements,
	}
	summary := analyze.Summary{
		Title: "database high-cardinality benchmark", DatabaseAnalysis: analysis,
		DatabaseCoverage: analyze.DatabaseCoverage{
			Status: "observed", StatusLabel: "данные собраны",
			ObservedCalls: statementCount * 100, KnownSQLCalls: statementCount * 100,
		},
	}
	directory := b.TempDir()
	reportPath := filepath.Join(directory, "database-high-cardinality.html")

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := report.WriteInspectWithOptions(reportPath, summary, report.ReportOptions{}); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	stat, err := os.Stat(reportPath)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportMetric(float64(stat.Size()), "html-bytes/op")
}

func BenchmarkWriteInfluenceLargeGraph(b *testing.B) {
	influence := buildLargeReportInfluence(20_000)
	directory := b.TempDir()
	reportPath := filepath.Join(directory, "large-influence.html")

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := report.WriteInfluenceWithOptions(reportPath, influence, "Большой граф влияния", report.ReportOptions{}); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	stat, err := os.Stat(reportPath)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportMetric(float64(stat.Size()), "html-bytes/op")
}
