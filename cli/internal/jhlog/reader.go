package jhlog

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"hash/crc32"
	"io"
	"math"
	"os"
)

type EventHandler func(Event, map[uint64]string) error

func StreamFile(path string, handle EventHandler) error {
	_, err := StreamFileWithResult(path, handle)
	return err
}

// ReadSessionHeader reads only the bounded current-version file header. It does not scan,
// allocate for, decompress, or validate any event chunk in the file.
func ReadSessionHeader(path string) (SegmentHeader, error) {
	file, err := os.Open(path)
	if err != nil {
		return SegmentHeader{}, err
	}
	defer file.Close()

	var prefix [magicSize]byte
	_, err = io.ReadFull(file, prefix[:])
	if err != nil {
		return SegmentHeader{}, fmt.Errorf("%s: read .jhlog magic: %w", path, err)
	}
	if !bytes.Equal(prefix[:], Magic) {
		return SegmentHeader{}, fmt.Errorf("%s: unsupported .jhlog format; expected %s", path, FormatVersionString)
	}
	header, err := readHeader(file)
	if err != nil {
		return SegmentHeader{}, fmt.Errorf("%s: read session header: %w", path, err)
	}
	if err := validateHeader(header); err != nil {
		return SegmentHeader{}, fmt.Errorf("%s: invalid session header: %w", path, err)
	}
	if err := validateSessionLogFilename(path, header); err != nil {
		return SegmentHeader{}, err
	}
	return header, nil
}

func StreamFileWithResult(path string, handle EventHandler) (StreamResult, error) {
	if handle == nil {
		handle = func(Event, map[uint64]string) error { return nil }
	}
	file, err := os.Open(path)
	if err != nil {
		return StreamResult{}, err
	}
	defer file.Close()

	result := newStreamResult(path)
	if info, statErr := file.Stat(); statErr == nil && info.Size() > 0 {
		result.InputBytes = uint64(info.Size())
	}
	var prefix [magicSize]byte
	n, prefixErr := io.ReadFull(file, prefix[:])
	if prefixErr != nil && !errors.Is(prefixErr, io.EOF) && !errors.Is(prefixErr, io.ErrUnexpectedEOF) {
		return result, prefixErr
	}
	if n < len(Magic) && bytes.Equal(prefix[:n], Magic[:n]) {
		return corruptResult(result, fmt.Errorf("incomplete file magic: %d of %d bytes", n, len(Magic)))
	}
	if n == len(Magic) && bytes.Equal(prefix[:], Magic) {
		digest := sha256.New()
		_, _ = digest.Write(prefix[:])
		return streamBinary(file, result, handle, digest)
	}
	if n == len(Magic) && bytes.Equal(prefix[:8], Magic[:8]) {
		return corruptResult(result, fmt.Errorf(
			"unsupported JHLOG version %d.%d.%d; expected %s",
			prefix[8], prefix[9], prefix[10], FormatVersionString,
		))
	}
	return corruptResult(result, fmt.Errorf("unsupported .jhlog format; expected %s", FormatVersionString))
}

func newStreamResult(source string) StreamResult {
	return StreamResult{
		Source:            source,
		Status:            SegmentStatusOpenClean,
		RecordBytesByType: map[EventType]uint64{},
		RecordsByType:     map[EventType]uint64{},
	}
}

