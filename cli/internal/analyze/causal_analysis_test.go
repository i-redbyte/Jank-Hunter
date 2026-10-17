package analyze

import (
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func causalTestHeader(run, process, session byte) jhlog.SegmentHeader {
	header := jhlog.DefaultSegmentHeader()
	header.RunID[0], header.ProcessInstanceID[0], header.SessionID[0] = run, process, session
	header.ProcessName, header.OSPID = "example.app", 42
	return header
}

func causalTestOpen(state *causalCollectorState, header jhlog.SegmentHeader) {
	state.startSegment(header)
	state.session(uint64(jhlog.CollectorConcurrencyDiagnostics))
}

func causalTestClose(state *causalCollectorState, header jhlog.SegmentHeader) {
	state.segmentResult(jhlog.StreamResult{Header: header, Sealed: true, Status: jhlog.SegmentStatusClosedClean,
		LatestQuality: &jhlog.QualitySnapshot{Counters: map[uint64]uint64{}}})
}

func causalTestSource(event *jhlog.CausalEvent, origin jhlog.CausalSourceOrigin, source string, line uint64) {
	event.SourceOrigin, event.SourceMethod, event.SourceLine = origin, source, line
	event.Presence |= jhlog.CausalHasSource
}

func TestCausalTimelineOffsetsUseFirstObservedEventWithoutRounding(t *testing.T) {
	header := causalTestHeader(1, 2, 3)
	var state causalCollectorState
	causalTestOpen(&state, header)
	const first uint64 = 18_000_000_000_000_001
	for _, observed := range []uint64{first + 5_000_000, first, first + 3_000_000} {
		event := &jhlog.CausalEvent{Version: 1, EntityKind: jhlog.CausalThread,
			EntityID: 1, Phase: 1, ObservedElapsedNanos: observed}
		causalTestSource(event, jhlog.CausalOriginProject, "example.app.Screen#start()V", 10)
		state.record(event)
	}
	causalTestClose(&state, header)
	view := state.finalize().AllEntities[0]
	if view.FirstNS != first || view.ObservedSpanNS != 5_000_000 || len(view.Events) != 3 ||
		view.Events[0].RelativeNS != 5_000_000 || view.Events[1].RelativeNS != 0 ||
		view.Events[2].RelativeNS != 3_000_000 {
		t.Fatalf("relative timeline changed or rounded: %+v", view)
	}
}

func TestCausalCollectorEntityAndEventLimitsRemainVisible(t *testing.T) {
	header := causalTestHeader(1, 2, 3)
	var entities causalCollectorState
	causalTestOpen(&entities, header)
	for id := uint64(1); id <= maxCausalEntities+1; id++ {
		event := &jhlog.CausalEvent{EntityKind: jhlog.CausalThread, EntityID: id, Phase: 1}
		causalTestSource(event, jhlog.CausalOriginProject, "app.Pool#newThread()V", 14)
		entities.record(event)
	}
	causalTestClose(&entities, header)
	result := entities.finalize()
	if result.Entities != maxCausalEntities || result.DroppedCLI != 1 || result.Complete {
		t.Fatalf("entity cap or loss visibility failed: entities=%d dropped=%d complete=%v", result.Entities, result.DroppedCLI, result.Complete)
	}
	var events causalCollectorState
	causalTestOpen(&events, header)
	for i := 0; i < maxCausalEvents+1; i++ {
		event := &jhlog.CausalEvent{EntityKind: jhlog.CausalThread, EntityID: 1, Phase: 1}
		causalTestSource(event, jhlog.CausalOriginProject, "app.Pool#newThread()V", 14)
		events.record(event)
	}
	causalTestClose(&events, header)
	result = events.finalize()
	if result.Events != maxCausalEvents || result.DroppedCLI != 1 || result.Complete {
		t.Fatalf("event cap or loss visibility failed: events=%d dropped=%d complete=%v", result.Events, result.DroppedCLI, result.Complete)
	}
}

func TestCausalCollectorBoundsEmptyScopesAndSegmentIndexes(t *testing.T) {
	var state causalCollectorState
	for index := 0; index < maxCausalScopes; index++ {
		header := causalTestHeader(byte(index), byte(index>>8), 3)
		state.startSegment(header)
	}
	if len(state.scopes) != maxCausalScopes {
		t.Fatalf("scope bound not reached: %d", len(state.scopes))
	}
	overflow := causalTestHeader(0, 0, 4)
	state.startSegment(overflow)
	state.record(&jhlog.CausalEvent{EntityKind: jhlog.CausalThread, EntityID: 1, Phase: 1})
	if len(state.scopes) != maxCausalScopes || state.dropped != 2 || len(state.nodes) != 0 {
		t.Fatalf("overflow scope was retained or loss hidden: scopes=%d dropped=%d nodes=%d", len(state.scopes), state.dropped, len(state.nodes))
	}
	var segments causalCollectorState
	header := causalTestHeader(1, 2, 3)
	segments.startSegment(header)
	segments.session(uint64(jhlog.CollectorConcurrencyDiagnostics))
	segments.segmentKeys = maxCausalSegmentKeys - 1
	for index := uint64(0); index < 2; index++ {
		part := header
		part.SegmentIndex = index
		segments.segmentResult(jhlog.StreamResult{Header: part, Sealed: true, Status: jhlog.SegmentStatusClosedClean,
			LatestQuality: &jhlog.QualitySnapshot{Counters: map[uint64]uint64{}}})
	}
	q := segments.scopes[causalScopeOf(header)]
	if !q.segmentGap || len(q.segmentSeen) != 1 || segments.segmentKeys != maxCausalSegmentKeys || segments.dropped != 1 ||
		segments.scopeComplete(causalScopeOf(header)) {
		t.Fatalf("segment key overflow was treated as complete: gap=%v retained=%d total=%d dropped=%d", q.segmentGap, len(q.segmentSeen), segments.segmentKeys, segments.dropped)
	}
}

func TestCausalOperationRequiresMeasuredMainTIDAndExactAppSource(t *testing.T) {
	header := causalTestHeader(1, 2, 3)
	var state causalCollectorState
	causalTestOpen(&state, header)
	main := &jhlog.CausalEvent{Version: 1, EntityKind: jhlog.CausalOperation, Phase: 1, EntityID: 1,
		ObservedElapsedNanos: 10, Presence: jhlog.CausalHasTargetTID | jhlog.CausalHasDurationNanos | jhlog.CausalHasOperationKind,
		TargetTID: 42, DurationNanos: 120_000_000, OperationKind: jhlog.CausalOperationContentRead}
	causalTestSource(main, jhlog.CausalOriginProject, "example.app.Screen#load()V", 74)
	state.record(main)
	background := *main
	background.EntityID, background.TargetTID = 2, 99
	state.record(&background)
	unknown := *main
	unknown.EntityID, unknown.SourceOrigin, unknown.SourceMethod = 3, jhlog.CausalOriginUnknown, "unknown.Library#read()V"
	state.record(&unknown)
	causalTestClose(&state, header)
	analysis := state.finalize()
	if len(analysis.Findings) != 1 || analysis.Findings[0].Count != 1 || analysis.Findings[0].SourceLine != 74 ||
		analysis.Findings[0].Rule != "main_thread_blocking_operation" || !analysis.Complete {
		t.Fatalf("measured app finding = %+v, coverage = %q", analysis.Findings, analysis.Coverage)
	}
}

func TestCausalResourceFindingRequiresBoundCompleteScopeAndNoTransfer(t *testing.T) {
	for _, scenario := range []struct {
		name                                  string
		bound, transfer, dropped, lateRelease bool
		want                                  int
	}{
		{"bound complete", true, false, false, false, 1},
		{"returned open resource", false, false, false, false, 0},
		{"transferred lease", true, true, false, false, 0},
		{"lossy window", true, false, true, false, 0},
		{"release after scope", true, false, false, true, 1},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			header := causalTestHeader(1, 2, 3)
			var state causalCollectorState
			causalTestOpen(&state, header)
			owner := &jhlog.CausalEvent{Version: 1, EntityKind: jhlog.CausalExecutorTask, Phase: 5, EntityID: 5, ObservedElapsedNanos: 20}
			causalTestSource(owner, jhlog.CausalOriginProject, "example.app.Store#save()V", 31)
			state.record(owner)
			lease := &jhlog.CausalEvent{Version: 1, EntityKind: jhlog.CausalResource, Phase: 2, EntityID: 9,
				ParentKind: jhlog.CausalExecutorTask, ParentID: 5, ObservedElapsedNanos: 10,
				Presence: jhlog.CausalHasResourceKind | jhlog.CausalHasLeaseScope, ResourceKind: jhlog.CausalResourceAutoCloseable,
				LeaseScopeKind: jhlog.CausalExecutorTask, LeaseScopeID: 5}
			if scenario.bound {
				lease.BoundToScope = 1
			}
			causalTestSource(lease, jhlog.CausalOriginProject, "example.app.Store#open()V", 27)
			state.record(lease)
			if scenario.transfer {
				transfer := *lease
				transfer.Phase, transfer.ObservedElapsedNanos, transfer.BoundToScope = 6, 15, 0
				state.record(&transfer)
			}
			if scenario.lateRelease {
				release := *lease
				release.Phase, release.ObservedElapsedNanos = 4, 1_020_000_000
				state.record(&release)
			}
			causalTestClose(&state, header)
			if scenario.dropped {
				state.scopes[causalScopeOf(header)].quality[jhlog.EventQualityCounterID(jhlog.EventCausalEvent, jhlog.QualityLossQueueFull)] = 1
			}
			analysis := state.finalize()
			if len(analysis.Findings) != scenario.want {
				t.Fatalf("findings = %+v, want %d", analysis.Findings, scenario.want)
			}
			if scenario.lateRelease && !strings.Contains(analysis.Findings[0].Consequence, "через 1019 мс") {
				t.Fatalf("late release duration lost: %+v", analysis.Findings[0])
			}
		})
	}
}

