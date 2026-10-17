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

const IdentityMigrationSchemaVersion = "jankhunter.identity-migration/v1"
const identityMigrationFileLimit = 1 << 20
const identityMigrationEntryLimit = 1024

type IdentityAlias struct {
	Baseline  string `json:"baseline"`
	Candidate string `json:"candidate"`
}

type IdentityMigration struct {
	Schema     string          `json:"schema"`
	Scenarios  []IdentityAlias `json:"scenarios,omitempty"`
	Operations []IdentityAlias `json:"operations,omitempty"`
	Problems   []IdentityAlias `json:"problems,omitempty"`
	SHA256     string          `json:"-"`
}

func LoadIdentityMigration(path string) (*IdentityMigration, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	payload, err := readBoundedFile(path, "identity migration", identityMigrationFileLimit)
	if err != nil {
		return nil, fmt.Errorf("read identity migration: %w", err)
	}
	var migration IdentityMigration
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&migration); err != nil {
		return nil, fmt.Errorf("decode identity migration: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("decode identity migration: trailing data")
	}
	if migration.Schema != IdentityMigrationSchemaVersion ||
		len(migration.Scenarios)+len(migration.Operations)+len(migration.Problems) > identityMigrationEntryLimit {
		return nil, fmt.Errorf("unsupported identity migration schema or cardinality")
	}
	for name, entries := range map[string][]IdentityAlias{
		"scenario": migration.Scenarios, "operation": migration.Operations, "problem": migration.Problems,
	} {
		if err := validateIdentityAliases(name, entries); err != nil {
			return nil, err
		}
	}
	sortIdentityAliases(migration.Scenarios)
	sortIdentityAliases(migration.Operations)
	sortIdentityAliases(migration.Problems)
	canonical, _ := json.Marshal(struct {
		Schema     string          `json:"schema"`
		Scenarios  []IdentityAlias `json:"scenarios,omitempty"`
		Operations []IdentityAlias `json:"operations,omitempty"`
		Problems   []IdentityAlias `json:"problems,omitempty"`
	}{migration.Schema, migration.Scenarios, migration.Operations, migration.Problems})
	digest := sha256.Sum256(canonical)
	migration.SHA256 = hex.EncodeToString(digest[:])
	return &migration, nil
}

func validateIdentityAliases(kind string, entries []IdentityAlias) error {
	left := make(map[string]struct{}, len(entries))
	right := make(map[string]struct{}, len(entries))
	edges := make(map[string]string, len(entries))
	for index := range entries {
		entry := &entries[index]
		entry.Baseline, entry.Candidate = strings.TrimSpace(entry.Baseline), strings.TrimSpace(entry.Candidate)
		if !validProblemAliasValue(entry.Baseline) || !validProblemAliasValue(entry.Candidate) || entry.Baseline == entry.Candidate {
			return fmt.Errorf("invalid %s identity alias at index %d", kind, index)
		}
		if _, exists := left[entry.Baseline]; exists {
			return fmt.Errorf("duplicate baseline %s identity %q", kind, entry.Baseline)
		}
		if _, exists := right[entry.Candidate]; exists {
			return fmt.Errorf("duplicate candidate %s identity %q", kind, entry.Candidate)
		}
		left[entry.Baseline], right[entry.Candidate], edges[entry.Baseline] = struct{}{}, struct{}{}, entry.Candidate
	}
	for start := range edges {
		seen := make(map[string]struct{}, len(entries))
		for current := start; current != ""; current = edges[current] {
			if _, exists := seen[current]; exists {
				return fmt.Errorf("cyclic %s identity migration", kind)
			}
			seen[current] = struct{}{}
		}
	}
	return nil
}

func sortIdentityAliases(entries []IdentityAlias) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Baseline != entries[j].Baseline {
			return entries[i].Baseline < entries[j].Baseline
		}
		return entries[i].Candidate < entries[j].Candidate
	})
}

