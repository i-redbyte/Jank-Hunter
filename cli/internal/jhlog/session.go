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
	Date  time.Time
	RunID ID128
	Index uint64
}

func validateSessionLogFilename(path string, header SegmentHeader) error {
	name, canonical := ParseSessionLogFilename(path)
	if canonical && name.RunID != header.RunID {
		return fmt.Errorf("%s: canonical filename run ID does not match JHLOG 2.0.0 header", path)
	}
	return nil
}

// ParseSessionLogFilename accepts only the canonical Android filename:
// jh-session-log.YYYY-MM-DD.<lowercase 128-bit run ID>.<canonical index>.jhlog.
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
	index, err := strconv.ParseUint(indexText, 10, 64)
	if err != nil || strconv.FormatUint(index, 10) != indexText {
		return SessionLogFilename{}, false
	}
	var runID ID128
	decoded, err := hex.Decode(runID[:], []byte(runIDText))
	if err != nil || decoded != len(runID) || runID.IsZero() || hex.EncodeToString(runID[:]) != runIDText {
		return SessionLogFilename{}, false
	}
	return SessionLogFilename{Date: date, RunID: runID, Index: index}, true
}

// Compare orders filenames by calendar date and then by their numeric index.
func (name SessionLogFilename) Compare(other SessionLogFilename) int {
	if name.Date.Before(other.Date) {
		return -1
	}
	if name.Date.After(other.Date) {
		return 1
	}
	switch {
	case name.Index < other.Index:
		return -1
	case name.Index > other.Index:
		return 1
	default:
		return 0
	}
}