func TestCausalUseOfExistingResourceDoesNotClaimAcquisitionOrOpenState(t *testing.T) {
	for _, scenario := range []struct {
		name       string
		releaseAt  uint64
		wantIssues int
	}{
		{"scope ended without close", 0, 1},
		{"close before scope end", 15, 0},
		{"close after scope end", 30, 1},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			header := causalTestHeader(1, 2, 3)
			var state causalCollectorState
			causalTestOpen(&state, header)
			owner := &jhlog.CausalEvent{Version: 1, EntityKind: jhlog.CausalExecutorTask,
				Phase: 5, EntityID: 5, ObservedElapsedNanos: 20}
			causalTestSource(owner, jhlog.CausalOriginProject, "example.app.Store#save()V", 31)
			state.record(owner)
			use := &jhlog.CausalEvent{Version: 1, EntityKind: jhlog.CausalResource, Phase: 7,
				EntityID: 9, ParentKind: jhlog.CausalExecutorTask, ParentID: 5,
				ObservedElapsedNanos: 10, Presence: jhlog.CausalHasResourceKind | jhlog.CausalHasLeaseScope,
				ResourceKind:   jhlog.CausalResourceAutoCloseable,
				LeaseScopeKind: jhlog.CausalExecutorTask, LeaseScopeID: 5, BoundToScope: 1}
			causalTestSource(use, jhlog.CausalOriginProject, "example.app.Store#use()V", 27)
			state.record(use)
			if scenario.releaseAt != 0 {
				release := *use
				release.Phase, release.ObservedElapsedNanos = 4, scenario.releaseAt
				state.record(&release)
			}
			causalTestClose(&state, header)
			analysis := state.finalize()
			wantReleased := uint64(0)
			if scenario.releaseAt != 0 {
				wantReleased = 1
			}
			if analysis.Metrics.ResourcesAcquired != 0 || analysis.Metrics.ResourcesObservedUse != 1 ||
				analysis.Metrics.ResourcesReleased != wantReleased ||
				len(analysis.Findings) != scenario.wantIssues {
				t.Fatalf("use was confused with acquisition/close: metrics=%+v findings=%+v", analysis.Metrics, analysis.Findings)
			}
			var useRole string
			for _, group := range analysis.CreatorGroups {
				if group.Source == "example.app.Store#use()V" {
					useRole = group.Role
				}
			}
			if useRole != "место наблюдения use; приобретение неизвестно" {
				t.Fatalf("use site was reported as the resource creator: %q", useRole)
			}
			if len(analysis.Findings) > 0 && (!strings.Contains(analysis.Findings[0].Consequence, "приобретение") ||
				!strings.Contains(analysis.Findings[0].Consequence, "открытость")) {
				t.Fatalf("original acquisition and open state must remain unknown: %+v", analysis.Findings[0])
			}
		})
	}
}

