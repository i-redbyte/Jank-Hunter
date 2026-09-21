package mathanalysis

import (
	"math"
	"testing"
)

func TestTopSpectralPeaksRetainOnlyBoundedResultStorage(t *testing.T) {
	for _, n := range []int{128, 2048} {
		points := make([]float64, n)
		for i := range points {
			points[i] = math.Sin(2 * math.Pi * float64(i) / 8)
		}
		peaks, _ := spectralPeaks("timer", 1000, points, 3)
		if len(peaks) == 0 || len(peaks) > 3 {
			t.Fatal("fixture must return one to three peaks")
		}
		if cap(peaks) > 3 {
			t.Fatalf("top-three output retains storage for %d peaks from %d samples", cap(peaks), n)
		}
	}
}
