package analyze

import (
	"math"
	"sort"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

const ComparisonSchemaVersion = "jankhunter.comparison/v2"

type EligibilityState string

const (
	EligibilityEligible   EligibilityState = "eligible"
	EligibilityIneligible EligibilityState = "ineligible"
	EligibilityUnknown    EligibilityState = "unknown"
)

type ComparisonDomain string

const (
	DomainLatency      ComparisonDomain = "latency"
	DomainFailure      ComparisonDomain = "failure_rate"
	DomainBudget       ComparisonDomain = "budget_breach_rate"
	DomainHTTP         ComparisonDomain = "http"
	DomainDatabase     ComparisonDomain = "database"
	DomainUI           ComparisonDomain = "ui"
	DomainMemory       ComparisonDomain = "memory"
	DomainRetention    ComparisonDomain = "retention"
	DomainCPU          ComparisonDomain = "cpu"
	DomainIO           ComparisonDomain = "io"
	DomainNetworkBytes ComparisonDomain = "network_bytes"
	DomainProblem      ComparisonDomain = "problems"
)

type EligibilityReason string

const (
	ReasonNone                EligibilityReason = ""
	ReasonApplicationMismatch EligibilityReason = "application_mismatch"
	ReasonApplicationUnknown  EligibilityReason = "application_unknown"
	ReasonProcessMismatch     EligibilityReason = "process_mismatch"
	ReasonProcessUnknown      EligibilityReason = "process_unknown"
	ReasonEnvironmentMismatch EligibilityReason = "environment_mismatch"
	ReasonEnvironmentUnknown  EligibilityReason = "environment_unknown"
	ReasonCollectorDisabled   EligibilityReason = "collector_disabled"
	ReasonCollectionLoss      EligibilityReason = "collection_loss"
	ReasonCollectionUnknown   EligibilityReason = "collection_unknown"
	ReasonFilterMismatch      EligibilityReason = "filter_mismatch"
	ReasonDetectorDrift       EligibilityReason = "detector_drift"
	ReasonScenarioUnavailable EligibilityReason = "scenario_unavailable"
)

type MetricEligibility struct {
	Domain ComparisonDomain  `json:"domain"`
	State  EligibilityState  `json:"state"`
	Reason EligibilityReason `json:"reason,omitempty"`
}

type ComparisonIdentity struct {
	State  EligibilityState  `json:"state"`
	Value  string            `json:"value,omitempty"`
	Reason EligibilityReason `json:"reason,omitempty"`
}

type ComparisonScope struct {
	Comparability       ScenarioComparability `json:"comparability"`
	Outcome             ComparisonChange      `json:"outcome"`
	Baseline            ScenarioCoverage      `json:"baseline"`
	Candidate           ScenarioCoverage      `json:"candidate"`
	ExactMatchGroups    int                   `json:"exact_match_groups"`
	CommonSubpathGroups int                   `json:"common_subpath_groups"`
	Application         ComparisonIdentity    `json:"application"`
	Process             ComparisonIdentity    `json:"process"`
	Metrics             []MetricEligibility   `json:"metrics"`
	Changes             []MetricChange        `json:"changes,omitempty"`
	matches             []scenarioMatch
}

type ComparisonEvidence string
type ComparisonConfidence string

const (
	EvidenceObserved   ComparisonEvidence   = "observed"
	EvidenceRepeated   ComparisonEvidence   = "repeated"
	ConfidenceLimited  ComparisonConfidence = "limited"
	ConfidenceModerate ComparisonConfidence = "moderate"
	ConfidenceStrong   ComparisonConfidence = "strong"
)

type MetricChange struct {
	Name        string               `json:"name"`
	Scope       string               `json:"scope,omitempty"`
	Stage       string               `json:"stage,omitempty"`
	Domain      ComparisonDomain     `json:"domain"`
	Unit        string               `json:"unit"`
	Eligibility MetricEligibility    `json:"eligibility"`
	Before      *float64             `json:"before,omitempty"`
	After       *float64             `json:"after,omitempty"`
	Absolute    *float64             `json:"absolute,omitempty"`
	Relative    *float64             `json:"relative,omitempty"`
	Change      ComparisonChange     `json:"change"`
	Evidence    ComparisonEvidence   `json:"evidence"`
	Confidence  ComparisonConfidence `json:"confidence"`
	Samples     uint64               `json:"samples"`
	Interval    *EffectInterval      `json:"interval,omitempty"`
}

type EffectInterval struct {
	Lower           float64 `json:"lower"`
	Upper           float64 `json:"upper"`
	ConfidenceLevel float64 `json:"confidence_level"`
	BaselineGroups  int     `json:"baseline_groups"`
	CandidateGroups int     `json:"candidate_groups"`
	Method          string  `json:"method"`
}

func buildComparisonScope(baseline, candidate Summary) ComparisonScope {
	matched := matchScenarioProfiles(baseline.OperationAnalysis, candidate.OperationAnalysis)
	result := ComparisonScope{
		Comparability: matched.Comparability,
		Outcome:       ChangeInsufficientData,
		Baseline:      matched.Baseline,
		Candidate:     matched.Candidate,
		matches:       matched.Matches,
	}
	for _, match := range matched.Matches {
		if match.Exact {
			result.ExactMatchGroups++
		} else {
			result.CommonSubpathGroups++
		}
	}
	result.Application = compareProfileApplication(baseline.OperationAnalysis, candidate.OperationAnalysis)
	result.Process = compareProfileProcess(baseline.OperationAnalysis, candidate.OperationAnalysis)
	if result.Application.State == EligibilityIneligible || result.Process.State == EligibilityIneligible {
		result.Comparability = ScenarioNone
		result.Outcome = ChangeNotComparable
	} else if result.Application.State == EligibilityUnknown || result.Process.State == EligibilityUnknown || !filtersEquivalent(baseline.AnalysisFilter, candidate.AnalysisFilter) {
		result.Comparability = ScenarioUnknown
	}
	result.Metrics = comparisonMetricEligibility(baseline, candidate, result)
	result.Changes = scopedMetricChanges(baseline, candidate, result)
	var outcome comparisonOutcome
	for _, change := range result.Changes {
		outcome.add(change.Change)
	}
	if result.Comparability == ScenarioFull || result.Comparability == ScenarioPartial {
		result.Outcome = outcome.change()
	}
	if result.Comparability == ScenarioNone {
		result.Outcome = ChangeNotComparable
	}
	return result
}

type scopeMetricDefinition struct {
	name          string
	domain        ComparisonDomain
	unit          string
	band          comparisonBand
	higherIsWorse bool
	values        func(OperationStats) (float64, float64, bool)
}

func scopedMetricChanges(baseline, candidate Summary, scope ComparisonScope) []MetricChange {
	definitions := [...]scopeMetricDefinition{
		{name: "Operation mean duration", domain: DomainLatency, unit: "ms", band: comparisonBand{Absolute: 1, Relative: .05}, higherIsWorse: true, values: func(s OperationStats) (float64, float64, bool) { return boundedMetricValues(s.TotalMS, s.Count) }},
		{name: "Operation failure rate", domain: DomainFailure, unit: "%", band: comparisonBand{Absolute: 1}, higherIsWorse: true, values: func(s OperationStats) (float64, float64, bool) {
			n, ok := addMetricCounters(s.Failures, s.Timeouts)
			if !ok {
				return 0, 0, false
			}
			return percentMetricValues(n, s.Count)
		}},
		{name: "Operation budget breach rate", domain: DomainBudget, unit: "%", band: comparisonBand{Absolute: 1}, higherIsWorse: true, values: func(s OperationStats) (float64, float64, bool) {
			return percentMetricValues(s.BudgetBreaches, s.Budgeted)
		}},
		{name: "HTTP calls per operation", domain: DomainHTTP, unit: "calls/op", band: comparisonBand{Absolute: .05, Relative: .05}, higherIsWorse: true, values: func(s OperationStats) (float64, float64, bool) { return boundedMetricValues(s.CorrelatedHTTP, s.Count) }},
		{name: "Database calls per operation", domain: DomainDatabase, unit: "calls/op", band: comparisonBand{Absolute: .05, Relative: .05}, higherIsWorse: true, values: func(s OperationStats) (float64, float64, bool) {
			return boundedMetricValues(s.CorrelatedDatabase, s.Count)
		}},
		{name: "Operation UI jank rate", domain: DomainUI, unit: "%", band: comparisonBand{Absolute: 1}, higherIsWorse: true, values: func(s OperationStats) (float64, float64, bool) {
			return percentMetricValues(s.CorrelatedUIJank, s.CorrelatedUIFrames)
		}},
		{name: "Operation max PSS", domain: DomainMemory, unit: "KB", band: comparisonBand{Absolute: 1024, Relative: .05}, higherIsWorse: true, values: func(s OperationStats) (float64, float64, bool) {
			return boundedMetricValues(s.MaxPSSKB, 1)
		}},
		{name: "Retained objects per operation", domain: DomainRetention, unit: "objects/op", band: comparisonBand{Absolute: .1, Relative: .05}, higherIsWorse: true, values: func(s OperationStats) (float64, float64, bool) {
			return boundedMetricValues(s.CorrelatedRetainedObjects, s.Count)
		}},
		{name: "Operation CPU average", domain: DomainCPU, unit: "%", band: comparisonBand{Absolute: 1, Relative: .05}, higherIsWorse: true, values: func(s OperationStats) (float64, float64, bool) {
			return scaledMetricValues(s.CorrelatedCPUSumX100, s.CorrelatedCPUSamples, 100)
		}},
		{name: "I/O duration per operation", domain: DomainIO, unit: "ms/op", band: comparisonBand{Absolute: 1, Relative: .05}, higherIsWorse: true, values: func(s OperationStats) (float64, float64, bool) {
			return scaledMetricValues(s.CorrelatedIODurationUS, s.Count, 1000)
		}},
		{name: "I/O bytes per operation", domain: DomainIO, unit: "bytes/op", band: comparisonBand{Absolute: 1024, Relative: .05}, higherIsWorse: true, values: func(s OperationStats) (float64, float64, bool) {
			if s.CorrelatedIOBytesKnown == 0 {
				return 0, 0, false
			}
			return boundedMetricValues(s.CorrelatedIOBytes, s.Count)
		}},
		{name: "HTTP duration per operation", domain: DomainHTTP, unit: "ms/op", band: comparisonBand{Absolute: 1, Relative: .05}, higherIsWorse: true, values: func(s OperationStats) (float64, float64, bool) {
			return boundedMetricValues(s.CorrelatedHTTPDurationMS, s.Count)
		}},
		{name: "Network bytes per operation", domain: DomainNetworkBytes, unit: "bytes/op", band: comparisonBand{Absolute: 1024, Relative: .05}, higherIsWorse: true, values: func(s OperationStats) (float64, float64, bool) {
			if s.CorrelatedHTTPBytesKnown == 0 {
				return 0, 0, false
			}
			total, ok := addMetricCounters(s.CorrelatedHTTPRxBytes, s.CorrelatedHTTPTxBytes)
			if !ok {
				return 0, 0, false
			}
			return boundedMetricValues(total, s.Count)
		}},
	}
	if scope.Comparability != ScenarioFull && scope.Comparability != ScenarioPartial {
		return nil
	}
	result := make([]MetricChange, 0, len(definitions))
	for _, definition := range definitions {
		eligibility := eligibilityForDomain(scope.Metrics, definition.domain)
		if eligibility.State != EligibilityEligible {
			continue
		}
		before, after, samples, ok := standardizedScopeMetric(baseline.OperationAnalysis, candidate.OperationAnalysis, scope.matches, definition.values)
		if !ok {
			continue
		}
		effect := compareMeasurement(comparisonMeasurement{Value: before, Samples: samples, State: measurementObserved}, comparisonMeasurement{Value: after, Samples: samples, State: measurementObserved}, definition.band, true, definition.higherIsWorse)
		change := MetricChange{Name: definition.name, Domain: definition.domain, Unit: definition.unit, Eligibility: eligibility, Before: numberPointer(before), After: numberPointer(after), Absolute: numberPointer(effect.Absolute), Change: effect.Change, Samples: samples}
		if effect.RelativeKnown {
			change.Relative = numberPointer(effect.Relative)
		}
		leftRuns, rightRuns := scopedMetricRunValues(baseline.OperationAnalysis, candidate.OperationAnalysis, scope.matches, definition.values)
		if interval, intervalOK := welchDifferenceInterval95(leftRuns, rightRuns, effect.Absolute); intervalOK {
			change.Interval = &interval
			change.Change = intervalAwareChange(interval, before, definition.band, definition.higherIsWorse)
		}
		change.Evidence, change.Confidence = scopeEvidenceLevel(len(leftRuns), len(rightRuns))
		result = append(result, change)
	}
	result = append(result, scopedStageMetricChanges(baseline, candidate, scope)...)
	return result
}

func scopedStageMetricChanges(baseline, candidate Summary, scope ComparisonScope) []MetricChange {
	if baseline.OperationAnalysis == nil || candidate.OperationAnalysis == nil {
		return nil
	}
	result := make([]MetricChange, 0)
	for _, match := range scope.matches {
		count := match.CommonSteps
		leftStart, rightStart := match.BaselineStepStart, match.CandidateStepStart
		if match.Exact && len(match.Baseline) > 0 && len(match.Candidate) > 0 {
			count = min(len(baseline.OperationAnalysis.Profiles[match.Baseline[0]].Steps), len(candidate.OperationAnalysis.Profiles[match.Candidate[0]].Steps))
		}
		for offset := 0; offset < count; offset++ {
			left, leftOK := aggregateStageStats(baseline.OperationAnalysis.Profiles, match.Baseline, leftStart+offset)
			right, rightOK := aggregateStageStats(candidate.OperationAnalysis.Profiles, match.Candidate, rightStart+offset)
			if !leftOK || !rightOK {
				continue
			}
			beforeNum, beforeDen, beforeOK := boundedMetricValues(left.TotalMS, left.Count)
			afterNum, afterDen, afterOK := boundedMetricValues(right.TotalMS, right.Count)
			if !beforeOK || !afterOK {
				continue
			}
			before, after := beforeNum/beforeDen, afterNum/afterDen
			effect := compareMeasurement(comparisonMeasurement{Value: before, Samples: left.Count, State: measurementObserved}, comparisonMeasurement{Value: after, Samples: right.Count, State: measurementObserved}, comparisonBand{Absolute: 1, Relative: .05}, true, true)
			result = append(result, MetricChange{
				Name: "Stage " + left.Operation + " mean duration", Scope: "stage", Stage: left.Operation,
				Domain: DomainLatency, Unit: "ms", Eligibility: eligibilityForDomain(scope.Metrics, DomainLatency),
				Before: numberPointer(before), After: numberPointer(after), Absolute: numberPointer(effect.Absolute),
				Change: effect.Change, Evidence: EvidenceObserved, Confidence: ConfidenceLimited, Samples: min(left.Count, right.Count),
			})
		}
	}
	return result
}

func aggregateStageStats(profiles []OperationProfile, indices []int, position int) (OperationStats, bool) {
	var result OperationStats
	for _, index := range indices {
		if index < 0 || index >= len(profiles) || position < 0 || position >= len(profiles[index].Steps) {
			return OperationStats{}, false
		}
		stats := profiles[index].Steps[position].Stats
		if stats.Count == 0 {
			return OperationStats{}, false
		}
		mergeOperationProfileStats(&result, stats)
	}
	return result, result.Count > 0
}

type runMetricAggregate struct{ numerator, denominator float64 }

func scopedMetricRunValues(baseline, candidate *OperationAnalysis, matches []scenarioMatch, values func(OperationStats) (float64, float64, bool)) ([]float64, []float64) {
	if baseline == nil || candidate == nil {
		return nil, nil
	}
	left := make(map[string]runMetricAggregate)
	right := make(map[string]runMetricAggregate)
	for _, match := range matches {
		if !match.Exact {
			continue
		}
		appendProfileRunMetrics(left, baseline.Profiles, match.Baseline, values)
		appendProfileRunMetrics(right, candidate.Profiles, match.Candidate, values)
	}
	return materializeRunMetrics(left), materializeRunMetrics(right)
}

func appendProfileRunMetrics(target map[string]runMetricAggregate, profiles []OperationProfile, indices []int, values func(OperationStats) (float64, float64, bool)) {
	for _, index := range indices {
		if index < 0 || index >= len(profiles) {
			continue
		}
		numerator, denominator, ok := values(profiles[index].Stats)
		if !ok || denominator <= 0 {
			continue
		}
		value := target[profiles[index].RunID]
		value.numerator += numerator
		value.denominator += denominator
		target[profiles[index].RunID] = value
	}
}

func materializeRunMetrics(values map[string]runMetricAggregate) []float64 {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]float64, 0, len(keys))
	for _, key := range keys {
		value := values[key]
		if value.denominator > 0 {
			result = append(result, value.numerator/value.denominator)
		}
	}
	return result
}

