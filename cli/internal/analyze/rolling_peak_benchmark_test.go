package analyze

import (
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

var rollingPeakBenchmarkSink uint64

func BenchmarkRollingPeakStreams(b *testing.B) {
	for _, name := range []string{"http_ordered", "http_reversed", "io_same_ms", "db_dense"} {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for iteration := 0; iteration < b.N; iteration++ {
				switch name {
				case "io_same_ms":
					var burst ioBurstAccumulator
					for i := 0; i < 100000; i++ {
						burst.add(1, 1000)
					}
					rollingPeakBenchmarkSink = burst.peak
				case "db_dense":
					var burst routeBurstAccumulator
					for i := uint64(0); i < 100000; i++ {
						burst.add(1, i/3)
					}
					rollingPeakBenchmarkSink = burst.peak
				default:
					log := jhlog.Log{Dict: map[uint64]string{1: "GET /peak"}, Events: make([]jhlog.Event, 20000)}
					for i := range log.Events {
						at := uint64(i + 1000)
						if name == "http_reversed" {
							at = uint64(21000 - i)
						}
						log.Events[i] = jhlog.Event{Type: jhlog.EventHTTP, TimeMS: at, HTTP: &jhlog.HTTPEvent{RouteRef: jhlog.LocalSymbol(1), DurationMS: 100, StatusCode: 200}}
					}
					summary := inspectLogsForTest("peak benchmark", []jhlog.Log{log})
					rollingPeakBenchmarkSink = summary.Routes[0].PeakRequestsPerSecond
				}
			}
		})
	}
}
