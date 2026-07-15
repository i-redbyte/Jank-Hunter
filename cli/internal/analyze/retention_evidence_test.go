package analyze

import (
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func TestClassifiedBaseEventsRespectRuntimeProblemDecision(t *testing.T) {
	dict := map[uint64]string{
		1: "com.app.Repository.load",
		2: "GET /slow-by-custom-policy",
		3: "FeedScreen",
	}
	summary := inspectLogsForTest("classified", []jhlog.Log{{
		Dict: dict,
		Events: []jhlog.Event{
			{
				Type:        jhlog.EventHTTP,
				Flags:       uint64(jhlog.FlagHTTPClassified),
				Attribution: attributionForTest(3, 1, 0, 0),
				HTTP:        &jhlog.HTTPEvent{OwnerID: 1, RouteID: 2, DurationMS: 5_000, Status: jhlog.Status2xx},
			},
			{
				Type:        jhlog.EventUIWindow,
				Flags:       uint64(jhlog.FlagUIClassified),
				Attribution: attributionForTest(3, 1, 0, 0),
				UIWindow:    &jhlog.UIWindowEvent{ScreenID: 3, WindowMS: 1_000, FrameCount: 60, JankCount: 5, P95MS: 80},
			},
		},
	}})

	if len(summary.ProblemWindows) != 0 {
		t.Fatalf("classified non-problems produced fallback windows: %+v", summary.ProblemWindows)
	}
}

func TestLegacyDerivedProblemDoesNotDoubleCanonicalBaseWindow(t *testing.T) {
	dict := map[uint64]string{
		1: "com.app.MainThreadOwner.run",
		2: "main_thread_stall",
	}
	attr := attributionForTest(0, 1, 0, 0)
	summary := inspectLogsForTest("legacy", []jhlog.Log{{
		Dict: dict,
		Events: []jhlog.Event{
			{Type: jhlog.EventStall, Attribution: attr, Stall: &jhlog.StallEvent{OwnerID: 1, DurationMS: 2_000}},
			{
				Type:        jhlog.EventProblem,
				Attribution: attr,
				Problem:     &jhlog.ProblemEvent{OwnerID: 1, KindID: 2, WindowMS: 2_000, Count: 1, MaxMS: 2_000},
			},
		},
	}})

	if len(summary.ProblemWindows) != 1 {
		t.Fatalf("problem windows = %+v", summary.ProblemWindows)
	}
	window := summary.ProblemWindows[0]
	if window.Kind != "main_thread_stall" || window.Windows != 1 || window.Count != 1 {
		t.Fatalf("canonical window was double-counted: %+v", window)
	}
}

func TestRetentionEvidenceChangesConfidenceAndSeverity(t *testing.T) {
	base := memoryLeakStats{
		className: "com.app.LeakedActivity",
		holder:    "com.app.Singleton",
		count:     10,
		maxAgeMs:  60_000,
	}
	timeOnly := base
	timeOnly.timeOnlyCount = base.count
	afterGC := base
	afterGC.afterExplicitGCCount = base.count
	heap := &HeapLeakEvidence{
		ClassName: "com.app.LeakedActivity",
		Holder:    "com.app.Singleton",
		GCRoot:    "sticky class",
		ReferencePath: []HeapPathElement{
			{ClassName: "GC root: sticky class", Kind: "gc_root"},
			{ClassName: "com.app.LeakedActivity", FieldName: "instance", Kind: "static"},
		},
	}

	timeSuspect := memoryLeakSuspectFromStats(timeOnly, 0, 0, nil, retentionDataQuality{})
	gcSuspect := memoryLeakSuspectFromStats(afterGC, 0, 0, nil, retentionDataQuality{})
	heapSuspect := memoryLeakSuspectFromStats(afterGC, 0, 0, heap, retentionDataQuality{})

	if timeSuspect.EvidenceKind != RetentionEvidenceTimeOnly || timeSuspect.Severity == "high" {
		t.Fatalf("time_only must remain an unconfirmed, capped signal: %+v", timeSuspect)
	}
	if gcSuspect.EvidenceKind != RetentionEvidenceAfterExplicitGC || gcSuspect.Score <= timeSuspect.Score {
		t.Fatalf("after_explicit_gc should be stronger than time_only: time=%+v gc=%+v", timeSuspect, gcSuspect)
	}
	if !heapSuspect.HeapEvidence || heapSuspect.EvidenceKind != RetentionEvidenceConfirmedHPROFPath || heapSuspect.Score <= gcSuspect.Score {
		t.Fatalf("confirmed HPROF path should be the strongest evidence: gc=%+v heap=%+v", gcSuspect, heapSuspect)
	}
}

func TestRetentionQualityLossDowngradesEvidenceConfidence(t *testing.T) {
	stats := memoryLeakStats{
		className:            "com.app.LeakedActivity",
		holder:               "com.app.Singleton",
		count:                2,
		maxAgeMs:             30_000,
		afterExplicitGCCount: 2,
	}
	clean := memoryLeakSuspectFromStats(stats, 0, 0, nil, retentionDataQuality{})
	degraded := memoryLeakSuspectFromStats(stats, 0, 0, nil, retentionDataQuality{
		runtimeMayBeIncomplete: true,
		runtimeNotes:           []string{"наблюдатель удержания достиг лимита"},
	})

	if clean.DataQuality != "complete" || !strings.HasPrefix(clean.EvidenceConfidence, "среднее") {
		t.Fatalf("clean confidence = %+v", clean)
	}
	if degraded.DataQuality != "degraded" || !strings.HasPrefix(degraded.EvidenceConfidence, "низкое") {
		t.Fatalf("degraded confidence was not downgraded: %+v", degraded)
	}
}

func TestCleanLiveSnapshotDoesNotDowngradeRetentionConfidence(t *testing.T) {
	c := &collector{}
	c.summary.CollectionSegments = []CollectionSegment{{
		Source: "session.jhlog",
		Status: string(jhlog.SegmentStatusOpenClean),
	}}

	quality := c.retentionDataQuality()

	if quality.runtimeMayBeIncomplete || len(quality.runtimeNotes) != 0 {
		t.Fatalf("clean live snapshot degraded runtime retention evidence: %+v", quality)
	}
}

func TestOpenSegmentWithTailDowngradesRetentionConfidence(t *testing.T) {
	c := &collector{}
	c.summary.CollectionSegments = []CollectionSegment{{
		Source:    "session.jhlog",
		Status:    string(jhlog.SegmentStatusOpenWithTail),
		TailBytes: 7,
	}}

	quality := c.retentionDataQuality()

	if !quality.runtimeMayBeIncomplete || len(quality.runtimeNotes) == 0 {
		t.Fatalf("open segment with tail must degrade runtime retention evidence: %+v", quality)
	}
}

func TestDictionaryTruncationOutsideRetainedFieldsDoesNotDowngradeLeakIdentity(t *testing.T) {
	c := &collector{
		qualitySnapshots: map[string]segmentQualityState{"main": {
			snapshot: jhlog.QualitySnapshot{Counters: map[uint64]uint64{
				jhlog.QualityDictionaryValueTruncated: 1,
			}},
		}},
	}

	quality := c.retentionDataQuality()

	if quality.dictionaryDegraded || len(quality.dictionaryNotes) != 0 {
		t.Fatalf("generic dictionary truncation degraded retained identity: %+v", quality)
	}
}

func TestTimeOnlyComparisonDeltaCannotBecomeHigh(t *testing.T) {
	before := MemoryLeakSuspect{
		ClassName:    "com.app.LeakedActivity",
		EvidenceKind: RetentionEvidenceTimeOnly,
		Count:        1,
		MaxAgeMS:     5_000,
		Score:        1,
		Severity:     "ok",
	}
	after := before
	after.Count = 100
	after.MaxAgeMS = 120_000
	after.Score = 100
	after.Severity = "medium"

	delta := buildLeakDelta("time-only", before, true, after, true)

	if delta.Severity == "high" {
		t.Fatalf("time_only comparison delta must remain unconfirmed: %+v", delta)
	}
}
