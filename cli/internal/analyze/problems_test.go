package analyze

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

var benchmarkProblemReport ProblemReport

func TestProblemEngineBuildsCompoundNetworkFinding(t *testing.T) {
	summary := completeProblemFixture()
	report, err := BuildProblemReport(summary)
	if err != nil {
		t.Fatalf("BuildProblemReport() error = %v", err)
	}
	var network ProblemFinding
	for _, finding := range report.Problems {
		if finding.Category == ProblemCategoryNetwork {
			network = finding
			break
		}
	}
	if network.ID == "" {
		t.Fatalf("network finding missing: %+v", report.Problems)
	}
	if network.Subcategory != "slow_storm" || !strings.Contains(network.Title, "частый и медленный") {
		t.Fatalf("network compound finding = %+v", network)
	}
	if network.Why.ClaimLevel != "unknown" || network.Confidence != "high" {
		t.Fatalf("network causal/confidence contract = %+v", network)
	}
	if len(network.PriorityBreakdown) != 5 || network.InvestigationPriority <= 0 || network.Severity == "info" {
		t.Fatalf("network investigation priority is not explainable: %+v", network)
	}
	if network.Frequency == nil || network.Frequency.RatePerSec == nil || *network.Frequency.RatePerSec < 4 {
		t.Fatalf("network exposure missing: %+v", network.Frequency)
	}
	if len(network.Evidence) < 3 || network.Evidence[0].Sample == nil {
		t.Fatalf("network evidence missing sample/denominator: %+v", network.Evidence)
	}
}

func TestProblemEngineFindsWebSocketReconnectAndFailureStorm(t *testing.T) {
	summary := Summary{
		DurationMS:        60_000,
		CollectionQuality: CollectionQuality{Complete: true},
		AnalysisInputs:    AnalysisInputCompleteness{Complete: true, RuntimeEvidence: true},
		WebSocketAnalysis: &WebSocketAnalysis{Opened: 12, Failures: 6, Reconnects: 5, Connections: []WebSocketConnectionStats{{
			Route: "GET /socket", Screen: "Chat", Owner: "RealtimeRepository",
			Opened: 12, Failures: 6, Reconnects: 5, ConnectP95MS: 1_800,
		}}},
	}

	report, err := BuildProblemReport(summary)
	if err != nil {
		t.Fatal(err)
	}
	finding := findingByDetector(report.Problems, "network.websocket_health")
	if finding == nil || finding.Where[0].Route != "GET /socket" ||
		!evidenceNamed(finding.Evidence, "Доля обрывов") ||
		!evidenceNamed(finding.Evidence, "Переподключения") {
		t.Fatalf("WebSocket finding = %+v", finding)
	}
}

func TestProblemEngineAttachesSampledStackAndSpecificActionToMainThreadStall(t *testing.T) {
	summary := Summary{
		DurationMS:        60_000,
		CollectionQuality: CollectionQuality{Complete: true},
		AnalysisInputs:    AnalysisInputCompleteness{Complete: true, RuntimeEvidence: true},
		ProblemWindows: []ProblemWindowStats{{
			Screen: "MainActivity", Owner: "ru.mail.im.app.di.components.ComponentFactoryImpl",
			Kind: "main_thread_stall", Windows: 1, Count: 1, MaxMS: 1_364, TotalWindowMS: 1_364,
		}},
		Owners: []OwnerStats{{
			Owner: "ru.mail.im.app.di.components.ComponentFactoryImpl", Kind: "main_thread_stall", Count: 1, MaxMS: 1_364,
			StackHint: "ru.mail.im.app.di.components.ComponentFactoryImpl.create(ComponentFactoryImpl.kt:483)",
		}},
	}
	report, err := BuildProblemReport(summary)
	if err != nil {
		t.Fatal(err)
	}
	finding := findingByDetector(report.Problems, "stability.main_thread_stall")
	if finding == nil {
		t.Fatalf("main-thread stall finding missing: %+v", report.Problems)
	}
	if len(finding.Where) != 1 || !strings.Contains(finding.Where[0].Method, "ComponentFactoryImpl.create") {
		t.Fatalf("sampled stack was not attached to the stall: %+v", finding.Where)
	}
	if !strings.Contains(finding.Title, "создания DI-компонента") {
		t.Fatalf("stall title does not explain the localized mechanism: %q", finding.Title)
	}
	if finding.Why.ClaimLevel != "correlated" || !strings.Contains(finding.Why.Summary, "снимок стека") {
		t.Fatalf("stall causal boundary is not explicit: %+v", finding.Why)
	}
	if len(finding.Recommendations) == 0 || !strings.Contains(finding.Recommendations[0].Action, "предоставления зависимостей") {
		t.Fatalf("stall recommendation is not specific: %+v", finding.Recommendations)
	}
	if !warningsContain(finding.RelatedCategories, ProblemCategoryDependencyInjection) {
		t.Fatalf("DI stall is not available through the DI category: %+v", finding.RelatedCategories)
	}
	coverage := categoryCoverageByID(report.Coverage, ProblemCategoryDependencyInjection)
	if coverage == nil || coverage.Label != "DI" || coverage.FindingCount != 1 {
		t.Fatalf("DI coverage = %+v", coverage)
	}
}

func TestProblemEngineFindsSlowFrequentDatabaseQueryOnMainThread(t *testing.T) {
	summary := Summary{
		DurationMS:        60_000,
		CollectionQuality: CollectionQuality{Complete: true},
		AnalysisInputs:    AnalysisInputCompleteness{Complete: true, RuntimeEvidence: true},
		DatabaseAnalysis: &DatabaseAnalysis{Main: DatabaseExecutionStats{Calls: 40}, Statements: []DatabaseStatementStats{{
			Query: "SELECT * FROM messages WHERE chat_id = ?", Operation: "чтение",
			Overall:            DatabaseExecutionStats{Calls: 40, P95DurationUS: 45_000, MaxDurationUS: 80_000},
			Main:               DatabaseExecutionStats{Calls: 40, P95DurationUS: 45_000, MaxDurationUS: 80_000},
			PeakCallsPerSecond: 20, RapidRepeats: 30,
			MainCorrelation:       DatabaseCorrelationStats{HTTPOverlaps: 2, WorkerOverlaps: 1},
			BackgroundCorrelation: DatabaseCorrelationStats{HTTPOverlaps: 3, FileIOOverlaps: 4},
			Contexts: []DatabaseStatementContextStats{{
				Source: "MessagesDao.load", Framework: "Room", Screen: "Chat",
				ContextOperation: "chat.open", Process: "app", Overall: DatabaseExecutionStats{Calls: 40},
			}},
		}}},
	}

	report, err := BuildProblemReport(summary)
	if err != nil {
		t.Fatal(err)
	}
	finding := findingByDetector(report.Problems, "io.database_calls")
	if finding == nil || finding.Where[0].Owner != "MessagesDao.load" ||
		finding.Where[0].Operation != "chat.open" || finding.Where[0].Process != "app" ||
		!evidenceNamed(finding.Evidence, "Граница верхних 5% длительности") ||
		!evidenceNamed(finding.Evidence, "Быстрые повторы") ||
		!evidenceNamed(finding.Evidence, "Подсистемы, работавшие в тот же интервал") {
		t.Fatalf("database finding = %+v", finding)
	}
	for _, evidence := range finding.Evidence {
		if evidence.Name == "Подсистемы, работавшие в тот же интервал" &&
			(!strings.Contains(evidence.Observed, "главный поток: HTTP 2") ||
				!strings.Contains(evidence.Observed, "фон: HTTP 3")) {
			t.Fatalf("thread-specific related evidence = %+v", evidence)
		}
	}
}

