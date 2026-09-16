package analyze

import (
	"math"
	"math/bits"
	"sort"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

type ioAggregate struct {
	stats     IOStats
	durations uint64SampleSet
	burst     ioBurstAccumulator
}

func (a *ioAggregate) add(event *jhlog.IOEvent, flags uint64, logIndex, endMS uint64) {
	a.stats.Count++
	a.stats.TotalDurationUS = saturatingUint64Sum(a.stats.TotalDurationUS, event.DurationUS)
	a.stats.MaxDurationUS = maxUint64(a.stats.MaxDurationUS, event.DurationUS)
	a.durations.add(event.DurationUS)
	a.burst.add(logIndex, endMS)
	if event.Outcome == jhlog.IOOutcomeFailure {
		a.stats.Failures++
	}
	if flags&uint64(jhlog.FlagIOBytesKnown) != 0 {
		a.stats.KnownByteOperations++
		a.stats.KnownByteDurationUS = saturatingUint64Sum(a.stats.KnownByteDurationUS, event.DurationUS)
		a.stats.Bytes = saturatingUint64Sum(a.stats.Bytes, event.Bytes)
		a.stats.MaxBytes = maxUint64(a.stats.MaxBytes, event.Bytes)
	}
}

func (a *ioAggregate) finalize() IOStats {
	a.stats.P50DurationUS = a.durations.percentile(0.50)
	a.stats.P95DurationUS = a.durations.percentile(0.95)
	a.stats.BytesPerSecond = bytesPerSecond(a.stats.Bytes, a.stats.KnownByteDurationUS)
	a.stats.PeakOperationsPerSecond = a.burst.peak
	a.stats.PeakWindowStartMS = a.burst.peakWindowStartMS
	a.stats.BurstEstimateStatus = a.burst.status()
	return a.stats
}

type ioAnalysisAccumulator struct {
	operations           uint64
	failures             uint64
	mainThreadOperations uint64
	mainThreadDurationUS uint64
	syncOperations       uint64
	knownByteOperations  uint64
	knownByteDurationUS  uint64
	bytes                uint64
	totalDurationUS      uint64
	durations            uint64SampleSet
	intervals            []ioInterval
}

func (a *ioAnalysisAccumulator) add(
	event *jhlog.IOEvent,
	flags uint64,
	logIndex uint64,
	endUS uint64,
) {
	a.operations++
	a.totalDurationUS = saturatingUint64Sum(a.totalDurationUS, event.DurationUS)
	a.durations.add(event.DurationUS)
	if event.Outcome == jhlog.IOOutcomeFailure {
		a.failures++
	}
	if flags&uint64(jhlog.FlagThreadMain) != 0 {
		a.mainThreadOperations++
		a.mainThreadDurationUS = saturatingUint64Sum(a.mainThreadDurationUS, event.DurationUS)
	}
	if event.Operation == jhlog.IOOperationFileSync {
		a.syncOperations++
	}
	if flags&uint64(jhlog.FlagIOBytesKnown) != 0 {
		a.knownByteOperations++
		a.knownByteDurationUS = saturatingUint64Sum(a.knownByteDurationUS, event.DurationUS)
		a.bytes = saturatingUint64Sum(a.bytes, event.Bytes)
	}
	startUS := uint64(0)
	if endUS > event.DurationUS {
		startUS = endUS - event.DurationUS
	}
	a.intervals = append(a.intervals, ioInterval{logIndex: logIndex, startUS: startUS, endUS: endUS})
}

func (a *ioAnalysisAccumulator) finalize(calls []IOStats) *IOAnalysis {
	if a.operations == 0 {
		return nil
	}
	var burst routeBurstAccumulator
	maxConcurrency, peakConcurrencyAtUS := scanIOIntervals(a.intervals, &burst)
	sources := make(map[string]struct{}, len(calls))
	for _, call := range calls {
		if !isUnknownAnalysisValue(call.Source) {
			sources[call.Source] = struct{}{}
		}
	}
	return &IOAnalysis{
		Operations: a.operations, Failures: a.failures,
		MainThreadOperations: a.mainThreadOperations, MainThreadDurationUS: a.mainThreadDurationUS,
		SyncOperations: a.syncOperations, KnownByteOperations: a.knownByteOperations,
		KnownByteDurationUS: a.knownByteDurationUS, Bytes: a.bytes, TotalDurationUS: a.totalDurationUS,
		P50DurationUS: a.durations.percentile(0.50), P95DurationUS: a.durations.percentile(0.95),
		MaxDurationUS: a.durations.max, BytesPerSecond: bytesPerSecond(a.bytes, a.knownByteDurationUS),
		PeakOperationsPerSecond: burst.peak, PeakWindowStartMS: burst.peakWindowStartMS,
		BurstEstimateStatus: burst.status(),
		MaxConcurrency:      maxConcurrency, PeakConcurrencyAtMS: peakConcurrencyAtUS / 1_000,
		SourceCount: len(sources), Calls: calls,
	}
}

type ioInterval struct {
	logIndex uint64
	startUS  uint64
	endUS    uint64
}

func maxIOConcurrency(intervals []ioInterval) (uint64, uint64) {
	return scanIOIntervals(intervals, nil)
}

// Start-sorted intervals let the existing end-time heap emit completions in order:
// every future interval ends no earlier than its start. This reuses stored intervals
// for an exact rolling peak without a second sort or an extra timestamp array.
func scanIOIntervals(intervals []ioInterval, completions *routeBurstAccumulator) (uint64, uint64) {
	if len(intervals) == 0 {
		return 0, 0
	}
	sort.Slice(intervals, func(i, j int) bool {
		if intervals[i].logIndex != intervals[j].logIndex {
			return intervals[i].logIndex < intervals[j].logIndex
		}
		if intervals[i].startUS != intervals[j].startUS {
			return intervals[i].startUS < intervals[j].startUS
		}
		return intervals[i].endUS < intervals[j].endUS
	})
	ends := make([]uint64, 0, min(len(intervals), 64))
	currentLog := intervals[0].logIndex
	var peak uint64
	var peakAtUS uint64
	for _, interval := range intervals {
		if interval.logIndex != currentLog {
			if completions != nil {
				for len(ends) > 0 {
					completions.add(currentLog, ends[0]/1_000)
					ends = popUint64MinHeap(ends)
				}
			}
			ends = ends[:0]
			currentLog = interval.logIndex
		}
		for len(ends) > 0 && ends[0] <= interval.startUS {
			if completions != nil {
				completions.add(currentLog, ends[0]/1_000)
			}
			ends = popUint64MinHeap(ends)
		}
		if interval.endUS == interval.startUS {
			if completions != nil {
				completions.add(currentLog, interval.endUS/1_000)
			}
			continue
		}
		ends = pushUint64MinHeap(ends, interval.endUS)
		if uint64(len(ends)) > peak {
			peak = uint64(len(ends))
			peakAtUS = interval.startUS
		}
	}
	if completions != nil {
		for len(ends) > 0 {
			completions.add(currentLog, ends[0]/1_000)
			ends = popUint64MinHeap(ends)
		}
	}
	return peak, peakAtUS
}

type ioBurstAccumulator = routeBurstAccumulator

func bytesPerSecond(bytes, durationUS uint64) uint64 {
	if bytes == 0 || durationUS == 0 {
		return 0
	}
	high, low := bits.Mul64(bytes, 1_000_000)
	if high >= durationUS {
		return math.MaxUint64
	}
	quotient, _ := bits.Div64(high, low, durationUS)
	return quotient
}

func ioEventEndUS(event jhlog.Event) uint64 {
	if event.TimeUS != 0 || event.TimeMS == 0 {
		return event.TimeUS
	}
	if event.TimeMS > math.MaxUint64/1_000 {
		return math.MaxUint64
	}
	return event.TimeMS * 1_000
}

func isUnknownAnalysisValue(value string) bool {
	return value == "" || value == "unknown"
}