func TestCausalResourceTransferRebindsScopeWithoutHidingLaterLeak(t *testing.T) {
	for _, scenario := range []struct {
		name          string
		initialBound  bool
		releaseBefore bool
		wantFindings  int
	}{
		{"bound scope transferred then left open", true, false, 1},
		{"unbound scope transferred to bound scope", false, false, 1},
		{"new owner releases before scope end", true, true, 0},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			header := causalTestHeader(1, 2, 3)
			var state causalCollectorState
			causalTestOpen(&state, header)
			for _, owner := range []struct{ id, ended uint64 }{{5, 20}, {6, 40}} {
				end := &jhlog.CausalEvent{Version: 1, EntityKind: jhlog.CausalExecutorTask, Phase: 5,
					EntityID: owner.id, ObservedElapsedNanos: owner.ended}
				causalTestSource(end, jhlog.CausalOriginProject, "example.app.Store#save()V", 31)
				state.record(end)
			}
			acquired := &jhlog.CausalEvent{Version: 1, EntityKind: jhlog.CausalResource, Phase: 2, EntityID: 9,
				ParentKind: jhlog.CausalExecutorTask, ParentID: 5, ObservedElapsedNanos: 10,
				Presence:     jhlog.CausalHasResourceKind | jhlog.CausalHasLeaseScope,
				ResourceKind: jhlog.CausalResourceAutoCloseable, LeaseScopeKind: jhlog.CausalExecutorTask,
				LeaseScopeID: 5}
			if scenario.initialBound {
				acquired.BoundToScope = 1
			}
			causalTestSource(acquired, jhlog.CausalOriginProject, "example.app.Store#open()V", 27)
			state.record(acquired)
			transfer := *acquired
			transfer.Phase, transfer.ObservedElapsedNanos = 6, 15
			transfer.LeaseScopeID, transfer.BoundToScope = 6, 1
			state.record(&transfer)
			if scenario.releaseBefore {
				released := transfer
				released.Phase, released.ObservedElapsedNanos = 4, 30
				state.record(&released)
			}
			causalTestClose(&state, header)
			analysis := state.finalize()
			if len(analysis.Findings) != scenario.wantFindings {
				t.Fatalf("findings = %+v, want %d", analysis.Findings, scenario.wantFindings)
			}
			if scenario.wantFindings != 0 && analysis.Findings[0].Rule != "bound_resource_not_released" {
				t.Fatalf("unexpected rule after transfer: %+v", analysis.Findings[0])
			}
		})
	}
}

func TestCausalRelationNeverCrossesEpochOrInventsMissingNode(t *testing.T) {
	var state causalCollectorState
	first := causalTestHeader(1, 2, 3)
	causalTestOpen(&state, first)
	child := &jhlog.CausalEvent{Version: 1, EntityKind: jhlog.CausalExecutorTask, Phase: 1, EntityID: 7,
		ParentKind: jhlog.CausalThread, ParentID: 6, ObservedElapsedNanos: 10}
	causalTestSource(child, jhlog.CausalOriginProject, "example.app.Screen#submit()V", 12)
	state.record(child)
	causalTestClose(&state, first)
	second := causalTestHeader(1, 2, 4)
	causalTestOpen(&state, second)
	parent := &jhlog.CausalEvent{Version: 1, EntityKind: jhlog.CausalThread, Phase: 6, EntityID: 6, ObservedElapsedNanos: 11}
	causalTestSource(parent, jhlog.CausalOriginProject, "example.app.Worker#run()V", 18)
	state.record(parent)
	causalTestClose(&state, second)
	analysis := state.finalize()
	for _, entity := range analysis.EntitiesForReview {
		if entity.ID == 7 && entity.ParentID != 0 {
			t.Fatalf("cross-epoch parent was invented: %+v", entity)
		}
	}
	if !strings.Contains(analysis.EntitiesForReview[0].Scope, "/") {
		t.Fatalf("missing scoped identity: %+v", analysis.EntitiesForReview[0])
	}
}

