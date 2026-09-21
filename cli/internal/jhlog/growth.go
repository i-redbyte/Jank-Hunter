package jhlog

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
)

const (
	growthLiveBytes           = 256
	growthHistoryMaximumBytes = 64 * 1024
	growthHistoryHeaderBytes  = 64
	growthHistorySessionBytes = 96
	growthHistoryDayBytes     = 80
	growthSessionLimit        = 256
	growthDayLimit            = 400
)

var (
	growthHistoryMagic = []byte{'J', 'H', 'G', 'P'}
	growthLiveMagic    = []byte{'J', 'H', 'G', 'L'}
)

func decodeLogGrowthRecord(kind LogGrowthRecordKind, raw []byte) (*LogGrowthRecord, error) {
	switch kind {
	case LogGrowthHistory:
		projection, err := decodeGrowthHistory(raw)
		if err != nil {
			return nil, err
		}
		return &LogGrowthRecord{
			Kind:       kind,
			Generation: projection.Generation,
			Projection: projection,
			Raw:        raw,
		}, nil
	case LogGrowthLive:
		generation, live, err := decodeGrowthLive(raw)
		if err != nil {
			return nil, err
		}
		return &LogGrowthRecord{Kind: kind, Generation: generation, Live: live, Raw: raw}, nil
	default:
		return nil, fmt.Errorf("unsupported log-growth record kind %d", kind)
	}
}

func decodeGrowthHistory(raw []byte) (*LogGrowthProjection, error) {
	if len(raw) < growthHistoryHeaderBytes || len(raw) > growthHistoryMaximumBytes {
		return nil, fmt.Errorf("history payload size %d is outside bounds", len(raw))
	}
	if !bytes.Equal(raw[:4], growthHistoryMagic) {
		return nil, fmt.Errorf("invalid history magic")
	}
	if raw[4] != 2 || raw[5] != 0 {
		return nil, fmt.Errorf("unsupported history schema %d.%d", raw[4], raw[5])
	}
	if binary.LittleEndian.Uint16(raw[6:8]) != growthHistoryHeaderBytes {
		return nil, fmt.Errorf("invalid history header size")
	}
	if total := binary.LittleEndian.Uint32(raw[8:12]); uint64(total) != uint64(len(raw)) {
		return nil, fmt.Errorf("history payload declares %d bytes, got %d", total, len(raw))
	}
	if binary.LittleEndian.Uint16(raw[12:14]) != growthHistorySessionBytes ||
		binary.LittleEndian.Uint16(raw[14:16]) != growthHistoryDayBytes {
		return nil, fmt.Errorf("unsupported history record sizes")
	}
	sessionCount := binary.LittleEndian.Uint32(raw[16:20])
	dayCount := binary.LittleEndian.Uint32(raw[20:24])
	if sessionCount > growthSessionLimit || dayCount > growthDayLimit {
		return nil, fmt.Errorf("history record counts %d/%d exceed bounds", sessionCount, dayCount)
	}
	expected := uint64(growthHistoryHeaderBytes) +
		uint64(sessionCount)*growthHistorySessionBytes + uint64(dayCount)*growthHistoryDayBytes
	if expected != uint64(len(raw)) {
		return nil, fmt.Errorf("history layout requires %d bytes, got %d", expected, len(raw))
	}
	storedCRC := binary.LittleEndian.Uint32(raw[60:64])
	crc := crc32.NewIEEE()
	_, _ = crc.Write(raw[:60])
	_, _ = crc.Write([]byte{0, 0, 0, 0})
	_, _ = crc.Write(raw[64:])
	if crc.Sum32() != storedCRC {
		return nil, fmt.Errorf("history CRC mismatch")
	}

	projection := &LogGrowthProjection{
		Generation:   binary.LittleEndian.Uint64(raw[24:32]),
		HasHistory:   true,
		CapturedAtMS: binary.LittleEndian.Uint64(raw[32:40]),
		Sessions:     make([]LogGrowthSession, 0, sessionCount),
		Days:         make([]LogGrowthDay, 0, dayCount),
	}
	offset := growthHistoryHeaderBytes
	for index := uint32(0); index < sessionCount; index++ {
		projection.Sessions = append(projection.Sessions, decodeGrowthSession(raw[offset:offset+growthHistorySessionBytes]))
		offset += growthHistorySessionBytes
	}
	for index := uint32(0); index < dayCount; index++ {
		projection.Days = append(projection.Days, decodeGrowthDay(raw[offset:offset+growthHistoryDayBytes]))
		offset += growthHistoryDayBytes
	}
	return projection, nil
}

