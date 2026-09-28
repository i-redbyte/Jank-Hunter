package analyze

import (
	"encoding/hex"
	"sort"
	"unicode/utf8"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

const operationProfileLimit = 2048
const operationProfileAttributeBytes = 128

// OperationProfile preserves joint conditions and acquisition identity. Marginal
// dimension tables cannot reconstruct these correlations. This alone is not proof
// of scenario equivalence: ordered stages and domain eligibility remain separate.
type OperationProfile struct {
	Root              bool                        `json:"root"`
	Steps             []OperationProfileStep      `json:"steps,omitempty"`
	RunID             string                      `json:"run_id"`
	ProcessInstanceID string                      `json:"process_instance_id"`
	ProcessName       string                      `json:"process_name"`
	Attributes        []OperationProfileAttribute `json:"attributes,omitempty"`
	Stats             OperationStats              `json:"stats"`
}
type OperationProfileAttribute struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}
type operationProfileKey struct {
	root         bool
	processName  string
	sequence     uint64
	run, process jhlog.ID128
	group        operationGroupKey
	attributes   [jhlog.MaxOperationAttributes]operationAttributeValue
	count        uint8
}

func profileAttributeValid(value string) bool {
	return value != "" && value != "unknown" && len(value) <= operationProfileAttributeBytes && utf8.ValidString(value)
}
func profileKey(active *activeOperation) (operationProfileKey, bool) {
	key := operationProfileKey{root: active.parentID == 0, processName: active.profileProcessName, run: active.profileRun, process: active.key.process, group: active.group, count: active.attributeCount, sequence: active.profileDigest}
	if !active.profileValid || active.profilePendingStages != 0 || key.run.IsZero() || key.process.IsZero() || int(key.count) > len(key.attributes) {
		return key, false
	}
	for i := 0; i < int(key.count); i++ {
		value := active.firstAttribute
		if i > 0 {
			value = active.moreAttributes[i-1]
		}
		if !profileAttributeValid(value.key) || !profileAttributeValid(value.value) {
			return key, false
		}
		// At most eight entries: insertion sort avoids reflection, closures and a
		// per-operation temporary slice while canonicalizing order without hashing away values.
		pos := i
		for pos > 0 && key.attributes[pos-1].key > value.key {
			key.attributes[pos] = key.attributes[pos-1]
			pos--
		}
		if pos > 0 && key.attributes[pos-1].key == value.key {
			return key, false
		}
		key.attributes[pos] = value
	}
	return key, true
}

// A handle stays stable in completedContexts when its initially shared aggregate
// is detached. Late events therefore follow the correct profile after a split.
type operationProfileAggregate struct {
	aggregate *operationAggregate
	steps     []OperationProfileStep
}

func cloneOperationAggregate(source *operationAggregate) *operationAggregate {
	result := *source
	if source.durationsMS.exact != nil {
		result.durationsMS.exact = append([]uint64(nil), source.durationsMS.exact...)
	}
	if source.durationsMS.approximation != nil {
		copy := *source.durationsMS.approximation
		result.durationsMS.approximation = &copy
	}
	return &result
}
func (a *operationAnalysisAccumulator) detachProfile(group operationGroupKey, keep *operationProfileAggregate) {
	owner := a.profileOwners[group]
	if owner != nil && owner != keep {
		owner.aggregate = cloneOperationAggregate(owner.aggregate)
		delete(a.profileOwners, group)
	}
}
func (a *operationAnalysisAccumulator) recordProfile(active *activeOperation, finish *jhlog.OperationEvent, durationMS uint64, overall *operationAggregate) {
	if !active.included {
		return
	}
	key, valid := profileKey(active)
	if !valid {
		a.detachProfile(active.group, nil)
		a.invalidProfileSamples++
		return
	}
	profile := a.profiles[key]
	if profile != nil && !profileStepsEqual(profile.steps, active.profileSteps) {
		a.detachProfile(active.group, nil)
		a.invalidProfileSamples++
		return
	}
	if profile != nil {
		for index := range profile.steps {
			mergeOperationProfileStats(&profile.steps[index].Stats, active.profileSteps[index].Stats)
		}
	}
	a.detachProfile(active.group, profile)
	if profile == nil {
		if len(a.profiles) >= operationProfileLimit {
			a.droppedProfileSamples++
			return
		}
		if a.profiles == nil {
			a.profiles = make(map[operationProfileKey]*operationProfileAggregate)
		}
		profile = &operationProfileAggregate{steps: append([]OperationProfileStep(nil), active.profileSteps...)}
		if overall != nil && overall.count == 0 {
			profile.aggregate = overall
			if a.profileOwners == nil {
				a.profileOwners = make(map[operationGroupKey]*operationProfileAggregate)
			}
			a.profileOwners[active.group] = profile
		} else {
			profile.aggregate = &operationAggregate{}
		}
		a.profiles[key] = profile
	}
	if profile.aggregate != overall {
		profile.aggregate.add(durationMS, finish.DurationUS, active.budgetUS, finish.Outcome, active.inclusive, active.database)
	}
	active.profileAggregate = profile
}

