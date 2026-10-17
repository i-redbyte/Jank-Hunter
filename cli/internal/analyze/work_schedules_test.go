package analyze

import (
	"fmt"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
	"testing"
)

func scheduleNumber(n uint64) *uint64 { return &n }
func TestSchedulesSeparateRequestsRegistryAndRetryCadence(t *testing.T) {
	p, f := uint64(1800000), uint64(300000)
	s := Summary{WorkSubmissions: &WorkSubmissionAnalysis{Observations: []WorkSubmissionObservation{
		{UUID: "id", Scope: "s", Process: "app", TimeMS: 100, Submission: &jhlog.WorkSubmissionEvent{Version: 4, Periodic: true, CallerSource: "app.schedule", RequestedPeriodMS: &p, RequestedFlexMS: &f}},
	}}, WorkRegistry: &WorkRegistryAnalysis{Snapshots: []WorkRegistrySnapshot{
		{Scope: "other", ObservedElapsedMS: 500, Jobs: []jhlog.WorkRegistryJob{{UUID: "id", PeriodMS: scheduleNumber(1)}}},
		{Scope: "s", Process: "app", ObservedElapsedMS: 200, Complete: true, Jobs: []jhlog.WorkRegistryJob{{UUID: "id", PeriodMS: scheduleNumber(3600000), FlexMS: &f}}},
	}}, WorkerAnalysis: &WorkerAnalysis{Attempts: []WorkerAttempt{
		{Scope: "s", Process: "app", UUID: "id", StartMS: scheduleNumber(1000), Worker: "app.Worker"},
		{Scope: "s", Process: "app", UUID: "id", StartMS: scheduleNumber(1100), RunAttempt: 1},
		{Scope: "s", Process: "app", UUID: "id", StartMS: scheduleNumber(2000)},
		{Scope: "s", Process: "app", UUID: "id", StartMS: scheduleNumber(9000), Generation: 1},
	}}}
	a := BuildWorkSchedules(s)
	var row *WorkScheduleRow
	for i := range a.Rows {
		if a.Rows[i].Scope == "s" {
			row = &a.Rows[i]
		}
	}
	if row == nil || row.Requested == nil || row.Registered == nil {
		t.Fatalf("missing layers: %+v", a)
	}
	if *row.Registered.PeriodMS != 3600000 || row.Cadence.Count != 0 || row.GenerationCadences[0].Cadence.Count != 1 || row.GenerationCadences[0].Cadence.MedianMS != 1000 || row.RetryStarts != 1 {
		t.Fatalf("mixed identity/generation/retry: %+v", row)
	}
	if !row.ParametersDiffer {
		t.Fatal("lost requested/registered difference")
	}
}
func TestSchedulesDoNotInventPeriodFromRepeatedOneTimeEnqueues(t *testing.T) {
	s := Summary{WorkSubmissions: &WorkSubmissionAnalysis{}}
	for i := uint64(0); i < 3; i++ {
		s.WorkSubmissions.Observations = append(s.WorkSubmissions.Observations, WorkSubmissionObservation{
			UUID: string(rune('a' + i)), Scope: "s", Process: "app", TimeMS: 1000 + i*75, Submission: &jhlog.WorkSubmissionEvent{Version: 2, CallerSource: "app.enqueue", HasUnique: true, UniqueName: "config", Policy: jhlog.WorkSubmissionReplace},
		})
	}
	a := BuildWorkSchedules(s)
	if len(a.Findings) != 1 || a.Findings[0].Kind != "replacement_churn" || a.Findings[0].Count != 3 {
		t.Fatalf("missing enqueue evidence: %+v", a.Findings)
	}
	for _, r := range a.Rows {
		if r.Requested.RequestedPeriodMS != nil || r.Cadence.Count != 0 {
			t.Fatal("invented schedule")
		}
	}
}
func TestSchedulesKeepMissingAndAmbiguousEvidenceUnknown(t *testing.T) {
	s := Summary{WorkRegistry: &WorkRegistryAnalysis{Snapshots: []WorkRegistrySnapshot{{Scope: "s", Unavailable: 1}}}, WorkerAnalysis: &WorkerAnalysis{Attempts: []WorkerAttempt{
		{Scope: "s", Process: "app", UUID: "id", StartMS: scheduleNumber(1), IdentityAmbiguous: true},
		{Scope: "s", Process: "app", UUID: "id", StartMS: scheduleNumber(2)},
	}}}
	a := BuildWorkSchedules(s)
	if a.RegistryUnavailable != 1 || len(a.Rows) != 1 || a.Rows[0].Cadence.Count != 0 || a.Rows[0].Requested != nil {
		t.Fatalf("fabricated evidence: %+v", a)
	}
}

