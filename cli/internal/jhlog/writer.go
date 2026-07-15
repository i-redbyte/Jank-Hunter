package jhlog

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
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
	w               io.Writer
	header          SegmentHeader
	chunkTarget     int
	gzipChunks      bool
	raw             bytes.Buffer
	recordCount     uint32
	sequence        uint32
	state           recordEncodeState
	closed          bool
	poisoned        error
	latestQuality   QualitySnapshot
	totalEvents     uint64
	totalDictionary uint64
	lastElapsedUS   uint64
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
	if err := writeAll(w, headerBytes); err != nil {
		return nil, fmt.Errorf("write file header: %w", err)
	}
	return &Writer{
		w:           w,
		header:      header,
		chunkTarget: target,
		gzipChunks:  options.GZIP,
		state: recordEncodeState{
			lastElapsedUS: int64(header.SegmentStartElapsedUS),
		},
		latestQuality: QualitySnapshot{Counters: map[uint64]uint64{}},
		lastElapsedUS: header.SegmentStartElapsedUS,
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

func (w *Writer) Header() SegmentHeader {
	header := w.header
	header.SymbolNamespace = append([]byte(nil), header.SymbolNamespace...)
	return header
}

func (w *Writer) SetQualitySnapshot(snapshot QualitySnapshot) {
	w.latestQuality = cloneQualitySnapshot(snapshot)
	if w.latestQuality.Counters == nil {
		w.latestQuality.Counters = map[uint64]uint64{}
	}
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
		w.SetQualitySnapshot(*event.Quality)
		return nil
	}
	if event.Type == EventSegmentEnd {
		return fmt.Errorf("segment end is reserved for Writer.Close")
	}

	record, nextState, elapsedUS, err := encodeRecord(event, w.state)
	if err != nil {
		return err
	}
	if len(record) > maxRawChunkSize {
		w.incrementQuality(QualityOversizedRecordTotal, 1)
		w.incrementQuality(EventQualityCounterID(event.Type, QualityLossOversized), 1)
		return fmt.Errorf("event type %d record is too large: %d > %d", event.Type, len(record), maxRawChunkSize)
	}
	if w.raw.Len() > 0 && w.raw.Len()+len(record) > w.chunkTarget {
		if err := w.Flush(); err != nil {
			return err
		}
		record, nextState, elapsedUS, err = encodeRecord(event, w.state)
		if err != nil {
			return err
		}
	}
	if w.raw.Len()+len(record) > maxRawChunkSize {
		if w.raw.Len() > 0 {
			if err := w.Flush(); err != nil {
				return err
			}
			record, nextState, elapsedUS, err = encodeRecord(event, w.state)
			if err != nil {
				return err
			}
		}
		if len(record) > maxRawChunkSize {
			w.incrementQuality(QualityOversizedRecordTotal, 1)
			w.incrementQuality(EventQualityCounterID(event.Type, QualityLossOversized), 1)
			return fmt.Errorf("event type %d record is too large: %d > %d", event.Type, len(record), maxRawChunkSize)
		}
	}
	if _, err := w.raw.Write(record); err != nil {
		return err
	}
	w.recordCount++
	w.state = nextState
	w.lastElapsedUS = elapsedUS
	if event.Type == EventDictionary {
		w.totalDictionary++
	} else {
		w.totalEvents++
		w.incrementQuality(QualityAcceptedEventTotal, 1)
		w.incrementQuality(QualityWrittenEventTotal, 1)
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
	if event.Attribution.Present {
		return event.Attribution
	}
	context := AttributionContext{Present: true}
	switch {
	case event.HTTP != nil:
		context.Owner = firstSymbol(event.HTTP.OwnerRef, event.HTTP.OwnerID)
	case event.UIWindow != nil:
		context.Screen = firstSymbol(event.UIWindow.ScreenRef, event.UIWindow.ScreenID)
	case event.Stall != nil:
		context.Owner = firstSymbol(event.Stall.OwnerRef, event.Stall.OwnerID)
	case event.Retained != nil:
		context = legacyContext(event.Retained.ScreenID, event.Retained.OwnerID, event.Retained.FlowID, event.Retained.StepID)
	case event.Flow != nil:
		context = legacyContext(event.Flow.ScreenID, event.Flow.OwnerID, event.Flow.FlowID, event.Flow.StepID)
	case event.LogSpam != nil:
		context = legacyContext(event.LogSpam.ScreenID, event.LogSpam.OwnerID, event.LogSpam.FlowID, event.LogSpam.StepID)
	case event.Problem != nil:
		context = legacyContext(event.Problem.ScreenID, event.Problem.OwnerID, event.Problem.FlowID, event.Problem.StepID)
	case event.RuntimeCall != nil:
		context = legacyContext(event.RuntimeCall.ScreenID, event.RuntimeCall.CallerID, event.RuntimeCall.FlowID, event.RuntimeCall.StepID)
		context.Owner = firstSymbol(event.RuntimeCall.CallerRef, event.RuntimeCall.CallerID)
	default:
		return AttributionContext{}
	}
	return context
}

func legacyContext(screenID, ownerID, flowID, stepID uint64) AttributionContext {
	return AttributionContext{
		Present: true,
		Screen:  LocalSymbol(screenID),
		Owner:   LocalSymbol(ownerID),
		Flow:    LocalSymbol(flowID),
		Step:    LocalSymbol(stepID),
	}
}

func firstSymbol(ref SymbolRef, legacyID uint64) SymbolRef {
	if !ref.IsUnknown() {
		return ref
	}
	return LocalSymbol(legacyID)
}

func equalAttribution(a, b AttributionContext) bool {
	return a.Screen == b.Screen && a.Owner == b.Owner && a.Flow == b.Flow && a.Step == b.Step
}

func writeAttribution(w io.Writer, context AttributionContext) error {
	var mask uint64
	refs := []struct {
		bit uint64
		ref SymbolRef
	}{
		{1 << 0, context.Screen},
		{1 << 1, context.Owner},
		{1 << 2, context.Flow},
		{1 << 3, context.Step},
	}
	for _, item := range refs {
		if !item.ref.IsUnknown() {
			mask |= item.bit
		}
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
	return nil
}

func writeSymbolRef(w io.Writer, ref SymbolRef) error {
	if ref.Stable {
		if err := writeUvarint(w, 1); err != nil {
			return err
		}
		var raw [8]byte
		binary.LittleEndian.PutUint64(raw[:], ref.StableID)
		return writeAll(w, raw[:])
	}
	if ref.LocalID > math.MaxUint64>>1 {
		return fmt.Errorf("local symbol id %d is too large", ref.LocalID)
	}
	return writeUvarint(w, ref.LocalID<<1)
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
		if err := writeRefs(
			firstSymbol(p.AppVersionRef, p.AppVersionID),
			firstSymbol(p.BuildRef, p.BuildID),
			firstSymbol(p.DeviceRef, p.DeviceID),
		); err != nil {
			return err
		}
		if err := writeValues(p.SDKInt); err != nil {
			return err
		}
		return writeRefs(
			firstSymbol(p.AndroidReleaseRef, p.AndroidReleaseID),
			firstSymbol(p.SecurityPatchRef, p.SecurityPatchID),
			firstSymbol(p.PrimaryABIRef, p.PrimaryABIID),
			firstSymbol(p.SupportedABIsRef, p.SupportedABIsID),
			firstSymbol(p.ManufacturerRef, p.ManufacturerID),
			firstSymbol(p.BrandRef, p.BrandID),
			firstSymbol(p.HardwareRef, p.HardwareID),
			firstSymbol(p.BoardRef, p.BoardID),
			firstSymbol(p.ProductRef, p.ProductID),
		)
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
		if err := writeSymbolRef(w, firstSymbol(p.RouteRef, p.RouteID)); err != nil {
			return err
		}
		return writeValues(p.DurationMS, p.DNSMS, p.ConnectMS, p.TTFBMS, uint64(p.Status), p.RxBytes, p.TxBytes)
	case EventUIWindow:
		p := event.UIWindow
		if p == nil {
			return fmt.Errorf("ui window payload is nil")
		}
		return writeValues(p.WindowMS, p.FrameCount, p.JankCount, p.P50MS, p.P95MS, p.P99MS)
	case EventStall:
		p := event.Stall
		if p == nil {
			return fmt.Errorf("stall payload is nil")
		}
		if err := writeSymbolRef(w, firstSymbol(p.StackRef, p.StackID)); err != nil {
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
		if err := writeRefs(firstSymbol(p.ClassRef, p.ClassID), firstSymbol(p.HolderRef, p.HolderID)); err != nil {
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
		if err := writeSymbolRef(w, firstSymbol(p.MetricRef, p.MetricID)); err != nil {
			return err
		}
		return writeValues(p.Value, count, sum, max, uint64(p.Mode))
	case EventFlow:
		p := event.Flow
		if p == nil {
			return fmt.Errorf("flow payload is nil")
		}
		return writeValues(p.Phase, p.InstanceID)
	case EventLogSpam:
		p := event.LogSpam
		if p == nil {
			return fmt.Errorf("log spam payload is nil")
		}
		if err := writeSymbolRef(w, firstSymbol(p.SourceRef, p.SourceID)); err != nil {
			return err
		}
		return writeValues(p.Level, p.Count)
	case EventProblem:
		p := event.Problem
		if p == nil {
			return fmt.Errorf("problem payload is nil")
		}
		if err := writeSymbolRef(w, firstSymbol(p.KindRef, p.KindID)); err != nil {
			return err
		}
		return writeValues(p.WindowMS, p.Count, p.MaxMS)
	case EventRuntimeCall:
		p := event.RuntimeCall
		if p == nil {
			return fmt.Errorf("runtime call payload is nil")
		}
		if err := writeSymbolRef(w, firstSymbol(p.CalleeRef, p.CalleeID)); err != nil {
			return err
		}
		return writeValues(p.Count, p.TotalMS, p.MaxMS)
	case EventQualitySnapshot:
		p := event.Quality
		if p == nil {
			return fmt.Errorf("quality snapshot payload is nil")
		}
		ids := make([]uint64, 0, len(p.Counters))
		for id := range p.Counters {
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
		return writeValues(uint64(p.Reason), p.TotalEventRecords, p.TotalDictionaryRecords, p.LastQualitySequence)
	default:
		return fmt.Errorf("unsupported event type %d", event.Type)
	}
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
