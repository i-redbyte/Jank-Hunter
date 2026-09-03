package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

func TestCommandEntrypointOwnsOnlyProcessBootstrap(t *testing.T) {
	payload, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Count(string(payload), "\n") + 1; lines > 120 {
		t.Fatalf("main.go has %d lines, want at most 120 bootstrap lines", lines)
	}

	owners := map[string][]string{
		"inspect_command.go":  {"runInspect", "selectLatestSessionLogs"},
		"compare_commands.go": {"runCompare", "runScorecard"},
		"analysis_options.go": {"takeAnalysisOptionsBuilder", "loadAndroidArtifactBundle"},
		"heap_inputs.go":      {"takeHeapInputFlags", "discoverHeapDumpsNearLogs"},
		"report_commands.go":  {"runExport", "runSize", "runProblems"},
		"input_flags.go":      {"takeStringFlag", "resolveLogArgs", "canonicalizeFileInputs"},
		"summary_output.go":   {"printSummary"},
	}
	fset := token.NewFileSet()
	for path, functions := range owners {
		file, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			t.Errorf("parse architecture owner %s: %v", path, parseErr)
			continue
		}
		declared := make(map[string]struct{}, len(functions))
		for _, declaration := range file.Decls {
			if function, ok := declaration.(*ast.FuncDecl); ok {
				declared[function.Name.Name] = struct{}{}
			}
		}
		for _, function := range functions {
			if _, ok := declared[function]; !ok {
				t.Errorf("%s must own %s", path, function)
			}
		}
	}
}
