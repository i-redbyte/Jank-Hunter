package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
)

func TestComparisonOverviewUsesScopeOutcomeAndSafeProblemTransitions(t *testing.T) {
	finding := analyze.ProblemFinding{Fingerprint: "chat", Title: "Открытие чата"}
	comparison := analyze.Comparison{
		Outcome: analyze.ChangeMixed,
		Scope: analyze.ComparisonScope{
			Comparability: analyze.ScenarioPartial,
			Baseline:      analyze.ScenarioCoverage{Total: 10, Matched: 6, TotalKnown: true},
			Candidate:     analyze.ScenarioCoverage{Total: 8, Matched: 6, TotalKnown: true},
			Changes: []analyze.MetricChange{
				{Name: "Latency", Change: analyze.ChangeImproved, Eligibility: analyze.MetricEligibility{State: analyze.EligibilityEligible}},
				{Name: "Errors", Change: analyze.ChangeRegressed, Eligibility: analyze.MetricEligibility{State: analyze.EligibilityEligible}},
				{Name: "Jank", Change: analyze.ChangeUnchanged, Eligibility: analyze.MetricEligibility{State: analyze.EligibilityEligible}},
				{Name: "Memory", Change: analyze.ChangeNotComparable, Eligibility: analyze.MetricEligibility{State: analyze.EligibilityIneligible, Reason: analyze.ReasonCollectorDisabled}},
			},
		},
		ProblemComparison: analyze.ProblemComparison{Deltas: []analyze.ProblemDelta{{
			Fingerprint: "chat", Baseline: &finding, Observation: analyze.ObservationNotObservedAfter,
			Change: analyze.ChangeInsufficientData, Confidence: analyze.ConfidenceLimited,
		}}},
	}

	view := comparisonOverview(comparison)
	if view.Headline != "Результат смешанный" || !strings.Contains(strings.ToLower(view.ScopeLine), "сопоставлена часть") {
		t.Fatalf("overview = %+v", view)
	}
	if view.Balance.Improved != 1 || view.Balance.Regressed != 1 || view.Balance.Unchanged != 1 || view.Balance.Excluded != 2 || view.Balance.NotObservedAfter != 1 {
		t.Fatalf("balance = %+v", view.Balance)
	}
	if strings.Contains(strings.ToLower(view.Excluded[1].Detail), "исправ") {
		t.Fatalf("absence was presented as a fix: %+v", view.Excluded[1])
	}
}

func TestComparisonOverviewExplainsNoCommonScenario(t *testing.T) {
	view := comparisonOverview(analyze.Comparison{
		Outcome: analyze.ChangeNotComparable,
		Scope:   analyze.ComparisonScope{Comparability: analyze.ScenarioNone},
	})
	if view.Headline != "В этих записях разные сценарии" || !strings.Contains(view.ScopeLine, "Запишите одинаковый сценарий") {
		t.Fatalf("overview = %+v", view)
	}
}

func TestComparisonOverviewExplainsCommonSubpathBoundary(t *testing.T) {
	line := comparisonScopeLine(analyze.ComparisonScope{
		Comparability:       analyze.ScenarioPartial,
		CommonSubpathGroups: 1,
		Baseline:            analyze.ScenarioCoverage{Total: 4, Matched: 2, TotalKnown: true},
		Candidate:           analyze.ScenarioCoverage{Total: 5, Matched: 3, TotalKnown: true},
	})
	if !strings.Contains(line, "общая последовательность действий") || !strings.Contains(line, "Метрики целых сценариев не сравниваются") {
		t.Fatalf("common-subpath boundary missing: %q", line)
	}
}

func TestComparisonOverviewPreservesVerifiedRareEventBound(t *testing.T) {
	item := comparisonProblemOverviewItem(analyze.ProblemDelta{
		Observation: analyze.ObservationNotObservedAfter,
		Change:      analyze.ChangeImproved,
		Note:        "Односторонняя 95% верхняя граница частоты ниже исходной.",
	})
	if !strings.Contains(item.Detail, "верхняя граница") || strings.Contains(item.Detail, "не подтверждает") {
		t.Fatalf("verified bound was replaced by generic absence text: %+v", item)
	}
}

func TestCompareFirstScreenUsesCalmOverviewAndNativeDisclosure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "compare.html")
	comparison := analyze.Comparison{
		SchemaVersion:  analyze.ComparisonSchemaVersion,
		Outcome:        analyze.ChangeNotComparable,
		Scope:          analyze.ComparisonScope{Comparability: analyze.ScenarioNone},
		CohortWarnings: []string{"Устройства различаются"},
	}
	if err := WriteCompareReportWithOptions(path, comparison, nil, nil, ReportOptions{}); err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	html := string(payload)
	for _, expected := range []string{"В этих записях разные сценарии", "Запишите одинаковый сценарий", "Изменилось", "Без заметных изменений", "Не вошло в сравнение", "<details", "<summary"} {
		if !strings.Contains(html, expected) {
			t.Fatalf("missing %q", expected)
		}
	}
	if warning := strings.Index(html, "Устройства различаются"); warning >= 0 && warning < strings.Index(html, "Основания сравнения") {
		t.Fatal("comparison warning leaked onto the first screen")
	}
}

func TestComparisonOverviewStateMatrix(t *testing.T) {
	tests := []struct {
		name          string
		comparability analyze.ScenarioComparability
		outcome       analyze.ComparisonChange
		want          string
	}{
		{"full improved", analyze.ScenarioFull, analyze.ChangeImproved, "Стало лучше"},
		{"partial regressed", analyze.ScenarioPartial, analyze.ChangeRegressed, "В сопоставленной части стало хуже"},
		{"none", analyze.ScenarioNone, analyze.ChangeNotComparable, "В этих записях разные сценарии"},
		{"unknown", analyze.ScenarioUnknown, analyze.ChangeInsufficientData, "Пока недостаточно данных"},
		{"mixed", analyze.ScenarioFull, analyze.ChangeMixed, "Результат смешанный"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			comparison := analyze.Comparison{
				SchemaVersion: analyze.ComparisonSchemaVersion,
				Outcome:       test.outcome,
				Scope: analyze.ComparisonScope{
					Comparability: test.comparability,
					Outcome:       test.outcome,
					Baseline:      analyze.ScenarioCoverage{Total: 10, Matched: 8, TotalKnown: true, TotalDurationMS: 1000, MatchedDurationMS: 800, DurationKnown: true},
					Candidate:     analyze.ScenarioCoverage{Total: 12, Matched: 8, TotalKnown: true, TotalDurationMS: 1200, MatchedDurationMS: 700, DurationKnown: true},
				},
			}
			path := filepath.Join(t.TempDir(), "compare.html")
			if err := WriteCompareReportWithOptions(path, comparison, nil, nil, ReportOptions{}); err != nil {
				t.Fatal(err)
			}
			payload, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			html := string(payload)
			if !strings.Contains(html, test.want) || !strings.Contains(html, "по длительности") ||
				!strings.Contains(html, "<details") || !strings.Contains(html, "<summary") {
				t.Fatalf("state %s missing contract: %s", test.name, html)
			}
		})
	}
}
