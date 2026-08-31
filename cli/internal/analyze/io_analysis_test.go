package analyze

import (
	"math"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func TestIOBurstUsesExactRollingWindowAndSeparatesLogs(t *testing.T) {
	var burst ioBurstAccumulator
	for _, timeMS := range []uint64{0, 500, 999, 1_000} {
		burst.add(1, timeMS)
	}
	if burst.peak != 3 || burst.peakWindowStartMS != 0 {
		t.Fatalf("rolling peak = %+v", burst)
	}
	burst.add(2, 0)
	if got := len(burst.timesMS) - burst.head; got != 1 || burst.peak != 3 {
		t.Fatalf("independent log joined into burst: active=%d state=%+v", got, burst)
	}
}

func TestMaxIOConcurrencyTreatsTouchingIntervalsAsNonOverlapping(t *testing.T) {
	intervals := []ioInterval{
		{logIndex: 1, startUS: 0, endUS: 100},
		{logIndex: 1, startUS: 50, endUS: 150},
		{logIndex: 1, startUS: 100, endUS: 200},
		{logIndex: 2, startUS: 0, endUS: 500},
	}
	peak, atUS := maxIOConcurrency(intervals)
	if peak != 2 || atUS != 50 {
		t.Fatalf("concurrency = %d at %d, want 2 at 50", peak, atUS)
	}
}

func TestBytesPerSecondIsExactAndSaturatesOverflow(t *testing.T) {
	if got := bytesPerSecond(1_048_576, 1_000_000); got != 1_048_576 {
		t.Fatalf("throughput = %d", got)
	}
	if got := bytesPerSecond(math.MaxUint64, 1); got != math.MaxUint64 {
		t.Fatalf("overflow throughput = %d", got)
	}
}

func TestInspectBuildsCriticalIOAnalysisWithoutDatabaseEvents(t *testing.T) {
	dict := map[uint64]string{
		1: "CheckoutScreen",
		2: "checkout.pay",
		3: "authorize",
		4: "CheckoutViewModel.submit",
		5: "CheckoutRepository.readPayload",
		6: "CheckoutRepository.commitPayload",
	}
	context := attributionForTest(1, 4, 2, 3)
	events := []jhlog.Event{
		{Type: jhlog.EventSession, TimeMS: 0, Session: &jhlog.SessionEvent{}},
		criticalIOEvent(100, 100_000, 4_096, true, false, jhlog.IOOperationFileRead, 5, context),
		criticalIOEvent(110, 100_000, 0, false, false, jhlog.IOOperationFileRead, 5, context),
		criticalIOEvent(120, 100_000, 8_192, true, true, jhlog.IOOperationFileRead, 5, context),
		criticalIOEvent(2_000, 10_000, 0, false, false, jhlog.IOOperationFileSync, 6, context),
	}

	summary := inspectLogsForTest("critical I/O", []jhlog.Log{{Dict: dict, Events: events}})
	analysis := summary.IOAnalysis
	if analysis == nil {
		t.Fatal("IOAnalysis is nil")
	}
	if analysis.Operations != 4 || analysis.Failures != 1 || analysis.MainThreadOperations != 4 ||
		analysis.KnownByteOperations != 2 || analysis.Bytes != 12_288 ||
		analysis.TotalDurationUS != 310_000 || analysis.P50DurationUS != 100_000 ||
		analysis.P95DurationUS != 100_000 || analysis.MaxDurationUS != 100_000 ||
		analysis.MaxConcurrency != 3 || analysis.SourceCount != 2 {
		t.Fatalf("I/O analysis = %+v", analysis)
	}
	if len(analysis.Calls) != 2 {
		t.Fatalf("I/O calls = %+v", analysis.Calls)
	}
	read := analysis.Calls[0]
	if read.Operation != "file_read" || read.Source != "CheckoutRepository.readPayload" ||
		read.Owner != "CheckoutViewModel.submit" || read.Screen != "CheckoutScreen" ||
		read.ContextOperation != "unknown" || !read.MainThread ||
		read.Count != 3 || read.Failures != 1 || read.KnownByteOperations != 2 ||
		read.Bytes != 12_288 || read.MaxBytes != 8_192 || read.P50DurationUS != 100_000 ||
		read.P95DurationUS != 100_000 || read.PeakOperationsPerSecond != 3 {
		t.Fatalf("file read group = %+v", read)
	}
}

func criticalIOEvent(
	timeMS uint64,
	durationUS uint64,
	bytes uint64,
	bytesKnown bool,
	failure bool,
	operation jhlog.IOOperationKind,
	sourceID uint64,
	attribution jhlog.AttributionContext,
) jhlog.Event {
	flags := uint64(jhlog.FlagThreadMain)
	if bytesKnown {
		flags |= uint64(jhlog.FlagIOBytesKnown)
	}
	outcome := jhlog.IOOutcomeSuccess
	if failure {
		outcome = jhlog.IOOutcomeFailure
	}
	return jhlog.Event{
		Type: jhlog.EventIO, TimeMS: timeMS, Flags: flags, Attribution: attribution,
		IO: &jhlog.IOEvent{
			SourceRef: jhlog.LocalSymbol(sourceID), Operation: operation, Outcome: outcome,
			DurationUS: durationUS, Bytes: bytes,
		},
	}
}
