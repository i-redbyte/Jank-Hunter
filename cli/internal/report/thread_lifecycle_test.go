package report

import (
	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
	"path/filepath"
	"strings"
	"testing"
)

func TestThreadPointReportDoesNotInventLiveThreadCount(t *testing.T) {
	path := filepath.Join(t.TempDir(), "points.html")
	summary := analyze.Summary{ThreadAnalysis: &analyze.ThreadAnalysis{LifecycleObserved: true, RunObservations: 1, Lifecycle: []analyze.ThreadLifecycleObservation{{Observation: &jhlog.ThreadLifecycleEvent{Stage: jhlog.ThreadRunObserved, Name: "short"}}}}}
	if err := WriteInspectWithOptions(path, summary, ReportOptions{}); err != nil {
		t.Fatal(err)
	}
	assertHTMLContains(t, path, "Точечные наблюдения потоков", "Системных снимков нет; число живых потоков неизвестно", "data-thread-event-rows")
}
func TestThreadPointComparisonKeepsAbsentCoverageUnknown(t *testing.T) {
	if !strings.Contains(threadLifecycleCounts(&analyze.ThreadAnalysis{}), "неизвестно") {
		t.Fatal("old snapshot treated as zero point observations")
	}
	text := threadLifecycleCounts(&analyze.ThreadAnalysis{LifecycleObserved: true, RunObservations: 2, StartReturns: 3, FactoryReturns: 4, DroppedLifecycle: 1})
	for _, want := range []string{"run 2", "start 3", "factory 4", "1"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q: %s", want, text)
		}
	}
}
