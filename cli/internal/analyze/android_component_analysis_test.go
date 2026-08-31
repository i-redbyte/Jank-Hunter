package analyze

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func TestAndroidComponentAnalysisSeparatesForegroundServiceFromVisibleUI(t *testing.T) {
	accumulator := newAndroidComponentAnalysisAccumulator(nil)
	header := androidAnalysisHeader(1, "main")
	accumulator.startLog(header)
	accumulator.addProcessState(jhlog.Event{TimeMS: 1_010, ProcessState: &jhlog.ProcessStateEvent{
		UIVisibility: jhlog.ProcessUIHidden,
		Importance:   jhlog.ProcessImportanceForegroundService,
		Reason:       jhlog.ProcessStateReasonComponentLifecycle,
	}})
	accumulator.addComponent("com.example.SyncService", "", jhlog.Event{
		TimeMS: 1_011,
		AndroidComponent: &jhlog.AndroidComponentEvent{
			ComponentRef: jhlog.StableSymbol(11), InstanceID: 1, FlowID: 1,
			Kind: jhlog.ComponentKindService, Stage: jhlog.ComponentServiceForegroundEnter,
			Outcome: jhlog.ComponentOutcomeSuccess, Flags: jhlog.ComponentFlagForeground,
		},
	})

	analysis := accumulator.finalize(CollectionQuality{
		ProcessScope: jhlog.ProcessScopeAll.String(), ProcessRosterComplete: true,
	})
	if analysis == nil || analysis.ProcessState.HiddenForegroundServiceSamples != 1 ||
		analysis.ProcessState.VisibleUISamples != 0 {
		t.Fatalf("process state = %+v", analysis)
	}
	if analysis.Services.ForegroundEntries != 1 || analysis.Services.ActiveForegroundAtEnd != 1 {
		t.Fatalf("service analysis = %+v", analysis.Services)
	}
	for _, finding := range analysis.Findings {
		if finding.ID == "android.service.foreground_misclassification" {
			t.Fatalf("valid hidden foreground service was misclassified: %+v", finding)
		}
	}
}

func TestAndroidComponentAnalysisDoesNotKeepFailedServiceCreationActive(t *testing.T) {
	accumulator := newAndroidComponentAnalysisAccumulator(nil)
	accumulator.startLog(androidAnalysisHeader(1, "main"))
	accumulator.addComponent("com.example.SyncService", "", jhlog.Event{
		TimeMS: 1_011,
		AndroidComponent: &jhlog.AndroidComponentEvent{
			ComponentRef: jhlog.StableSymbol(11), InstanceID: 1, FlowID: 1,
			Kind: jhlog.ComponentKindService, Stage: jhlog.ComponentServiceCreated,
			Outcome: jhlog.ComponentOutcomeFailure, DurationUS: 20_000,
		},
	})

	analysis := accumulator.finalize(CollectionQuality{
		ProcessScope: jhlog.ProcessScopeAll.String(), ProcessRosterComplete: true,
	})
	if analysis.Services.ActiveInstancesAtEnd != 0 {
		t.Fatalf("failed service creation remained active: %+v", analysis.Services)
	}
}

func TestAndroidComponentAnalysisFindsReceiverDeadlineRisks(t *testing.T) {
	accumulator := newAndroidComponentAnalysisAccumulator(nil)
	accumulator.startLog(androidAnalysisHeader(1, "main"))
	accumulator.addComponent("com.example.SyncReceiver", "com.example.SYNC", jhlog.Event{
		TimeMS: 1_020,
		AndroidComponent: &jhlog.AndroidComponentEvent{
			ComponentRef: jhlog.StableSymbol(12), InstanceID: 2, FlowID: 2,
			Kind: jhlog.ComponentKindReceiver, Stage: jhlog.ComponentReceiverFinished,
			Outcome: jhlog.ComponentOutcomeSuccess, DurationUS: 12_000_000,
			Flags: jhlog.ComponentFlagAsync | jhlog.ComponentFlagOrdered,
		},
	})

	analysis := accumulator.finalize(CollectionQuality{ProcessScope: jhlog.ProcessScopeAll.String(), ProcessRosterComplete: true})
	if analysis.Receivers.AsyncCompleted != 1 || analysis.Receivers.AsyncDeadlineRisks != 1 {
		t.Fatalf("receiver analysis = %+v", analysis.Receivers)
	}
	if !hasAndroidFinding(analysis.Findings, "android.receiver.async_deadline_risk") {
		t.Fatalf("findings = %+v", analysis.Findings)
	}
}

