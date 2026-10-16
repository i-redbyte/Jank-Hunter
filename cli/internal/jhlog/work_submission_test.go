package jhlog

import (
	"bytes"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestWorkSubmissionRoundTripPreservesIntentNotAcceptance(t *testing.T) {
	p := &WorkSubmissionEvent{Version: 1, UUIDMost: ^uint64(0), UUIDLeast: 7, ObservedElapsedNanos: 100, Periodic: true, HasUnique: true, UniqueName: "sync", Policy: WorkSubmissionUpdate, OmittedRequests: 9000}
	path := filepath.Join(t.TempDir(), "submission.jhlog")
	writeClosedEvents(t, path, []Event{{Type: EventWorkSubmission, DiagnosticEvent: &DiagnosticEvent{WorkSubmission: p}}})
	log, err := readLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(log.Events[0].WorkSubmission, p) {
		t.Fatal("submission metadata lost")
	}
}
func TestWorkSubmissionExplicitBuilderHintsRoundTrip(t *testing.T) {
	p := &WorkSubmissionEvent{Version: 3, UUIDMost: 11, UUIDLeast: 22, CallerSource: "app.Queue.enqueue",
		BackoffPolicy: 2, RequestedBackoffMS: 12_000, QuotaPolicy: 1}
	path := filepath.Join(t.TempDir(), "builder-hints.jhlog")
	writeClosedEvents(t, path, []Event{{Type: EventWorkSubmission, DiagnosticEvent: &DiagnosticEvent{WorkSubmission: p}}})
	log, err := readLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(log.Events[0].WorkSubmission, p) {
		t.Fatalf("builder hints lost: %+v", log.Events)
	}
}
func TestWorkSubmissionRejectsPolicyMismatchAndUnboundedNames(t *testing.T) {
	for _, p := range []*WorkSubmissionEvent{
		{Version: 1, CallerSource: "caller"},
		{Version: 2},
		{Version: 2, CallerSource: strings.Repeat("s", 513)},
		{Version: 1, Policy: WorkSubmissionKeep},
		{Version: 1, HasUnique: true, Policy: WorkSubmissionUpdate},
		{Version: 1, Periodic: true, HasUnique: true, Policy: WorkSubmissionAppend},
		{Version: 1, HasUnique: true, Policy: WorkSubmissionKeep, UniqueName: string(bytes.Repeat([]byte("x"), 513))},
		{Version: 2, CallerSource: "caller", BackoffPolicy: 1},
		{Version: 3, CallerSource: "caller", BackoffPolicy: 3},
		{Version: 3, CallerSource: "caller", RequestedBackoffMS: 1},
		{Version: 3, CallerSource: "caller", QuotaPolicy: 3},
	} {
		var output bytes.Buffer
		if err := (eventPayloadEncoder{writer: &output}).encodeWorkSubmission(p); err == nil {
			t.Fatalf("invalid intent accepted: %+v", p)
		}
	}
}
func TestWorkSubmissionBypassesFiveBitMicroPageTypeColumn(t *testing.T) {
	_, err := encodeMicroPageRecord([]microPageRow{{eventType: EventWorkSubmission}}, nil, false)
	if err == nil {
		t.Fatal("type33 entered a five-bit column")
	}
}

func TestDirectSubmissionKeepsTimeAndContextAcrossMicroPage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mixed.jhlog")
	submission := func(at uint64) Event {
		return Event{Type: EventWorkSubmission, TimeMS: at, Attribution: AttributionContext{Present: true, OperationID: 42}, DiagnosticEvent: &DiagnosticEvent{WorkSubmission: &WorkSubmissionEvent{Version: 1, UUIDLeast: 7}}}
	}
	events := []Event{submission(100), {Type: EventMemory, TimeMS: 200, Attribution: AttributionContext{Present: true}, Memory: &MemoryEvent{PSSKB: 1}}, submission(300)}
	writeClosedEvents(t, path, events)
	log, err := readLog(path)
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range events {
		got := log.Events[i]
		if got.TimeMS != want.TimeMS || got.Attribution.OperationID != want.Attribution.OperationID {
			t.Fatalf("row %d state contaminated across page: got time=%d operation=%d; want time=%d operation=%d", i, got.TimeMS, got.Attribution.OperationID, want.TimeMS, want.Attribution.OperationID)
		}
	}
}

