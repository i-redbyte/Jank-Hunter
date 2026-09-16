package mathanalysis

import (
	"fmt"
	"math"
	"math/rand"
	"testing"
)

func TestSpectralPowersMatchDirectTransform(t *testing.T) {
	for _, n := range []int{0, 1, 2, 3, 4, 7, 12, 31, 64, 127, 256, 509, 2047, 2048} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			rng := rand.New(rand.NewSource(int64(n + 1)))
			points := make([]float64, n)
			for i := range points {
				points[i] = rng.Float64()*2 - 1
			}
			got := dftPowers(points)
			want := directPowerOracle(points)
			if len(got) != len(want) {
				t.Fatalf("frequency count=%d want%d", len(got), len(want))
			}
			for k := range want {
				if math.Abs(got[k]-want[k]) > 1e-8*math.Max(1, want[k]) {
					t.Fatalf("bin%d=%g want%g", k+1, got[k], want[k])
				}
			}
		})
	}
}

func directPowerOracle(points []float64) []float64 {
	powers := make([]float64, len(points)/2)
	for k := range powers {
		var real, imag float64
		for j, x := range points {
			angle := -2 * math.Pi * float64((k+1)*j) / float64(len(points))
			real += x * math.Cos(angle)
			imag += x * math.Sin(angle)
		}
		powers[k] = real*real + imag*imag
	}
	return powers
}

func TestPeriodicAnalysisDoesNotAliasSkippedSamples(t *testing.T) {
	points := make([]float64, 4097)
	for i := range points {
		points[i] = 2 + math.Sin(2*math.Pi*float64(i)/3)
	}
	signal := analyzePeriodicSignal("fast timer", "count", 1000, points)
	if len(signal.Peaks) == 0 || signal.Peaks[0].PeriodMS < 2900 || signal.Peaks[0].PeriodMS > 3100 {
		t.Fatalf("three-second source aliased into another period: %+v", signal.Peaks)
	}
}

var spectralPowerSink []float64
var periodicSignalSink PeriodicSignal

func BenchmarkSpectralPowers(b *testing.B) {
	for _, n := range []int{2047, 2048} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			points := make([]float64, n)
			for i := range points {
				points[i] = math.Sin(2 * math.Pi * float64(i) / 32)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				spectralPowerSink = dftPowers(points)
			}
		})
	}
}

func BenchmarkPeriodicTwentySignals(b *testing.B) {
	points := make([]float64, 2048)
	for i := range points {
		points[i] = 2 + math.Sin(2*math.Pi*float64(i)/32)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := 0; j < 20; j++ {
			periodicSignalSink = analyzePeriodicSignal("timer", "count", 1000, points)
		}
	}
}

func TestConstantSeriesCannotBecomePeriodicFromRounding(t *testing.T) {
	for _, value := range []float64{0, 1, 0.1, 1e18 + 128} {
		points := make([]float64, 127)
		for i := range points {
			points[i] = value
		}
		signal := analyzePeriodicSignal("constant", "count", 1000, points)
		if len(signal.Peaks) > 0 || signal.FirstSignificantLagMS != 0 {
			t.Fatalf("constant %g became periodic: lag=%d peaks=%v", value, signal.FirstSignificantLagMS, signal.Peaks)
		}
	}
}

func TestSpectralTransformPreservesEnergyAndNyquist(t *testing.T) {
	for _, n := range []int{31, 64, 2047, 2048, 50000} {
		points := make([]float64, n)
		var sum, energy float64
		for i := range points {
			points[i] = float64(i%7) - 3
			sum += points[i]
			energy += points[i] * points[i]
		}
		powers := dftPowers(points)
		var measured float64
		for _, p := range powers {
			measured += 2 * p
		}
		if n%2 == 0 {
			measured -= powers[len(powers)-1]
		}
		want := float64(n)*energy - sum*sum
		if math.Abs(measured-want) > 1e-8*math.Max(1, want) {
			t.Fatalf("N=%d energy=%g want%g", n, measured, want)
		}
	}
	points := make([]float64, 64)
	for i := range points {
		points[i] = 1
		if i%2 == 1 {
			points[i] = -1
		}
	}
	powers := dftPowers(points)
	if math.Abs(powers[31]-4096) > 1e-8 {
		t.Fatalf("Nyquist power=%g", powers[31])
	}
	for _, p := range powers[:31] {
		if p > 1e-8 {
			t.Fatal("Nyquist leaked into other frequency bins")
		}
	}
}

func TestCenteringPreservesVariationOnLargeOffset(t *testing.T) {
	points, shifted := make([]float64, 127), make([]float64, 127)
	for i := range points {
		points[i] = float64(i%7) - 3
		shifted[i] = points[i] + float64(uint64(1)<<50)
	}
	a, b := centeredValues(points), centeredValues(shifted)
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("large DC offset changed variation at%d: %g/%g", i, a[i], b[i])
		}
	}
}

func TestSpectrumKeepsMultiplePeriods(t *testing.T) {
	points := make([]float64, 4096)
	for i := range points {
		points[i] = 2 + math.Sin(2*math.Pi*float64(i)/8) + 0.5*math.Sin(2*math.Pi*float64(i)/4)
	}
	signal := analyzePeriodicSignal("two timers", "count", 1000, points)
	found4, found8 := false, false
	for _, peak := range signal.Peaks {
		found4 = found4 || (peak.PeriodMS > 3950 && peak.PeriodMS < 4050)
		found8 = found8 || (peak.PeriodMS > 7950 && peak.PeriodMS < 8050)
	}
	if !found4 || !found8 {
		t.Fatalf("lost a real source period: %+v", signal.Peaks)
	}
}
