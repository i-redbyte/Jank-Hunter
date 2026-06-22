package analyze

import (
	"fmt"
	"math"
	"strings"
	"time"
)

type ValidationScorecard struct {
	SchemaVersion int                     `json:"schema_version"`
	Purpose       string                  `json:"purpose"`
	GeneratedAt   string                  `json:"generated_at"`
	Artifacts     ScorecardArtifacts      `json:"artifacts"`
	DataQuality   ScorecardDataQuality    `json:"data_quality"`
	LeakCompare   LeakCompareStats        `json:"leak_compare"`
	Scores        map[string]ScorecardRow `json:"scores"`
	Summary       ScorecardSummary        `json:"summary"`
}

type ScorecardArtifacts struct {
	BaselineJHLog        []string `json:"baseline_jhlog"`
	CandidateJHLog       []string `json:"candidate_jhlog"`
	BaselineHeapSources  []string `json:"baseline_heap_sources,omitempty"`
	CandidateHeapSources []string `json:"candidate_heap_sources,omitempty"`
}

type ScorecardDataQuality struct {
	Confidence      string   `json:"confidence"`
	BaselineLogs    int      `json:"baseline_logs"`
	CandidateLogs   int      `json:"candidate_logs"`
	BaselineEvents  int      `json:"baseline_events"`
	CandidateEvents int      `json:"candidate_events"`
	CohortWarnings  []string `json:"cohort_warnings,omitempty"`
	Warnings        []string `json:"warnings,omitempty"`
}

type ScorecardRow struct {
	Weight      int      `json:"weight"`
	Score0To10  float64  `json:"score_0_to_10"`
	Status      string   `json:"status"`
	Evidence    string   `json:"evidence"`
	NextActions []string `json:"next_actions,omitempty"`
}

type ScorecardSummary struct {
	WeightedScore0To10 float64  `json:"weighted_score_0_to_10"`
	GoNoGo             string   `json:"go_no_go"`
	NextActions        []string `json:"next_actions"`
}

var scorecardScoreOrder = []string{
	"data_quality",
	"retained_signal",
	"heap_actionability",
	"compare_stability",
	"ci_gate_readiness",
	"junior_readability",
}

func BuildValidationScorecard(
	baselinePaths []string,
	candidatePaths []string,
	comparison Comparison,
) ValidationScorecard {
	leakCompare := BuildLeakCompareReport(comparison)
	scores := map[string]ScorecardRow{
		"data_quality":       dataQualityScore(comparison),
		"retained_signal":    retainedSignalScore(comparison.Candidate),
		"heap_actionability": heapActionabilityScore(leakCompare.Candidate),
		"compare_stability":  compareStabilityScore(leakCompare, comparison.Confidence()),
		"ci_gate_readiness":  ciGateReadinessScore(comparison),
		"junior_readability": juniorReadabilityScore(leakCompare.Candidate),
	}
	summary := scorecardSummary(scores, comparison, leakCompare)
	return ValidationScorecard{
		SchemaVersion: 1,
		Purpose:       "Real-world validation readiness scorecard for Jank Hunter Android logs.",
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
		Artifacts: ScorecardArtifacts{
			BaselineJHLog:        append([]string{}, baselinePaths...),
			CandidateJHLog:       append([]string{}, candidatePaths...),
			BaselineHeapSources:  comparison.Baseline.HeapSources(),
			CandidateHeapSources: comparison.Candidate.HeapSources(),
		},
		DataQuality: ScorecardDataQuality{
			Confidence:      comparison.Confidence(),
			BaselineLogs:    comparison.Baseline.LogCount,
			CandidateLogs:   comparison.Candidate.LogCount,
			BaselineEvents:  comparison.Baseline.EventCount,
			CandidateEvents: comparison.Candidate.EventCount,
			CohortWarnings:  append([]string{}, comparison.Warnings...),
			Warnings:        append(append([]string{}, comparison.Baseline.Warnings...), comparison.Candidate.Warnings...),
		},
		LeakCompare: leakCompare.Stats,
		Scores:      scores,
		Summary:     summary,
	}
}

