package analyze

import (
	"encoding/binary"
	"math/bits"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

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
