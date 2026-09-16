package analyze

import (
	"math"
	"math/big"
	"math/rand"
	"testing"
)

func TestMetricSumArithmeticMatchesBigIntegers(t *testing.T) {
	random := rand.New(rand.NewSource(13))
	for index := 0; index < 2048; index++ {
		count := random.Uint64() | 1
		value := random.Uint64()
		sum := new(big.Int).Mul(new(big.Int).SetUint64(count), new(big.Int).SetUint64(value))
		low := sum.Uint64()
		high := new(big.Int).Rsh(new(big.Int).Set(sum), 64).Uint64()
		if got := metricSumAverage(low, high, count); got != value {
			t.Fatalf("mean=%d, want %d", got, value)
		}
		scaled := new(big.Int).Mul(new(big.Int).SetUint64(value), big.NewInt(100))
		want := scaled.Uint64()
		if scaled.BitLen() > 64 {
			want = math.MaxUint64
		}
		if got := metricSumScaledAverage(low, high, count, 100); got != want {
			t.Fatalf("scaled mean=%d, want %d", got, want)
		}
	}
}

func TestWorkerMetricRangeAfterLargePrefixKeepsItsSamples(t *testing.T) {
	const value = uint64(1<<63 - 1)
	index := workerPointIndex{points: []workerPoint{
		{timeMS: 1, value: value, count: 1, max: value},
		{timeMS: 2, value: value, count: 1, max: value},
		{timeMS: 3, value: value, count: 1, max: value},
	}}
	index.build()
	got := index.query(3, 3)
	if got.count != 1 || got.sum != value || got.maximum != value {
		t.Fatalf("late range lost its sum after prefix saturation: %+v", got)
	}
}

func TestGenericGaugeMeanAccumulatesLargeRecordsExactly(t *testing.T) {
	const value = uint64(1<<63 - 1)
	var gauge gaugeStats
	for index := 0; index < 3; index++ {
		gauge.add(value, 1, value, value, 1)
	}
	if gauge.value() != value {
		t.Fatalf("generic gauge mean = %d, want %d", gauge.value(), value)
	}
}