func TestAndroidComponentAnalysisCorrelatesUniqueBinderPairWithoutClaimingExactLink(t *testing.T) {
	catalog := &AndroidComponentCatalog{aidl: map[androidAIDLTransactionKey]string{
		{descriptor: "com.example.ISync", code: 7}: "refresh",
	}}
	accumulator := newAndroidComponentAnalysisAccumulator(catalog)
	client := androidAnalysisHeader(1, "main")
	server := androidAnalysisHeader(2, "sync")
	server.RunID = client.RunID

	accumulator.startLog(client)
	accumulator.addBinder("com.example.ISync", "refresh", jhlog.Event{
		TimeMS: 1_025, Flags: uint64(jhlog.FlagThreadMain),
		BinderTransaction: &jhlog.BinderTransactionEvent{
			CallID: 1, Direction: jhlog.BinderDirectionClient, TransactionCode: 7,
			Outcome: jhlog.BinderOutcomeSuccess, DurationUS: 20_000,
		},
	})
	accumulator.startLog(server)
	accumulator.addBinder("com.example.ISync", "", jhlog.Event{
		TimeMS: 1_022,
		BinderTransaction: &jhlog.BinderTransactionEvent{
			CallID: 2, Direction: jhlog.BinderDirectionServer, TransactionCode: 7,
			Outcome: jhlog.BinderOutcomeSuccess, DurationUS: 15_000,
		},
	})

	analysis := accumulator.finalize(CollectionQuality{ProcessScope: jhlog.ProcessScopeAll.String(), ProcessRosterComplete: true})
	if analysis.Binder.CorrelatedPairs != 1 || analysis.Binder.UnmatchedClients != 0 || len(analysis.Binder.Flows) != 1 {
		t.Fatalf("Binder analysis = %+v", analysis.Binder)
	}
	flow := analysis.Binder.Flows[0]
	if flow.Method != "refresh" || flow.Confidence != "high" || flow.ClaimLevel != "correlated" ||
		flow.ClientProcess != "main" || flow.ServerProcess != "sync" || !flow.CrossProcess {
		t.Fatalf("flow = %+v", flow)
	}
}

func TestAndroidComponentAnalysisLeavesAmbiguousBinderCandidatesUnmatched(t *testing.T) {
	accumulator := newAndroidComponentAnalysisAccumulator(nil)
	client := androidAnalysisHeader(1, "main")
	server := androidAnalysisHeader(2, "sync")
	server.RunID = client.RunID
	accumulator.startLog(client)
	accumulator.addBinder("com.example.ISync", "refresh", binderAnalysisEvent(1_025, 1, jhlog.BinderDirectionClient, 20_000))
	accumulator.startLog(server)
	accumulator.addBinder("com.example.ISync", "refresh", binderAnalysisEvent(1_020, 2, jhlog.BinderDirectionServer, 10_000))
	accumulator.addBinder("com.example.ISync", "refresh", binderAnalysisEvent(1_022, 3, jhlog.BinderDirectionServer, 12_000))

	analysis := accumulator.finalize(CollectionQuality{ProcessScope: jhlog.ProcessScopeAll.String(), ProcessRosterComplete: true})
	if analysis.Binder.CorrelatedPairs != 0 || analysis.Binder.AmbiguousClients != 1 ||
		analysis.Binder.UnmatchedClients != 1 {
		t.Fatalf("Binder analysis = %+v", analysis.Binder)
	}
}

func TestAndroidComponentAnalysisDoesNotCorrelateConflictingKnownMethods(t *testing.T) {
	accumulator := newAndroidComponentAnalysisAccumulator(nil)
	client := androidAnalysisHeader(1, "main")
	server := androidAnalysisHeader(2, "sync")
	server.RunID = client.RunID
	accumulator.startLog(client)
	accumulator.addBinder("com.example.ISync", "refresh", binderAnalysisEvent(1_025, 1, jhlog.BinderDirectionClient, 20_000))
	accumulator.startLog(server)
	accumulator.addBinder("com.example.ISync", "delete", binderAnalysisEvent(1_022, 2, jhlog.BinderDirectionServer, 15_000))

	analysis := accumulator.finalize(CollectionQuality{ProcessScope: jhlog.ProcessScopeAll.String(), ProcessRosterComplete: true})
	if analysis.Binder.CorrelatedPairs != 0 || analysis.Binder.UnmatchedClients != 1 ||
		analysis.Binder.UnmatchedServers != 1 {
		t.Fatalf("Binder analysis = %+v", analysis.Binder)
	}
}

