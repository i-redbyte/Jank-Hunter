package analyze

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

const ProblemAliasesSchemaVersion = "jankhunter.problem-aliases/v1"
const problemAliasesFileLimit = 1 << 20
const problemAliasesEntryLimit = 1024
const problemAliasValueLimit = 256

type ProblemAlias struct {
	Baseline  string `json:"baseline"`
	Candidate string `json:"candidate"`
}

type ProblemAliases struct {
	Schema  string         `json:"schema"`
	Entries []ProblemAlias `json:"aliases"`
	SHA256  string         `json:"-"`
}

func LoadProblemAliases(path string) (*ProblemAliases, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	payload, err := readBoundedFile(path, "problem aliases", problemAliasesFileLimit)
	if err != nil {
		return nil, fmt.Errorf("read problem aliases: %w", err)
	}
	var aliases ProblemAliases
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&aliases); err != nil {
		return nil, fmt.Errorf("decode problem aliases: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("decode problem aliases: trailing data")
	}
	if aliases.Schema != ProblemAliasesSchemaVersion || len(aliases.Entries) > problemAliasesEntryLimit {
		return nil, fmt.Errorf("unsupported problem aliases schema or cardinality")
	}
	baselineSeen := make(map[string]struct{}, len(aliases.Entries))
	candidateSeen := make(map[string]struct{}, len(aliases.Entries))
	for i := range aliases.Entries {
		entry := &aliases.Entries[i]
		entry.Baseline, entry.Candidate = strings.TrimSpace(entry.Baseline), strings.TrimSpace(entry.Candidate)
		if !validProblemAliasValue(entry.Baseline) || !validProblemAliasValue(entry.Candidate) {
			return nil, fmt.Errorf("invalid problem alias at index %d", i)
		}
		if _, exists := baselineSeen[entry.Baseline]; exists {
			return nil, fmt.Errorf("duplicate baseline problem alias %q", entry.Baseline)
		}
		if _, exists := candidateSeen[entry.Candidate]; exists {
			return nil, fmt.Errorf("duplicate candidate problem alias %q", entry.Candidate)
		}
		baselineSeen[entry.Baseline], candidateSeen[entry.Candidate] = struct{}{}, struct{}{}
	}
	sort.Slice(aliases.Entries, func(i, j int) bool {
		if aliases.Entries[i].Baseline != aliases.Entries[j].Baseline {
			return aliases.Entries[i].Baseline < aliases.Entries[j].Baseline
		}
		return aliases.Entries[i].Candidate < aliases.Entries[j].Candidate
	})
	canonical, _ := json.Marshal(struct {
		Schema  string         `json:"schema"`
		Aliases []ProblemAlias `json:"aliases"`
	}{Schema: aliases.Schema, Aliases: aliases.Entries})
	digest := sha256.Sum256(canonical)
	aliases.SHA256 = hex.EncodeToString(digest[:])
	return &aliases, nil
}

func validProblemAliasValue(value string) bool {
	if value == "" || len(value) > problemAliasValueLimit {
		return false
	}
	for i := range value {
		if value[i] < 0x20 || value[i] == 0x7f {
			return false
		}
	}
	return true
}
