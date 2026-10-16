package jhlog

import (
	"bytes"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestWorkRegistryRoundTripKeepsScheduleStopAndSafeMetadataDistinct(t *testing.T) {
	period, flex := uint64(900000), uint64(300000)
	earliest, stop := int64(1234567), int64(-256)
	size := uint64(16)
	packet := &WorkRegistryEvent{Version: 1, SnapshotID: 2, QueryStartedMS: 10, ObservedMS: 15, IntervalMS: 30000, PacketCount: 1, TotalObserved: 1, RetainedJobs: 1,
		Jobs: []WorkRegistryJob{{UUID: "00000000-0000-0000-0000-000000000001", InstanceID: 42, State: 0, RunAttempt: 2, Generation: 3, InitialDelayMS: 1000,
			PeriodMS: &period, FlexMS: &flex, EarliestEligibleUnixMS: &earliest, StopReason: &stop, NetworkType: "CONNECTED", Constraints: 3, Tags: []string{"sync"},
			Progress: []WorkDataField{{Key: "token", Type: "string", PayloadBytes: &size}}}}}
	path := filepath.Join(t.TempDir(), "registry.jhlog")
	writeClosedEvents(t, path, []Event{{Type: EventWorkRegistry, DiagnosticEvent: &DiagnosticEvent{WorkRegistry: packet}}})
	log, err := readLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(log.Events) != 1 || !reflect.DeepEqual(log.Events[0].WorkRegistry, packet) {
		t.Fatalf("registry roundtrip: %+v", log.Events)
	}
}

func TestWorkRegistryRejectsIncompletePacketClaims(t *testing.T) {
	valid := WorkRegistryEvent{Version: 1, SnapshotID: 1, IntervalMS: 30000, PacketCount: 1}
	cases := []WorkRegistryEvent{valid, valid, valid}
	cases[0].PacketIndex = 1
	cases[1].RetainedJobs = 1
	cases[2].Unavailable = 1
	cases[2].TotalObserved = 10
	for _, packet := range cases {
		var out bytes.Buffer
		if err := (eventPayloadEncoder{writer: &out}).encodeWorkRegistry(&packet); err == nil {
			t.Fatalf("invalid coverage accepted: %+v", packet)
		}
	}
}

func TestWorkRegistryMinifiedAndroidFixture(t *testing.T) {
	log, err := readLog("../../../wire/testdata/work-registry-r8-5.1.0.jhlog")
	if err != nil {
		t.Fatal(err)
	}
	packets, blocked, delayed := 0, false, false
	for _, event := range log.Events {
		if event.DiagnosticEvent == nil || event.WorkRegistry == nil {
			continue
		}
		packets++
		for _, row := range event.WorkRegistry.Jobs {
			blocked = blocked || row.State == 4
			delayed = delayed || row.InitialDelayMS >= 3600000
		}
	}
	if packets == 0 || !blocked || !delayed || !log.Result.Sealed {
		t.Fatalf("packets=%d blocked=%v delayed=%v sealed=%v", packets, blocked, delayed, log.Result.Sealed)
	}
}

func TestWorkRegistryDecoderRejectsReservedPresenceBits(t *testing.T) {
	var out bytes.Buffer
	encoder := eventPayloadEncoder{writer: &out}
	if err := encoder.threadString("00000000-0000-0000-0000-000000000001"); err != nil {
		t.Fatal(err)
	}
	if err := encoder.writeValues(1, 0, 0, 0, 0, 8); err != nil {
		t.Fatal(err)
	}
	_, err := decodeRegistryJob(&recordReader{data: out.Bytes()})
	if err == nil || !strings.Contains(err.Error(), "presence bits") {
		t.Fatalf("reserved bit: %v", err)
	}
}
