package analyze

import (
	"sort"
	"strings"
)

const semanticRuntimeCallerPrefix = "jankhunter.semantic.v1."

const (
	SemanticDomainCompose = "compose"
	SemanticDomainRoom    = "room"
	SemanticDomainWorker  = "worker"
)

// SemanticWorkStats is a typed projection of a runtime-call root edge produced by the SDK.
// Keeping it on the existing bounded runtime-call transport avoids emitting an event for every
// recomposition while preserving screen/flow/step context and main-thread attribution.
type SemanticWorkStats struct {
	Domain     string
	Operation  string
	Outcome    string
	MainThread bool
	Screen     string
	Flow       string
	Step       string
	Owner      string
	Count      uint64
	TotalMS    uint64
	MaxMS      uint64
}

func SemanticWork(summary Summary) []SemanticWorkStats {
	result := make([]SemanticWorkStats, 0)
	for _, call := range summary.RuntimeCalls {
		domain, operation, outcome, mainThread, ok := parseSemanticRuntimeCaller(call.Caller)
		if !ok {
			continue
		}
		result = append(result, SemanticWorkStats{
			Domain: domain, Operation: operation, Outcome: outcome, MainThread: mainThread,
			Screen: call.Screen, Flow: call.Flow, Step: call.Step, Owner: call.Callee,
			Count: call.Count, TotalMS: call.TotalMS, MaxMS: call.MaxMS,
		})
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Domain != result[j].Domain {
			return result[i].Domain < result[j].Domain
		}
		if result[i].MainThread != result[j].MainThread {
			return result[i].MainThread
		}
		if result[i].MaxMS != result[j].MaxMS {
			return result[i].MaxMS > result[j].MaxMS
		}
		if result[i].Count != result[j].Count {
			return result[i].Count > result[j].Count
		}
		return semanticWorkKey(result[i]) < semanticWorkKey(result[j])
	})
	return result
}

// ActionableSemanticWork removes compiler-generated Compose boundaries that mirror a named
// composable with the same measurements. Raw RuntimeCalls remain untouched for drill-downs.
func ActionableSemanticWork(summary Summary) []SemanticWorkStats {
	items := SemanticWork(summary)
	result := make([]SemanticWorkStats, 0, len(items))
	for _, item := range items {
		duplicate := -1
		for index := range result {
			if composeSemanticDuplicate(result[index], item) {
				duplicate = index
				break
			}
		}
		if duplicate < 0 {
			result = append(result, item)
			continue
		}
		if composeGeneratedOwner(result[duplicate].Owner) && !composeGeneratedOwner(item.Owner) {
			result[duplicate] = item
		}
	}
	return dropShadowedContextlessComposeWork(collapseActionableComposeOwners(result))
}

// collapseActionableComposeOwners handles nested compiler lambdas that resolve to the same
// source-level function but have slightly different inclusive timings. The boundaries overlap,
// so adding their values would double count work; retaining the largest observation preserves a
// conservative diagnosis and guarantees one actionable target in the problem report.
func collapseActionableComposeOwners(items []SemanticWorkStats) []SemanticWorkStats {
	result := make([]SemanticWorkStats, 0, len(items))
	positions := make(map[string]int, len(items))
	for _, item := range items {
		if item.Domain == SemanticDomainCompose {
			item.Owner = actionableComposeOwner(item.Owner)
		}
		key := semanticWorkKey(item)
		index, exists := positions[key]
		if !exists {
			positions[key] = len(result)
			result = append(result, item)
			continue
		}
		result[index].Count = maxUint64(result[index].Count, item.Count)
		result[index].TotalMS = maxUint64(result[index].TotalMS, item.TotalMS)
		result[index].MaxMS = maxUint64(result[index].MaxMS, item.MaxMS)
	}
	return result
}

// dropShadowedContextlessComposeWork removes an initial, unattributed observation when the same
// source-level operation was measured at least as strongly inside a named scenario. The raw row
// remains available through SemanticWork and RuntimeCalls; only the primary diagnosis is reduced.
func dropShadowedContextlessComposeWork(items []SemanticWorkStats) []SemanticWorkStats {
	result := make([]SemanticWorkStats, 0, len(items))
	for index, item := range items {
		if item.Domain == SemanticDomainCompose && !hasSemanticContext(item) {
			shadowed := false
			for candidateIndex, candidate := range items {
				if candidateIndex == index || !hasSemanticContext(candidate) || !sameSemanticTarget(item, candidate) {
					continue
				}
				if candidate.Count >= item.Count && candidate.TotalMS >= item.TotalMS && candidate.MaxMS >= item.MaxMS {
					shadowed = true
					break
				}
			}
			if shadowed {
				continue
			}
		}
		result = append(result, item)
	}
	return result
}

