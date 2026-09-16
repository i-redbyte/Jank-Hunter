package analyze

import (
	"encoding/json"
	"testing"
)

func TestUnfinishedWorkAndRejectedRepeatsAlonePreserveDeliveryCompleteness(t *testing.T) {
	quality := crashCounterCollectionQuality(t, "unrelated", 0, map[uint64]uint64{
		0x203e: 3, 0x203f: 2, 0x2044: 1, 0x2045: 2, 0x2046: 3, 0x2047: 4,
	})
	if !quality.Complete || quality.KnownLostEvents != 0 || quality.BoundedEvidenceLoss != 0 || quality.DiagnosticCompletenessPercent != 100 {
		t.Fatalf("a boundary observation reduced event delivery completeness: %+v", quality)
	}
}

func TestAsyncEpochBoundaryObservationsDoNotInventDeliveryLoss(t *testing.T) {
	quality := crashCounterCollectionQuality(t, "unrelated", 0, map[uint64]uint64{
		0x203e: 3, 0x203f: 2, 0x2044: 1, 0x2045: 2, 0x2046: 3, 0x2047: 4, 0x2048: 1,
	})
	if quality.KnownLostEvents != 0 || quality.BoundedEvidenceLoss != 0 || quality.DiagnosticCompletenessPercent != -1 {
		t.Fatalf("boundary observations became invented delivery losses: %+v", quality)
	}
	data, err := json.Marshal(quality)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Epoch struct {
			Stale       uint64 `json:"stale_completions"`
			Duplicate   uint64 `json:"duplicate_completions"`
			HTTP        uint64 `json:"unfinished_http"`
			Database    uint64 `json:"unfinished_database"`
			Worker      uint64 `json:"unfinished_worker"`
			Transaction uint64 `json:"unfinished_database_transactions"`
			Completing  uint64 `json:"completions_in_progress_at_stop"`
		} `json:"async_lifecycle"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	got := decoded.Epoch
	if got.Stale != 3 || got.Duplicate != 2 || got.HTTP != 1 || got.Database != 2 || got.Worker != 3 || got.Transaction != 4 || got.Completing != 1 {
		t.Fatalf("epoch boundary state is missing: %+v", got)
	}
}

func TestAsyncTokenCapacityOrIdentityExhaustionCannotPassCompletenessGate(t *testing.T) {
	for _, id := range []uint64{0x2040, 0x2041, 0x2042, 0x2048} {
		quality := crashCounterCollectionQuality(t, "unrelated", 0, map[uint64]uint64{id: 1})
		if quality.DiagnosticCompletenessPercent != -1 || quality.DiagnosticCompletenessLevel != "unknown" {
			t.Fatalf("counter %x silently claims full coverage: %+v", id, quality)
		}
		baseline := Summary{LogCount: 10, EventCount: 1000, CollectionQuality: crashCounterCollectionQuality(t, "unrelated", 0)}
		candidate := baseline
		candidate.CollectionQuality = quality
		gate := EvaluateGate(Compare(baseline, candidate), ThresholdConfig{Metrics: map[string]MetricThreshold{
			"diagnostic_completeness_percent": {MaxRegressionAbs: floatPointer(1)},
		}})
		if !gate.Failed {
			t.Fatalf("counter %x passed completeness gate", id)
		}
	}
}

func TestAsyncEpochQualityWarningsExplainObservationLimits(t *testing.T) {
	warnings := qualityCounterWarnings(map[uint64]uint64{0x203e: 3, 0x2044: 2, 0x2048: 1}, true)
	for _, phrase := range []string{"предыдущей сессии", "не завершились к границе сессии", "завершение уже обрабатывалось"} {
		if !warningsContain(warnings, phrase) {
			t.Fatalf("missing %q in %+v", phrase, warnings)
		}
	}
}
