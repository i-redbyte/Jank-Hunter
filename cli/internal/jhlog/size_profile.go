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
	"math"
	"os"
	"sort"
)

type SizeProfile struct {
	Files    []SizeProfileFile `json:"files"`
	Types    []SizeProfileType `json:"types"`
	Warnings []string          `json:"warnings,omitempty"`
}

type SizeProfileFile struct {
	Path            string   `json:"path"`
	Format          string   `json:"format"`
	FileBytes       uint64   `json:"file_bytes"`
	BodyBytes       uint64   `json:"body_bytes"`
	Events          uint64   `json:"events"`
	Warnings        []string `json:"warnings,omitempty"`
	CompressionRate float64  `json:"compression_rate,omitempty"`
}

type SizeProfileType struct {
	Type     EventType `json:"type"`
	Name     string    `json:"name"`
	Events   uint64    `json:"events"`
	Bytes    uint64    `json:"bytes"`
	AvgBytes float64   `json:"avg_bytes"`
	Percent  float64   `json:"percent"`
	Files    uint64    `json:"files"`
}

func ProfileFiles(paths []string) (SizeProfile, error) {
	profile := SizeProfile{}
	byType := map[EventType]*SizeProfileType{}
	seenInFile := map[EventType]struct{}{}
	var totalBody uint64

	for _, path := range paths {
		fileProfile, typeBytes, typeEvents, err := ProfileFile(path)
		if err != nil {
			return SizeProfile{}, err
		}
		profile.Files = append(profile.Files, fileProfile)
		profile.Warnings = append(profile.Warnings, fileProfile.Warnings...)
		totalBody += fileProfile.BodyBytes
		clear(seenInFile)
		for eventType, bytes := range typeBytes {
			row := byType[eventType]
			if row == nil {
				row = &SizeProfileType{Type: eventType, Name: EventTypeName(eventType)}
				byType[eventType] = row
			}
			row.Bytes += bytes
			row.Events += typeEvents[eventType]
			if _, ok := seenInFile[eventType]; !ok {
				row.Files++
				seenInFile[eventType] = struct{}{}
			}
		}
	}

	profile.Types = make([]SizeProfileType, 0, len(byType))
	for _, row := range byType {
		if row.Events > 0 {
			row.AvgBytes = roundFloat(float64(row.Bytes) / float64(row.Events))
		}
		if totalBody > 0 {
			row.Percent = roundFloat((float64(row.Bytes) / float64(totalBody)) * 100)
		}
		profile.Types = append(profile.Types, *row)
	}
	sort.Slice(profile.Types, func(i, j int) bool {
		if profile.Types[i].Bytes == profile.Types[j].Bytes {
			return profile.Types[i].Name < profile.Types[j].Name
		}
		return profile.Types[i].Bytes > profile.Types[j].Bytes
	})
	return profile, nil
}

func ProfileFile(path string) (SizeProfileFile, map[EventType]uint64, map[EventType]uint64, error) {
	file, err := os.Open(path)
	if err != nil {
		return SizeProfileFile{}, nil, nil, err
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return SizeProfileFile{}, nil, nil, err
	}
	fileBytes := uint64(stat.Size())
	var prefix [8]byte
	n, err := io.ReadFull(file, prefix[:])
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return SizeProfileFile{}, nil, nil, err
	}
	if n == len(Magic) && bytes.Equal(prefix[:7], Magic[:7]) {
		return profileBinaryFile(path, file, fileBytes, prefix[7])
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return SizeProfileFile{}, nil, nil, err
	}
	return profileJSONLFile(path, file, fileBytes)
}

func profileBinaryFile(path string, compressedBody io.Reader, fileBytes uint64, version byte) (SizeProfileFile, map[EventType]uint64, map[EventType]uint64, error) {
	if version != FormatVersion {
		return SizeProfileFile{}, nil, nil, fmt.Errorf("%s: unsupported jhlog version %d, cli supports current pre-release version %d", path, version, FormatVersion)
	}
	gzipReader, err := gzip.NewReader(compressedBody)
	if err != nil {
		return SizeProfileFile{}, nil, nil, fmt.Errorf("%s: compressed jhlog body: %w", path, err)
	}
	defer gzipReader.Close()

	fileProfile := SizeProfileFile{
		Path:      path,
		Format:    "binary-gzip",
		FileBytes: fileBytes,
	}
	typeBytes := map[EventType]uint64{}
	typeEvents := map[EventType]uint64{}
	reader := &countingCompactReader{reader: bufio.NewReader(gzipReader)}
	for {
		before := reader.BytesRead()
		eventType, err := readCompactEventTypeAndSkip(reader)
		consumed := reader.BytesRead() - before
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				if consumed > 0 {
					fileProfile.Warnings = append(fileProfile.Warnings, fmt.Sprintf("%s: ignored partial trailing compact event", path))
				}
				break
			}
			return SizeProfileFile{}, nil, nil, fmt.Errorf("%s: profile compact event: %w", path, err)
		}
		typeBytes[eventType] += consumed
		typeEvents[eventType]++
		fileProfile.Events++
	}
	fileProfile.BodyBytes = reader.BytesRead()
	fileProfile.CompressionRate = compressionRate(fileProfile.BodyBytes+uint64(len(Magic)), fileProfile.FileBytes)
	return fileProfile, typeBytes, typeEvents, nil
}

type countingCompactReader struct {
	reader    *bufio.Reader
	bytesRead uint64
}

func (r *countingCompactReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.bytesRead += uint64(n)
	return n, err
}

