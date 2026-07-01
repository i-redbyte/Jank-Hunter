package jhlog

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

const firstMetadataDeltaUptimeThresholdMS uint64 = 60_000

func StreamFile(path string, handle func(Event, map[uint64]string) error) error {
	_, err := StreamFileWithWarnings(path, handle)
	return err
}

func StreamFileWithWarnings(path string, handle func(Event, map[uint64]string) error) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var prefix [8]byte
	n, err := io.ReadFull(file, prefix[:])
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, err
	}
	if n == len(Magic) && bytes.Equal(prefix[:7], Magic[:7]) {
		version := prefix[7]
		if version != FormatVersion {
			return nil, fmt.Errorf("%s: unsupported jhlog version %d, cli supports current pre-release version %d", path, version, FormatVersion)
		}
		body, closeBody, err := gzipBinaryBody(file)
		if err != nil {
			return nil, fmt.Errorf("%s: compressed jhlog body: %w", path, err)
		}
		if closeBody != nil {
			defer closeBody.Close()
		}
		log, err := streamBinary(body, path, version, handle)
		return log.Warnings, err
	}

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	return nil, streamJSONL(file, path, handle)
}

func gzipBinaryBody(r io.Reader) (io.Reader, io.Closer, error) {
	reader := bufio.NewReader(r)
	gzipReader, err := gzip.NewReader(reader)
	if err != nil {
		return nil, nil, err
	}
	return gzipReader, gzipReader, nil
}

func streamBinary(
	r io.Reader,
	source string,
	version uint8,
	handle func(Event, map[uint64]string) error,
) (Log, error) {
	log := Log{
		Source:  source,
		Version: version,
		Dict:    map[uint64]string{},
		Kinds:   map[uint64]DictKind{},
	}
	reader := bufio.NewReader(r)
	var currentMS uint64
	eventCount := 0
	decodeState := compactDecodeState{}
	for {
		event, deltaMS, err := readCompactEvent(reader, source, &decodeState)
		if errors.Is(err, io.EOF) {
			return log, nil
		}
		if err != nil {
			return toleratePartial(log, source, "compact event", err)
		}
		deltaMS = normalizeFirstMetadataDelta(event, deltaMS, eventCount)
		currentMS += deltaMS
		event.TimeMS = currentMS
		event.DeltaMS = deltaMS
		event.Source = source
		if event.Dictionary != nil {
			log.Dict[event.Dictionary.ID] = event.Dictionary.Value
			log.Kinds[event.Dictionary.ID] = event.Dictionary.Kind
		}
		if err := handle(event, log.Dict); err != nil {
			return log, err
		}
		eventCount++
	}
}

func normalizeFirstMetadataDelta(event Event, deltaMS uint64, eventCount int) uint64 {
	if eventCount != 0 || deltaMS < firstMetadataDeltaUptimeThresholdMS {
		return deltaMS
	}
	switch event.Type {
	case EventDictionary, EventSession:
		return 0
	default:
		return deltaMS
	}
}

type compactDecodeState struct {
	lastContext contextTuple
	hasContext  bool
}

type compactEventReader interface {
	io.Reader
	io.ByteReader
}

func readCompactEvent(reader compactEventReader, source string, decodeState *compactDecodeState) (Event, uint64, error) {
	header, err := reader.ReadByte()
	if err != nil {
		return Event{}, 0, err
	}
	eventType := EventType(header) & compactEventTypeMask
	deltaCode := (header >> compactHeaderDeltaShift) & 0x03
	deltaMS, err := readCompactDelta(reader, deltaCode)
	if err != nil {
		return Event{}, 0, fmt.Errorf("timestamp delta: %w", err)
	}
	flags := uint64(0)
	if header&compactHeaderHasFlags != 0 {
		flags, err = binary.ReadUvarint(reader)
		if err != nil {
			return Event{}, 0, fmt.Errorf("flags: %w", err)
		}
	}
	event := Event{
		Type:   eventType,
		Flags:  flags,
		Source: source,
	}
	if header&compactHeaderHasPayloadLen != 0 {
		payloadLen, err := binary.ReadUvarint(reader)
		if err != nil {
			return Event{}, 0, fmt.Errorf("payload length: %w", err)
		}
		if payloadLen > 4*1024*1024 {
			return Event{}, 0, fmt.Errorf("suspicious payload length %d", payloadLen)
		}
		payload := make([]byte, payloadLen)
		if _, err := io.ReadFull(reader, payload); err != nil {
			return Event{}, 0, fmt.Errorf("payload: %w", err)
		}
		if err := decodePayload(payload, &event, decodeState); err != nil {
			warning := err.Error()
			event.Warnings = append(event.Warnings, warning)
		}
		return event, deltaMS, nil
	}
	if err := decodeFixedCompactPayload(reader, &event, decodeState); err != nil {
		return Event{}, 0, fmt.Errorf("payload: %w", err)
	}
	return event, deltaMS, nil
}

