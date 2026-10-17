package analyze

import (
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
	"math"
	"sort"
)

type ScenarioComparability string

const (
	ScenarioFull    ScenarioComparability = "full"
	ScenarioPartial ScenarioComparability = "partial"
	ScenarioNone    ScenarioComparability = "none"
	ScenarioUnknown ScenarioComparability = "unknown"
)

// Counts describe root operation instances only, never roots plus their stages.
// TotalKnown is false when profiling losses prevent a complete denominator.
type ScenarioCoverage struct {
	Total             uint64 `json:"total"`
	Matched           uint64 `json:"matched"`
	Unknown           uint64 `json:"unknown"`
	TotalKnown        bool   `json:"total_known"`
	TotalDurationMS   uint64 `json:"total_duration_ms"`
	MatchedDurationMS uint64 `json:"matched_duration_ms"`
	UnknownDurationMS uint64 `json:"unknown_duration_ms"`
	DurationKnown     bool   `json:"duration_known"`
}
type scenarioMatch struct {
	Baseline           []int
	Candidate          []int
	Exact              bool
	CommonSteps        int
	BaselineStepStart  int
	CandidateStepStart int
}
type scenarioMatches struct {
	Comparability ScenarioComparability
	Baseline      ScenarioCoverage
	Candidate     ScenarioCoverage
	Matches       []scenarioMatch
}

