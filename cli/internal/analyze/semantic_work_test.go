package analyze

import (
	"fmt"
	"math/rand"
	"reflect"
	"strings"
	"testing"
)

var benchmarkSemanticWork []SemanticWorkStats

func TestSemanticWorkParsesTypedRuntimeRoots(t *testing.T) {
	items := SemanticWork(Summary{RuntimeCalls: []RuntimeCallStats{{
		Caller: "jankhunter.semantic.v1.compose.draw.main", Callee: "com.app.Avatar.draw",
		Screen: "Profile", Count: 8, TotalMS: 60, MaxMS: 24,
	}, {
		Caller: "jankhunter.semantic.v1.room.dao.background", Callee: "com.app.UserDao_Impl.load",
		Count: 2, TotalMS: 700, MaxMS: 500,
	}, {
		Caller: "jankhunter.semantic.v1.worker.retry.background", Callee: "com.app.SyncWorker",
		Count: 1, TotalMS: 4_000, MaxMS: 4_000,
	}, {
		Caller: "com.app.Screen.render", Callee: "com.app.Repository.load", Count: 1,
	}}})

	if len(items) != 3 {
		t.Fatalf("semantic work count = %d, want 3: %+v", len(items), items)
	}
	if items[0].Domain != SemanticDomainCompose || items[0].Operation != "draw" || !items[0].MainThread {
		t.Fatalf("compose projection = %+v", items[0])
	}
	if items[1].Domain != SemanticDomainRoom || items[1].MainThread {
		t.Fatalf("room projection = %+v", items[1])
	}
	if items[2].Domain != SemanticDomainWorker || items[2].Outcome != "retry" {
		t.Fatalf("worker projection = %+v", items[2])
	}
}

func TestActionableSemanticWorkCollapsesGeneratedComposeMirror(t *testing.T) {
	summary := Summary{RuntimeCalls: []RuntimeCallStats{{
		Screen: "ComposeLab", Operation: "list.scroll",
		Caller: "jankhunter.semantic.v1.compose.composition.main",
		Callee: "com.app.FeedKt$Feed$lambda$2$$inlined$items$default$4.invoke",
		Count:  360, TotalMS: 2_196, MaxMS: 11,
	}, {
		Screen: "ComposeLab", Operation: "list.scroll",
		Caller: "jankhunter.semantic.v1.compose.composition.main",
		Callee: "com.app.FeedKt.FeedRow",
		Count:  360, TotalMS: 2_193, MaxMS: 11,
	}}}

	if raw := SemanticWork(summary); len(raw) != 2 {
		t.Fatalf("raw semantic evidence was unexpectedly changed: %+v", raw)
	}
	actionable := ActionableSemanticWork(summary)
	if len(actionable) != 1 || actionable[0].Owner != "com.app.FeedKt.FeedRow" {
		t.Fatalf("generated Compose mirror was not replaced by the named function: %+v", actionable)
	}

	report, err := BuildProblemReport(Summary{
		DurationMS:   60_000,
		Screens:      []ScreenStats{{Screen: "ComposeLab", Frames: 180, FrameDeadlineUS: 16_667}},
		RuntimeCalls: summary.RuntimeCalls,
	})
	if err != nil {
		t.Fatal(err)
	}
	var findings []ProblemFinding
	for _, finding := range report.Problems {
		if finding.DetectorID == "ui.compose_work" {
			findings = append(findings, finding)
		}
	}
	if len(findings) != 1 || !strings.Contains(findings[0].Title, "FeedRow") {
		t.Fatalf("Compose mirror produced duplicate findings: %+v", findings)
	}
}

