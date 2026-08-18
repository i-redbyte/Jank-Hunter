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
				HTTP:        &jhlog.HTTPEvent{RouteRef: jhlog.LocalSymbol(2), DurationMS: 5_000, Status: jhlog.Status2xx},
			},
			{
				Type:        jhlog.EventUIWindow,
				Flags:       uint64(jhlog.FlagUIClassified),
				Attribution: attributionForTest(3, 1, 0, 0),
				UIWindow:    &jhlog.UIWindowEvent{WindowMS: 1_000, FrameCount: 60, JankCount: 5, P95MS: 80},
			},
		},
	}})

	if len(summary.ProblemWindows) != 0 {
		t.Fatalf("classified non-problems produced fallback windows: %+v", summary.ProblemWindows)
	}
}

func TestUIProblemWindowUsesObservedP99AsMaximum(t *testing.T) {
	dict := map[uint64]string{1: "CheckoutActivity"}
	summary := inspectLogsForTest("ui p99", []jhlog.Log{{
		Dict: dict,
		Events: []jhlog.Event{{
			Type:        jhlog.EventUIWindow,
			Flags:       uint64(jhlog.FlagUIClassified | jhlog.FlagUIProblem),
			Attribution: attributionForTest(1, 0, 0, 0),
			UIWindow: &jhlog.UIWindowEvent{
				WindowMS:   1_000,
				FrameCount: 60,
				JankCount:  2,
				P95MS:      16,
				P99MS:      48,
			},
		}},
	}})

	if len(summary.ProblemWindows) != 1 || summary.ProblemWindows[0].MaxMS != 48 {
		t.Fatalf("UI problem maximum = %+v, want observed p99=48", summary.ProblemWindows)
	}
}

func TestHeapDumpPauseIsAttributedToDiagnostics(t *testing.T) {
	dict := map[uint64]string{
		1: "jankhunter.heap_dump.created.count",
		2: "android.view.DisplayEventReceiver",
		3: "android.view.DisplayEventReceiver.nativeGetLatestVsyncEventData",
	}
	summary := inspectLogsForTest("heap dump stall", []jhlog.Log{{
		Dict: dict,
		Events: []jhlog.Event{
			{
				Type:   jhlog.EventCounter,
				TimeMS: 1_000,
				Metric: &jhlog.MetricEvent{MetricRef: jhlog.LocalSymbol(1), Value: 1},
			},
			{
				Type:        jhlog.EventStall,
				TimeMS:      1_200,
				Attribution: attributionForTest(0, 2, 0, 0),
				Stall:       &jhlog.StallEvent{StackRef: jhlog.LocalSymbol(3), DurationMS: 700},
			},
		},
	}})

	if len(summary.ProblemWindows) != 1 {
		t.Fatalf("heap dump stall windows = %+v", summary.ProblemWindows)
	}
	window := summary.ProblemWindows[0]
	if window.Owner != "jankhunter.heap_dump" || window.Flow != "jankhunter.diagnostics" || window.Step != "heap_dump" {
		t.Fatalf("heap dump stall attribution = %+v", window)
	}
}

func TestJankHunterSampleClassIsApplicationOwned(t *testing.T) {
	suspect := memoryLeakSuspectFromStats(
		memoryLeakStats{
			className:            "io.jankhunter.sample.RetainedCheckoutCache",
			holder:               "sample.auto.retention.checkout_cache",
			count:                1,
			maxAgeMs:             8_000,
			afterExplicitGCCount: 1,
		},
		0,
		0,
		nil,
		retentionDataQuality{},
	)

	if !suspect.UserOwned || suspect.SystemRetained || suspect.ObjectKind != "пользовательский объект" {
		t.Fatalf("sample retention ownership is incorrect: %+v", suspect)
	}
}

func TestJankHunterRuntimeClassIsSystemOwned(t *testing.T) {
	suspect := memoryLeakSuspectFromStats(
		memoryLeakStats{
			className:            "io.jankhunter.runtime.internal.SampleState",
			holder:               "io.jankhunter.runtime.JankHunter",
			count:                1,
			maxAgeMs:             8_000,
			afterExplicitGCCount: 1,
		},
		0,
		0,
		nil,
		retentionDataQuality{},
	)

	if suspect.UserOwned || !suspect.SystemRetained {
		t.Fatalf("runtime retention ownership is incorrect: %+v", suspect)
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
