package analyze

import (
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func TestLegacyLifecyclePartialCoverageWithoutRetainedTargets(t *testing.T) {
	const metric = "jankhunter.lifecycle.coverage.legacy_partial.count"
	for _, count := range []uint64{0, 1, 7} {
		log := jhlog.Log{Dict: map[uint64]string{1: metric}, Events: []jhlog.Event{
			{Type: jhlog.EventCounter, Metric: &jhlog.MetricEvent{MetricRef: jhlog.LocalSymbol(1), Value: count}},
		}}
		summary := inspectLogsForTest("legacy", []jhlog.Log{log})
		warnings := strings.Join(summary.Warnings, " ")
		partial := strings.Contains(warnings, "lifecycle/binding-покрытие частичное")
		if partial != (count > 0) {
			t.Fatalf("count=%d partial=%v warnings=%s", count, partial, warnings)
		}
		if count > 0 && !strings.Contains(warnings, "обновите Gradle plugin") {
			t.Fatal(warnings)
		}
		if strings.Contains(warnings, "покрытие полное") {
			t.Fatal("absence is not completeness")
		}
	}
}
