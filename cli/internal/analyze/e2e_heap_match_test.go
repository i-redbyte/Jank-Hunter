package analyze

import "testing"

func TestE2EOwnerHintsMatchLoadedHprof(t *testing.T) {
	path := "/Users/red_byte/work/Jank-Hunter/reports/android-e2e/logs/retained-1790037169646-io.jankhunter.sample.MemoryScenarioActivity-1.hprof"
	evidence, err := LoadHeapEvidenceFiles([]string{path}, []string{
		"io.jankhunter.sample.MemoryScenarioActivity",
		"io.jankhunter.sample.RetainedCheckoutScreen",
		"io.jankhunter.sample.RetainedCheckoutCache",
	})
	if err != nil {
		t.Fatal(err)
	}
	cases := []memoryLeakStats{
		{
			className:            "io.jankhunter.sample.RetainedCheckoutScreen",
			holder:               "sample.auto.retention.activity_registry",
			count:                1,
			afterExplicitGCCount: 1,
		},
		{
			className:            "io.jankhunter.sample.MemoryScenarioActivity",
			holder:               "sample.auto.retention.activity_reference",
			count:                1,
			afterExplicitGCCount: 1,
		},
	}
	for _, item := range cases {
		candidate := bestHeapEvidence(item, evidence)
		if candidate == nil {
			for _, leak := range evidence.Leaks {
				if leak.ClassName == item.className {
					t.Logf("leak=%+v hintMatch=%v holderMatch=%v", leak, retentionOwnerHintMatchesHeap(item, leak), heapHolderMatchesRuntime(item, leak))
				}
			}
			t.Fatalf("%s: no heap candidate", item.className)
		}
		if !heapConfirmsWatchedRetention(item, candidate) {
			t.Fatalf("%s: hint=%q holder=%q path=%d", item.className, item.holder, candidate.Holder, len(candidate.ReferencePath))
		}
	}
}