func hasSemanticContext(item SemanticWorkStats) bool {
	return !unknownSemanticContext(item.Flow) || !unknownSemanticContext(item.Step)
}

func unknownSemanticContext(value string) bool {
	value = strings.TrimSpace(value)
	return value == "" || strings.EqualFold(value, "unknown")
}

func sameSemanticTarget(left, right SemanticWorkStats) bool {
	return left.Domain == right.Domain && left.Operation == right.Operation && left.Outcome == right.Outcome &&
		left.MainThread == right.MainThread && left.Screen == right.Screen && left.Owner == right.Owner
}

func composeSemanticDuplicate(left, right SemanticWorkStats) bool {
	if left.Domain != SemanticDomainCompose || right.Domain != SemanticDomainCompose {
		return false
	}
	if !composeGeneratedOwner(left.Owner) && !composeGeneratedOwner(right.Owner) {
		return false
	}
	if left.Operation != right.Operation || left.MainThread != right.MainThread ||
		left.Screen != right.Screen || left.Flow != right.Flow || left.Step != right.Step ||
		left.Count != right.Count || absoluteDifference(left.MaxMS, right.MaxMS) > 1 {
		return false
	}
	tolerance := maxUint64(5, maxUint64(left.TotalMS, right.TotalMS)/20)
	return absoluteDifference(left.TotalMS, right.TotalMS) <= tolerance
}

func composeGeneratedOwner(owner string) bool {
	return strings.Contains(owner, "$$inlined$") || strings.Contains(owner, "$lambda$") ||
		strings.Contains(owner, "ComposableSingletons$")
}

func actionableComposeOwner(owner string) string {
	if index := strings.Index(owner, "$$inlined$"); index >= 0 {
		prefix := owner[:index]
		if classEnd := strings.Index(prefix, "$"); classEnd >= 0 {
			className := prefix[:classEnd]
			functionName := strings.Trim(prefix[classEnd+1:], "$")
			if lambda := strings.Index(functionName, "$lambda$"); lambda >= 0 {
				functionName = functionName[:lambda]
			}
			if functionName != "" {
				return className + "." + functionName
			}
		}
	}
	if index := strings.Index(owner, "$lambda$"); index >= 0 {
		return strings.TrimSuffix(owner[:index], ".")
	}
	return owner
}

func absoluteDifference(left, right uint64) uint64 {
	if left >= right {
		return left - right
	}
	return right - left
}

func IsSemanticRuntimeCall(caller string) bool {
	return strings.HasPrefix(caller, semanticRuntimeCallerPrefix)
}

func hasSemanticDomain(summary Summary, domain string) bool {
	for _, item := range SemanticWork(summary) {
		if item.Domain == domain {
			return true
		}
	}
	return false
}

func parseSemanticRuntimeCaller(caller string) (domain, operation, outcome string, mainThread, ok bool) {
	value, found := strings.CutPrefix(caller, semanticRuntimeCallerPrefix)
	if !found {
		return "", "", "", false, false
	}
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return "", "", "", false, false
	}
	thread := parts[2]
	if thread != "main" && thread != "background" {
		return "", "", "", false, false
	}
	mainThread = thread == "main"
	switch parts[0] {
	case SemanticDomainCompose:
		if !oneOf(parts[1], "composition", "measure", "layout", "draw") {
			return "", "", "", false, false
		}
		return parts[0], parts[1], "", mainThread, true
	case SemanticDomainRoom:
		if parts[1] != "dao" {
			return "", "", "", false, false
		}
		return parts[0], parts[1], "", mainThread, true
	case SemanticDomainWorker:
		if !oneOf(parts[1], "success", "failure", "retry", "cancelled", "unknown") {
			return "", "", "", false, false
		}
		return parts[0], "execution", parts[1], mainThread, true
	default:
		return "", "", "", false, false
	}
}

func semanticWorkKey(item SemanticWorkStats) string {
	return strings.Join([]string{item.Domain, item.Operation, item.Outcome, item.Screen, item.Flow, item.Step, item.Owner}, "\x00")
}

func oneOf(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if value == candidate {
			return true
		}
	}
	return false
}
