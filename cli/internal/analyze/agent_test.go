package analyze

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

type fixtureEventStream struct {
	source string
	events []jhlog.Event
	open   bool
}

func (stream fixtureEventStream) Stream(handle jhlog.EventHandler) (jhlog.StreamResult, error) {
	dict := map[uint64]string{}
	for _, input := range stream.events {
		event := input
		event.Source = stream.source
		if event.Dictionary != nil {
			dict[event.Dictionary.ID] = event.Dictionary.Value
		}
		if err := handle(event, dict); err != nil {
			return jhlog.StreamResult{}, err
		}
	}
	status := jhlog.SegmentStatusClosedClean
	if stream.open {
		status = jhlog.SegmentStatusOpenWithTail
	}
	return jhlog.StreamResult{Source: stream.source, Version: jhlog.FormatVersion, Status: status, Sealed: !stream.open, Events: uint64(len(stream.events))}, nil
}

func TestAgentImageDecodeGCStallEvidence(t *testing.T) {
	summary, err := InspectEventStreamsWithOptions("image", []jhlog.CanonicalEventStream{
		fixtureEventStream{source: "current-v9", events: agentImageFixture(false)},
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !summary.Agent.Available || summary.Agent.EffectivePreset != "CAUSAL" || summary.Agent.GC.Count != 1 {
		t.Fatalf("agent summary = %+v", summary.Agent)
	}
	if summary.Agent.Quality.SequenceGaps != 0 || len(summary.Agent.Findings) == 0 {
		t.Fatalf("quality/findings = %+v / %+v", summary.Agent.Quality, summary.Agent.Findings)
	}
	finding := summary.Agent.Findings[0]
	if finding.EvidenceLevel != "STRONG_ASSOCIATION" || finding.Confidence != "HIGH" ||
		!strings.Contains(finding.SuspectedCause, "изображения") || !strings.Contains(finding.Method, "BitmapFactory") {
		t.Fatalf("finding = %+v", finding)
	}
	if len(finding.EvidenceChain) < 3 || !strings.Contains(strings.Join(finding.PositiveEvidence, " "), "сборки мусора") {
		t.Fatalf("evidence chain = %+v", finding.EvidenceChain)
	}
	if finding.Method != "android.graphics.BitmapFactory.decodeStream(java.io.InputStream): android.graphics.Bitmap" {
		t.Fatalf("readable method = %q", finding.Method)
	}
}

func TestAgentNoDataPreservesCoreSummary(t *testing.T) {
	events := []jhlog.Event{{Type: jhlog.EventStall, TimeMS: 500, Stall: &jhlog.StallEvent{DurationMS: 200}}}
	summary, err := InspectEventStreamsWithOptions("old", []jhlog.CanonicalEventStream{fixtureEventStream{source: "old", events: events}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if summary.StallCount != 1 || summary.StallMaxMS != 200 || summary.Agent.EventCount != 0 || summary.Agent.Availability != "данных агента нет" {
		t.Fatalf("legacy projection changed: %+v", summary)
	}
}

func TestAgentSequenceGapMarksIncompleteAndLowersConfidence(t *testing.T) {
	complete, err := InspectEventStreamsWithOptions("fixture", []jhlog.CanonicalEventStream{fixtureEventStream{source: "ring", events: agentImageFixture(false)}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	incomplete, err := InspectEventStreamsWithOptions("fixture", []jhlog.CanonicalEventStream{fixtureEventStream{source: "ring", events: agentImageFixture(true), open: true}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if incomplete.Agent.Quality.SequenceGaps != 1 || len(incomplete.Agent.DataGaps) == 0 {
		t.Fatalf("incomplete quality = %+v gaps=%v", incomplete.Agent.Quality, incomplete.Agent.DataGaps)
	}
	if len(complete.Agent.Findings) == 0 || len(incomplete.Agent.Findings) == 0 || incomplete.Agent.Findings[0].ConfidenceScore >= complete.Agent.Findings[0].ConfidenceScore {
		t.Fatalf("confidence complete=%+v incomplete=%+v", complete.Agent.Findings, incomplete.Agent.Findings)
	}
}

func TestCanonicalCurrentAndRingAdaptersProduceEquivalentAgentSummary(t *testing.T) {
	events := agentImageFixture(false)
	current, err := InspectEventStreamsWithOptions("same", []jhlog.CanonicalEventStream{fixtureEventStream{source: "adapter", events: events}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	ring, err := InspectEventStreamsWithOptions("same", []jhlog.CanonicalEventStream{fixtureEventStream{source: "adapter", events: append([]jhlog.Event(nil), events...)}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	currentJSON, _ := json.Marshal(current.Agent)
	ringJSON, _ := json.Marshal(ring.Agent)
	if string(currentJSON) != string(ringJSON) {
		t.Fatalf("canonical adapters differ:\n%s\n%s", currentJSON, ringJSON)
	}
}

func TestAgentAggregatorChunkMergeIsOrderIndependentForAdditiveEvidence(t *testing.T) {
	events := []jhlog.Event{
		agentEvent("merge", 100, jhlog.AgentGCInterval, 1, 100, 10),
		agentEvent("merge", 200, jhlog.AgentGCInterval, 2, 200, 20),
		agentEvent("merge", 300, jhlog.AgentMonitorContentionInterval, 3, 300, 30),
	}
	onePass := newAgentAggregator()
	left := newAgentAggregator()
	right := newAgentAggregator()
	for index, event := range events {
		onePass.add(nil, event, FlowStats{})
		if index%2 == 0 {
			left.add(nil, event, FlowStats{})
		} else {
			right.add(nil, event, FlowStats{})
		}
	}
	mergedA := newAgentAggregator()
	mergedA.merge(left)
	mergedA.merge(right)
	mergedB := newAgentAggregator()
	mergedB.merge(right)
	mergedB.merge(left)
	want := onePass.snapshot()
	for _, got := range []AgentSummary{mergedA.snapshot(), mergedB.snapshot()} {
		if !reflect.DeepEqual(got.GC, want.GC) || !reflect.DeepEqual(got.Contention, want.Contention) || got.Quality.SequenceGaps != want.Quality.SequenceGaps {
			t.Fatalf("merged snapshot differs: got=%+v want=%+v", got, want)
		}
	}
}

func TestAgentCompareSurfacesCompatibilityBeforeDeltas(t *testing.T) {
	baseline := Summary{Agent: AgentSummary{EventCount: 10, EffectivePreset: "LIGHT", ConfigHash: "0x1",
		Capabilities: AgentCapabilitySummary{Active: 0x3}, GC: AgentIntervalSummary{Count: 2, TotalMS: 20}}}
	candidate := Summary{Agent: AgentSummary{EventCount: 20, EffectivePreset: "CAUSAL", ConfigHash: "0x2",
		Capabilities: AgentCapabilitySummary{Active: 0xf}, GC: AgentIntervalSummary{Count: 4, TotalMS: 80},
		DataGaps: []string{"пропуск последовательности"}, Stacks: AgentStackSummary{Hotspots: []AgentStackHotspot{{Methods: []string{"app.Foo.work(): void"}}}}}}
	comparison := Compare(baseline, candidate)
	if !comparison.Agent.ConfigMismatch || !comparison.Agent.CapabilityMismatch || len(comparison.Agent.Warnings) < 3 {
		t.Fatalf("agent comparison = %+v", comparison.Agent)
	}
	if len(comparison.Agent.NewStackSuspects) != 1 || comparison.Agent.NewStackSuspects[0] != "app.Foo.work(): void" {
		t.Fatalf("stack suspects = %v", comparison.Agent.NewStackSuspects)
	}
	foundGC := false
	for _, delta := range comparison.Deltas {
		if delta.Name == "JVM TI: время сборки мусора" {
			foundGC = true
		}
	}
	if !foundGC {
		t.Fatal("JVM TI GC delta missing")
	}
}

func TestAgentAggregatorCardinalityIsBounded(t *testing.T) {
	aggregator := newAgentAggregator()
	for index := 0; index < 20_000; index++ {
		source := "source-" + strings.Repeat("x", index%31) + string(rune(index+1))
		event := agentEvent(source, uint64(index+1), jhlog.AgentThreadStackSample, uint64(index+1), uint64(index+1), 0)
		event.Agent.Payload0 = uint64(index + 1)
		aggregator.add(nil, event, FlowStats{})
	}
	if len(aggregator.sources) > maxAgentSources || len(aggregator.sequences) > maxAgentSources ||
		len(aggregator.stacks) > maxAgentFingerprints || len(aggregator.stackSamples) > maxAgentTimelineItems {
		t.Fatalf("unbounded agent state: sources=%d sequences=%d stacks=%d samples=%d", len(aggregator.sources), len(aggregator.sequences), len(aggregator.stacks), len(aggregator.stackSamples))
	}
	if !aggregator.sourceCapacityLost || len(aggregator.snapshot().DataGaps) == 0 {
		t.Fatal("capacity loss must remain visible as quality evidence")
	}
}

func BenchmarkAgentAggregatorBoundedStreaming(b *testing.B) {
	event := agentEvent("bench", 100, jhlog.AgentGCInterval, 1, 100, 10_000)
	b.ReportAllocs()
	for iteration := 0; iteration < b.N; iteration++ {
		aggregator := newAgentAggregator()
		for sequence := uint64(1); sequence <= 10_000; sequence++ {
			event.Agent.ProducerSequence = sequence
			aggregator.add(nil, event, FlowStats{})
		}
		if len(aggregator.gc.Top) > maxAgentTopIntervals || len(aggregator.sequences) != 1 {
			b.Fatal("bounded state invariant failed")
		}
	}
}

func agentImageFixture(dropGCSequence bool) []jhlog.Event {
	context := jhlog.AttributionContext{Present: true, Screen: jhlog.LocalSymbol(1), Owner: jhlog.LocalSymbol(2), Flow: jhlog.LocalSymbol(3)}
	events := []jhlog.Event{
		{Type: jhlog.EventDictionary, Dictionary: &jhlog.DictionaryEntry{ID: 1, Kind: jhlog.DictScreen, Value: "Feed"}},
		{Type: jhlog.EventDictionary, Dictionary: &jhlog.DictionaryEntry{ID: 2, Kind: jhlog.DictOwner, Value: "FeedImages"}},
		{Type: jhlog.EventDictionary, Dictionary: &jhlog.DictionaryEntry{ID: 3, Kind: jhlog.DictGeneric, Value: "Feed scroll"}},
		{Type: jhlog.EventDictionary, Dictionary: &jhlog.DictionaryEntry{ID: 4, Kind: jhlog.DictMethod, Value: "Landroid/graphics/BitmapFactory;->decodeStream(Ljava/io/InputStream;)Landroid/graphics/Bitmap;"}},
		{Type: jhlog.EventAgent, TimeUS: 900_000, Agent: &jhlog.AgentEvent{SemanticType: jhlog.AgentStatus, SchemaVersion: 1, ProducerSequence: 1, Payload0: 2, Payload2: 0xCAFE, Payload3: 128 * 1024}},
		{Type: jhlog.EventAgent, TimeUS: 910_000, Agent: &jhlog.AgentEvent{SemanticType: jhlog.AgentCapability, SchemaVersion: 1, ProducerSequence: 2, Payload0: 0x3f, Payload1: 0x3f, Payload2: 0x3f, Payload3: 0x3f}},
		{Type: jhlog.EventAgent, TimeUS: 920_000, Attribution: context, Agent: &jhlog.AgentEvent{SemanticType: jhlog.AgentCorrelationLink, SchemaVersion: 1, ContextToken: 77, EventFlags: jhlog.AgentFlagContextDefinition, Payload0: 77}},
		{Type: jhlog.EventAgent, TimeUS: 930_000, Agent: &jhlog.AgentEvent{SemanticType: jhlog.AgentMethodDefinition, SchemaVersion: 1, Payload0: 44, MethodRef: jhlog.LocalSymbol(4)}},
		{Type: jhlog.EventAgent, TimeUS: 940_000, Agent: &jhlog.AgentEvent{SemanticType: jhlog.AgentStackDefinition, SchemaVersion: 1, ProducerSequence: 3, ContextToken: 77, Payload0: 0xB17, Payload1: 44, Payload3: 1 << 32}},
		{Type: jhlog.EventAgent, TimeUS: 950_000, Agent: &jhlog.AgentEvent{SemanticType: jhlog.AgentStatus, SchemaVersion: 1, Payload0: 0x100, Payload1: 2, Payload2: 0xCAFE}},
		{Type: jhlog.EventAgent, TimeUS: 1_000_000, Agent: &jhlog.AgentEvent{SemanticType: jhlog.AgentClockSync, SchemaVersion: 1, Payload0: 1_000_000_000, Payload1: 10_000}},
		{Type: jhlog.EventAgent, TimeUS: 1_150_000, Agent: &jhlog.AgentEvent{SemanticType: jhlog.AgentGCInterval, SchemaVersion: 1, ProducerSequence: 4, Payload0: 1_050_000_000, Payload1: 100_000_000}},
		{Type: jhlog.EventStall, TimeMS: 1_200, Attribution: context, Stall: &jhlog.StallEvent{DurationMS: 200}},
		{Type: jhlog.EventUIWindow, TimeMS: 1_200, Attribution: context, UIWindow: &jhlog.UIWindowEvent{WindowMS: 200, FrameCount: 12, JankCount: 7}},
		{Type: jhlog.EventAgent, TimeUS: 1_200_000, Agent: &jhlog.AgentEvent{SemanticType: jhlog.AgentThreadStackSample, SchemaVersion: 1, ProducerSequence: 5, ThreadToken: 11, ContextToken: 77, Payload0: 0xB17, Payload2: uint64(1) | uint64(1)<<32}},
		{Type: jhlog.EventAgent, TimeUS: 1_210_000, Agent: &jhlog.AgentEvent{SemanticType: jhlog.AgentQualitySnapshot, SchemaVersion: 1, ProducerSequence: 6, Payload0: 8}},
	}
	if !dropGCSequence {
		return events
	}
	filtered := make([]jhlog.Event, 0, len(events)-1)
	for _, event := range events {
		if event.Agent != nil && event.Agent.ProducerSequence == 4 {
			continue
		}
		filtered = append(filtered, event)
	}
	return filtered
}

func agentEvent(source string, timeUS uint64, semantic jhlog.AgentSemanticType, sequence, startNS, durationNS uint64) jhlog.Event {
	return jhlog.Event{Type: jhlog.EventAgent, Source: source, TimeUS: timeUS, TimeMS: timeUS / 1_000,
		Agent: &jhlog.AgentEvent{SemanticType: semantic, SchemaVersion: 1, ProducerSequence: sequence, ThreadToken: 7, Payload0: startNS, Payload1: durationNS}}
}
