package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/mathanalysis"
)

func TestWriteReports(t *testing.T) {
	t.Setenv("JH_LANG", "en")
	summary := analyze.Summary{
		Title:       "sample.jhlog",
		LogCount:    1,
		EventCount:  27,
		HTTPCount:   3,
		HTTPFailed:  1,
		HTTPP95MS:   612,
		UIFrames:    1122,
		UIJank:      90,
		UIJankPct:   8.02,
		UIAvgFPS:    56.1,
		StallCount:  1,
		StallMaxMS:  1240,
		MemoryMaxKB: 188240,
		Environment: analyze.RunEnvironment{
			Title:    "Pixel 8",
			Subtitle: "Android 15 · 0.1.0-debug (100) · process main",
			Items: []analyze.InfoItem{
				{Label: "Battery", Value: "82%", Detail: "charging · 32.0 C"},
				{Label: "Network", Value: "wifi", Detail: "validated yes · metered no · VPN no"},
			},
		},
		Routes: []analyze.RouteStats{
			{Route: "GET /feed", Count: 2, Failures: 0, P95MS: 612, MaxMS: 612, OwnerSample: "FeedRepository.refresh"},
		},
		Screens: []analyze.ScreenStats{
			{Screen: "Feed", Frames: 1122, JankyFrames: 90, JankRatePct: 8.02, AvgFPS: 56.1, P95MS: 24},
		},
		Owners: []analyze.OwnerStats{
			{Owner: "FeedRepository.refresh", Kind: "http", Count: 2, MaxMS: 612},
		},
	}

	dir := t.TempDir()
	inspectPath := filepath.Join(dir, "inspect.html")
	if err := WriteInspect(inspectPath, summary); err != nil {
		t.Fatalf("WriteInspect() error = %v", err)
	}
	assertHTMLContains(t, inspectPath, "Runtime Signal Report", "Device Context", "Pixel 8", "Network Routes", "Heuristic Verdict", "GET /feed", "λ Анализ", `href="inspect-math.html"`)

	mathInspectPath := filepath.Join(dir, "inspect-math.html")
	if err := WriteMathInspect(mathInspectPath, sampleMathReport(summary)); err != nil {
		t.Fatalf("WriteMathInspect() error = %v", err)
	}
	assertHTMLContains(t, mathInspectPath, "Математический анализ", "Качество данных", "Сетевые циклы", "Детали раздела")

	comparePath := filepath.Join(dir, "compare.html")
	comparison := analyze.Compare(summary, summary)
	if err := WriteCompareReport(
		comparePath,
		comparison,
		[]LogReport{{Name: "old/sample.jhlog", Anchor: "baseline-log-1", Summary: summary}},
		[]LogReport{{Name: "new/sample.jhlog", Anchor: "candidate-log-1", Summary: summary}},
	); err != nil {
		t.Fatalf("WriteCompareReport() error = %v", err)
	}
	assertHTMLContains(t, comparePath, "Regression Control Deck", "Candidate Device Context", "Per-log Drill-down", "Heuristic Verdict", "old/sample.jhlog", "new/sample.jhlog", "λ Анализ", `href="compare-math.html"`)

	mathComparePath := filepath.Join(dir, "compare-math.html")
	if err := WriteMathCompare(mathComparePath, sampleCompareMathReport(comparison, summary)); err != nil {
		t.Fatalf("WriteMathCompare() error = %v", err)
	}
	assertHTMLContains(t, mathComparePath, "Математический анализ сравнения", "Качество сравнения", "Сетевые циклы")
}

