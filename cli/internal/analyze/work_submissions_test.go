package analyze

import (
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
	"testing"
)

func TestWorkSubmissionDoesNotInventRegisteredJobOrExecution(t *testing.T) {
	event := jhlog.Event{Type: jhlog.EventWorkSubmission, TimeMS: 100, DiagnosticEvent: &jhlog.DiagnosticEvent{WorkSubmission: &jhlog.WorkSubmissionEvent{Version: 1, UUIDLeast: 7, HasUnique: true, UniqueName: "sync", Policy: jhlog.WorkSubmissionKeep, OmittedRequests: 99}}}
	s := inspectLogsForTest("intent", []jhlog.Log{{Events: []jhlog.Event{event, event}}})
	if s.WorkSubmissions == nil || len(s.WorkSubmissions.Observations) != 2 || s.WorkSubmissions.OmittedRequests != 198 {
		t.Fatal("lost repeated call evidence")
	}
	if s.WorkRegistry != nil || s.WorkerAnalysis != nil {
		t.Fatal("submission invented job acceptance/execution")
	}
}
func TestWorkSubmissionHistoryBoundedIndependentlyOfRegistry(t *testing.T) {
	c := &collector{}
	event := jhlog.Event{DiagnosticEvent: &jhlog.DiagnosticEvent{WorkSubmission: &jhlog.WorkSubmissionEvent{Version: 1}}}
	for i := 0; i < 50001; i++ {
		c.recordWorkSubmission(event)
	}
	if len(c.workSubmissions.observations) != 50000 || c.workSubmissions.dropped != 1 {
		t.Fatal("submission history unbounded")
	}
}
func TestSubmissionComparisonProjectionKeepsCountsWithoutDetailedHistory(t *testing.T) {
	source := &WorkSubmissionAnalysis{CapturedCalls: 10, UniqueCalls: 4, PeriodicCalls: 2, Observations: []WorkSubmissionObservation{{UUID: "id"}}}
	result := comparisonSnapshotSummary(Summary{WorkSubmissions: source}).WorkSubmissions
	if len(result.Observations) != 0 || !result.HistoryOmitted || result.CapturedCalls != 10 || result.UniqueCalls != 4 || result.PeriodicCalls != 2 || len(source.Observations) != 1 {
		t.Fatal("incorrect submission projection")
	}
}
