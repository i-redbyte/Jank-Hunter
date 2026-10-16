package analyze

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAdaptiveMetadataBuildArtifactsRemainReadable(t *testing.T) {
	dir := t.TempDir()
	metadata := filepath.Join(dir, "artifact-metadata.json")
	if err := os.WriteFile(metadata, []byte(`{"format":1,"kind":"artifact-metadata","symbolNamespace":"00112233445566778899aabbccddeeff","metadataCompatibilityMode":"STRICT","metadataReaderIdentity":"reader-digest"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadArtifactMetadataNamespace(metadata); err != nil {
		t.Fatal(err)
	}
	diagnostics := filepath.Join(dir, "instrumentation-diagnostics.jsonl")
	if err := os.WriteFile(diagnostics, []byte(`{"format":2,"pass":"causal","class":"app.Future","metadataStatus":"UNSUPPORTED_VERSION","metadataVersion":"99.0","metadataReader":"","methods":1}`+"\n"+`{"format":2,"pass":"main","class":"app.Future","methods":1}`+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := LoadInstrumentationDiagnostics(diagnostics)
	if err != nil {
		t.Fatal(err)
	}
	if result.MethodCount != 1 || result.ClassCount != 1 || len(result.Classes[0].Passes) != 2 {
		t.Fatalf("metadata passes must not duplicate main counts: %+v", result)
	}
}
