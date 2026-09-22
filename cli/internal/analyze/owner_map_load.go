package analyze

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

)

func LoadOwnerMap(path string) (*OwnerMap, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return loadOwnerMapJSONL(path, data)
}

// ReadOwnerMapNamespace validates and returns only the bounded first metadata record. Artifact
// discovery uses it to avoid loading every symbol entry from every build variant into memory.
func ReadOwnerMapNamespace(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		lineData := []byte(line)
		var envelope ownerMapEnvelope
		if err := json.Unmarshal(lineData, &envelope); err != nil {
			return nil, fmt.Errorf("parse owner map line %d: %w", lineNumber, err)
		}
		if err := validateOwnerMapFormat(path, envelope.Format); err != nil {
			return nil, fmt.Errorf("parse owner map line %d: %w", lineNumber, err)
		}
		if envelope.Kind != "metadata" {
			return nil, fmt.Errorf("%s: parse owner map line %d: metadata must be the first record", path, lineNumber)
		}
		var raw ownerMapMetadataRecord
		if err := decodeOwnerMapRecord(lineData, &raw); err != nil {
			return nil, fmt.Errorf("%s: parse owner map line %d: %w", path, lineNumber, err)
		}
		namespace, err := decodeOwnerMapNamespace(raw.SymbolNamespace)
		if err != nil {
			return nil, fmt.Errorf("%s: parse owner map line %d: %w", path, lineNumber, err)
		}
		return namespace, nil
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("%s: owner map has no metadata record", path)
}

// LoadOwnerMaps loads and combines module-local owner maps into the single
// process-wide stable-symbol namespace used by a .jhlog session. Every map is
// validated independently before it participates in the merge.
func LoadOwnerMaps(paths []string) (*OwnerMap, error) {
	if len(paths) == 0 {
		return nil, nil
	}

	merged := &OwnerMap{Entries: make(map[string]string)}
	entrySources := make(map[string]string)
	namespaceSource := ""
	for _, path := range paths {
		if path == "" {
			return nil, fmt.Errorf("owner map path must not be empty")
		}
		ownerMap, err := LoadOwnerMap(path)
		if err != nil {
			return nil, fmt.Errorf("load owner map %q: %w", path, err)
		}
		if ownerMap == nil {
			return nil, fmt.Errorf("load owner map %q: empty owner map", path)
		}
		if namespaceSource == "" {
			merged.SymbolNamespace = append([]byte(nil), ownerMap.SymbolNamespace...)
			namespaceSource = path
		} else if !bytes.Equal(merged.SymbolNamespace, ownerMap.SymbolNamespace) {
			return nil, fmt.Errorf(
				"owner maps %q and %q use different symbolNamespace values: %s and %s",
				namespaceSource,
				path,
				hexOrEmpty(merged.SymbolNamespace),
				hexOrEmpty(ownerMap.SymbolNamespace),
			)
		}

		ids := make([]string, 0, len(ownerMap.Entries))
		for id := range ownerMap.Entries {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			owner := ownerMap.Entries[id]
			if existing, ok := merged.Entries[id]; ok {
				if existing != owner {
					return nil, fmt.Errorf(
						"owner maps %q and %q contain conflicting stable ID %q: %q and %q",
						entrySources[id],
						path,
						id,
						existing,
						owner,
					)
				}
				continue
			}
			merged.Entries[id] = owner
			entrySources[id] = path
		}
	}
	if err := validateLoadedOwnerMap(merged); err != nil {
		return nil, fmt.Errorf("merge owner maps: %w", err)
	}
	return merged, nil
}

type ownerMapEnvelope struct {
	Format int    `json:"format"`
	Kind   string `json:"kind"`
}

type ownerMapMetadataRecord struct {
	Format                  int             `json:"format"`
	Kind                    string          `json:"kind"`
	Variant                 string          `json:"variant"`
	IDAlgorithm             string          `json:"idAlgorithm"`
	IDEncoding              string          `json:"idEncoding"`
	GeneratedOwners         bool            `json:"generatedOwners"`
	SymbolNamespace         string          `json:"symbolNamespace"`
	IncludeWholeApplication bool            `json:"includeWholeApplication"`
	Hooks                   map[string]bool `json:"hooks"`
	AndroidNamespace        string          `json:"androidNamespace"`
	IncludePackages         []string        `json:"includePackages"`
	ExcludePackages         []string        `json:"excludePackages"`
}

type ownerMapEntryRecord struct {
	Format     int    `json:"format"`
	Kind       string `json:"kind"`
	ID         string `json:"id"`
	Owner      string `json:"owner"`
	ClassName  string `json:"class"`
	MethodName string `json:"method"`
	Descriptor string `json:"descriptor"`
}