func TestAndroidComponentAnalysisCountsOneWayCallsOnceAtClientBoundary(t *testing.T) {
	accumulator := newAndroidComponentAnalysisAccumulator(nil)
	client := androidAnalysisHeader(1, "main")
	server := androidAnalysisHeader(2, "sync")
	server.RunID = client.RunID
	clientEvent := binderAnalysisEvent(1_025, 1, jhlog.BinderDirectionClient, 20_000)
	clientEvent.BinderTransaction.Flags = jhlog.BinderFlagOneway
	serverEvent := binderAnalysisEvent(1_026, 2, jhlog.BinderDirectionServer, 15_000)
	serverEvent.BinderTransaction.Flags = jhlog.BinderFlagOneway
	accumulator.startLog(client)
	accumulator.addBinder("com.example.ISync", "refresh", clientEvent)
	accumulator.startLog(server)
	accumulator.addBinder("com.example.ISync", "refresh", serverEvent)

	analysis := accumulator.finalize(CollectionQuality{ProcessScope: jhlog.ProcessScopeAll.String(), ProcessRosterComplete: true})
	if analysis.Binder.OneWayCalls != 1 {
		t.Fatalf("one-way calls = %d, want 1", analysis.Binder.OneWayCalls)
	}
}

func TestAndroidComponentAnalysisReportsEventsWithoutCorrelationMetadata(t *testing.T) {
	accumulator := newAndroidComponentAnalysisAccumulator(nil)
	accumulator.startLog(androidAnalysisHeader(1, "main"))
	accumulator.addBinder("", "refresh", binderAnalysisEvent(1_025, 1, jhlog.BinderDirectionClient, 20_000))

	analysis := accumulator.finalize(CollectionQuality{
		ProcessScope: jhlog.ProcessScopeAll.String(), ProcessRosterComplete: true,
		ProcessRosterDeclarationComplete: true,
	})
	if analysis.Binder.UncorrelatableEvents != 1 || !analysis.Partial ||
		!strings.Contains(strings.Join(analysis.PartialReasons, " "), "без дескриптора") {
		t.Fatalf("analysis = %+v", analysis)
	}
}

func TestAndroidComponentAnalysisSummarizesStaticCatalogCoverage(t *testing.T) {
	catalog := &AndroidComponentCatalog{Available: true, Source: "/tmp/android-components-catalog.jsonl", Components: []AndroidComponentCatalogEntry{
		{
			ClassName: "com.example.SyncService", Kind: "service", Coverage: "full",
			EntryPoints: []string{"onCreate", "onStartCommand"}, InstrumentedEntryPoints: []string{"onCreate", "onStartCommand"},
		},
		{
			ClassName: "com.example.SyncReceiver", Kind: "receiver", Coverage: "partial",
			EntryPoints: []string{"onReceive", "goAsync"}, InstrumentedEntryPoints: []string{"onReceive"}, UncoveredEntryPoints: []string{"goAsync"},
			AIDLDescriptor: "com.example.ISync", Transactions: []AndroidAIDLTransaction{{Code: 7, Method: "refresh"}},
		},
	}}
	analysis := newAndroidComponentAnalysisAccumulator(catalog).finalize(CollectionQuality{
		ProcessScope: jhlog.ProcessScopeAll.String(), ProcessRosterComplete: true,
	})

	coverage := analysis.Coverage
	if !coverage.CatalogAvailable || coverage.Components != 2 || coverage.Services != 1 ||
		coverage.Receivers != 1 || coverage.Full != 1 || coverage.Partial != 1 || coverage.None != 0 ||
		coverage.EntryPoints != 4 || coverage.InstrumentedEntryPoints != 3 || coverage.UncoveredEntryPoints != 1 ||
		coverage.AIDLInterfaces != 1 || coverage.AIDLTransactions != 1 {
		t.Fatalf("coverage = %+v", coverage)
	}
}

