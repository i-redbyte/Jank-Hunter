package jhlog

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

func ReadFile(path string) (Log, error) {
	file, err := os.Open(path)
	if err != nil {
		return Log{}, err
	}
	defer file.Close()

	var prefix [8]byte
	n, err := io.ReadFull(file, prefix[:])
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return Log{}, err
	}
	if n == len(Magic) && bytes.Equal(prefix[:7], Magic[:7]) {
		version := prefix[7]
		if version > FormatVersion {
			return Log{}, fmt.Errorf("%s: unsupported jhlog version %d, cli supports up to %d", path, version, FormatVersion)
		}
		return readBinary(file, path, version)
	}

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return Log{}, err
	}
	return readJSONL(file, path)
}

func StreamFile(path string, handle func(Event, map[uint64]string) error) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	var prefix [8]byte
	n, err := io.ReadFull(file, prefix[:])
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return err
	}
	if n == len(Magic) && bytes.Equal(prefix[:7], Magic[:7]) {
		version := prefix[7]
		if version > FormatVersion {
			return fmt.Errorf("%s: unsupported jhlog version %d, cli supports up to %d", path, version, FormatVersion)
		}
		_, err := streamBinary(file, path, version, handle)
		return err
	}

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	return streamJSONL(file, path, handle)
}

func readBinary(r io.Reader, source string, version uint8) (Log, error) {
	if version >= 2 {
		return readBinaryV2(r, source, version)
	}
	return readBinaryV1(r, source, version)
}

func readBinaryV1(r io.Reader, source string, version uint8) (Log, error) {
	reader := bufio.NewReader(r)
	log := Log{
		Source:  source,
		Version: version,
		Dict:    map[uint64]string{},
		Kinds:   map[uint64]DictKind{},
	}
	var currentMS uint64
	for {
		eventType, err := binary.ReadUvarint(reader)
		if errors.Is(err, io.EOF) {
			return log, nil
		}
		if err != nil {
			return toleratePartial(log, source, "event type", err)
		}

		deltaMS, err := binary.ReadUvarint(reader)
		if err != nil {
			return toleratePartial(log, source, "timestamp delta", err)
		}
		flags, err := binary.ReadUvarint(reader)
		if err != nil {
			return toleratePartial(log, source, "flags", err)
		}
		payloadLen, err := binary.ReadUvarint(reader)
		if err != nil {
			return toleratePartial(log, source, "payload length", err)
		}
		if payloadLen > 4*1024*1024 {
			return log, fmt.Errorf("%s: suspicious payload length %d", source, payloadLen)
		}

		payload := make([]byte, payloadLen)
		if _, err := io.ReadFull(reader, payload); err != nil {
			return toleratePartial(log, source, "payload", err)
		}

		currentMS += deltaMS
		event := Event{
			Type:    EventType(eventType),
			TimeMS:  currentMS,
			DeltaMS: deltaMS,
			Flags:   flags,
			Source:  source,
		}
		if err := decodePayload(payload, &event); err != nil {
			warning := err.Error()
			event.Warnings = append(event.Warnings, warning)
			log.Warnings = append(log.Warnings, fmt.Sprintf("event at %dms: %s", event.TimeMS, warning))
		}
		if event.Dictionary != nil {
			log.Dict[event.Dictionary.ID] = event.Dictionary.Value
			log.Kinds[event.Dictionary.ID] = event.Dictionary.Kind
		}
		log.Events = append(log.Events, event)
	}
}

func streamBinary(
	r io.Reader,
	source string,
	version uint8,
	handle func(Event, map[uint64]string) error,
) (Log, error) {
	if version >= 2 {
		return streamBinaryV2(r, source, version, handle)
	}
	return streamBinaryV1(r, source, version, handle)
}

func streamBinaryV1(
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
	for {
		eventType, err := binary.ReadUvarint(reader)
		if errors.Is(err, io.EOF) {
			return log, nil
		}
		if err != nil {
			return toleratePartial(log, source, "event type", err)
		}

		deltaMS, err := binary.ReadUvarint(reader)
		if err != nil {
			return toleratePartial(log, source, "timestamp delta", err)
		}
		flags, err := binary.ReadUvarint(reader)
		if err != nil {
			return toleratePartial(log, source, "flags", err)
		}
		payloadLen, err := binary.ReadUvarint(reader)
		if err != nil {
			return toleratePartial(log, source, "payload length", err)
		}
		if payloadLen > 4*1024*1024 {
			return log, fmt.Errorf("%s: suspicious payload length %d", source, payloadLen)
		}

		payload := make([]byte, payloadLen)
		if _, err := io.ReadFull(reader, payload); err != nil {
			return toleratePartial(log, source, "payload", err)
		}

		currentMS += deltaMS
		event := Event{
			Type:    EventType(eventType),
			TimeMS:  currentMS,
			DeltaMS: deltaMS,
			Flags:   flags,
			Source:  source,
		}
		if err := decodePayload(payload, &event); err != nil {
			warning := err.Error()
			event.Warnings = append(event.Warnings, warning)
			log.Warnings = append(log.Warnings, fmt.Sprintf("event at %dms: %s", event.TimeMS, warning))
		}
		if event.Dictionary != nil {
			log.Dict[event.Dictionary.ID] = event.Dictionary.Value
			log.Kinds[event.Dictionary.ID] = event.Dictionary.Kind
		}
		if err := handle(event, log.Dict); err != nil {
			return log, err
		}
	}
}

