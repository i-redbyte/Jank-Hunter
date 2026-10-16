package analyze

import "fmt"

// ReadArtifactMetadataNamespace validates symbol compatibility, not build identity.
// Exact build binding additionally requires the post-packaging manifest and mapping digest.
func ReadArtifactMetadataNamespace(path string) ([]byte, error) {
	data, err := readBoundedFile(path, "artifact metadata", maxArtifactMetadataBytes)
	if err != nil {
		return nil, err
	}
	var metadata artifactMetadataRecord
	if err := decodeStrictJSON(data, &metadata); err != nil {
		return nil, fmt.Errorf("%s: parse artifact metadata: %w", path, err)
	}
	if err := validateArtifactFormat(path, "artifact metadata", metadata.Format, ArtifactMetadataFormat); err != nil {
		return nil, err
	}
	if metadata.Kind != "artifact-metadata" {
		return nil, fmt.Errorf("%s: artifact metadata kind must be %q", path, "artifact-metadata")
	}
	namespace, err := decodeSymbolNamespace(metadata.SymbolNamespace)
	if err != nil {
		return nil, fmt.Errorf("%s: parse artifact metadata: %w", path, err)
	}
	return namespace, nil
}

type artifactMetadataRecord struct {
	MetadataCompatibilityMode string          `json:"metadataCompatibilityMode,omitempty"`
	MetadataReaderIdentity    string          `json:"metadataReaderIdentity,omitempty"`
	Format                    int             `json:"format"`
	Kind                      string          `json:"kind"`
	Variant                   string          `json:"variant"`
	IDAlgorithm               string          `json:"idAlgorithm"`
	IDEncoding                string          `json:"idEncoding"`
	SymbolNamespace           string          `json:"symbolNamespace"`
	IncludeWholeApplication   bool            `json:"includeWholeApplication"`
	NetworkWholeApplication   bool            `json:"networkWholeApplication"`
	DatabaseWholeApplication  bool            `json:"databaseWholeApplication"`
	Hooks                     map[string]bool `json:"hooks"`
	AndroidNamespace          string          `json:"androidNamespace"`
	IncludePackages           []string        `json:"includePackages"`
	ExcludePackages           []string        `json:"excludePackages"`
}