func TestProblemEngineDoesNotClassifySlowBackgroundDistributionAsMainThread(t *testing.T) {
	summary := Summary{
		DurationMS:        60_000,
		CollectionQuality: CollectionQuality{Complete: true},
		AnalysisInputs:    AnalysisInputCompleteness{Complete: true, RuntimeEvidence: true},
		DatabaseAnalysis: &DatabaseAnalysis{Statements: []DatabaseStatementStats{{
			Query:      "SELECT value FROM samples",
			Operation:  "чтение",
			Overall:    DatabaseExecutionStats{Calls: 26, P95DurationUS: 200_000, MaxDurationUS: 200_000},
			Main:       DatabaseExecutionStats{Calls: 1, P95DurationUS: 1_000, MaxDurationUS: 1_000},
			Background: DatabaseExecutionStats{Calls: 25, P95DurationUS: 200_000, MaxDurationUS: 200_000},
			Contexts: []DatabaseStatementContextStats{{
				Source: "SamplesDao.load", Framework: "Room", Screen: "Samples",
				ContextOperation: "samples.open", Overall: DatabaseExecutionStats{Calls: 26},
			}},
		}}},
	}

	report, err := BuildProblemReport(summary)
	if err != nil {
		t.Fatal(err)
	}
	finding := findingByDetector(report.Problems, "io.database_calls")
	if finding == nil {
		t.Fatal("background database finding is missing")
	}
	if finding.Subcategory != "database_background_slow" || evidenceNamed(finding.Evidence, "На главном потоке: верхние 5%") {
		t.Fatalf("database finding = %+v", finding)
	}
}

func TestProblemEnginePromotesOnlyImportedPlanEvidenceToObserved(t *testing.T) {
	summary := Summary{
		DurationMS:        60_000,
		CollectionQuality: CollectionQuality{Complete: true},
		AnalysisInputs:    AnalysisInputCompleteness{Complete: true, RuntimeEvidence: true},
		DatabaseAnalysis: &DatabaseAnalysis{Evidence: &DatabaseEvidenceAnalysis{
			LoadedStatements: 1, MatchedStatements: 1, PlanStatements: 1,
			Findings: []DatabasePlanFinding{{
				Kind: "scan", ClaimLevel: "observed", Query: "SELECT value FROM message WHERE chat_id = ?",
				Operation: "query", Table: "message",
				Summary: "Импортированный план подтверждает SCAN message.",
				Action:  "Проверьте ожидаемую селективность.",
			}},
		}},
	}

	report, err := BuildProblemReport(summary)
	if err != nil {
		t.Fatal(err)
	}
	finding := findingByDetector(report.Problems, "io.database_plan_evidence")
	if finding == nil || finding.Status != "observed" || finding.Why.ClaimLevel != "linked" ||
		finding.Where[0].Owner != "message" || !evidenceNamed(finding.Evidence, "Шаг плана SQL-запроса") {
		t.Fatalf("database plan finding = %+v", finding)
	}
}

func TestProblemEngineExplainsDatabaseFailureTaxonomyAndOnlyMeasuredPhases(t *testing.T) {
	summary := Summary{
		DurationMS:        60_000,
		CollectionQuality: CollectionQuality{Complete: true},
		AnalysisInputs:    AnalysisInputCompleteness{Complete: true, RuntimeEvidence: true},
		DatabaseAnalysis: &DatabaseAnalysis{Statements: []DatabaseStatementStats{{
			Query: "UPDATE messages SET state = ? WHERE id = ?", Operation: "обновление",
			Overall:    DatabaseExecutionStats{Calls: 10, Failures: 2, MaxDurationUS: 30_000},
			Background: DatabaseExecutionStats{Calls: 10, MaxDurationUS: 30_000},
			Telemetry: DatabaseTelemetryStats{
				FailureKinds: []NamedValue{{Name: "busy_locked", Value: 2}},
				LockWait:     DatabasePhaseStats{Samples: 2, P95DurationUS: 5_000},
			},
			Contexts: []DatabaseStatementContextStats{{
				Source: "MessagesDao.update", Framework: "Room", Screen: "Chat",
				ContextOperation: "message.send", Overall: DatabaseExecutionStats{Calls: 10, Failures: 2},
			}},
		}}},
	}

	report, err := BuildProblemReport(summary)
	if err != nil {
		t.Fatal(err)
	}
	finding := findingByDetector(report.Problems, "io.database_calls")
	if finding == nil || !strings.Contains(finding.WhatHappened, "БД занята или заблокирована: 2") ||
		!evidenceNamed(finding.Evidence, "Классы ошибок") ||
		!evidenceNamed(finding.Evidence, "Ожидание блокировки: верхние 5%") ||
		evidenceNamed(finding.Evidence, "Ожидание соединения: верхние 5%") {
		t.Fatalf("database taxonomy/phase finding = %+v", finding)
	}
	if len(finding.Recommendations) == 0 ||
		!strings.Contains(finding.Recommendations[len(finding.Recommendations)-1].Rationale, "БД занята или заблокирована") {
		t.Fatalf("database failure recommendation = %+v", finding.Recommendations)
	}
	if strings.Contains(finding.Title, "запрос") || strings.Contains(finding.WhatHappened, "запрос") {
		t.Fatalf("write operation is described as a query: %+v", finding)
	}
}

func TestProblemEngineDoesNotRecommendQueryPlanForSlowBackgroundWrite(t *testing.T) {
	summary := Summary{
		DurationMS:        60_000,
		CollectionQuality: CollectionQuality{Complete: true},
		AnalysisInputs:    AnalysisInputCompleteness{Complete: true, RuntimeEvidence: true},
		DatabaseAnalysis: &DatabaseAnalysis{Statements: []DatabaseStatementStats{{
			Query: "UPDATE messages SET state = ? WHERE id = ?", Operation: "обновление",
			Overall:    DatabaseExecutionStats{Calls: 8, P95DurationUS: 250_000, MaxDurationUS: 300_000},
			Background: DatabaseExecutionStats{Calls: 8, P95DurationUS: 250_000, MaxDurationUS: 300_000},
		}}},
	}

	report, err := BuildProblemReport(summary)
	if err != nil {
		t.Fatal(err)
	}
	finding := findingByDetector(report.Problems, "io.database_calls")
	if finding == nil || len(finding.Recommendations) == 0 {
		t.Fatalf("slow write finding = %+v", finding)
	}
	action := finding.Recommendations[0].Action
	if strings.Contains(action, "QUERY PLAN") || strings.Contains(action, "объём выборки") ||
		(!strings.Contains(strings.ToLower(action), "транзак") && !strings.Contains(strings.ToLower(action), "batch")) {
		t.Fatalf("slow write recommendation is not operation-aware: %q", action)
	}
}

