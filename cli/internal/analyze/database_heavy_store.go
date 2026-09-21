package analyze

import (
	"container/heap"
	"math"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

const (
	databaseFrequencySketchWidth = 65_536
	databaseFrequencySketchDepth = 4
	databaseContextPerStatement  = 8
)

type databaseStatementEntry struct {
	key                    databaseStatementKey
	keyHash                uint64
	statementFingerprint   uint64
	stats                  databaseAggregate
	telemetry              databaseTelemetryAggregate
	contexts               []databaseContextEntry
	estimatedCalls         uint64
	frequencyEstimateError uint64
	retentionClass         uint8
	maxDurationUS          uint64
	heapIndex              int
}

type databaseContextEntry struct {
	key                    databaseContextKey
	keyHash                uint64
	stats                  databaseAggregate
	estimatedCalls         uint64
	frequencyEstimateError uint64
	retentionClass         uint8
	maxDurationUS          uint64
}

type databaseStatementStore struct {
	capacity               int
	entries                map[databaseStatementKey]*databaseStatementEntry
	ranking                databaseStatementHeap
	statementFrequency     databaseFrequencySketch
	contextFrequency       databaseFrequencySketch
	droppedStatementEvents uint64
	droppedContextEvents   uint64
	evictedStatements      uint64
	evictedContexts        uint64
}

func newDatabaseStatementStore(capacity int) databaseStatementStore {
	return databaseStatementStore{
		capacity: capacity,
		entries:  make(map[databaseStatementKey]*databaseStatementEntry, capacity),
		ranking:  make(databaseStatementHeap, 0, capacity),
	}
}

func (s *databaseStatementStore) add(
	statementKey databaseStatementKey,
	contextKey databaseContextKey,
	event *jhlog.DatabaseEvent,
	flags, logIndex, timeMS uint64,
) uint64 {
	keyHash := databaseStatementKeyHash(statementKey)
	estimatedCalls := s.statementFrequency.add(keyHash)
	retentionClass := databaseEventRetentionClass(event, flags)
	entry := s.entries[statementKey]
	if entry == nil {
		entry = s.admitStatement(statementKey, keyHash, estimatedCalls, retentionClass, event.DurationUS)
		if entry == nil {
			s.droppedStatementEvents = saturatingUint64Sum(s.droppedStatementEvents, 1)
			return estimatedCalls
		}
	} else {
		entry.estimatedCalls = estimatedCalls
		entry.retentionClass = max(entry.retentionClass, retentionClass)
		entry.maxDurationUS = maxUint64(entry.maxDurationUS, event.DurationUS)
	}
	if entry.statementFingerprint == 0 {
		entry.statementFingerprint = event.StatementFingerprint
	}
	entry.stats.add(event, flags, logIndex, timeMS)
	entry.telemetry.add(event)
	entry.frequencyEstimateError = maxUint64(
		entry.frequencyEstimateError,
		saturatingUint64Sub(entry.estimatedCalls, entry.stats.overall.calls),
	)
	heap.Fix(&s.ranking, entry.heapIndex)
	s.addContext(entry, contextKey, event, flags, logIndex, timeMS, retentionClass)
	return estimatedCalls
}

func (s *databaseStatementStore) admitStatement(
	key databaseStatementKey,
	keyHash, estimatedCalls uint64,
	retentionClass uint8,
	durationUS uint64,
) *databaseStatementEntry {
	candidate := databaseRetentionScore{
		keyHash: keyHash, estimatedCalls: estimatedCalls,
		retentionClass: retentionClass, maxDurationUS: durationUS,
	}
	if s.capacity == 0 || len(s.entries) >= s.capacity &&
		(len(s.ranking) == 0 || databaseRetentionPriorityCompare(s.ranking[0].retentionScore(), candidate) >= 0) {
		return nil
	}
	if len(s.entries) >= s.capacity {
		evicted := heap.Pop(&s.ranking).(*databaseStatementEntry)
		delete(s.entries, evicted.key)
		s.evictedStatements = saturatingUint64Sum(s.evictedStatements, 1)
		s.droppedStatementEvents = saturatingUint64Sum(
			s.droppedStatementEvents,
			evicted.stats.overall.calls,
		)
	}
	entry := &databaseStatementEntry{
		key: key, keyHash: keyHash, estimatedCalls: estimatedCalls,
		frequencyEstimateError: estimatedCalls - 1,
		retentionClass:         retentionClass, maxDurationUS: durationUS, heapIndex: -1,
	}
	s.entries[key] = entry
	heap.Push(&s.ranking, entry)
	return entry
}

func (s *databaseStatementStore) addContext(
	statement *databaseStatementEntry,
	key databaseContextKey,
	event *jhlog.DatabaseEvent,
	flags, logIndex, timeMS uint64,
	retentionClass uint8,
) {
	keyHash := databaseContextKeyHash(key)
	estimatedCalls := s.contextFrequency.add(keyHash)
	for index := range statement.contexts {
		entry := &statement.contexts[index]
		if entry.key != key {
			continue
		}
		entry.estimatedCalls = estimatedCalls
		entry.retentionClass = max(entry.retentionClass, retentionClass)
		entry.maxDurationUS = maxUint64(entry.maxDurationUS, event.DurationUS)
		entry.stats.add(event, flags, logIndex, timeMS)
		entry.frequencyEstimateError = maxUint64(
			entry.frequencyEstimateError,
			saturatingUint64Sub(entry.estimatedCalls, entry.stats.overall.calls),
		)
		return
	}
	candidate := databaseContextEntry{
		key: key, keyHash: keyHash, estimatedCalls: estimatedCalls,
		frequencyEstimateError: estimatedCalls - 1,
		retentionClass:         retentionClass, maxDurationUS: event.DurationUS,
	}
	if len(statement.contexts) < databaseContextPerStatement {
		candidate.stats.add(event, flags, logIndex, timeMS)
		statement.contexts = append(statement.contexts, candidate)
		return
	}
	minimum := 0
	for index := 1; index < len(statement.contexts); index++ {
		if databaseContextEntryLess(&statement.contexts[index], &statement.contexts[minimum]) {
			minimum = index
		}
	}
	if databaseRetentionPriorityCompare(
		statement.contexts[minimum].retentionScore(),
		candidate.retentionScore(),
	) >= 0 {
		s.droppedContextEvents = saturatingUint64Sum(s.droppedContextEvents, 1)
		return
	}
	evicted := &statement.contexts[minimum]
	s.droppedContextEvents = saturatingUint64Sum(s.droppedContextEvents, evicted.stats.overall.calls)
	s.evictedContexts = saturatingUint64Sum(s.evictedContexts, 1)
	candidate.stats.add(event, flags, logIndex, timeMS)
	*evicted = candidate
}

func databaseEventRetentionClass(event *jhlog.DatabaseEvent, flags uint64) uint8 {
	if event.Outcome == jhlog.DatabaseOutcomeFailure {
		return 4
	}
	if flags&uint64(jhlog.FlagThreadMain) != 0 {
		if event.DurationUS >= 16_000 {
			return 3
		}
		return 2
	}
	if event.DurationUS >= 100_000 {
		return 1
	}
	return 0
}

type databaseStatementHeap []*databaseStatementEntry

func (h databaseStatementHeap) Len() int { return len(h) }

func (h databaseStatementHeap) Less(i, j int) bool {
	return databaseStatementEntryLess(h[i], h[j])
}

func (h databaseStatementHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].heapIndex = i
	h[j].heapIndex = j
}

