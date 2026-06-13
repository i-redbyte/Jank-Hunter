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
	if n == len(Magic) && bytes.Equal(prefix[:], Magic) {
		return readBinary(file, path)
	}

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return Log{}, err
	}
	return readJSONL(file, path)
}

func readBinary(r io.Reader, source string) (Log, error) {
	reader := bufio.NewReader(r)
	log := Log{
		Source: source,
		Dict:   map[uint64]string{},
		Kinds:  map[uint64]DictKind{},
	}
	var currentMS uint64
	for {
		eventType, err := binary.ReadUvarint(reader)
		if errors.Is(err, io.EOF) {
			return log, nil
		}
		if err != nil {
			return log, fmt.Errorf("%s: read event type: %w", source, err)
		}

		deltaMS, err := binary.ReadUvarint(reader)
		if err != nil {
			return log, fmt.Errorf("%s: read timestamp delta: %w", source, err)
		}
		flags, err := binary.ReadUvarint(reader)
		if err != nil {
			return log, fmt.Errorf("%s: read flags: %w", source, err)
		}
		payloadLen, err := binary.ReadUvarint(reader)
		if err != nil {
			return log, fmt.Errorf("%s: read payload length: %w", source, err)
		}
		if payloadLen > 4*1024*1024 {
			return log, fmt.Errorf("%s: suspicious payload length %d", source, payloadLen)
		}

		payload := make([]byte, payloadLen)
		if _, err := io.ReadFull(reader, payload); err != nil {
			return log, fmt.Errorf("%s: read payload: %w", source, err)
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
			event.Warnings = append(event.Warnings, err.Error())
		}
		if event.Dictionary != nil {
			log.Dict[event.Dictionary.ID] = event.Dictionary.Value
			log.Kinds[event.Dictionary.ID] = event.Dictionary.Kind
		}
		log.Events = append(log.Events, event)
	}
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
		app, err := read()
		if err != nil {
			return err
		}
		build, err := read()
		if err != nil {
			return err
		}
		device, err := read()
		if err != nil {
			return err
		}
		sdk, err := read()
		if err != nil {
			return err
		}
		event.Session = &SessionEvent{AppVersionID: app, BuildID: build, DeviceID: device, SDKInt: sdk}
	case EventContext:
		network, err := read()
		if err != nil {
			return err
		}
		battery, err := read()
		if err != nil {
			return err
		}
		mem, err := read()
		if err != nil {
			return err
		}
		event.Context = &ContextEvent{Network: NetworkKind(network), BatteryPct: battery, AvailMemoryKB: mem}
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
		Source: source,
		Dict:   map[uint64]string{},
		Kinds:  map[uint64]DictKind{},
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

func ExportJSONL(log Log, w io.Writer) error {
	encoder := json.NewEncoder(w)
	for _, event := range log.Events {
		if err := encoder.Encode(event); err != nil {
			return err
		}
	}
	return nil
}
