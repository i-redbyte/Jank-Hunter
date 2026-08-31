package jhlog

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestLogGrowthControlRecordRoundTripsWithoutInflatingEventTotals(t *testing.T) {
	path := filepath.Join(t.TempDir(), "growth.jhlog")
	closer, writer, err := Create(path)
	if err != nil {
		t.Fatal(err)
	}
	live := testGrowthLivePayload(7, 0x11, 0x22, 1_000, 2_000, true)
	if err := writer.WriteEvent(Event{
		Type: EventLogGrowth,
		LogGrowth: &LogGrowthRecord{
			Kind: LogGrowthLive,
			Raw:  live,
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := closer.Close(); err != nil {
		t.Fatal(err)
	}

	log, err := readLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if log.Result.Events != 0 || log.Result.DataRecords != 0 {
		t.Fatalf("growth metadata inflated data totals: %+v", log.Result)
	}
	if log.Result.ControlRecords != 3 {
		t.Fatalf("control records = %d, want growth + quality + end", log.Result.ControlRecords)
	}
	if log.Result.LogGrowth == nil || log.Result.LogGrowth.Live == nil {
		t.Fatalf("log growth was not decoded: %+v", log.Result.LogGrowth)
	}
	if got := log.Result.LogGrowth.Live; got.SessionID != "0000000000000033" ||
		got.GeneratedBytes != 2_000 || !got.Completed {
		t.Fatalf("decoded live growth = %+v", got)
	}
}

func TestLogGrowthKeepsHistoryAndLiveGenerationsIndependent(t *testing.T) {
	history := &LogGrowthProjection{
		Generation: 4,
		HasHistory: true,
		Sessions:   []LogGrowthSession{{SessionID: "completed", Completed: true}},
	}
	newestLive := &LogGrowthSession{SessionID: "active", GeneratedBytes: 200}
	staleLive := &LogGrowthSession{SessionID: "active", GeneratedBytes: 100}
	result := StreamResult{}

	applyLogGrowthRecord(&result, &LogGrowthRecord{Generation: 4, Projection: history})
	applyLogGrowthRecord(&result, &LogGrowthRecord{Generation: 100, Live: newestLive})
	applyLogGrowthRecord(&result, &LogGrowthRecord{Generation: 99, Live: staleLive})

	if result.LogGrowth == nil || result.LogGrowth.Generation != 4 || result.LogGrowth.LiveGeneration != 100 {
		t.Fatalf("independent growth generations were conflated: %+v", result.LogGrowth)
	}
	if len(result.LogGrowth.Sessions) != 1 || result.LogGrowth.Live != newestLive {
		t.Fatalf("history or newest live snapshot was lost: %+v", result.LogGrowth)
	}

	newerHistory := &LogGrowthProjection{
		Generation: 5,
		HasHistory: true,
		Sessions:   []LogGrowthSession{{SessionID: "newer", Completed: true}},
	}
	applyLogGrowthRecord(&result, &LogGrowthRecord{Generation: 5, Projection: newerHistory})
	if result.LogGrowth != newerHistory || result.LogGrowth.Live != newestLive || result.LogGrowth.LiveGeneration != 100 {
		t.Fatalf("newer history did not preserve the newest live snapshot: %+v", result.LogGrowth)
	}
}

func testGrowthLivePayload(generation, high, low, retained, generated uint64, completed bool) []byte {
	raw := make([]byte, growthLiveBytes)
	copy(raw, growthLiveMagic)
	raw[4] = 2
	raw[5] = 0
	binary.LittleEndian.PutUint16(raw[6:8], growthLiveBytes)
	binary.LittleEndian.PutUint64(raw[8:16], generation)
	binary.LittleEndian.PutUint64(raw[16:24], high)
	binary.LittleEndian.PutUint64(raw[24:32], low)
	binary.LittleEndian.PutUint32(raw[32:36], 20260814)
	if completed {
		binary.LittleEndian.PutUint32(raw[36:40], 1)
	}
	binary.LittleEndian.PutUint64(raw[40:48], 1_000)
	binary.LittleEndian.PutUint64(raw[48:56], 2_000)
	binary.LittleEndian.PutUint64(raw[56:64], 50*1024*1024)
	binary.LittleEndian.PutUint64(raw[64:72], retained)
	binary.LittleEndian.PutUint64(raw[72:80], generated)
	binary.LittleEndian.PutUint32(raw[len(raw)-4:], crc32.ChecksumIEEE(raw[:len(raw)-4]))
	return raw
}

func TestLogGrowthRejectsPreBudgetSemanticsSchema(t *testing.T) {
	raw := testGrowthLivePayload(1, 2, 3, 4, 5, false)
	raw[4] = 1
	binary.LittleEndian.PutUint32(raw[len(raw)-4:], crc32.ChecksumIEEE(raw[:len(raw)-4]))
	if _, _, err := decodeGrowthLive(raw); err == nil {
		t.Fatal("growth schema 1 must not be interpreted with the total-budget semantics")
	}
}

func TestWriteSampleStreamsCommittedVersionThree(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.jhlog")
	if err := WriteSample(path); err != nil {
		t.Fatalf("WriteSample() error = %v", err)
	}

	log, err := readLog(path)
	if err != nil {
		t.Fatalf("readLog() error = %v", err)
	}
	if log.Result.Status != SegmentStatusClosedClean {
		t.Fatalf("status = %q, want %q", log.Result.Status, SegmentStatusClosedClean)
	}
	if !log.Result.Sealed {
		t.Fatal("sample 3.0.0 log is closed but not FINAL-sealed")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if log.Result.InputBytes != uint64(info.Size()) || log.Result.LatestDataEventUnixMS != 30_100 {
		t.Fatalf("physical/freshness metadata = bytes %d, latest %d", log.Result.InputBytes, log.Result.LatestDataEventUnixMS)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	wantDigest := sha256.Sum256(raw)
	if !bytes.Equal(log.Result.SegmentDigest, wantDigest[:]) {
		t.Fatalf("segment digest = %x, want %x", log.Result.SegmentDigest, wantDigest)
	}
	if log.Result.LatestQuality == nil || log.Result.SegmentEnd == nil {
		t.Fatalf("terminal metadata missing: %+v", log.Result)
	}
	if len(log.Events) == 0 || log.Dict[20] != "GET /feed" {
		t.Fatalf("sample did not stream: events=%d dict[20]=%q", len(log.Events), log.Dict[20])
	}
	if log.Events[len(log.Events)-1].TimeUS == 0 {
		t.Fatalf("expected producer timestamps")
	}
	if log.Result.Events != uint64(len(log.Events)) {
		t.Fatalf("result events = %d, callback events = %d", log.Result.Events, len(log.Events))
	}
	if log.Result.Events != log.Result.DataRecords {
		t.Fatalf("semantic events = %d, data records = %d", log.Result.Events, log.Result.DataRecords)
	}
	if log.Result.TotalRecords != log.Result.DataRecords+log.Result.DictionaryRecords+log.Result.ControlRecords {
		t.Fatalf("record classes do not add up: %+v", log.Result)
	}
	if log.Result.ControlRecords != 2 {
		t.Fatalf("control records = %d, want FINAL quality + segment end", log.Result.ControlRecords)
	}
	if log.Result.SegmentEnd.TotalEventRecords != log.Result.DataRecords ||
		log.Result.SegmentEnd.TotalDictionaryRecords != log.Result.DictionaryRecords {
		t.Fatalf("segment totals do not match decoded classes: end=%+v result=%+v", log.Result.SegmentEnd, log.Result)
	}
	if got := log.Result.LatestQuality.Counters[QualityCommittedChunkTotal]; got != uint64(log.Result.CommittedChunks) {
		t.Fatalf("committed quality = %d, chunks = %d", got, log.Result.CommittedChunks)
	}
}

func TestFormatMagicAndFeatureBitsGolden(t *testing.T) {
	want := []byte{'J', 'H', 'L', 'O', 'G', '\r', '\n', 0x81, 3, 0, 0}
	if !bytes.Equal(Magic, want) {
		t.Fatalf("magic = %v, want %v", Magic, want)
	}
	if RequiredFeatures != 0x3ffff || OptionalFeatures != 0x01 {
		t.Fatalf("features = required 0x%x optional 0x%x", RequiredFeatures, OptionalFeatures)
	}
}

func TestDatabaseStatementFingerprintMatchesWireContract(t *testing.T) {
	if got := DatabaseStatementFingerprint("hello"); got != 0xa430d84680aabd0b {
		t.Fatalf("fingerprint = 0x%x", got)
	}
	if got := DatabaseStatementFingerprint(""); got != 0 {
		t.Fatalf("empty fingerprint = 0x%x", got)
	}
}

func TestDeclaredProcessRosterRoundTripsInHeader(t *testing.T) {
	header := DefaultSegmentHeader()
	header.ProcessName = "main"
	header.TimezoneOffsetMinutes = 180
	header.ExpectedProcessCount = 2
	header.ExpectedProcessFingerprint = ProcessRosterFingerprint([]string{"main", "remote"})
	header.ProcessRosterDeclarationComplete = true
	raw, normalized, err := encodeFileHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeHeaderPayload(raw[magicSize+8:])
	if err != nil {
		t.Fatal(err)
	}
	if decoded.ExpectedProcessCount != 2 || decoded.TimezoneOffsetMinutes != 180 || !decoded.ProcessRosterDeclarationComplete ||
		!bytes.Equal(decoded.ExpectedProcessFingerprint, normalized.ExpectedProcessFingerprint) {
		t.Fatalf("process roster did not round-trip: decoded=%+v normalized=%+v", decoded, normalized)
	}
}

func TestOperationLifecycleRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operation-lifecycle.jhlog")
	attributes := []OperationAttribute{
		{KeyRef: LocalSymbol(2), ValueRef: LocalSymbol(3)},
		{KeyRef: LocalSymbol(4), ValueRef: LocalSymbol(5)},
	}
	context := AttributionContext{Present: true, Screen: LocalSymbol(6), OperationID: 41}
	events := []Event{
		{
			Type: EventOperation, TimeUS: 1_000_000, Attribution: context,
			Operation: &OperationEvent{
				NameRef: LocalSymbol(1), ID: 42, ParentID: 41, Phase: OperationPhaseStarted,
				Kind: OperationKindUser, BudgetUS: 2_000_000, Attributes: attributes,
			},
		},
		{
			Type: EventOperation, TimeUS: 2_250_000, Attribution: context,
			Operation: &OperationEvent{
				NameRef: LocalSymbol(1), ID: 42, ParentID: 41, Phase: OperationPhaseFinished,
				Kind: OperationKindUser, Outcome: OperationOutcomeSuccess, DurationUS: 1_250_000,
				BudgetUS: 2_000_000, Attributes: attributes,
			},
		},
	}
	writeClosedEvents(t, path, events)
	log, err := readLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(log.Events) != len(events) {
		t.Fatalf("operation event count = %d, want %d", len(log.Events), len(events))
	}
	for index := range events {
		if log.Events[index].Attribution != context || !reflect.DeepEqual(log.Events[index].Operation, events[index].Operation) {
			t.Fatalf("operation event %d = %+v, want %+v", index, log.Events[index], events[index])
		}
	}
}

func TestOperationLifecycleValidation(t *testing.T) {
	valid := OperationEvent{
		NameRef: LocalSymbol(1), ID: 1, Phase: OperationPhaseStarted, Kind: OperationKindUser,
	}
	tests := []struct {
		name  string
		event OperationEvent
		want  string
	}{
		{name: "missing ID", event: OperationEvent{NameRef: LocalSymbol(1), Phase: OperationPhaseStarted, Kind: OperationKindUser}, want: "operation ID must be non-zero"},
		{name: "self parent", event: OperationEvent{NameRef: LocalSymbol(1), ID: 1, ParentID: 1, Phase: OperationPhaseStarted, Kind: OperationKindUser}, want: "own parent"},
		{name: "missing name", event: OperationEvent{ID: 1, Phase: OperationPhaseStarted, Kind: OperationKindUser}, want: "name reference"},
		{name: "unknown kind", event: OperationEvent{NameRef: LocalSymbol(1), ID: 1, Phase: OperationPhaseStarted}, want: "operation kind"},
		{name: "start with outcome", event: OperationEvent{NameRef: LocalSymbol(1), ID: 1, Phase: OperationPhaseStarted, Kind: OperationKindUser, Outcome: OperationOutcomeSuccess}, want: "cannot have outcome"},
		{name: "finish without outcome", event: OperationEvent{NameRef: LocalSymbol(1), ID: 1, Phase: OperationPhaseFinished, Kind: OperationKindUser}, want: "requires a supported outcome"},
		{name: "duplicate key", event: OperationEvent{NameRef: LocalSymbol(1), ID: 1, Phase: OperationPhaseStarted, Kind: OperationKindUser, Attributes: []OperationAttribute{{KeyRef: LocalSymbol(2), ValueRef: LocalSymbol(3)}, {KeyRef: LocalSymbol(2), ValueRef: LocalSymbol(4)}}}, want: "duplicates a key"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, _, err := encodeRecord(Event{Type: EventOperation, Operation: &test.event}, recordEncodeState{})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("encodeRecord() error = %v, want %q", err, test.want)
			}
		})
	}
	_, _, _, err := encodeRecord(Event{Type: EventOperation, Operation: &valid}, recordEncodeState{})
	if err != nil {
		t.Fatalf("valid operation rejected: %v", err)
	}
}

func TestVersionThreeHeaderRejectsUnparsedTrailingBytes(t *testing.T) {
	raw, _, err := encodeFileHeader(DefaultSegmentHeader())
	if err != nil {
		t.Fatal(err)
	}
	payload := append([]byte(nil), raw[magicSize+8:]...)
	payload = append(payload, 0)
	if _, err := decodeHeaderPayload(payload); err == nil || !strings.Contains(err.Error(), "trailing bytes") {
		t.Fatalf("trailing header payload error = %v", err)
	}
}

func TestDeclaredProcessRosterRejectsMissingFingerprint(t *testing.T) {
	header := DefaultSegmentHeader()
	header.ExpectedProcessCount = 1
	header.ExpectedProcessFingerprint = nil
	if _, _, err := encodeFileHeader(header); err == nil || !strings.Contains(err.Error(), "fingerprint") {
		t.Fatalf("missing process roster fingerprint error = %v", err)
	}
}

func TestVersionThreeRejectsMissingMandatoryWireFeatures(t *testing.T) {
	header := DefaultSegmentHeader()
	header.RequiredFeatures &^= FeatureSegmentDigestChain
	if _, _, err := encodeFileHeader(header); err == nil || !strings.Contains(err.Error(), "required feature contract") {
		t.Fatalf("missing digest-chain feature error = %v", err)
	}
}

func TestVersionThreeAcceptsOnlyCanonicalFeatureContracts(t *testing.T) {
	tests := []struct {
		name     string
		required uint64
		optional uint64
		wantErr  bool
	}{
		{name: "exact", required: RequiredFeatures, optional: OptionalFeatures},
		{name: "best effort", required: BestEffortFeatures, optional: OptionalFeatures},
		{name: "unknown required bit", required: RequiredFeatures | 1<<63, optional: OptionalFeatures, wantErr: true},
		{name: "missing required bit", required: RequiredFeatures &^ FeatureProcessRoster, optional: OptionalFeatures, wantErr: true},
		{name: "unknown optional bit", required: RequiredFeatures, optional: OptionalFeatures | 1<<63, wantErr: true},
		{name: "missing optional contract", required: RequiredFeatures, optional: 0, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			header := DefaultSegmentHeader()
			header.RequiredFeatures = test.required
			header.OptionalFeatures = test.optional
			_, _, err := encodeFileHeader(header)
			if (err != nil) != test.wantErr {
				t.Fatalf("encodeFileHeader() error = %v, wantErr = %t", err, test.wantErr)
			}
		})
	}
}

func TestSegmentDigestCanBeLinkedIntoSuccessorHeader(t *testing.T) {
	firstPath := filepath.Join(t.TempDir(), "first.jhlog")
	firstFile, firstWriter, err := Create(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := firstWriter.WriteEvent(Event{Type: EventCounter, Metric: &MetricEvent{MetricRef: LocalSymbol(1), Value: 7}}); err != nil {
		t.Fatal(err)
	}
	if err := firstWriter.CloseWithReason(SegmentEndRotation); err != nil {
		t.Fatal(err)
	}
	if err := firstFile.Close(); err != nil {
		t.Fatal(err)
	}
	predecessor := firstWriter.SegmentDigest()
	if len(predecessor) != segmentDigestSize {
		t.Fatalf("sealed predecessor digest has %d bytes", len(predecessor))
	}

	header := DefaultSegmentHeader()
	header.SegmentIndex = 1
	header.PreviousSegmentDigest = predecessor
	var second bytes.Buffer
	secondWriter, err := NewWriterWithHeader(&second, header)
	if err != nil {
		t.Fatal(err)
	}
	if err := secondWriter.CloseWithReason(SegmentEndShutdown); err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeHeaderPayload(second.Bytes()[magicSize+8 : firstChunkOffset(t, second.Bytes())])
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded.PreviousSegmentDigest, predecessor) {
		t.Fatalf("successor predecessor digest = %x, want %x", decoded.PreviousSegmentDigest, predecessor)
	}
}

func TestRuntimeCallColumnarBlockRoundTripsAsSemanticRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime-block.jhlog")
	closer, writer, err := Create(path)
	if err != nil {
		t.Fatal(err)
	}
	events := []Event{
		{Type: EventRuntimeCall, TimeUS: 10_000, Producer: ProducerMetadata{HasThread: true, ThreadID: 7}, Attribution: AttributionContext{
			Present: true, Screen: LocalSymbol(11), Owner: StableSymbol(0x101), OperationID: 41,
		}, RuntimeCall: &RuntimeCallEvent{
			CalleeRef: StableSymbol(0x201), Count: 3, TotalMS: 12, MaxMS: 7,
		}},
		{Type: EventRuntimeCall, TimeUS: 10_000, Producer: ProducerMetadata{HasThread: true, ThreadID: 7}, Attribution: AttributionContext{
			Present: true, Screen: LocalSymbol(12), Owner: StableSymbol(0x102), OperationID: 42,
		}, RuntimeCall: &RuntimeCallEvent{
			CalleeRef: StableSymbol(0x202), Count: 4, TotalMS: 19, MaxMS: 9,
		}},
	}
	if err := writer.WriteRuntimeCallBlock(events); err != nil {
		t.Fatal(err)
	}
	if err := closer.Close(); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	offset := firstChunkOffset(t, raw)
	metadata, err := parseChunkHeader(raw[offset:offset+chunkHeaderSize], 0)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.RecordCount != 1 {
		t.Fatalf("physical runtime block records = %d, want 1", metadata.RecordCount)
	}

	log, err := readLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(log.Events) != 2 || log.Result.DataRecords != 2 || log.Result.Events != 2 ||
		log.Result.RecordsByType[EventRuntimeCall] != 2 || log.Result.RuntimeGraphLogicalCalls != 7 {
		t.Fatalf("columnar semantic accounting = events:%d result:%+v", len(log.Events), log.Result)
	}
	first, second := log.Events[0], log.Events[1]
	if first.Attribution.Owner.ID != 0x101 || second.RuntimeCall.CalleeRef.ID != 0x202 ||
		first.Attribution.Screen.ID != 11 || second.Attribution.OperationID != 42 || second.RuntimeCall.Count != 4 ||
		first.TimeUS != second.TimeUS || second.DeltaUS != 0 {
		t.Fatalf("decoded runtime rows = first:%+v second:%+v", first, second)
	}
	if log.Result.SegmentEnd == nil || log.Result.SegmentEnd.TotalEventRecords != 2 ||
		log.Result.LatestQuality.Counters[QualityWrittenEventTotal] != 2 ||
		log.Result.LatestQuality.Counters[QualityRuntimeGraphInputTotal] != 7 ||
		log.Result.LatestQuality.Counters[QualityRuntimeGraphEmittedTotal] != 7 {
		t.Fatalf("columnar terminal proof = end:%+v quality:%+v", log.Result.SegmentEnd, log.Result.LatestQuality)
	}
}

