package analyze

import (
	"encoding/binary"
	"sort"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

const (
	databaseTransactionActiveLimit      = 4_096
	databaseTransactionActiveInitial    = 16
	databaseTransactionActiveCapacity   = databaseTransactionActiveLimit * 2
	databaseTransactionDetailLimit      = 1_024
	databaseTransactionMainSlowUS       = 16_000
	databaseTransactionBackgroundSlowUS = 100_000
	databaseTransactionManyStatements   = 20
)

type databaseTransactionKey struct {
	processInstanceID jhlog.ID128
	logIndex          uint64
	transactionID     uint64
}

type databaseTransactionContext struct {
	source            string
	screen            string
	contextOwner      string
	contextOperation  string
	process           string
	processInstanceID jhlog.ID128
	sessionID         jhlog.ID128
	transactionID     uint64
	parentID          uint64
	mode              jhlog.DatabaseTransactionMode
	mainThread        bool
}

type databaseTransactionActiveSlot struct {
	key        databaseTransactionKey
	value      databaseTransactionContext
	generation uint32
	occupied   bool
}

type databaseTransactionActiveTable struct {
	slots      []databaseTransactionActiveSlot
	count      int
	tombstones int
	generation uint32
}

func (t *databaseTransactionActiveTable) put(
	key databaseTransactionKey,
	value databaseTransactionContext,
) (duplicate, full bool) {
	if len(t.slots) == 0 {
		t.slots = make([]databaseTransactionActiveSlot, databaseTransactionActiveInitial)
		t.generation = 1
	}
	if t.count < databaseTransactionActiveLimit &&
		(t.count+t.tombstones+1)*2 > len(t.slots) {
		t.resize()
	}
	index, found := t.find(key)
	if found {
		return true, false
	}
	if t.count >= databaseTransactionActiveLimit {
		return false, true
	}
	slot := &t.slots[index]
	if slot.generation == t.generation && !slot.occupied {
		t.tombstones--
	}
	slot.key = key
	slot.value = value
	slot.generation = t.generation
	slot.occupied = true
	t.count++
	return false, false
}

func (t *databaseTransactionActiveTable) take(
	key databaseTransactionKey,
) (databaseTransactionContext, bool) {
	if t.count == 0 {
		return databaseTransactionContext{}, false
	}
	index, found := t.find(key)
	if !found {
		return databaseTransactionContext{}, false
	}
	slot := &t.slots[index]
	value := slot.value
	slot.key = databaseTransactionKey{}
	slot.value = databaseTransactionContext{}
	slot.occupied = false
	t.count--
	if t.count == 0 {
		t.nextGeneration()
	} else {
		t.tombstones++
	}
	return value, true
}

func (t *databaseTransactionActiveTable) find(key databaseTransactionKey) (int, bool) {
	mask := len(t.slots) - 1
	index := int(databaseTransactionKeyHashValue(key)) & mask
	firstTombstone := -1
	for probes := 0; probes < len(t.slots); probes++ {
		slot := &t.slots[index]
		if slot.generation != t.generation {
			if firstTombstone >= 0 {
				return firstTombstone, false
			}
			return index, false
		}
		if slot.occupied {
			if slot.key == key {
				return index, true
			}
		} else if firstTombstone < 0 {
			firstTombstone = index
		}
		index = (index + 1) & mask
	}
	return firstTombstone, false
}

func (t *databaseTransactionActiveTable) resize() {
	capacity := len(t.slots)
	if capacity < databaseTransactionActiveCapacity {
		capacity <<= 1
	}
	previous := t.slots
	previousGeneration := t.generation
	t.slots = make([]databaseTransactionActiveSlot, capacity)
	t.count = 0
	t.tombstones = 0
	t.generation = 1
	for index := range previous {
		slot := &previous[index]
		if slot.generation != previousGeneration || !slot.occupied {
			continue
		}
		t.insertRehashed(slot.key, slot.value)
	}
}

func (t *databaseTransactionActiveTable) insertRehashed(
	key databaseTransactionKey,
	value databaseTransactionContext,
) {
	index, _ := t.find(key)
	slot := &t.slots[index]
	slot.key = key
	slot.value = value
	slot.generation = t.generation
	slot.occupied = true
	t.count++
}

func (t *databaseTransactionActiveTable) nextGeneration() {
	t.tombstones = 0
	t.generation++
	if t.generation != 0 {
		return
	}
	clear(t.slots)
	t.generation = 1
}

func databaseTransactionKeyHashValue(key databaseTransactionKey) uint64 {
	hash := key.transactionID ^ mixDatabaseTransactionHash(key.logIndex+0x9e3779b97f4a7c15)
	lower := binary.LittleEndian.Uint64(key.processInstanceID[:8])
	upper := binary.LittleEndian.Uint64(key.processInstanceID[8:])
	return mixDatabaseTransactionHash(hash ^ mixDatabaseTransactionHash(lower) ^ upper)
}

func mixDatabaseTransactionHash(value uint64) uint64 {
	value ^= value >> 30
	value *= 0xbf58476d1ce4e5b9
	value ^= value >> 27
	value *= 0x94d049bb133111eb
	return value ^ value>>31
}

type databaseTransactionSample struct {
	key            databaseTransactionKey
	context        databaseTransactionContext
	outcome        jhlog.DatabaseTransactionOutcome
	failureKind    jhlog.DatabaseFailureKind
	durationUS     uint64
	statementCount uint64
	readCount      uint64
	writeCount     uint64
	complete       bool
	missingStart   bool
}

type databaseTransactionAccumulator struct {
	active              databaseTransactionActiveTable
	details             []databaseTransactionSample
	durations           operationDurationSummary
	events              uint64
	begun               uint64
	completed           uint64
	incomplete          uint64
	missingStart        uint64
	duplicateStart      uint64
	droppedActiveStarts uint64
	droppedDetails      uint64
	evictedDetails      uint64
	success             uint64
	rollbacks           uint64
	failures            uint64
	mainThread          uint64
	background          uint64
	nested              uint64
	totalDurationUS     uint64
	totalStatementCount uint64
	totalReadCount      uint64
	totalWriteCount     uint64
	maxStatementCount   uint64
	modes               [5]uint64
	outcomes            [4]uint64
	failureKinds        [8]uint64
}

func (a *databaseTransactionAccumulator) add(
	event *jhlog.DatabaseTransactionEvent,
	flags uint64,
	logIndex uint64,
	source string,
	context SignalContextStats,
	process string,
	processInstanceID, sessionID jhlog.ID128,
) {
	a.events++
	key := databaseTransactionScopeKey(processInstanceID, logIndex, event.TransactionID)
	current := databaseTransactionContext{
		source: source, screen: context.Screen, contextOwner: context.Owner,
		contextOperation: context.Operation, process: process,
		processInstanceID: processInstanceID, sessionID: sessionID,
		transactionID: event.TransactionID, parentID: event.ParentID, mode: event.Mode,
		mainThread: flags&uint64(jhlog.FlagThreadMain) != 0,
	}
	switch event.Stage {
	case jhlog.DatabaseTransactionBegin:
		a.addBegin(key, current)
	case jhlog.DatabaseTransactionTerminal:
		a.addTerminal(key, current, event)
	}
}

func databaseTransactionScopeKey(
	processInstanceID jhlog.ID128,
	logIndex, transactionID uint64,
) databaseTransactionKey {
	key := databaseTransactionKey{processInstanceID: processInstanceID, transactionID: transactionID}
	if processInstanceID.IsZero() {
		key.logIndex = logIndex
	}
	return key
}

func (a *databaseTransactionAccumulator) addBegin(
	key databaseTransactionKey,
	context databaseTransactionContext,
) {
	duplicate, full := a.active.put(key, context)
	if duplicate {
		a.duplicateStart++
		return
	}
	a.begun++
	if full {
		a.droppedActiveStarts++
	}
}

func (a *databaseTransactionAccumulator) addTerminal(
	key databaseTransactionKey,
	terminal databaseTransactionContext,
	event *jhlog.DatabaseTransactionEvent,
) {
	start, hasStart := a.active.take(key)
	if hasStart {
		terminal = mergeDatabaseTransactionContext(start, terminal)
	} else {
		a.missingStart++
	}
	sample := databaseTransactionSample{
		key: key, context: terminal, outcome: event.Outcome, failureKind: event.FailureKind,
		durationUS: event.DurationUS, statementCount: event.StatementCount,
		readCount: event.ReadCount, writeCount: event.WriteCount,
		complete: true, missingStart: !hasStart,
	}
	a.completed++
	a.durations.add(event.DurationUS)
	a.totalDurationUS = saturatingUint64Sum(a.totalDurationUS, event.DurationUS)
	a.totalStatementCount = saturatingUint64Sum(a.totalStatementCount, event.StatementCount)
	a.totalReadCount = saturatingUint64Sum(a.totalReadCount, event.ReadCount)
	a.totalWriteCount = saturatingUint64Sum(a.totalWriteCount, event.WriteCount)
	a.maxStatementCount = maxUint64(a.maxStatementCount, event.StatementCount)
	a.countTransaction(sample)
	a.admitTransaction(sample)
}

func mergeDatabaseTransactionContext(
	start, terminal databaseTransactionContext,
) databaseTransactionContext {
	terminal.source = firstKnown(terminal.source, start.source)
	terminal.screen = firstKnown(terminal.screen, start.screen)
	terminal.contextOwner = firstKnown(terminal.contextOwner, start.contextOwner)
	terminal.contextOperation = firstKnown(terminal.contextOperation, start.contextOperation)
	terminal.process = firstKnown(terminal.process, start.process)
	if terminal.processInstanceID.IsZero() {
		terminal.processInstanceID = start.processInstanceID
	}
	if terminal.sessionID.IsZero() {
		terminal.sessionID = start.sessionID
	}
	if terminal.parentID == 0 {
		terminal.parentID = start.parentID
	}
	if terminal.mode == jhlog.DatabaseTransactionModeUnknown {
		terminal.mode = start.mode
	}
	terminal.mainThread = terminal.mainThread || start.mainThread
	return terminal
}

func (a *databaseTransactionAccumulator) countTransaction(sample databaseTransactionSample) {
	if sample.context.mainThread {
		a.mainThread++
	} else {
		a.background++
	}
	if sample.context.parentID != 0 {
		a.nested++
	}
	if int(sample.context.mode) < len(a.modes) {
		a.modes[sample.context.mode]++
	}
	if sample.complete {
		if int(sample.outcome) < len(a.outcomes) {
			a.outcomes[sample.outcome]++
		}
		switch sample.outcome {
		case jhlog.DatabaseTransactionSuccess:
			a.success++
		case jhlog.DatabaseTransactionRollback:
			a.rollbacks++
		case jhlog.DatabaseTransactionFailure:
			a.failures++
			if int(sample.failureKind) < len(a.failureKinds) {
				a.failureKinds[sample.failureKind]++
			}
		}
	}
}

func (a *databaseTransactionAccumulator) finalize() *DatabaseTransactionAnalysis {
	return a.finalizeWithCorrelations(nil)
}

func (a *databaseTransactionAccumulator) finalizeWithCorrelations(
	correlations map[databaseTransactionKey]DatabaseCorrelationStats,
) *DatabaseTransactionAnalysis {
	if a.events == 0 {
		return nil
	}
	for index := range a.active.slots {
		slot := &a.active.slots[index]
		if slot.generation != a.active.generation || !slot.occupied {
			continue
		}
		context := slot.value
		sample := databaseTransactionSample{key: slot.key, context: context}
		a.incomplete++
		a.countTransaction(sample)
		a.admitTransaction(sample)
	}
	a.active = databaseTransactionActiveTable{}
	rows := make([]DatabaseTransactionStats, len(a.details))
	for index := range a.details {
		sample := a.details[index]
		rows[index] = databaseTransactionStats(sample, correlations[sample.key])
	}
	sort.Slice(rows, func(i, j int) bool {
		return databaseTransactionStatsLess(rows[j], rows[i])
	})
	outcomes := databaseNamedValues(a.outcomes[:], databaseTransactionOutcomeName)
	if a.incomplete > 0 {
		outcomes = append(outcomes, NamedValue{Name: "incomplete", Value: a.incomplete})
	}
	return &DatabaseTransactionAnalysis{
		Events: a.events, Begun: a.begun, Completed: a.completed, Incomplete: a.incomplete,
		MissingStart: a.missingStart, DuplicateStart: a.duplicateStart,
		DroppedActiveStarts:       a.droppedActiveStarts,
		DroppedTransactionDetails: a.droppedDetails,
		EvictedTransactionDetails: a.evictedDetails,
		Success:                   a.success, Rollbacks: a.rollbacks, Failures: a.failures,
		MainThread: a.mainThread, Background: a.background, Nested: a.nested,
		P50DurationUS: a.durations.percentile(0.50),
		P95DurationUS: a.durations.percentile(0.95), MaxDurationUS: a.durations.max,
		TotalDurationUS: a.totalDurationUS, MaxStatementCount: a.maxStatementCount,
		TotalStatementCount: a.totalStatementCount,
		TotalReadCount:      a.totalReadCount, TotalWriteCount: a.totalWriteCount,
		QuantilesApproximated: a.durations.approximated(),
		Modes:                 databaseNamedValues(a.modes[:], databaseTransactionModeName),
		Outcomes:              outcomes,
		FailureKinds:          databaseNamedValues(a.failureKinds[:], databaseFailureKindName),
		Transactions:          rows,
	}
}

func (a *databaseTransactionAccumulator) admitTransaction(sample databaseTransactionSample) {
	if len(a.details) < databaseTransactionDetailLimit {
		a.details = append(a.details, sample)
		databaseTransactionHeapUp(a.details, len(a.details)-1)
		return
	}
	if databaseTransactionSampleCompare(a.details[0], sample) >= 0 {
		a.droppedDetails++
		return
	}
	a.details[0] = sample
	databaseTransactionHeapDown(a.details, 0)
	a.droppedDetails++
	a.evictedDetails++
}

func databaseTransactionHeapUp(values []databaseTransactionSample, index int) {
	for index > 0 {
		parent := (index - 1) >> 1
		if databaseTransactionSampleCompare(values[parent], values[index]) <= 0 {
			return
		}
		values[parent], values[index] = values[index], values[parent]
		index = parent
	}
}

func databaseTransactionHeapDown(values []databaseTransactionSample, index int) {
	for {
		left := index*2 + 1
		if left >= len(values) {
			return
		}
		smallest := left
		right := left + 1
		if right < len(values) && databaseTransactionSampleCompare(values[right], values[left]) < 0 {
			smallest = right
		}
		if databaseTransactionSampleCompare(values[index], values[smallest]) <= 0 {
			return
		}
		values[index], values[smallest] = values[smallest], values[index]
		index = smallest
	}
}

func databaseTransactionSampleCompare(left, right databaseTransactionSample) int {
	leftClass, rightClass := databaseTransactionRetentionClass(left), databaseTransactionRetentionClass(right)
	if leftClass != rightClass {
		return compareUint8(leftClass, rightClass)
	}
	if left.durationUS != right.durationUS {
		return compareUint64(left.durationUS, right.durationUS)
	}
	if left.statementCount != right.statementCount {
		return compareUint64(left.statementCount, right.statementCount)
	}
	if left.context.transactionID != right.context.transactionID {
		return compareUint64(left.context.transactionID, right.context.transactionID)
	}
	if left.context.process != right.context.process {
		if left.context.process < right.context.process {
			return -1
		}
		return 1
	}
	return 0
}

func compareUint8(left, right uint8) int {
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}

func compareUint64(left, right uint64) int {
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}

func databaseTransactionRetentionClass(sample databaseTransactionSample) uint8 {
	switch {
	case sample.complete && sample.outcome == jhlog.DatabaseTransactionFailure:
		return 5
	case !sample.complete || sample.outcome == jhlog.DatabaseTransactionRollback:
		return 4
	case sample.context.mainThread && sample.durationUS >= databaseTransactionMainSlowUS,
		!sample.context.mainThread && sample.durationUS >= databaseTransactionBackgroundSlowUS:
		return 3
	case sample.statementCount >= databaseTransactionManyStatements:
		return 2
	case sample.context.parentID != 0:
		return 1
	default:
		return 0
	}
}

func databaseTransactionStats(
	sample databaseTransactionSample,
	correlation DatabaseCorrelationStats,
) DatabaseTransactionStats {
	outcome := "incomplete"
	if sample.complete {
		outcome = databaseTransactionOutcomeName(int(sample.outcome))
	}
	return DatabaseTransactionStats{
		Source: sample.context.source, Screen: sample.context.screen,
		ContextOwner:      sample.context.contextOwner,
		ContextOperation:  sample.context.contextOperation,
		Process:           sample.context.process,
		ProcessInstanceID: databaseIdentity(sample.context.processInstanceID),
		SessionID:         databaseIdentity(sample.context.sessionID),
		Mode:              databaseTransactionModeName(int(sample.context.mode)), Outcome: outcome,
		FailureKind:   databaseTransactionFailureName(sample.failureKind),
		TransactionID: sample.context.transactionID, ParentID: sample.context.parentID,
		DurationUS: sample.durationUS, StatementCount: sample.statementCount,
		ReadCount: sample.readCount, WriteCount: sample.writeCount,
		MainThread: sample.context.mainThread, Complete: sample.complete,
		MissingStart: sample.missingStart, Correlation: correlation,
	}
}

func databaseTransactionStatsLess(left, right DatabaseTransactionStats) bool {
	leftClass := databaseTransactionStatsRetentionClass(left)
	rightClass := databaseTransactionStatsRetentionClass(right)
	if leftClass != rightClass {
		return leftClass < rightClass
	}
	if left.DurationUS != right.DurationUS {
		return left.DurationUS < right.DurationUS
	}
	if left.StatementCount != right.StatementCount {
		return left.StatementCount < right.StatementCount
	}
	if left.TransactionID != right.TransactionID {
		return left.TransactionID < right.TransactionID
	}
	return left.Process < right.Process
}

func databaseTransactionStatsRetentionClass(row DatabaseTransactionStats) uint8 {
	switch {
	case row.Outcome == "failure":
		return 5
	case !row.Complete || row.Outcome == "rollback":
		return 4
	case row.MainThread && row.DurationUS >= databaseTransactionMainSlowUS,
		!row.MainThread && row.DurationUS >= databaseTransactionBackgroundSlowUS:
		return 3
	case row.StatementCount >= databaseTransactionManyStatements:
		return 2
	case row.ParentID != 0:
		return 1
	default:
		return 0
	}
}

func databaseTransactionModeName(value int) string {
	switch jhlog.DatabaseTransactionMode(value) {
	case jhlog.DatabaseTransactionDeferred:
		return "deferred"
	case jhlog.DatabaseTransactionImmediate:
		return "immediate"
	case jhlog.DatabaseTransactionExclusive:
		return "exclusive"
	case jhlog.DatabaseTransactionReadOnly:
		return "read_only"
	default:
		return "unknown"
	}
}

func databaseTransactionOutcomeName(value int) string {
	switch jhlog.DatabaseTransactionOutcome(value) {
	case jhlog.DatabaseTransactionSuccess:
		return "success"
	case jhlog.DatabaseTransactionRollback:
		return "rollback"
	case jhlog.DatabaseTransactionFailure:
		return "failure"
	default:
		return "unknown"
	}
}

func databaseTransactionFailureName(value jhlog.DatabaseFailureKind) string {
	if value == jhlog.DatabaseFailureNone {
		return "none"
	}
	return databaseFailureKindName(int(value))
}
