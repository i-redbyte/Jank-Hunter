package jhlog

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
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

func StreamFileWithWarnings(path string, handle EventHandler) ([]string, error) {
	result, err := StreamFileWithResult(path, handle)
	return result.Warnings, err
}

// ReadSessionHeader reads only the bounded v9 file header. It does not scan,
// allocate for, decompress, or validate any event chunk in the file.
func ReadSessionHeader(path string) (SegmentHeader, error) {
	file, err := os.Open(path)
	if err != nil {
		return SegmentHeader{}, err
	}
	defer file.Close()

	var prefix [magicSize]byte
	if _, err := io.ReadFull(file, prefix[:]); err != nil {
		return SegmentHeader{}, fmt.Errorf("%s: read .jhlog magic: %w", path, err)
	}
	if !bytes.Equal(prefix[:], Magic) {
		if bytes.Equal(prefix[:7], Magic[:7]) {
			return SegmentHeader{}, fmt.Errorf("%s: unsupported jhlog version %d, CLI supports %d", path, prefix[7], FormatVersion)
		}
		return SegmentHeader{}, fmt.Errorf("%s: invalid v9 .jhlog magic", path)
	}
	header, err := readV9Header(file)
	if err != nil {
		return SegmentHeader{}, fmt.Errorf("%s: read v9 session header: %w", path, err)
	}
	if err := validateV9Header(header); err != nil {
		return SegmentHeader{}, fmt.Errorf("%s: invalid v9 session header: %w", path, err)
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
	var prefix [magicSize]byte
	n, prefixErr := io.ReadFull(file, prefix[:])
	if prefixErr != nil && !errors.Is(prefixErr, io.EOF) && !errors.Is(prefixErr, io.ErrUnexpectedEOF) {
		return result, prefixErr
	}
	if n < len(Magic) && bytes.Equal(prefix[:n], Magic[:n]) {
		return corruptResult(result, fmt.Errorf("incomplete file magic: %d of %d bytes", n, len(Magic)))
	}
	if n == len(Magic) && bytes.Equal(prefix[:7], Magic[:7]) {
		result.Version = prefix[7]
		if prefix[7] != FormatVersion {
			return corruptResult(result, fmt.Errorf("unsupported jhlog version %d, CLI supports %d", prefix[7], FormatVersion))
		}
		return streamBinaryV9(file, result, handle)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return result, err
	}
	result.Version = 0
	return streamJSONL(file, result, handle)
}

func newStreamResult(source string) StreamResult {
	return StreamResult{
		Source:            source,
		Version:           FormatVersion,
		Status:            SegmentStatusOpenClean,
		RecordBytesByType: map[EventType]uint64{},
		RecordsByType:     map[EventType]uint64{},
	}
}

func streamBinaryV9(file *os.File, result StreamResult, handle EventHandler) (StreamResult, error) {
	header, err := readV9Header(file)
	if err != nil {
		return corruptResult(result, err)
	}
	if err := validateV9Header(header); err != nil {
		return corruptResult(result, fmt.Errorf("invalid v9 session header: %w", err))
	}
	result.Header = header

	dict := map[uint64]string{}
	kinds := map[uint64]DictKind{}
	var expectedSequence uint32
	var dataRecords uint64
	var dictionaryRecords uint64
	for {
		chunkStart, err := file.Seek(0, io.SeekCurrent)
		if err != nil {
			return result, err
		}
		var rawHeader [chunkHeaderSize]byte
		n, err := io.ReadFull(file, rawHeader[:])
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
		if _, err := io.ReadFull(file, stored); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				result.Status = SegmentStatusOpenWithTail
				result.TailBytes = physicalTailBytes(file, chunkStart, chunkHeaderSize)
				return result, nil
			}
			return result, fmt.Errorf("%s: read chunk %d payload: %w", result.Source, metadata.Sequence, err)
		}
		var trailer [commitTrailerSize]byte
		if _, err := io.ReadFull(file, trailer[:]); err != nil {
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

		chunkSummary, err := decodeChunkRecords(raw, metadata, header, result.Source, dict, kinds, handle, &result)
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
		if n, err := file.Read(extra[:]); n != 0 || !errors.Is(err, io.EOF) {
			if err != nil && !errors.Is(err, io.EOF) {
				return result, fmt.Errorf("%s: verify EOF after FINAL: %w", result.Source, err)
			}
			return corruptResult(result, fmt.Errorf("bytes found after FINAL chunk"))
		}
		result.Status = SegmentStatusClosedClean
		result.Sealed = true
		result.TailBytes = 0
		return result, nil
	}
}

