package report

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

func TestHTMLRendererStaysDecomposed(t *testing.T) {
	assertReportSourceLineLimit(t, "html.go", 900)
	assertReportSourceLineLimit(t, "problem_views.go", 900)
	owners := map[string]string{
		"template_funcs.go":          "reportTemplateFuncs",
		"influence_html.go":          "buildInfluenceHTMLData",
		"formatters.go":              "humanDuration",
		"problem_views.go":           "codeProblemCategoryStats",
		"problem_compare_details.go": "memoryLeakCompareRows",
		"comparison_views.go":        "routeCompareRows",
		"heuristics.go":              "inspectMathHeuristic",
		"graphs.go":                  "leakGraphSVG",
	}
	for path, function := range owners {
		assertReportFileOwnsFunction(t, path, function)
	}
}

func assertReportSourceLineLimit(t *testing.T, path string, limit int) {
	t.Helper()
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	lines := 1 + strings.Count(string(payload), "\n")
	if lines > limit {
		t.Fatalf("%s has %d lines, want <= %d", path, lines, limit)
	}
}

func assertReportFileOwnsFunction(t *testing.T, path, name string) {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && function.Name.Name == name {
			return
		}
	}
	t.Fatalf("%s does not own %s", path, name)
}
