package analyze

import (
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
	"math"
	"testing"
)

func TestThreadKernelStartKeepsTickAndCollectionBoundaryUncertainty(t *testing.T) {
	for _, tc := range []struct {
		ticks, hz, encodedStart uint64
		before                  *bool
		from, to                uint64
	}{
		{9, 100, 101, boolPointer(true), 90, 100},
		{10, 100, 101, nil, 100, 110},
		{11, 100, 101, boolPointer(false), 110, 120},
		{9, 100, 0, nil, 90, 100},
		{9, 100, 10002, nil, 90, 100},
		{1, 3, 1001, boolPointer(true), 333, 667},
	} {
		got := threadKernelStart(tc.ticks, tc.hz, 1000, tc.encodedStart, 1000, 5000)
		if got == nil || got.FromElapsedMS != tc.from || got.ByElapsedMS != tc.to {
			t.Fatalf("wrong tick interval: %+v", got)
		}
		if (got.BeforeCollection == nil) != (tc.before == nil) || got.BeforeCollection != nil && *got.BeforeCollection != *tc.before {
			t.Fatalf("invented pre-collection certainty: %+v", got)
		}
		if got.FromUnixMS == nil || *got.FromUnixMS != 4000+tc.from {
			t.Fatal("calendar anchor lost")
		}
	}
	for _, tc := range [][3]uint64{{1, 0, 1000}, {math.MaxUint64, 100, 1000}, {1001, 1, 1000}, {math.MaxUint64 - 1, 1, math.MaxUint64}} {
		if got := threadKernelStart(tc[0], tc[1], tc[2], 101, 1000, 5000); got != nil {
			t.Fatalf("accepted invalid or future start: %+v", got)
		}
	}
	if got := threadKernelStart(9, 100, 1000, 0, 0, 0); got == nil || got.FromUnixMS != nil || got.ByUnixMS != nil {
		t.Fatal("missing anchor became calendar time")
	}
}
func boolPointer(v bool) *bool { return &v }

func TestThreadOriginUsesRecordingProcessAndCollectionStartNotFirstSnapshot(t *testing.T) {
	s := threadCollectorState{snapshots: []threadRawSnapshot{{log: 1, time: 6000, snapshot: &jhlog.ThreadSnapshotEvent{ElapsedMS: 2000, TicksPerSecond: 100, Threads: []jhlog.ThreadObservation{{TID: 42, StartTicks: 9, Name: "renamed-main"}, {TID: 43, StartTicks: 150, Name: "main"}}}}}}
	s.finishLog(1, jhlog.StreamResult{Header: jhlog.SegmentHeader{OSPID: 42, SegmentStartElapsedUS: 1000000, SegmentStartUnixMS: 5000}, LatestQuality: &jhlog.QualitySnapshot{Counters: map[uint64]uint64{jhlog.QualityCollectionWindowStartElapsedMS: 1001}}})
	p := s.finalize().Processes[0]
	if p.Histories[0].MainThread == nil || !*p.Histories[0].MainThread || p.Histories[1].MainThread == nil || *p.Histories[1].MainThread {
		t.Fatal("thread name used as ownership evidence")
	}
	if !*p.Histories[0].KernelStart.BeforeCollection || *p.Histories[1].KernelStart.BeforeCollection {
		t.Fatal("first snapshot treated as start of collection")
	}
	if !p.Histories[1].PresentAtObservationStart {
		t.Fatal("first snapshot membership lost")
	}
	unknownState := threadCollectorState{snapshots: s.snapshots}
	unknown := unknownState.finalize()
	if unknown.Processes[0].Histories[0].MainThread != nil {
		t.Fatal("missing PID invented a non-main classification")
	}
}

func TestR8KernelOriginsIdentifyMainAndPreCollectionThreads(t *testing.T) {
	summary, err := InspectFilesWithOptions("R8 origins", []string{"../../../wire/testdata/work-submission-r8-5.1.0.jhlog"}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if summary.ThreadAnalysis == nil {
		t.Fatal("no thread evidence")
	}
	var main, before int
	for _, p := range summary.ThreadAnalysis.Processes {
		for _, h := range p.Histories {
			if h.MainThread != nil && *h.MainThread {
				main++
			}
			if h.KernelStart != nil && h.KernelStart.BeforeCollection != nil && *h.KernelStart.BeforeCollection {
				before++
			}
		}
	}
	if main != 1 || before == 0 {
		t.Fatalf("main=%d precollection=%d", main, before)
	}
}