func eligibilityForDomain(values []MetricEligibility, domain ComparisonDomain) MetricEligibility {
	for _, value := range values {
		if value.Domain == domain {
			return value
		}
	}
	return MetricEligibility{Domain: domain, State: EligibilityUnknown, Reason: ReasonScenarioUnavailable}
}

func standardizedScopeMetric(baseline, candidate *OperationAnalysis, matches []scenarioMatch, values func(OperationStats) (float64, float64, bool)) (float64, float64, uint64, bool) {
	if baseline == nil || candidate == nil || len(matches) == 0 {
		return 0, 0, 0, false
	}
	var baselineNumerator, baselineDenominator, candidateWeighted float64
	var samples uint64
	for _, match := range matches {
		if !match.Exact {
			continue
		}
		beforeNum, beforeDen, beforeOK := aggregateProfileMetric(baseline.Profiles, match.Baseline, values)
		afterNum, afterDen, afterOK := aggregateProfileMetric(candidate.Profiles, match.Candidate, values)
		if !beforeOK || !afterOK || beforeDen <= 0 || afterDen <= 0 {
			return 0, 0, 0, false
		}
		baselineNumerator += beforeNum
		baselineDenominator += beforeDen
		candidateWeighted += (afterNum / afterDen) * beforeDen
		if beforeDen > float64(math.MaxUint64-samples) {
			return 0, 0, 0, false
		}
		samples += uint64(beforeDen)
	}
	if baselineDenominator <= 0 || !finiteComparisonValue(baselineNumerator) || !finiteComparisonValue(candidateWeighted) {
		return 0, 0, 0, false
	}
	return baselineNumerator / baselineDenominator, candidateWeighted / baselineDenominator, samples, true
}

