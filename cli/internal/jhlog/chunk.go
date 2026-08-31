package jhlog

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"unicode/utf8"
)

const (
	maxHeaderPayloadSize  = 4 * 1024
	defaultRawChunkTarget = 64 * 1024
	maxRawChunkSize       = 256 * 1024
	maxStoredChunkSize    = 512 * 1024
	processScopeHashSize  = 32
	segmentDigestSize     = 32
	chunkHeaderSize       = 32
	commitTrailerSize     = 20

	chunkFlagGZIP   uint16 = 1 << 0
	chunkFlagFinal  uint16 = 1 << 1
	chunkKnownFlags        = chunkFlagGZIP | chunkFlagFinal
)

var (
	chunkMagic  = [4]byte{'J', 'H', 'C', '1'}
	commitMagic = [4]byte{'J', 'H', 'C', 'M'}
)

type chunkMetadata struct {
	Flags       uint16
	Sequence    uint32
	StoredLen   uint32
	RawLen      uint32
	RecordCount uint32
	RawCRC      uint32
}

func normalizedHeader(header SegmentHeader) SegmentHeader {
	if header.ProcessScope == ProcessScopeUnknown {
		header.ProcessScope = ProcessScopeAll
	}
	header.SymbolNamespace = append([]byte(nil), header.SymbolNamespace...)
	header.ProcessScopeFingerprint = append([]byte(nil), header.ProcessScopeFingerprint...)
	header.PreviousSegmentDigest = append([]byte(nil), header.PreviousSegmentDigest...)
	header.ExpectedProcessFingerprint = append([]byte(nil), header.ExpectedProcessFingerprint...)
	if header.ExpectedProcessCount == 0 {
		processName := header.ProcessName
		if processName == "" {
			processName = "unknown"
		}
		header.ExpectedProcessCount = 1
		header.ExpectedProcessFingerprint = ProcessRosterFingerprint([]string{processName})
		header.ProcessRosterDeclarationComplete = true
	}
	return header
}

func encodeFileHeader(header SegmentHeader) ([]byte, SegmentHeader, error) {
	header = normalizedHeader(header)
	if header.Schema != HeaderSchemaV2 {
		return nil, SegmentHeader{}, fmt.Errorf("unsupported header schema %d", header.Schema)
	}
	if err := validateFeatureContract(header.RequiredFeatures, header.OptionalFeatures); err != nil {
		return nil, SegmentHeader{}, err
	}
	if !utf8.ValidString(header.ProcessName) {
		return nil, SegmentHeader{}, fmt.Errorf("process name is not valid UTF-8")
	}
	if err := validateProcessScope(header.ProcessScope, header.AllowedProcessCount, header.ProcessScopeFingerprint); err != nil {
		return nil, SegmentHeader{}, err
	}

	var payload bytes.Buffer
	for _, value := range []uint64{
		header.Schema,
		header.RequiredFeatures,
		header.OptionalFeatures,
	} {
		if err := writeUvarint(&payload, value); err != nil {
			return nil, SegmentHeader{}, err
		}
	}
	for _, id := range []ID128{header.RunID, header.ProcessInstanceID, header.SessionID} {
		if _, err := payload.Write(id[:]); err != nil {
			return nil, SegmentHeader{}, err
		}
	}
	for _, value := range []uint64{
		header.SegmentIndex,
		header.OSPID,
		header.CollectorStartElapsedUS,
		header.SegmentStartElapsedUS,
		header.SegmentStartUnixMS,
		header.IdentitySource,
	} {
		if err := writeUvarint(&payload, value); err != nil {
			return nil, SegmentHeader{}, err
		}
	}
	if header.TimezoneOffsetMinutes < -maxTimezoneOffsetMinutes || header.TimezoneOffsetMinutes > maxTimezoneOffsetMinutes {
		return nil, SegmentHeader{}, fmt.Errorf("timezone offset %d is outside -%d..%d minutes", header.TimezoneOffsetMinutes, maxTimezoneOffsetMinutes, maxTimezoneOffsetMinutes)
	}
	if err := writeUvarint(&payload, encodeSVarint(header.TimezoneOffsetMinutes)); err != nil {
		return nil, SegmentHeader{}, err
	}
	if err := writeLengthDelimited(&payload, []byte(header.ProcessName)); err != nil {
		return nil, SegmentHeader{}, err
	}
	if err := writeLengthDelimited(&payload, header.SymbolNamespace); err != nil {
		return nil, SegmentHeader{}, err
	}
	if err := writeUvarint(&payload, uint64(header.ProcessScope)); err != nil {
		return nil, SegmentHeader{}, err
	}
	if err := writeUvarint(&payload, header.AllowedProcessCount); err != nil {
		return nil, SegmentHeader{}, err
	}
	if err := writeLengthDelimited(&payload, header.ProcessScopeFingerprint); err != nil {
		return nil, SegmentHeader{}, err
	}
	if err := validatePreviousSegmentDigest(header.SegmentIndex, header.PreviousSegmentDigest); err != nil {
		return nil, SegmentHeader{}, err
	}
	if err := writeLengthDelimited(&payload, header.PreviousSegmentDigest); err != nil {
		return nil, SegmentHeader{}, err
	}
	if err := validateProcessRoster(header.ExpectedProcessCount, header.ExpectedProcessFingerprint); err != nil {
		return nil, SegmentHeader{}, err
	}
	if err := writeUvarint(&payload, header.ExpectedProcessCount); err != nil {
		return nil, SegmentHeader{}, err
	}
	if err := writeLengthDelimited(&payload, header.ExpectedProcessFingerprint); err != nil {
		return nil, SegmentHeader{}, err
	}
	complete := uint64(0)
	if header.ProcessRosterDeclarationComplete {
		complete = 1
	}
	if err := writeUvarint(&payload, complete); err != nil {
		return nil, SegmentHeader{}, err
	}
	if payload.Len() > maxHeaderPayloadSize {
		return nil, SegmentHeader{}, fmt.Errorf("header payload too large: %d > %d", payload.Len(), maxHeaderPayloadSize)
	}

	out := make([]byte, len(Magic)+8+payload.Len())
	copy(out, Magic)
	binary.LittleEndian.PutUint32(out[len(Magic):], uint32(payload.Len()))
	binary.LittleEndian.PutUint32(out[len(Magic)+4:], crc32.ChecksumIEEE(payload.Bytes()))
	copy(out[len(Magic)+8:], payload.Bytes())
	return out, header, nil
}

