package analyze

import (
	"encoding/json"
	"testing"
)

func BenchmarkEvaluateConfiguredGate(b *testing.B) {
	var config ThresholdConfig
	if err := json.Unmarshal([]byte(`{"metrics":{"HTTP p95":{"max_regression_pct":10}}}`), &config); err != nil {
		b.Fatal(err)
	}
	comparison := Compare(Summary{LogCount: 5, EventCount: 500, HTTPCount: 100, HTTPP95MS: 100}, Summary{LogCount: 5, EventCount: 500, HTTPCount: 100, HTTPP95MS: 120})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result := EvaluateGate(comparison, config)
		if !result.Failed || len(result.Failures) != 1 {
			b.Fatalf("expected one measured regression: %+v", result)
		}
	}
}
