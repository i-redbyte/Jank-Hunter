package jhlog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestGaugeWideSumSurvivesScalarAndMicroPageRoundTrips(t *testing.T) {
	for _, count := range []int{1, 128} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			var metric MetricEvent
			if err := json.Unmarshal([]byte(`{"value":4611686018427387904,"count":4,"sum":0,"sum_high":1,"max":4611686018427387904,"mode":1}`), &metric); err != nil {
				t.Fatal(err)
			}
			events := make([]Event, count)
			for index := range events {
				copy := metric
				events[index] = Event{Type: EventGauge, TimeMS: uint64(index + 1), Metric: &copy}
			}
			path := filepath.Join(t.TempDir(), "wide.jhlog")
			writeClosedEvents(t, path, events)
			log, err := readLog(path)
			if err != nil {
				t.Fatal(err)
			}
			if len(log.Events) != count {
				t.Fatalf("events = %d, want %d", len(log.Events), count)
			}
			for _, event := range log.Events {
				data, err := json.Marshal(event.Metric)
				if err != nil {
					t.Fatal(err)
				}
				var got struct {
					Sum  uint64 `json:"sum"`
					High uint64 `json:"sum_high"`
				}
				if err := json.Unmarshal(data, &got); err != nil {
					t.Fatal(err)
				}
				if got.Sum != 0 || got.High != 1 {
					t.Fatalf("wide sum lost: %s", data)
				}
			}
			if log.Result.Header.RequiredFeatures&(1<<27) == 0 {
				t.Fatal("wide sum has no mandatory feature")
			}
		})
	}
}

func TestStateGaugeMayKeepAPeakAboveItsLatestValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.jhlog")
	writeClosedEvents(t, path, []Event{{Type: EventGauge, Metric: &MetricEvent{Value: 5, Count: 2, Sum: 5, Max: 10, Mode: MetricModeState}}})
	log, err := readLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(log.Events) != 1 || log.Events[0].Metric.Value != 5 || log.Events[0].Metric.Max != 10 {
		t.Fatalf("state gauge lost: %+v", log)
	}
}

func TestLegacyGaugeFeatureStillReadsAndCannotSilentlyWriteAWideSum(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.jhlog")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	header := DefaultSegmentHeader()
	header.RequiredFeatures &^= FeatureGaugeWideSum
	writer, err := NewWriterWithOptions(file, WriterOptions{Header: header, GZIP: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteEvent(Event{Type: EventGauge, Metric: &MetricEvent{Value: 7, Count: 1, Sum: 7, Max: 7}}); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteEvent(Event{Type: EventGauge, Metric: &MetricEvent{Value: 1, Count: 4, SumHigh: 1, Max: 1 << 62}}); err == nil {
		t.Fatal("legacy writer silently discarded high sum")
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	log, err := readLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(log.Events) != 1 || log.Events[0].Metric.SumHigh != 0 || log.Events[0].Metric.Sum != 7 {
		t.Fatalf("legacy gauge changed: %+v", log.Events)
	}
}

func TestScheduledARTFixturePreservesBothWordsIncludingZeroLowWord(t *testing.T) {
	log, err := readLog("../../../wire/testdata/scheduled-timing-5.1.0.jhlog")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][3]uint64{
		"executor.wide_two.scheduled_delay_ms":      {2, ^uint64(0) - 1, 0},
		"executor.wide_three.scheduled_delay_ms":    {3, 1<<63 - 3, 1},
		"executor.wide_zero_low.scheduled_delay_ms": {4, 0, 1},
	}
	for _, event := range log.Events {
		if event.Metric == nil {
			continue
		}
		name := ResolveSymbol(log.Dict, event.Metric.MetricRef)
		expected, ok := want[name]
		if !ok {
			continue
		}
		got := [3]uint64{event.Metric.Count, event.Metric.Sum, event.Metric.SumHigh}
		if got != expected {
			t.Fatalf("%s = %v, want %v", name, got, expected)
		}
		delete(want, name)
	}
	if len(want) != 0 {
		t.Fatalf("missing exact sums: %v", want)
	}
}
