package report

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
)

func TestScheduledTimingReportSeparatesPlannedDelayAndZeroLateness(t *testing.T) {
	var executor analyze.AsyncExecutorStats
	if err := json.Unmarshal([]byte(`{"Name":"scheduled-fixture","ScheduledDelaySamples":1,"AvgScheduledDelayMS":5000,"MaxScheduledDelayMS":5000,"ScheduledLatenessSamples":3,"AvgScheduledLatenessMS":0,"MaxScheduledLatenessMS":0}`), &executor); err != nil {
		t.Fatal(err)
	}
	summary := analyze.Summary{Title: "scheduled", CollectionQuality: sampleCollectionQuality(), AsyncAnalysis: &analyze.AsyncAnalysis{Executors: []analyze.AsyncExecutorStats{executor}}}
	path := filepath.Join(t.TempDir(), "scheduled.html")
	if err := WriteInspectWithOptions(path, summary, ReportOptions{}); err != nil {
		t.Fatal(err)
	}
	assertHTMLContains(t, path, "Заданная задержка", "Опоздание запуска", "3 замеров", "Старый wait_ms может включать заданную задержку")
}
