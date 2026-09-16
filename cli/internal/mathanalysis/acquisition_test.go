package mathanalysis

import (
	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"testing"
)

func TestMathDoesNotTreatUnverifiedClockGroupsAsIndependentRuns(t *testing.T) {
	first := writeRunOffsetTimelineFixture(t, "first.jhlog", 1, 1000)
	second := writeRunOffsetTimelineFixture(t, "second.jhlog", 2, 100000)
	report, err := analyzeInspectForTest(t, []string{first, second}, analyze.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if report.IndependentRunCount != 0 {
		t.Fatalf("missing process identities claimed %d independent runs", report.IndependentRunCount)
	}
	if report.Markov.SequenceComparable || report.Markov.Forecast.Direction != markovForecastInsufficient {
		t.Fatal("overlay became one chronological sequence")
	}
}

func TestDependentAcquisitionsKeepSeparateClockGuard(t *testing.T) {
	first := writeRunOffsetTimelineFixture(t, "first.jhlog", 1, 1000, 7)
	second := writeRunOffsetTimelineFixture(t, "second.jhlog", 2, 100000, 7)
	report, err := analyzeInspectForTest(t, []string{first, second}, analyze.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if report.IndependentRunCount != 1 || report.TimelineGroupCount != 2 || report.Markov.IndependentRunCount != 1 || report.Markov.TimelineGroupCount != 2 {
		t.Fatalf("wrong acquisition/clock units: acquisition=%d clocks=%d markov=%+v", report.IndependentRunCount, report.TimelineGroupCount, report.Markov)
	}
	if report.Markov.SequenceComparable || report.Markov.Forecast.Direction != markovForecastInsufficient {
		t.Fatal("dependent but overlaid clocks enabled forecasting")
	}
}
