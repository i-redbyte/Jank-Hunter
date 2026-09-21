package jhlog

import (
	"encoding/binary"
	"fmt"
	"path/filepath"
	"testing"
)

func TestSessionHTTPCollectionStateRoundTrip(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		header := DefaultSegmentHeader()
		header.RequiredFeatures |= 1 << 30
		path := filepath.Join(t.TempDir(), "http-state.jhlog")
		f, w, err := CreateWithHeader(path, header)
		if err != nil {
			t.Fatal(err)
		}
		flags := uint64(0)
		if enabled {
			flags = 1 << 11
		}
		if err := w.WriteEvent(Event{Type: EventSession, Session: &SessionEvent{CollectorFlags: flags}}); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
		var sessions int
		result, err := StreamFileWithResult(path, func(event Event, _ map[uint64]string) error {
			if event.Session != nil {
				sessions++
				if event.Session.CollectorFlags != flags {
					t.Errorf("enabled=%v: flags=%x", enabled, event.Session.CollectorFlags)
				}
			}
			return nil
		})
		if err != nil || sessions != 1 || result.Header.RequiredFeatures&(1<<30) == 0 {
			t.Fatalf("state provenance lost: %v sessions%d header%x", err, sessions, result.Header.RequiredFeatures)
		}
	}
}

func TestSessionCannotClaimHTTPStateWithoutFeature(t *testing.T) {
	header := DefaultSegmentHeader()
	header.RequiredFeatures &^= 1 << 30
	f, w, err := CreateWithHeader(filepath.Join(t.TempDir(), "legacy.jhlog"), header)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := w.WriteEvent(Event{Type: EventSession, Session: &SessionEvent{CollectorFlags: 1 << 11}}); err == nil {
		t.Fatal("HTTP state accepted without required feature")
	}
}

func TestLegacySessionDecoderRejectsHTTPFlagWithoutFeature(t *testing.T) {
	payload := binary.AppendUvarint(make([]byte, 13), uint64(CollectorHTTP))
	event := Event{Type: EventSession}
	state := segmentDecodeState{legacyHTTPCollectionState: true}
	if err := decodeCorePayload(&recordReader{data: payload}, &event, "", "", &state, make([]uint64, 16)); err == nil {
		t.Fatal("legacy decoder accepted new HTTP declaration")
	}
}

func TestARTSessionHTTPStateAndCollectionWindow(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		path := fmt.Sprintf("../../../wire/testdata/http-state-%t-5.1.0.jhlog", enabled)
		sessions := 0
		var sessionMS uint64
		r, err := StreamFileWithResult(path, func(e Event, _ map[uint64]string) error {
			if e.Session != nil {
				sessions++
				sessionMS = e.TimeUS / 1000
				if (e.Session.CollectorFlags&uint64(CollectorHTTP) != 0) != enabled {
					t.Errorf("enabled=%v flags=%x", enabled, e.Session.CollectorFlags)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if sessions != 1 || r.Header.RequiredFeatures&FeatureHTTPCollectionState == 0 || r.Status != SegmentStatusClosedClean || r.LatestQuality == nil {
			t.Fatalf("incomplete ART fixture: %+v", r)
		}
		start, end := r.LatestQuality.Counters[QualityCollectionWindowStartElapsedMS], r.LatestQuality.Counters[QualityCollectionWindowEndElapsedMS]
		if start < sessionMS || end <= start || end > r.LatestQuality.CapturedElapsedUS/1000 {
			t.Fatalf("invalid window: session=%d start=%d end=%d final=%d", sessionMS, start, end, r.LatestQuality.CapturedElapsedUS/1000)
		}
	}
}

func TestCollectionWindowCannotClaimLegacyFeatureContract(t *testing.T) {
	header := DefaultSegmentHeader()
	header.RequiredFeatures &^= FeatureHTTPCollectionState
	f, w, err := CreateWithHeader(filepath.Join(t.TempDir(), "legacy-window.jhlog"), header)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w.SetQualitySnapshot(QualitySnapshot{Counters: map[uint64]uint64{QualityCollectionWindowStartElapsedMS: 100}})
	if err := w.Close(); err == nil {
		t.Error("new collection-window metadata written under legacy required features")
	}
	var payload []byte
	for _, v := range []uint64{1, 0, 1, QualityCollectionWindowStartElapsedMS, 100} {
		payload = binary.AppendUvarint(payload, v)
	}
	event := Event{Type: EventQualitySnapshot}
	state := segmentDecodeState{legacyHTTPCollectionState: true, qualityCounters: map[uint64]uint64{}}
	if err := decodeQualityPayload(&recordReader{data: payload}, &event, &state, make([]uint64, 16)); err == nil {
		t.Error("legacy decoder accepted required-feature window metadata")
	}
}
