package analyze

import (
	"encoding/json"
	"testing"
)

func TestUnattributedHandlerPostsHaveSeparateUnknownStateAndPreserveDeliveryCompleteness(t *testing.T) {
	quality := crashCounterCollectionQuality(t, "unrelated", 0, map[uint64]uint64{0x203d: 7})
	if !quality.Complete || quality.DiagnosticCompletenessPercent != 100 || quality.BoundedEvidenceLoss != 0 || quality.KnownLostEvents != 0 {
		t.Fatalf("attribution coverage changed event delivery completeness: %+v", quality)
	}
	encoded, err := json.Marshal(quality)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Async struct {
			Status string `json:"status"`
			Posts  uint64 `json:"handler_posts_without_context"`
		} `json:"async_attribution"`
	}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Async.Status != "unknown" || decoded.Async.Posts != 7 {
		t.Fatalf("async attribution state missing: %s", encoded)
	}
	baseline := Summary{LogCount: 10, EventCount: 1000, CollectionQuality: crashCounterCollectionQuality(t, "unrelated", 0)}
	candidate := baseline
	candidate.CollectionQuality = quality
	gate := EvaluateGate(Compare(baseline, candidate), ThresholdConfig{Metrics: map[string]MetricThreshold{"diagnostic_completeness_percent": {MaxRegressionAbs: floatPointer(1)}}})
	if gate.Failed {
		t.Fatalf("delivery completeness gate was affected by attribution: %+v", gate)
	}
}

func TestHandlerPostCoverageWarningExplainsUnknownAttributionAndPossibleCancellation(t *testing.T) {
	warnings := qualityCounterWarnings(map[uint64]uint64{0x203d: 7}, true)
	if len(warnings) != 1 || !warningsContain(warnings, "связь отправки с выполнением неизвестна") || !warningsContain(warnings, "могут быть отменены") || !warningsContain(warnings, "7") {
		t.Fatalf("unavailable Handler attribution was hidden or treated as lost execution: %+v", warnings)
	}
}
