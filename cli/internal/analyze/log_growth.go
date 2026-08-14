package analyze

import (
	"sort"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

const maximumDetailedLogGrowthSessions = 1_024

func buildLogGrowthSummary(results []jhlog.StreamResult) LogGrowthSummary {
	var baseline *jhlog.LogGrowthProjection
	allSessions := map[string]jhlog.LogGrowthSession{}
	liveSessions := map[string]jhlog.LogGrowthSession{}
	for _, result := range results {
		projection := result.LogGrowth
		if projection == nil {
			continue
		}
		if baseline == nil || newerGrowthProjection(projection, baseline) {
			baseline = projection
		}
		for _, session := range projection.Sessions {
			mergeGrowthSession(allSessions, session)
		}
		if projection.Live != nil {
			mergeGrowthSession(allSessions, *projection.Live)
			mergeGrowthSession(liveSessions, *projection.Live)
		}
	}
	if baseline == nil {
		return LogGrowthSummary{}
	}

	baselineSessions := make(map[string]struct{}, len(baseline.Sessions))
	for _, session := range baseline.Sessions {
		baselineSessions[session.SessionID] = struct{}{}
	}
	days := make(map[uint32]jhlog.LogGrowthDay, len(baseline.Days))
	for _, day := range baseline.Days {
		days[day.DayKey] = day
	}
	for _, session := range liveSessions {
		if _, included := baselineSessions[session.SessionID]; included {
			continue
		}
		if baseline.CapturedAtMS > 0 && session.StartedAtMS < baseline.CapturedAtMS {
			continue
		}
		day := days[session.DayKey]
		days[session.DayKey] = addSessionToGrowthDay(day, session)
	}

	sessions := make([]jhlog.LogGrowthSession, 0, len(allSessions))
	var current *jhlog.LogGrowthSession
	for _, session := range allSessions {
		sessions = append(sessions, session)
		if !session.Completed && (current == nil || session.StartedAtMS > current.StartedAtMS) {
			copy := session
			current = &copy
		}
	}
	sort.Slice(sessions, func(i, j int) bool {
		if sessions[i].StartedAtMS == sessions[j].StartedAtMS {
			return sessions[i].SessionID < sessions[j].SessionID
		}
		return sessions[i].StartedAtMS < sessions[j].StartedAtMS
	})
	if len(sessions) > maximumDetailedLogGrowthSessions {
		sessions = append([]jhlog.LogGrowthSession(nil), sessions[len(sessions)-maximumDetailedLogGrowthSessions:]...)
	}

	dayRows := make([]jhlog.LogGrowthDay, 0, len(days))
	for _, day := range days {
		dayRows = append(dayRows, day)
	}
	sort.Slice(dayRows, func(i, j int) bool { return dayRows[i].DayKey < dayRows[j].DayKey })
	return LogGrowthSummary{
		Available:         true,
		HistoryGeneration: baseline.Generation,
		CapturedAtMS:      baseline.CapturedAtMS,
		Sessions:          sessions,
		Days:              dayRows,
		CurrentSession:    current,
	}
}

func newerGrowthProjection(candidate, current *jhlog.LogGrowthProjection) bool {
	if candidate.Generation != current.Generation {
		return candidate.Generation > current.Generation
	}
	return candidate.CapturedAtMS > current.CapturedAtMS
}

func mergeGrowthSession(target map[string]jhlog.LogGrowthSession, candidate jhlog.LogGrowthSession) {
	current, exists := target[candidate.SessionID]
	if !exists || newerGrowthSession(candidate, current) {
		target[candidate.SessionID] = candidate
	}
}

func newerGrowthSession(candidate, current jhlog.LogGrowthSession) bool {
	if candidate.GeneratedBytes != current.GeneratedBytes {
		return candidate.GeneratedBytes > current.GeneratedBytes
	}
	if candidate.EndedAtMS != current.EndedAtMS {
		return candidate.EndedAtMS > current.EndedAtMS
	}
	if candidate.Completed != current.Completed {
		return candidate.Completed
	}
	if candidate.OverflowCount != current.OverflowCount {
		return candidate.OverflowCount > current.OverflowCount
	}
	if candidate.EvictedChunkCount != current.EvictedChunkCount {
		return candidate.EvictedChunkCount > current.EvictedChunkCount
	}
	if candidate.EvictedBytes != current.EvictedBytes {
		return candidate.EvictedBytes > current.EvictedBytes
	}
	if candidate.MaximumRetainedBytes != current.MaximumRetainedBytes {
		return candidate.MaximumRetainedBytes > current.MaximumRetainedBytes
	}
	if candidate.LastOverflowAtMS != current.LastOverflowAtMS {
		return candidate.LastOverflowAtMS > current.LastOverflowAtMS
	}
	return candidate.Recovered && !current.Recovered
}

func addSessionToGrowthDay(day jhlog.LogGrowthDay, session jhlog.LogGrowthSession) jhlog.LogGrowthDay {
	day.DayKey = session.DayKey
	day.SessionCount = saturatingUint64Sum(day.SessionCount, 1)
	if session.EndedAtMS >= session.StartedAtMS {
		day.TotalDurationMS = saturatingUint64Sum(day.TotalDurationMS, session.EndedAtMS-session.StartedAtMS)
	}
	day.GeneratedBytes = saturatingUint64Sum(day.GeneratedBytes, session.GeneratedBytes)
	day.MaximumRetainedBytes = maxUint64(day.MaximumRetainedBytes, session.MaximumRetainedBytes)
	if session.ConfiguredLimitBytes > 0 {
		fill := saturatingMultiply(session.MaximumRetainedBytes, 1_000) / session.ConfiguredLimitBytes
		day.MaximumFillPermille = maxUint64(day.MaximumFillPermille, fill)
	}
	if session.OverflowCount > 0 {
		day.SessionsReachingLimit = saturatingUint64Sum(day.SessionsReachingLimit, 1)
	}
	day.OverflowCount = saturatingUint64Sum(day.OverflowCount, session.OverflowCount)
	day.EvictedChunkCount = saturatingUint64Sum(day.EvictedChunkCount, session.EvictedChunkCount)
	day.EvictedBytes = saturatingUint64Sum(day.EvictedBytes, session.EvictedBytes)
	return day
}

func saturatingMultiply(value, multiplier uint64) uint64 {
	if value == 0 || multiplier == 0 {
		return 0
	}
	if value > ^uint64(0)/multiplier {
		return ^uint64(0)
	}
	return value * multiplier
}
