package mathanalysis

import "testing"

func TestComputeIntegralScoresUsesKnownAreas(t *testing.T) {
	timeline := []TimelineBucket{
		{
			StartMS:           0,
			EndMS:             1000,
			HTTPCount:         1,
			HTTPFailed:        1,
			HTTPP95DurationMS: 500,
			DNSCount:          4,
			ConnectCount:      2,
			UIFrames:          100,
			UIJankyFrames:     10,
			MemoryPSSKB:       100 * 1024,
			AvailableMemoryKB: 300 * 1024,
		},
		{
			StartMS:           1000,
			EndMS:             2000,
			HTTPCount:         1,
			HTTPP95DurationMS: 700,
			UIFrames:          100,
			UIJankyFrames:     20,
			MemoryPSSKB:       132 * 1024,
			AvailableMemoryKB: 200 * 1024,
		},
		{
			StartMS:           2000,
			EndMS:             3000,
			UIFrames:          100,
			UIJankyFrames:     0,
			MemoryPSSKB:       100 * 1024,
			AvailableMemoryKB: 300 * 1024,
		},
	}
	loops := []NetworkLoopFinding{{BurnScore: 10}}

	scores := computeIntegralScores(timeline, loops)

	assertFloat(t, integralScoreValue(scores, "jank_pressure_area"), 30)
	assertFloat(t, integralScoreValue(scores, "latency_pain_area"), 600)
	assertFloat(t, integralScoreValue(scores, "network_failure_burn"), 12.5)
	assertFloat(t, integralScoreValue(scores, "memory_pressure_area"), 88)
	assertFloat(t, integralScoreValue(scores, "recovery_debt"), 3)
}

func TestCompareIntegralScoresReportsRegression(t *testing.T) {
	baseline := []IntegralScore{{
		ID:       "latency_pain_area",
		Title:    "Площадь сетевой задержки",
		Formula:  "Σ max(0, HTTP p95 - 300ms) * Δt",
		Unit:     "ms*с",
		Value:    100,
		Severity: "ok",
	}}
	candidate := []IntegralScore{{
		ID:       "latency_pain_area",
		Title:    "Площадь сетевой задержки",
		Formula:  "Σ max(0, HTTP p95 - 300ms) * Δt",
		Unit:     "ms*с",
		Value:    700,
		Severity: "medium",
	}}

	deltas := compareIntegralScores(baseline, candidate)
	if len(deltas) != 1 {
		t.Fatalf("len(deltas) = %d, want 1", len(deltas))
	}
	if deltas[0].Severity != "high" {
		t.Fatalf("delta severity = %q, want high: %+v", deltas[0].Severity, deltas[0])
	}
	assertFloat(t, deltas[0].Delta, 600)
}

func integralScoreValue(scores []IntegralScore, id string) float64 {
	for _, score := range scores {
		if score.ID == id {
			return score.Value
		}
	}
	return 0
}
