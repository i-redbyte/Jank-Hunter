package jhlog

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"math"
	"os"
)

const (
	v1FileHeaderOffset   = int64(16)
	v1SuperblockAOffset  = int64(8 * 1024)
	v1SuperblockBOffset  = v1SuperblockAOffset + v1SuperblockBytes
	v1ArenaOffset        = int64(80 * 1024)
	v1SuperblockBytes    = int64(256)
	v1ChunkHeaderBytes   = 40
	v1CommitTrailerBytes = 24
	v1SuperblockSchema   = uint16(1)
)

var (
	v1SuperblockMagic = []byte{'J', 'H', 'S', 'B'}
	v1ChunkMagic      = []byte{'J', 'H', 'C', '1'}
	v1CommitMagic     = []byte{'J', 'H', 'C', 'M'}
	errV1ArenaGap     = errors.New("1.0 arena gap")
)

type v1Superblock struct {
	generation     uint64
	arenaCapacity  uint64
	head           uint64
	tail           uint64
	usedBytes      uint64
	firstSequence  uint64
	nextSequence   uint64
	chunkCount     uint64
	wrapCount      uint64
	evictedChunks  uint64
	evictedBytes   uint64
	generatedBytes uint64
	retainedBytes  uint64
}

func readV1SessionHeader(file *os.File) (SegmentHeader, error) {
	if err := readAndValidateV1Prefix(file); err != nil {
		return SegmentHeader{}, err
	}
	if _, err := file.Seek(v1FileHeaderOffset, io.SeekStart); err != nil {
		return SegmentHeader{}, err
	}
	header, err := readV9Header(file)
	if err != nil {
		return SegmentHeader{}, fmt.Errorf("read 1.0 session header: %w", err)
	}
	if err := validateV9Header(header); err != nil {
		return SegmentHeader{}, fmt.Errorf("invalid 1.0 session header: %w", err)
	}
	return header, nil
}

func streamBinaryV1(file *os.File, result StreamResult, handle EventHandler) (StreamResult, error) {
	header, err := readV1SessionHeader(file)
	if err != nil {
		return corruptResult(result, err)
	}
	result.Header = header
	result.FormatMajor = CurrentFormatMajor
	result.FormatMinor = CurrentFormatMinor
	result.LogGrowth, result.Warnings = readV1LogGrowth(file, result.Warnings)

	superblock, err := readLatestV1Superblock(file)
	if err != nil {
		return corruptResult(result, err)
	}
	if superblock.firstSequence > math.MaxUint32 || superblock.nextSequence > math.MaxUint32+1 {
		return corruptResult(result, fmt.Errorf("1.0 chunk sequence exceeds supported range"))
	}
	minimumChunkBytes := uint64(v1ChunkHeaderBytes + v1CommitTrailerBytes + 1)
	if superblock.chunkCount > superblock.arenaCapacity/minimumChunkBytes+1 {
		return corruptResult(result, fmt.Errorf("1.0 chunk count %d exceeds arena bounds", superblock.chunkCount))
	}

	position := superblock.head
	expectedSequence := superblock.firstSequence
	for index := uint64(0); index < superblock.chunkCount; index++ {
		metadata, raw, next, readErr := readV1Chunk(file, superblock, position, expectedSequence)
		if readErr != nil && position != 0 && errors.Is(readErr, errV1ArenaGap) {
			position = 0
			metadata, raw, next, readErr = readV1Chunk(file, superblock, position, expectedSequence)
		}
		if readErr != nil {
			return corruptResult(result, fmt.Errorf("1.0 chunk %d: %w", expectedSequence, readErr))
		}

		// Format 1.0 repeats every referenced dictionary definition in the same chunk.
		dict := map[uint64]string{}
		kinds := map[uint64]DictKind{}
		_, decodeErr := decodeChunkRecords(
			raw,
			metadata,
			header,
			result.Source,
			dict,
			kinds,
			handle,
			&result,
		)
		if decodeErr != nil {
			var callback callbackError
			if errors.As(decodeErr, &callback) {
				return result, callback.error
			}
			return corruptResult(result, fmt.Errorf("1.0 chunk %d records: %w", expectedSequence, decodeErr))
		}
		result.StoredChunkBytes += uint64(metadata.StoredLen)
		result.CommittedChunks++
		position = next
		expectedSequence++
	}

	if expectedSequence != superblock.nextSequence {
		return corruptResult(
			result,
			fmt.Errorf("1.0 sequence frontier %d, expected %d", expectedSequence, superblock.nextSequence),
		)
	}
	if result.SegmentEnd != nil {
		result.Status = SegmentStatusClosedClean
		result.Sealed = true
	} else {
		result.Status = SegmentStatusOpenClean
	}
	return result, nil
}

