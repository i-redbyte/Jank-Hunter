package analyze_test

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/benchfixture"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

var benchmarkSummary analyze.Summary

func BenchmarkInspectRepresentative(b *testing.B) {
	profile, err := benchfixture.ProfileByName("representative")
	if err != nil {
		b.Fatal(err)
	}
	path := filepath.Join(b.TempDir(), "representative.jhlog")
	metadata, err := benchfixture.Write(path, profile)
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		summary, err := analyze.InspectFilesWithOptions("benchmark", []string{path}, analyze.Options{})
		if err != nil {
			b.Fatal(err)
		}
		if summary.EventCount != metadata.Events {
			b.Fatalf("analyzed events = %d, want %d", summary.EventCount, metadata.Events)
		}
		if summary.TotalRecordCount != uint64(metadata.TotalRecords) ||
			summary.DataRecordCount != uint64(metadata.DataRecords) ||
			summary.DictionaryRecords != uint64(metadata.DictionaryRecords) ||
			summary.ControlRecords != uint64(metadata.ControlRecords) ||
			summary.Dictionary != metadata.DictionaryEntries {
			b.Fatalf(
				"analyzed records = total:%d data:%d dictionary:%d control:%d entries:%d, want %d/%d/%d/%d/%d",
				summary.TotalRecordCount,
				summary.DataRecordCount,
				summary.DictionaryRecords,
				summary.ControlRecords,
				summary.Dictionary,
				metadata.TotalRecords,
				metadata.DataRecords,
				metadata.DictionaryRecords,
				metadata.ControlRecords,
				metadata.DictionaryEntries,
			)
		}
		benchmarkSummary = summary
	}
	b.ReportMetric(float64(metadata.Events), "events/op")
}

func BenchmarkInspectDatabaseMillionEvents(b *testing.B) {
	path := filepath.Join(b.TempDir(), "database-million.jhlog")
	writeDatabaseMillionBenchmarkLog(b, path)

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		summary, err := analyze.InspectFilesWithOptions("database-million", []string{path}, analyze.Options{})
		if err != nil {
			b.Fatal(err)
		}
		analysis := summary.DatabaseAnalysis
		if analysis == nil || analysis.Overall.Calls != databaseBenchmarkEventCount {
			b.Fatalf("database calls = %+v, want %d", analysis, databaseBenchmarkEventCount)
		}
		if len(analysis.Statements) > databaseBenchmarkCardinality {
			b.Fatalf("retained database statements = %d, want <= %d", len(analysis.Statements), databaseBenchmarkCardinality)
		}
		benchmarkSummary = summary
	}
	b.ReportMetric(databaseBenchmarkEventCount, "database-events/op")
}

func writeDatabaseMillionBenchmarkLog(b *testing.B, path string) {
	b.Helper()
	closer, writer, err := jhlog.Create(path)
	if err != nil {
		b.Fatal(err)
	}
	defer func() {
		if err := closer.Close(); err != nil {
			b.Fatal(err)
		}
	}()
	write := func(event jhlog.Event) {
		if err := writer.WriteEvent(event); err != nil {
			b.Fatal(err)
		}
	}
	write(benchmarkDictionaryEvent(jhlog.DictScreen, 1, "DatabaseBenchmark"))
	write(benchmarkDictionaryEvent(jhlog.DictOwner, 2, "DatabaseBenchmarkDao"))
	write(benchmarkDictionaryEvent(jhlog.DictOperation, 3, "database.benchmark"))
	write(benchmarkDictionaryEvent(jhlog.DictStableSymbol, databaseBenchmarkSourceID, "DatabaseBenchmarkDao.load"))
	for index := 0; index < databaseBenchmarkCardinality; index++ {
		write(benchmarkDictionaryEvent(
			jhlog.DictGeneric,
			databaseBenchmarkQueryBase+uint64(index),
			fmt.Sprintf("SELECT value_%d FROM benchmark WHERE id = ?", index),
		))
	}
	write(jhlog.Event{
		Type: jhlog.EventSession,
		Session: &jhlog.SessionEvent{
			SDKInt: 35, CollectorFlags: uint64(jhlog.CollectorDatabase), ProcessName: "benchmark",
		},
	})
	attribution := jhlog.AttributionContext{
		Present: true, Screen: jhlog.LocalSymbol(1), Owner: jhlog.LocalSymbol(2), OperationID: 42,
	}
	write(jhlog.Event{
		Type: jhlog.EventOperation, TimeUS: 1_000,
		Attribution: attribution,
		Operation: &jhlog.OperationEvent{
			NameRef: jhlog.LocalSymbol(3), ID: 42, Phase: jhlog.OperationPhaseStarted,
			Kind: jhlog.OperationKindUser,
		},
	})
	uiBuckets := make([]uint64, jhlog.UIFrameHistogramBucketCount)
	uiBuckets[len(uiBuckets)-1] = 1
	for index := uint64(0); index < databaseBenchmarkEventCount; index++ {
		duration := uint64(500)
		if index%1_000 == 0 {
			duration = 20_000
		}
		write(jhlog.Event{
			Type: jhlog.EventDatabase, TimeUS: 2_000 + index*1_000,
			Flags: uint64(index&1) * uint64(jhlog.FlagThreadMain), Attribution: attribution,
			Database: &jhlog.DatabaseEvent{
				QueryRef:             jhlog.LocalSymbol(databaseBenchmarkQueryBase + index%databaseBenchmarkCardinality),
				SourceRef:            jhlog.StableSymbol(databaseBenchmarkSourceID),
				StatementFingerprint: index%databaseBenchmarkCardinality + 1,
				Framework:            jhlog.DatabaseFrameworkRoom,
				Operation:            jhlog.DatabaseOperationQuery,
				Outcome:              jhlog.DatabaseOutcomeSuccess,
				Boundary:             jhlog.DatabaseBoundaryMaterialize,
				DurationUS:           duration,
			},
		})
		if index < databaseBenchmarkIntervalCount {
			write(jhlog.Event{
				Type: jhlog.EventUIWindow, TimeUS: 2_000 + index*1_000,
				Flags: uint64(jhlog.FlagUIProblem), Attribution: attribution,
				UIWindow: &jhlog.UIWindowEvent{
					WindowMS: 16, FrameCount: 1, JankCount: 1,
					Source: jhlog.UIFrameSourceJankStats, FrameDeadlineUS: 16_667,
					FrameDurationBuckets: uiBuckets,
				},
			})
		}
	}
	write(jhlog.Event{
		Type: jhlog.EventOperation, TimeUS: 2_000 + databaseBenchmarkEventCount*1_000,
		Attribution: attribution,
		Operation: &jhlog.OperationEvent{
			NameRef: jhlog.LocalSymbol(3), ID: 42, Phase: jhlog.OperationPhaseFinished,
			Kind: jhlog.OperationKindUser, Outcome: jhlog.OperationOutcomeSuccess,
			DurationUS: databaseBenchmarkEventCount * 1_000,
		},
	})
	if err := writer.Close(); err != nil {
		b.Fatal(err)
	}
}

func benchmarkDictionaryEvent(kind jhlog.DictKind, id uint64, value string) jhlog.Event {
	return jhlog.Event{
		Type:       jhlog.EventDictionary,
		Dictionary: &jhlog.DictionaryEntry{Kind: kind, ID: id, Value: value},
	}
}

const (
	databaseBenchmarkEventCount    = 1_000_000
	databaseBenchmarkCardinality   = 4_096
	databaseBenchmarkIntervalCount = 4_096
	databaseBenchmarkQueryBase     = 100
	databaseBenchmarkSourceID      = 0x10_001
)