func readCompactDelta(reader io.ByteReader, code byte) (uint64, error) {
	switch code {
	case compactDeltaZero:
		return 0, nil
	case compactDeltaUint8:
		b, err := reader.ReadByte()
		return uint64(b), err
	case compactDeltaUint16:
		lo, err := reader.ReadByte()
		if err != nil {
			return 0, err
		}
		hi, err := reader.ReadByte()
		if err != nil {
			return 0, err
		}
		return uint64(lo) | uint64(hi)<<8, nil
	default:
		return binary.ReadUvarint(reader)
	}
}

func decodeFixedCompactPayload(reader io.ByteReader, event *Event, decodeState *compactDecodeState) error {
	read := func() (uint64, error) {
		return binary.ReadUvarint(reader)
	}
	switch event.Type {
	case EventHTTP:
		values, err := readN(read, 9)
		if err != nil {
			return err
		}
		event.HTTP = &HTTPEvent{
			OwnerID: values[0], RouteID: values[1], DurationMS: values[2], DNSMS: values[3],
			ConnectMS: values[4], TTFBMS: values[5], Status: StatusClass(values[6]),
			RxBytes: values[7], TxBytes: values[8],
		}
	case EventUIWindow:
		values, err := readN(read, 7)
		if err != nil {
			return err
		}
		event.UIWindow = &UIWindowEvent{
			ScreenID: values[0], WindowMS: values[1], FrameCount: values[2], JankCount: values[3],
			P50MS: values[4], P95MS: values[5], P99MS: values[6],
		}
	case EventStall:
		values, err := readN(read, 3)
		if err != nil {
			return err
		}
		event.Stall = &StallEvent{OwnerID: values[0], StackID: values[1], DurationMS: values[2]}
	case EventMemory:
		values, err := readN(read, 3)
		if err != nil {
			return err
		}
		event.Memory = &MemoryEvent{PSSKB: values[0], JavaHeapKB: values[1], NativeHeapKB: values[2]}
	case EventRetained:
		screenID, ownerID, flowID, stepID, err := readContextIDs(read, event.Flags, decodeState)
		if err != nil {
			return err
		}
		values, err := readN(read, 4)
		if err != nil {
			return err
		}
		event.Retained = &RetainedEvent{
			ScreenID: screenID,
			OwnerID:  ownerID,
			FlowID:   flowID,
			StepID:   stepID,
			ClassID:  values[0],
			HolderID: values[1],
			AgeMS:    values[2],
			Count:    values[3],
		}
	case EventCounter, EventGauge:
		values, err := readN(read, 2)
		if err != nil {
			return err
		}
		event.Metric = metricFromValues(values, event.Type)
	case EventFlow, EventLogSpam, EventProblem, EventRuntimeCall:
		return fmt.Errorf("compact event type %d requires payload length", event.Type)
	default:
		return fmt.Errorf("compact event type %d requires payload length", event.Type)
	}
	return nil
}

func toleratePartial(log Log, source, stage string, err error) (Log, error) {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		log.Warnings = append(log.Warnings, fmt.Sprintf("%s: ignored partial trailing %s", source, stage))
		return log, nil
	}
	return log, fmt.Errorf("%s: read %s: %w", source, stage, err)
}

