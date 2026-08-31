package jhlog

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"hash/crc32"
	"io"
	"math"
	"os"
	"sort"
	"unicode/utf8"
)

type WriterOptions struct {
	Header         SegmentHeader
	RawChunkTarget int
	GZIP           bool
}

type recordEncodeState struct {
	lastElapsedUS int64
	lastContext   AttributionContext
	hasContext    bool
}

type Writer struct {
	w                        io.Writer
	header                   SegmentHeader
	chunkTarget              int
	gzipChunks               bool
	raw                      bytes.Buffer
	recordCount              uint32
	sequence                 uint32
	state                    recordEncodeState
	closed                   bool
	poisoned                 error
	latestQuality            QualitySnapshot
	totalEvents              uint64
	totalDictionary          uint64
	runtimeGraphLogicalCalls uint64
	lastElapsedUS            uint64
	digest                   hash.Hash
	segmentDigest            []byte
}

func NewWriter(w io.Writer) (*Writer, error) {
	return NewWriterWithOptions(w, WriterOptions{
		Header: DefaultSegmentHeader(),
		GZIP:   true,
	})
}

func NewWriterWithHeader(w io.Writer, header SegmentHeader) (*Writer, error) {
	return NewWriterWithOptions(w, WriterOptions{Header: header, GZIP: true})
}

func NewWriterWithOptions(w io.Writer, options WriterOptions) (*Writer, error) {
	if w == nil {
		return nil, fmt.Errorf("jhlog writer is nil")
	}
	headerBytes, header, err := encodeFileHeader(options.Header)
	if err != nil {
		return nil, err
	}
	if header.SegmentStartElapsedUS > math.MaxInt64 {
		return nil, fmt.Errorf("segment start elapsed time %d exceeds signed timestamp range", header.SegmentStartElapsedUS)
	}
	target := options.RawChunkTarget
	if target == 0 {
		target = defaultRawChunkTarget
	}
	if target < 1 || target > maxRawChunkSize {
		return nil, fmt.Errorf("raw chunk target %d is outside 1..%d", target, maxRawChunkSize)
	}
	digest := sha256.New()
	tracked := io.MultiWriter(w, digest)
	if err := writeAll(tracked, headerBytes); err != nil {
		return nil, fmt.Errorf("write file header: %w", err)
	}
	return &Writer{
		w:           tracked,
		header:      header,
		chunkTarget: target,
		gzipChunks:  options.GZIP,
		state: recordEncodeState{
			lastElapsedUS: int64(header.SegmentStartElapsedUS),
		},
		latestQuality: QualitySnapshot{Counters: map[uint64]uint64{}},
		lastElapsedUS: header.SegmentStartElapsedUS,
		digest:        digest,
	}, nil
}

func Create(path string) (io.Closer, *Writer, error) {
	return CreateWithHeader(path, DefaultSegmentHeader())
}

func CreateWithHeader(path string, header SegmentHeader) (io.Closer, *Writer, error) {
	file, err := os.Create(path)
	if err != nil {
		return nil, nil, err
	}
	writer, err := NewWriterWithHeader(file, header)
	if err != nil {
		_ = file.Close()
		return nil, nil, err
	}
	closer := &logFile{file: file, writer: writer}
	return closer, writer, nil
}

type logFile struct {
	file   *os.File
	writer *Writer
	closed bool
}

func (f *logFile) Close() error {
	if f.closed {
		return nil
	}
	f.closed = true
	writerErr := f.writer.Close()
	fileErr := f.file.Close()
	return errors.Join(writerErr, fileErr)
}

func (w *Writer) SetQualitySnapshot(snapshot QualitySnapshot) {
	w.latestQuality = cloneQualitySnapshot(snapshot)
	if w.latestQuality.Counters == nil {
		w.latestQuality.Counters = map[uint64]uint64{}
	}
}

// SegmentDigest returns the exact SHA-256 of a successfully sealed segment.
func (w *Writer) SegmentDigest() []byte {
	return append([]byte(nil), w.segmentDigest...)
}

func (w *Writer) WriteEvent(event Event) error {
	if w.closed {
		return fmt.Errorf("jhlog writer is closed")
	}
	if w.poisoned != nil {
		return w.poisoned
	}
	if event.Type == EventQualitySnapshot {
		if event.Quality == nil {
			return fmt.Errorf("quality snapshot payload is nil")
		}
		for id := range event.Quality.Counters {
			if !IsKnownQualityCounter(id) {
				return fmt.Errorf("unsupported quality counter id %d", id)
			}
		}
		w.SetQualitySnapshot(*event.Quality)
		return nil
	}
	if event.Type == EventSegmentEnd {
		return fmt.Errorf("segment end is reserved for Writer.Close")
	}
	if event.Type == EventRuntimeCall {
		return w.WriteRuntimeCallBlock([]Event{event})
	}
	return w.writeSemanticRecord(event.Type, 1, func(state recordEncodeState) ([]byte, recordEncodeState, uint64, error) {
		return encodeRecord(event, state)
	})
}

// WriteRuntimeCallBlock writes up to MaxRuntimeCallBlockRows observations as one SoA wire record.
func (w *Writer) WriteRuntimeCallBlock(events []Event) error {
	if w.closed {
		return fmt.Errorf("jhlog writer is closed")
	}
	if w.poisoned != nil {
		return w.poisoned
	}
	if len(events) == 0 || len(events) > MaxRuntimeCallBlockRows {
		return fmt.Errorf("runtime call block row count %d is outside 1..%d", len(events), MaxRuntimeCallBlockRows)
	}
	firstElapsedUS, firstHasTime, err := eventElapsedUS(events[0])
	if err != nil {
		return err
	}
	firstHasThread := events[0].Producer.HasThread || events[0].Producer.ThreadID != 0
	calls := make([]runtimeCallRow, len(events))
	logicalCalls := uint64(0)
	for index := range events {
		event := events[index]
		if event.Type != EventRuntimeCall || event.RuntimeCall == nil {
			return fmt.Errorf("runtime call block row %d is not a runtime call", index)
		}
		elapsedUS, hasTime, err := eventElapsedUS(event)
		if err != nil {
			return err
		}
		hasThread := event.Producer.HasThread || event.Producer.ThreadID != 0
		if hasTime != firstHasTime || elapsedUS != firstElapsedUS || hasThread != firstHasThread ||
			event.Producer.ThreadID != events[0].Producer.ThreadID || eventAttributes(event) != eventAttributes(events[0]) {
			return fmt.Errorf("runtime call block row %d has different producer metadata", index)
		}
		if event.RuntimeCall.Count == 0 {
			return fmt.Errorf("runtime call block row %d has zero logical calls", index)
		}
		if event.RuntimeCall.MaxMS > event.RuntimeCall.TotalMS {
			return fmt.Errorf(
				"runtime call block row %d max duration %d exceeds total %d",
				index,
				event.RuntimeCall.MaxMS,
				event.RuntimeCall.TotalMS,
			)
		}
		if math.MaxUint64-logicalCalls < event.RuntimeCall.Count {
			return fmt.Errorf("runtime call block logical call count overflows uint64")
		}
		logicalCalls += event.RuntimeCall.Count
		context := eventAttribution(event)
		calls[index] = runtimeCallRow{
			screen:      context.Screen,
			caller:      context.Owner,
			operationID: context.OperationID,
			callee:      event.RuntimeCall.CalleeRef,
			count:       event.RuntimeCall.Count,
			total:       event.RuntimeCall.TotalMS,
			max:         event.RuntimeCall.MaxMS,
		}
	}
	synthetic := events[0]
	synthetic.runtimeCalls = calls
	if math.MaxUint64-w.runtimeGraphLogicalCalls < logicalCalls {
		return fmt.Errorf("runtime graph logical call total overflows uint64")
	}
	err = w.writeSemanticRecord(EventRuntimeCall, uint64(len(events)), func(state recordEncodeState) ([]byte, recordEncodeState, uint64, error) {
		return encodeRecord(synthetic, state)
	})
	if err == nil {
		w.runtimeGraphLogicalCalls += logicalCalls
	}
	return err
}

