package analyze

import (
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func comparisonScopeInput(run, process, processName string) Summary {
	profile := matchTestProfile("chat.open", run, 40)
	profile.ProcessInstanceID = process
	profile.ProcessName = processName
	return Summary{
		OperationAnalysis: &OperationAnalysis{Profiles: []OperationProfile{profile}, Completed: 40},
		CollectionQuality: CollectionQuality{Complete: true, DiagnosticCompletenessPercent: 100},
		CollectorSessions: 1,
		CollectorFlagsAll: ^uint64(0),
		Devices:           []NamedValue{{Name: "pixel-8", Value: 1}},
		SDKs:              []NamedValue{{Name: "35", Value: 1}},
		Network:           []NamedValue{{Name: "wifi", Value: 1}},
		Processes:         []NamedValue{{Name: processName, Value: 1}},
		Detectors:         []DetectorMetadata{{ID: "operation.failure", Version: "1"}},
	}
}

func eligibilityByDomain(t *testing.T, values []MetricEligibility, domain ComparisonDomain) MetricEligibility {
	t.Helper()
	for _, value := range values {
		if value.Domain == domain {
			return value
		}
	}
	t.Fatalf("missing eligibility %s", domain)
	return MetricEligibility{}
}

func TestComparisonScopeRequiresKnownSameApplicationAndProcessRole(t *testing.T) {
	baseline := comparisonScopeInput("01000000000000000000000000000000", "11000000000000000000000000000000", "com.example.app")
	candidate := comparisonScopeInput("02000000000000000000000000000000", "22000000000000000000000000000000", "com.example.app")
	result := buildComparisonScope(baseline, candidate)
	if result.Comparability != ScenarioFull || result.Application.State != EligibilityEligible || result.Process.State != EligibilityEligible {
		t.Fatalf("same application/process rejected: %+v", result)
	}

	candidate.OperationAnalysis.Profiles[0].ProcessName = "com.other.app"
	result = buildComparisonScope(baseline, candidate)
	if result.Comparability != ScenarioNone || result.Application.State != EligibilityIneligible || result.Outcome != ChangeNotComparable {
		t.Fatalf("different applications compared: %+v", result)
	}

	candidate.OperationAnalysis.Profiles[0].ProcessName = "unknown"
	result = buildComparisonScope(baseline, candidate)
	if result.Comparability != ScenarioUnknown || result.Application.State != EligibilityUnknown || result.Outcome != ChangeInsufficientData {
		t.Fatalf("unknown application treated as compatible: %+v", result)
	}
}

func TestComparisonScopeAllowsBuildChangeButSeparatesEnvironmentAndCollectorEligibility(t *testing.T) {
	baseline := comparisonScopeInput("01000000000000000000000000000000", "11000000000000000000000000000000", "com.example.app:worker")
	candidate := comparisonScopeInput("02000000000000000000000000000000", "22000000000000000000000000000000", "com.example.app:worker")
	baseline.Builds = []NamedValue{{Name: "100", Value: 1}}
	candidate.Builds = []NamedValue{{Name: "101", Value: 1}}
	result := buildComparisonScope(baseline, candidate)
	if result.Comparability != ScenarioFull {
		t.Fatalf("build change blocked before/after: %+v", result)
	}

	candidate.Devices[0].Name = "tablet"
	result = buildComparisonScope(baseline, candidate)
	if got := eligibilityByDomain(t, result.Metrics, DomainMemory); got.State != EligibilityIneligible || result.Comparability != ScenarioFull {
		t.Fatalf("device mismatch not isolated to metric: %+v", result)
	}

	candidate.Devices = nil
	result = buildComparisonScope(baseline, candidate)
	if got := eligibilityByDomain(t, result.Metrics, DomainMemory); got.State != EligibilityUnknown {
		t.Fatalf("missing device evidence treated as compatible: %+v", got)
	}

	candidate.Devices = baseline.Devices
	candidate.CollectorFlagsAll &^= uint64(jhlog.CollectorDatabase)
	result = buildComparisonScope(baseline, candidate)
	if got := eligibilityByDomain(t, result.Metrics, DomainDatabase); got.State != EligibilityIneligible || got.Reason != ReasonCollectorDisabled {
		t.Fatalf("disabled collector hidden: %+v", got)
	}
}

func TestComparisonScopeLossFilterAndDetectorDriftStayUnknown(t *testing.T) {
	baseline := comparisonScopeInput("01000000000000000000000000000000", "11000000000000000000000000000000", "com.example.app")
	candidate := comparisonScopeInput("02000000000000000000000000000000", "22000000000000000000000000000000", "com.example.app")
	candidate.CollectionQuality.KnownLostEvents = 1
	candidate.AnalysisFilter = &Filter{ScreenContains: "chat"}
	baseline.AnalysisFilter = &Filter{ScreenContains: "feed"}
	candidate.Detectors[0].Version = "2"
	result := buildComparisonScope(baseline, candidate)
	for _, domain := range []ComparisonDomain{DomainLatency, DomainFailure, DomainProblem} {
		got := eligibilityByDomain(t, result.Metrics, domain)
		if got.State != EligibilityUnknown {
			t.Fatalf("%s unexpectedly eligible: %+v", domain, got)
		}
	}
	if result.Comparability != ScenarioUnknown || result.Outcome != ChangeInsufficientData {
		t.Fatalf("unknown eligibility produced verdict: %+v", result)
	}
}

func TestComparisonScopeReportsPartialCoverageWithoutGlobalVerdict(t *testing.T) {
	baseline := comparisonScopeInput("01000000000000000000000000000000", "11000000000000000000000000000000", "com.example.app")
	candidate := comparisonScopeInput("02000000000000000000000000000000", "22000000000000000000000000000000", "com.example.app")
	baseline.OperationAnalysis.Profiles = append(baseline.OperationAnalysis.Profiles, matchTestProfile("search", baseline.OperationAnalysis.Profiles[0].RunID, 60))
	baseline.OperationAnalysis.Profiles[1].ProcessInstanceID = baseline.OperationAnalysis.Profiles[0].ProcessInstanceID
	baseline.OperationAnalysis.Profiles[1].ProcessName = "com.example.app"
	result := buildComparisonScope(baseline, candidate)
	if result.Comparability != ScenarioPartial || result.Baseline.Total != 100 || result.Baseline.Matched != 40 || result.Candidate.Total != 40 || result.Outcome != ChangeUnchanged {
		t.Fatalf("partial coverage overstated: %+v", result)
	}
}
