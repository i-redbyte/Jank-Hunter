package analyze

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadInstrumentationDiagnosticsAggregatesJSONL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "instrumentation-diagnostics.jsonl")
	if err := os.WriteFile(path, []byte(
		`{"format":1,"class":"com.app.Feed","methods":3,"ignoredMethods":1,"annotatedMethods":1,"skippedMethods":[{"reason":"constructor","count":1}],"hooks":[{"intent":"logspam.android.util.Log.d","signature":"logspam.android.util.Log.d","method":"load()V","line":42,"count":2}],"annotations":[{"owner":"FeedOwner","screen":"Feed","operation":"feed.open","operationKind":"USER","operationBudgetMs":250,"count":1}]}`+"\n"+
			`{"format":1,"class":"com.app.Net","methods":2,"ignoredMethods":0,"annotatedMethods":0,"skippedMethods":[],"hooks":[{"intent":"okhttp.install_event_listener_factory","signature":"okhttp3.builder.build.v3","bridge":"okhttp3.bridge.v3","method":"client()V","line":12,"count":1}],"decisions":[{"kind":"unsupported","module":"okhttp","family":"okhttp","reason":"unsupported_signature","method":"client()V","line":13,"count":2},{"kind":"warning","module":"class_hierarchy","family":"metadata","reason":"metadata_load_failed","method":"broken.Parent","detail":"java.lang.IllegalStateException: invalid class metadata","count":1}],"annotations":[]}`+"\n",
	), 0o644); err != nil {
		t.Fatalf("write diagnostics fixture: %v", err)
	}

	diagnostics, err := LoadInstrumentationDiagnostics(path)
	if err != nil {
		t.Fatalf("LoadInstrumentationDiagnostics() error = %v", err)
	}
	if diagnostics == nil || !diagnostics.Available {
		t.Fatalf("expected diagnostics to be available")
	}
	if diagnostics.ClassCount != 2 || diagnostics.MethodCount != 5 {
		t.Fatalf("unexpected totals: %+v", diagnostics)
	}
	if diagnostics.HookCount != 3 || diagnostics.AnnotatedMethodCount != 1 || diagnostics.IgnoredMethodCount != 1 {
		t.Fatalf("unexpected aggregate counts: %+v", diagnostics)
	}
	if got := diagnostics.Hooks[0].Intent; got != "logspam.android.util.Log.d" {
		t.Fatalf("top hook = %q", got)
	}
	if got := diagnostics.Hooks[0].Line; got != 42 {
		t.Fatalf("top hook line = %d", got)
	}
	if got := diagnostics.Hooks[0].Method; got != "load()V" {
		t.Fatalf("top hook method = %q", got)
	}
	if got := diagnostics.Annotations[0].Operation; got != "feed.open" {
		t.Fatalf("annotation operation = %q", got)
	}
	if got := diagnostics.Annotations[0].OperationBudgetMS; got != 250 {
		t.Fatalf("annotation budget = %d", got)
	}
	if len(diagnostics.Decisions) != 2 || diagnostics.Decisions[0].Reason != "unsupported_signature" {
		t.Fatalf("unexpected decisions: %+v", diagnostics.Decisions)
	}
	if got := diagnostics.Decisions[1].Detail; got != "java.lang.IllegalStateException: invalid class metadata" {
		t.Fatalf("hierarchy decision detail = %q", got)
	}
	if got := diagnostics.Decisions[0].Line; got != 13 {
		t.Fatalf("decision line = %d", got)
	}
	if got := diagnostics.Decisions[0].Method; got != "client()V" {
		t.Fatalf("decision method = %q", got)
	}
	if len(diagnostics.Warnings) != 1 || !strings.Contains(diagnostics.Warnings[0], "broken.Parent") {
		t.Fatalf("hierarchy warning is not actionable: %+v", diagnostics.Warnings)
	}
	if got := diagnostics.Classes[0].ClassName; got != "com.app.Feed" {
		t.Fatalf("top class = %q", got)
	}
	if len(diagnostics.Classes) != 2 {
		t.Fatalf("all diagnostic classes were not retained: %+v", diagnostics.Classes)
	}
}

func TestLoadInstrumentationDiagnosticsRetainsEveryClass(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "instrumentation-diagnostics.jsonl")
	var contents strings.Builder
	for index := 0; index < 250; index++ {
		fmt.Fprintf(
			&contents,
			"{\"format\":1,\"class\":\"com.app.Class%03d\",\"methods\":1,\"ignoredMethods\":0,\"annotatedMethods\":0}\n",
			index,
		)
	}
	if err := os.WriteFile(path, []byte(contents.String()), 0o644); err != nil {
		t.Fatalf("write diagnostics fixture: %v", err)
	}

	diagnostics, err := LoadInstrumentationDiagnostics(path)
	if err != nil {
		t.Fatalf("LoadInstrumentationDiagnostics() error = %v", err)
	}
	if got := len(diagnostics.Classes); got != 250 {
		t.Fatalf("diagnostic classes = %d, want 250", got)
	}
}

func TestLoadInstrumentationDiagnosticsRejectsUnsupportedFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "instrumentation-diagnostics.jsonl")
	if err := os.WriteFile(path, []byte(`{"format":2,"class":"com.app.Feed"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write diagnostics fixture: %v", err)
	}

	if _, err := LoadInstrumentationDiagnostics(path); err == nil {
		t.Fatal("LoadInstrumentationDiagnostics() accepted unsupported format")
	}
}
