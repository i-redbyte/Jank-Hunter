package analyze

import (
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
	"testing"
)

func TestShortLivedRunObservationSurvivesWithoutAnySystemSnapshot(t *testing.T) {
	e := jhlog.Event{Type: jhlog.EventThreadLifecycle, TimeMS: 10, Producer: jhlog.ProducerMetadata{HasThread: true, ThreadID: 42}, DiagnosticEvent: &jhlog.DiagnosticEvent{ThreadLifecycle: &jhlog.ThreadLifecycleEvent{Version: 1, Stage: jhlog.ThreadRunObserved, TargetTID: 42, JavaID: 7, Name: "short", Source: "Task.run"}}}
	s := inspectLogsForTest("short", []jhlog.Log{{Events: []jhlog.Event{e}}})
	if s.ThreadAnalysis == nil || len(s.ThreadAnalysis.Lifecycle) != 1 {
		t.Fatal("lost thread between scans")
	}
	if len(s.ThreadAnalysis.Processes) != 0 {
		t.Fatal("run observation invented a system snapshot")
	}
	o := s.ThreadAnalysis.Lifecycle[0]
	if o.TimeMS != 10 || o.TimeBasis != "record_time" || o.RecordingTID != "42" || o.ContextCaptured {
		t.Fatalf("invented provenance: %+v", o)
	}
}
func TestThreadLifecycleCLIHistoryIsBoundedIndependentlyOfSnapshots(t *testing.T) {
	c := &collector{}
	for i := 0; i < 100010; i++ {
		c.recordThreadLifecycle(jhlog.Event{DiagnosticEvent: &jhlog.DiagnosticEvent{ThreadLifecycle: &jhlog.ThreadLifecycleEvent{Version: 1, Stage: jhlog.ThreadRunObserved, TargetTID: 42}}})
	}
	a := c.threadAnalysis.finalize()
	if a == nil || len(a.Lifecycle) > 100000 || a.DroppedLifecycle == 0 {
		t.Fatal("unbounded thread lifecycle history")
	}
}

func TestPortableSnapshotRetainsThreadPointCountsWithoutPointHistory(t *testing.T) {
	original := &ThreadAnalysis{LifecycleObserved: true, RunObservations: 2, StartReturns: 3, DroppedLifecycle: 1, Lifecycle: []ThreadLifecycleObservation{{}}}
	compact := comparisonThreadSummary(original)
	if !compact.LifecycleObserved || compact.RunObservations != 2 || compact.StartReturns != 3 || compact.DroppedLifecycle != 1 || len(compact.Lifecycle) != 0 || len(original.Lifecycle) != 1 {
		t.Fatal("lost counts or retained/mutated point detail")
	}
}

func TestThreadPointPreservesExactOperationIDSeparatelyFromResolvedName(t *testing.T) {
	c := &collector{}
	c.recordThreadLifecycle(jhlog.Event{Attribution: jhlog.AttributionContext{Present: true, OperationID: ^uint64(0)}, DiagnosticEvent: &jhlog.DiagnosticEvent{ThreadLifecycle: &jhlog.ThreadLifecycleEvent{Version: 1, Stage: jhlog.ThreadRunObserved, TargetTID: 42}}})
	point := c.threadAnalysis.finalize().Lifecycle[0]
	if !point.ContextCaptured || point.OperationID != "18446744073709551615" {
		t.Fatalf("lost exact context ID: %+v", point)
	}
}
