package analyze

import (
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

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