func (h *databaseStatementHeap) Push(value any) {
	entry := value.(*databaseStatementEntry)
	entry.heapIndex = len(*h)
	*h = append(*h, entry)
}

func (h *databaseStatementHeap) Pop() any {
	old := *h
	last := len(old) - 1
	entry := old[last]
	old[last] = nil
	entry.heapIndex = -1
	*h = old[:last]
	return entry
}

func databaseStatementEntryLess(left, right *databaseStatementEntry) bool {
	return databaseRetentionScoreLess(left.retentionScore(), right.retentionScore())
}

type databaseRetentionScore struct {
	keyHash        uint64
	estimatedCalls uint64
	maxDurationUS  uint64
	retentionClass uint8
}

func (e *databaseStatementEntry) retentionScore() databaseRetentionScore {
	return databaseRetentionScore{
		keyHash: e.keyHash, estimatedCalls: e.estimatedCalls,
		maxDurationUS: e.maxDurationUS, retentionClass: e.retentionClass,
	}
}

func (e *databaseContextEntry) retentionScore() databaseRetentionScore {
	return databaseRetentionScore{
		keyHash: e.keyHash, estimatedCalls: e.estimatedCalls,
		maxDurationUS: e.maxDurationUS, retentionClass: e.retentionClass,
	}
}

