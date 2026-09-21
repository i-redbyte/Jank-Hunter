package analyze

import (
	"encoding/json"
	"github.com/i-redbyte/jank-hunter/cli/internal/traffic"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loadGateTestConfig(t *testing.T, body string) (ThresholdConfig, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "thresholds.json")
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	return LoadThresholdConfig(path)
}

func TestGateRejectsUnknownAndNonNumericMetricConstraints(t *testing.T) {
	for _, body := range []string{
		`{"metrics":{"HTTP p955":{"max_regression_abs":1}}}`,
		`{"metrics":{"Process mix":{"max_regression_pct":1}}}`,
		`{"metrics":{"HTTP p95":{}}}`,
	} {
		if _, err := loadGateTestConfig(t, body); err == nil {
			t.Errorf("ineffective metric constraint accepted: %s", body)
		}
	}
}

func TestGateExplicitZeroIsEnforcedAndOmittedLimitIsInactive(t *testing.T) {
	comparison := Compare(Summary{HTTPCount: 5, HTTPP95MS: 100}, Summary{HTTPCount: 5, HTTPP95MS: 200})
	for _, body := range []string{
		`{"metrics":{"HTTP p95":{"max_regression_abs":0}}}`,
		`{"metrics":{"HTTP p95":{"max_regression_pct":0}}}`,
	} {
		config, err := loadGateTestConfig(t, body)
		if err != nil {
			t.Fatal(err)
		}
		if result := EvaluateGate(comparison, config); !result.Failed {
			t.Errorf("explicit zero ignored: %s", body)
		}
	}
	config, err := loadGateTestConfig(t, `{"metrics":{"HTTP p95":{"max_severity":"critical"}}}`)
	if err != nil {
		t.Fatal(err)
	}
	if result := EvaluateGate(comparison, config); result.Failed {
		t.Fatalf("omitted numeric limit was enabled: %+v", result)
	}
}

func TestGateRequestedMetricMustBeAvailable(t *testing.T) {
	config, err := loadGateTestConfig(t, `{"metrics":{"HTTP p95":{"max_regression_abs":1}}}`)
	if err != nil {
		t.Fatal(err)
	}
	withHTTP := Summary{HTTPCount: 5, HTTPP95MS: 100}
	for _, comparison := range []Comparison{
		Compare(withHTTP, Summary{}), Compare(Summary{}, withHTTP), Compare(Summary{}, Summary{}), {},
	} {
		result := EvaluateGate(comparison, config)
		if !result.Failed || !strings.Contains(strings.Join(result.Failures, ";"), "cannot be evaluated") {
			t.Errorf("missing metric passed: %+v", result)
		}
	}
}

func TestGateCannotInventRelativeRegressionAtZeroBaseline(t *testing.T) {
	config, err := loadGateTestConfig(t, `{"metrics":{"HTTP p95":{"max_regression_pct":150}}}`)
	if err != nil {
		t.Fatal(err)
	}
	result := EvaluateGate(Compare(Summary{HTTPCount: 1}, Summary{HTTPCount: 1, HTTPP95MS: 100}), config)
	if !result.Failed || !strings.Contains(strings.Join(result.Failures, ";"), "zero baseline") {
		t.Fatalf("undefined percentage passed using fabricated100percent: %+v", result)
	}
}

func TestGateEmptyComparisonCannotPassRequestedConfidence(t *testing.T) {
	if result := EvaluateGate(Comparison{}, ThresholdConfig{MinConfidence: "high"}); !result.Failed {
		t.Fatal("missing comparison passed confidence requirement")
	}
}

func TestGateExplicitZeroLeakLimitIsEnforced(t *testing.T) {
	comparison := Comparison{Candidate: Summary{MemoryLeaks: []MemoryLeakSuspect{{ClassName: "example.Screen", Count: 1, Severity: "high"}}}}
	config, err := loadGateTestConfig(t, `{"leaks":{"max_candidate_total":0}}`)
	if err != nil {
		t.Fatal(err)
	}
	if result := EvaluateGate(comparison, config); !result.Failed {
		t.Fatal("explicit zero leak count ignored")
	}
}

func gateIntPointer(value int) *int { return &value }

func TestGateRegistryCoversEveryComparisonMetric(t *testing.T) {
	for _, delta := range Compare(Summary{}, Summary{}).Deltas {
		if known, _ := gateMetricKind(delta.Name); !known {
			t.Errorf("unregistered comparison key %q", delta.Name)
		}
	}
}

func TestGateProgrammaticInputsCannotBypassValidation(t *testing.T) {
	for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1), -1} {
		for _, config := range []ThresholdConfig{
			{Metrics: map[string]MetricThreshold{"HTTP p95": {MaxRegressionAbs: &value}}},
			{AndroidComponents: AndroidComponentGateThreshold{Enabled: true, MaxBinderClientP95IncreasePct: &value}},
		} {
			if result := EvaluateGate(Comparison{}, config); !result.Failed {
				t.Errorf("invalid limit %v passed", value)
			}
		}
	}
	if result := EvaluateGate(Comparison{}, ThresholdConfig{Metrics: map[string]MetricThreshold{"HTTP p955": {MaxSeverity: "high"}}}); !result.Failed {
		t.Fatal("programmatic unknown metric passed")
	}
}

