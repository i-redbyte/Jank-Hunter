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
// recomposition while preserving screen/operation context and main-thread attribution.
type SemanticWorkStats struct {
	Domain           string
	Operation        string
	Outcome          string
	MainThread       bool
	Screen           string
	ContextOperation string
	Owner            string
	Count            uint64
	TotalMS          uint64
	MaxMS            uint64
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
			Screen: call.Screen, ContextOperation: call.Operation, Owner: call.Callee,
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
	if hasGeneratedComposeOwner(items) {
		items = collapseGeneratedComposeMirrors(items)
	}
	return dropShadowedContextlessComposeWork(collapseActionableComposeOwners(items))
}

func hasGeneratedComposeOwner(items []SemanticWorkStats) bool {
	for _, item := range items {
		if item.Domain == SemanticDomainCompose && composeGeneratedOwner(item.Owner) {
			return true
		}
	}
	return false
}

func collapseGeneratedComposeMirrors(items []SemanticWorkStats) []SemanticWorkStats {
	result := items[:0]
	duplicateGroups := make(map[composeDuplicateGroupKey]semanticIndexChain)
	nextDuplicate := make([]int, 0, len(items))
	for _, item := range items {
		if item.Domain != SemanticDomainCompose {
			result = append(result, item)
			nextDuplicate = append(nextDuplicate, 0)
			continue
		}
		key := composeDuplicateGroupKey{
			Operation: item.Operation, MainThread: item.MainThread,
			Screen: item.Screen, ContextOperation: item.ContextOperation, Count: item.Count,
		}
		duplicate := -1
		chain := duplicateGroups[key]
		for oneBasedIndex := chain.head; oneBasedIndex != 0; oneBasedIndex = nextDuplicate[oneBasedIndex-1] {
			index := oneBasedIndex - 1
			if composeSemanticDuplicate(result[index], item) {
				duplicate = index
				break
			}
		}
		if duplicate < 0 {
			result = append(result, item)
			nextDuplicate = append(nextDuplicate, 0)
			oneBasedIndex := len(result)
			if chain.head == 0 {
				chain.head = oneBasedIndex
			} else {
				nextDuplicate[chain.tail-1] = oneBasedIndex
			}
			chain.tail = oneBasedIndex
			duplicateGroups[key] = chain
			continue
		}
		if composeGeneratedOwner(result[duplicate].Owner) && !composeGeneratedOwner(item.Owner) {
			result[duplicate] = item
		}
	}
	return result
}

// collapseActionableComposeOwners handles nested compiler lambdas that resolve to the same
// source-level function but have slightly different inclusive timings. The boundaries overlap,
// so adding their values would double count work; retaining the largest observation preserves a
// conservative diagnosis and guarantees one actionable target in the problem report.
func collapseActionableComposeOwners(items []SemanticWorkStats) []SemanticWorkStats {
	result := items[:0]
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
	contextless := false
	for _, item := range items {
		if item.Domain == SemanticDomainCompose && !hasSemanticContext(item) {
			contextless = true
			break
		}
	}
	if !contextless {
		return items
	}
	contextualHeads := make(map[semanticTargetKey]int)
	nextContextual := make([]int, len(items))
	for index, item := range items {
		if item.Domain == SemanticDomainCompose && hasSemanticContext(item) {
			key := semanticTargetKeyFor(item)
			nextContextual[index] = contextualHeads[key]
			contextualHeads[key] = index + 1
		}
	}
	result := make([]SemanticWorkStats, 0, len(items))
	for _, item := range items {
		if item.Domain == SemanticDomainCompose && !hasSemanticContext(item) {
			shadowed := false
			for oneBasedIndex := contextualHeads[semanticTargetKeyFor(item)]; oneBasedIndex != 0; oneBasedIndex = nextContextual[oneBasedIndex-1] {
				candidate := items[oneBasedIndex-1]
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

type composeDuplicateGroupKey struct {
	Operation        string
	MainThread       bool
	Screen           string
	ContextOperation string
	Count            uint64
}

type semanticIndexChain struct {
	head int
	tail int
}

type semanticTargetKey struct {
	Domain     string
	Operation  string
	Outcome    string
	MainThread bool
	Screen     string
	Owner      string
}

func semanticTargetKeyFor(item SemanticWorkStats) semanticTargetKey {
	return semanticTargetKey{
		Domain: item.Domain, Operation: item.Operation, Outcome: item.Outcome,
		MainThread: item.MainThread, Screen: item.Screen, Owner: item.Owner,
	}
}

func hasSemanticContext(item SemanticWorkStats) bool {
	return !unknownSemanticContext(item.ContextOperation)
}

func unknownSemanticContext(value string) bool {
	value = strings.TrimSpace(value)
	return value == "" || strings.EqualFold(value, "unknown")
}

func composeSemanticDuplicate(left, right SemanticWorkStats) bool {
	if left.Domain != SemanticDomainCompose || right.Domain != SemanticDomainCompose {
		return false
	}
	if !composeGeneratedOwner(left.Owner) && !composeGeneratedOwner(right.Owner) {
		return false
	}
	if left.Operation != right.Operation || left.MainThread != right.MainThread ||
		left.Screen != right.Screen || left.ContextOperation != right.ContextOperation ||
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
	if domain == SemanticDomainWorker && hasTypedWorkerLifecycle(summary) {
		return true
	}
	for _, item := range SemanticWork(summary) {
		if item.Domain == domain {
			return true
		}
	}
	return false
}

func hasTypedWorkerLifecycle(summary Summary) bool {
	return summary.WorkerAnalysis != nil &&
		(summary.WorkerAnalysis.Enqueued+summary.WorkerAnalysis.Started+summary.WorkerAnalysis.Finished > 0)
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
	return strings.Join([]string{item.Domain, item.Operation, item.Outcome, item.Screen, item.ContextOperation, item.Owner}, "\x00")
}

func oneOf(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if value == candidate {
			return true
		}
	}
	return false
}
