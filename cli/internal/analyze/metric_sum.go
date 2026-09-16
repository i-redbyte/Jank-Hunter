package analyze

import (
	"math"
	"math/bits"
)

func addMetricSum(low, high, valueLow, valueHigh uint64) (uint64, uint64) {
	low, carry := bits.Add64(low, valueLow, 0)
	high, overflow := bits.Add64(high, valueHigh, carry)
	if overflow != 0 {
		return math.MaxUint64, math.MaxUint64
	}
	return low, high
}

func metricSumAverage(low, high, count uint64) uint64 {
	if count == 0 {
		return 0
	}
	if high >= count {
		return math.MaxUint64
	}
	quotient, _ := bits.Div64(high, low, count)
	return quotient
}

func metricSumScaledAverage(low, high, count, scale uint64) uint64 {
	if count == 0 || scale == 0 {
		return 0
	}
	if high >= count {
		return math.MaxUint64
	}
	quotient, remainder := bits.Div64(high, low, count)
	if quotient > math.MaxUint64/scale {
		return math.MaxUint64
	}
	h, l := bits.Mul64(remainder, scale)
	fraction, _ := bits.Div64(h, l, count)
	return saturatingUint64Sum(quotient*scale, fraction)
}