func aggregateProfileMetric(profiles []OperationProfile, indices []int, values func(OperationStats) (float64, float64, bool)) (float64, float64, bool) {
	var numerator, denominator float64
	for _, index := range indices {
		if index < 0 || index >= len(profiles) {
			return 0, 0, false
		}
		n, d, ok := values(profiles[index].Stats)
		if !ok {
			return 0, 0, false
		}
		numerator += n
		denominator += d
	}
	return numerator, denominator, finiteComparisonValue(numerator) && finiteComparisonValue(denominator)
}

const maxExactMetricInteger = uint64(1 << 53)

func boundedMetricValues(numerator, denominator uint64) (float64, float64, bool) {
	if denominator == 0 || numerator > maxExactMetricInteger || denominator > maxExactMetricInteger {
		return 0, 0, false
	}
	return float64(numerator), float64(denominator), true
}
func percentMetricValues(numerator, denominator uint64) (float64, float64, bool) {
	n, d, ok := boundedMetricValues(numerator, denominator)
	return n * 100, d, ok
}
func scaledMetricValues(numerator, denominator, divisor uint64) (float64, float64, bool) {
	n, d, ok := boundedMetricValues(numerator, denominator)
	if !ok || divisor == 0 {
		return 0, 0, false
	}
	return n / float64(divisor), d, true
}
func addMetricCounters(left, right uint64) (uint64, bool) {
	if math.MaxUint64-left < right {
		return 0, false
	}
	return left + right, true
}
func numberPointer(value float64) *float64 { return &value }

