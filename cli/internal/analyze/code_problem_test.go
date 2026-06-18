package analyze

import (
	"slices"
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
	if strings.Contains(feedActivity.Evidence, "память=60000 KB") {
		t.Fatalf("retained age leaked into memory evidence: %+v", feedActivity)
	}
	if !strings.Contains(feedActivity.Evidence, "память=4096 KB") {
		t.Fatalf("heap retained size missing from evidence: %+v", feedActivity)
	}
}
