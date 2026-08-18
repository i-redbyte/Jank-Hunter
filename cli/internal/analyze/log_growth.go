package analyze

import (
	"fmt"
	"sort"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func buildLogGrowthSummary(results []jhlog.StreamResult) LogGrowthSummary {
	baselines := map[string]*jhlog.LogGrowthProjection{}
	hasProjection := false
	allSessions := map[string]jhlog.LogGrowthSession{}
	liveSessions := map[string]processGrowthSession{}
	freshnessByProcess := map[string]*processGrowthFreshness{}
	for _, result := range results {
		process := result.Header.ProcessName
		freshness := freshnessByProcess[process]
		if freshness == nil {
			freshness = &processGrowthFreshness{sessionIDs: map[string]struct{}{}}
			freshnessByProcess[process] = freshness
		}
		freshness.inputBytes = saturatingUint64Sum(freshness.inputBytes, result.InputBytes)
		freshness.latestDataEventMS = maxUint64(freshness.latestDataEventMS, result.LatestDataEventUnixMS)
		freshness.sessionIDs[string(result.Header.SessionID[:])] = struct{}{}
		projection := result.LogGrowth
		if projection == nil {
			continue
		}
		hasProjection = true
		baseline := baselines[process]
		if hasGrowthHistory(projection) && (baseline == nil || newerGrowthProjection(projection, baseline)) {
			baselines[process] = projection
		}
		for _, session := range projection.Sessions {
			mergeGrowthSession(allSessions, session)
		}
		if projection.Live != nil {
			if freshness.live == nil || newerGrowthSession(*projection.Live, *freshness.live) {
				copy := *projection.Live
				freshness.live = &copy
			}
			mergeGrowthSession(allSessions, *projection.Live)
			current, exists := liveSessions[projection.Live.SessionID]
			if !exists || newerGrowthSession(*projection.Live, current.session) {
				liveSessions[projection.Live.SessionID] = processGrowthSession{
					process: process,
					session: *projection.Live,
				}
			}
		}
	}
	if !hasProjection {
		return LogGrowthSummary{}
	}

	baselineSessions := map[string]struct{}{}
	days := map[uint32]jhlog.LogGrowthDay{}
	var capturedAtMS uint64
	var historyGeneration uint64
	for _, baseline := range baselines {
		for _, session := range baseline.Sessions {
			baselineSessions[session.SessionID] = struct{}{}
		}
		for _, day := range baseline.Days {
			days[day.DayKey] = mergeGrowthDay(days[day.DayKey], day)
		}
		capturedAtMS = maxUint64(capturedAtMS, baseline.CapturedAtMS)
		historyGeneration = maxUint64(historyGeneration, baseline.Generation)
	}
	for _, live := range liveSessions {
		session := live.session
		if _, included := baselineSessions[session.SessionID]; included {
			continue
		}
		baseline := baselines[live.process]
		if baseline != nil && baseline.CapturedAtMS > 0 && session.StartedAtMS < baseline.CapturedAtMS {
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
	dayRows := make([]jhlog.LogGrowthDay, 0, len(days))
	for _, day := range days {
		dayRows = append(dayRows, day)
	}
	sort.Slice(dayRows, func(i, j int) bool { return dayRows[i].DayKey < dayRows[j].DayKey })
	return LogGrowthSummary{
		Available:         true,
		HistoryGeneration: historyGeneration,
		CapturedAtMS:      capturedAtMS,
		Sessions:          sessions,
		Days:              dayRows,
		CurrentSession:    current,
		LiveCapturedAtMS:  growthLiveCapturedAt(freshnessByProcess),
		LatestDataEventMS: growthLatestDataEventAt(freshnessByProcess),
		InputBytes:        growthInputBytes(freshnessByProcess),
		SnapshotLagMS:     growthSnapshotLagMS(freshnessByProcess),
		SnapshotLagBytes:  growthSnapshotLagBytes(freshnessByProcess),
		FreshnessStatus:   growthFreshnessStatus(freshnessByProcess),
		FreshnessReason:   growthFreshnessReason(freshnessByProcess),
	}
}

type processGrowthSession struct {
	process string
	session jhlog.LogGrowthSession
}

type processGrowthFreshness struct {
	sessionIDs        map[string]struct{}
	inputBytes        uint64
	latestDataEventMS uint64
	live              *jhlog.LogGrowthSession
}

const growthFreshnessTimeToleranceMS = 5_000

func growthLiveCapturedAt(processes map[string]*processGrowthFreshness) uint64 {
	var latest uint64
	for _, process := range processes {
		if process.live != nil {
			latest = maxUint64(latest, process.live.EndedAtMS)
		}
	}
	return latest
}

func growthLatestDataEventAt(processes map[string]*processGrowthFreshness) uint64 {
	var latest uint64
	for _, process := range processes {
		latest = maxUint64(latest, process.latestDataEventMS)
	}
	return latest
}

func growthInputBytes(processes map[string]*processGrowthFreshness) uint64 {
	var total uint64
	for _, process := range processes {
		total = saturatingUint64Sum(total, process.inputBytes)
	}
	return total
}

func growthSnapshotLagMS(processes map[string]*processGrowthFreshness) uint64 {
	var lag uint64
	for _, process := range processes {
		if process.live != nil && process.latestDataEventMS > process.live.EndedAtMS {
			lag = maxUint64(lag, process.latestDataEventMS-process.live.EndedAtMS)
		}
	}
	return lag
}

func growthSnapshotLagBytes(processes map[string]*processGrowthFreshness) uint64 {
	var lag uint64
	for _, process := range processes {
		if len(process.sessionIDs) == 1 && process.live != nil && process.inputBytes > process.live.GeneratedBytes {
			lag = saturatingUint64Sum(lag, process.inputBytes-process.live.GeneratedBytes)
		}
	}
	return lag
}

func growthFreshnessStatus(processes map[string]*processGrowthFreshness) string {
	if len(processes) == 0 {
		return "unknown"
	}
	for _, process := range processes {
		if process.live == nil || len(process.sessionIDs) != 1 {
			return "unknown"
		}
		if process.latestDataEventMS > process.live.EndedAtMS &&
			process.latestDataEventMS-process.live.EndedAtMS > growthFreshnessTimeToleranceMS {
			return "stale"
		}
		byteTolerance := maxUint64(64*1024, process.live.GeneratedBytes/20)
		if process.inputBytes > process.live.GeneratedBytes &&
			process.inputBytes-process.live.GeneratedBytes > byteTolerance {
			return "stale"
		}
	}
	return "fresh"
}

func growthFreshnessReason(processes map[string]*processGrowthFreshness) string {
	status := growthFreshnessStatus(processes)
	lagMS := growthSnapshotLagMS(processes)
	lagBytes := growthSnapshotLagBytes(processes)
	switch status {
	case "fresh":
		return fmt.Sprintf("growth checkpoint согласован с последними committed events; lag=%d мс, физический хвост=%d байт", lagMS, lagBytes)
	case "stale":
		return fmt.Sprintf("после growth checkpoint есть committed events или существенный физический хвост; lag=%d мс, хвост=%d байт", lagMS, lagBytes)
	default:
		return "freshness нельзя доказать: отсутствует live checkpoint или вход содержит несколько session одного процесса"
	}
}

func hasGrowthHistory(projection *jhlog.LogGrowthProjection) bool {
	return projection != nil && (projection.HasHistory || len(projection.Sessions) > 0 || len(projection.Days) > 0)
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
	if candidate.LimitReachedCount != current.LimitReachedCount {
		return candidate.LimitReachedCount > current.LimitReachedCount
	}
	if candidate.SegmentRotationCount != current.SegmentRotationCount {
		return candidate.SegmentRotationCount > current.SegmentRotationCount
	}
	if candidate.ArchiveEvictedBytes != current.ArchiveEvictedBytes {
		return candidate.ArchiveEvictedBytes > current.ArchiveEvictedBytes
	}
	if candidate.MaximumRetainedBytes != current.MaximumRetainedBytes {
		return candidate.MaximumRetainedBytes > current.MaximumRetainedBytes
	}
	if candidate.LastLimitReachedMS != current.LastLimitReachedMS {
		return candidate.LastLimitReachedMS > current.LastLimitReachedMS
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
	if session.LimitReachedCount > 0 {
		day.SessionsReachingLimit = saturatingUint64Sum(day.SessionsReachingLimit, 1)
	}
	day.LimitReachedCount = saturatingUint64Sum(day.LimitReachedCount, session.LimitReachedCount)
	day.SegmentRotationCount = saturatingUint64Sum(day.SegmentRotationCount, session.SegmentRotationCount)
	day.ArchiveEvictedBytes = saturatingUint64Sum(day.ArchiveEvictedBytes, session.ArchiveEvictedBytes)
	return day
}

func mergeGrowthDay(current, candidate jhlog.LogGrowthDay) jhlog.LogGrowthDay {
	current.DayKey = candidate.DayKey
	current.SessionCount = saturatingUint64Sum(current.SessionCount, candidate.SessionCount)
	current.TotalDurationMS = saturatingUint64Sum(current.TotalDurationMS, candidate.TotalDurationMS)
	current.GeneratedBytes = saturatingUint64Sum(current.GeneratedBytes, candidate.GeneratedBytes)
	current.MaximumRetainedBytes = maxUint64(current.MaximumRetainedBytes, candidate.MaximumRetainedBytes)
	current.MaximumFillPermille = maxUint64(current.MaximumFillPermille, candidate.MaximumFillPermille)
	current.SessionsReachingLimit = saturatingUint64Sum(
		current.SessionsReachingLimit,
		candidate.SessionsReachingLimit,
	)
	current.LimitReachedCount = saturatingUint64Sum(current.LimitReachedCount, candidate.LimitReachedCount)
	current.SegmentRotationCount = saturatingUint64Sum(
		current.SegmentRotationCount,
		candidate.SegmentRotationCount,
	)
	current.ArchiveEvictedBytes = saturatingUint64Sum(
		current.ArchiveEvictedBytes,
		candidate.ArchiveEvictedBytes,
	)
	return current
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
