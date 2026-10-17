package analyze

import (
	"math"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func cpuThreadEvent(recorded, observed, user, system, start uint64) jhlog.Event {
	return jhlog.Event{Type: jhlog.EventThreadSnapshot, TimeMS: recorded, DiagnosticEvent: &jhlog.DiagnosticEvent{ThreadSnapshot: &jhlog.ThreadSnapshotEvent{Version: 1, ElapsedMS: observed, TicksPerSecond: 100, Complete: true, Threads: []jhlog.ThreadObservation{{TID: 1, StartTicks: start, UserTicks: user, SystemTicks: system, OSState: "S"}}}}}
}
func TestThreadCPUUsesMonotonicSamplesAndKeepsKernelTickPrecision(t *testing.T) {
	a := inspectLogsForTest("cpu", []jhlog.Log{{Events: []jhlog.Event{cpuThreadEvent(999, 1000, 10, 2, 1), cpuThreadEvent(1000, 2000, 35, 7, 1)}}}).ThreadAnalysis
	h := a.Processes[0].Histories[0]
	if h.Observations[0].CPU != nil {
		t.Fatal("invented initial CPU delta")
	}
	d := h.Observations[1].CPU
	if d == nil || d.UserTicks != 25 || d.SystemTicks != 5 || d.ElapsedMS != 1000 || math.Abs(d.CorePercent-30) > 0.001 {
		t.Fatalf("wrong CPU: %+v", d)
	}
}
func TestThreadCPURejectsCounterResetClockRegressionAndTidReuse(t *testing.T) {
	for _, second := range []jhlog.Event{cpuThreadEvent(20, 2000, 1, 1, 1), cpuThreadEvent(20, 900, 20, 4, 1), cpuThreadEvent(20, 2000, 20, 4, 2)} {
		a := inspectLogsForTest("reset", []jhlog.Log{{Events: []jhlog.Event{cpuThreadEvent(10, 1000, 10, 2, 1), second}}}).ThreadAnalysis
		for _, h := range a.Processes[0].Histories {
			for _, o := range h.Observations {
				if o.CPU != nil {
					t.Fatalf("invalid delta: %+v", o)
				}
			}
		}
	}
}
func TestThreadObservationTimeUsesHeaderClockAnchor(t *testing.T) {
	c := newCollector("clock", 1, Options{})
	c.startLog(jhlog.SegmentHeader{SegmentStartElapsedUS: 1000000, SegmentStartUnixMS: 5000})
	c.add(nil, cpuThreadEvent(999999, 1200, 0, 0, 1))
	a := c.threadAnalysis.finalize()
	if a.Processes[0].Snapshots[0].TimeMS != 5200 {
		t.Fatalf("used queue time: %+v", a.Processes[0].Snapshots)
	}
}

func TestThreadClockFallbackIsExplicit(t *testing.T) {
	c := newCollector("no clock", 1, Options{})
	c.startLog(jhlog.SegmentHeader{})
	c.add(nil, cpuThreadEvent(999, 1200, 0, 0, 1))
	p := c.threadAnalysis.finalize().Processes[0].Snapshots[0]
	if p.TimeMS != 999 || p.TimeBasis != "record_time" {
		t.Fatalf("unmarked fallback: %+v", p)
	}
}
