package analyze

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func TestCrashDrainIncompletenessDoesNotBecomeInventedLostEventCounts(t *testing.T) {
	for _, stage := range []string{"metrics", "hooks", "graph", "writer", "concurrent"} {
		t.Run(stage, func(t *testing.T) {
			quality := crashCounterCollectionQuality(t, "jankhunter.runtime.crash_flush.incomplete."+stage+".count", 2)
			if quality.Complete || quality.Level == "high" {
				t.Fatalf("incomplete crash drain reported complete: %+v", quality)
			}
			if quality.DiagnosticCompletenessLevel != "unknown" || quality.DiagnosticCompletenessPercent != -1 {
				t.Fatalf("unknown upstream loss presented as maximal completeness: level=%s percent=%v", quality.DiagnosticCompletenessLevel, quality.DiagnosticCompletenessPercent)
			}
			if quality.KnownLostEvents != 0 || quality.BoundedEvidenceLoss != 0 {
				t.Fatalf("attempt count was misrepresented as lost events: %+v", quality)
			}
			if !warningsContain(quality.Reasons, "аварийное сохранение") {
				t.Fatalf("crash drain reason missing: %+v", quality.Reasons)
			}
		})
	}
}

func TestCrashCountAloneAndZeroIncompleteCountDoNotInventDataLoss(t *testing.T) {
	for _, test := range []struct {
		name  string
		value uint64
	}{
		{"jankhunter.runtime.crash.count", 1},
		{"jankhunter.runtime.crash_flush.incomplete.writer.count", 0},
	} {
		quality := crashCounterCollectionQuality(t, test.name, test.value)
		if !quality.Complete || quality.Level != "high" || quality.KnownLostEvents != 0 {
			t.Fatalf("diagnostic counter invented incompleteness: %+v", quality)
		}
	}
}

func crashCounterCollectionQuality(t *testing.T, name string, value uint64, additional ...map[uint64]uint64) CollectionQuality {
	t.Helper()
	collector := newCollector("crash drain", 1, Options{})
	collector.add(map[uint64]string{1: name}, jhlog.Event{
		Type:   jhlog.EventCounter,
		Metric: &jhlog.MetricEvent{MetricRef: jhlog.LocalSymbol(1), Value: value},
	})
	snapshot := jhlog.QualitySnapshot{Sequence: 1, Counters: map[uint64]uint64{
		jhlog.QualityAcceptedEventTotal: 1,
		jhlog.QualityWrittenEventTotal:  1,
	}}
	for _, extra := range additional {
		for key, value := range extra {
			snapshot.Counters[key] = value
		}
	}
	collector.addStreamResult(jhlog.StreamResult{
		Source: "crash.jhlog", Header: collectionTestHeader(71, 0),
		Status: jhlog.SegmentStatusClosedClean, Sealed: true, LatestQuality: &snapshot,
		SegmentEnd: &jhlog.SegmentEndEvent{Reason: jhlog.SegmentEndShutdown}, DataRecords: 1,
	})
	if err := collector.validateSegmentIdentityConsistency(); err != nil {
		t.Fatal(err)
	}
	collector.finalizeCollectionQuality()
	return collector.summary.CollectionQuality
}