func streamBinary(file *os.File, result StreamResult, handle EventHandler, digest hash.Hash) (StreamResult, error) {
	tracked := io.TeeReader(file, digest)
	header, err := readHeader(tracked)
	if err != nil {
		return corruptResult(result, err)
	}
	if err := validateHeader(header); err != nil {
		return corruptResult(result, fmt.Errorf("invalid session header: %w", err))
	}
	if err := validateSessionLogFilename(result.Source, header); err != nil {
		return corruptResult(result, err)
	}
	result.Header = header
	symbolNamespace := hex.EncodeToString(header.SymbolNamespace)

	dict := map[uint64]string{}
	kinds := map[uint64]DictKind{}
	segmentState := &segmentDecodeState{
		stableAliases:       map[uint64]uint64{},
		stableIDs:           map[uint64]uint64{},
		databaseDescriptors: map[uint64]databaseDescriptorKey{},
		runtimeEdges:        make([]runtimeEdgeKey, 0, 256),
		qualityCounters:     map[uint64]uint64{},
	}
	var expectedSequence uint32
	var dataRecords uint64
	var dictionaryRecords uint64
	runtimeCallScratch := make([]runtimeCallRow, MaxRuntimeCallBlockRows)
	for {
		chunkStart, err := file.Seek(0, io.SeekCurrent)
		if err != nil {
			return result, err
		}
		var rawHeader [chunkHeaderSize]byte
		n, err := io.ReadFull(tracked, rawHeader[:])
		if errors.Is(err, io.EOF) && n == 0 {
			result.Status = SegmentStatusOpenClean
			return result, nil
		}
		if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
			result.Status = SegmentStatusOpenWithTail
			result.TailBytes = physicalTailBytes(file, chunkStart, uint64(n))
			return result, nil
		}
		if err != nil {
			return result, fmt.Errorf("%s: read chunk header: %w", result.Source, err)
		}
		metadata, err := parseChunkHeader(rawHeader[:], expectedSequence)
		if err != nil {
			return corruptResult(result, fmt.Errorf("chunk %d header: %w", expectedSequence, err))
		}
		if err := validateChunkCompressionPolicy(header.OptionalFeatures, metadata.Flags); err != nil {
			return corruptResult(result, fmt.Errorf("chunk %d compression policy: %w", metadata.Sequence, err))
		}

		stored := make([]byte, int(metadata.StoredLen))
		if _, err := io.ReadFull(tracked, stored); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				result.Status = SegmentStatusOpenWithTail
				result.TailBytes = physicalTailBytes(file, chunkStart, chunkHeaderSize)
				return result, nil
			}
			return result, fmt.Errorf("%s: read chunk %d payload: %w", result.Source, metadata.Sequence, err)
		}
		var trailer [commitTrailerSize]byte
		if _, err := io.ReadFull(tracked, trailer[:]); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				result.Status = SegmentStatusOpenWithTail
				result.TailBytes = physicalTailBytes(file, chunkStart, uint64(chunkHeaderSize)+uint64(metadata.StoredLen))
				return result, nil
			}
			return result, fmt.Errorf("%s: read chunk %d commit trailer: %w", result.Source, metadata.Sequence, err)
		}
		if err := validateCommitTrailer(trailer[:], metadata); err != nil {
			return corruptResult(result, fmt.Errorf("chunk %d commit trailer: %w", metadata.Sequence, err))
		}
		raw, err := decompressChunk(stored, metadata)
		if err != nil {
			return corruptResult(result, fmt.Errorf("chunk %d decompression: %w", metadata.Sequence, err))
		}
		if len(raw) != int(metadata.RawLen) {
			return corruptResult(result, fmt.Errorf("chunk %d raw length %d, expected %d", metadata.Sequence, len(raw), metadata.RawLen))
		}
		if computed := crc32.ChecksumIEEE(raw); computed != metadata.RawCRC {
			return corruptResult(result, fmt.Errorf("chunk %d raw CRC mismatch: stored %08x, computed %08x", metadata.Sequence, metadata.RawCRC, computed))
		}

		chunkSummary, err := decodeChunkRecords(
			raw,
			metadata,
			header,
			symbolNamespace,
			result.Source,
			dict,
			kinds,
			runtimeCallScratch,
			handle,
			&result,
			segmentState,
		)
		if err != nil {
			var callback callbackError
			if errors.As(err, &callback) {
				return result, callback.error
			}
			return corruptResult(result, fmt.Errorf("chunk %d records: %w", metadata.Sequence, err))
		}
		dataRecords += chunkSummary.dataRecords
		dictionaryRecords += chunkSummary.dictionaryRecords
		result.CommittedChunks++
		result.StoredChunkBytes += uint64(metadata.StoredLen)
		expectedSequence++

		if metadata.Flags&chunkFlagFinal == 0 {
			if chunkSummary.segmentEndRecords != 0 {
				return corruptResult(result, fmt.Errorf("non-FINAL chunk %d contains SEGMENT_END", metadata.Sequence))
			}
			continue
		}
		if metadata.RecordCount != 2 || chunkSummary.dataRecords != 0 || chunkSummary.dictionaryRecords != 0 ||
			chunkSummary.qualityRecords != 1 || chunkSummary.segmentEndRecords != 1 ||
			chunkSummary.firstRecordType != EventQualitySnapshot || chunkSummary.lastRecordType != EventSegmentEnd {
			return corruptResult(result, fmt.Errorf("FINAL chunk must contain exactly one quality snapshot and one segment end; got %d and %d", chunkSummary.qualityRecords, chunkSummary.segmentEndRecords))
		}
		if result.LatestQuality == nil || result.SegmentEnd == nil {
			return corruptResult(result, fmt.Errorf("FINAL chunk is missing control metadata"))
		}
		if result.SegmentEnd.LastQualitySequence != result.LatestQuality.Sequence {
			return corruptResult(result, fmt.Errorf("segment end quality sequence %d differs from latest snapshot %d", result.SegmentEnd.LastQualitySequence, result.LatestQuality.Sequence))
		}
		if result.SegmentEnd.TotalEventRecords != dataRecords {
			return corruptResult(result, fmt.Errorf("segment end event count %d differs from decoded %d", result.SegmentEnd.TotalEventRecords, dataRecords))
		}
		if result.SegmentEnd.TotalDictionaryRecords != dictionaryRecords {
			return corruptResult(result, fmt.Errorf("segment end dictionary count %d differs from decoded %d", result.SegmentEnd.TotalDictionaryRecords, dictionaryRecords))
		}
		var extra [1]byte
		if n, err := tracked.Read(extra[:]); n != 0 || !errors.Is(err, io.EOF) {
			if err != nil && !errors.Is(err, io.EOF) {
				return result, fmt.Errorf("%s: verify EOF after FINAL: %w", result.Source, err)
			}
			return corruptResult(result, fmt.Errorf("bytes found after FINAL chunk"))
		}
		result.Status = SegmentStatusClosedClean
		result.Sealed = true
		result.TailBytes = 0
		result.SegmentDigest = digest.Sum(nil)
		return result, nil
	}
}