func TestCausalJankHunterExcludedAndFindingsCoalesced(t *testing.T) {
	header := causalTestHeader(1, 2, 3)
	var state causalCollectorState
	causalTestOpen(&state, header)
	for id := uint64(1); id <= 3; id++ {
		event := &jhlog.CausalEvent{Version: 1, EntityKind: jhlog.CausalOperation, Phase: 1, EntityID: id,
			Presence:  jhlog.CausalHasTargetTID | jhlog.CausalHasDurationNanos | jhlog.CausalHasOperationKind,
			TargetTID: 42, DurationNanos: (100 + id) * 1_000_000, OperationKind: jhlog.CausalOperationFileRead}
		causalTestSource(event, jhlog.CausalOriginProject, "example.app.Home#open()V", 22)
		if id == 3 {
			causalTestSource(event, jhlog.CausalOriginDependency, "io.jankhunter.runtime.Scan#read()V", 3)
		}
		state.record(event)
	}
	causalTestClose(&state, header)
	analysis := state.finalize()
	if analysis.JHExcluded != 1 || len(analysis.Findings) != 1 || analysis.Findings[0].Count != 2 || analysis.Findings[0].MaxDurationMS != 102 {
		t.Fatalf("excluded/coalesced = %+v", analysis)
	}
}

func TestCausalJankHunterInternalNamespaceExcludedRegardlessOfBuildOrigin(t *testing.T) {
	header := causalTestHeader(1, 2, 3)
	var state causalCollectorState
	causalTestOpen(&state, header)
	for id, source := range []string{
		"io.jankhunter.workmanager.WorkerTelemetry#record()V",
		"io.jankhunter.okhttp3.Interceptor#intercept()V",
		"io.jankhunter.runtime.CausalBridge#emit()V",
		"io.jankhunter.sample.MainActivity#load()V",
		"io.jankhunter.feature.CustomPool#submit()V",
	} {
		event := &jhlog.CausalEvent{Version: 1, EntityKind: jhlog.CausalOperation, Phase: 1, EntityID: uint64(id + 1),
			Presence:  jhlog.CausalHasTargetTID | jhlog.CausalHasDurationNanos | jhlog.CausalHasOperationKind,
			TargetTID: 42, DurationNanos: 150_000_000, OperationKind: jhlog.CausalOperationFileRead}
		causalTestSource(event, jhlog.CausalOriginProject, source, 12)
		state.record(event)
	}
	causalTestClose(&state, header)
	analysis := state.finalize()
	if analysis.JHExcluded != 3 || analysis.AppEntities != 2 || analysis.FindingGroups != 2 {
		t.Fatalf("internal JH filter mixed with sample/app sources: excluded=%d app=%d findings=%d", analysis.JHExcluded, analysis.AppEntities, analysis.FindingGroups)
	}
	for _, entity := range analysis.AllEntities {
		if entity.ID <= 3 && !entity.IsJankHunter || entity.ID >= 4 && entity.IsJankHunter {
			t.Fatalf("wrong JH classification for %q: %+v", entity.Source, entity)
		}
	}
}

func TestCausalWorkerImplementationRemainsVisibleBehindInternalAdapter(t *testing.T) {
	header := causalTestHeader(1, 2, 3)
	var state causalCollectorState
	causalTestOpen(&state, header)
	adapter := &jhlog.CausalEvent{Version: 1, EntityKind: jhlog.CausalWorkerAttempt, Phase: 1, EntityID: 10}
	causalTestSource(adapter, jhlog.CausalOriginProject, "io.jankhunter.workmanager.WorkerTelemetry#begin()V", 3)
	state.record(adapter)
	implementation := *adapter
	implementation.Phase = 6
	causalTestSource(&implementation, jhlog.CausalOriginProject, "io.jankhunter.sample.SyncWorker#doWork()V", 44)
	state.record(&implementation)
	causalTestClose(&state, header)
	analysis := state.finalize()
	if analysis.JHExcluded != 0 || analysis.AppEntities != 1 || len(analysis.AllEntities) != 1 || analysis.AllEntities[0].IsJankHunter ||
		analysis.AllEntities[0].AppCaller != "io.jankhunter.sample.SyncWorker#doWork()V" ||
		analysis.AllEntities[0].AppCallerLine != 44 {
		t.Fatalf("Worker implementation lost behind internal adapter: %+v", analysis)
	}
}

