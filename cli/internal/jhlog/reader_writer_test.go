package jhlog

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteSampleStreamsCommittedV9(t *testing.T) {
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
		t.Fatal("sample v9 log is closed but not FINAL-sealed")
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
		t.Fatalf("known semantic events = %d, data records = %d", log.Result.Events, log.Result.DataRecords)
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
	want := []byte{'J', 'H', 'L', 'O', 'G', '\r', '\n', 9}
	if !bytes.Equal(Magic, want) {
		t.Fatalf("magic = %v, want %v", Magic, want)
	}
	if RequiredFeaturesV9 != 0x7f || OptionalFeaturesV9 != 0x01 {
		t.Fatalf("features = required 0x%x optional 0x%x", RequiredFeaturesV9, OptionalFeaturesV9)
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

func TestLegacyJSONLAccountsRecordClassesWithoutInflatingEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.jhlog")
	raw := strings.Join([]string{
		`{"type":1,"dictionary":{"id":1,"value":"memory.pss"}}`,
		`{"type":7,"time_ms":10,"memory":{"pss_kb":42}}`,
		`{"type":99,"time_ms":11}`,
		`{"type":15,"quality":{"sequence":1}}`,
		`{"type":16,"segment_end":{"reason":0}}`,
	}, "\n")
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}

	callbacks := 0
	result, err := StreamFileWithResult(path, func(Event, map[uint64]string) error {
		callbacks++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Version != 0 || result.Sealed || result.Status != SegmentStatusClosedClean {
		t.Fatalf("legacy stream identity/status = %+v", result)
	}
	if result.TotalRecords != 5 || result.DictionaryRecords != 1 || result.DataRecords != 2 || result.ControlRecords != 2 {
		t.Fatalf("legacy record accounting = %+v", result)
	}
	if result.Events != 1 || callbacks != 2 {
		t.Fatalf("semantic events=%d callbacks=%d, want one event plus one dictionary callback", result.Events, callbacks)
	}
}

func TestRetainedEvidenceRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "retained-evidence.jhlog")
	writeClosedEvents(t, path, []Event{{
		Type: EventRetained,
		Retained: &RetainedEvent{
			ClassID:  1,
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

func TestLegacyRetainedPayloadDefaultsToTimeOnly(t *testing.T) {
	var payload bytes.Buffer
	for _, value := range []uint64{2, 0, 30_000, 1} { // local class id=1, no holder, age, count
		if err := writeUvarint(&payload, value); err != nil {
			t.Fatal(err)
		}
	}
	event := Event{Type: EventRetained}
	known, err := decodeEventPayload(bytes.NewReader(payload.Bytes()), &event, DefaultSegmentHeader())
	if err != nil || !known {
		t.Fatalf("decodeEventPayload() known=%t err=%v", known, err)
	}
	if event.Retained == nil || event.Retained.Evidence != RetentionEvidenceTimeOnly {
		t.Fatalf("legacy retained evidence = %+v", event.Retained)
	}
}

func TestSegmentEndReasonsRoundTripAndRemainForwardCompatible(t *testing.T) {
	if SegmentEndNormal != 0 || SegmentEndSizeLimit != 1 || SegmentEndIOError != 2 || SegmentEndShutdown != 3 {
		t.Fatalf("segment end reason wire values changed: normal=%d size=%d io=%d shutdown=%d", SegmentEndNormal, SegmentEndSizeLimit, SegmentEndIOError, SegmentEndShutdown)
	}
	for _, reason := range []SegmentEndReason{
		SegmentEndNormal,
		SegmentEndSizeLimit,
		SegmentEndIOError,
		SegmentEndShutdown,
		99,
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
}

func TestSizeLimitQualityCountersKeepWireNames(t *testing.T) {
	if QualityEventLostAfterSizeLimitTotal != 17 || QualityLossSizeLimit != 5 {
		t.Fatalf("size-limit quality wire values changed: total=%d reason=%d", QualityEventLostAfterSizeLimitTotal, QualityLossSizeLimit)
	}
	if got := QualityCounterName(QualityEventLostAfterSizeLimitTotal); got != "event_lost_after_size_limit_total" {
		t.Fatalf("QualityCounterName() = %q", got)
	}
}

func TestProfileFilesReportsV9ControlAndEventSizes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.jhlog")
	if err := WriteSample(path); err != nil {
		t.Fatal(err)
	}
	profile, err := ProfileFiles([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if len(profile.Files) != 1 || profile.Files[0].Format != "binary-v9-chunked" || profile.Files[0].Status != SegmentStatusClosedClean {
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
		{Type: EventSession, TimeMS: 1, Session: &SessionEvent{AppVersionID: 1, BuildID: 2, DeviceID: 3, SDKInt: 35, DeviceRooted: true}},
		{Type: EventContext, TimeMS: 2, Flags: uint64(FlagAppForeground), Context: &ContextEvent{Network: NetworkVPN, BatteryPct: 50, AvailMemoryKB: 1024, BatteryState: 3, BatteryTempDeciC: -45, LowMemory: true, NetworkMetered: true, NetworkValidated: true, NetworkVPN: true, RxBytes: 1000, TxBytes: 2000, TotalMemoryKB: 4096, FreeStorageKB: 8192, TotalStorageKB: 16384}},
		{Type: EventGauge, TimeMS: 3, Metric: &MetricEvent{MetricID: 4, Value: 130, Count: 2, Sum: 260, Max: 160, Mode: MetricModeAverage}},
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

func TestAtomicContextAndSameContextRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "context.jhlog")
	context := AttributionContext{Present: true, Screen: LocalSymbol(1), Owner: LocalSymbol(2), Flow: LocalSymbol(3)}
	events := []Event{
		{Type: EventFlow, TimeMS: 10, Attribution: context, Flow: &FlowEvent{}},
		{Type: EventLogSpam, TimeMS: 20, Attribution: context, LogSpam: &LogSpamEvent{SourceID: 4, Level: 5, Count: 9}},
		{Type: EventProblem, TimeMS: 30, Attribution: context, Problem: &ProblemEvent{KindID: 5, WindowMS: 5000, Count: 9, MaxMS: 9}},
	}
	writeClosedEvents(t, path, events)
	log, err := readLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if !log.Events[0].Attribution.Present || log.Events[0].Flags&uint64(FlagSameContext) != 0 {
		t.Fatalf("first context = %+v flags=%x", log.Events[0].Attribution, log.Events[0].Flags)
	}
	for _, event := range log.Events[1:] {
		if event.Flags&uint64(FlagSameContext) == 0 || event.Attribution != context {
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
		if event.Flags&uint64(FlagSameContext) != 0 {
			t.Fatalf("context incorrectly reused across chunks: %+v", event)
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
	if err := writer.WriteEvent(Event{Type: EventRuntimeCall, TimeMS: 1, RuntimeCall: &RuntimeCallEvent{CallerRef: StableSymbol(0x11), CalleeRef: StableSymbol(0x22), Count: 1}}); err != nil {
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
	if call == nil || !call.CallerRef.Stable || !call.CalleeRef.Stable || call.CallerRef.Namespace != "aabb" || call.CalleeRef.Namespace != "aabb" {
		t.Fatalf("runtime symbols = %+v", call)
	}
	if call.CallerID != 0 || call.CalleeID != 0 {
		t.Fatalf("stable symbols leaked into local IDs: %+v", call)
	}
}

func TestEmbeddedStableDefinitionsDoNotOverwriteLocalDictionaryIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stable-dictionary-namespace.jhlog")
	writeClosedEvents(t, path, []Event{
		{Type: EventDictionary, Dictionary: &DictionaryEntry{Kind: DictOwner, ID: 1, Value: "local.Owner.call"}},
		{Type: EventDictionary, Dictionary: &DictionaryEntry{Kind: DictStableSymbol, ID: 1, Value: "stable.Owner.call"}},
		{Type: EventHTTP, HTTP: &HTTPEvent{OwnerID: 1, Status: Status2xx}},
	})

	var resolved string
	if _, err := StreamFileWithResult(path, func(event Event, dict map[uint64]string) error {
		if event.HTTP != nil {
			resolved = Resolve(dict, event.HTTP.OwnerID)
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
	if result.Status != SegmentStatusOpenClean || result.TailBytes != 0 || len(result.Warnings) != 0 || result.Events != 1 {
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
	if result.Status != SegmentStatusOpenWithTail || result.TailBytes != uint64(len(partialHeader)) || len(result.Warnings) != 0 || result.Events != 1 {
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
	for _, version := range []byte{FormatVersion - 1, FormatVersion + 1} {
		path := filepath.Join(t.TempDir(), "version.jhlog")
		raw := append([]byte(nil), Magic...)
		raw[7] = version
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		result, err := StreamFileWithResult(path, nil)
		if err == nil || result.Status != SegmentStatusCorrupt || !strings.Contains(err.Error(), "unsupported jhlog version") {
			t.Fatalf("version=%d result=%+v err=%v", version, result, err)
		}
	}
}

func TestReaderSurfacesUnsupportedDictionaryEncoding(t *testing.T) {
	path := filepath.Join(t.TempDir(), "unsupported-dictionary-encoding.jhlog")
	writeClosedEvents(t, path, []Event{{
		Type: EventDictionary,
		Dictionary: &DictionaryEntry{
			Kind:     DictOwner,
			ID:       42,
			Encoding: 7,
			Data:     []byte{0x01, 0x02},
		},
	}})

	result, err := StreamFileWithResult(path, nil)
	if err != nil {
		t.Fatalf("StreamFileWithResult() error = %v", err)
	}
	if warning := strings.Join(result.Warnings, "\n"); !strings.Contains(warning, "dictionary value 42 uses unsupported encoding 7") {
		t.Fatalf("warnings = %q", warning)
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
		Source:  path,
		Version: FormatVersion,
		Dict:    map[uint64]string{},
		Kinds:   map[uint64]DictKind{},
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
	log.Warnings = result.Warnings
	log.Result = result
	return log, err
}