func validateHeader(header SegmentHeader) error {
	if header.SegmentStartElapsedUS > math.MaxInt64 {
		return fmt.Errorf("segment start elapsed time %d exceeds signed timestamp range", header.SegmentStartElapsedUS)
	}
	if header.CollectorStartElapsedUS > header.SegmentStartElapsedUS {
		return fmt.Errorf(
			"collector start elapsed time %d is after segment start %d",
			header.CollectorStartElapsedUS,
			header.SegmentStartElapsedUS,
		)
	}
	return nil
}

func readHeader(reader io.Reader) (SegmentHeader, error) {
	var fixed [8]byte
	if _, err := io.ReadFull(reader, fixed[:]); err != nil {
		return SegmentHeader{}, fmt.Errorf("read file header fields: %w", err)
	}
	headerLength := binary.LittleEndian.Uint32(fixed[:4])
	if headerLength > maxHeaderPayloadSize {
		return SegmentHeader{}, fmt.Errorf("file header payload length %d exceeds %d", headerLength, maxHeaderPayloadSize)
	}
	payload := make([]byte, int(headerLength))
	if _, err := io.ReadFull(reader, payload); err != nil {
		return SegmentHeader{}, fmt.Errorf("read file header payload: %w", err)
	}
	storedCRC := binary.LittleEndian.Uint32(fixed[4:8])
	if computed := crc32.ChecksumIEEE(payload); storedCRC != computed {
		return SegmentHeader{}, fmt.Errorf("file header CRC mismatch: stored %08x, computed %08x", storedCRC, computed)
	}
	header, err := decodeHeaderPayload(payload)
	if err != nil {
		return SegmentHeader{}, fmt.Errorf("decode file header: %w", err)
	}
	return header, nil
}