func decodePayload(payload []byte, event *Event, decodeState *compactDecodeState) error {
	reader := bytes.NewReader(payload)
	read := func() (uint64, error) {
		return binary.ReadUvarint(reader)
	}
	switch event.Type {
	case EventDictionary:
		kind, err := read()
		if err != nil {
			return err
		}
		id, err := read()
		if err != nil {
			return err
		}
		value, err := readDictionaryValue(reader)
		if err != nil {
			return err
		}
		event.Dictionary = &DictionaryEntry{Kind: DictKind(kind), ID: id, Value: value}
	case EventSession:
		values, err := readRemaining(reader)
		if err != nil {
			return err
		}
		if len(values) < 4 {
			return fmt.Errorf("session payload has %d values, expected at least 4", len(values))
		}
		session := &SessionEvent{
			AppVersionID: values[0],
			BuildID:      values[1],
			DeviceID:     values[2],
			SDKInt:       values[3],
			DeviceRooted: event.Flags&uint64(FlagDeviceRooted) != 0,
		}
		if len(values) > 4 {
			session.ProcessID = values[4]
		}
		if len(values) > 5 {
			session.AndroidReleaseID = values[5]
		}
		if len(values) > 6 {
			session.SecurityPatchID = values[6]
		}
		if len(values) > 7 {
			session.PrimaryABIID = values[7]
		}
		if len(values) > 8 {
			session.SupportedABIsID = values[8]
		}
		if len(values) > 9 {
			session.ManufacturerID = values[9]
		}
		if len(values) > 10 {
			session.BrandID = values[10]
		}
		if len(values) > 11 {
			session.HardwareID = values[11]
		}
		if len(values) > 12 {
			session.BoardID = values[12]
		}
		if len(values) > 13 {
			session.ProductID = values[13]
		}
		event.Session = session
	case EventContext:
		values, err := readRemaining(reader)
		if err != nil {
			return err
		}
		if len(values) < 10 {
			return fmt.Errorf("context payload has %d values, expected at least 10", len(values))
		}
		context := &ContextEvent{
			Network:          NetworkKind(values[0]),
			BatteryPct:       values[1],
			AvailMemoryKB:    values[2],
			BatteryState:     values[3],
			BatteryTempDeciC: decodeSVarint(values[4]),
			RxBytes:          values[5],
			TxBytes:          values[6],
			TotalMemoryKB:    values[7],
			FreeStorageKB:    values[8],
			TotalStorageKB:   values[9],
			LowMemory:        event.Flags&uint64(FlagContextLowMemory) != 0,
			NetworkMetered:   event.Flags&uint64(FlagNetworkMetered) != 0,
			NetworkValidated: event.Flags&uint64(FlagNetworkValidated) != 0,
			NetworkVPN:       event.Flags&uint64(FlagNetworkVPN) != 0,
		}
		event.Context = context
	case EventHTTP:
		values, err := readN(read, 9)
		if err != nil {
			return err
		}
		event.HTTP = &HTTPEvent{
			OwnerID: values[0], RouteID: values[1], DurationMS: values[2], DNSMS: values[3],
			ConnectMS: values[4], TTFBMS: values[5], Status: StatusClass(values[6]),
			RxBytes: values[7], TxBytes: values[8],
		}
	case EventUIWindow:
		values, err := readN(read, 7)
		if err != nil {
			return err
		}
		event.UIWindow = &UIWindowEvent{
			ScreenID: values[0], WindowMS: values[1], FrameCount: values[2], JankCount: values[3],
			P50MS: values[4], P95MS: values[5], P99MS: values[6],
		}
	case EventStall:
		values, err := readN(read, 3)
		if err != nil {
			return err
		}
		event.Stall = &StallEvent{OwnerID: values[0], StackID: values[1], DurationMS: values[2]}
	case EventMemory:
		values, err := readN(read, 3)
		if err != nil {
			return err
		}
		event.Memory = &MemoryEvent{PSSKB: values[0], JavaHeapKB: values[1], NativeHeapKB: values[2]}
	case EventRetained:
		screenID, ownerID, flowID, stepID, err := readContextIDs(read, event.Flags, decodeState)
		if err != nil {
			return err
		}
		values, err := readN(read, 4)
		if err != nil {
			return err
		}
		event.Retained = &RetainedEvent{
			ScreenID: screenID,
			OwnerID:  ownerID,
			FlowID:   flowID,
			StepID:   stepID,
			ClassID:  values[0],
			HolderID: values[1],
			AgeMS:    values[2],
			Count:    values[3],
		}
	case EventCounter, EventGauge:
		values, err := readRemaining(reader)
		if err != nil {
			return err
		}
		if len(values) < 2 {
			return fmt.Errorf("metric payload has %d values, expected at least 2", len(values))
		}
		event.Metric = metricFromValues(values, event.Type)
	case EventFlow:
		screenID, ownerID, flowID, stepID, err := readContextIDs(read, event.Flags, decodeState)
		if err != nil {
			return err
		}
		event.Flow = &FlowEvent{ScreenID: screenID, OwnerID: ownerID, FlowID: flowID, StepID: stepID}
	case EventLogSpam:
		screenID, ownerID, flowID, stepID, err := readContextIDs(read, event.Flags, decodeState)
		if err != nil {
			return err
		}
		values, err := readN(read, 3)
		if err != nil {
			return err
		}
		event.LogSpam = &LogSpamEvent{
			ScreenID: screenID,
			OwnerID:  ownerID,
			FlowID:   flowID,
			StepID:   stepID,
			SourceID: values[0],
			Level:    values[1],
			Count:    values[2],
		}
	case EventProblem:
		screenID, ownerID, flowID, stepID, err := readContextIDs(read, event.Flags, decodeState)
		if err != nil {
			return err
		}
		values, err := readN(read, 4)
		if err != nil {
			return err
		}
		event.Problem = &ProblemEvent{
			ScreenID: screenID,
			OwnerID:  ownerID,
			FlowID:   flowID,
			StepID:   stepID,
			KindID:   values[0],
			WindowMS: values[1],
			Count:    values[2],
			MaxMS:    values[3],
		}
	case EventRuntimeCall:
		screenID, callerID, flowID, stepID, err := readContextIDs(read, event.Flags, decodeState)
		if err != nil {
			return err
		}
		values, err := readN(read, 4)
		if err != nil {
			return err
		}
		event.RuntimeCall = &RuntimeCallEvent{
			ScreenID: screenID,
			CallerID: callerID,
			FlowID:   flowID,
			StepID:   stepID,
			CalleeID: values[0],
			Count:    values[1],
			TotalMS:  values[2],
			MaxMS:    values[3],
		}
	default:
		return fmt.Errorf("unsupported event type %d", event.Type)
	}
	return nil
}