func TestCausalWorkerEnqueueCallerDoesNotClaimAcceptance(t *testing.T) {
	header := causalTestHeader(1, 2, 3)
	var state causalCollectorState
	causalTestOpen(&state, header)
	bound := &jhlog.CausalEvent{Version: 1, EntityKind: jhlog.CausalWorkerAttempt,
		Phase: 1, EntityID: 10, Presence: jhlog.CausalHasWorkerBinding | jhlog.CausalHasAppCaller,
		WorkerInstanceID: 23, AppCallerMethod: "example.app.Screen#schedule()V", AppCallerLine: 42}
	causalTestSource(bound, jhlog.CausalOriginDependency, "library.Worker#doWork()V", 11)
	state.record(bound)
	causalTestClose(&state, header)
	analysis := state.finalize()
	if len(analysis.AllEntities) != 1 || !strings.Contains(analysis.AllEntities[0].AppCallerEvidence, "не доказывает регистрацию") ||
		analysis.AllEntities[0].Responsibility != "library_initiated_by_app" {
		t.Fatalf("observed enqueue caller was treated as accepted WorkManager creation: %+v", analysis.AllEntities)
	}
}

func TestCausalWorkerKeepsEnqueueCallerSeparateFromImplementation(t *testing.T) {
	header := causalTestHeader(1, 2, 3)
	var state causalCollectorState
	causalTestOpen(&state, header)
	bound := &jhlog.CausalEvent{Version: 1, EntityKind: jhlog.CausalWorkerAttempt,
		Phase: 1, EntityID: 10, Presence: jhlog.CausalHasWorkerBinding | jhlog.CausalHasAppCaller,
		WorkerInstanceID: 23, AppCallerMethod: "example.app.Screen#enqueueRetryWorker()V", AppCallerLine: 33}
	state.record(bound)
	implementation := *bound
	implementation.Phase = 6
	implementation.Presence &^= jhlog.CausalHasAppCaller
	implementation.AppCallerMethod, implementation.AppCallerLine = "", 0
	causalTestSource(&implementation, jhlog.CausalOriginProject, "example.app.RetryScenarioWorker#doJankHunterWork()V", 13)
	state.record(&implementation)
	causalTestClose(&state, header)
	analysis := state.finalize()
	if len(analysis.AllEntities) != 1 {
		t.Fatalf("worker attempt missing: %+v", analysis)
	}
	view := analysis.AllEntities[0]
	if view.Source != "example.app.RetryScenarioWorker#doJankHunterWork()V" || view.SourceLine != 13 ||
		view.AppCaller != "example.app.Screen#enqueueRetryWorker()V" || view.AppCallerLine != 33 ||
		view.Responsibility != "app_direct" || !strings.Contains(view.AppCallerEvidence, "не доказывает регистрацию") {
		t.Fatalf("implementation overwrote exact enqueue caller: %+v", view)
	}
}

func TestCausalUnknownChildDoesNotInheritAppAttributionThroughJH(t *testing.T) {
	header := causalTestHeader(1, 2, 3)
	var state causalCollectorState
	causalTestOpen(&state, header)
	app := &jhlog.CausalEvent{Version: 1, EntityKind: jhlog.CausalExecutorTask, Phase: 1, EntityID: 1}
	causalTestSource(app, jhlog.CausalOriginProject, "example.app.Screen#load()V", 8)
	state.record(app)
	internal := &jhlog.CausalEvent{Version: 1, EntityKind: jhlog.CausalDirectExecution, Phase: 1,
		EntityID: 2, ParentKind: jhlog.CausalExecutorTask, ParentID: 1}
	causalTestSource(internal, jhlog.CausalOriginProject, "io.jankhunter.runtime.Recorder#run()V", 12)
	state.record(internal)
	unknown := &jhlog.CausalEvent{Version: 1, EntityKind: jhlog.CausalOperation, Phase: 1,
		EntityID: 3, ParentKind: jhlog.CausalDirectExecution, ParentID: 2,
		Presence: jhlog.CausalHasOperationKind, OperationKind: jhlog.CausalOperationFileRead}
	causalTestSource(unknown, jhlog.CausalOriginUnknown, "unknown.Input#read()V", 0)
	state.record(unknown)
	causalTestClose(&state, header)
	analysis := state.finalize()
	if analysis.AppEntities != 1 || analysis.LibraryInitiated != 0 || analysis.UnknownEntities != 1 || analysis.JHExcluded != 1 {
		t.Fatalf("app responsibility leaked through JH boundary: app=%d library=%d unknown=%d JH=%d", analysis.AppEntities, analysis.LibraryInitiated, analysis.UnknownEntities, analysis.JHExcluded)
	}
}

func TestCausalCoroutineCancellationNeedsTerminalCallback(t *testing.T) {
	for _, terminal := range []bool{false, true} {
		header := causalTestHeader(1, 2, 3)
		var state causalCollectorState
		causalTestOpen(&state, header)
		request := &jhlog.CausalEvent{Version: 1, EntityKind: jhlog.CausalCoroutine, Phase: 4, EntityID: 10,
			ObservedElapsedNanos: 10, CallResult: 0}
		causalTestSource(request, jhlog.CausalOriginProject, "example.app.Screen#cancel()V", 25)
		state.record(request)
		returned := *request
		returned.CallResult, returned.ObservedElapsedNanos = 1, 11
		state.record(&returned)
		if terminal {
			end := *request
			end.Phase, end.ObservedElapsedNanos = 5, 12
			state.record(&end)
		}
		causalTestClose(&state, header)
		analysis := state.finalize()
		if len(analysis.AllEntities) != 1 {
			t.Fatalf("missing coroutine entity: %+v", analysis)
		}
		note := analysis.AllEntities[0].LifecycleNote
		if terminal && strings.Contains(note, "неизвестны") || !terminal && !strings.Contains(note, "неизвестны") {
			t.Fatalf("cancel return used as terminal outcome (terminal=%v): %q", terminal, note)
		}
	}
}