func mergeOperationProfileStats(target *OperationStats, source OperationStats) {
	if target.Count == 0 {
		*target = source
		return
	}
	target.Count = saturatingUint64Sum(target.Count, source.Count)
	target.Success = saturatingUint64Sum(target.Success, source.Success)
	target.Failures = saturatingUint64Sum(target.Failures, source.Failures)
	target.Cancelled = saturatingUint64Sum(target.Cancelled, source.Cancelled)
	target.Timeouts = saturatingUint64Sum(target.Timeouts, source.Timeouts)
	target.TotalMS = saturatingUint64Sum(target.TotalMS, source.TotalMS)
	target.MaxMS = maxUint64(target.MaxMS, source.MaxMS)
	target.Budgeted = saturatingUint64Sum(target.Budgeted, source.Budgeted)
	target.BudgetBreaches = saturatingUint64Sum(target.BudgetBreaches, source.BudgetBreaches)
	target.CorrelatedHTTP = saturatingUint64Sum(target.CorrelatedHTTP, source.CorrelatedHTTP)
	target.CorrelatedHTTPFailures = saturatingUint64Sum(target.CorrelatedHTTPFailures, source.CorrelatedHTTPFailures)
	target.CorrelatedHTTPDurationMS = saturatingUint64Sum(target.CorrelatedHTTPDurationMS, source.CorrelatedHTTPDurationMS)
	target.CorrelatedHTTPRxBytes = saturatingUint64Sum(target.CorrelatedHTTPRxBytes, source.CorrelatedHTTPRxBytes)
	target.CorrelatedHTTPTxBytes = saturatingUint64Sum(target.CorrelatedHTTPTxBytes, source.CorrelatedHTTPTxBytes)
	target.CorrelatedHTTPBytesKnown = saturatingUint64Sum(target.CorrelatedHTTPBytesKnown, source.CorrelatedHTTPBytesKnown)
	target.CorrelatedDatabase = saturatingUint64Sum(target.CorrelatedDatabase, source.CorrelatedDatabase)
	target.CorrelatedDatabaseErrors = saturatingUint64Sum(target.CorrelatedDatabaseErrors, source.CorrelatedDatabaseErrors)
	target.CorrelatedDatabaseMain = saturatingUint64Sum(target.CorrelatedDatabaseMain, source.CorrelatedDatabaseMain)
	target.CorrelatedDatabaseUS = saturatingUint64Sum(target.CorrelatedDatabaseUS, source.CorrelatedDatabaseUS)
	target.CorrelatedUIFrames = saturatingUint64Sum(target.CorrelatedUIFrames, source.CorrelatedUIFrames)
	target.CorrelatedUIJank = saturatingUint64Sum(target.CorrelatedUIJank, source.CorrelatedUIJank)
	target.CorrelatedIO = saturatingUint64Sum(target.CorrelatedIO, source.CorrelatedIO)
	target.CorrelatedIODurationUS = saturatingUint64Sum(target.CorrelatedIODurationUS, source.CorrelatedIODurationUS)
	target.CorrelatedIOBytes = saturatingUint64Sum(target.CorrelatedIOBytes, source.CorrelatedIOBytes)
	target.CorrelatedIOBytesKnown = saturatingUint64Sum(target.CorrelatedIOBytesKnown, source.CorrelatedIOBytesKnown)
	target.CorrelatedCPUSumX100 = saturatingUint64Sum(target.CorrelatedCPUSumX100, source.CorrelatedCPUSumX100)
	target.CorrelatedCPUSamples = saturatingUint64Sum(target.CorrelatedCPUSamples, source.CorrelatedCPUSamples)
	target.CorrelatedRetainedObjects = saturatingUint64Sum(target.CorrelatedRetainedObjects, source.CorrelatedRetainedObjects)
	target.CorrelatedMetricEvents = saturatingUint64Sum(target.CorrelatedMetricEvents, source.CorrelatedMetricEvents)
	target.MaxPSSKB = maxUint64(target.MaxPSSKB, source.MaxPSSKB)
	if target.Budgeted > 0 {
		target.BudgetBreachRatePct = float64(target.BudgetBreaches) * 100 / float64(target.Budgeted)
	}
	if target.CorrelatedUIFrames > 0 {
		target.CorrelatedUIJankRatePct = float64(target.CorrelatedUIJank) * 100 / float64(target.CorrelatedUIFrames)
	}
}
func (a *operationAnalysisAccumulator) profileRows() []OperationProfile {
	if len(a.profiles) == 0 {
		return nil
	}
	result := make([]OperationProfile, 0, len(a.profiles))
	for key, profile := range a.profiles {
		row := OperationProfile{Root: key.root, Steps: profile.steps, RunID: hex.EncodeToString(key.run[:]), ProcessInstanceID: hex.EncodeToString(key.process[:]), ProcessName: key.processName, Stats: operationStats(key.group, profile.aggregate)}
		if key.count > 0 {
			row.Attributes = make([]OperationProfileAttribute, key.count)
			for i := range row.Attributes {
				row.Attributes[i] = OperationProfileAttribute{Key: key.attributes[i].key, Value: key.attributes[i].value}
			}
		}
		result = append(result, row)
	}
	sort.Slice(result, func(i, j int) bool { return operationProfileLess(result[i], result[j]) })
	return result
}
func operationProfileLess(left, right OperationProfile) bool {
	if left.Root != right.Root {
		return left.Root
	}
	if left.RunID != right.RunID {
		return left.RunID < right.RunID
	}
	if left.ProcessInstanceID != right.ProcessInstanceID {
		return left.ProcessInstanceID < right.ProcessInstanceID
	}
	if left.ProcessName != right.ProcessName {
		return left.ProcessName < right.ProcessName
	}
	l := operationGroupKey{name: left.Stats.Operation, kind: left.Stats.Kind, screen: left.Stats.Screen}
	r := operationGroupKey{name: right.Stats.Operation, kind: right.Stats.Kind, screen: right.Stats.Screen}
	if l != r {
		return operationGroupLess(l, r)
	}
	for i := 0; i < len(left.Attributes) && i < len(right.Attributes); i++ {
		if left.Attributes[i].Key != right.Attributes[i].Key {
			return left.Attributes[i].Key < right.Attributes[i].Key
		}
		if left.Attributes[i].Value != right.Attributes[i].Value {
			return left.Attributes[i].Value < right.Attributes[i].Value
		}
	}
	if len(left.Attributes) != len(right.Attributes) {
		return len(left.Attributes) < len(right.Attributes)
	}
	return profileStepsLess(left.Steps, right.Steps)
}

// Attribute values require dictionary text; unresolved local IDs are scoped to a
// file and cannot be used as equal workload categories across acquisitions.
func profileSymbolsKnown(dict map[uint64]string, operation *jhlog.OperationEvent) bool {
	if operation.NameRef.IsUnknown() {
		return false
	}
	if !operation.NameRef.Stable && dict[operation.NameRef.ID] == "" {
		return false
	}
	for _, attribute := range operation.Attributes {
		if attribute.KeyRef.Stable || attribute.ValueRef.Stable ||
			dict[attribute.KeyRef.ID] == "" || dict[attribute.ValueRef.ID] == "" {
			return false
		}
	}
	return true
}
