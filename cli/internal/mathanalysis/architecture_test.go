package mathanalysis

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

func TestMarkovAnalysisHasModelComparisonAndPresentationOwners(t *testing.T) {
	assertMathSourceLineLimit(t, "markov.go", 700)
	assertMathSourceLineLimit(t, "markov_compare.go", 400)
	assertMathSourceLineLimit(t, "markov_presentation.go", 260)
	assertMathFileOwnsFunction(t, "markov.go", "buildMarkovModel")
	assertMathFileOwnsFunction(t, "markov_compare.go", "compareMarkovModels")
	assertMathFileOwnsFunction(t, "markov_presentation.go", "markovFindings")
}

func TestNetworkLoopAnalysisHasCollectionDetectionComparisonAndLabelOwners(t *testing.T) {
	assertMathSourceLineLimit(t, "networkloop.go", 260)
	assertMathSourceLineLimit(t, "networkloop_detection.go", 300)
	assertMathSourceLineLimit(t, "networkloop_compare.go", 220)
	assertMathSourceLineLimit(t, "networkloop_labels.go", 400)
	assertMathFileOwnsFunction(t, "networkloop.go", "newNetworkLoopCollector")
	assertMathFileOwnsFunction(t, "networkloop_detection.go", "analyzeNetworkLoopSignal")
	assertMathFileOwnsFunction(t, "networkloop_compare.go", "compareNetworkLoops")
	assertMathFileOwnsFunction(t, "networkloop_labels.go", "classifyNetworkMetric")
}

func TestPureMathStagesDoNotDependOnLogParsingOrAggregation(t *testing.T) {
	paths := []string{
		"markov_compare.go",
		"markov_presentation.go",
		"networkloop_detection.go",
		"networkloop_compare.go",
		"networkloop_labels.go",
	}
	for _, path := range paths {
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, imported := range file.Imports {
			dependency := strings.Trim(imported.Path.Value, "\"")
			if strings.Contains(dependency, "/internal/analyze") ||
				strings.Contains(dependency, "/internal/jhlog") {
				t.Fatalf("%s pure stage imports %s", path, dependency)
			}
		}
	}
}

func assertMathFileOwnsFunction(t *testing.T, path, name string) {
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

func assertMathSourceLineLimit(t *testing.T, path string, limit int) {
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