func TestWriteReportsRussian(t *testing.T) {
	t.Setenv("JH_LANG", "ru")
	summary := analyze.Summary{
		Title:      "sample.jhlog",
		LogCount:   1,
		EventCount: 27,
		HTTPCount:  3,
		HTTPFailed: 1,
		HTTPP95MS:  612,
		UIFrames:   1122,
		UIJankPct:  8.02,
		UIAvgFPS:   56.1,
		Environment: analyze.RunEnvironment{
			Title:    "Pixel 8",
			Subtitle: "Android 15 · 0.1.0-debug (100) · process main",
			Items: []analyze.InfoItem{
				{Label: "Battery", Value: "82%", Detail: "charging · 32.0 C"},
			},
		},
		Routes: []analyze.RouteStats{
			{Route: "GET /feed", Count: 2, P95MS: 612},
		},
	}

	dir := t.TempDir()
	inspectPath := filepath.Join(dir, "inspect-ru.html")
	if err := WriteInspect(inspectPath, summary); err != nil {
		t.Fatalf("WriteInspect() error = %v", err)
	}
	assertHTMLContains(t, inspectPath, `<html lang="ru">`, "Отчет по runtime-сигналам", "Контекст устройства", "Батарея", "Сетевые маршруты", "Эвристический итог", "λ Анализ")

	comparePath := filepath.Join(dir, "compare-ru.html")
	if err := WriteCompareReport(
		comparePath,
		analyze.Compare(summary, summary),
		[]LogReport{{Name: "old/sample.jhlog", Anchor: "baseline-log-1", Summary: summary}},
		[]LogReport{{Name: "new/sample.jhlog", Anchor: "candidate-log-1", Summary: summary}},
	); err != nil {
		t.Fatalf("WriteCompareReport() error = %v", err)
	}
	assertHTMLContains(t, comparePath, "Панель контроля регрессий", "Детали по каждому логу", "Эвристический итог", "Baseline-логи", "λ Анализ")
}

func TestMathReportPath(t *testing.T) {
	tests := map[string]string{
		"report.html":               "report-math.html",
		"/tmp/report.html":          "/tmp/report-math.html",
		"/tmp/report":               "/tmp/report-math.html",
		"/tmp/report.with.dots.htm": "/tmp/report.with.dots-math.htm",
	}
	for input, want := range tests {
		if got := MathReportPath(input); got != want {
			t.Fatalf("MathReportPath(%q) = %q, want %q", input, got, want)
		}
	}
}

func sampleMathReport(summary analyze.Summary) mathanalysis.MathReport {
	return mathanalysis.MathReport{
		Title:       "sample.jhlog",
		SourcePaths: []string{"sample.jhlog"},
		Summary:     summary,
		Findings: []mathanalysis.Finding{{
			Severity: "ok",
			Title:    "Данных достаточно",
			Detail:   "Каркас математического отчета готов.",
		}},
		Sections: []mathanalysis.MathSection{
			{ID: "quality", Title: "Качество данных", Status: "ok", Summary: "Сводка качества данных."},
			{ID: "network-loops", Title: "Сетевые циклы", Status: "pending", Summary: "Каркас детектора сетевых циклов."},
		},
	}
}

func sampleCompareMathReport(comparison analyze.Comparison, summary analyze.Summary) mathanalysis.CompareMathReport {
	inspectMath := sampleMathReport(summary)
	return mathanalysis.CompareMathReport{
		Title:      "база против кандидата",
		Baseline:   inspectMath,
		Candidate:  inspectMath,
		Comparison: comparison,
		Findings: []mathanalysis.Finding{{
			Severity: "ok",
			Title:    "Сравнение готово",
			Detail:   "Каркас математического сравнения готов.",
		}},
		Sections: []mathanalysis.MathSection{
			{ID: "quality", Title: "Качество сравнения", Status: "ok", Summary: "Сводка качества сравнения."},
			{ID: "network-loops", Title: "Сетевые циклы", Status: "pending", Summary: "Каркас compare-детектора сетевых циклов."},
		},
	}
}

func assertHTMLContains(t *testing.T, path string, needles ...string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	html := string(data)
	if strings.Contains(html, "ZgotmplZ") {
		t.Fatalf("%s contains escaped unsafe template CSS", path)
	}
	for _, needle := range needles {
		if !strings.Contains(html, needle) {
			t.Fatalf("%s does not contain %q", path, needle)
		}
	}
}
