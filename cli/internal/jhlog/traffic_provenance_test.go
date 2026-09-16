package jhlog

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestContextTrafficProvenanceRoundTrip(t *testing.T) {
	for flags := 0; flags < 4; flags++ {
		t.Run(fmt.Sprint(flags), func(t *testing.T) {
			var payload ContextEvent
			encoded := fmt.Sprintf(`{"rx_bytes":0,"tx_bytes":123,"traffic_uid_plus_one":10001,"traffic_known_flags":%d}`, flags)
			if err := json.Unmarshal([]byte(encoded), &payload); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "traffic.jhlog")
			file, writer, err := Create(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := writer.WriteEvent(Event{Type: EventContext, TimeMS: 1000, Context: &payload}); err != nil {
				t.Fatal(err)
			}
			if err := writer.CloseWithReason(SegmentEndNormal); err != nil {
				t.Fatal(err)
			}
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}
			header, err := ReadSessionHeader(path)
			if err != nil {
				t.Fatal(err)
			}
			if header.RequiredFeatures&(1<<29) == 0 {
				t.Error("new provenance requires feature29 so readers cannot silently ignore it")
			}
			seen := false
			if err := StreamFile(path, func(event Event, _ map[uint64]string) error {
				if event.Context != nil {
					seen = true
					raw, _ := json.Marshal(event.Context)
					var got map[string]any
					if err := json.Unmarshal(raw, &got); err != nil {
						return err
					}
					if got["traffic_uid_plus_one"] != float64(10001) {
						t.Errorf("UID provenance lost: %s", raw)
					}
					actual := float64(0)
					if value, ok := got["traffic_known_flags"].(float64); ok {
						actual = value
					}
					if actual != float64(flags) {
						t.Errorf("known zero/unsupported collapsed: %s", raw)
					}
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if !seen {
				t.Fatal("context missing")
			}
		})
	}
}

func TestContextTrafficProvenanceCannotBeSilentlyWrittenAsLegacy(t *testing.T) {
	var payload ContextEvent
	if err := json.Unmarshal([]byte(`{"traffic_uid_plus_one":10001,"traffic_known_flags":3}`), &payload); err != nil {
		t.Fatal(err)
	}
	header := DefaultSegmentHeader()
	header.RequiredFeatures &^= 1 << 29
	file, writer, err := CreateWithHeader(filepath.Join(t.TempDir(), "legacy.jhlog"), header)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := writer.WriteEvent(Event{Type: EventContext, TimeMS: 1, Context: &payload}); err == nil {
		t.Fatal("writer silently discards requested provenance under a legacy feature set")
	}
}

func TestAndroidUIDTrafficFixturePreservesCollectorAndManualProvenance(t *testing.T) {
	log, err := readLog("../../../wire/testdata/uid-traffic-5.1.0.jhlog")
	if err != nil {
		t.Fatal(err)
	}
	if log.Result.Header.RequiredFeatures&FeatureUIDTraffic == 0 || log.Result.Status != SegmentStatusClosedClean {
		t.Fatal("missing provenance or incomplete ART log")
	}
	var contexts []*ContextEvent
	for _, event := range log.Events {
		if event.Context != nil {
			contexts = append(contexts, event.Context)
		}
	}
	if len(contexts) != 7 {
		t.Fatalf("contexts=%d want7", len(contexts))
	}
	uid := contexts[0].TrafficUIDPlusOne
	if uid == 0 || contexts[1].TrafficUIDPlusOne != uid {
		t.Fatal("real SystemContextSampler lost process UID")
	}
	for flags := uint64(0); flags < 4; flags++ {
		c := contexts[2+flags]
		if c.TrafficUIDPlusOne != uid || c.TrafficKnownFlags != uint8(flags) || c.RxBytes != 0 || c.TxBytes != 0 {
			t.Fatalf("flags=%d got=%+v", flags, c)
		}
	}
	if contexts[6].TrafficUIDPlusOne != 0 || contexts[6].TrafficKnownFlags != 0 {
		t.Fatal("manual telemetry invented UID provenance")
	}
}

func TestTrafficWireRejectsInvalidProvenance(t *testing.T) {
	for _, payload := range []ContextEvent{{TrafficUIDPlusOne: 1<<31 + 1}, {TrafficUIDPlusOne: 1, TrafficKnownFlags: 4}, {TrafficKnownFlags: 1}} {
		file, writer, err := Create(filepath.Join(t.TempDir(), "invalid.jhlog"))
		if err != nil {
			t.Fatal(err)
		}
		err = writer.WriteEvent(Event{Type: EventContext, TimeMS: 1, Context: &payload})
		file.Close()
		if err == nil {
			t.Fatalf("invalid provenance accepted: %+v", payload)
		}
	}
}

func TestUIDTrafficARTBenchmarkAccounting(t *testing.T) {
	root := os.Getenv("JANKHUNTER_UID_TRAFFIC_ART_EVIDENCE")
	if root == "" {
		t.Skip("ART benchmark artifacts not requested")
	}
	paths, err := filepath.Glob(filepath.Join(root, "uid-traffic-gm5_*-*.jhlog"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 12 {
		t.Fatalf("ART logs=%d want12", len(paths))
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			contexts := 0
			var last uint64
			modern := strings.Contains(path, "_after_")
			result, err := StreamFileWithResult(path, func(e Event, _ map[uint64]string) error {
				if e.Context != nil {
					c := e.Context
					if c.RxBytes != uint64(contexts) || c.TxBytes != uint64(contexts) {
						t.Errorf("counter sequence at%d=%d/%d", contexts, c.RxBytes, c.TxBytes)
					}
					last = c.RxBytes
					contexts++
					if modern && (c.TrafficUIDPlusOne == 0 || c.TrafficKnownFlags != 3) {
						t.Errorf("provenance lost: %+v", c)
					}
					if !modern && (c.TrafficUIDPlusOne != 0 || c.TrafficKnownFlags != 0) {
						t.Error("legacy invented knownness")
					}
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if contexts != 25000 || last != 24999 || result.Status != SegmentStatusClosedClean {
				t.Fatalf("contexts=%d last=%d status=%s", contexts, last, result.Status)
			}
		})
	}
}

func TestTrafficDecoderRejectsInvalidValuesBeforeNarrowing(t *testing.T) {
	for _, values := range [][2]uint64{{1 << 32, 0}, {1, 256}, {0, 1}, {1<<31 + 1, 0}} {
		payload := make([]byte, 10)
		payload = binary.AppendUvarint(payload, values[0])
		payload = binary.AppendUvarint(payload, values[1])
		event := Event{Type: EventContext}
		if err := decodeCorePayload(&recordReader{data: payload}, &event, "", "", &segmentDecodeState{}, make([]uint64, 16)); err == nil {
			t.Fatalf("invalid provenance wrapped into valid fields: %v", values)
		}
	}
}
