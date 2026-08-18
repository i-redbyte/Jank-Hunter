package analyze

import (
	"strconv"
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func TestBuildLogGrowthSummaryUsesNewestDailyBaselineWithoutDoubleCounting(t *testing.T) {
	first := growthSession("first", 100, 150, true)
	second := growthSession("second", 160, 190, true)
	current := growthSession("current", 200, 240, false)
	results := []jhlog.StreamResult{
		{LogGrowth: &jhlog.LogGrowthProjection{
			Generation:   2,
			CapturedAtMS: 100,
			Sessions:     []jhlog.LogGrowthSession{first},
			Days:         []jhlog.LogGrowthDay{{DayKey: 20270115, SessionCount: 1, GeneratedBytes: 100}},
			Live:         &second,
		}},
		{LogGrowth: &jhlog.LogGrowthProjection{
			Generation:   3,
			CapturedAtMS: 200,
			Sessions:     []jhlog.LogGrowthSession{first, second},
			Days:         []jhlog.LogGrowthDay{{DayKey: 20270115, SessionCount: 2, GeneratedBytes: 200}},
			Live:         &current,
		}},
	}

	summary := buildLogGrowthSummary(results)
	if !summary.Available || summary.HistoryGeneration != 3 || summary.CurrentSession == nil {
		t.Fatalf("summary = %+v", summary)
	}
	if len(summary.Sessions) != 3 {
		t.Fatalf("sessions = %+v", summary.Sessions)
	}
	if len(summary.Days) != 1 || summary.Days[0].SessionCount != 3 || summary.Days[0].GeneratedBytes != 300 {
		t.Fatalf("days = %+v", summary.Days)
	}
	if summary.CurrentSession.SessionID != "current" {
		t.Fatalf("current = %+v", summary.CurrentSession)
	}
}

func TestBuildLogGrowthSummaryIsIndependentOfInputOrderWhenCompletionMatchesLastCheckpoint(t *testing.T) {
	partial := growthSession("same", 100, 200, false)
	partial.GeneratedBytes = 500
	partial.MaximumRetainedBytes = 450
	partial.LimitReachedCount = 1
	partial.SegmentRotationCount = 3
	partial.ArchiveEvictedBytes = 50
	completed := partial
	completed.Completed = true
	current := growthSession("current", 300, 350, false)
	current.DayKey = 20270116
	current.GeneratedBytes = 80

	results := []jhlog.StreamResult{
		{LogGrowth: &jhlog.LogGrowthProjection{
			Generation:   4,
			CapturedAtMS: 200,
			Live:         &partial,
		}},
		{LogGrowth: &jhlog.LogGrowthProjection{
			Generation:   5,
			CapturedAtMS: 250,
			Sessions:     []jhlog.LogGrowthSession{completed},
			Days: []jhlog.LogGrowthDay{{
				DayKey:                completed.DayKey,
				SessionCount:          1,
				GeneratedBytes:        completed.GeneratedBytes,
				MaximumRetainedBytes:  completed.MaximumRetainedBytes,
				SessionsReachingLimit: 1,
				LimitReachedCount:     completed.LimitReachedCount,
				SegmentRotationCount:  completed.SegmentRotationCount,
				ArchiveEvictedBytes:   completed.ArchiveEvictedBytes,
			}},
		}},
		{LogGrowth: &jhlog.LogGrowthProjection{
			Generation:   4,
			CapturedAtMS: 350,
			Live:         &current,
		}},
	}

	for _, order := range permutationsOfThree() {
		ordered := []jhlog.StreamResult{results[order[0]], results[order[1]], results[order[2]]}
		summary := buildLogGrowthSummary(ordered)
		if summary.HistoryGeneration != 5 || len(summary.Sessions) != 2 || len(summary.Days) != 2 {
			t.Fatalf("order %v: summary = %+v", order, summary)
		}
		if !summary.Sessions[0].Completed || summary.Sessions[0].SessionID != completed.SessionID {
			t.Fatalf("order %v: completed session = %+v", order, summary.Sessions[0])
		}
		if summary.CurrentSession == nil || summary.CurrentSession.SessionID != current.SessionID {
			t.Fatalf("order %v: current session = %+v", order, summary.CurrentSession)
		}
		if summary.Days[0].SessionCount != 1 || summary.Days[0].GeneratedBytes != completed.GeneratedBytes {
			t.Fatalf("order %v: completed day = %+v", order, summary.Days[0])
		}
		if summary.Days[1].SessionCount != 1 || summary.Days[1].GeneratedBytes != current.GeneratedBytes {
			t.Fatalf("order %v: current day = %+v", order, summary.Days[1])
		}
	}
}

func TestBuildLogGrowthSummaryDoesNotLetRotatedLiveSegmentEraseHistory(t *testing.T) {
	completed := growthSession("completed", 100, 200, true)
	active := growthSession("active", 300, 350, false)
	active.DayKey = 20270116
	results := []jhlog.StreamResult{
		{LogGrowth: &jhlog.LogGrowthProjection{
			Generation:   5,
			HasHistory:   true,
			CapturedAtMS: 250,
			Sessions:     []jhlog.LogGrowthSession{completed},
			Days: []jhlog.LogGrowthDay{{
				DayKey:         completed.DayKey,
				SessionCount:   1,
				GeneratedBytes: completed.GeneratedBytes,
			}},
		}},
		{LogGrowth: &jhlog.LogGrowthProjection{
			LiveGeneration: 100,
			Live:           &active,
		}},
	}

	for _, ordered := range [][]jhlog.StreamResult{results, {results[1], results[0]}} {
		summary := buildLogGrowthSummary(ordered)
		if summary.HistoryGeneration != 5 || summary.CapturedAtMS != 250 {
			t.Fatalf("history baseline was erased: %+v", summary)
		}
		if len(summary.Days) != 2 || summary.Days[0].DayKey != completed.DayKey ||
			summary.Days[0].SessionCount != 1 || summary.Days[1].DayKey != active.DayKey {
			t.Fatalf("completed history and active day were not both retained: %+v", summary.Days)
		}
		if summary.CurrentSession == nil || summary.CurrentSession.SessionID != active.SessionID {
			t.Fatalf("active rotated session missing: %+v", summary.CurrentSession)
		}
	}
}

func TestBuildLogGrowthSummaryKeepsEveryDetailedSession(t *testing.T) {
	const sessionCount = 1_031
	sessions := make([]jhlog.LogGrowthSession, sessionCount)
	for index := range sessions {
		sessions[index] = growthSession(strconv.Itoa(index), uint64(index), uint64(index+1), true)
	}
	summary := buildLogGrowthSummary([]jhlog.StreamResult{{LogGrowth: &jhlog.LogGrowthProjection{
		Generation: 1,
		Sessions:   sessions,
	}}})

	if len(summary.Sessions) != sessionCount {
		t.Fatalf("session count = %d", len(summary.Sessions))
	}
	if summary.Sessions[0].SessionID != "0" ||
		summary.Sessions[len(summary.Sessions)-1].SessionID != strconv.Itoa(len(sessions)-1) {
		t.Fatalf("complete range = %q..%q", summary.Sessions[0].SessionID, summary.Sessions[len(summary.Sessions)-1].SessionID)
	}
}

func TestBuildLogGrowthSummaryMergesIndependentProcessHistories(t *testing.T) {
	mainSession := growthSession("main-session", 100, 200, true)
	mainSession.GeneratedBytes = 600
	remoteSession := growthSession("remote-session", 110, 220, true)
	remoteSession.GeneratedBytes = 400
	remoteSession.SegmentRotationCount = 2
	results := []jhlog.StreamResult{
		{
			Header: jhlog.SegmentHeader{ProcessName: "main"},
			LogGrowth: &jhlog.LogGrowthProjection{
				Generation:   7,
				CapturedAtMS: 300,
				Sessions:     []jhlog.LogGrowthSession{mainSession},
				Days: []jhlog.LogGrowthDay{{
					DayKey:         mainSession.DayKey,
					SessionCount:   1,
					GeneratedBytes: mainSession.GeneratedBytes,
				}},
			},
		},
		{
			Header: jhlog.SegmentHeader{ProcessName: "remote"},
			LogGrowth: &jhlog.LogGrowthProjection{
				Generation:   3,
				CapturedAtMS: 250,
				Sessions:     []jhlog.LogGrowthSession{remoteSession},
				Days: []jhlog.LogGrowthDay{{
					DayKey:               remoteSession.DayKey,
					SessionCount:         1,
					GeneratedBytes:       remoteSession.GeneratedBytes,
					SegmentRotationCount: remoteSession.SegmentRotationCount,
				}},
			},
		},
	}

	summary := buildLogGrowthSummary(results)
	if len(summary.Sessions) != 2 || len(summary.Days) != 1 {
		t.Fatalf("process histories were not merged: %+v", summary)
	}
	day := summary.Days[0]
	if day.SessionCount != 2 || day.GeneratedBytes != 1_000 || day.SegmentRotationCount != 2 {
		t.Fatalf("merged day = %+v", day)
	}
	if summary.HistoryGeneration != 7 || summary.CapturedAtMS != 300 {
		t.Fatalf("summary frontier = generation %d captured %d", summary.HistoryGeneration, summary.CapturedAtMS)
	}
}

func TestBuildLogGrowthSummaryMarksCheckpointFreshAtCommittedFrontier(t *testing.T) {
	live := growthSession("current", 1_000, 10_000, false)
	live.GeneratedBytes = 100_000
	result := jhlog.StreamResult{
		Header:                jhlog.SegmentHeader{ProcessName: "main", SessionID: jhlog.ID128{1}},
		InputBytes:            100_100,
		LatestDataEventUnixMS: 11_000,
		LogGrowth:             &jhlog.LogGrowthProjection{Live: &live},
	}

	summary := buildLogGrowthSummary([]jhlog.StreamResult{result})
	if summary.FreshnessStatus != "fresh" || summary.SnapshotLagMS != 1_000 ||
		summary.SnapshotLagBytes != 100 || summary.LiveCapturedAtMS != 10_000 {
		t.Fatalf("fresh growth summary = %+v", summary)
	}
}

func TestBuildLogGrowthSummaryExposesStaleActiveCheckpoint(t *testing.T) {
	live := growthSession("current", 1_000, 10_000, false)
	live.GeneratedBytes = 27_741
	result := jhlog.StreamResult{
		Header:                jhlog.SegmentHeader{ProcessName: "main", SessionID: jhlog.ID128{2}},
		InputBytes:            48_350,
		LatestDataEventUnixMS: 93_000,
		LogGrowth:             &jhlog.LogGrowthProjection{Live: &live},
	}

	summary := buildLogGrowthSummary([]jhlog.StreamResult{result})
	if summary.FreshnessStatus != "stale" || summary.SnapshotLagMS != 83_000 ||
		summary.SnapshotLagBytes != 20_609 || summary.LatestDataEventMS != 93_000 ||
		!strings.Contains(summary.FreshnessReason, "committed events") {
		t.Fatalf("stale growth summary = %+v", summary)
	}
}

func TestBuildLogGrowthSummaryDoesNotClaimFreshnessWhenAProcessHasNoCheckpoint(t *testing.T) {
	live := growthSession("main-current", 1_000, 10_000, false)
	results := []jhlog.StreamResult{
		{
			Header:                jhlog.SegmentHeader{ProcessName: "main", SessionID: jhlog.ID128{3}},
			InputBytes:            100,
			LatestDataEventUnixMS: 10_000,
			LogGrowth:             &jhlog.LogGrowthProjection{Live: &live},
		},
		{
			Header:                jhlog.SegmentHeader{ProcessName: "remote", SessionID: jhlog.ID128{4}},
			InputBytes:            100,
			LatestDataEventUnixMS: 10_000,
		},
	}

	summary := buildLogGrowthSummary(results)
	if summary.FreshnessStatus != "unknown" || !strings.Contains(summary.FreshnessReason, "отсутствует live checkpoint") {
		t.Fatalf("multi-process freshness without remote checkpoint = %+v", summary)
	}
}

func permutationsOfThree() [][3]int {
	return [][3]int{{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0}}
}

func growthSession(id string, started, ended uint64, completed bool) jhlog.LogGrowthSession {
	return jhlog.LogGrowthSession{
		SessionID:            id,
		DayKey:               20270115,
		StartedAtMS:          started,
		EndedAtMS:            ended,
		ConfiguredLimitBytes: 1_000,
		MaximumRetainedBytes: 500,
		GeneratedBytes:       100,
		Completed:            completed,
	}
}
