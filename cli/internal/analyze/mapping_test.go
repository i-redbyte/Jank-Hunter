package analyze

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadNameMappingDeobfuscatesClassesOwnersAndHeap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mapping.txt")
	data := "com.app.feature.FeedPresenter -> a.b:\n" +
		"    1:1:void render():10:10 -> a\n" +
		"com.app.feature.FeedActivity -> c:\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("write mapping: %v", err)
	}
	mapping, err := LoadNameMapping(path)
	if err != nil {
		t.Fatalf("LoadNameMapping() error = %v", err)
	}

	if got := mapping.Deobfuscate("a.b.render"); got != "com.app.feature.FeedPresenter.render" {
		t.Fatalf("owner = %q", got)
	}
	if got := mapping.Deobfuscate("GC root: c"); got != "GC root: com.app.feature.FeedActivity" {
		t.Fatalf("gc root = %q", got)
	}

	heap := DeobfuscateHeapEvidence(&HeapEvidence{Leaks: []HeapLeakEvidence{{
		ClassName:     "c",
		Holder:        "a.b",
		DominatorTree: []string{"c × 1"},
		ReferencePath: []HeapPathElement{{ClassName: "a.b"}, {ClassName: "c"}},
	}}}, mapping)
	if heap.Leaks[0].ClassName != "com.app.feature.FeedActivity" {
		t.Fatalf("heap class = %q", heap.Leaks[0].ClassName)
	}
	if heap.Leaks[0].Holder != "com.app.feature.FeedPresenter" {
		t.Fatalf("heap holder = %q", heap.Leaks[0].Holder)
	}
}