func comparisonEvidenceLevel(baseline, candidate Summary) (ComparisonEvidence, ComparisonConfidence) {
	before, after := AcquisitionEvidenceFor(baseline), AcquisitionEvidenceFor(candidate)
	if !before.IdentityComplete || !after.IdentityComplete {
		return EvidenceObserved, ConfidenceLimited
	}
	return scopeEvidenceLevel(before.IndependentGroups, after.IndependentGroups)
}

func scopeEvidenceLevel(baselineGroups, candidateGroups int) (ComparisonEvidence, ComparisonConfidence) {
	groups := min(baselineGroups, candidateGroups)
	if groups >= 5 {
		return EvidenceRepeated, ConfidenceStrong
	}
	if groups >= 2 {
		return EvidenceRepeated, ConfidenceModerate
	}
	return EvidenceObserved, ConfidenceLimited
}

func compareProfileApplication(baseline, candidate *OperationAnalysis) ComparisonIdentity {
	before, beforeOK := profileIdentity(baseline, true)
	after, afterOK := profileIdentity(candidate, true)
	if !beforeOK || !afterOK {
		return ComparisonIdentity{State: EligibilityUnknown, Reason: ReasonApplicationUnknown}
	}
	if before != after {
		return ComparisonIdentity{State: EligibilityIneligible, Reason: ReasonApplicationMismatch}
	}
	return ComparisonIdentity{State: EligibilityEligible, Value: before}
}

