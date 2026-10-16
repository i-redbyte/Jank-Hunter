package analyze

import (
	"fmt"
	"math"
	"reflect"
	"testing"
)

func TestScenarioMatchingPermutationAndOverflow(t *testing.T) {
	b, c := matchTestInputs()
	b.Profiles = append(b.Profiles, matchTestProfile("search", b.Profiles[0].RunID, 40))
	c.Profiles = append(c.Profiles, matchTestProfile("search", c.Profiles[0].RunID, 50))
	first := matchScenarioProfiles(b, c)
	c.Profiles[0], c.Profiles[1] = c.Profiles[1], c.Profiles[0]
	next := matchScenarioProfiles(b, c)
	if first.Comparability != next.Comparability || first.Baseline != next.Baseline || first.Candidate != next.Candidate || len(first.Matches) != len(next.Matches) {
		t.Fatalf("input order changed matching: %+v / %+v", first, next)
	}
	for i, group := range next.Matches {
		if b.Profiles[group.Baseline[0]].Stats.Operation != c.Profiles[group.Candidate[0]].Stats.Operation || b.Profiles[first.Matches[i].Baseline[0]].Stats.Operation != b.Profiles[group.Baseline[0]].Stats.Operation {
			t.Fatal("permutation changed semantic pairing/order")
		}
	}
	b.Profiles[0].Stats.Count = math.MaxUint64
	result := matchScenarioProfiles(b, c)
	if result.Baseline.TotalKnown || result.Baseline.Total != math.MaxUint64 || result.Comparability == ScenarioFull {
		t.Fatalf("overflow concealed: %+v", result)
	}
}

func BenchmarkScenarioMatchingLimit(b *testing.B) {
	base, cand := matchTestInputs()
	base.Profiles = make([]OperationProfile, operationProfileLimit)
	cand.Profiles = make([]OperationProfile, operationProfileLimit)
	for i := range base.Profiles {
		name := fmt.Sprintf("scope-%04d", i)
		base.Profiles[i] = matchTestProfile(name, "01000000000000000000000000000000", 20)
		cand.Profiles[i] = matchTestProfile(name, "02000000000000000000000000000000", 30)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result := matchScenarioProfiles(base, cand)
		if result.Comparability != ScenarioFull || len(result.Matches) != operationProfileLimit {
			b.Fatal("scope lost at capacity")
		}
	}
}

