package analyze

import (
	"encoding/hex"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func TestResourceOnlyLogUsesRecordingSessionIdentity(t *testing.T) {
	state := threadCollectorState{resources: []threadRawResource{{log: 1, end: 10, startKnown: true}}}
	result := jhlog.StreamResult{Source: "resource-only.jhlog"}
	state.finishLog(1, result)
	got := state.finalize().Resources[0].Scope
	want := hex.EncodeToString([]byte(workerStreamIdentity(result)))
	if got != want {
		t.Fatalf("resource detached from its recording session: got %q, want %q", got, want)
	}
}

func TestThreadResourcesKeepCompletionEvidenceAndDerivedStartSeparate(t *testing.T) {
	producer := jhlog.ProducerMetadata{HasThread: true, ThreadID: 151}
	events := []jhlog.Event{
		{Type: jhlog.EventSession, TimeMS: 1, Session: &jhlog.SessionEvent{CollectorFlags: uint64(jhlog.CollectorThread)}},
		{Type: jhlog.EventHTTP, TimeMS: 120, Producer: producer, HTTP: &jhlog.HTTPEvent{InitiatorRef: jhlog.LocalSymbol(1), RouteRef: jhlog.LocalSymbol(4), DurationMS: 20}},
		{Type: jhlog.EventIO, TimeMS: 150, Producer: producer, IO: &jhlog.IOEvent{SourceRef: jhlog.LocalSymbol(2), Operation: jhlog.IOOperationFileRead, DurationUS: 2_001}},
		{Type: jhlog.EventDatabase, TimeMS: 180, Producer: producer, Database: &jhlog.DatabaseEvent{SourceRef: jhlog.LocalSymbol(3), DurationUS: 5_000}},
	}
	analysis := inspectLogsForTest("thread resources", []jhlog.Log{{Dict: map[uint64]string{
		1: "Feed.fetch", 2: "Cache.read", 3: "FeedDao.load", 4: "GET /feed"}, Events: events}}).ThreadAnalysis
	if analysis == nil || len(analysis.Resources) != 3 {
		t.Fatalf("lost resource completions: %+v", analysis)
	}
	for i, kind := range []string{"http", "file_read", "database"} {
		row := analysis.Resources[i]
		if row.Kind != kind || row.CompletionTID != "151" || !row.StartDerived || row.EndMS == 0 {
			t.Fatalf("resource evidence %d: %+v", i, row)
		}
	}
	if analysis.Resources[0].StartMS == nil || *analysis.Resources[0].StartMS != 100 ||
		analysis.Resources[0].EndMS != 120 || analysis.Resources[1].StartMS == nil ||
		*analysis.Resources[1].StartMS != 147 || analysis.Resources[0].Source != "Feed.fetch" ||
		analysis.Resources[0].Target != "GET /feed" ||
		analysis.Resources[1].Source != "Cache.read" || analysis.Resources[2].Source != "FeedDao.load" {
		t.Fatalf("derived intervals: %+v", analysis.Resources)
	}
}

func TestThreadResourceDoesNotInventStartWhenDurationExceedsClock(t *testing.T) {
	events := []jhlog.Event{
		{Type: jhlog.EventSession, Session: &jhlog.SessionEvent{CollectorFlags: uint64(jhlog.CollectorThread)}},
		{Type: jhlog.EventHTTP, TimeMS: 5, Producer: jhlog.ProducerMetadata{HasThread: true, ThreadID: 151},
			HTTP: &jhlog.HTTPEvent{DurationMS: 20}},
	}
	analysis := inspectLogsForTest("unknown start", []jhlog.Log{{Events: events}}).ThreadAnalysis
	if analysis == nil || len(analysis.Resources) != 1 || analysis.Resources[0].StartMS != nil ||
		analysis.Resources[0].StartDerived || analysis.Resources[0].EndMS != 5 {
		t.Fatalf("invented start from impossible duration: %+v", analysis)
	}
}

func TestThreadHTTPRouteNeverBecomesCallerSource(t *testing.T) {
	events := []jhlog.Event{
		{Type: jhlog.EventSession, Session: &jhlog.SessionEvent{CollectorFlags: uint64(jhlog.CollectorThread)}},
		{Type: jhlog.EventHTTP, TimeMS: 120, Producer: jhlog.ProducerMetadata{HasThread: true, ThreadID: 151},
			HTTP: &jhlog.HTTPEvent{RouteRef: jhlog.LocalSymbol(1), DurationMS: 20}},
	}
	analysis := inspectLogsForTest("http route", []jhlog.Log{{Dict: map[uint64]string{1: "GET /feed"}, Events: events}}).ThreadAnalysis
	if analysis == nil || len(analysis.Resources) != 1 || analysis.Resources[0].Source != "unknown" ||
		analysis.Resources[0].Target != "GET /feed" {
		t.Fatalf("route misattributed as code source: %+v", analysis)
	}
}

func TestThreadResourcesStayDormantWithoutThreadCollector(t *testing.T) {
	events := []jhlog.Event{{Type: jhlog.EventHTTP, TimeMS: 120,
		Producer: jhlog.ProducerMetadata{HasThread: true, ThreadID: 151},
		HTTP:     &jhlog.HTTPEvent{DurationMS: 20}}}
	if analysis := inspectLogsForTest("without threads", []jhlog.Log{{Events: events}}).ThreadAnalysis; analysis != nil {
		t.Fatalf("unexpected thread evidence: %+v", analysis)
	}
}

func TestThreadResourceCollectorFlagDoesNotLeakAcrossSessions(t *testing.T) {
	producer := jhlog.ProducerMetadata{HasThread: true, ThreadID: 151}
	resource := jhlog.Event{Type: jhlog.EventIO, TimeMS: 120, Producer: producer,
		IO: &jhlog.IOEvent{Operation: jhlog.IOOperationFileRead, DurationUS: 1_000}}
	logs := []jhlog.Log{
		{Events: []jhlog.Event{{Type: jhlog.EventSession, Session: &jhlog.SessionEvent{
			CollectorFlags: uint64(jhlog.CollectorThread)}}, resource}},
		{Events: []jhlog.Event{{Type: jhlog.EventSession, Session: &jhlog.SessionEvent{}}, resource}},
	}
	analysis := inspectLogsForTest("thread flag reset", logs).ThreadAnalysis
	if analysis == nil || len(analysis.Resources) != 1 {
		t.Fatalf("collector flag leaked into disabled session: %+v", analysis)
	}
}

func BenchmarkThreadResourceCollection50k(b *testing.B) {
	event := jhlog.Event{TimeMS: 100_000, Producer: jhlog.ProducerMetadata{HasThread: true, ThreadID: 151}}
	b.ReportAllocs()
	for run := 0; run < b.N; run++ {
		collector := newCollector("resource benchmark", 1, Options{})
		collector.startLog(jhlog.SegmentHeader{})
		collector.threadAnalysis.resourceOn = true
		for index := 0; index < 50_000; index++ {
			event.TimeMS = uint64(100_000 + index)
			collector.recordThreadResource(event, "file_read", "app.Cache.read", "", "completed", 3)
		}
		if len(collector.threadAnalysis.finalize().Resources) != 50_000 {
			b.Fatal("resource retention limit changed")
		}
	}
}

func BenchmarkThreadResourceCollectorBaseline(b *testing.B) {
	b.ReportAllocs()
	for run := 0; run < b.N; run++ {
		collector := newCollector("resource benchmark", 1, Options{})
		collector.startLog(jhlog.SegmentHeader{})
		_ = collector.threadAnalysis.finalize()
	}
}

func BenchmarkThreadResourceCollectionDisabled50k(b *testing.B) {
	event := jhlog.Event{TimeMS: 100_000, Producer: jhlog.ProducerMetadata{HasThread: true, ThreadID: 151}}
	b.ReportAllocs()
	for run := 0; run < b.N; run++ {
		collector := newCollector("resource benchmark", 1, Options{})
		collector.startLog(jhlog.SegmentHeader{})
		for index := 0; index < 50_000; index++ {
			collector.recordThreadResource(event, "file_read", "app.Cache.read", "", "completed", 3)
		}
		if collector.threadAnalysis.finalize() != nil {
			b.Fatal("disabled collector retained a resource")
		}
	}
}