func TestSchedulesBoundLargeCaptureAndKeepInputsImmutable(t *testing.T) {
	s := Summary{WorkSubmissions: &WorkSubmissionAnalysis{}}
	for i := 0; i < 1000; i++ {
		s.WorkSubmissions.Observations = append(s.WorkSubmissions.Observations, WorkSubmissionObservation{Scope: "s", UUID: fmt.Sprint(i), Submission: &jhlog.WorkSubmissionEvent{ObservedElapsedNanos: uint64(1000 - i), Version: 2, CallerSource: "app.schedule"}})
	}
	a := BuildWorkSchedules(s)
	if len(a.Rows) != scheduleRowLimit || a.OmittedRows != 1000-scheduleRowLimit {
		t.Fatalf("unbounded rows: %d / %d", len(a.Rows), a.OmittedRows)
	}
	if s.WorkSubmissions.Observations[0].UUID != "0" || s.WorkSubmissions.Observations[0].Submission.ObservedElapsedNanos != 1000 {
		t.Fatal("derived view mutated input")
	}
}
func TestSchedulesNeverTreatStaleOrIncompleteEligibilityAsCoScheduling(t *testing.T) {
	when := int64(1000)
	for _, snap := range []WorkRegistrySnapshot{
		{Scope: "s", Process: "app", Complete: false, AgeAtCaptureMS: scheduleNumber(1), IntervalMS: 10},
		{Scope: "s", Process: "app", Complete: true, AgeAtCaptureMS: scheduleNumber(1000), IntervalMS: 10},
	} {
		snap.Jobs = []jhlog.WorkRegistryJob{{UUID: "a", State: 0, EarliestEligibleUnixMS: &when}, {UUID: "b", State: 0, EarliestEligibleUnixMS: &when}}
		a := BuildWorkSchedules(Summary{WorkRegistry: &WorkRegistryAnalysis{Snapshots: []WorkRegistrySnapshot{snap}}})
		if len(a.Findings) != 0 {
			t.Fatal("incomplete/stale evidence became co-scheduling recommendation")
		}
	}
}
func BenchmarkWorkSchedulesLargeCapture(b *testing.B) {
	s := Summary{WorkSubmissions: &WorkSubmissionAnalysis{}}
	for i := 0; i < 50000; i++ {
		s.WorkSubmissions.Observations = append(s.WorkSubmissions.Observations, WorkSubmissionObservation{Scope: "s", UUID: fmt.Sprint(i % 512), Submission: &jhlog.WorkSubmissionEvent{Version: 2, CallerSource: "app.schedule", ObservedElapsedNanos: uint64(i)}})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		BuildWorkSchedules(s)
	}
}

func TestScheduleComparisonIncludesDefaultWindowAndRequestKind(t *testing.T) {
	period := uint64(1800000)
	p := &jhlog.WorkSubmissionEvent{Version: 4, Periodic: true, RequestedPeriodMS: &period}
	j := &jhlog.WorkRegistryJob{PeriodMS: &period, FlexMS: scheduleNumber(300000)}
	if !scheduleRequestedDiffers(p, j) {
		t.Fatal("default full-period window was treated as matching an explicit narrow window")
	}
	j.FlexMS = &period
	if scheduleRequestedDiffers(p, j) {
		t.Fatal("matching default window was reported as different")
	}
	if !scheduleRequestedDiffers(&jhlog.WorkSubmissionEvent{Periodic: false}, j) {
		t.Fatal("request kind mismatch hidden")
	}
}

func TestSchedulesDistinguishObservedEmptyRegistryFromUnavailableSource(t *testing.T) {
	a := BuildWorkSchedules(Summary{WorkRegistry: &WorkRegistryAnalysis{Snapshots: []WorkRegistrySnapshot{{Scope: "s", Complete: true, TotalObserved: 0}, {Scope: "s", Unavailable: 1}}}})
	if a.EmptyCompleteSnapshots != 1 || a.RegistryUnavailableReasons[1] != 1 {
		t.Fatalf("empty observation became unknown: %+v", a)
	}
}

func TestScheduleChurnHonorsNanosecondBoundary(t *testing.T) {
	s := Summary{WorkSubmissions: &WorkSubmissionAnalysis{}}
	for i, at := range []uint64{1, 1000000002} {
		s.WorkSubmissions.Observations = append(s.WorkSubmissions.Observations, WorkSubmissionObservation{Scope: "s", UUID: fmt.Sprint(i), Submission: &jhlog.WorkSubmissionEvent{HasUnique: true, UniqueName: "config", Policy: jhlog.WorkSubmissionReplace, ObservedElapsedNanos: at}})
	}
	if findings := BuildWorkSchedules(s).Findings; len(findings) != 0 {
		t.Fatalf("rounded timestamps created false churn: %+v", findings)
	}
}
