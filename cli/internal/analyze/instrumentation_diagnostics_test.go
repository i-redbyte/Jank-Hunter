package analyze

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadInstrumentationDiagnosticsAggregatesJSONL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "instrumentation-diagnostics.jsonl")
	if err := os.WriteFile(path, []byte(
		`{"format":1,"class":"com.app.Feed","methods":3,"ignoredMethods":1,"annotatedMethods":1,"skippedMethods":[{"reason":"constructor","count":1}],"hooks":[{"intent":"logspam.android.util.Log.d","signature":"logspam.android.util.Log.d","count":2}],"annotations":[{"owner":"FeedOwner","screen":"Feed","flow":"feed.open","trace":"load","count":1}]}`+"\n"+
			`{"format":1,"class":"com.app.Net","methods":2,"ignoredMethods":0,"annotatedMethods":0,"skippedMethods":[],"hooks":[{"intent":"okhttp.install_event_listener_factory","signature":"okhttp3.builder.build.v3","bridge":"okhttp3.bridge.v3","count":1}],"decisions":[{"kind":"unsupported","module":"okhttp","family":"okhttp","reason":"unsupported_signature","count":2}],"annotations":[]}`+"\n",
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
	if got := diagnostics.Annotations[0].Flow; got != "feed.open" {
		t.Fatalf("annotation flow = %q", got)
	}
	if len(diagnostics.Decisions) != 1 || diagnostics.Decisions[0].Reason != "unsupported_signature" {
		t.Fatalf("unexpected decisions: %+v", diagnostics.Decisions)
	}
	if got := diagnostics.TopClasses[0].ClassName; got != "com.app.Feed" {
		t.Fatalf("top class = %q", got)
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