func TestProblemEngineMergesOnlyIntervalLinkedMainThreadSQLIntoUIIncident(t *testing.T) {
	base := Summary{
		DurationMS:        60_000,
		CollectionQuality: CollectionQuality{Complete: true},
		AnalysisInputs:    AnalysisInputCompleteness{Complete: true, RuntimeEvidence: true},
		Screens: []ScreenStats{{
			Screen: "Feed", Frames: 200, JankyFrames: 40, JankRatePct: 20,
			FrameP95MS: 40, FrameP99MS: 64, FrameDeadlineUS: 16_667, FrameSource: "jankstats",
		}},
	}
	base.DatabaseAnalysis = &DatabaseAnalysis{Statements: []DatabaseStatementStats{{
		Query: "SELECT item FROM feed WHERE id = ?", Operation: "чтение",
		Overall:         DatabaseExecutionStats{Calls: 5, MaxDurationUS: 48_000},
		Main:            DatabaseExecutionStats{Calls: 5, MaxDurationUS: 48_000},
		MainCorrelation: DatabaseCorrelationStats{UIWindowOverlaps: 1, UIOverlapMaxDurationUS: 48_000},
		Contexts: []DatabaseStatementContextStats{{
			Source: "FeedDao.load", Screen: "Feed", ContextOperation: "feed.open",
			Overall: DatabaseExecutionStats{Calls: 5}, Main: DatabaseExecutionStats{Calls: 5},
			MainCorrelation: DatabaseCorrelationStats{UIWindowOverlaps: 1, UIOverlapMaxDurationUS: 48_000},
		}},
	}}}

	report, err := BuildProblemReport(base)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Incidents) != 1 || len(report.Incidents[0].RelatedFindings) != 2 {
		t.Fatalf("linked SQL and UI were not merged: %+v", report.Incidents)
	}
	if !strings.Contains(report.Incidents[0].WhatHappened, "2 связанных сигнала") {
		t.Fatalf("merged incident explanation = %+v", report.Incidents[0])
	}

	base.DatabaseAnalysis.Statements[0].MainCorrelation = DatabaseCorrelationStats{
		UIWindowOverlaps: 1, UIOverlapMaxDurationUS: 1_000,
	}
	base.DatabaseAnalysis.Statements[0].Contexts[0].MainCorrelation = DatabaseCorrelationStats{
		UIWindowOverlaps: 1, UIOverlapMaxDurationUS: 1_000,
	}
	report, err = BuildProblemReport(base)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Incidents) != 2 {
		t.Fatalf("short overlapping SQL was merged as a UI cause: %+v", report.Incidents)
	}

	base.DatabaseAnalysis.Statements[0].MainCorrelation = DatabaseCorrelationStats{}
	base.DatabaseAnalysis.Statements[0].Contexts[0].MainCorrelation = DatabaseCorrelationStats{}
	report, err = BuildProblemReport(base)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Incidents) != 2 {
		t.Fatalf("unlinked SQL was merged by screen name alone: %+v", report.Incidents)
	}
}

func TestProblemEngineBuildsUniversalOperationFinding(t *testing.T) {
	summary := completeProblemFixture()
	summary.OperationAnalysis = &OperationAnalysis{Operations: []OperationStats{{
		Operation: "content.open", Kind: "user", Screen: "Catalog",
		Count: 40, Success: 35, Failures: 3, Timeouts: 2,
		P50MS: 300, P90MS: 800, P95MS: 1_200, MaxMS: 2_500, TotalMS: 18_000,
		Budgeted: 40, BudgetBreaches: 12, BudgetBreachRatePct: 30,
		CorrelatedStalls: 4, CorrelatedHTTPFailures: 2, CorrelatedUIFrames: 600,
		CorrelatedUIJank: 90, CorrelatedUIJankRatePct: 15,
	}}}

	report, err := BuildProblemReport(summary)
	if err != nil {
		t.Fatal(err)
	}
	finding := findingByDetector(report.Problems, "operations.health")
	if finding == nil {
		t.Fatalf("operation finding missing: %+v", report.Problems)
	}
	if finding.Category != ProblemCategoryOperations || finding.Why.ClaimLevel != "linked" ||
		len(finding.Where) != 1 || finding.Where[0].Operation != "content.open" || finding.Where[0].Screen != "Catalog" {
		t.Fatalf("operation finding identity = %+v", finding)
	}
	if !evidenceNamed(finding.Evidence, "Нарушения бюджета") ||
		!evidenceNamed(finding.Evidence, "Неуспешные завершения") ||
		!evidenceNamed(finding.Evidence, "Граница верхних 5% длительности") {
		t.Fatalf("operation evidence = %+v", finding.Evidence)
	}
}

func TestProblemEngineUsesPlainRussianForStartupFinding(t *testing.T) {
	summary := Summary{
		DurationMS:        60_000,
		CollectionQuality: CollectionQuality{Complete: true},
		AnalysisInputs:    AnalysisInputCompleteness{Complete: true, RuntimeEvidence: true},
		StartupAnalysis:   &StartupAnalysis{ColdResumeSamples: 2, AvgColdResumeMS: 2_600, MaxColdResumeMS: 5_000},
	}

	report, err := BuildProblemReport(summary)
	if err != nil {
		t.Fatal(err)
	}
	finding := findingByDetector(report.Problems, "ui.startup_cold")
	if finding == nil {
		t.Fatalf("startup finding missing: %+v", report.Problems)
	}
	visibleText := problemFindingVisibleText(*finding)
	for _, expected := range []string{"Первое открытие экрана", "Максимум первого открытия", "Запуск приложения"} {
		if !strings.Contains(visibleText, expected) {
			t.Fatalf("startup finding misses %q: %s", expected, visibleText)
		}
	}
	for _, forbidden := range []string{"Activity resume", "Cold-process", "first resume", "clean process", "critical path", "process-level", "lifecycle tracker", "Startup"} {
		if strings.Contains(visibleText, forbidden) {
			t.Fatalf("startup finding contains untranslated term %q: %s", forbidden, visibleText)
		}
	}
}

func TestProblemEngineUsesPlainRussianForResourceFindings(t *testing.T) {
	summary := Summary{
		DurationMS:        60_000,
		LowMemoryCount:    4,
		MemoryMaxKB:       512_000,
		AvailMemoryMinKB:  128_000,
		CollectionQuality: CollectionQuality{Complete: true},
		AnalysisInputs:    AnalysisInputCompleteness{Complete: true, RuntimeEvidence: true},
		Gauges:            []NamedValue{{Name: "process.cpu.core_percent_x100", Value: 8_500}},
	}

	report, err := BuildProblemReport(summary)
	if err != nil {
		t.Fatal(err)
	}
	for _, detector := range []string{"memory.pressure", "cpu.process_saturation"} {
		finding := findingByDetector(report.Problems, detector)
		if finding == nil {
			t.Fatalf("%s finding missing: %+v", detector, report.Problems)
		}
		visible := strings.ToLower(problemFindingVisibleText(*finding))
		for _, forbidden := range []string{
			"low-memory", "samples", "max pss", "memory pressure", "allocator",
			"headroom", "trend", "process cpu", "task", "spike", "runtime",
			"method trace", "process-level", "ui tail", "per-thread", "linkage",
		} {
			if strings.Contains(visible, forbidden) {
				t.Fatalf("%s finding contains untranslated term %q: %s", detector, forbidden, visible)
			}
		}
	}
}

func TestProblemEngineDoesNotInventSlowOperationWithoutExplicitBudget(t *testing.T) {
	summary := Summary{
		DurationMS:        60_000,
		CollectionQuality: CollectionQuality{Complete: true},
		AnalysisInputs:    AnalysisInputCompleteness{Complete: true, RuntimeEvidence: true},
		OperationAnalysis: &OperationAnalysis{Operations: []OperationStats{{
			Operation: "domain.work", Kind: "background", Count: 100,
			Success: 100, P50MS: 12_000, P90MS: 20_000, P95MS: 24_000, MaxMS: 30_000,
		}}},
	}

	report, err := BuildProblemReport(summary)
	if err != nil {
		t.Fatal(err)
	}
	if finding := findingByDetector(report.Problems, "operations.health"); finding != nil {
		t.Fatalf("operation without product budget was classified as slow: %+v", finding)
	}
}

func TestCompareProblemsMarksWorsenedOperationHealthAsRegression(t *testing.T) {
	operation := OperationStats{
		Operation: "content.open", Kind: "screen", Screen: "Catalog", Count: 40, Success: 40,
		P50MS: 200, P90MS: 400, P95MS: 500, MaxMS: 700, Budgeted: 40,
	}
	baseline := Summary{
		DurationMS:        60_000,
		CollectionQuality: CollectionQuality{Complete: true},
		AnalysisInputs:    AnalysisInputCompleteness{Complete: true, RuntimeEvidence: true},
		OperationAnalysis: &OperationAnalysis{Completed: 40, Operations: []OperationStats{operation}},
	}
	baseline.OperationAnalysis.Operations[0].BudgetBreaches = 4
	baseline.OperationAnalysis.Operations[0].BudgetBreachRatePct = 10
	baseline = attachProblemReport(t, baseline)

	candidate := baseline
	candidate.OperationAnalysis = &OperationAnalysis{Completed: 40, Operations: []OperationStats{operation}}
	candidate.OperationAnalysis.Operations[0].BudgetBreaches = 20
	candidate.OperationAnalysis.Operations[0].BudgetBreachRatePct = 50
	candidate = attachProblemReport(t, candidate)

	comparison := CompareProblems(baseline, candidate, true)
	if len(comparison.Deltas) != 1 || comparison.Deltas[0].Status != "regressed" ||
		comparison.Deltas[0].Candidate == nil || comparison.Deltas[0].Candidate.DetectorID != "operations.health" {
		t.Fatalf("operation problem comparison = %+v", comparison.Deltas)
	}
}

