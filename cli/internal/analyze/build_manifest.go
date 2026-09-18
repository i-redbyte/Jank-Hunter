package analyze

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

type artifactSourceIdentity struct{ path, digest string }

type ArtifactIdentityEvidence struct {
	Status              string
	VerifiedArtifacts   int
	UnverifiedArtifacts int
	// Package hashes are manifest claims until the actual APK/AAB is supplied and hashed.
	PackageHashesVerified bool
}

type buildManifestHash struct {
	File   string `json:"file"`
	SHA256 string `json:"sha256"`
}
type buildManifest struct {
	Format              int                 `json:"format"`
	Kind                string              `json:"kind"`
	Variant             string              `json:"variant"`
	SymbolNamespace     string              `json:"symbolNamespace"`
	MappingSHA256       string              `json:"mappingSha256"`
	IdentityAssetSHA256 string              `json:"identityAssetSha256"`
	Packages            []buildManifestHash `json:"packages"`
	Artifacts           []buildManifestHash `json:"artifacts"`
}

func loadBuildManifest(path string) (*buildManifest, error) {
	data, err := readBoundedFile(path, "build manifest", 1<<20)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var manifest buildManifest
	if err := decodeStrictJSON(data, &manifest); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if manifest.Format != 1 || manifest.Kind != "build-manifest" || manifest.Variant == "" {
		return nil, fmt.Errorf("%s: unsupported build manifest", path)
	}
	namespace, err := hex.DecodeString(manifest.SymbolNamespace)
	if err != nil || len(namespace) != 16 || manifest.SymbolNamespace != strings.ToLower(manifest.SymbolNamespace) {
		return nil, fmt.Errorf("%s: invalid manifest namespace", path)
	}
	if manifest.MappingSHA256 != "" && !isSHA256(manifest.MappingSHA256) {
		return nil, fmt.Errorf("%s: invalid mapping digest", path)
	}
	if !isSHA256(manifest.IdentityAssetSHA256) {
		return nil, fmt.Errorf("%s: invalid asset digest", path)
	}
	if len(manifest.Packages) == 0 || len(manifest.Packages) > 1024 || len(manifest.Artifacts) == 0 || len(manifest.Artifacts) > 32 {
		return nil, fmt.Errorf("%s: invalid manifest cardinality", path)
	}
	for _, entries := range [][]buildManifestHash{manifest.Packages, manifest.Artifacts} {
		seen := map[string]bool{}
		for _, entry := range entries {
			if entry.File == "" || entry.File == "." || entry.File == ".." || strings.ContainsAny(entry.File, "/\\\x00") || len(entry.File) > 1024 || !isSHA256(entry.SHA256) || seen[entry.File] {
				return nil, fmt.Errorf("%s: invalid or duplicate manifest entry %q", path, entry.File)
			}
			seen[entry.File] = true
		}
	}
	state := "mapped"
	if manifest.MappingSHA256 == "" {
		state = "unminified"
	}
	asset := fmt.Sprintf("schema=1\nstate=%s\nmapping-sha256=%s\nsymbol-namespace=%s\n", state, manifest.MappingSHA256, manifest.SymbolNamespace)
	digest := sha256.Sum256([]byte(asset))
	if hex.EncodeToString(digest[:]) != manifest.IdentityAssetSHA256 {
		return nil, fmt.Errorf("%s: identity asset SHA-256 mismatch", path)
	}
	return &manifest, nil
}

func isSHA256(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && value == strings.ToLower(value)
}

func (options Options) validateArtifactIdentities(inputs []SessionInput) (ArtifactIdentityEvidence, error) {
	evidence := ArtifactIdentityEvidence{Status: "not_requested"}
	sources := map[string]artifactSourceIdentity{}
	if options.ClassGraph != nil {
		sources["class-graph.jsonl"] = options.ClassGraph.sourceIdentity
	}
	if options.InstrumentationDiagnostics != nil {
		sources["instrumentation-diagnostics.jsonl"] = options.InstrumentationDiagnostics.sourceIdentity
	}
	if options.DependencyInjectionCatalog != nil {
		sources["di-catalog.jsonl"] = options.DependencyInjectionCatalog.sourceIdentity
	}
	if options.AndroidComponentCatalog != nil {
		sources["android-components-catalog.jsonl"] = options.AndroidComponentCatalog.sourceIdentity
	}
	if options.LambdaCaptures != nil {
		sources["lambda-captures.jsonl"] = options.LambdaCaptures.sourceIdentity
	}
	for name, source := range sources {
		verified, matched := false, false
		var mismatch error
		if source.path != "" {
			for _, kind := range []string{"apk", "aab"} {
				manifest, err := loadBuildManifest(filepath.Join(filepath.Dir(source.path), "build-manifest-"+kind+".json"))
				if err != nil {
					mismatch = err
					continue
				}
				if manifest == nil {
					continue
				}
				bound, err := matchArtifactManifest(manifest, name, source, inputs)
				if err != nil {
					mismatch = err
					continue
				}
				matched = true
				verified = verified || bound
			}
		}
		// APK and AAB may have been packaged in different builds. A stale sibling manifest
		// is not the selected build; require at least one complete match for these parsed bytes.
		if !matched && mismatch != nil {
			return evidence, mismatch
		}
		if verified {
			evidence.VerifiedArtifacts++
		} else {
			evidence.UnverifiedArtifacts++
		}
	}
	if len(sources) > 0 {
		evidence.Status = "mapping_bound"
		if evidence.UnverifiedArtifacts > 0 {
			evidence.Status = "unverified"
		}
	}
	return evidence, nil
}

func matchArtifactManifest(manifest *buildManifest, name string, source artifactSourceIdentity, inputs []SessionInput) (bool, error) {
	expected := ""
	for _, entry := range manifest.Artifacts {
		if entry.File == name {
			expected = entry.SHA256
			break
		}
	}
	if expected == "" || source.digest != expected {
		return false, fmt.Errorf("artifact SHA-256 mismatch for %q", source.path)
	}
	allMapped := len(inputs) > 0
	namespace, _ := hex.DecodeString(manifest.SymbolNamespace)
	for _, input := range inputs {
		if len(input.Header.SymbolNamespace) > 0 && !bytes.Equal(namespace, input.Header.SymbolNamespace) {
			return false, fmt.Errorf("artifact namespace mismatch for %q", input.Path)
		}
		identity := input.Header.BuildIdentity
		if input.Header.Schema == jhlog.HeaderSchemaV3 && identity.State == jhlog.BuildIdentityMapped {
			if identity.MappingSHA256 != manifest.MappingSHA256 {
				return false, fmt.Errorf("artifact mapping identity mismatch for %q", input.Path)
			}
		} else {
			allMapped = false
			if identity.State == jhlog.BuildIdentityUnminified && manifest.MappingSHA256 != "" {
				return false, fmt.Errorf("mapped artifacts do not match unminified log %q", input.Path)
			}
		}
	}
	return allMapped, nil
}
