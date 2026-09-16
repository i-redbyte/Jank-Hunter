package analyze

import (
	"encoding/json"
	"math/rand"
	"path/filepath"
	"sort"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func TestRollingBurstCrossesFixedSecondBoundary(t *testing.T) {
	var burst routeBurstAccumulator
	for i := 0; i < 15; i++ {
		burst.add(1, 999)
	}
	for i := 0; i < 15; i++ {
		burst.add(1, 1001)
	}
	if burst.peak != 30 || burst.peakWindowStartMS != 999 || burst.approximate {
		t.Fatalf("fixed-second bins are not a rolling peak: %+v", burst)
	}
}

func TestRollingBurstLongOrderedStreamDoesNotBecomeApproximate(t *testing.T) {
	var burst routeBurstAccumulator
	for i := uint64(0); i < 10000; i++ {
		burst.add(1, i*100)
	}
	if burst.peak != 10 || burst.approximate {
		t.Fatalf("ordered history can expire exactly: peak=%d approximate=%v", burst.peak, burst.approximate)
	}
}

func TestRollingBurstDisorderCannotClaimExact(t *testing.T) {
	var burst routeBurstAccumulator
	for _, at := range []uint64{2000, 999, 1001} {
		burst.add(1, at)
	}
	if !burst.approximate {
		t.Fatal("streaming result claims exact despite missing historical order")
	}
}

func TestHTTPRollingPeakMatchesPermutationOracle(t *testing.T) {
	for seed := int64(0); seed < 48; seed++ {
		rng := rand.New(rand.NewSource(seed))
		var logs []jhlog.Log
		want := uint64(0)
		for run := 0; run < 2; run++ {
			times := make([]uint64, 30)
			for i := range times {
				times[i] = uint64(rng.Intn(4))*1000 + uint64(rng.Intn(4))
			}
			for _, start := range times {
				count := uint64(0)
				for _, at := range times {
					if at >= start && at-start < 1000 {
						count++
					}
				}
				if count > want {
					want = count
				}
			}
			rng.Shuffle(len(times), func(i, j int) { times[i], times[j] = times[j], times[i] })
			log := jhlog.Log{Dict: map[uint64]string{1: "GET /peak"}}
			for _, at := range times {
				log.Events = append(log.Events, jhlog.Event{Type: jhlog.EventHTTP, TimeMS: at, HTTP: &jhlog.HTTPEvent{RouteRef: jhlog.LocalSymbol(1), DurationMS: 1, StatusCode: 200}})
			}
			logs = append(logs, log)
		}
		summary := inspectLogsForTest("rolling", logs)
		if len(summary.Routes) != 1 || summary.Routes[0].PeakRequestsPerSecond != want || summary.Routes[0].BurstEstimateStatus != "exact_rolling_second" {
			t.Fatalf("seed=%d: independently sorted-count oracle=%d, routes=%+v", seed, want, summary.Routes)
		}
	}
}

func TestRollingBurstMatchesOracleAtEveryPrefix(t *testing.T) {
	for _, offset := range []uint64{0, ^uint64(0) - 6000} {
		var burst routeBurstAccumulator
		var times []uint64
		var want uint64
		for i := uint64(0); i < 5000; i++ {
			at := offset + i
			for repeat := uint64(0); repeat <= i%3; repeat++ {
				times = append(times, at)
				burst.add(1, at)
				count := uint64(0)
				for j := len(times) - 1; j >= 0 && at-times[j] < 1000; j-- {
					count++
				}
				want = max(want, count)
				if burst.peak != want || burst.approximate || burst.size > 1000 || len(burst.buckets) > 1000 {
					t.Fatalf("at=%d peak=%d want=%d bins=%d", at, burst.peak, want, burst.size)
				}
			}
		}
	}
}

func TestRollingBurstHalfOpenBoundaryAndLogIsolation(t *testing.T) {
	var burst routeBurstAccumulator
	for _, at := range []uint64{0, 999, 1000, 1999} {
		burst.add(1, at)
	}
	if burst.peak != 2 || burst.peakWindowStartMS != 0 {
		t.Fatalf("half-open window: %+v", burst)
	}
	burst.add(2, 1999)
	burst.add(2, 1999)
	if burst.peak != 2 || burst.approximate {
		t.Fatal("independent logs combined")
	}
}

func TestIOOverallRollingPeakRestoresCompletionOrder(t *testing.T) {
	for seed := int64(0); seed < 32; seed++ {
		rng := rand.New(rand.NewSource(seed))
		var a ioAnalysisAccumulator
		var want uint64
		for log := uint64(1); log <= 2; log++ {
			times := make([]uint64, 100)
			for i := range times {
				times[i] = uint64(rng.Intn(5000))*1000 + uint64(rng.Intn(1000))
			}
			for _, end := range times {
				a.add(&jhlog.IOEvent{DurationUS: uint64(rng.Intn(int(end + 1)))}, 0, log, end)
			}
			sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
			for i, start := range times {
				count := uint64(0)
				for _, end := range times[i:] {
					if end/1000-start/1000 < 1000 {
						count++
					}
				}
				want = max(want, count)
			}
		}
		got := a.finalize(nil)
		if got.PeakOperationsPerSecond != want || got.BurstEstimateStatus != "exact_rolling_second" {
			t.Fatalf("seed%d: %+v want%d", seed, got, want)
		}
	}
}

func TestDatabaseRollingPeakPrecisionAtEveryLevel(t *testing.T) {
	for _, disorder := range []bool{false, true} {
		log := jhlog.Log{Dict: map[uint64]string{1: "SELECT value FROM samples"}}
		times := []uint64{999, 1001, 1001}
		if disorder {
			times = []uint64{2000, 999, 1001}
		}
		for _, at := range times {
			log.Events = append(log.Events, jhlog.Event{Type: jhlog.EventDatabase, TimeMS: at, Database: &jhlog.DatabaseEvent{QueryRef: jhlog.LocalSymbol(1), Operation: jhlog.DatabaseOperationQuery, Outcome: jhlog.DatabaseOutcomeSuccess, DurationUS: 1000}})
		}
		summary := inspectLogsForTest("db peak", []jhlog.Log{log})
		db := summary.DatabaseAnalysis
		want := "exact_rolling_second"
		if disorder {
			want = "lower_bound_rolling_second"
		}
		if db == nil || len(db.Statements) != 1 || len(db.Statements[0].Contexts) != 1 {
			t.Fatalf("missing DB groups: %+v", db)
		}
		for _, status := range []string{db.BurstEstimateStatus, db.Statements[0].BurstEstimateStatus, db.Statements[0].Contexts[0].BurstEstimateStatus} {
			if status != want {
				t.Fatalf("status=%s want%s", status, want)
			}
		}
		if (len(rollingPeakWarnings(summary)) > 0) != disorder {
			t.Fatal("precision warning mismatch")
		}
	}
}

func TestRollingPeakEvictedHistoryCannotClaimExact(t *testing.T) {
	var burst routeBurstAccumulator
	burst.add(1, 999)
	if burst.statusWithMissing(true) != "lower_bound_rolling_second" {
		t.Fatal("missing group history claims exact")
	}
	if (&routeBurstAccumulator{}).status() != "unknown" {
		t.Fatal("no observations claim exact")
	}
}

func TestRollingPeakContinuesAcrossVerifiedRotation(t *testing.T) {
	dir := t.TempDir()
	paths := []string{filepath.Join(dir, "first.jhlog"), filepath.Join(dir, "second.jhlog")}
	header := collectionTestHeader(21, 0)
	for i, path := range paths {
		file, writer, err := jhlog.CreateWithHeader(path, header)
		if err != nil {
			t.Fatal(err)
		}
		if err = writer.WriteEvent(jhlog.Event{Type: jhlog.EventDictionary, Dictionary: &jhlog.DictionaryEntry{ID: 1, Kind: jhlog.DictGeneric, Value: "GET /peak"}}); err != nil {
			t.Fatal(err)
		}
		for j := 0; j < 15; j++ {
			at := uint64(999 + i*2)
			for _, event := range []jhlog.Event{
				{Type: jhlog.EventHTTP, TimeMS: at, HTTP: &jhlog.HTTPEvent{RouteRef: jhlog.LocalSymbol(1), DurationMS: 1, StatusCode: 200}},
				{Type: jhlog.EventIO, TimeMS: at, IO: &jhlog.IOEvent{Operation: jhlog.IOOperationFileRead, Outcome: jhlog.IOOutcomeSuccess, DurationUS: 1000}},
				{Type: jhlog.EventDatabase, TimeMS: at, Database: &jhlog.DatabaseEvent{SourceRef: jhlog.LocalSymbol(1), Framework: jhlog.DatabaseFrameworkSQLite, Boundary: jhlog.DatabaseBoundaryMaterialize, Operation: jhlog.DatabaseOperationQuery, Outcome: jhlog.DatabaseOutcomeSuccess, DurationUS: 1000}},
			} {
				if err = writer.WriteEvent(event); err != nil {
					t.Fatal(err)
				}
			}
		}
		reason := jhlog.SegmentEndNormal
		if i == 0 {
			reason = jhlog.SegmentEndRotation
		}
		if err = writer.CloseWithReason(reason); err != nil {
			t.Fatal(err)
		}
		header.SegmentIndex++
		header.PreviousSegmentDigest = writer.SegmentDigest()
		if err = file.Close(); err != nil {
			t.Fatal(err)
		}
	}
	for _, input := range [][]string{paths, {paths[1], paths[0]}} {
		summary, err := inspectFilesForTest("rotation peak", input)
		if err != nil {
			t.Fatal(err)
		}
		if len(summary.Routes) != 1 || summary.Routes[0].PeakRequestsPerSecond != 30 || summary.IOAnalysis.PeakOperationsPerSecond != 30 || summary.DatabaseAnalysis.PeakCallsPerSecond != 30 {
			t.Fatal("verified rotation split a rolling peak")
		}
	}
}

func TestZeroDurationHTTPHasCompletionButNoConcurrency(t *testing.T) {
	peak, _ := maxHTTPConcurrency([]httpInterval{{logIndex: 1, startMS: 999, endMS: 999}, {logIndex: 1, startMS: 1001, endMS: 1001}})
	if peak != 0 {
		t.Fatalf("empty intervals create concurrency=%d", peak)
	}
}

func TestZeroDurationIOHasNoConcurrency(t *testing.T) {
	peak, _ := maxIOConcurrency([]ioInterval{{logIndex: 1, startUS: 999000, endUS: 999000}})
	if peak != 0 {
		t.Fatalf("empty IO interval creates concurrency=%d", peak)
	}
}

func TestStreamingBurstPrecisionReachesIOJSON(t *testing.T) {
	var a ioAggregate
	for _, at := range []uint64{2000, 999, 1001} {
		a.add(&jhlog.IOEvent{DurationUS: 1000}, 0, 1, at)
	}
	data, err := json.Marshal(a.finalize())
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if raw["BurstEstimateStatus"] != "lower_bound_rolling_second" {
		t.Fatalf("IO precision lost: %v", raw["BurstEstimateStatus"])
	}
}

func TestIOBurstRepeatedTimestampStorageIsBounded(t *testing.T) {
	var burst ioBurstAccumulator
	for i := 0; i < 100000; i++ {
		burst.add(1, 1000)
	}
	if burst.peak != 100000 {
		t.Fatal("same-time events lost")
	}
	if len(burst.buckets) > 1000 {
		t.Fatalf("one timestamp stored %d individual entries", len(burst.buckets))
	}
}
