package analyze

import (
	"fmt"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
	"strings"
	"testing"
)

func BenchmarkWorkRegistryLongSession(b *testing.B) {
	names := make([]string, 128)
	for i := range names {
		names[i] = fmt.Sprintf("00000000-0000-0000-0000-%012d", i)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		c := newCollector("registry benchmark", 1, Options{})
		c.startLog(jhlog.DefaultSegmentHeader())
		for snapshot := uint64(1); snapshot <= 1000; snapshot++ {
			for packet := uint64(0); packet < 16; packet++ {
				rows := make([]jhlog.WorkRegistryJob, 8)
				for row := range rows {
					rows[row] = jhlog.WorkRegistryJob{UUID: strings.Clone(names[int(packet)*8+row]), NetworkType: "NOT_REQUIRED"}
				}
				p := &jhlog.WorkRegistryEvent{Version: 1, SnapshotID: snapshot, QueryStartedMS: snapshot * 30000, ObservedMS: snapshot * 30000, IntervalMS: 30000,
					PacketIndex: packet, PacketCount: 16, TotalObserved: 128, RetainedJobs: 128, Jobs: rows}
				c.recordWorkRegistry(jhlog.Event{DiagnosticEvent: &jhlog.DiagnosticEvent{WorkRegistry: p}})
			}
		}
		c.finishLog()
		result := c.finish().WorkRegistry
		if result == nil || result.DroppedPackets == 0 {
			b.Fatal("benchmark did not exercise retained history limit")
		}
	}
}