func readBinaryV2(r io.Reader, source string, version uint8) (Log, error) {
	reader := bufio.NewReader(r)
	log := Log{
		Source:  source,
		Version: version,
		Dict:    map[uint64]string{},
		Kinds:   map[uint64]DictKind{},
	}
	var currentMS uint64
	for {
		event, deltaMS, err := readCompactEvent(reader, source)
		if errors.Is(err, io.EOF) {
			return log, nil
		}
		if err != nil {
			return toleratePartial(log, source, "compact event", err)
		}
		currentMS += deltaMS
		event.TimeMS = currentMS
		event.DeltaMS = deltaMS
		event.Source = source
		if event.Dictionary != nil {
			log.Dict[event.Dictionary.ID] = event.Dictionary.Value
			log.Kinds[event.Dictionary.ID] = event.Dictionary.Kind
		}
		log.Events = append(log.Events, event)
	}
}

func streamBinaryV2(
	r io.Reader,
	source string,
	version uint8,
	handle func(Event, map[uint64]string) error,
) (Log, error) {
	reader := bufio.NewReader(r)
	log := Log{
		Source:  source,
		Version: version,
		Dict:    map[uint64]string{},
		Kinds:   map[uint64]DictKind{},
	}
	var currentMS uint64
	for {
		event, deltaMS, err := readCompactEvent(reader, source)
		if errors.Is(err, io.EOF) {
			return log, nil
		}
		if err != nil {
			return toleratePartial(log, source, "compact event", err)
		}
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
	}
}

func readCompactEvent(reader *bufio.Reader, source string) (Event, uint64, error) {
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
		if err := decodePayload(payload, &event); err != nil {
			warning := err.Error()
			event.Warnings = append(event.Warnings, warning)
		}
		return event, deltaMS, nil
	}
	if err := decodeFixedCompactPayload(reader, &event); err != nil {
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

func decodeFixedCompactPayload(reader io.ByteReader, event *Event) error {
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
		values, err := readN(read, 3)
		if err != nil {
			return err
		}
		event.Retained = &RetainedEvent{ClassID: values[0], AgeMS: values[1], Count: values[2]}
	case EventCounter, EventGauge:
		values, err := readN(read, 2)
		if err != nil {
			return err
		}
		event.Metric = &MetricEvent{MetricID: values[0], Value: values[1]}
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

func decodePayload(payload []byte, event *Event) error {
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
		value, err := readString(reader)
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
		if len(values) < 3 {
			return fmt.Errorf("context payload has %d values, expected at least 3", len(values))
		}
		context := &ContextEvent{
			Network:       NetworkKind(values[0]),
			BatteryPct:    values[1],
			AvailMemoryKB: values[2],
		}
		if len(values) > 3 {
			context.BatteryState = values[3]
		}
		if len(values) > 4 {
			context.BatteryTempDeciC = values[4]
		}
		if len(values) > 5 {
			context.LowMemory = values[5] != 0
		}
		if len(values) > 6 {
			context.NetworkMetered = values[6] != 0
		}
		if len(values) > 7 {
			context.NetworkValidated = values[7] != 0
		}
		if len(values) > 8 {
			context.RxBytes = values[8]
		}
		if len(values) > 9 {
			context.TxBytes = values[9]
		}
		if len(values) > 10 {
			context.TotalMemoryKB = values[10]
		}
		if len(values) > 11 {
			context.FreeStorageKB = values[11]
		}
		if len(values) > 12 {
			context.TotalStorageKB = values[12]
		}
		if len(values) > 13 {
			context.NetworkVPN = values[13] != 0
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
		values, err := readN(read, 3)
		if err != nil {
			return err
		}
		event.Retained = &RetainedEvent{ClassID: values[0], AgeMS: values[1], Count: values[2]}
	case EventCounter, EventGauge:
		values, err := readN(read, 2)
		if err != nil {
			return err
		}
		event.Metric = &MetricEvent{MetricID: values[0], Value: values[1]}
	default:
		return fmt.Errorf("unsupported event type %d", event.Type)
	}
	return nil
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

func readString(reader io.ByteReader) (string, error) {
	length, err := binary.ReadUvarint(reader)
	if err != nil {
		return "", err
	}
	if length > 1024*1024 {
		return "", fmt.Errorf("string too large: %d", length)
	}
	buf := make([]byte, length)
	fullReader, ok := reader.(io.Reader)
	if !ok {
		return "", fmt.Errorf("reader cannot read bytes")
	}
	_, err = io.ReadFull(fullReader, buf)
	return string(buf), err
}

func readJSONL(r io.Reader, source string) (Log, error) {
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
			return log, fmt.Errorf("%s:%d: decode jsonl: %w", source, line, err)
		}
		event.Source = source
		if event.Dictionary != nil {
			log.Dict[event.Dictionary.ID] = event.Dictionary.Value
			log.Kinds[event.Dictionary.ID] = event.Dictionary.Kind
		}
		log.Events = append(log.Events, event)
	}
	return log, scanner.Err()
}

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

func ExportJSONL(log Log, w io.Writer) error {
	encoder := json.NewEncoder(w)
	for _, event := range log.Events {
		if err := encoder.Encode(event); err != nil {
			return err
		}
	}
	return nil
}
