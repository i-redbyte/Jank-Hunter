package jhlog

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func processTestSegment(t *testing.T, session, process byte) []byte {
	t.Helper()
	header := DefaultSegmentHeader()
	header.RunID[0], header.ProcessInstanceID[0], header.SessionID[0] = 1, process, session
	header.ProcessName = "main"
	var buffer bytes.Buffer
	writer, err := NewWriterWithOptions(&buffer, WriterOptions{Header: header})
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range []Event{
		{Type: EventDictionary, Dictionary: &DictionaryEntry{Kind: DictMetric, ID: 1, Value: fmt.Sprintf("epoch.%d", session)}},
		{Type: EventCounter, TimeMS: uint64(session) * 1000, Metric: &MetricEvent{MetricRef: LocalSymbol(1), Value: uint64(session)}},
	} {
		if err := writer.WriteEvent(event); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func TestProcessFileKeepsEpochRangesAndQualityIndependent(t *testing.T) {
	first, second := processTestSegment(t, 1, 2), processTestSegment(t, 3, 2)
	path := filepath.Join(t.TempDir(), "process.jhlog")
	payload := append(append(append([]byte{}, ProcessMagic...), first...), second...)
	if err := os.WriteFile(path, payload, 0600); err != nil {
		t.Fatal(err)
	}
	segments, err := ReadFileSegments(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(segments) != 2 {
		t.Fatalf("segments = %d", len(segments))
	}
	if segments[0].Offset != int64(len(ProcessMagic)) || segments[1].Offset != int64(len(ProcessMagic)+len(first)) {
		t.Fatalf("ranges do not reference original bytes: %+v", segments)
	}
	for index, segment := range segments {
		result, err := StreamFileSegment(path, segment, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !result.Sealed || result.LatestQuality == nil || result.Header.SessionID != segment.Header.SessionID {
			t.Fatalf("epoch %d lost its own quality: %+v", index, result)
		}
	}
	values := map[string]uint64{}
	result, err := StreamFileWithResult(path, func(event Event, dict map[uint64]string) error {
		if event.Metric != nil {
			values[dict[event.Metric.MetricRef.ID]] += event.Metric.Value
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if values["epoch.1"] != 1 || values["epoch.3"] != 3 || result.Events != 2 || len(result.Segments) != 2 || result.LatestQuality != nil {
		t.Fatalf("process stream mixed epoch dictionaries or quality: values=%v result=%+v", values, result)
	}
}

func TestProcessFileRejectsForeignProcessAndJunkAfterSealedEpoch(t *testing.T) {
	first := processTestSegment(t, 1, 2)
	for name, tail := range map[string][]byte{
		"foreign process": processTestSegment(t, 3, 4),
		"junk":            []byte("unrelated bytes after a sealed epoch"),
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "process.jhlog")
			data := append(append(append([]byte{}, ProcessMagic...), first...), tail...)
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := ReadFileSegments(path); err == nil {
				t.Fatal("invalid process container accepted")
			}
		})
	}
}

func TestProcessFilePreservesUncommittedTailAndRawFileCompatibility(t *testing.T) {
	first, second := processTestSegment(t, 1, 2), processTestSegment(t, 3, 2)
	for _, wrapped := range []bool{false, true} {
		path := filepath.Join(t.TempDir(), "process.jhlog")
		data := append([]byte{}, first...)
		if wrapped {
			data = append(append(append([]byte{}, ProcessMagic...), first...), second[:len(second)-3]...)
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		segments, err := ReadFileSegments(path)
		if err != nil {
			t.Fatal(err)
		}
		if wrapped && len(segments) != 2 || !wrapped && len(segments) != 1 {
			t.Fatalf("segments = %d", len(segments))
		}
		result, err := StreamFileSegment(path, segments[len(segments)-1], nil)
		if err != nil {
			t.Fatal(err)
		}
		if wrapped && (result.Status != SegmentStatusOpenWithTail || result.TailBytes == 0) {
			t.Fatalf("incomplete tail was lost: %+v", result)
		}
	}
}

func TestProcessFileKeepsSealedDataWhenNextEpochHeaderIsTorn(t *testing.T) {
	first, second := processTestSegment(t, 1, 2), processTestSegment(t, 3, 2)
	for _, length := range []int{1, magicSize - 1, magicSize + 2, magicSize + 10} {
		path := filepath.Join(t.TempDir(), "process.jhlog")
		data := append(append(append([]byte{}, ProcessMagic...), first...), second[:length]...)
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		result, err := StreamFileWithResult(path, nil)
		if err != nil {
			t.Fatalf("tail length %d: %v", length, err)
		}
		if result.Events != 1 || result.TailBytes != uint64(length) || result.Sealed || result.Status != SegmentStatusOpenWithTail {
			t.Fatalf("tail length %d: lost sealed data or tail: %+v", length, result)
		}
	}
}

func TestProcessFileIndexedRangeIgnoresLaterAppends(t *testing.T) {
	first, second := processTestSegment(t, 1, 2), processTestSegment(t, 3, 2)
	path := filepath.Join(t.TempDir(), "process.jhlog")
	payload := append(append([]byte{}, ProcessMagic...), first...)
	if err := os.WriteFile(path, payload, 0600); err != nil {
		t.Fatal(err)
	}
	ranges, err := ReadFileSegments(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(payload, second...), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := StreamFileSegment(path, ranges[0], nil)
	if err != nil || result.Events != 1 || !result.Sealed {
		t.Fatalf("snapshot included later bytes: %+v %v", result, err)
	}
	if err := os.WriteFile(path, append(append([]byte{}, ProcessMagic...), second...), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := StreamFileSegment(path, ranges[0], nil); err == nil {
		t.Fatal("changed segment identity accepted")
	}
}
