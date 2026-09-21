package mathanalysis

import (
	"encoding/json"
	"fmt"
	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
	"path/filepath"
	"testing"
)

func BenchmarkTrafficCollection(b *testing.B) {
	for _, tc := range []struct {
		name      string
		processes int
		known     bool
	}{{"single_20k", 1, true}, {"shared_uid_4x20k", 4, true}, {"legacy_4x20k", 4, false}} {
		b.Run(tc.name, func(b *testing.B) {
			var paths []string
			for process := 1; process <= tc.processes; process++ {
				header := jhlog.DefaultSegmentHeader()
				header.RunID = jhlog.ID128{1}
				header.ProcessInstanceID = jhlog.ID128{byte(process)}
				header.SessionID = jhlog.ID128{byte(process)}
				path := filepath.Join(b.TempDir(), fmt.Sprintf("traffic-%d.jhlog", process))
				file, writer, err := jhlog.CreateWithHeader(path, header)
				if err != nil {
					b.Fatal(err)
				}
				for i := 0; i < 20000; i++ {
					payload := jhlog.ContextEvent{RxBytes: uint64(i*100 + process), TxBytes: uint64(i*10 + process)}
					if tc.known {
						if err := json.Unmarshal([]byte(`{"traffic_uid_plus_one":10001,"traffic_known_flags":3}`), &payload); err != nil {
							b.Fatal(err)
						}
					}
					if err := writer.WriteEvent(jhlog.Event{Type: jhlog.EventContext, TimeMS: uint64(i * 10), Context: &payload}); err != nil {
						b.Fatal(err)
					}
				}
				if err := writer.Close(); err != nil {
					b.Fatal(err)
				}
				if err := file.Close(); err != nil {
					b.Fatal(err)
				}
				paths = append(paths, path)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				summary, err := analyze.InspectFilesWithOptions(tc.name, paths, analyze.Options{})
				if err != nil {
					b.Fatal(err)
				}
				result, err := analyzeMathInputs(paths, analyze.Options{})
				if err != nil {
					b.Fatal(err)
				}
				if summary.ContextCount != tc.processes*20000 || len(result.Timeline) == 0 {
					b.Fatal("incomplete benchmark result")
				}
			}
		})
	}
}
