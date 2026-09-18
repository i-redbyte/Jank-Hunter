package analyze

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func mappingIdentityFixture(t *testing.T, identity jhlog.BuildIdentity) (string, *NameMapping) {
	t.Helper()
	directory := t.TempDir()
	mappingPath := filepath.Join(directory, "mapping.txt")
	if err := os.WriteFile(mappingPath, []byte("original.Second -> a:\n"), 0600); err != nil {
		t.Fatal(err)
	}
	mapping, err := LoadNameMapping(mappingPath)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "run.jhlog")
	header := jhlog.DefaultSegmentHeader()
	header.BuildIdentity = identity
	file, writer, err := jhlog.CreateWithHeader(path, header)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteEvent(jhlog.Event{Type: jhlog.EventDictionary, Dictionary: &jhlog.DictionaryEntry{Kind: jhlog.DictClass, ID: 1, Value: "a"}}); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteEvent(jhlog.Event{Type: jhlog.EventRetained, TimeMS: 1000, Retained: &jhlog.RetainedEvent{ClassRef: jhlog.LocalSymbol(1), Count: 1, AgeMS: 1000}}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return path, mapping
}

func TestWrongMappingWithSameShortClassNameIsRejected(t *testing.T) {
	hash := sha256.Sum256([]byte("original.First -> a:\n"))
	path, mapping := mappingIdentityFixture(t, jhlog.BuildIdentity{State: jhlog.BuildIdentityMapped, MappingSHA256: hex.EncodeToString(hash[:])})
	_, err := InspectFilesWithOptions("wrong mapping", []string{path}, Options{ObfuscationMap: mapping})
	if err == nil || !strings.Contains(err.Error(), "mapping identity mismatch") {
		t.Fatalf("foreign mapping was accepted: %v", err)
	}
}

func TestUnverifiedMappingRequiresExplicitTrust(t *testing.T) {
	path, mapping := mappingIdentityFixture(t, jhlog.BuildIdentity{})
	if _, err := InspectFilesWithOptions("unknown identity", []string{path}, Options{ObfuscationMap: mapping}); err == nil {
		t.Fatal("unknown mapping was silently applied")
	}
}

func TestMappingIdentityPolicy(t *testing.T) {
	hash := sha256.Sum256([]byte("original.Second -> a:\n"))
	matching := jhlog.BuildIdentity{State: jhlog.BuildIdentityMapped, MappingSHA256: hex.EncodeToString(hash[:])}
	for _, test := range []struct {
		name     string
		identity jhlog.BuildIdentity
		allow    bool
		status   MappingIdentityStatus
		reject   bool
	}{
		{name: "verified", identity: matching, status: MappingVerified},
		{name: "explicit trust remains unverified", allow: true, status: MappingUnverified},
		{name: "unminified cannot be overridden", identity: jhlog.BuildIdentity{State: jhlog.BuildIdentityUnminified}, allow: true, reject: true},
		{name: "mismatch cannot be overridden", identity: jhlog.BuildIdentity{State: jhlog.BuildIdentityMapped, MappingSHA256: strings.Repeat("0", 64)}, allow: true, reject: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			path, mapping := mappingIdentityFixture(t, test.identity)
			summary, err := InspectFilesWithOptions("identity", []string{path}, Options{ObfuscationMap: mapping, AllowUnverifiedMapping: test.allow})
			if test.reject {
				if err == nil {
					t.Fatal("contradictory mapping accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if summary.MappingIdentity.Status != test.status || !summary.MappingIdentity.Applied || summary.MappingIdentity.SHA256 != mapping.digest {
				t.Fatalf("wrong evidence: %+v", summary.MappingIdentity)
			}
		})
	}
}

func TestLegacyAndMixedMappingEvidence(t *testing.T) {
	digest := strings.Repeat("a", 64)
	mapping := &NameMapping{digest: digest}
	verified := SessionInput{Path: "mapped", Header: jhlog.DefaultSegmentHeader()}
	verified.Header.BuildIdentity = jhlog.BuildIdentity{State: jhlog.BuildIdentityMapped, MappingSHA256: digest}
	legacy := SessionInput{Path: "legacy", Header: jhlog.DefaultSegmentHeader()}
	legacy.Header.Schema = jhlog.HeaderSchemaV2
	for _, inputs := range [][]SessionInput{{legacy}, {verified, legacy}} {
		if _, err := ValidateMappingInputs(inputs, mapping, false); err == nil {
			t.Fatal("legacy log accepted without explicit trust")
		}
		evidence, err := ValidateMappingInputs(inputs, mapping, true)
		if err != nil {
			t.Fatal(err)
		}
		if evidence.Status != MappingUnverified || evidence.UnverifiedLogs != 1 || evidence.VerifiedLogs != len(inputs)-1 {
			t.Fatalf("mixed provenance lost: %+v", evidence)
		}
	}
}

func TestClassOnlyAnalysisRejectsFutureMappingVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mapping.txt")
	text := "# {\"id\":\"com.android.tools.r8.mapping\",\"version\":\"99.0\"}\noriginal.Holder -> a:\n"
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	mapping, err := LoadNameMapping(path)
	if err != nil {
		t.Fatal(err)
	}
	input := SessionInput{Header: jhlog.DefaultSegmentHeader()}
	input.Header.BuildIdentity = jhlog.BuildIdentity{State: jhlog.BuildIdentityMapped, MappingSHA256: mapping.digest}
	if _, err := ValidateMappingInputs([]SessionInput{input}, mapping, false); err == nil || !strings.Contains(err.Error(), "99.0") {
		t.Fatalf("expected future-version rejection without any stack request: %v", err)
	}
}
