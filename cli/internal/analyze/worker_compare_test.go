package analyze

import (
	"fmt"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
	"testing"
)

func workerComparisonSummary(duration, starts uint64) Summary {
	return Summary{DurationMS: duration, CollectorFlagsAll: uint64(jhlog.CollectorWorker), CollectionQuality: CollectionQuality{Complete: true, ProcessRosterComplete: true, ProcessScope: "all", ProcessScopeFingerprint: "app"}, WorkerAnalysis: &WorkerAnalysis{Started: starts, Finished: starts, RunSamples: starts, RunP95MS: 40, WaitSamples: 0}}
}

func TestWorkerComparisonRejectsChangedObservationStrategy(t *testing.T) {
	before, after := workerComparisonSummary(60000, 2), workerComparisonSummary(60000, 3)
	before.Counters = []NamedValue{{Name: "jankhunter.observation.tasks.active.WRAPPERS.count", Value: 1},
		{Name: "jankhunter.observation.coroutines.active.WRAPPERS.count", Value: 1}}
	after.Counters = []NamedValue{{Name: "jankhunter.observation.tasks.active.IN_PLACE.count", Value: 1},
		{Name: "jankhunter.observation.coroutines.active.WRAPPERS.count", Value: 1}}
	comparison := compareWorkerDiagnostics(before, after, ComparisonScope{Comparability: ScenarioFull})
	for _, metric := range comparison.Lifecycle {
		if metric.Comparable || metric.Delta != nil {
			t.Fatalf("different observation contracts compared as equivalent: %+v", metric)
		}
	}
}
func TestWorkerClassComparisonBoundsAndSortsRows(t *testing.T) {
	before := workerComparisonSummary(60000, 0)
	for index := 249; index >= 0; index-- {
		before.WorkerAnalysis.Workers = append(before.WorkerAnalysis.Workers, WorkerStats{Worker: fmt.Sprintf("app.Worker%03d", index), Started: 1})
	}
	rows, omitted := compareWorkerClasses(before, Summary{}, false)
	if len(rows) != workerComparisonClassLimit || omitted != 50 || rows[0].Class != "app.Worker000" || rows[len(rows)-1].Class != "app.Worker199" {
		t.Fatalf("unbounded or unstable class rows: %d, %d, first=%+v, last=%+v", len(rows), omitted, rows[0], rows[len(rows)-1])
	}
}
func TestWorkerComparisonDoesNotTurnAbsentTelemetryIntoZero(t *testing.T) {
	c := compareWorkerDiagnostics(Summary{}, workerComparisonSummary(60000, 5), ComparisonScope{Comparability: ScenarioFull})
	for _, m := range c.Lifecycle {
		if m.Baseline != nil || m.Comparable || m.Delta != nil {
			t.Fatalf("invented baseline: %+v", m)
		}
	}
}
func TestWorkerComparisonNormalizesOnlyCompleteMatchedScope(t *testing.T) {
	c := compareWorkerDiagnostics(workerComparisonSummary(60000, 2), workerComparisonSummary(120000, 4), ComparisonScope{Comparability: ScenarioFull})
	var found bool
	for _, m := range c.Lifecycle {
		if m.Key == "starts_per_minute" {
			found = true
			if !m.Comparable || m.Baseline == nil || *m.Baseline != 2 || *m.Candidate != 2 || *m.Delta != 0 {
				t.Fatalf("wrong exposure: %+v", m)
			}
		}
		if m.Key == "wait_p95_ms" && (m.Baseline != nil || m.Comparable) {
			t.Fatal("unknown wait became zero")
		}
	}
	if !found {
		t.Fatal("missing normalized metric")
	}
	c = compareWorkerDiagnostics(workerComparisonSummary(60000, 2), workerComparisonSummary(120000, 4), ComparisonScope{})
	for _, m := range c.Lifecycle {
		if m.Comparable {
			t.Fatal("unknown scenario treated as comparable")
		}
	}
}