func loadOwnerMapJSONL(path string, data []byte) (*OwnerMap, error) {
	out := &OwnerMap{Entries: map[string]string{}}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	lineNumber := 0
	recordNumber := 0
	metadataSeen := false
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		recordNumber++
		lineData := []byte(line)
		var envelope ownerMapEnvelope
		if err := json.Unmarshal(lineData, &envelope); err != nil {
			return nil, fmt.Errorf("parse owner map line %d: %w", lineNumber, err)
		}
		if err := validateOwnerMapFormat(path, envelope.Format); err != nil {
			return nil, fmt.Errorf("parse owner map line %d: %w", lineNumber, err)
		}
		switch envelope.Kind {
		case "metadata":
			if metadataSeen || recordNumber != 1 {
				return nil, fmt.Errorf("%s: parse owner map line %d: metadata must be the first and only metadata record", path, lineNumber)
			}
			var raw ownerMapMetadataRecord
			if err := decodeOwnerMapRecord(lineData, &raw); err != nil {
				return nil, fmt.Errorf("%s: parse owner map line %d: %w", path, lineNumber, err)
			}
			if err := addOwnerMapMetadata(out, raw); err != nil {
				return nil, fmt.Errorf("%s: parse owner map line %d: %w", path, lineNumber, err)
			}
			metadataSeen = true
		case "entry":
			if !metadataSeen {
				return nil, fmt.Errorf("%s: parse owner map line %d: entry appears before metadata", path, lineNumber)
			}
			var raw ownerMapEntryRecord
			if err := decodeOwnerMapRecord(lineData, &raw); err != nil {
				return nil, fmt.Errorf("%s: parse owner map line %d: %w", path, lineNumber, err)
			}
			if err := addOwnerMapEntry(out.Entries, raw); err != nil {
				return nil, fmt.Errorf("%s: parse owner map line %d: %w", path, lineNumber, err)
			}
		default:
			return nil, fmt.Errorf("%s: parse owner map line %d: unsupported record kind %q", path, lineNumber, envelope.Kind)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if err := validateLoadedOwnerMap(out); err != nil {
		return nil, fmt.Errorf("%s: parse owner map: %w", path, err)
	}
	return out, nil
}

func decodeOwnerMapRecord(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func addOwnerMapMetadata(out *OwnerMap, raw ownerMapMetadataRecord) error {
	decoded, err := decodeOwnerMapNamespace(raw.SymbolNamespace)
	if err != nil {
		return err
	}
	if len(out.SymbolNamespace) > 0 && !bytes.Equal(out.SymbolNamespace, decoded) {
		return fmt.Errorf("conflicting symbolNamespace metadata")
	}
	out.SymbolNamespace = decoded
	return nil
}

func decodeOwnerMapNamespace(value string) ([]byte, error) {
	if value == "" {
		return nil, fmt.Errorf("metadata record has no symbolNamespace")
	}
	if len(value) != ownerMapNamespaceBytes*2 {
		return nil, fmt.Errorf("symbolNamespace must contain exactly %d lowercase hexadecimal bytes", ownerMapNamespaceBytes)
	}
	decoded, err := hex.DecodeString(value)
	if err != nil || hex.EncodeToString(decoded) != value {
		return nil, fmt.Errorf("symbolNamespace must contain lowercase hexadecimal bytes")
	}
	return decoded, nil
}

func validateLoadedOwnerMap(ownerMap *OwnerMap) error {
	if len(ownerMap.SymbolNamespace) != ownerMapNamespaceBytes {
		return fmt.Errorf("owner map metadata symbolNamespace must contain exactly %d bytes", ownerMapNamespaceBytes)
	}
	return nil
}

const ownerMapNamespaceBytes = 16

func addOwnerMapEntry(out map[string]string, entry ownerMapEntryRecord) error {
	if !isCanonicalStableOwnerID(entry.ID) {
		return fmt.Errorf("owner map id %q is not canonical; expected stable:0x followed by 16 lowercase hexadecimal digits", entry.ID)
	}
	name := strings.TrimSpace(entry.Owner)
	if name == "" {
		return fmt.Errorf("owner map entry %q has no owner", entry.ID)
	}
	if existing, ok := out[entry.ID]; ok {
		if existing != name {
			return fmt.Errorf("conflicting owner map entry %q: %q and %q", entry.ID, existing, name)
		}
		return nil
	}
	out[entry.ID] = name
	return nil
}

func validateOwnerMapFormat(path string, got int) error {
	return validateArtifactFormat(path, "owner map", got, OwnerMapFormat)
}
