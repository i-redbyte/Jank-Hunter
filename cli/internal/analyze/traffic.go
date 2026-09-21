package analyze

import (
	"fmt"
	"math"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
	"github.com/i-redbyte/jank-hunter/cli/internal/traffic"
)

func (c *collector) recordTraffic(context *jhlog.ContextEvent, timeMS uint64) {
	c.trafficTracker.Observe(context, timeMS, func(direction int, interval traffic.Interval) {
		if interval.Low == interval.High {
			return
		}
		// Summary needs the union only, so a monotonic source chain is one range.
		interval.TimeMS = 0
		ranges := c.trafficRanges[direction]
		if n := len(ranges); n > 0 && ranges[n-1].Domain == interval.Domain && ranges[n-1].High == interval.Low {
			ranges[n-1].High = interval.High
		} else {
			c.trafficRanges[direction] = append(ranges, interval)
		}
	})
}

func (c *collector) finalizeTraffic(summary *Summary) {
	if summary.ContextCount == 0 {
		return
	}
	scratch := make([]int, 0, max(len(c.trafficRanges[0]), len(c.trafficRanges[1])))
	for direction, total := range []*uint64{&summary.TrafficRxMax, &summary.TrafficTxMax} {
		traffic.Union(c.trafficRanges[direction], scratch, func(count, timeMS uint64) {
			if count > math.MaxUint64-*total {
				*total = math.MaxUint64
				c.trafficTracker.MarkOverflow(direction)
			} else {
				*total += count
			}
		})
	}
	evidence := c.trafficTracker.Evidence()
	summary.CollectionQuality.Traffic = &evidence
	if !evidence.Exact(0) || !evidence.Exact(1) {
		summary.Warnings = append(summary.Warnings, fmt.Sprintf(
			"Качество сбора: UID-трафик RX=%s, TX=%s; неизвестное происхождение снимков RX/TX=%d/%d, недоступные счётчики=%d/%d, сбросы=%d/%d, разрывы сегментов=%d, незакрытые сегменты=%d. Числовые значения сохраняют нижнюю границу наблюдаемого трафика; точное сравнение и временные ряды недоступного направления исключены.",
			trafficStateLabel(evidence.RX.State), trafficStateLabel(evidence.TX.State), evidence.RX.UnknownProvenance, evidence.TX.UnknownProvenance,
			evidence.RX.UnavailableSamples, evidence.TX.UnavailableSamples, evidence.RX.Resets, evidence.TX.Resets, evidence.BrokenBoundaries, evidence.UnsealedSegments))
	}
}

func trafficDelta(baseline, candidate Summary, direction int) Delta {
	name, before, after := "UID RX delta", baseline.TrafficRxMax, candidate.TrafficRxMax
	if direction == 1 {
		name, before, after = "UID TX delta", baseline.TrafficTxMax, candidate.TrafficTxMax
	}
	knownSamples := func(summary Summary) uint64 {
		e := summary.CollectionQuality.Traffic
		if e == nil || !e.Exact(direction) {
			return 0
		}
		if direction == 0 {
			return e.RX.Intervals
		}
		return e.TX.Intervals
	}
	result := observedDelta(name, before, after, "байт", true, knownSamples(baseline), knownSamples(candidate), "точная дельта UID-трафика недоступна")
	display := func(summary Summary, value uint64, fallback string) string {
		e := summary.CollectionQuality.Traffic
		if e == nil {
			return fallback
		}
		state := e.RX.State
		if direction == 1 {
			state = e.TX.State
		}
		if state == "lower_bound" {
			return fmt.Sprintf("≥ %d байт", value)
		}
		if state == "unknown" {
			return "неизвестно"
		}
		return fallback
	}
	result.Baseline = display(baseline, before, result.Baseline)
	result.Candidate = display(candidate, after, result.Candidate)
	return result
}

func trafficStateLabel(state string) string {
	switch state {
	case "exact":
		return "измерено"
	case "lower_bound":
		return "нижняя граница"
	default:
		return "неизвестно"
	}
}
