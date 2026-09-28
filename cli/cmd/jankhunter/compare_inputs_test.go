package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/comparisoninput"
)

func TestCompareAcceptsReportAndMixedInputsWithPortableParity(t *testing.T) {
	directory := t.TempDir()
	baselineLog := filepath.Join(directory, "baseline.jhlog")
	if err := runSample([]string{"--out", baselineLog}); err != nil {
		t.Fatal(err)
	}
	candidateLog := copyFileForTest(t, baselineLog, filepath.Join(directory, "candidate.jhlog"))
	baselineReport := filepath.Join(directory, "baseline.html")
	candidateReport := filepath.Join(directory, "candidate.html")
	if err := runInspect([]string{baselineLog, "--out", baselineReport}); err != nil {
		t.Fatal(err)
	}
	if err := runInspect([]string{candidateLog, "--out", candidateReport}); err != nil {
		t.Fatal(err)
	}
	directBaseline, err := analyze.InspectFilesWithOptions("baseline", []string{baselineLog}, analyze.Options{})
	if err != nil {
		t.Fatal(err)
	}
	directCandidate, err := analyze.InspectFilesWithOptions("candidate", []string{candidateLog}, analyze.Options{})
	if err != nil {
		t.Fatal(err)
	}
	want := portableComparisonProjection(analyze.Compare(directBaseline, directCandidate))

	inputs := []struct {
		name string
		args []string
	}{
		{name: "report-report", args: []string{"--baseline-report", baselineReport, "--candidate-report", candidateReport}},
		{name: "report-log", args: []string{"--baseline-report", baselineReport, "--candidate", candidateLog}},
		{name: "log-report", args: []string{"--baseline", baselineLog, "--candidate-report", candidateReport}},
	}
	for _, input := range inputs {
		t.Run(input.name, func(t *testing.T) {
			out := filepath.Join(directory, input.name+".html")
			args := append(append([]string(nil), input.args...), "--out", out)
			if err := runCompare(args); err != nil {
				t.Fatal(err)
			}
			document, summaries, err := comparisoninput.ReadComparisonSnapshotDocument(out)
			if err != nil {
				t.Fatal(err)
			}
			if document.Kind != analyze.ComparisonSnapshotCompare || len(summaries) != 2 {
				t.Fatalf("comparison snapshot = %+v", document)
			}
			got := portableComparisonProjection(analyze.Compare(summaries[0], summaries[1]))
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("portable comparison changed\n got: %+v\nwant: %+v", got, want)
			}
		})
	}
}

func TestCompareReportFlagsRejectAmbiguousAndLegacyInputs(t *testing.T) {
	directory := t.TempDir()
	logPath := filepath.Join(directory, "sample.jhlog")
	if err := runSample([]string{"--out", logPath}); err != nil {
		t.Fatal(err)
	}
	inspectReport := filepath.Join(directory, "inspect.html")
	if err := runInspect([]string{logPath, "--out", inspectReport}); err != nil {
		t.Fatal(err)
	}
	invalidAliases := filepath.Join(directory, "invalid-aliases.json")
	if err := os.WriteFile(invalidAliases, []byte(`{"schema":"wrong","aliases":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	invalidMigration := filepath.Join(directory, "invalid-migration.json")
	if err := os.WriteFile(invalidMigration, []byte(`{"schema":"wrong"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	aliasCandidateLog := copyFileForTest(t, logPath, filepath.Join(directory, "alias-candidate.jhlog"))
	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{name: "mutually exclusive", args: []string{"--baseline", logPath, "--baseline-report", inspectReport, "--candidate-report", inspectReport}, want: "only one baseline input"},
		{name: "legacy", args: []string{"--baseline-report", filepath.Join(directory, "legacy.html"), "--candidate-report", inspectReport}, want: "does not contain a comparison snapshot"},
		{name: "invalid problem aliases", args: []string{"--baseline-report", inspectReport, "--candidate", aliasCandidateLog, "--problem-aliases", invalidAliases}, want: "problem aliases"},
		{name: "invalid identity migration", args: []string{"--baseline-report", inspectReport, "--candidate", aliasCandidateLog, "--identity-migration", invalidMigration}, want: "identity migration"},
		{name: "exclusive identity mappings", args: []string{"--baseline-report", inspectReport, "--candidate", aliasCandidateLog, "--identity-migration", invalidMigration, "--problem-aliases", invalidAliases}, want: "only one identity mapping"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.name == "legacy" {
				if err := os.WriteFile(filepath.Join(directory, "legacy.html"), []byte("<html><body>old report</body></html>"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := runCompare(test.args); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
	compareReport := filepath.Join(directory, "compare.html")
	candidate := copyFileForTest(t, logPath, filepath.Join(directory, "candidate.jhlog"))
	if err := runCompare([]string{"--baseline", logPath, "--candidate", candidate, "--out", compareReport}); err != nil {
		t.Fatal(err)
	}
	if err := runCompare([]string{"--baseline-report", compareReport, "--candidate-report", inspectReport}); err == nil || !strings.Contains(err.Error(), "compare report") {
		t.Fatalf("ambiguous compare report error = %v", err)
	}
}

type portableComparison struct {
	Schema   string
	Outcome  analyze.ComparisonChange
	Scope    analyze.ComparisonScope
	Deltas   []analyze.Delta
	Problems []portableProblemDelta
}

func portableComparisonProjection(value analyze.Comparison) portableComparison {
	result := portableComparison{Schema: value.SchemaVersion, Outcome: value.Outcome, Scope: value.Scope, Deltas: value.Deltas, Problems: make([]portableProblemDelta, len(value.ProblemComparison.Deltas))}
	for index, delta := range value.ProblemComparison.Deltas {
		result.Problems[index] = portableProblemDelta{Fingerprint: delta.Fingerprint, Status: delta.Status, Comparable: delta.Comparable, Observation: delta.Observation, Change: delta.Change, Evidence: delta.Evidence, Confidence: delta.Confidence}
	}
	return result
}

type portableProblemDelta struct {
	Fingerprint string
	Status      string
	Comparable  bool
	Observation analyze.ProblemObservation
	Change      analyze.ComparisonChange
	Evidence    analyze.ComparisonEvidence
	Confidence  analyze.ComparisonConfidence
}