func TestUnknownCrashCompletenessKeepsNumericJSONAndBlocksRequestedQualityGate(t *testing.T) {
	quality := crashCounterCollectionQuality(t, "jankhunter.runtime.crash_flush.incomplete.metrics.count", 1)
	encoded, err := json.Marshal(quality)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"diagnostic_completeness_percent":-1,`) {
		t.Errorf("numeric sentinel missing: %s", encoded)
	}
	baseline := Summary{LogCount: 10, EventCount: 1000, CollectionQuality: crashCounterCollectionQuality(t, "jankhunter.runtime.crash.count", 1)}
	candidate := baseline
	candidate.CollectionQuality = quality
	for _, config := range []ThresholdConfig{
		{MinConfidence: "low"}, {RequireCleanCohorts: true},
		{Metrics: map[string]MetricThreshold{"diagnostic_completeness_percent": {MaxRegressionAbs: floatPointer(1)}}},
	} {
		result := EvaluateGate(Compare(baseline, candidate), config)
		if !result.Failed {
			t.Errorf("unknown completeness passed requested gate: %+v", config)
		}
	}
	vector := BuildEvidenceQualityVector(candidate)
	if vector.Dimensions[0].Status == EvidenceQualityComplete {
		t.Error("unknown crash drain reported complete in evidence vector")
	}
}

func TestMetricFlushTimeoutIsAnUnknownVolumeNotOneLostEventPerAttempt(t *testing.T) {
	quality := crashCounterCollectionQuality(t, "unused", 0, map[uint64]uint64{jhlog.QualityMetricFlushTimeout: 2})
	if quality.DiagnosticCompletenessPercent != -1 || quality.DiagnosticCompletenessLevel != "unknown" || quality.BoundedEvidenceLoss != 0 || quality.KnownLostEvents != 0 {
		t.Fatalf("timeout attempts became a made-up loss volume: score=%v level=%s bounded=%v lost=%v", quality.DiagnosticCompletenessPercent, quality.DiagnosticCompletenessLevel, quality.BoundedEvidenceLoss, quality.KnownLostEvents)
	}
}

func TestARTCrashDrainFixturesKeepUnknownVolumeSeparateFromTransportLoss(t *testing.T) {
	for _, variant := range []string{"complete", "incomplete"} {
		path := "../../../wire/testdata/crash-drain-" + variant + "-5.1.0.jhlog"
		summary, err := InspectFilesWithOptions(variant, []string{path}, Options{})
		if err != nil {
			t.Fatal(err)
		}
		counts := map[string]uint64{}
		for _, counter := range summary.Counters {
			counts[counter.Name] = counter.Value
		}
		if counts["jankhunter.runtime.crash.count"] != 1 {
			t.Fatalf("missing original crash: %v", counts)
		}
		quality := summary.CollectionQuality
		if quality.KnownLostEvents != 0 || quality.BoundedEvidenceLoss != 0 {
			t.Fatalf("invented transport loss: %+v", quality)
		}
		if variant == "complete" {
			if counts["app.crash.pending.metric"] != 7 || counts["app.crash.PendingMethod"] != 1 || len(summary.RuntimeCalls) != 1 || summary.RuntimeCalls[0].Count != 1 || !quality.Complete || quality.DiagnosticCompletenessPercent != 100 {
				t.Fatalf("complete ART crash drain lost buffers: counters=%v graph=%v quality=%+v", counts, summary.RuntimeCalls, quality)
			}
		} else {
			if quality.Complete || quality.DiagnosticCompletenessPercent != -1 || quality.DiagnosticCompletenessLevel != "unknown" {
				t.Fatalf("incomplete ART crash reported complete: %+v", quality)
			}
			if counts["app.crash.unsaved.metric"] != 0 {
				t.Fatal("fixture unexpectedly drained blocked aggregate")
			}
			for _, stage := range []string{"metrics", "hooks", "graph", "writer"} {
				if counts["jankhunter.runtime.crash_flush.incomplete."+stage+".count"] != 1 {
					t.Fatalf("missing incomplete stage: %s", stage)
				}
			}
		}
	}
}

func TestCompletenessGateRespectsKnownZeroBoundariesAndMissingModels(t *testing.T) {
	for _, test := range []struct {
		before, after float64
		model         string
		failed        bool
	}{
		{100, 95, "model", false}, {100, 94, "model", true}, {0, 0, "model", false}, {0, 100, "model", false}, {100, 100, "", true},
	} {
		baseline := Summary{CollectionQuality: CollectionQuality{DiagnosticCompletenessPercent: test.before, DiagnosticCompletenessModel: test.model}}
		candidate := Summary{CollectionQuality: CollectionQuality{DiagnosticCompletenessPercent: test.after, DiagnosticCompletenessModel: test.model}}
		gate := EvaluateGate(Compare(baseline, candidate), ThresholdConfig{Metrics: map[string]MetricThreshold{"diagnostic_completeness_percent": {MaxRegressionAbs: floatPointer(5)}}})
		if gate.Failed != test.failed {
			t.Errorf("test=%+v gate=%+v", test, gate)
		}
	}
}
