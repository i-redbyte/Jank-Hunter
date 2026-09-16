package mathanalysis

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func BenchmarkNetworkLoopContextPipeline(b *testing.B) {
	for _, mode := range []string{"observed", "mixed", "missing"} {
		b.Run(mode, func(b *testing.B) {
			path := filepath.Join(b.TempDir(), "loops.jhlog")
			f, w, err := jhlog.CreateWithHeader(path, completeHTTPTestHeader())
			if err != nil {
				b.Fatal(err)
			}
			if err := w.WriteEvent(jhlog.Event{Type: jhlog.EventSession, TimeMS: 1, Session: &jhlog.SessionEvent{CollectorFlags: uint64(jhlog.CollectorHTTP)}}); err != nil {
				b.Fatal(err)
			}
			for route := 0; route < 16; route++ {
				for column := 0; column < 4; column++ {
					kind := jhlog.DictOwner
					value := fmt.Sprintf("Owner%d_%d", route, column)
					if column == 3 {
						kind = jhlog.DictRoute
						value = fmt.Sprintf("GET /%d", route)
					}
					entry := jhlog.DictionaryEntry{Kind: kind, ID: uint64(route*4 + column + 1), Value: value}
					if err := w.WriteEvent(jhlog.Event{Type: jhlog.EventDictionary, Dictionary: &entry}); err != nil {
						b.Fatal(err)
					}
				}
			}
			count := 0
			for burst := 0; burst <= 60; burst++ {
				for route := 0; route < 16; route++ {
					width := 3
					if burst == 0 {
						width = 1
					}
					for offset := 0; offset < width; offset++ {
						owner := uint64(route*4 + 2)
						if burst == 0 {
							owner = uint64(route*4 + 1)
						} else if offset == 2 {
							if mode == "mixed" {
								owner = uint64(route*4 + 3)
							}
							if mode == "missing" {
								owner = 0
							}
						}
						event := jhlog.Event{Type: jhlog.EventHTTP, TimeMS: uint64(burst*4000 + route*10 + offset + 100), Attribution: jhlog.AttributionContext{Present: owner != 0, Owner: jhlog.LocalSymbol(owner)}, HTTP: &jhlog.HTTPEvent{RouteRef: jhlog.LocalSymbol(uint64(route*4 + 4)), DurationMS: 150, DNSMS: 5, ConnectMS: 7, Status: jhlog.Status2xx}}
						if err := w.WriteEvent(event); err != nil {
							b.Fatal(err)
						}
						count++
					}
				}
			}
			w.SetQualitySnapshot(jhlog.QualitySnapshot{CapturedElapsedUS: 241001000, Counters: map[uint64]uint64{jhlog.QualityCollectionWindowStartElapsedMS: 1, jhlog.QualityCollectionWindowEndElapsedMS: 241001}})
			if err := w.Close(); err != nil {
				b.Fatal(err)
			}
			if err := f.Close(); err != nil {
				b.Fatal(err)
			}
			paths := []string{path}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				summary, err := analyze.InspectFilesWithOptions("loop", paths, analyze.Options{})
				if err != nil {
					b.Fatal(err)
				}
				result, err := AnalyzeInspectWithSummary(paths, analyze.Options{}, summary)
				if err != nil || summary.HTTPCount != count || len(result.NetworkLoops) == 0 || len(result.CollectionLimits) != 0 {
					b.Fatalf("incomplete benchmark: %v limits%+v", err, result.CollectionLimits)
				}
			}
		})
	}
}