func validateV9Header(header SegmentHeader) error {
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

func readV9Header(reader io.Reader) (SegmentHeader, error) {
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
	source string,
	dict map[uint64]string,
	kinds map[uint64]DictKind,
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
		event, known, nextState, err := decodeRecord(body, state, header, source, RecordPosition{
			ChunkSequence: metadata.Sequence,
			RecordIndex:   index,
		})
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
		result.RecordsByType[event.Type]++
		result.TotalRecords++
		result.Warnings = append(result.Warnings, event.Warnings...)

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
		default:
			summary.dataRecords++
			result.DataRecords++
		}
		if !known {
			continue
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

type callbackError struct{ error }

func decodeRecord(
	body []byte,
	state recordDecodeState,
	header SegmentHeader,
	source string,
	position RecordPosition,
) (Event, bool, recordDecodeState, error) {
	reader := bytes.NewReader(body)
	eventType, err := binary.ReadUvarint(reader)
	if err != nil {
		return Event{}, false, state, fmt.Errorf("type: %w", err)
	}
	flags, err := binary.ReadUvarint(reader)
	if err != nil {
		return Event{}, false, state, fmt.Errorf("envelope flags: %w", err)
	}
	knownEnvelopeFlags := uint64(EnvelopeHasTime | EnvelopeHasThread | EnvelopeHasContext | EnvelopeSameContext | EnvelopeHasAttributes)
	if flags&^knownEnvelopeFlags != 0 {
		return Event{}, false, state, fmt.Errorf("unsupported envelope flags 0x%x", flags&^knownEnvelopeFlags)
	}
	if flags&uint64(EnvelopeSameContext) != 0 && flags&uint64(EnvelopeHasContext) == 0 {
		return Event{}, false, state, fmt.Errorf("SAME_CONTEXT without HAS_CONTEXT")
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
			return Event{}, false, state, fmt.Errorf("producer timestamp delta: %w", err)
		}
		delta := decodeSVarint(rawDelta)
		elapsed, err := addSignedTimestamp(state.lastElapsedUS, delta)
		if err != nil {
			return Event{}, false, state, err
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
			return Event{}, false, state, fmt.Errorf("producer thread: %w", err)
		}
		event.Producer.HasThread = true
		event.Producer.ThreadID = threadID
	}
	if flags&uint64(EnvelopeHasContext) != 0 {
		if flags&uint64(EnvelopeSameContext) != 0 {
			if !state.hasContext {
				return Event{}, false, state, fmt.Errorf("SAME_CONTEXT without prior context in chunk")
			}
			event.Attribution = state.lastContext
			event.Attribution.Present = true
			event.Flags |= uint64(FlagSameContext)
		} else {
			context, err := readAttribution(reader, header.SymbolNamespace)
			if err != nil {
				return Event{}, false, state, err
			}
			event.Attribution = context
			nextState.lastContext = context
			nextState.hasContext = true
		}
		event.Flags |= compatibilityContextFlags(event.Attribution)
	}
	if flags&uint64(EnvelopeHasAttributes) != 0 {
		attributes, err := binary.ReadUvarint(reader)
		if err != nil {
			return Event{}, false, state, fmt.Errorf("event attributes: %w", err)
		}
		event.Flags |= attributes
	}

	known, err := decodeEventPayload(reader, &event, header)
	if err != nil {
		return Event{}, false, state, err
	}
	return event, known, nextState, nil
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

func readAttribution(reader *bytes.Reader, namespace []byte) (AttributionContext, error) {
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
		ref, err := readSymbolRef(reader, namespace)
		if err != nil {
			return AttributionContext{}, fmt.Errorf("context symbol: %w", err)
		}
		*item.ref = ref
	}
	return context, nil
}

func compatibilityContextFlags(context AttributionContext) uint64 {
	var flags uint64
	if !context.Screen.IsUnknown() {
		flags |= uint64(FlagHasScreen)
	}
	if !context.Owner.IsUnknown() {
		flags |= uint64(FlagHasOwner)
	}
	if !context.Flow.IsUnknown() {
		flags |= uint64(FlagHasFlow)
	}
	if !context.Step.IsUnknown() {
		flags |= uint64(FlagHasStep)
	}
	return flags
}

func readSymbolRef(reader *bytes.Reader, namespace []byte) (SymbolRef, error) {
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
		return StableSymbolInNamespace(binary.LittleEndian.Uint64(raw[:]), namespace), nil
	case token&1 == 0:
		return LocalSymbol(token >> 1), nil
	default:
		return SymbolRef{}, fmt.Errorf("reserved symbol token %d", token)
	}
}

