package analyze

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func comparisonProblemFinding(fingerprint, version string, priority int, locations ...ProblemLocation) ProblemFinding {
	return ProblemFinding{Fingerprint: fingerprint, ID: fingerprint, DetectorID: "ui.test", DetectorVersion: version, Category: ProblemCategoryUI, Severity: "high", Confidence: "high", InvestigationPriority: priority, Title: fingerprint, Where: locations}
}

func comparisonProblemSummary(run, process, operation string, findings ...ProblemFinding) Summary {
	summary := comparisonScopeInput(run, process, "com.example.app")
	summary.OperationAnalysis.Profiles[0].Stats.Operation = operation
	summary.Problems = findings
	summary.CategoryCoverage = []CategoryCoverage{{Category: ProblemCategoryUI, Status: "problems_found"}}
	summary.Detectors = []DetectorMetadata{{ID: "ui.test", Version: "1"}}
	return summary
}

func TestProblemMatchingSurvivesCodeLocationAndPriorityChange(t *testing.T) {
	before := comparisonProblemFinding("old-fingerprint", "1", 10, ProblemLocation{Screen: "Chat", Operation: "chat.open", Class: "OldPresenter", Method: "render"})
	after := comparisonProblemFinding("new-fingerprint", "1", 99, ProblemLocation{Screen: "Chat", Operation: "chat.open", Class: "NewPresenter", Method: "bind"})
	baseline := comparisonProblemSummary("01000000000000000000000000000000", "11000000000000000000000000000000", "chat.open", before)
	candidate := comparisonProblemSummary("02000000000000000000000000000000", "22000000000000000000000000000000", "chat.open", after)
	deltas := Compare(baseline, candidate).ProblemComparison.Deltas
	if len(deltas) != 1 || deltas[0].Observation != ObservationBoth || deltas[0].Change != ChangeInsufficientData || !deltas[0].Comparable {
		t.Fatalf("semantic target was split or priority became effect: %+v", deltas)
	}
}

func TestProblemAbsenceDistinguishesNotObservedFromTargetNotExercised(t *testing.T) {
	finding := comparisonProblemFinding("chat-problem", "1", 50, ProblemLocation{Screen: "Chat", Operation: "chat.open"})
	baseline := comparisonProblemSummary("01000000000000000000000000000000", "11000000000000000000000000000000", "chat.open", finding)
	candidate := comparisonProblemSummary("02000000000000000000000000000000", "22000000000000000000000000000000", "chat.open")
	delta := Compare(baseline, candidate).ProblemComparison.Deltas[0]
	if delta.Observation != ObservationNotObservedAfter || delta.Status == "resolved" || delta.Change != ChangeInsufficientData || !delta.Comparable {
		t.Fatalf("verified absence overstated: %+v", delta)
	}

	candidate.OperationAnalysis.Profiles[0].Stats.Operation = "video.call"
	delta = Compare(baseline, candidate).ProblemComparison.Deltas[0]
	if delta.Observation != ObservationTargetNotExercised || delta.Comparable || delta.Status == "resolved" {
		t.Fatalf("unexercised target became fix: %+v", delta)
	}
}

func TestProblemAbsenceRequiresRareEventUpperBoundBeforeImprovement(t *testing.T) {
	finding := comparisonProblemFinding("rare-chat-problem", "1", 50, ProblemLocation{Screen: "Chat", Operation: "chat.open"})
	finding.Frequency = &ProblemFrequency{Count: 5}
	baseline := comparisonProblemSummary("01000000000000000000000000000000", "11000000000000000000000000000000", "chat.open", finding)
	candidate := comparisonProblemSummary("02000000000000000000000000000000", "22000000000000000000000000000000", "chat.open")
	baseline.OperationAnalysis.Profiles[0].Stats.Count = 100
	candidate.OperationAnalysis.Profiles[0].Stats.Count = 40
	delta := Compare(baseline, candidate).ProblemComparison.Deltas[0]
	if delta.Change != ChangeInsufficientData || delta.Status == string(ChangeImproved) {
		t.Fatalf("short zero-event exposure claimed improvement: %+v", delta)
	}

	candidate.OperationAnalysis.Profiles[0].Stats.Count = 100
	delta = Compare(baseline, candidate).ProblemComparison.Deltas[0]
	if delta.Observation != ObservationNotObservedAfter || delta.Change != ChangeImproved || delta.Status != string(ChangeImproved) || !strings.Contains(delta.Note, "верхняя граница") {
		t.Fatalf("sufficient zero-event exposure not recognized: %+v", delta)
	}
}

