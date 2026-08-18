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
	"unicode/utf8"
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
	n, err := io.ReadFull(file, prefix[:])
	if isLegacyV1(prefix[:n]) {
		return SegmentHeader{}, legacyV1UnsupportedError(path)
	}
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
	if isLegacyV1(prefix[:n]) {
		return corruptResult(result, legacyV1UnsupportedError(path))
	}
	if n < len(Magic) && bytes.Equal(prefix[:n], Magic[:n]) {
		return corruptResult(result, fmt.Errorf("incomplete file magic: %d of %d bytes", n, len(Magic)))
	}
	if n == len(Magic) && bytes.Equal(prefix[:], Magic) {
		digest := sha256.New()
		_, _ = digest.Write(prefix[:])
		return streamBinary(file, result, handle, digest)
	}
	return corruptResult(result, fmt.Errorf("unsupported .jhlog format; expected %s", FormatVersionString))
}

func isLegacyV1(prefix []byte) bool {
	return len(prefix) >= len(legacyV1Magic) && bytes.Equal(prefix[:len(legacyV1Magic)], legacyV1Magic)
}

func legacyV1UnsupportedError(path string) error {
	return fmt.Errorf(
		"%s: legacy JHLOG 1.0 is not supported; capture a new log (expected JHLOG %s)",
		path,
		FormatVersionString,
	)
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
		event, nextState, err := decodeRecord(body, state, header, symbolNamespace, source, RecordPosition{
			ChunkSequence: metadata.Sequence,
			RecordIndex:   index,
		}, runtimeCallScratch)
		if err != nil {
			return summary, fmt.Errorf("record %d: %w", index, err)
		}
		state = nextState
		if index == 0 {
			summary.firstRecordType = event.Type
		}
		summary.lastRecordType = event.Type
		result.RawRecordBytes += recordBytes
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
		if event.Dictionary != nil {
			if event.Dictionary.Kind != DictStableSymbol {
				kinds[event.Dictionary.ID] = event.Dictionary.Kind
				if event.Dictionary.Encoding == 0 {
					dict[event.Dictionary.ID] = event.Dictionary.Value
				}
			}
		}
		if event.Quality != nil {
			if result.LatestQuality != nil {
				if err := ValidateQualityProgression(*result.LatestQuality, *event.Quality); err != nil {
					return chunkDecodeSummary{}, fmt.Errorf("quality snapshot: %w", err)
				}
			}
			if result.LatestQuality == nil || event.Quality.Sequence >= result.LatestQuality.Sequence {
				quality := cloneQualitySnapshot(*event.Quality)
				result.LatestQuality = &quality
			}
			continue
		}
		if event.SegmentEnd != nil {
			end := *event.SegmentEnd
			result.SegmentEnd = &end
			continue
		}
		if event.LogGrowth != nil {
			applyLogGrowthRecord(result, event.LogGrowth)
			continue
		}
		if event.Type == EventRuntimeCall {
			for rowIndex := range event.runtimeCalls {
				row := event.runtimeCalls[rowIndex]
				if math.MaxUint64-result.RuntimeGraphLogicalCalls < row.count {
					return summary, fmt.Errorf("runtime graph logical call total overflows uint64")
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
					Present: true,
					Screen:  row.screen,
					Owner:   row.caller,
					Flow:    row.flow,
					Step:    row.step,
				}
				if rowIndex > 0 {
					expanded.DeltaUS = 0
					expanded.DeltaMS = 0
				}
				result.Events++
				if err := handle(expanded, dict); err != nil {
					return summary, callbackError{err}
				}
			}
			continue
		}
		if event.Type.IsSemanticData() {
			result.Events++
		}
		if err := handle(event, dict); err != nil {
			return summary, callbackError{err}
		}
	}
	if reader.Len() != 0 {
		return summary, fmt.Errorf("record count %d leaves %d unparsed raw bytes", metadata.RecordCount, reader.Len())
	}
	return summary, nil
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
	header SegmentHeader,
	symbolNamespace string,
	source string,
	position RecordPosition,
	runtimeCallScratch []runtimeCallRow,
) (Event, recordDecodeState, error) {
	reader := bytes.NewReader(body)
	eventType, err := binary.ReadUvarint(reader)
	if err != nil {
		return Event{}, state, fmt.Errorf("type: %w", err)
	}
	flags, err := binary.ReadUvarint(reader)
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
		rawDelta, err := binary.ReadUvarint(reader)
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
		threadID, err := binary.ReadUvarint(reader)
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
			context, err := readAttribution(reader, symbolNamespace)
			if err != nil {
				return Event{}, state, err
			}
			event.Attribution = context
			nextState.lastContext = context
			nextState.hasContext = true
		}
	}
	if flags&uint64(EnvelopeHasAttributes) != 0 {
		attributes, err := binary.ReadUvarint(reader)
		if err != nil {
			return Event{}, state, fmt.Errorf("event attributes: %w", err)
		}
		event.Flags |= attributes
	}

	if err := decodeEventPayload(reader, &event, header, symbolNamespace, runtimeCallScratch); err != nil {
		return Event{}, state, err
	}
	if reader.Len() != 0 {
		return Event{}, state, fmt.Errorf("event type %d leaves %d trailing payload bytes", event.Type, reader.Len())
	}
	return event, nextState, nil
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

