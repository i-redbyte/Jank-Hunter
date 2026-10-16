package jhlog

import (
	"bytes"
	"fmt"
	"testing"
)

func TestCausalLossDistributionIsBoundedAndPreservesKindPhaseThreadCategory(t *testing.T) {
	for kind := uint64(1); kind <= 10; kind++ {
		for main := uint64(0); main <= 1; main++ {
			for phase := uint64(1); phase <= 15; phase++ {
				id := 0x2200 + (kind-1)*32 + main*16 + phase
				want := fmt.Sprintf("causal_kind_%d_thread_%d_phase_%d_pre_admission_loss_total", kind, main, phase)
				if !IsKnownQualityCounter(id) || QualityCounterName(id) != want {
					t.Fatalf("invalid loss projection: %x", id)
				}
			}
		}
	}
	for _, id := range []uint64{0x2200, 0x2210, 0x2340, 0x23ff} {
		if IsKnownQualityCounter(id) {
			t.Fatalf("invalid category accepted: %x", id)
		}
	}
}

func TestQuality8802WireDeltasPreserveCausalLossEvidence(t *testing.T) {
	state := decodeSegmentState(nil)
	var first *QualitySnapshot
	for index, delta := range []byte{7, 2} {
		payload := []byte{1, 0, 1, 0xe2, 0x44, delta}
		event := Event{Type: EventQualitySnapshot}
		reader := &recordReader{data: bytes.Clone(payload)}
		if err := decodeEventPayload(reader, &event, "", "", nil, state); err != nil {
			t.Fatal(err)
		}
		if reader.Len() != 0 || event.Quality.Sequence != uint64(index+1) {
			t.Fatalf("invalid quality frontier: remaining=%d quality=%+v", reader.Len(), event.Quality)
		}
		if index == 0 {
			first = event.Quality
		}
		want := uint64(7)
		if index == 1 {
			want = 9
		}
		if got := event.Quality.Counters[8802]; got != want {
			t.Fatalf("counter 8802=%d, want %d", got, want)
		}
	}
	if first.Counters[8802] != 7 {
		t.Fatal("later delta mutated the earlier quality snapshot")
	}
	if name := QualityCounterName(8802); name != "causal_kind_4_thread_0_phase_2_pre_admission_loss_total" {
		t.Fatalf("quality evidence was renamed or ignored: %q", name)
	}
}