func physicalTailBytes(file *os.File, chunkStart int64, minimum uint64) uint64 {
	stat, err := file.Stat()
	if err != nil || stat.Size() <= chunkStart {
		return minimum
	}
	return uint64(stat.Size() - chunkStart)
}

func corruptResult(result StreamResult, cause error) (StreamResult, error) {
	result.Status = SegmentStatusCorrupt
	return result, fmt.Errorf("%s: corrupt .jhlog: %w", result.Source, cause)
}

type chunkDecodeSummary struct {
	dataRecords       uint64
	dictionaryRecords uint64
	qualityRecords    uint64
	segmentEndRecords uint64
	firstRecordType   EventType
	lastRecordType    EventType
}

type recordDecodeState struct {
	lastElapsedUS int64
	lastContext   AttributionContext
	hasContext    bool
}

type segmentDecodeState struct {
	stableAliases             map[uint64]uint64
	stableIDs                 map[uint64]uint64
	databaseDescriptors       map[uint64]databaseDescriptorKey
	runtimeEdges              []runtimeEdgeKey
	qualitySequence           uint64
	qualityCapturedUS         uint64
	qualityCounters           map[uint64]uint64
	logGrowthPrevious         [3][]byte
	dictionaryPrevious        [dictKindCount][]byte
	dictionaryTokens          [][]byte
	dictionaryTokenBytes      int
	lastDatabaseTransactionID uint64
}

func decodeChunkRecords(
	raw []byte,
	metadata chunkMetadata,
	header SegmentHeader,
	symbolNamespace string,
	source string,
	dict map[uint64]string,
	kinds map[uint64]DictKind,
	runtimeCallScratch []runtimeCallRow,
	handle EventHandler,
	result *StreamResult,
	segmentState *segmentDecodeState,
) (chunkDecodeSummary, error) {
	reader := bytes.NewReader(raw)
	state := recordDecodeState{lastElapsedUS: int64(header.SegmentStartElapsedUS)}
	summary := chunkDecodeSummary{}
	for index := uint32(0); index < metadata.RecordCount; index++ {
		before := reader.Len()
		bodyLength, err := binary.ReadUvarint(reader)
		if err != nil {
			return summary, fmt.Errorf("record %d length: %w", index, err)
		}
		if bodyLength > maxRawChunkSize || bodyLength > uint64(reader.Len()) {
			return summary, fmt.Errorf("record %d body length %d exceeds remaining %d", index, bodyLength, reader.Len())
		}
		bodyStart := len(raw) - reader.Len()
		bodyEnd := bodyStart + int(bodyLength)
		body := raw[bodyStart:bodyEnd]
		if _, err := reader.Seek(int64(bodyLength), io.SeekCurrent); err != nil {
			return summary, fmt.Errorf("record %d body seek: %w", index, err)
		}
		recordBytes := uint64(before - reader.Len())
		event, nextState, err := decodeRecord(body, state, header.ProcessName, symbolNamespace, source, RecordPosition{
			ChunkSequence: metadata.Sequence,
			RecordIndex:   index,
		}, runtimeCallScratch, segmentState)
		if err != nil {
			return summary, fmt.Errorf("record %d: %w", index, err)
		}
		state = nextState
		result.RawRecordBytes += recordBytes
		if event.microPage == nil {
			if index == 0 {
				summary.firstRecordType = event.Type
			}
			summary.lastRecordType = event.Type
			if err := consumeDecodedEvent(
				event, recordBytes, header, dict, kinds, handle, result, &summary,
			); err != nil {
				return summary, err
			}
			continue
		}
		var totalWeight uint64
		for pageIndex := range event.microPage.events {
			totalWeight += event.microPage.weights[pageIndex]
		}
		var allocated uint64
		for pageIndex := range event.microPage.events {
			pageEvent := event.microPage.events[pageIndex]
			pageBytes := recordBytes * event.microPage.weights[pageIndex] / totalWeight
			if pageIndex == len(event.microPage.events)-1 {
				pageBytes = recordBytes - allocated
			}
			allocated += pageBytes
			if index == 0 && pageIndex == 0 {
				summary.firstRecordType = pageEvent.Type
			}
			summary.lastRecordType = pageEvent.Type
			if err := consumeDecodedEvent(
				pageEvent, pageBytes, header, dict, kinds, handle, result, &summary,
			); err != nil {
				return summary, err
			}
		}
	}
	if reader.Len() != 0 {
		return summary, fmt.Errorf("record count %d leaves %d unparsed raw bytes", metadata.RecordCount, reader.Len())
	}
	return summary, nil
}

