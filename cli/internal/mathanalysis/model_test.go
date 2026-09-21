package mathanalysis

import (
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
)

func TestDataQualityFindingsIncludeSummaryWarnings(t *testing.T) {
	findings := dataQualityFindingsForRuns(analyze.Summary{
		LogCount:     1,
		EventCount:   100,
		HTTPCount:    10,
		UIFrames:     600,
		ContextCount: 5,
		Warnings:     []string{"ignored partial trailing compact event"},
	}, 1)

	if sectionStatus(findings) != "medium" {
		t.Fatalf("sectionStatus() = %q, want medium", sectionStatus(findings))
	}
	if !findingDetailsContain(findings, "ignored partial trailing compact event") {
		t.Fatalf("summary warning was not surfaced in findings: %+v", findings)
	}
}

func TestDataQualityFindingsHideInternalBufferCounters(t *testing.T) {
	findings := dataQualityFindingsForRuns(analyze.Summary{
		LogCount:     1,
		EventCount:   100,
		HTTPCount:    10,
		UIFrames:     600,
		ContextCount: 5,
		Warnings: []string{
			"Качество сбора: ограниченные runtime-реестры потеряли 25661 элементов evidence.",
			"Качество сбора: writer отклонил batch runtime-графа: 25661.",
			"Анализ компонентов Android и IPC частичный: записан 1 из 3 ожидаемых процессов.",
		},
	}, 1)

	if findingDetailsContain(findings, "25661") || findingDetailsContain(findings, "runtime-реестры") {
		t.Fatalf("internal counters leaked into user findings: %+v", findings)
	}
	if !findingDetailsContain(findings, "1 из 3 ожидаемых процессов") {
		t.Fatalf("actionable coverage warning was removed: %+v", findings)
	}
}

func TestCompareFindingsIncludeBaselineAndCandidateWarnings(t *testing.T) {
	findings := compareFindings(analyze.Comparison{
		Baseline: analyze.Summary{
			Warnings: []string{"ignored partial trailing baseline event"},
		},
		Candidate: analyze.Summary{
			Warnings: []string{"candidate filter removed global signals"},
		},
	})

	if sectionStatus(findings) != "medium" {
		t.Fatalf("sectionStatus() = %q, want medium", sectionStatus(findings))
	}
	for _, want := range []string{
		"Базовый прогон: ignored partial trailing baseline event",
		"Проверяемый прогон: candidate filter removed global signals",
	} {
		if !findingDetailsContain(findings, want) {
			t.Fatalf("warning %q was not surfaced in findings: %+v", want, findings)
		}
	}
}

func TestComparisonStatusPreservesEmptyPolicyAndWorstSeverity(t *testing.T) {
	if got := comparisonStatus([]RobustDelta(nil), "medium"); got != "medium" {
		t.Fatalf("empty robust status = %q, want medium", got)
	}
	if got := comparisonStatus([]CausalDelta(nil), "ok"); got != "ok" {
		t.Fatalf("empty causal status = %q, want ok", got)
	}

	tests := []struct {
		name   string
		deltas []RobustDelta
		want   string
	}{
		{name: "all ok", deltas: []RobustDelta{{Severity: "ok"}}, want: "ok"},
		{name: "medium", deltas: []RobustDelta{{Severity: "ok"}, {Severity: "medium"}}, want: "medium"},
		{name: "high takes precedence", deltas: []RobustDelta{{Severity: "medium"}, {Severity: "high"}}, want: "high"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := comparisonStatus(test.deltas, "medium"); got != test.want {
				t.Fatalf("comparisonStatus() = %q, want %q", got, test.want)
			}
		})
	}
}

func findingDetailsContain(findings []Finding, want string) bool {
	for _, finding := range findings {
		if strings.Contains(finding.Detail, want) {
			return true
		}
	}
	return false
}