func matchTestProfile(name, run string, count uint64) OperationProfile {
	return OperationProfile{Root: true, RunID: run, ProcessInstanceID: "10000000000000000000000000000000",
		Stats:      OperationStats{Operation: name, Kind: "user", Screen: "Chat", Count: count, P95MS: 100},
		Steps:      []OperationProfileStep{{Operation: "load", Kind: "stage", Screen: "Chat"}, {Operation: "render", Kind: "stage", Screen: "Chat"}},
		Attributes: []OperationProfileAttribute{{Key: "cache", Value: "cold"}, {Key: "dataset", Value: "small"}},
	}
}
func matchTestInputs() (*OperationAnalysis, *OperationAnalysis) {
	return &OperationAnalysis{Profiles: []OperationProfile{matchTestProfile("chat.open", "01000000000000000000000000000000", 20)}},
		&OperationAnalysis{Profiles: []OperationProfile{matchTestProfile("chat.open", "02000000000000000000000000000000", 30)}}
}
func TestScenarioMatchingIgnoresMeasuredEffectsAndAttributeOrder(t *testing.T) {
	b, c := matchTestInputs()
	c.Profiles[0].Stats.P95MS = 9000
	c.Profiles[0].Stats.Failures = 15
	c.Profiles[0].Attributes[0], c.Profiles[0].Attributes[1] = c.Profiles[0].Attributes[1], c.Profiles[0].Attributes[0]
	before := append([]OperationProfileAttribute(nil), c.Profiles[0].Attributes...)
	r := matchScenarioProfiles(b, c)
	if r.Comparability != ScenarioFull || len(r.Matches) != 1 || r.Baseline.Matched != 20 || r.Candidate.Matched != 30 {
		t.Fatalf("same scenario lost because of effect/order: %+v", r)
	}
	if !reflect.DeepEqual(before, c.Profiles[0].Attributes) {
		t.Fatal("matching mutated source")
	}
}
func TestScenarioMatchingRejectsDifferentWorkloadAndStageOrder(t *testing.T) {
	for _, change := range []string{"operation", "screen", "workload", "sequence", "revision"} {
		t.Run(change, func(t *testing.T) {
			b, c := matchTestInputs()
			switch change {
			case "operation":
				c.Profiles[0].Stats.Operation = "search"
			case "screen":
				c.Profiles[0].Stats.Screen = "Video"
			case "workload":
				c.Profiles[0].Attributes[0].Value = "warm"
			case "sequence":
				c.Profiles[0].Steps[0], c.Profiles[0].Steps[1] = c.Profiles[0].Steps[1], c.Profiles[0].Steps[0]
			case "revision":
				b.Profiles[0].Attributes = append(b.Profiles[0].Attributes, OperationProfileAttribute{Key: "jh.scenario_rev", Value: "1"})
				c.Profiles[0].Attributes = append(c.Profiles[0].Attributes, OperationProfileAttribute{Key: "jh.scenario_rev", Value: "2"})
			}
			r := matchScenarioProfiles(b, c)
			if r.Comparability != ScenarioNone || len(r.Matches) != 0 {
				t.Fatalf("false match: %+v", r)
			}
		})
	}
}
func TestScenarioMatchingPartialCoverageDoesNotCountNestedOperationsTwice(t *testing.T) {
	b, c := matchTestInputs()
	b.Profiles = append(b.Profiles, matchTestProfile("search", b.Profiles[0].RunID, 40))
	c.Profiles = append(c.Profiles, matchTestProfile("attachments", c.Profiles[0].RunID, 10))
	child := c.Profiles[0]
	child.Root = false
	child.Stats.Count = 300
	c.Profiles = append(c.Profiles, child)
	r := matchScenarioProfiles(b, c)
	if r.Comparability != ScenarioPartial || r.Baseline.Total != 60 || r.Candidate.Total != 40 || r.Baseline.Matched != 20 || r.Candidate.Matched != 30 || !r.Baseline.TotalKnown || !r.Candidate.TotalKnown {
		t.Fatalf("wrong scope/denominator: %+v", r)
	}
}
func TestScenarioMatchingPreservesIndependentRunsWithoutArtificialPairing(t *testing.T) {
	b, c := matchTestInputs()
	b.Profiles = append(b.Profiles, matchTestProfile("chat.open", "03000000000000000000000000000000", 40))
	r := matchScenarioProfiles(b, c)
	if r.Comparability != ScenarioFull || len(r.Matches) != 1 || len(r.Matches[0].Baseline) != 2 || len(r.Matches[0].Candidate) != 1 || r.Baseline.Matched != 60 {
		t.Fatalf("run grouping lost: %+v", r)
	}
}
func TestScenarioMatchingUnknownEvidenceNeverLooksComplete(t *testing.T) {
	for _, change := range []string{"absent", "legacy", "loss", "identity", "duplicate_key", "no_steps", "duplicate_capture", "same_capture"} {
		t.Run(change, func(t *testing.T) {
			b, c := matchTestInputs()
			switch change {
			case "absent":
				c = nil
			case "legacy":
				c.Profiles = nil
				c.Completed = 30
			case "loss":
				c.InvalidProfileSamples = 1
			case "identity":
				c.Profiles[0].RunID = ""
			case "duplicate_key":
				c.Profiles[0].Attributes = append(c.Profiles[0].Attributes, c.Profiles[0].Attributes[0])
			case "no_steps":
				c.Profiles[0].Steps = nil
			case "duplicate_capture":
				c.Profiles = append(c.Profiles, c.Profiles[0])
			case "same_capture":
				c.Profiles[0].RunID = b.Profiles[0].RunID
			}
			r := matchScenarioProfiles(b, c)
			if r.Comparability == ScenarioFull || r.Comparability == ScenarioNone {
				t.Fatalf("unknown classified as known: %+v", r)
			}
			if change != "loss" && len(r.Matches) != 0 {
				t.Fatalf("unknown evidence was matched: %+v", r)
			}
			if change == "loss" && (r.Candidate.TotalKnown || r.Comparability != ScenarioPartial) {
				t.Fatalf("loss hidden: %+v", r)
			}
		})
	}
}
func TestScenarioMatchingBoundedProfileInput(t *testing.T) {
	b, c := matchTestInputs()
	for i := 0; i < operationProfileLimit; i++ {
		c.Profiles = append(c.Profiles, matchTestProfile(fmt.Sprintf("scope%d", i), c.Profiles[0].RunID, 1))
	}
	r := matchScenarioProfiles(b, c)
	if r.Comparability != ScenarioUnknown || len(r.Matches) != 0 || r.Candidate.TotalKnown {
		t.Fatalf("oversize input accepted: %+v", r)
	}
}

