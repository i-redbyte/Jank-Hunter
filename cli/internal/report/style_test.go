package report

import (
	"strings"
	"testing"
)

func TestReportStylesheetContainsCurrentTheme(t *testing.T) {
	stylesheet := reportStylesheet(true)
	for _, marker := range []string{
		"--bg: #111512",
		`font-family: "SF Pro Text"`,
		"padding: 8px !important",
		".table-cell-clip",
		".data-ticket",
		"--data-ticket-label",
		"Как получена оценка",
	} {
		if !strings.Contains(stylesheet, marker) {
			t.Fatalf("stylesheet does not contain %q", marker)
		}
	}
	if !strings.Contains(stylesheet, compactStylesheet(mathCSS)) {
		t.Fatal("stylesheet is missing math CSS")
	}
}

func TestCompactStylesheetPreservesStringsAndSelectorBoundaries(t *testing.T) {
	stylesheet := compactStylesheet("  .first,\n  .second {\n    content: \"two words\";\n    color: red;\n  }\n")
	want := `.first,.second {content: "two words";color: red;}`
	if stylesheet != want {
		t.Fatalf("compactStylesheet() = %q, want %q", stylesheet, want)
	}
}

func assertCurrentReportStyle(t *testing.T, path string) {
	t.Helper()
	assertHTMLContains(
		t,
		path,
		"--bg: #111512",
		`font-family: "SF Pro Text"`,
		"padding: 8px !important",
	)
	assertHTMLNotContains(t, path, "data-report-style", "modernReport")
}
