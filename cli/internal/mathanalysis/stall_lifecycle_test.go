package mathanalysis

import (
	"path/filepath"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func TestMathCountsStallLifecycleOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stall.jhlog")
	closer, writer, err := jhlog.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteEvent(jhlog.Event{Type: jhlog.EventDictionary, Dictionary: &jhlog.DictionaryEntry{Kind: jhlog.DictOwner, ID: 1, Value: "example.Screen"}}); err != nil {
		t.Fatal(err)
	}
	for _, state := range []jhlog.StallState{jhlog.StallStateOngoing, jhlog.StallStateRecovered} {
		err := writer.WriteEvent(jhlog.Event{Type: jhlog.EventStall, TimeMS: uint64(state) * 200,
			Attribution: jhlog.AttributionContext{Present: true, Owner: jhlog.LocalSymbol(1)},
			Stall:       &jhlog.StallEvent{IncidentID: 1, State: state, DurationMS: uint64(state) * 200},
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := closer.Close(); err != nil {
		t.Fatal(err)
	}
	result, err := analyzeMathInputs([]string{path}, analyze.Options{})
	if err != nil {
		t.Fatal(err)
	}
	var count int
	for _, bucket := range result.Timeline {
		count += bucket.StallCount
	}
	if count != 1 {
		t.Fatalf("math timeline counted one incident %d times", count)
	}
}

func TestStallLifecycleSurvivesRotationInSummaryAndMath(t *testing.T) {
	for _, completed := range []bool{false, true} {
		paths := writeRotatedStallFixture(t, completed)
		result, err := analyzeMathInputs(paths, analyze.Options{})
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for _, bucket := range result.Timeline {
			count += bucket.StallCount
		}
		if count != 1 {
			t.Fatalf("completed=%v timeline count=%d", completed, count)
		}
		samples := 0
		for key, set := range result.RobustSamples {
			if key.Metric == "Пауза главного потока" {
				if key.Name != "old.Owner" {
					t.Fatalf("rotated dictionary changed stall owner: %s", key.Name)
				}
				samples += set.seen
			}
		}
		if samples != 1 {
			t.Fatalf("completed=%v robust samples=%d", completed, samples)
		}
		summary, err := analyze.InspectFilesWithOptions("rotated", paths, analyze.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if summary.StallCount != 1 {
			t.Fatalf("summary counted %d stalls", summary.StallCount)
		}
		if completed && summary.StallStates.Recovered != 1 || !completed && summary.StallStates.Ongoing != 1 {
			t.Fatalf("completed=%v states=%+v", completed, summary.StallStates)
		}
	}
}

func writeRotatedStallFixture(t *testing.T, completed bool) []string {
	t.Helper()
	directory := t.TempDir()
	header := jhlog.DefaultSegmentHeader()
	header.RunID[0] = 1
	header.ProcessInstanceID[0] = 2
	header.SessionID[0] = 3
	paths := []string{filepath.Join(directory, "part0.jhlog"), filepath.Join(directory, "part1.jhlog")}
	for index, path := range paths {
		header.SegmentIndex = uint64(index)
		closer, writer, err := jhlog.CreateWithHeader(path, header)
		if err != nil {
			t.Fatal(err)
		}
		owner := "old.Owner"
		if index == 1 && !completed {
			owner = "unrelated.Owner"
		}
		if err := writer.WriteEvent(jhlog.Event{Type: jhlog.EventDictionary, Dictionary: &jhlog.DictionaryEntry{Kind: jhlog.DictOwner, ID: 1, Value: owner}}); err != nil {
			t.Fatal(err)
		}
		if index == 0 || completed {
			state := jhlog.StallStateOngoing
			if index == 1 {
				state = jhlog.StallStateRecovered
			}
			if err := writer.WriteEvent(jhlog.Event{Type: jhlog.EventStall, TimeMS: uint64(index+1) * 200,
				Attribution: jhlog.AttributionContext{Present: true, Owner: jhlog.LocalSymbol(1)},
				Stall:       &jhlog.StallEvent{IncidentID: 17, State: state, DurationMS: uint64(index+1) * 200},
			}); err != nil {
				t.Fatal(err)
			}
		}
		if index == 0 {
			if err := writer.CloseWithReason(jhlog.SegmentEndRotation); err != nil {
				t.Fatal(err)
			}
			header.PreviousSegmentDigest = writer.SegmentDigest()
		}
		if err := closer.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return []string{paths[1], paths[0]}
}
