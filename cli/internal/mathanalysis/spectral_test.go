package mathanalysis

import (
	"math"
	"testing"
)

func TestAutocorrelationDetectsPeriodicPulse(t *testing.T) {
	points := make([]float64, 32)
	for i := range points {
		if i%4 == 0 {
			points[i] = 1
		}
	}

	signal := analyzePeriodicSignal("HTTP ошибки", "шт", 1000, points)

	if signal.FirstSignificantLagMS != 4000 {
		t.Fatalf("FirstSignificantLagMS = %d, want 4000; signal=%+v", signal.FirstSignificantLagMS, signal)
	}
	if len(signal.TopLags) == 0 || signal.TopLags[0].LagMS != 4000 {
		t.Fatalf("top autocorrelation lag = %+v, want 4000ms", signal.TopLags)
	}
}

func TestSpectralAnalysisDetectsSinePeriod(t *testing.T) {
	points := make([]float64, 64)
	for i := range points {
		points[i] = math.Sin(2 * math.Pi * float64(i) / 8)
	}

	signal := analyzePeriodicSignal("UI доля jank", "%", 1000, points)

	if len(signal.Peaks) == 0 {
		t.Fatalf("expected spectral peaks: %+v", signal)
	}
	period := signal.Peaks[0].PeriodMS
	if period < 7500 || period > 8500 {
		t.Fatalf("top spectral period = %dms, want around 8000ms; peaks=%+v", period, signal.Peaks)
	}
	if signal.SpectralEntropy > 0.5 {
		t.Fatalf("SpectralEntropy = %.3f, want a concentrated spectrum", signal.SpectralEntropy)
	}
}

func TestSpectralAnalysisPreservesPeriodAfterDownsampling(t *testing.T) {
	points := make([]float64, 4_097)
	for index := range points {
		points[index] = math.Sin(2 * math.Pi * float64(index) / 64)
	}

	signal := analyzePeriodicSignal("HTTP запросы", "шт", 1_000, points)

	if !signal.Approximated || signal.AnalysisBucketMS <= signal.BucketMS {
		t.Fatalf("long signal must expose effective downsample step: %+v", signal)
	}
	if len(signal.Peaks) == 0 || signal.Peaks[0].PeriodMS < 60_000 || signal.Peaks[0].PeriodMS > 68_000 {
		t.Fatalf("downsampled period must stay near 64s: %+v", signal.Peaks)
	}
}

func TestSpectralAnalysisHidesDeterministicBackgroundNoise(t *testing.T) {
	points := make([]float64, 256)
	state := uint32(7)
	for index := range points {
		state = state*1_664_525 + 1_013_904_223
		points[index] = float64(state>>8) / float64(1<<24)
	}

	signal := analyzePeriodicSignal("шум", "знач.", 1_000, points)

	if len(signal.Peaks) != 0 || signal.FirstSignificantLagMS != 0 {
		t.Fatalf("background noise must not become a periodic claim: %+v", signal)
	}
}

func TestPeriodicAnalysisUsesLongestMeasuredRunInsteadOfZeroFilledGaps(t *testing.T) {
	timeline := make([]TimelineBucket, 20)
	for index := range timeline {
		timeline[index] = TimelineBucket{StartMS: uint64(index) * 1_000, EndMS: uint64(index+1) * 1_000}
		if index == 6 {
			continue
		}
		timeline[index].UIFrames = 100
		timeline[index].UIJankyFrames = uint64(5 + index%3)
	}

	signals, _ := buildPeriodicAnalysisWithRouteDefinitions(timeline, timelineScale{bucketMS: 1_000}, nil)
	for _, signal := range signals {
		if signal.Signal != "Доля подтормаживаний UI" {
			continue
		}
		if signal.TotalBucketCount != 20 || signal.ObservedBucketCount != 19 || signal.SampleCount != 13 {
			t.Fatalf("periodic coverage is wrong: %+v", signal)
		}
		return
	}
	t.Fatalf("UI periodic signal not found: %+v", signals)
}
