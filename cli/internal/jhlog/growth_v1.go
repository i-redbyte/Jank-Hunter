package jhlog

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"os"
)

const (
	v1LiveAOffset          = int64(8*1024 + 2*v1SuperblockBytes)
	v1LiveBOffset          = v1LiveAOffset + v1LiveBytes
	v1HistoryOffset        = int64(12 * 1024)
	v1HistoryMaximumBytes  = 64 * 1024
	v1LiveBytes            = int64(256)
	v1HistoryHeaderBytes   = 64
	v1HistorySessionBytes  = 96
	v1HistoryDayBytes      = 80
	v1EmbeddedSessionLimit = 256
	v1EmbeddedDayLimit     = 400
)

var (
	v1GrowthHistoryMagic = []byte{'J', 'H', 'G', 'P'}
	v1GrowthLiveMagic    = []byte{'J', 'H', 'G', 'L'}
)

func readV1LogGrowth(file *os.File, warnings []string) (*LogGrowthProjection, []string) {
	history, historyErr := readV1GrowthHistory(file)
	if historyErr != nil {
		warnings = append(warnings, fmt.Sprintf("ignored invalid log-growth history: %v", historyErr))
	}
	live, liveErr := readLatestV1GrowthLive(file)
	if liveErr != nil {
		warnings = append(warnings, fmt.Sprintf("ignored invalid log-growth live summary: %v", liveErr))
	}
	if history == nil && live == nil {
		return nil, warnings
	}
	if history == nil {
		history = &LogGrowthProjection{}
	}
	history.Live = live
	return history, warnings
}

func readV1GrowthHistory(file *os.File) (*LogGrowthProjection, error) {
	header := make([]byte, v1HistoryHeaderBytes)
	if _, err := file.ReadAt(header, v1HistoryOffset); err != nil {
		return nil, nil
	}
	if !bytes.Equal(header[:4], v1GrowthHistoryMagic) {
		return nil, nil
	}
	if header[4] != CurrentFormatMajor || header[5] != CurrentFormatMinor {
		return nil, fmt.Errorf("schema %d.%d is unsupported", header[4], header[5])
	}
	if binary.LittleEndian.Uint16(header[6:8]) != v1HistoryHeaderBytes {
		return nil, fmt.Errorf("header size is invalid")
	}
	total := binary.LittleEndian.Uint32(header[8:12])
	if total < v1HistoryHeaderBytes || total > v1HistoryMaximumBytes {
		return nil, fmt.Errorf("payload size %d is outside bounds", total)
	}
	raw := make([]byte, total)
	if _, err := file.ReadAt(raw, v1HistoryOffset); err != nil {
		return nil, err
	}
	storedCRC := binary.LittleEndian.Uint32(raw[60:64])
	for index := 60; index < 64; index++ {
		raw[index] = 0
	}
	if crc32.ChecksumIEEE(raw) != storedCRC {
		return nil, fmt.Errorf("CRC mismatch")
	}
	sessionBytes := binary.LittleEndian.Uint16(raw[12:14])
	dayBytes := binary.LittleEndian.Uint16(raw[14:16])
	sessionCount := binary.LittleEndian.Uint32(raw[16:20])
	dayCount := binary.LittleEndian.Uint32(raw[20:24])
	if sessionBytes != v1HistorySessionBytes || dayBytes != v1HistoryDayBytes {
		return nil, fmt.Errorf("record sizes %d/%d are unsupported", sessionBytes, dayBytes)
	}
	if sessionCount > v1EmbeddedSessionLimit || dayCount > v1EmbeddedDayLimit {
		return nil, fmt.Errorf("record counts %d/%d exceed bounds", sessionCount, dayCount)
	}
	expected := uint64(v1HistoryHeaderBytes) +
		uint64(sessionCount)*v1HistorySessionBytes +
		uint64(dayCount)*v1HistoryDayBytes
	if expected != uint64(total) {
		return nil, fmt.Errorf("payload layout requires %d bytes, got %d", expected, total)
	}

	projection := &LogGrowthProjection{
		Generation:   binary.LittleEndian.Uint64(raw[24:32]),
		CapturedAtMS: binary.LittleEndian.Uint64(raw[32:40]),
		Sessions:     make([]LogGrowthSession, 0, sessionCount),
		Days:         make([]LogGrowthDay, 0, dayCount),
	}
	offset := v1HistoryHeaderBytes
	for index := uint32(0); index < sessionCount; index++ {
		projection.Sessions = append(projection.Sessions, decodeV1GrowthSession(raw[offset:offset+v1HistorySessionBytes]))
		offset += v1HistorySessionBytes
	}
	for index := uint32(0); index < dayCount; index++ {
		projection.Days = append(projection.Days, decodeV1GrowthDay(raw[offset:offset+v1HistoryDayBytes]))
		offset += v1HistoryDayBytes
	}
	return projection, nil
}