func decodeHeaderPayload(payload []byte) (SegmentHeader, error) {
	if len(payload) > maxHeaderPayloadSize {
		return SegmentHeader{}, fmt.Errorf("header payload too large: %d > %d", len(payload), maxHeaderPayloadSize)
	}
	reader := bytes.NewReader(payload)
	read := func(name string) (uint64, error) {
		value, err := binary.ReadUvarint(reader)
		if err != nil {
			return 0, fmt.Errorf("%s: %w", name, err)
		}
		return value, nil
	}

	var header SegmentHeader
	var err error
	if header.Schema, err = read("schema"); err != nil {
		return SegmentHeader{}, err
	}
	if header.Schema != HeaderSchemaV2 {
		return SegmentHeader{}, fmt.Errorf("unsupported header schema %d", header.Schema)
	}
	if header.RequiredFeatures, err = read("required features"); err != nil {
		return SegmentHeader{}, err
	}
	if header.OptionalFeatures, err = read("optional features"); err != nil {
		return SegmentHeader{}, err
	}
	if err := validateFeatureContract(header.RequiredFeatures, header.OptionalFeatures); err != nil {
		return SegmentHeader{}, err
	}
	for _, target := range []*ID128{&header.RunID, &header.ProcessInstanceID, &header.SessionID} {
		if _, err := io.ReadFull(reader, target[:]); err != nil {
			return SegmentHeader{}, fmt.Errorf("identity: %w", err)
		}
	}
	values := []*uint64{
		&header.SegmentIndex,
		&header.OSPID,
		&header.CollectorStartElapsedUS,
		&header.SegmentStartElapsedUS,
		&header.SegmentStartUnixMS,
		&header.IdentitySource,
	}
	names := []string{
		"segment index",
		"pid",
		"collector start elapsed us",
		"segment start elapsed us",
		"segment start unix ms",
		"identity source",
	}
	for i := range values {
		if *values[i], err = read(names[i]); err != nil {
			return SegmentHeader{}, err
		}
	}
	timezoneOffset, readErr := read("timezone offset minutes")
	if readErr != nil {
		return SegmentHeader{}, readErr
	}
	header.TimezoneOffsetMinutes = decodeSVarint(timezoneOffset)
	if header.TimezoneOffsetMinutes < -maxTimezoneOffsetMinutes || header.TimezoneOffsetMinutes > maxTimezoneOffsetMinutes {
		return SegmentHeader{}, fmt.Errorf("timezone offset %d is outside -%d..%d minutes", header.TimezoneOffsetMinutes, maxTimezoneOffsetMinutes, maxTimezoneOffsetMinutes)
	}
	processName, err := readBoundedBytes(reader, "process name", maxHeaderPayloadSize)
	if err != nil {
		return SegmentHeader{}, err
	}
	if !utf8.Valid(processName) {
		return SegmentHeader{}, fmt.Errorf("process name is not valid UTF-8")
	}
	header.ProcessName = string(processName)
	header.SymbolNamespace, err = readBoundedBytes(reader, "symbol namespace", maxHeaderPayloadSize)
	if err != nil {
		return SegmentHeader{}, err
	}
	scope, readErr := read("process scope")
	if readErr != nil {
		return SegmentHeader{}, readErr
	}
	header.ProcessScope = ProcessScope(scope)
	if header.AllowedProcessCount, readErr = read("allowed process count"); readErr != nil {
		return SegmentHeader{}, readErr
	}
	header.ProcessScopeFingerprint, readErr = readBoundedBytes(reader, "process scope fingerprint", processScopeHashSize)
	if readErr != nil {
		return SegmentHeader{}, readErr
	}
	if err := validateProcessScope(header.ProcessScope, header.AllowedProcessCount, header.ProcessScopeFingerprint); err != nil {
		return SegmentHeader{}, err
	}
	header.PreviousSegmentDigest, err = readBoundedBytes(reader, "previous segment digest", segmentDigestSize)
	if err != nil {
		return SegmentHeader{}, err
	}
	if err := validatePreviousSegmentDigest(header.SegmentIndex, header.PreviousSegmentDigest); err != nil {
		return SegmentHeader{}, err
	}
	if header.ExpectedProcessCount, err = read("expected process count"); err != nil {
		return SegmentHeader{}, err
	}
	header.ExpectedProcessFingerprint, err = readBoundedBytes(reader, "expected process fingerprint", processScopeHashSize)
	if err != nil {
		return SegmentHeader{}, err
	}
	complete, readErr := read("process roster declaration complete")
	if readErr != nil {
		return SegmentHeader{}, readErr
	}
	if complete > 1 {
		return SegmentHeader{}, fmt.Errorf("process roster declaration flag %d is not boolean", complete)
	}
	header.ProcessRosterDeclarationComplete = complete == 1
	if err := validateProcessRoster(header.ExpectedProcessCount, header.ExpectedProcessFingerprint); err != nil {
		return SegmentHeader{}, err
	}
	if reader.Len() != 0 {
		return SegmentHeader{}, fmt.Errorf("header schema %d leaves %d trailing bytes", header.Schema, reader.Len())
	}
	return header, nil
}