func (summary Summary) HeapSources() []string {
	seen := map[string]struct{}{}
	var sources []string
	for _, leak := range summary.MemoryLeaks {
		source := strings.TrimSpace(leak.HeapSource)
		if source == "" {
			continue
		}
		if _, ok := seen[source]; ok {
			continue
		}
		seen[source] = struct{}{}
		sources = append(sources, source)
	}
	return sources
}

func dataQualityScore(comparison Comparison) ScorecardRow {
	score := scoreForConfidence(comparison.Confidence())
	if len(comparison.Warnings) > 0 {
		score -= math.Min(2, float64(len(comparison.Warnings))*0.5)
	}
	return ScorecardRow{
		Weight:     20,
		Score0To10: roundScore(score),
		Status:     statusForScore(score),
		Evidence: fmt.Sprintf(
			"confidence=%s, baseline logs/events=%d/%d, candidate logs/events=%d/%d, cohort warnings=%d",
			comparison.Confidence(),
			comparison.Baseline.LogCount,
			comparison.Baseline.EventCount,
			comparison.Candidate.LogCount,
			comparison.Candidate.EventCount,
			len(comparison.Warnings),
		),
		NextActions: dataQualityActions(comparison),
	}
}

func retainedSignalScore(summary Summary) ScorecardRow {
	report := BuildLeakReport(summary)
	total := report.Stats.TotalSuspects
	if total == 0 {
		return ScorecardRow{
			Weight:      20,
			Score0To10:  5,
			Status:      "not_exercised",
			Evidence:    "candidate run has no retained-object suspects; clean result is useful, but leak detector recall is not exercised",
			NextActions: []string{"Run controlled Activity/Fragment/ViewModel/listener leak scenarios plus one clean watched object."},
		}
	}
	knownHolderRate := ratio(total-report.Stats.UnknownHolder, total)
	userOwnedRate := ratio(report.Stats.UserOwned, total)
	score := 4 + knownHolderRate*3 + userOwnedRate*2
	if report.Stats.MaxAgeMS >= 30_000 {
		score += 1
	}
	return ScorecardRow{
		Weight:     20,
		Score0To10: roundScore(score),
		Status:     statusForScore(score),
		Evidence: fmt.Sprintf(
			"leaks=%d, known_holder_rate=%.0f%%, user_owned_rate=%.0f%%, max_age_ms=%d",
			total,
			knownHolderRate*100,
			userOwnedRate*100,
			report.Stats.MaxAgeMS,
		),
		NextActions: retainedSignalActions(report),
	}
}

func heapActionabilityScore(report LeakReport) ScorecardRow {
	total := report.Stats.TotalSuspects
	if total == 0 {
		return ScorecardRow{
			Weight:      20,
			Score0To10:  5,
			Status:      "not_exercised",
			Evidence:    "no candidate leak suspects, so heap path quality was not exercised",
			NextActions: []string{"Run at least one retained-object scenario with --heap-dump or --heap-evidence."},
		}
	}
	heapRate := ratio(report.Stats.HeapConfirmed, total)
	fieldCount := 0
	pathCount := 0
	alternativeCount := 0
	for _, item := range report.Items {
		if item.Suspect.HolderField != "" {
			fieldCount++
		}
		if len(item.Suspect.ReferencePath) > 0 {
			pathCount++
		}
		if len(item.Suspect.AlternativePaths) > 0 {
			alternativeCount++
		}
	}
	score := 3 + heapRate*3 + ratio(fieldCount, total)*1.5 + ratio(pathCount, total)*1.5 + ratio(alternativeCount, total)
	return ScorecardRow{
		Weight:     20,
		Score0To10: roundScore(score),
		Status:     statusForScore(score),
		Evidence: fmt.Sprintf(
			"heap_confirmed=%d/%d, holder_field=%d, reference_path=%d, alternative_paths=%d",
			report.Stats.HeapConfirmed,
			total,
			fieldCount,
			pathCount,
			alternativeCount,
		),
		NextActions: heapActionabilityActions(report),
	}
}

