package traffic

import (
	"bytes"
	"math"
	"math/rand"
	"slices"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func TestUnionAgainstIndependentByteSet(t *testing.T) {
	// The oracle enumerates bytes independently; it shares neither sorting nor
	// heap logic with the production sweep. Shuffle input to rule out order bias.
	for seed := int64(0); seed < 512; seed++ {
		rng := rand.New(rand.NewSource(seed))
		var input []Interval
		oracle := map[[3]uint64]uint64{}
		for count := 0; count < 64; count++ {
			run, uid := uint64(1+rng.Intn(3)), uint64(1+rng.Intn(3))
			lo, hi := uint64(rng.Intn(32)), uint64(rng.Intn(32))
			if lo > hi {
				lo, hi = hi, lo
			}
			at := uint64(rng.Intn(16))
			input = append(input, Interval{Domain{jhlog.ID128{byte(run)}, uid}, lo, hi, at})
			for value := lo; value < hi; value++ {
				key := [3]uint64{run, uid, value}
				if old, ok := oracle[key]; !ok || at < old {
					oracle[key] = at
				}
			}
		}
		want := map[uint64]uint64{}
		for _, at := range oracle {
			want[at]++
		}
		for permutation := 0; permutation < 4; permutation++ {
			rng.Shuffle(len(input), func(i, j int) { input[i], input[j] = input[j], input[i] })
			got := map[uint64]uint64{}
			Union(input, make([]int, 0, len(input)), func(count, at uint64) { got[at] += count })
			if len(got) != len(want) {
				t.Fatalf("seed=%d: got=%v want=%v", seed, got, want)
			}
			for at, count := range want {
				if got[at] != count {
					t.Fatalf("seed=%d time=%d got=%v want=%v", seed, at, got, want)
				}
			}
		}
	}
}

func TestUnionDomainAndCounterBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input []Interval
		want  uint64
	}{
		{"overlap", []Interval{{Domain{jhlog.ID128{1}, 1}, 10, 20, 1}, {Domain{jhlog.ID128{1}, 1}, 15, 25, 2}}, 15},
		{"different_UID", []Interval{{Domain{jhlog.ID128{1}, 1}, 10, 20, 1}, {Domain{jhlog.ID128{1}, 2}, 10, 20, 2}}, 20},
		{"different_run", []Interval{{Domain{jhlog.ID128{1}, 1}, 10, 20, 1}, {Domain{jhlog.ID128{2}, 1}, 10, 20, 2}}, 20},
		{"unknown_UID_lower_bound", []Interval{{Domain{jhlog.ID128{1}, 1}, 10, 20, 1}, {Domain{jhlog.ID128{1}, 0}, 10, 20, 2}}, 10},
		{"unknown_run_lower_bound", []Interval{{Domain{jhlog.ID128{1}, 1}, 10, 20, 1}, {Domain{}, 10, 20, 2}}, 10},
		{"uint64_end", []Interval{{Domain{jhlog.ID128{1}, 1}, 0, math.MaxUint64, 1}, {Domain{jhlog.ID128{1}, 1}, 10, math.MaxUint64, 2}}, math.MaxUint64},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got uint64
			Union(tc.input, make([]int, 0, len(tc.input)), func(n, _ uint64) { got += n })
			if got != tc.want {
				t.Fatalf("got=%d want=%d", got, tc.want)
			}
		})
	}
}

func TestTrackerKnownZeroUnavailableResetAndUIDChange(t *testing.T) {
	header := jhlog.DefaultSegmentHeader()
	header.RunID = jhlog.ID128{1}
	header.ProcessInstanceID = jhlog.ID128{1}
	header.SessionID = jhlog.ID128{1}
	var tracker Tracker
	tracker.StartSegment(header)
	var rx, tx []uint64
	emit := func(d int, i Interval) {
		if d == 0 {
			rx = append(rx, i.High-i.Low)
		} else {
			tx = append(tx, i.High-i.Low)
		}
	}
	tracker.Observe(&jhlog.ContextEvent{TrafficUIDPlusOne: 1, TrafficKnownFlags: 3}, 0, emit)
	tracker.Observe(&jhlog.ContextEvent{TrafficUIDPlusOne: 1, TrafficKnownFlags: 3}, 1, emit)
	if !tracker.Evidence().Exact(0) || !tracker.Evidence().Exact(1) {
		t.Fatal("supported zero must be known")
	}
	tracker.Observe(&jhlog.ContextEvent{TrafficUIDPlusOne: 1, TrafficKnownFlags: 2, TxBytes: 5}, 2, emit)
	tracker.Observe(&jhlog.ContextEvent{TrafficUIDPlusOne: 1, TrafficKnownFlags: 3, RxBytes: 100, TxBytes: 8}, 3, emit)
	tracker.Observe(&jhlog.ContextEvent{TrafficUIDPlusOne: 1, TrafficKnownFlags: 3, RxBytes: 110, TxBytes: 1}, 4, emit)
	tracker.Observe(&jhlog.ContextEvent{TrafficUIDPlusOne: 2, TrafficKnownFlags: 3, RxBytes: 999, TxBytes: 999}, 5, emit)
	if !slices.Equal(rx, []uint64{0, 10}) || !slices.Equal(tx, []uint64{0, 5, 3}) {
		t.Fatalf("RX=%v TX=%v", rx, tx)
	}
	e := tracker.Evidence()
	if e.RX.UnavailableSamples != 1 || e.TX.Resets != 1 || e.Exact(0) || e.Exact(1) {
		t.Fatalf("quality=%+v", e)
	}
}

