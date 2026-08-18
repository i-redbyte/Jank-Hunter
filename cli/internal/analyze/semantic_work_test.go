package analyze

import (
	"strings"
	"testing"
)

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
		Screen: "ComposeLab", Flow: "list", Step: "scroll",
		Caller: "jankhunter.semantic.v1.compose.composition.main",
		Callee: "com.app.FeedKt$Feed$lambda$2$$inlined$items$default$4.invoke",
		Count:  360, TotalMS: 2_196, MaxMS: 11,
	}, {
		Screen: "ComposeLab", Flow: "list", Step: "scroll",
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
		Callee: "com.app.ExpensiveLayout", Screen: "ComposeLab", Flow: "measure_lab", Step: "repeat_measure",
		Count: 20, TotalMS: 840, MaxMS: 42,
	}}}

	if raw := SemanticWork(summary); len(raw) != 2 {
		t.Fatalf("raw contextless evidence was unexpectedly removed: %+v", raw)
	}
	actionable := ActionableSemanticWork(summary)
	if len(actionable) != 1 || actionable[0].Flow != "measure_lab" || actionable[0].Count != 20 {
		t.Fatalf("weaker contextless Compose observation was not shadowed: %+v", actionable)
	}
}

func TestActionableSemanticWorkKeepsStrongerContextlessComposeObservation(t *testing.T) {
	actionable := ActionableSemanticWork(Summary{RuntimeCalls: []RuntimeCallStats{{
		Caller: "jankhunter.semantic.v1.compose.draw.main",
		Callee: "com.app.Canvas", Screen: "ComposeLab", Count: 1, TotalMS: 90, MaxMS: 90,
	}, {
		Caller: "jankhunter.semantic.v1.compose.draw.main",
		Callee: "com.app.Canvas", Screen: "ComposeLab", Flow: "draw_lab", Step: "repeat_draw",
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