// Matching establishes structural scope only. Device/configuration, collection
// quality and per-domain measurement eligibility must still be checked separately.
// Sorted exact keys avoid a cross-product search: O(P log P * (A + S)), P <= 2048,
// A <= 8, S <= 64. Runs sharing a key form a stratum, not invented paired trials.
func matchScenarioProfiles(baseline, candidate *OperationAnalysis) scenarioMatches {
	result := scenarioMatches{Comparability: ScenarioUnknown}
	base := scenarioRefs(baseline, &result.Baseline)
	cand := scenarioRefs(candidate, &result.Candidate)
	// A capture reused across inputs is not a before/after observation, even if
	// files, process subsets, or measured effects differ.
	sharedRuns := make(map[string]uint8, len(base)+len(cand))
	for _, r := range base {
		sharedRuns[r.profile.RunID] |= 1
	}
	for _, r := range cand {
		sharedRuns[r.profile.RunID] |= 2
	}
	base = excludeSharedScenarioRuns(base, sharedRuns, &result.Baseline)
	cand = excludeSharedScenarioRuns(cand, sharedRuns, &result.Candidate)
	baseMatched := make([]bool, len(base))
	candidateMatched := make([]bool, len(cand))
	var baseIndices, candidateIndices []int
	for i, j := 0, 0; i < len(base) && j < len(cand); {
		order := compareScenarioRef(base[i], cand[j])
		if order < 0 {
			i++
			continue
		}
		if order > 0 {
			j++
			continue
		}
		endI, endJ := i+1, j+1
		for endI < len(base) && compareScenarioRef(base[i], base[endI]) == 0 {
			endI++
		}
		for endJ < len(cand) && compareScenarioRef(cand[j], cand[endJ]) == 0 {
			endJ++
		}
		if result.Matches == nil {
			result.Matches = make([]scenarioMatch, 0, min(len(base), len(cand)))
			baseIndices = make([]int, 0, len(base))
			candidateIndices = make([]int, 0, len(cand))
		}
		baseStart, candidateStart := len(baseIndices), len(candidateIndices)
		for k := i; k < endI; k++ {
			baseIndices = append(baseIndices, base[k].index)
			baseMatched[k] = true
			addScenarioCount(&result.Baseline.Matched, base[k].profile.Stats.Count, &result.Baseline)
			addScenarioDuration(&result.Baseline.MatchedDurationMS, base[k].profile.Stats.TotalMS, &result.Baseline)
		}
		for k := j; k < endJ; k++ {
			candidateIndices = append(candidateIndices, cand[k].index)
			candidateMatched[k] = true
			addScenarioCount(&result.Candidate.Matched, cand[k].profile.Stats.Count, &result.Candidate)
			addScenarioDuration(&result.Candidate.MatchedDurationMS, cand[k].profile.Stats.TotalMS, &result.Candidate)
		}
		result.Matches = append(result.Matches, scenarioMatch{
			Baseline:  baseIndices[baseStart:len(baseIndices):len(baseIndices)],
			Candidate: candidateIndices[candidateStart:len(candidateIndices):len(candidateIndices)],
			Exact:     true,
		})
		i, j = endI, endJ
	}
	approximate, ambiguousBase, ambiguousCandidate := matchCommonScenarioSubpaths(base, cand, baseMatched, candidateMatched)
	for _, match := range approximate {
		baseMatched[match.baseline], candidateMatched[match.candidate] = true, true
		_, baselineStart, candidateStart := longestCommonScenarioSubpathRange(base[match.baseline].profile.Steps, cand[match.candidate].profile.Steps)
		addScenarioCount(&result.Baseline.Matched, base[match.baseline].profile.Stats.Count, &result.Baseline)
		addScenarioCount(&result.Candidate.Matched, cand[match.candidate].profile.Stats.Count, &result.Candidate)
		addCommonScenarioDuration(&result.Baseline, base[match.baseline].profile.Steps, baselineStart, match.commonSteps)
		addCommonScenarioDuration(&result.Candidate, cand[match.candidate].profile.Steps, candidateStart, match.commonSteps)
		result.Matches = append(result.Matches, scenarioMatch{
			Baseline:           []int{base[match.baseline].index},
			Candidate:          []int{cand[match.candidate].index},
			CommonSteps:        match.commonSteps,
			BaselineStepStart:  baselineStart,
			CandidateStepStart: candidateStart,
		})
	}
	for i, ambiguous := range ambiguousBase {
		if ambiguous && !baseMatched[i] {
			result.Baseline.TotalKnown = false
			addScenarioCount(&result.Baseline.Unknown, base[i].profile.Stats.Count, &result.Baseline)
			addScenarioDuration(&result.Baseline.UnknownDurationMS, base[i].profile.Stats.TotalMS, &result.Baseline)
		}
	}
	for i, ambiguous := range ambiguousCandidate {
		if ambiguous && !candidateMatched[i] {
			result.Candidate.TotalKnown = false
			addScenarioCount(&result.Candidate.Unknown, cand[i].profile.Stats.Count, &result.Candidate)
			addScenarioDuration(&result.Candidate.UnknownDurationMS, cand[i].profile.Stats.TotalMS, &result.Candidate)
		}
	}
	known := result.Baseline.TotalKnown && result.Candidate.TotalKnown && result.Baseline.Unknown == 0 && result.Candidate.Unknown == 0
	if len(result.Matches) > 0 {
		result.Comparability = ScenarioPartial
		if known && len(approximate) == 0 && result.Baseline.Total == result.Baseline.Matched && result.Candidate.Total == result.Candidate.Matched {
			result.Comparability = ScenarioFull
		}
	} else if known && result.Baseline.Total > 0 && result.Candidate.Total > 0 {
		result.Comparability = ScenarioNone
	}
	return result
}

type commonScenarioMatch struct {
	baseline, candidate int
	commonSteps         int
}

type commonScenarioProposal struct {
	index, score int
	ambiguous    bool
}

// Common-subpath matching is intentionally conservative. It can establish a
// partial structural overlap, but never makes whole-root measurements eligible.
// A mutual unique best match prevents many-to-one reuse. Candidate lookup uses
// a bounded hash index, so the fallback remains O(P*S*32*S), P<=2048, S<=64.
func matchCommonScenarioSubpaths(base, candidate []scenarioRef, baseMatched, candidateMatched []bool) ([]commonScenarioMatch, []bool, []bool) {
	baseProposals := commonScenarioProposals(base, candidate, baseMatched, candidateMatched)
	candidateProposals := commonScenarioProposals(candidate, base, candidateMatched, baseMatched)
	ambiguousBase, ambiguousCandidate := make([]bool, len(base)), make([]bool, len(candidate))
	result := make([]commonScenarioMatch, 0)
	for i, proposal := range baseProposals {
		if proposal.ambiguous {
			ambiguousBase[i] = true
			markCommonScenarioContenders(base[i], candidate, candidateMatched, proposal.score, ambiguousCandidate)
			continue
		}
		if proposal.index < 0 {
			continue
		}
		reverse := candidateProposals[proposal.index]
		if reverse.ambiguous || reverse.index != i {
			ambiguousBase[i], ambiguousCandidate[proposal.index] = true, true
			continue
		}
		result = append(result, commonScenarioMatch{baseline: i, candidate: proposal.index, commonSteps: proposal.score})
	}
	return result, ambiguousBase, ambiguousCandidate
}