func readLatestV1GrowthLive(file *os.File) (*LogGrowthSession, error) {
	aPresent, aGeneration, aSession, aErr := readV1GrowthLiveAt(file, v1LiveAOffset)
	bPresent, bGeneration, bSession, bErr := readV1GrowthLiveAt(file, v1LiveBOffset)
	present := aPresent || bPresent
	switch {
	case aErr == nil && bErr == nil && aSession != nil && bSession != nil:
		if bGeneration > aGeneration {
			return bSession, nil
		}
		return aSession, nil
	case aErr == nil && aSession != nil:
		if bPresent && bErr != nil {
			return aSession, fmt.Errorf("B: %w", bErr)
		}
		return aSession, nil
	case bErr == nil && bSession != nil:
		if aPresent && aErr != nil {
			return bSession, fmt.Errorf("A: %w", aErr)
		}
		return bSession, nil
	case !present:
		return nil, nil
	default:
		return nil, fmt.Errorf("A: %v; B: %v", aErr, bErr)
	}
}

func readV1GrowthLiveAt(file *os.File, offset int64) (bool, uint64, *LogGrowthSession, error) {
	raw := make([]byte, v1LiveBytes)
	if _, err := file.ReadAt(raw, offset); err != nil {
		return false, 0, nil, nil
	}
	if !bytes.Equal(raw[:4], v1GrowthLiveMagic) {
		return false, 0, nil, nil
	}
	if raw[4] != CurrentFormatMajor || raw[5] != CurrentFormatMinor ||
		binary.LittleEndian.Uint16(raw[6:8]) != uint16(v1LiveBytes) {
		return true, 0, nil, fmt.Errorf("unsupported live schema")
	}
	storedCRC := binary.LittleEndian.Uint32(raw[len(raw)-4:])
	if crc32.ChecksumIEEE(raw[:len(raw)-4]) != storedCRC {
		return true, 0, nil, fmt.Errorf("CRC mismatch")
	}
	generation := binary.LittleEndian.Uint64(raw[8:16])
	high := binary.LittleEndian.Uint64(raw[16:24])
	low := binary.LittleEndian.Uint64(raw[24:32])
	flags := binary.LittleEndian.Uint32(raw[36:40])
	session := &LogGrowthSession{
		SessionID:            fmt.Sprintf("%016x", high^low),
		DayKey:               binary.LittleEndian.Uint32(raw[32:36]),
		StartedAtMS:          binary.LittleEndian.Uint64(raw[40:48]),
		EndedAtMS:            binary.LittleEndian.Uint64(raw[48:56]),
		ConfiguredLimitBytes: binary.LittleEndian.Uint64(raw[56:64]),
		MaximumRetainedBytes: binary.LittleEndian.Uint64(raw[64:72]),
		GeneratedBytes:       binary.LittleEndian.Uint64(raw[72:80]),
		OverflowCount:        binary.LittleEndian.Uint64(raw[80:88]),
		EvictedChunkCount:    binary.LittleEndian.Uint64(raw[88:96]),
		EvictedBytes:         binary.LittleEndian.Uint64(raw[96:104]),
		FirstOverflowAtMS:    binary.LittleEndian.Uint64(raw[104:112]),
		LastOverflowAtMS:     binary.LittleEndian.Uint64(raw[112:120]),
		Completed:            flags&1 != 0,
	}
	return true, generation, session, nil
}

func decodeV1GrowthSession(raw []byte) LogGrowthSession {
	flags := binary.LittleEndian.Uint32(raw[12:16])
	return LogGrowthSession{
		SessionID:            fmt.Sprintf("%016x", binary.LittleEndian.Uint64(raw[0:8])),
		DayKey:               binary.LittleEndian.Uint32(raw[8:12]),
		StartedAtMS:          binary.LittleEndian.Uint64(raw[16:24]),
		EndedAtMS:            binary.LittleEndian.Uint64(raw[24:32]),
		ConfiguredLimitBytes: binary.LittleEndian.Uint64(raw[32:40]),
		MaximumRetainedBytes: binary.LittleEndian.Uint64(raw[40:48]),
		GeneratedBytes:       binary.LittleEndian.Uint64(raw[48:56]),
		OverflowCount:        binary.LittleEndian.Uint64(raw[56:64]),
		EvictedChunkCount:    binary.LittleEndian.Uint64(raw[64:72]),
		EvictedBytes:         binary.LittleEndian.Uint64(raw[72:80]),
		FirstOverflowAtMS:    binary.LittleEndian.Uint64(raw[80:88]),
		LastOverflowAtMS:     binary.LittleEndian.Uint64(raw[88:96]),
		Completed:            true,
		Recovered:            flags&1 != 0,
	}
}

func decodeV1GrowthDay(raw []byte) LogGrowthDay {
	return LogGrowthDay{
		DayKey:                binary.LittleEndian.Uint32(raw[0:4]),
		SessionCount:          binary.LittleEndian.Uint64(raw[8:16]),
		TotalDurationMS:       binary.LittleEndian.Uint64(raw[16:24]),
		GeneratedBytes:        binary.LittleEndian.Uint64(raw[24:32]),
		MaximumRetainedBytes:  binary.LittleEndian.Uint64(raw[32:40]),
		MaximumFillPermille:   binary.LittleEndian.Uint64(raw[40:48]),
		SessionsReachingLimit: binary.LittleEndian.Uint64(raw[48:56]),
		OverflowCount:         binary.LittleEndian.Uint64(raw[56:64]),
		EvictedChunkCount:     binary.LittleEndian.Uint64(raw[64:72]),
		EvictedBytes:          binary.LittleEndian.Uint64(raw[72:80]),
	}
}