func compareStabilityScore(report LeakCompareReport, confidence string) ScorecardRow {
	if len(report.Deltas) == 0 {
		return ScorecardRow{
			Weight:      15,
			Score0To10:  6,
			Status:      "not_exercised",
			Evidence:    "no leak deltas to match",
			NextActions: []string{"Run one candidate-only leak and one fixed-leak scenario to exercise matching stability."},
		}
	}
	high := 0
	medium := 0
	for _, delta := range report.Deltas {
		switch {
		case strings.HasPrefix(delta.MatchConfidence, "high"):
			high++
		case strings.HasPrefix(delta.MatchConfidence, "medium"):
			medium++
		}
	}
	score := scoreForConfidence(confidence)*0.4 + ratio(high, len(report.Deltas))*4 + ratio(medium, len(report.Deltas))*2
	return ScorecardRow{
		Weight:     15,
		Score0To10: roundScore(score),
		Status:     statusForScore(score),
		Evidence: fmt.Sprintf(
			"compare_confidence=%s, deltas=%d, high_match=%d, medium_match=%d",
			confidence,
			len(report.Deltas),
			high,
			medium,
		),
		NextActions: compareStabilityActions(report),
	}
}

func ciGateReadinessScore(comparison Comparison) ScorecardRow {
	score := scoreForConfidence(comparison.Confidence())
	if len(comparison.Warnings) > 0 {
		score -= 2
	}
	if comparison.Baseline.LogCount >= 5 && comparison.Candidate.LogCount >= 5 {
		score += 1
	}
	score = clampScore(score)
	return ScorecardRow{
		Weight:     15,
		Score0To10: roundScore(score),
		Status:     statusForScore(score),
		Evidence: fmt.Sprintf(
			"confidence=%s, baseline_logs=%d, candidate_logs=%d, warnings=%d",
			comparison.Confidence(),
			comparison.Baseline.LogCount,
			comparison.Candidate.LogCount,
			len(comparison.Warnings),
		),
		NextActions: ciGateActions(comparison),
	}
}

func juniorReadabilityScore(report LeakReport) ScorecardRow {
	if len(report.Items) == 0 {
		return ScorecardRow{
			Weight:      10,
			Score0To10:  6,
			Status:      "not_exercised",
			Evidence:    "no leak rows to explain",
			NextActions: []string{"Exercise at least one leak and verify the report gives investigation, fix and verification steps."},
		}
	}
	complete := 0
	for _, item := range report.Items {
		if len(item.Suspect.InvestigationSteps) > 0 &&
			len(item.Suspect.FixExamples) > 0 &&
			len(item.Suspect.VerificationSteps) > 0 &&
			item.Suspect.LeakChainSummary != "" {
			complete++
		}
	}
	score := 3 + ratio(complete, len(report.Items))*7
	return ScorecardRow{
		Weight:     10,
		Score0To10: roundScore(score),
		Status:     statusForScore(score),
		Evidence: fmt.Sprintf(
			"complete_explanations=%d/%d",
			complete,
			len(report.Items),
		),
		NextActions: readabilityActions(report, complete),
	}
}

func scorecardSummary(
	scores map[string]ScorecardRow,
	comparison Comparison,
	leakCompare LeakCompareReport,
) ScorecardSummary {
	totalWeight := 0
	weighted := 0.0
	var actions []string
	for _, key := range scorecardScoreOrder {
		row, ok := scores[key]
		if !ok {
			continue
		}
		totalWeight += row.Weight
		weighted += row.Score0To10 * float64(row.Weight)
		actions = append(actions, row.NextActions...)
	}
	score := 0.0
	if totalWeight > 0 {
		score = weighted / float64(totalWeight)
	}
	if len(comparison.Warnings) > 0 {
		actions = append(actions, "Align cohorts before trusting regression gates: app version, SDK, device, process and network mix should match.")
	}
	if leakCompare.Candidate.Stats.RuntimeOnly > 0 && leakCompare.Candidate.Stats.HeapConfirmed == 0 {
		actions = append(actions, "Collect candidate heap evidence for high or repeated runtime-only leaks.")
	}
	return ScorecardSummary{
		WeightedScore0To10: roundScore(score),
		GoNoGo:             goNoGo(score, comparison),
		NextActions:        uniqueStrings(actions),
	}
}

