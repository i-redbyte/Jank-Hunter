package mathanalysis

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func BenchmarkHTTPCountCollectionPipeline(b *testing.B) {
	for _, owners := range []int{1, 16} {
		b.Run(fmt.Sprintf("owners_%d_20k", owners), func(b *testing.B) {
			path := filepath.Join(b.TempDir(), "http.jhlog")
			header := jhlog.DefaultSegmentHeader()
			header.RunID = jhlog.ID128{1}
			header.ProcessInstanceID = jhlog.ID128{1}
			header.SessionID = jhlog.ID128{1}
			modern := header.RequiredFeatures&(1<<30) != 0
			file, writer, err := jhlog.CreateWithHeader(path, header)
			if err != nil {
				b.Fatal(err)
			}
			for i := 0; i < owners+32; i++ {
				entry := jhlog.DictionaryEntry{Kind: jhlog.DictOwner, ID: uint64(i + 1), Value: fmt.Sprintf("Owner%d", i)}
				if i >= owners {
					entry.Kind, entry.Value = jhlog.DictRoute, fmt.Sprintf("GET /route%d", i-owners)
				}
				if err := writer.WriteEvent(jhlog.Event{Type: jhlog.EventDictionary, Dictionary: &entry}); err != nil {
					b.Fatal(err)
				}
			}
			flags := uint64(0)
			if modern {
				flags = 1 << 11
			}
			if err := writer.WriteEvent(jhlog.Event{Type: jhlog.EventSession, TimeMS: 1, Session: &jhlog.SessionEvent{CollectorFlags: flags}}); err != nil {
				b.Fatal(err)
			}
			for i := 0; i < 20000; i++ {
				event := jhlog.Event{Type: jhlog.EventHTTP, TimeMS: uint64(i*2 + 1), Attribution: jhlog.AttributionContext{Present: true, Owner: jhlog.LocalSymbol(uint64((i/32)%owners + 1))}, HTTP: &jhlog.HTTPEvent{RouteRef: jhlog.LocalSymbol(uint64(owners + i%32 + 1)), DurationMS: 100, Status: jhlog.Status2xx}}
				if i%16 == 0 {
					event.HTTP.DNSMS, event.HTTP.ConnectMS = 5, 7
				}
				if err := writer.WriteEvent(event); err != nil {
					b.Fatal(err)
				}
			}
			counters := map[uint64]uint64{}
			if modern {
				counters[0x204b] = 1
				counters[0x204c] = 40001
			}
			writer.SetQualitySnapshot(jhlog.QualitySnapshot{CapturedElapsedUS: 40001000, Counters: counters})
			if err := writer.Close(); err != nil {
				b.Fatal(err)
			}
			if err := file.Close(); err != nil {
				b.Fatal(err)
			}
			paths := []string{path}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				summary, err := analyze.InspectFilesWithOptions("benchmark", paths, analyze.Options{})
				if err != nil {
					b.Fatal(err)
				}
				report, err := AnalyzeInspectWithSummary(paths, analyze.Options{}, summary)
				if err != nil || report.Summary.HTTPCount != 20000 || len(report.Timeline) != 40 || len(report.CollectionLimits) != 0 {
					b.Fatalf("incomplete pipeline: %v %+v", err, report.CollectionLimits)
				}
			}
		})
	}
}
