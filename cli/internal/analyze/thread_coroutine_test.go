package analyze

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func TestThreadAnalysisPreservesScopedCoroutineSegmentsWithoutSnapshots(t *testing.T) {
	s := threadCollectorState{
		identities: map[uint64]string{1: "scope-a"},
		coroutineSegments: []threadRawCoroutineSegment{{log: 1, process: "app",
			segment: &jhlog.CoroutineSegmentEvent{Version: 2, Token: 81, Generation: 4,
				JavaThreadID: 11, ExitJavaThreadID: 11, StartedElapsedMS: 100,
				EndedElapsedMS: 120, State: 0, EntryTID: 151, ExitTID: 151, Owner: "app.Feed.load"}}},
	}
	analysis := s.finalize()
	if analysis == nil || len(analysis.CoroutineSegments) != 1 ||
		analysis.CoroutineSegments[0].Scope != "scope-a" ||
		analysis.CoroutineSegments[0].EntryJavaThreadID != "11" ||
		analysis.CoroutineSegments[0].EntryTID != "151" {
		t.Fatalf("coroutine segment or process scope lost: %+v", analysis)
	}
}

func TestThreadAnalysisKeepsRunningCoroutineExitUnknown(t *testing.T) {
	s := threadCollectorState{
		identities: map[uint64]string{1: "scope-a"},
		coroutineSegments: []threadRawCoroutineSegment{{log: 1, process: "app",
			segment: &jhlog.CoroutineSegmentEvent{Version: 3, Token: 81, Generation: 4,
				JavaThreadID: 11, StartedElapsedMS: 100, EndedElapsedMS: 120,
				State: 4, EntryTID: 151, Owner: "app.Feed.load"}}},
	}
	segment := s.finalize().CoroutineSegments[0]
	if segment.State != "running_observed" || segment.ExitJavaThreadID != "" || segment.ExitTID != "" ||
		segment.EndedElapsedMS != nil || segment.ObservedThroughElapsedMS == nil || *segment.ObservedThroughElapsedMS != 120 {
		t.Fatalf("running coroutine was reported as finished: %+v", segment)
	}
	encoded, err := json.Marshal(segment)
	if err != nil || !strings.Contains(string(encoded), `"EndedElapsedMS":null`) ||
		!strings.Contains(string(encoded), `"ObservedThroughElapsedMS":120`) {
		t.Fatalf("JSON invented a known end: %s (%v)", encoded, err)
	}
}

func TestThreadAnalysisOrdersCaptureBeforeLaterExitDespiteWriterRace(t *testing.T) {
	s := threadCollectorState{
		identities: map[uint64]string{1: "scope-a"},
		coroutineSegments: []threadRawCoroutineSegment{
			{log: 1, segment: &jhlog.CoroutineSegmentEvent{Version: 2, Token: 81, Generation: 4,
				JavaThreadID: 11, ExitJavaThreadID: 11, EntryTID: 151, ExitTID: 151,
				StartedElapsedMS: 100, EndedElapsedMS: 130, State: 1, Owner: "app.Feed.load"}},
			{log: 1, segment: &jhlog.CoroutineSegmentEvent{Version: 3, Token: 81, Generation: 4,
				JavaThreadID: 11, EntryTID: 151, StartedElapsedMS: 100, EndedElapsedMS: 120,
				State: 4, Owner: "app.Feed.load"}},
		},
	}
	segments := s.finalize().CoroutineSegments
	if len(segments) != 2 || segments[0].State != "running_observed" || segments[1].State != "completed" {
		t.Fatalf("capture was shown after its later exit: %+v", segments)
	}
}