func (w *Writer) writeSemanticRecord(
	eventType EventType,
	semanticCount uint64,
	encode func(recordEncodeState) ([]byte, recordEncodeState, uint64, error),
) error {
	record, nextState, elapsedUS, err := encode(w.state)
	if err != nil {
		return err
	}
	if len(record) > maxRawChunkSize {
		w.incrementQuality(QualityOversizedRecordTotal, 1)
		w.incrementQuality(EventQualityCounterID(eventType, QualityLossOversized), semanticCount)
		return fmt.Errorf("event type %d record is too large: %d > %d", eventType, len(record), maxRawChunkSize)
	}
	if w.raw.Len() > 0 && w.raw.Len()+len(record) > w.chunkTarget {
		if err := w.Flush(); err != nil {
			return err
		}
		record, nextState, elapsedUS, err = encode(w.state)
		if err != nil {
			return err
		}
	}
	if w.raw.Len()+len(record) > maxRawChunkSize {
		if w.raw.Len() > 0 {
			if err := w.Flush(); err != nil {
				return err
			}
			record, nextState, elapsedUS, err = encode(w.state)
			if err != nil {
				return err
			}
		}
		if len(record) > maxRawChunkSize {
			w.incrementQuality(QualityOversizedRecordTotal, 1)
			w.incrementQuality(EventQualityCounterID(eventType, QualityLossOversized), semanticCount)
			return fmt.Errorf("event type %d record is too large: %d > %d", eventType, len(record), maxRawChunkSize)
		}
	}
	if _, err := w.raw.Write(record); err != nil {
		return err
	}
	w.recordCount++
	w.state = nextState
	w.lastElapsedUS = elapsedUS
	if eventType == EventDictionary {
		w.totalDictionary += semanticCount
	} else if eventType.IsSemanticData() {
		w.totalEvents += semanticCount
		w.incrementQuality(QualityAcceptedEventTotal, semanticCount)
		w.incrementQuality(QualityWrittenEventTotal, semanticCount)
	}
	return nil
}

func (w *Writer) Flush() error {
	if w.closed {
		return fmt.Errorf("jhlog writer is closed")
	}
	if w.poisoned != nil {
		return w.poisoned
	}
	if w.raw.Len() == 0 {
		return nil
	}
	raw := append([]byte(nil), w.raw.Bytes()...)
	if err := w.commitChunk(raw, w.recordCount, false); err != nil {
		return err
	}
	w.raw.Reset()
	w.recordCount = 0
	w.resetChunkState()
	return nil
}

func (w *Writer) Close() error {
	return w.CloseWithReason(SegmentEndNormal)
}

func (w *Writer) CloseWithReason(reason SegmentEndReason) error {
	if w.closed {
		return w.poisoned
	}
	if w.poisoned != nil {
		w.closed = true
		return w.poisoned
	}
	if !reason.supported() {
		return fmt.Errorf("unsupported segment end reason %d", reason)
	}
	if err := w.Flush(); err != nil {
		w.closed = true
		return err
	}

	quality := cloneQualitySnapshot(w.latestQuality)
	if quality.Sequence == 0 {
		quality.Sequence = 1
	}
	if quality.CapturedElapsedUS == 0 {
		quality.CapturedElapsedUS = w.lastElapsedUS
	}
	if quality.Counters == nil {
		quality.Counters = map[uint64]uint64{}
	}
	quality.Counters[QualityAcceptedEventTotal] = maxUint64(quality.Counters[QualityAcceptedEventTotal], w.totalEvents)
	quality.Counters[QualityWrittenEventTotal] = maxUint64(quality.Counters[QualityWrittenEventTotal], w.totalEvents)
	quality.Counters[QualityRuntimeGraphInputTotal] = maxUint64(
		quality.Counters[QualityRuntimeGraphInputTotal],
		w.runtimeGraphLogicalCalls,
	)
	quality.Counters[QualityRuntimeGraphEmittedTotal] = w.runtimeGraphLogicalCalls
	// A terminal snapshot is observable only after its FINAL chunk commits, so
	// it can truthfully include that chunk before the bytes are encoded.
	committedBeforeFinal := maxUint64(quality.Counters[QualityCommittedChunkTotal], uint64(w.sequence))
	if committedBeforeFinal == math.MaxUint64 {
		w.closed = true
		return fmt.Errorf("committed chunk quality counter overflow")
	}
	quality.Counters[QualityCommittedChunkTotal] = committedBeforeFinal + 1

	end := SegmentEndEvent{
		Reason:                 reason,
		TotalEventRecords:      w.totalEvents,
		TotalDictionaryRecords: w.totalDictionary,
		LastQualitySequence:    quality.Sequence,
	}
	state := recordEncodeState{lastElapsedUS: int64(w.header.SegmentStartElapsedUS)}
	qualityRecord, state, _, err := encodeRecord(Event{Type: EventQualitySnapshot, Quality: &quality}, state)
	if err != nil {
		w.closed = true
		return err
	}
	endRecord, _, _, err := encodeRecord(Event{Type: EventSegmentEnd, SegmentEnd: &end}, state)
	if err != nil {
		w.closed = true
		return err
	}
	finalRaw := make([]byte, 0, len(qualityRecord)+len(endRecord))
	finalRaw = append(finalRaw, qualityRecord...)
	finalRaw = append(finalRaw, endRecord...)
	if len(finalRaw) > maxRawChunkSize {
		w.closed = true
		return fmt.Errorf("final control chunk is too large: %d", len(finalRaw))
	}
	if err := w.commitChunk(finalRaw, 2, true); err != nil {
		w.closed = true
		return err
	}
	w.latestQuality = quality
	w.segmentDigest = w.digest.Sum(nil)
	w.closed = true
	return nil
}

