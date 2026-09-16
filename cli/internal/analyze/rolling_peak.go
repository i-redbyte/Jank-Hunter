package analyze

// On the recorded millisecond clock, a half-open second contains at most 1000
// distinct timestamps. Equal-time events share a bin, independently of their rate.
const rollingSecondBins = 1000

type routeBurstAccumulator struct {
	buckets                 []uint64Frequency
	head, size              int
	windowCount             uint64
	logIndex, lastTimeMS    uint64
	peak, peakWindowStartMS uint64
	hasLog, approximate     bool
}

func (s *routeBurstAccumulator) add(logIndex, timeMS uint64) {
	if !s.hasLog || logIndex != s.logIndex || timeMS < s.lastTimeMS {
		if s.hasLog && (logIndex < s.logIndex || logIndex == s.logIndex && timeMS < s.lastTimeMS) {
			// Expired history cannot reconstruct an arbitrary late event's windows.
			// Each retained monotone block still proves a lower bound on the peak.
			s.approximate = true
		}
		s.head, s.size, s.windowCount = 0, 0, 0
		s.logIndex, s.hasLog = logIndex, true
	}
	s.lastTimeMS = timeMS
	for s.size > 0 && timeMS-s.buckets[s.head].value >= 1000 {
		if s.buckets[s.head].count > s.windowCount {
			s.windowCount = 0
			s.approximate = true
		} else {
			s.windowCount -= s.buckets[s.head].count
		}
		s.head = (s.head + 1) % len(s.buckets)
		s.size--
	}
	last := 0
	if s.size > 0 {
		last = (s.head + s.size - 1) % len(s.buckets)
	}
	if s.size > 0 && s.buckets[last].value == timeMS {
		s.buckets[last].count = saturatingUint64Sum(s.buckets[last].count, 1)
	} else {
		if s.size == len(s.buckets) {
			capacity := min(rollingSecondBins, max(1, len(s.buckets)*2))
			next := make([]uint64Frequency, capacity)
			for i := 0; i < s.size; i++ {
				next[i] = s.buckets[(s.head+i)%len(s.buckets)]
			}
			s.buckets, s.head = next, 0
		}
		s.buckets[(s.head+s.size)%len(s.buckets)] = uint64Frequency{value: timeMS, count: 1}
		s.size++
	}
	if s.windowCount == ^uint64(0) {
		s.approximate = true
	} else {
		s.windowCount++
	}
	if s.windowCount > s.peak {
		s.peak = s.windowCount
		s.peakWindowStartMS = s.buckets[s.head].value
	}
}

func (s *routeBurstAccumulator) status() string {
	return s.statusWithMissing(false)
}

func (s *routeBurstAccumulator) statusWithMissing(missing bool) string {
	if !s.hasLog {
		return "unknown"
	}
	if s.approximate || missing {
		return "lower_bound_rolling_second"
	}
	return "exact_rolling_second"
}