func compareProfileProcess(baseline, candidate *OperationAnalysis) ComparisonIdentity {
	before, beforeOK := profileIdentity(baseline, false)
	after, afterOK := profileIdentity(candidate, false)
	if !beforeOK || !afterOK {
		return ComparisonIdentity{State: EligibilityUnknown, Reason: ReasonProcessUnknown}
	}
	if before != after {
		return ComparisonIdentity{State: EligibilityIneligible, Reason: ReasonProcessMismatch}
	}
	return ComparisonIdentity{State: EligibilityEligible, Value: before}
}

func profileIdentity(analysis *OperationAnalysis, application bool) (string, bool) {
	if analysis == nil {
		return "", false
	}
	value := ""
	for i := range analysis.Profiles {
		profile := &analysis.Profiles[i]
		if !profile.Root {
			continue
		}
		current := strings.TrimSpace(profile.ProcessName)
		if application {
			if separator := strings.IndexByte(current, ':'); separator >= 0 {
				current = current[:separator]
			}
		}
		if !profileAttributeValid(current) {
			return "", false
		}
		if value == "" {
			value = current
		} else if value != current {
			return "", false
		}
	}
	return value, value != ""
}

func comparisonMetricEligibility(baseline, candidate Summary, scope ComparisonScope) []MetricEligibility {
	domains := [...]ComparisonDomain{DomainLatency, DomainFailure, DomainBudget, DomainHTTP, DomainDatabase, DomainUI, DomainMemory, DomainRetention, DomainCPU, DomainIO, DomainNetworkBytes, DomainProblem}
	result := make([]MetricEligibility, 0, len(domains))
	for _, domain := range domains {
		result = append(result, metricEligibility(baseline, candidate, scope, domain))
	}
	return result
}