func (w *Writer) commitChunk(raw []byte, recordCount uint32, final bool) error {
	if len(raw) > maxRawChunkSize {
		return fmt.Errorf("raw chunk is too large: %d", len(raw))
	}
	stored := raw
	flags := uint16(0)
	if w.gzipChunks {
		var err error
		stored, err = compressChunk(raw)
		if err != nil {
			return fmt.Errorf("compress chunk %d: %w", w.sequence, err)
		}
		flags |= chunkFlagGZIP
	}
	if final {
		flags |= chunkFlagFinal
	}
	if len(stored) > maxStoredChunkSize {
		return fmt.Errorf("stored chunk is too large: %d", len(stored))
	}
	metadata := chunkMetadata{
		Flags:       flags,
		Sequence:    w.sequence,
		StoredLen:   uint32(len(stored)),
		RawLen:      uint32(len(raw)),
		RecordCount: recordCount,
		RawCRC:      crc32.ChecksumIEEE(raw),
	}
	header := marshalChunkHeader(metadata)
	trailer := marshalCommitTrailer(metadata)
	for _, part := range [][]byte{header[:], stored, trailer[:]} {
		if err := writeAll(w.w, part); err != nil {
			w.incrementQuality(QualityWriterIOErrorTotal, 1)
			w.incrementQuality(QualityFailedChunkTotal, 1)
			w.poisoned = fmt.Errorf("write chunk %d: %w", w.sequence, err)
			return w.poisoned
		}
	}
	w.sequence++
	w.incrementQuality(QualityCommittedChunkTotal, 1)
	return nil
}

func (w *Writer) resetChunkState() {
	w.state = recordEncodeState{lastElapsedUS: int64(w.header.SegmentStartElapsedUS)}
}

func (w *Writer) incrementQuality(id, delta uint64) {
	if w.latestQuality.Counters == nil {
		w.latestQuality.Counters = map[uint64]uint64{}
	}
	w.latestQuality.Counters[id] += delta
}

func cloneQualitySnapshot(snapshot QualitySnapshot) QualitySnapshot {
	clone := snapshot
	clone.Counters = make(map[uint64]uint64, len(snapshot.Counters))
	for id, value := range snapshot.Counters {
		clone.Counters[id] = value
	}
	return clone
}

func maxUint64(a, b uint64) uint64 {
	if a > b {
		return a
	}
	return b
}

func encodeRecord(event Event, state recordEncodeState) ([]byte, recordEncodeState, uint64, error) {
	if event.Type == 0 {
		return nil, state, 0, fmt.Errorf("event type is zero")
	}
	context := eventAttribution(event)
	if event.Type == EventRuntimeCall {
		// Runtime calls carry per-row attribution inside their columnar payload.
		context = AttributionContext{}
	}
	attributes := eventAttributes(event)
	elapsedUS, hasTime, err := eventElapsedUS(event)
	if err != nil {
		return nil, state, 0, err
	}
	hasThread := event.Producer.HasThread || event.Producer.ThreadID != 0

	envelope := EnvelopeFlag(0)
	if hasTime {
		envelope |= EnvelopeHasTime
	}
	if hasThread {
		envelope |= EnvelopeHasThread
	}
	if context.Present {
		envelope |= EnvelopeHasContext
		if state.hasContext && equalAttribution(state.lastContext, context) {
			envelope |= EnvelopeSameContext
		}
	}
	if attributes != 0 {
		envelope |= EnvelopeHasAttributes
	}

	nextState := state
	var body bytes.Buffer
	if err := writeUvarint(&body, uint64(event.Type)); err != nil {
		return nil, state, 0, err
	}
	if err := writeUvarint(&body, uint64(envelope)); err != nil {
		return nil, state, 0, err
	}
	if hasTime {
		if elapsedUS > math.MaxInt64 {
			return nil, state, 0, fmt.Errorf("producer elapsed time %d exceeds signed timestamp range", elapsedUS)
		}
		delta := int64(elapsedUS) - state.lastElapsedUS
		if err := writeUvarint(&body, encodeSVarint(delta)); err != nil {
			return nil, state, 0, err
		}
		nextState.lastElapsedUS = int64(elapsedUS)
	} else {
		elapsedUS = uint64(state.lastElapsedUS)
	}
	if hasThread {
		if err := writeUvarint(&body, event.Producer.ThreadID); err != nil {
			return nil, state, 0, err
		}
	}
	if context.Present && envelope&EnvelopeSameContext == 0 {
		if err := writeAttribution(&body, context); err != nil {
			return nil, state, 0, err
		}
		nextState.lastContext = context
		nextState.hasContext = true
	}
	if attributes != 0 {
		if err := writeUvarint(&body, attributes); err != nil {
			return nil, state, 0, err
		}
	}
	if err := encodeEventPayload(&body, event); err != nil {
		return nil, state, 0, err
	}
	var record bytes.Buffer
	if err := writeUvarint(&record, uint64(body.Len())); err != nil {
		return nil, state, 0, err
	}
	if _, err := record.Write(body.Bytes()); err != nil {
		return nil, state, 0, err
	}
	return record.Bytes(), nextState, elapsedUS, nil
}

func eventElapsedUS(event Event) (uint64, bool, error) {
	if event.Producer.HasTime || event.Producer.ElapsedUS != 0 {
		return event.Producer.ElapsedUS, true, nil
	}
	if event.TimeUS != 0 {
		return event.TimeUS, true, nil
	}
	if event.TimeMS != 0 {
		if event.TimeMS > math.MaxUint64/1000 {
			return 0, false, fmt.Errorf("event time milliseconds overflows microseconds: %d", event.TimeMS)
		}
		return event.TimeMS * 1000, true, nil
	}
	return 0, false, nil
}

func eventAttributes(event Event) uint64 {
	attributes := event.Flags & semanticAttributeMask
	if event.Context != nil {
		if event.Context.LowMemory {
			attributes |= uint64(FlagContextLowMemory)
		}
		if event.Context.NetworkMetered {
			attributes |= uint64(FlagNetworkMetered)
		}
		if event.Context.NetworkValidated {
			attributes |= uint64(FlagNetworkValidated)
		}
		if event.Context.NetworkVPN {
			attributes |= uint64(FlagNetworkVPN)
		}
	}
	if event.Session != nil && event.Session.DeviceRooted {
		attributes |= uint64(FlagDeviceRooted)
	}
	return attributes
}

func eventAttribution(event Event) AttributionContext {
	return event.Attribution
}

func equalAttribution(a, b AttributionContext) bool {
	return a.Screen == b.Screen && a.Owner == b.Owner && a.OperationID == b.OperationID
}

func writeAttribution(w io.Writer, context AttributionContext) error {
	var mask uint64
	refs := []struct {
		bit uint64
		ref SymbolRef
	}{
		{1 << 0, context.Screen},
		{1 << 1, context.Owner},
	}
	for _, item := range refs {
		if !item.ref.IsUnknown() {
			mask |= item.bit
		}
	}
	if context.OperationID != 0 {
		mask |= 1 << 2
	}
	if err := writeUvarint(w, mask); err != nil {
		return err
	}
	for _, item := range refs {
		if mask&item.bit == 0 {
			continue
		}
		if err := writeSymbolRef(w, item.ref); err != nil {
			return err
		}
	}
	if mask&(1<<2) != 0 {
		if err := writeUvarint(w, context.OperationID); err != nil {
			return err
		}
	}
	return nil
}