func consumeDecodedEvent(
	event Event,
	recordBytes uint64,
	header SegmentHeader,
	dict map[uint64]string,
	kinds map[uint64]DictKind,
	handle EventHandler,
	result *StreamResult,
	summary *chunkDecodeSummary,
) error {
	result.RecordBytesByType[event.Type] += recordBytes
	semanticRecords := uint64(1)
	if event.Type == EventRuntimeCall {
		semanticRecords = uint64(len(event.runtimeCalls))
	}
	result.RecordsByType[event.Type] += semanticRecords
	result.TotalRecords += semanticRecords
	switch event.Type {
	case EventDictionary:
		summary.dictionaryRecords++
		result.DictionaryRecords++
	case EventQualitySnapshot:
		summary.qualityRecords++
		result.ControlRecords++
	case EventSegmentEnd:
		summary.segmentEndRecords++
		result.ControlRecords++
	case EventLogGrowth:
		result.ControlRecords++
	default:
		summary.dataRecords += semanticRecords
		result.DataRecords += semanticRecords
		result.LatestDataEventUnixMS = maxUint64(
			result.LatestDataEventUnixMS,
			eventUnixMS(header, event.TimeMS),
		)
	}
	if event.Dictionary != nil && event.Dictionary.Kind != DictStableSymbol {
		kinds[event.Dictionary.ID] = event.Dictionary.Kind
		if event.Dictionary.Encoding == 0 {
			dict[event.Dictionary.ID] = event.Dictionary.Value
		}
	}
	if event.Quality != nil {
		if result.LatestQuality != nil {
			if err := ValidateQualityProgression(*result.LatestQuality, *event.Quality); err != nil {
				return fmt.Errorf("quality snapshot: %w", err)
			}
		}
		if result.LatestQuality == nil || event.Quality.Sequence >= result.LatestQuality.Sequence {
			quality := cloneQualitySnapshot(*event.Quality)
			result.LatestQuality = &quality
		}
		return nil
	}
	if event.SegmentEnd != nil {
		end := *event.SegmentEnd
		result.SegmentEnd = &end
		return nil
	}
	if event.LogGrowth != nil {
		applyLogGrowthRecord(result, event.LogGrowth)
		return nil
	}
	if event.Type == EventRuntimeCall {
		for rowIndex := range event.runtimeCalls {
			row := event.runtimeCalls[rowIndex]
			if math.MaxUint64-result.RuntimeGraphLogicalCalls < row.count {
				return fmt.Errorf("runtime graph logical call total overflows uint64")
			}
			result.RuntimeGraphLogicalCalls += row.count
			call := RuntimeCallEvent{
				CalleeRef: row.callee,
				Count:     row.count,
				TotalMS:   row.total,
				MaxMS:     row.max,
			}
			expanded := event
			expanded.runtimeCalls = nil
			expanded.RuntimeCall = &call
			expanded.Attribution = AttributionContext{
				Present:     true,
				Screen:      row.screen,
				Owner:       row.caller,
				OperationID: row.operationID,
			}
			if rowIndex > 0 {
				expanded.DeltaUS = 0
				expanded.DeltaMS = 0
			}
			result.Events++
			if err := handle(expanded, dict); err != nil {
				return callbackError{err}
			}
		}
		return nil
	}
	if event.Type.IsSemanticData() {
		result.Events++
	}
	if err := handle(event, dict); err != nil {
		return callbackError{err}
	}
	return nil
}