const maxTimezoneOffsetMinutes int64 = 14 * 60

func validateFeatureContract(required, optional uint64) error {
	if required != RequiredFeatures && required != BestEffortFeatures {
		return fmt.Errorf(
			"required feature contract 0x%x is not JHLOG %s EXACT 0x%x or BEST_EFFORT 0x%x",
			required,
			FormatVersionString,
			RequiredFeatures,
			BestEffortFeatures,
		)
	}
	if optional != OptionalFeatures {
		return fmt.Errorf(
			"optional feature contract 0x%x differs from JHLOG %s contract 0x%x",
			optional,
			FormatVersionString,
			OptionalFeatures,
		)
	}
	return nil
}

func validatePreviousSegmentDigest(segmentIndex uint64, digest []byte) error {
	if segmentIndex == 0 {
		if len(digest) != 0 {
			return fmt.Errorf("first segment cannot declare a predecessor digest")
		}
		return nil
	}
	if len(digest) != segmentDigestSize {
		return fmt.Errorf("segment %d predecessor digest has %d bytes, want %d", segmentIndex, len(digest), segmentDigestSize)
	}
	return nil
}

func validateProcessRoster(expectedCount uint64, fingerprint []byte) error {
	if expectedCount == 0 {
		return fmt.Errorf("process roster must contain at least one process")
	}
	if len(fingerprint) != processScopeHashSize {
		return fmt.Errorf("process roster fingerprint has %d bytes, want %d", len(fingerprint), processScopeHashSize)
	}
	return nil
}