func TestRuntimeCallWriterRejectsInvalidLogicalAggregates(t *testing.T) {
	for _, test := range []struct {
		name string
		call RuntimeCallEvent
	}{
		{name: "zero count", call: RuntimeCallEvent{}},
		{name: "max exceeds total", call: RuntimeCallEvent{Count: 1, TotalMS: 2, MaxMS: 3}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			writer, err := NewWriter(&output)
			if err != nil {
				t.Fatal(err)
			}
			err = writer.WriteEvent(Event{Type: EventRuntimeCall, RuntimeCall: &test.call})
			if err == nil {
				t.Fatalf("invalid runtime call was accepted: %+v", test.call)
			}
		})
	}
}

func TestRuntimeCallColumnarBlockRejectsInvalidRowCounts(t *testing.T) {
	for _, rowCount := range []uint64{0, MaxRuntimeCallBlockRows + 1} {
		var body bytes.Buffer
		_ = writeUvarint(&body, uint64(EventRuntimeCall))
		_ = writeUvarint(&body, 0)
		_ = writeUvarint(&body, rowCount)
		_, _, err := decodeRecord(
			body.Bytes(),
			recordDecodeState{},
			DefaultSegmentHeader(),
			"",
			"invalid",
			RecordPosition{},
			nil,
		)
		if err == nil || !strings.Contains(err.Error(), "row count") {
			t.Fatalf("row count %d error = %v", rowCount, err)
		}
	}
}

