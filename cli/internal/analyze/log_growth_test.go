package analyze

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func TestBuildLogGrowthSummaryUsesNewestDailyBaselineWithoutDoubleCounting(t *testing.T) {
	first := growthSession("first", 100, 150, true)
	second := growthSession("second", 160, 190, true)
	current := growthSession("current", 200, 240, false)
	results := []jhlog.StreamResult{
		{LogGrowth: &jhlog.LogGrowthProjection{
			Generation:   2,
			CapturedAtMS: 100,
			Sessions:     []jhlog.LogGrowthSession{first},
			Days:         []jhlog.LogGrowthDay{{DayKey: 20270115, SessionCount: 1, GeneratedBytes: 100}},
			Live:         &second,
		}},
		{LogGrowth: &jhlog.LogGrowthProjection{
			Generation:   3,
			CapturedAtMS: 200,
			Sessions:     []jhlog.LogGrowthSession{first, second},
			Days:         []jhlog.LogGrowthDay{{DayKey: 20270115, SessionCount: 2, GeneratedBytes: 200}},
			Live:         &current,
		}},
	}

	summary := buildLogGrowthSummary(results)
	if !summary.Available || summary.HistoryGeneration != 3 || summary.CurrentSession == nil {
		t.Fatalf("summary = %+v", summary)
	}
	if len(summary.Sessions) != 3 {
		t.Fatalf("sessions = %+v", summary.Sessions)
	}
	if len(summary.Days) != 1 || summary.Days[0].SessionCount != 3 || summary.Days[0].GeneratedBytes != 300 {
		t.Fatalf("days = %+v", summary.Days)
	}
	if summary.CurrentSession.SessionID != "current" {
		t.Fatalf("current = %+v", summary.CurrentSession)
	}
}

func TestBuildLogGrowthSummaryIsIndependentOfInputOrderWhenCompletionMatchesLastCheckpoint(t *testing.T) {
	partial := growthSession("same", 100, 200, false)
	partial.GeneratedBytes = 500
	partial.MaximumRetainedBytes = 450
	partial.OverflowCount = 2
	partial.EvictedChunkCount = 3
	partial.EvictedBytes = 50
	completed := partial
	completed.Completed = true
	current := growthSession("current", 300, 350, false)
	current.DayKey = 20270116
	current.GeneratedBytes = 80

	results := []jhlog.StreamResult{
		{LogGrowth: &jhlog.LogGrowthProjection{
			Generation:   4,
			CapturedAtMS: 200,
			Live:         &partial,
		}},
		{LogGrowth: &jhlog.LogGrowthProjection{
			Generation:   5,
			CapturedAtMS: 250,
			Sessions:     []jhlog.LogGrowthSession{completed},
			Days: []jhlog.LogGrowthDay{{
				DayKey:                completed.DayKey,
				SessionCount:          1,
				GeneratedBytes:        completed.GeneratedBytes,
				MaximumRetainedBytes:  completed.MaximumRetainedBytes,
				SessionsReachingLimit: 1,
				OverflowCount:         completed.OverflowCount,
				EvictedChunkCount:     completed.EvictedChunkCount,
				EvictedBytes:          completed.EvictedBytes,
			}},
		}},
		{LogGrowth: &jhlog.LogGrowthProjection{
			Generation:   4,
			CapturedAtMS: 350,
			Live:         &current,
		}},
	}

	for _, order := range permutationsOfThree() {
		ordered := []jhlog.StreamResult{results[order[0]], results[order[1]], results[order[2]]}
		summary := buildLogGrowthSummary(ordered)
		if summary.HistoryGeneration != 5 || len(summary.Sessions) != 2 || len(summary.Days) != 2 {
			t.Fatalf("order %v: summary = %+v", order, summary)
		}
		if !summary.Sessions[0].Completed || summary.Sessions[0].SessionID != completed.SessionID {
			t.Fatalf("order %v: completed session = %+v", order, summary.Sessions[0])
		}
		if summary.CurrentSession == nil || summary.CurrentSession.SessionID != current.SessionID {
			t.Fatalf("order %v: current session = %+v", order, summary.CurrentSession)
		}
		if summary.Days[0].SessionCount != 1 || summary.Days[0].GeneratedBytes != completed.GeneratedBytes {
			t.Fatalf("order %v: completed day = %+v", order, summary.Days[0])
		}
		if summary.Days[1].SessionCount != 1 || summary.Days[1].GeneratedBytes != current.GeneratedBytes {
			t.Fatalf("order %v: current day = %+v", order, summary.Days[1])
		}
	}
}