func eventUnixMS(header SegmentHeader, eventElapsedMS uint64) uint64 {
	segmentElapsedMS := header.SegmentStartElapsedUS / 1_000
	if eventElapsedMS <= segmentElapsedMS {
		return header.SegmentStartUnixMS
	}
	delta := eventElapsedMS - segmentElapsedMS
	if math.MaxUint64-header.SegmentStartUnixMS < delta {
		return math.MaxUint64
	}
	return header.SegmentStartUnixMS + delta
}

func ValidateQualityProgression(previous, current QualitySnapshot) error {
	if current.Sequence < previous.Sequence {
		return fmt.Errorf("sequence regressed from %d to %d", previous.Sequence, current.Sequence)
	}
	if current.CapturedElapsedUS < previous.CapturedElapsedUS {
		return fmt.Errorf(
			"captured elapsed time regressed from %d to %d",
			previous.CapturedElapsedUS,
			current.CapturedElapsedUS,
		)
	}
	for id, previousValue := range previous.Counters {
		if currentValue := current.Counters[id]; currentValue < previousValue {
			return fmt.Errorf("counter %d regressed from %d to %d", id, previousValue, currentValue)
		}
	}
	return nil
}

type callbackError struct{ error }

func decodeRecord(
	body []byte,
	state recordDecodeState,
	processName string,
	symbolNamespace string,
	source string,
	position RecordPosition,
	runtimeCallScratch []runtimeCallRow,
	optionalSegmentState ...*segmentDecodeState,
) (Event, recordDecodeState, error) {
	segmentState := decodeSegmentState(optionalSegmentState)
	reader := recordReader{data: body}
	eventType, err := reader.readUvarint()
	if err != nil {
		return Event{}, state, fmt.Errorf("type: %w", err)
	}
	flags, err := reader.readUvarint()
	if err != nil {
		return Event{}, state, fmt.Errorf("envelope flags: %w", err)
	}
	knownEnvelopeFlags := uint64(EnvelopeHasTime | EnvelopeHasThread | EnvelopeHasContext | EnvelopeSameContext | EnvelopeHasAttributes)
	if flags&^knownEnvelopeFlags != 0 {
		return Event{}, state, fmt.Errorf("unsupported envelope flags 0x%x", flags&^knownEnvelopeFlags)
	}
	if flags&uint64(EnvelopeSameContext) != 0 && flags&uint64(EnvelopeHasContext) == 0 {
		return Event{}, state, fmt.Errorf("SAME_CONTEXT without HAS_CONTEXT")
	}

	event := Event{
		Type:     EventType(eventType),
		Source:   source,
		Position: position,
	}
	nextState := state
	if flags&uint64(EnvelopeHasTime) != 0 {
		rawDelta, err := reader.readUvarint()
		if err != nil {
			return Event{}, state, fmt.Errorf("producer timestamp delta: %w", err)
		}
		delta := decodeSVarint(rawDelta)
		elapsed, err := addSignedTimestamp(state.lastElapsedUS, delta)
		if err != nil {
			return Event{}, state, err
		}
		event.DeltaUS = delta
		if delta >= 0 {
			event.DeltaMS = uint64(delta) / 1000
		}
		event.Producer.HasTime = true
		event.Producer.ElapsedUS = uint64(elapsed)
		nextState.lastElapsedUS = elapsed
	}
	event.TimeUS = uint64(nextState.lastElapsedUS)
	event.TimeMS = event.TimeUS / 1000
	if flags&uint64(EnvelopeHasThread) != 0 {
		threadID, err := reader.readUvarint()
		if err != nil {
			return Event{}, state, fmt.Errorf("producer thread: %w", err)
		}
		event.Producer.HasThread = true
		event.Producer.ThreadID = threadID
	}
	if flags&uint64(EnvelopeHasContext) != 0 {
		if flags&uint64(EnvelopeSameContext) != 0 {
			if !state.hasContext {
				return Event{}, state, fmt.Errorf("SAME_CONTEXT without prior context in chunk")
			}
			event.Attribution = state.lastContext
			event.Attribution.Present = true
		} else {
			context, err := readAttribution(&reader, symbolNamespace, segmentState.stableAliases)
			if err != nil {
				return Event{}, state, err
			}
			event.Attribution = context
			nextState.lastContext = context
			nextState.hasContext = true
		}
	}
	if flags&uint64(EnvelopeHasAttributes) != 0 {
		attributes, err := reader.readUvarint()
		if err != nil {
			return Event{}, state, fmt.Errorf("event attributes: %w", err)
		}
		event.Flags |= attributes
	}
	if event.Type == eventMicroPage {
		if flags != 0 {
			return Event{}, state, fmt.Errorf("micro-page envelope must be empty")
		}
		page, pageState, err := decodeMicroPage(
			&reader,
			event,
			state,
			processName,
			symbolNamespace,
			segmentState,
		)
		if err != nil {
			return Event{}, state, err
		}
		event.microPage = page
		return event, pageState, nil
	}

	if err := decodeEventPayload(&reader, &event, processName, symbolNamespace, runtimeCallScratch, segmentState); err != nil {
		return Event{}, state, err
	}
	if reader.Len() != 0 {
		return Event{}, state, fmt.Errorf("event type %d leaves %d trailing payload bytes", event.Type, reader.Len())
	}
	return event, nextState, nil
}