func BenchmarkUnionOverlap(b *testing.B) {
	input := make([]Interval, 100_000)
	for i := range input {
		input[i] = Interval{Domain{jhlog.ID128{1}, 1}, uint64(i), uint64(i + 100), uint64(len(input) - i)}
	}
	work := make([]Interval, len(input))
	scratch := make([]int, 0, len(input))
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		copy(work, input)
		var total uint64
		Union(work, scratch, func(n, _ uint64) { total += n })
		if total != 100_099 {
			b.Fatal(total)
		}
	}
}

func TestTrackerContinuationRequiresVerifiedRotationBoundary(t *testing.T) {
	for _, name := range []string{"continuous", "index_gap", "run", "process", "session", "collector_clock", "clock_reversed", "digest", "open", "normal_end", "sample_time_reversed"} {
		t.Run(name, func(t *testing.T) {
			h := jhlog.DefaultSegmentHeader()
			h.RunID = jhlog.ID128{1}
			h.ProcessInstanceID = jhlog.ID128{1}
			h.SessionID = jhlog.ID128{1}
			h.CollectorStartElapsedUS = 10
			h.SegmentStartElapsedUS = 100
			var tracker Tracker
			tracker.StartSegment(h)
			var total uint64
			emit := func(d int, i Interval) {
				if d == 0 {
					total += i.High - i.Low
				}
			}
			tracker.Observe(&jhlog.ContextEvent{TrafficUIDPlusOne: 1, TrafficKnownFlags: 3, RxBytes: 100, TxBytes: 100}, 1000, emit)
			result := jhlog.StreamResult{Sealed: true, Status: jhlog.SegmentStatusClosedClean, SegmentDigest: bytes.Repeat([]byte{1}, 32), SegmentEnd: &jhlog.SegmentEndEvent{Reason: jhlog.SegmentEndRotation}}
			if name == "open" {
				result.Sealed = false
				result.Status = jhlog.SegmentStatusOpenClean
			}
			if name == "normal_end" {
				result.SegmentEnd.Reason = jhlog.SegmentEndNormal
			}
			tracker.EndSegment(result)
			h.SegmentIndex = 1
			h.SegmentStartElapsedUS = 200
			h.PreviousSegmentDigest = append([]byte(nil), result.SegmentDigest...)
			switch name {
			case "index_gap":
				h.SegmentIndex = 2
			case "run":
				h.RunID = jhlog.ID128{2}
			case "process":
				h.ProcessInstanceID = jhlog.ID128{2}
			case "session":
				h.SessionID = jhlog.ID128{2}
			case "collector_clock":
				h.CollectorStartElapsedUS = 11
			case "clock_reversed":
				h.SegmentStartElapsedUS = 99
			case "digest":
				h.PreviousSegmentDigest[0] = 2
			}
			tracker.StartSegment(h)
			at := uint64(2000)
			if name == "sample_time_reversed" {
				at = 999
			}
			tracker.Observe(&jhlog.ContextEvent{TrafficUIDPlusOne: 1, TrafficKnownFlags: 3, RxBytes: 1100, TxBytes: 1100}, at, emit)
			want := uint64(0)
			if name == "continuous" {
				want = 1000
			}
			if total != want {
				t.Fatalf("delta=%d want%d", total, want)
			}
			if tracker.Evidence().Exact(0) != (name == "continuous") {
				t.Fatalf("quality=%+v", tracker.Evidence())
			}
		})
	}
}