func TestRuntimeCallColumnarBlockRejectsInvalidLogicalAggregates(t *testing.T) {
	for _, test := range []struct {
		name  string
		count uint64
		total uint64
		max   uint64
	}{
		{name: "zero count", count: 0},
		{name: "max exceeds total", count: 1, total: 2, max: 3},
	} {
		t.Run(test.name, func(t *testing.T) {
			var body bytes.Buffer
			_ = writeUvarint(&body, uint64(EventRuntimeCall))
			_ = writeUvarint(&body, 0)
			_ = writeUvarint(&body, 1)
			for range 5 {
				_ = writeUvarint(&body, 0)
			}
			_ = writeUvarint(&body, test.count)
			_ = writeUvarint(&body, test.total)
			_ = writeUvarint(&body, test.max)
			if _, _, err := decodeRecord(
				body.Bytes(),
				recordDecodeState{},
				DefaultSegmentHeader(),
				"",
				"invalid",
				RecordPosition{},
				nil,
			); err == nil {
				t.Fatal("invalid runtime aggregate was decoded")
			}
		})
	}
}

func TestProcessScopeRoundTripsInVersionThreeHeader(t *testing.T) {
	header := DefaultSegmentHeader()
	header.ProcessScope = ProcessScopeAllowlist
	header.AllowedProcessCount = 3
	header.ProcessScopeFingerprint = bytes.Repeat([]byte{0xa5}, processScopeHashSize)
	raw, normalized, err := encodeFileHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeHeaderPayload(raw[magicSize+8:])
	if err != nil {
		t.Fatal(err)
	}
	if decoded.ProcessScope != ProcessScopeAllowlist || decoded.AllowedProcessCount != 3 ||
		normalized.ProcessScope != ProcessScopeAllowlist ||
		!bytes.Equal(decoded.ProcessScopeFingerprint, header.ProcessScopeFingerprint) {
		t.Fatalf("process scope did not round-trip: decoded=%+v normalized=%+v", decoded, normalized)
	}
}

func TestProcessScopeRejectsContradictoryHeader(t *testing.T) {
	header := DefaultSegmentHeader()
	header.ProcessScope = ProcessScopeAll
	header.AllowedProcessCount = 1
	if _, _, err := encodeFileHeader(header); err == nil || !strings.Contains(err.Error(), "cannot declare") {
		t.Fatalf("encodeFileHeader() error = %v", err)
	}
}

func TestProcessAllowlistRequiresFullFingerprint(t *testing.T) {
	header := DefaultSegmentHeader()
	header.ProcessScope = ProcessScopeAllowlist
	header.AllowedProcessCount = 1
	if _, _, err := encodeFileHeader(header); err == nil || !strings.Contains(err.Error(), "fingerprint") {
		t.Fatalf("encodeFileHeader() error = %v", err)
	}
}

func TestQualityProgressionRejectsRegressedCounter(t *testing.T) {
	previous := QualitySnapshot{
		Sequence: 1, CapturedElapsedUS: 100,
		Counters: map[uint64]uint64{QualityAcceptedEventTotal: 5},
	}
	current := QualitySnapshot{
		Sequence: 2, CapturedElapsedUS: 200,
		Counters: map[uint64]uint64{QualityAcceptedEventTotal: 4},
	}

	err := ValidateQualityProgression(previous, current)
	if err == nil || !strings.Contains(err.Error(), "counter 1 regressed from 5 to 4") {
		t.Fatalf("ValidateQualityProgression() error = %v", err)
	}
}

func TestKnownQualityCounterSetIsClosedForVersionThree(t *testing.T) {
	known := []uint64{
		QualityAcceptedEventTotal,
		QualityRuntimeEventBackpressureNanos,
		QualityRuntimeGraphDisabled,
		QualityRuntimeHookFailureTotal,
		QualityJankStatsDependencyMissing,
		QualityRuntimeHookUnclassifiedFailure,
		QualityPreparedStatementRegistryEviction,
		QualityPreparedStatementResolutionMiss,
		QualityReceiverAsyncRegistryEviction,
		QualityReceiverAsyncResolutionMiss,
		EventQualityCounterID(EventRuntimeCall, QualityLossAdmissionContention),
	}
	for _, id := range known {
		if !IsKnownQualityCounter(id) {
			t.Fatalf("counter %d should be known", id)
		}
	}
	for _, id := range []uint64{
		0,
		0x1000,
		0x1fff,
		0x2002,
		0x201e,
		0x7fff,
	} {
		if IsKnownQualityCounter(id) {
			t.Fatalf("counter %d should be unknown", id)
		}
	}
}

func TestWriterUsesHeaderAndCommittedGZIPChunks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chunked.jhlog")
	if err := WriteSample(path); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	chunkOffset := firstChunkOffset(t, raw)
	if !bytes.Equal(raw[chunkOffset:chunkOffset+4], chunkMagic[:]) {
		t.Fatalf("chunk magic = %q", raw[chunkOffset:chunkOffset+4])
	}
	flags := binary.LittleEndian.Uint16(raw[chunkOffset+6 : chunkOffset+8])
	if flags&chunkFlagGZIP == 0 {
		t.Fatalf("first chunk flags = 0x%x, want gzip", flags)
	}
	storedLength := binary.LittleEndian.Uint32(raw[chunkOffset+12 : chunkOffset+16])
	trailerOffset := chunkOffset + chunkHeaderSize + int(storedLength)
	if !bytes.Equal(raw[trailerOffset:trailerOffset+4], commitMagic[:]) {
		t.Fatalf("commit magic = %q", raw[trailerOffset:trailerOffset+4])
	}
}

