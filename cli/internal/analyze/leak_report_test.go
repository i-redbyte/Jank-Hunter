package analyze

import (
	"strings"
	"testing"
)

func TestCompareLeakSuspectsClassifiesStatuses(t *testing.T) {
	baseline := []MemoryLeakSuspect{
		{ClassName: "com.app.ResolvedActivity", Holder: "com.app.Holder", Count: 2, MaxAgeMS: 30_000, Score: 12, Severity: "high"},
		{ClassName: "com.app.WorseFragment", Holder: "com.app.Presenter", Count: 1, MaxAgeMS: 15_000, Score: 7, Severity: "medium"},
		{ClassName: "com.app.BetterBinding", Holder: "com.app.Adapter", Count: 4, MaxAgeMS: 60_000, Score: 16, Severity: "high"},
		{ClassName: "com.app.SameCache", Holder: "com.app.Repository", Count: 1, MaxAgeMS: 15_000, Score: 5, Severity: "medium"},
	}
	candidate := []MemoryLeakSuspect{
		{ClassName: "com.app.WorseFragment", Holder: "com.app.Presenter", Count: 8, MaxAgeMS: 70_000, Score: 20, Severity: "high"},
		{ClassName: "com.app.BetterBinding", Holder: "com.app.Adapter", Count: 1, MaxAgeMS: 10_000, Score: 4, Severity: "ok"},
		{ClassName: "com.app.SameCache", Holder: "com.app.Repository", Count: 1, MaxAgeMS: 15_000, Score: 5, Severity: "medium"},
		{ClassName: "com.app.NewActivity", Holder: "com.app.Singleton", Count: 1, MaxAgeMS: 30_000, Score: 11, Severity: "medium"},
	}

	deltas := CompareLeakSuspects(baseline, candidate)
	statusByClass := map[string]string{}
	for _, delta := range deltas {
		row := delta.Candidate
		if !delta.HasCandidate {
			row = delta.Baseline
		}
		statusByClass[row.ClassName] = delta.Status
	}

	assertLeakStatus(t, statusByClass, "com.app.NewActivity", LeakDeltaNew)
	assertLeakStatus(t, statusByClass, "com.app.WorseFragment", LeakDeltaWorse)
	assertLeakStatus(t, statusByClass, "com.app.BetterBinding", LeakDeltaBetter)
	assertLeakStatus(t, statusByClass, "com.app.ResolvedActivity", LeakDeltaResolved)
	assertLeakStatus(t, statusByClass, "com.app.SameCache", LeakDeltaSame)
}

func TestCompareLeakSuspectsMatchesByChainFingerprint(t *testing.T) {
	baseline := []MemoryLeakSuspect{
		{
			ClassName:        "com.app.checkout.CheckoutActivity",
			Holder:           "OldPresenter",
			Count:            2,
			MaxAgeMS:         30_000,
			Score:            12,
			Severity:         "medium",
			HeapEvidence:     true,
			ChainFingerprint: "chain:checkout-activity",
		},
	}
	candidate := []MemoryLeakSuspect{
		{
			ClassName:        "com.app.checkout.CheckoutActivity",
			Holder:           "NewPresenter",
			Count:            2,
			MaxAgeMS:         30_000,
			Score:            12,
			Severity:         "medium",
			HeapEvidence:     true,
			ChainFingerprint: "chain:checkout-activity",
		},
	}

	deltas := CompareLeakSuspects(baseline, candidate)
	if len(deltas) != 1 {
		t.Fatalf("expected one matched delta, got %+v", deltas)
	}
	if deltas[0].Status != LeakDeltaSame {
		t.Fatalf("status = %q, want same; delta=%+v", deltas[0].Status, deltas[0])
	}
	if !strings.Contains(deltas[0].MatchConfidence, "отпечаток цепочки из дампа памяти") {
		t.Fatalf("unexpected match confidence: %q", deltas[0].MatchConfidence)
	}
}

