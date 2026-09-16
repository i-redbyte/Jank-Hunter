package analyze

import (
	"encoding/json"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func TestScheduledDelayLatenessAndLegacyWaitRemainIndependentIncludingZero(t *testing.T) {
	var accumulator asyncAnalysisAccumulator
	add := func(name string, value uint64) {
		accumulator.add("executor.timer."+name, jhlog.Event{Type: jhlog.EventGauge, Metric: &jhlog.MetricEvent{Value: value, Count: 1, Sum: value, Max: value}})
	}
	add("wait_ms", 1_000)
	add("scheduled_delay_ms", 5_000)
	for _, late := range []uint64{0, 2, 7} {
		add("scheduled_lateness_ms", late)
	}
	result := accumulator.finalize()
	if result == nil || len(result.Executors) != 1 {
		t.Fatalf("missing scheduled executor: %+v", result)
	}
	executor := result.Executors[0]
	if executor.WaitSamples != 1 || executor.AvgWaitMS != 1_000 {
		t.Fatalf("new scheduling timing contaminated legacy wait: %+v", executor)
	}
	data, err := json.Marshal(executor)
	if err != nil {
		t.Fatal(err)
	}
	// Name is textual, so decode the numeric fields through a typed view.
	var decoded struct{ ScheduledDelaySamples, AvgScheduledDelayMS, MaxScheduledDelayMS, ScheduledLatenessSamples, AvgScheduledLatenessMS, MaxScheduledLatenessMS uint64 }
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.ScheduledDelaySamples != 1 || decoded.AvgScheduledDelayMS != 5_000 || decoded.MaxScheduledDelayMS != 5_000 || decoded.ScheduledLatenessSamples != 3 || decoded.AvgScheduledLatenessMS != 3 || decoded.MaxScheduledLatenessMS != 7 {
		t.Fatalf("scheduled timing missing, rounded or merged: %s", data)
	}
}

func TestConfiguredScheduledDelayAloneDoesNotDiagnoseAQueueBlock(t *testing.T) {
	dict := map[uint64]string{1: "executor.timer.scheduled_delay_ms", 2: "executor.timer.scheduled_lateness_ms"}
	metric := func(ref, value, count uint64) jhlog.Event {
		return jhlog.Event{Type: jhlog.EventGauge, TimeMS: 1_000, Metric: &jhlog.MetricEvent{MetricRef: jhlog.LocalSymbol(ref), Value: value, Count: count, Sum: value * count, Max: value}}
	}
	summary := inspectLogsForTest("scheduled", []jhlog.Log{{Dict: dict, Events: []jhlog.Event{metric(1, 60_000, 20), metric(2, 0, 20)}}})
	if summary.AsyncAnalysis == nil || len(summary.AsyncAnalysis.Executors) != 1 {
		t.Fatal("scheduled metrics were ignored")
	}
	if summary.AsyncAnalysis.Executors[0].WaitSamples != 0 {
		t.Fatal("planned delay became queue wait")
	}
	for _, problem := range summary.Problems {
		if problem.DetectorID == "cpu.async_queue" {
			t.Fatalf("planned delay became queue diagnosis: %+v", problem)
		}
	}
}

func TestScheduledMeanAcrossLargeRecordsDoesNotSaturateTheSum(t *testing.T) {
	const value = uint64(1<<63 - 1)
	var accumulator asyncAnalysisAccumulator
	for i := 0; i < 3; i++ {
		accumulator.add("executor.timer.scheduled_delay_ms", jhlog.Event{Type: jhlog.EventGauge, Metric: &jhlog.MetricEvent{Value: value, Count: 1, Sum: value, Max: value}})
	}
	data, err := json.Marshal(accumulator.finalize().Executors[0])
	if err != nil {
		t.Fatal(err)
	}
	var got struct{ AvgScheduledDelayMS uint64 }
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.AvgScheduledDelayMS != value {
		t.Fatalf("mean = %d, want %d", got.AvgScheduledDelayMS, value)
	}
}

func TestScheduledARTFixturePreservesExactMeansCountsAndCancellation(t *testing.T) {
	summary, err := InspectFilesWithOptions("scheduled ART", []string{"../../../wire/testdata/scheduled-timing-5.1.0.jhlog"}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	quality := summary.CollectionQuality
	if !quality.Complete || quality.KnownLostEvents != 0 || quality.AcceptedEvents != quality.WrittenEvents {
		t.Fatalf("incomplete fixture: %+v", quality)
	}
	if summary.AsyncAnalysis == nil || len(summary.AsyncAnalysis.Executors) != 8 {
		t.Fatalf("missing executor timing: %+v", summary.AsyncAnalysis)
	}
	wantRuns := map[string]uint64{"once": 1, "callable": 1, "rate": 4, "delay": 4, "cancelled": 0, "wide_two": 0, "wide_three": 0, "wide_zero_low": 0}
	wantDelay := map[string]uint64{"once": 40, "callable": 80, "rate": 60, "delay": 60, "cancelled": 86400000, "wide_two": 1<<63 - 1, "wide_three": 1<<63 - 1, "wide_zero_low": 1 << 62}
	wantSamples := map[string]uint64{"once": 1, "callable": 1, "rate": 1, "delay": 1, "cancelled": 1, "wide_two": 2, "wide_three": 3, "wide_zero_low": 4}
	for _, executor := range summary.AsyncAnalysis.Executors {
		if executor.Started != wantRuns[executor.Name] || executor.ScheduledLatenessSamples != wantRuns[executor.Name] || executor.WaitSamples != 0 || executor.AvgScheduledDelayMS != wantDelay[executor.Name] || executor.ScheduledDelaySamples != wantSamples[executor.Name] {
			t.Errorf("fixture timing mismatch: %+v", executor)
		}
	}
}

func TestLegacyMixedQueueWaitIsNotAResolvedProblemAfterScheduledMetricMigration(t *testing.T) {
	legacy := Summary{AsyncAnalysis: &AsyncAnalysis{Executors: []AsyncExecutorStats{{Name: "timer", WaitSamples: 20, MaxWaitMS: 1000}}}, Problems: []ProblemFinding{{Fingerprint: "timer-queue", DetectorID: "cpu.async_queue", Where: []ProblemLocation{{Owner: "timer"}}}}}
	modern := Summary{AsyncAnalysis: &AsyncAnalysis{Executors: []AsyncExecutorStats{{Name: "timer", ScheduledDelaySamples: 20, AvgScheduledDelayMS: 1000, ScheduledLatenessSamples: 20}}}}
	for _, pair := range [][2]Summary{{legacy, modern}, {modern, legacy}} {
		comparison := CompareProblems(pair[0], pair[1], true)
		if len(comparison.Deltas) != 1 {
			t.Fatalf("missing comparison: %+v", comparison)
		}
		delta := comparison.Deltas[0]
		if delta.Comparable || delta.Status != "not_comparable" || delta.Note == "" {
			t.Fatalf("metric migration was treated as a queue change: %+v", delta)
		}
	}
}