func commonScenarioProposals(source, target []scenarioRef, sourceMatched, targetMatched []bool) []commonScenarioProposal {
	proposals := make([]commonScenarioProposal, len(source))
	for i := range proposals {
		proposals[i].index = -1
	}
	index := make(map[uint64][]int, len(target))
	overflow := make(map[uint64]bool)
	for i := range target {
		if targetMatched[i] {
			continue
		}
		for stepIndex, step := range target[i].profile.Steps {
			hash := scenarioStepHash(step)
			duplicate := false
			for previous := 0; previous < stepIndex; previous++ {
				if scenarioStepEqual(step, target[i].profile.Steps[previous]) {
					duplicate = true
					break
				}
			}
			if duplicate || overflow[hash] {
				continue
			}
			entries := index[hash]
			if len(entries) == problemMatchCandidatesLimit {
				delete(index, hash)
				overflow[hash] = true
				continue
			}
			index[hash] = append(entries, i)
		}
	}
	for i := range source {
		if sourceMatched[i] {
			continue
		}
		var candidates []int
		hadOverflow := false
		for _, step := range source[i].profile.Steps {
			hash := scenarioStepHash(step)
			if overflow[hash] {
				hadOverflow = true
				continue
			}
			entries := index[hash]
			if len(entries) > 0 && (candidates == nil || len(entries) < len(candidates)) {
				candidates = entries
			}
		}
		if candidates == nil && hadOverflow {
			proposals[i] = commonScenarioProposal{index: -1, score: -1, ambiguous: true}
			continue
		}
		bestIndex, bestScore, ties := -1, 0, 0
		for _, candidateIndex := range candidates {
			if !commonScenarioConditionsEqual(source[i], target[candidateIndex]) {
				continue
			}
			score := longestCommonScenarioSubpath(source[i].profile.Steps, target[candidateIndex].profile.Steps)
			if score < 2 || score == len(source[i].profile.Steps) && score == len(target[candidateIndex].profile.Steps) {
				continue
			}
			if score > bestScore {
				bestIndex, bestScore, ties = candidateIndex, score, 1
			} else if score == bestScore {
				ties++
			}
		}
		proposals[i] = commonScenarioProposal{index: bestIndex, score: bestScore, ambiguous: ties > 1}
	}
	return proposals
}

func markCommonScenarioContenders(source scenarioRef, targets []scenarioRef, targetMatched []bool, score int, ambiguous []bool) {
	if score == 0 || score == 1 {
		return
	}
	for i := range targets {
		candidateScore := longestCommonScenarioSubpath(source.profile.Steps, targets[i].profile.Steps)
		scoreMatches := candidateScore == score || score < 0 && candidateScore >= 2
		if !targetMatched[i] && commonScenarioConditionsEqual(source, targets[i]) && scoreMatches &&
			!(candidateScore == len(source.profile.Steps) && candidateScore == len(targets[i].profile.Steps)) {
			ambiguous[i] = true
		}
	}
}

func commonScenarioConditionsEqual(left, right scenarioRef) bool {
	if left.profile.ProcessName != right.profile.ProcessName || left.count != right.count {
		return false
	}
	for i := 0; i < left.count; i++ {
		if left.attributes[i] != right.attributes[i] {
			return false
		}
	}
	return true
}

func longestCommonScenarioSubpath(left, right []OperationProfileStep) int {
	length, _, _ := longestCommonScenarioSubpathRange(left, right)
	return length
}