func TestBuildLogGrowthSummaryKeepsExactNewestDetailedSessionWindow(t *testing.T) {
	const extra = 7
	sessions := make([]jhlog.LogGrowthSession, maximumDetailedLogGrowthSessions+extra)
	for index := range sessions {
		sessions[index] = growthSession(strconv.Itoa(index), uint64(index), uint64(index+1), true)
	}
	summary := buildLogGrowthSummary([]jhlog.StreamResult{{LogGrowth: &jhlog.LogGrowthProjection{
		Generation: 1,
		Sessions:   sessions,
	}}})

	if len(summary.Sessions) != maximumDetailedLogGrowthSessions {
		t.Fatalf("session count = %d", len(summary.Sessions))
	}
	if summary.Sessions[0].SessionID != strconv.Itoa(extra) ||
		summary.Sessions[len(summary.Sessions)-1].SessionID != strconv.Itoa(len(sessions)-1) {
		t.Fatalf("retained range = %q..%q", summary.Sessions[0].SessionID, summary.Sessions[len(summary.Sessions)-1].SessionID)
	}
}

func permutationsOfThree() [][3]int {
	return [][3]int{{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0}}
}

func TestInspectSeveralFormat1LogsKeepsNewestSessionFactsWithoutDoubleCounting(t *testing.T) {
	first := growthSession("first", 100, 180, true)
	first.SessionID = "0000000000000001"
	first.DayKey = 20270114
	secondPartial := growthSession("second", 200, 240, false)
	secondPartial.SessionID = "0000000000000002"
	secondPartial.GeneratedBytes = 300
	secondFinal := secondPartial
	secondFinal.EndedAtMS = 280
	secondFinal.GeneratedBytes = 900
	secondFinal.MaximumRetainedBytes = 800
	secondFinal.OverflowCount = 2
	secondFinal.EvictedChunkCount = 5
	secondFinal.EvictedBytes = 400
	secondFinal.Completed = true
	current := growthSession("current", 400, 460, false)
	current.SessionID = "0000000000000003"
	current.DayKey = 20270116
	current.GeneratedBytes = 700
	current.OverflowCount = 3

	projections := []*jhlog.LogGrowthProjection{
		{
			Generation:   10,
			CapturedAtMS: 190,
			Sessions:     []jhlog.LogGrowthSession{first},
			Days: []jhlog.LogGrowthDay{{
				DayKey:         first.DayKey,
				SessionCount:   1,
				GeneratedBytes: first.GeneratedBytes,
			}},
			Live: &secondPartial,
		},
		{
			Generation:   11,
			CapturedAtMS: 300,
			Sessions:     []jhlog.LogGrowthSession{first, secondFinal},
			Days: []jhlog.LogGrowthDay{
				{DayKey: first.DayKey, SessionCount: 1, GeneratedBytes: first.GeneratedBytes},
				{DayKey: secondFinal.DayKey, SessionCount: 1, GeneratedBytes: secondFinal.GeneratedBytes, OverflowCount: 2},
			},
			Live: &current,
		},
		{
			Generation:   9,
			CapturedAtMS: 150,
			Sessions:     []jhlog.LogGrowthSession{first},
			Live:         &secondPartial,
		},
	}
	directory := t.TempDir()
	paths := make([]string, 0, len(projections))
	for index, projection := range projections {
		path := filepath.Join(directory, "growth-"+strconv.Itoa(index)+".jhlog")
		writeFormat1GrowthLog(t, path, projection)
		paths = append(paths, path)
	}

	summary, err := InspectFilesWithOptions("several format 1.0 logs", paths, Options{})
	if err != nil {
		t.Fatal(err)
	}
	growth := summary.LogGrowth
	if growth.HistoryGeneration != 11 || len(growth.Sessions) != 3 || len(growth.Days) != 3 {
		t.Fatalf("summary = %+v", summary)
	}
	if growth.Sessions[1].SessionID != secondFinal.SessionID || growth.Sessions[1].GeneratedBytes != 900 || !growth.Sessions[1].Completed {
		t.Fatalf("second session was not replaced with its final facts: %+v", growth.Sessions[1])
	}
	if growth.CurrentSession == nil || growth.CurrentSession.SessionID != current.SessionID {
		t.Fatalf("current session = %+v", growth.CurrentSession)
	}
	lastDay := growth.Days[len(growth.Days)-1]
	if lastDay.DayKey != current.DayKey || lastDay.SessionCount != 1 || lastDay.OverflowCount != 3 {
		t.Fatalf("current day = %+v", lastDay)
	}
}

