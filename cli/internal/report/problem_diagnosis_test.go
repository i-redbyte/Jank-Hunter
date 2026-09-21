package report

import (
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
)

func TestProblemDiagnosisDoesNotInventMainThreadPauseForNetworkCandidate(t *testing.T) {
	finding := analyze.ProblemFinding{
		DetectorID: "ui.jank_tail", Category: analyze.ProblemCategoryUI,
		WhatHappened: "На экране медленными были 20% кадров.",
		Where:        []analyze.ProblemLocation{{Screen: "FeedActivity"}},
		Why:          analyze.ProblemWhy{ClaimLevel: "unknown", Summary: "Подтормаживания измерены на экране."},
	}
	diagnosis := problemDiagnosis(analyze.Summary{
		Screens: []analyze.ScreenStats{{
			Screen: "FeedActivity", Frames: 200, JankyFrames: 40, JankRatePct: 20,
		}},
		SignalContexts: []analyze.SignalContextStats{{
			Screen: "FeedActivity", Operation: "feed.load", Owner: "FeedPresenter", RouteSample: "GET /feed",
			HTTPCount: 2, HTTPP95MS: 1_500,
		}},
	}, finding)
	if len(diagnosis.Causes) != 1 || len(diagnosis.CausalChain) == 0 {
		t.Fatalf("network diagnosis is incomplete: %+v", diagnosis)
	}
	for _, step := range diagnosis.CausalChain {
		text := strings.ToLower(step.Text)
		if strings.Contains(text, "главный поток действительно") || strings.Contains(text, "измеренной паузы") {
			t.Fatalf("network correlation invented a main-thread stall: %+v", diagnosis.CausalChain)
		}
	}
}

func TestProblemDiagnosisHidesInternalCollectionCounters(t *testing.T) {
	finding := analyze.ProblemFinding{
		DetectorID: "io.database_repeated_in_scope",
		Category:   analyze.ProblemCategoryIO,
		Why: analyze.ProblemWhy{
			ClaimLevel: "hypothesis",
			Summary:    "Повторы подтверждены, одинаковые параметры не доказаны.",
		},
		Limitations: []string{
			"ограниченные runtime-реестры потеряли 25661 элементов evidence",
			"writer отклонил batch runtime-графа: 16",
			"Значения параметров SQL намеренно не записываются.",
		},
	}

	diagnosis := problemDiagnosis(analyze.Summary{}, finding)
	joined := strings.Join(diagnosis.MissingProof, " ")
	if strings.Contains(joined, "25661") || strings.Contains(joined, "writer") || strings.Contains(joined, "evidence") {
		t.Fatalf("internal counters leaked into investigation plan: %+v", diagnosis.MissingProof)
	}
	if !strings.Contains(joined, "Значения параметров SQL") {
		t.Fatalf("actionable causal limitation was removed: %+v", diagnosis.MissingProof)
	}
	for _, value := range diagnosis.TechnicalLimitations {
		if strings.Contains(value, "25661") || strings.Contains(value, "writer") {
			t.Fatalf("internal counters leaked into technical details: %+v", diagnosis.TechnicalLimitations)
		}
	}
}
