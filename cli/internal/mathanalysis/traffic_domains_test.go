package mathanalysis

import (
	"fmt"
	"math"
	"path/filepath"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
	"github.com/i-redbyte/jank-hunter/cli/internal/traffic"
)

type trafficSource struct {
	run, process byte
	uid, flags   uint64
	values       []uint64
}

func writeUIDTraffic(t testing.TB, source trafficSource) string {
	t.Helper()
	header := jhlog.DefaultSegmentHeader()
	header.RunID = jhlog.ID128{source.run}
	header.ProcessInstanceID = jhlog.ID128{source.process}
	header.SessionID = jhlog.ID128{source.process}
	path := filepath.Join(t.TempDir(), fmt.Sprintf("traffic-%d-%d.jhlog", source.run, source.process))
	file, writer, err := jhlog.CreateWithHeader(path, header)
	if err != nil {
		t.Fatal(err)
	}
	for index, value := range source.values {
		if err := writer.WriteEvent(jhlog.Event{Type: jhlog.EventContext, TimeMS: 1000 + uint64(index)*1000,
			Context: &jhlog.ContextEvent{TrafficUIDPlusOne: uint32(source.uid), TrafficKnownFlags: uint8(source.flags), RxBytes: value, TxBytes: value}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestUIDTrafficSummaryAndTimelineUseSameUnion(t *testing.T) {
	for _, tc := range []struct {
		name    string
		sources []trafficSource
		want    uint64
		exact   bool
	}{
		{"overlap", []trafficSource{{1, 1, 10001, 3, []uint64{100, 1100}}, {1, 2, 10001, 3, []uint64{400, 1400}}}, 1300, true},
		{"disjoint", []trafficSource{{1, 1, 10001, 3, []uint64{100, 200}}, {1, 2, 10001, 3, []uint64{300, 400}}}, 200, true},
		{"different_UID", []trafficSource{{1, 1, 10001, 3, []uint64{100, 1100}}, {1, 2, 10002, 3, []uint64{100, 1100}}}, 2000, true},
		{"different_run", []trafficSource{{1, 1, 10001, 3, []uint64{100, 1100}}, {2, 2, 10001, 3, []uint64{100, 1100}}}, 2000, true},
		{"unknown_UID", []trafficSource{{1, 1, 10001, 3, []uint64{100, 1100}}, {1, 2, 0, 0, []uint64{400, 1400}}}, 1300, false},
		{"supported_zero", []trafficSource{{1, 1, 10001, 3, []uint64{0, 0}}}, 0, true},
		{"unsupported_zero", []trafficSource{{1, 1, 10001, 0, []uint64{0, 0}}}, 0, false},
		{"reset", []trafficSource{{1, 1, 10001, 3, []uint64{100, 300, 100, 200}}}, 200, false},
		{"overflow", []trafficSource{{1, 1, 10001, 3, []uint64{0, math.MaxUint64}}, {1, 2, 10002, 3, []uint64{0, math.MaxUint64}}}, math.MaxUint64, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var paths []string
			for _, source := range tc.sources {
				paths = append(paths, writeUIDTraffic(t, source))
			}
			for reverse := 0; reverse < 2; reverse++ {
				if reverse == 1 {
					for i, j := 0, len(paths)-1; i < j; i, j = i+1, j-1 {
						paths[i], paths[j] = paths[j], paths[i]
					}
				}
				summary, err := analyze.InspectFilesWithOptions(tc.name, paths, analyze.Options{})
				if err != nil {
					t.Fatal(err)
				}
				inputs, err := analyzeMathInputs(paths, analyze.Options{})
				if err != nil {
					t.Fatal(err)
				}
				var rx, tx uint64
				for _, bucket := range inputs.Timeline {
					rx = saturatingAddUint64(rx, bucket.TrafficRxBytes)
					tx = saturatingAddUint64(tx, bucket.TrafficTxBytes)
					if !tc.exact && (bucket.TrafficRXKnown || bucket.TrafficTXKnown) {
						t.Fatal("unknown evidence entered exact traffic series")
					}
				}
				if summary.TrafficRxMax != tc.want || summary.TrafficTxMax != tc.want || rx != tc.want || tx != tc.want {
					t.Fatalf("summary=%d/%d timeline=%d/%d want=%d", summary.TrafficRxMax, summary.TrafficTxMax, rx, tx, tc.want)
				}
				e := summary.CollectionQuality.Traffic
				if e == nil || e.Exact(0) != tc.exact || e.Exact(1) != tc.exact {
					t.Fatalf("quality=%+v want exact=%v", e, tc.exact)
				}
				for _, delta := range analyze.Compare(summary, summary).Deltas {
					if (delta.Name == "UID RX delta" || delta.Name == "UID TX delta") && delta.Comparable != tc.exact {
						t.Fatalf("comparison=%+v", delta)
					}
				}
			}
		})
	}
}

func TestUIDTrafficKnownnessIsIndependentPerDirection(t *testing.T) {
	path := writeUIDTraffic(t, trafficSource{1, 1, 10001, 2, []uint64{0, 100}})
	summary, err := analyze.InspectFilesWithOptions("RX unavailable", []string{path}, analyze.Options{})
	if err != nil {
		t.Fatal(err)
	}
	inputs, err := analyzeMathInputs([]string{path}, analyze.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if summary.CollectionQuality.Traffic.Exact(0) || !summary.CollectionQuality.Traffic.Exact(1) || summary.TrafficRxMax != 0 || summary.TrafficTxMax != 100 {
		t.Fatalf("summary=%+v", summary.CollectionQuality.Traffic)
	}
	if hasSeries(inputs.Series, "Дельта RX трафика") || !hasSeries(inputs.Series, "Дельта TX трафика") {
		t.Fatal("one unsupported counter disabled the other direction or became measured zero")
	}
}

func TestUIDTrafficStorageUsesSharedMathBudget(t *testing.T) {
	budget := newCollectionBudget(1_000)
	other := budget.account("other collection")
	if !other.reserve(900) {
		t.Fatal("setup")
	}
	collector := timelineTraffic{account: budget.account("UID traffic intervals")}
	collector.append(0, traffic.Interval{Low: 1, High: 2})
	if budget.err() == nil || len(collector.ranges[0]) != 0 {
		t.Fatal("traffic allocated outside the shared budget")
	}
	collector.account.close()
	other.close()
	if budget.used != 0 {
		t.Fatalf("reserved=%d", budget.used)
	}
}
