package jhlog

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func causalShortThreadGolden(t *testing.T) []byte {
	t.Helper()
	header := DefaultSegmentHeader()
	header.ProcessName = "example.app"
	header.RunID[0], header.ProcessInstanceID[0], header.SessionID[0] = 1, 2, 3
	var output bytes.Buffer
	writer, err := NewWriterWithOptions(&output, WriterOptions{Header: header, GZIP: false})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteEvent(Event{Type: EventSession, TimeUS: 1_000_000,
		Session: &SessionEvent{ProcessName: "example.app", SDKInt: 35,
			CollectorFlags: 0x1fff | uint64(CollectorConcurrencyDiagnostics|CollectorConcurrencyDetailed)}}); err != nil {
		t.Fatal(err)
	}
	for index, phase := range []uint64{1, 3, 4, 6, 7, 11} {
		observed := uint64(1_000_000_000 + index*1_000_000)
		causal := &CausalEvent{Version: 1, EntityKind: CausalThread, Phase: phase, EntityID: 7,
			ObservedElapsedNanos: observed, SourceOrigin: CausalOriginProject,
			Presence: CausalHasSource, SourceSiteID: 9,
			SourceMethod: "example.app.Screen#startWorker()V", SourceLine: 42}
		if phase == 6 || phase == 7 {
			causal.Presence |= CausalHasTargetTID | CausalHasTargetJavaID
			causal.TargetTID, causal.TargetJavaID = 77, 99
		}
		if phase == 7 {
			causal.Presence |= CausalHasDurationNanos | CausalHasCPUNanos
			causal.DurationNanos, causal.CPUNanos = 1_000_000, 100_000
		}
		if err := writer.WriteEvent(Event{Type: EventCausalEvent, TimeUS: observed / 1_000,
			DiagnosticEvent: &DiagnosticEvent{CausalEvent: causal}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func TestCausalShortThreadGoldenAndMixedLegacyVersions(t *testing.T) {
	goldenPath := filepath.Join("..", "..", "..", "wire", "testdata", "causal-short-thread-5.3.0.bin")
	want := causalShortThreadGolden(t)
	if os.Getenv("JH_UPDATE_CAUSAL_GOLDEN") == "1" {
		if err := os.WriteFile(goldenPath, want, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("committed 5.3 fixture differs from deterministic writer output")
	}
	paths := []string{
		filepath.Join("..", "..", "..", "wire", "testdata", "legacy-5.0.0.jhlog"),
		filepath.Join("..", "..", "..", "wire", "testdata", "thread-lifecycle-r8-5.1.0.jhlog"),
		filepath.Join("..", "..", "..", "wire", "testdata", "causal-short-thread-5.2.0.bin"),
		goldenPath,
	}
	for index, path := range paths {
		var causal, snapshots uint64
		result, err := StreamFileWithResult(path, func(event Event, _ map[uint64]string) error {
			if event.DiagnosticEvent == nil {
				return nil
			}
			if event.CausalEvent != nil {
				causal++
				if index < 2 || event.CausalEvent.EntityKind != CausalThread || event.CausalEvent.EntityID != 7 {
					t.Errorf("unexpected causal identity in %s: %+v", path, event.CausalEvent)
				}
			}
			if event.ThreadSnapshot != nil {
				snapshots++
			}
			return nil
		})
		if err != nil {
			t.Fatalf("mixed version %s: %v", path, err)
		}
		version := result.FormatVersion
		if result.ProcessFile {
			version = ""
			for _, segment := range result.Segments {
				if segment.FormatVersion != "5.1.0" || segment.Status != SegmentStatusClosedClean || !segment.Sealed {
					t.Fatalf("mixed process segment %s: version=%s status=%s sealed=%v", path,
						segment.FormatVersion, segment.Status, segment.Sealed)
				}
			}
			if len(result.Segments) > 0 {
				version = result.Segments[0].FormatVersion
			}
		}
		if version != []string{"5.0.0", "5.1.0", "5.2.0", "5.3.0"}[index] ||
			result.Status != SegmentStatusClosedClean || !result.Sealed {
			t.Fatalf("mixed version status %s: version=%s status=%s sealed=%v", path,
				version, result.Status, result.Sealed)
		}
		if index >= 2 && (causal != 6 || snapshots != 0 || result.RecordsByType[EventCausalEvent] != 6) {
			t.Fatalf("short-lived 5.2 thread needs six point events and no snapshot: causal=%d snapshots=%d records=%d", causal, snapshots, result.RecordsByType[EventCausalEvent])
		}
		if index < 2 && causal != 0 {
			t.Fatalf("legacy version %s acquired invented causal data: %d", path, causal)
		}
	}
}

func TestCausalEventRoundTripKeepsExplicitIdentityAndKnownZero(t *testing.T) {
	want := &CausalEvent{
		Version: 1, EntityKind: CausalOperation, Phase: 1, EntityID: 17,
		ParentKind: CausalExecutorTask, ParentID: 3, ActorKind: CausalExecutorTask, ActorID: 3,
		ObservedElapsedNanos: 9_000_000_000, SourceOrigin: CausalOriginDependency,
		Presence:        CausalHasTargetJavaID | CausalHasTargetTID | CausalHasSource | CausalHasAppCaller | CausalHasDurationNanos | CausalHasReadBytes | CausalHasWrittenBytes | CausalHasOperationKind | CausalHasLeaseID,
		TargetJavaID:    88,
		TargetTID:       123,
		SourceSiteID:    10,
		SourceMethod:    "library.Store#read()V",
		SourceLine:      40,
		AppCallerSiteID: 11,
		AppCallerMethod: "app.Screen#load()V",
		AppCallerLine:   74,
		DurationNanos:   550_000,
		ReadBytes:       0,
		WrittenBytes:    0,
		OperationKind:   1,
		LeaseID:         19,
	}
	path := filepath.Join(t.TempDir(), "causal.jhlog")
	writeClosedEvents(t, path, []Event{{Type: EventCausalEvent, DiagnosticEvent: &DiagnosticEvent{CausalEvent: want}}})
	log, err := readLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(log.Events) != 1 || !reflect.DeepEqual(log.Events[0].CausalEvent, want) {
		t.Fatalf("causal round-trip lost fields: %+v", log.Events)
	}
	if log.Result.RecordsByType[EventCausalEvent] != 1 || log.Result.Events != 1 {
		t.Fatalf("type39 should be a direct semantic record: %+v", log.Result)
	}
}

func TestCausalPeriodicCadenceAndWorkerBindingRoundTrip(t *testing.T) {
	want := &CausalEvent{
		Version: 1, EntityKind: CausalExecutorTask, Phase: 1, EntityID: 7,
		ParentKind: CausalSeries, ParentID: 2, SchedulerKind: CausalExecutor, SchedulerID: 4,
		ObservedElapsedNanos: 100, SourceOrigin: CausalOriginProject,
		Presence: CausalHasSource | CausalHasOccurrence | CausalHasScheduledFor |
			CausalHasRequestedPeriod | CausalHasScheduleCadence,
		SourceMethod: "app.Poller#tick()V", SourceLine: 13, Occurrence: 3,
		ScheduledForNanos: 200, RequestedPeriodNanos: 1_000_000_000, ScheduleCadence: 2,
	}
	var output bytes.Buffer
	if err := (eventPayloadEncoder{writer: &output}).encodeCausalEvent(want); err != nil {
		t.Fatal(err)
	}
	event := Event{Type: EventCausalEvent}
	if err := decodeCausalEvent(&recordReader{data: output.Bytes()}, &event); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(event.CausalEvent, want) {
		t.Fatalf("periodic wire mismatch: %+v", event.CausalEvent)
	}

	worker := &CausalEvent{Version: 1, EntityKind: CausalWorkerAttempt, Phase: 1, EntityID: 8,
		ObservedElapsedNanos: 300, Presence: CausalHasWorkerBinding, WorkerInstanceID: 9, RunAttempt: 2, Generation: 3}
	output.Reset()
	if err := (eventPayloadEncoder{writer: &output}).encodeCausalEvent(worker); err != nil {
		t.Fatal(err)
	}
	event = Event{Type: EventCausalEvent}
	if err := decodeCausalEvent(&recordReader{data: output.Bytes()}, &event); err != nil || !reflect.DeepEqual(event.CausalEvent, worker) {
		t.Fatalf("worker binding wire mismatch: %+v, %v", event.CausalEvent, err)
	}
}

func TestCausalWorkerBindingKeepsUnsignedLegacyInstanceID(t *testing.T) {
	// Runtime worker IDs are signed Long bit patterns. A negative Kotlin value
	// must remain the same unsigned uvarint key as the legacy worker identity.
	const instanceID uint64 = 0xfedcba9876543210
	identity := &WorkerIdentityEvent{Version: 1, InstanceID: instanceID, UUIDMost: 0x1234, UUIDLeast: 0x5678}
	worker := &CausalEvent{Version: 1, EntityKind: CausalWorkerAttempt, Phase: 1, EntityID: 7,
		ObservedElapsedNanos: 300, Presence: CausalHasWorkerBinding,
		WorkerInstanceID: instanceID, RunAttempt: 2, Generation: 3}
	path := filepath.Join(t.TempDir(), "worker-unsigned.jhlog")
	writeClosedEvents(t, path, []Event{
		{Type: EventWorkerIdentity, DiagnosticEvent: &DiagnosticEvent{WorkerIdentity: identity}},
		{Type: EventCausalEvent, DiagnosticEvent: &DiagnosticEvent{CausalEvent: worker}},
	})
	log, err := readLog(path)
	if err != nil {
		t.Fatal(err)
	}
	var legacyID, causalID uint64
	for _, event := range log.Events {
		if event.WorkerIdentity != nil {
			legacyID = event.WorkerIdentity.InstanceID
		}
		if event.CausalEvent != nil {
			causalID = event.CausalEvent.WorkerInstanceID
		}
	}
	if legacyID != instanceID || causalID != instanceID || legacyID != causalID {
		t.Fatalf("signed Long bit pattern lost across legacy/causal wire: legacy=%x causal=%x", legacyID, causalID)
	}
}

func TestCausalLifecycleDistinguishesRequestReturnThrowAndImplementation(t *testing.T) {
	for _, event := range []CausalEvent{
		{Version: 1, EntityKind: CausalHandlerPost, Phase: 1, EntityID: 12,
			Presence: CausalHasRequestedDelay, RequestedDelayNanos: 0},
		{Version: 1, EntityKind: CausalHandlerPost, Phase: 1, EntityID: 13,
			Presence: CausalHasRequestedDelay, RequestedDelayNanos: 500_000_000},
		{Version: 1, EntityKind: CausalHandlerPost, Phase: 7, EntityID: 11},
		{Version: 1, EntityKind: CausalHandlerPost, Phase: 8, EntityID: 11},
		{Version: 1, EntityKind: CausalSeries, Phase: 3, EntityID: 1},
		{Version: 1, EntityKind: CausalSeries, Phase: 4, EntityID: 1, Presence: CausalHasCallResult, CallResult: 2},
		{Version: 1, EntityKind: CausalExecutor, Phase: 7, EntityID: 2},
		{Version: 1, EntityKind: CausalWorkerAttempt, Phase: 6, EntityID: 3, ActorKind: CausalWorkerAttempt, ActorID: 3,
			Presence: CausalHasSource, SourceOrigin: CausalOriginProject, SourceMethod: "app.SyncWorker#doWork()V", SourceLine: 31},
		{Version: 1, EntityKind: CausalOperation, Phase: 1, EntityID: 4,
			Presence: CausalHasDurationNanos | CausalHasOperationKind, OperationKind: CausalOperationContentWrite},
		{Version: 1, EntityKind: CausalDirectExecution, Phase: 2, EntityID: 5,
			ParentKind: CausalThread, ParentID: 1, Presence: CausalHasTargetTID | CausalHasDurationNanos | CausalHasCPUNanos,
			TargetTID: 42, DurationNanos: 500, CPUNanos: 100},
		{Version: 1, EntityKind: CausalResource, Phase: 6, EntityID: 6,
			Presence: CausalHasResourceKind | CausalHasLeaseScope, ResourceKind: CausalResourceAutoCloseable},
		{Version: 1, EntityKind: CausalResource, Phase: 2, EntityID: 7,
			Presence: CausalHasResourceKind | CausalHasLeaseScope, ResourceKind: CausalResourceAutoCloseable},
		{Version: 1, EntityKind: CausalResource, Phase: 7, EntityID: 8,
			Presence: CausalHasResourceKind | CausalHasLeaseScope, ResourceKind: CausalResourceAutoCloseable,
			LeaseScopeKind: CausalExecutorTask, LeaseScopeID: 5, BoundToScope: 1},
		{Version: 1, EntityKind: CausalCoroutine, Phase: 6, EntityID: 9,
			Presence: CausalHasSource, SourceOrigin: CausalOriginProject,
			SourceMethod: "app.StateMachine#invokeSuspend(Ljava/lang/Object;)Ljava/lang/Object;", SourceLine: 42},
	} {
		var payload bytes.Buffer
		if err := (eventPayloadEncoder{writer: &payload}).encodeCausalEvent(&event); err != nil {
			t.Fatalf("encode %+v: %v", event, err)
		}
		var decoded Event
		if err := decodeCausalEvent(&recordReader{data: payload.Bytes()}, &decoded); err != nil || !reflect.DeepEqual(decoded.CausalEvent, &event) {
			t.Fatalf("round-trip %+v: %+v, %v", event, decoded.CausalEvent, err)
		}
	}
}

func TestCausalEventRejectsUnsupportedOrInventedEvidence(t *testing.T) {
	base := CausalEvent{Version: 1, EntityKind: CausalThread, Phase: 4, EntityID: 1, ObservedElapsedNanos: 100}
	for _, mutation := range []func(*CausalEvent){
		func(v *CausalEvent) { v.Presence = 1 << 22 },
		func(v *CausalEvent) { v.Presence = CausalHasRequestedDelay },
		func(v *CausalEvent) {
			v.EntityKind = CausalHandlerPost
			v.Phase = 2
			v.Presence = CausalHasRequestedDelay
		},
		func(v *CausalEvent) { v.ParentKind = CausalThread },
		func(v *CausalEvent) { v.Presence = CausalHasSource; v.SourceOrigin = CausalOriginProject },
		func(v *CausalEvent) { v.EntityKind = CausalOperation; v.Phase = 1 },
		func(v *CausalEvent) { v.EntityKind = CausalWorkerAttempt; v.Phase = 1 },
		func(v *CausalEvent) { v.Presence = CausalHasTerminalOutcome; v.TerminalOutcome = 7 },
		func(v *CausalEvent) { v.Presence = CausalHasScheduleCadence; v.ScheduleCadence = 2 },
		func(v *CausalEvent) {
			v.EntityKind = CausalResource
			v.Phase = 2
			v.Presence = CausalHasResourceKind | CausalHasLeaseScope
			v.ResourceKind = CausalResourceAutoCloseable
			v.BoundToScope = 1
		},
		func(v *CausalEvent) {
			v.EntityKind = CausalDirectExecution
			v.Phase = 1
			v.SchedulerKind = CausalExecutor
			v.SchedulerID = 1
		},
		func(v *CausalEvent) {
			v.EntityKind = CausalResource
			v.Phase = 6
			v.Presence = CausalHasResourceKind
			v.ResourceKind = 1
		},
		func(v *CausalEvent) {
			v.EntityKind = CausalResource
			v.Phase = 8
			v.Presence = CausalHasResourceKind
			v.ResourceKind = 1
		},
		func(v *CausalEvent) { v.EntityKind = CausalCoroutine; v.Phase = 7 },
	} {
		bad := base
		mutation(&bad)
		var payload bytes.Buffer
		if err := (eventPayloadEncoder{writer: &payload}).encodeCausalEvent(&bad); err == nil {
			t.Fatalf("invalid causal event accepted: %+v", bad)
		}
	}
	var payload bytes.Buffer
	if err := (eventPayloadEncoder{writer: &payload}).encodeCausalEvent(&base); err != nil {
		t.Fatal(err)
	}
	encoded := payload.Bytes()
	for n := 0; n < len(encoded); n++ {
		event := Event{Type: EventCausalEvent}
		if err := decodeCausalEvent(&recordReader{data: encoded[:n]}, &event); err == nil {
			t.Fatalf("accepted truncated payload %d/%d", n, len(encoded))
		}
	}
}

func TestCausalCollectorMaskAndLegacyFeatureGate(t *testing.T) {
	var output bytes.Buffer
	encoder := eventPayloadEncoder{writer: &output}
	for _, flags := range []uint64{0x1fff, uint64(CollectorConcurrencyDiagnostics), uint64(CollectorConcurrencyDiagnostics | CollectorConcurrencyDetailed)} {
		output.Reset()
		if err := encoder.encodeSession(&SessionEvent{CollectorFlags: flags}, false, false); err != nil {
			t.Fatalf("current collector mask 0x%x rejected: %v", flags, err)
		}
	}
	if err := encoder.encodeSession(&SessionEvent{CollectorFlags: uint64(CollectorConcurrencyDetailed)}, false, false); err == nil {
		t.Fatal("detailed without diagnostics accepted")
	}
	if err := encoder.encodeSession(&SessionEvent{CollectorFlags: uint64(CollectorConcurrencyDiagnostics)}, false, true); err == nil {
		t.Fatal("causal collector bit accepted without feature32")
	}
	if err := encoder.encodeSession(&SessionEvent{CollectorFlags: 1 << 15}, false, false); err == nil {
		t.Fatal("unknown collector bit accepted")
	}
	header := DefaultSegmentHeader()
	header.RequiredFeatures &^= FeatureCausalConcurrency
	var file bytes.Buffer
	w, err := NewWriterWithOptions(&file, WriterOptions{Header: header})
	if err != nil {
		t.Fatal(err)
	}
	err = w.WriteEvent(Event{Type: EventCausalEvent, DiagnosticEvent: &DiagnosticEvent{CausalEvent: &CausalEvent{Version: 1, EntityKind: CausalThread, Phase: 4, EntityID: 1}}})
	if err == nil || !strings.Contains(err.Error(), "feature32") {
		t.Fatalf("type39 without feature32: %v", err)
	}
}