func problemAliasesFromMigration(migration *IdentityMigration) *ProblemAliases {
	if migration == nil || len(migration.Problems) == 0 {
		return nil
	}
	entries := make([]ProblemAlias, len(migration.Problems))
	for index, entry := range migration.Problems {
		entries[index] = ProblemAlias(entry)
	}
	return &ProblemAliases{Schema: ProblemAliasesSchemaVersion, Entries: entries, SHA256: migration.SHA256}
}

func migratedBaselineAnalysis(source *OperationAnalysis, migration *IdentityMigration) *OperationAnalysis {
	if source == nil || migration == nil || len(migration.Scenarios)+len(migration.Operations) == 0 {
		return source
	}
	result := *source
	result.Profiles = append([]OperationProfile(nil), source.Profiles...)
	scenarios := identityAliasMap(migration.Scenarios)
	operations := identityAliasMap(migration.Operations)
	for index := range result.Profiles {
		profile := &result.Profiles[index]
		profile.Attributes = append([]OperationProfileAttribute(nil), profile.Attributes...)
		profile.Steps = append([]OperationProfileStep(nil), profile.Steps...)
		if replacement := operations[profile.Stats.Operation]; replacement != "" {
			profile.Stats.Operation = replacement
		}
		for attributeIndex := range profile.Attributes {
			attribute := &profile.Attributes[attributeIndex]
			if attribute.Key == "jh.scenario" {
				if replacement := scenarios[attribute.Value]; replacement != "" {
					attribute.Value = replacement
				}
			}
		}
		for stepIndex := range profile.Steps {
			step := &profile.Steps[stepIndex]
			if replacement := operations[step.Operation]; replacement != "" {
				step.Operation = replacement
				step.Stats.Operation = replacement
			}
		}
	}
	return &result
}

func identityAliasMap(entries []IdentityAlias) map[string]string {
	if len(entries) == 0 {
		return nil
	}
	result := make(map[string]string, len(entries))
	for _, entry := range entries {
		result[entry.Baseline] = entry.Candidate
	}
	return result
}

func ValidateIdentityMigration(migration *IdentityMigration, baseline, candidate Summary) error {
	if migration == nil {
		return nil
	}
	checks := []struct {
		kind        string
		entries     []IdentityAlias
		left, right map[string]struct{}
	}{
		{"scenario", migration.Scenarios, summaryScenarioIdentities(baseline), summaryScenarioIdentities(candidate)},
		{"operation", migration.Operations, summaryOperationIdentities(baseline), summaryOperationIdentities(candidate)},
		{"problem", migration.Problems, summaryProblemIdentities(baseline), summaryProblemIdentities(candidate)},
	}
	for _, check := range checks {
		for _, entry := range check.entries {
			if _, ok := check.left[entry.Baseline]; !ok {
				return fmt.Errorf("baseline %s identity %q is absent", check.kind, entry.Baseline)
			}
			if _, ok := check.right[entry.Candidate]; !ok {
				return fmt.Errorf("candidate %s identity %q is absent", check.kind, entry.Candidate)
			}
		}
	}
	return nil
}

func summaryScenarioIdentities(summary Summary) map[string]struct{} {
	result := make(map[string]struct{})
	if summary.OperationAnalysis == nil {
		return result
	}
	for _, profile := range summary.OperationAnalysis.Profiles {
		for _, attribute := range profile.Attributes {
			if attribute.Key == "jh.scenario" {
				result[attribute.Value] = struct{}{}
			}
		}
	}
	return result
}

func summaryOperationIdentities(summary Summary) map[string]struct{} {
	result := make(map[string]struct{})
	if summary.OperationAnalysis == nil {
		return result
	}
	for _, profile := range summary.OperationAnalysis.Profiles {
		result[profile.Stats.Operation] = struct{}{}
		for _, step := range profile.Steps {
			result[step.Operation] = struct{}{}
		}
	}
	return result
}

func summaryProblemIdentities(summary Summary) map[string]struct{} {
	result := make(map[string]struct{}, len(summary.Problems))
	for _, problem := range summary.Problems {
		result[problem.Fingerprint] = struct{}{}
	}
	return result
}