func decodeEventPayload(reader *bytes.Reader, event *Event, header SegmentHeader) (bool, error) {
	read := func(name string) (uint64, error) {
		value, err := binary.ReadUvarint(reader)
		if err != nil {
			return 0, fmt.Errorf("%s: %w", name, err)
		}
		return value, nil
	}
	readRef := func(name string) (SymbolRef, error) {
		ref, err := readSymbolRef(reader, header.SymbolNamespace)
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
			return true, err
		}
		if values[3] > uint64(reader.Len()) {
			return true, fmt.Errorf("dictionary data length %d exceeds remaining %d", values[3], reader.Len())
		}
		data := make([]byte, int(values[3]))
		if _, err := io.ReadFull(reader, data); err != nil {
			return true, fmt.Errorf("dictionary data: %w", err)
		}
		entry := &DictionaryEntry{Kind: DictKind(values[0]), ID: values[1], Encoding: values[2], Data: data}
		if entry.Encoding == 0 {
			if !utf8.Valid(data) {
				return true, fmt.Errorf("dictionary value %d is not valid UTF-8", entry.ID)
			}
			entry.Value = string(data)
		} else {
			event.Warnings = append(event.Warnings, fmt.Sprintf("dictionary value %d uses unsupported encoding %d", entry.ID, entry.Encoding))
		}
		event.Dictionary = entry
	case EventSession:
		refs := make([]SymbolRef, 12)
		for i, name := range []string{
			"app version", "build", "device",
		} {
			ref, err := readRef(name)
			if err != nil {
				return true, err
			}
			refs[i] = ref
		}
		sdk, err := read("SDK")
		if err != nil {
			return true, err
		}
		for i, name := range []string{
			"Android release", "security patch", "primary ABI", "supported ABIs",
			"manufacturer", "brand", "hardware", "board", "product",
		} {
			ref, err := readRef(name)
			if err != nil {
				return true, err
			}
			refs[i+3] = ref
		}
		event.Session = &SessionEvent{
			AppVersionRef: refs[0], AppVersionID: refs[0].LegacyID(),
			BuildRef: refs[1], BuildID: refs[1].LegacyID(),
			DeviceRef: refs[2], DeviceID: refs[2].LegacyID(),
			SDKInt: sdk, ProcessName: header.ProcessName,
			AndroidReleaseRef: refs[3], AndroidReleaseID: refs[3].LegacyID(),
			SecurityPatchRef: refs[4], SecurityPatchID: refs[4].LegacyID(),
			PrimaryABIRef: refs[5], PrimaryABIID: refs[5].LegacyID(),
			SupportedABIsRef: refs[6], SupportedABIsID: refs[6].LegacyID(),
			ManufacturerRef: refs[7], ManufacturerID: refs[7].LegacyID(),
			BrandRef: refs[8], BrandID: refs[8].LegacyID(),
			HardwareRef: refs[9], HardwareID: refs[9].LegacyID(),
			BoardRef: refs[10], BoardID: refs[10].LegacyID(),
			ProductRef: refs[11], ProductID: refs[11].LegacyID(),
			DeviceRooted: event.Flags&uint64(FlagDeviceRooted) != 0,
		}
	case EventContext:
		values, err := readValues("network", "battery percent", "available memory", "battery state", "battery temperature", "rx bytes", "tx bytes", "total memory", "free storage", "total storage")
		if err != nil {
			return true, err
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
			return true, err
		}
		values, err := readValues("duration", "DNS", "connect", "TTFB", "status", "rx bytes", "tx bytes")
		if err != nil {
			return true, err
		}
		event.HTTP = &HTTPEvent{
			OwnerRef: event.Attribution.Owner, OwnerID: event.Attribution.Owner.LegacyID(),
			RouteRef: route, RouteID: route.LegacyID(), DurationMS: values[0], DNSMS: values[1],
			ConnectMS: values[2], TTFBMS: values[3], Status: StatusClass(values[4]), RxBytes: values[5], TxBytes: values[6],
		}
	case EventUIWindow:
		values, err := readValues("window", "frames", "jank", "p50", "p95", "p99")
		if err != nil {
			return true, err
		}
		event.UIWindow = &UIWindowEvent{
			ScreenRef: event.Attribution.Screen, ScreenID: event.Attribution.Screen.LegacyID(),
			WindowMS: values[0], FrameCount: values[1], JankCount: values[2], P50MS: values[3], P95MS: values[4], P99MS: values[5],
		}
	case EventStall:
		stack, err := readRef("stack")
		if err != nil {
			return true, err
		}
		duration, err := read("duration")
		if err != nil {
			return true, err
		}
		event.Stall = &StallEvent{
			OwnerRef: event.Attribution.Owner, OwnerID: event.Attribution.Owner.LegacyID(),
			StackRef: stack, StackID: stack.LegacyID(), DurationMS: duration,
		}
	case EventMemory:
		values, err := readValues("PSS", "Java heap", "native heap")
		if err != nil {
			return true, err
		}
		event.Memory = &MemoryEvent{PSSKB: values[0], JavaHeapKB: values[1], NativeHeapKB: values[2]}
	case EventRetained:
		classRef, err := readRef("retained class")
		if err != nil {
			return true, err
		}
		holderRef, err := readRef("holder")
		if err != nil {
			return true, err
		}
		values, err := readValues("age", "count")
		if err != nil {
			return true, err
		}
		evidence := RetentionEvidenceTimeOnly
		if reader.Len() > 0 {
			value, err := read("retention evidence")
			if err != nil {
				return true, err
			}
			evidence = RetentionEvidence(value)
			if evidence != RetentionEvidenceTimeOnly && evidence != RetentionEvidenceAfterExplicitGC {
				return true, fmt.Errorf("unsupported retention evidence %d", value)
			}
		}
		event.Retained = &RetainedEvent{
			ScreenID: event.Attribution.Screen.LegacyID(), OwnerID: event.Attribution.Owner.LegacyID(),
			FlowID: event.Attribution.Flow.LegacyID(), StepID: event.Attribution.Step.LegacyID(),
			ClassRef: classRef, ClassID: classRef.LegacyID(), HolderRef: holderRef, HolderID: holderRef.LegacyID(),
			AgeMS: values[0], Count: values[1], Evidence: evidence,
		}
	case EventCounter, EventGauge:
		metricRef, err := readRef("metric")
		if err != nil {
			return true, err
		}
		values, err := readValues("value", "count", "sum", "max", "mode")
		if err != nil {
			return true, err
		}
		event.Metric = &MetricEvent{MetricRef: metricRef, MetricID: metricRef.LegacyID(), Value: values[0], Count: values[1], Sum: values[2], Max: values[3], Mode: MetricMode(values[4])}
	case EventFlow:
		values, err := readValues("phase", "instance id")
		if err != nil {
			return true, err
		}
		event.Flow = &FlowEvent{
			ScreenID: event.Attribution.Screen.LegacyID(), OwnerID: event.Attribution.Owner.LegacyID(),
			FlowID: event.Attribution.Flow.LegacyID(), StepID: event.Attribution.Step.LegacyID(),
			Phase: values[0], InstanceID: values[1],
		}
	case EventLogSpam:
		sourceRef, err := readRef("log source")
		if err != nil {
			return true, err
		}
		values, err := readValues("level", "count")
		if err != nil {
			return true, err
		}
		event.LogSpam = &LogSpamEvent{
			ScreenID: event.Attribution.Screen.LegacyID(), OwnerID: event.Attribution.Owner.LegacyID(),
			FlowID: event.Attribution.Flow.LegacyID(), StepID: event.Attribution.Step.LegacyID(),
			SourceRef: sourceRef, SourceID: sourceRef.LegacyID(), Level: values[0], Count: values[1],
		}
	case EventProblem:
		kindRef, err := readRef("problem kind")
		if err != nil {
			return true, err
		}
		values, err := readValues("window", "count", "max")
		if err != nil {
			return true, err
		}
		event.Problem = &ProblemEvent{
			ScreenID: event.Attribution.Screen.LegacyID(), OwnerID: event.Attribution.Owner.LegacyID(),
			FlowID: event.Attribution.Flow.LegacyID(), StepID: event.Attribution.Step.LegacyID(),
			KindRef: kindRef, KindID: kindRef.LegacyID(), WindowMS: values[0], Count: values[1], MaxMS: values[2],
		}
	case EventRuntimeCall:
		calleeRef, err := readRef("callee")
		if err != nil {
			return true, err
		}
		values, err := readValues("count", "total", "max")
		if err != nil {
			return true, err
		}
		event.RuntimeCall = &RuntimeCallEvent{
			ScreenID: event.Attribution.Screen.LegacyID(), CallerRef: event.Attribution.Owner, CallerID: event.Attribution.Owner.LegacyID(),
			FlowID: event.Attribution.Flow.LegacyID(), StepID: event.Attribution.Step.LegacyID(),
			CalleeRef: calleeRef, CalleeID: calleeRef.LegacyID(), Count: values[0], TotalMS: values[1], MaxMS: values[2],
		}
	case EventQualitySnapshot:
		values, err := readValues("quality sequence", "quality captured time", "quality entry count")
		if err != nil {
			return true, err
		}
		entryCount := values[2]
		if entryCount > uint64(reader.Len()/2) {
			return true, fmt.Errorf("quality entry count %d exceeds remaining payload", entryCount)
		}
		quality := &QualitySnapshot{Sequence: values[0], CapturedElapsedUS: values[1], Counters: make(map[uint64]uint64, int(entryCount))}
		for i := uint64(0); i < entryCount; i++ {
			entry, err := readValues("quality counter id", "quality counter value")
			if err != nil {
				return true, err
			}
			if _, duplicate := quality.Counters[entry[0]]; duplicate {
				return true, fmt.Errorf("duplicate quality counter id %d", entry[0])
			}
			quality.Counters[entry[0]] = entry[1]
		}
		event.Quality = quality
	case EventSegmentEnd:
		values, err := readValues("segment end reason", "total event records", "total dictionary records", "last quality sequence")
		if err != nil {
			return true, err
		}
		event.SegmentEnd = &SegmentEndEvent{Reason: SegmentEndReason(values[0]), TotalEventRecords: values[1], TotalDictionaryRecords: values[2], LastQualitySequence: values[3]}
	default:
		return false, nil
	}
	return true, nil
}

