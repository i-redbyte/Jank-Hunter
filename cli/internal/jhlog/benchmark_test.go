package jhlog_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/benchfixture"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func BenchmarkStreamFileRepresentative(b *testing.B) {
	path, metadata := writeRepresentativeFixture(b)
	stat, err := os.Stat(path)
	if err != nil {
		b.Fatal(err)
	}
	b.SetBytes(stat.Size())
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		deliveredRecords := 0
		if err := jhlog.StreamFile(path, func(jhlog.Event, map[uint64]string) error {
			deliveredRecords++
			return nil
		}); err != nil {
			b.Fatal(err)
		}
		wantDelivered := metadata.Events + metadata.DictionaryRecords
		if deliveredRecords != wantDelivered {
			b.Fatalf("delivered records = %d, want %d", deliveredRecords, wantDelivered)
		}
	}
	b.ReportMetric(float64(metadata.Events), "events/op")
}

func BenchmarkProfileFileRepresentative(b *testing.B) {
	path, metadata := writeRepresentativeFixture(b)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		profile, _, _, err := jhlog.ProfileFile(path)
		if err != nil {
			b.Fatal(err)
		}
		if profile.Events != uint64(metadata.Events) {
			b.Fatalf("profiled events = %d, want %d", profile.Events, metadata.Events)
		}
		if profile.Records != uint64(metadata.TotalRecords) ||
			profile.DataRecords != uint64(metadata.DataRecords) ||
			profile.Dictionary != uint64(metadata.DictionaryRecords) ||
			profile.Control != uint64(metadata.ControlRecords) {
			b.Fatalf(
				"profiled records = total:%d data:%d dictionary:%d control:%d, want %d/%d/%d/%d",
				profile.Records,
				profile.DataRecords,
				profile.Dictionary,
				profile.Control,
				metadata.TotalRecords,
				metadata.DataRecords,
				metadata.DictionaryRecords,
				metadata.ControlRecords,
			)
		}
	}
	b.ReportMetric(float64(metadata.Events), "events/op")
}

func writeRepresentativeFixture(b *testing.B) (string, benchfixture.Metadata) {
	b.Helper()
	profile, err := benchfixture.ProfileByName("representative")
	if err != nil {
		b.Fatal(err)
	}
	path := filepath.Join(b.TempDir(), "representative.jhlog")
	metadata, err := benchfixture.Write(path, profile)
	if err != nil {
		b.Fatal(err)
	}
	return path, metadata
}
