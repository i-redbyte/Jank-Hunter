package analyze

import "testing"

func TestClosestStackPrefersContentionRelatedSample(t *testing.T) {
	aggregator := newAgentAggregator()
	aggregator.sources["log"] = struct{}{}
	aggregator.stacks[0xA] = &agentStackState{
		methodIDs: []uint64{1},
		context:   agentContext{owner: "FeedImages", flow: "FeedImages.load"},
	}
	aggregator.stacks[0xB] = &agentStackState{
		methodIDs: []uint64{2},
		context:   agentContext{owner: "FeedImages", flow: "FeedImages.load"},
	}
	aggregator.methods[1] = "io.jankhunter.artti.internal.ArtTiNativeBridge.nativeCaptureStack(java.lang.Thread, int, long, long): int"
	aggregator.methods[2] = "android.graphics.BitmapFactory.decodeStream(java.io.InputStream): android.graphics.Bitmap"
	symptom := agentSymptom{
		source:  "log",
		startNS: 100,
		endNS:   200,
		context: agentContext{owner: "FeedImages", flow: "FeedImages.load"},
	}
	aggregator.stackSamples = []agentStackSample{
		{timeNS: 150, fingerprint: 0xA, sequence: 10, relatedSequence: 0, trigger: 1, source: "log"},
		{timeNS: 160, fingerprint: 0xB, sequence: 11, relatedSequence: 42, trigger: agentStackTriggerLongContention, source: "log"},
	}
	sample, ok := aggregator.closestStack(symptom, 42)
	if !ok || sample.fingerprint != 0xB {
		t.Fatalf("expected contention-related stack, got ok=%v sample=%+v", ok, sample)
	}
}

func TestClosestStackPrefersExplicitDecodeCapture(t *testing.T) {
	aggregator := newAgentAggregator()
	aggregator.sources["log"] = struct{}{}
	aggregator.stacks[0xA] = &agentStackState{
		methodIDs: []uint64{1},
		context:   agentContext{owner: "FeedImages", flow: "FeedImages.load"},
	}
	aggregator.stacks[0xB] = &agentStackState{
		methodIDs: []uint64{2},
		context:   agentContext{owner: "FeedImages", flow: "FeedImages.load"},
	}
	aggregator.methods[1] = "kotlinx.coroutines.DelayKt.delay(long, kotlin.coroutines.Continuation): java.lang.Object"
	aggregator.methods[2] = "android.graphics.BitmapFactory.decodeStream(java.io.InputStream): android.graphics.Bitmap"
	symptom := agentSymptom{
		source:  "log",
		startNS: 100,
		endNS:   200,
		context: agentContext{owner: "FeedImages", flow: "FeedImages.load"},
	}
	aggregator.stackSamples = []agentStackSample{
		{timeNS: 160, fingerprint: 0xA, sequence: 10, relatedSequence: 42, trigger: agentStackTriggerLongContention, source: "log"},
		{timeNS: 161, fingerprint: 0xB, sequence: 11, relatedSequence: 0, trigger: agentStackTriggerExplicitEvidence, source: "log"},
	}
	sample, ok := aggregator.closestStack(symptom, 42)
	if !ok || sample.fingerprint != 0xB {
		t.Fatalf("expected explicit decode stack, got ok=%v sample=%+v", ok, sample)
	}
}
