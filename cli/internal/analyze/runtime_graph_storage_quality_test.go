package analyze

import (
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func TestGraphStorageSkippedEntriesAreUnknownEdgesAndCannotPassCompletenessGate(t *testing.T) {
	const storageSkippedEntries = 0x204a
	if !jhlog.IsKnownQualityCounter(storageSkippedEntries) {
		t.Error("graph storage omission counter is not recognized")
	}
	quality := crashCounterCollectionQuality(t, "unrelated", 0, map[uint64]uint64{storageSkippedEntries: 7})
	if quality.BoundedEvidenceLoss != 0 || quality.DiagnosticCompletenessPercent != -1 || quality.Complete {
		t.Errorf("method omissions must not become a fabricated edge count or full coverage: %+v", quality)
	}
	baseline := Summary{LogCount: 10, EventCount: 1000, CollectionQuality: crashCounterCollectionQuality(t, "unrelated", 0)}
	candidate := baseline
	candidate.CollectionQuality = quality
	gate := EvaluateGate(Compare(baseline, candidate), ThresholdConfig{Metrics: map[string]MetricThreshold{
		"diagnostic_completeness_percent": {MaxRegressionAbs: floatPointer(1)},
	}})
	if !gate.Failed {
		t.Error("unknown missing edges passed completeness gate")
	}
}

func TestGraphStorageARTFixturePreservesEveryCapturedEdgeAndUnknownOmittedVolume(t *testing.T) {
	summary, err := InspectFilesWithOptions("graph storage", []string{"../../../wire/testdata/runtime-graph-storage-5.1.0.jhlog"}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	q := summary.CollectionQuality
	if q.RuntimeGraphInputEvents != 255 || q.RuntimeGraphEmittedEvents != 255 || q.DecodedRuntimeGraphCalls != 255 ||
		q.KnownLostEvents != 0 || q.BoundedEvidenceLoss != 0 || q.DiagnosticCompletenessPercent != -1 || q.Complete {
		t.Fatalf("captured and omitted edges confused: %+v", q)
	}
	if !warningsContain(q.Reasons, "пропущено входов в методы 7") {
		t.Fatalf("missing storage coverage reason: %+v", q.Reasons)
	}
}