func TestGateNonFiniteMeasurementsCannotPass(t *testing.T) {
	config := ThresholdConfig{Metrics: map[string]MetricThreshold{"HTTP p95": {MaxRegressionAbs: floatPointer(0)}}}
	for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		comparison := Compare(Summary{HTTPCount: 1, HTTPP95MS: 100}, Summary{HTTPCount: 1, HTTPP95MS: 100})
		comparison.Deltas[0].CandidateValue = value
		if result := EvaluateGate(comparison, config); !result.Failed {
			t.Errorf("non-finite measurement %v passed", value)
		}
	}
}

func TestGateZeroAndOmittedLimitsSurviveJSONRoundTrip(t *testing.T) {
	config, err := loadGateTestConfig(t, `{"metrics":{"HTTP p95":{"max_regression_abs":0}},"leaks":{"max_new":0}}`)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	var decoded ThresholdConfig
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	threshold := decoded.Metrics["HTTP p95"]
	if threshold.MaxRegressionAbs == nil || *threshold.MaxRegressionAbs != 0 || threshold.MaxRegressionPct != nil || decoded.Leaks.MaxNew == nil || *decoded.Leaks.MaxNew != 0 || decoded.Leaks.MaxHigh != nil {
		t.Fatalf("optional limits changed meaning: %s", data)
	}
}

func TestGateAbsoluteLimitCanMeasureNewMetricAndUnchangedZero(t *testing.T) {
	for _, candidate := range []uint64{0, 100} {
		comparison := Compare(Summary{HTTPCount: 1}, Summary{HTTPCount: 1, HTTPP95MS: candidate})
		config := ThresholdConfig{Metrics: map[string]MetricThreshold{"HTTP p95": {MaxRegressionAbs: floatPointer(100)}}}
		if result := EvaluateGate(comparison, config); result.Failed {
			t.Fatalf("measured absolute delta refused: %+v", result)
		}
	}
	config := ThresholdConfig{Metrics: map[string]MetricThreshold{"HTTP p95": {MaxRegressionPct: floatPointer(0)}}}
	if result := EvaluateGate(Compare(Summary{HTTPCount: 1}, Summary{HTTPCount: 1}), config); result.Failed {
		t.Fatal("unchanged measured zero is not a regression")
	}
}

func TestGateAndroidConstraintsRejectUnknownRelativeAndNonFiniteValues(t *testing.T) {
	config := ThresholdConfig{AndroidComponents: AndroidComponentGateThreshold{Enabled: true, MaxBinderClientP95IncreasePct: floatPointer(150)}}
	for _, metric := range []Delta{
		{Name: androidMetricBinderClientP95, Comparable: true, CandidateValue: 20, RegressionAbs: 20, RegressionPct: 100},
		{Name: androidMetricBinderClientP95, Comparable: true, BaselineValue: 10, CandidateValue: math.NaN(), RegressionPct: math.NaN()},
	} {
		comparison := Comparison{AndroidComponents: AndroidComponentComparison{Comparable: true, Metrics: []Delta{metric}}}
		if result := EvaluateGate(comparison, config); !result.Failed {
			t.Errorf("Android component unavailable regression passed: %+v", metric)
		}
	}
}

func TestGateSuppliedFileMustConfigureAnActualCheck(t *testing.T) {
	for _, body := range []string{`{}`, `{"metrics":{},"leaks":{}}`, `{"android_components":{"enabled":false}}`} {
		if _, err := loadGateTestConfig(t, body); err == nil {
			t.Errorf("empty gate accepted: %s", body)
		}
	}
	if result := EvaluateGate(Comparison{}, ThresholdConfig{}); result.Failed {
		t.Fatal("absent programmatic gate must remain disabled")
	}
}

func TestUIDTrafficGateRequiresExactDirectionAndAcceptsKnownZero(t *testing.T) {
	for _, state := range []string{"unknown", "lower_bound", "exact"} {
		summary := Summary{ContextCount: 2, CollectionQuality: CollectionQuality{Traffic: &traffic.Evidence{RX: traffic.DirectionEvidence{State: state, Intervals: 1}, TX: traffic.DirectionEvidence{State: "exact", Intervals: 1}}}}
		for _, name := range []string{"UID RX delta", "UID TX delta"} {
			comparison := Compare(summary, summary)
			config := ThresholdConfig{Metrics: map[string]MetricThreshold{name: {MaxRegressionAbs: floatPointer(0)}}}
			got := EvaluateGate(comparison, config)
			wantFailed := name == "UID RX delta" && state != "exact"
			if got.Failed != wantFailed {
				t.Fatalf("state=%s metric=%s result=%+v", state, name, got)
			}
		}
	}
}