func readAndValidateV1Prefix(file *os.File) error {
	var prefix [10]byte
	if _, err := file.ReadAt(prefix[:], 0); err != nil {
		return fmt.Errorf("read 1.0 file prefix: %w", err)
	}
	expected := [10]byte{'J', 'H', 'L', 'O', 'G', '\r', '\n', CurrentFormatMarker, CurrentFormatMajor, CurrentFormatMinor}
	if prefix != expected {
		return fmt.Errorf("invalid .jhlog 1.0 prefix")
	}
	return nil
}

func readLatestV1Superblock(file *os.File) (v1Superblock, error) {
	a, aErr := readV1SuperblockAt(file, v1SuperblockAOffset)
	b, bErr := readV1SuperblockAt(file, v1SuperblockBOffset)
	switch {
	case aErr == nil && bErr == nil:
		if b.generation > a.generation {
			return b, nil
		}
		return a, nil
	case aErr == nil:
		return a, nil
	case bErr == nil:
		return b, nil
	default:
		return v1Superblock{}, fmt.Errorf("no valid 1.0 superblock: A: %v; B: %v", aErr, bErr)
	}
}

func readV1SuperblockAt(file *os.File, offset int64) (v1Superblock, error) {
	raw := make([]byte, v1SuperblockBytes)
	if _, err := file.ReadAt(raw, offset); err != nil {
		return v1Superblock{}, err
	}
	if !bytes.Equal(raw[:4], v1SuperblockMagic) {
		return v1Superblock{}, fmt.Errorf("magic mismatch")
	}
	if schema := binary.LittleEndian.Uint16(raw[4:6]); schema != v1SuperblockSchema {
		return v1Superblock{}, fmt.Errorf("schema %d is unsupported", schema)
	}
	storedCRC := binary.LittleEndian.Uint32(raw[len(raw)-4:])
	if computed := crc32.ChecksumIEEE(raw[:len(raw)-4]); storedCRC != computed {
		return v1Superblock{}, fmt.Errorf("CRC mismatch")
	}
	block := v1Superblock{
		generation:     binary.LittleEndian.Uint64(raw[8:16]),
		arenaCapacity:  binary.LittleEndian.Uint64(raw[16:24]),
		head:           binary.LittleEndian.Uint64(raw[24:32]),
		tail:           binary.LittleEndian.Uint64(raw[32:40]),
		usedBytes:      binary.LittleEndian.Uint64(raw[40:48]),
		firstSequence:  binary.LittleEndian.Uint64(raw[48:56]),
		nextSequence:   binary.LittleEndian.Uint64(raw[56:64]),
		chunkCount:     binary.LittleEndian.Uint64(raw[64:72]),
		wrapCount:      binary.LittleEndian.Uint64(raw[72:80]),
		evictedChunks:  binary.LittleEndian.Uint64(raw[80:88]),
		evictedBytes:   binary.LittleEndian.Uint64(raw[88:96]),
		generatedBytes: binary.LittleEndian.Uint64(raw[96:104]),
		retainedBytes:  binary.LittleEndian.Uint64(raw[104:112]),
	}
	if block.generation == 0 || block.arenaCapacity == 0 {
		return v1Superblock{}, fmt.Errorf("empty state")
	}
	if block.head > block.arenaCapacity || block.tail > block.arenaCapacity || block.usedBytes > block.arenaCapacity {
		return v1Superblock{}, fmt.Errorf("arena offsets exceed capacity")
	}
	if block.firstSequence > block.nextSequence || block.chunkCount != block.nextSequence-block.firstSequence {
		return v1Superblock{}, fmt.Errorf("sequence window is inconsistent")
	}
	if block.wrapCount > block.evictedChunks {
		return v1Superblock{}, fmt.Errorf("wrap count exceeds evicted chunk count")
	}
	if block.evictedBytes > block.generatedBytes ||
		block.generatedBytes-block.evictedBytes != block.usedBytes {
		return v1Superblock{}, fmt.Errorf("generated, evicted, and retained arena bytes are inconsistent")
	}
	if block.retainedBytes < uint64(v1ArenaOffset) ||
		block.retainedBytes-uint64(v1ArenaOffset) > block.arenaCapacity {
		return v1Superblock{}, fmt.Errorf("retained file bytes exceed arena bounds")
	}
	return block, nil
}

