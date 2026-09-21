package mathanalysis

import (
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
	"testing"
)

func TestTimelineUsesOnlyActualFirstByteAndKeepsKnownZero(t *testing.T) {
	c := timelineCollector{scale: newTimelineScale(0, 1000, true), buckets: map[uint64]*timelineBucketAgg{}}
	var symbols mathSymbolResolver
	var stream timelineStreamState
	for _, sample := range []struct{ flags, value uint64 }{
		{0, 900}, {uint64(jhlog.FlagHTTPTTFBObserved), 0},
		{uint64(jhlog.FlagHTTPTTFBObserved | jhlog.FlagHTTPTTFBKnown), 0},
		{uint64(jhlog.FlagHTTPTTFBObserved | jhlog.FlagHTTPTTFBKnown), 400},
	} {
		c.add(jhlog.Event{Type: jhlog.EventHTTP, TimeMS: 100, Flags: sample.flags,
			HTTP: &jhlog.HTTPEvent{DurationMS: 1000, TTFBMS: sample.value}}, nil, &stream, &symbols)
	}
	result := c.finish()
	if !result[0].HasTTFB || result[0].TTFBMS != 200 {
		t.Fatalf("TTFB bucket=%+v", result[0])
	}
	if c.buckets[0].ttfbCount != 2 {
		t.Fatal("unknown/legacy data entered the sample population")
	}
}
