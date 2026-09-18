package analyze

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestInstrumentationPassesPreserveCoverageWithoutDoubleCounting(t *testing.T) {
	main := `{"format":2,"pass":"main","class":"app.Screen","methods":2,"methodFilterIncluded":2,"hooks":[{"intent":"method","signature":"method","method":"render()V","count":1}]}`
	lifecycle := `{"format":2,"pass":"lifecycle","class":"app.Screen","methods":2,"methodFilterIncluded":2,"hooks":[{"intent":"lifecycle.watch_retained","signature":"android.lifecycle.onDestroy()V","method":"onDestroy()V","count":1}]}`
	path := filepath.Join(t.TempDir(), "diagnostics.jsonl")
	var previous *InstrumentationDiagnostics
	for _, rows := range []string{main + "\n" + lifecycle, lifecycle + "\n" + main} {
		if err := os.WriteFile(path, []byte(rows), 0600); err != nil {
			t.Fatal(err)
		}
		got, err := LoadInstrumentationDiagnostics(path)
		if err != nil {
			t.Fatal(err)
		}
		if got.ClassCount != 1 || got.MethodCount != 2 || got.MethodFilterIncluded != 2 || got.HookCount != 2 || len(got.Classes) != 1 || len(got.Classes[0].Hooks) != 2 {
			t.Fatalf("wrong combined coverage: %+v", got)
		}
		// Byte identity changes with shard order; the semantic diagnostics must not.
		semantic := *got
		semantic.sourceIdentity = artifactSourceIdentity{}
		if previous != nil && !reflect.DeepEqual(previous, &semantic) {
			t.Fatal("shard order changes diagnostics")
		}
		previous = &semantic
	}
}

func TestInstrumentationPassesRejectMalformedIdentity(t *testing.T) {
	for _, rows := range []string{
		`{"format":2,"class":"app.Screen","methods":2}`,
		`{"format":2,"pass":"unknown","class":"app.Screen","methods":2}`,
		`{"format":1,"pass":"main","class":"app.Screen","methods":2}`,
		"{\"format\":2,\"pass\":\"main\",\"class\":\"app.Screen\",\"methods\":2}\n{\"format\":2,\"pass\":\"main\",\"class\":\"app.Screen\",\"methods\":3}",
		"{\"format\":1,\"class\":\"app.Screen\",\"methods\":2}\n{\"format\":2,\"pass\":\"lifecycle\",\"class\":\"app.Screen\",\"methods\":2}",
	} {
		path := filepath.Join(t.TempDir(), "diagnostics.jsonl")
		if err := os.WriteFile(path, []byte(rows), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadInstrumentationDiagnostics(path); err == nil {
			t.Fatalf("accepted malformed identity: %s", rows)
		}
	}
}

func TestInstrumentationLifecycleOnlyRetainsPassCounters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "diagnostics.jsonl")
	row := `{"format":2,"pass":"lifecycle","class":"app.Screen","methods":3,"annotatedMethods":1,"annotations":[{"owner":"screen","count":1}],"methodFilterIncluded":3,"hooks":[{"intent":"lifecycle.watch_retained","signature":"onDestroy","method":"onDestroy()V","count":1}]}`
	if err := os.WriteFile(path, []byte(row), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := LoadInstrumentationDiagnostics(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.MethodCount != 0 || got.AnnotatedMethodCount != 0 || got.ClassCount != 1 || got.HookCount != 1 {
		t.Fatalf("lifecycle counters treated as main: %+v", got)
	}
	passes := got.Classes[0].Passes
	if len(passes) != 1 || passes[0].Methods != 3 || passes[0].AnnotatedMethods != 1 || passes[0].Annotations[0].Owner != "screen" {
		t.Fatalf("lost provenance: %+v", passes)
	}
}

func BenchmarkInstrumentationPasses(b *testing.B) {
	for _, versioned := range []bool{false, true} {
		b.Run(fmt.Sprintf("versioned=%t", versioned), func(b *testing.B) {
			path := filepath.Join(b.TempDir(), "diagnostics.jsonl")
			var rows strings.Builder
			for i := 0; i < 2048; i++ {
				if versioned {
					fmt.Fprintf(&rows, "{\"format\":2,\"pass\":\"main\",\"class\":\"app.C%d\",\"methods\":8}\n{\"format\":2,\"pass\":\"lifecycle\",\"class\":\"app.C%d\",\"methods\":8}\n", i, i)
				} else {
					fmt.Fprintf(&rows, "{\"format\":1,\"class\":\"app.C%d\",\"methods\":8}\n", i)
				}
			}
			if err := os.WriteFile(path, []byte(rows.String()), 0600); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := LoadInstrumentationDiagnostics(path); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
