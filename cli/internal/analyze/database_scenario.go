package analyze

import (
	"math"
	"math/bits"
	"sort"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

const (
	databaseScenarioGroupLimit     = 8_192
	databaseScenarioCandidateLimit = 1_024
	databaseScenarioRepeatCalls    = 4
	databaseScenarioExactScopeCap  = 4_096
	databaseScenarioHLLPrecision   = 10
	databaseScenarioHLLRegisters   = 1 << databaseScenarioHLLPrecision
)

type databaseScenarioScopeKind uint8

const (
	databaseScenarioOperationScope databaseScenarioScopeKind = iota + 1
	databaseScenarioTransactionScope
)

type databaseScenarioScopeKey struct {
	processInstanceID jhlog.ID128
	sessionID         jhlog.ID128
	logIndex          uint64
	id                uint64
	kind              databaseScenarioScopeKind
}

type databaseScenarioGroupKey struct {
	scope       databaseScenarioScopeKey
	fingerprint uint64
	operation   jhlog.DatabaseOperation
}

type databaseScenarioGroup struct {
	key               databaseScenarioGroupKey
	statement         databaseStatementKey
	source            string
	screen            string
	contextOperation  string
	estimatedCalls    uint64
	retainedCalls     uint64
	totalDurationUS   uint64
	maxDurationUS     uint64
	firstTimeUS       uint64
	lastTimeUS        uint64
	mainThreadCalls   uint64
	failures          uint64
	transactionLinked bool
}

type databaseDistinctScopeCounter struct {
	exact       map[uint64]struct{}
	registers   [databaseScenarioHLLRegisters]uint8
	approximate bool
}

func (c *databaseDistinctScopeCounter) add(hash uint64) {
	mixed := databaseSketchHash(hash, 0)
	index := mixed & (databaseScenarioHLLRegisters - 1)
	remaining := mixed >> databaseScenarioHLLPrecision
	rank := uint8(bits.LeadingZeros64(remaining) + 1 - databaseScenarioHLLPrecision)
	if rank > c.registers[index] {
		c.registers[index] = rank
	}
	if c.approximate {
		return
	}
	if c.exact == nil {
		c.exact = make(map[uint64]struct{}, 64)
	}
	if _, exists := c.exact[mixed]; exists {
		return
	}
	if len(c.exact) >= databaseScenarioExactScopeCap {
		c.approximate = true
		return
	}
	c.exact[mixed] = struct{}{}
}

func (c *databaseDistinctScopeCounter) count() uint64 {
	if !c.approximate {
		return uint64(len(c.exact))
	}
	const alpha = 0.7205407583220416
	sum := 0.0
	zeros := 0
	for _, register := range c.registers {
		sum += math.Ldexp(1, -int(register))
		if register == 0 {
			zeros++
		}
	}
	estimate := alpha * databaseScenarioHLLRegisters * databaseScenarioHLLRegisters / sum
	if zeros > 0 && estimate <= 2.5*databaseScenarioHLLRegisters {
		estimate = databaseScenarioHLLRegisters * math.Log(databaseScenarioHLLRegisters/float64(zeros))
	}
	minimum := float64(len(c.exact))
	if estimate < minimum {
		estimate = minimum
	}
	return uint64(math.Round(estimate))
}

type databaseScenarioAccumulator struct {
	capacity          int
	groups            []databaseScenarioGroup
	groupIndex        map[databaseScenarioGroupKey]int
	heap              []int
	heapPosition      []int
	frequency         databaseFrequencySketch
	operationScopes   databaseDistinctScopeCounter
	transactionScopes databaseDistinctScopeCounter
	droppedEvents     uint64
	evictedGroups     uint64
}

func newDatabaseScenarioAccumulator(capacity int) databaseScenarioAccumulator {
	return databaseScenarioAccumulator{capacity: capacity}
}

func (a *databaseScenarioAccumulator) add(
	statement databaseStatementKey,
	context databaseContextKey,
	event *jhlog.DatabaseEvent,
	flags, logIndex, timeUS uint64,
) {
	if event == nil || a.capacity == 0 {
		return
	}
	fingerprint := event.StatementFingerprint
	if fingerprint == 0 {
		fingerprint = databaseStatementKeyHash(statement)
	}
	if context.operationID != 0 {
		scope := databaseScenarioScopeKey{
			processInstanceID: context.processInstanceID,
			sessionID:         context.sessionID,
			id:                context.operationID,
			kind:              databaseScenarioOperationScope,
		}
		if context.processInstanceID.IsZero() || context.sessionID.IsZero() {
			scope.logIndex = logIndex
		}
		a.operationScopes.add(databaseScenarioScopeHash(scope))
		a.addGroup(scope, fingerprint, statement, context, event, flags, timeUS)
	}
	if event.TransactionID != 0 {
		scope := databaseScenarioScopeKey{
			processInstanceID: context.processInstanceID,
			sessionID:         context.sessionID,
			id:                event.TransactionID,
			kind:              databaseScenarioTransactionScope,
		}
		if context.processInstanceID.IsZero() || context.sessionID.IsZero() {
			scope.logIndex = logIndex
		}
		a.transactionScopes.add(databaseScenarioScopeHash(scope))
		a.addGroup(scope, fingerprint, statement, context, event, flags, timeUS)
	}
}

func (a *databaseScenarioAccumulator) addGroup(
	scope databaseScenarioScopeKey,
	fingerprint uint64,
	statement databaseStatementKey,
	context databaseContextKey,
	event *jhlog.DatabaseEvent,
	flags, timeUS uint64,
) {
	key := databaseScenarioGroupKey{scope: scope, fingerprint: fingerprint, operation: event.Operation}
	hash := databaseScenarioGroupHash(key)
	estimatedCalls := a.frequency.add(hash)
	if index, exists := a.groupIndex[key]; exists {
		group := &a.groups[index]
		databaseScenarioAddSample(group, event, flags, timeUS)
		group.estimatedCalls = maxUint64(group.estimatedCalls, estimatedCalls)
		a.scenarioHeapDown(a.heapPosition[index])
		return
	}
	candidate := databaseScenarioGroup{
		key: key, statement: statement, source: context.source, screen: context.screen,
		contextOperation: context.contextOperation, estimatedCalls: estimatedCalls,
		transactionLinked: scope.kind == databaseScenarioTransactionScope,
	}
	databaseScenarioAddSample(&candidate, event, flags, timeUS)
	if a.groupIndex == nil {
		a.groupIndex = make(map[databaseScenarioGroupKey]int, min(a.capacity, 256))
		a.groups = make([]databaseScenarioGroup, 0, a.capacity)
		a.heap = make([]int, 0, a.capacity)
		a.heapPosition = make([]int, 0, a.capacity)
	}
	if len(a.groups) < a.capacity {
		index := len(a.groups)
		a.groups = append(a.groups, candidate)
		a.groupIndex[key] = index
		a.heap = append(a.heap, index)
		a.heapPosition = append(a.heapPosition, len(a.heap)-1)
		a.scenarioHeapUp(len(a.heap) - 1)
		return
	}
	root := a.heap[0]
	if !databaseScenarioGroupLess(a.groups[root], candidate) {
		a.droppedEvents = saturatingUint64Sum(a.droppedEvents, 1)
		return
	}
	discardedEvents := a.groups[root].retainedCalls
	delete(a.groupIndex, a.groups[root].key)
	a.groups[root] = candidate
	a.groupIndex[key] = root
	a.droppedEvents = saturatingUint64Sum(a.droppedEvents, discardedEvents)
	a.evictedGroups = saturatingUint64Sum(a.evictedGroups, 1)
	a.scenarioHeapDown(0)
}

func databaseScenarioAddSample(
	group *databaseScenarioGroup,
	event *jhlog.DatabaseEvent,
	flags, timeUS uint64,
) {
	if group.retainedCalls == 0 || timeUS < group.firstTimeUS {
		group.firstTimeUS = timeUS
	}
	group.retainedCalls++
	group.totalDurationUS = saturatingUint64Sum(group.totalDurationUS, event.DurationUS)
	group.maxDurationUS = maxUint64(group.maxDurationUS, event.DurationUS)
	group.lastTimeUS = maxUint64(group.lastTimeUS, timeUS)
	if flags&uint64(jhlog.FlagThreadMain) != 0 {
		group.mainThreadCalls++
	}
	if event.Outcome == jhlog.DatabaseOutcomeFailure {
		group.failures++
	}
}

func (a *databaseScenarioAccumulator) scenarioHeapUp(position int) {
	for position > 0 {
		parent := (position - 1) >> 1
		if !a.scenarioHeapLess(position, parent) {
			return
		}
		a.scenarioHeapSwap(position, parent)
		position = parent
	}
}

func (a *databaseScenarioAccumulator) scenarioHeapDown(position int) {
	for {
		left := position*2 + 1
		if left >= len(a.heap) {
			return
		}
		smallest := left
		right := left + 1
		if right < len(a.heap) && a.scenarioHeapLess(right, left) {
			smallest = right
		}
		if !a.scenarioHeapLess(smallest, position) {
			return
		}
		a.scenarioHeapSwap(position, smallest)
		position = smallest
	}
}

func (a *databaseScenarioAccumulator) scenarioHeapLess(left, right int) bool {
	return databaseScenarioGroupLess(a.groups[a.heap[left]], a.groups[a.heap[right]])
}

func (a *databaseScenarioAccumulator) scenarioHeapSwap(left, right int) {
	a.heap[left], a.heap[right] = a.heap[right], a.heap[left]
	a.heapPosition[a.heap[left]] = left
	a.heapPosition[a.heap[right]] = right
}

func databaseScenarioGroupLess(left, right databaseScenarioGroup) bool {
	if left.estimatedCalls != right.estimatedCalls {
		return left.estimatedCalls < right.estimatedCalls
	}
	if left.totalDurationUS != right.totalDurationUS {
		return left.totalDurationUS < right.totalDurationUS
	}
	if left.mainThreadCalls != right.mainThreadCalls {
		return left.mainThreadCalls < right.mainThreadCalls
	}
	if left.failures != right.failures {
		return left.failures < right.failures
	}
	return databaseScenarioGroupHash(left.key) < databaseScenarioGroupHash(right.key)
}

type databaseScenarioAggregateKey struct {
	fingerprint      uint64
	operation        jhlog.DatabaseOperation
	scopeKind        databaseScenarioScopeKind
	contextOperation string
	source           string
	screen           string
}

func (a *databaseScenarioAccumulator) finalize() DatabaseScenarioAnalysis {
	result := DatabaseScenarioAnalysis{
		ObservedOperationScopes:   a.operationScopes.count(),
		ObservedTransactionScopes: a.transactionScopes.count(),
		ScopeCountsApproximated:   a.operationScopes.approximate || a.transactionScopes.approximate,
		DroppedEvents:             a.droppedEvents, EvictedGroups: a.evictedGroups,
		FrequencyEstimateError: a.frequency.estimatedError(),
	}
	if len(a.groups) == 0 {
		return result
	}
	aggregates := make(map[databaseScenarioAggregateKey]*DatabaseScenarioStats)
	for index := range a.groups {
		group := &a.groups[index]
		if group.estimatedCalls < databaseScenarioRepeatCalls {
			continue
		}
		key := databaseScenarioAggregateKey{
			fingerprint: group.key.fingerprint, operation: group.key.operation,
			scopeKind: group.key.scope.kind, contextOperation: group.contextOperation,
			source: group.source, screen: group.screen,
		}
		candidate := aggregates[key]
		if candidate == nil {
			candidate = &DatabaseScenarioStats{
				Kind: databaseScenarioKind(group.key.operation), ClaimLevel: "hypothesis",
				ScopeKind: databaseScenarioScopeName(group.key.scope.kind),
				Query:     group.statement.query, Source: group.source, Screen: group.screen,
				ContextOperation:     group.contextOperation,
				Operation:            databaseOperationName(group.key.operation),
				StatementFingerprint: group.key.fingerprint,
			}
			aggregates[key] = candidate
		}
		candidate.AffectedScopes++
		if group.transactionLinked {
			candidate.TransactionLinkedScopes++
		}
		candidate.EstimatedCalls = saturatingUint64Sum(candidate.EstimatedCalls, group.estimatedCalls)
		candidate.RetainedCalls = saturatingUint64Sum(candidate.RetainedCalls, group.retainedCalls)
		candidate.MaxCallsPerScope = maxUint64(candidate.MaxCallsPerScope, group.estimatedCalls)
		candidate.TotalDurationUS = saturatingUint64Sum(candidate.TotalDurationUS, group.totalDurationUS)
		candidate.MaxDurationUS = maxUint64(candidate.MaxDurationUS, group.maxDurationUS)
		candidate.MaxSequenceSpanUS = maxUint64(
			candidate.MaxSequenceSpanUS,
			saturatingUint64Sub(group.lastTimeUS, group.firstTimeUS),
		)
		candidate.MainThreadCalls = saturatingUint64Sum(candidate.MainThreadCalls, group.mainThreadCalls)
		candidate.Failures = saturatingUint64Sum(candidate.Failures, group.failures)
		candidate.CostLowerBound = candidate.CostLowerBound || group.retainedCalls < group.estimatedCalls
	}
	result.Candidates = make([]DatabaseScenarioStats, 0, min(len(aggregates), databaseScenarioCandidateLimit))
	for _, candidate := range aggregates {
		if candidate.ScopeKind == "operation" {
			candidate.ObservedScopes = result.ObservedOperationScopes
		} else {
			candidate.ObservedScopes = result.ObservedTransactionScopes
		}
		candidate.CallsPerAffectedScope = databaseScenarioRatio(candidate.EstimatedCalls, candidate.AffectedScopes)
		candidate.CallsPerObservedScope = databaseScenarioRatio(candidate.EstimatedCalls, candidate.ObservedScopes)
		result.Candidates = append(result.Candidates, *candidate)
	}
	sort.Slice(result.Candidates, func(i, j int) bool {
		left, right := result.Candidates[i], result.Candidates[j]
		if left.MainThreadCalls != right.MainThreadCalls {
			return left.MainThreadCalls > right.MainThreadCalls
		}
		if left.TotalDurationUS != right.TotalDurationUS {
			return left.TotalDurationUS > right.TotalDurationUS
		}
		if left.MaxCallsPerScope != right.MaxCallsPerScope {
			return left.MaxCallsPerScope > right.MaxCallsPerScope
		}
		if left.Query != right.Query {
			return left.Query < right.Query
		}
		return left.ScopeKind < right.ScopeKind
	})
	if len(result.Candidates) > databaseScenarioCandidateLimit {
		result.DroppedCandidates = uint64(len(result.Candidates) - databaseScenarioCandidateLimit)
		result.Candidates = result.Candidates[:databaseScenarioCandidateLimit]
	}
	return result
}

func databaseScenarioRatio(numerator, denominator uint64) float64 {
	if denominator == 0 {
		return 0
	}
	return float64(numerator) / float64(denominator)
}

func databaseScenarioKind(operation jhlog.DatabaseOperation) string {
	switch operation {
	case jhlog.DatabaseOperationInsert, jhlog.DatabaseOperationUpdate,
		jhlog.DatabaseOperationDelete, jhlog.DatabaseOperationExecute,
		jhlog.DatabaseOperationStatement:
		return "batch_candidate"
	default:
		return "possible_n_plus_one_or_duplicate"
	}
}

func databaseScenarioScopeName(kind databaseScenarioScopeKind) string {
	if kind == databaseScenarioTransactionScope {
		return "transaction"
	}
	return "operation"
}

func databaseScenarioScopeHash(scope databaseScenarioScopeKey) uint64 {
	hash := databaseHashOffset
	for _, value := range scope.processInstanceID {
		hash = databaseHashByte(hash, value)
	}
	for _, value := range scope.sessionID {
		hash = databaseHashByte(hash, value)
	}
	hash = databaseHashUint64(hash, scope.logIndex)
	hash = databaseHashUint64(hash, scope.id)
	return databaseHashByte(hash, byte(scope.kind))
}

func databaseScenarioGroupHash(group databaseScenarioGroupKey) uint64 {
	hash := databaseScenarioScopeHash(group.scope)
	hash = databaseHashUint64(hash, group.fingerprint)
	return databaseHashByte(hash, byte(group.operation))
}
