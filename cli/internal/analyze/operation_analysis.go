package analyze

import (
	"encoding/binary"
	"fmt"
	"math"
	"math/bits"
	"sort"
	"strings"
	"time"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

const (
	operationIncidentLimit          = 100
	operationActiveLimit            = 4_096
	operationIgnoredActiveLimit     = 4_096
	operationCompletedContextLimit  = 4_096
	operationCompletedTableSize     = 8_192
	operationParentDepthLimit       = 64
	operationGroupLimit             = 2_048
	operationTimeSlotLimit          = 8_192
	operationDimensionLimit         = 8_192
	operationStageLimit             = 4_096
	operationComparisonMinSample    = 20
	operationExactDurationLimit     = 128
	operationDatabaseStatementLimit = 4
	operationHourMS                 = uint64(60 * 60 * 1000)
)

type operationInstanceKey struct {
	process jhlog.ID128
	id      uint64
}

type operationGroupKey struct {
	name   string
	kind   string
	screen string
}

type operationSlotKey struct {
	startUnixMS uint64
	offsetMin   int64
	group       operationGroupKey
}

type operationDimensionKey struct {
	group operationGroupKey
	key   string
	value string
}

type operationStageKey struct {
	parent operationGroupKey
	stage  string
}

type operationAttributeValue struct {
	key   string
	value string
}

type activeOperation struct {
	key            operationInstanceKey
	parentID       uint64
	group          operationGroupKey
	startUnixMS    uint64
	offsetMin      int64
	budgetUS       uint64
	firstAttribute operationAttributeValue
	moreAttributes []operationAttributeValue
	attributeCount uint8
	inclusive      operationSignals
	database       operationDatabaseSignals
	included       bool
}

type completedOperationContext struct {
	parentID      uint64
	name          string
	included      bool
	aggregate     *operationAggregate
	slotAggregate *operationAggregate
}

type completedOperationEntry struct {
	key     operationInstanceKey
	hash    uint64
	context completedOperationContext
}

// completedOperationStore is a fixed-capacity FIFO ring backed by an allocation-stable open-
// addressed index. Backward-shift deletion prevents tombstones and map bucket growth during long
// analyses with high operation churn.
type completedOperationStore struct {
	entries []completedOperationEntry
	slots   []uint16
	cursor  int
}

func (s *completedOperationStore) get(key operationInstanceKey) (completedOperationContext, bool) {
	if len(s.slots) == 0 {
		return completedOperationContext{}, false
	}
	_, index, found := s.find(key, operationInstanceHash(key))
	if !found {
		return completedOperationContext{}, false
	}
	return s.entries[index].context, true
}

func (s *completedOperationStore) put(
	key operationInstanceKey,
	context completedOperationContext,
) bool {
	if len(s.slots) == 0 {
		s.entries = make([]completedOperationEntry, 0, operationCompletedContextLimit)
		s.slots = make([]uint16, operationCompletedTableSize)
	}
	hash := operationInstanceHash(key)
	_, index, found := s.find(key, hash)
	if found {
		s.entries[index].context = context
		return false
	}
	evicted := len(s.entries) == operationCompletedContextLimit
	if evicted {
		index = s.cursor
		oldEntry := s.entries[index]
		oldSlot, _, oldFound := s.find(oldEntry.key, oldEntry.hash)
		if !oldFound {
			panic("completed operation index is inconsistent")
		}
		s.deleteSlot(oldSlot)
		s.entries[index] = completedOperationEntry{key: key, hash: hash, context: context}
		s.cursor++
		if s.cursor == operationCompletedContextLimit {
			s.cursor = 0
		}
	} else {
		index = len(s.entries)
		s.entries = append(s.entries, completedOperationEntry{key: key, hash: hash, context: context})
	}
	slot, _, duplicate := s.find(key, hash)
	if duplicate {
		panic("completed operation key unexpectedly duplicated")
	}
	s.slots[slot] = uint16(index + 1)
	return evicted
}

func (s *completedOperationStore) find(key operationInstanceKey, hash uint64) (int, int, bool) {
	mask := len(s.slots) - 1
	slot := int(hash) & mask
	for {
		encoded := s.slots[slot]
		if encoded == 0 {
			return slot, 0, false
		}
		index := int(encoded - 1)
		if s.entries[index].key == key {
			return slot, index, true
		}
		slot = (slot + 1) & mask
	}
}

func (s *completedOperationStore) deleteSlot(slot int) {
	mask := len(s.slots) - 1
	hole := slot
	for next := (hole + 1) & mask; ; next = (next + 1) & mask {
		encoded := s.slots[next]
		if encoded == 0 {
			s.slots[hole] = 0
			return
		}
		index := int(encoded - 1)
		home := int(s.entries[index].hash) & mask
		if probeDistance(home, next, mask) > probeDistance(home, hole, mask) {
			s.slots[hole] = encoded
			hole = next
		}
	}
}

func (s *completedOperationStore) release() {
	s.entries = nil
	s.slots = nil
	s.cursor = 0
}

func probeDistance(home, slot, mask int) int {
	return (slot - home) & mask
}

func operationInstanceHash(key operationInstanceKey) uint64 {
	low := binary.LittleEndian.Uint64(key.process[:8])
	high := binary.LittleEndian.Uint64(key.process[8:])
	value := key.id ^ bits.RotateLeft64(low, 21) ^ bits.RotateLeft64(high, 43)
	value ^= value >> 30
	value *= 0xbf58476d1ce4e5b9
	value ^= value >> 27
	value *= 0x94d049bb133111eb
	return value ^ value>>31
}

type operationSignals struct {
	httpCount       uint64
	httpFailures    uint64
	httpDurationMS  uint64
	webSocketCount  uint64
	webSocketErrors uint64
	databaseCount   uint64
	databaseErrors  uint64
	databaseMain    uint64
	databaseUS      uint64
	composeCount    uint64
	composeMS       uint64
	workerCount     uint64
	workerFailures  uint64
	workerMS        uint64
	stallCount      uint64
	stallMaxMS      uint64
	uiFrames        uint64
	uiJank          uint64
	ioCount         uint64
	ioDurationUS    uint64
	ioBytes         uint64
	problemCount    uint64
	logRecords      uint64
	runtimeCalls    uint64
	runtimeTotalMS  uint64
	metricEvents    uint64
	retainedObjects uint64
	maxPSSKB        uint64
}

func (s *operationSignals) merge(other operationSignals) {
	s.httpCount = saturatingUint64Sum(s.httpCount, other.httpCount)
	s.httpFailures = saturatingUint64Sum(s.httpFailures, other.httpFailures)
	s.httpDurationMS = saturatingUint64Sum(s.httpDurationMS, other.httpDurationMS)
	s.webSocketCount = saturatingUint64Sum(s.webSocketCount, other.webSocketCount)
	s.webSocketErrors = saturatingUint64Sum(s.webSocketErrors, other.webSocketErrors)
	s.databaseCount = saturatingUint64Sum(s.databaseCount, other.databaseCount)
	s.databaseErrors = saturatingUint64Sum(s.databaseErrors, other.databaseErrors)
	s.databaseMain = saturatingUint64Sum(s.databaseMain, other.databaseMain)
	s.databaseUS = saturatingUint64Sum(s.databaseUS, other.databaseUS)
	s.composeCount = saturatingUint64Sum(s.composeCount, other.composeCount)
	s.composeMS = saturatingUint64Sum(s.composeMS, other.composeMS)
	s.workerCount = saturatingUint64Sum(s.workerCount, other.workerCount)
	s.workerFailures = saturatingUint64Sum(s.workerFailures, other.workerFailures)
	s.workerMS = saturatingUint64Sum(s.workerMS, other.workerMS)
	s.stallCount = saturatingUint64Sum(s.stallCount, other.stallCount)
	s.stallMaxMS = maxUint64(s.stallMaxMS, other.stallMaxMS)
	s.uiFrames = saturatingUint64Sum(s.uiFrames, other.uiFrames)
	s.uiJank = saturatingUint64Sum(s.uiJank, other.uiJank)
	s.ioCount = saturatingUint64Sum(s.ioCount, other.ioCount)
	s.ioDurationUS = saturatingUint64Sum(s.ioDurationUS, other.ioDurationUS)
	s.ioBytes = saturatingUint64Sum(s.ioBytes, other.ioBytes)
	s.problemCount = saturatingUint64Sum(s.problemCount, other.problemCount)
	s.logRecords = saturatingUint64Sum(s.logRecords, other.logRecords)
	s.runtimeCalls = saturatingUint64Sum(s.runtimeCalls, other.runtimeCalls)
	s.runtimeTotalMS = saturatingUint64Sum(s.runtimeTotalMS, other.runtimeTotalMS)
	s.metricEvents = saturatingUint64Sum(s.metricEvents, other.metricEvents)
	s.retainedObjects = saturatingUint64Sum(s.retainedObjects, other.retainedObjects)
	s.maxPSSKB = maxUint64(s.maxPSSKB, other.maxPSSKB)
}

type operationDatabaseSignals struct {
	statements [operationDatabaseStatementLimit]OperationDatabaseStatementStats
	count      uint8
}

type operationAggregate struct {
	count          uint64
	success        uint64
	failures       uint64
	cancelled      uint64
	timeouts       uint64
	durationsMS    operationDurationSummary
	totalMS        uint64
	budgeted       uint64
	budgetBreaches uint64
	signals        operationSignals
	database       operationDatabaseSignals
}

func (a *operationAggregate) add(
	durationMS, durationUS, budgetUS uint64,
	outcome jhlog.OperationOutcome,
	signals operationSignals,
	database operationDatabaseSignals,
) {
	a.count++
	a.durationsMS.add(durationMS)
	a.totalMS = saturatingUint64Sum(a.totalMS, durationMS)
	switch outcome {
	case jhlog.OperationOutcomeSuccess:
		a.success++
	case jhlog.OperationOutcomeFailure:
		a.failures++
	case jhlog.OperationOutcomeCancelled:
		a.cancelled++
	case jhlog.OperationOutcomeTimeout:
		a.timeouts++
	}
	if budgetUS > 0 {
		a.budgeted++
		if durationUS > budgetUS {
			a.budgetBreaches++
		}
	}
	a.signals.merge(signals)
	a.database.merge(database)
}

type operationStageAggregate struct {
	count       uint64
	durationsMS operationDurationSummary
	totalMS     uint64
}

type operationDurationSummary struct {
	exact         []uint64
	approximation *operationQuantileApproximation
	count         uint64
	max           uint64
	sorted        bool
}

type operationQuantileApproximation struct {
	p50 p2Quantile
	p90 p2Quantile
	p95 p2Quantile
}

type p2Quantile struct {
	probability float64
	count       uint64
	initial     [5]float64
	heights     [5]float64
	positions   [5]int64
	desired     [5]float64
}

func (s *operationDurationSummary) add(value uint64) {
	s.count++
	s.max = maxUint64(s.max, value)
	if s.approximation != nil {
		s.approximation.add(value)
		return
	}
	if len(s.exact) < operationExactDurationLimit {
		s.exact = append(s.exact, value)
		s.sorted = false
		return
	}
	s.approximation = newOperationQuantileApproximation()
	for _, exact := range s.exact {
		s.approximation.add(exact)
	}
	s.exact = nil
	s.approximation.add(value)
}

func (s *operationDurationSummary) percentile(probability float64) uint64 {
	if s.count == 0 {
		return 0
	}
	if s.approximation != nil {
		p50, p90, p95 := s.approximation.quantiles(s.max)
		switch probability {
		case 0.50:
			return p50
		case 0.90:
			return p90
		case 0.95:
			return p95
		default:
			panic(fmt.Sprintf("unsupported operation percentile %.4f", probability))
		}
	}
	if !s.sorted {
		sort.Slice(s.exact, func(i, j int) bool { return s.exact[i] < s.exact[j] })
		s.sorted = true
	}
	target := int(math.Ceil(float64(len(s.exact)) * probability))
	target = max(1, min(target, len(s.exact)))
	return s.exact[target-1]
}

func (s *operationDurationSummary) approximated() bool {
	return s.approximation != nil
}

func newOperationQuantileApproximation() *operationQuantileApproximation {
	return &operationQuantileApproximation{
		p50: p2Quantile{probability: 0.50},
		p90: p2Quantile{probability: 0.90},
		p95: p2Quantile{probability: 0.95},
	}
}

func (a *operationQuantileApproximation) add(value uint64) {
	a.p50.add(value)
	a.p90.add(value)
	a.p95.add(value)
}

func (a *operationQuantileApproximation) quantiles(maximum uint64) (uint64, uint64, uint64) {
	p50 := minUint64(a.p50.value(), maximum)
	p90 := maxUint64(p50, minUint64(a.p90.value(), maximum))
	p95 := maxUint64(p90, minUint64(a.p95.value(), maximum))
	return p50, p90, p95
}

func (q *p2Quantile) add(value uint64) {
	x := float64(value)
	if q.count < uint64(len(q.initial)) {
		q.initial[q.count] = x
		q.count++
		if q.count == uint64(len(q.initial)) {
			q.initialize()
		}
		return
	}

	q.count++
	cell := 0
	switch {
	case x < q.heights[0]:
		q.heights[0] = x
	case x >= q.heights[4]:
		q.heights[4] = x
		cell = 3
	default:
		for cell < 3 && x >= q.heights[cell+1] {
			cell++
		}
	}
	for index := cell + 1; index < len(q.positions); index++ {
		q.positions[index]++
	}
	increments := [5]float64{0, q.probability / 2, q.probability, (1 + q.probability) / 2, 1}
	for index := range q.desired {
		q.desired[index] += increments[index]
	}
	for index := 1; index < len(q.heights)-1; index++ {
		delta := q.desired[index] - float64(q.positions[index])
		direction := int64(0)
		if delta >= 1 && q.positions[index+1]-q.positions[index] > 1 {
			direction = 1
		} else if delta <= -1 && q.positions[index-1]-q.positions[index] < -1 {
			direction = -1
		}
		if direction == 0 {
			continue
		}
		candidate := q.parabolic(index, direction)
		if candidate > q.heights[index-1] && candidate < q.heights[index+1] {
			q.heights[index] = candidate
		} else {
			neighbor := index + int(direction)
			q.heights[index] += float64(direction) *
				(q.heights[neighbor] - q.heights[index]) /
				float64(q.positions[neighbor]-q.positions[index])
		}
		q.positions[index] += direction
	}
}

func (q *p2Quantile) initialize() {
	sort.Float64s(q.initial[:])
	copy(q.heights[:], q.initial[:])
	q.positions = [5]int64{1, 2, 3, 4, 5}
	q.desired = [5]float64{
		1,
		1 + 2*q.probability,
		1 + 4*q.probability,
		3 + 2*q.probability,
		5,
	}
}

func (q *p2Quantile) parabolic(index int, direction int64) float64 {
	position := q.positions[index]
	leftPosition := q.positions[index-1]
	rightPosition := q.positions[index+1]
	height := q.heights[index]
	adjustment := (float64(position-leftPosition+direction)*(q.heights[index+1]-height)/float64(rightPosition-position) +
		float64(rightPosition-position-direction)*(height-q.heights[index-1])/float64(position-leftPosition))
	return height + float64(direction)/float64(rightPosition-leftPosition)*adjustment
}

func (q *p2Quantile) value() uint64 {
	if q.count == 0 {
		return 0
	}
	if q.count < uint64(len(q.initial)) {
		copy := q.initial
		values := copy[:q.count]
		sort.Float64s(values)
		target := int(math.Ceil(float64(q.count)*q.probability)) - 1
		return uint64(values[max(0, min(target, len(values)-1))])
	}
	value := math.Round(q.heights[2])
	if value <= 0 {
		return 0
	}
	if value >= math.Ldexp(1, 64) {
		return ^uint64(0)
	}
	return uint64(value)
}

type operationAnalysisAccumulator struct {
	header                    jhlog.SegmentHeader
	active                    map[operationInstanceKey]*activeOperation
	ignoredActive             map[operationInstanceKey]struct{}
	freeActive                []*activeOperation
	completedContexts         completedOperationStore
	operations                map[operationGroupKey]*operationAggregate
	timeSlots                 map[operationSlotKey]*operationAggregate
	dimensions                map[operationDimensionKey]*operationAggregate
	stages                    map[operationStageKey]*operationStageAggregate
	incidents                 operationIncidentHeap
	started                   uint64
	completed                 uint64
	missingStart              uint64
	duplicateStart            uint64
	inconsistentLifecycle     uint64
	missingParent             uint64
	unmatchedSignals          uint64
	lateSignalEvents          uint64
	droppedActiveStarts       uint64
	droppedSignalEvents       uint64
	droppedSignalRollups      uint64
	completedContextEvictions uint64
	droppedOperationSamples   uint64
	droppedTimeSlotSamples    uint64
	droppedDimensionSamples   uint64
	droppedStageSamples       uint64
}

func newOperationAnalysisAccumulator() operationAnalysisAccumulator {
	return operationAnalysisAccumulator{
		active:        make(map[operationInstanceKey]*activeOperation),
		ignoredActive: make(map[operationInstanceKey]struct{}),
		operations:    make(map[operationGroupKey]*operationAggregate),
		timeSlots:     make(map[operationSlotKey]*operationAggregate),
		dimensions:    make(map[operationDimensionKey]*operationAggregate),
		stages:        make(map[operationStageKey]*operationStageAggregate),
		incidents:     make(operationIncidentHeap, 0, operationIncidentLimit),
	}
}

func (a *operationAnalysisAccumulator) startLog(header jhlog.SegmentHeader) {
	a.header = header
}

func (a *operationAnalysisAccumulator) activeName(operationID uint64) string {
	if operationID == 0 {
		return "unknown"
	}
	active := a.active[operationInstanceKey{process: a.header.ProcessInstanceID, id: operationID}]
	if active == nil {
		completed, exists := a.completedContexts.get(operationInstanceKey{
			process: a.header.ProcessInstanceID,
			id:      operationID,
		})
		if !exists {
			return "unknown"
		}
		return completed.name
	}
	return active.group.name
}

func (a *operationAnalysisAccumulator) recordLifecycle(
	dict map[uint64]string,
	event jhlog.Event,
	screen string,
	filter Filter,
) {
	operation := event.Operation
	if operation == nil {
		return
	}
	key := operationInstanceKey{process: a.header.ProcessInstanceID, id: operation.ID}
	switch operation.Phase {
	case jhlog.OperationPhaseStarted:
		a.started++
		if _, exists := a.active[key]; exists {
			a.duplicateStart++
			return
		}
		if _, exists := a.ignoredActive[key]; exists {
			a.duplicateStart++
			return
		}
		if len(a.active) >= operationActiveLimit {
			a.droppedActiveStarts++
			if len(a.ignoredActive) < operationIgnoredActiveLimit {
				a.ignoredActive[key] = struct{}{}
			}
			return
		}
		name := attrValue(jhlog.ResolveSymbol(dict, operation.NameRef))
		active := a.acquireActive()
		active.key = key
		active.parentID = operation.ParentID
		active.group = operationGroupKey{name: name, kind: operationKindName(operation.Kind), screen: attrValue(screen)}
		active.startUnixMS = operationEventUnixMS(a.header, event.TimeMS)
		active.offsetMin = a.header.TimezoneOffsetMinutes
		active.budgetUS = operation.BudgetUS
		active.included = containsFilter(screen, filter.ScreenContains)
		active.setAttributes(dict, operation.Attributes)
		a.active[key] = active
	case jhlog.OperationPhaseFinished:
		active := a.active[key]
		if active == nil {
			if _, droppedByLimit := a.ignoredActive[key]; droppedByLimit {
				delete(a.ignoredActive, key)
			} else {
				a.missingStart++
			}
			active = a.acquireActive()
			active.key = key
			active.parentID = operation.ParentID
			active.group = operationGroupKey{
				name: attrValue(jhlog.ResolveSymbol(dict, operation.NameRef)),
				kind: operationKindName(operation.Kind), screen: attrValue(screen),
			}
			active.startUnixMS = operationRecoveredStartUnixMS(a.header, event.TimeMS, operation.DurationUS)
			active.offsetMin = a.header.TimezoneOffsetMinutes
			active.budgetUS = operation.BudgetUS
			active.included = containsFilter(screen, filter.ScreenContains)
			active.setAttributes(dict, operation.Attributes)
			a.complete(active, operation)
			a.recycleActive(active)
			return
		}
		name := attrValue(jhlog.ResolveSymbol(dict, operation.NameRef))
		if name != active.group.name || operation.Kind != operationKindFromName(active.group.kind) ||
			operation.ParentID != active.parentID || operation.BudgetUS != active.budgetUS ||
			!active.attributesEqual(dict, operation.Attributes) {
			a.inconsistentLifecycle++
		}
		a.complete(active, operation)
		delete(a.active, key)
		a.recycleActive(active)
	}
}

func (a *activeOperation) setAttributes(dict map[uint64]string, attributes []jhlog.OperationAttribute) {
	a.attributeCount = uint8(len(attributes))
	if len(attributes) == 0 {
		return
	}
	a.firstAttribute = operationAttributeValue{
		key:   attrValue(jhlog.ResolveSymbol(dict, attributes[0].KeyRef)),
		value: attrValue(jhlog.ResolveSymbol(dict, attributes[0].ValueRef)),
	}
	additional := len(attributes) - 1
	if cap(a.moreAttributes) < additional {
		a.moreAttributes = make([]operationAttributeValue, additional)
	} else {
		a.moreAttributes = a.moreAttributes[:additional]
	}
	for index := 1; index < len(attributes); index++ {
		item := attributes[index]
		a.moreAttributes[index-1] = operationAttributeValue{
			key:   attrValue(jhlog.ResolveSymbol(dict, item.KeyRef)),
			value: attrValue(jhlog.ResolveSymbol(dict, item.ValueRef)),
		}
	}
}

func (a *activeOperation) attributesEqual(
	dict map[uint64]string,
	attributes []jhlog.OperationAttribute,
) bool {
	if len(attributes) != int(a.attributeCount) {
		return false
	}
	if len(attributes) == 0 {
		return true
	}
	if a.firstAttribute.key != attrValue(jhlog.ResolveSymbol(dict, attributes[0].KeyRef)) ||
		a.firstAttribute.value != attrValue(jhlog.ResolveSymbol(dict, attributes[0].ValueRef)) {
		return false
	}
	for index := 1; index < len(attributes); index++ {
		item := attributes[index]
		attribute := a.moreAttributes[index-1]
		if attribute.key != attrValue(jhlog.ResolveSymbol(dict, item.KeyRef)) ||
			attribute.value != attrValue(jhlog.ResolveSymbol(dict, item.ValueRef)) {
			return false
		}
	}
	return true
}

func (a *operationAnalysisAccumulator) acquireActive() *activeOperation {
	last := len(a.freeActive) - 1
	if last < 0 {
		return &activeOperation{}
	}
	active := a.freeActive[last]
	a.freeActive = a.freeActive[:last]
	return active
}

func (a *operationAnalysisAccumulator) recycleActive(active *activeOperation) {
	active.key = operationInstanceKey{}
	active.parentID = 0
	active.group = operationGroupKey{}
	active.startUnixMS = 0
	active.offsetMin = 0
	active.budgetUS = 0
	active.firstAttribute = operationAttributeValue{}
	for index := range active.moreAttributes {
		active.moreAttributes[index] = operationAttributeValue{}
	}
	active.moreAttributes = active.moreAttributes[:0]
	active.attributeCount = 0
	active.inclusive = operationSignals{}
	active.database = operationDatabaseSignals{}
	active.included = false
	a.freeActive = append(a.freeActive, active)
}

func (a *operationAnalysisAccumulator) complete(active *activeOperation, finish *jhlog.OperationEvent) {
	durationMS := microsecondsToMillisecondsCeil(finish.DurationUS)
	var aggregate *operationAggregate
	var slotAggregate *operationAggregate
	if active.included {
		var retained bool
		aggregate, retained = boundedOperationAggregateFor(a.operations, active.group, operationGroupLimit)
		if !retained {
			a.droppedOperationSamples++
			a.retainIncident(operationIncident(active, finish, durationMS))
			a.rememberCompletedContext(active, nil, nil)
			a.completed++
			a.rollUpToParent(active, durationMS)
			return
		}
		aggregate.add(
			durationMS, finish.DurationUS, active.budgetUS, finish.Outcome, active.inclusive, active.database,
		)
		slot := operationSlot(active.startUnixMS, active.offsetMin)
		slotKey := operationSlotKey{startUnixMS: slot, offsetMin: active.offsetMin, group: active.group}
		if retainedSlot, kept := boundedOperationAggregateFor(a.timeSlots, slotKey, operationTimeSlotLimit); kept {
			slotAggregate = retainedSlot
			slotAggregate.add(
				durationMS, finish.DurationUS, active.budgetUS, finish.Outcome,
				active.inclusive, active.database,
			)
		} else {
			a.droppedTimeSlotSamples++
		}
		if active.attributeCount > 0 {
			a.addDimension(active, active.firstAttribute, finish, durationMS)
			for _, dimension := range active.moreAttributes {
				a.addDimension(active, dimension, finish, durationMS)
			}
		}
		a.retainIncident(operationIncident(active, finish, durationMS))
	}
	a.rememberCompletedContext(active, aggregate, slotAggregate)
	a.completed++
	a.rollUpToParent(active, durationMS)
}

func (a *operationAnalysisAccumulator) rememberCompletedContext(
	active *activeOperation,
	aggregate *operationAggregate,
	slotAggregate *operationAggregate,
) {
	context := completedOperationContext{
		parentID:      active.parentID,
		name:          active.group.name,
		included:      active.included,
		aggregate:     aggregate,
		slotAggregate: slotAggregate,
	}
	if a.completedContexts.put(active.key, context) {
		a.completedContextEvictions++
	}
}

func (a *operationAnalysisAccumulator) addDimension(
	active *activeOperation,
	dimension operationAttributeValue,
	finish *jhlog.OperationEvent,
	durationMS uint64,
) {
	key := operationDimensionKey{group: active.group, key: dimension.key, value: dimension.value}
	if aggregate, kept := boundedOperationAggregateFor(a.dimensions, key, operationDimensionLimit); kept {
		aggregate.add(
			durationMS, finish.DurationUS, active.budgetUS, finish.Outcome,
			operationSignals{}, operationDatabaseSignals{},
		)
	} else {
		a.droppedDimensionSamples++
	}
}

func (a *operationAnalysisAccumulator) rollUpToParent(active *activeOperation, durationMS uint64) {
	if active.parentID == 0 {
		return
	}
	parentKey := operationInstanceKey{process: active.key.process, id: active.parentID}
	parent := a.active[parentKey]
	if parent == nil {
		a.missingParent++
		return
	}
	parent.inclusive.merge(active.inclusive)
	parent.database.merge(active.database)
	if active.group.kind == operationKindName(jhlog.OperationKindStage) && active.included && parent.included {
		stageKey := operationStageKey{parent: parent.group, stage: active.group.name}
		stage := a.stages[stageKey]
		if stage == nil {
			if len(a.stages) >= operationStageLimit {
				a.droppedStageSamples++
				return
			}
			stage = &operationStageAggregate{}
			a.stages[stageKey] = stage
		}
		stage.count++
		stage.durationsMS.add(durationMS)
		stage.totalMS = saturatingUint64Sum(stage.totalMS, durationMS)
	}
}

func boundedOperationAggregateFor[K comparable](
	target map[K]*operationAggregate,
	key K,
	limit int,
) (*operationAggregate, bool) {
	value := target[key]
	if value == nil {
		if len(target) >= limit {
			return nil, false
		}
		value = &operationAggregate{}
		target[key] = value
	}
	return value, true
}

func (a *operationAnalysisAccumulator) recordSignal(event jhlog.Event, operationID uint64, owner ...string) {
	if operationID == 0 || event.Operation != nil {
		return
	}
	caller := ""
	if len(owner) > 0 {
		caller = owner[0]
	}
	signal, tracked := operationSignal(event, caller)
	if !tracked {
		return
	}
	a.recordResolvedSignal(operationID, signal, nil)
}

func (a *operationAnalysisAccumulator) recordDatabaseStatement(
	operationID uint64,
	query, source, operation string,
	database *jhlog.DatabaseEvent,
	flags uint64,
) {
	if operationID == 0 || database == nil {
		return
	}
	signal, _ := operationSignal(jhlog.Event{Database: database, Flags: flags}, "")
	statement := OperationDatabaseStatementStats{
		Query: query, Source: source, Operation: operation, Calls: 1,
		TotalDurationUS: database.DurationUS, MaxDurationUS: database.DurationUS,
	}
	if database.Outcome == jhlog.DatabaseOutcomeFailure {
		statement.Failures = 1
	}
	if flags&uint64(jhlog.FlagThreadMain) != 0 {
		statement.MainThreadCalls = 1
	}
	a.recordResolvedSignal(operationID, signal, &statement)
}

func (a *operationAnalysisAccumulator) recordResolvedSignal(
	operationID uint64,
	signal operationSignals,
	database *OperationDatabaseStatementStats,
) {
	key := operationInstanceKey{process: a.header.ProcessInstanceID, id: operationID}
	active := a.active[key]
	if active != nil {
		active.inclusive.merge(signal)
		if database != nil {
			active.database.add(*database)
		}
		return
	}
	if _, droppedByLimit := a.ignoredActive[key]; droppedByLimit {
		a.droppedSignalEvents++
		return
	}
	if _, exists := a.completedContexts.get(key); !exists {
		a.unmatchedSignals++
		return
	}
	a.lateSignalEvents++
	a.rollUpLateSignal(key, signal, database)
}

func (s *operationDatabaseSignals) merge(other operationDatabaseSignals) {
	for index := uint8(0); index < other.count; index++ {
		s.add(other.statements[index])
	}
}

func (s *operationDatabaseSignals) add(candidate OperationDatabaseStatementStats) {
	for index := uint8(0); index < s.count; index++ {
		current := &s.statements[index]
		if current.Query != candidate.Query || current.Source != candidate.Source ||
			current.Operation != candidate.Operation {
			continue
		}
		current.Calls = saturatingUint64Sum(current.Calls, candidate.Calls)
		current.Failures = saturatingUint64Sum(current.Failures, candidate.Failures)
		current.MainThreadCalls = saturatingUint64Sum(current.MainThreadCalls, candidate.MainThreadCalls)
		current.TotalDurationUS = saturatingUint64Sum(current.TotalDurationUS, candidate.TotalDurationUS)
		current.MaxDurationUS = maxUint64(current.MaxDurationUS, candidate.MaxDurationUS)
		return
	}
	if s.count < operationDatabaseStatementLimit {
		s.statements[s.count] = candidate
		s.count++
		return
	}
	minimum := 0
	for index := 1; index < operationDatabaseStatementLimit; index++ {
		if operationDatabaseStatementLess(s.statements[index], s.statements[minimum]) {
			minimum = index
		}
	}
	if operationDatabaseStatementLess(s.statements[minimum], candidate) {
		s.statements[minimum] = candidate
	}
}

func operationDatabaseStatementLess(left, right OperationDatabaseStatementStats) bool {
	if left.Failures != right.Failures {
		return left.Failures < right.Failures
	}
	if left.MainThreadCalls != right.MainThreadCalls {
		return left.MainThreadCalls < right.MainThreadCalls
	}
	if left.TotalDurationUS != right.TotalDurationUS {
		return left.TotalDurationUS < right.TotalDurationUS
	}
	if left.MaxDurationUS != right.MaxDurationUS {
		return left.MaxDurationUS < right.MaxDurationUS
	}
	if left.Calls != right.Calls {
		return left.Calls < right.Calls
	}
	if left.Query != right.Query {
		return left.Query > right.Query
	}
	if left.Source != right.Source {
		return left.Source > right.Source
	}
	return left.Operation > right.Operation
}

func operationDatabaseStatements(signals operationDatabaseSignals) []OperationDatabaseStatementStats {
	if signals.count == 0 {
		return nil
	}
	result := make([]OperationDatabaseStatementStats, signals.count)
	copy(result, signals.statements[:signals.count])
	sort.Slice(result, func(i, j int) bool {
		return operationDatabaseStatementLess(result[j], result[i])
	})
	return result
}

func operationSignal(event jhlog.Event, owner string) (operationSignals, bool) {
	var signal operationSignals
	switch {
	case event.HTTP != nil:
		signal.httpCount = 1
		signal.httpDurationMS = event.HTTP.DurationMS
		if httpEventFailed(event.HTTP, event.Flags) {
			signal.httpFailures = 1
		}
	case event.WebSocket != nil:
		signal.webSocketCount = 1
		if event.WebSocket.Stage == jhlog.WebSocketStageFailed {
			signal.webSocketErrors = 1
		}
	case event.Database != nil:
		signal.databaseCount = 1
		signal.databaseUS = event.Database.DurationUS
		if event.Database.Outcome == jhlog.DatabaseOutcomeFailure {
			signal.databaseErrors = 1
		}
		if event.Flags&uint64(jhlog.FlagThreadMain) != 0 {
			signal.databaseMain = 1
		}
	case event.Worker != nil:
		if event.Worker.Stage != jhlog.WorkerStageFinished {
			return operationSignals{}, false
		}
		signal.workerCount = 1
		signal.workerMS = event.Worker.DurationMS
		if event.Worker.Outcome != jhlog.WorkerOutcomeSuccess {
			signal.workerFailures = 1
		}
	case event.Stall != nil:
		signal.stallCount = 1
		signal.stallMaxMS = event.Stall.DurationMS
	case event.UIWindow != nil:
		signal.uiFrames = event.UIWindow.FrameCount
		signal.uiJank = event.UIWindow.JankCount
	case event.IO != nil:
		signal.ioCount = 1
		signal.ioDurationUS = event.IO.DurationUS
		if event.Flags&uint64(jhlog.FlagIOBytesKnown) != 0 {
			signal.ioBytes = event.IO.Bytes
		}
	case event.Problem != nil:
		signal.problemCount = maxUint64(event.Problem.Count, 1)
	case event.LogSpam != nil:
		signal.logRecords = event.LogSpam.Count
	case event.RuntimeCall != nil:
		signal.runtimeCalls = event.RuntimeCall.Count
		signal.runtimeTotalMS = event.RuntimeCall.TotalMS
		if strings.HasPrefix(owner, "jankhunter.semantic.v1.compose.") {
			signal.composeCount = event.RuntimeCall.Count
			signal.composeMS = event.RuntimeCall.TotalMS
		}
	case event.Metric != nil:
		signal.metricEvents = 1
	case event.Retained != nil:
		signal.retainedObjects = event.Retained.Count
	case event.Memory != nil:
		signal.maxPSSKB = event.Memory.PSSKB
	default:
		return operationSignals{}, false
	}
	return signal, true
}

func (a *operationAnalysisAccumulator) rollUpLateSignal(
	key operationInstanceKey,
	signal operationSignals,
	database *OperationDatabaseStatementStats,
) {
	var visited [operationParentDepthLimit]operationInstanceKey
	for depth := 0; depth < operationParentDepthLimit; depth++ {
		for index := 0; index < depth; index++ {
			if visited[index] == key {
				a.droppedSignalRollups++
				return
			}
		}
		visited[depth] = key
		context, exists := a.completedContexts.get(key)
		if !exists {
			return
		}
		if context.included {
			if context.aggregate != nil {
				context.aggregate.signals.merge(signal)
				if database != nil {
					context.aggregate.database.add(*database)
				}
			}
			if context.slotAggregate != nil {
				context.slotAggregate.signals.merge(signal)
				if database != nil {
					context.slotAggregate.database.add(*database)
				}
			}
			a.mergeLateSignalIntoIncident(key, signal, database)
		}
		if context.parentID == 0 {
			return
		}
		key = operationInstanceKey{process: key.process, id: context.parentID}
		if parent := a.active[key]; parent != nil {
			parent.inclusive.merge(signal)
			if database != nil {
				parent.database.add(*database)
			}
			return
		}
	}
	a.droppedSignalRollups++
}

func (a *operationAnalysisAccumulator) finalize() *OperationAnalysis {
	if a.started == 0 && a.completed == 0 && a.missingStart == 0 {
		return nil
	}
	result := &OperationAnalysis{
		Started:                   a.started,
		Completed:                 a.completed,
		MissingFinish:             uint64(len(a.active) + len(a.ignoredActive)),
		MissingStart:              a.missingStart,
		DuplicateStart:            a.duplicateStart,
		InconsistentLifecycle:     a.inconsistentLifecycle,
		MissingParent:             a.missingParent,
		UnmatchedSignalEvents:     a.unmatchedSignals,
		LateSignalEvents:          a.lateSignalEvents,
		DroppedActiveStarts:       a.droppedActiveStarts,
		DroppedSignalEvents:       a.droppedSignalEvents,
		DroppedSignalRollups:      a.droppedSignalRollups,
		CompletedContextEvictions: a.completedContextEvictions,
		DroppedOperationSamples:   a.droppedOperationSamples,
		DroppedTimeSlotSamples:    a.droppedTimeSlotSamples,
		DroppedDimensionSamples:   a.droppedDimensionSamples,
		DroppedStageSamples:       a.droppedStageSamples,
	}
	for key, aggregate := range a.operations {
		result.Operations = append(result.Operations, operationStats(key, aggregate))
	}
	for key, aggregate := range a.timeSlots {
		stats := operationStats(key.group, aggregate)
		result.TimeSlots = append(result.TimeSlots, OperationTimeSlot{
			StartUnixMS: key.startUnixMS, TimezoneOffsetMin: key.offsetMin,
			Label:     operationSlotLabel(key.startUnixMS, key.offsetMin),
			Operation: stats.Operation, Kind: stats.Kind, Screen: stats.Screen,
			Count: stats.Count, Failures: stats.Failures + stats.Timeouts,
			P50MS: stats.P50MS, P90MS: stats.P90MS, P95MS: stats.P95MS, MaxMS: stats.MaxMS,
			QuantilesApproximated: stats.QuantilesApproximated,
			Budgeted:              stats.Budgeted, BudgetBreaches: stats.BudgetBreaches,
			BudgetBreachRatePct: stats.BudgetBreachRatePct,
			CorrelatedHTTP:      stats.CorrelatedHTTP, CorrelatedHTTPFailures: stats.CorrelatedHTTPFailures,
			CorrelatedHTTPDurationMS: stats.CorrelatedHTTPDurationMS,
			CorrelatedWebSocket:      stats.CorrelatedWebSocket, CorrelatedWebSocketErrors: stats.CorrelatedWebSocketErrors,
			CorrelatedDatabase: stats.CorrelatedDatabase, CorrelatedDatabaseErrors: stats.CorrelatedDatabaseErrors,
			CorrelatedDatabaseMain: stats.CorrelatedDatabaseMain, CorrelatedDatabaseUS: stats.CorrelatedDatabaseUS,
			WorstDatabaseStatements: stats.WorstDatabaseStatements,
			CorrelatedCompose:       stats.CorrelatedCompose, CorrelatedComposeMS: stats.CorrelatedComposeMS,
			CorrelatedWorkers: stats.CorrelatedWorkers, CorrelatedWorkerFailures: stats.CorrelatedWorkerFailures,
			CorrelatedWorkerMS: stats.CorrelatedWorkerMS,
			CorrelatedStalls:   stats.CorrelatedStalls, CorrelatedStallMaxMS: stats.CorrelatedStallMaxMS,
			CorrelatedUIFrames: stats.CorrelatedUIFrames, CorrelatedUIJank: stats.CorrelatedUIJank,
			CorrelatedUIJankRatePct: stats.CorrelatedUIJankRatePct,
			CorrelatedIO:            stats.CorrelatedIO, CorrelatedIODurationUS: stats.CorrelatedIODurationUS,
			CorrelatedIOBytes: stats.CorrelatedIOBytes, CorrelatedProblems: stats.CorrelatedProblems,
			CorrelatedLogRecords:      stats.CorrelatedLogRecords,
			CorrelatedRuntimeCalls:    stats.CorrelatedRuntimeCalls,
			CorrelatedRuntimeTotalMS:  stats.CorrelatedRuntimeTotalMS,
			CorrelatedMetricEvents:    stats.CorrelatedMetricEvents,
			CorrelatedRetainedObjects: stats.CorrelatedRetainedObjects,
			MaxPSSKB:                  stats.MaxPSSKB,
		})
	}
	for key, aggregate := range a.dimensions {
		stats := operationStats(key.group, aggregate)
		result.Dimensions = append(result.Dimensions, OperationDimensionStats{
			Operation: stats.Operation, Kind: stats.Kind, Screen: stats.Screen,
			Key: key.key, Value: key.value, Count: stats.Count, Failures: stats.Failures + stats.Timeouts,
			P90MS: stats.P90MS, P95MS: stats.P95MS, MaxMS: stats.MaxMS, Budgeted: stats.Budgeted,
			QuantilesApproximated: stats.QuantilesApproximated,
			BudgetBreaches:        stats.BudgetBreaches, BudgetBreachRatePct: stats.BudgetBreachRatePct,
		})
	}
	for key, aggregate := range a.stages {
		parentTotal := uint64(0)
		if parent := a.operations[key.parent]; parent != nil {
			parentTotal = parent.totalMS
		}
		share := 0.0
		if parentTotal > 0 {
			share = float64(aggregate.totalMS) * 100 / float64(parentTotal)
		}
		result.Stages = append(result.Stages, OperationStageStats{
			ParentOperation: key.parent.name, ParentKind: key.parent.kind, Screen: key.parent.screen,
			Stage: key.stage, Count: aggregate.count, P50MS: aggregate.durationsMS.percentile(0.50),
			P90MS: aggregate.durationsMS.percentile(0.90),
			P95MS: aggregate.durationsMS.percentile(0.95), MaxMS: aggregate.durationsMS.max,
			QuantilesApproximated: aggregate.durationsMS.approximated(),
			TotalMS:               aggregate.totalMS, ParentTotalMS: parentTotal, SharePct: share,
		})
	}
	for index := range a.incidents {
		materializeOperationIncidentDatabaseStatements(&a.incidents[index])
	}
	result.WorstIncidents = append(result.WorstIncidents, a.incidents...)
	sort.Slice(result.WorstIncidents, func(i, j int) bool {
		return incidentWorse(result.WorstIncidents[i], result.WorstIncidents[j])
	})
	sortOperationAnalysis(result)
	return result
}

func (a *operationAnalysisAccumulator) release() {
	a.header = jhlog.SegmentHeader{}
	a.active = nil
	a.ignoredActive = nil
	a.freeActive = nil
	a.completedContexts.release()
	a.operations = nil
	a.timeSlots = nil
	a.dimensions = nil
	a.stages = nil
	a.incidents = nil
}

func operationStats(key operationGroupKey, aggregate *operationAggregate) OperationStats {
	stats := OperationStats{
		Operation: key.name, Kind: key.kind, Screen: key.screen,
		Count: aggregate.count, Success: aggregate.success, Failures: aggregate.failures,
		Cancelled: aggregate.cancelled, Timeouts: aggregate.timeouts,
		P50MS: aggregate.durationsMS.percentile(0.50), P90MS: aggregate.durationsMS.percentile(0.90),
		P95MS:                 aggregate.durationsMS.percentile(0.95),
		QuantilesApproximated: aggregate.durationsMS.approximated(),
		MaxMS:                 aggregate.durationsMS.max, TotalMS: aggregate.totalMS,
		Budgeted: aggregate.budgeted, BudgetBreaches: aggregate.budgetBreaches,
		CorrelatedHTTP:            aggregate.signals.httpCount,
		CorrelatedHTTPFailures:    aggregate.signals.httpFailures,
		CorrelatedHTTPDurationMS:  aggregate.signals.httpDurationMS,
		CorrelatedWebSocket:       aggregate.signals.webSocketCount,
		CorrelatedWebSocketErrors: aggregate.signals.webSocketErrors,
		CorrelatedDatabase:        aggregate.signals.databaseCount,
		CorrelatedDatabaseErrors:  aggregate.signals.databaseErrors,
		CorrelatedDatabaseMain:    aggregate.signals.databaseMain,
		CorrelatedDatabaseUS:      aggregate.signals.databaseUS,
		WorstDatabaseStatements:   operationDatabaseStatements(aggregate.database),
		CorrelatedCompose:         aggregate.signals.composeCount,
		CorrelatedComposeMS:       aggregate.signals.composeMS,
		CorrelatedWorkers:         aggregate.signals.workerCount,
		CorrelatedWorkerFailures:  aggregate.signals.workerFailures,
		CorrelatedWorkerMS:        aggregate.signals.workerMS,
		CorrelatedStalls:          aggregate.signals.stallCount,
		CorrelatedStallMaxMS:      aggregate.signals.stallMaxMS,
		CorrelatedUIFrames:        aggregate.signals.uiFrames,
		CorrelatedUIJank:          aggregate.signals.uiJank,
		CorrelatedIO:              aggregate.signals.ioCount,
		CorrelatedIODurationUS:    aggregate.signals.ioDurationUS,
		CorrelatedIOBytes:         aggregate.signals.ioBytes,
		CorrelatedProblems:        aggregate.signals.problemCount,
		CorrelatedLogRecords:      aggregate.signals.logRecords,
		CorrelatedRuntimeCalls:    aggregate.signals.runtimeCalls,
		CorrelatedRuntimeTotalMS:  aggregate.signals.runtimeTotalMS,
		CorrelatedMetricEvents:    aggregate.signals.metricEvents,
		CorrelatedRetainedObjects: aggregate.signals.retainedObjects,
		MaxPSSKB:                  aggregate.signals.maxPSSKB,
	}
	if stats.Budgeted > 0 {
		stats.BudgetBreachRatePct = float64(stats.BudgetBreaches) * 100 / float64(stats.Budgeted)
	}
	if stats.CorrelatedUIFrames > 0 {
		stats.CorrelatedUIJankRatePct = float64(stats.CorrelatedUIJank) * 100 / float64(stats.CorrelatedUIFrames)
	}
	return stats
}

func operationIncident(active *activeOperation, finish *jhlog.OperationEvent, durationMS uint64) OperationIncident {
	budgetMS := microsecondsToMillisecondsCeil(active.budgetUS)
	exceeded := uint64(0)
	if active.budgetUS > 0 && finish.DurationUS > active.budgetUS {
		exceeded = microsecondsToMillisecondsCeil(finish.DurationUS - active.budgetUS)
	}
	incident := OperationIncident{
		instanceKey: active.key,
		StartUnixMS: active.startUnixMS, Operation: active.group.name, Kind: active.group.kind,
		Screen: active.group.screen, Outcome: operationOutcomeName(finish.Outcome), DurationMS: durationMS,
		BudgetMS: budgetMS, BudgetExceededMS: exceeded,
	}
	mergeOperationIncidentSignals(&incident, active.inclusive, active.database)
	incident.Score = operationIncidentScore(incident)
	return incident
}

func mergeOperationIncidentSignals(
	incident *OperationIncident,
	signal operationSignals,
	database operationDatabaseSignals,
) {
	incident.CorrelatedHTTP = saturatingUint64Sum(incident.CorrelatedHTTP, signal.httpCount)
	incident.HTTPFailures = saturatingUint64Sum(incident.HTTPFailures, signal.httpFailures)
	incident.HTTPDurationMS = saturatingUint64Sum(incident.HTTPDurationMS, signal.httpDurationMS)
	incident.CorrelatedWebSocket = saturatingUint64Sum(incident.CorrelatedWebSocket, signal.webSocketCount)
	incident.WebSocketErrors = saturatingUint64Sum(incident.WebSocketErrors, signal.webSocketErrors)
	incident.CorrelatedDatabase = saturatingUint64Sum(incident.CorrelatedDatabase, signal.databaseCount)
	incident.DatabaseErrors = saturatingUint64Sum(incident.DatabaseErrors, signal.databaseErrors)
	incident.DatabaseMainThread = saturatingUint64Sum(incident.DatabaseMainThread, signal.databaseMain)
	incident.DatabaseDurationUS = saturatingUint64Sum(incident.DatabaseDurationUS, signal.databaseUS)
	for index := uint8(0); index < database.count; index++ {
		mergeOperationIncidentDatabaseStatement(incident, database.statements[index])
	}
	incident.CorrelatedCompose = saturatingUint64Sum(incident.CorrelatedCompose, signal.composeCount)
	incident.ComposeDurationMS = saturatingUint64Sum(incident.ComposeDurationMS, signal.composeMS)
	incident.CorrelatedWorkers = saturatingUint64Sum(incident.CorrelatedWorkers, signal.workerCount)
	incident.WorkerFailures = saturatingUint64Sum(incident.WorkerFailures, signal.workerFailures)
	incident.WorkerDurationMS = saturatingUint64Sum(incident.WorkerDurationMS, signal.workerMS)
	incident.CorrelatedStalls = saturatingUint64Sum(incident.CorrelatedStalls, signal.stallCount)
	incident.StallMaxMS = maxUint64(incident.StallMaxMS, signal.stallMaxMS)
	incident.CorrelatedUIFrames = saturatingUint64Sum(incident.CorrelatedUIFrames, signal.uiFrames)
	incident.CorrelatedUIJank = saturatingUint64Sum(incident.CorrelatedUIJank, signal.uiJank)
	incident.CorrelatedIO = saturatingUint64Sum(incident.CorrelatedIO, signal.ioCount)
	incident.IODurationUS = saturatingUint64Sum(incident.IODurationUS, signal.ioDurationUS)
	incident.IOBytes = saturatingUint64Sum(incident.IOBytes, signal.ioBytes)
	incident.CorrelatedProblems = saturatingUint64Sum(incident.CorrelatedProblems, signal.problemCount)
	incident.CorrelatedLogRecords = saturatingUint64Sum(incident.CorrelatedLogRecords, signal.logRecords)
	incident.CorrelatedRuntimeCalls = saturatingUint64Sum(incident.CorrelatedRuntimeCalls, signal.runtimeCalls)
	incident.CorrelatedRuntimeTotalMS = saturatingUint64Sum(incident.CorrelatedRuntimeTotalMS, signal.runtimeTotalMS)
	incident.CorrelatedMetricEvents = saturatingUint64Sum(incident.CorrelatedMetricEvents, signal.metricEvents)
	incident.CorrelatedRetainedObjects = saturatingUint64Sum(incident.CorrelatedRetainedObjects, signal.retainedObjects)
	incident.MaxPSSKB = maxUint64(incident.MaxPSSKB, signal.maxPSSKB)
}

func mergeOperationIncidentDatabaseStatement(
	incident *OperationIncident,
	candidate OperationDatabaseStatementStats,
) {
	for index := uint8(0); index < incident.databaseStatementCount; index++ {
		current := &incident.databaseStatements[index]
		if current.Query != candidate.Query || current.Source != candidate.Source ||
			current.Operation != candidate.Operation {
			continue
		}
		current.Calls = saturatingUint64Sum(current.Calls, candidate.Calls)
		current.Failures = saturatingUint64Sum(current.Failures, candidate.Failures)
		current.MainThreadCalls = saturatingUint64Sum(current.MainThreadCalls, candidate.MainThreadCalls)
		current.TotalDurationUS = saturatingUint64Sum(current.TotalDurationUS, candidate.TotalDurationUS)
		current.MaxDurationUS = maxUint64(current.MaxDurationUS, candidate.MaxDurationUS)
		return
	}
	if incident.databaseStatementCount < operationDatabaseStatementLimit {
		incident.databaseStatements[incident.databaseStatementCount] = candidate
		incident.databaseStatementCount++
		return
	}
	minimum := 0
	for index := 1; index < operationDatabaseStatementLimit; index++ {
		if operationDatabaseStatementLess(incident.databaseStatements[index], incident.databaseStatements[minimum]) {
			minimum = index
		}
	}
	if operationDatabaseStatementLess(incident.databaseStatements[minimum], candidate) {
		incident.databaseStatements[minimum] = candidate
	}
}

func materializeOperationIncidentDatabaseStatements(incident *OperationIncident) {
	if incident.databaseStatementCount == 0 {
		return
	}
	incident.WorstDatabaseStatements = make([]OperationDatabaseStatementStats, incident.databaseStatementCount)
	copy(incident.WorstDatabaseStatements, incident.databaseStatements[:incident.databaseStatementCount])
	sort.Slice(incident.WorstDatabaseStatements, func(i, j int) bool {
		return operationDatabaseStatementLess(incident.WorstDatabaseStatements[j], incident.WorstDatabaseStatements[i])
	})
}

func operationIncidentScore(incident OperationIncident) uint64 {
	rank := uint64(0)
	switch incident.Outcome {
	case "timeout":
		rank = 4
	case "failure":
		rank = 3
	case "cancelled":
		rank = 2
	default:
		if incident.BudgetExceededMS > 0 {
			rank = 1
		}
	}
	score := rank * 1_000_000_000_000_000_000
	score = saturatingUint64Sum(score, minUint64(incident.DurationMS, 999_999_999_999)*1_000_000)
	score = saturatingUint64Sum(score, minUint64(incident.StallMaxMS, 999_999)*1_000)
	score = saturatingUint64Sum(score, minUint64(incident.HTTPFailures+incident.WebSocketErrors+incident.DatabaseErrors+incident.WorkerFailures+incident.CorrelatedProblems, 999))
	return score
}

func (a *operationAnalysisAccumulator) retainIncident(incident OperationIncident) {
	if incident.Score == 0 {
		return
	}
	if a.incidents.Len() < operationIncidentLimit {
		a.incidents.push(incident)
		return
	}
	if incidentWorse(incident, a.incidents[0]) {
		a.incidents.replaceRoot(incident)
	}
}

func (a *operationAnalysisAccumulator) mergeLateSignalIntoIncident(
	key operationInstanceKey,
	signal operationSignals,
	database *OperationDatabaseStatementStats,
) {
	for index := range a.incidents {
		if a.incidents[index].instanceKey != key {
			continue
		}
		incident := &a.incidents[index]
		var databaseSignals operationDatabaseSignals
		if database != nil {
			databaseSignals.add(*database)
		}
		mergeOperationIncidentSignals(incident, signal, databaseSignals)
		incident.Score = operationIncidentScore(*incident)
		a.incidents.restore(index)
		return
	}
}

type operationIncidentHeap []OperationIncident

func (h operationIncidentHeap) Len() int { return len(h) }
func (h operationIncidentHeap) Less(i, j int) bool {
	return incidentWorse(h[j], h[i])
}
func (h operationIncidentHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }

func (h *operationIncidentHeap) push(value OperationIncident) {
	*h = append(*h, value)
	index := len(*h) - 1
	for index > 0 {
		parent := (index - 1) / 2
		if !h.Less(index, parent) {
			break
		}
		h.Swap(index, parent)
		index = parent
	}
}

func (h *operationIncidentHeap) replaceRoot(value OperationIncident) {
	(*h)[0] = value
	index := 0
	for {
		left := index*2 + 1
		if left >= len(*h) {
			return
		}
		smallest := left
		right := left + 1
		if right < len(*h) && h.Less(right, left) {
			smallest = right
		}
		if !h.Less(smallest, index) {
			return
		}
		h.Swap(index, smallest)
		index = smallest
	}
}

func (h *operationIncidentHeap) restore(index int) {
	if index > 0 {
		parent := (index - 1) / 2
		if h.Less(index, parent) {
			for index > 0 {
				parent = (index - 1) / 2
				if !h.Less(index, parent) {
					return
				}
				h.Swap(index, parent)
				index = parent
			}
			return
		}
	}
	for {
		left := index*2 + 1
		if left >= len(*h) {
			return
		}
		smallest := left
		right := left + 1
		if right < len(*h) && h.Less(right, left) {
			smallest = right
		}
		if !h.Less(smallest, index) {
			return
		}
		h.Swap(index, smallest)
		index = smallest
	}
}

func incidentWorse(left, right OperationIncident) bool {
	if left.Score != right.Score {
		return left.Score > right.Score
	}
	if left.StartUnixMS != right.StartUnixMS {
		return left.StartUnixMS < right.StartUnixMS
	}
	if left.Operation != right.Operation {
		return left.Operation < right.Operation
	}
	if left.Screen != right.Screen {
		return left.Screen < right.Screen
	}
	return left.DurationMS > right.DurationMS
}

func operationEventUnixMS(header jhlog.SegmentHeader, eventElapsedMS uint64) uint64 {
	segmentElapsedMS := header.SegmentStartElapsedUS / 1_000
	if eventElapsedMS <= segmentElapsedMS {
		return header.SegmentStartUnixMS
	}
	return saturatingUint64Sum(header.SegmentStartUnixMS, eventElapsedMS-segmentElapsedMS)
}

func operationRecoveredStartUnixMS(
	header jhlog.SegmentHeader,
	eventElapsedMS uint64,
	durationUS uint64,
) uint64 {
	finishedUnixMS := operationEventUnixMS(header, eventElapsedMS)
	durationMS := microsecondsToMillisecondsCeil(durationUS)
	if durationMS >= finishedUnixMS {
		return 0
	}
	return finishedUnixMS - durationMS
}

func operationSlot(unixMS uint64, offsetMin int64) uint64 {
	offsetMS, validOffset := operationTimezoneOffsetMagnitudeMS(offsetMin)
	if !validOffset {
		return unixMS - unixMS%operationHourMS
	}
	if offsetMin >= 0 {
		if unixMS > ^uint64(0)-offsetMS {
			return unixMS - unixMS%operationHourMS
		}
		localHour := unixMS + offsetMS
		localHour -= localHour % operationHourMS
		if localHour < offsetMS {
			return 0
		}
		return localHour - offsetMS
	}
	if unixMS < offsetMS {
		return 0
	}
	localHour := unixMS - offsetMS
	localHour -= localHour % operationHourMS
	return saturatingUint64Sum(localHour, offsetMS)
}

func operationSlotLabel(startUnixMS uint64, offsetMin int64) string {
	date := "время вне диапазона"
	if localMS, ok := operationLocalUnixMS(startUnixMS, offsetMin); ok {
		date = time.UnixMilli(localMS).UTC().Format("2006-01-02 15:00")
	}
	sign := "+"
	offset := offsetMin
	if offset < 0 {
		sign = "-"
		if offset == math.MinInt64 {
			return fmt.Sprintf("%s (смещение вне диапазона)", date)
		}
		offset = -offset
	}
	return fmt.Sprintf("%s (UTC%s%02d:%02d)", date, sign, offset/60, offset%60)
}

func operationTimezoneOffsetMagnitudeMS(offsetMin int64) (uint64, bool) {
	if offsetMin > math.MaxInt64/60_000 || offsetMin < math.MinInt64/60_000 {
		return 0, false
	}
	offsetMS := offsetMin * 60_000
	if offsetMS >= 0 {
		return uint64(offsetMS), true
	}
	return uint64(-(offsetMS + 1)) + 1, true
}

func operationLocalUnixMS(startUnixMS uint64, offsetMin int64) (int64, bool) {
	if startUnixMS > math.MaxInt64 ||
		offsetMin > math.MaxInt64/60_000 || offsetMin < math.MinInt64/60_000 {
		return 0, false
	}
	start := int64(startUnixMS)
	offsetMS := offsetMin * 60_000
	if offsetMS > 0 && start > math.MaxInt64-offsetMS {
		return 0, false
	}
	if offsetMS < 0 && start < math.MinInt64-offsetMS {
		return 0, false
	}
	return start + offsetMS, true
}

func operationKindName(kind jhlog.OperationKind) string {
	switch kind {
	case jhlog.OperationKindUser:
		return "user"
	case jhlog.OperationKindScreen:
		return "screen"
	case jhlog.OperationKindBackground:
		return "background"
	case jhlog.OperationKindSystem:
		return "system"
	case jhlog.OperationKindStage:
		return "stage"
	default:
		return "unknown"
	}
}

func operationKindFromName(name string) jhlog.OperationKind {
	switch name {
	case "user":
		return jhlog.OperationKindUser
	case "screen":
		return jhlog.OperationKindScreen
	case "background":
		return jhlog.OperationKindBackground
	case "system":
		return jhlog.OperationKindSystem
	case "stage":
		return jhlog.OperationKindStage
	default:
		return jhlog.OperationKindUnknown
	}
}

func operationOutcomeName(outcome jhlog.OperationOutcome) string {
	switch outcome {
	case jhlog.OperationOutcomeSuccess:
		return "success"
	case jhlog.OperationOutcomeFailure:
		return "failure"
	case jhlog.OperationOutcomeCancelled:
		return "cancelled"
	case jhlog.OperationOutcomeTimeout:
		return "timeout"
	default:
		return "unknown"
	}
}

func sortOperationAnalysis(result *OperationAnalysis) {
	sort.Slice(result.Operations, func(i, j int) bool {
		left, right := result.Operations[i], result.Operations[j]
		if left.P95MS != right.P95MS {
			return left.P95MS > right.P95MS
		}
		if left.Count != right.Count {
			return left.Count > right.Count
		}
		return operationGroupLess(
			operationGroupKey{name: left.Operation, kind: left.Kind, screen: left.Screen},
			operationGroupKey{name: right.Operation, kind: right.Kind, screen: right.Screen},
		)
	})
	sort.Slice(result.TimeSlots, func(i, j int) bool {
		left, right := result.TimeSlots[i], result.TimeSlots[j]
		if left.StartUnixMS != right.StartUnixMS {
			return left.StartUnixMS < right.StartUnixMS
		}
		return operationGroupLess(
			operationGroupKey{name: left.Operation, kind: left.Kind, screen: left.Screen},
			operationGroupKey{name: right.Operation, kind: right.Kind, screen: right.Screen},
		)
	})
	sort.Slice(result.Dimensions, func(i, j int) bool {
		left, right := result.Dimensions[i], result.Dimensions[j]
		if left.P95MS != right.P95MS {
			return left.P95MS > right.P95MS
		}
		if left.Operation != right.Operation {
			return left.Operation < right.Operation
		}
		if left.Key != right.Key {
			return left.Key < right.Key
		}
		if left.Value != right.Value {
			return left.Value < right.Value
		}
		return left.Screen < right.Screen
	})
	sort.Slice(result.Stages, func(i, j int) bool {
		left, right := result.Stages[i], result.Stages[j]
		if left.TotalMS != right.TotalMS {
			return left.TotalMS > right.TotalMS
		}
		if left.ParentOperation != right.ParentOperation {
			return left.ParentOperation < right.ParentOperation
		}
		if left.Stage != right.Stage {
			return left.Stage < right.Stage
		}
		return left.Screen < right.Screen
	})
}

func operationGroupLess(left, right operationGroupKey) bool {
	if left.name != right.name {
		return left.name < right.name
	}
	if left.kind != right.kind {
		return left.kind < right.kind
	}
	return left.screen < right.screen
}

func compareOperationAnalysis(baseline, candidate Summary) []OperationDelta {
	base := make(map[operationGroupKey]OperationStats)
	cand := make(map[operationGroupKey]OperationStats)
	keys := make(map[operationGroupKey]struct{})
	if baseline.OperationAnalysis != nil {
		for _, stats := range baseline.OperationAnalysis.Operations {
			key := operationGroupKey{name: stats.Operation, kind: stats.Kind, screen: stats.Screen}
			base[key] = stats
			keys[key] = struct{}{}
		}
	}
	if candidate.OperationAnalysis != nil {
		for _, stats := range candidate.OperationAnalysis.Operations {
			key := operationGroupKey{name: stats.Operation, kind: stats.Kind, screen: stats.Screen}
			cand[key] = stats
			keys[key] = struct{}{}
		}
	}
	rows := make([]OperationDelta, 0, len(keys))
	for key := range keys {
		before, hasBefore := base[key]
		after, hasAfter := cand[key]
		identity := before
		if !hasBefore {
			identity = after
		}
		row := OperationDelta{
			Operation: identity.Operation, Kind: identity.Kind, Screen: identity.Screen,
			BaselineCount: before.Count, CandidateCount: after.Count,
			BaselineP95MS: before.P95MS, CandidateP95MS: after.P95MS,
			BaselineQuantilesApproximated:  before.QuantilesApproximated,
			CandidateQuantilesApproximated: after.QuantilesApproximated,
			P95ChangeMS:                    float64(after.P95MS) - float64(before.P95MS),
			P95ChangePct:                   operationRelativeChange(before.P95MS, after.P95MS),
			BaselineBudgeted:               before.Budgeted,
			CandidateBudgeted:              after.Budgeted,
			BaselineBudgetBreachPct:        before.BudgetBreachRatePct,
			CandidateBudgetBreachPct:       after.BudgetBreachRatePct,
			BaselineFailureRatePct:         operationFailureRate(before),
			CandidateFailureRatePct:        operationFailureRate(after),
			BaselineDatabaseCallsPerOperation: databasePerOperation(
				float64(before.CorrelatedDatabase), before.Count,
			),
			CandidateDatabaseCallsPerOperation: databasePerOperation(
				float64(after.CorrelatedDatabase), after.Count,
			),
			BaselineDatabaseMainRatePct: databasePercent(
				before.CorrelatedDatabaseMain, before.CorrelatedDatabase,
			),
			CandidateDatabaseMainRatePct: databasePercent(
				after.CorrelatedDatabaseMain, after.CorrelatedDatabase,
			),
			BaselineDatabaseFailureRatePct: databasePercent(
				before.CorrelatedDatabaseErrors, before.CorrelatedDatabase,
			),
			CandidateDatabaseFailureRatePct: databasePercent(
				after.CorrelatedDatabaseErrors, after.CorrelatedDatabase,
			),
			BaselineDatabaseWallMSPerOperation: databasePerOperation(
				float64(before.CorrelatedDatabaseUS)/1_000, before.Count,
			),
			CandidateDatabaseWallMSPerOperation: databasePerOperation(
				float64(after.CorrelatedDatabaseUS)/1_000, after.Count,
			),
		}
		row.FailureRateChangePP = row.CandidateFailureRatePct - row.BaselineFailureRatePct
		switch {
		case !hasBefore:
			row.Status, row.Severity, row.Confidence = "new", "ok", "low"
			row.Note = "Операция отсутствует в базовом наборе; ухудшение не заявляется без сопоставимой выборки."
		case !hasAfter:
			row.Status, row.Severity, row.Confidence = "removed", "ok", "low"
			row.Note = "Операция отсутствует в новом наборе."
		default:
			minimum := minUint64(before.Count, after.Count)
			row.Confidence = operationComparisonConfidence(minimum, baseline, candidate)
			row.Comparable = minimum >= operationComparisonMinSample
			row.DatabaseComparable = row.Comparable &&
				before.CorrelatedDatabase > 0 && after.CorrelatedDatabase > 0
			row.BudgetComparable = row.Comparable &&
				minUint64(before.Budgeted, after.Budgeted) >= operationComparisonMinSample
			if row.BudgetComparable {
				row.BudgetBreachChangePP = after.BudgetBreachRatePct - before.BudgetBreachRatePct
			}
			if !row.Comparable {
				row.Status, row.Severity = "insufficient_data", "ok"
				row.Note = fmt.Sprintf(
					"Для описательного сравнения нужно минимум %d завершений в каждом наборе; сейчас минимум %d.",
					operationComparisonMinSample,
					minimum,
				)
			} else {
				row.Severity = operationDeltaSeverity(row)
				switch {
				case row.Severity == "high" || row.Severity == "medium" || row.Severity == "low":
					row.Status = "regressed"
				case row.P95ChangeMS < 0 || row.BudgetBreachChangePP < 0 || row.FailureRateChangePP < 0:
					row.Status = "improved"
				default:
					row.Status = "stable"
				}
				row.Note = "Описательное сравнение одинаковой операции; причинность требует одинаковых условий и достаточной выборки."
				if !row.BudgetComparable && (before.Budgeted > 0 || after.Budgeted > 0) {
					row.Note += fmt.Sprintf(
						" Изменение нарушений бюджета не учитывалось: нужно минимум %d операций с заданным бюджетом в каждом наборе; база %d, кандидат %d.",
						operationComparisonMinSample,
						before.Budgeted,
						after.Budgeted,
					)
				}
				if row.BaselineQuantilesApproximated || row.CandidateQuantilesApproximated {
					row.Confidence = lowerConfidenceLevel(row.Confidence, "medium")
					row.Note += " Граница 95% оценена потоковым алгоритмом с ограниченной памятью."
				}
			}
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		left, right := rows[i], rows[j]
		if operationSeverityRank(left.Severity) != operationSeverityRank(right.Severity) {
			return operationSeverityRank(left.Severity) > operationSeverityRank(right.Severity)
		}
		if left.P95ChangeMS != right.P95ChangeMS {
			return left.P95ChangeMS > right.P95ChangeMS
		}
		return operationGroupLess(
			operationGroupKey{name: left.Operation, kind: left.Kind, screen: left.Screen},
			operationGroupKey{name: right.Operation, kind: right.Kind, screen: right.Screen},
		)
	})
	return rows
}

func operationRelativeChange(before, after uint64) float64 {
	if before == 0 {
		return 0
	}
	return (float64(after) - float64(before)) * 100 / float64(before)
}

func operationFailureRate(stats OperationStats) float64 {
	if stats.Count == 0 {
		return 0
	}
	return float64(stats.Failures+stats.Timeouts) * 100 / float64(stats.Count)
}

func operationComparisonConfidence(samples uint64, baseline, candidate Summary) string {
	level := "low"
	if samples >= 100 {
		level = "high"
	} else if samples >= 20 {
		level = "medium"
	}
	return lowerConfidenceLevel(
		lowerConfidenceLevel(level, collectionConfidenceCap(baseline)),
		collectionConfidenceCap(candidate),
	)
}

func operationDeltaSeverity(row OperationDelta) string {
	if (row.P95ChangeMS >= 500 && row.P95ChangePct >= 50) || row.BudgetBreachChangePP >= 20 ||
		row.FailureRateChangePP >= 10 {
		return "high"
	}
	if (row.P95ChangeMS >= 200 && row.P95ChangePct >= 20) || row.BudgetBreachChangePP >= 10 ||
		row.FailureRateChangePP >= 5 {
		return "medium"
	}
	if row.P95ChangeMS > 0 || row.BudgetBreachChangePP > 0 || row.FailureRateChangePP > 0 {
		return "low"
	}
	return "ok"
}

func operationSeverityRank(value string) int {
	switch value {
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	default:
		return 0
	}
}