func TestReaderAcceptsRawChunkCodec(t *testing.T) {
	path := filepath.Join(t.TempDir(), "raw.jhlog")
	var output bytes.Buffer
	writer, err := NewWriterWithOptions(&output, WriterOptions{Header: DefaultSegmentHeader(), GZIP: false})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteEvent(Event{Type: EventMemory, TimeMS: 10, Memory: &MemoryEvent{PSSKB: 1, JavaHeapKB: 2, NativeHeapKB: 3}}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, output.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := StreamFileWithResult(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != SegmentStatusClosedClean || result.Events != 1 {
		t.Fatalf("result = %+v", result)
	}
}

func TestRetainedEvidenceRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "retained-evidence.jhlog")
	writeClosedEvents(t, path, []Event{{
		Type: EventRetained,
		Retained: &RetainedEvent{
			ClassRef: LocalSymbol(1),
			AgeMS:    30_000,
			Count:    2,
			Evidence: RetentionEvidenceAfterExplicitGC,
		},
	}})

	var retained *RetainedEvent
	if _, err := StreamFileWithResult(path, func(event Event, _ map[uint64]string) error {
		if event.Retained != nil {
			copy := *event.Retained
			retained = &copy
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if retained == nil || retained.Evidence != RetentionEvidenceAfterExplicitGC {
		t.Fatalf("retained evidence = %+v", retained)
	}
}

func TestRetainedPayloadRequiresKnownEvidence(t *testing.T) {
	tests := []struct {
		name     string
		evidence *uint64
		message  string
	}{
		{name: "missing", message: "retention evidence"},
		{name: "unknown zero", evidence: pointerTo(uint64(RetentionEvidenceUnknown)), message: "unsupported retention evidence 0"},
		{name: "unknown future value", evidence: pointerTo(uint64(99)), message: "unsupported retention evidence 99"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var payload bytes.Buffer
			for _, value := range []uint64{2, 0, 30_000, 1} { // local class id=1, no holder, age, count
				if err := writeUvarint(&payload, value); err != nil {
					t.Fatal(err)
				}
			}
			if test.evidence != nil {
				if err := writeUvarint(&payload, *test.evidence); err != nil {
					t.Fatal(err)
				}
			}
			event := Event{Type: EventRetained}
			err := decodeEventPayload(&recordReader{data: payload.Bytes()}, &event, DefaultSegmentHeader(), "", nil)
			if err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("invalid retained evidence: err=%v event=%+v", err, event.Retained)
			}
		})
	}
}

func pointerTo[T any](value T) *T { return &value }

func TestSegmentEndReasonsUseClosedJH100Contract(t *testing.T) {
	if SegmentEndNormal != 0 || SegmentEndSizeLimit != 1 || SegmentEndIOError != 2 || SegmentEndShutdown != 3 || SegmentEndRotation != 4 || SegmentEndStorageBudget != 5 {
		t.Fatalf("segment end reason wire values changed: normal=%d size=%d io=%d shutdown=%d rotation=%d storage=%d", SegmentEndNormal, SegmentEndSizeLimit, SegmentEndIOError, SegmentEndShutdown, SegmentEndRotation, SegmentEndStorageBudget)
	}
	for _, reason := range []SegmentEndReason{
		SegmentEndNormal,
		SegmentEndSizeLimit,
		SegmentEndIOError,
		SegmentEndShutdown,
		SegmentEndRotation,
		SegmentEndStorageBudget,
	} {
		t.Run(reason.String(), func(t *testing.T) {
			var output bytes.Buffer
			writer, err := NewWriter(&output)
			if err != nil {
				t.Fatalf("NewWriter() error = %v", err)
			}
			if err := writer.CloseWithReason(reason); err != nil {
				t.Fatalf("CloseWithReason(%d) error = %v", reason, err)
			}
			path := filepath.Join(t.TempDir(), "reason.jhlog")
			if err := os.WriteFile(path, output.Bytes(), 0o600); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}
			result, err := StreamFileWithResult(path, nil)
			if err != nil {
				t.Fatalf("StreamFileWithResult() error = %v", err)
			}
			if result.SegmentEnd == nil || result.SegmentEnd.Reason != reason {
				t.Fatalf("segment end = %+v, want reason %d", result.SegmentEnd, reason)
			}
		})
	}
	var output bytes.Buffer
	writer, err := NewWriter(&output)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.CloseWithReason(99); err == nil || !strings.Contains(err.Error(), "unsupported segment end reason") {
		t.Fatalf("unknown segment end reason error = %v", err)
	}
}

func TestSizeLimitQualityCountersKeepWireNames(t *testing.T) {
	if QualityEventLostAfterSizeLimitTotal != 17 || QualityLossSizeLimit != 5 {
		t.Fatalf("size-limit quality wire values changed: total=%d reason=%d", QualityEventLostAfterSizeLimitTotal, QualityLossSizeLimit)
	}
	if got := QualityCounterName(QualityEventLostAfterSizeLimitTotal); got != "event_lost_after_size_limit_total" {
		t.Fatalf("QualityCounterName() = %q", got)
	}
}

func TestBufferedRuntimeGraphQualityCountersKeepWireNames(t *testing.T) {
	cases := map[uint64]string{
		QualityRuntimeGraphShutdownLoss:          "runtime_graph_shutdown_loss_total",
		QualityRuntimeGraphWriterRejectionLoss:   "runtime_graph_writer_rejection_loss_total",
		QualityRuntimeGraphDisabled:              "runtime_graph_disabled_total",
		QualityRuntimeHookFailureTotal:           "runtime_hook_failure_total",
		QualityJankStatsDependencyMissing:        "jankstats_dependency_missing_total",
		QualityRuntimeHookUnclassifiedFailure:    "runtime_hook_unclassified_failure_total",
		QualityPreparedStatementRegistryEviction: "prepared_statement_registry_eviction_total",
		QualityPreparedStatementResolutionMiss:   "prepared_statement_resolution_miss_after_eviction_total",
	}
	for id, want := range cases {
		if got := QualityCounterName(id); got != want {
			t.Fatalf("QualityCounterName(%d) = %q, want %q", id, got, want)
		}
	}
}

func TestRuntimeEventTransportQualityCountersKeepWireNames(t *testing.T) {
	cases := map[uint64]string{
		QualityRuntimeEventBufferCapacityLoss:   "runtime_event_buffer_capacity_loss_total",
		QualityRuntimeEventRegistryCapacityLoss: "runtime_event_registry_capacity_loss_total",
		QualityMethodCounterCardinalityLoss:     "method_counter_cardinality_loss_total",
		QualityRuntimeEventWriterRejectionLoss:  "runtime_event_writer_rejection_loss_total",
		QualityRuntimeGraphInputTotal:           "runtime_graph_input_total",
		QualityRuntimeGraphEmittedTotal:         "runtime_graph_emitted_total",
		QualityRuntimeGraphBackpressureCount:    "runtime_graph_backpressure_count_total",
		QualityRuntimeGraphBackpressureNanos:    "runtime_graph_backpressure_nanos_total",
		QualityWriterBackpressureCount:          "writer_backpressure_count_total",
		QualityWriterBackpressureNanos:          "writer_backpressure_nanos_total",
		QualityRuntimeEventBackpressureCount:    "runtime_event_backpressure_count_total",
		QualityRuntimeEventBackpressureNanos:    "runtime_event_backpressure_nanos_total",
	}
	for id, want := range cases {
		if got := QualityCounterName(id); got != want {
			t.Fatalf("QualityCounterName(%d) = %q, want %q", id, got, want)
		}
	}
}

func TestProfileFilesReportsJH100ControlAndEventSizes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.jhlog")
	if err := WriteSample(path); err != nil {
		t.Fatal(err)
	}
	profile, err := ProfileFiles([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if len(profile.Files) != 1 || profile.Files[0].Format != "jhlog-3.0.0" || profile.Files[0].Status != SegmentStatusClosedClean {
		t.Fatalf("file profile = %+v", profile.Files)
	}
	rows := map[EventType]SizeProfileType{}
	for _, row := range profile.Types {
		rows[row.Type] = row
	}
	for _, eventType := range []EventType{EventDictionary, EventHTTP, EventGauge, EventQualitySnapshot, EventSegmentEnd} {
		if row := rows[eventType]; row.Events == 0 || row.Bytes == 0 {
			t.Fatalf("missing size row for %s: %+v", EventTypeName(eventType), row)
		}
	}
}

func TestSessionContextAndMetricRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "payloads.jhlog")
	events := []Event{
		{Type: EventSession, TimeMS: 1, Session: &SessionEvent{AppVersionRef: LocalSymbol(1), BuildRef: LocalSymbol(2), DeviceRef: LocalSymbol(3), SDKInt: 35, DeviceRooted: true}},
		{Type: EventContext, TimeMS: 2, Flags: uint64(FlagAppForeground), Context: &ContextEvent{Network: NetworkVPN, BatteryPct: 50, AvailMemoryKB: 1024, BatteryState: 3, BatteryTempDeciC: -45, LowMemory: true, NetworkMetered: true, NetworkValidated: true, NetworkVPN: true, RxBytes: 1000, TxBytes: 2000, TotalMemoryKB: 4096, FreeStorageKB: 8192, TotalStorageKB: 16384}},
		{Type: EventGauge, TimeMS: 3, Metric: &MetricEvent{MetricRef: LocalSymbol(4), Value: 130, Count: 2, Sum: 260, Max: 160, Mode: MetricModeAverage}},
	}
	writeClosedEvents(t, path, events)
	log, err := readLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(log.Events) != len(events) {
		t.Fatalf("events = %d, want %d", len(log.Events), len(events))
	}
	if !log.Events[0].Session.DeviceRooted || log.Events[0].Flags&uint64(FlagDeviceRooted) == 0 {
		t.Fatalf("root flag did not round-trip: %+v", log.Events[0])
	}
	context := log.Events[1].Context
	if context == nil || !context.LowMemory || !context.NetworkMetered || !context.NetworkValidated || !context.NetworkVPN || context.BatteryTempDeciC != -45 {
		t.Fatalf("context = %+v", context)
	}
	metric := log.Events[2].Metric
	if metric == nil || metric.Value != 130 || metric.Count != 2 || metric.Sum != 260 || metric.Max != 160 || metric.Mode != MetricModeAverage {
		t.Fatalf("metric = %+v", metric)
	}
}

func TestVersionThreeTypedEvidenceRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "typed-evidence.jhlog")
	buckets := make([]uint64, UIFrameHistogramBucketCount)
	buckets[1] = 50
	buckets[5] = 45
	buckets[8] = 5
	events := []Event{
		{
			Type: EventSession, TimeMS: 1,
			Session: &SessionEvent{CollectorFlags: uint64(CollectorFPS | CollectorJankStats | CollectorProcessExit | CollectorIOTracing)},
		},
		{
			Type: EventUIWindow, TimeMS: 10_000, Flags: uint64(FlagThreadMain),
			UIWindow: &UIWindowEvent{
				WindowMS: 10_000, FrameCount: 100, JankCount: 5,
				Source: UIFrameSourceJankStats, FrameDeadlineUS: 16_667, FrameDurationBuckets: buckets,
			},
		},
		{
			Type: EventProcessExit, TimeMS: 10_100,
			ProcessExit: &ProcessExitEvent{
				Reason: 6, TimestampUnixMS: 1_750_000_000_000, Importance: 100,
				PSSKB: 256_000, RSSKB: 320_000, ProcessRef: LocalSymbol(10),
			},
		},
		{
			Type: EventIO, TimeMS: 10_200, Flags: uint64(FlagThreadMain | FlagIOBytesKnown),
			IO: &IOEvent{
				SourceRef: StableSymbol(0x32621), Operation: IOOperationContentRead,
				Outcome: IOOutcomeSuccess, DurationUS: 275_000, Bytes: 4_096,
			},
		},
	}
	writeClosedEvents(t, path, events)
	log, err := readLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(log.Events) != len(events) {
		t.Fatalf("events = %d, want %d", len(log.Events), len(events))
	}
	if got := log.Events[0].Session.CollectorFlags; got != events[0].Session.CollectorFlags {
		t.Fatalf("collector flags = 0x%x, want 0x%x", got, events[0].Session.CollectorFlags)
	}
	window := log.Events[1].UIWindow
	if window == nil || window.Source != UIFrameSourceJankStats || window.FrameDeadlineUS != 16_667 ||
		window.P50MS != 12 || window.P95MS != 32 || window.P99MS != 67 || !slices.Equal(window.FrameDurationBuckets, buckets) {
		t.Fatalf("UI window = %+v", window)
	}
	exit := log.Events[2].ProcessExit
	if exit == nil || exit.Reason != 6 || exit.TimestampUnixMS != 1_750_000_000_000 || exit.ProcessRef != LocalSymbol(10) {
		t.Fatalf("process exit = %+v", exit)
	}
	ioEvent := log.Events[3]
	if ioEvent.IO == nil || ioEvent.IO.Operation != IOOperationContentRead || ioEvent.IO.DurationUS != 275_000 ||
		ioEvent.IO.Bytes != 4_096 || ioEvent.IO.Outcome != IOOutcomeSuccess ||
		ioEvent.IO.SourceRef != StableSymbol(0x32621) || ioEvent.Flags&uint64(FlagThreadMain|FlagIOBytesKnown) == 0 {
		t.Fatalf("I/O event = %+v", ioEvent)
	}
}

