package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
)

func TestInspectOverviewIsTheFirstMainSection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inspect.html")
	summary := analyze.Summary{
		Title:                "sample.jhlog",
		ProblemSchemaVersion: analyze.ProblemSchemaVersion,
		ProblemSummary: analyze.ProblemSummary{
			Total: 5,
			High:  2,
		},
	}
	if err := WriteInspectWithOptions(path, summary, ReportOptions{}); err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	html := string(payload)
	overview := strings.Index(html, `<section id="overview"`)
	problems := strings.Index(html, `<section id="problems"`)
	if overview < 0 || problems < 0 || overview > problems {
		t.Fatalf("overview must lead the report: overview=%d problems=%d", overview, problems)
	}
	if !strings.Contains(html[overview:problems], `aria-label="Основные показатели"`) {
		t.Fatal("leading overview does not identify the main metrics")
	}
}

func TestInspectProblemSummaryUsesSeparateFactsInReportHeader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inspect.html")
	summary := analyze.Summary{
		ProblemSchemaVersion: analyze.ProblemSchemaVersion,
		ProblemSummary: analyze.ProblemSummary{
			Total: 5,
			High:  2,
		},
	}
	if err := WriteInspectWithOptions(path, summary, ReportOptions{}); err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	html := string(payload)
	headerEnd := strings.Index(html, `</header>`)
	overview := strings.Index(html, `<section id="overview"`)
	if headerEnd < 0 || overview < 0 || headerEnd > overview {
		t.Fatalf("report header must precede overview: headerEnd=%d overview=%d", headerEnd, overview)
	}
	for _, metric := range []string{"problems", "signals", "priority"} {
		marker := `data-summary-metric="` + metric + `"`
		if !strings.Contains(html[:headerEnd], marker) {
			t.Fatalf("problem summary metric %q is not a separate header element", metric)
		}
		if strings.Contains(html[overview:], marker) {
			t.Fatalf("problem summary metric %q still consumes overview space", metric)
		}
	}
	if strings.Contains(html, "Найдено проблем: 5; связанных сигналов:") {
		t.Fatal("problem summary facts are still merged into one sentence")
	}
}

func TestInspectUsesHumanReadableDataSizes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inspect.html")
	summary := analyze.Summary{
		MemoryCount:  1,
		MemoryMaxKB:  933_057,
		ContextCount: 1,
		TrafficRxMax: 88_139_074,
		TrafficTxMax: 2_264_351,
	}
	if err := WriteInspectWithOptions(path, summary, ReportOptions{}); err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	html := string(payload)
	for _, want := range []string{"911.2 МБ", "84.1 МБ", "2.2 МБ"} {
		if !strings.Contains(html, want) {
			t.Fatalf("inspect report does not contain compact data size %q", want)
		}
	}
	for _, raw := range []string{"933057 КБ", "88139074 байт", "2264351 байт"} {
		if strings.Contains(html, raw) {
			t.Fatalf("inspect report still contains raw data size %q", raw)
		}
	}
}

func TestReportNavigationOwnsOneAnchorScroll(t *testing.T) {
	for _, marker := range []string{
		"event.preventDefault();",
		"history.pushState(null, '', link.hash);",
		"--report-anchor-offset:",
		":where(section[id], details[id])",
	} {
		if !strings.Contains(reportJS+modernCSS, marker) {
			t.Fatalf("stable anchor navigation is missing %q", marker)
		}
	}
}

func TestReportNavigationTracksWrappedStickyNavHeight(t *testing.T) {
	for _, marker := range []string{
		"const syncAnchorOffset = () =>",
		"anchorNav.getBoundingClientRect().height",
		"document.documentElement.style.setProperty('--report-anchor-offset'",
		"new ResizeObserver(syncAnchorOffset)",
	} {
		if !strings.Contains(reportJS, marker) {
			t.Fatalf("responsive anchor navigation is missing %q", marker)
		}
	}
	const competingBundleScroll = `if (href.charAt(0) === "#") {
      event.preventDefault();
      scrollToFragment(href);`
	if strings.Contains(bundledPageBridge, competingBundleScroll) {
		t.Fatal("bundle bridge still starts a second same-page scroll")
	}
	if !strings.Contains(bundledPageBridge, `if (href.charAt(0) === "#") return;`) {
		t.Fatal("bundle bridge does not leave same-page navigation to the embedded report")
	}
}

func TestProblemCardsKeepQualityCaveatsInTheMathAppendix(t *testing.T) {
	diagnosisStart := strings.Index(sharedComponentsTemplate, `{{- define "problem-diagnosis" -}}`)
	diagnosisEnd := strings.Index(sharedComponentsTemplate[diagnosisStart:], `{{- end -}}`)
	if diagnosisStart < 0 || diagnosisEnd < 0 {
		t.Fatal("problem diagnosis template is missing")
	}
	diagnosis := sharedComponentsTemplate[diagnosisStart : diagnosisStart+diagnosisEnd]
	for _, forbidden := range []string{
		"problem-analysis-boundary",
		"<h4>Чего не хватает</h4>",
		"<h4>Ограничения данных</h4>",
	} {
		if strings.Contains(inspectTemplate+diagnosis, forbidden) {
			t.Fatalf("problem scan layer still contains quality caveat %q", forbidden)
		}
	}
	for _, marker := range []string{
		"Ограничения данных по найденным проблемам",
		"problemQualityNotes .",
	} {
		if !strings.Contains(sharedComponentsTemplate, marker) {
			t.Fatalf("math quality appendix is missing %q", marker)
		}
	}
}

func TestUIDiagnosisUsesSummaryFirstProgressiveDisclosure(t *testing.T) {
	summary := strings.Index(inspectTemplate, `<p class="ui-diagnosis"><strong>Итог анализа:</strong>`)
	details := strings.Index(inspectTemplate, `<details class="ui-cause-details">`)
	if summary < 0 || details < 0 || summary > details {
		t.Fatalf("UI diagnosis must precede collapsed cause details: summary=%d details=%d", summary, details)
	}
	if !strings.Contains(inspectTemplate[details:], "Показать причины и план проверки") {
		t.Fatal("UI cause disclosure has no clear action label")
	}
}
