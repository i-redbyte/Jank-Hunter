package mathanalysis

import (
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
)

func TestDataQualityFindingsIncludeSummaryWarnings(t *testing.T) {
	findings := dataQualityFindings(analyze.Summary{
		LogCount:     1,
		EventCount:   100,
		HTTPCount:    10,
		UIFrames:     600,
		ContextCount: 5,
		Warnings:     []string{"ignored partial trailing compact event"},
	})

	if sectionStatus(findings) != "medium" {
		t.Fatalf("sectionStatus() = %q, want medium", sectionStatus(findings))
	}
	if !findingDetailsContain(findings, "ignored partial trailing compact event") {
		t.Fatalf("summary warning was not surfaced in findings: %+v", findings)
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
		"База: ignored partial trailing baseline event",
		"Кандидат: candidate filter removed global signals",
	} {
		if !findingDetailsContain(findings, want) {
			t.Fatalf("warning %q was not surfaced in findings: %+v", want, findings)
		}
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
