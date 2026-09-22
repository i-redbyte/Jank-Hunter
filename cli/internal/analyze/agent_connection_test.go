package analyze

import (
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func TestAgentStackSampleDecodesTriggerFromHighWord(t *testing.T) {
	aggregator := newAgentAggregator()
	aggregator.addStackSample(
		jhlog.Event{TimeUS: 1_000, Source: "log"},
		&jhlog.AgentEvent{
			SemanticType:     jhlog.AgentThreadStackSample,
			ProducerSequence: 1,
			Payload0:         0xAB,
			Payload2:         uint64(42) | uint64(3)<<32,
		},
		agentContext{},
	)
	if len(aggregator.stackSamples) != 1 {
		t.Fatalf("samples=%d", len(aggregator.stackSamples))
	}
	if aggregator.stackSamples[0].trigger != 3 {
		t.Fatalf("trigger=%d", aggregator.stackSamples[0].trigger)
	}
}

func TestEnrichAgentSummaryWithoutAgentEvents(t *testing.T) {
	summary := AgentSummary{EventCount: 0}
	enrichAgentSummary(&summary)
	if len(summary.ConnectionGuide) == 0 || len(summary.PlatformLimits) == 0 {
		t.Fatalf("guide=%v limits=%v", summary.ConnectionGuide, summary.PlatformLimits)
	}
}

func TestQualityPenaltyScalesWithNativeLoss(t *testing.T) {
	low := AgentSummary{Quality: AgentQualitySummary{OtherNativeLossTotal: 2}}
	high := AgentSummary{Quality: AgentQualitySummary{OtherNativeLossTotal: 40}}
	if qualityPenalty(low) >= qualityPenalty(high) {
		t.Fatalf("low=%d high=%d", qualityPenalty(low), qualityPenalty(high))
	}
}
