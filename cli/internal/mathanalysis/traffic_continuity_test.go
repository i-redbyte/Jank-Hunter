package mathanalysis

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func writeTrafficSegments(t testing.TB, process byte, indices []uint64, samples [][]uint64) []string {
	t.Helper()
	var paths []string
	var previous []byte
	for i, index := range indices {
		header := jhlog.DefaultSegmentHeader()
		header.RunID = jhlog.ID128{1}
		header.ProcessInstanceID = jhlog.ID128{process}
		header.SessionID = jhlog.ID128{process}
		header.SegmentIndex = index
		header.PreviousSegmentDigest = previous
		path := filepath.Join(t.TempDir(), fmt.Sprintf("process%d-segment%d.jhlog", process, index))
		file, writer, err := jhlog.CreateWithHeader(path, header)
		if err != nil {
			t.Fatal(err)
		}
		for j, value := range samples[i] {
			event := jhlog.Event{Type: jhlog.EventContext, TimeMS: 1000 + uint64(i+j)*1000, Context: &jhlog.ContextEvent{RxBytes: value, TxBytes: value * 2, AvailMemoryKB: 1000}}
			if err := writer.WriteEvent(event); err != nil {
				t.Fatal(err)
			}
		}
		reason := jhlog.SegmentEndNormal
		if i+1 < len(indices) {
			reason = jhlog.SegmentEndRotation
		}
		if err := writer.CloseWithReason(reason); err != nil {
			t.Fatal(err)
		}
		previous = writer.SegmentDigest()
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
	}
	return paths
}

func TestTrafficSamplesContinueOnlyAcrossCompatibleSegments(t *testing.T) {
	for _, tc := range []struct {
		name    string
		indices []uint64
		want    uint64
	}{{"continuous", []uint64{0, 1}, 1000}, {"missing_segment", []uint64{0, 2}, 0}} {
		t.Run(tc.name, func(t *testing.T) {
			paths := writeTrafficSegments(t, 1, tc.indices, [][]uint64{{100}, {1100}})
			for _, reverse := range []bool{false, true} {
				if reverse {
					paths[0], paths[1] = paths[1], paths[0]
				}
				summary, err := analyze.InspectFilesWithOptions("traffic", paths, analyze.Options{})
				if err != nil {
					t.Fatal(err)
				}
				inputs, err := analyzeMathInputs(paths, analyze.Options{})
				if err != nil {
					t.Fatal(err)
				}
				var rx, tx uint64
				for _, bucket := range inputs.Timeline {
					rx += bucket.TrafficRxBytes
					tx += bucket.TrafficTxBytes
				}
				if rx != tc.want || tx != tc.want*2 || summary.TrafficRxMax != tc.want || summary.TrafficTxMax != tc.want*2 {
					t.Errorf("reverse=%v summary RX/TX=%d/%d math=%d/%d, want=%d/%d", reverse, summary.TrafficRxMax, summary.TrafficTxMax, rx, tx, tc.want, tc.want*2)
				}
			}
		})
	}
}

func TestUIDTrafficWithoutProvenanceCannotClaimExactComparison(t *testing.T) {
	paths := append(writeTrafficSegments(t, 1, []uint64{0}, [][]uint64{{100, 1100}}), writeTrafficSegments(t, 2, []uint64{0}, [][]uint64{{100, 1100}})...)
	summary, err := analyze.InspectFilesWithOptions("unknown UID", paths, analyze.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, delta := range analyze.Compare(summary, summary).Deltas {
		if (delta.Name == "UID RX delta" || delta.Name == "UID TX delta") && delta.Comparable {
			t.Errorf("UID is not recorded, overlapping process counters claimed exact: %s=%s, RX=%d/TX=%d", delta.Name, delta.Baseline, summary.TrafficRxMax, summary.TrafficTxMax)
		}
	}
}