func decodeGrowthLive(raw []byte) (uint64, *LogGrowthSession, error) {
	if len(raw) != growthLiveBytes {
		return 0, nil, fmt.Errorf("live payload size %d, expected %d", len(raw), growthLiveBytes)
	}
	if !bytes.Equal(raw[:4], growthLiveMagic) {
		return 0, nil, fmt.Errorf("invalid live magic")
	}
	if raw[4] != 2 || raw[5] != 0 || binary.LittleEndian.Uint16(raw[6:8]) != growthLiveBytes {
		return 0, nil, fmt.Errorf("unsupported live schema")
	}
	storedCRC := binary.LittleEndian.Uint32(raw[len(raw)-4:])
	if crc32.ChecksumIEEE(raw[:len(raw)-4]) != storedCRC {
		return 0, nil, fmt.Errorf("live CRC mismatch")
	}
	generation := binary.LittleEndian.Uint64(raw[8:16])
	high := binary.LittleEndian.Uint64(raw[16:24])
	low := binary.LittleEndian.Uint64(raw[24:32])
	flags := binary.LittleEndian.Uint32(raw[36:40])
	if flags&^uint32(1) != 0 {
		return 0, nil, fmt.Errorf("unsupported live flags 0x%x", flags)
	}
	session := &LogGrowthSession{
		SessionID:            fmt.Sprintf("%016x", high^low),
		DayKey:               binary.LittleEndian.Uint32(raw[32:36]),
		StartedAtMS:          binary.LittleEndian.Uint64(raw[40:48]),
		EndedAtMS:            binary.LittleEndian.Uint64(raw[48:56]),
		ConfiguredLimitBytes: binary.LittleEndian.Uint64(raw[56:64]),
		MaximumRetainedBytes: binary.LittleEndian.Uint64(raw[64:72]),
		GeneratedBytes:       binary.LittleEndian.Uint64(raw[72:80]),
		LimitReachedCount:    binary.LittleEndian.Uint64(raw[80:88]),
		SegmentRotationCount: binary.LittleEndian.Uint64(raw[88:96]),
		ArchiveEvictedBytes:  binary.LittleEndian.Uint64(raw[96:104]),
		FirstLimitReachedMS:  binary.LittleEndian.Uint64(raw[104:112]),
		LastLimitReachedMS:   binary.LittleEndian.Uint64(raw[112:120]),
		Completed:            flags&1 != 0,
	}
	return generation, session, nil
}

func decodeGrowthSession(raw []byte) LogGrowthSession {
	flags := binary.LittleEndian.Uint32(raw[12:16])
	return LogGrowthSession{
		SessionID:            fmt.Sprintf("%016x", binary.LittleEndian.Uint64(raw[0:8])),
		DayKey:               binary.LittleEndian.Uint32(raw[8:12]),
		StartedAtMS:          binary.LittleEndian.Uint64(raw[16:24]),
		EndedAtMS:            binary.LittleEndian.Uint64(raw[24:32]),
		ConfiguredLimitBytes: binary.LittleEndian.Uint64(raw[32:40]),
		MaximumRetainedBytes: binary.LittleEndian.Uint64(raw[40:48]),
		GeneratedBytes:       binary.LittleEndian.Uint64(raw[48:56]),
		LimitReachedCount:    binary.LittleEndian.Uint64(raw[56:64]),
		SegmentRotationCount: binary.LittleEndian.Uint64(raw[64:72]),
		ArchiveEvictedBytes:  binary.LittleEndian.Uint64(raw[72:80]),
		FirstLimitReachedMS:  binary.LittleEndian.Uint64(raw[80:88]),
		LastLimitReachedMS:   binary.LittleEndian.Uint64(raw[88:96]),
		Completed:            true,
		Recovered:            flags&1 != 0,
	}
}

func decodeGrowthDay(raw []byte) LogGrowthDay {
	return LogGrowthDay{
		DayKey:                binary.LittleEndian.Uint32(raw[0:4]),
		SessionCount:          binary.LittleEndian.Uint64(raw[8:16]),
		TotalDurationMS:       binary.LittleEndian.Uint64(raw[16:24]),
		GeneratedBytes:        binary.LittleEndian.Uint64(raw[24:32]),
		MaximumRetainedBytes:  binary.LittleEndian.Uint64(raw[32:40]),
		MaximumFillPermille:   binary.LittleEndian.Uint64(raw[40:48]),
		SessionsReachingLimit: binary.LittleEndian.Uint64(raw[48:56]),
		LimitReachedCount:     binary.LittleEndian.Uint64(raw[56:64]),
		SegmentRotationCount:  binary.LittleEndian.Uint64(raw[64:72]),
		ArchiveEvictedBytes:   binary.LittleEndian.Uint64(raw[72:80]),
	}
}

func applyLogGrowthRecord(result *StreamResult, record *LogGrowthRecord) {
	if record == nil {
		return
	}
	if record.Projection != nil {
		candidate := record.Projection
		if result.LogGrowth == nil {
			result.LogGrowth = candidate
		} else if !result.LogGrowth.HasHistory || candidate.Generation > result.LogGrowth.Generation ||
			(candidate.Generation == result.LogGrowth.Generation && candidate.CapturedAtMS >= result.LogGrowth.CapturedAtMS) {
			live := result.LogGrowth.Live
			liveGeneration := result.LogGrowth.LiveGeneration
			result.LogGrowth = candidate
			result.LogGrowth.Live = live
			result.LogGrowth.LiveGeneration = liveGeneration
		}
	}
	if record.Live == nil {
		return
	}
	if result.LogGrowth == nil {
		result.LogGrowth = &LogGrowthProjection{}
	}
	if record.Generation >= result.LogGrowth.LiveGeneration {
		result.LogGrowth.LiveGeneration = record.Generation
		result.LogGrowth.Live = record.Live
	}
}
