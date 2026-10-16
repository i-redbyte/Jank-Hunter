package analyze

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIdentityMigrationIsStrictCanonicalAndOrderIndependent(t *testing.T) {
	dir := t.TempDir()
	firstPath := filepath.Join(dir, "first.json")
	secondPath := filepath.Join(dir, "second.json")
	first := "{\"schema\":\"jankhunter.identity-migration/v1\",\"scenarios\":[{\"baseline\":\"chat.old\",\"candidate\":\"chat.new\"}],\"operations\":[{\"baseline\":\"load.old\",\"candidate\":\"load.new\"}],\"problems\":[{\"baseline\":\"problem.old\",\"candidate\":\"problem.new\"}]}"
	second := "{\"schema\":\"jankhunter.identity-migration/v1\",\"problems\":[{\"baseline\":\"problem.old\",\"candidate\":\"problem.new\"}],\"operations\":[{\"baseline\":\"load.old\",\"candidate\":\"load.new\"}],\"scenarios\":[{\"baseline\":\"chat.old\",\"candidate\":\"chat.new\"}]}"
	if err := os.WriteFile(firstPath, []byte(first), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secondPath, []byte(second), 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := LoadIdentityMigration(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	b, err := LoadIdentityMigration(secondPath)
	if err != nil {
		t.Fatal(err)
	}
	if a.SHA256 == "" || a.SHA256 != b.SHA256 {
		t.Fatalf("digest mismatch: %q %q", a.SHA256, b.SHA256)
	}
	invalid := []string{
		"{\"schema\":\"jankhunter.identity-migration/v1\",\"scenarios\":[{\"baseline\":\"a\",\"candidate\":\"b\"},{\"baseline\":\"a\",\"candidate\":\"c\"}]}",
		"{\"schema\":\"jankhunter.identity-migration/v1\",\"operations\":[{\"baseline\":\"a\",\"candidate\":\"b\"},{\"baseline\":\"c\",\"candidate\":\"b\"}]}",
		"{\"schema\":\"jankhunter.identity-migration/v1\",\"scenarios\":[{\"baseline\":\"a\",\"candidate\":\"b\"},{\"baseline\":\"b\",\"candidate\":\"a\"}]}",
	}
	for index, payload := range invalid {
		path := filepath.Join(dir, idHex(byte(index+1))+".json")
		if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadIdentityMigration(path); err == nil {
			t.Fatalf("invalid migration accepted: %s", payload)
		}
	}
}

func TestIdentityMigrationMatchesRenamedScenarioOperationAndProblem(t *testing.T) {
	baseline, candidate := scopedComparisonPair()
	baseline.OperationAnalysis.Profiles[0].Stats.Operation = "chat.old"
	baseline.OperationAnalysis.Profiles[0].Steps[0].Operation = "load.old"
	baseline.OperationAnalysis.Profiles[0].Attributes = append(baseline.OperationAnalysis.Profiles[0].Attributes, OperationProfileAttribute{Key: "jh.scenario", Value: "scenario.old"})
	candidate.OperationAnalysis.Profiles[0].Stats.Operation = "chat.new"
	candidate.OperationAnalysis.Profiles[0].Steps[0].Operation = "load.new"
	candidate.OperationAnalysis.Profiles[0].Attributes = append(candidate.OperationAnalysis.Profiles[0].Attributes, OperationProfileAttribute{Key: "jh.scenario", Value: "scenario.new"})
	baseline.Problems = []ProblemFinding{{Fingerprint: "problem.old", DetectorID: "operation.failure", Category: ProblemCategoryOperations}}
	candidate.Problems = []ProblemFinding{{Fingerprint: "problem.new", DetectorID: "operation.failure", Category: ProblemCategoryOperations}}
	migration := &IdentityMigration{
		Schema:     IdentityMigrationSchemaVersion,
		Scenarios:  []IdentityAlias{{Baseline: "scenario.old", Candidate: "scenario.new"}},
		Operations: []IdentityAlias{{Baseline: "chat.old", Candidate: "chat.new"}, {Baseline: "load.old", Candidate: "load.new"}},
		Problems:   []IdentityAlias{{Baseline: "problem.old", Candidate: "problem.new"}},
		SHA256:     "digest",
	}
	comparison := CompareWithOptions(baseline, candidate, ComparisonOptions{IdentityMigration: migration})
	if err := ValidateIdentityMigration(migration, baseline, candidate); err != nil {
		t.Fatal(err)
	}
	if comparison.Scope.Comparability != ScenarioFull || comparison.IdentityMigrationSHA256 != "digest" {
		t.Fatalf("migration not applied: %+v", comparison.Scope)
	}
	if len(comparison.ProblemComparison.Deltas) != 1 ||
		len(comparison.ProblemComparison.Deltas[0].BaselineChildren) != 1 ||
		len(comparison.ProblemComparison.Deltas[0].CandidateChildren) != 1 {
		t.Fatalf("problem migration not applied: %+v", comparison.ProblemComparison)
	}
}

func TestIdentityMigrationRejectsMissingEntities(t *testing.T) {
	baseline, candidate := scopedComparisonPair()
	migration := &IdentityMigration{Schema: IdentityMigrationSchemaVersion, Scenarios: []IdentityAlias{{Baseline: "missing", Candidate: "also-missing"}}}
	if err := ValidateIdentityMigration(migration, baseline, candidate); err == nil {
		t.Fatal("missing identities accepted")
	}
}
