package analyze

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestLoadNameMappingDeobfuscatesClassesOwnersAndHeap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mapping.txt")
	data := "com.app.Root -> a:\n" +
		"com.app.feature.FeedPresenter -> a.b:\n" +
		"    1:1:void render():10:10 -> a\n" +
		"com.app.feature.FeedActivity -> c:\n" +
		"com.app.feature.FeedActivity$bind$1 -> d:\n" +
		"# {\"id\":\"sourceFile\",\"fileName\":\"FeedActivity.kt\"}\n" +
		"    android.app.Activity this$0 -> e\n"
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

	input := &HeapEvidence{Leaks: []HeapLeakEvidence{{
		ClassName:      "c",
		Holder:         "a.b",
		HolderField:    "d.e",
		GCRootCategory: "class/static",
		LeakPattern:    "Сильная цепочка от корня GC удерживает объект",
		DominatorTree:  []string{"c × 1"},
		ReferencePath:  []HeapPathElement{{ClassName: "d"}, {ClassName: "c", FieldName: "e"}},
		AlternativePaths: [][]HeapPathElement{
			{{ClassName: "d"}, {ClassName: "c", FieldName: "e"}},
		},
		ReferenceMatchers: []string{"c"},
	}}}
	heap := DeobfuscateHeapEvidence(input, mapping)
	if heap.Leaks[0].ClassName != "com.app.feature.FeedActivity" {
		t.Fatalf("heap class = %q", heap.Leaks[0].ClassName)
	}
	if heap.Leaks[0].Holder != "com.app.feature.FeedPresenter" {
		t.Fatalf("heap holder = %q", heap.Leaks[0].Holder)
	}
	if got := heap.Leaks[0].ReferencePath[1].FieldName; got != "this$0" {
		t.Fatalf("heap capture field = %q, want %q", got, "this$0")
	}
	if got := heap.Leaks[0].HolderField; got != "com.app.feature.FeedActivity$bind$1.this$0" {
		t.Fatalf("heap holder field = %q", got)
	}
	if !slices.Contains(heap.Leaks[0].ReferenceMatchers, "kotlin.lambda_capture") {
		t.Fatalf("deobfuscated heap matchers = %v", heap.Leaks[0].ReferenceMatchers)
	}
	if got := heap.Leaks[0].LeakPattern; got != "Activity удерживается цепочкой статического поля или одиночки" {
		t.Fatalf("deobfuscated heap leak pattern = %q", got)
	}
	if input.Leaks[0].AlternativePaths[0][0].ClassName != "d" || input.Leaks[0].DominatorTree[0] != "c × 1" ||
		input.Leaks[0].ReferenceMatchers[0] != "c" {
		t.Fatalf("DeobfuscateHeapEvidence mutated its input: %+v", input.Leaks[0])
	}
	capture := LambdaCapture{
		Implementation: "com.app.feature.FeedActivity$bind$1",
		Representation: "class",
		Values: []LambdaCapturedValue{{
			Type: "com.app.feature.FeedActivity", Role: "capture:this$0", Strength: "strong",
		}},
	}
	leaks := []MemoryLeakSuspect{{HeapEvidence: true, ReferencePath: heap.Leaks[0].ReferencePath}}
	if risks := lambdaCaptureRisks(capture, buildLambdaHeapEvidenceIndex(leaks)); len(risks) != 1 || risks[0].confidence != "high" {
		t.Fatalf("deobfuscated exact lambda/HPROF correlation = %+v", risks)
	}
}

func TestLoadNameMappingKeepsAmbiguousObfuscatedFieldName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mapping.txt")
	data := "com.app.feature.AmbiguousLambda -> a:\n" +
		"    android.app.Activity this$0 -> b\n" +
		"    android.view.View L$0 -> b\n" +
		"com.app.feature.FeedActivity -> c:\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("write mapping: %v", err)
	}
	mapping, err := LoadNameMapping(path)
	if err != nil {
		t.Fatalf("LoadNameMapping() error = %v", err)
	}

	heap := DeobfuscateHeapEvidence(&HeapEvidence{Leaks: []HeapLeakEvidence{{
		ClassName: "c",
		ReferencePath: []HeapPathElement{
			{ClassName: "a"},
			{ClassName: "c", FieldName: "b"},
		},
	}}}, mapping)
	if got := heap.Leaks[0].ReferencePath[1].FieldName; got != "b" {
		t.Fatalf("ambiguous heap field = %q, want conservative original %q", got, "b")
	}
}

func TestLoadNameMappingDoesNotRetainUnrelatedFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mapping.txt")
	data := "com.app.feature.Model -> a:\n" +
		"    java.lang.String title -> b\n" +
		"    java.lang.String subtitle -> c\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("write mapping: %v", err)
	}
	mapping, err := LoadNameMapping(path)
	if err != nil {
		t.Fatalf("LoadNameMapping() error = %v", err)
	}
	if len(mapping.fields) != 0 {
		t.Fatalf("retained %d unrelated field mappings", len(mapping.fields))
	}
}

func TestExpandMappedClassAliasesIncludesObfuscatedHeapTargets(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mapping.txt")
	data := "com.app.feature.FeedActivity -> a:\n" +
		"com.app.feature.OtherActivity -> b:\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("write mapping: %v", err)
	}
	mapping, err := LoadNameMapping(path)
	if err != nil {
		t.Fatalf("LoadNameMapping() error = %v", err)
	}

	aliases := ExpandMappedClassAliases([]string{"com.app.feature.FeedActivity"}, mapping)
	if !slices.Contains(aliases, "com.app.feature.FeedActivity") || !slices.Contains(aliases, "a") {
		t.Fatalf("mapped heap targets = %v", aliases)
	}
	if slices.Contains(aliases, "b") {
		t.Fatalf("unrelated obfuscated class leaked into heap targets: %v", aliases)
	}
}
