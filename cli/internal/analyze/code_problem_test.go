package analyze

import (
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"
)

var (
	codeProblemEvidenceAllocationSink       string
	codeProblemImpactAllocationSink         string
	codeProblemRecommendationAllocationSink string
)

func TestCodeProblemRegistryAddsRiskCategoriesAndDrillDown(t *testing.T) {
	summary := Summary{
		Routes: []RouteStats{{
			Route:       "GET /feed",
			Count:       6,
			P95MS:       900,
			MaxMS:       1_200,
			OwnerSample: "com.app.FeedRepository.load",
		}},
		ProblemWindows: []ProblemWindowStats{{
			Screen:    "Feed",
			Operation: "feed.open.load",
			Owner:     "com.app.FeedPresenter.render",
			Kind:      "main_thread_io",
			Windows:   1,
			Count:     2,
			MaxMS:     2_500,
		}, {
			Owner:   "com.app.FeedRepository.load",
			Kind:    "http_slow_or_failed",
			Windows: 1,
			Count:   1,
			MaxMS:   1_200,
		}},
		MemoryLeaks: []MemoryLeakSuspect{{
			ClassName:           "com.app.FeedActivity",
			Holder:              "com.app.FeedPresenter",
			Screen:              "Feed",
			Operation:           "feed.open.render",
			Count:               1,
			MaxAgeMS:            30_000,
			EstimatedRetainedKB: 8 * 1024,
			Severity:            "medium",
			ObjectKind:          "экран / Activity",
			Score:               9,
		}},
		LogSpam: []LogSpamStats{{
			Screen:    "Feed",
			Operation: "feed.open.render",
			Owner:     "com.app.FeedPresenter.render",
			Source:    "android.util.Log.d",
			Level:     "debug",
			Count:     250,
		}},
	}

	rows := BuildCodeProblemRegistry(summary)
	if len(rows) == 0 {
		t.Fatalf("expected code problems")
	}
	var presenterMethod CodeProblemStats
	var presenterHolder CodeProblemStats
	for _, row := range rows {
		if row.ClassName == "com.app.FeedPresenter" && row.Method == "render" {
			presenterMethod = row
		}
		if row.ClassName == "com.app.FeedPresenter" && row.Method == "" {
			presenterHolder = row
		}
	}
	if presenterMethod.ClassName == "" || presenterHolder.ClassName == "" {
		t.Fatalf("expected presenter method and holder rows: %+v", rows)
	}
	for _, category := range []string{codeCategoryANR, codeCategoryMainIO, codeCategoryLogSpam} {
		if !slices.Contains(presenterMethod.Categories, category) {
			t.Fatalf("presenter method categories missing %q: %+v", category, presenterMethod.Categories)
		}
	}
	for _, category := range []string{codeCategoryLifecycle, codeCategoryOOM} {
		if !slices.Contains(presenterHolder.Categories, category) {
			t.Fatalf("presenter holder categories missing %q: %+v", category, presenterHolder.Categories)
		}
	}
	if len(presenterHolder.DrillDown) == 0 {
		t.Fatalf("expected drill-down: %+v", presenterHolder)
	}
	if presenterHolder.DrillDown[0].Evidence == "" || presenterHolder.DrillDown[0].Recommendation == "" {
		t.Fatalf("drill-down should include evidence and recommendation: %+v", presenterHolder.DrillDown)
	}

	var repository CodeProblemStats
	for _, row := range rows {
		if row.ClassName == "com.app.FeedRepository" {
			repository = row
		}
	}
	if !slices.Contains(repository.Categories, codeCategoryDuplicate) {
		t.Fatalf("repository should be marked duplicate network: %+v", repository.Categories)
	}
}

func TestCodeProblemRegistryDoesNotTreatRetainedAgeAsMemory(t *testing.T) {
	rows := BuildCodeProblemRegistry(Summary{
		Owners: []OwnerStats{{
			Owner: "com.app.FeedActivity",
			Kind:  "retained_object",
			Count: 1,
			MaxMS: 60_000,
		}},
		MemoryLeaks: []MemoryLeakSuspect{{
			ClassName:           "com.app.BigBitmapHolder",
			Holder:              "com.app.FeedActivity",
			Count:               1,
			MaxAgeMS:            60_000,
			EstimatedRetainedKB: 4_096,
			Severity:            "medium",
			Score:               8,
		}},
	})

	var feedActivity CodeProblemStats
	for _, row := range rows {
		if row.ClassName == "com.app.FeedActivity" && row.Owner == "com.app.FeedActivity" {
			feedActivity = row
		}
	}
	if strings.Contains(feedActivity.Evidence, "память=60000 КБ") {
		t.Fatalf("retained age leaked into memory evidence: %+v", feedActivity)
	}
	if !strings.Contains(feedActivity.Evidence, "память=4096 КБ") {
		t.Fatalf("heap retained size missing from evidence: %+v", feedActivity)
	}
}

