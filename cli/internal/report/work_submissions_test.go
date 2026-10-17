package report

import (
	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"path/filepath"
	"strings"
	"testing"
)

func TestSubmissionOnlyReportKeepsCallAndAcceptanceSeparate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "submission.html")
	if err := WriteInspectWithOptions(path, analyze.Summary{WorkSubmissions: &analyze.WorkSubmissionAnalysis{CapturedCalls: 2}}, ReportOptions{}); err != nil {
		t.Fatal(err)
	}
	assertHTMLContains(t, path, "data-work-submissions", "Параметры вызовов постановки", "Возврат Operation не подтверждает регистрацию")
	if !strings.Contains(workSubmissionSummary(nil), "неизвестно") {
		t.Fatal("absent submission source became zero")
	}
}

func TestSubmissionComparisonKeepsAbsentSourceUnknown(t *testing.T) {
	comparison := analyze.Compare(analyze.Summary{}, analyze.Summary{WorkSubmissions: &analyze.WorkSubmissionAnalysis{CapturedCalls: 14, UniqueCalls: 3, PeriodicCalls: 1}})
	path := filepath.Join(t.TempDir(), "compare.html")
	if err := WriteCompareReportWithOptions(path, comparison, nil, nil, ReportOptions{}); err != nil {
		t.Fatal(err)
	}
	assertHTMLContains(t, path, "неизвестно: нет наблюдений вызова", "Захвачено запросов в вызовах: 14", "не подтверждённые изменения расписаний")
}
