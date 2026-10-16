package analyze

import (
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
	"testing"
)

func dynamicsPoint(time uint64, complete bool, count int) ThreadSnapshotPoint {
	rows := make([]jhlog.ThreadObservation, count)
	for i := range rows {
		rows[i] = jhlog.ThreadObservation{TID: uint64(i + 1), StartTicks: 1, Name: "pool", OSState: "S"}
	}
	return ThreadSnapshotPoint{TimeMS: time, TimeBasis: "observation_clock", Snapshot: &jhlog.ThreadSnapshotEvent{ElapsedMS: time, Complete: complete, IntervalMS: 1000, Threads: rows}}
}
func TestThreadDynamicsUsesMeasuredExposureAndKeepsMissingCPUUnknown(t *testing.T) {
	p := ThreadProcessAnalysis{Appearances: 4, Snapshots: []ThreadSnapshotPoint{dynamicsPoint(1000, true, 1), dynamicsPoint(3000, true, 5)}}
	d := summarizeThreadDynamics(p)
	if d.WindowMS != 2000 || d.AppearancesPerMinute == nil || *d.AppearancesPerMinute != 120 || d.MeanObservedThreadCorePercent != nil {
		t.Fatalf("invented CPU or wrong exposure: %+v", d)
	}
	p.Histories = []ThreadHistory{{Observations: []ThreadHistoryPoint{{CPU: &ThreadCPUDelta{ElapsedMS: 1000, CPUTimeMS: 0}}, {CPU: &ThreadCPUDelta{ElapsedMS: 2000, CPUTimeMS: 900, CorePercent: 45}}}}}
	d = summarizeThreadDynamics(p)
	if d.CPUSamples != 2 || d.ObservedThreadTimeMS != 3000 || d.CPUTimeMS != 900 || d.MeanObservedThreadCorePercent == nil || *d.MeanObservedThreadCorePercent != 30 {
		t.Fatalf("incorrect measured CPU: %+v", d)
	}
}
func TestThreadGrowthSignalRequiresConsecutiveCompleteObservations(t *testing.T) {
	p := ThreadProcessAnalysis{Snapshots: []ThreadSnapshotPoint{dynamicsPoint(1000, true, 1), dynamicsPoint(2000, true, 3), dynamicsPoint(3000, true, 5), dynamicsPoint(4000, true, 7)}}
	d := summarizeThreadDynamics(p)
	if len(d.Signals) != 1 || d.Signals[0].Code != "observed_growth" || d.Signals[0].Limitation == "" {
		t.Fatalf("growth evidence missing: %+v", d.Signals)
	}
	p.Snapshots[1].Snapshot.Complete = false
	if len(summarizeThreadDynamics(p).Signals) != 0 {
		t.Fatal("partial snapshots proved growth")
	}
	p.Snapshots[1].Snapshot.Complete = true
	p.Snapshots[2].Snapshot.ElapsedMS = 10000
	if len(summarizeThreadDynamics(p).Signals) != 0 {
		t.Fatal("clock rollback or gap proved sustained growth")
	}
}
func TestThreadCompositionIsBoundedAndNeverInventsPoolOwnership(t *testing.T) {
	p := ThreadProcessAnalysis{Snapshots: []ThreadSnapshotPoint{dynamicsPoint(1, true, 300)}}
	for i := range p.Snapshots[0].Snapshot.Threads {
		p.Snapshots[0].Snapshot.Threads[i].Name = string(rune(1000 + i))
	}
	d := summarizeThreadDynamics(p)
	if len(d.LastNames) != 200 || d.OmittedNames != 100 {
		t.Fatalf("unbounded composition: %+v", d)
	}
}
func TestThreadSignalsBoundHighCPUAndKeepOSWaitDistinct(t *testing.T) {
	p := ThreadProcessAnalysis{}
	for i := 0; i < 60; i++ {
		p.Histories = append(p.Histories, ThreadHistory{TID: uint64(i + 1), Observations: []ThreadHistoryPoint{{TimeMS: 2000, CPU: &ThreadCPUDelta{ElapsedMS: 1000, CPUTimeMS: 900, CorePercent: 90}, Row: jhlog.ThreadObservation{OSState: "D"}}}})
	}
	d := summarizeThreadDynamics(p)
	if len(d.Signals) != 50 || d.OmittedSignals != 10 {
		t.Fatalf("signal budget violated: %d/%d", len(d.Signals), d.OmittedSignals)
	}
	for _, s := range d.Signals {
		if s.Code != "measured_cpu" || s.Limitation == "" {
			t.Fatalf("wrong signal: %+v", s)
		}
	}
}

func TestThreadDynamicsSurvivesPortableComparisonProjection(t *testing.T) {
	p := ThreadProcessAnalysis{Snapshots: []ThreadSnapshotPoint{dynamicsPoint(1000, true, 1), dynamicsPoint(2000, true, 2)}, Appearances: 1}
	p.Dynamics = summarizeThreadDynamics(p)
	compact := comparisonThreadSummary(&ThreadAnalysis{Processes: []ThreadProcessAnalysis{p}})
	d := compact.Processes[0].Dynamics
	if d == nil || d.WindowMS != 1000 || len(d.LastNames) != 1 || d.LastNames[0].Count != 2 || d.MeanObservedThreadCorePercent != nil {
		t.Fatalf("lost statistics: %+v", d)
	}
}

func TestThreadBurstAndRepeatedOSWaitHaveDistinctEvidence(t *testing.T) {
	p := ThreadProcessAnalysis{Snapshots: []ThreadSnapshotPoint{dynamicsPoint(1000, true, 1), dynamicsPoint(2000, true, 11)}}
	p.Histories = []ThreadHistory{{TID: 42, StartTicks: 7, Observations: []ThreadHistoryPoint{
		{TimeMS: 1000, ElapsedMS: 1000, Row: jhlog.ThreadObservation{OSState: "D"}},
		{TimeMS: 2000, ElapsedMS: 2000, Row: jhlog.ThreadObservation{OSState: "D"}},
		{TimeMS: 3000, ElapsedMS: 3000, Row: jhlog.ThreadObservation{OSState: "D"}},
	}}}
	d := summarizeThreadDynamics(p)
	found := map[string]bool{}
	for _, s := range d.Signals {
		found[s.Code] = true
	}
	if !found["observed_burst"] || !found["repeated_os_wait"] {
		t.Fatalf("missing evidence: %+v", d.Signals)
	}
	p.Histories[0].Observations[1].Row.OSState = "S"
	p.Snapshots[0].Snapshot.Complete = false
	if len(summarizeThreadDynamics(p).Signals) != 0 {
		t.Fatal("partial burst or interrupted OS wait claimed")
	}
}

func BenchmarkThreadDynamics100000Observations(b *testing.B) {
	p := ThreadProcessAnalysis{}
	for t := 0; t < 1000; t++ {
		h := ThreadHistory{TID: uint64(t + 1)}
		for n := 0; n < 100; n++ {
			h.Observations = append(h.Observations, ThreadHistoryPoint{TimeMS: uint64(n * 1000), ElapsedMS: uint64(n * 1000), CPU: &ThreadCPUDelta{ElapsedMS: 1000, CPUTimeMS: 10, CorePercent: 1}, Row: jhlog.ThreadObservation{OSState: "S"}})
		}
		p.Histories = append(p.Histories, h)
	}
	for n := 0; n < 100; n++ {
		p.Snapshots = append(p.Snapshots, dynamicsPoint(uint64(n*1000), true, 1000))
	}
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		if summarizeThreadDynamics(p).CPUSamples != 100000 {
			b.Fatal("lost observations")
		}
	}
}