func decodeSegmentState(optional []*segmentDecodeState) *segmentDecodeState {
	if len(optional) > 0 && optional[0] != nil {
		return optional[0]
	}
	return &segmentDecodeState{
		stableAliases:       map[uint64]uint64{},
		stableIDs:           map[uint64]uint64{},
		databaseDescriptors: map[uint64]databaseDescriptorKey{},
		runtimeEdges:        make([]runtimeEdgeKey, 0, 256),
		qualityCounters:     map[uint64]uint64{},
	}
}

func addSignedTimestamp(current, delta int64) (int64, error) {
	if delta > 0 && current > math.MaxInt64-delta {
		return 0, fmt.Errorf("producer timestamp overflows int64")
	}
	if delta < 0 && delta < -current {
		return 0, fmt.Errorf("producer timestamp becomes negative")
	}
	return current + delta, nil
}

func readDatabaseTransactionID(
	reader *recordReader,
	state *segmentDecodeState,
) (uint64, error) {
	encoded, err := reader.readUvarint()
	if err != nil {
		return 0, fmt.Errorf("database transaction ID: %w", err)
	}
	transactionID, err := addSignedDatabaseTransactionID(
		state.lastDatabaseTransactionID,
		decodeSVarint(encoded),
	)
	if err != nil {
		return 0, err
	}
	if transactionID == 0 {
		return 0, fmt.Errorf("database transaction ID must be non-zero")
	}
	state.lastDatabaseTransactionID = transactionID
	return transactionID, nil
}