func TestInvestigationPriorityIsExplainablePublicContract(t *testing.T) {
	breakdown := priority(20, 8, 5, 3, 2, "impact", "magnitude", "exposure", "breadth", "compound")
	if got := investigationPriority(breakdown); got != 38 {
		t.Fatalf("investigation priority = %d, want 38", got)
	}
	maximum := 0
	for _, component := range breakdown {
		maximum += component.Maximum
	}
	if maximum != 100 {
		t.Fatalf("priority component maxima = %d, want 100", maximum)
	}
	payload, err := json.Marshal(ProblemFinding{
		InvestigationPriority: 38,
		PriorityBreakdown:     breakdown,
	})
	if err != nil {
		t.Fatal(err)
	}
	jsonText := string(payload)
	if !strings.Contains(jsonText, `"investigation_priority":38`) ||
		!strings.Contains(jsonText, `"priority_breakdown"`) ||
		strings.Contains(jsonText, `"risk_score"`) || strings.Contains(jsonText, `"rank_breakdown"`) {
		t.Fatalf("public priority JSON contract = %s", jsonText)
	}
}

func TestInvestigationPriorityValidationRejectsInconsistentContract(t *testing.T) {
	breakdown := priority(20, 8, 5, 3, 2, "impact", "magnitude", "exposure", "breadth", "compound")
	finding := ProblemFinding{InvestigationPriority: 39, PriorityBreakdown: breakdown}
	if err := validateInvestigationPriority(finding); err == nil || !strings.Contains(err.Error(), "component sum 38") {
		t.Fatalf("inconsistent priority validation error = %v", err)
	}

	finding.InvestigationPriority = 38
	finding.PriorityBreakdown[0].Maximum = 41
	if err := validateInvestigationPriority(finding); err == nil || !strings.Contains(err.Error(), "impact 0..40") {
		t.Fatalf("changed component maximum validation error = %v", err)
	}
}

func BenchmarkBuildProblemReportRepresentativeHighCardinalityNetwork(b *testing.B) {
	const routeCount = 2_000
	summary := completeProblemFixture()
	summary.Routes = make([]RouteStats, routeCount)
	summary.SignalContexts = make([]SignalContextStats, routeCount)
	summary.NetworkAnalysis = &NetworkAnalysis{Calls: make([]NetworkCallStats, routeCount)}
	for index := range routeCount {
		route := fmt.Sprintf("GET /benchmark/%04d", index)
		summary.Routes[index] = RouteStats{Route: route, Count: 20, P95MS: 2_000}
		summary.SignalContexts[index] = SignalContextStats{RouteSample: route, Screen: "Screen", Operation: "operation", Owner: "OperationOwner"}
		summary.NetworkAnalysis.Calls[index] = NetworkCallStats{
			Route: route, Screen: "Screen", Operation: "operation", Initiator: "Repository.load",
		}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		report, err := BuildProblemReport(summary)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkProblemReport = report
	}
}

func TestProblemEngineExplainsNetworkPhaseRetriesAndExactCallsite(t *testing.T) {
	summary := completeProblemFixture()
	summary.Routes = []RouteStats{{
		Route: "POST /checkout", Count: 40, Failures: 4, TransportFailures: 2, HTTP5xx: 2,
		P50MS: 600, P95MS: 1_200, MaxMS: 1_800, TotalDurationMS: 26_000,
		Retries: 6, Redirects: 2, ConnectFailures: 3, MaxConcurrency: 5,
		Phases: []HTTPPhaseStats{
			{Name: "queue", SampleCount: 40, P95MS: 120},
			{Name: "ttfb", SampleCount: 40, P95MS: 900},
		},
	}}
	summary.NetworkAnalysis = &NetworkAnalysis{Calls: []NetworkCallStats{{
		Route: "POST /checkout", Service: "payments", Initiator: "CheckoutRepository.authorize",
		Screen: "Checkout", Operation: "checkout.pay.authorize", Owner: "CheckoutViewModel.submit", Count: 40,
	}}}

	report, err := BuildProblemReport(summary)
	if err != nil {
		t.Fatal(err)
	}
	finding := findingByDetector(report.Problems, "network.route_health")
	if finding == nil {
		t.Fatalf("network finding missing: %+v", report.Problems)
	}
	if !evidenceNamed(finding.Evidence, "Граница верхних 5% фазы «TTFB»") || !evidenceNamed(finding.Evidence, "Повторы запросов") ||
		!evidenceNamed(finding.Evidence, "Максимальная одновременность") {
		t.Fatalf("advanced network evidence = %+v", finding.Evidence)
	}
	if finding.Cost == nil || finding.Cost.WallTimeMS == nil || *finding.Cost.WallTimeMS != 26_000 {
		t.Fatalf("network cumulative wait = %+v", finding.Cost)
	}
	if !problemLocationExists(finding.Where, ProblemLocation{
		Screen: "Checkout", Operation: "checkout.pay.authorize", Route: "POST /checkout", Owner: "CheckoutRepository.authorize",
	}) {
		t.Fatalf("exact callsite is absent: %+v", finding.Where)
	}
	if !strings.Contains(finding.WhatHappened, "TTFB") || !strings.Contains(finding.WhatHappened, "6 повтор") {
		t.Fatalf("advanced network explanation = %q", finding.WhatHappened)
	}
}

func TestNetworkProblemLocationsIndexesAndDeduplicatesRouteContexts(t *testing.T) {
	const routeA = "GET /feed"
	const routeB = "POST /sync"
	summary := Summary{
		Routes: []RouteStats{
			{Route: routeA, OwnerSample: "FeedService"},
			{Route: routeB, OwnerSample: "SyncService"},
		},
		NetworkAnalysis: &NetworkAnalysis{Calls: []NetworkCallStats{
			{Route: routeA, Screen: "Feed", Operation: "refresh.load", Initiator: "FeedRepository.load"},
			{Route: routeA, Screen: "Feed", Operation: "refresh.load", Initiator: "FeedRepository.load"},
		}},
		SignalContexts: []SignalContextStats{{
			RouteSample: routeB, Screen: "Settings", Operation: "sync.submit", Owner: "SyncViewModel",
		}},
	}

	locations := networkProblemLocations(summary)
	if len(locations[routeA]) != 2 || !problemLocationExists(locations[routeA], ProblemLocation{
		Screen: "Feed", Operation: "refresh.load", Route: routeA, Owner: "FeedRepository.load",
	}) {
		t.Fatalf("route A locations = %+v", locations[routeA])
	}
	if len(locations[routeB]) != 2 || !problemLocationExists(locations[routeB], ProblemLocation{
		Screen: "Settings", Operation: "sync.submit", Route: routeB, Owner: "SyncViewModel",
	}) {
		t.Fatalf("route B locations = %+v", locations[routeB])
	}
	if problemLocationExists(locations[routeA], ProblemLocation{Route: routeB, Owner: "SyncViewModel"}) {
		t.Fatalf("route contexts crossed: %+v", locations)
	}
}

