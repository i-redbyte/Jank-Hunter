package analyze

import (
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func TestComparisonSnapshotOmitsThreadDetailButPreservesCoverageAndCounts(t *testing.T) {
	large := strings.Repeat("x", comparisonSnapshotSummaryMaxBytes+1)
	summary := Summary{ThreadAnalysis: &ThreadAnalysis{DroppedSnapshots: 2,
		Resources:         []ThreadResourceObservation{{Source: large}},
		CoroutineSegments: []ThreadCoroutineSegment{{Owner: large}},
		Processes: []ThreadProcessAnalysis{{Process: "app", StartCount: 1, PeakCount: 3, LastCount: 2, PartialSnapshots: 1,
			Snapshots: []ThreadSnapshotPoint{{Snapshot: &jhlog.ThreadSnapshotEvent{IntervalMS: 5000, Threads: []jhlog.ThreadObservation{{Name: large}}}}},
			Histories: []ThreadHistory{{Observations: []ThreadHistoryPoint{{Row: jhlog.ThreadObservation{Name: large}}}}},
		}}}}
	snapshot, err := NewComparisonSnapshot(summary, "2026-10-06T12:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	restored, err := ValidateComparisonSnapshot(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	p := restored.ThreadAnalysis.Processes[0]
	if p.StartCount != 1 || p.PeakCount != 3 || p.LastCount != 2 || p.PartialSnapshots != 1 || restored.ThreadAnalysis.DroppedSnapshots != 2 {
		t.Fatal("lost sufficient statistics")
	}
	if len(p.Snapshots) != 0 || len(p.Histories) != 0 || !restored.ThreadAnalysis.HistoryOmitted || p.SamplingIntervalMS != 5000 {
		t.Fatal("retained detail or lost cadence")
	}
	if len(restored.ThreadAnalysis.Resources) != 0 || len(restored.ThreadAnalysis.CoroutineSegments) != 0 {
		t.Fatal("retained resource or coroutine histories in comparison snapshot")
	}
	if len(summary.ThreadAnalysis.Processes[0].Snapshots) != 1 || len(summary.ThreadAnalysis.Processes[0].Histories) != 1 {
		t.Fatal("mutated inspect history")
	}
	if len(summary.ThreadAnalysis.Resources) != 1 || len(summary.ThreadAnalysis.CoroutineSegments) != 1 {
		t.Fatal("mutated inspect resource or coroutine history")
	}
}
func TestComparisonSnapshotDoesNotInventUniformThreadCadence(t *testing.T) {
	summary := Summary{ThreadAnalysis: &ThreadAnalysis{Processes: []ThreadProcessAnalysis{{Snapshots: []ThreadSnapshotPoint{{Snapshot: &jhlog.ThreadSnapshotEvent{IntervalMS: 500}}, {Snapshot: &jhlog.ThreadSnapshotEvent{IntervalMS: 5000}}}}}}}
	compact := comparisonSnapshotSummary(summary)
	if compact.ThreadAnalysis.Processes[0].SamplingIntervalMS != 0 {
		t.Fatal("hidden collection cadence change")
	}
}

func TestComparisonSnapshotProjectsWorkerDetailsWithoutMutatingInspect(t *testing.T) {
	rows := []jhlog.WorkRegistryJob{{UUID: "job", State: 0, InitialDelayMS: 5000, NetworkType: "CONNECTED", Progress: []jhlog.WorkDataField{{Key: "large"}}, Output: []jhlog.WorkDataField{{Key: "output"}}}}
	summary := Summary{WorkerAnalysis: &WorkerAnalysis{Started: 3, Attempts: []WorkerAttempt{{UUID: "job"}}}, WorkRegistry: &WorkRegistryAnalysis{Snapshots: []WorkRegistrySnapshot{{Scope: "s", ObservedElapsedMS: 20, Jobs: rows}, {Scope: "s", ObservedElapsedMS: 10, Jobs: rows}}}}
	projected := comparisonSnapshotSummary(summary)
	if !projected.WorkerAnalysis.AttemptsOmitted || len(projected.WorkerAnalysis.Attempts) != 0 || projected.WorkerAnalysis.Started != 3 {
		t.Fatal("lost worker statistics")
	}
	if !projected.WorkRegistry.HistoryOmitted || len(projected.WorkRegistry.Snapshots) != 1 || projected.WorkRegistry.Snapshots[0].ObservedElapsedMS != 20 {
		t.Fatal("wrong latest registry")
	}
	p := projected.WorkRegistry.Snapshots[0]
	if !p.DataMetadataOmitted || len(p.Jobs[0].Progress) != 0 || p.Jobs[0].InitialDelayMS != 5000 || p.Jobs[0].NetworkType != "CONNECTED" {
		t.Fatal("lost schedule or retained irrelevant metadata")
	}
	if len(summary.WorkerAnalysis.Attempts) != 1 || len(summary.WorkRegistry.Snapshots) != 2 || len(rows[0].Progress) != 1 {
		t.Fatal("mutated inspect details")
	}
}