func validateProcessScope(scope ProcessScope, allowedProcessCount uint64, fingerprint []byte) error {
	switch scope {
	case ProcessScopeAll, ProcessScopeMainOnly:
		if allowedProcessCount != 0 || len(fingerprint) != 0 {
			return fmt.Errorf("process scope %s cannot declare an allowlist identity", scope)
		}
	case ProcessScopeAllowlist:
		if allowedProcessCount == 0 {
			return fmt.Errorf("process allowlist scope must declare at least one process")
		}
		if len(fingerprint) != processScopeHashSize {
			return fmt.Errorf("process allowlist fingerprint has %d bytes, want %d", len(fingerprint), processScopeHashSize)
		}
	default:
		return fmt.Errorf("unknown process scope %d", scope)
	}
	return nil
}

func writeLengthDelimited(w io.Writer, value []byte) error {
	if err := writeUvarint(w, uint64(len(value))); err != nil {
		return err
	}
	return writeAll(w, value)
}

func readBoundedBytes(reader *bytes.Reader, name string, limit uint64) ([]byte, error) {
	length, err := binary.ReadUvarint(reader)
	if err != nil {
		return nil, fmt.Errorf("%s length: %w", name, err)
	}
	if length > limit || length > uint64(reader.Len()) {
		return nil, fmt.Errorf("%s length %d exceeds remaining %d", name, length, reader.Len())
	}
	value := make([]byte, int(length))
	if _, err := io.ReadFull(reader, value); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return value, nil
}

func marshalChunkHeader(metadata chunkMetadata) [chunkHeaderSize]byte {
	var out [chunkHeaderSize]byte
	copy(out[0:4], chunkMagic[:])
	binary.LittleEndian.PutUint16(out[4:6], chunkHeaderSize)
	binary.LittleEndian.PutUint16(out[6:8], metadata.Flags)
	binary.LittleEndian.PutUint32(out[8:12], metadata.Sequence)
	binary.LittleEndian.PutUint32(out[12:16], metadata.StoredLen)
	binary.LittleEndian.PutUint32(out[16:20], metadata.RawLen)
	binary.LittleEndian.PutUint32(out[20:24], metadata.RecordCount)
	binary.LittleEndian.PutUint32(out[24:28], metadata.RawCRC)
	binary.LittleEndian.PutUint32(out[28:32], crc32.ChecksumIEEE(out[:28]))
	return out
}

func parseChunkHeader(raw []byte, expectedSequence uint32) (chunkMetadata, error) {
	if len(raw) != chunkHeaderSize {
		return chunkMetadata{}, fmt.Errorf("chunk header size %d", len(raw))
	}
	if !bytes.Equal(raw[0:4], chunkMagic[:]) {
		return chunkMetadata{}, fmt.Errorf("invalid chunk magic %q", raw[0:4])
	}
	if got := binary.LittleEndian.Uint16(raw[4:6]); got != chunkHeaderSize {
		return chunkMetadata{}, fmt.Errorf("invalid chunk header size %d", got)
	}
	if want, got := binary.LittleEndian.Uint32(raw[28:32]), crc32.ChecksumIEEE(raw[:28]); want != got {
		return chunkMetadata{}, fmt.Errorf("chunk header CRC mismatch: stored %08x, computed %08x", want, got)
	}
	metadata := chunkMetadata{
		Flags:       binary.LittleEndian.Uint16(raw[6:8]),
		Sequence:    binary.LittleEndian.Uint32(raw[8:12]),
		StoredLen:   binary.LittleEndian.Uint32(raw[12:16]),
		RawLen:      binary.LittleEndian.Uint32(raw[16:20]),
		RecordCount: binary.LittleEndian.Uint32(raw[20:24]),
		RawCRC:      binary.LittleEndian.Uint32(raw[24:28]),
	}
	if metadata.Flags&^chunkKnownFlags != 0 {
		return chunkMetadata{}, fmt.Errorf("unsupported chunk flags 0x%x", metadata.Flags&^chunkKnownFlags)
	}
	if metadata.Sequence != expectedSequence {
		return chunkMetadata{}, fmt.Errorf("chunk sequence %d, expected %d", metadata.Sequence, expectedSequence)
	}
	if metadata.RawLen > maxRawChunkSize {
		return chunkMetadata{}, fmt.Errorf("raw chunk length %d exceeds %d", metadata.RawLen, maxRawChunkSize)
	}
	if metadata.StoredLen > maxStoredChunkSize {
		return chunkMetadata{}, fmt.Errorf("stored chunk length %d exceeds %d", metadata.StoredLen, maxStoredChunkSize)
	}
	if metadata.RecordCount > metadata.RawLen {
		return chunkMetadata{}, fmt.Errorf("record count %d is impossible for %d raw bytes", metadata.RecordCount, metadata.RawLen)
	}
	if metadata.Flags&chunkFlagGZIP == 0 && metadata.StoredLen != metadata.RawLen {
		return chunkMetadata{}, fmt.Errorf("raw chunk stored length %d differs from raw length %d", metadata.StoredLen, metadata.RawLen)
	}
	return metadata, nil
}

