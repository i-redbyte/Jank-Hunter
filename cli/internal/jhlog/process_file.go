package jhlog

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
)

// ProcessMagic identifies an append-only sequence of complete JHLOG segments.
// Each segment retains its own dictionaries, configuration, quality and digest.
var ProcessMagic = []byte{'J', 'H', 'L', 'O', 'G', '\r', '\n', 0x82, 1, 0, 0}

// FileSegment is a bounded view into an original file, never an extracted copy.
type FileSegment struct {
	Header             SegmentHeader
	Offset             int64
	Length             int64
	ProcessFile        bool
	ContainerTailBytes int64
}

func ReadFileSegments(path string) ([]FileSegment, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	return readFileSegments(file, path, info.Size())
}

func readFileSegments(file *os.File, path string, size int64) ([]FileSegment, error) {
	var prefix [magicSize]byte
	if _, err := file.ReadAt(prefix[:], 0); err != nil {
		return nil, fmt.Errorf("%s: process file header: %w", path, err)
	}
	wrapped := bytes.Equal(prefix[:], ProcessMagic)
	var offset int64
	if wrapped {
		offset = int64(len(ProcessMagic))
	}
	var segments []FileSegment
	for offset < size {
		reader := io.NewSectionReader(file, offset, size-offset)
		if n, err := io.ReadFull(reader, prefix[:]); err != nil {
			if wrapped && len(segments) > 0 && incompleteRead(err) &&
				(bytes.HasPrefix(Magic, prefix[:n]) || bytes.HasPrefix(legacyMagicV500, prefix[:n])) {
				segments[len(segments)-1].ContainerTailBytes = size - offset
				return segments, nil
			}
			return nil, fmt.Errorf("%s: epoch magic at %d: %w", path, offset, err)
		}
		if !supportedFileMagic(prefix[:]) {
			return nil, fmt.Errorf("%s: unsupported epoch magic at %d", path, offset)
		}
		header, err := readHeader(reader)
		if err != nil {
			if wrapped && len(segments) > 0 && incompleteRead(err) {
				segments[len(segments)-1].ContainerTailBytes = size - offset
				return segments, nil
			}
			return nil, fmt.Errorf("%s: epoch header at %d: %w", path, offset, err)
		}
		if err := validateHeader(header); err != nil {
			return nil, err
		}
		if len(segments) == 0 {
			if err := validateSessionLogFilename(path, header); err != nil {
				return nil, err
			}
		}
		if !wrapped {
			return []FileSegment{{Header: header, Length: size}}, nil
		}
		if header.ProcessInstanceID.IsZero() {
			return nil, fmt.Errorf("%s: process file has no process identity", path)
		}
		if len(segments) > 0 {
			first := segments[0].Header
			if first.ProcessInstanceID != header.ProcessInstanceID || first.ProcessName != header.ProcessName || first.RunID != header.RunID {
				return nil, fmt.Errorf("%s: process file mixes different process or run identities", path)
			}
		}
		length, err := scanSegmentBoundary(reader, size-offset)
		if err != nil {
			return nil, fmt.Errorf("%s: epoch at %d: %w", path, offset, err)
		}
		segments = append(segments, FileSegment{Header: header, Offset: offset, Length: length, ProcessFile: true})
		offset += length
	}
	if len(segments) == 0 {
		return nil, fmt.Errorf("%s: process file has no epochs", path)
	}
	return segments, nil
}

func streamProcessFile(file *os.File, result StreamResult, handle EventHandler) (StreamResult, error) {
	segments, err := readFileSegments(file, result.Source, int64(result.InputBytes))
	if err != nil {
		return corruptResult(result, err)
	}
	result.Header = segments[0].Header
	result.ProcessFile = true
	result.FormatVersion = "process-1.0.0"
	result.Status, result.Sealed = SegmentStatusClosedClean, true
	for _, segment := range segments {
		part, err := streamFileSegment(file, result.Source, segment, handle)
		if err != nil {
			return corruptResult(result, err)
		}
		result.Segments = append(result.Segments, part)
		if !part.Sealed {
			result.Status, result.Sealed = part.Status, false
		}
		result.TailBytes += part.TailBytes
		result.CommittedChunks += part.CommittedChunks
		result.TotalRecords += part.TotalRecords
		result.DataRecords += part.DataRecords
		result.DictionaryRecords += part.DictionaryRecords
		result.ControlRecords += part.ControlRecords
		result.Events += part.Events
		result.RawRecordBytes += part.RawRecordBytes
		result.StoredChunkBytes += part.StoredChunkBytes
		result.RuntimeGraphLogicalCalls += part.RuntimeGraphLogicalCalls
		result.LatestDataEventUnixMS = max(result.LatestDataEventUnixMS, part.LatestDataEventUnixMS)
		for kind, value := range part.RecordBytesByType {
			result.RecordBytesByType[kind] += value
		}
		for kind, value := range part.RecordsByType {
			result.RecordsByType[kind] += value
		}
	}
	return result, nil
}

