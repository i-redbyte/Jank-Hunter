package analyze

import (
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func TestPowerCoverageDescribesMeasuredContextWithoutClaimingEnergyConsumption(t *testing.T) {
	summary := Summary{
		DurationMS:        60_000,
		CollectionQuality: CollectionQuality{Complete: true},
		AnalysisInputs:    AnalysisInputCompleteness{Complete: true, RuntimeEvidence: true},
		CollectorSessions: 1,
		CollectorFlagsAll: uint64(jhlog.CollectorSystemSampler),
		Gauges: []NamedGauge{
			{Name: "battery.level_pct", Value: 80, sampleCount: 4},
			{Name: "device.thermal.status", Value: 1, maximum: 1, sampleCount: 4},
		},
	}

	report, err := BuildProblemReport(summary)
	if err != nil {
		t.Fatal(err)
	}
	coverage := categoryCoverageByID(report.Coverage, ProblemCategoryPower)
	if coverage == nil {
		t.Fatal("power coverage is missing")
	}
	visible := strings.Join(append(
		[]string{coverage.Label, coverage.Explanation, coverage.NextAction},
		append(coverage.RequiredEvidence, coverage.AvailableEvidence...)...,
	), " ")
	for _, unsupportedClaim := range []string{"расход батареи", "энергопотребление", "потраченная энергия"} {
		if strings.Contains(strings.ToLower(visible), unsupportedClaim) {
			t.Fatalf("power coverage claims unmeasured %q: %s", unsupportedClaim, visible)
		}
	}
	for _, measuredSignal := range []string{"уровень заряда", "thermal status"} {
		if !strings.Contains(strings.ToLower(visible), strings.ToLower(measuredSignal)) {
			t.Fatalf("power coverage misses measured signal %q: %s", measuredSignal, visible)
		}
	}
	if strings.Contains(strings.ToLower(visible), "состояние зарядки") {
		t.Fatalf("power coverage claims a signal absent from the summary: %s", visible)
	}
}

func TestPowerCoverageDoesNotTreatBatteryContextAsThermalMeasurement(t *testing.T) {
	summary := Summary{
		DurationMS:        60_000,
		CollectionQuality: CollectionQuality{Complete: true},
		AnalysisInputs:    AnalysisInputCompleteness{Complete: true, RuntimeEvidence: true},
		CollectorSessions: 1,
		CollectorFlagsAll: uint64(jhlog.CollectorSystemSampler),
		Gauges: []NamedGauge{{
			Name: "battery.level_pct", Value: 80, sampleCount: 10,
		}},
	}

	report, err := BuildProblemReport(summary)
	if err != nil {
		t.Fatal(err)
	}
	coverage := categoryCoverageByID(report.Coverage, ProblemCategoryPower)
	if coverage == nil {
		t.Fatal("power coverage is missing")
	}
	if coverage.Status != "insufficient_data" {
		t.Fatalf("battery-only power coverage status = %q, want insufficient_data", coverage.Status)
	}
	if coverage.NextAction == "" {
		t.Fatal("battery-only power coverage has no next action")
	}
}

func TestThermalFindingUsesPeakAndActualSampleCount(t *testing.T) {
	summary := Summary{
		DurationMS:        60_000,
		CollectionQuality: CollectionQuality{Complete: true},
		AnalysisInputs:    AnalysisInputCompleteness{Complete: true, RuntimeEvidence: true},
		Gauges: []NamedGauge{{
			Name: "device.thermal.status", Value: 1, maximum: 4, sampleCount: 5,
		}},
	}

	report, err := BuildProblemReport(summary)
	if err != nil {
		t.Fatal(err)
	}
	finding := findingByDetector(report.Problems, "power.thermal_pressure")
	if finding == nil {
		t.Fatal("thermal peak was not detected after the device cooled down")
	}
	if finding.Confidence != "high" {
		t.Fatalf("thermal confidence = %q, want high", finding.Confidence)
	}
	if len(finding.Evidence) != 1 || finding.Evidence[0].Observed != "4" ||
		finding.Evidence[0].Sample == nil || *finding.Evidence[0].Sample != 5 {
		t.Fatalf("thermal evidence = %+v", finding.Evidence)
	}
}
