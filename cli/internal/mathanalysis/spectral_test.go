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