func TestAdvancedIOValidation(t *testing.T) {
	valid := IOEvent{Operation: IOOperationFileRead, Outcome: IOOutcomeSuccess}
	tests := []struct {
		name  string
		event IOEvent
		flags uint64
		want  string
	}{
		{name: "unknown outcome", event: IOEvent{Operation: IOOperationFileRead}, want: "I/O outcome must be success or failure"},
		{name: "invalid outcome", event: IOEvent{Operation: IOOperationFileRead, Outcome: IOOutcome(9)}, want: "unsupported I/O outcome 9"},
		{name: "removed database read", event: IOEvent{Operation: IOOperationKind(4), Outcome: IOOutcomeSuccess}, want: "unsupported I/O operation 4"},
		{name: "removed database write", event: IOEvent{Operation: IOOperationKind(5), Outcome: IOOutcomeSuccess}, want: "unsupported I/O operation 5"},
		{name: "bytes without coverage", event: IOEvent{Operation: IOOperationFileRead, Outcome: IOOutcomeSuccess, Bytes: 1}, want: "I/O bytes require bytes-known flag"},
		{name: "unsupported flag", event: valid, flags: uint64(FlagWorkerPeriodic), want: "unsupported semantic flag"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, _, err := encodeRecord(Event{Type: EventIO, Flags: test.flags, IO: &test.event}, recordEncodeState{})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("encodeRecord() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestAdvancedHTTPEventRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "advanced-http.jhlog")
	want := &HTTPEvent{
		RouteRef:        LocalSymbol(1),
		ServiceRef:      LocalSymbol(2),
		InitiatorRef:    StableSymbol(0x1234),
		DurationMS:      850,
		QueueMS:         30,
		DNSMS:           12,
		ConnectMS:       45,
		TLSMS:           20,
		RequestMS:       8,
		TTFBMS:          510,
		ResponseMS:      220,
		StatusCode:      503,
		Status:          Status5xx,
		FailurePhase:    HTTPFailurePhaseResponse,
		FailureKind:     HTTPFailureKindProtocol,
		Protocol:        HTTPProtocol2,
		RxBytes:         42_120,
		TxBytes:         740,
		Attempts:        2,
		DNSAttempts:     1,
		ConnectAttempts: 2,
		TLSAttempts:     1,
		ConnectFailures: 1,
		TLSFailures:     0,
		Redirects:       1,
	}
	writeClosedEvents(t, path, []Event{{
		Type:  EventHTTP,
		Flags: uint64(FlagHTTPFailed | FlagHTTPTLS | FlagHTTPResponseBytesKnown | FlagHTTPRequestBytesKnown),
		HTTP:  want,
	}})
	log, err := readLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(log.Events) != 1 || log.Events[0].HTTP == nil {
		t.Fatalf("HTTP events = %+v", log.Events)
	}
	if got := log.Events[0].HTTP; !reflect.DeepEqual(got, want) {
		t.Fatalf("HTTP event = %+v, want %+v", got, want)
	}
	wantFlags := uint64(FlagHTTPFailed | FlagHTTPTLS | FlagHTTPResponseBytesKnown | FlagHTTPRequestBytesKnown)
	if got := log.Events[0].Flags; got != wantFlags {
		t.Fatalf("HTTP flags = 0x%x, want 0x%x", got, wantFlags)
	}
}

func TestAdvancedHTTPEventValidation(t *testing.T) {
	valid := HTTPEvent{DurationMS: 100, StatusCode: 200, Attempts: 1}
	tests := []struct {
		name string
		edit func(*HTTPEvent)
		want string
	}{
		{name: "invalid status code", edit: func(event *HTTPEvent) { event.StatusCode = 99 }, want: "HTTP status code 99"},
		{name: "phase exceeds total", edit: func(event *HTTPEvent) { event.TLSMS = 101 }, want: "HTTP TLS duration 101 exceeds request duration 100"},
		{name: "invalid failure phase", edit: func(event *HTTPEvent) { event.FailurePhase = HTTPFailurePhase(99) }, want: "unsupported HTTP failure phase 99"},
		{name: "invalid failure kind", edit: func(event *HTTPEvent) { event.FailureKind = HTTPFailureKind(99) }, want: "unsupported HTTP failure kind 99"},
		{name: "invalid protocol", edit: func(event *HTTPEvent) { event.Protocol = HTTPProtocol(99) }, want: "unsupported HTTP protocol 99"},
		{name: "connect failures exceed attempts", edit: func(event *HTTPEvent) { event.ConnectFailures = 1 }, want: "HTTP connect failure count 1 exceeds connect attempt count 0"},
		{name: "TLS failures exceed attempts", edit: func(event *HTTPEvent) { event.TLSFailures = 1 }, want: "HTTP TLS failure count 1 exceeds TLS attempt count 0"},
		{name: "redirects exceed attempts", edit: func(event *HTTPEvent) { event.Redirects = 2 }, want: "HTTP redirect count 2 exceeds attempt count 1"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			event := valid
			test.edit(&event)
			_, _, _, err := encodeRecord(Event{Type: EventHTTP, HTTP: &event}, recordEncodeState{})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("encodeRecord() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestWorkerLifecycleRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "worker-lifecycle.jhlog")
	events := []Event{
		{
			Type: EventWorker, TimeMS: 100, Flags: uint64(FlagWorkerPeriodic),
			Worker: &WorkerEvent{InstanceID: 0x32621, Stage: WorkerStageEnqueued},
		},
		{
			Type: EventWorker, TimeMS: 350,
			Worker: &WorkerEvent{
				WorkerRef: StableSymbol(0x1234), InstanceID: 0x32621,
				Stage: WorkerStageStarted, RunAttempt: 2, Generation: 3,
			},
		},
		{
			Type: EventWorker, TimeMS: 850, Flags: uint64(FlagWorkerStopReasonKnown),
			Worker: &WorkerEvent{
				WorkerRef: StableSymbol(0x1234), InstanceID: 0x32621,
				Stage: WorkerStageFinished, Outcome: WorkerOutcomeCancelled,
				DurationMS: 500, RunAttempt: 2, Generation: 3, StopReason: 4,
			},
		},
	}
	writeClosedEvents(t, path, events)
	log, err := readLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(log.Events) != len(events) {
		t.Fatalf("worker event count = %d, want %d", len(log.Events), len(events))
	}
	for index := range events {
		if log.Events[index].TimeMS != events[index].TimeMS || log.Events[index].Flags != events[index].Flags ||
			!reflect.DeepEqual(log.Events[index].Worker, events[index].Worker) {
			t.Fatalf("worker event %d = %+v, want %+v", index, log.Events[index], events[index])
		}
	}
}

func TestWorkerLifecycleValidation(t *testing.T) {
	valid := WorkerEvent{InstanceID: 1, Stage: WorkerStageStarted}
	tests := []struct {
		name  string
		event WorkerEvent
		flags uint64
		want  string
	}{
		{name: "missing instance", event: WorkerEvent{Stage: WorkerStageStarted}, want: "worker instance ID must be non-zero"},
		{name: "missing worker", event: valid, want: "started or finished worker requires a worker reference"},
		{name: "invalid stage", event: WorkerEvent{InstanceID: 1, Stage: WorkerStage(99)}, want: "unsupported worker stage 99"},
		{name: "outcome before finish", event: WorkerEvent{InstanceID: 1, Stage: WorkerStageStarted, Outcome: WorkerOutcomeSuccess}, want: "worker outcome is only valid for finished stage"},
		{name: "duration before finish", event: WorkerEvent{InstanceID: 1, Stage: WorkerStageStarted, DurationMS: 1}, want: "worker duration is only valid for finished stage"},
		{name: "missing terminal outcome", event: WorkerEvent{InstanceID: 1, Stage: WorkerStageFinished}, want: "finished worker requires an outcome"},
		{name: "invalid outcome", event: WorkerEvent{InstanceID: 1, Stage: WorkerStageFinished, Outcome: WorkerOutcome(99)}, want: "unsupported worker outcome 99"},
		{name: "stop reason before finish", event: valid, flags: uint64(FlagWorkerStopReasonKnown), want: "worker stop reason is only valid for finished stage"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, _, err := encodeRecord(Event{Type: EventWorker, Flags: test.flags, Worker: &test.event}, recordEncodeState{})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("encodeRecord() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestWebSocketLifecycleRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "websocket-lifecycle.jhlog")
	events := []Event{
		{
			Type: EventWebSocket, TimeMS: 100,
			WebSocket: &WebSocketEvent{
				RouteRef: StableSymbol(0x91), ConnectionID: 0x32621,
				Stage: WebSocketStageOpened, DurationMS: 75, StatusCode: 101,
			},
		},
		{
			Type: EventWebSocket, TimeMS: 12_100,
			WebSocket: &WebSocketEvent{
				RouteRef: StableSymbol(0x91), ConnectionID: 0x32621,
				Stage: WebSocketStageClosed, DurationMS: 12_000, StatusCode: 101,
				CloseCode: 1000, TextMessages: 7, BinaryMessages: 3, ReceivedBytes: 4_096,
			},
		},
	}
	writeClosedEvents(t, path, events)
	log, err := readLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(log.Events) != len(events) {
		t.Fatalf("WebSocket event count = %d, want %d", len(log.Events), len(events))
	}
	for index := range events {
		if log.Events[index].TimeMS != events[index].TimeMS ||
			!reflect.DeepEqual(log.Events[index].WebSocket, events[index].WebSocket) {
			t.Fatalf("WebSocket event %d = %+v, want %+v", index, log.Events[index], events[index])
		}
	}
}

func TestWebSocketLifecycleValidation(t *testing.T) {
	valid := WebSocketEvent{ConnectionID: 1, Stage: WebSocketStageOpened, StatusCode: 101}
	tests := []struct {
		name  string
		event WebSocketEvent
		want  string
	}{
		{name: "missing connection", event: WebSocketEvent{Stage: WebSocketStageOpened}, want: "WebSocket connection ID must be non-zero"},
		{name: "invalid stage", event: WebSocketEvent{ConnectionID: 1, Stage: WebSocketStage(99)}, want: "unsupported WebSocket stage 99"},
		{name: "invalid status", event: WebSocketEvent{ConnectionID: 1, Stage: WebSocketStageOpened, StatusCode: 99}, want: "WebSocket status code 99"},
		{name: "close code on open", event: WebSocketEvent{ConnectionID: 1, Stage: WebSocketStageOpened, CloseCode: 1000}, want: "WebSocket close code is only valid for closed stage"},
		{name: "failure on open", event: WebSocketEvent{ConnectionID: 1, Stage: WebSocketStageOpened, FailureKind: WebSocketFailureTimeout}, want: "WebSocket failure kind is only valid for failed stage"},
		{name: "messages on open", event: WebSocketEvent{ConnectionID: 1, Stage: WebSocketStageOpened, TextMessages: 1}, want: "WebSocket traffic is only valid for terminal stages"},
		{name: "missing failure", event: WebSocketEvent{ConnectionID: 1, Stage: WebSocketStageFailed}, want: "failed WebSocket requires a failure kind"},
		{name: "invalid failure", event: WebSocketEvent{ConnectionID: 1, Stage: WebSocketStageFailed, FailureKind: WebSocketFailureKind(99)}, want: "unsupported WebSocket failure kind 99"},
		{name: "invalid close code", event: WebSocketEvent{ConnectionID: 1, Stage: WebSocketStageClosed, CloseCode: 999}, want: "WebSocket close code 999"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			event := test.event
			if test.name == "invalid status" {
				event = valid
				event.StatusCode = 99
			}
			_, _, _, err := encodeRecord(Event{Type: EventWebSocket, WebSocket: &event}, recordEncodeState{})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("encodeRecord() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestDatabaseQueryRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "database.jhlog")
	event := Event{
		Type: EventDatabase, TimeMS: 400, Flags: uint64(FlagThreadMain),
		Database: &DatabaseEvent{
			QueryRef: LocalSymbol(1), SourceRef: StableSymbol(0x32621),
			Framework: DatabaseFrameworkRoom, Operation: DatabaseOperationQuery,
			Outcome: DatabaseOutcomeSuccess, FailureKind: DatabaseFailureNone,
			Boundary: DatabaseBoundaryExecute, StatementFingerprint: 0x7123,
			ResultKnown: true, ResultKind: DatabaseResultAffectedRows,
			ResultCountBucket: DatabaseCountTwoToTen,
			TransactionID:     17, StatementToken: 29,
			PhaseMask:  DatabasePhaseLockWait | DatabasePhaseExecute,
			LockWaitUS: 10_000, ExecuteUS: 200_000, DurationUS: 250_000,
		},
	}
	writeClosedEvents(t, path, []Event{event})
	log, err := readLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(log.Events) != 1 || !reflect.DeepEqual(log.Events[0].Database, event.Database) ||
		log.Events[0].Flags != event.Flags {
		t.Fatalf("database event = %+v, want %+v", log.Events, event)
	}
}

func TestDatabaseTransactionRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "database-transaction.jhlog")
	events := []Event{
		{
			Type: EventDatabaseTransaction, TimeMS: 100, Flags: uint64(FlagThreadMain),
			DatabaseTransaction: &DatabaseTransactionEvent{
				SourceRef: StableSymbol(0x51), TransactionID: 7, ParentID: 3,
				Stage: DatabaseTransactionBegin, Mode: DatabaseTransactionImmediate,
			},
		},
		{
			Type: EventDatabaseTransaction, TimeMS: 200, Flags: uint64(FlagThreadMain),
			DatabaseTransaction: &DatabaseTransactionEvent{
				SourceRef: StableSymbol(0x51), TransactionID: 7, ParentID: 3,
				Stage: DatabaseTransactionTerminal, Mode: DatabaseTransactionImmediate,
				Outcome: DatabaseTransactionSuccess, DurationUS: 100_000,
				StatementCount: 4, ReadCount: 3, WriteCount: 1,
			},
		},
	}
	writeClosedEvents(t, path, events)
	log, err := readLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(log.Events) != len(events) ||
		!reflect.DeepEqual(log.Events[0].DatabaseTransaction, events[0].DatabaseTransaction) ||
		!reflect.DeepEqual(log.Events[1].DatabaseTransaction, events[1].DatabaseTransaction) {
		t.Fatalf("database transaction events = %+v, want %+v", log.Events, events)
	}
}

func TestAndroidComponentsAndBinderRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "android-components.jhlog")
	events := []Event{
		{
			Type: EventProcessState, TimeMS: 100,
			ProcessState: &ProcessStateEvent{
				UIVisibility: ProcessUIVisible, Importance: ProcessImportanceForegroundService,
				AndroidImportance: 125, Reason: ProcessStateReasonComponentLifecycle,
			},
		},
		{
			Type: EventAndroidComponent, TimeMS: 200,
			AndroidComponent: &AndroidComponentEvent{
				ComponentRef: StableSymbol(0x419), ActionRef: LocalSymbol(1),
				InstanceID: 11, FlowID: 12, Kind: ComponentKindService,
				Stage: ComponentServiceStartCommand, Outcome: ComponentOutcomeSuccess,
				DurationUS: 9_000, Flags: ComponentFlagForeground,
			},
		},
		{
			Type: EventBinderTransaction, TimeMS: 300, Flags: uint64(FlagThreadMain),
			BinderTransaction: &BinderTransactionEvent{
				DescriptorRef: LocalSymbol(2), MethodRef: LocalSymbol(3), CallID: 31,
				Direction: BinderDirectionClient, TransactionCode: 7,
				Outcome: BinderOutcomeSuccess, FailureKind: BinderFailureNone,
				DurationUS: 4_000, Flags: BinderFlagOneway,
			},
		},
	}
	writeClosedEvents(t, path, events)
	log, err := readLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(log.Events) != len(events) ||
		!reflect.DeepEqual(log.Events[0].ProcessState, events[0].ProcessState) ||
		!reflect.DeepEqual(log.Events[1].AndroidComponent, events[1].AndroidComponent) ||
		!reflect.DeepEqual(log.Events[2].BinderTransaction, events[2].BinderTransaction) {
		t.Fatalf("Android component events = %+v, want %+v", log.Events, events)
	}
}

func TestDatabaseQueryValidation(t *testing.T) {
	tests := []struct {
		name  string
		event DatabaseEvent
		want  string
	}{
		{name: "missing source", event: DatabaseEvent{Framework: DatabaseFrameworkSQLite, Operation: DatabaseOperationQuery, Outcome: DatabaseOutcomeSuccess}, want: "database source is required"},
		{name: "invalid framework", event: DatabaseEvent{SourceRef: StableSymbol(1), Framework: DatabaseFramework(99), Operation: DatabaseOperationQuery, Outcome: DatabaseOutcomeSuccess}, want: "unsupported database framework 99"},
		{name: "invalid operation", event: DatabaseEvent{SourceRef: StableSymbol(1), Framework: DatabaseFrameworkSQLite, Operation: DatabaseOperation(99), Outcome: DatabaseOutcomeSuccess}, want: "unsupported database operation 99"},
		{name: "invalid outcome", event: DatabaseEvent{SourceRef: StableSymbol(1), Framework: DatabaseFrameworkSQLite, Operation: DatabaseOperationQuery, Outcome: DatabaseOutcome(99)}, want: "unsupported database outcome 99"},
		{name: "missing boundary", event: DatabaseEvent{SourceRef: StableSymbol(1), Framework: DatabaseFrameworkSQLite, Operation: DatabaseOperationQuery, Outcome: DatabaseOutcomeSuccess}, want: "database boundary is required"},
		{name: "success with failure", event: DatabaseEvent{SourceRef: StableSymbol(1), Framework: DatabaseFrameworkSQLite, Operation: DatabaseOperationQuery, Outcome: DatabaseOutcomeSuccess, Boundary: DatabaseBoundaryExecute, FailureKind: DatabaseFailureOther}, want: "successful database call cannot have failure kind"},
		{name: "unknown failed kind", event: DatabaseEvent{SourceRef: StableSymbol(1), Framework: DatabaseFrameworkSQLite, Operation: DatabaseOperationQuery, Outcome: DatabaseOutcomeFailure, Boundary: DatabaseBoundaryExecute}, want: "failed database call requires failure kind"},
		{name: "result flag mismatch", event: DatabaseEvent{SourceRef: StableSymbol(1), Framework: DatabaseFrameworkSQLite, Operation: DatabaseOperationQuery, Outcome: DatabaseOutcomeSuccess, Boundary: DatabaseBoundaryExecute, ResultKind: DatabaseResultRows}, want: "database result fields require known flag"},
		{name: "phase exceeds total", event: DatabaseEvent{SourceRef: StableSymbol(1), Framework: DatabaseFrameworkSQLite, Operation: DatabaseOperationQuery, Outcome: DatabaseOutcomeSuccess, Boundary: DatabaseBoundaryExecute, PhaseMask: DatabasePhaseExecute, ExecuteUS: 11, DurationUS: 10}, want: "database phases exceed total duration"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, _, err := encodeRecord(Event{Type: EventDatabase, Database: &test.event}, recordEncodeState{})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("encodeRecord() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestVersionTwoLogIsRejectedAfterCleanBreak(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy-v2.jhlog")
	legacyMagic := append([]byte(nil), Magic...)
	legacyMagic[8] = 2
	if err := os.WriteFile(path, legacyMagic, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readLog(path); err == nil || !strings.Contains(err.Error(), "unsupported JHLOG version") {
		t.Fatalf("legacy v2 error = %v", err)
	}
}

func TestAtomicContextAndSameContextRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "context.jhlog")
	context := AttributionContext{Present: true, Screen: LocalSymbol(1), Owner: LocalSymbol(2), OperationID: 3}
	events := []Event{
		{Type: EventProblem, TimeMS: 10, Attribution: context, Problem: &ProblemEvent{KindRef: LocalSymbol(5), WindowMS: 1000, Count: 1, MaxMS: 10}},
		{Type: EventLogSpam, TimeMS: 20, Attribution: context, LogSpam: &LogSpamEvent{SourceRef: LocalSymbol(4), Level: 5, Count: 9}},
		{Type: EventProblem, TimeMS: 30, Attribution: context, Problem: &ProblemEvent{KindRef: LocalSymbol(5), WindowMS: 5000, Count: 9, MaxMS: 9}},
	}
	_, state, _, err := encodeRecord(events[0], recordEncodeState{})
	if err != nil {
		t.Fatal(err)
	}
	secondRecord, _, _, err := encodeRecord(events[1], state)
	if err != nil {
		t.Fatal(err)
	}
	reader := bytes.NewReader(secondRecord)
	if _, err := binary.ReadUvarint(reader); err != nil {
		t.Fatal(err)
	}
	envelopeFlags, err := binary.ReadUvarint(reader)
	if err != nil {
		t.Fatal(err)
	}
	if envelopeFlags&uint64(EnvelopeSameContext) == 0 {
		t.Fatalf("second record envelope flags = 0x%x, want SAME_CONTEXT", envelopeFlags)
	}
	writeClosedEvents(t, path, events)
	log, err := readLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if log.Events[0].Attribution != context {
		t.Fatalf("first context = %+v", log.Events[0].Attribution)
	}
	for _, event := range log.Events[1:] {
		if event.Attribution != context {
			t.Fatalf("same context not preserved: %+v", event)
		}
	}
}

func TestContextStateResetsAtChunkBoundary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "context-reset.jhlog")
	var output bytes.Buffer
	writer, err := NewWriterWithOptions(&output, WriterOptions{Header: DefaultSegmentHeader(), RawChunkTarget: 1, GZIP: true})
	if err != nil {
		t.Fatal(err)
	}
	context := AttributionContext{Present: true, Owner: LocalSymbol(7)}
	for i := 0; i < 2; i++ {
		if err := writer.WriteEvent(Event{Type: EventMemory, TimeMS: uint64(i + 1), Attribution: context, Memory: &MemoryEvent{PSSKB: uint64(i + 1)}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, output.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	var events []Event
	result, err := StreamFileWithResult(path, func(event Event, _ map[uint64]string) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.CommittedChunks != 3 || len(events) != 2 {
		t.Fatalf("chunks=%d events=%d", result.CommittedChunks, len(events))
	}
	for _, event := range events {
		if event.Attribution != context {
			t.Fatalf("context not preserved across chunks: %+v", event)
		}
	}
}

func TestSignedProducerDeltasAllowQueueReordering(t *testing.T) {
	path := filepath.Join(t.TempDir(), "signed-time.jhlog")
	events := []Event{
		{Type: EventMemory, Producer: ProducerMetadata{HasTime: true, ElapsedUS: 2_000}, Memory: &MemoryEvent{PSSKB: 1}},
		{Type: EventMemory, Producer: ProducerMetadata{HasTime: true, ElapsedUS: 1_500}, Memory: &MemoryEvent{PSSKB: 2}},
	}
	writeClosedEvents(t, path, events)
	log, err := readLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if log.Events[0].TimeUS != 2_000 || log.Events[1].TimeUS != 1_500 || log.Events[1].DeltaUS != -500 {
		t.Fatalf("times = %+v, %+v", log.Events[0], log.Events[1])
	}
}

func TestStableRuntimeSymbolsKeepHeaderNamespace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stable.jhlog")
	header := DefaultSegmentHeader()
	header.SymbolNamespace = []byte{0xaa, 0xbb}
	file, writer, err := createWithHeaderForTest(path, header)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteEvent(Event{Type: EventRuntimeCall, TimeMS: 1, Attribution: AttributionContext{Present: true, Owner: StableSymbol(0x11)}, RuntimeCall: &RuntimeCallEvent{CalleeRef: StableSymbol(0x22), Count: 1}}); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	log, err := readLog(path)
	if err != nil {
		t.Fatal(err)
	}
	call := log.Events[0].RuntimeCall
	if call == nil || !log.Events[0].Attribution.Owner.Stable || !call.CalleeRef.Stable ||
		log.Events[0].Attribution.Owner.Namespace != "aabb" || call.CalleeRef.Namespace != "aabb" {
		t.Fatalf("runtime event = %+v", log.Events[0])
	}
}

func TestEmbeddedStableDefinitionsDoNotOverwriteLocalDictionaryIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stable-dictionary-namespace.jhlog")
	writeClosedEvents(t, path, []Event{
		{Type: EventDictionary, Dictionary: &DictionaryEntry{Kind: DictOwner, ID: 1, Value: "local.Owner.call"}},
		{Type: EventDictionary, Dictionary: &DictionaryEntry{Kind: DictStableSymbol, ID: 1, Value: "stable.Owner.call"}},
		{Type: EventHTTP, Attribution: AttributionContext{Present: true, Owner: LocalSymbol(1)}, HTTP: &HTTPEvent{Status: Status2xx}},
	})

	var resolved string
	if _, err := StreamFileWithResult(path, func(event Event, dict map[uint64]string) error {
		if event.HTTP != nil {
			resolved = ResolveSymbol(dict, event.Attribution.Owner)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if resolved != "local.Owner.call" {
		t.Fatalf("local dictionary was polluted by stable definition: %q", resolved)
	}
}

func TestOpenCleanAndOpenWithTailAreStructuredStatuses(t *testing.T) {
	openPath := filepath.Join(t.TempDir(), "open.jhlog")
	var output bytes.Buffer
	writer, err := NewWriter(&output)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteEvent(Event{Type: EventMemory, TimeMS: 1, Memory: &MemoryEvent{PSSKB: 1}}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Flush(); err != nil {
		t.Fatal(err)
	}
	committed := append([]byte(nil), output.Bytes()...)
	if err := os.WriteFile(openPath, committed, 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := StreamFileWithResult(openPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != SegmentStatusOpenClean || result.TailBytes != 0 || result.Events != 1 {
		t.Fatalf("open result = %+v", result)
	}

	tailPath := filepath.Join(t.TempDir(), "tail.jhlog")
	partialHeader := []byte{'J', 'H', 'C', '9', 32, 0, 1}
	withTail := append(committed, partialHeader...)
	if err := os.WriteFile(tailPath, withTail, 0o600); err != nil {
		t.Fatal(err)
	}
	result, err = StreamFileWithResult(tailPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != SegmentStatusOpenWithTail || result.TailBytes != uint64(len(partialHeader)) || result.Events != 1 {
		t.Fatalf("tail result = %+v", result)
	}
}

func TestBytesAfterFinalAreCorruption(t *testing.T) {
	path := filepath.Join(t.TempDir(), "after-final.jhlog")
	writeClosedEvents(t, path, []Event{{Type: EventMemory, Memory: &MemoryEvent{PSSKB: 1}}})
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte{1}); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	result, err := StreamFileWithResult(path, nil)
	if err == nil || result.Status != SegmentStatusCorrupt || !strings.Contains(err.Error(), "after FINAL") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestCommittedPayloadCorruptionIsRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corrupt.jhlog")
	writeClosedEvents(t, path, []Event{{Type: EventMemory, Memory: &MemoryEvent{PSSKB: 1}}})
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	chunkOffset := firstChunkOffset(t, raw)
	storedLength := binary.LittleEndian.Uint32(raw[chunkOffset+12 : chunkOffset+16])
	if storedLength == 0 {
		t.Fatal("empty stored payload")
	}
	raw[chunkOffset+chunkHeaderSize+int(storedLength)/2] ^= 0xff
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := StreamFileWithResult(path, nil)
	if err == nil || result.Status != SegmentStatusCorrupt {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestReaderRejectsOtherBinaryVersions(t *testing.T) {
	for _, version := range []struct {
		name   string
		marker byte
		major  byte
		want   string
	}{
		{name: "unknown marker 0x80", marker: 0x80, major: 2, want: "unsupported .jhlog format"},
		{name: "previous major", marker: 0x81, major: 1, want: "unsupported JHLOG version 1.0.0; expected 3.0.0"},
		{name: "unknown marker", marker: 0x82, major: 2, want: "unsupported .jhlog format"},
	} {
		t.Run(version.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "version.jhlog")
			raw := append([]byte(nil), Magic...)
			raw[7] = version.marker
			raw[8] = version.major
			if err := os.WriteFile(path, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			result, err := StreamFileWithResult(path, nil)
			if err == nil || result.Status != SegmentStatusCorrupt || !strings.Contains(err.Error(), version.want) {
				t.Fatalf("marker=%d major=%d result=%+v err=%v", version.marker, version.major, result, err)
			}
		})
	}
}

func TestWriterRejectsUnsupportedDictionaryEncoding(t *testing.T) {
	var output bytes.Buffer
	writer, err := NewWriter(&output)
	if err != nil {
		t.Fatal(err)
	}
	err = writer.WriteEvent(Event{Type: EventDictionary, Dictionary: &DictionaryEntry{
		Kind: DictOwner, ID: 42, Encoding: 7, Data: []byte{0x01, 0x02},
	}})
	if err == nil || !strings.Contains(err.Error(), "unsupported dictionary encoding 7") {
		t.Fatalf("dictionary encoding error = %v", err)
	}
}

func TestVersionThreeRejectsUnsupportedRecordContracts(t *testing.T) {
	tests := []struct {
		name  string
		event Event
		want  string
	}{
		{
			name:  "event type",
			event: Event{Type: EventType(99)},
			want:  "unsupported event type 99",
		},
		{
			name: "dictionary kind",
			event: Event{Type: EventDictionary, Dictionary: &DictionaryEntry{
				Kind: DictKind(99), ID: 1, Value: "value",
			}},
			want: "unsupported dictionary kind 99",
		},
		{
			name: "quality counter",
			event: Event{Type: EventQualitySnapshot, Quality: &QualitySnapshot{
				Sequence: 1, Counters: map[uint64]uint64{0x7fff: 1},
			}},
			want: "unsupported quality counter id 32767",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			writer, err := NewWriter(&output)
			if err != nil {
				t.Fatal(err)
			}
			err = writer.WriteEvent(test.event)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("WriteEvent() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestVersionThreeWriterRejectsUnknownSegmentEndReason(t *testing.T) {
	var output bytes.Buffer
	writer, err := NewWriter(&output)
	if err != nil {
		t.Fatal(err)
	}
	err = writer.CloseWithReason(99)
	if err == nil || !strings.Contains(err.Error(), "unsupported segment end reason 99") {
		t.Fatalf("CloseWithReason() error = %v", err)
	}
}

func TestVersionThreeDecoderRejectsUnsupportedEventTypes(t *testing.T) {
	for _, eventType := range []EventType{99} {
		event := Event{Type: eventType}
		err := decodeEventPayload(&recordReader{}, &event, DefaultSegmentHeader(), "test", nil)
		want := fmt.Sprintf("unsupported event type %d", eventType)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("event type %d: decodeEventPayload() error = %v", eventType, err)
		}
	}
}

func writeClosedEvents(t *testing.T, path string, events []Event) {
	t.Helper()
	file, writer, err := Create(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if err := writer.WriteEvent(event); err != nil {
			t.Fatal(err)
		}
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func createWithHeaderForTest(path string, header SegmentHeader) (interface{ Close() error }, *Writer, error) {
	return CreateWithHeader(path, header)
}

func firstChunkOffset(t *testing.T, raw []byte) int {
	t.Helper()
	if len(raw) < len(Magic)+8 {
		t.Fatalf("file too short: %d", len(raw))
	}
	headerLength := int(binary.LittleEndian.Uint32(raw[len(Magic) : len(Magic)+4]))
	offset := len(Magic) + 8 + headerLength
	if offset+chunkHeaderSize > len(raw) {
		t.Fatalf("chunk offset %d exceeds file size %d", offset, len(raw))
	}
	return offset
}

func readLog(path string) (Log, error) {
	log := Log{
		Source: path,
		Dict:   map[uint64]string{},
		Kinds:  map[uint64]DictKind{},
	}
	result, err := StreamFileWithResult(path, func(event Event, _ map[uint64]string) error {
		if event.Dictionary != nil && event.Dictionary.Kind != DictStableSymbol {
			log.Dict[event.Dictionary.ID] = event.Dictionary.Value
			log.Kinds[event.Dictionary.ID] = event.Dictionary.Kind
		}
		if event.Type.IsSemanticData() {
			log.Events = append(log.Events, event)
		}
		return nil
	})
	log.Result = result
	return log, err
}