func TestBuildLeakReportUsesHeapModeWhenEvidenceExists(t *testing.T) {
	report := BuildLeakReport(Summary{MemoryLeaks: []MemoryLeakSuspect{{
		ClassName:           "com.app.LeakedActivity",
		Holder:              "com.app.Singleton",
		Count:               1,
		MaxAgeMS:            30_000,
		HeapEvidence:        true,
		EstimatedRetainedKB: 8192,
		ReferencePath: []HeapPathElement{
			{ClassName: "GC root: sticky class", Kind: "gc_root", ObjectID: "0x1"},
			{ClassName: "com.app.Singleton", FieldName: "activity", Kind: "static", ObjectID: "0x2"},
			{ClassName: "com.app.LeakedActivity", FieldName: "activity", Kind: "field", ObjectID: "0x3"},
		},
	}}})

	if report.Mode != LeakModeHeap {
		t.Fatalf("BuildLeakReport mode = %q, want %q", report.Mode, LeakModeHeap)
	}
	if len(report.Items) != 1 || !BuildLeakGraph(report.Items[0].Suspect).HasHeapPath {
		t.Fatalf("expected heap graph item, got %+v", report.Items)
	}
}

func TestBuildLeakReportShowsHPROFCheckWithoutReachablePath(t *testing.T) {
	report := BuildLeakReport(Summary{MemoryLeaks: []MemoryLeakSuspect{{
		ClassName:     "com.app.ReleasedActivity",
		Count:         1,
		HeapCandidate: true,
		EvidenceKind:  RetentionEvidenceUnconfirmedHPROF,
	}}})

	if report.Mode != LeakModeHeap {
		t.Fatalf("BuildLeakReport mode = %q, want %q when HPROF was checked", report.Mode, LeakModeHeap)
	}
	if report.Stats.UnconfirmedHPROF != 1 {
		t.Fatalf("unconfirmed HPROF count = %d, want 1", report.Stats.UnconfirmedHPROF)
	}
	if !strings.Contains(report.ModeTitle, "HPROF проверен") ||
		!strings.Contains(report.ModeHint, "путь от GC root не найден") {
		t.Fatalf("report hides HPROF result: title=%q hint=%q", report.ModeTitle, report.ModeHint)
	}
}

func TestBuildLeakCompareReportKeepsUnconfirmedHPROFCounts(t *testing.T) {
	comparison := Comparison{
		Baseline: Summary{MemoryLeaks: []MemoryLeakSuspect{{
			ClassName: "com.app.OldActivity", HeapCandidate: true,
			EvidenceKind: RetentionEvidenceUnconfirmedHPROF,
		}}},
		Candidate: Summary{MemoryLeaks: []MemoryLeakSuspect{
			{ClassName: "com.app.NewActivity", HeapCandidate: true, EvidenceKind: RetentionEvidenceUnconfirmedHPROF},
			{ClassName: "com.app.View", HeapCandidate: true, EvidenceKind: RetentionEvidenceUnconfirmedHPROF},
		}},
	}

	report := BuildLeakCompareReport(comparison)
	if report.Stats.HeapUnconfirmedBefore != 1 || report.Stats.HeapUnconfirmedAfter != 2 {
		t.Fatalf("unconfirmed HPROF counts = %d -> %d, want 1 -> 2", report.Stats.HeapUnconfirmedBefore, report.Stats.HeapUnconfirmedAfter)
	}
}

func TestBuildLeakReportDoesNotAddPotentiallyOverlappingRetainedSizes(t *testing.T) {
	report := BuildLeakReport(Summary{MemoryLeaks: []MemoryLeakSuspect{
		{ClassName: "com.app.Parent", Count: ^uint64(0), EstimatedRetainedKB: 8_192},
		{ClassName: "com.app.Child", Count: 1, EstimatedRetainedKB: 4_096},
	}})

	if report.Stats.TotalRetained != ^uint64(0) {
		t.Fatalf("retained count wrapped to %d", report.Stats.TotalRetained)
	}
	if report.Stats.EstimatedRetainedKB != 8_192 {
		t.Fatalf("retained-size headline = %d KB, want largest single estimate 8192 KB", report.Stats.EstimatedRetainedKB)
	}
}

func TestBuildLeakReportKeepsLargeRegistryAllocationBounded(t *testing.T) {
	const suspectCount = 256
	summary := Summary{MemoryLeaks: make([]MemoryLeakSuspect, suspectCount)}
	for index := range summary.MemoryLeaks {
		summary.MemoryLeaks[index] = MemoryLeakSuspect{
			ClassName: "com.example.LeakedActivity", Holder: "com.example.AppCache",
			Screen: "Feed", Operation: "open", Count: 1, MaxAgeMS: 30_000,
			DominatorPath: []string{"экран: Feed", "держатель: AppCache", "удержанный объект: LeakedActivity"},
		}
	}

	allocations := testing.AllocsPerRun(10, func() {
		_ = BuildLeakReport(summary)
	})
	if allocations > 3_000 {
		t.Fatalf("BuildLeakReport allocates %.0f objects for %d rows, want at most 3000", allocations, suspectCount)
	}
}

