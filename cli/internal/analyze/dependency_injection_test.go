package analyze

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLoadDependencyInjectionCatalogAndBuildReport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "di-catalog.jsonl")
	fixture := strings.Join([]string{
		`{"format":1,"kind":"metadata","variant":"debug","semantics":"build_time_di","edgeDirection":"consumer_to_dependency","runtimeTracing":false,"affectsScore":false}`,
		`{"format":1,"kind":"class","name":"com.app.FeedViewModel","framework":"hilt","roles":["consumer"],"generated":false,"scopes":["javax.inject.Singleton"],"components":["dagger.hilt.components.SingletonComponent"]}`,
		`{"format":1,"kind":"class","name":"com.app.FeedViewModel","framework":"hilt","roles":["entry_point"],"generated":false,"scopes":[],"components":[]}`,
		`{"format":1,"kind":"edge","consumer":"com.app.FeedViewModel","dependency":"com.app.FeedRepository","framework":"hilt","injectionKind":"constructor","site":"com.app.FeedViewModel#<init>(Lcom/app/FeedRepository;)V","qualifiers":[],"resolution":"declared"}`,
		`{"format":1,"kind":"edge","consumer":"com.app.FeedViewModel","dependency":"com.app.FeedRepository","framework":"hilt","injectionKind":"constructor","site":"com.app.FeedViewModel#<init>(Lcom/app/FeedRepository;)V","qualifiers":[],"resolution":"declared"}`,
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	catalog, err := LoadDependencyInjectionCatalog(path)
	if err != nil {
		t.Fatalf("LoadDependencyInjectionCatalog() error = %v", err)
	}
	if !catalog.Available || catalog.Variant != "debug" {
		t.Fatalf("catalog = %+v", catalog)
	}
	if len(catalog.Classes) != 1 || len(catalog.Edges) != 1 {
		t.Fatalf("classes=%d edges=%d, want deduplicated 1/1", len(catalog.Classes), len(catalog.Edges))
	}
	if got := strings.Join(catalog.Classes[0].Roles, ","); got != "consumer,entry_point" {
		t.Fatalf("roles = %q", got)
	}

	report := BuildDependencyInjectionReport(catalog, Summary{
		CodeProblems: []CodeProblemStats{{ClassName: "com.app.FeedViewModel", RuntimeEvidence: true}},
	})
	if report.Disclaimer != DependencyInjectionDisclaimer {
		t.Fatalf("disclaimer = %q", report.Disclaimer)
	}
	if len(report.Classes) != 1 || len(report.Classes[0].Observed) == 0 {
		t.Fatalf("report classes = %+v", report.Classes)
	}
	if len(report.Edges) != 1 || !report.Edges[0].ConsumerObserved {
		t.Fatalf("report edges = %+v", report.Edges)
	}
}

func TestDependencyInjectionCatalogRejectsRuntimeOrScoringSemantics(t *testing.T) {
	for name, metadata := range map[string]string{
		"runtime tracing": `{"format":1,"kind":"metadata","variant":"debug","semantics":"build_time_di","edgeDirection":"consumer_to_dependency","runtimeTracing":true,"affectsScore":false}`,
		"scoring":         `{"format":1,"kind":"metadata","variant":"debug","semantics":"build_time_di","edgeDirection":"consumer_to_dependency","runtimeTracing":false,"affectsScore":true}`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "di-catalog.jsonl")
			if err := os.WriteFile(path, []byte(metadata+"\n"), 0o600); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}
			if _, err := LoadDependencyInjectionCatalog(path); err == nil {
				t.Fatal("LoadDependencyInjectionCatalog() accepted unsafe metadata")
			}
		})
	}
}

func TestDependencyInjectionCatalogDoesNotChangeRuntimeAnalysis(t *testing.T) {
	withoutCatalog := newCollector("same", 0, Options{}).finish()
	withCatalog := newCollector("same", 0, Options{
		DependencyInjectionCatalog: &DependencyInjectionCatalog{
			Available: true,
			Classes: []DependencyInjectionClass{{
				Name:      "com.app.Consumer",
				Framework: "dagger2",
			}},
			Edges: []DependencyInjectionEdge{{
				Consumer:      "com.app.Consumer",
				Dependency:    "com.app.Dependency",
				Framework:     "dagger2",
				InjectionKind: "constructor",
				Site:          "com.app.Consumer#<init>()V",
				Resolution:    "declared",
			}},
		},
	}).finish()

	if !reflect.DeepEqual(withoutCatalog, withCatalog) {
		t.Fatalf("DI catalog changed runtime summary\nwithout=%+v\nwith=%+v", withoutCatalog, withCatalog)
	}
}

func TestDependencyInjectionObservationLookupUsesNestedClassAncestors(t *testing.T) {
	observed := map[string]map[string]struct{}{
		"com.app.Outer": {"runtime": {}},
	}
	if got := dependencyInjectionObservations("com.app.Outer$Inner", observed); len(got) != 1 || got[0] != "runtime" {
		t.Fatalf("observations = %#v", got)
	}
}
