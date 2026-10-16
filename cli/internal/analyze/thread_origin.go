package analyze

import (
	"math"
	"math/bits"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

// Kernel task start is quantized by CLK_TCK. Neither endpoint denotes Thread
// object creation, Thread.start return, or entry to the application run method.
type ThreadKernelStart struct {
	FromElapsedMS, ByElapsedMS uint64
	FromUnixMS, ByUnixMS       *uint64
	BeforeCollection           *bool
}

type threadOriginMetadata struct {
	pid, encodedCollectionStart, anchorElapsed, anchorUnix uint64
}

func (s *threadCollectorState) recordOrigin(index uint64, result jhlog.StreamResult) {
	if s.origins == nil {
		s.origins = make(map[uint64]threadOriginMetadata)
	}
	v := threadOriginMetadata{pid: result.Header.OSPID, anchorElapsed: result.Header.SegmentStartElapsedUS / 1000, anchorUnix: result.Header.SegmentStartUnixMS}
	if result.LatestQuality != nil {
		v.encodedCollectionStart = result.LatestQuality.Counters[jhlog.QualityCollectionWindowStartElapsedMS]
	}
	s.origins[index] = v
}

func threadKernelStart(ticks, hz, observedBy, encodedCollectionStart, anchorElapsed, anchorUnix uint64) *ThreadKernelStart {
	if hz == 0 || ticks == math.MaxUint64 {
		return nil
	}
	from, _, ok := threadTickMS(ticks, hz)
	if !ok || from > observedBy {
		return nil
	}
	by, remainder, ok := threadTickMS(ticks+1, hz)
	if !ok || remainder != 0 && by == math.MaxUint64 {
		return nil
	}
	if remainder != 0 {
		by++
	}
	v := &ThreadKernelStart{FromElapsedMS: from, ByElapsedMS: by, FromUnixMS: observationUnixMS(anchorElapsed, anchorUnix, from), ByUnixMS: observationUnixMS(anchorElapsed, anchorUnix, by)}
	// RuntimeCoordinator encodes elapsed milliseconds + 1 (zero means absent).
	// Millisecond collection timestamps themselves cover [start, start+1).
	if encodedCollectionStart > 0 && encodedCollectionStart-1 <= observedBy {
		start := encodedCollectionStart - 1
		if by <= start {
			b := true
			v.BeforeCollection = &b
		} else if from >= encodedCollectionStart {
			b := false
			v.BeforeCollection = &b
		}
	}
	return v
}
func threadTickMS(ticks, hz uint64) (uint64, uint64, bool) {
	high, low := bits.Mul64(ticks, 1000)
	if high >= hz {
		return 0, 0, false
	}
	quotient, remainder := bits.Div64(high, low, hz)
	return quotient, remainder, true
}