func TestScenarioMatchingFindsUniqueCommonSubpathWithoutComparingWholeRoot(t *testing.T) {
	b, c := matchTestInputs()
	b.Profiles[0].Steps = []OperationProfileStep{
		{Operation: "open", Kind: "stage", Screen: "Chat"},
		{Operation: "load", Kind: "stage", Screen: "Chat"},
		{Operation: "render", Kind: "stage", Screen: "Chat"},
	}
	c.Profiles[0].Stats.Operation = "chat.open.from.push"
	c.Profiles[0].Steps = []OperationProfileStep{
		{Operation: "resolve.push", Kind: "stage", Screen: "Push"},
		{Operation: "load", Kind: "stage", Screen: "Chat"},
		{Operation: "render", Kind: "stage", Screen: "Chat"},
		{Operation: "mark.read", Kind: "stage", Screen: "Chat"},
	}

	r := matchScenarioProfiles(b, c)
	if r.Comparability != ScenarioPartial || len(r.Matches) != 1 {
		t.Fatalf("unique common subpath not matched: %+v", r)
	}
	if r.Matches[0].Exact || r.Matches[0].CommonSteps != 2 {
		t.Fatalf("common subpath promoted to exact match: %+v", r.Matches[0])
	}
	if r.Baseline.Matched != 20 || r.Candidate.Matched != 30 {
		t.Fatalf("common subpath coverage lost: %+v", r)
	}
	base, cand := comparisonScopeInput("01000000000000000000000000000000", "11000000000000000000000000000000", "com.example.app"), comparisonScopeInput("02000000000000000000000000000000", "22000000000000000000000000000000", "com.example.app")
	base.OperationAnalysis.Profiles[0] = b.Profiles[0]
	cand.OperationAnalysis.Profiles[0] = c.Profiles[0]
	base.OperationAnalysis.Profiles[0].ProcessName, cand.OperationAnalysis.Profiles[0].ProcessName = "com.example.app", "com.example.app"
	base.OperationAnalysis.Profiles[0].ProcessInstanceID, cand.OperationAnalysis.Profiles[0].ProcessInstanceID = "11000000000000000000000000000000", "22000000000000000000000000000000"
	scope := buildComparisonScope(base, cand)
	if len(scope.Changes) != 0 || scope.Outcome != ChangeInsufficientData {
		t.Fatalf("whole-root metrics leaked into common-subpath comparison: %+v", scope)
	}
}

func TestScenarioMatchingDoesNotGuessAmbiguousCommonSubpath(t *testing.T) {
	b, c := matchTestInputs()
	b.Profiles[0].Steps = []OperationProfileStep{{Operation: "load", Kind: "stage", Screen: "Chat"}, {Operation: "render", Kind: "stage", Screen: "Chat"}}
	for _, name := range []string{"chat.from.push", "chat.from.search"} {
		p := matchTestProfile(name, c.Profiles[0].RunID, 15)
		p.Steps = append([]OperationProfileStep{{Operation: "prepare", Kind: "stage", Screen: "Chat"}}, b.Profiles[0].Steps...)
		c.Profiles = append(c.Profiles, p)
	}
	c.Profiles = c.Profiles[1:]

	r := matchScenarioProfiles(b, c)
	if r.Comparability != ScenarioUnknown || len(r.Matches) != 0 || r.Baseline.Unknown != 20 || r.Candidate.Unknown != 30 {
		t.Fatalf("ambiguous subpath was guessed: %+v", r)
	}
}

func TestScenarioMatchingRejectsWeakOrIncompatibleApproximation(t *testing.T) {
	for _, change := range []string{"single_stage", "conditions", "explicit_revision", "process"} {
		t.Run(change, func(t *testing.T) {
			b, c := matchTestInputs()
			c.Profiles[0].Stats.Operation = "other"
			switch change {
			case "single_stage":
				c.Profiles[0].Steps = []OperationProfileStep{{Operation: "load", Kind: "stage", Screen: "Chat"}}
			case "conditions":
				c.Profiles[0].Attributes[0].Value = "warm"
			case "explicit_revision":
				b.Profiles[0].Attributes = append(b.Profiles[0].Attributes, OperationProfileAttribute{Key: "jh.scenario_rev", Value: "1"})
				c.Profiles[0].Attributes = append(c.Profiles[0].Attributes, OperationProfileAttribute{Key: "jh.scenario_rev", Value: "2"})
			case "process":
				c.Profiles[0].ProcessName = "other.process"
			}
			r := matchScenarioProfiles(b, c)
			if r.Comparability != ScenarioNone || len(r.Matches) != 0 {
				t.Fatalf("incompatible approximation accepted: %+v", r)
			}
		})
	}
}

