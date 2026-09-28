package analyze

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

func TestComparisonCorpusManifestHash(t *testing.T) {
	payload, err := os.ReadFile("testdata/comparison/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(payload)
	if got, want := hex.EncodeToString(digest[:]), "9e5992cfd26a4cfc83a56bcc3a9a55636a304db031ffc1b69da0223db895f8f6"; got != want {
		t.Fatalf("comparison corpus hash=%s, want %s; update the reviewed manifest intentionally", got, want)
	}
}

type comparisonCorpus struct {
	Schema string                 `json:"schema"`
	Cases  []comparisonCorpusCase `json:"cases"`
}

type comparisonCorpusCase struct {
	Name          string                `json:"name"`
	Baseline      []comparisonCorpusRow `json:"baseline"`
	Candidate     []comparisonCorpusRow `json:"candidate"`
	Comparability ScenarioComparability `json:"comparability"`
	Outcome       ComparisonChange      `json:"outcome"`
}

type comparisonCorpusRow struct {
	Operation string   `json:"operation"`
	Screen    string   `json:"screen"`
	Count     uint64   `json:"count"`
	Failures  uint64   `json:"failures"`
	TotalMS   uint64   `json:"total_ms"`
	Steps     []string `json:"steps"`
}

func TestComparisonCorpusExpectedOutcomes(t *testing.T) {
	payload, err := os.ReadFile("testdata/comparison/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus comparisonCorpus
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&corpus); err != nil {
		t.Fatal(err)
	}
	if corpus.Schema != "jankhunter.comparison-corpus/v1" || len(corpus.Cases) < 4 {
		t.Fatalf("invalid corpus header: %+v", corpus)
	}
	for _, test := range corpus.Cases {
		t.Run(test.Name, func(t *testing.T) {
			comparison := Compare(corpusSummary(test.Baseline, true), corpusSummary(test.Candidate, false))
			if comparison.Scope.Comparability != test.Comparability || comparison.Outcome != test.Outcome {
				t.Fatalf("got scope=%s outcome=%s, want %s/%s", comparison.Scope.Comparability, comparison.Outcome, test.Comparability, test.Outcome)
			}
		})
	}
}

func corpusSummary(rows []comparisonCorpusRow, baseline bool) Summary {
	run, process := "02000000000000000000000000000000", "22000000000000000000000000000000"
	if baseline {
		run, process = "01000000000000000000000000000000", "11000000000000000000000000000000"
	}
	profiles := make([]OperationProfile, len(rows))
	for i, row := range rows {
		steps := make([]OperationProfileStep, len(row.Steps))
		for j, step := range row.Steps {
			steps[j] = OperationProfileStep{Operation: step, Kind: "stage", Screen: row.Screen}
		}
		profiles[i] = OperationProfile{
			Root: true, RunID: run, ProcessInstanceID: process, ProcessName: "com.example.app",
			Stats: OperationStats{Operation: row.Operation, Kind: "user", Screen: row.Screen, Count: row.Count, Success: row.Count - row.Failures, Failures: row.Failures, TotalMS: row.TotalMS},
			Steps: steps, Attributes: []OperationProfileAttribute{{Key: "cache", Value: "cold"}},
		}
	}
	return Summary{
		OperationAnalysis: &OperationAnalysis{Profiles: profiles},
		CollectionQuality: CollectionQuality{Complete: true, DiagnosticCompletenessPercent: 100},
		CollectorSessions: 1, CollectorFlagsAll: ^uint64(0),
		Devices: []NamedValue{{Name: "pixel", Value: 1}}, SDKs: []NamedValue{{Name: "35", Value: 1}},
		Network: []NamedValue{{Name: "wifi", Value: 1}}, Processes: []NamedValue{{Name: "com.example.app", Value: 1}},
		Detectors: []DetectorMetadata{{ID: "operation.failure", Version: "1"}},
	}
}

func TestComparisonCorpusFiveProblemTransitions(t *testing.T) {
	baseline, candidate := corpusSummary(nil, true), corpusSummary(nil, false)
	baseline.CategoryCoverage = []CategoryCoverage{{Category: ProblemCategoryUI, Status: "problems_found"}}
	candidate.CategoryCoverage = []CategoryCoverage{{Category: ProblemCategoryUI, Status: "problems_found"}}
	baselineProblems := make([]ProblemFinding, 5)
	candidateProblems := make([]ProblemFinding, 0, 3)
	for i := range baselineProblems {
		operation := fmt.Sprintf("scenario.%d", i)
		beforeProfile := matchTestProfile(operation, "01000000000000000000000000000000", 100)
		afterProfile := matchTestProfile(operation, "02000000000000000000000000000000", 100)
		beforeProfile.ProcessName, afterProfile.ProcessName = "com.example.app", "com.example.app"
		beforeProfile.ProcessInstanceID, afterProfile.ProcessInstanceID = "11000000000000000000000000000000", "22000000000000000000000000000000"
		baseline.OperationAnalysis.Profiles = append(baseline.OperationAnalysis.Profiles, beforeProfile)
		candidate.OperationAnalysis.Profiles = append(candidate.OperationAnalysis.Profiles, afterProfile)
		location := ProblemLocation{Screen: "Chat", Operation: operation}
		baselineProblems[i] = comparisonProblemFinding(fmt.Sprintf("problem-%d", i), "1", 50, location)
	}
	baselineProblems[0].Frequency, baselineProblems[1].Frequency = &ProblemFrequency{Count: 5}, &ProblemFrequency{Count: 5}
	ratesBefore := []float64{0.10, 0.10, 0.10}
	ratesAfter := []float64{0.05, 0.10, 0.20}
	for i := 2; i < 5; i++ {
		baselineProblems[i].Frequency = &ProblemFrequency{Count: 10, RatePerSec: &ratesBefore[i-2]}
		finding := baselineProblems[i]
		finding.Frequency = &ProblemFrequency{Count: uint64(ratesAfter[i-2] * 100), RatePerSec: &ratesAfter[i-2]}
		candidateProblems = append(candidateProblems, finding)
	}
	baseline.Problems, candidate.Problems = baselineProblems, candidateProblems
	comparison := Compare(baseline, candidate)
	counts := map[ComparisonChange]int{}
	for _, delta := range comparison.ProblemComparison.Deltas {
		counts[delta.Change]++
	}
	if len(comparison.ProblemComparison.Deltas) != 5 || counts[ChangeImproved] != 3 || counts[ChangeUnchanged] != 1 || counts[ChangeRegressed] != 1 || comparison.Outcome != ChangeMixed {
		t.Fatalf("five-problem transition matrix lost: outcome=%s counts=%v deltas=%+v", comparison.Outcome, counts, comparison.ProblemComparison.Deltas)
	}
}
