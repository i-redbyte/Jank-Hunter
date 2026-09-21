package analyze

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestOfficialHeapRetraceUsesDeclaringOwnerAndPreservesUnknownType(t *testing.T) {
	if os.Getenv("JANK_HUNTER_RETRACE_HOME") == "" {
		t.Skip("requires offline official Retrace bundle")
	}
	path := filepath.Join(t.TempDir(), "mapping.txt")
	source := "original.Base -> a:\n    java.lang.Object binding -> x\noriginal.Child -> b:\n    java.lang.Object unrelated -> x\n    java.lang.String alternative -> x\noriginal.Target -> c:\n"
	if err := os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	mapping, err := LoadNameMapping(path)
	if err != nil {
		t.Fatal(err)
	}
	heap := &HeapEvidence{Leaks: []HeapLeakEvidence{{ClassName: "c", ReferencePath: []HeapPathElement{
		{ClassName: "b", Kind: "root_object"},
		{ClassName: "c", FieldName: "x", DeclaringClass: "a", Kind: "field"},
	}, AlternativePaths: [][]HeapPathElement{
		{{ClassName: "c", FieldName: "x", DeclaringClass: "b", Kind: "field"}},
		{{ClassName: "c", FieldName: "x", Kind: "field"}},
		{{ClassName: "c", FieldName: "x", DeclaringClass: "b", DeclaredType: "Ljava/lang/String;", Kind: "field"}},
	}}}}
	prepared, err := mapping.prepareHeapRetrace(context.Background(), heap)
	if err != nil {
		t.Fatal(err)
	}
	output := DeobfuscateHeapEvidence(heap, prepared).Leaks[0]
	if output.HolderField != "original.Base.binding" {
		t.Fatalf("wrong qualified inherited field: %s", output.HolderField)
	}
	inherited := output.ReferencePath[1]
	if inherited.FieldName != "binding" || inherited.DeclaringClass != "original.Base" || inherited.FieldRetrace.Status != "resolved" {
		t.Fatalf("inherited field guessed from Child: %+v", inherited)
	}
	ambiguous := output.AlternativePaths[0][0]
	if ambiguous.FieldName != "x" || ambiguous.FieldRetrace.Status != "ambiguous" || len(ambiguous.FieldRetrace.Alternatives) != 2 {
		t.Fatalf("ambiguity discarded: %+v", ambiguous)
	}
	missing := output.AlternativePaths[1][0]
	if missing.FieldName != "x" || missing.FieldRetrace.Status != "missing_declaring_owner" {
		t.Fatalf("invented declaring owner: %+v", missing)
	}
	typed := output.AlternativePaths[2][0]
	if typed.FieldName != "alternative" || typed.FieldRetrace.Status != "resolved" {
		t.Fatalf("exact type not used: %+v", typed)
	}
	if heap.Leaks[0].ReferencePath[1].FieldName != "x" {
		t.Fatal("mutated input")
	}
}
