package jhlog

import "testing"

func TestARTTerminalGrowthSnapshotDoesNotReferenceDiscardedBase(t *testing.T) {
	for name, reason := range map[string]SegmentEndReason{"io": SegmentEndIOError, "size": SegmentEndSizeLimit, "budget": SegmentEndStorageBudget} {
		t.Run(name, func(t *testing.T) {
			log, err := readLog("../../../wire/testdata/growth-terminal-" + name + "-5.1.0.jhlog")
			if err != nil {
				t.Fatal(err)
			}
			r := log.Result
			if !r.Sealed || r.Status != SegmentStatusClosedClean || r.SegmentEnd == nil || r.SegmentEnd.Reason != reason {
				t.Fatalf("lost terminal reason or integrity: %+v", r)
			}
			if r.LogGrowth == nil || r.LogGrowth.Live == nil || !r.LogGrowth.Live.Completed {
				t.Fatalf("terminal snapshot lost: %+v", r.LogGrowth)
			}
		})
	}
}