func writeSymbolRef(w io.Writer, ref SymbolRef) error {
	if ref.Stable {
		if err := writeUvarint(w, 1); err != nil {
			return err
		}
		var raw [8]byte
		binary.LittleEndian.PutUint64(raw[:], ref.ID)
		return writeAll(w, raw[:])
	}
	if ref.ID > math.MaxUint64>>1 {
		return fmt.Errorf("local symbol id %d is too large", ref.ID)
	}
	return writeUvarint(w, ref.ID<<1)
}

func encodeEventPayload(w io.Writer, event Event) error {
	writeValues := func(values ...uint64) error {
		for _, value := range values {
			if err := writeUvarint(w, value); err != nil {
				return err
			}
		}
		return nil
	}
	writeRefs := func(refs ...SymbolRef) error {
		for _, ref := range refs {
			if err := writeSymbolRef(w, ref); err != nil {
				return err
			}
		}
		return nil
	}

	switch event.Type {
	case EventDictionary:
		p := event.Dictionary
		if p == nil {
			return fmt.Errorf("dictionary payload is nil")
		}
		if p.Kind > DictAttributeValue {
			return fmt.Errorf("unsupported dictionary kind %d", p.Kind)
		}
		if p.Encoding != 0 {
			return fmt.Errorf("unsupported dictionary encoding %d", p.Encoding)
		}
		data := p.Data
		if data == nil {
			data = []byte(p.Value)
		}
		if p.Encoding == 0 && !utf8.Valid(data) {
			return fmt.Errorf("dictionary value %d is not valid UTF-8", p.ID)
		}
		if err := writeValues(uint64(p.Kind), p.ID, p.Encoding, uint64(len(data))); err != nil {
			return err
		}
		return writeAll(w, data)
	case EventSession:
		p := event.Session
		if p == nil {
			return fmt.Errorf("session payload is nil")
		}
		if p.CollectorFlags&^uint64(CollectorKnownMask) != 0 {
			return fmt.Errorf("unsupported collector flags 0x%x", p.CollectorFlags)
		}
		if err := writeRefs(
			p.AppVersionRef,
			p.BuildRef,
			p.DeviceRef,
		); err != nil {
			return err
		}
		if err := writeValues(p.SDKInt); err != nil {
			return err
		}
		if err := writeRefs(
			p.AndroidReleaseRef,
			p.SecurityPatchRef,
			p.PrimaryABIRef,
			p.SupportedABIsRef,
			p.ManufacturerRef,
			p.BrandRef,
			p.HardwareRef,
			p.BoardRef,
			p.ProductRef,
		); err != nil {
			return err
		}
		return writeValues(p.CollectorFlags)
	case EventContext:
		p := event.Context
		if p == nil {
			return fmt.Errorf("device context payload is nil")
		}
		return writeValues(
			uint64(p.Network), p.BatteryPct, p.AvailMemoryKB, p.BatteryState,
			encodeSVarint(p.BatteryTempDeciC), p.RxBytes, p.TxBytes,
			p.TotalMemoryKB, p.FreeStorageKB, p.TotalStorageKB,
		)
	case EventHTTP:
		p := event.HTTP
		if p == nil {
			return fmt.Errorf("http payload is nil")
		}
		statusCode := effectiveHTTPStatusCode(p)
		if err := validateHTTPEvent(p, statusCode); err != nil {
			return err
		}
		if err := writeRefs(p.RouteRef, p.ServiceRef, p.InitiatorRef); err != nil {
			return err
		}
		return writeValues(
			p.DurationMS, p.QueueMS, p.DNSMS, p.ConnectMS, p.TLSMS, p.RequestMS, p.TTFBMS, p.ResponseMS,
			uint64(statusCode), uint64(p.FailurePhase), uint64(p.FailureKind), uint64(p.Protocol),
			p.RxBytes, p.TxBytes, uint64(p.Attempts), uint64(p.DNSAttempts), uint64(p.ConnectAttempts),
			uint64(p.TLSAttempts), uint64(p.ConnectFailures), uint64(p.TLSFailures), uint64(p.Redirects),
		)
	case EventUIWindow:
		p := event.UIWindow
		if p == nil {
			return fmt.Errorf("ui window payload is nil")
		}
		if err := validateUIWindow(p); err != nil {
			return err
		}
		if err := writeValues(p.WindowMS, p.FrameCount, p.JankCount, uint64(p.Source), p.FrameDeadlineUS); err != nil {
			return err
		}
		return writeValues(p.FrameDurationBuckets...)
	case EventStall:
		p := event.Stall
		if p == nil {
			return fmt.Errorf("stall payload is nil")
		}
		if err := writeSymbolRef(w, p.StackRef); err != nil {
			return err
		}
		return writeValues(p.DurationMS)
	case EventMemory:
		p := event.Memory
		if p == nil {
			return fmt.Errorf("memory payload is nil")
		}
		return writeValues(p.PSSKB, p.JavaHeapKB, p.NativeHeapKB)
	case EventRetained:
		p := event.Retained
		if p == nil {
			return fmt.Errorf("retained payload is nil")
		}
		if err := writeRefs(p.ClassRef, p.HolderRef); err != nil {
			return err
		}
		return writeValues(p.AgeMS, p.Count, uint64(p.Evidence.Effective()))
	case EventCounter, EventGauge:
		p := event.Metric
		if p == nil {
			return fmt.Errorf("metric payload is nil")
		}
		count := p.Count
		if count == 0 {
			count = 1
		}
		sum := p.Sum
		if sum == 0 {
			sum = p.Value
		}
		max := p.Max
		if max == 0 {
			max = p.Value
		}
		if err := writeSymbolRef(w, p.MetricRef); err != nil {
			return err
		}
		return writeValues(p.Value, count, sum, max, uint64(p.Mode))
	case EventOperation:
		p := event.Operation
		if p == nil {
			return fmt.Errorf("operation payload is nil")
		}
		if err := validateOperationEvent(p); err != nil {
			return err
		}
		if err := writeSymbolRef(w, p.NameRef); err != nil {
			return err
		}
		if err := writeValues(
			p.ID,
			p.ParentID,
			uint64(p.Phase),
			uint64(p.Kind),
			uint64(p.Outcome),
			p.DurationUS,
			p.BudgetUS,
			uint64(len(p.Attributes)),
		); err != nil {
			return err
		}
		for _, attribute := range p.Attributes {
			if err := writeSymbolRef(w, attribute.KeyRef); err != nil {
				return err
			}
			if err := writeSymbolRef(w, attribute.ValueRef); err != nil {
				return err
			}
		}
		return nil
	case EventLogSpam:
		p := event.LogSpam
		if p == nil {
			return fmt.Errorf("log spam payload is nil")
		}
		if err := writeSymbolRef(w, p.SourceRef); err != nil {
			return err
		}
		return writeValues(p.Level, p.Count)
	case EventProblem:
		p := event.Problem
		if p == nil {
			return fmt.Errorf("problem payload is nil")
		}
		if err := writeSymbolRef(w, p.KindRef); err != nil {
			return err
		}
		return writeValues(p.WindowMS, p.Count, p.MaxMS)
	case EventRuntimeCall:
		p := event.RuntimeCall
		if p == nil {
			return fmt.Errorf("runtime call payload is nil")
		}
		calls := event.runtimeCalls
		if len(calls) == 0 {
			context := eventAttribution(event)
			calls = []runtimeCallRow{{
				screen:      context.Screen,
				caller:      context.Owner,
				operationID: context.OperationID,
				callee:      p.CalleeRef,
				count:       p.Count,
				total:       p.TotalMS,
				max:         p.MaxMS,
			}}
		}
		if len(calls) > MaxRuntimeCallBlockRows {
			return fmt.Errorf("runtime call block row count %d exceeds %d", len(calls), MaxRuntimeCallBlockRows)
		}
		if err := writeValues(uint64(len(calls))); err != nil {
			return err
		}
		for index := range calls {
			if err := writeSymbolRef(w, calls[index].screen); err != nil {
				return err
			}
		}
		for index := range calls {
			if err := writeSymbolRef(w, calls[index].caller); err != nil {
				return err
			}
		}
		for index := range calls {
			if err := writeValues(calls[index].operationID); err != nil {
				return err
			}
		}
		for index := range calls {
			if err := writeSymbolRef(w, calls[index].callee); err != nil {
				return err
			}
		}
		for index := range calls {
			if err := writeValues(calls[index].count); err != nil {
				return err
			}
		}
		for index := range calls {
			if err := writeValues(calls[index].total); err != nil {
				return err
			}
		}
		for index := range calls {
			if err := writeValues(calls[index].max); err != nil {
				return err
			}
		}
		return nil
	case EventProcessExit:
		p := event.ProcessExit
		if p == nil {
			return fmt.Errorf("process exit payload is nil")
		}
		if p.TimestampUnixMS == 0 || p.TimestampUnixMS > math.MaxInt64 {
			return fmt.Errorf("process exit timestamp must be positive")
		}
		if err := writeValues(p.Reason, p.TimestampUnixMS, p.Importance, p.PSSKB, p.RSSKB); err != nil {
			return err
		}
		return writeSymbolRef(w, p.ProcessRef)
	case EventIO:
		p := event.IO
		if p == nil {
			return fmt.Errorf("I/O payload is nil")
		}
		if err := validateIOEvent(p, event.Flags); err != nil {
			return err
		}
		if err := writeSymbolRef(w, p.SourceRef); err != nil {
			return err
		}
		return writeValues(uint64(p.Operation), uint64(p.Outcome), p.DurationUS, p.Bytes)
	case EventWorker:
		p := event.Worker
		if p == nil {
			return fmt.Errorf("worker payload is nil")
		}
		if err := validateWorkerEvent(p, event.Flags); err != nil {
			return err
		}
		if err := writeSymbolRef(w, p.WorkerRef); err != nil {
			return err
		}
		return writeValues(
			p.InstanceID,
			uint64(p.Stage),
			uint64(p.Outcome),
			p.DurationMS,
			uint64(p.RunAttempt),
			uint64(p.Generation),
			uint64(p.StopReason),
		)
	case EventWebSocket:
		p := event.WebSocket
		if p == nil {
			return fmt.Errorf("WebSocket payload is nil")
		}
		if err := validateWebSocketEvent(p); err != nil {
			return err
		}
		if err := writeSymbolRef(w, p.RouteRef); err != nil {
			return err
		}
		return writeValues(
			p.ConnectionID,
			uint64(p.Stage),
			p.DurationMS,
			uint64(p.StatusCode),
			uint64(p.CloseCode),
			uint64(p.FailureKind),
			p.TextMessages,
			p.BinaryMessages,
			p.ReceivedBytes,
			uint64(p.ReconnectOrdinal),
		)
	case EventDatabase:
		p := event.Database
		if p == nil {
			return fmt.Errorf("database payload is nil")
		}
		if err := validateDatabaseEvent(p); err != nil {
			return err
		}
		if err := writeSymbolRef(w, p.QueryRef); err != nil {
			return err
		}
		if err := writeSymbolRef(w, p.SourceRef); err != nil {
			return err
		}
		return writeValues(
			p.StatementFingerprint,
			uint64(p.Framework),
			uint64(p.Operation),
			uint64(p.Outcome),
			uint64(p.FailureKind),
			uint64(p.Boundary),
			boolUint64(p.ResultKnown),
			uint64(p.ResultKind),
			uint64(p.ResultCountBucket),
			p.TransactionID,
			p.StatementToken,
			uint64(p.PhaseMask),
			p.PoolWaitUS,
			p.LockWaitUS,
			p.ExecuteUS,
			p.MaterializeUS,
			p.DurationUS,
		)
	case EventDatabaseTransaction:
		p := event.DatabaseTransaction
		if p == nil {
			return fmt.Errorf("database transaction payload is nil")
		}
		if err := validateDatabaseTransactionEvent(p); err != nil {
			return err
		}
		if err := writeSymbolRef(w, p.SourceRef); err != nil {
			return err
		}
		return writeValues(
			p.TransactionID,
			p.ParentID,
			uint64(p.Stage),
			uint64(p.Mode),
			uint64(p.Outcome),
			uint64(p.FailureKind),
			p.DurationUS,
			p.StatementCount,
			p.ReadCount,
			p.WriteCount,
		)
	case EventProcessState:
		p := event.ProcessState
		if p == nil {
			return fmt.Errorf("process state payload is nil")
		}
		if err := validateProcessStateEvent(p); err != nil {
			return err
		}
		return writeValues(
			uint64(p.UIVisibility),
			uint64(p.Importance),
			uint64(p.AndroidImportance),
			uint64(p.Reason),
		)
	case EventAndroidComponent:
		p := event.AndroidComponent
		if p == nil {
			return fmt.Errorf("Android component payload is nil")
		}
		if err := validateAndroidComponentEvent(p); err != nil {
			return err
		}
		if err := writeSymbolRef(w, p.ComponentRef); err != nil {
			return err
		}
		if err := writeSymbolRef(w, p.ActionRef); err != nil {
			return err
		}
		return writeValues(
			p.InstanceID,
			p.FlowID,
			uint64(p.Kind),
			uint64(p.Stage),
			uint64(p.Outcome),
			p.DurationUS,
			uint64(p.Flags),
		)
	case EventBinderTransaction:
		p := event.BinderTransaction
		if p == nil {
			return fmt.Errorf("Binder transaction payload is nil")
		}
		if err := validateBinderTransactionEvent(p, event.Flags); err != nil {
			return err
		}
		if err := writeSymbolRef(w, p.DescriptorRef); err != nil {
			return err
		}
		if err := writeSymbolRef(w, p.MethodRef); err != nil {
			return err
		}
		return writeValues(
			p.CallID,
			uint64(p.Direction),
			uint64(p.TransactionCode),
			uint64(p.Outcome),
			uint64(p.FailureKind),
			p.DurationUS,
			uint64(p.Flags),
		)
	case EventQualitySnapshot:
		p := event.Quality
		if p == nil {
			return fmt.Errorf("quality snapshot payload is nil")
		}
		ids := make([]uint64, 0, len(p.Counters))
		for id := range p.Counters {
			if !IsKnownQualityCounter(id) {
				return fmt.Errorf("unsupported quality counter id %d", id)
			}
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		if err := writeValues(p.Sequence, p.CapturedElapsedUS, uint64(len(ids))); err != nil {
			return err
		}
		for _, id := range ids {
			if err := writeValues(id, p.Counters[id]); err != nil {
				return err
			}
		}
		return nil
	case EventSegmentEnd:
		p := event.SegmentEnd
		if p == nil {
			return fmt.Errorf("segment end payload is nil")
		}
		if !p.Reason.supported() {
			return fmt.Errorf("unsupported segment end reason %d", p.Reason)
		}
		return writeValues(uint64(p.Reason), p.TotalEventRecords, p.TotalDictionaryRecords, p.LastQualitySequence)
	case EventLogGrowth:
		p := event.LogGrowth
		if p == nil {
			return fmt.Errorf("log-growth payload is nil")
		}
		if p.Kind != LogGrowthHistory && p.Kind != LogGrowthLive {
			return fmt.Errorf("unsupported log-growth record kind %d", p.Kind)
		}
		if len(p.Raw) == 0 {
			return fmt.Errorf("log-growth raw payload is empty")
		}
		if err := writeValues(uint64(p.Kind), uint64(len(p.Raw))); err != nil {
			return err
		}
		return writeAll(w, p.Raw)
	default:
		return fmt.Errorf("unsupported event type %d", event.Type)
	}
}

func validateOperationEvent(event *OperationEvent) error {
	if event.ID == 0 {
		return fmt.Errorf("operation ID must be non-zero")
	}
	if event.ParentID == event.ID {
		return fmt.Errorf("operation cannot be its own parent")
	}
	if event.NameRef.IsUnknown() {
		return fmt.Errorf("operation requires a name reference")
	}
	if event.Kind <= OperationKindUnknown || event.Kind > OperationKindStage {
		return fmt.Errorf("unsupported operation kind %d", event.Kind)
	}
	switch event.Phase {
	case OperationPhaseStarted:
		if event.Outcome != OperationOutcomeUnknown || event.DurationUS != 0 {
			return fmt.Errorf("started operation cannot have outcome or duration")
		}
	case OperationPhaseFinished:
		if event.Outcome <= OperationOutcomeUnknown || event.Outcome > OperationOutcomeTimeout {
			return fmt.Errorf("finished operation requires a supported outcome")
		}
	default:
		return fmt.Errorf("unsupported operation phase %d", event.Phase)
	}
	if len(event.Attributes) > MaxOperationAttributes {
		return fmt.Errorf("operation attribute count %d exceeds %d", len(event.Attributes), MaxOperationAttributes)
	}
	keys := [MaxOperationAttributes]SymbolRef{}
	for index, attribute := range event.Attributes {
		if attribute.KeyRef.IsUnknown() || attribute.ValueRef.IsUnknown() {
			return fmt.Errorf("operation attribute %d requires key and value references", index)
		}
		for previous := 0; previous < index; previous++ {
			if keys[previous] == attribute.KeyRef {
				return fmt.Errorf("operation attribute %d duplicates a key", index)
			}
		}
		keys[index] = attribute.KeyRef
	}
	return nil
}

func validateIOEvent(event *IOEvent, flags uint64) error {
	supportedOperation := event.Operation >= IOOperationFileRead && event.Operation <= IOOperationFileSync ||
		event.Operation >= IOOperationContentRead && event.Operation <= IOOperationContentWrite
	if !supportedOperation {
		return fmt.Errorf("unsupported I/O operation %d", event.Operation)
	}
	if event.Outcome > IOOutcomeFailure {
		return fmt.Errorf("unsupported I/O outcome %d", event.Outcome)
	}
	if event.Outcome == IOOutcomeUnknown {
		return fmt.Errorf("I/O outcome must be success or failure")
	}
	if event.Bytes > 0 && flags&uint64(FlagIOBytesKnown) == 0 {
		return fmt.Errorf("I/O bytes require bytes-known flag")
	}
	allowed := uint64(FlagThreadMain | FlagAppForeground | FlagIOBytesKnown)
	if unsupported := flags &^ allowed; unsupported != 0 {
		return fmt.Errorf("unsupported semantic flag 0x%x for I/O", unsupported)
	}
	return nil
}

func effectiveHTTPStatusCode(event *HTTPEvent) uint16 {
	if event.StatusCode != 0 {
		return event.StatusCode
	}
	switch event.Status {
	case Status1xx:
		return 100
	case Status2xx:
		return 200
	case Status3xx:
		return 300
	case Status4xx:
		return 400
	case Status5xx:
		return 500
	default:
		return 0
	}
}

func validateHTTPEvent(event *HTTPEvent, statusCode uint16) error {
	if statusCode != 0 && (statusCode < 100 || statusCode > 599) {
		return fmt.Errorf("HTTP status code %d is outside 100..599", statusCode)
	}
	statusClass := StatusClassForHTTPCode(statusCode)
	if event.Status > Status5xx {
		return fmt.Errorf("unsupported HTTP status class %d", event.Status)
	}
	if event.Status != StatusUnknown && event.Status != statusClass {
		return fmt.Errorf("HTTP status class %d conflicts with status code %d", event.Status, statusCode)
	}
	if err := validateHTTPPhase("queue", event.QueueMS, event.DurationMS); err != nil {
		return err
	}
	if err := validateHTTPPhase("DNS", event.DNSMS, event.DurationMS); err != nil {
		return err
	}
	if err := validateHTTPPhase("connect", event.ConnectMS, event.DurationMS); err != nil {
		return err
	}
	if err := validateHTTPPhase("TLS", event.TLSMS, event.DurationMS); err != nil {
		return err
	}
	if err := validateHTTPPhase("request", event.RequestMS, event.DurationMS); err != nil {
		return err
	}
	if err := validateHTTPPhase("TTFB", event.TTFBMS, event.DurationMS); err != nil {
		return err
	}
	if err := validateHTTPPhase("response", event.ResponseMS, event.DurationMS); err != nil {
		return err
	}
	if event.FailurePhase > HTTPFailurePhaseCancelled {
		return fmt.Errorf("unsupported HTTP failure phase %d", event.FailurePhase)
	}
	if event.FailureKind > HTTPFailureKindOther {
		return fmt.Errorf("unsupported HTTP failure kind %d", event.FailureKind)
	}
	if event.Protocol > HTTPProtocol3 {
		return fmt.Errorf("unsupported HTTP protocol %d", event.Protocol)
	}
	if event.ConnectFailures > event.ConnectAttempts {
		return fmt.Errorf(
			"HTTP connect failure count %d exceeds connect attempt count %d",
			event.ConnectFailures,
			event.ConnectAttempts,
		)
	}
	if event.TLSFailures > event.TLSAttempts {
		return fmt.Errorf(
			"HTTP TLS failure count %d exceeds TLS attempt count %d",
			event.TLSFailures,
			event.TLSAttempts,
		)
	}
	if event.Redirects > event.Attempts {
		return fmt.Errorf("HTTP redirect count %d exceeds attempt count %d", event.Redirects, event.Attempts)
	}
	return nil
}

func validateHTTPPhase(name string, phaseDuration, requestDuration uint64) error {
	if phaseDuration > requestDuration {
		return fmt.Errorf("HTTP %s duration %d exceeds request duration %d", name, phaseDuration, requestDuration)
	}
	return nil
}

func validateWorkerEvent(event *WorkerEvent, flags uint64) error {
	if event.InstanceID == 0 {
		return fmt.Errorf("worker instance ID must be non-zero")
	}
	if event.Stage <= WorkerStageUnknown || event.Stage > WorkerStageFinished {
		return fmt.Errorf("unsupported worker stage %d", event.Stage)
	}
	if event.Outcome > WorkerOutcomeCancelled {
		return fmt.Errorf("unsupported worker outcome %d", event.Outcome)
	}
	finished := event.Stage == WorkerStageFinished
	if !finished && event.Outcome != WorkerOutcomeUnknown {
		return fmt.Errorf("worker outcome is only valid for finished stage")
	}
	if !finished && event.DurationMS != 0 {
		return fmt.Errorf("worker duration is only valid for finished stage")
	}
	if finished && event.Outcome == WorkerOutcomeUnknown {
		return fmt.Errorf("finished worker requires an outcome")
	}
	if !finished && flags&uint64(FlagWorkerStopReasonKnown) != 0 {
		return fmt.Errorf("worker stop reason is only valid for finished stage")
	}
	if flags&uint64(FlagWorkerStopReasonKnown) == 0 && event.StopReason != 0 {
		return fmt.Errorf("worker stop reason requires the known flag")
	}
	if event.Stage != WorkerStageEnqueued && event.WorkerRef.ID == 0 {
		return fmt.Errorf("started or finished worker requires a worker reference")
	}
	return nil
}

func validateWebSocketEvent(event *WebSocketEvent) error {
	if event.ConnectionID == 0 {
		return fmt.Errorf("WebSocket connection ID must be non-zero")
	}
	if event.Stage <= WebSocketStageUnknown || event.Stage > WebSocketStageFailed {
		return fmt.Errorf("unsupported WebSocket stage %d", event.Stage)
	}
	if event.StatusCode != 0 && (event.StatusCode < 100 || event.StatusCode > 599) {
		return fmt.Errorf("WebSocket status code %d is outside 100..599", event.StatusCode)
	}
	if event.Stage != WebSocketStageClosed && event.CloseCode != 0 {
		return fmt.Errorf("WebSocket close code is only valid for closed stage")
	}
	if event.CloseCode != 0 && (event.CloseCode < 1000 || event.CloseCode > 4999) {
		return fmt.Errorf("WebSocket close code %d is outside 1000..4999", event.CloseCode)
	}
	if event.Stage != WebSocketStageFailed && event.FailureKind != WebSocketFailureUnknown {
		return fmt.Errorf("WebSocket failure kind is only valid for failed stage")
	}
	if event.Stage == WebSocketStageFailed && event.FailureKind == WebSocketFailureUnknown {
		return fmt.Errorf("failed WebSocket requires a failure kind")
	}
	if event.FailureKind > WebSocketFailureOther {
		return fmt.Errorf("unsupported WebSocket failure kind %d", event.FailureKind)
	}
	if event.Stage == WebSocketStageOpened &&
		(event.TextMessages != 0 || event.BinaryMessages != 0 || event.ReceivedBytes != 0) {
		return fmt.Errorf("WebSocket traffic is only valid for terminal stages")
	}
	return nil
}

func validateDatabaseEvent(event *DatabaseEvent) error {
	if event.SourceRef.ID == 0 {
		return fmt.Errorf("database source is required")
	}
	if event.Framework <= DatabaseFrameworkUnknown || event.Framework > DatabaseFrameworkCustom {
		return fmt.Errorf("unsupported database framework %d", event.Framework)
	}
	if event.Operation <= DatabaseOperationUnknown || event.Operation > DatabaseOperationStatement {
		return fmt.Errorf("unsupported database operation %d", event.Operation)
	}
	if event.Outcome <= DatabaseOutcomeUnknown || event.Outcome > DatabaseOutcomeFailure {
		return fmt.Errorf("unsupported database outcome %d", event.Outcome)
	}
	if event.Boundary <= DatabaseBoundaryUnknown || event.Boundary > DatabaseBoundaryManual {
		return fmt.Errorf("database boundary is required and must be supported, got %d", event.Boundary)
	}
	if event.FailureKind > DatabaseFailureOther {
		return fmt.Errorf("unsupported database failure kind %d", event.FailureKind)
	}
	if event.Outcome == DatabaseOutcomeSuccess && event.FailureKind != DatabaseFailureNone {
		return fmt.Errorf("successful database call cannot have failure kind")
	}
	if event.Outcome == DatabaseOutcomeFailure && event.FailureKind == DatabaseFailureNone {
		return fmt.Errorf("failed database call requires failure kind")
	}
	if !event.ResultKnown {
		if event.ResultKind != DatabaseResultUnknown || event.ResultCountBucket != DatabaseCountUnknown {
			return fmt.Errorf("database result fields require known flag")
		}
	} else {
		if event.ResultKind <= DatabaseResultUnknown || event.ResultKind > DatabaseResultAffectedRows {
			return fmt.Errorf("known database result requires supported kind")
		}
		if event.ResultCountBucket <= DatabaseCountUnknown || event.ResultCountBucket > DatabaseCountOverHundred {
			return fmt.Errorf("known database result requires count bucket")
		}
	}
	if event.PhaseMask & ^(DatabasePhasePoolWait|DatabasePhaseLockWait|DatabasePhaseExecute|DatabasePhaseMaterialize) != 0 {
		return fmt.Errorf("unsupported database phase mask 0x%x", event.PhaseMask)
	}
	phaseValues := []struct {
		bit   DatabasePhase
		value uint64
	}{
		{DatabasePhasePoolWait, event.PoolWaitUS},
		{DatabasePhaseLockWait, event.LockWaitUS},
		{DatabasePhaseExecute, event.ExecuteUS},
		{DatabasePhaseMaterialize, event.MaterializeUS},
	}
	remaining := event.DurationUS
	for _, phase := range phaseValues {
		if event.PhaseMask&phase.bit == 0 && phase.value != 0 {
			return fmt.Errorf("database phase value %d is present without mask 0x%x", phase.value, phase.bit)
		}
		if phase.value > remaining {
			return fmt.Errorf("database phases exceed total duration")
		}
		remaining -= phase.value
	}
	return nil
}

func validateDatabaseTransactionEvent(event *DatabaseTransactionEvent) error {
	if event.SourceRef.ID == 0 {
		return fmt.Errorf("database transaction source is required")
	}
	if event.TransactionID == 0 {
		return fmt.Errorf("database transaction ID is required")
	}
	if event.ParentID == event.TransactionID {
		return fmt.Errorf("database transaction cannot be its own parent")
	}
	if event.Stage <= DatabaseTransactionStageUnknown || event.Stage > DatabaseTransactionTerminal {
		return fmt.Errorf("unsupported database transaction stage %d", event.Stage)
	}
	if event.Mode > DatabaseTransactionReadOnly {
		return fmt.Errorf("unsupported database transaction mode %d", event.Mode)
	}
	if event.FailureKind > DatabaseFailureOther {
		return fmt.Errorf("unsupported database transaction failure kind %d", event.FailureKind)
	}
	if event.Stage == DatabaseTransactionBegin {
		if event.Outcome != DatabaseTransactionOutcomeUnknown || event.FailureKind != DatabaseFailureNone ||
			event.DurationUS != 0 || event.StatementCount != 0 || event.ReadCount != 0 || event.WriteCount != 0 {
			return fmt.Errorf("database transaction begin cannot contain terminal fields")
		}
		return nil
	}
	if event.Outcome <= DatabaseTransactionOutcomeUnknown || event.Outcome > DatabaseTransactionFailure {
		return fmt.Errorf("database transaction terminal requires supported outcome")
	}
	if event.Outcome == DatabaseTransactionFailure && event.FailureKind == DatabaseFailureNone {
		return fmt.Errorf("failed database transaction requires failure kind")
	}
	if event.Outcome != DatabaseTransactionFailure && event.FailureKind != DatabaseFailureNone {
		return fmt.Errorf("non-failed database transaction cannot have failure kind")
	}
	if event.ReadCount > event.StatementCount || event.WriteCount > event.StatementCount-event.ReadCount {
		return fmt.Errorf("database transaction read/write counts exceed statement count")
	}
	return nil
}

func validateProcessStateEvent(event *ProcessStateEvent) error {
	if event.UIVisibility > ProcessUIVisible {
		return fmt.Errorf("unsupported process UI visibility %d", event.UIVisibility)
	}
	if event.Importance > ProcessImportanceCached {
		return fmt.Errorf("unsupported process importance %d", event.Importance)
	}
	if event.Reason < ProcessStateReasonPeriodicSample || event.Reason > ProcessStateReasonComponentLifecycle {
		return fmt.Errorf("unsupported process state reason %d", event.Reason)
	}
	return nil
}

func validateAndroidComponentEvent(event *AndroidComponentEvent) error {
	if event.ComponentRef.ID == 0 {
		return fmt.Errorf("Android component reference is required")
	}
	if event.InstanceID == 0 || event.FlowID == 0 {
		return fmt.Errorf("Android component instance and flow IDs must be non-zero")
	}
	if !validComponentStage(event.Kind, event.Stage) {
		return fmt.Errorf("unsupported Android component kind/stage %d/%d", event.Kind, event.Stage)
	}
	if event.Outcome > ComponentOutcomeCancelled {
		return fmt.Errorf("unsupported Android component outcome %d", event.Outcome)
	}
	if unsupported := event.Flags &^ componentFlagKnownMask; unsupported != 0 {
		return fmt.Errorf("unsupported Android component flags 0x%x", unsupported)
	}
	return nil
}

func validComponentStage(kind ComponentKind, stage ComponentStage) bool {
	switch kind {
	case ComponentKindService:
		return stage >= ComponentServiceCreated && stage <= ComponentServiceTimeout
	case ComponentKindReceiver:
		return stage >= ComponentReceiverStarted && stage <= ComponentReceiverFinished
	default:
		return false
	}
}

func validateBinderTransactionEvent(event *BinderTransactionEvent, semanticFlags uint64) error {
	if event.CallID == 0 {
		return fmt.Errorf("Binder transaction call ID must be non-zero")
	}
	if event.Direction < BinderDirectionClient || event.Direction > BinderDirectionServer {
		return fmt.Errorf("unsupported Binder direction %d", event.Direction)
	}
	if event.Outcome < BinderOutcomeSuccess || event.Outcome > BinderOutcomeUnhandled {
		return fmt.Errorf("unsupported Binder outcome %d", event.Outcome)
	}
	if event.FailureKind > BinderFailureOther {
		return fmt.Errorf("unsupported Binder failure kind %d", event.FailureKind)
	}
	if event.Outcome == BinderOutcomeFailure && event.FailureKind == BinderFailureNone {
		return fmt.Errorf("failed Binder transaction requires failure kind")
	}
	if event.Outcome != BinderOutcomeFailure && event.FailureKind != BinderFailureNone {
		return fmt.Errorf("non-failed Binder transaction cannot have failure kind")
	}
	if unsupported := event.Flags &^ binderFlagKnownMask; unsupported != 0 {
		return fmt.Errorf("unsupported Binder flags 0x%x", unsupported)
	}
	allowedSemanticFlags := uint64(FlagThreadMain | FlagAppForeground)
	if unsupported := semanticFlags &^ allowedSemanticFlags; unsupported != 0 {
		return fmt.Errorf("unsupported semantic flags 0x%x for Binder", unsupported)
	}
	return nil
}

func boolUint64(value bool) uint64 {
	if value {
		return 1
	}
	return 0
}

func validateUIWindow(window *UIWindowEvent) error {
	if window.WindowMS == 0 {
		return fmt.Errorf("UI window duration must be positive")
	}
	if window.JankCount > window.FrameCount {
		return fmt.Errorf("UI jank count %d exceeds frame count %d", window.JankCount, window.FrameCount)
	}
	if window.Source <= UIFrameSourceUnknown || window.Source > UIFrameSourceChoreographer {
		return fmt.Errorf("unsupported UI frame source %d", window.Source)
	}
	if window.FrameDeadlineUS == 0 {
		return fmt.Errorf("UI frame deadline must be positive")
	}
	if len(window.FrameDurationBuckets) != UIFrameHistogramBucketCount {
		return fmt.Errorf("UI frame histogram has %d buckets; want %d", len(window.FrameDurationBuckets), UIFrameHistogramBucketCount)
	}
	var total uint64
	for _, count := range window.FrameDurationBuckets {
		if math.MaxUint64-total < count {
			return fmt.Errorf("UI frame histogram count overflow")
		}
		total += count
	}
	if total != window.FrameCount {
		return fmt.Errorf("UI frame histogram count %d differs from frame count %d", total, window.FrameCount)
	}
	return nil
}

func writeUvarint(w io.Writer, value uint64) error {
	var raw [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(raw[:], value)
	return writeAll(w, raw[:n])
}

func encodeSVarint(value int64) uint64 {
	return uint64(value<<1) ^ uint64(value>>63)
}

func decodeSVarint(value uint64) int64 {
	return int64(value>>1) ^ -int64(value&1)
}
