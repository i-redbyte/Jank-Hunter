package analyze

import (
	"strings"
	"testing"
)

func TestProblemIncidentsMergeSameScreenUISignalsAndKeepRawContext(t *testing.T) {
	findings := []ProblemFinding{
		{
			ID: "stall", Fingerprint: strings.Repeat("a", 64), DetectorID: "stability.main_thread_stall",
			Category: ProblemCategoryStability, Severity: "high", RiskScore: 70, Confidence: "low",
			Title: "Главный поток остановился", WhatHappened: "Пауза 1200 мс.",
			Where:             []ProblemLocation{{Screen: "Feed", Owner: "androidx.compose.runtime.SlotTable"}},
			Why:               ProblemWhy{ClaimLevel: "unknown"},
			ConfidenceReasons: []string{"Слишком мало наблюдений."},
			RankBreakdown:     risk(30, 20, 10, 5, 5, "", "", "", "", ""),
		},
		{
			ID: "compose", Fingerprint: strings.Repeat("b", 64), DetectorID: "ui.compose_work",
			Category: ProblemCategoryUI, Severity: "high", RiskScore: 61, Confidence: "high",
			Title: "FeedRow выполняется слишком долго", WhatHappened: "Максимум 48 мс.",
			Where:             []ProblemLocation{{Screen: "Feed", Flow: "scroll", Owner: "com.example.feed.FeedRow"}},
			Why:               ProblemWhy{ClaimLevel: "linked"},
			ConfidenceReasons: []string{"Причина подтверждена прямой связью."},
			Recommendations:   []ProblemRecommendation{{Action: "Проверить FeedRow"}},
			RankBreakdown:     risk(28, 15, 10, 4, 4, "", "", "", "", ""),
		},
		{
			ID: "jank", Fingerprint: strings.Repeat("c", 64), DetectorID: "ui.jank_tail",
			Category: ProblemCategoryUI, Severity: "medium", RiskScore: 55, Confidence: "high",
			Title: "Экран дёргается", WhatHappened: "12% медленных кадров.",
			Where: []ProblemLocation{{Screen: "Feed"}}, Why: ProblemWhy{ClaimLevel: "correlated"},
			RankBreakdown: risk(28, 12, 10, 2, 3, "", "", "", "", ""),
		},
		{
			ID: "network", Fingerprint: strings.Repeat("d", 64), DetectorID: "network.route_health",
			Category: ProblemCategoryNetwork, Severity: "medium", RiskScore: 50, Confidence: "medium",
			Title: "GET /feed медленный", Where: []ProblemLocation{{Screen: "Feed", Route: "GET /feed"}},
			Why:           ProblemWhy{ClaimLevel: "unknown"},
			RankBreakdown: risk(20, 15, 10, 5, 0, "", "", "", "", ""),
		},
	}

	incidents := buildProblemIncidents(findings)
	if len(incidents) != 2 {
		t.Fatalf("incidents = %d, want 2: %+v", len(incidents), incidents)
	}
	ui := incidents[0]
	if len(ui.RelatedFindings) != 3 || len(ui.RelatedCategories) != 2 {
		t.Fatalf("UI incident lost related signals: %+v", ui)
	}
	if ui.RiskScore != 70 || ui.Severity != "high" || ui.Confidence != "high" {
		t.Fatalf("UI incident priority/confidence = %+v", ui)
	}
	if len(ui.ConfidenceReasons) != 1 || strings.Contains(ui.ConfidenceReasons[0], "мало наблюдений") {
		t.Fatalf("incident confidence explanation contradicts the selected confidence: %+v", ui.ConfidenceReasons)
	}
	if ui.DetectorID != "ui.compose_work" || !strings.Contains(ui.Recommendations[0].Action, "FeedRow") {
		t.Fatalf("framework owner was preferred over application code: %+v", ui)
	}
	if !strings.Contains(ui.Title, "Feed") || len(ui.Where) != 1 || ui.Where[0].Owner != "com.example.feed.FeedRow" {
		t.Fatalf("UI incident context was not merged: %+v", ui)
	}
	if incidents[1].DetectorID != "network.route_health" {
		t.Fatalf("unrelated network finding was merged into UI incident: %+v", incidents[1])
	}
}

func TestApplicationSymbolRejectsFrameworkAndDiagnosticOwners(t *testing.T) {
	for _, value := range []string{
		"android.app.ActivityThread",
		"androidx.compose.runtime.SlotTable",
		"Handler (android.os.Handler)",
		"com.google.android.material.button.MaterialButton",
		"leakcanary.internal.RequestPermissionActivity",
	} {
		if isApplicationSymbol(value) {
			t.Fatalf("framework symbol %q was classified as application code", value)
		}
	}
	if !isApplicationSymbol("com.example.feed.FeedView.onDraw") {
		t.Fatal("application owner was not recognized")
	}
	if !isApplicationSymbol("com.google.mycompany.feed.FeedView.onDraw") {
		t.Fatal("application code under a com.google namespace was hidden")
	}
}

func TestUIIncidentFingerprintStaysStableWhenRelatedSignalsChange(t *testing.T) {
	ui := ProblemFinding{
		ID: "ui", Fingerprint: strings.Repeat("a", 64), DetectorID: "ui.jank_tail",
		Category: ProblemCategoryUI, Severity: "medium", RiskScore: 50, Confidence: "high",
		Where: []ProblemLocation{{Screen: "Feed"}}, Why: ProblemWhy{ClaimLevel: "correlated"},
		RankBreakdown: risk(20, 10, 10, 5, 5, "", "", "", "", ""),
	}
	stall := ProblemFinding{
		ID: "stall", Fingerprint: strings.Repeat("b", 64), DetectorID: "stability.main_thread_stall",
		Category: ProblemCategoryStability, Severity: "high", RiskScore: 60, Confidence: "medium",
		Where:         []ProblemLocation{{Screen: "Feed", Owner: "com.example.FeedLoader.load"}},
		Why:           ProblemWhy{ClaimLevel: "linked"},
		RankBreakdown: risk(30, 15, 8, 4, 3, "", "", "", "", ""),
	}

	before := buildProblemIncidents([]ProblemFinding{ui})
	after := buildProblemIncidents([]ProblemFinding{ui, stall})
	if len(before) != 1 || len(after) != 1 || before[0].Fingerprint != after[0].Fingerprint {
		t.Fatalf("UI incident identity changed with related evidence: before=%+v after=%+v", before, after)
	}
}
