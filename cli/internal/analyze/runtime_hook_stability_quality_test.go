package analyze

import (
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
	"testing"
)

func TestProducerDegradationNeverMeansCompleteCollection(t *testing.T) {
	for _, id := range []uint64{jhlog.QualityHookReentrancy, jhlog.QualityCollectorBreaker, jhlog.QualityRegistryContention, jhlog.QualityHookCounterContention} {
		t.Run(jhlog.QualityCounterName(id), func(t *testing.T) {
			if !jhlog.IsKnownQualityCounter(id) {
				t.Fatal("unknown stability counter")
			}
			quality := crashCounterCollectionQuality(t, "unrelated", 0, map[uint64]uint64{id: 3, jhlog.QualityRuntimeHookFailureTotal: 3})
			if quality.Complete || quality.CriticalRuntimeHookFailures != 3 || len(quality.RuntimeHookFailureDetails) != 1 {
				t.Fatalf("producer degradation hidden: %+v", quality)
			}
		})
	}
}
