package analyze

import (
	"path/filepath"
	"testing"
)

func TestMixedLegacyAndCausalGoldenKeepsEventOnlyThread(t *testing.T) {
	fixture := func(name string) string {
		return filepath.Join("..", "..", "..", "wire", "testdata", name)
	}
	summary, err := InspectFilesWithOptions("5.0 5.1 5.2", []string{
		fixture("legacy-5.0.0.jhlog"),
		fixture("thread-lifecycle-r8-5.1.0.jhlog"),
		fixture("causal-short-thread-5.2.0.bin"),
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	causal := summary.CausalAnalysis
	if causal == nil || causal.Metrics.ThreadGenerations != 1 || causal.Metrics.ThreadCreated != 1 ||
		causal.Metrics.ThreadStarts != 1 || causal.ObservedThreadGenerations != 1 ||
		causal.RootRunObservedThreadGenerations != 1 {
		t.Fatalf("event-only 5.2 thread disappeared while reading legacy files: %+v", causal)
	}
}
