package analyze

import (
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
	"testing"
)

func TestThreadAnnotationsPreserveScopeAndDoNotInventCausality(t *testing.T) {
	c := newCollector("annotations", 1, Options{})
	c.startLog(jhlog.SegmentHeader{})
	c.add(map[uint64]string{1: "refresh"}, jhlog.Event{Type: jhlog.EventOperation, TimeMS: 15, Operation: &jhlog.OperationEvent{ID: 42, NameRef: jhlog.LocalSymbol(1), Phase: jhlog.OperationPhaseStarted}})
	c.add(nil, jhlog.Event{Type: jhlog.EventProcessState, TimeMS: 17, ProcessState: &jhlog.ProcessStateEvent{UIVisibility: jhlog.ProcessUIHidden}})
	c.add(nil, jhlog.Event{Type: jhlog.EventUIWindow, TimeMS: 20, UIWindow: &jhlog.UIWindowEvent{WindowMS: 5, JankCount: 2}})
	c.add(nil, jhlog.Event{Type: jhlog.EventThreadSnapshot, TimeMS: 25, DiagnosticEvent: &jhlog.DiagnosticEvent{ThreadSnapshot: &jhlog.ThreadSnapshotEvent{Version: 1}}})
	a := c.threadAnalysis.finalize()
	if len(a.Annotations) != 3 {
		t.Fatalf("missing timeline evidence: %+v", a.Annotations)
	}
	for _, mark := range a.Annotations {
		if mark.Scope != a.Processes[0].Scope {
			t.Fatal("annotation lost recording scope")
		}
	}
	if a.Annotations[0].OperationID != "42" || a.Annotations[0].Label != "refresh" || a.Annotations[2].DurationMS != 5 {
		t.Fatal("annotation semantics lost")
	}
	compact := comparisonThreadSummary(a)
	if len(compact.Annotations) != 0 || len(a.Annotations) != 3 {
		t.Fatal("portable summary retains or mutates detailed annotations")
	}
}
func TestThreadAnnotationsBoundHistoryAndIgnoreUnrelatedEvents(t *testing.T) {
	c := &collector{}
	for i := 0; i < 6000; i++ {
		c.recordThreadAnnotation(nil, jhlog.Event{ProcessState: &jhlog.ProcessStateEvent{}})
	}
	if len(c.threadAnalysis.annotations) != 5000 || c.threadAnalysis.droppedAnnotations != 1000 {
		t.Fatal("unbounded annotation history")
	}
	c.recordThreadAnnotation(nil, jhlog.Event{Memory: &jhlog.MemoryEvent{}})
	if c.threadAnalysis.droppedAnnotations != 1000 {
		t.Fatal("unrelated event counted as annotation loss")
	}
}

func TestThreadLastSnapshotAgeUsesMonotonicCaptureBoundary(t *testing.T) {
	s := threadCollectorState{snapshots: []threadRawSnapshot{{log: 1, time: 1000, snapshot: &jhlog.ThreadSnapshotEvent{ElapsedMS: 100, Complete: true}}}}
	s.finishLog(1, jhlog.StreamResult{Header: jhlog.SegmentHeader{SegmentStartElapsedUS: 100000, SegmentStartUnixMS: 1000}, LatestQuality: &jhlog.QualitySnapshot{CapturedElapsedUS: 150000}})
	p := s.finalize().Processes[0]
	if p.LastSnapshotAgeMS == nil || *p.LastSnapshotAgeMS != 50 {
		t.Fatalf("unknown or incorrect sample freshness: %+v", p)
	}
	if p.ObservationEndMS == nil || *p.ObservationEndMS != 1050 {
		t.Fatal("capture boundary lacks clock mapping")
	}
	s2 := threadCollectorState{snapshots: s.snapshots}
	if s2.finalize().Processes[0].LastSnapshotAgeMS != nil {
		t.Fatal("missing capture boundary became zero age")
	}
}

func TestThreadAnnotationUsesSameClockAsSnapshots(t *testing.T) {
	c := &collector{}
	c.startDiagnosticSegment(jhlog.SegmentHeader{SegmentStartElapsedUS: 100000, SegmentStartUnixMS: 1000000})
	c.recordThreadAnnotation(nil, jhlog.Event{TimeMS: 150, ProcessState: &jhlog.ProcessStateEvent{UIVisibility: jhlog.ProcessUIVisible}})
	got := c.threadAnalysis.annotations[0].value
	if got.TimeMS != 1000050 || got.TimeBasis != "observation_clock" {
		t.Fatalf("elapsed time put on calendar axis: %+v", got)
	}
}

func TestR8ThreadAndWorkerTimelineShareCalendarAxis(t *testing.T) {
	summary, err := InspectFilesWithOptions("R8 timeline", []string{"../../../wire/testdata/thread-lifecycle-r8-5.1.0.jhlog"}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	a := summary.ThreadAnalysis
	if a == nil || len(a.Processes) == 0 || len(a.Annotations) == 0 || summary.WorkerAnalysis == nil {
		t.Fatal("missing fixture evidence")
	}
	p := a.Processes[0]
	var linked bool
	for _, attempt := range summary.WorkerAnalysis.Attempts {
		if attempt.Scope != p.Scope || attempt.StartUnixMS == nil {
			continue
		}
		for _, snap := range p.Snapshots {
			distance := float64(*attempt.StartUnixMS) - float64(snap.TimeMS)
			if distance < 0 {
				distance = -distance
			}
			if distance <= float64(snap.Snapshot.IntervalMS+snap.Snapshot.ScanDurationMS) {
				linked = true
			}
		}
	}
	if !linked {
		t.Fatal("real worker and snapshot timestamps cannot be compared")
	}
	for _, mark := range a.Annotations {
		if mark.TimeBasis != "observation_clock" || mark.TimeMS < 1700000000000 {
			t.Fatalf("elapsed timestamp on calendar plot: %+v", mark)
		}
	}
}

func TestThreadAnnotationsSurviveWithoutThreadSnapshots(t *testing.T) {
	c := newCollector("annotations-only", 1, Options{})
	c.startLog(jhlog.SegmentHeader{})
	c.add(nil, jhlog.Event{Type: jhlog.EventProcessState, TimeMS: 17, ProcessState: &jhlog.ProcessStateEvent{UIVisibility: jhlog.ProcessUIHidden}})
	a := c.threadAnalysis.finalize()
	if a == nil || len(a.Annotations) != 1 || a.Annotations[0].Kind != "ui_visibility" {
		t.Fatalf("annotation-only capture lost: %+v", a)
	}
	if len(a.Processes) != 0 {
		t.Fatal("annotations invented thread snapshots")
	}
}

func TestThreadAnnotationLossSurvivesWithoutRetainedEvents(t *testing.T) {
	s := threadCollectorState{droppedAnnotations: 2}
	a := s.finalize()
	if a == nil || a.DroppedAnnotations != 2 {
		t.Fatalf("annotation loss vanished: %+v", a)
	}
}