func longestCommonScenarioSubpathRange(left, right []OperationProfileStep) (int, int, int) {
	var previous, current [operationProfileStepLimit + 1]uint8
	best := uint8(0)
	bestLeftEnd, bestRightEnd := 0, 0
	for i := range left {
		for j := range right {
			if scenarioStepEqual(left[i], right[j]) {
				current[j+1] = previous[j] + 1
				if current[j+1] > best {
					best = current[j+1]
					bestLeftEnd, bestRightEnd = i+1, j+1
				}
			} else {
				current[j+1] = 0
			}
		}
		previous, current = current, previous
	}
	length := int(best)
	return length, bestLeftEnd - length, bestRightEnd - length
}

func scenarioStepEqual(left, right OperationProfileStep) bool {
	return left.Operation == right.Operation && left.Kind == right.Kind && left.Screen == right.Screen
}

func scenarioStepHash(step OperationProfileStep) uint64 {
	const offset64 = uint64(1469598103934665603)
	const prime64 = uint64(1099511628211)
	hash := offset64
	for _, value := range [...]string{step.Operation, step.Kind, step.Screen} {
		for i := range value {
			hash = (hash ^ uint64(value[i])) * prime64
		}
		hash = (hash ^ 0xff) * prime64
	}
	return hash
}

type scenarioRef struct {
	profile    *OperationProfile
	index      int
	attributes [jhlog.MaxOperationAttributes]OperationProfileAttribute
	count      int
}

