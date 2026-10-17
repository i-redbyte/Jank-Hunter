package analyze

import (
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
	"testing"
)

func TestAttemptJobObservationDoesNotInventAttemptCompletionOrRunning(t *testing.T) {
	age := uint64(25)
	at := uint64(200)
	a := &WorkerAnalysis{Attempts: []WorkerAttempt{{UUID: "id", Scope: "session", Outcome: "unknown", Coverage: "missing_finish", RunAttempt: 0}}}
	registry := &WorkRegistryAnalysis{Snapshots: []WorkRegistrySnapshot{{Scope: "session", ObservedElapsedMS: 20, ObservedUnixMS: &at, AgeAtCaptureMS: &age, IntervalMS: 10, Jobs: []jhlog.WorkRegistryJob{{UUID: "id", State: 1, RunAttempt: 1}}}}}
	attachWorkerJobObservations(a, registry)
	got := a.Attempts[0]
	if got.LastJobObservation == nil || got.LastJobObservation.State != 1 || got.LastJobObservation.Freshness != "stale" {
		t.Fatalf("lost positive but stale job observation: %+v", got)
	}
	if got.EndMS != nil || got.DurationMS != nil || got.Outcome != "unknown" || got.Coverage != "missing_finish" {
		t.Fatal("job observation invented attempt lifecycle")
	}
}
func TestAttemptJobObservationKeepsOnlySameSessionProgressMetadata(t *testing.T) {
	bytes := uint64(4)
	a := &WorkerAnalysis{Attempts: []WorkerAttempt{{UUID: "id", Scope: "session"}}}
	registry := &WorkRegistryAnalysis{Snapshots: []WorkRegistrySnapshot{
		{Scope: "other", ObservedElapsedMS: 30, Jobs: []jhlog.WorkRegistryJob{{UUID: "id", Progress: []jhlog.WorkDataField{{Key: "wrong", Type: "string"}}}}},
		{Scope: "session", ObservedElapsedMS: 20, Jobs: []jhlog.WorkRegistryJob{{UUID: "id", Progress: []jhlog.WorkDataField{{Key: "done", Type: "int", PayloadBytes: &bytes}}, OmittedProgressKeys: 2}}},
	}}
	attachWorkerJobObservations(a, registry)
	got := a.Attempts[0].LastJobObservation
	if got == nil || len(got.Progress) != 1 || got.Progress[0].Key != "done" || got.OmittedProgressKeys != 2 {
		t.Fatalf("progress metadata lost or taken from another session: %+v", got)
	}
}
func TestAttemptJobObservationSeparatesScopesAndRejectsConflict(t *testing.T) {
	a := &WorkerAnalysis{Attempts: []WorkerAttempt{{UUID: "id", Scope: "one"}, {UUID: "id", Scope: "two"}, {UUID: "id", Scope: "three"}, {UUID: "id", Scope: "one", IdentityAmbiguous: true}}}
	registry := &WorkRegistryAnalysis{Snapshots: []WorkRegistrySnapshot{
		{Scope: "one", ObservedElapsedMS: 10, Jobs: []jhlog.WorkRegistryJob{{UUID: "id", State: 1}}},
		{Scope: "two", ObservedElapsedMS: 20, CoverageReason: "conflicting_packets", Jobs: []jhlog.WorkRegistryJob{{UUID: "id", State: 2}}},
	}}
	attachWorkerJobObservations(a, registry)
	if a.Attempts[0].LastJobObservation == nil {
		t.Fatal("missing captured job")
	}
	for _, a := range a.Attempts[1:] {
		if a.LastJobObservation != nil {
			t.Fatal("cross-session or ambiguous evidence")
		}
	}
	if a.Attempts[0].LastJobObservation.Freshness != "unknown" {
		t.Fatal("unknown age became fresh")
	}
}
func TestAttemptJobObservationKeepsLastPositiveWhenLaterSnapshotOmitsJob(t *testing.T) {
	age := uint64(5)
	a := &WorkerAnalysis{Attempts: []WorkerAttempt{{UUID: "id", Scope: "one"}}}
	registry := &WorkRegistryAnalysis{Snapshots: []WorkRegistrySnapshot{
		{Scope: "one", ObservedElapsedMS: 20, IntervalMS: 10, AgeAtCaptureMS: &age, Jobs: []jhlog.WorkRegistryJob{{UUID: "id", State: 0}}},
		{Scope: "one", ObservedElapsedMS: 10, Jobs: []jhlog.WorkRegistryJob{{UUID: "id", State: 1}}},
		{Scope: "one", ObservedElapsedMS: 30, Complete: true},
	}}
	attachWorkerJobObservations(a, registry)
	got := a.Attempts[0].LastJobObservation
	if got == nil || got.State != 0 || got.Freshness != "fresh" || !got.AbsentFromLaterCompleteSnapshot {
		t.Fatalf("lost uncertainty after disappearance: %+v", got)
	}
}