func TestActionableSemanticWorkDoesNotMergeDifferentContextsOrMeasurements(t *testing.T) {
	summary := Summary{RuntimeCalls: []RuntimeCallStats{{
		Screen: "ComposeLab", Operation: "context.a",
		Caller: "jankhunter.semantic.v1.compose.composition.main",
		Callee: "com.app.FeedKt$Feed$lambda$2$$inlined$items$default$4.invoke",
		Count:  10, TotalMS: 100, MaxMS: 10,
	}, {
		Screen: "ComposeLab", Operation: "context.b",
		Caller: "jankhunter.semantic.v1.compose.composition.main",
		Callee: "com.app.FeedKt.FeedRow",
		Count:  10, TotalMS: 100, MaxMS: 10,
	}, {
		Screen: "ComposeLab", Operation: "context.c",
		Caller: "jankhunter.semantic.v1.compose.composition.main",
		Callee: "com.app.SlowKt$Slow$lambda$1$$inlined$Box$1.invoke",
		Count:  10, TotalMS: 100, MaxMS: 10,
	}, {
		Screen: "ComposeLab", Operation: "context.c",
		Caller: "jankhunter.semantic.v1.compose.composition.main",
		Callee: "com.app.SlowKt.SlowNamed",
		Count:  10, TotalMS: 200, MaxMS: 10,
	}}}

	actionable := ActionableSemanticWork(summary)
	if len(actionable) != 4 {
		t.Fatalf("unrelated Compose observations were merged: %+v", actionable)
	}
}

func TestActionableSemanticWorkMatchesQuadraticReference(t *testing.T) {
	random := rand.New(rand.NewSource(32621))
	owners := [...]string{
		"com.app.FeedKt.FeedRow",
		"com.app.FeedKt$Feed$lambda$2$$inlined$items$default$4.invoke",
		"com.app.MainActivity.onCreate$lambda$0$0",
		"com.app.Canvas.draw",
	}
	callers := [...]string{
		"jankhunter.semantic.v1.compose.composition.main",
		"jankhunter.semantic.v1.compose.draw.main",
		"jankhunter.semantic.v1.room.dao.background",
		"jankhunter.semantic.v1.worker.success.background",
	}
	for iteration := 0; iteration < 300; iteration++ {
		calls := make([]RuntimeCallStats, random.Intn(80))
		for index := range calls {
			calls[index] = RuntimeCallStats{
				Caller: callers[random.Intn(len(callers))],
				Callee: owners[random.Intn(len(owners))],
				Screen: fmt.Sprintf("Screen%d", random.Intn(3)),
				Operation: func() string {
					if random.Intn(4) == 0 {
						return ""
					}
					return fmt.Sprintf("operation.%d", random.Intn(4))
				}(),
				Count: uint64(random.Intn(8) + 1), TotalMS: uint64(random.Intn(240)), MaxMS: uint64(random.Intn(50)),
			}
		}
		summary := Summary{RuntimeCalls: calls}
		got := ActionableSemanticWork(summary)
		want := actionableSemanticWorkQuadraticReference(summary)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("optimized semantic work differs at iteration %d:\n got %+v\nwant %+v", iteration, got, want)
		}
	}
}