func metricEligibility(baseline, candidate Summary, scope ComparisonScope, domain ComparisonDomain) MetricEligibility {
	result := MetricEligibility{Domain: domain, State: EligibilityEligible}
	if scope.Comparability == ScenarioNone {
		result.State, result.Reason = EligibilityIneligible, ReasonScenarioUnavailable
		return result
	}
	if scope.Comparability == ScenarioUnknown || len(scope.matches) == 0 {
		result.State, result.Reason = EligibilityUnknown, ReasonScenarioUnavailable
		return result
	}
	if !filtersEquivalent(baseline.AnalysisFilter, candidate.AnalysisFilter) {
		result.State, result.Reason = EligibilityUnknown, ReasonFilterMismatch
		return result
	}
	if reason := collectionEligibilityReason(baseline.CollectionQuality, candidate.CollectionQuality); reason != ReasonNone {
		result.State, result.Reason = EligibilityUnknown, reason
		return result
	}
	if flag := domainCollectorFlag(domain); flag != 0 && (baseline.CollectorFlagsAll&flag == 0 || candidate.CollectorFlagsAll&flag == 0) {
		result.State, result.Reason = EligibilityIneligible, ReasonCollectorDisabled
		return result
	}
	if domain == DomainProblem && !detectorsEquivalent(baseline.Detectors, candidate.Detectors) {
		result.State, result.Reason = EligibilityUnknown, ReasonDetectorDrift
		return result
	}
	if domain == DomainLatency || domain == DomainUI || domain == DomainMemory || domain == DomainRetention || domain == DomainCPU || domain == DomainIO {
		state, reason := namedEnvironmentEligibility(baseline.Devices, candidate.Devices)
		if state != EligibilityEligible {
			result.State, result.Reason = state, reason
			return result
		}
		state, reason = namedEnvironmentEligibility(baseline.SDKs, candidate.SDKs)
		if state != EligibilityEligible {
			result.State, result.Reason = state, reason
			return result
		}
	}
	if domain == DomainHTTP || domain == DomainNetworkBytes {
		state, reason := namedEnvironmentEligibility(baseline.Network, candidate.Network)
		if state != EligibilityEligible {
			result.State, result.Reason = state, reason
		}
	}
	return result
}