func dataQualityActions(comparison Comparison) []string {
	var actions []string
	if comparison.Confidence() != "high" {
		actions = append(actions, "Collect 5+ logs and 500+ events per baseline/candidate cohort for high confidence.")
	}
	if len(comparison.Warnings) > 0 {
		actions = append(actions, "Normalize cohorts before comparing regressions.")
	}
	return actions
}

func retainedSignalActions(report LeakReport) []string {
	var actions []string
	if report.Stats.UnknownHolder > 0 {
		actions = append(actions, "Add ownerHint/withOwner around leak-prone flows to reduce unknown holders.")
	}
	if report.Stats.UserOwned < report.Stats.TotalSuspects {
		actions = append(actions, "Improve lifecycle/ASM attribution so retained rows point to app-owned holders.")
	}
	return actions
}

func heapActionabilityActions(report LeakReport) []string {
	var actions []string
	if report.Stats.RuntimeOnly > 0 {
		actions = append(actions, "Run the same scenario with --heap-dump or --heap-evidence to confirm GC root and holder field.")
	}
	for _, item := range report.Items {
		if item.Suspect.HeapEvidence && item.Suspect.HolderField == "" {
			actions = append(actions, "Improve heap parser/reference matching for holder fields.")
			break
		}
	}
	return actions
}

func compareStabilityActions(report LeakCompareReport) []string {
	var actions []string
	for _, delta := range report.Deltas {
		if strings.HasPrefix(delta.MatchConfidence, "low") {
			actions = append(actions, "Add heap chain fingerprint or ownerHint for low-confidence leak matches.")
			break
		}
	}
	if report.Stats.New > 0 || report.Stats.Worse > 0 {
		actions = append(actions, "Add CI leak thresholds for new/worse leaks once cohorts are stable.")
	}
	return actions
}

func ciGateActions(comparison Comparison) []string {
	if comparison.Confidence() == "high" && len(comparison.Warnings) == 0 {
		return []string{"Enable compare --thresholds in CI with fail_on_new/fail_on_worse leak rules."}
	}
	return []string{"Keep CI gate advisory until confidence is medium/high and cohort warnings are resolved."}
}

func readabilityActions(report LeakReport, complete int) []string {
	if complete == len(report.Items) {
		return nil
	}
	return []string{"Ensure every leak row has investigation_steps, fix_examples, verification_steps and a plain-language chain summary."}
}

func scoreForConfidence(confidence string) float64 {
	switch confidence {
	case "high":
		return 9
	case "medium":
		return 7
	default:
		return 4
	}
}

func statusForScore(score float64) string {
	switch {
	case score >= 8:
		return "strong"
	case score >= 6:
		return "needs_work"
	default:
		return "weak"
	}
}

func goNoGo(score float64, comparison Comparison) string {
	switch {
	case score >= 8 && comparison.Confidence() != "low" && len(comparison.Warnings) == 0:
		return "go"
	case score >= 6:
		return "qa_only"
	default:
		return "blocked"
	}
}

func ratio(numerator, denominator int) float64 {
	if denominator <= 0 {
		return 0
	}
	return float64(numerator) / float64(denominator)
}

func roundScore(score float64) float64 {
	return math.Round(clampScore(score)*10) / 10
}

func clampScore(score float64) float64 {
	if score < 0 {
		return 0
	}
	if score > 10 {
		return 10
	}
	return score
}
