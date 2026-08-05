package report

import (
	"strings"
	"testing"
)

func TestParseReportStyle(t *testing.T) {
	for _, test := range []struct {
		value string
		want  ReportStyle
	}{
		{value: "", want: ReportStyleModern},
		{value: "modern", want: ReportStyleModern},
		{value: " MODERN ", want: ReportStyleModern},
		{value: "legacy", want: ReportStyleLegacy},
	} {
		got, err := ParseReportStyle(test.value)
		if err != nil {
			t.Fatalf("ParseReportStyle(%q) error = %v", test.value, err)
		}
		if got != test.want {
			t.Fatalf("ParseReportStyle(%q) = %q, want %q", test.value, got, test.want)
		}
	}
	if _, err := ParseReportStyle("unknown"); err == nil {
		t.Fatal("ParseReportStyle(unknown) succeeded")
	}
}

func TestReportStylesheetKeepsLegacyAndUsesModernByDefault(t *testing.T) {
	modern := reportStylesheet("", true)
	legacy := reportStylesheet(ReportStyleLegacy, true)
	for _, marker := range []string{
		"--bg: #06140b",
		`font-family: "SF Pro Text"`,
		"padding: 8px !important",
		".table-cell-clip",
		".data-ticket",
		"--data-ticket-label",
		"Контекст расчёта",
	} {
		if !strings.Contains(modern, marker) {
			t.Fatalf("modern stylesheet does not contain %q", marker)
		}
	}
	if !strings.Contains(modern, compactStylesheet(mathCSS)) {
		t.Fatal("modern math stylesheet is missing math CSS")
	}
	if strings.Contains(legacy, "--bg: #06140b") {
		t.Fatal("legacy stylesheet contains modern theme")
	}
	if !strings.Contains(legacy, "--cyan: #6ff7ff") || !strings.Contains(legacy, compactStylesheet(mathCSS)) {
		t.Fatal("legacy stylesheet does not preserve previous CSS")
	}
}

func TestCompactStylesheetPreservesStringsAndSelectorBoundaries(t *testing.T) {
	stylesheet := compactStylesheet("  .first,\n  .second {\n    content: \"two words\";\n    color: red;\n  }\n")
	want := `.first,.second {content: "two words";color: red;}`
	if stylesheet != want {
		t.Fatalf("compactStylesheet() = %q, want %q", stylesheet, want)
	}
}

func assertModernReportStyle(t *testing.T, path string) {
	t.Helper()
	assertHTMLContains(
		t,
		path,
		`data-report-style="modern"`,
		"--bg: #06140b",
		`font-family: "SF Pro Text"`,
		"padding: 8px !important",
		"modernReport",
	)
}