func TestProblemMatchingRejectsDetectorDriftAndAmbiguousTargets(t *testing.T) {
	before := comparisonProblemFinding("before", "1", 50, ProblemLocation{Screen: "Chat", Operation: "chat.open"})
	after := comparisonProblemFinding("after", "2", 50, ProblemLocation{Screen: "Chat", Operation: "chat.open"})
	baseline := comparisonProblemSummary("01000000000000000000000000000000", "11000000000000000000000000000000", "chat.open", before)
	candidate := comparisonProblemSummary("02000000000000000000000000000000", "22000000000000000000000000000000", "chat.open", after)
	candidate.Detectors[0].Version = "2"
	delta := Compare(baseline, candidate).ProblemComparison.Deltas[0]
	if delta.Observation != ObservationMeasurementUnavailable || delta.Comparable {
		t.Fatalf("detector drift compared: %+v", delta)
	}

	before.DetectorVersion = "1"
	before.Where = append(before.Where, ProblemLocation{Screen: "Search", Operation: "search"})
	baseline = comparisonProblemSummary("01000000000000000000000000000000", "11000000000000000000000000000000", "chat.open", before)
	candidate = comparisonProblemSummary("02000000000000000000000000000000", "22000000000000000000000000000000", "chat.open", comparisonProblemFinding("after", "1", 50, ProblemLocation{Screen: "Chat", Operation: "chat.open"}))
	deltas := Compare(baseline, candidate).ProblemComparison.Deltas
	for _, item := range deltas {
		if item.Status == "new" || item.Status == "resolved" {
			t.Fatalf("ambiguity guessed: %+v", deltas)
		}
	}
	found := false
	for _, item := range deltas {
		found = found || item.Observation == ObservationAmbiguousMatch
	}
	if !found {
		t.Fatalf("ambiguity hidden: %+v", deltas)
	}
}

func TestProblemMatchingKeepsSplitMergeChildren(t *testing.T) {
	location := ProblemLocation{Screen: "Chat", Operation: "chat.open"}
	first := comparisonProblemFinding("first", "1", 40, location)
	second := comparisonProblemFinding("second", "1", 45, location)
	merged := comparisonProblemFinding("merged", "1", 50, location)
	baseline := comparisonProblemSummary("01000000000000000000000000000000", "11000000000000000000000000000000", "chat.open", first, second)
	candidate := comparisonProblemSummary("02000000000000000000000000000000", "22000000000000000000000000000000", "chat.open", merged)
	deltas := Compare(baseline, candidate).ProblemComparison.Deltas
	if len(deltas) != 1 || len(deltas[0].BaselineChildren) != 2 || len(deltas[0].CandidateChildren) != 1 || deltas[0].Observation != ObservationBoth {
		t.Fatalf("split/merge evidence lost: %+v", deltas)
	}
}

func TestProblemEffectContributesToOverallOutcome(t *testing.T) {
	location := ProblemLocation{Screen: "Chat", Operation: "chat.open"}
	before := comparisonProblemFinding("same", "1", 10, location)
	after := comparisonProblemFinding("same", "1", 10, location)
	before.Evidence = []ProblemEvidence{{Name: "Error rate", Numerator: u64ptr(10), Denominator: u64ptr(100)}}
	after.Evidence = []ProblemEvidence{{Name: "Error rate", Numerator: u64ptr(20), Denominator: u64ptr(100)}}
	baseline := comparisonProblemSummary("01000000000000000000000000000000", "11000000000000000000000000000000", "chat.open", before)
	candidate := comparisonProblemSummary("02000000000000000000000000000000", "22000000000000000000000000000000", "chat.open", after)
	comparison := Compare(baseline, candidate)
	if comparison.ProblemComparison.Deltas[0].Change != ChangeRegressed || comparison.Outcome != ChangeRegressed {
		t.Fatalf("problem effect missing from outcome: %+v", comparison)
	}
}

