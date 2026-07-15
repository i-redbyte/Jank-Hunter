package analyze_test

import (
	"path/filepath"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/benchfixture"
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