func TestRuntimeLeakGraphUsesTypedContextRelations(t *testing.T) {
	graph := runtimeLeakGraph(MemoryLeakSuspect{
		ClassName: "com.app.LeakedView",
		DominatorPath: []string{
			"экран: FeedActivity",
			"операция: feed.open.render",
			"держатель: FeedPresenter",
			"метод: bind",
			"удержанный объект: com.app.LeakedView",
		},
	})

	if graph.Title != "Контекст обнаружения удержанного объекта" {
		t.Fatalf("runtime graph title = %q", graph.Title)
	}
	want := []string{
		"операция на экране",
		"связано с вероятным владельцем",
		"место наблюдения",
		"объект оставался жив после lifecycle",
	}
	if len(graph.Edges) != len(want) {
		t.Fatalf("runtime graph edges = %+v", graph.Edges)
	}
	for index, label := range want {
		if graph.Edges[index].Label != label {
			t.Fatalf("edge %d label = %q, want %q", index, graph.Edges[index].Label, label)
		}
	}
	if graph.Nodes[3].Kind != "method" {
		t.Fatalf("method node kind = %q", graph.Nodes[3].Kind)
	}
}

func TestRuntimeRetentionSummaryDoesNotCallContextAReferenceChain(t *testing.T) {
	summary := retainedLeakChainSummary(
		memoryLeakStats{screen: "FeedActivity", operation: "feed.open.render", count: 2},
		"com.app.FeedPresenter.bind",
		"com.app.LeakedView",
		"View",
		"среднее: пользовательский держатель и контекст",
		RetentionEvidenceAfterExplicitGC,
	)
	if strings.Contains(summary, "Доверие цепочки") {
		t.Fatalf("runtime summary mislabels context as a reference chain: %q", summary)
	}
	if !strings.Contains(summary, "Надёжность связи с данными выполнения") {
		t.Fatalf("runtime summary has no attribution label: %q", summary)
	}
}

func TestSaturatingSignedUint64DeltaKeepsExtremeValuesRepresentable(t *testing.T) {
	maximum := ^uint64(0)
	if got, want := saturatingSignedUint64Delta(0, maximum), int64(1<<63-1); got != want {
		t.Fatalf("positive saturated delta = %d, want %d", got, want)
	}
	if got, want := saturatingSignedUint64Delta(maximum, 0), int64(-1<<63); got != want {
		t.Fatalf("negative saturated delta = %d, want %d", got, want)
	}
	if got, want := saturatingSignedUint64Delta(9, 4), int64(-5); got != want {
		t.Fatalf("ordinary delta = %d, want %d", got, want)
	}
}

func TestLeakCompareVerdictTranslatesConfidence(t *testing.T) {
	verdict := leakCompareVerdict(LeakCompareStats{ChangeLabel: "без изменений", BaselineTotal: 1, CandidateTotal: 1}, "low")
	if !strings.Contains(verdict, "Надёжность сравнения: низкое") || strings.Contains(verdict, "Надёжность сравнения: low") {
		t.Fatalf("leak compare verdict = %q", verdict)
	}
}

func TestRetainedEvidenceUsesReadableLabelsWithoutDuplicates(t *testing.T) {
	evidence := retainedEvidence(
		memoryLeakStats{count: 2, maxAgeMs: 15_000, timeOnlyCount: 1, afterExplicitGCCount: 1},
		0,
		0,
		0,
		"",
		nil,
		RetentionEvidenceAfterExplicitGC,
	)

	for _, forbidden := range []string{"time_only", "after_explicit_gc", "уровень=", "смысл="} {
		if strings.Contains(evidence, forbidden) {
			t.Fatalf("internal label %q leaked into evidence: %q", forbidden, evidence)
		}
	}
	for _, required := range []string{"Основание:", "Что это значит:", "После задержки: 1", "После явного GC: 1"} {
		if !strings.Contains(evidence, required) {
			t.Fatalf("evidence does not contain %q: %q", required, evidence)
		}
	}
}

func assertLeakStatus(t *testing.T, statuses map[string]string, className, want string) {
	t.Helper()
	if got := statuses[className]; got != want {
		t.Fatalf("status for %s = %q, want %q; all statuses=%+v", className, got, want, statuses)
	}
}