func TestCausalFindingTotalRemainsHonestWhenFirstScreenIsCapped(t *testing.T) {
	header := causalTestHeader(1, 2, 3)
	var state causalCollectorState
	causalTestOpen(&state, header)
	for id := uint64(1); id <= 25; id++ {
		event := &jhlog.CausalEvent{Version: 1, EntityKind: jhlog.CausalOperation, Phase: 1, EntityID: id,
			Presence:  jhlog.CausalHasTargetTID | jhlog.CausalHasDurationNanos | jhlog.CausalHasOperationKind,
			TargetTID: 42, DurationNanos: 150_000_000, OperationKind: jhlog.CausalOperationFileRead}
		causalTestSource(event, jhlog.CausalOriginProject, "example.app.Store#read()V", id)
		state.record(event)
	}
	causalTestClose(&state, header)
	analysis := state.finalize()
	if analysis.FindingGroups != 25 || analysis.FindingsOmitted != 5 || len(analysis.Findings) != 20 {
		t.Fatalf("finding total hidden by display cap: groups=%d omitted=%d displayed=%d", analysis.FindingGroups, analysis.FindingsOmitted, len(analysis.Findings))
	}
}

func TestCausalCostInheritsAppInitiatorOnlyThroughExactParent(t *testing.T) {
	header := causalTestHeader(1, 2, 3)
	var state causalCollectorState
	causalTestOpen(&state, header)
	owner := &jhlog.CausalEvent{Version: 1, EntityKind: jhlog.CausalExecutorTask, Phase: 1, EntityID: 1}
	causalTestSource(owner, jhlog.CausalOriginProject, "example.app.Home#load()V", 16)
	state.record(owner)
	operation := &jhlog.CausalEvent{Version: 1, EntityKind: jhlog.CausalOperation, Phase: 1, EntityID: 2,
		ParentKind: jhlog.CausalExecutorTask, ParentID: 1,
		Presence:  jhlog.CausalHasTargetTID | jhlog.CausalHasDurationNanos | jhlog.CausalHasOperationKind,
		TargetTID: 42, DurationNanos: 170_000_000, OperationKind: jhlog.CausalOperationFileRead}
	causalTestSource(operation, jhlog.CausalOriginUnknown, "legacy.io.read", 0)
	state.record(operation)
	causalTestClose(&state, header)
	analysis := state.finalize()
	if len(analysis.Findings) != 1 || analysis.Findings[0].Source != "example.app.Home#load()V" ||
		analysis.Findings[0].Responsibility != "app_initiated_origin_unknown" || analysis.AppInitiated != 1 ||
		analysis.UnknownOriginInitiated != 1 || analysis.LibraryInitiated != 0 {
		t.Fatalf("exact app parent not used: %+v", analysis)
	}
	var childView *CausalEntityView
	for i := range analysis.EntitiesForReview {
		if analysis.EntitiesForReview[i].ID == 2 {
			childView = &analysis.EntitiesForReview[i]
		}
	}
	if childView == nil || childView.ParentID != 1 || childView.ParentAnchor == "" || childView.AppCallerLine != 16 ||
		!strings.Contains(childView.AppCallerEvidence, "parent ID") {
		t.Fatalf("causal parent drilldown lost: %+v", childView)
	}
}

func TestCausalLibraryAttributionRequiresDependencyOrigin(t *testing.T) {
	header := causalTestHeader(1, 2, 3)
	var state causalCollectorState
	causalTestOpen(&state, header)
	parent := &jhlog.CausalEvent{Version: 1, EntityKind: jhlog.CausalExecutorTask, Phase: 1, EntityID: 1}
	causalTestSource(parent, jhlog.CausalOriginProject, "example.app.Home#load()V", 16)
	state.record(parent)
	for _, item := range []struct {
		id     uint64
		origin jhlog.CausalSourceOrigin
	}{{2, jhlog.CausalOriginUnknown}, {3, jhlog.CausalOriginDependency}} {
		operation := &jhlog.CausalEvent{Version: 1, EntityKind: jhlog.CausalOperation, Phase: 1, EntityID: item.id,
			ParentKind: jhlog.CausalExecutorTask, ParentID: 1,
			Presence:  jhlog.CausalHasTargetTID | jhlog.CausalHasDurationNanos | jhlog.CausalHasOperationKind,
			TargetTID: 42, DurationNanos: 170_000_000, OperationKind: jhlog.CausalOperationFileRead}
		causalTestSource(operation, item.origin, "legacy.io.Input#read()V", 0)
		state.record(operation)
	}
	causalTestClose(&state, header)
	analysis := state.finalize()
	if analysis.AppInitiated != 2 || analysis.LibraryInitiated != 1 || analysis.UnknownOriginInitiated != 1 ||
		len(analysis.Findings) != 2 {
		t.Fatalf("unknown origin was promoted to library: %+v", analysis)
	}
	for _, finding := range analysis.Findings {
		if finding.Rule != "main_thread_blocking_operation" {
			t.Fatalf("wrong finding: %+v", finding)
		}
	}
}

