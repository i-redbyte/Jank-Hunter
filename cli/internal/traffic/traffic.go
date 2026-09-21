// Package traffic measures observed UID-counter coverage, not packet arrival times.
package traffic

import (
	"bytes"
	"math"
	"slices"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

type Domain struct {
	Run        jhlog.ID128
	UIDPlusOne uint64
}

// Interval is a half-open range in cumulative byte-counter space. TimeMS is the
// end of the observation, not a claim about when individual bytes arrived.
type Interval struct {
	Domain            Domain
	Low, High, TimeMS uint64
}

type DirectionEvidence struct {
	State              string `json:"state"`
	Samples            uint64 `json:"samples"`
	Intervals          uint64 `json:"intervals"`
	UnknownProvenance  uint64 `json:"unknown_provenance"`
	UnavailableSamples uint64 `json:"unavailable_samples"`
	Resets             uint64 `json:"resets"`
	Overflow           bool   `json:"overflow"`
}

type Evidence struct {
	RX               DirectionEvidence `json:"rx"`
	TX               DirectionEvidence `json:"tx"`
	BrokenBoundaries uint64            `json:"broken_boundaries"`
	UnsealedSegments uint64            `json:"unsealed_segments"`
}

func (e Evidence) Exact(direction int) bool {
	if direction == 0 {
		return e.RX.State == "exact"
	}
	return e.TX.State == "exact"
}

type sample struct {
	domain        Domain
	value, timeMS uint64
	usable        bool
}

// Tracker holds only one source's last observations. Ordered input must put
// every session's segments together; no process-to-process subtraction occurs.
type Tracker struct {
	header   jhlog.SegmentHeader
	last     [2]sample
	previous boundary
	started  bool
	evidence Evidence
}

type boundary struct {
	run, process, session        jhlog.ID128
	index, start, collectorStart uint64
	digest                       [32]byte
	valid                        bool
}

func (t *Tracker) StartSegment(h jhlog.SegmentHeader) {
	p := t.previous
	continuous := p.valid && !h.SessionID.IsZero() && !h.ProcessInstanceID.IsZero() && !h.RunID.IsZero() &&
		p.run == h.RunID && p.process == h.ProcessInstanceID && p.session == h.SessionID &&
		p.index < math.MaxUint64 && h.SegmentIndex == p.index+1 &&
		p.collectorStart == h.CollectorStartElapsedUS && h.SegmentStartElapsedUS >= p.start &&
		bytes.Equal(p.digest[:], h.PreviousSegmentDigest)
	if !continuous {
		if h.SegmentIndex > 0 || (t.started && h.SessionID == t.header.SessionID && !h.SessionID.IsZero()) {
			t.evidence.BrokenBoundaries++
		}
		t.last = [2]sample{}
	}
	t.header, t.started, t.previous = h, true, boundary{}
}

func (t *Tracker) EndSegment(r jhlog.StreamResult) {
	h := t.header
	p := boundary{run: h.RunID, process: h.ProcessInstanceID, session: h.SessionID,
		index: h.SegmentIndex, start: h.SegmentStartElapsedUS, collectorStart: h.CollectorStartElapsedUS}
	clean := r.Sealed && r.Status == jhlog.SegmentStatusClosedClean && r.TailBytes == 0
	if !clean {
		t.evidence.UnsealedSegments++
	}
	p.valid = clean && r.SegmentEnd != nil && r.SegmentEnd.Reason == jhlog.SegmentEndRotation && len(r.SegmentDigest) == len(p.digest)
	copy(p.digest[:], r.SegmentDigest)
	t.previous = p
}

// Observe returns only differences between compatible successive samples.
// Legacy counters remain available as uncertain observations. Explicitly
// unsupported new counters never become a baseline of zero.
func (t *Tracker) Observe(c *jhlog.ContextEvent, timeMS uint64, emit func(int, Interval)) {
	values := [2]uint64{c.RxBytes, c.TxBytes}
	for direction, value := range values {
		e := t.direction(direction)
		e.Samples++
		provenance := c.TrafficUIDPlusOne != 0 && !t.header.RunID.IsZero() &&
			!t.header.ProcessInstanceID.IsZero() && !t.header.SessionID.IsZero()
		if !provenance {
			e.UnknownProvenance++
		}
		usable := c.TrafficUIDPlusOne == 0 || c.TrafficKnownFlags&(1<<direction) != 0
		if !usable {
			e.UnavailableSamples++
		}
		next := sample{Domain{t.header.RunID, uint64(c.TrafficUIDPlusOne)}, value, timeMS, usable}
		previous := t.last[direction]
		if usable && previous.usable && previous.domain == next.domain {
			if value < previous.value || timeMS < previous.timeMS {
				e.Resets++
			} else {
				e.Intervals++
				emit(direction, Interval{next.domain, previous.value, value, timeMS})
			}
		}
		t.last[direction] = next
	}
}

func (t *Tracker) direction(direction int) *DirectionEvidence {
	if direction == 0 {
		return &t.evidence.RX
	}
	return &t.evidence.TX
}

func (t *Tracker) MarkOverflow(direction int) { t.direction(direction).Overflow = true }

func (t *Tracker) Evidence() Evidence {
	e := t.evidence
	for _, d := range []*DirectionEvidence{&e.RX, &e.TX} {
		d.State = "exact"
		if d.Intervals == 0 {
			d.State = "unknown"
		} else if d.UnknownProvenance > 0 ||
			d.UnavailableSamples > 0 || d.Resets > 0 || d.Overflow || e.BrokenBoundaries > 0 || e.UnsealedSegments > 0 {
			d.State = "lower_bound"
		}
	}
	return e
}

// NormalizeDomains collapses uncertain identities conservatively. When a run
// contains an unknown UID, unioning every UID's counter ranges gives a lower
// bound, never a fabricated sum of potentially duplicate process observations.
// Without a run ID even the run partition is unknown, so all ranges are united.
func NormalizeDomains(intervals []Interval) {
	slices.SortFunc(intervals, func(a, b Interval) int { return bytes.Compare(a.Domain.Run[:], b.Domain.Run[:]) })
	if len(intervals) > 0 && intervals[0].Domain.Run.IsZero() {
		for i := range intervals {
			intervals[i].Domain = Domain{}
		}
		return
	}
	for first := 0; first < len(intervals); {
		last, unknown := first+1, intervals[first].Domain.UIDPlusOne == 0
		for last < len(intervals) && intervals[last].Domain.Run == intervals[first].Domain.Run {
			unknown = unknown || intervals[last].Domain.UIDPlusOne == 0
			last++
		}
		if unknown {
			for i := first; i < last; i++ {
				intervals[i].Domain.UIDPlusOne = 0
			}
		}
		first = last
	}
}

func intervalOrder(a, b Interval) int {
	if c := bytes.Compare(a.Domain.Run[:], b.Domain.Run[:]); c != 0 {
		return c
	}
	if a.Domain.UIDPlusOne < b.Domain.UIDPlusOne {
		return -1
	}
	if a.Domain.UIDPlusOne > b.Domain.UIDPlusOne {
		return 1
	}
	if a.Low < b.Low {
		return -1
	}
	if a.Low > b.Low {
		return 1
	}
	if a.High < b.High {
		return -1
	}
	if a.High > b.High {
		return 1
	}
	if a.TimeMS < b.TimeMS {
		return -1
	}
	if a.TimeMS > b.TimeMS {
		return 1
	}
	return 0
}

// Union performs an O(n log n) sweep with O(n) caller-owned scratch. Each byte
// is assigned to the earliest observation that confirms it. A heap ordered by
// observation time uses lazy expiry; hidden ends need no separate event array.
// The callback receives disjoint byte lengths and their observation time.
func Union(intervals []Interval, scratch []int, emit func(uint64, uint64)) {
	NormalizeDomains(intervals)
	slices.SortFunc(intervals, intervalOrder)
	for first := 0; first < len(intervals); {
		last := first + 1
		for last < len(intervals) && intervals[last].Domain == intervals[first].Domain {
			last++
		}
		unionDomain(intervals[first:last], scratch[:0], emit)
		first = last
	}
}

func unionDomain(intervals []Interval, heap []int, emit func(uint64, uint64)) {
	less := func(a, b int) bool {
		if intervals[a].TimeMS != intervals[b].TimeMS {
			return intervals[a].TimeMS < intervals[b].TimeMS
		}
		return a < b
	}
	position, index := intervals[0].Low, 0
	for index < len(intervals) || len(heap) > 0 {
		if len(heap) == 0 && index < len(intervals) && position < intervals[index].Low {
			position = intervals[index].Low
		}
		for index < len(intervals) && intervals[index].Low <= position {
			if intervals[index].High > position {
				heap = append(heap, index)
				for child := len(heap) - 1; child > 0; {
					parent := (child - 1) / 2
					if !less(heap[child], heap[parent]) {
						break
					}
					heap[child], heap[parent] = heap[parent], heap[child]
					child = parent
				}
			}
			index++
		}
		for len(heap) > 0 && intervals[heap[0]].High <= position {
			heap[0] = heap[len(heap)-1]
			heap = heap[:len(heap)-1]
			for parent := 0; parent*2+1 < len(heap); {
				child := parent*2 + 1
				if child+1 < len(heap) && less(heap[child+1], heap[child]) {
					child++
				}
				if !less(heap[child], heap[parent]) {
					break
				}
				heap[child], heap[parent] = heap[parent], heap[child]
				parent = child
			}
		}
		if len(heap) == 0 {
			continue
		}
		end := intervals[heap[0]].High
		if index < len(intervals) && intervals[index].Low < end {
			end = intervals[index].Low
		}
		emit(end-position, intervals[heap[0]].TimeMS)
		position = end
	}
}