func TestCodeProblemFinishMatchesMaterializeAllSelection(t *testing.T) {
	optimized := codeProblemSelectionFixture()
	reference := codeProblemSelectionFixture()

	got := optimized.finish()
	want := finishCodeProblemsMaterializeAllForTest(&reference)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("optimized top problems differ from materialize-all reference\ngot:  %+v\nwant: %+v", got, want)
	}
	if len(got) != 260 || cap(got) != len(got) {
		t.Fatalf("complete result len/cap = %d/%d, want 260/260", len(got), cap(got))
	}
}

func TestCodeProblemDrillDownKeepsOnlyObservedContextTuples(t *testing.T) {
	builder := codeProblemBuilder{items: map[codeProblemKey]*codeProblemAccumulator{}}
	item := builder.item("com.app.FeedPresenter", "render", "com.app.FeedPresenter.render")
	for index := range 15 {
		item.addContextSignal(
			fmt.Sprintf("screen-%02d", index),
			fmt.Sprintf("operation-%02d", index),
			"",
			CodeProblemSignal{
				Name:     fmt.Sprintf("signal-%02d", index),
				Category: codeCategoryRuntime,
				Severity: "medium",
				Score:    1,
				Count:    uint64(index + 1),
			},
		)
	}

	rows := builder.finish()
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	row := rows[0]
	if len(row.Screens) != 15 || len(row.Operations) != 15 || len(row.DrillDown) != 15 {
		t.Fatalf("context evidence was truncated: %+v", row)
	}
	for index, detail := range row.DrillDown {
		wantSuffix := fmt.Sprintf("%02d", index)
		if detail.Screen != "screen-"+wantSuffix || detail.Operation != "operation-"+wantSuffix {
			t.Fatalf("drill-down %d fabricated a context tuple: %+v", index, detail)
		}
		wantSignal := "signal-" + wantSuffix
		if !reflect.DeepEqual(detail.Signals, []string{wantSignal}) {
			t.Fatalf("drill-down %d signals = %v, want only %q", index, detail.Signals, wantSignal)
		}
		if !strings.Contains(detail.Evidence, fmt.Sprintf("наблюдений=%d", index+1)) {
			t.Fatalf("drill-down %d evidence is not context-specific: %q", index, detail.Evidence)
		}
	}
}

func TestCodeProblemSeverityMatchesDisplayedScoreBands(t *testing.T) {
	if got := codeProblemSeverity(4.9, nil); got != "ok" {
		t.Fatalf("score 4.9 severity = %q", got)
	}
	if got := codeProblemSeverity(5, nil); got != "medium" {
		t.Fatalf("score 5 severity = %q", got)
	}
	if got := codeProblemSeverity(15, []CodeProblemSignal{{Category: codeCategoryNetwork}}); got != "high" {
		t.Fatalf("score 15 severity = %q", got)
	}
	if got := codeProblemSeverity(20, []CodeProblemSignal{{Category: codeCategoryInfluence}}); got != "medium" {
		t.Fatalf("static influence-only score severity = %q", got)
	}
}

func codeProblemSelectionFixture() codeProblemBuilder {
	builder := codeProblemBuilder{items: map[codeProblemKey]*codeProblemAccumulator{}}
	for index := range 260 {
		className := fmt.Sprintf("com.app.problem.Class%03d", index)
		method := fmt.Sprintf("method%02d", index%17)
		item := builder.item(className, method, className+"."+method)
		item.addContextSignal(
			fmt.Sprintf("screen-%d", index%4),
			fmt.Sprintf("operation-%d-%d", index%7, index%3),
			fmt.Sprintf("route-%d", index%5),
			CodeProblemSignal{
				Name:     fmt.Sprintf("signal-%d", index%3),
				Category: codeCategoryRuntime,
				Severity: "medium",
				Score:    float64((index*37)%53)/10 + 0.04,
				Count:    uint64(index + 1),
				Detail:   fmt.Sprintf("evidence-%03d", index),
			},
		)
	}
	return builder
}

// finishCodeProblemsMaterializeAllForTest independently materializes and sorts
// every row so finish must preserve the full registry and rounded-score ties.
func finishCodeProblemsMaterializeAllForTest(builder *codeProblemBuilder) []CodeProblemStats {
	out := make([]CodeProblemStats, 0, len(builder.items))
	for _, item := range builder.items {
		row := item.toStats()
		if row.Score <= 0 && len(row.Signals) == 0 {
			continue
		}
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			if out[i].ClassName == out[j].ClassName {
				return out[i].Method < out[j].Method
			}
			return out[i].ClassName < out[j].ClassName
		}
		return out[i].Score > out[j].Score
	})
	return out
}

func TestCodeProblemLookupDoesNotAllocate(t *testing.T) {
	builder := codeProblemBuilder{items: map[codeProblemKey]*codeProblemAccumulator{}}
	builder.item("com.app.FeedPresenter", "render", "com.app.FeedPresenter.render")

	allocations := testing.AllocsPerRun(1_000, func() {
		builder.item("com.app.FeedPresenter", "render", "com.app.FeedPresenter.render")
	})
	if allocations != 0 {
		t.Fatalf("code-problem lookup allocates %.2f objects, want zero", allocations)
	}
}

