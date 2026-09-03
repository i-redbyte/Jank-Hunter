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
	"unicode/utf8"
)

const (
	maxDatabaseDescriptors          = 4_096
	maxRuntimeEdges                 = 65_536
	dictKindCount                   = int(DictAttributeValue) + 1
	controlDeltaFull         uint64 = 0
	controlDeltaPrefixSuffix uint64 = 1
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

type eventPayloadEncodeState struct {
	lastDatabaseTransactionID uint64
}

type databaseDescriptorKey struct {
	query       SymbolRef
	source      SymbolRef
	fingerprint uint64
	framework   DatabaseFramework
	operation   DatabaseOperation
	boundary    DatabaseBoundary
}

type Writer struct {
	w                   io.Writer
	header              SegmentHeader
	chunkTarget         int
	gzipChunks          bool
	raw                 bytes.Buffer
	payload             bytes.Buffer
	recordCount         uint32
	sequence            uint32
	state               recordEncodeState
	closed              bool
	poisoned            error
	latestQuality       QualitySnapshot
	totalEvents         uint64
	totalDictionary     uint64
	runtimeBlock        runtimeBlockEncoder
	lastElapsedUS       uint64
	digest              hash.Hash
	segmentDigest       []byte
	stableAliases       map[uint64]uint64
	databaseDescriptors map[databaseDescriptorKey]uint64
	microPage           microPageBuilder
	microPageFlushing   bool
	chunkHeader         [chunkHeaderSize]byte
	chunkTrailer        [commitTrailerSize]byte
	logGrowthPrevious   [3][]byte
	dictionaryPrevious  [dictKindCount][]byte
	dictionaryTokens    dictionaryTokenEncoder
	payloadState        eventPayloadEncodeState
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
	options.Header.OptionalFeatures = options.Header.OptionalFeatures&^compressionFeatures |
		compressionOptionalFeatures(options.GZIP)&compressionFeatures
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
	writer := &Writer{
		w:           tracked,
		header:      header,
		chunkTarget: target,
		gzipChunks:  options.GZIP,
		state: recordEncodeState{
			lastElapsedUS: int64(header.SegmentStartElapsedUS),
		},
		latestQuality:       QualitySnapshot{Counters: map[uint64]uint64{}},
		lastElapsedUS:       header.SegmentStartElapsedUS,
		digest:              digest,
		stableAliases:       map[uint64]uint64{},
		databaseDescriptors: map[databaseDescriptorKey]uint64{},
		runtimeBlock:        newRuntimeBlockEncoder(),
		microPage:           newMicroPageBuilder(),
		payloadState:        eventPayloadEncodeState{},
	}
	writer.payload.Grow(256)
	return writer, nil
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
	var pendingStableID uint64
	var hasPendingStableAlias bool
	if event.Type == EventDictionary && event.Dictionary != nil && event.Dictionary.Kind == DictStableSymbol {
		entry := *event.Dictionary
		if alias, ok := w.stableAliases[entry.ID]; ok {
			entry.Alias = alias
		} else {
			entry.Alias = uint64(len(w.stableAliases)) + 1
			pendingStableID = entry.ID
			hasPendingStableAlias = true
		}
		event.Dictionary = &entry
	}
	var pendingDictionaryKind DictKind
	var pendingDictionaryData []byte
	var hasPendingDictionary bool
	var hasPendingDictionaryTokens bool
	if event.Type == EventDictionary && event.Dictionary != nil {
		if event.Dictionary.Kind > DictAttributeValue {
			return fmt.Errorf("unsupported dictionary kind %d", event.Dictionary.Kind)
		}
		entry, data, err := prepareDictionaryFront(*event.Dictionary, w.dictionaryPrevious[event.Dictionary.Kind])
		if err != nil {
			return err
		}
		if encoded, tokenized := w.dictionaryTokens.prepare(entry.Kind, data, int(entry.frontPrefix)); tokenized {
			entry.tokenData = encoded
			entry.tokenReady = true
			hasPendingDictionaryTokens = true
		}
		event.Dictionary = &entry
		pendingDictionaryKind = entry.Kind
		pendingDictionaryData = data
		hasPendingDictionary = true
	}
	var pendingDescriptor databaseDescriptorKey
	var hasPendingDescriptor bool
	if event.Type == EventDatabase && event.Database != nil {
		if err := validateDatabaseEvent(event.Database); err != nil {
			return err
		}
		database := *event.Database
		pendingDescriptor = databaseDescriptorKey{
			query: database.QueryRef, source: database.SourceRef, fingerprint: database.StatementFingerprint,
			framework: database.Framework, operation: database.Operation, boundary: database.Boundary,
		}
		if descriptorID := w.databaseDescriptors[pendingDescriptor]; descriptorID != 0 {
			database.descriptorID = descriptorID
			database.descriptorPrepared = true
		} else if len(w.databaseDescriptors) < maxDatabaseDescriptors {
			database.descriptorID = uint64(len(w.databaseDescriptors)) + 1
			database.descriptorDefinition = true
			database.descriptorPrepared = true
			hasPendingDescriptor = true
		} else {
			// ID zero is the exact inline fallback when the bounded descriptor registry is full.
			database.descriptorDefinition = true
			database.descriptorPrepared = true
		}
		event.Database = &database
	}
	var pendingLogGrowthKind LogGrowthRecordKind
	if event.Type == EventLogGrowth && event.LogGrowth != nil {
		kind := event.LogGrowth.Kind
		if kind != LogGrowthHistory && kind != LogGrowthLive {
			return fmt.Errorf("unsupported log-growth record kind %d", kind)
		}
		prepared, err := prepareLogGrowthDelta(*event.LogGrowth, w.logGrowthPrevious[kind])
		if err != nil {
			return err
		}
		event.LogGrowth = &prepared
		pendingLogGrowthKind = prepared.Kind
	}
	err := w.writeEventRecord(event, 1)
	if err != nil {
		return err
	}
	if hasPendingStableAlias {
		w.stableAliases[pendingStableID] = event.Dictionary.Alias
	}
	if hasPendingDictionary {
		w.dictionaryPrevious[pendingDictionaryKind] = append(
			w.dictionaryPrevious[pendingDictionaryKind][:0],
			pendingDictionaryData...,
		)
	}
	if hasPendingDictionaryTokens {
		w.dictionaryTokens.commit(pendingDictionaryData)
	}
	if hasPendingDescriptor {
		w.databaseDescriptors[pendingDescriptor] = event.Database.descriptorID
	}
	if pendingLogGrowthKind != 0 {
		w.logGrowthPrevious[pendingLogGrowthKind] = append(
			w.logGrowthPrevious[pendingLogGrowthKind][:0],
			event.LogGrowth.Raw...,
		)
	}
	return nil
}

func prepareLogGrowthDelta(record LogGrowthRecord, previous []byte) (LogGrowthRecord, error) {
	if record.Kind != LogGrowthHistory && record.Kind != LogGrowthLive {
		return LogGrowthRecord{}, fmt.Errorf("unsupported log-growth record kind %d", record.Kind)
	}
	if len(record.Raw) == 0 {
		return LogGrowthRecord{}, fmt.Errorf("log-growth raw payload is empty")
	}
	prefix := 0
	for prefix < len(previous) && prefix < len(record.Raw) && previous[prefix] == record.Raw[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(previous)-prefix && suffix < len(record.Raw)-prefix &&
		previous[len(previous)-suffix-1] == record.Raw[len(record.Raw)-suffix-1] {
		suffix++
	}
	middle := len(record.Raw) - prefix - suffix
	record.wireMode = controlDeltaFull
	if len(previous) > 0 && uvarintSize(uint64(prefix))+uvarintSize(uint64(suffix))+middle < len(record.Raw) {
		record.wireMode = controlDeltaPrefixSuffix
		record.wirePrefix = uint64(prefix)
		record.wireSuffix = uint64(suffix)
	}
	record.wireReady = true
	return record, nil
}

// WriteRuntimeCallBlock writes up to MaxRuntimeCallBlockRows observations as one SoA wire record.
func (w *Writer) WriteRuntimeCallBlock(events []Event) error {
	if w.closed {
		return fmt.Errorf("jhlog writer is closed")
	}
	if w.poisoned != nil {
		return w.poisoned
	}
	synthetic, logicalCalls, addedCount, err := w.runtimeBlock.prepare(events)
	if err != nil {
		return err
	}
	err = w.writeEventRecord(synthetic, uint64(len(events)))
	if err == nil {
		w.runtimeBlock.commit(logicalCalls)
	} else {
		w.runtimeBlock.rollback(addedCount)
	}
	return err
}

func (w *Writer) writeEventRecord(event Event, semanticCount uint64) error {
	if !event.Type.IsSemanticData() {
		if err := w.flushMicroPage(); err != nil {
			return err
		}
		basePayloadState := w.payloadState
		nextPayloadState := basePayloadState
		err := w.writeSemanticRecord(event.Type, semanticCount, func(state recordEncodeState) ([]byte, recordEncodeState, uint64, error) {
			nextPayloadState = basePayloadState
			return encodeRecordWithPayloadState(
				event,
				state,
				w.stableAliases,
				&nextPayloadState,
			)
		})
		if err == nil {
			w.payloadState = nextPayloadState
		}
		return err
	}

	w.payload.Reset()
	nextPayloadState := w.payloadState
	if err := encodeEventPayloadWithState(&w.payload, event, w.stableAliases, &nextPayloadState); err != nil {
		return err
	}
	elapsedUS, hasTime, err := eventElapsedUS(event)
	if err != nil {
		return err
	}
	if w.payload.Len() > maxMicroPagePayloadBytes {
		if err := w.flushMicroPage(); err != nil {
			return err
		}
		basePayloadState := w.payloadState
		candidate := basePayloadState
		err := w.writeSemanticRecord(event.Type, semanticCount, func(state recordEncodeState) ([]byte, recordEncodeState, uint64, error) {
			candidate = basePayloadState
			return encodeRecordWithPayloadState(
				event,
				state,
				w.stableAliases,
				&candidate,
			)
		})
		if err == nil {
			w.payloadState = candidate
		}
		return err
	}
	if !w.microPage.canAppend(w.payload.Len()) {
		if err := w.flushMicroPage(); err != nil {
			return err
		}
	}
	context := eventAttribution(event)
	if event.Type == EventRuntimeCall {
		context = AttributionContext{}
	}
	w.microPage.append(microPageRow{
		eventType:  event.Type,
		producer:   event.Producer,
		context:    context,
		attributes: eventAttributes(event),
		payload:    w.payload.Bytes(),
		elapsedUS:  elapsedUS, hasTime: hasTime,
	})
	w.payloadState = nextPayloadState
	w.totalEvents += semanticCount
	w.incrementQuality(QualityAcceptedEventTotal, semanticCount)
	w.incrementQuality(QualityWrittenEventTotal, semanticCount)
	if hasTime {
		w.lastElapsedUS = elapsedUS
	}
	if len(w.microPage.rows) == maxMicroPageRows || w.microPage.payloadBytes >= w.chunkTarget {
		return w.flushMicroPage()
	}
	return nil
}

func (w *Writer) flushMicroPage() error {
	if len(w.microPage.rows) == 0 || w.microPageFlushing {
		return nil
	}
	record, err := encodeMicroPageRecord(
		w.microPage.rows,
		w.stableAliases,
		useRANSSections(w.header.OptionalFeatures, w.gzipChunks),
	)
	if err != nil {
		return err
	}
	w.microPageFlushing = true
	defer func() { w.microPageFlushing = false }()
	if len(record) > maxRawChunkSize {
		return fmt.Errorf("micro-page record is too large: %d > %d", len(record), maxRawChunkSize)
	}
	if w.raw.Len() > 0 && w.raw.Len()+len(record) > w.chunkTarget {
		if err := w.flushRawChunk(); err != nil {
			return err
		}
	}
	if w.raw.Len()+len(record) > maxRawChunkSize {
		if err := w.flushRawChunk(); err != nil {
			return err
		}
	}
	if _, err := w.raw.Write(record); err != nil {
		return err
	}
	w.recordCount++
	w.microPage.reset()
	return nil
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
	if err := w.flushMicroPage(); err != nil {
		return err
	}
	return w.flushRawChunk()
}

func (w *Writer) flushRawChunk() error {
	if w.raw.Len() == 0 {
		return nil
	}
	if err := w.commitChunk(w.raw.Bytes(), w.recordCount, false); err != nil {
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
		w.runtimeBlock.logicalCalls,
	)
	quality.Counters[QualityRuntimeGraphEmittedTotal] = w.runtimeBlock.logicalCalls
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
	fillChunkHeader(&w.chunkHeader, metadata)
	fillCommitTrailer(&w.chunkTrailer, metadata)
	writeErr := writeAll(w.w, w.chunkHeader[:])
	if writeErr == nil {
		writeErr = writeAll(w.w, stored)
	}
	if writeErr == nil {
		writeErr = writeAll(w.w, w.chunkTrailer[:])
	}
	if writeErr != nil {
		w.incrementQuality(QualityWriterIOErrorTotal, 1)
		w.incrementQuality(QualityFailedChunkTotal, 1)
		w.poisoned = fmt.Errorf("write chunk %d: %w", w.sequence, writeErr)
		return w.poisoned
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

func prepareDictionaryFront(entry DictionaryEntry, previous []byte) (DictionaryEntry, []byte, error) {
	data, err := dictionaryData(entry)
	if err != nil {
		return DictionaryEntry{}, nil, err
	}
	entry.frontPrefix = uint64(commonUTF8Prefix(previous, data))
	entry.frontData = data
	entry.frontReady = true
	return entry, data, nil
}

func dictionaryData(entry DictionaryEntry) ([]byte, error) {
	if entry.Kind > DictAttributeValue {
		return nil, fmt.Errorf("unsupported dictionary kind %d", entry.Kind)
	}
	if entry.Encoding != 0 {
		return nil, fmt.Errorf("unsupported dictionary encoding %d", entry.Encoding)
	}
	data := entry.Data
	if data == nil {
		data = []byte(entry.Value)
	}
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("dictionary value %d is not valid UTF-8", entry.ID)
	}
	return data, nil
}

func commonUTF8Prefix(previous, current []byte) int {
	limit := min(len(previous), len(current))
	prefix := 0
	for prefix < limit && previous[prefix] == current[prefix] {
		prefix++
	}
	if prefix == len(previous) || prefix == len(current) {
		return prefix
	}
	for prefix > 0 && current[prefix]&0xc0 == 0x80 {
		prefix--
	}
	return prefix
}

func maxUint64(a, b uint64) uint64 {
	if a > b {
		return a
	}
	return b
}

func encodeRecord(event Event, state recordEncodeState) ([]byte, recordEncodeState, uint64, error) {
	return encodeRecordWithAliases(event, state, nil)
}

func encodeRecordWithAliases(
	event Event,
	state recordEncodeState,
	stableAliases map[uint64]uint64,
) ([]byte, recordEncodeState, uint64, error) {
	return encodeRecordWithPayloadState(event, state, stableAliases, nil)
}

func encodeRecordWithPayloadState(
	event Event,
	state recordEncodeState,
	stableAliases map[uint64]uint64,
	payloadState *eventPayloadEncodeState,
) ([]byte, recordEncodeState, uint64, error) {
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
		if err := writeAttribution(&body, context, stableAliases); err != nil {
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
	if err := encodeEventPayloadWithState(&body, event, stableAliases, payloadState); err != nil {
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

func uvarintSize(value uint64) int {
	size := 1
	for value >= 0x80 {
		value >>= 7
		size++
	}
	return size
}

func writeUvarint(w io.Writer, value uint64) error {
	if byteWriter, ok := w.(io.ByteWriter); ok {
		for value >= 0x80 {
			if err := byteWriter.WriteByte(byte(value) | 0x80); err != nil {
				return err
			}
			value >>= 7
		}
		return byteWriter.WriteByte(byte(value))
	}
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
