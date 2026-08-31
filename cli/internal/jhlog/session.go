package jhlog

import (
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	sessionLogFilenamePrefix = "jh-session-log."
	sessionLogFilenameSuffix = ".jhlog"
	sessionLogDateLayout     = "2006-01-02"
)

// SessionLogFilename is the ordering information carried by a canonical
// Android log filename. RunID lets storage and discovery group a complete
// multi-process launch without opening every log file.
type SessionLogFilename struct {
	Date              time.Time
	RunID             ID128
	DailySessionIndex uint64
	SegmentIndex      uint64
}

func validateSessionLogFilename(path string, header SegmentHeader) error {
	name, canonical := ParseSessionLogFilename(path)
	if canonical && name.RunID != header.RunID {
		return fmt.Errorf("%s: canonical filename run ID does not match JHLOG %s header", path, FormatVersionString)
	}
	return nil
}

// ParseSessionLogFilename accepts only the canonical Android filename:
// jh-session-log.YYYY-MM-DD.<lowercase 128-bit run ID>.<daily index>[-<segment index>].jhlog.
func ParseSessionLogFilename(path string) (SessionLogFilename, bool) {
	base := filepath.Base(path)
	if !strings.HasPrefix(base, sessionLogFilenamePrefix) || !strings.HasSuffix(base, sessionLogFilenameSuffix) {
		return SessionLogFilename{}, false
	}
	body := strings.TrimSuffix(strings.TrimPrefix(base, sessionLogFilenamePrefix), sessionLogFilenameSuffix)
	const dateBytes = len("2006-01-02")
	const runIDHexBytes = len(ID128{}) * 2
	if len(body) <= dateBytes+1+runIDHexBytes+1 || body[dateBytes] != '.' || body[dateBytes+1+runIDHexBytes] != '.' {
		return SessionLogFilename{}, false
	}
	dateText := body[:dateBytes]
	runIDText := body[dateBytes+1 : dateBytes+1+runIDHexBytes]
	indexText := body[dateBytes+1+runIDHexBytes+1:]
	date, err := time.Parse(sessionLogDateLayout, dateText)
	if err != nil || date.Format(sessionLogDateLayout) != dateText {
		return SessionLogFilename{}, false
	}
	dailyIndexText, segmentIndexText, hasSegment := strings.Cut(indexText, "-")
	if hasSegment && strings.Contains(segmentIndexText, "-") {
		return SessionLogFilename{}, false
	}
	dailySessionIndex, ok := parseCanonicalIndex(dailyIndexText)
	if !ok {
		return SessionLogFilename{}, false
	}
	var segmentIndex uint64
	if hasSegment {
		segmentIndex, ok = parseCanonicalIndex(segmentIndexText)
		if !ok || segmentIndex == 0 {
			return SessionLogFilename{}, false
		}
	}
	var runID ID128
	decoded, err := hex.Decode(runID[:], []byte(runIDText))
	if err != nil || decoded != len(runID) || runID.IsZero() || hex.EncodeToString(runID[:]) != runIDText {
		return SessionLogFilename{}, false
	}
	return SessionLogFilename{
		Date:              date,
		RunID:             runID,
		DailySessionIndex: dailySessionIndex,
		SegmentIndex:      segmentIndex,
	}, true
}

func parseCanonicalIndex(value string) (uint64, bool) {
	if value == "" || len(value) > 1 && value[0] == '0' {
		return 0, false
	}
	index, err := strconv.ParseUint(value, 10, 64)
	return index, err == nil
}

// CompareSession orders application runs without letting their segment counts affect recency.
func (name SessionLogFilename) CompareSession(other SessionLogFilename) int {
	if name.Date.Before(other.Date) {
		return -1
	}
	if name.Date.After(other.Date) {
		return 1
	}
	switch {
	case name.DailySessionIndex < other.DailySessionIndex:
		return -1
	case name.DailySessionIndex > other.DailySessionIndex:
		return 1
	default:
		return 0
	}
}

// Compare orders filenames by calendar date, daily session, and physical segment.
func (name SessionLogFilename) Compare(other SessionLogFilename) int {
	if comparison := name.CompareSession(other); comparison != 0 {
		return comparison
	}
	switch {
	case name.SegmentIndex < other.SegmentIndex:
		return -1
	case name.SegmentIndex > other.SegmentIndex:
		return 1
	default:
		return 0
	}
}