func TestCausalReviewPrioritizesIssueOverFirstHundredEntities(t *testing.T) {
	header := causalTestHeader(1, 2, 3)
	var state causalCollectorState
	causalTestOpen(&state, header)
	for id := uint64(1); id <= 150; id++ {
		event := &jhlog.CausalEvent{Version: 1, EntityKind: jhlog.CausalOperation, Phase: 1, EntityID: id,
			Presence:  jhlog.CausalHasTargetTID | jhlog.CausalHasDurationNanos | jhlog.CausalHasOperationKind,
			TargetTID: 42, DurationNanos: 5_000_000, OperationKind: jhlog.CausalOperationFileRead}
		if id == 150 {
			event.DurationNanos = 150_000_000
		}
		causalTestSource(event, jhlog.CausalOriginProject, "example.app.Store#read()V", id)
		state.record(event)
	}
	causalTestClose(&state, header)
	analysis := state.finalize()
	if len(analysis.EntitiesForReview) != 100 || analysis.ReviewOmitted != 50 || len(analysis.Findings) != 1 ||
		analysis.EntitiesForReview[0].ID != 150 || analysis.Findings[0].Anchor != analysis.EntitiesForReview[0].Anchor {
		t.Fatalf("issue drilldown lost behind cap: findings=%+v first=%+v omitted=%d", analysis.Findings, analysis.EntitiesForReview[0], analysis.ReviewOmitted)
	}
}

func TestCausalEventViewKeepsWhoActedOnEveryPhase(t *testing.T) {
	header := causalTestHeader(1, 2, 3)
	var state causalCollectorState
	causalTestOpen(&state, header)
	thread := &jhlog.CausalEvent{Version: 1, EntityKind: jhlog.CausalThread, Phase: 6, EntityID: 10}
	causalTestSource(thread, jhlog.CausalOriginProject, "example.app.Worker#run()V", 5)
	state.record(thread)
	resource := &jhlog.CausalEvent{Version: 1, EntityKind: jhlog.CausalResource, Phase: 2, EntityID: 20,
		ParentKind: jhlog.CausalThread, ParentID: 10, ActorKind: jhlog.CausalThread, ActorID: 10,
		Presence:       jhlog.CausalHasResourceKind | jhlog.CausalHasLeaseScope,
		ResourceKind:   jhlog.CausalResourceAutoCloseable,
		LeaseScopeKind: jhlog.CausalThread, LeaseScopeID: 10, BoundToScope: 0}
	causalTestSource(resource, jhlog.CausalOriginProject, "example.app.Worker#open()V", 7)
	state.record(resource)
	release := *resource
	release.Phase, release.SourceMethod, release.SourceLine = 4, "example.app.Worker#close()V", 17
	state.record(&release)
	causalTestClose(&state, header)
	analysis := state.finalize()
	for _, view := range analysis.EntitiesForReview {
		if view.Kind != uint64(jhlog.CausalResource) {
			continue
		}
		if len(view.Events) != 2 || view.Events[1].SourceLine != 17 || view.Events[1].ActorID != 10 ||
			view.Events[1].Source != "example.app.Worker#close()V" || view.Events[1].LeaseScopeID != 10 {
			t.Fatalf("release actor/source/scope lost: %+v", view.Events)
		}
		return
	}
	t.Fatal("resource not presented")
}

func TestCausalQueueAndScheduleDelayRemainReviewWithoutMeasuredHarm(t *testing.T) {
	header := causalTestHeader(1, 2, 3)
	var state causalCollectorState
	causalTestOpen(&state, header)
	queued := &jhlog.CausalEvent{Version: 1, EntityKind: jhlog.CausalExecutorTask, Phase: 1, EntityID: 1,
		ObservedElapsedNanos: 1_000_000_000,
		Presence:             jhlog.CausalHasQueueEntered | jhlog.CausalHasScheduledFor,
		QueueEnteredNanos:    1_000_000_000, ScheduledForNanos: 1_100_000_000}
	causalTestSource(queued, jhlog.CausalOriginProject, "example.app.Poller#schedule()V", 25)
	state.record(queued)
	start := &jhlog.CausalEvent{Version: 1, EntityKind: jhlog.CausalExecutorTask, Phase: 4, EntityID: 1,
		ObservedElapsedNanos: 1_500_000_000}
	state.record(start)
	causalTestClose(&state, header)
	analysis := state.finalize()
	if len(analysis.Findings) != 0 || len(analysis.Observations) != 2 {
		t.Fatalf("delay promoted to defect or lost: %+v", analysis)
	}
	for _, observation := range analysis.Observations {
		if observation.SourceLine != 25 || observation.Anchor != analysis.EntitiesForReview[0].Anchor {
			t.Fatalf("review lost site or drilldown: %+v", observation)
		}
	}
}

func TestCausalCoverageNamesLossReasonAndSuppressesAbsenceBasedDefect(t *testing.T) {
	header := causalTestHeader(1, 2, 3)
	var state causalCollectorState
	causalTestOpen(&state, header)
	owner := &jhlog.CausalEvent{Version: 1, EntityKind: jhlog.CausalExecutorTask, Phase: 5, EntityID: 1, ObservedElapsedNanos: 20}
	causalTestSource(owner, jhlog.CausalOriginProject, "example.app.Task#run()V", 8)
	state.record(owner)
	lease := &jhlog.CausalEvent{Version: 1, EntityKind: jhlog.CausalResource, Phase: 2, EntityID: 2,
		ParentKind: jhlog.CausalExecutorTask, ParentID: 1, ObservedElapsedNanos: 10,
		Presence: jhlog.CausalHasResourceKind | jhlog.CausalHasLeaseScope, ResourceKind: 1,
		LeaseScopeKind: jhlog.CausalExecutorTask, LeaseScopeID: 1, BoundToScope: 1}
	state.record(lease)
	causalTestClose(&state, header)
	state.scopes[causalScopeOf(header)].quality[jhlog.EventQualityCounterID(jhlog.EventCausalEvent, jhlog.QualityLossQueueFull)] = 3
	analysis := state.finalize()
	if analysis.Complete || len(analysis.Findings) != 0 || analysis.TransportLoss != 3 ||
		len(analysis.TransportLossByReason) != 1 || analysis.TransportLossByReason[0].Reason != "очередь заполнена" {
		t.Fatalf("loss coverage failure: %+v", analysis)
	}
}

