package analyze

import (
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