func TestLegacyLargeDirectSnapshotAlsoKeepsMicroPageBoundaryState(t *testing.T) {
	rows := make([]ThreadObservation, 600)
	for i := range rows {
		rows[i] = ThreadObservation{TID: uint64(i + 1), Name: string(bytes.Repeat([]byte("n"), 128)), OSState: "S"}
	}
	snapshot := func(at uint64) Event {
		return Event{Type: EventThreadSnapshot, TimeMS: at, Attribution: AttributionContext{Present: true, OperationID: 42}, DiagnosticEvent: &DiagnosticEvent{ThreadSnapshot: &ThreadSnapshotEvent{Version: 1, Threads: rows}}}
	}
	events := []Event{snapshot(100), {Type: EventMemory, TimeMS: 200, Attribution: AttributionContext{Present: true}, Memory: &MemoryEvent{PSSKB: 1}}, snapshot(300)}
	path := filepath.Join(t.TempDir(), "legacy-mixed.jhlog")
	writeClosedEvents(t, path, events)
	log, err := readLog(path)
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range events {
		got := log.Events[i]
		if got.TimeMS != want.TimeMS || got.Attribution.OperationID != want.Attribution.OperationID {
			t.Fatalf("legacy mixed record %d contaminated: %+v", i, got)
		}
	}
}

func TestMinifiedSubmissionFixtureKeepsUniquePoliciesAndCallerContext(t *testing.T) {
	log, err := readLog("../../../wire/testdata/work-submission-r8-5.1.0.jhlog")
	if err != nil {
		t.Fatal(err)
	}
	calls, keep, update := 0, 0, 0
	for _, event := range log.Events {
		if event.DiagnosticEvent == nil || event.WorkSubmission == nil {
			continue
		}
		p := event.WorkSubmission
		calls++
		if p.HasUnique {
			if !event.Attribution.Present || event.Attribution.OperationID == 0 {
				t.Fatal("lost caller context at direct/page boundary")
			}
			switch p.Policy {
			case WorkSubmissionKeep:
				keep++
			case WorkSubmissionUpdate:
				update++
			}
		}
	}
	if calls != 14 || keep != 2 || update != 1 {
		t.Fatalf("missing call evidence: calls=%d keep=%d update=%d", calls, keep, update)
	}
}

func TestMinifiedNestedEnqueueCallsRemainSeparateFromChainDeclarations(t *testing.T) {
	log, err := readLog("../../../wire/testdata/work-nested-enqueue-r8-5.1.0.jhlog")
	if err != nil {
		t.Fatal(err)
	}
	var submissions, sourced, roots, edges int
	for _, event := range log.Events {
		if event.DiagnosticEvent == nil {
			continue
		}
		if direct := event.WorkSubmission; direct != nil {
			submissions++
			if direct.CallerSource != "" {
				sourced++
			}
		}
		if event.WorkChainRoot != nil {
			roots++
		}
		if event.WorkChainEdge != nil {
			edges++
		}
	}
	if submissions != 18 || sourced != 9 || roots != 3 || edges != 3 {
		t.Fatalf("nested enqueue lost metadata or mixed declarations: direct=%d sourced=%d roots=%d edges=%d",
			submissions, sourced, roots, edges)
	}
}

func TestWorkSubmissionCallerSourceRoundTripAndLegacyUnknown(t *testing.T) {
	for _, version := range []uint64{1, 2} {
		value := &WorkSubmissionEvent{Version: version}
		if version == 2 {
			value.CallerSource = "example.Sync.enqueue"
			value.SourceTruncated = true
		}
		path := filepath.Join(t.TempDir(), "caller.jhlog")
		writeClosedEvents(t, path, []Event{{Type: EventWorkSubmission, DiagnosticEvent: &DiagnosticEvent{WorkSubmission: value}}})
		log, err := readLog(path)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(value, log.Events[0].WorkSubmission) {
			t.Fatal("caller source or unknown source lost")
		}
	}
}

