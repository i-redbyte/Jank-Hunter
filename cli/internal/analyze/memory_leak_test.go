package analyze

import (
	"fmt"
	"testing"
)

func TestBuildMemoryLeakSuspectsKeepsEveryQualifiedSuspect(t *testing.T) {
	const suspectCount = 96
	items := make(map[string]*memoryLeakStats, suspectCount)
	for index := range suspectCount {
		className := fmt.Sprintf("com.app.Leak%03d", index)
		items[className] = &memoryLeakStats{
			className:     className,
			holder:        "com.app.Root",
			count:         1,
			maxAgeMs:      uint64(100_000 - index),
			timeOnlyCount: 1,
		}
	}

	got := buildMemoryLeakSuspects(items, 0, 0, nil, retentionDataQuality{})
	if len(got) != suspectCount {
		t.Fatalf("suspect count = %d, want %d", len(got), suspectCount)
	}
	seen := make(map[string]struct{}, len(got))
	for _, suspect := range got {
		seen[suspect.ClassName] = struct{}{}
	}
	for className := range items {
		if _, ok := seen[className]; !ok {
			t.Fatalf("qualified suspect %q was silently omitted", className)
		}
	}
}