func TestCodeProblemKeyCannotCollideOnFieldSeparator(t *testing.T) {
	builder := codeProblemBuilder{items: map[codeProblemKey]*codeProblemAccumulator{}}
	builder.item("com.app.Feed\x00render", "now", "first")
	builder.item("com.app.Feed", "render\x00now", "second")

	if got := len(builder.items); got != 2 {
		t.Fatalf("code-problem registry has %d entries, want two distinct locations", got)
	}
}

func TestCodeProblemDrillDownAllocationBudget(t *testing.T) {
	accumulator := &codeProblemAccumulator{
		className: "com.app.FeedPresenter",
		method:    "render",
		contexts: []codeProblemContextAccumulator{{
			context: codeProblemContext{screen: "Feed", operation: "feed.render", route: "GET /feed"},
			signals: []string{"stall", "jank", "runtime", "network"},
			count:   4,
			totalMS: 120,
			maxMS:   60,
		}},
	}
	allocations := testing.AllocsPerRun(1_000, func() {
		codeProblemDrillDown(accumulator, "inspect")
	})
	if allocations > 8 {
		t.Fatalf("drill-down allocates %.2f objects, want at most 8", allocations)
	}
}

func TestCodeProblemEvidenceAllocationBudget(t *testing.T) {
	accumulator := &codeProblemAccumulator{
		problems:     1,
		mainThreadMS: 2,
		networkMS:    3,
		uiJank:       4,
		logSpam:      5,
		retained:     6,
		memoryKB:     7,
		runtimeCalls: 8,
		maxMS:        9,
	}
	want := "Сводка сигналов: проблем=1, главный поток=2 мс, сеть=3 мс, медленных кадров=4, логов=5, удержано=6, память=7 КБ, вызовов=8, макс=9 мс."
	if got := codeProblemEvidence(accumulator); got != want {
		t.Fatalf("evidence = %q, want %q", got, want)
	}
	allocations := testing.AllocsPerRun(1_000, func() {
		codeProblemEvidenceAllocationSink = codeProblemEvidence(accumulator)
	})
	if allocations > 2 {
		t.Fatalf("code-problem evidence allocates %.2f objects, want at most 2", allocations)
	}
}

func TestCodeProblemGuidanceAllocationBudget(t *testing.T) {
	categories := []string{codeCategoryNetwork, codeCategoryRuntime, codeCategoryNetwork}
	wantImpact := "увеличивает задержки сценария и может создавать сетевые циклы; утяжеляет цепочку выполнения в измеренном сценарии; увеличивает задержки сценария и может создавать сетевые циклы; пока нет подтверждения выполнением в этом прогоне."
	wantRecommendation := "проверьте дедупликацию запросов, кеширование, таймауты и повторные фоновые циклы; проверьте цепочку вызовов и стоимость вызываемого метода."
	if got := codeProblemImpact(categories, false); got != wantImpact {
		t.Fatalf("impact = %q, want %q", got, wantImpact)
	}
	if got := codeProblemRecommendation(categories); got != wantRecommendation {
		t.Fatalf("recommendation = %q, want %q", got, wantRecommendation)
	}
	allocations := testing.AllocsPerRun(1_000, func() {
		codeProblemImpactAllocationSink = codeProblemImpact(categories, false)
		codeProblemRecommendationAllocationSink = codeProblemRecommendation(categories)
	})
	if allocations > 4 {
		t.Fatalf("code-problem guidance allocates %.2f objects, want at most 4", allocations)
	}
}

func TestCodeProblemMaterializationReusesOwnedValueSlices(t *testing.T) {
	accumulator := &codeProblemAccumulator{
		className:    "com.app.FeedPresenter",
		method:       "render",
		categories:   []string{codeCategoryRuntime, codeCategoryUI},
		problemNames: []string{"Runtime", "Jank"},
		signals: []CodeProblemSignal{
			{Name: "Runtime", Category: codeCategoryRuntime, Score: 1},
			{Name: "Jank", Category: codeCategoryUI, Score: 2},
		},
		contexts: []codeProblemContextAccumulator{{
			context: codeProblemContext{screen: "Feed"},
			signals: []string{"Runtime", "Jank"},
		}},
	}
	categoryStorage := &accumulator.categories[0]
	problemStorage := &accumulator.problemNames[0]
	signalStorage := &accumulator.signals[0]
	contextSignalStorage := &accumulator.contexts[0].signals[0]

	row := accumulator.toStats()
	if &row.Categories[0] != categoryStorage || &row.Problems[0] != problemStorage || &row.Signals[0] != signalStorage {
		t.Fatal("code-problem materialization copied an accumulator-owned value slice")
	}
	if &row.DrillDown[0].Signals[0] != contextSignalStorage {
		t.Fatal("code-problem drill-down copied accumulator-owned signals")
	}
}
