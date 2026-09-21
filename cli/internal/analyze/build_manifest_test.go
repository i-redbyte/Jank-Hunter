package analyze

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func manifestFixture(t *testing.T) (string, *ClassGraph, buildManifest) {
	t.Helper()
	directory := t.TempDir()
	path := filepath.Join(directory, "class-graph.jsonl")
	data := []byte("{\"format\":1,\"class\":\"original.Screen\",\"edges\":[]}\n")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	graph, err := LoadClassGraph(path)
	if err != nil {
		t.Fatal(err)
	}
	digest := func(data []byte) string { value := sha256.Sum256(data); return hex.EncodeToString(value[:]) }
	mapping := strings.Repeat("a", 64)
	namespace := strings.Repeat("0", 32)
	asset := fmt.Sprintf("schema=1\nstate=mapped\nmapping-sha256=%s\nsymbol-namespace=%s\n", mapping, namespace)
	manifest := buildManifest{Format: 1, Kind: "build-manifest", Variant: "release", SymbolNamespace: namespace, MappingSHA256: mapping, IdentityAssetSHA256: digest([]byte(asset)), Packages: []buildManifestHash{{File: "app-release.apk", SHA256: strings.Repeat("b", 64)}}, Artifacts: []buildManifestHash{{File: "class-graph.jsonl", SHA256: digest(data)}}}
	writeTestManifest(t, directory, manifest)
	return directory, graph, manifest
}

func writeTestManifest(t *testing.T, directory string, manifest buildManifest) {
	t.Helper()
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "build-manifest-apk.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestInspectRejectsForeignArtifactBuildWithSameNamespace(t *testing.T) {
	_, graph, _ := manifestFixture(t)
	header := jhlog.DefaultSegmentHeader()
	header.SymbolNamespace = make([]byte, 16)
	header.BuildIdentity = jhlog.BuildIdentity{State: jhlog.BuildIdentityMapped, MappingSHA256: strings.Repeat("c", 64)}
	path := filepath.Join(t.TempDir(), "run.jhlog")
	file, writer, err := jhlog.CreateWithHeader(path, header)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := InspectFilesWithOptions("foreign artifacts", []string{path}, Options{ClassGraph: graph}); err == nil || !strings.Contains(err.Error(), "artifact mapping identity mismatch") {
		t.Fatalf("foreign same-namespace artifacts accepted: %v", err)
	}
}

func TestArtifactEvidenceBindsParsedBytesAndDoesNotClaimPackageVerification(t *testing.T) {
	directory, graph, manifest := manifestFixture(t)
	header := jhlog.DefaultSegmentHeader()
	header.SymbolNamespace = make([]byte, 16)
	header.BuildIdentity = jhlog.BuildIdentity{State: jhlog.BuildIdentityMapped, MappingSHA256: manifest.MappingSHA256}
	inputs := []SessionInput{{Path: "run", Header: header}}
	options := Options{ClassGraph: graph}
	// Changing the path after parsing must not replace the evidence already loaded into memory.
	if err := os.WriteFile(filepath.Join(directory, "class-graph.jsonl"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	evidence, err := options.validateArtifactIdentities(inputs)
	if err != nil {
		t.Fatal(err)
	}
	if evidence.Status != "mapping_bound" || evidence.VerifiedArtifacts != 1 || evidence.PackageHashesVerified {
		t.Fatalf("wrong scope of verification: %+v", evidence)
	}
	inputs[0].Header.Schema = jhlog.HeaderSchemaV2
	inputs[0].Header.BuildIdentity = jhlog.BuildIdentity{}
	evidence, err = options.validateArtifactIdentities(inputs)
	if err != nil || evidence.Status != "unverified" {
		t.Fatalf("legacy artifact binding: %+v %v", evidence, err)
	}
	manifest.Artifacts[0].SHA256 = strings.Repeat("d", 64)
	writeTestManifest(t, directory, manifest)
	if _, err := options.validateArtifactIdentities(inputs); err == nil {
		t.Fatal("wrong parsed artifact hash accepted")
	}
}

func TestBuildManifestRejectsMalformedIdentityAndUnsafeEntries(t *testing.T) {
	for _, mutation := range []func(*buildManifest){
		func(m *buildManifest) { m.Artifacts = append(m.Artifacts, m.Artifacts[0]) },
		func(m *buildManifest) { m.Artifacts[0].File = "../other" },
		func(m *buildManifest) { m.IdentityAssetSHA256 = strings.Repeat("f", 64) },
		func(m *buildManifest) { m.Format = 2 },
	} {
		directory, _, manifest := manifestFixture(t)
		mutation(&manifest)
		writeTestManifest(t, directory, manifest)
		if _, err := loadBuildManifest(filepath.Join(directory, "build-manifest-apk.json")); err == nil {
			t.Fatal("malformed manifest accepted")
		}
	}
}

func TestArtifactManifestSelectsMatchingPackageBuildAmongStaleSiblings(t *testing.T) {
	directory, graph, manifest := manifestFixture(t)
	valid, err := os.ReadFile(filepath.Join(directory, "build-manifest-apk.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "build-manifest-aab.json"), valid, 0600); err != nil {
		t.Fatal(err)
	}
	manifest.Artifacts[0].SHA256 = strings.Repeat("f", 64)
	writeTestManifest(t, directory, manifest)
	header := jhlog.DefaultSegmentHeader()
	header.SymbolNamespace = make([]byte, 16)
	header.BuildIdentity = jhlog.BuildIdentity{State: jhlog.BuildIdentityMapped, MappingSHA256: manifest.MappingSHA256}
	evidence, err := (Options{ClassGraph: graph}).validateArtifactIdentities([]SessionInput{{Header: header}})
	if err != nil || evidence.Status != "mapping_bound" {
		t.Fatalf("matching AAB manifest hidden by stale APK: %+v %v", evidence, err)
	}
}

func TestNamespaceAloneDoesNotClaimVerifiedArtifactIdentity(t *testing.T) {
	collector := &collector{collectorInputState: collectorInputState{artifactNamespace: make([]byte, 16), diagnostics: &InstrumentationDiagnostics{Available: true, ClassCount: 1}}}
	result := collector.analysisInputCompleteness(Summary{LogCount: 1, DataRecordCount: 1, Influence: InfluenceSummary{HasClassGraph: true}})
	if result.ArtifactIdentityVerified || result.Complete {
		t.Fatalf("namespace misrepresented as build identity: %+v", result)
	}
}