func marshalCommitTrailer(metadata chunkMetadata) [commitTrailerSize]byte {
	var out [commitTrailerSize]byte
	copy(out[0:4], commitMagic[:])
	binary.LittleEndian.PutUint32(out[4:8], metadata.Sequence)
	binary.LittleEndian.PutUint32(out[8:12], metadata.StoredLen)
	binary.LittleEndian.PutUint32(out[12:16], metadata.RawLen)
	binary.LittleEndian.PutUint32(out[16:20], metadata.RawCRC)
	return out
}

func validateCommitTrailer(raw []byte, metadata chunkMetadata) error {
	if len(raw) != commitTrailerSize {
		return fmt.Errorf("commit trailer size %d", len(raw))
	}
	if !bytes.Equal(raw[0:4], commitMagic[:]) {
		return fmt.Errorf("invalid commit trailer magic %q", raw[0:4])
	}
	if got := binary.LittleEndian.Uint32(raw[4:8]); got != metadata.Sequence {
		return fmt.Errorf("commit sequence %d, expected %d", got, metadata.Sequence)
	}
	if got := binary.LittleEndian.Uint32(raw[8:12]); got != metadata.StoredLen {
		return fmt.Errorf("commit stored length %d, expected %d", got, metadata.StoredLen)
	}
	if got := binary.LittleEndian.Uint32(raw[12:16]); got != metadata.RawLen {
		return fmt.Errorf("commit raw length %d, expected %d", got, metadata.RawLen)
	}
	if got := binary.LittleEndian.Uint32(raw[16:20]); got != metadata.RawCRC {
		return fmt.Errorf("commit raw CRC %08x, expected %08x", got, metadata.RawCRC)
	}
	return nil
}

func compressChunk(raw []byte) ([]byte, error) {
	var stored bytes.Buffer
	writer, err := gzip.NewWriterLevel(&stored, gzip.DefaultCompression)
	if err != nil {
		return nil, err
	}
	if _, err := writer.Write(raw); err != nil {
		_ = writer.Close()
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return stored.Bytes(), nil
}

func decompressChunk(stored []byte, metadata chunkMetadata) ([]byte, error) {
	if metadata.Flags&chunkFlagGZIP == 0 {
		return append([]byte(nil), stored...), nil
	}
	compressed := bytes.NewReader(stored)
	reader, err := gzip.NewReader(compressed)
	if err != nil {
		return nil, fmt.Errorf("gzip header: %w", err)
	}
	reader.Multistream(false)
	raw, readErr := io.ReadAll(io.LimitReader(reader, int64(maxRawChunkSize)+1))
	closeErr := reader.Close()
	if readErr != nil {
		return nil, fmt.Errorf("gzip payload: %w", readErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("gzip close: %w", closeErr)
	}
	if len(raw) > maxRawChunkSize {
		return nil, fmt.Errorf("decompressed chunk exceeds %d bytes", maxRawChunkSize)
	}
	if compressed.Len() != 0 {
		return nil, fmt.Errorf("gzip chunk has %d trailing bytes", compressed.Len())
	}
	return raw, nil
}

func writeAll(w io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := w.Write(data)
		if n > 0 {
			data = data[n:]
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}