func scenarioRefs(analysis *OperationAnalysis, coverage *ScenarioCoverage) []scenarioRef {
	if analysis == nil || len(analysis.Profiles) == 0 || len(analysis.Profiles) > operationProfileLimit {
		return nil
	}
	coverage.TotalKnown = analysis.InvalidProfileSamples == 0 && analysis.DroppedProfileSamples == 0 && analysis.MissingFinish == 0 && analysis.DroppedActiveStarts == 0
	coverage.DurationKnown = coverage.TotalKnown
	refs := make([]scenarioRef, 0, len(analysis.Profiles))
	for i := range analysis.Profiles {
		p := &analysis.Profiles[i]
		if !p.Root {
			continue
		}
		addScenarioCount(&coverage.Total, p.Stats.Count, coverage)
		addScenarioDuration(&coverage.TotalDurationMS, p.Stats.TotalMS, coverage)
		ref, valid := makeScenarioRef(p, i)
		if !valid {
			addScenarioCount(&coverage.Unknown, p.Stats.Count, coverage)
			addScenarioDuration(&coverage.UnknownDurationMS, p.Stats.TotalMS, coverage)
			coverage.TotalKnown = false
			coverage.DurationKnown = false
			continue
		}
		refs = append(refs, ref)
	}
	if coverage.Total == 0 {
		coverage.TotalKnown = false
		coverage.DurationKnown = false
	}
	sort.Slice(refs, func(i, j int) bool {
		order := compareScenarioRef(refs[i], refs[j])
		if order != 0 {
			return order < 0
		}
		if refs[i].profile.RunID != refs[j].profile.RunID {
			return refs[i].profile.RunID < refs[j].profile.RunID
		}
		return refs[i].profile.ProcessInstanceID < refs[j].profile.ProcessInstanceID
	})
	// Reject repeated rows for the same scope in one capture instead of silently
	// doubling exposure. Distinct runs and distinct processes remain separate rows.
	write := 0
	for i := 0; i < len(refs); {
		end := i + 1
		for end < len(refs) && compareScenarioRef(refs[i], refs[end]) == 0 && refs[i].profile.RunID == refs[end].profile.RunID && refs[i].profile.ProcessInstanceID == refs[end].profile.ProcessInstanceID {
			end++
		}
		if end == i+1 {
			refs[write] = refs[i]
			write++
		} else {
			coverage.TotalKnown = false
			coverage.DurationKnown = false
			for j := i; j < end; j++ {
				addScenarioCount(&coverage.Unknown, refs[j].profile.Stats.Count, coverage)
				addScenarioDuration(&coverage.UnknownDurationMS, refs[j].profile.Stats.TotalMS, coverage)
			}
		}
		i = end
	}
	return refs[:write]
}
func makeScenarioRef(p *OperationProfile, index int) (scenarioRef, bool) {
	ref := scenarioRef{profile: p, index: index, count: len(p.Attributes)}
	if p.Stats.Count == 0 || !scenarioAcquisitionIDValid(p.RunID) || !scenarioAcquisitionIDValid(p.ProcessInstanceID) ||
		!profileAttributeValid(p.Stats.Operation) || !profileAttributeValid(p.Stats.Kind) || !profileAttributeValid(p.Stats.Screen) ||
		len(p.Steps) == 0 || len(p.Steps) > operationProfileStepLimit || ref.count > len(ref.attributes) {
		return ref, false
	}
	for _, step := range p.Steps {
		if !profileAttributeValid(step.Operation) || step.Kind != "stage" || !profileAttributeValid(step.Screen) {
			return ref, false
		}
	}
	for i, a := range p.Attributes {
		if !profileAttributeValid(a.Key) || !profileAttributeValid(a.Value) {
			return ref, false
		}
		pos := i
		for pos > 0 && ref.attributes[pos-1].Key > a.Key {
			ref.attributes[pos] = ref.attributes[pos-1]
			pos--
		}
		if pos > 0 && ref.attributes[pos-1].Key == a.Key {
			return ref, false
		}
		ref.attributes[pos] = a
	}
	return ref, true
}
func scenarioAcquisitionIDValid(value string) bool {
	if len(value) != 32 {
		return false
	}
	nonzero := false
	for i := range value {
		c := value[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
		nonzero = nonzero || c != '0'
	}
	return nonzero
}
func compareScenarioRef(a, b scenarioRef) int {
	if a.profile.ProcessName < b.profile.ProcessName {
		return -1
	}
	if a.profile.ProcessName > b.profile.ProcessName {
		return 1
	}
	for i, left := range [3]string{a.profile.Stats.Operation, a.profile.Stats.Kind, a.profile.Stats.Screen} {
		right := [3]string{b.profile.Stats.Operation, b.profile.Stats.Kind, b.profile.Stats.Screen}[i]
		if left < right {
			return -1
		}
		if left > right {
			return 1
		}
	}
	for i := 0; i < a.count && i < b.count; i++ {
		if a.attributes[i].Key < b.attributes[i].Key {
			return -1
		}
		if a.attributes[i].Key > b.attributes[i].Key {
			return 1
		}
		if a.attributes[i].Value < b.attributes[i].Value {
			return -1
		}
		if a.attributes[i].Value > b.attributes[i].Value {
			return 1
		}
	}
	if a.count < b.count {
		return -1
	}
	if a.count > b.count {
		return 1
	}
	if profileStepsLess(a.profile.Steps, b.profile.Steps) {
		return -1
	}
	if profileStepsLess(b.profile.Steps, a.profile.Steps) {
		return 1
	}
	return 0
}
func excludeSharedScenarioRuns(refs []scenarioRef, runs map[string]uint8, coverage *ScenarioCoverage) []scenarioRef {
	write := 0
	for _, ref := range refs {
		if runs[ref.profile.RunID] == 3 {
			coverage.TotalKnown = false
			coverage.DurationKnown = false
			addScenarioCount(&coverage.Unknown, ref.profile.Stats.Count, coverage)
			addScenarioDuration(&coverage.UnknownDurationMS, ref.profile.Stats.TotalMS, coverage)
			continue
		}
		refs[write] = ref
		write++
	}
	return refs[:write]
}
func addScenarioCount(target *uint64, value uint64, coverage *ScenarioCoverage) {
	if math.MaxUint64-*target < value {
		*target = math.MaxUint64
		coverage.TotalKnown = false
		return
	}
	*target += value
}

func addScenarioDuration(target *uint64, value uint64, coverage *ScenarioCoverage) {
	if math.MaxUint64-*target < value {
		*target = math.MaxUint64
		coverage.DurationKnown = false
		return
	}
	*target += value
}

func addCommonScenarioDuration(coverage *ScenarioCoverage, steps []OperationProfileStep, start, count int) {
	var result uint64
	if start < 0 || count < 0 || start+count > len(steps) {
		coverage.DurationKnown = false
		return
	}
	for index := start; index < start+count; index++ {
		if math.MaxUint64-result < steps[index].Stats.TotalMS {
			coverage.DurationKnown = false
			return
		}
		result += steps[index].Stats.TotalMS
	}
	addScenarioDuration(&coverage.MatchedDurationMS, result, coverage)
}