func databaseRetentionScoreLess(left, right databaseRetentionScore) bool {
	comparison := databaseRetentionPriorityCompare(left, right)
	if comparison != 0 {
		return comparison < 0
	}
	return left.keyHash < right.keyHash
}

func databaseRetentionPriorityCompare(left, right databaseRetentionScore) int {
	if left.retentionClass != right.retentionClass {
		if left.retentionClass < right.retentionClass {
			return -1
		}
		return 1
	}
	if left.retentionClass > 0 && left.maxDurationUS != right.maxDurationUS {
		if left.maxDurationUS < right.maxDurationUS {
			return -1
		}
		return 1
	}
	if left.estimatedCalls != right.estimatedCalls {
		if left.estimatedCalls < right.estimatedCalls {
			return -1
		}
		return 1
	}
	if left.maxDurationUS != right.maxDurationUS {
		if left.maxDurationUS < right.maxDurationUS {
			return -1
		}
		return 1
	}
	return 0
}

func databaseContextEntryLess(left, right *databaseContextEntry) bool {
	return databaseRetentionScoreLess(left.retentionScore(), right.retentionScore())
}

type databaseFrequencySketch struct {
	counters []uint32
	total    uint64
}

func (s *databaseFrequencySketch) add(hash uint64) uint64 {
	if s.counters == nil {
		s.counters = make([]uint32, databaseFrequencySketchWidth*databaseFrequencySketchDepth)
	}
	s.total = saturatingUint64Sum(s.total, 1)
	var indices [databaseFrequencySketchDepth]int
	minimum := uint32(math.MaxUint32)
	for row := 0; row < databaseFrequencySketchDepth; row++ {
		mixed := databaseSketchHash(hash, row)
		index := row*databaseFrequencySketchWidth + int(mixed&(databaseFrequencySketchWidth-1))
		indices[row] = index
		value := s.counters[index]
		minimum = min(minimum, value)
	}
	if minimum == math.MaxUint32 {
		return uint64(minimum)
	}
	next := minimum + 1
	for _, index := range indices {
		if s.counters[index] == minimum {
			s.counters[index] = next
		}
	}
	return uint64(next)
}

func (s *databaseFrequencySketch) estimatedError() uint64 {
	if s.total == 0 {
		return 0
	}
	return (s.total + databaseFrequencySketchWidth - 1) / databaseFrequencySketchWidth
}

func databaseSketchHash(value uint64, row int) uint64 {
	value ^= uint64(row+1) * 0x9e3779b97f4a7c15
	value ^= value >> 30
	value *= 0xbf58476d1ce4e5b9
	value ^= value >> 27
	value *= 0x94d049bb133111eb
	return value ^ value>>31
}

func databaseStatementKeyHash(key databaseStatementKey) uint64 {
	hash := databaseHashString(databaseHashOffset, key.query)
	hash = databaseHashString(hash, key.operation)
	hash = databaseHashString(hash, key.fallbackSource)
	return databaseHashString(hash, key.fallbackFramework)
}

func databaseContextKeyHash(key databaseContextKey) uint64 {
	hash := databaseStatementKeyHash(key.statement)
	hash = databaseHashString(hash, key.source)
	hash = databaseHashString(hash, key.framework)
	hash = databaseHashString(hash, key.screen)
	hash = databaseHashString(hash, key.contextOwner)
	hash = databaseHashString(hash, key.contextOperation)
	hash = databaseHashUint64(hash, key.operationID)
	hash = databaseHashString(hash, key.process)
	for _, value := range key.processInstanceID {
		hash = databaseHashByte(hash, value)
	}
	for _, value := range key.sessionID {
		hash = databaseHashByte(hash, value)
	}
	return hash
}

func databaseHashString(hash uint64, value string) uint64 {
	for index := 0; index < len(value); index++ {
		hash = databaseHashByte(hash, value[index])
	}
	return databaseHashByte(hash, 0xff)
}

func databaseHashUint64(hash, value uint64) uint64 {
	for range 8 {
		hash = databaseHashByte(hash, byte(value))
		value >>= 8
	}
	return hash
}

func databaseHashByte(hash uint64, value byte) uint64 {
	return (hash ^ uint64(value)) * databaseHashPrime
}

func saturatingUint64Sub(left, right uint64) uint64 {
	if left <= right {
		return 0
	}
	return left - right
}

const (
	databaseHashOffset = uint64(14695981039346656037)
	databaseHashPrime  = uint64(1099511628211)
)