func TestInspectFilesRetainsStaticCatalogWhenRuntimeEvidenceIsAbsent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.jhlog")
	writeAndroidAnalysisLog(t, path, androidAnalysisHeader(1, "main"), nil, nil)
	catalog := &AndroidComponentCatalog{Available: true, Components: []AndroidComponentCatalogEntry{{
		ClassName: "com.example.SyncService", Kind: "service", Coverage: "full",
		EntryPoints: []string{"onCreate"}, InstrumentedEntryPoints: []string{"onCreate"},
	}}}

	summary, err := InspectFilesWithOptions("catalog-only", []string{path}, Options{AndroidComponentCatalog: catalog})
	if err != nil {
		t.Fatal(err)
	}
	if summary.AndroidComponents == nil || summary.AndroidComponents.Available ||
		!summary.AndroidComponents.Coverage.CatalogAvailable || summary.AndroidComponents.Coverage.Components != 1 {
		t.Fatalf("Android component analysis = %+v", summary.AndroidComponents)
	}
}

func TestAndroidComponentAnalysisAllowsMainOnlyWithExplicitPartialWarning(t *testing.T) {
	accumulator := newAndroidComponentAnalysisAccumulator(nil)
	accumulator.startLog(androidAnalysisHeader(1, "main"))
	analysis := accumulator.finalize(CollectionQuality{
		ProcessScope: jhlog.ProcessScopeMainOnly.String(), ExpectedProcessCount: 2,
		ObservedProcessCount: 1, ProcessRosterComplete: false,
	})
	if !analysis.Partial || len(analysis.PartialReasons) == 0 ||
		!strings.Contains(strings.Join(analysis.PartialReasons, " "), "только основной процесс") {
		t.Fatalf("partial analysis = %+v", analysis)
	}
}

func TestInspectFilesBuildsCrossProcessAndroidComponentAnalysis(t *testing.T) {
	directory := t.TempDir()
	clientHeader := androidAnalysisHeader(1, "main")
	serverHeader := androidAnalysisHeader(2, "sync")
	serverHeader.RunID = clientHeader.RunID
	roster := jhlog.ProcessRosterFingerprint([]string{"main", "sync"})
	for _, header := range []*jhlog.SegmentHeader{&clientHeader, &serverHeader} {
		header.ProcessScope = jhlog.ProcessScopeAll
		header.ExpectedProcessCount = 2
		header.ExpectedProcessFingerprint = roster
		header.ProcessRosterDeclarationComplete = true
	}
	clientPath := filepath.Join(directory, "client.jhlog")
	serverPath := filepath.Join(directory, "server.jhlog")
	writeAndroidAnalysisLog(t, clientPath, clientHeader, []jhlog.DictionaryEntry{
		{Kind: jhlog.DictGeneric, ID: 1, Value: "com.example.ISync"},
		{Kind: jhlog.DictGeneric, ID: 2, Value: "refresh"},
	}, []jhlog.Event{{
		Type: jhlog.EventBinderTransaction, TimeMS: 1_025, Flags: uint64(jhlog.FlagThreadMain),
		BinderTransaction: &jhlog.BinderTransactionEvent{
			DescriptorRef: jhlog.LocalSymbol(1), MethodRef: jhlog.LocalSymbol(2),
			CallID: 1, Direction: jhlog.BinderDirectionClient, TransactionCode: 7,
			Outcome: jhlog.BinderOutcomeSuccess, DurationUS: 20_000,
		},
	}})
	writeAndroidAnalysisLog(t, serverPath, serverHeader, []jhlog.DictionaryEntry{
		{Kind: jhlog.DictGeneric, ID: 1, Value: "com.example.ISync"},
	}, []jhlog.Event{{
		Type: jhlog.EventBinderTransaction, TimeMS: 1_022,
		BinderTransaction: &jhlog.BinderTransactionEvent{
			DescriptorRef: jhlog.LocalSymbol(1), CallID: 2,
			Direction: jhlog.BinderDirectionServer, TransactionCode: 7,
			Outcome: jhlog.BinderOutcomeSuccess, DurationUS: 15_000,
		},
	}})
	catalog := &AndroidComponentCatalog{Available: true, aidl: map[androidAIDLTransactionKey]string{
		{descriptor: "com.example.ISync", code: 7}: "refresh",
	}}

	summary, err := InspectFilesWithOptions("IPC", []string{clientPath, serverPath}, Options{
		AndroidComponentCatalog: catalog,
	})
	if err != nil {
		t.Fatal(err)
	}
	if summary.AndroidComponents == nil || summary.AndroidComponents.Binder.CorrelatedPairs != 1 ||
		summary.AndroidComponents.Partial {
		t.Fatalf("Android component summary = %+v", summary.AndroidComponents)
	}
}

