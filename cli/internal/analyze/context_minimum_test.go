package analyze

import (
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func TestContextMinimumPreservesObservedZeroAcrossLogs(t *testing.T) {
	for _, samples := range [][]uint64{{0, 10}, {10, 0, 20}} {
		var logs []jhlog.Log
		for _, sample := range samples {
			logs = append(logs, jhlog.Log{Events: []jhlog.Event{{Type: jhlog.EventContext,
				Context: &jhlog.ContextEvent{BatteryPct: sample, AvailMemoryKB: sample}}}})
		}
		summary := inspectLogsForTest("zero minimum", logs)
		if summary.BatteryMinPct != 0 || summary.AvailMemoryMinKB != 0 {
			t.Fatalf("zero minimum overwritten for %v: battery=%d memory=%d", samples, summary.BatteryMinPct, summary.AvailMemoryMinKB)
		}
	}
}
