package jhlog

import (
	"bytes"
	"path/filepath"
	"testing"
)

func TestHTTPFirstByteWireRejectsContradictoryKnownness(t *testing.T) {
	for _, test := range []struct {
		name  string
		flags uint64
		value uint64
	}{
		{"known without semantics", 1 << 25, 0},
		{"unknown with a value", 1 << 24, 50},
	} {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			err := encodeEventPayloadWithState(&output, Event{Type: EventHTTP, Flags: test.flags,
				HTTP: &HTTPEvent{DurationMS: 100, TTFBMS: test.value}}, nil, nil)
			if err == nil {
				t.Fatal("contradictory first-byte measurement was accepted")
			}
		})
	}
}

func TestHTTPFirstByteScalarAndMicroPagePreserveKnownZeroAndUnknown(t *testing.T) {
	for _, count := range []int{1, 128} {
		events := make([]Event, count)
		for index := range events {
			flags := uint64(FlagHTTPTTFBObserved)
			if index%2 == 0 {
				flags |= uint64(FlagHTTPTTFBKnown)
			}
			events[index] = Event{Type: EventHTTP, TimeMS: uint64(index + 1), Flags: flags,
				HTTP: &HTTPEvent{DurationMS: 100, StatusCode: 200}}
		}
		path := filepath.Join(t.TempDir(), "first-byte.jhlog")
		writeClosedEvents(t, path, events)
		log, err := readLog(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(log.Events) != count {
			t.Fatalf("events=%d, want %d", len(log.Events), count)
		}
		for index, event := range log.Events {
			if event.HTTP.TTFBMS != 0 || HasObservedHTTPFirstByte(event.Flags) != (index%2 == 0) {
				t.Fatalf("known zero changed at %d: %+v", index, event)
			}
		}
	}
}

func TestAndroidHTTPFirstByteFixturePreservesTransportEvidence(t *testing.T) {
	log, err := readLog("../../../wire/testdata/http-first-byte-5.1.0.jhlog")
	if err != nil {
		t.Fatal(err)
	}
	if log.Result.Header.RequiredFeatures&FeatureHTTPFirstByte == 0 {
		t.Fatal("missing mandatory feature")
	}
	var values []uint64
	unknown := 0
	for _, event := range log.Events {
		if event.HTTP == nil {
			continue
		}
		if HasObservedHTTPFirstByte(event.Flags) {
			values = append(values, event.HTTP.TTFBMS)
		} else {
			unknown++
		}
	}
	if len(values) != 2 || values[0] != 500 || values[1] != 0 || unknown != 1 {
		t.Fatalf("ART first-byte values=%v unknown=%d", values, unknown)
	}
}
