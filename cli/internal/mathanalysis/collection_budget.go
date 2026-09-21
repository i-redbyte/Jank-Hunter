package mathanalysis

import "fmt"

const defaultMathCollectionMemoryBytes uint64 = 128 * 1024 * 1024

// These conservative charges cover map growth and retained collector/result ownership, not total
// process RSS, parser dictionaries or the caller's already-built application summary.
const (
	mathMapBaseBytes  uint64 = 512
	mathMapEntryBytes uint64 = 128
)

type collectionBudgetError struct{ limit CollectionLimit }

func (e *collectionBudgetError) Error() string {
	if work := e.limit.Work; work != nil {
		return fmt.Sprintf("spectral work limit: %d operations requested with %d of %d consumed", work.RequestedOperations, work.ConsumedOperations, work.LimitOperations)
	}

	return fmt.Sprintf("mathematical collection memory limit: %s needs %d bytes with %d of %d reserved",
		e.limit.Component, e.limit.RequestedBytes, e.limit.ReservedBytes, e.limit.LimitBytes)
}

type collectionBudget struct {
	workLimit uint64
	workUsed  uint64
	limit     uint64
	used      uint64
	peak      uint64
	failure   *collectionBudgetError
}

func newCollectionBudget(limit uint64) *collectionBudget {
	if limit == 0 {
		limit = defaultMathCollectionMemoryBytes
	}
	return &collectionBudget{limit: limit, workLimit: defaultSpectralWorkOperations}
}

func (b *collectionBudget) account(component string) *collectionAccount {
	return &collectionAccount{budget: b, component: component}
}

func (b *collectionBudget) err() error {
	if b.failure == nil {
		return nil
	}
	return b.failure
}

type collectionAccount struct {
	budget    *collectionBudget
	component string
	reserved  uint64
}

func (a *collectionAccount) reserve(bytes uint64) bool {
	if a == nil {
		return true
	}
	b := a.budget
	if b.failure != nil {
		return false
	}
	if bytes > b.limit-b.used {
		return a.reject(bytes)
	}
	b.used += bytes
	a.reserved += bytes
	if b.used > b.peak {
		b.peak = b.used
	}
	return true
}

func (a *collectionAccount) reserveItems(count int, itemBytes uint64) bool {
	if a == nil {
		return true
	}
	if count < 0 || (itemBytes > 0 && uint64(count) > ^uint64(0)/itemBytes) {
		return a.reject(^uint64(0))
	}
	return a.reserve(uint64(count) * itemBytes)
}

func (a *collectionAccount) reject(bytes uint64) bool {
	if a.budget.failure == nil {
		a.budget.failure = &collectionBudgetError{CollectionLimit{Component: a.component, LimitBytes: a.budget.limit,
			ReservedBytes: a.budget.used, RequestedBytes: bytes}}
	}
	return false
}

func (a *collectionAccount) release(bytes uint64) {
	if a == nil {
		return
	}
	if bytes > a.reserved {
		panic("mathematical collection storage released twice")
	}
	a.reserved -= bytes
	a.budget.used -= bytes
}

func (a *collectionAccount) close() {
	if a != nil {
		a.release(a.reserved)
	}
}

func (a *collectionAccount) scratch(component string) *collectionAccount {
	if a == nil {
		return nil
	}
	return a.budget.account(component)
}

func (a *collectionAccount) canReserve(bytes uint64) bool {
	return a == nil || (a.budget.failure == nil && bytes <= a.budget.limit-a.budget.used)
}