func TestProblemEngineUsesTypedProcessExitAndIOEvidence(t *testing.T) {
	summary := Summary{
		DurationMS:        60_000,
		CollectionQuality: CollectionQuality{Complete: true},
		AnalysisInputs:    AnalysisInputCompleteness{Complete: true, RuntimeEvidence: true},
		CollectorSessions: 1,
		CollectorFlagsAll: uint64(jhlog.CollectorProcessExit | jhlog.CollectorIOTracing),
		ProcessExits: []ProcessExitStats{{
			Reason: 6, ReasonLabel: "ANR", Count: 2, LatestTimestampUnixMS: 1_750_000_000_000,
			Importance: 100, MaxPSSKB: 256_000, MaxRSSKB: 320_000, Process: "main",
		}},
		IOAnalysis: &IOAnalysis{Operations: 8, MainThreadOperations: 8, Calls: []IOStats{{
			Operation: "file_read", MainThread: true, Count: 8,
			TotalDurationUS: 800_000, MaxDurationUS: 275_000, Bytes: 32_768,
			Screen: "Feed", ContextOperation: "refresh.load", Owner: "FeedRepository.load",
		}}},
	}
	report, err := BuildProblemReport(summary)
	if err != nil {
		t.Fatal(err)
	}
	exit := findingByDetector(report.Problems, "stability.historical_process_exit")
	if exit == nil || exit.Why.ClaimLevel != "linked" || !strings.Contains(exit.WhatHappened, "main") || !evidenceUsesSource(exit.Evidence, "typed_application_exit_info") {
		t.Fatalf("typed process exit finding = %+v", exit)
	}
	ioFinding := findingByDetector(report.Problems, "io.main_thread")
	if ioFinding == nil || ioFinding.Cost == nil || ioFinding.Cost.MainThreadBlockedMS == nil || *ioFinding.Cost.MainThreadBlockedMS != 800 || !evidenceUsesSource(ioFinding.Evidence, "typed_io") {
		t.Fatalf("typed I/O finding = %+v", ioFinding)
	}
}

func TestProblemEngineUsesCriticalIOSourcePeakFailuresSyncAndKnownBytes(t *testing.T) {
	summary := Summary{
		DurationMS:        3_600_000,
		CollectionQuality: CollectionQuality{Complete: true},
		AnalysisInputs:    AnalysisInputCompleteness{Complete: true, RuntimeEvidence: true},
		CollectorSessions: 1,
		CollectorFlagsAll: uint64(jhlog.CollectorIOTracing),
		IOAnalysis: &IOAnalysis{Calls: []IOStats{
			{
				Operation: "file_write", Source: "CacheStore.flush", Count: 60,
				KnownByteOperations: 60, Bytes: 60 * 4_096, MaxBytes: 4_096,
				P95DurationUS: 2_000, MaxDurationUS: 4_000, TotalDurationUS: 90_000,
				PeakOperationsPerSecond: 12, Owner: "FeedRepository.cache", ContextOperation: "feed.refresh",
			},
			{
				Operation: "content_read", Source: "AvatarStore.open", Count: 4, Failures: 3,
				P95DurationUS: 3_000, MaxDurationUS: 3_000, TotalDurationUS: 8_000,
				Owner: "ProfileRepository.avatar", ContextOperation: "profile.open",
			},
			{
				Operation: "file_sync", Source: "DraftStore.commit", MainThread: true, Count: 1,
				P95DurationUS: 2_000, MaxDurationUS: 2_000, TotalDurationUS: 2_000,
				Owner: "ComposerViewModel.save", Screen: "Composer",
			},
			{
				Operation: "file_read", Source: "AttachmentStore.read", MainThread: true, Count: 1,
				KnownByteOperations: 1, Bytes: 2 * 1024 * 1024, MaxBytes: 2 * 1024 * 1024,
				P95DurationUS: 4_000, MaxDurationUS: 4_000, TotalDurationUS: 4_000,
				Owner: "AttachmentViewModel.open", Screen: "Attachment",
			},
		}},
	}

	report, err := BuildProblemReport(summary)
	if err != nil {
		t.Fatal(err)
	}
	storm := findingBySubcategory(report.Problems, "small_io_storm_file_write")
	if storm == nil || !strings.Contains(storm.WhatHappened, "12 операций/с") ||
		!problemLocationExists(storm.Where, ProblemLocation{Operation: "feed.refresh", Owner: "CacheStore.flush"}) {
		t.Fatalf("peak/source I/O storm = %+v", storm)
	}
	failures := findingBySubcategory(report.Problems, "io_failures_content_read")
	if failures == nil || !evidenceNamed(failures.Evidence, "Неуспешные операции") {
		t.Fatalf("I/O failures = %+v", failures)
	}
	sync := findingBySubcategory(report.Problems, "main_thread_sync_file_sync")
	if sync == nil || !strings.Contains(strings.ToLower(sync.Title), "синхронизац") {
		t.Fatalf("main-thread sync = %+v", sync)
	}
	large := findingBySubcategory(report.Problems, "main_thread_large_file_read")
	if large == nil || !evidenceNamed(large.Evidence, "Максимальный известный объём") {
		t.Fatalf("large main-thread I/O = %+v", large)
	}
}

func findingBySubcategory(findings []ProblemFinding, subcategory string) *ProblemFinding {
	for index := range findings {
		if findings[index].Subcategory == subcategory {
			return &findings[index]
		}
	}
	return nil
}

func evidenceNamed(evidence []ProblemEvidence, name string) bool {
	for _, item := range evidence {
		if item.Name == name {
			return true
		}
	}
	return false
}

func problemFindingVisibleText(finding ProblemFinding) string {
	var builder strings.Builder
	builder.WriteString(finding.Title)
	builder.WriteByte('\n')
	builder.WriteString(finding.WhatHappened)
	builder.WriteByte('\n')
	builder.WriteString(finding.Why.Summary)
	for _, evidence := range finding.Evidence {
		builder.WriteByte('\n')
		builder.WriteString(evidence.Name)
	}
	for _, component := range finding.PriorityBreakdown {
		builder.WriteByte('\n')
		builder.WriteString(component.Component)
		builder.WriteByte('\n')
		builder.WriteString(component.Explanation)
	}
	for _, recommendation := range finding.Recommendations {
		builder.WriteByte('\n')
		builder.WriteString(recommendation.Action)
		builder.WriteByte('\n')
		builder.WriteString(recommendation.Rationale)
		builder.WriteByte('\n')
		builder.WriteString(recommendation.Verification)
	}
	for _, limitation := range finding.Limitations {
		builder.WriteByte('\n')
		builder.WriteString(limitation)
	}
	for _, drilldown := range finding.Drilldowns {
		builder.WriteByte('\n')
		builder.WriteString(drilldown.Label)
	}
	return builder.String()
}

func problemLocationExists(locations []ProblemLocation, want ProblemLocation) bool {
	for _, location := range locations {
		if location == want {
			return true
		}
	}
	return false
}

func TestConfiguredCollectorWithoutSamplesIsNotReportedHealthy(t *testing.T) {
	summary := Summary{
		DurationMS:        60_000,
		CollectionQuality: CollectionQuality{Complete: true},
		CollectorSessions: 1,
		CollectorFlagsAll: uint64(jhlog.CollectorFPS | jhlog.CollectorIOTracing),
	}
	report, err := BuildProblemReport(summary)
	if err != nil {
		t.Fatal(err)
	}
	for _, category := range report.Coverage {
		if (category.Category == ProblemCategoryUI || category.Category == ProblemCategoryIO) && category.Status != "insufficient_data" {
			t.Fatalf("configured empty category = %+v", category)
		}
	}
}

