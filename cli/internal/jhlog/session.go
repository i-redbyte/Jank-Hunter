package jhlog

import (
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
// Android log filename. Session and process identity deliberately do not live
// here; callers must read them from the bounded v9 file header.
type SessionLogFilename struct {
	Date  time.Time
	Index uint64
}

// ParseSessionLogFilename accepts only the canonical Android filename:
// jh-session-log.YYYY-MM-DD.<canonical non-negative decimal index>.jhlog.
func ParseSessionLogFilename(path string) (SessionLogFilename, bool) {
	base := filepath.Base(path)
	if !strings.HasPrefix(base, sessionLogFilenamePrefix) || !strings.HasSuffix(base, sessionLogFilenameSuffix) {
		return SessionLogFilename{}, false
	}
	body := strings.TrimSuffix(strings.TrimPrefix(base, sessionLogFilenamePrefix), sessionLogFilenameSuffix)
	dot := strings.LastIndexByte(body, '.')
	if dot <= 0 || dot == len(body)-1 {
		return SessionLogFilename{}, false
	}
	dateText, indexText := body[:dot], body[dot+1:]
	date, err := time.Parse(sessionLogDateLayout, dateText)
	if err != nil || date.Format(sessionLogDateLayout) != dateText {
		return SessionLogFilename{}, false
	}
	index, err := strconv.ParseUint(indexText, 10, 64)
	if err != nil || strconv.FormatUint(index, 10) != indexText {
		return SessionLogFilename{}, false
	}
	return SessionLogFilename{Date: date, Index: index}, true
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