func addSignedDatabaseTransactionID(current uint64, delta int64) (uint64, error) {
	if current > math.MaxInt64 {
		return 0, fmt.Errorf("database transaction ID %d exceeds signed delta range", current)
	}
	base := int64(current)
	if delta > 0 && base > math.MaxInt64-delta {
		return 0, fmt.Errorf("database transaction ID delta overflows int64")
	}
	if delta < 0 && delta <= -base {
		return 0, fmt.Errorf("database transaction ID delta produces a non-positive value")
	}
	return uint64(base + delta), nil
}

type recordReader struct {
	data   []byte
	offset int
}

func (r *recordReader) Len() int {
	return len(r.data) - r.offset
}

func (r *recordReader) readUvarint() (uint64, error) {
	var value uint64
	for shift := uint(0); shift < 64; shift += 7 {
		if r.offset >= len(r.data) {
			if shift == 0 {
				return 0, io.EOF
			}
			return 0, io.ErrUnexpectedEOF
		}
		current := r.data[r.offset]
		r.offset++
		if current < 0x80 {
			if shift == 63 && current > 1 {
				return 0, errUvarintOverflow
			}
			return value | uint64(current)<<shift, nil
		}
		value |= uint64(current&0x7f) << shift
	}
	return 0, errUvarintOverflow
}

func (r *recordReader) readFull(target []byte) error {
	if len(target) > r.Len() {
		return io.ErrUnexpectedEOF
	}
	copy(target, r.data[r.offset:r.offset+len(target)])
	r.offset += len(target)
	return nil
}

var errUvarintOverflow = errors.New("binary: varint overflows a 64-bit integer")

func readAttribution(
	reader *recordReader,
	symbolNamespace string,
	stableAliases map[uint64]uint64,
) (AttributionContext, error) {
	mask, err := reader.readUvarint()
	if err != nil {
		return AttributionContext{}, fmt.Errorf("context presence mask: %w", err)
	}
	if mask&^uint64(0x7) != 0 {
		return AttributionContext{}, fmt.Errorf("unsupported context presence bits 0x%x", mask&^uint64(0x7))
	}
	context := AttributionContext{Present: true}
	targets := []struct {
		bit uint64
		ref *SymbolRef
	}{
		{1 << 0, &context.Screen},
		{1 << 1, &context.Owner},
	}
	for _, item := range targets {
		if mask&item.bit == 0 {
			continue
		}
		ref, err := readSymbolRef(reader, symbolNamespace, stableAliases)
		if err != nil {
			return AttributionContext{}, fmt.Errorf("context symbol: %w", err)
		}
		*item.ref = ref
	}
	if mask&(1<<2) != 0 {
		operationID, err := reader.readUvarint()
		if err != nil {
			return AttributionContext{}, fmt.Errorf("context operation ID: %w", err)
		}
		if operationID == 0 {
			return AttributionContext{}, fmt.Errorf("context operation ID must be non-zero")
		}
		context.OperationID = operationID
	}
	return context, nil
}

func readSymbolRef(
	reader *recordReader,
	symbolNamespace string,
	stableAliases map[uint64]uint64,
) (SymbolRef, error) {
	token, err := reader.readUvarint()
	if err != nil {
		return SymbolRef{}, err
	}
	switch {
	case token == 0:
		return SymbolRef{}, nil
	case token == 1:
		var raw [8]byte
		if err := reader.readFull(raw[:]); err != nil {
			return SymbolRef{}, err
		}
		return SymbolRef{ID: binary.LittleEndian.Uint64(raw[:]), Namespace: symbolNamespace, Stable: true}, nil
	case token&1 == 0:
		return LocalSymbol(token >> 1), nil
	default:
		alias := token >> 1
		stableID, ok := stableAliases[alias]
		if !ok {
			return SymbolRef{}, fmt.Errorf("undefined stable symbol alias %d", alias)
		}
		return SymbolRef{ID: stableID, Namespace: symbolNamespace, Stable: true}, nil
	}
}