func TestProblemEngineDoesNotPresentMissingCoverageAsHealthy(t *testing.T) {
	report, err := BuildProblemReport(Summary{DurationMS: 60_000})
	if err != nil {
		t.Fatalf("BuildProblemReport() error = %v", err)
	}
	if report.Summary.Verdict != "incomplete" || report.Summary.Unchecked == 0 {
		t.Fatalf("empty report verdict = %+v", report.Summary)
	}
	for _, category := range report.Coverage {
		if category.Status == "healthy" {
			t.Fatalf("missing category %q rendered healthy: %+v", category.Category, category)
		}
		if category.Explanation == "" {
			t.Fatalf("coverage explanation missing: %+v", category)
		}
	}
}

func TestIncompleteProcessRosterDoesNotClaimEventLossForEveryCategory(t *testing.T) {
	summary := completeProblemFixture()
	summary.CollectionQuality = CollectionQuality{
		Complete:                false,
		ChainValid:              true,
		ExactAdmission:          true,
		CounterInvariantsValid:  true,
		QualityProgressionValid: true,
		SegmentsWithQuality:     1,
		AcceptedEvents:          100,
		WrittenEvents:           100,
		ExpectedProcessCount:    3,
		ObservedProcessCount:    1,
		ProcessRosterComplete:   false,
	}
	report, err := BuildProblemReport(summary)
	if err != nil {
		t.Fatalf("BuildProblemReport() error = %v", err)
	}
	for _, category := range report.Coverage {
		if category.Status == "collection_degraded" {
			t.Fatalf("process uncertainty was presented as event loss: %+v", category)
		}
		if strings.Contains(category.Explanation, "потер") || strings.Contains(category.Explanation, "повреж") {
			t.Fatalf("misleading event-loss explanation: %+v", category)
		}
	}
}

func TestActiveIntactSessionDoesNotLowerEveryFindingConfidence(t *testing.T) {
	quality := CollectionQuality{
		Complete:                false,
		Level:                   "high",
		ChainValid:              true,
		ExactAdmission:          true,
		CounterInvariantsValid:  true,
		QualityProgressionValid: true,
		SegmentsWithQuality:     1,
		UnsealedSegments:        1,
		AcceptedEvents:          100,
		WrittenEvents:           100,
		ProcessRosterComplete:   true,
		ProcessScopeConsistent:  true,
		RunCohortConsistent:     true,
		KnownLostEvents:         0,
		DamagedSegments:         0,
		Notices:                 []string{"Активная сессия корректно прочитана до последнего зафиксированного блока."},
	}
	summary := Summary{
		CollectionQuality: quality,
		AnalysisInputs:    AnalysisInputCompleteness{Complete: true},
	}

	confidence, reasons, limits := problemConfidence(summary, 10, 3, true)
	if confidence != "high" {
		t.Fatalf("active intact session confidence = %q, want high; reasons=%v limits=%v", confidence, reasons, limits)
	}
	if strings.Contains(strings.Join(reasons, " "), "потер") || len(limits) != 0 {
		t.Fatalf("active intact session was presented as data loss: reasons=%v limits=%v", reasons, limits)
	}
}

func TestActualCollectionLossStillLowersFindingConfidence(t *testing.T) {
	summary := Summary{
		CollectionQuality: CollectionQuality{
			Complete:                false,
			ChainValid:              true,
			ExactAdmission:          true,
			CounterInvariantsValid:  true,
			QualityProgressionValid: true,
			SegmentsWithQuality:     1,
			AcceptedEvents:          100,
			WrittenEvents:           99,
			KnownLostEvents:         1,
			Reasons:                 []string{"Потеряно одно событие."},
		},
		AnalysisInputs: AnalysisInputCompleteness{Complete: true},
	}

	confidence, reasons, limits := problemConfidence(summary, 10, 3, true)
	if confidence != "low" {
		t.Fatalf("lossy session confidence = %q, want low; reasons=%v limits=%v", confidence, reasons, limits)
	}
	if !strings.Contains(strings.Join(reasons, " "), "нижней оценкой") || len(limits) == 0 {
		t.Fatalf("lossy session limitation missing: reasons=%v limits=%v", reasons, limits)
	}
}

func TestProblemFingerprintsAndOrderIgnoreInputOrder(t *testing.T) {
	left := completeProblemFixture()
	right := completeProblemFixture()
	right.Routes[0], right.Routes[1] = right.Routes[1], right.Routes[0]
	right.Screens[0], right.Screens[1] = right.Screens[1], right.Screens[0]

	leftReport, leftErr := BuildProblemReport(left)
	rightReport, rightErr := BuildProblemReport(right)
	if leftErr != nil || rightErr != nil {
		t.Fatalf("BuildProblemReport() errors = %v / %v", leftErr, rightErr)
	}
	leftIDs := findingIdentities(leftReport.Problems)
	rightIDs := findingIdentities(rightReport.Problems)
	if !reflect.DeepEqual(leftIDs, rightIDs) {
		t.Fatalf("fingerprints/order changed with input order\nleft=%v\nright=%v", leftIDs, rightIDs)
	}
}

func TestProblemEngineCapsConfidenceForSmallSamples(t *testing.T) {
	summary := completeProblemFixture()
	summary.Routes = []RouteStats{{Route: "GET /single", Count: 1, Failures: 1, P95MS: 2_000}}
	report, err := BuildProblemReport(summary)
	if err != nil {
		t.Fatalf("BuildProblemReport() error = %v", err)
	}
	var network ProblemFinding
	for _, finding := range report.Problems {
		if finding.DetectorID == "network.route_health" {
			network = finding
		}
	}
	if network.ID == "" || network.Confidence != "low" {
		t.Fatalf("small sample confidence = %+v", network)
	}
	if network.Severity != "low" || network.InvestigationPriority > 34 {
		t.Fatalf("single request was ranked as a recurring defect: %+v", network)
	}
	if !strings.Contains(network.Title, "единственном наблюдении") || strings.Contains(network.Title, "часто") {
		t.Fatalf("single failure title exaggerates frequency: %q", network.Title)
	}
	if !warningsContain(network.Limitations, "Малая выборка") {
		t.Fatalf("small sample limitation missing: %+v", network.Limitations)
	}
	if strings.Contains(network.WhatHappened, "0.00/с") || strings.Contains(network.WhatHappened, "p95") {
		t.Fatalf("single request is described with misleading rate/percentile: %q", network.WhatHappened)
	}
	if !strings.Contains(network.WhatHappened, "единственный вызов занял 2000 мс") {
		t.Fatalf("single request has no junior-friendly duration: %q", network.WhatHappened)
	}
}

