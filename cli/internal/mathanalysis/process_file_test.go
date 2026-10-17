package mathanalysis

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func TestProcessFileMatchesIndependentEpochMath(t *testing.T) {
	root := t.TempDir()
	var process bytes.Buffer
	process.Write(jhlog.ProcessMagic)
	var paths []string
	for epoch := byte(1); epoch <= 3; epoch++ {
		var raw bytes.Buffer
		header := jhlog.DefaultSegmentHeader()
		header.RunID[0], header.ProcessInstanceID[0], header.SessionID[0] = 1, 2, epoch
		writer, err := jhlog.NewWriterWithOptions(&raw, jhlog.WriterOptions{Header: header})
		if err != nil {
			t.Fatal(err)
		}
		if err := writer.WriteEvent(jhlog.Event{Type: jhlog.EventDictionary, Dictionary: &jhlog.DictionaryEntry{Kind: jhlog.DictGeneric, ID: 1, Value: fmt.Sprintf("GET /epoch%d", epoch)}}); err != nil {
			t.Fatal(err)
		}
		for i := uint64(0); i < 5; i++ {
			if err := writer.WriteEvent(jhlog.Event{Type: jhlog.EventHTTP, TimeMS: uint64(epoch)*10000 + i*1000, HTTP: &jhlog.HTTPEvent{RouteRef: jhlog.LocalSymbol(1), DurationMS: uint64(epoch) * 100, Status: jhlog.Status2xx}}); err != nil {
				t.Fatal(err)
			}
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(root, fmt.Sprintf("epoch%d.jhlog", epoch))
		if err := os.WriteFile(path, raw.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
		process.Write(raw.Bytes())
	}
	path := filepath.Join(root, "process.jhlog")
	if err := os.WriteFile(path, process.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	raw, err := analyzeMathInputs(paths, analyze.Options{})
	if err != nil {
		t.Fatal(err)
	}
	wrapped, err := analyzeMathInputs([]string{path}, analyze.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(raw.Timeline, wrapped.Timeline) || !reflect.DeepEqual(raw.Series, wrapped.Series) ||
		!reflect.DeepEqual(raw.RouteDefinitions, wrapped.RouteDefinitions) ||
		!reflect.DeepEqual(raw.NetworkLoops, wrapped.NetworkLoops) || raw.Scale != wrapped.Scale || raw.TimelineGroups != wrapped.TimelineGroups {
		t.Fatal("process container changed timeline, series, routes, network loops or scale")
	}
	if len(raw.RobustSamples) != len(wrapped.RobustSamples) {
		t.Fatal("robust populations differ")
	}
	for key, before := range raw.RobustSamples {
		after, ok := wrapped.RobustSamples[key]
		if !ok {
			t.Fatalf("missing robust sample %v", key)
		}
		a, b := *before, *after
		a.account, b.account = nil, nil
		if !reflect.DeepEqual(a, b) {
			t.Fatalf("robust sample differs: %v", key)
		}
	}
}
