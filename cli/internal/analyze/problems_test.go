package analyze

import (
	"reflect"
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

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
	if len(network.RankBreakdown) != 5 || network.RiskScore <= 0 || network.Severity == "info" {
		t.Fatalf("network risk is not explainable: %+v", network)
	}
	if network.Frequency == nil || network.Frequency.RatePerSec == nil || *network.Frequency.RatePerSec < 4 {
		t.Fatalf("network exposure missing: %+v", network.Frequency)
	}
	if len(network.Evidence) < 3 || network.Evidence[0].Sample == nil {
		t.Fatalf("network evidence missing sample/denominator: %+v", network.Evidence)
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
		IOOperations: []IOStats{{
			Operation: "database_read", MainThread: true, Count: 8,
			TotalDurationUS: 800_000, MaxDurationUS: 275_000, Bytes: 32_768,
			Screen: "Feed", Flow: "refresh", Step: "load", Owner: "FeedRepository.load",
		}},
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
	if !strings.Contains(strings.Join(reasons, " "), "потер") || len(limits) == 0 {
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
	if network.Severity != "low" || network.RiskScore > 34 {
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

func TestProblemSeverityBoundaries(t *testing.T) {
	for score, want := range map[int]string{0: "info", 1: "low", 34: "low", 35: "medium", 59: "medium", 60: "high", 79: "high", 80: "critical", 100: "critical"} {
		if got := severityForRisk(score); got != want {
			t.Fatalf("severityForRisk(%d) = %q, want %q", score, got, want)
		}
	}
}

func TestProblemBuilderKeepsReportWhenDetectorEmitsSameTargetTwice(t *testing.T) {
	builder := problemBuilder{}
	for _, title := range []string{"Менее полезное наблюдение", "Более опасное наблюдение"} {
		riskParts := risk(10, 5, 3, 2, 0, "impact", "magnitude", "exposure", "breadth", "compound")
		if strings.HasPrefix(title, "Более") {
			riskParts = risk(30, 20, 10, 2, 0, "impact", "magnitude", "exposure", "breadth", "compound")
		}
		builder.add(ProblemFinding{
			DetectorID: "ui.compose_work", Category: ProblemCategoryUI, Subcategory: "compose_composition",
			Confidence: "high", Title: title, Where: []ProblemLocation{{Screen: "Main", Owner: "com.app.Main"}},
			Why: ProblemWhy{ClaimLevel: "linked"}, RankBreakdown: riskParts,
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
		Flows: []FlowStats{{Screen: "Feed", Flow: "refresh", Step: "load", Owner: "FeedRepository.load", RouteSample: "GET /feed", HTTPCount: 186}},
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