func actionableSemanticWorkQuadraticReference(summary Summary) []SemanticWorkStats {
	items := SemanticWork(summary)
	result := make([]SemanticWorkStats, 0, len(items))
	for _, item := range items {
		duplicate := -1
		for index := range result {
			if composeSemanticDuplicate(result[index], item) {
				duplicate = index
				break
			}
		}
		if duplicate < 0 {
			result = append(result, item)
			continue
		}
		if composeGeneratedOwner(result[duplicate].Owner) && !composeGeneratedOwner(item.Owner) {
			result[duplicate] = item
		}
	}
	result = collapseActionableComposeOwners(result)
	filtered := make([]SemanticWorkStats, 0, len(result))
	for index, item := range result {
		if item.Domain == SemanticDomainCompose && !hasSemanticContext(item) {
			shadowed := false
			for candidateIndex, candidate := range result {
				if candidateIndex == index || !hasSemanticContext(candidate) ||
					semanticTargetKeyFor(item) != semanticTargetKeyFor(candidate) {
					continue
				}
				if candidate.Count >= item.Count && candidate.TotalMS >= item.TotalMS && candidate.MaxMS >= item.MaxMS {
					shadowed = true
					break
				}
			}
			if shadowed {
				continue
			}
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func TestActionableSemanticWorkHumanizesGeneratedComposeOwner(t *testing.T) {
	actionable := ActionableSemanticWork(Summary{RuntimeCalls: []RuntimeCallStats{{
		Caller: "jankhunter.semantic.v1.compose.composition.main",
		Callee: "com.app.MainActivity.onCreate$lambda$0$0",
		Count:  4, TotalMS: 120, MaxMS: 35,
	}}})
	if len(actionable) != 1 || actionable[0].Owner != "com.app.MainActivity.onCreate" {
		t.Fatalf("generated Compose owner was not humanized: %+v", actionable)
	}
	if got := actionableComposeOwner("com.app.FeedKt$Feed$lambda$2$$inlined$items$default$4.invoke"); got != "com.app.FeedKt.Feed" {
		t.Fatalf("inlined Compose owner = %q", got)
	}
}

func TestActionableSemanticWorkCollapsesNestedGeneratedLambdasAfterHumanizing(t *testing.T) {
	summary := Summary{RuntimeCalls: []RuntimeCallStats{{
		Caller: "jankhunter.semantic.v1.compose.composition.main",
		Callee: "com.app.MainActivity.onCreate$lambda$0",
		Screen: "Main", Count: 4, TotalMS: 108, MaxMS: 46,
	}, {
		Caller: "jankhunter.semantic.v1.compose.composition.main",
		Callee: "com.app.MainActivity.onCreate$lambda$0$0",
		Screen: "Main", Count: 4, TotalMS: 81, MaxMS: 33,
	}}}

	actionable := ActionableSemanticWork(summary)
	if len(actionable) != 1 {
		t.Fatalf("nested generated owners were not collapsed: %+v", actionable)
	}
	if actionable[0].Owner != "com.app.MainActivity.onCreate" ||
		actionable[0].Count != 4 || actionable[0].TotalMS != 108 || actionable[0].MaxMS != 46 {
		t.Fatalf("collapsed owner lost the strongest observation: %+v", actionable[0])
	}

	report, err := BuildProblemReport(Summary{
		DurationMS:   60_000,
		Screens:      []ScreenStats{{Screen: "Main", Frames: 180, FrameDeadlineUS: 16_667}},
		RuntimeCalls: summary.RuntimeCalls,
	})
	if err != nil {
		t.Fatalf("nested generated owners invalidated the complete problem report: %v", err)
	}
	composeFindings := 0
	for _, finding := range report.Problems {
		if finding.DetectorID == "ui.compose_work" {
			composeFindings++
		}
	}
	if composeFindings != 1 {
		t.Fatalf("nested generated owners produced %d Compose findings: %+v", composeFindings, report.Problems)
	}
}

func TestActionableSemanticWorkDropsWeakerContextlessComposeObservation(t *testing.T) {
	summary := Summary{RuntimeCalls: []RuntimeCallStats{{
		Caller: "jankhunter.semantic.v1.compose.measure.main",
		Callee: "com.app.ExpensiveLayout", Screen: "ComposeLab",
		Count: 1, TotalMS: 42, MaxMS: 42,
	}, {
		Caller: "jankhunter.semantic.v1.compose.measure.main",
		Callee: "com.app.ExpensiveLayout", Screen: "ComposeLab", Operation: "measure_lab.repeat_measure",
		Count: 20, TotalMS: 840, MaxMS: 42,
	}}}

	if raw := SemanticWork(summary); len(raw) != 2 {
		t.Fatalf("raw contextless evidence was unexpectedly removed: %+v", raw)
	}
	actionable := ActionableSemanticWork(summary)
	if len(actionable) != 1 || actionable[0].ContextOperation != "measure_lab.repeat_measure" || actionable[0].Count != 20 {
		t.Fatalf("weaker contextless Compose observation was not shadowed: %+v", actionable)
	}
}

func TestActionableSemanticWorkKeepsStrongerContextlessComposeObservation(t *testing.T) {
	actionable := ActionableSemanticWork(Summary{RuntimeCalls: []RuntimeCallStats{{
		Caller: "jankhunter.semantic.v1.compose.draw.main",
		Callee: "com.app.Canvas", Screen: "ComposeLab", Count: 1, TotalMS: 90, MaxMS: 90,
	}, {
		Caller: "jankhunter.semantic.v1.compose.draw.main",
		Callee: "com.app.Canvas", Screen: "ComposeLab", Operation: "draw_lab.repeat_draw",
		Count: 10, TotalMS: 480, MaxMS: 48,
	}}})
	if len(actionable) != 2 {
		t.Fatalf("stronger contextless evidence was hidden: %+v", actionable)
	}
}

func TestSemanticWorkUsesRussianCountForms(t *testing.T) {
	for value, want := range map[uint64]string{
		1: "1 выполнение", 2: "2 выполнения", 5: "5 выполнений", 11: "11 выполнений", 21: "21 выполнение",
	} {
		if got := russianCountUint64(value, "выполнение", "выполнения", "выполнений"); got != want {
			t.Fatalf("russianCountUint64(%d) = %q, want %q", value, got, want)
		}
	}
}

func BenchmarkActionableSemanticWorkHighCardinality(b *testing.B) {
	const boundaries = 2_048
	calls := make([]RuntimeCallStats, boundaries)
	for index := range calls {
		calls[index] = RuntimeCallStats{
			Caller: "jankhunter.semantic.v1.compose.composition.main",
			Callee: fmt.Sprintf("com.app.feature%04d.Component", index),
			Screen: "BenchmarkScreen", Operation: fmt.Sprintf("benchmark.operation.%04d", index),
			Count: uint64(index%500 + 1), TotalMS: uint64(index%2_000 + 1), MaxMS: uint64(index%40 + 1),
		}
	}
	summary := Summary{RuntimeCalls: calls}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		benchmarkSemanticWork = ActionableSemanticWork(summary)
	}
}

func TestSemanticWorkBuildsHumanReadableProblems(t *testing.T) {
	summary := Summary{
		DurationMS: 60_000,
		Screens: []ScreenStats{{
			Screen: "ComposeFeed", Frames: 180, JankyFrames: 24, JankRatePct: 13.3,
			FrameDeadlineUS: 16_667, FrameDeadlineStatus: "consistent", FrameSource: "jankstats",
		}},
		RuntimeCalls: []RuntimeCallStats{{
			Screen: "ComposeFeed", Caller: "jankhunter.semantic.v1.compose.draw.main",
			Callee: "com.app.FeedCanvas.draw", Count: 150, TotalMS: 900, MaxMS: 42,
		}, {
			Screen: "ComposeFeed", Caller: "jankhunter.semantic.v1.room.dao.main",
			Callee: "com.app.FeedDao_Impl.load", Count: 2, TotalMS: 64, MaxMS: 38,
		}, {
			Caller: "jankhunter.semantic.v1.worker.retry.background",
			Callee: "com.app.SyncWorker.doWork", Count: 1, TotalMS: 4_000, MaxMS: 4_000,
		}},
	}

	report, err := BuildProblemReport(summary)
	if err != nil {
		t.Fatalf("BuildProblemReport() error = %v", err)
	}
	compose := findingByDetector(report.Problems, "ui.compose_work")
	if compose == nil || compose.Why.ClaimLevel != "correlated" || !strings.Contains(compose.WhatHappened, "Композиция") && !strings.Contains(compose.WhatHappened, "Отрисовка") {
		t.Fatalf("compose finding = %+v", compose)
	}
	room := findingByDetector(report.Problems, "io.room_main_thread")
	if room == nil || !strings.Contains(room.Title, "главный поток") {
		t.Fatalf("room finding = %+v", room)
	}
	worker := findingByDetector(report.Problems, "cpu.worker_execution")
	if worker == nil || !strings.Contains(worker.Title, "повтор") {
		t.Fatalf("worker finding = %+v", worker)
	}
}

func TestComposeWorkCorrelatesWithLongFrameTailWithoutSystemJankFlag(t *testing.T) {
	summary := Summary{
		DurationMS: 60_000,
		Screens: []ScreenStats{{
			Screen: "ComposeLab", Frames: 180, JankyFrames: 0, JankRatePct: 0,
			FrameP95MS: 80, FrameP99MS: 160, FrameDeadlineUS: 16_667,
		}},
		RuntimeCalls: []RuntimeCallStats{{
			Screen: "ComposeLab", Caller: "jankhunter.semantic.v1.compose.draw.main",
			Callee: "com.app.HeavyCanvas", Count: 30, TotalMS: 1_200, MaxMS: 48,
		}},
	}

	report, err := BuildProblemReport(summary)
	if err != nil {
		t.Fatal(err)
	}
	finding := findingByDetector(report.Problems, "ui.compose_work")
	if finding == nil || finding.Why.ClaimLevel != "correlated" {
		t.Fatalf("Compose work was not correlated with the measured long-frame tail: %+v", finding)
	}
}
