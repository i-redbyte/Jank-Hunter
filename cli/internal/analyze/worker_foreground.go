package analyze

import (
	"encoding/hex"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

// Foreground events belong only to a uniquely identified, observed attempt.
// The boundary is the end of our attempt observation, not an OS service exit.
func attachWorkerForeground(attempts []WorkerAttempt, events []workerRawEvent) uint64 {
	indices := workerAttemptIntervals(attempts, false)
	var unmatched uint64
	for _, event := range events {
		if event.stage != jhlog.WorkerStageForegroundEntered && event.stage != jhlog.WorkerStageForegroundAttemptBoundary {
			continue
		}
		key := attemptJoinKey{hex.EncodeToString([]byte(event.scope)), event.instanceID, event.runAttempt, event.generation}
		index := indices[key]
		if index == nil {
			unmatched++
			continue
		}
		position, unique := index.uniqueAt(event.timeMS)
		if !unique {
			unmatched++
			continue
		}
		attempt := &attempts[position]
		at := event.timeMS
		if event.stage == jhlog.WorkerStageForegroundEntered {
			if attempt.ForegroundEnteredMS == nil || at < *attempt.ForegroundEnteredMS {
				attempt.ForegroundEnteredMS = &at
			}
		} else if attempt.ForegroundBoundaryMS == nil || at > *attempt.ForegroundBoundaryMS {
			attempt.ForegroundBoundaryMS = &at
		}
	}
	return unmatched
}