func streamJSONL(r io.Reader, result StreamResult, handle EventHandler) (StreamResult, error) {
	dict := map[uint64]string{}
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1024), 8*1024*1024)
	line := 0
	for scanner.Scan() {
		line++
		raw := bytes.TrimSpace(scanner.Bytes())
		if len(raw) == 0 {
			continue
		}
		var event Event
		if err := json.Unmarshal(raw, &event); err != nil {
			return result, fmt.Errorf("%s:%d: decode JSONL: %w", result.Source, line, err)
		}
		event.Source = result.Source
		result.TotalRecords++
		result.Warnings = append(result.Warnings, event.Warnings...)
		result.RecordBytesByType[event.Type] += uint64(len(raw))
		result.RecordsByType[event.Type]++
		result.RawRecordBytes += uint64(len(raw))
		if event.Type == EventDictionary || event.Dictionary != nil {
			result.DictionaryRecords++
			if event.Dictionary != nil && event.Dictionary.Kind != DictStableSymbol {
				dict[event.Dictionary.ID] = event.Dictionary.Value
			}
			if err := handle(event, dict); err != nil {
				return result, err
			}
			continue
		}
		if event.Quality != nil || event.SegmentEnd != nil || event.Type == EventQualitySnapshot || event.Type == EventSegmentEnd {
			result.ControlRecords++
			if event.Quality != nil && (result.LatestQuality == nil || event.Quality.Sequence >= result.LatestQuality.Sequence) {
				quality := cloneQualitySnapshot(*event.Quality)
				result.LatestQuality = &quality
			}
			if event.SegmentEnd != nil {
				end := *event.SegmentEnd
				result.SegmentEnd = &end
			}
			continue
		}
		result.DataRecords++
		if !event.Type.IsSemanticData() {
			continue
		}
		result.Events++
		if err := handle(event, dict); err != nil {
			return result, err
		}
	}
	if err := scanner.Err(); err != nil {
		return result, err
	}
	result.Status = SegmentStatusClosedClean
	return result, nil
}