func readContextIDs(read func() (uint64, error), flags uint64, decodeState *compactDecodeState) (screenID, ownerID, flowID, stepID uint64, err error) {
	if flags&uint64(FlagSameContext) != 0 {
		if decodeState == nil || !decodeState.hasContext {
			return 0, 0, 0, 0, fmt.Errorf("same-context flag without prior context")
		}
		context := decodeState.lastContext
		return context.screenID, context.ownerID, context.flowID, context.stepID, nil
	}
	if flags&uint64(FlagHasScreen) != 0 {
		if screenID, err = read(); err != nil {
			return 0, 0, 0, 0, err
		}
	}
	if flags&uint64(FlagHasOwner) != 0 {
		if ownerID, err = read(); err != nil {
			return 0, 0, 0, 0, err
		}
	}
	if flags&uint64(FlagHasFlow) != 0 {
		if flowID, err = read(); err != nil {
			return 0, 0, 0, 0, err
		}
	}
	if flags&uint64(FlagHasStep) != 0 {
		if stepID, err = read(); err != nil {
			return 0, 0, 0, 0, err
		}
	}
	if decodeState != nil {
		decodeState.lastContext = contextTuple{screenID: screenID, ownerID: ownerID, flowID: flowID, stepID: stepID}
		decodeState.hasContext = true
	}
	return screenID, ownerID, flowID, stepID, nil
}

func readN(read func() (uint64, error), count int) ([]uint64, error) {
	values := make([]uint64, count)
	for i := range values {
		value, err := read()
		if err != nil {
			return nil, err
		}
		values[i] = value
	}
	return values, nil
}