func readAttribution(reader *bytes.Reader, symbolNamespace string) (AttributionContext, error) {
	mask, err := binary.ReadUvarint(reader)
	if err != nil {
		return AttributionContext{}, fmt.Errorf("context presence mask: %w", err)
	}
	if mask&^uint64(0x0f) != 0 {
		return AttributionContext{}, fmt.Errorf("unsupported context presence bits 0x%x", mask&^uint64(0x0f))
	}
	context := AttributionContext{Present: true}
	targets := []struct {
		bit uint64
		ref *SymbolRef
	}{
		{1 << 0, &context.Screen},
		{1 << 1, &context.Owner},
		{1 << 2, &context.Flow},
		{1 << 3, &context.Step},
	}
	for _, item := range targets {
		if mask&item.bit == 0 {
			continue
		}
		ref, err := readSymbolRef(reader, symbolNamespace)
		if err != nil {
			return AttributionContext{}, fmt.Errorf("context symbol: %w", err)
		}
		*item.ref = ref
	}
	return context, nil
}

func readSymbolRef(reader *bytes.Reader, symbolNamespace string) (SymbolRef, error) {
	token, err := binary.ReadUvarint(reader)
	if err != nil {
		return SymbolRef{}, err
	}
	switch {
	case token == 0:
		return SymbolRef{}, nil
	case token == 1:
		var raw [8]byte
		if _, err := io.ReadFull(reader, raw[:]); err != nil {
			return SymbolRef{}, err
		}
		return SymbolRef{ID: binary.LittleEndian.Uint64(raw[:]), Namespace: symbolNamespace, Stable: true}, nil
	case token&1 == 0:
		return LocalSymbol(token >> 1), nil
	default:
		return SymbolRef{}, fmt.Errorf("reserved symbol token %d", token)
	}
}