func TestProblemEngineExplainsLongFrameTailWithoutMisleadingZeroRate(t *testing.T) {
	report, err := BuildProblemReport(Summary{
		DurationMS:        60_000,
		CollectionQuality: CollectionQuality{Complete: true},
		Screens: []ScreenStats{{
			Screen: "CustomViewLab", Frames: 214, JankyFrames: 0, JankRatePct: 0,
			FrameP95MS: 100, FrameP99MS: 250, FrameSource: "jankstats",
			FrameDeadlineUS: 32_000, FrameDeadlineStatus: "consistent",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	finding := findingByDetector(report.Problems, "ui.jank_tail")
	if finding == nil {
		t.Fatalf("tail-only UI finding missing: %+v", report.Problems)
	}
	if !strings.Contains(finding.Title, "выходят за целевое время") || strings.Contains(finding.WhatHappened, "Подтормаживали 0.0%") {
		t.Fatalf("tail-only UI finding is misleading: %+v", finding)
	}
	if finding.Frequency != nil || finding.Evidence[0].Observed != "не отмечены" {
		t.Fatalf("zero system flag was rendered as zero real incidents: %+v", finding)
	}
}

func TestProblemDetectorConfigFailsClosed(t *testing.T) {
	cfg := DefaultProblemDetectorConfig()
	cfg.HTTPFailureRate = 2
	if _, err := BuildProblemReportWithConfig(Summary{}, cfg); err == nil {
		t.Fatal("invalid detector config was accepted")
	}
}

func TestProblemPriorityBandBoundaries(t *testing.T) {
	for score, want := range map[int]string{0: "info", 1: "low", 34: "low", 35: "medium", 59: "medium", 60: "high", 79: "high", 80: "critical", 100: "critical"} {
		if got := severityForPriority(score); got != want {
			t.Fatalf("severityForPriority(%d) = %q, want %q", score, got, want)
		}
	}
}

func TestProblemBuilderKeepsReportWhenDetectorEmitsSameTargetTwice(t *testing.T) {
	builder := problemBuilder{}
	for _, title := range []string{"Менее полезное наблюдение", "Более опасное наблюдение"} {
		priorityParts := priority(10, 5, 3, 2, 0, "impact", "magnitude", "exposure", "breadth", "compound")
		if strings.HasPrefix(title, "Более") {
			priorityParts = priority(30, 20, 10, 2, 0, "impact", "magnitude", "exposure", "breadth", "compound")
		}
		builder.add(ProblemFinding{
			DetectorID: "ui.compose_work", Category: ProblemCategoryUI, Subcategory: "compose_composition",
			Confidence: "high", Title: title, Where: []ProblemLocation{{Screen: "Main", Owner: "com.app.Main"}},
			Why: ProblemWhy{ClaimLevel: "linked"}, PriorityBreakdown: priorityParts,
		})
	}

	builder.finishFindings()
	if len(builder.findings) != 1 || builder.findings[0].Title != "Более опасное наблюдение" {
		t.Fatalf("duplicate detector target invalidated or weakened report: %+v", builder.findings)
	}
}

func TestCompareProblemsUsesFingerprintStatuses(t *testing.T) {
	baseline := completeProblemFixture()
	baseline.Routes[0].Count = 20
	baseline.Routes[0].Failures = 0
	baseline.Routes[0].P95MS = 800
	baseline.Routes[0].P50MS = 300
	baseline = attachProblemReport(t, baseline)

	candidate := completeProblemFixture()
	candidate.Routes = append(candidate.Routes, RouteStats{Route: "GET /new", Count: 30, Failures: 10, P95MS: 1_000})
	candidate = attachProblemReport(t, candidate)

	comparison := CompareProblems(baseline, candidate, true)
	statuses := map[string]int{}
	for _, delta := range comparison.Deltas {
		statuses[delta.Status]++
	}
	if statuses["regressed"] == 0 || statuses["new"] == 0 || statuses["persistent"] == 0 {
		t.Fatalf("problem compare statuses = %+v; deltas=%+v", statuses, comparison.Deltas)
	}
	for index := 1; index < len(comparison.Deltas); index++ {
		if problemDeltaRank(comparison.Deltas[index-1].Status) < problemDeltaRank(comparison.Deltas[index].Status) {
			t.Fatalf("problem deltas are not status-ranked: %+v", comparison.Deltas)
		}
	}
}

func TestProblemGateUsesCanonicalFindingsAndCoverage(t *testing.T) {
	baseline := completeProblemFixture()
	baseline.Routes = baseline.Routes[1:]
	baseline = attachProblemReport(t, baseline)
	candidate := attachProblemReport(t, completeProblemFixture())
	comparison := Compare(baseline, candidate)
	zero := 0
	result := EvaluateGate(comparison, ThresholdConfig{Problems: ProblemGateThreshold{
		MaxHigh: &zero, FailOnNew: true, MinConfidence: "high", RequiredCoverage: []string{ProblemCategoryIO},
	}})
	if !result.Failed || !warningsContain(result.Failures, "new problem") || !warningsContain(result.Failures, "required problem coverage io_storage") {
		t.Fatalf("canonical problem gate = %+v", result)
	}
}

func TestDatabaseScenarioFindingIsEvidenceBoundedHypothesis(t *testing.T) {
	summary := completeProblemFixture()
	summary.DatabaseAnalysis = &DatabaseAnalysis{Scenarios: DatabaseScenarioAnalysis{
		ObservedOperationScopes: 10,
		Candidates: []DatabaseScenarioStats{{
			Kind: "possible_n_plus_one_or_duplicate", ClaimLevel: "hypothesis",
			ScopeKind: "operation", Query: "SELECT item FROM feed WHERE id = ?",
			Source: "FeedDao.load", Screen: "Feed", ContextOperation: "feed.open",
			Operation: "query", StatementFingerprint: 101,
			AffectedScopes: 2, ObservedScopes: 10, EstimatedCalls: 12, RetainedCalls: 12,
			MaxCallsPerScope: 7, CallsPerAffectedScope: 6, CallsPerObservedScope: 1.2,
			TotalDurationUS: 24_000, MaxDurationUS: 4_000, MainThreadCalls: 4,
		}},
	}}

	report, err := BuildProblemReport(summary)
	if err != nil {
		t.Fatal(err)
	}
	finding := findingByDetector(report.Problems, "io.database_repeated_in_scope")
	if finding == nil || finding.Why.ClaimLevel != "hypothesis" {
		t.Fatalf("scenario finding = %+v", finding)
	}
	if !strings.Contains(finding.WhatHappened, "7") ||
		!strings.Contains(finding.Why.Summary, "нельзя отличить") ||
		!strings.Contains(finding.Recommendations[0].Action, "пакет") {
		t.Fatalf("scenario explanation = %+v", finding)
	}
	assertProblemTextIsRussian(t, *finding)
	for _, text := range []string{finding.Title, finding.WhatHappened, finding.Why.Summary} {
		if strings.Contains(strings.ToLower(text), "bind value") {
			t.Fatalf("scenario leaks parameter semantics: %q", text)
		}
	}
}

func TestDatabaseScenarioFindingExplainsMissingSQLTemplateThroughKnownDAO(t *testing.T) {
	summary := completeProblemFixture()
	summary.DatabaseAnalysis = &DatabaseAnalysis{Scenarios: DatabaseScenarioAnalysis{
		ObservedTransactionScopes: 12_923,
		Candidates: []DatabaseScenarioStats{{
			Kind: "batch_candidate", ClaimLevel: "hypothesis", ScopeKind: "transaction",
			Query: "unknown", Source: "ru.mail.im.persistence.room.dao.MessageDataDao_Impl.insertOrFailAndReturnId$lambda$0",
			Screen: "ru.mail.MainActivity", ContextOperation: "messages.store", Operation: "insert",
			AffectedScopes: 175, ObservedScopes: 12_923, EstimatedCalls: 3_037, RetainedCalls: 512,
			MaxCallsPerScope: 135, CallsPerAffectedScope: 17.35, CallsPerObservedScope: 0.24,
			TotalDurationUS: 2_363_000,
		}},
	}}

	report, err := BuildProblemReport(summary)
	if err != nil {
		t.Fatal(err)
	}
	finding := findingByDetector(report.Problems, "io.database_repeated_in_scope")
	if finding == nil {
		t.Fatal("database scenario finding is missing")
	}
	joined := strings.Join([]string{
		finding.Title,
		finding.WhatHappened,
		finding.Why.Summary,
		strings.Join(finding.Why.Factors, " "),
		strings.Join(finding.Limitations, " "),
	}, " ")
	for _, expected := range []string{
		"SQL-шаблон не записан",
		"MessageDataDao_Impl.insertOrFailAndReturnId$lambda$0",
		"одной транзакции",
		"175 транзакциях",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("scenario explanation %q does not contain %q", joined, expected)
		}
	}
	assertProblemTextIsRussian(t, *finding)
}

func TestProblemConfidenceDoesNotCopyInternalLossCountersIntoFinding(t *testing.T) {
	confidence, reasons, limitations := problemConfidence(Summary{CollectionQuality: CollectionQuality{
		Complete:        false,
		AcceptedEvents:  30_000,
		WrittenEvents:   29_000,
		KnownLostEvents: 1_000,
		Reasons: []string{
			"ограниченные runtime-реестры потеряли 25661 элементов evidence",
			"writer отклонил batch runtime-графа: 16",
		},
	}}, 10, 2, true)
	if confidence != "low" || len(reasons) == 0 {
		t.Fatalf("confidence = %q, reasons = %+v", confidence, reasons)
	}
	if len(limitations) != 1 || !strings.Contains(strings.ToLower(limitations[0]), "повторным прогоном") {
		t.Fatalf("user-facing limitations = %+v", limitations)
	}
	for _, value := range limitations {
		if strings.Contains(value, "25661") || strings.Contains(value, "writer") || strings.Contains(value, "evidence") {
			t.Fatalf("internal collection counter leaked into problem: %q", value)
		}
	}
}

func TestDependencyInjectionCategoryIsAddedOnlyForKnownDIClasses(t *testing.T) {
	summary := completeProblemFixture()
	summary.DatabaseAnalysis = &DatabaseAnalysis{Scenarios: DatabaseScenarioAnalysis{Candidates: []DatabaseScenarioStats{{
		Kind: "batch_candidate", ClaimLevel: "hypothesis", ScopeKind: "transaction",
		Query: "INSERT INTO messages VALUES (?)", Source: "com.app.di.MessageRepository.insert",
		AffectedScopes: 2, ObservedScopes: 10, EstimatedCalls: 8, RetainedCalls: 8,
		MaxCallsPerScope: 4, CallsPerObservedScope: 0.8, TotalDurationUS: 10_000,
	}}}}
	catalog := &DependencyInjectionCatalog{Available: true, Classes: []DependencyInjectionClass{{
		Name: "com.app.di.MessageRepository", Framework: "hilt",
	}}}

	report, err := buildProblemReportWithCatalog(summary, DefaultProblemDetectorConfig(), catalog)
	if err != nil {
		t.Fatal(err)
	}
	finding := findingByDetector(report.Problems, "io.database_repeated_in_scope")
	if finding == nil || !warningsContain(finding.RelatedCategories, ProblemCategoryDependencyInjection) {
		t.Fatalf("DI-related finding = %+v", finding)
	}
	coverage := categoryCoverageByID(report.Coverage, ProblemCategoryDependencyInjection)
	if coverage == nil || coverage.Label != "DI" || coverage.FindingCount == 0 {
		t.Fatalf("DI coverage = %+v", coverage)
	}

	withoutCatalog, err := BuildProblemReport(summary)
	if err != nil {
		t.Fatal(err)
	}
	if categoryCoverageByID(withoutCatalog.Coverage, ProblemCategoryDependencyInjection) != nil {
		t.Fatalf("DI category appeared without enabled DI analysis: %+v", withoutCatalog.Coverage)
	}
}

func TestCategoryCoverageDoesNotExposeInternalCollectionLossCounters(t *testing.T) {
	summary := completeProblemFixture()
	summary.CollectionQuality = CollectionQuality{
		Complete:        false,
		AcceptedEvents:  30_000,
		WrittenEvents:   29_000,
		KnownLostEvents: 1_000,
		Reasons: []string{
			"ограниченные runtime-реестры потеряли 25661 элементов evidence",
			"writer отклонил batch runtime-графа: 16",
		},
	}
	report, err := BuildProblemReport(summary)
	if err != nil {
		t.Fatal(err)
	}
	for _, coverage := range report.Coverage {
		text := strings.ToLower(coverage.Explanation + " " + strings.Join(coverage.AvailableEvidence, " ") + " " + strings.Join(coverage.MissingEvidence, " "))
		for _, forbidden := range []string{"25661", "writer", "evidence", "runtime-реестр", "зарегистрированные потери"} {
			if strings.Contains(text, forbidden) {
				t.Fatalf("category %q exposes internal loss detail %q: %+v", coverage.Category, forbidden, coverage)
			}
		}
	}
}

func assertProblemTextIsRussian(t *testing.T, finding ProblemFinding) {
	t.Helper()
	parts := []string{finding.Title, finding.WhatHappened, finding.Why.Summary}
	parts = append(parts, finding.Why.Factors...)
	parts = append(parts, finding.Impact...)
	parts = append(parts, finding.Limitations...)
	for _, item := range finding.Evidence {
		parts = append(parts, item.Name)
	}
	for _, item := range finding.Recommendations {
		parts = append(parts, item.Action, item.Rationale, item.Verification)
	}
	joined := strings.ToLower(strings.Join(parts, " "))
	for _, forbidden := range []string{
		"unknown", "batching", "batch/", "join/", "transaction scope", "scopes",
		"fingerprint", "fan-out", "evidence", "round trips", "main samples", "statement",
		"source", "calls/scope", "total db duration", "bounded", "samples", "events",
	} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("problem text contains %q: %s", forbidden, joined)
		}
	}
}

func categoryCoverageByID(values []CategoryCoverage, category string) *CategoryCoverage {
	for index := range values {
		if values[index].Category == category {
			return &values[index]
		}
	}
	return nil
}

func completeProblemFixture() Summary {
	return Summary{
		DurationMS:        40_000,
		HTTPCount:         190,
		UIFrames:          1_400,
		MemoryCount:       4,
		CollectionQuality: CollectionQuality{Complete: true},
		AnalysisInputs:    AnalysisInputCompleteness{Complete: true, RuntimeEvidence: true},
		Routes: []RouteStats{
			{Route: "GET /feed", Count: 186, Failures: 24, P50MS: 800, P95MS: 1_800, MaxMS: 4_000, BytesRx: 10_000_000, OwnerSample: "FeedRepository.load", PeakRequestsPerSecond: 12},
			{Route: "GET /avatar", Count: 4, P95MS: 80},
		},
		SignalContexts: []SignalContextStats{{Screen: "Feed", Operation: "refresh.load", Owner: "FeedRepository.load", RouteSample: "GET /feed", HTTPCount: 186}},
		Screens: []ScreenStats{
			{Screen: "Feed", Frames: 1_000, JankyFrames: 180, JankRatePct: 18, WindowCount: 5, FrameP95MS: 42, FrameP99MS: 90, FrameSource: "jankstats", FrameDeadlineUS: 16_667, FrameDeadlineStatus: "consistent", FrameDistributionState: "mergeable_histogram_v2"},
			{Screen: "Settings", Frames: 400, JankyFrames: 2, JankRatePct: 0.5, WindowCount: 2, FrameP95MS: 14, FrameSource: "jankstats", FrameDeadlineUS: 16_667, FrameDeadlineStatus: "consistent", FrameDistributionState: "mergeable_histogram_v2"},
		},
		LogSpam: []LogSpamStats{{Screen: "Feed", Owner: "FeedPresenter.render", Source: "Log", Level: "D", Count: 500}},
		Gauges:  []NamedValue{{Name: "process.cpu.core_percent_x100", Value: 8_500}, {Name: "device.thermal.status", Value: 3}},
	}
}

func findingIdentities(values []ProblemFinding) []string {
	out := make([]string, len(values))
	for index, value := range values {
		out[index] = value.Fingerprint + ":" + value.Severity
	}
	return out
}

func findingByDetector(values []ProblemFinding, detectorID string) *ProblemFinding {
	for index := range values {
		if values[index].DetectorID == detectorID {
			return &values[index]
		}
	}
	return nil
}

func evidenceUsesSource(values []ProblemEvidence, source string) bool {
	for _, value := range values {
		if value.Source == source {
			return true
		}
	}
	return false
}

func attachProblemReport(t *testing.T, summary Summary) Summary {
	t.Helper()
	report, err := BuildProblemReport(summary)
	if err != nil {
		t.Fatalf("BuildProblemReport() error = %v", err)
	}
	summary.ProblemSchemaVersion = ProblemSchemaVersion
	summary.ProblemSummary = report.Summary
	summary.Problems = report.Problems
	summary.CategoryCoverage = report.Coverage
	summary.Detectors = report.Registry
	return summary
}
