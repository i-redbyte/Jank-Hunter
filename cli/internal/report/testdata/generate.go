//go:build ignore

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
	"github.com/i-redbyte/jank-hunter/cli/internal/report"
)

type fixtureSpec struct {
	name       string
	incomplete bool
}

func main() {
	specs := []fixtureSpec{
		{name: "runtime-graph-contextual-100"},
		{name: "runtime-graph-incomplete-100", incomplete: true},
	}
	for _, spec := range specs {
		if err := generate(spec); err != nil {
			panic(err)
		}
	}
}

func generate(spec fixtureSpec) error {
	base := filepath.Join("testdata", spec.name)
	if err := writeLog(base+".jhlog", spec.incomplete); err != nil {
		return err
	}
	summary, err := analyze.InspectFilesWithOptions(spec.name, []string{base + ".jhlog"}, analyze.Options{})
	if err != nil {
		return err
	}
	payload, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(base+".golden.json", append(payload, '\n'), 0o644); err != nil {
		return err
	}
	if err := report.WriteInspectWithOptions(base+".golden.html", summary, report.ReportOptions{
		GeneratedAt: "2026-08-08T00:00:00Z",
	}); err != nil {
		return err
	}
	html, err := os.ReadFile(base + ".golden.html")
	if err != nil {
		return err
	}
	return os.WriteFile(base+".golden.html", normalizeHTML(html), 0o644)
}

func normalizeHTML(payload []byte) []byte {
	lines := bytes.Split(payload, []byte{'\n'})
	for index := range lines {
		lines[index] = bytes.TrimRight(lines[index], " \t")
	}
	return bytes.Join(lines, []byte{'\n'})
}

func writeLog(path string, incomplete bool) (result error) {
	header := jhlog.DefaultSegmentHeader()
	header.RunID[0], header.ProcessInstanceID[0], header.SessionID[0] = 1, 2, 3
	header.OSPID = 42
	header.IdentitySource = 1
	header.ProcessName = "main"
	closer, writer, err := jhlog.CreateWithHeader(path, header)
	if err != nil {
		return err
	}
	defer func() {
		if err := closer.Close(); result == nil && err != nil {
			result = fmt.Errorf("close fixture: %w", err)
		}
	}()
	entries := []jhlog.DictionaryEntry{
		{Kind: jhlog.DictAppVersion, ID: 1, Value: "1.0"},
		{Kind: jhlog.DictBuild, ID: 2, Value: "1"},
		{Kind: jhlog.DictDevice, ID: 3, Value: "fixture"},
		{Kind: jhlog.DictProcess, ID: 4, Value: "main"},
		{Kind: jhlog.DictScreen, ID: 10, Value: "Feed"},
		{Kind: jhlog.DictScreen, ID: 11, Value: "Checkout"},
		{Kind: jhlog.DictOwner, ID: 20, Value: "Caller.run"},
		{Kind: jhlog.DictOwner, ID: 21, Value: "Callee.load"},
		{Kind: jhlog.DictGeneric, ID: 30, Value: "open"},
		{Kind: jhlog.DictGeneric, ID: 31, Value: "network"},
		{Kind: jhlog.DictGeneric, ID: 32, Value: "render"},
	}
	for index := range entries {
		entry := entries[index]
		if err := writer.WriteEvent(jhlog.Event{Type: jhlog.EventDictionary, Dictionary: &entry}); err != nil {
			return err
		}
	}
	if err := writer.WriteEvent(jhlog.Event{
		Type: jhlog.EventSession, TimeMS: 1,
		Session: &jhlog.SessionEvent{AppVersionRef: jhlog.LocalSymbol(1), BuildRef: jhlog.LocalSymbol(2), DeviceRef: jhlog.LocalSymbol(3), SDKInt: 35},
	}); err != nil {
		return err
	}
	for _, event := range []jhlog.Event{
		runtimeCall(10, 10, 1, 10, 10),
		runtimeCall(20, 11, 1, 20, 20),
	} {
		if err := writer.WriteEvent(event); err != nil {
			return err
		}
	}
	quality := map[uint64]uint64{
		jhlog.QualityRuntimeGraphInputTotal:   2,
		jhlog.QualityRuntimeGraphEmittedTotal: 2,
	}
	if incomplete {
		quality[jhlog.QualityRuntimeGraphInputTotal] = 10
		quality[jhlog.QualityRuntimeGraphWriterRejectionLoss] = 8
	}
	writer.SetQualitySnapshot(jhlog.QualitySnapshot{Sequence: 1, Counters: quality})
	return nil
}

func runtimeCall(timeMS, screen, count, total, max uint64) jhlog.Event {
	return jhlog.Event{
		Type:   jhlog.EventRuntimeCall,
		TimeMS: timeMS,
		Attribution: jhlog.AttributionContext{
			Present: true, Screen: jhlog.LocalSymbol(screen), Owner: jhlog.LocalSymbol(20),
		},
		RuntimeCall: &jhlog.RuntimeCallEvent{
			CalleeRef: jhlog.LocalSymbol(21), Count: count, TotalMS: total, MaxMS: max,
		},
	}
}
