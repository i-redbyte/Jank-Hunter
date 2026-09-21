package mathanalysis

import (
	"strings"
	"testing"
)

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

	scores := computeIntegralScoresForRuns(timeline, loops, 1)

	assertFloat(t, integralScoreValue(scores, "jank_pressure_area"), 30)
	assertFloat(t, integralScoreValue(scores, "latency_pain_area"), 600)
	assertFloat(t, integralScoreValue(scores, "main_thread_stall_burden"), 0)
	assertFloat(t, integralScoreValue(scores, "network_failure_burn"), 12.5)
	assertFloat(t, integralScoreValue(scores, "memory_pressure_area"), 88)
	assertFloat(t, integralScoreValue(scores, "recovery_debt"), 3)
}

func TestComputeIntegralScoresReturnsNoSyntheticZerosWithoutTimeline(t *testing.T) {
	if scores := computeIntegralScoresForRuns(nil, nil, 1); len(scores) != 0 {
		t.Fatalf("empty timeline must not produce synthetic zero scores: %+v", scores)
	}
}

func TestComputeIntegralScoresIncludesMainThreadStalls(t *testing.T) {
	timeline := []TimelineBucket{
		{StartMS: 0, EndMS: 1_000, StallCount: 2, StallMaxMS: 600},
		{StartMS: 1_000, EndMS: 2_000, StallCount: 1, StallMaxMS: 1_100},
	}

	scores := computeIntegralScoresForRuns(timeline, nil, 1)
	assertFloat(t, integralScoreValue(scores, "main_thread_stall_burden"), 1_500)
	if got := integralScoreByID(scores, "main_thread_stall_burden").Severity; got != "medium" {
		t.Fatalf("stall burden severity = %q, want medium", got)
	}
}

func TestComputeIntegralScoresNormalizesCountBasedNetworkLoadAcrossRuns(t *testing.T) {
	timeline := []TimelineBucket{{StartMS: 0, EndMS: 1_000, HTTPFailed: 4}}
	loops := []NetworkLoopFinding{{BurnScore: 8}}

	scores := computeIntegralScoresForRuns(timeline, loops, 4)
	score := integralScoreByID(scores, "network_failure_burn")
	assertFloat(t, score.Value, 3)
	if score.RunCount != 4 || !strings.Contains(score.Formula, "число_прогонов") {
		t.Fatalf("multi-run score metadata is incomplete: %+v", score)
	}
}

func TestCompareIntegralScoresDoesNotShowPercentForZeroBaseline(t *testing.T) {
	baseline := []IntegralScore{{ID: "latency_pain_area", Title: "Задержка", Unit: "мс*с", Value: 0}}
	candidate := []IntegralScore{{ID: "latency_pain_area", Title: "Задержка", Unit: "мс*с", Value: 100}}

	deltas := compareIntegralScores(baseline, candidate)
	if len(deltas) != 1 || deltas[0].DeltaPctAvailable {
		t.Fatalf("zero baseline percentage must be unavailable: %+v", deltas)
	}
}

func TestCompareIntegralScoresDoesNotCallDifferentDurationsRegression(t *testing.T) {
	baseline := []IntegralScore{{ID: "latency_pain_area", Title: "Задержка", Unit: "мс*с", Value: 100, DurationMS: 10_000}}
	candidate := []IntegralScore{{ID: "latency_pain_area", Title: "Задержка", Unit: "мс*с", Value: 300, DurationMS: 30_000}}

	deltas := compareIntegralScores(baseline, candidate)
	if len(deltas) != 1 || deltas[0].Comparable || deltas[0].Severity != "medium" || deltas[0].DeltaPctAvailable {
		t.Fatalf("different durations must produce a comparability warning: %+v", deltas)
	}
}

func TestCompareIntegralScoresDoesNotCompareDifferentRunCounts(t *testing.T) {
	baseline := []IntegralScore{{ID: "latency_pain_area", Value: 100, DurationMS: 10_000, RunCount: 1}}
	candidate := []IntegralScore{{ID: "latency_pain_area", Value: 200, DurationMS: 10_000, RunCount: 2}}

	deltas := compareIntegralScores(baseline, candidate)
	if len(deltas) != 1 || deltas[0].Comparable || deltas[0].DeltaPctAvailable {
		t.Fatalf("different run counts must be incomparable: %+v", deltas)
	}
}

func TestCompareIntegralScoresReportsRegression(t *testing.T) {
	baseline := []IntegralScore{{
		ID:       "latency_pain_area",
		Title:    "Площадь сетевой задержки",
		Formula:  "Σ max(0, HTTP p95 - 300ms) * Δt",
		Unit:     "мс*с",
		Value:    100,
		Severity: "ok",
	}}
	candidate := []IntegralScore{{
		ID:       "latency_pain_area",
		Title:    "Площадь сетевой задержки",
		Formula:  "Σ max(0, HTTP p95 - 300ms) * Δt",
		Unit:     "мс*с",
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

func TestCompareIntegralFindingsKeepsQualityWarningAndRegression(t *testing.T) {
	deltas := []IntegralDelta{
		{Title: "Несопоставимая", Comparable: false, Severity: "medium", Summary: "разное число прогонов"},
		{Title: "Регрессия", Comparable: true, Severity: "high", Delta: 500, Summary: "нагрузка выросла"},
	}

	findings := compareIntegralFindings(deltas)
	if len(findings) != 2 || findings[0].Severity != "medium" || findings[1].Severity != "high" {
		t.Fatalf("quality warning and regression must both remain visible: %+v", findings)
	}
}

func integralScoreValue(scores []IntegralScore, id string) float64 {
	for _, score := range scores {
		if score.ID == id {
			return score.Value
		}
	}
	return 0
}

func integralScoreByID(scores []IntegralScore, id string) IntegralScore {
	for _, score := range scores {
		if score.ID == id {
			return score
		}
	}
	return IntegralScore{}
}