func TestR8DirectWorkSubmissionSourceSurvivesShrinkingWithoutDuplicateAdapters(t *testing.T) {
	log, err := readLog("../../../wire/testdata/work-submission-direct-r8-5.1.0.jhlog")
	if err != nil {
		t.Fatal(err)
	}
	var all, callers int
	for _, event := range log.Events {
		if event.DiagnosticEvent == nil || event.WorkSubmission == nil {
			continue
		}
		all++
		p := event.WorkSubmission
		if p.Version == 2 {
			callers++
			if !strings.HasPrefix(p.CallerSource, "com.example.jhsmoke.WorkerExecutionProbe") {
				t.Fatalf("obfuscated or missing caller: %q", p.CallerSource)
			}
		}
	}
	if all != 16 || callers != 7 {
		t.Fatalf("missing or duplicate captures: all=%d automatic=%d", all, callers)
	}
}

func TestWorkSubmissionSourceFlagIsVersionScoped(t *testing.T) {
	for _, payload := range [][]byte{{1, 1, 2, 3, 8, 0, 0}, {2, 1, 2, 3, 16, 0, 0}} {
		var event Event
		if err := decodeWorkSubmission(&recordReader{data: payload}, &event); err == nil {
			t.Fatal("reserved/version-incompatible source flag accepted")
		}
	}
}

func TestWorkScheduleHintsRoundTripAndValidation(t *testing.T) {
	period, flex, delay := uint64(1800000), uint64(300000), uint64(0)
	for _, v := range []*WorkSubmissionEvent{
		{Version: 4, CallerSource: "app.schedule", Periodic: true, RequestedPeriodMS: &period, RequestedFlexMS: &flex, RequestedInitialDelayMS: &delay},
		{Version: 4, CallerSource: "app.schedule", Periodic: true, RequestedPeriodMS: &period},
		{Version: 4, CallerSource: "app.schedule", RequestedInitialDelayMS: &delay},
	} {
		path := filepath.Join(t.TempDir(), "schedule.jhlog")
		writeClosedEvents(t, path, []Event{{Type: EventWorkSubmission, DiagnosticEvent: &DiagnosticEvent{WorkSubmission: v}}})
		log, err := readLog(path)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(log.Events[0].WorkSubmission, v) {
			t.Fatalf("lost schedule: %+v", log.Events[0].WorkSubmission)
		}
	}
	for _, v := range []*WorkSubmissionEvent{
		{Version: 3, CallerSource: "app.schedule", RequestedInitialDelayMS: &delay},
		{Version: 4, CallerSource: "app.schedule", RequestedPeriodMS: &period},
		{Version: 4, CallerSource: "app.schedule", Periodic: true, RequestedFlexMS: &flex},
	} {
		if validWorkSubmission(v) {
			t.Fatalf("invalid schedule accepted: %+v", v)
		}
	}
}

func TestAndroidArtScheduleFixtureUsesSchemaFour(t *testing.T) {
	log, err := readLog("../../../wire/testdata/work-schedule-art-5.3.0.jhlog")
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, event := range log.Events {
		if event.DiagnosticEvent == nil || event.WorkSubmission == nil {
			continue
		}
		p := event.WorkSubmission
		found++
		if p.Version != 4 || p.RequestedPeriodMS == nil || *p.RequestedPeriodMS != 1800000 || p.RequestedFlexMS == nil || *p.RequestedFlexMS != 300000 || p.RequestedInitialDelayMS == nil || *p.RequestedInitialDelayMS != 0 {
			t.Fatalf("ART schedule lost: %+v", p)
		}
	}
	if found != 1 {
		t.Fatalf("ART submission count %d", found)
	}
}

func TestAndroidArtActiveFacadePreservesSchedulesAcrossDisable(t *testing.T) {
	log, err := readLog("../../../wire/testdata/work-schedule-active-art-5.3.0.jhlog")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]uint64{"active-first": 1800000, "active-second": 2400000}
	for _, event := range log.Events {
		if event.DiagnosticEvent == nil || event.WorkSubmission == nil {
			continue
		}
		p := event.WorkSubmission
		period, ok := want[p.UniqueName]
		if !ok {
			t.Fatalf("unexpected or disabled submission: %+v", p)
		}
		if p.Version != 4 || p.RequestedPeriodMS == nil || *p.RequestedPeriodMS != period || p.RequestedFlexMS == nil || *p.RequestedFlexMS != 300000 || p.RequestedInitialDelayMS == nil || *p.RequestedInitialDelayMS != 0 {
			t.Fatalf("facade schedule lost: %+v", p)
		}
		delete(want, p.UniqueName)
	}
	if len(want) != 0 {
		t.Fatalf("missing active submissions: %v", want)
	}
}
