package main

import (
	"encoding/hex"
	"fmt"
	"path/filepath"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func sessionSelectionPath(dir, date string, sessionByte byte, dailyIndex, segmentIndex uint64) string {
	var runID jhlog.ID128
	runID[0] = sessionByte
	name := fmt.Sprintf(
		"jh-session-log.%s.%s.%d",
		date,
		hex.EncodeToString(runID[:]),
		dailyIndex,
	)
	if segmentIndex > 0 {
		name += fmt.Sprintf("-%d", segmentIndex)
	}
	return filepath.Join(dir, name+".jhlog")
}
