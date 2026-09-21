package mathanalysis

import (
	"unsafe"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
	"github.com/i-redbyte/jank-hunter/cli/internal/traffic"
)

type timelineTraffic struct {
	tracker traffic.Tracker
	account *collectionAccount
	ranges  [2][]traffic.Interval
}

func (c *timelineCollector) observeTraffic(event jhlog.Event) {
	c.traffic.tracker.Observe(event.Context, event.TimeMS, func(direction int, interval traffic.Interval) {
		agg := c.bucket(event.TimeMS)
		if agg == nil {
			return
		}
		agg.bucket.HasTrafficSample = true
		if direction == 0 {
			agg.bucket.TrafficRXKnown = true
		} else {
			agg.bucket.TrafficTXKnown = true
		}
		if interval.Low == interval.High {
			return
		}
		interval.TimeMS = agg.bucket.StartMS
		c.traffic.append(direction, interval)
	})
}

func (t *timelineTraffic) append(direction int, interval traffic.Interval) {
	ranges := t.ranges[direction]
	if n := len(ranges); n > 0 && ranges[n-1].Domain == interval.Domain && ranges[n-1].High == interval.Low && ranges[n-1].TimeMS == interval.TimeMS {
		ranges[n-1].High = interval.High
		return
	}
	if len(ranges) == cap(ranges) {
		capacity := max(16, cap(ranges)*2)
		if capacity < cap(ranges) || !t.account.reserveItems(capacity, uint64(unsafe.Sizeof(traffic.Interval{}))) {
			return
		}
		next := make([]traffic.Interval, len(ranges), capacity)
		copy(next, ranges)
		t.account.release(uint64(cap(ranges)) * uint64(unsafe.Sizeof(traffic.Interval{})))
		ranges = next
	}
	t.ranges[direction] = append(ranges, interval)
}

func (c *timelineCollector) finishTraffic() {
	t := &c.traffic
	capacity := max(len(t.ranges[0]), len(t.ranges[1]))
	if !t.account.reserveItems(capacity, uint64(unsafe.Sizeof(int(0)))) {
		return
	}
	scratch := make([]int, 0, capacity)
	defer t.account.release(uint64(capacity) * uint64(unsafe.Sizeof(int(0))))
	for direction := range t.ranges {
		var overall uint64
		traffic.Union(t.ranges[direction], scratch, func(count, timeMS uint64) {
			if count > maxUint64Value-overall {
				t.tracker.MarkOverflow(direction)
				overall = maxUint64Value
			} else {
				overall += count
			}
			agg := c.bucket(timeMS)
			if agg == nil {
				return
			}
			total := &agg.bucket.TrafficRxBytes
			if direction == 1 {
				total = &agg.bucket.TrafficTxBytes
			}
			if count > maxUint64Value-*total {
				*total = maxUint64Value
				t.tracker.MarkOverflow(direction)
			} else {
				*total += count
			}
		})
	}
	evidence := t.tracker.Evidence()
	for _, agg := range c.buckets {
		agg.bucket.TrafficRXKnown = agg.bucket.TrafficRXKnown && evidence.Exact(0)
		agg.bucket.TrafficTXKnown = agg.bucket.TrafficTXKnown && evidence.Exact(1)
	}
}