func decodeEventPayload(
	reader *bytes.Reader,
	event *Event,
	header SegmentHeader,
	symbolNamespace string,
	runtimeCallScratch []runtimeCallRow,
) error {
	read := func(name string) (uint64, error) {
		value, err := binary.ReadUvarint(reader)
		if err != nil {
			return 0, fmt.Errorf("%s: %w", name, err)
		}
		return value, nil
	}
	readRef := func(name string) (SymbolRef, error) {
		ref, err := readSymbolRef(reader, symbolNamespace)
		if err != nil {
			return SymbolRef{}, fmt.Errorf("%s: %w", name, err)
		}
		return ref, nil
	}
	readValues := func(names ...string) ([]uint64, error) {
		values := make([]uint64, len(names))
		for i, name := range names {
			value, err := read(name)
			if err != nil {
				return nil, err
			}
			values[i] = value
		}
		return values, nil
	}

	switch event.Type {
	case EventDictionary:
		values, err := readValues("dictionary kind", "dictionary local id", "dictionary encoding", "dictionary data length")
		if err != nil {
			return err
		}
		if values[3] > uint64(reader.Len()) {
			return fmt.Errorf("dictionary data length %d exceeds remaining %d", values[3], reader.Len())
		}
		data := make([]byte, int(values[3]))
		if _, err := io.ReadFull(reader, data); err != nil {
			return fmt.Errorf("dictionary data: %w", err)
		}
		entry := &DictionaryEntry{Kind: DictKind(values[0]), ID: values[1], Encoding: values[2], Data: data}
		if entry.Kind > DictStableSymbol {
			return fmt.Errorf("unsupported dictionary kind %d", entry.Kind)
		}
		if entry.Encoding != 0 {
			return fmt.Errorf("unsupported dictionary encoding %d for value %d", entry.Encoding, entry.ID)
		}
		if !utf8.Valid(data) {
			return fmt.Errorf("dictionary value %d is not valid UTF-8", entry.ID)
		}
		entry.Value = string(data)
		event.Dictionary = entry
	case EventSession:
		refs := make([]SymbolRef, 12)
		for i, name := range []string{
			"app version", "build", "device",
		} {
			ref, err := readRef(name)
			if err != nil {
				return err
			}
			refs[i] = ref
		}
		sdk, err := read("SDK")
		if err != nil {
			return err
		}
		for i, name := range []string{
			"Android release", "security patch", "primary ABI", "supported ABIs",
			"manufacturer", "brand", "hardware", "board", "product",
		} {
			ref, err := readRef(name)
			if err != nil {
				return err
			}
			refs[i+3] = ref
		}
		collectorFlags, err := read("collector flags")
		if err != nil {
			return err
		}
		if collectorFlags&^uint64(CollectorKnownMask) != 0 {
			return fmt.Errorf("unsupported collector flags 0x%x", collectorFlags)
		}
		event.Session = &SessionEvent{
			AppVersionRef: refs[0],
			BuildRef:      refs[1],
			DeviceRef:     refs[2],
			SDKInt:        sdk, CollectorFlags: collectorFlags, ProcessName: header.ProcessName,
			AndroidReleaseRef: refs[3],
			SecurityPatchRef:  refs[4],
			PrimaryABIRef:     refs[5],
			SupportedABIsRef:  refs[6],
			ManufacturerRef:   refs[7],
			BrandRef:          refs[8],
			HardwareRef:       refs[9],
			BoardRef:          refs[10],
			ProductRef:        refs[11],
			DeviceRooted:      event.Flags&uint64(FlagDeviceRooted) != 0,
		}
	case EventContext:
		values, err := readValues("network", "battery percent", "available memory", "battery state", "battery temperature", "rx bytes", "tx bytes", "total memory", "free storage", "total storage")
		if err != nil {
			return err
		}
		if values[0] > uint64(NetworkVPN) {
			return fmt.Errorf("unsupported network kind %d", values[0])
		}
		if values[1] > 100 {
			return fmt.Errorf("battery percent %d exceeds 100", values[1])
		}
		if values[7] > 0 && values[2] > values[7] {
			return fmt.Errorf("available memory %d exceeds total memory %d", values[2], values[7])
		}
		if values[9] > 0 && values[8] > values[9] {
			return fmt.Errorf("free storage %d exceeds total storage %d", values[8], values[9])
		}
		event.Context = &ContextEvent{
			Network: NetworkKind(values[0]), BatteryPct: values[1], AvailMemoryKB: values[2],
			BatteryState: values[3], BatteryTempDeciC: decodeSVarint(values[4]),
			RxBytes: values[5], TxBytes: values[6], TotalMemoryKB: values[7],
			FreeStorageKB: values[8], TotalStorageKB: values[9],
			LowMemory:        event.Flags&uint64(FlagContextLowMemory) != 0,
			NetworkMetered:   event.Flags&uint64(FlagNetworkMetered) != 0,
			NetworkValidated: event.Flags&uint64(FlagNetworkValidated) != 0,
			NetworkVPN:       event.Flags&uint64(FlagNetworkVPN) != 0,
		}
	case EventHTTP:
		route, err := readRef("route")
		if err != nil {
			return err
		}
		values, err := readValues("duration", "DNS", "connect", "TTFB", "status", "rx bytes", "tx bytes")
		if err != nil {
			return err
		}
		if values[4] > uint64(Status5xx) {
			return fmt.Errorf("unsupported HTTP status class %d", values[4])
		}
		for index, phase := range values[1:4] {
			if phase > values[0] {
				return fmt.Errorf("HTTP phase %d duration %d exceeds request duration %d", index, phase, values[0])
			}
		}
		event.HTTP = &HTTPEvent{
			RouteRef: route, DurationMS: values[0], DNSMS: values[1],
			ConnectMS: values[2], TTFBMS: values[3], Status: StatusClass(values[4]), RxBytes: values[5], TxBytes: values[6],
		}
	case EventUIWindow:
		values, err := readValues("window", "frames", "jank", "source", "frame deadline")
		if err != nil {
			return err
		}
		buckets, err := readValues(
			"frames <=8ms", "frames <=12ms", "frames <=16ms", "frames <=20ms", "frames <=24ms",
			"frames <=32ms", "frames <=40ms", "frames <=50ms", "frames <=67ms", "frames <=100ms",
			"frames <=250ms", "frames <=1000ms", "frames >1000ms",
		)
		if err != nil {
			return err
		}
		window := &UIWindowEvent{
			WindowMS: values[0], FrameCount: values[1], JankCount: values[2], Source: UIFrameSource(values[3]),
			FrameDeadlineUS: values[4], FrameDurationBuckets: buckets,
		}
		if err := validateUIWindow(window); err != nil {
			return err
		}
		window.P50MS = UIFrameHistogramQuantileMS(buckets, 50)
		window.P95MS = UIFrameHistogramQuantileMS(buckets, 95)
		window.P99MS = UIFrameHistogramQuantileMS(buckets, 99)
		event.UIWindow = window
	case EventStall:
		stack, err := readRef("stack")
		if err != nil {
			return err
		}
		duration, err := read("duration")
		if err != nil {
			return err
		}
		event.Stall = &StallEvent{
			StackRef: stack, DurationMS: duration,
		}
	case EventMemory:
		values, err := readValues("PSS", "Java heap", "native heap")
		if err != nil {
			return err
		}
		event.Memory = &MemoryEvent{PSSKB: values[0], JavaHeapKB: values[1], NativeHeapKB: values[2]}
	case EventRetained:
		classRef, err := readRef("retained class")
		if err != nil {
			return err
		}
		holderRef, err := readRef("holder")
		if err != nil {
			return err
		}
		values, err := readValues("age", "count")
		if err != nil {
			return err
		}
		value, err := read("retention evidence")
		if err != nil {
			return err
		}
		evidence := RetentionEvidence(value)
		if evidence != RetentionEvidenceTimeOnly && evidence != RetentionEvidenceAfterExplicitGC {
			return fmt.Errorf("unsupported retention evidence %d", value)
		}
		event.Retained = &RetainedEvent{
			ClassRef: classRef, HolderRef: holderRef,
			AgeMS: values[0], Count: values[1], Evidence: evidence,
		}
	case EventCounter, EventGauge:
		metricRef, err := readRef("metric")
		if err != nil {
			return err
		}
		values, err := readValues("value", "count", "sum", "max", "mode")
		if err != nil {
			return err
		}
		if values[1] == 0 {
			return fmt.Errorf("metric count must be positive")
		}
		if values[4] > uint64(MetricModeBooleanRate) {
			return fmt.Errorf("unsupported metric mode %d", values[4])
		}
		if values[3] > values[2] {
			return fmt.Errorf("metric max %d exceeds sum %d", values[3], values[2])
		}
		event.Metric = &MetricEvent{MetricRef: metricRef, Value: values[0], Count: values[1], Sum: values[2], Max: values[3], Mode: MetricMode(values[4])}
	case EventLogSpam:
		sourceRef, err := readRef("log source")
		if err != nil {
			return err
		}
		values, err := readValues("level", "count")
		if err != nil {
			return err
		}
		event.LogSpam = &LogSpamEvent{
			SourceRef: sourceRef, Level: values[0], Count: values[1],
		}
	case EventProblem:
		kindRef, err := readRef("problem kind")
		if err != nil {
			return err
		}
		values, err := readValues("window", "count", "max")
		if err != nil {
			return err
		}
		event.Problem = &ProblemEvent{
			KindRef: kindRef, WindowMS: values[0], Count: values[1], MaxMS: values[2],
		}
	case EventRuntimeCall:
		rowCount, err := read("runtime call row count")
		if err != nil {
			return err
		}
		if rowCount == 0 || rowCount > MaxRuntimeCallBlockRows {
			return fmt.Errorf("runtime call row count %d is outside 1..%d", rowCount, MaxRuntimeCallBlockRows)
		}
		if rowCount > uint64(reader.Len()/8) {
			return fmt.Errorf("runtime call row count %d exceeds remaining payload", rowCount)
		}
		var calls []runtimeCallRow
		if int(rowCount) <= len(runtimeCallScratch) {
			calls = runtimeCallScratch[:int(rowCount)]
			clear(calls)
		} else {
			calls = make([]runtimeCallRow, int(rowCount))
		}
		for index := range calls {
			ref, err := readRef("screen")
			if err != nil {
				return err
			}
			calls[index].screen = ref
		}
		for index := range calls {
			ref, err := readRef("caller")
			if err != nil {
				return err
			}
			calls[index].caller = ref
		}
		for index := range calls {
			ref, err := readRef("flow")
			if err != nil {
				return err
			}
			calls[index].flow = ref
		}
		for index := range calls {
			ref, err := readRef("step")
			if err != nil {
				return err
			}
			calls[index].step = ref
		}
		for index := range calls {
			ref, err := readRef("callee")
			if err != nil {
				return err
			}
			calls[index].callee = ref
		}
		for index := range calls {
			value, err := read("count")
			if err != nil {
				return err
			}
			calls[index].count = value
		}
		for index := range calls {
			value, err := read("total")
			if err != nil {
				return err
			}
			calls[index].total = value
		}
		for index := range calls {
			value, err := read("max")
			if err != nil {
				return err
			}
			calls[index].max = value
		}
		for index := range calls {
			if calls[index].count == 0 {
				return fmt.Errorf("runtime call row %d has zero logical calls", index)
			}
			if calls[index].max > calls[index].total {
				return fmt.Errorf(
					"runtime call row %d max duration %d exceeds total %d",
					index,
					calls[index].max,
					calls[index].total,
				)
			}
		}
		event.runtimeCalls = calls
	case EventProcessExit:
		values, err := readValues("exit reason", "exit timestamp", "exit importance", "exit PSS", "exit RSS")
		if err != nil {
			return err
		}
		processRef, err := readRef("exit process")
		if err != nil {
			return err
		}
		if values[1] == 0 || values[1] > math.MaxInt64 {
			return fmt.Errorf("process exit timestamp must be positive")
		}
		event.ProcessExit = &ProcessExitEvent{
			Reason: values[0], TimestampUnixMS: values[1], Importance: values[2],
			PSSKB: values[3], RSSKB: values[4], ProcessRef: processRef,
		}
	case EventIO:
		values, err := readValues("I/O operation", "I/O duration", "I/O bytes")
		if err != nil {
			return err
		}
		operation := IOOperationKind(values[0])
		if operation <= IOOperationUnknown || operation > IOOperationContentWrite {
			return fmt.Errorf("unsupported I/O operation %d", operation)
		}
		event.IO = &IOEvent{Operation: operation, DurationUS: values[1], Bytes: values[2]}
	case EventQualitySnapshot:
		values, err := readValues("quality sequence", "quality captured time", "quality entry count")
		if err != nil {
			return err
		}
		entryCount := values[2]
		if entryCount > uint64(reader.Len()/2) {
			return fmt.Errorf("quality entry count %d exceeds remaining payload", entryCount)
		}
		quality := &QualitySnapshot{Sequence: values[0], CapturedElapsedUS: values[1], Counters: make(map[uint64]uint64, int(entryCount))}
		for i := uint64(0); i < entryCount; i++ {
			entry, err := readValues("quality counter id", "quality counter value")
			if err != nil {
				return err
			}
			if _, duplicate := quality.Counters[entry[0]]; duplicate {
				return fmt.Errorf("duplicate quality counter id %d", entry[0])
			}
			if !IsKnownQualityCounter(entry[0]) {
				return fmt.Errorf("unsupported quality counter id %d", entry[0])
			}
			quality.Counters[entry[0]] = entry[1]
		}
		event.Quality = quality
	case EventSegmentEnd:
		values, err := readValues("segment end reason", "total event records", "total dictionary records", "last quality sequence")
		if err != nil {
			return err
		}
		reason := SegmentEndReason(values[0])
		if !reason.supported() {
			return fmt.Errorf("unsupported segment end reason %d", reason)
		}
		event.SegmentEnd = &SegmentEndEvent{Reason: reason, TotalEventRecords: values[1], TotalDictionaryRecords: values[2], LastQualitySequence: values[3]}
	case EventLogGrowth:
		values, err := readValues("log-growth kind", "log-growth payload length")
		if err != nil {
			return err
		}
		if values[1] > uint64(reader.Len()) {
			return fmt.Errorf("log-growth payload length %d exceeds remaining %d", values[1], reader.Len())
		}
		raw := make([]byte, int(values[1]))
		if _, err := io.ReadFull(reader, raw); err != nil {
			return fmt.Errorf("log-growth payload: %w", err)
		}
		record, err := decodeLogGrowthRecord(LogGrowthRecordKind(values[0]), raw)
		if err != nil {
			return err
		}
		event.LogGrowth = record
	default:
		return fmt.Errorf("unsupported event type %d", event.Type)
	}
	return nil
}
