package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
)

func TestProductionSurfaceExcludesTestOnlyWrappers(t *testing.T) {
	forbidden := map[string]map[string]struct{}{
		"main.go": {
			"build": {},
		},
		filepath.Join("..", "..", "internal", "analyze", "aggregate.go"): {
			"addStreamResult": {},
		},
		filepath.Join("..", "..", "internal", "analyze", "graph_index.go"): {
			"RelevantEdges": {},
			"addEdges":      {},
		},
		filepath.Join("..", "..", "internal", "atomicfile", "atomicfile.go"): {
			"WriteFile": {},
		},
		filepath.Join("..", "..", "internal", "mathanalysis", "causal.go"): {
			"shortestGraphPath": {},
		},
		filepath.Join("..", "..", "internal", "mathanalysis", "integral.go"): {
			"computeIntegralScores": {},
		},
		filepath.Join("..", "..", "internal", "mathanalysis", "model.go"): {
			"dataQualityFindings": {},
		},
		filepath.Join("..", "..", "internal", "report", "html.go"): {
			"MathReportPath":                {},
			"LeakReportPath":                {},
			"InfluenceReportPath":           {},
			"DiagnosticsReportPath":         {},
			"DependencyInjectionReportPath": {},
			"canonicalCompanionReportPath":  {},
		},
	}
	fset := token.NewFileSet()
	for path, names := range forbidden {
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok {
				continue
			}
			if _, forbidden := names[function.Name.Name]; forbidden {
				t.Errorf("%s retains test-only production function %s", path, function.Name.Name)
			}
		}
	}
}
