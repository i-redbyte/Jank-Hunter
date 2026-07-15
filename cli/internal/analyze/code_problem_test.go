package analyze

import (
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"
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
			Screen:  "Feed",
			Flow:    "feed.open",
			Step:    "load",
			Owner:   "com.app.FeedPresenter.render",
			Kind:    "main_thread_io",
			Windows: 1,
			Count:   2,
			MaxMS:   2_500,
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
			Flow:                "feed.open",
			Step:                "render",
			Count:               1,
			MaxAgeMS:            30_000,
			EstimatedRetainedKB: 8 * 1024,
			Severity:            "medium",
			ObjectKind:          "экран / Activity",
			Score:               9,
		}},
		LogSpam: []LogSpamStats{{
			Screen: "Feed",
			Flow:   "feed.open",
			Step:   "render",
			Owner:  "com.app.FeedPresenter.render",
			Source: "android.util.Log.d",
			Level:  "debug",
			Count:  250,
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
	if len(got) != 200 || cap(got) != len(got) {
		t.Fatalf("bounded result len/cap = %d/%d, want 200/200", len(got), cap(got))
	}
}

func codeProblemSelectionFixture() codeProblemBuilder {
	builder := codeProblemBuilder{items: map[string]*codeProblemAccumulator{}}
	for index := range 260 {
		className := fmt.Sprintf("com.app.problem.Class%03d", index)
		method := fmt.Sprintf("method%02d", index%17)
		item := builder.item(className, method, className+"."+method)
		item.addContext(
			fmt.Sprintf("screen-%d", index%4),
			fmt.Sprintf("flow-%d", index%7),
			fmt.Sprintf("step-%d", index%3),
			fmt.Sprintf("route-%d", index%5),
		)
		item.addSignal(CodeProblemSignal{
			Name:     fmt.Sprintf("signal-%d", index%3),
			Category: codeCategoryRuntime,
			Severity: "medium",
			Score:    float64((index*37)%53)/10 + 0.04,
			Count:    uint64(index + 1),
			Detail:   fmt.Sprintf("evidence-%03d", index),
		})
	}
	return builder
}

// finishCodeProblemsMaterializeAllForTest preserves the pre-optimization
// algorithm so the bounded implementation must select and serialize exactly
// the same top rows, including rounded-score ties.
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
	if len(out) > 200 {
		out = out[:200]
	}
	return out
}
