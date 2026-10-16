package analyze

import (
	"fmt"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
	"testing"
)

func registryEvent(index uint64, jobs []jhlog.WorkRegistryJob) jhlog.Event {
	return jhlog.Event{Type: jhlog.EventWorkRegistry, TimeMS: 9999, DiagnosticEvent: &jhlog.DiagnosticEvent{WorkRegistry: &jhlog.WorkRegistryEvent{
		Version: 1, SnapshotID: 1, QueryStartedMS: 10, ObservedMS: 15, IntervalMS: 30000, PacketIndex: index, PacketCount: 2, TotalObserved: 9, RetainedJobs: 9, Jobs: jobs,
	}}}
}

func TestRegistryMissingPacketNeverBecomesAnEmptyCompleteRegistry(t *testing.T) {
	event := registryEvent(1, []jhlog.WorkRegistryJob{{UUID: "job9", State: 1}})
	summary := inspectLogsForTest("partial", []jhlog.Log{{Events: []jhlog.Event{event}}})
	if summary.WorkRegistry == nil || len(summary.WorkRegistry.Snapshots) != 1 {
		t.Fatal("missing registry")
	}
	s := summary.WorkRegistry.Snapshots[0]
	if s.Complete || s.CoverageReason != "missing_packets" || len(s.Jobs) != 1 {
		t.Fatalf("lost uncertainty: %+v", s)
	}
	if inspectLogsForTest("old", []jhlog.Log{{}}).WorkRegistry != nil {
		t.Fatal("invented old-log registry")
	}
}

func TestRegistryReassemblesPacketsAndAnchorsObservationClock(t *testing.T) {
	rows := make([]jhlog.WorkRegistryJob, 8)
	for i := range rows {
		rows[i] = jhlog.WorkRegistryJob{UUID: fmt.Sprint(i)}
	}
	header := jhlog.SegmentHeader{SegmentStartElapsedUS: 5000, SegmentStartUnixMS: 1000, ProcessName: "app"}
	summary := inspectLogsForTest("complete", []jhlog.Log{{Result: jhlog.StreamResult{Header: header}, Events: []jhlog.Event{
		registryEvent(1, []jhlog.WorkRegistryJob{{UUID: "last"}}), registryEvent(0, rows), registryEvent(0, rows),
	}}})
	s := summary.WorkRegistry.Snapshots[0]
	if !s.Complete || len(s.Jobs) != 9 || s.ObservedUnixMS == nil || *s.ObservedUnixMS != 1010 {
		t.Fatalf("bad reconstruction: %+v", s)
	}
}

func TestRegistrySnapshotIDsDoNotMergeAcrossSessions(t *testing.T) {
	event := registryEvent(1, []jhlog.WorkRegistryJob{{UUID: "job9"}})
	summary := inspectLogsForTest("sessions", []jhlog.Log{{Events: []jhlog.Event{event}}, {Events: []jhlog.Event{event}}})
	if len(summary.WorkRegistry.Snapshots) != 2 {
		t.Fatal("merged distinct source sessions")
	}
}

func TestRegistryFreshnessUsesCapturedMonotonicCollectionWindow(t *testing.T) {
	c := newCollector("freshness", 1, Options{})
	header := jhlog.DefaultSegmentHeader()
	c.startLog(header)
	c.add(nil, registryEvent(1, []jhlog.WorkRegistryJob{{UUID: "job9"}}))
	c.addStreamResult(jhlog.StreamResult{Header: header, LatestQuality: &jhlog.QualitySnapshot{Counters: map[uint64]uint64{jhlog.QualityCollectionWindowEndElapsedMS: 25}}})
	c.finishLog()
	s := c.finish().WorkRegistry.Snapshots[0]
	if s.AgeAtCaptureMS == nil || *s.AgeAtCaptureMS != 10 {
		t.Fatalf("bad freshness: %+v", s)
	}
}

func TestLiveRegistryFreshnessUsesQualityTimestampWithoutInventingCollectionEnd(t *testing.T) {
	c := newCollector("live", 1, Options{})
	header := jhlog.DefaultSegmentHeader()
	c.startLog(header)
	c.add(nil, registryEvent(1, []jhlog.WorkRegistryJob{{UUID: "job9"}}))
	c.addStreamResult(jhlog.StreamResult{Header: header, LatestQuality: &jhlog.QualitySnapshot{CapturedElapsedUS: 25000}})
	c.finishLog()
	s := c.finish().WorkRegistry.Snapshots[0]
	if s.AgeAtCaptureMS == nil || *s.AgeAtCaptureMS != 10 {
		t.Fatalf("live freshness missing: %+v", s)
	}
}

func TestRegistryRetentionBudgetReportsDroppedHistory(t *testing.T) {
	c := newCollector("bounded", 1, Options{})
	c.startLog(jhlog.DefaultSegmentHeader())
	for index := uint64(1); index <= 12500; index++ {
		rows := make([]jhlog.WorkRegistryJob, 8)
		for row := range rows {
			rows[row] = jhlog.WorkRegistryJob{UUID: fmt.Sprint(row)}
		}
		packet := &jhlog.WorkRegistryEvent{Version: 1, SnapshotID: index, ObservedMS: index, IntervalMS: 30000, PacketCount: 1, TotalObserved: 8, RetainedJobs: 8, Jobs: rows}
		c.recordWorkRegistry(jhlog.Event{DiagnosticEvent: &jhlog.DiagnosticEvent{WorkRegistry: packet}})
	}
	c.finishLog()
	result := c.finish().WorkRegistry
	if result.DroppedPackets == 0 || len(result.Snapshots) >= 12500 {
		t.Fatal("retention budget did not bound history")
	}
}
