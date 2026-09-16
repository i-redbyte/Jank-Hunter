package mathanalysis

import "math/bits"

const defaultSpectralWorkOperations uint64 = 256 * 1024 * 1024

// Work units bound FFT butterflies, autocorrelation products and linear sample passes.
// This is an algorithmic bound across all signals and both comparison sides, not CPU time.
func (b *collectionBudget) chargeSpectralWork(operations uint64) bool {
	if b == nil {
		return true
	}
	if b.failure != nil {
		return false
	}
	if operations > b.workLimit-b.workUsed {
		b.failure = &collectionBudgetError{CollectionLimit{Component: "spectral analysis", Work: &SpectralWorkLimit{
			LimitOperations: b.workLimit, ConsumedOperations: b.workUsed, RequestedOperations: operations,
		}}}
		return false
	}
	b.workUsed += operations
	return true
}

func spectralWorkRequirement(n int) uint64 {
	if n < 2 {
		return uint64(n)
	}
	size := uint64(n)
	lags := min(n/3, maxAutocorrLag)
	autocorrelation := uint64(lags)*size - uint64(lags*(lags+1)/2)
	var transforms uint64
	if n&(n-1) == 0 {
		transforms = size / 2 * uint64(bits.Len(uint(n))-1)
	} else {
		m := uint64(1) << bits.Len(uint(2*n-2))
		transforms = 3*(m/2)*uint64(bits.Len64(m)-1) + m
	}
	return transforms + autocorrelation + 32*size
}

func spectralScratchBytes(n int) uint64 {
	if n < 2 {
		return 0
	}
	size := uint64(n)
	if n&(n-1) == 0 {
		return 128 * size
	}
	m := uint64(1) << bits.Len(uint(2*n-2))
	return 128*size + 32*m
}

func newCollectionBudgetWithWork(memoryLimit, workLimit uint64) *collectionBudget {
	budget := newCollectionBudget(memoryLimit)
	if workLimit > 0 {
		budget.workLimit = workLimit
	}
	return budget
}