func (r *countingCompactReader) ReadByte() (byte, error) {
	b, err := r.reader.ReadByte()
	if err == nil {
		r.bytesRead++
	}
	return b, err
}

func (r *countingCompactReader) BytesRead() uint64 {
	return r.bytesRead
}

func readCompactEventTypeAndSkip(reader compactEventReader) (EventType, error) {
	header, err := reader.ReadByte()
	if err != nil {
		return 0, err
	}
	eventType := EventType(header) & compactEventTypeMask
	deltaCode := (header >> compactHeaderDeltaShift) & 0x03
	if _, err := readCompactDelta(reader, deltaCode); err != nil {
		return 0, fmt.Errorf("timestamp delta: %w", err)
	}
	flags := uint64(0)
	if header&compactHeaderHasFlags != 0 {
		flags, err = binary.ReadUvarint(reader)
		if err != nil {
			return 0, fmt.Errorf("flags: %w", err)
		}
	}
	if header&compactHeaderHasPayloadLen != 0 {
		payloadLen, err := binary.ReadUvarint(reader)
		if err != nil {
			return 0, fmt.Errorf("payload length: %w", err)
		}
		if payloadLen > 4*1024*1024 {
			return 0, fmt.Errorf("suspicious payload length %d", payloadLen)
		}
		if _, err := io.CopyN(io.Discard, reader, int64(payloadLen)); err != nil {
			return 0, fmt.Errorf("payload: %w", err)
		}
		return eventType, nil
	}
	if err := skipFixedCompactPayload(reader, eventType, flags); err != nil {
		return 0, fmt.Errorf("payload: %w", err)
	}
	return eventType, nil
}

func skipFixedCompactPayload(reader compactEventReader, eventType EventType, flags uint64) error {
	switch eventType {
	case EventHTTP:
		return skipUvarints(reader, 9)
	case EventUIWindow:
		return skipUvarints(reader, 7)
	case EventStall:
		return skipUvarints(reader, 3)
	case EventMemory:
		return skipUvarints(reader, 3)
	case EventRetained:
		if err := skipContextIDs(reader, flags); err != nil {
			return err
		}
		return skipUvarints(reader, 4)
	case EventCounter, EventGauge:
		return skipUvarints(reader, 2)
	default:
		return fmt.Errorf("compact event type %d requires payload length", eventType)
	}
}

func skipContextIDs(reader compactEventReader, flags uint64) error {
	if flags&uint64(FlagSameContext) != 0 {
		return nil
	}
	count := 0
	if flags&uint64(FlagHasScreen) != 0 {
		count++
	}
	if flags&uint64(FlagHasOwner) != 0 {
		count++
	}
	if flags&uint64(FlagHasFlow) != 0 {
		count++
	}
	if flags&uint64(FlagHasStep) != 0 {
		count++
	}
	return skipUvarints(reader, count)
}

func skipUvarints(reader compactEventReader, count int) error {
	for range count {
		if _, err := binary.ReadUvarint(reader); err != nil {
			return err
		}
	}
	return nil
}

func profileJSONLFile(path string, body io.Reader, fileBytes uint64) (SizeProfileFile, map[EventType]uint64, map[EventType]uint64, error) {
	fileProfile := SizeProfileFile{
		Path:      path,
		Format:    "jsonl",
		FileBytes: fileBytes,
		BodyBytes: fileBytes,
	}
	typeBytes := map[EventType]uint64{}
	typeEvents := map[EventType]uint64{}
	reader := bufio.NewReader(body)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			recorded, err := profileJSONLLine(path, line, typeBytes, typeEvents)
			if err != nil {
				return SizeProfileFile{}, nil, nil, err
			}
			if recorded {
				fileProfile.Events++
			}
		}
		if err == nil {
			continue
		}
		if errors.Is(err, io.EOF) {
			break
		}
		return SizeProfileFile{}, nil, nil, err
	}
	return fileProfile, typeBytes, typeEvents, nil
}

func profileJSONLLine(path string, line []byte, typeBytes map[EventType]uint64, typeEvents map[EventType]uint64) (bool, error) {
	trimmed := bytes.TrimSpace(line)
	if len(trimmed) == 0 {
		return false, nil
	}
	var event Event
	if err := json.Unmarshal(trimmed, &event); err != nil {
		return false, fmt.Errorf("%s: decode jsonl: %w", path, err)
	}
	typeBytes[event.Type] += uint64(len(line))
	typeEvents[event.Type]++
	return true, nil
}

func EventTypeName(eventType EventType) string {
	switch eventType {
	case EventDictionary:
		return "dictionary"
	case EventSession:
		return "session"
	case EventContext:
		return "context"
	case EventHTTP:
		return "http"
	case EventUIWindow:
		return "ui_window"
	case EventStall:
		return "stall"
	case EventMemory:
		return "memory"
	case EventRetained:
		return "retained"
	case EventCounter:
		return "counter"
	case EventGauge:
		return "gauge"
	case EventFlow:
		return "flow"
	case EventLogSpam:
		return "log_spam"
	case EventProblem:
		return "problem"
	case EventRuntimeCall:
		return "runtime_call"
	default:
		return fmt.Sprintf("event_%d", eventType)
	}
}

func compressionRate(bodyBytes uint64, fileBytes uint64) float64 {
	if fileBytes == 0 {
		return 0
	}
	return roundFloat(float64(bodyBytes) / float64(fileBytes))
}

func roundFloat(value float64) float64 {
	return math.Round(value*10) / 10
}