// Indexing touches only framing bytes, skipping compressed payloads without decoding them.
func scanSegmentBoundary(reader *io.SectionReader, size int64) (int64, error) {
	var raw [chunkHeaderSize]byte
	var trailer [commitTrailerSize]byte
	for sequence := uint32(0); ; sequence++ {
		position, err := reader.Seek(0, io.SeekCurrent)
		if err != nil {
			return 0, err
		}
		if size-position < chunkHeaderSize {
			return size, nil
		}
		if _, err := io.ReadFull(reader, raw[:]); err != nil {
			return 0, err
		}
		metadata, err := parseChunkHeader(raw[:], sequence)
		if err != nil {
			return 0, err
		}
		remaining := size - position - chunkHeaderSize
		if remaining < int64(metadata.StoredLen)+commitTrailerSize {
			return size, nil
		}
		if _, err := reader.Seek(int64(metadata.StoredLen), io.SeekCurrent); err != nil {
			return 0, err
		}
		if _, err := io.ReadFull(reader, trailer[:]); err != nil {
			return 0, err
		}
		if err := validateCommitTrailer(trailer[:], metadata); err != nil {
			return 0, err
		}
		if metadata.Flags&chunkFlagFinal != 0 {
			return reader.Seek(0, io.SeekCurrent)
		}
	}
}

func StreamFileSegment(path string, segment FileSegment, handle EventHandler) (StreamResult, error) {
	file, err := os.Open(path)
	if err != nil {
		return StreamResult{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return StreamResult{}, err
	}
	if segment.Offset < 0 || segment.Length < 0 || segment.Offset > info.Size() || segment.Length > info.Size()-segment.Offset {
		return StreamResult{}, fmt.Errorf("%s: segment range is outside the file", path)
	}
	return streamFileSegment(file, path, segment, handle)
}

func streamFileSegment(file *os.File, path string, segment FileSegment, handle EventHandler) (StreamResult, error) {
	if handle == nil {
		handle = func(Event, map[uint64]string) error { return nil }
	}
	reader := io.NewSectionReader(file, segment.Offset, segment.Length)
	var magic [magicSize]byte
	if _, err := io.ReadFull(reader, magic[:]); err != nil {
		return StreamResult{}, err
	}
	if !supportedFileMagic(magic[:]) {
		return StreamResult{}, fmt.Errorf("%s: segment magic changed", path)
	}
	result := newStreamResult(path)
	result.ProcessFile = segment.ProcessFile
	result.InputBytes = uint64(segment.Length)
	result.FormatVersion = fmt.Sprintf("%d.%d.%d", magic[8], magic[9], magic[10])
	digest := sha256.New()
	digest.Write(magic[:])
	result, err := streamBinary(reader, result, handle, digest, bytes.Equal(magic[:], legacyMagicV500), !segment.ProcessFile)
	if err != nil {
		return result, err
	}
	header := result.Header
	if header.RunID != segment.Header.RunID || header.ProcessInstanceID != segment.Header.ProcessInstanceID ||
		header.SessionID != segment.Header.SessionID || header.SegmentIndex != segment.Header.SegmentIndex {
		return corruptResult(result, fmt.Errorf("segment identity changed after indexing"))
	}
	if segment.ContainerTailBytes > 0 {
		result.InputBytes += uint64(segment.ContainerTailBytes)
		result.TailBytes += uint64(segment.ContainerTailBytes)
		result.Sealed, result.Status = false, SegmentStatusOpenWithTail
	}
	return result, nil
}

func incompleteRead(err error) bool {
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)
}
