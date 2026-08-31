package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
)

func TestInspectExplainsInvestigationPriorityAndNeverCallsItProbability(t *testing.T) {
	path := filepath.Join(t.TempDir(), "priority.html")
	summary := investigationPrioritySummary()
	if err := WriteInspectWithOptions(path, summary, ReportOptions{}); err != nil {
		t.Fatal(err)
	}
	assertInvestigationPriorityExplanation(t, path)
}

func TestCompareExplainsInvestigationPriorityAndNeverCallsItProbability(t *testing.T) {
	path := filepath.Join(t.TempDir(), "priority-compare.html")
	summary := investigationPrioritySummary()
	if err := WriteCompareReportWithOptions(path, analyze.Compare(summary, summary), nil, nil, ReportOptions{}); err != nil {
		t.Fatal(err)
	}
	assertInvestigationPriorityExplanation(t, path)
}

func investigationPrioritySummary() analyze.Summary {
	finding := analyze.ProblemFinding{
		ID: "problem-1", Category: analyze.ProblemCategoryUI, Severity: "medium", Status: "observed",
		Confidence: "high", Title: "Example", WhatHappened: "Observed", Why: analyze.ProblemWhy{ClaimLevel: "linked", Summary: "Direct"},
		InvestigationPriority: 38,
		PriorityBreakdown: []analyze.ProblemPriorityComponent{
			{Component: "impact", Score: 20, Maximum: 40, Explanation: "user impact"},
			{Component: "magnitude", Score: 8, Maximum: 25, Explanation: "magnitude"},
			{Component: "exposure", Score: 5, Maximum: 20, Explanation: "exposure"},
			{Component: "breadth", Score: 3, Maximum: 10, Explanation: "breadth"},
			{Component: "compounding", Score: 2, Maximum: 5, Explanation: "compound"},
		},
		Recommendations: []analyze.ProblemRecommendation{{Action: "Inspect", Verification: "Repeat"}},
	}
	return analyze.Summary{
		Title: "priority.jhlog", ProblemSchemaVersion: analyze.ProblemSchemaVersion,
		ProblemSummary: analyze.ProblemSummary{Verdict: "problems_found", Total: 1, Medium: 1, Headline: "One"},
		Problems:       []analyze.ProblemFinding{finding}, CollectionQuality: sampleCollectionQuality(),
	}
}

func assertInvestigationPriorityExplanation(t *testing.T, path string) {
	t.Helper()
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	html := string(payload)
	for _, expected := range []string{
		"Индекс приоритета расследования", "38", "20 / 40", "8 / 25", "5 / 20", "3 / 10", "2 / 5",
		"не вероятность", "не ожидаемый ущерб", "Достоверность и уровень связи учитываются отдельно",
	} {
		if !strings.Contains(html, expected) {
			t.Fatalf("priority explanation misses %q", expected)
		}
	}
	if strings.Contains(html, "оценка риска 38") || strings.Contains(html, "вероятность 38") {
		t.Fatalf("priority is presented as risk/probability: %s", html)
	}
}