func TestProblemMatchingCandidateLimitFailsClosed(t *testing.T) {
	location := ProblemLocation{Screen: "Chat", Operation: "chat.open"}
	baseline := make([]ProblemFinding, problemMatchCandidatesLimit+1)
	for i := range baseline {
		baseline[i] = comparisonProblemFinding(string(rune('a'+i)), "1", i, location)
	}
	groups := semanticProblemGroups(baseline, []ProblemFinding{comparisonProblemFinding("candidate", "1", 1, location)})
	if len(groups) != 1 || !groups[0].ambiguous || len(groups[0].baseline) != problemMatchCandidatesLimit+1 {
		t.Fatalf("candidate overflow was truncated or guessed: %+v", groups)
	}
}

func TestExactProblemMatchKeepsPublicFingerprint(t *testing.T) {
	finding := comparisonProblemFinding("stable-fingerprint", "1", 1, ProblemLocation{Screen: "Chat", Operation: "chat.open"})
	baseline := comparisonProblemSummary("01000000000000000000000000000000", "11000000000000000000000000000000", "chat.open", finding)
	candidate := comparisonProblemSummary("02000000000000000000000000000000", "22000000000000000000000000000000", "chat.open", finding)
	delta := Compare(baseline, candidate).ProblemComparison.Deltas[0]
	if delta.Fingerprint != finding.Fingerprint {
		t.Fatalf("internal index leaked: %q", delta.Fingerprint)
	}
}

func TestExplicitProblemAliasMatchesRenamedFingerprint(t *testing.T) {
	before := comparisonProblemFinding("old-detector-id", "1", 10, ProblemLocation{Screen: "Chat", Operation: "chat.open"})
	after := comparisonProblemFinding("new-detector-id", "1", 10, ProblemLocation{Screen: "Conversation", Operation: "conversation.open"})
	baseline := comparisonProblemSummary("01000000000000000000000000000000", "11000000000000000000000000000000", "chat.open", before)
	candidate := comparisonProblemSummary("02000000000000000000000000000000", "22000000000000000000000000000000", "chat.open", after)

	without := Compare(baseline, candidate).ProblemComparison.Deltas
	if len(without) != 2 {
		t.Fatalf("renamed target was guessed without explicit alias: %+v", without)
	}
	aliases := &ProblemAliases{Schema: ProblemAliasesSchemaVersion, Entries: []ProblemAlias{{Baseline: before.Fingerprint, Candidate: after.Fingerprint}}}
	with := CompareWithOptions(baseline, candidate, ComparisonOptions{ProblemAliases: aliases}).ProblemComparison.Deltas
	if len(with) != 1 || with[0].Observation != ObservationBoth || len(with[0].BaselineChildren) != 1 || len(with[0].CandidateChildren) != 1 {
		t.Fatalf("explicit alias not applied: %+v", with)
	}
}

func TestLoadProblemAliasesIsStrictBoundedAndHasStableDigest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "aliases.json")
	valid := `{"schema":"jankhunter.problem-aliases/v1","aliases":[{"baseline":"old","candidate":"new"}]}`
	if err := os.WriteFile(path, []byte(valid), 0600); err != nil {
		t.Fatal(err)
	}
	first, err := LoadProblemAliases(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := LoadProblemAliases(path)
	if err != nil || first.SHA256 == "" || first.SHA256 != second.SHA256 || len(first.Entries) != 1 {
		t.Fatalf("unstable alias manifest: first=%+v second=%+v err=%v", first, second, err)
	}
	for name, payload := range map[string]string{
		"unknown_field":   strings.TrimSuffix(valid, "}") + `,"extra":true}`,
		"duplicate_left":  `{"schema":"jankhunter.problem-aliases/v1","aliases":[{"baseline":"old","candidate":"a"},{"baseline":"old","candidate":"b"}]}`,
		"duplicate_right": `{"schema":"jankhunter.problem-aliases/v1","aliases":[{"baseline":"a","candidate":"new"},{"baseline":"b","candidate":"new"}]}`,
		"blank":           `{"schema":"jankhunter.problem-aliases/v1","aliases":[{"baseline":"","candidate":"new"}]}`,
		"wrong_schema":    `{"schema":"v0","aliases":[]}`,
	} {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(payload), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadProblemAliases(path); err == nil {
				t.Fatal("invalid alias manifest accepted")
			}
		})
	}
}