func TestProblemReportIncludesActionableAndroidComponentFinding(t *testing.T) {
	summary := Summary{AndroidComponents: &AndroidComponentAnalysis{
		Available: true,
		Findings: []AndroidComponentFinding{{
			ID: "android.binder.main_thread_slow", Severity: "high",
			Title:       "Медленный Binder-вызов на главном потоке",
			Explanation: "client transact занял 40 мс", Descriptor: "com.example.ISync",
			Method: "refresh", Process: "main", Count: 2, MaxDurationUS: 40_000,
			ClaimLevel: "linked",
		}},
	}}
	report, err := BuildProblemReport(summary)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, finding := range report.Problems {
		if finding.DetectorID == "android.binder.main_thread_slow" {
			found = true
			if finding.Category != ProblemCategoryAndroidComponents || finding.Why.ClaimLevel != "linked" {
				t.Fatalf("problem = %+v", finding)
			}
		}
	}
	if !found {
		t.Fatalf("problems = %+v", report.Problems)
	}
}

var benchmarkAndroidBinderAnalysis AndroidBinderAnalysis

func BenchmarkAndroidBinderCorrelationTenThousandEvents(b *testing.B) {
	client := androidAnalysisHeader(1, "main")
	server := androidAnalysisHeader(2, "sync")
	server.RunID = client.RunID
	b.ReportAllocs()
	for range b.N {
		accumulator := newAndroidComponentAnalysisAccumulator(nil)
		for index := uint64(0); index < 5_000; index++ {
			base := uint64(1_010) + index*10
			accumulator.startLog(client)
			accumulator.addBinder(
				"com.example.ISync",
				"refresh",
				binderAnalysisEvent(base+5, index+1, jhlog.BinderDirectionClient, 2_000),
			)
			accumulator.startLog(server)
			accumulator.addBinder(
				"com.example.ISync",
				"refresh",
				binderAnalysisEvent(base+4, index+5_001, jhlog.BinderDirectionServer, 1_000),
			)
		}
		benchmarkAndroidBinderAnalysis = accumulator.finalize(CollectionQuality{
			ProcessScope: jhlog.ProcessScopeAll.String(), ProcessRosterComplete: true,
			ProcessRosterDeclarationComplete: true,
		}).Binder
	}
}

func androidAnalysisHeader(process byte, name string) jhlog.SegmentHeader {
	header := jhlog.DefaultSegmentHeader()
	header.RunID[0] = 1
	header.ProcessInstanceID[0] = process
	header.SessionID[0] = process
	header.ProcessName = name
	header.SegmentStartElapsedUS = 1_000_000
	header.SegmentStartUnixMS = 10_000
	return header
}

func binderAnalysisEvent(
	timeMS uint64,
	callID uint64,
	direction jhlog.BinderDirection,
	durationUS uint64,
) jhlog.Event {
	return jhlog.Event{TimeMS: timeMS, BinderTransaction: &jhlog.BinderTransactionEvent{
		CallID: callID, Direction: direction, TransactionCode: 7,
		Outcome: jhlog.BinderOutcomeSuccess, DurationUS: durationUS,
	}}
}

func hasAndroidFinding(findings []AndroidComponentFinding, id string) bool {
	for _, finding := range findings {
		if finding.ID == id {
			return true
		}
	}
	return false
}

func writeAndroidAnalysisLog(
	t *testing.T,
	path string,
	header jhlog.SegmentHeader,
	dictionary []jhlog.DictionaryEntry,
	events []jhlog.Event,
) {
	t.Helper()
	closer, writer, err := jhlog.CreateWithHeader(path, header)
	if err != nil {
		t.Fatal(err)
	}
	for index := range dictionary {
		entry := dictionary[index]
		if err := writer.WriteEvent(jhlog.Event{Type: jhlog.EventDictionary, Dictionary: &entry}); err != nil {
			t.Fatal(err)
		}
	}
	for _, event := range events {
		if err := writer.WriteEvent(event); err != nil {
			t.Fatal(err)
		}
	}
	if err := closer.Close(); err != nil {
		t.Fatal(err)
	}
}
