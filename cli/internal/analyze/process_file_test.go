package analyze

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func TestInspectProcessFileAnalyzesEveryEpochOnce(t *testing.T) {
	var buffer bytes.Buffer
	buffer.Write(jhlog.ProcessMagic)
	for epoch := byte(1); epoch <= 3; epoch++ {
		header := jhlog.DefaultSegmentHeader()
		header.RunID[0], header.ProcessInstanceID[0], header.SessionID[0] = 1, 2, epoch
		header.ProcessName = "main"
		writer, err := jhlog.NewWriterWithOptions(&buffer, jhlog.WriterOptions{Header: header})
		if err != nil {
			t.Fatal(err)
		}
		for _, event := range []jhlog.Event{
			{Type: jhlog.EventDictionary, Dictionary: &jhlog.DictionaryEntry{Kind: jhlog.DictMetric, ID: 1, Value: fmt.Sprintf("epoch.%d", epoch)}},
			{Type: jhlog.EventCounter, TimeMS: uint64(epoch) * 1000, Metric: &jhlog.MetricEvent{MetricRef: jhlog.LocalSymbol(1), Value: uint64(epoch)}},
		} {
			if err := writer.WriteEvent(event); err != nil {
				t.Fatal(err)
			}
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(t.TempDir(), "process.jhlog")
	if err := os.WriteFile(path, buffer.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	summary, err := InspectFilesWithOptions("one process", []string{path}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if summary.LogCount != 1 || summary.EventCount != 3 || len(summary.CollectionSegments) != 3 {
		t.Fatalf("process file lost or duplicated epochs: logs=%d events=%d segments=%d", summary.LogCount, summary.EventCount, len(summary.CollectionSegments))
	}
	values := namedValuesByName(summary.Counters)
	for epoch := 1; epoch <= 3; epoch++ {
		name := fmt.Sprintf("epoch.%d", epoch)
		if values[name].Value != uint64(epoch) {
			t.Fatalf("counter %s = %+v", name, values[name])
		}
	}
}

func TestProcessSnapshotFrontierDoesNotClaimMissingPhysicalSegment(t *testing.T) {
	header := jhlog.DefaultSegmentHeader()
	header.ProcessName = "main"
	header.RunID[0], header.ProcessInstanceID[0], header.SessionID[0] = 1, 2, 3
	var raw bytes.Buffer
	writer, err := jhlog.NewWriterWithOptions(&raw, jhlog.WriterOptions{Header: header})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.CloseWithReason(jhlog.SegmentEndRotation); err != nil {
		t.Fatal(err)
	}
	for _, wrapped := range []bool{false, true} {
		data := raw.Bytes()
		if wrapped {
			data = append(append([]byte{}, jhlog.ProcessMagic...), data...)
		}
		path := filepath.Join(t.TempDir(), "snapshot.jhlog")
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		summary, err := InspectFilesWithOptions("snapshot", []string{path}, Options{})
		if err != nil {
			t.Fatal(err)
		}
		if summary.CollectionQuality.ChainValid != wrapped {
			t.Fatalf("wrapped=%v: chain valid=%v, reasons=%v", wrapped, summary.CollectionQuality.ChainValid, summary.CollectionQuality.Reasons)
		}
	}
}
