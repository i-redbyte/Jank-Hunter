package analyze

import (
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func TestHTTPFirstByteSamplesRequireObservedSemanticsAndRetainKnownZero(t *testing.T) {
	const observed = uint64(1 << 24)
	const known = uint64(1 << 25)
	for _, test := range []struct {
		name         string
		flags, value uint64
		wantSamples  int
	}{
		{"legacy header intent", 0, 900, 0},
		{"missing observation", observed, 0, 0},
		{"unknown cannot carry an exact sample", observed, 200, 0},
		{"known alone lacks semantics", known, 400, 0},
		{"actual zero", observed | known, 0, 1},
		{"actual first byte", observed | known, 500, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			var aggregate httpAggregate
			aggregate.add(&jhlog.HTTPEvent{DurationMS: 1000, TTFBMS: test.value}, test.flags, 0, 1000, false)
			if got := aggregate.phases[5].seen; got != test.wantSamples {
				t.Fatalf("TTFB samples=%d, want %d", got, test.wantSamples)
			}
			if test.wantSamples > 0 && phaseAverage(&aggregate, 5) != test.value {
				t.Fatalf("TTFB average=%d, want %d", phaseAverage(&aggregate, 5), test.value)
			}
		})
	}
}

func TestHTTPFirstByteCoverageCountsAreIndependentOfDeliveryCompleteness(t *testing.T) {
	c := newCollector("first byte", 1, Options{})
	for _, flags := range []uint64{0, uint64(jhlog.FlagHTTPTTFBObserved), uint64(jhlog.FlagHTTPTTFBObserved | jhlog.FlagHTTPTTFBKnown)} {
		c.add(nil, jhlog.Event{Type: jhlog.EventHTTP, TimeMS: 100, Flags: flags, HTTP: &jhlog.HTTPEvent{DurationMS: 100}})
	}
	summary := c.finish()
	quality := summary.CollectionQuality.HTTPFirstByte
	if quality == nil || quality.Known != 1 || quality.Unknown != 1 || quality.Legacy != 1 {
		t.Fatalf("first-byte coverage=%+v", quality)
	}
	if summary.CollectionQuality.KnownLostEvents != 0 {
		t.Fatal("timing coverage fabricated lost events")
	}
}