func readV1Chunk(
	file *os.File,
	block v1Superblock,
	position uint64,
	expectedSequence uint64,
) (chunkMetadata, []byte, uint64, error) {
	headerBytes := uint64(v1ChunkHeaderBytes)
	if position > block.arenaCapacity || headerBytes > block.arenaCapacity-position {
		return chunkMetadata{}, nil, position, fmt.Errorf("%w: header exceeds arena", errV1ArenaGap)
	}
	if position > uint64(math.MaxInt64-v1ArenaOffset)-headerBytes {
		return chunkMetadata{}, nil, position, fmt.Errorf("chunk position exceeds supported file offset")
	}
	header := make([]byte, v1ChunkHeaderBytes)
	if _, err := file.ReadAt(header, v1ArenaOffset+int64(position)); err != nil {
		if position != 0 && errors.Is(err, io.EOF) {
			return chunkMetadata{}, nil, position, fmt.Errorf("%w: unallocated arena tail", errV1ArenaGap)
		}
		return chunkMetadata{}, nil, position, fmt.Errorf("read header: %w", err)
	}
	if !bytes.Equal(header[:4], v1ChunkMagic) {
		return chunkMetadata{}, nil, position, fmt.Errorf("%w: magic mismatch", errV1ArenaGap)
	}
	if size := binary.LittleEndian.Uint16(header[4:6]); size != v1ChunkHeaderBytes {
		return chunkMetadata{}, nil, position, fmt.Errorf("header size %d", size)
	}
	sequence := binary.LittleEndian.Uint64(header[8:16])
	if sequence != expectedSequence || sequence > math.MaxUint32 {
		return chunkMetadata{}, nil, position, fmt.Errorf("sequence %d, expected %d", sequence, expectedSequence)
	}
	storedCRC := binary.LittleEndian.Uint32(header[32:36])
	if computed := crc32.ChecksumIEEE(header[:32]); storedCRC != computed {
		return chunkMetadata{}, nil, position, fmt.Errorf("header CRC mismatch")
	}
	metadata := chunkMetadata{
		Flags:       binary.LittleEndian.Uint16(header[6:8]),
		Sequence:    uint32(sequence),
		StoredLen:   binary.LittleEndian.Uint32(header[16:20]),
		RawLen:      binary.LittleEndian.Uint32(header[20:24]),
		RecordCount: binary.LittleEndian.Uint32(header[24:28]),
		RawCRC:      binary.LittleEndian.Uint32(header[28:32]),
	}
	if metadata.StoredLen == 0 || metadata.RawLen == 0 || metadata.RawLen > maxRawChunkSize {
		return chunkMetadata{}, nil, position, fmt.Errorf("invalid stored/raw lengths %d/%d", metadata.StoredLen, metadata.RawLen)
	}
	if metadata.StoredLen > maxStoredChunkSize {
		return chunkMetadata{}, nil, position, fmt.Errorf("stored length %d exceeds limit %d", metadata.StoredLen, maxStoredChunkSize)
	}
	total := uint64(v1ChunkHeaderBytes) + uint64(metadata.StoredLen) + uint64(v1CommitTrailerBytes)
	if total > block.arenaCapacity || position > block.arenaCapacity-total {
		return chunkMetadata{}, nil, position, fmt.Errorf("chunk exceeds arena")
	}
	if position > uint64(math.MaxInt64-v1ArenaOffset)-total {
		return chunkMetadata{}, nil, position, fmt.Errorf("chunk exceeds supported file offset")
	}
	stored := make([]byte, metadata.StoredLen)
	storedOffset := v1ArenaOffset + int64(position) + v1ChunkHeaderBytes
	if _, err := file.ReadAt(stored, storedOffset); err != nil {
		return chunkMetadata{}, nil, position, fmt.Errorf("read payload: %w", err)
	}
	trailer := make([]byte, v1CommitTrailerBytes)
	if _, err := file.ReadAt(trailer, storedOffset+int64(metadata.StoredLen)); err != nil {
		return chunkMetadata{}, nil, position, fmt.Errorf("read commit: %w", err)
	}
	if !bytes.Equal(trailer[:4], v1CommitMagic) ||
		binary.LittleEndian.Uint64(trailer[4:12]) != sequence ||
		binary.LittleEndian.Uint32(trailer[12:16]) != metadata.StoredLen ||
		binary.LittleEndian.Uint32(trailer[16:20]) != metadata.RawLen ||
		binary.LittleEndian.Uint32(trailer[20:24]) != metadata.RawCRC {
		return chunkMetadata{}, nil, position, fmt.Errorf("commit trailer mismatch")
	}
	raw, err := decompressChunk(stored, metadata)
	if err != nil {
		return chunkMetadata{}, nil, position, fmt.Errorf("decompress: %w", err)
	}
	if len(raw) != int(metadata.RawLen) || crc32.ChecksumIEEE(raw) != metadata.RawCRC {
		return chunkMetadata{}, nil, position, fmt.Errorf("raw payload integrity mismatch")
	}
	next := position + total
	if next >= block.arenaCapacity {
		next = 0
	}
	return metadata, raw, next, nil
}