const (
	testV1FileHeaderOffset  = 16
	testV1SuperblockAOffset = 8 * 1024
	testV1SuperblockBOffset = testV1SuperblockAOffset + testV1SuperblockBytes
	testV1LiveAOffset       = testV1SuperblockBOffset + testV1SuperblockBytes
	testV1HistoryOffset     = 12 * 1024
	testV1ArenaOffset       = 80 * 1024
	testV1SuperblockBytes   = 256
	testV1LiveBytes         = 256
	testV1HistoryHeader     = 64
	testV1HistorySession    = 96
	testV1HistoryDay        = 80
)

func writeFormat1GrowthLog(t *testing.T, path string, projection *jhlog.LogGrowthProjection) {
	t.Helper()
	const arenaCapacity = 512
	header := jhlog.DefaultSegmentHeader()
	header.ProcessName = "growth-test"
	var encoded bytes.Buffer
	if _, err := jhlog.NewWriterWithHeader(&encoded, header); err != nil {
		t.Fatal(err)
	}
	raw := make([]byte, testV1ArenaOffset+arenaCapacity)
	copy(raw, []byte{'J', 'H', 'L', 'O', 'G', '\r', '\n', jhlog.CurrentFormatMarker, jhlog.CurrentFormatMajor, jhlog.CurrentFormatMinor})
	copy(raw[testV1FileHeaderOffset:], encoded.Bytes()[len(jhlog.Magic):])
	copy(raw[testV1SuperblockAOffset:], marshalTestV1Superblock(1, arenaCapacity))
	copy(raw[testV1SuperblockBOffset:], marshalTestV1Superblock(2, arenaCapacity))
	copy(raw[testV1HistoryOffset:], marshalTestV1GrowthHistory(t, projection))
	if projection.Live != nil {
		copy(raw[testV1LiveAOffset:], marshalTestV1GrowthLive(t, projection.Generation, *projection.Live))
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func marshalTestV1Superblock(generation uint64, arenaCapacity int) []byte {
	raw := make([]byte, testV1SuperblockBytes)
	copy(raw, []byte{'J', 'H', 'S', 'B'})
	binary.LittleEndian.PutUint16(raw[4:6], 1)
	values := []uint64{
		generation,
		uint64(arenaCapacity),
		0,
		0,
		0,
		0,
		0,
		0,
		0,
		0,
		0,
		0,
		testV1ArenaOffset,
	}
	for index, value := range values {
		binary.LittleEndian.PutUint64(raw[8+index*8:], value)
	}
	binary.LittleEndian.PutUint32(raw[len(raw)-4:], crc32.ChecksumIEEE(raw[:len(raw)-4]))
	return raw
}

func marshalTestV1GrowthHistory(t *testing.T, projection *jhlog.LogGrowthProjection) []byte {
	t.Helper()
	total := testV1HistoryHeader + len(projection.Sessions)*testV1HistorySession + len(projection.Days)*testV1HistoryDay
	raw := make([]byte, total)
	copy(raw, []byte{'J', 'H', 'G', 'P'})
	raw[4] = jhlog.CurrentFormatMajor
	raw[5] = jhlog.CurrentFormatMinor
	binary.LittleEndian.PutUint16(raw[6:8], testV1HistoryHeader)
	binary.LittleEndian.PutUint32(raw[8:12], uint32(total))
	binary.LittleEndian.PutUint16(raw[12:14], testV1HistorySession)
	binary.LittleEndian.PutUint16(raw[14:16], testV1HistoryDay)
	binary.LittleEndian.PutUint32(raw[16:20], uint32(len(projection.Sessions)))
	binary.LittleEndian.PutUint32(raw[20:24], uint32(len(projection.Days)))
	binary.LittleEndian.PutUint64(raw[24:32], projection.Generation)
	binary.LittleEndian.PutUint64(raw[32:40], projection.CapturedAtMS)
	offset := testV1HistoryHeader
	for _, session := range projection.Sessions {
		id := parseTestGrowthSessionID(t, session.SessionID)
		record := raw[offset : offset+testV1HistorySession]
		binary.LittleEndian.PutUint64(record[0:8], id)
		binary.LittleEndian.PutUint32(record[8:12], session.DayKey)
		if session.Recovered {
			binary.LittleEndian.PutUint32(record[12:16], 1)
		}
		writeTestUint64s(record[16:], []uint64{
			session.StartedAtMS,
			session.EndedAtMS,
			session.ConfiguredLimitBytes,
			session.MaximumRetainedBytes,
			session.GeneratedBytes,
			session.OverflowCount,
			session.EvictedChunkCount,
			session.EvictedBytes,
			session.FirstOverflowAtMS,
			session.LastOverflowAtMS,
		})
		offset += testV1HistorySession
	}
	for _, day := range projection.Days {
		record := raw[offset : offset+testV1HistoryDay]
		binary.LittleEndian.PutUint32(record[0:4], day.DayKey)
		writeTestUint64s(record[8:], []uint64{
			day.SessionCount,
			day.TotalDurationMS,
			day.GeneratedBytes,
			day.MaximumRetainedBytes,
			day.MaximumFillPermille,
			day.SessionsReachingLimit,
			day.OverflowCount,
			day.EvictedChunkCount,
			day.EvictedBytes,
		})
		offset += testV1HistoryDay
	}
	binary.LittleEndian.PutUint32(raw[60:64], crc32.ChecksumIEEE(raw))
	return raw
}

func marshalTestV1GrowthLive(t *testing.T, generation uint64, session jhlog.LogGrowthSession) []byte {
	t.Helper()
	raw := make([]byte, testV1LiveBytes)
	copy(raw, []byte{'J', 'H', 'G', 'L'})
	raw[4] = jhlog.CurrentFormatMajor
	raw[5] = jhlog.CurrentFormatMinor
	binary.LittleEndian.PutUint16(raw[6:8], testV1LiveBytes)
	binary.LittleEndian.PutUint64(raw[8:16], generation)
	binary.LittleEndian.PutUint64(raw[16:24], parseTestGrowthSessionID(t, session.SessionID))
	binary.LittleEndian.PutUint32(raw[32:36], session.DayKey)
	if session.Completed {
		binary.LittleEndian.PutUint32(raw[36:40], 1)
	}
	writeTestUint64s(raw[40:], []uint64{
		session.StartedAtMS,
		session.EndedAtMS,
		session.ConfiguredLimitBytes,
		session.MaximumRetainedBytes,
		session.GeneratedBytes,
		session.OverflowCount,
		session.EvictedChunkCount,
		session.EvictedBytes,
		session.FirstOverflowAtMS,
		session.LastOverflowAtMS,
	})
	binary.LittleEndian.PutUint32(raw[len(raw)-4:], crc32.ChecksumIEEE(raw[:len(raw)-4]))
	return raw
}

func parseTestGrowthSessionID(t *testing.T, value string) uint64 {
	t.Helper()
	id, err := strconv.ParseUint(value, 16, 64)
	if err != nil {
		t.Fatalf("parse growth session ID %q: %v", value, err)
	}
	return id
}

func writeTestUint64s(target []byte, values []uint64) {
	for index, value := range values {
		binary.LittleEndian.PutUint64(target[index*8:], value)
	}
}

func growthSession(id string, started, ended uint64, completed bool) jhlog.LogGrowthSession {
	return jhlog.LogGrowthSession{
		SessionID:            id,
		DayKey:               20270115,
		StartedAtMS:          started,
		EndedAtMS:            ended,
		ConfiguredLimitBytes: 1_000,
		MaximumRetainedBytes: 500,
		GeneratedBytes:       100,
		Completed:            completed,
	}
}