func metricFromValues(values []uint64, eventType EventType) *MetricEvent {
	event := &MetricEvent{
		MetricID: values[0],
		Value:    values[1],
		Count:    1,
		Sum:      values[1],
		Max:      values[1],
		Mode:     defaultMetricMode(eventType),
	}
	if len(values) > 2 {
		event.Count = values[2]
	}
	if len(values) > 3 {
		event.Sum = values[3]
	}
	if len(values) > 4 {
		event.Max = values[4]
	}
	if len(values) > 5 {
		event.Mode = MetricMode(values[5])
	}
	if event.Count == 0 {
		event.Count = 1
	}
	if event.Sum == 0 {
		event.Sum = event.Value
	}
	if event.Max == 0 {
		event.Max = event.Value
	}
	return event
}

func readRemaining(reader *bytes.Reader) ([]uint64, error) {
	var values []uint64
	for reader.Len() > 0 {
		value, err := binary.ReadUvarint(reader)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, nil
}

func decodeSVarint(value uint64) int64 {
	return int64(value>>1) ^ -int64(value&1)
}

func readDictionaryValue(reader io.ByteReader) (string, error) {
	length, err := binary.ReadUvarint(reader)
	if err != nil {
		return "", err
	}
	if length == 0 {
		codec, err := binary.ReadUvarint(reader)
		if err != nil {
			return "", err
		}
		return readEncodedDictionaryValue(reader, codec)
	}
	return readRawString(reader, length)
}

func readEncodedDictionaryValue(reader io.ByteReader, codec uint64) (string, error) {
	switch codec {
	case dictValueCodecUTF8:
		length, err := binary.ReadUvarint(reader)
		if err != nil {
			return "", err
		}
		return readRawString(reader, length)
	case dictValueCodecBCDDecimal:
		digits, err := binary.ReadUvarint(reader)
		if err != nil {
			return "", err
		}
		return readBCDDigits(reader, int(digits))
	case dictValueCodecBCDISODate:
		value, err := readBCDDigits(reader, 8)
		if err != nil {
			return "", err
		}
		if len(value) != 8 {
			return "", fmt.Errorf("invalid bcd date length %d", len(value))
		}
		return value[:4] + "-" + value[4:6] + "-" + value[6:8], nil
	default:
		return "", fmt.Errorf("unsupported dictionary value codec %d", codec)
	}
}

func readBCDDigits(reader io.ByteReader, digits int) (string, error) {
	if digits < 0 || digits > 1024*1024 {
		return "", fmt.Errorf("invalid bcd digit count %d", digits)
	}
	buf := make([]byte, 0, digits)
	for len(buf) < digits {
		raw, err := reader.ReadByte()
		if err != nil {
			return "", err
		}
		hi := raw >> 4
		lo := raw & 0x0f
		if hi > 9 {
			return "", fmt.Errorf("invalid bcd high nibble %x", hi)
		}
		buf = append(buf, '0'+hi)
		if len(buf) == digits {
			if lo != 0x0f {
				return "", fmt.Errorf("invalid bcd padding nibble %x", lo)
			}
			break
		}
		if lo > 9 {
			return "", fmt.Errorf("invalid bcd low nibble %x", lo)
		}
		buf = append(buf, '0'+lo)
	}
	return string(buf), nil
}

func readRawString(reader io.ByteReader, length uint64) (string, error) {
	if length > 1024*1024 {
		return "", fmt.Errorf("string too large: %d", length)
	}
	buf := make([]byte, length)
	fullReader, ok := reader.(io.Reader)
	if !ok {
		return "", fmt.Errorf("reader cannot read bytes")
	}
	_, err := io.ReadFull(fullReader, buf)
	return string(buf), err
}

const (
	dictValueCodecUTF8       uint64 = 0
	dictValueCodecBCDDecimal uint64 = 1
	dictValueCodecBCDISODate uint64 = 2
)

func streamJSONL(r io.Reader, source string, handle func(Event, map[uint64]string) error) error {
	log := Log{
		Source:  source,
		Version: FormatVersion,
		Dict:    map[uint64]string{},
		Kinds:   map[uint64]DictKind{},
	}
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
			return fmt.Errorf("%s:%d: decode jsonl: %w", source, line, err)
		}
		event.Source = source
		if event.Dictionary != nil {
			log.Dict[event.Dictionary.ID] = event.Dictionary.Value
			log.Kinds[event.Dictionary.ID] = event.Dictionary.Kind
		}
		if err := handle(event, log.Dict); err != nil {
			return err
		}
	}
	return scanner.Err()
}