func TestWorkerClassComparisonRequiresVerifiedNamesAndCompleteCoverage(t *testing.T) {
	before := workerComparisonSummary(60000, 2)
	after := workerComparisonSummary(60000, 3)
	before.WorkerAnalysis.Workers = []WorkerStats{{Worker: "app.Sync", Started: 2}}
	after.WorkerAnalysis.Workers = []WorkerStats{{Worker: "app.Sync", Started: 3}}
	scope := ComparisonScope{Comparability: ScenarioFull}
	rows := compareWorkerDiagnostics(before, after, scope).Classes
	if len(rows) != 1 || rows[0].Comparable || rows[0].DeltaStarts != nil {
		t.Fatalf("unverified runtime names were compared: %+v", rows)
	}
	before.MappingIdentity = MappingIdentityEvidence{Applied: true, Status: MappingVerified, SHA256: "first"}
	after.MappingIdentity = MappingIdentityEvidence{Applied: true, Status: MappingVerified, SHA256: "second"}
	rows = compareWorkerDiagnostics(before, after, scope).Classes
	if len(rows) != 1 || !rows[0].Comparable || rows[0].DeltaStarts == nil || *rows[0].DeltaStarts != 1 {
		t.Fatalf("verified source class names were not compared: %+v", rows)
	}
	after.WorkerAnalysis.DroppedLifecycleEvents = 1
	rows = compareWorkerDiagnostics(before, after, scope).Classes
	if rows[0].Comparable || rows[0].DeltaStarts != nil {
		t.Fatalf("incomplete lifecycle evidence compared: %+v", rows)
	}
}
func TestRegistryScheduleComparisonDoesNotUseUUIDAsCrossRunIdentity(t *testing.T) {
	makeSummary := func(uuid string) Summary {
		s := workerComparisonSummary(60000, 1)
		age := uint64(0)
		s.WorkRegistry = &WorkRegistryAnalysis{Snapshots: []WorkRegistrySnapshot{{Scope: uuid, Process: "app", Complete: true, IntervalMS: 30000, AgeAtCaptureMS: &age, Jobs: []jhlog.WorkRegistryJob{{UUID: uuid, InitialDelayMS: 5000, NetworkType: "CONNECTED", Constraints: 1}}}}}
		return s
	}
	c := compareWorkerDiagnostics(makeSummary("random-a"), makeSummary("random-b"), ComparisonScope{Comparability: ScenarioFull})
	if len(c.Registry) != 1 || !c.Registry[0].Comparable || len(c.Registry[0].Schedules) != 1 {
		t.Fatalf("schedule matching: %+v", c.Registry)
	}
	row := c.Registry[0].Schedules[0]
	if row.Baseline != 1 || row.Candidate != 1 {
		t.Fatalf("UUID used as identity: %+v", row)
	}
}

func TestScheduleGroupingDoesNotMergeDifferentURILists(t *testing.T) {
	a := jhlog.WorkRegistryJob{ContentTriggers: []string{"a, b", "c"}}
	b := jhlog.WorkRegistryJob{ContentTriggers: []string{"a", "b, c"}}
	if workerScheduleKey(a) == workerScheduleKey(b) {
		t.Fatal("ambiguous string concatenation merged constraints")
	}
}
func TestRegistryTruncatedConstraintsAreNotComparable(t *testing.T) {
	age := uint64(0)
	a := workerComparisonSummary(60000, 1)
	a.WorkRegistry = &WorkRegistryAnalysis{Snapshots: []WorkRegistrySnapshot{{Scope: "s", Process: "app", Complete: true, IntervalMS: 30000, AgeAtCaptureMS: &age, Jobs: []jhlog.WorkRegistryJob{{OmittedContentTriggers: 1}}}}}
	if compareWorkerDiagnostics(a, a, ComparisonScope{Comparability: ScenarioFull}).Registry[0].Comparable {
		t.Fatal("omitted constraints treated as complete")
	}
}

func TestRegistryComparisonDoesNotRequireExecutedAttempts(t *testing.T) {
	age := uint64(0)
	a := workerComparisonSummary(60000, 0)
	a.WorkerAnalysis = nil
	a.WorkRegistry = &WorkRegistryAnalysis{Snapshots: []WorkRegistrySnapshot{{Scope: "s", Process: "app", Complete: true, IntervalMS: 30000, AgeAtCaptureMS: &age}}}
	if !compareWorkerDiagnostics(a, a, ComparisonScope{Comparability: ScenarioFull}).Registry[0].Comparable {
		t.Fatal("registry without executions cannot be compared")
	}
}

func TestCatalogComparisonPreservesMissingInventoryAndUnknownApplicationScope(t *testing.T) {
	available := &WorkerCatalogAnalysis{Observations: []WorkerCatalogObservation{{Catalog: &jhlog.WorkerCatalogEvent{Status: "available", Classes: []string{"app.Worker"}}}}}
	row := compareWorkerCatalogs(nil, available)
	if row.BaselineCount != nil || row.CandidateCount == nil || *row.CandidateCount != 1 || row.Comparable {
		t.Fatalf("catalog absence: %+v", row)
	}
	a := Summary{WorkerCatalog: available}
	if compareWorkerDiagnostics(a, a, ComparisonScope{}).Catalog.Comparable {
		t.Fatal("unverified application scope matched catalog classes")
	}
}

func TestRegistryComparisonUsesLatestEpochOfSameProvenProcessInstance(t *testing.T) {
	age := uint64(0)
	oldScope := "010000000000000000000000000000000200000000000000000000000000000003000000000000000000000000000000"
	newScope := "010000000000000000000000000000000200000000000000000000000000000004000000000000000000000000000000"
	a := workerComparisonSummary(60000, 0)
	a.WorkRegistry = &WorkRegistryAnalysis{Snapshots: []WorkRegistrySnapshot{{Scope: oldScope, Process: "app", ObservedElapsedMS: 10, Unavailable: 1}, {Scope: newScope, Process: "app", ObservedElapsedMS: 20, Complete: true, IntervalMS: 30000, AgeAtCaptureMS: &age}}}
	row := compareWorkerDiagnostics(a, a, ComparisonScope{Comparability: ScenarioFull}).Registry[0]
	if !row.BaselineKnown || !row.Comparable {
		t.Fatalf("SDK restart confused with different process: %+v", row)
	}
}