func filtersEquivalent(before, after *Filter) bool {
	if before == nil {
		before = &Filter{}
	}
	if after == nil {
		after = &Filter{}
	}
	return *before == *after
}

func collectionEligibilityReason(before, after CollectionQuality) EligibilityReason {
	if before.KnownLostEvents > 0 || after.KnownLostEvents > 0 || before.BoundedEvidenceLoss > 0 || after.BoundedEvidenceLoss > 0 {
		return ReasonCollectionLoss
	}
	if !before.Complete || !after.Complete || before.DiagnosticCompletenessPercent < 0 || after.DiagnosticCompletenessPercent < 0 {
		return ReasonCollectionUnknown
	}
	return ReasonNone
}

func domainCollectorFlag(domain ComparisonDomain) uint64 {
	switch domain {
	case DomainHTTP:
		return uint64(jhlog.CollectorHTTP)
	case DomainDatabase:
		return uint64(jhlog.CollectorDatabase)
	case DomainUI:
		return uint64(jhlog.CollectorFPS | jhlog.CollectorJankStats)
	case DomainMemory:
		return uint64(jhlog.CollectorSystemSampler)
	case DomainRetention:
		return uint64(jhlog.CollectorRetainedObjects)
	case DomainCPU:
		return uint64(jhlog.CollectorSystemSampler)
	case DomainIO:
		return uint64(jhlog.CollectorIOTracing)
	case DomainNetworkBytes:
		return uint64(jhlog.CollectorHTTP)
	default:
		return 0
	}
}

func namedEnvironmentEligibility(before, after []NamedValue) (EligibilityState, EligibilityReason) {
	if namedValueTotal(before) == 0 || namedValueTotal(after) == 0 {
		return EligibilityUnknown, ReasonEnvironmentUnknown
	}
	if namedDistributionDistance(before, after) > 0 {
		return EligibilityIneligible, ReasonEnvironmentMismatch
	}
	return EligibilityEligible, ReasonNone
}

func detectorsEquivalent(before, after []DetectorMetadata) bool {
	if len(before) == 0 || len(after) == 0 || len(before) != len(after) {
		return false
	}
	left := make([]string, len(before))
	right := make([]string, len(after))
	for i := range before {
		left[i] = before[i].ID + "\x00" + before[i].Version
	}
	for i := range after {
		right[i] = after[i].ID + "\x00" + after[i].Version
	}
	sort.Strings(left)
	sort.Strings(right)
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