func TestCausalResourceAbsenceRequiresHooksWithoutEvidenceLoss(t *testing.T) {
	for _, scenario := range []struct {
		name         string
		counters     map[uint64]uint64
		wantComplete bool
	}{
		{"no hook failure", map[uint64]uint64{}, true},
		{"unclassified failure", map[uint64]uint64{jhlog.QualityRuntimeHookFailureTotal: 1}, false},
		{"resource hook failure", map[uint64]uint64{jhlog.QualityRuntimeHookFailureTotal: 1, jhlog.QualityRuntimeHookUnclassifiedFailure: 1}, false},
		{"async wrapper failure", map[uint64]uint64{jhlog.QualityRuntimeHookFailureTotal: 1, jhlog.QualityRuntimeHookAsyncWrapperFailure: 1}, false},
		{"working collector fallback", map[uint64]uint64{jhlog.QualityRuntimeHookFailureTotal: 1, jhlog.QualityJankStatsDependencyMissing: 1}, true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			header := causalTestHeader(1, 2, 3)
			var state causalCollectorState
			causalTestOpen(&state, header)
			owner := &jhlog.CausalEvent{Version: 1, EntityKind: jhlog.CausalExecutorTask, Phase: 5, EntityID: 5, ObservedElapsedNanos: 20}
			causalTestSource(owner, jhlog.CausalOriginProject, "app.Store#save()V", 31)
			state.record(owner)
			lease := &jhlog.CausalEvent{Version: 1, EntityKind: jhlog.CausalResource, Phase: 2, EntityID: 9, ObservedElapsedNanos: 10, ParentKind: jhlog.CausalExecutorTask, ParentID: 5, Presence: jhlog.CausalHasResourceKind | jhlog.CausalHasLeaseScope, ResourceKind: jhlog.CausalResourceAutoCloseable, LeaseScopeKind: jhlog.CausalExecutorTask, LeaseScopeID: 5, BoundToScope: 1}
			causalTestSource(lease, jhlog.CausalOriginProject, "app.Store#open()V", 27)
			state.record(lease)
			state.segmentResult(jhlog.StreamResult{Header: header, Sealed: true, Status: jhlog.SegmentStatusClosedClean, LatestQuality: &jhlog.QualitySnapshot{Counters: scenario.counters}})
			result := state.finalize()
			if result.Complete != scenario.wantComplete || (len(result.Findings) == 1) != scenario.wantComplete {
				t.Fatalf("hook evidence loss ignored or fallback misclassified: complete=%v findings=%d", result.Complete, len(result.Findings))
			}
		})
	}
}

func TestCausalLifecycleBeyondRetainedEventLimitRemainsVisible(t *testing.T) {
	for _, scenario := range []struct {
		name                    string
		kind                    jhlog.CausalEntityKind
		fillerPhase, finalPhase uint64
		want                    string
	}{
		{"coroutine terminal", jhlog.CausalCoroutine, 2, 5, "завершение работы наблюдалось"},
		{"coroutine cancellation", jhlog.CausalCoroutine, 2, 4, "вызов отмены наблюдался"},
		{"thread root exit", jhlog.CausalThread, 8, 7, "корневой run завершился"},
		{"thread end", jhlog.CausalThread, 8, 11, "завершение потока наблюдалось"},
		{"resource release", jhlog.CausalResource, 5, 4, "release наблюдался"},
		{"worker retry", jhlog.CausalWorkerAttempt, 6, 3, "попытка вернула retry"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			header := causalTestHeader(1, 2, 3)
			var state causalCollectorState
			causalTestOpen(&state, header)
			for i := 0; i < maxCausalNodeEvents; i++ {
				e := &jhlog.CausalEvent{Version: 1, EntityKind: scenario.kind, EntityID: 7, Phase: scenario.fillerPhase, ObservedElapsedNanos: uint64(i + 1)}
				causalTestSource(e, jhlog.CausalOriginProject, "app.Work#run()V", 12)
				state.record(e)
			}
			state.record(&jhlog.CausalEvent{Version: 1, EntityKind: scenario.kind, EntityID: 7, Phase: scenario.finalPhase, ObservedElapsedNanos: 100})
			causalTestClose(&state, header)
			result := state.finalize()
			if len(result.AllEntities) != 1 || !strings.Contains(result.AllEntities[0].LifecycleNote, scenario.want) {
				t.Fatalf("lifecycle detail lost after retention limit: %q, want %q", result.AllEntities[0].LifecycleNote, scenario.want)
			}
			if len(result.AllEntities[0].Events) != maxCausalNodeEvents || result.AllEntities[0].OmittedEvents != 1 {
				t.Fatalf("event retention bound changed")
			}
		})
	}
}
