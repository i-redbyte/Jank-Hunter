package analyze

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestR8InlineStackRestoresEveryOriginalFrame(t *testing.T) {
	if os.Getenv("JANK_HUNTER_RETRACE_HOME") == "" {
		t.Skip("requires built offline Retrace bundle")
	}
	fixture := filepath.Join("testdata", "r8-inline-9.0.32")
	mapping, err := LoadNameMapping(filepath.Join(fixture, "mapping.txt"))
	if err != nil {
		t.Fatal(err)
	}
	read := func(name string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(fixture, name))
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(data))
	}
	results, err := mapping.RetraceStacks(context.Background(), []string{read("stack.txt")})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := results[0].Text(), read("expected.txt"); got != want {
		t.Fatalf("R8 inline stack\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestRetracePreservesExplicitStackTruncation(t *testing.T) {
	if os.Getenv("JANK_HUNTER_RETRACE_HOME") == "" {
		t.Skip("requires offline Retrace bundle")
	}
	mapping, err := LoadNameMapping(filepath.Join("testdata", "r8-inline-9.0.32", "mapping.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"!", "\tat Unknown.method(Unknown.java:1)\n[Jank Hunter: stack truncated]"} {
		evidence, err := mapping.RetraceStacks(context.Background(), []string{raw})
		if err != nil {
			t.Fatal(err)
		}
		if !evidence[0].Truncated || !strings.Contains(evidence[0].StatusLabel(), "обрезан") {
			t.Fatalf("truncation lost: %+v", evidence)
		}
	}
}
