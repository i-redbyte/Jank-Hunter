package jhlog

import (
	"strings"
	"testing"
)

func TestRuntimeGenerationARTFixtureHasIsolatedEventsAndExactQuality(t *testing.T) {
	log := Log{Dict: make(map[uint64]string)}
	stable := make(map[uint64]string)
	result, err := StreamFileWithResult("../../../wire/testdata/runtime-generation-5.1.0.jhlog", func(event Event, dict map[uint64]string) error {
		if entry := event.Dictionary; entry != nil {
			if entry.Kind == DictStableSymbol {
				stable[entry.ID] = entry.Value
			} else {
				log.Dict[entry.ID] = entry.Value
			}
		}
		if event.Type.IsSemanticData() {
			log.Events = append(log.Events, event)
		}
		return nil
	})
	log.Result = result
	if err != nil {
		t.Fatal(err)
	}
	if !log.Result.Sealed || log.Result.Status != SegmentStatusClosedClean || log.Result.LatestQuality == nil {
		t.Fatalf("generation writer not sealed cleanly: %+v", log.Result)
	}
	for id, want := range map[uint64]uint64{
		QualityRuntimeEventGenerationCapacityLoss:      2,
		QualityRuntimeGraphGenerationCapacityLoss:      1,
		QualityRuntimeGraphGenerationSkippedEntryTotal: 1,
		QualityRuntimeGraphInputTotal:                  2,
		QualityRuntimeGraphEmittedTotal:                1,
	} {
		if got := log.Result.LatestQuality.Counters[id]; got != want {
			t.Errorf("%s=%d, want %d", QualityCounterName(id), got, want)
		}
		if !IsKnownQualityCounter(id) {
			t.Errorf("quality ID %x not recognized", id)
		}
	}
	var methods, edges int
	for _, event := range log.Events {
		if event.Metric != nil && event.Metric.MetricRef.Stable && stable[event.Metric.MetricRef.ID] == "new-generation-only" {
			methods++
			if event.Metric.Value != 1 {
				t.Errorf("new method count=%d", event.Metric.Value)
			}
		}
		if event.RuntimeCall != nil {
			edges++
		}
	}
	if methods != 1 || edges != 1 {
		t.Fatalf("new generation events: methods=%d edges=%d", methods, edges)
	}
	for _, value := range stable {
		if strings.Contains(value, "old-generation") || value == "old-root" || value == "old-child" || value == "waiting-method" {
			t.Errorf("another generation or rejected payload leaked into the new writer: %s", value)
		}
	}
}
