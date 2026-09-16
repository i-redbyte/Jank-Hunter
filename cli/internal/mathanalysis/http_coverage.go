package mathanalysis

import (
	"bytes"
	"fmt"
	"sort"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

const (
	CountObserved    = "observed"
	CountMissing     = "missing"
	CountUnsupported = "unsupported"
)

// Count states describe complete bucket counts of SDK HTTP completions. Raw
// received events remain available even when their complete count is unknown.
func httpCountPresent(b TimelineBucket) bool {
	return b.HTTPCountState == CountObserved || (b.HTTPCountState == "" && b.HTTPCount > 0)
}

type httpEpochKey struct {
	run, process, session jhlog.ID128
	start                 uint64
	fallback              string
}
type httpCoverageEpoch struct {
	source                           string
	start, end                       uint64
	windowStartMS, windowEndMS       uint64
	known, enabled, invalid, started bool
	previous                         jhlog.StreamResult
}
type httpCoverageCollector struct {
	account *collectionAccount
	epochs  map[httpEpochKey]*httpCoverageEpoch
	current *httpCoverageEpoch
}

func (c *httpCoverageCollector) begin(path string, h jhlog.SegmentHeader) {
	key := httpEpochKey{run: h.RunID, process: h.ProcessInstanceID, session: h.SessionID, start: h.CollectorStartElapsedUS}
	source := fmt.Sprintf("%x/%x", h.RunID, h.ProcessInstanceID)
	if h.RunID.IsZero() || h.ProcessInstanceID.IsZero() || h.SessionID.IsZero() {
		key.fallback = path
		source = path
	}
	if c.epochs == nil {
		c.epochs = make(map[httpEpochKey]*httpCoverageEpoch)
	}
	epoch := c.epochs[key]
	if epoch == nil {
		if !c.account.reserve(2048 + uint64(len(source)+len(path))) {
			return
		}
		epoch = &httpCoverageEpoch{source: source, invalid: key.fallback != "" || h.SegmentIndex != 0}
		c.epochs[key] = epoch
	} else {
		p := epoch.previous
		if !p.Sealed || p.SegmentEnd == nil || p.SegmentEnd.Reason != jhlog.SegmentEndRotation ||
			p.Header.SegmentIndex == maxUint64Value || h.SegmentIndex != p.Header.SegmentIndex+1 ||
			len(p.SegmentDigest) != 32 || !bytes.Equal(p.SegmentDigest, h.PreviousSegmentDigest) ||
			h.SegmentStartElapsedUS < p.Header.SegmentStartElapsedUS {
			epoch.invalid = true
		}
	}
	if h.RequiredFeatures&jhlog.FeatureHTTPCollectionState == 0 {
		epoch.invalid = true
	}
	c.current = epoch
}

func (c *httpCoverageCollector) observe(event jhlog.Event) {
	e := c.current
	if e == nil || event.Session == nil {
		return
	}
	enabled := event.Session.CollectorFlags&uint64(jhlog.CollectorHTTP) != 0
	if e.known && e.enabled != enabled {
		e.invalid = true
	}
	e.enabled, e.known = enabled, true
	if !e.started {
		e.start = event.TimeUS
		e.started = true
	}
}

func (c *httpCoverageCollector) end(r jhlog.StreamResult) {
	e := c.current
	if e == nil {
		return
	}
	if !r.Sealed || r.Status != jhlog.SegmentStatusClosedClean || r.TailBytes != 0 || r.LatestQuality == nil ||
		r.SegmentEnd == nil || (r.SegmentEnd.Reason != jhlog.SegmentEndNormal && r.SegmentEnd.Reason != jhlog.SegmentEndShutdown && r.SegmentEnd.Reason != jhlog.SegmentEndRotation) {
		e.invalid = true
	}
	if r.LatestQuality != nil {
		e.observeBound(&e.windowStartMS, r.LatestQuality.Counters[jhlog.QualityCollectionWindowStartElapsedMS])
		e.observeBound(&e.windowEndMS, r.LatestQuality.Counters[jhlog.QualityCollectionWindowEndElapsedMS])
		if httpCollectionLoss(r.LatestQuality.Counters) {
			e.invalid = true
		}
		end := r.LatestQuality.CapturedElapsedUS
		if end < e.start || end < e.end {
			e.invalid = true
		}
		e.end = end
	}
	// Only the boundary is retained; per-record maps belong to the reader.
	e.previous = jhlog.StreamResult{Header: jhlog.SegmentHeader{SegmentIndex: r.Header.SegmentIndex, SegmentStartElapsedUS: r.Header.SegmentStartElapsedUS}, Sealed: r.Sealed, SegmentEnd: r.SegmentEnd, SegmentDigest: r.SegmentDigest}
	c.current = nil
}

func httpCollectionLoss(counters map[uint64]uint64) bool {
	if counters[jhlog.QualityAcceptedEventTotal] > counters[jhlog.QualityWrittenEventTotal] {
		return true
	}
	for id, value := range counters {
		if value == 0 {
			continue
		}
		if id >= jhlog.QualityQueueFullTotal && id <= jhlog.QualityEventLostAfterStorageBudget && id != jhlog.QualityCommittedChunkTotal {
			return true
		}
		if id >= jhlog.EventQualityCounterID(jhlog.EventHTTP, jhlog.QualityLossQueueFull) && id <= jhlog.EventQualityCounterID(jhlog.EventHTTP, jhlog.QualityLossStorageBudget) {
			return true
		}
		switch id {
		case jhlog.QualityRuntimeHookFailureTotal, jhlog.QualityRuntimeHookInstrumentationFailure,
			jhlog.QualityRuntimeHookCollectorFailure, jhlog.QualityRuntimeHookContextFailure, jhlog.QualityRuntimeHookUnclassifiedFailure,
			jhlog.QualityAsyncCompletionInvalid, jhlog.QualityAsyncTokenCapacityRejected, jhlog.QualityAsyncTokenIdExhausted,
			jhlog.QualityAsyncUnfinishedHTTP, jhlog.QualityAsyncCompletionInProgressAtStop:
			return true
		}
	}
	return false
}

type httpCoverageRange struct {
	source     string
	start, end uint64
	enabled    bool
	invalid    bool
}

// Sorting source ranges and two difference arrays bound the work to
// O(S log S + B), with O(S+B) charged memory; no process-by-bucket matrix.
func (c *httpCoverageCollector) apply(timeline []TimelineBucket, normalizer runTimelineNormalizer) {
	if len(timeline) == 0 {
		return
	}
	scratch := c.account.scratch("HTTP count coverage sweep")
	defer scratch.close()
	if !scratch.reserveItems(len(c.epochs), 128) || !scratch.reserveItems(len(timeline)+1, 16) {
		return
	}
	ranges := make([]httpCoverageRange, 0, len(c.epochs))
	for key, e := range c.epochs {
		base := normalizer.baseByKey[fmt.Sprintf("run:%x", key.run[:])]
		windowValid := e.windowStartMS > 0 && e.windowEndMS >= e.windowStartMS && e.windowEndMS <= maxUint64Value/1000
		startUS, endUS := max(e.start, e.windowStartMS*1000), min(e.end, e.windowEndMS*1000)
		windowValid = windowValid && startUS <= endUS
		startUS, endUS = safeCounterDelta(base*1000, startUS), safeCounterDelta(base*1000, endUS)
		start, end := startUS/1000, endUS/1000
		if startUS%1000 != 0 {
			start++
		}
		invalid := !windowValid || e.invalid || !e.known || !e.started || e.previous.SegmentEnd == nil || e.previous.SegmentEnd.Reason == jhlog.SegmentEndRotation
		ranges = append(ranges, httpCoverageRange{e.source, start, end, e.enabled, invalid})
	}
	sort.Slice(ranges, func(i, j int) bool {
		if ranges[i].source != ranges[j].source {
			return ranges[i].source < ranges[j].source
		}
		return ranges[i].start < ranges[j].start
	})
	observed, unsupported := make([]int, len(timeline)+1), make([]int, len(timeline)+1)
	sources := 0
	bucketMS := timeline[0].EndMS - timeline[0].StartMS
	for i := 0; i < len(ranges); {
		j := i + 1
		for j < len(ranges) && ranges[j].source == ranges[i].source {
			j++
		}
		sources++
		overlap := false
		for k := i + 1; k < j; k++ {
			if ranges[k].start < ranges[k-1].end {
				overlap = true
			}
		}
		for k := i; k < j; k++ {
			r := ranges[k]
			if r.invalid || overlap || bucketMS == 0 {
				continue
			}
			lo := r.start / bucketMS
			if r.start%bucketMS != 0 {
				lo++
			}
			hi := r.end / bucketMS
			if lo >= hi || lo >= uint64(len(timeline)) {
				continue
			}
			hi = min(hi, uint64(len(timeline)))
			delta := unsupported
			if r.enabled {
				delta = observed
			}
			delta[lo]++
			delta[hi]--
		}
		i = j
	}
	active, disabled := 0, 0
	for i := range timeline {
		active += observed[i]
		disabled += unsupported[i]
		state := CountMissing
		if sources > 0 && active == sources {
			state = CountObserved
		} else if sources > 0 && disabled == sources {
			state = CountUnsupported
		}
		if state == CountUnsupported && timeline[i].HTTPCount > 0 {
			state = CountMissing
		}
		timeline[i].HTTPCountState = state
		if state == CountObserved {
			timeline[i].HasObservation = true
		}
	}
}

func HTTPCountCoverageExplanation(timeline []TimelineBucket) string {
	missing, unsupported := 0, 0
	for _, b := range timeline {
		if b.HTTPCountState == CountMissing {
			missing++
		}
		if b.HTTPCountState == CountUnsupported {
			unsupported++
		}
	}
	if missing+unsupported == 0 {
		return ""
	}
	return fmt.Sprintf("Наблюдение HTTP-счётчиков: неизвестны %d интервалов, сбор отключён или недоступен в %d. Ноль используется только при подтверждённом сборе за весь интервал во всех входных процессах. Старый формат, неполная цепочка и потери не доказывают тихую сеть; полученные события сохранены. Неполные счётчики исключены из анализа временных рядов и интегральной сетевой нагрузки. В таблице ≥ означает нижнюю границу, н/д означает отсутствие полного счётчика.", missing, unsupported)
}

func longestHTTPCountRun(timeline []TimelineBucket) (int, int) {
	bestStart, bestEnd, start := 0, 0, 0
	for i, b := range timeline {
		if !httpCountPresent(b) {
			start = i + 1
			continue
		}
		if i+1-start > bestEnd-bestStart {
			bestStart, bestEnd = start, i+1
		}
	}
	return bestStart, bestEnd
}

func completeHTTPCounts(timeline []TimelineBucket) bool {
	for _, b := range timeline {
		if b.HTTPCountState != "" && !httpCountPresent(b) {
			return false
		}
	}
	return true
}

func (e *httpCoverageEpoch) observeBound(destination *uint64, value uint64) {
	if value == 0 {
		return
	}
	if *destination != 0 && *destination != value {
		e.invalid = true
	}
	*destination = value
}