func TestScenarioMatchingCandidateOverflowBecomesUnknown(t *testing.T) {
	b, c := matchTestInputs()
	b.Profiles[0].Steps = []OperationProfileStep{
		{Operation: "load", Kind: "stage", Screen: "Chat"},
		{Operation: "render", Kind: "stage", Screen: "Chat"},
	}
	c.Profiles = c.Profiles[:0]
	for i := 0; i <= problemMatchCandidatesLimit; i++ {
		p := matchTestProfile(fmt.Sprintf("candidate-%d", i), "02000000000000000000000000000000", 1)
		p.ProcessInstanceID = fmt.Sprintf("%032x", i+1)
		p.Steps = []OperationProfileStep{
			{Operation: fmt.Sprintf("prepare-%d", i), Kind: "stage", Screen: "Chat"},
			{Operation: "load", Kind: "stage", Screen: "Chat"},
			{Operation: "render", Kind: "stage", Screen: "Chat"},
		}
		c.Profiles = append(c.Profiles, p)
	}
	r := matchScenarioProfiles(b, c)
	if r.Comparability != ScenarioUnknown || len(r.Matches) != 0 || r.Baseline.Unknown != 20 || r.Candidate.Unknown != problemMatchCandidatesLimit+1 {
		t.Fatalf("candidate overflow hidden: %+v", r)
	}
}

func TestScenarioMatchingIgnoresMeasuredStageStats(t *testing.T) {
	baseline, candidate := matchTestInputs()
	baseline.Profiles[0].Steps[0].Stats = OperationStats{Operation: "load", Kind: "stage", Screen: "Chat", Count: 10, TotalMS: 100}
	candidate.Profiles[0].Steps[0].Stats = OperationStats{Operation: "load", Kind: "stage", Screen: "Chat", Count: 10, TotalMS: 900}
	matches := matchScenarioProfiles(baseline, candidate)
	if matches.Comparability != ScenarioFull || len(matches.Matches) != 1 || !matches.Matches[0].Exact {
		t.Fatalf("measured stage values changed identity: %+v", matches)
	}
}

func TestCommonSubpathComparesOnlyMatchedStageStats(t *testing.T) {
	baselineSummary, candidateSummary := scopedComparisonPair()
	baseline := &baselineSummary.OperationAnalysis.Profiles[0]
	candidate := &candidateSummary.OperationAnalysis.Profiles[0]
	baseline.Steps = []OperationProfileStep{
		{Operation: "prepare", Kind: "stage", Screen: "Chat", Stats: OperationStats{Operation: "prepare", Kind: "stage", Screen: "Chat", Count: 10, TotalMS: 9_000}},
		{Operation: "load", Kind: "stage", Screen: "Chat", Stats: OperationStats{Operation: "load", Kind: "stage", Screen: "Chat", Count: 10, TotalMS: 1_000}},
		{Operation: "render", Kind: "stage", Screen: "Chat", Stats: OperationStats{Operation: "render", Kind: "stage", Screen: "Chat", Count: 10, TotalMS: 2_000}},
	}
	candidate.Steps = []OperationProfileStep{
		{Operation: "load", Kind: "stage", Screen: "Chat", Stats: OperationStats{Operation: "load", Kind: "stage", Screen: "Chat", Count: 10, TotalMS: 800}},
		{Operation: "render", Kind: "stage", Screen: "Chat", Stats: OperationStats{Operation: "render", Kind: "stage", Screen: "Chat", Count: 10, TotalMS: 1_500}},
		{Operation: "attachments", Kind: "stage", Screen: "Chat", Stats: OperationStats{Operation: "attachments", Kind: "stage", Screen: "Chat", Count: 10, TotalMS: 99_000}},
	}
	baseline.Stats.TotalMS = 12_000
	candidate.Stats.TotalMS = 101_300
	comparison := Compare(baselineSummary, candidateSummary)
	if comparison.Scope.Comparability != ScenarioPartial {
		t.Fatalf("comparability=%s", comparison.Scope.Comparability)
	}
	if comparison.Scope.Baseline.MatchedDurationMS != 3_000 || comparison.Scope.Baseline.TotalDurationMS != 12_000 ||
		comparison.Scope.Candidate.MatchedDurationMS != 2_300 || comparison.Scope.Candidate.TotalDurationMS != 101_300 ||
		!comparison.Scope.Baseline.DurationKnown || !comparison.Scope.Candidate.DurationKnown {
		t.Fatalf("duration coverage=%+v / %+v", comparison.Scope.Baseline, comparison.Scope.Candidate)
	}
	load := metricChangeByName(t, comparison.Scope.Changes, "Stage load mean duration")
	if load.Scope != "stage" || load.Stage != "load" || load.Change != ChangeImproved {
		t.Fatalf("load change=%+v", load)
	}
	for _, change := range comparison.Scope.Changes {
		if change.Stage == "prepare" || change.Stage == "attachments" {
			t.Fatalf("unmatched stage leaked into scope: %+v", change)
		}
	}
}
