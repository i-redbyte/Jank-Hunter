package analyze

import (
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
	"strings"
	"testing"
)

func TestThreadAnalysisPreservesUncertaintyAndTidGeneration(t *testing.T) {
	row := func(tid, start uint64, name string) jhlog.ThreadObservation {
		return jhlog.ThreadObservation{TID: tid, StartTicks: start, Name: name, OSState: "S"}
	}
	event := func(time uint64, complete bool, rows ...jhlog.ThreadObservation) jhlog.Event {
		return jhlog.Event{Type: jhlog.EventThreadSnapshot, TimeMS: time, DiagnosticEvent: &jhlog.DiagnosticEvent{ThreadSnapshot: &jhlog.ThreadSnapshotEvent{Version: 1, Complete: complete, Threads: rows}}}
	}
	summary := inspectLogsForTest("threads", []jhlog.Log{{Events: []jhlog.Event{
		event(10, true, row(1, 1, "main"), row(2, 1, "pool")),
		event(20, false, row(1, 1, "main")),
		event(30, true, row(1, 1, "renamed"), row(2, 2, "new")),
	}}})
	a := summary.ThreadAnalysis
	if a == nil || len(a.Processes) != 1 {
		t.Fatalf("missing analysis: %+v", a)
	}
	p := a.Processes[0]
	if p.StartCount != 2 || p.PeakCount != 2 || p.LastCount != 2 || p.Appearances != 1 || p.Disappearances != 1 || p.PartialSnapshots != 1 {
		t.Fatalf("bad summary: %+v", p)
	}
	if len(p.Histories) != 3 {
		t.Fatalf("generations merged: %+v", p.Histories)
	}
	if p.Histories[1].DisappearedAfterMS != 10 || p.Histories[1].DisappearedByMS != 30 {
		t.Fatalf("invented disappearance: %+v", p.Histories[1])
	}
}

func TestOldLogsDoNotInventZeroThreadTelemetry(t *testing.T) {
	if inspectLogsForTest("old", []jhlog.Log{{}}).ThreadAnalysis != nil {
		t.Fatal("invented thread coverage")
	}
}

func TestThreadHistoryAlsoBoundsVariableJavaMetadataBytes(t *testing.T) {
	c := &collector{}
	rows := make([]jhlog.ThreadObservation, 2048)
	for i := range rows {
		rows[i] = jhlog.ThreadObservation{TID: uint64(i + 1), Name: strings.Repeat("n", 1024), Java: &jhlog.JavaThreadObservation{ID: uint64(i + 1), Name: strings.Repeat("j", 1024), State: "RUNNABLE", Source: strings.Repeat("s", 1024)}}
	}
	for i := 0; i < 10; i++ {
		c.recordThreadSnapshot(jhlog.Event{DiagnosticEvent: &jhlog.DiagnosticEvent{ThreadSnapshot: &jhlog.ThreadSnapshotEvent{Version: 2, Threads: rows}}})
	}
	if c.threadAnalysis.dropped == 0 || len(c.threadAnalysis.snapshots) == 10 {
		t.Fatal("bounded row count retained unbounded variable-width metadata")
	}
}

func TestThreadDisappearanceIncludesNonAtomicScanDuration(t *testing.T) {
	s := threadCollectorState{snapshots: []threadRawSnapshot{
		{time: 100, timeBasis: "observation_clock", snapshot: &jhlog.ThreadSnapshotEvent{Complete: true, ElapsedMS: 10, ScanDurationMS: 5, Threads: []jhlog.ThreadObservation{{TID: 1, StartTicks: 1}}}},
		{time: 200, timeBasis: "observation_clock", snapshot: &jhlog.ThreadSnapshotEvent{Complete: true, ElapsedMS: 110, ScanDurationMS: 17}},
	}}
	h := s.finalize().Processes[0].Histories[0]
	if h.DisappearedAfterMS != 100 || h.DisappearedByMS != 217 {
		t.Fatalf("scan start incorrectly used as disappearance deadline: %+v", h)
	}
}

func TestThreadReappearanceRetainsGenerationAfterCompleteGap(t *testing.T) {
	row := jhlog.ThreadObservation{TID: 7, StartTicks: 42}
	s := threadCollectorState{snapshots: []threadRawSnapshot{
		{time: 100, snapshot: &jhlog.ThreadSnapshotEvent{Complete: true, Threads: []jhlog.ThreadObservation{row}}},
		{time: 200, snapshot: &jhlog.ThreadSnapshotEvent{Complete: true}},
		{time: 300, snapshot: &jhlog.ThreadSnapshotEvent{Complete: true, Threads: []jhlog.ThreadObservation{row}}},
	}}
	p := s.finalize().Processes[0]
	if len(p.Histories) != 1 || len(p.Histories[0].Observations) != 2 || p.Disappearances != 0 || p.Histories[0].DisappearedByMS != 0 {
		t.Fatalf("reappearing generation was lost: %+v", p)
	}
}

func BenchmarkThreadAnalysisInactiveHistory(b *testing.B) {
	rows := make([]jhlog.ThreadObservation, 2000)
	for i := range rows {
		rows[i] = jhlog.ThreadObservation{TID: uint64(i + 1), StartTicks: 1}
	}
	snapshots := []threadRawSnapshot{{time: 1, snapshot: &jhlog.ThreadSnapshotEvent{Complete: true, Threads: rows}}}
	for i := 0; i < 200; i++ {
		snapshots = append(snapshots, threadRawSnapshot{time: uint64(i + 2), snapshot: &jhlog.ThreadSnapshotEvent{Complete: true}})
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		state := threadCollectorState{snapshots: snapshots}
		_ = state.finalize()
	}
}

func TestThreadSnapshotsUseMonotonicOrderAcrossCalendarClockRollback(t *testing.T) {
	s := threadCollectorState{snapshots: []threadRawSnapshot{
		{time: 1000, timeBasis: "observation_clock", snapshot: &jhlog.ThreadSnapshotEvent{Complete: true, ElapsedMS: 100, TicksPerSecond: 100, Threads: []jhlog.ThreadObservation{{TID: 7, StartTicks: 1, UserTicks: 1}}}},
		{time: 900, timeBasis: "observation_clock", snapshot: &jhlog.ThreadSnapshotEvent{Complete: true, ElapsedMS: 200, TicksPerSecond: 100, Threads: []jhlog.ThreadObservation{{TID: 7, StartTicks: 1, UserTicks: 3}, {TID: 8, StartTicks: 2}}}},
	}}
	p := s.finalize().Processes[0]
	if p.StartCount != 1 || p.LastCount != 2 || p.Appearances != 1 || p.Disappearances != 0 {
		t.Fatalf("calendar adjustment inverted thread history: %+v", p)
	}
	h := p.Histories[0]
	if len(h.Observations) != 2 || h.Observations[1].CPU == nil || h.Observations[1].CPU.CPUTimeMS != 20 {
		t.Fatalf("calendar adjustment lost monotonic CPU delta: %+v", h)
	}
}
