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
	contextual bool
	incomplete bool
	agent      bool
}

func main() {
	specs := []fixtureSpec{
		{name: "runtime-graph-legacy-v9"},
		{name: "runtime-graph-buffered-v9", contextual: true},
		{name: "runtime-graph-incomplete-v9", contextual: true, incomplete: true},
		{name: "art-ti-evidence-v9", contextual: true, agent: true},
	}
	for _, spec := range specs {
		if err := generate(spec); err != nil {
			panic(err)
		}
	}
}

func generate(spec fixtureSpec) error {
	base := filepath.Join("testdata", spec.name)
	if err := writeLog(base+".jhlog", spec.contextual, spec.incomplete, spec.agent); err != nil {
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

func writeLog(path string, contextual, incomplete, agent bool) (result error) {
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
		{Kind: jhlog.DictFlow, ID: 30, Value: "open"},
		{Kind: jhlog.DictStep, ID: 31, Value: "network"},
		{Kind: jhlog.DictStep, ID: 32, Value: "render"},
	}
	for index := range entries {
		entry := entries[index]
		if err := writer.WriteEvent(jhlog.Event{Type: jhlog.EventDictionary, Dictionary: &entry}); err != nil {
			return err
		}
	}
	if err := writer.WriteEvent(jhlog.Event{
		Type: jhlog.EventSession, TimeMS: 1,
		Session: &jhlog.SessionEvent{AppVersionID: 1, BuildID: 2, DeviceID: 3, ProcessID: 4, SDKInt: 35},
	}); err != nil {
		return err
	}
	if contextual {
		for _, event := range []jhlog.Event{
			{Type: jhlog.EventRuntimeCall, TimeMS: 10, RuntimeCall: runtimeCall(10, 31, 1, 10, 10)},
			{Type: jhlog.EventRuntimeCall, TimeMS: 20, RuntimeCall: runtimeCall(11, 32, 1, 20, 20)},
		} {
			if err := writer.WriteEvent(event); err != nil {
				return err
			}
		}
	} else if err := writer.WriteEvent(jhlog.Event{
		Type: jhlog.EventRuntimeCall, TimeMS: 20, RuntimeCall: runtimeCall(10, 31, 2, 30, 20),
	}); err != nil {
		return err
	}
	if agent {
		method := jhlog.DictionaryEntry{Kind: jhlog.DictMethod, ID: 40, Value: "Landroid/graphics/BitmapFactory;->decodeStream(Ljava/io/InputStream;)Landroid/graphics/Bitmap;"}
		if err := writer.WriteEvent(jhlog.Event{Type: jhlog.EventDictionary, Dictionary: &method}); err != nil {
			return err
		}
		context := jhlog.AttributionContext{Present: true, Screen: jhlog.LocalSymbol(10), Owner: jhlog.LocalSymbol(20), Flow: jhlog.LocalSymbol(30), Step: jhlog.LocalSymbol(32)}
		agentEvents := []jhlog.Event{
			{Type: jhlog.EventAgent, TimeUS: 900_000, Agent: &jhlog.AgentEvent{SemanticType: jhlog.AgentStatus, SchemaVersion: 1, ProducerSequence: 1, Payload0: 2, Payload2: 0xCAFE, Payload3: 131_072}},
			{Type: jhlog.EventAgent, TimeUS: 910_000, Agent: &jhlog.AgentEvent{SemanticType: jhlog.AgentCapability, SchemaVersion: 1, ProducerSequence: 2, Payload0: 0x3f, Payload1: 0x3f, Payload2: 0x3f, Payload3: 0x3f}},
			{Type: jhlog.EventAgent, TimeUS: 920_000, Attribution: context, Agent: &jhlog.AgentEvent{SemanticType: jhlog.AgentCorrelationLink, SchemaVersion: 1, ContextToken: 77, EventFlags: jhlog.AgentFlagContextDefinition, Payload0: 77}},
			{Type: jhlog.EventAgent, TimeUS: 930_000, Agent: &jhlog.AgentEvent{SemanticType: jhlog.AgentMethodDefinition, SchemaVersion: 1, Payload0: 44, MethodRef: jhlog.LocalSymbol(40)}},
			{Type: jhlog.EventAgent, TimeUS: 940_000, Agent: &jhlog.AgentEvent{SemanticType: jhlog.AgentStackDefinition, SchemaVersion: 1, ProducerSequence: 3, ContextToken: 77, Payload0: 0xB17, Payload1: 44, Payload3: 1 << 32}},
			{Type: jhlog.EventAgent, TimeUS: 950_000, Agent: &jhlog.AgentEvent{SemanticType: jhlog.AgentStatus, SchemaVersion: 1, Payload0: 0x100, Payload1: 2, Payload2: 0xCAFE}},
			{Type: jhlog.EventAgent, TimeUS: 1_000_000, Agent: &jhlog.AgentEvent{SemanticType: jhlog.AgentClockSync, SchemaVersion: 1, Payload0: 1_000_000_000, Payload1: 10_000}},
			{Type: jhlog.EventAgent, TimeUS: 1_150_000, Agent: &jhlog.AgentEvent{SemanticType: jhlog.AgentGCInterval, SchemaVersion: 1, ProducerSequence: 4, Payload0: 1_050_000_000, Payload1: 100_000_000}},
			{Type: jhlog.EventStall, TimeMS: 1_200, Attribution: context, Stall: &jhlog.StallEvent{DurationMS: 200}},
			{Type: jhlog.EventUIWindow, TimeMS: 1_200, Attribution: context, UIWindow: &jhlog.UIWindowEvent{WindowMS: 200, FrameCount: 12, JankCount: 7}},
			{Type: jhlog.EventAgent, TimeUS: 1_200_000, Agent: &jhlog.AgentEvent{SemanticType: jhlog.AgentThreadStackSample, SchemaVersion: 1, ProducerSequence: 5, ThreadToken: 11, ContextToken: 77, Payload0: 0xB17, Payload2: uint64(1) | uint64(1)<<32}},
			{Type: jhlog.EventAgent, TimeUS: 1_210_000, Agent: &jhlog.AgentEvent{SemanticType: jhlog.AgentQualitySnapshot, SchemaVersion: 1, ProducerSequence: 6, Payload0: 8}},
		}
		for _, event := range agentEvents {
			if err := writer.WriteEvent(event); err != nil {
				return err
			}
		}
	}
	quality := map[uint64]uint64{}
	if contextual {
		quality[jhlog.QualityRuntimeGraphInputTotal] = 2
		quality[jhlog.QualityRuntimeGraphEmittedTotal] = 2
	}
	if incomplete {
		quality[jhlog.QualityRuntimeGraphInputTotal] = 10
		quality[jhlog.QualityRuntimeGraphCircuitBreakerTrip] = 1
		quality[jhlog.QualityRuntimeGraphCircuitBreakerDrop] = 8
	}
	writer.SetQualitySnapshot(jhlog.QualitySnapshot{Sequence: 1, Counters: quality})
	return nil
}

func runtimeCall(screen, step, count, total, max uint64) *jhlog.RuntimeCallEvent {
	return &jhlog.RuntimeCallEvent{
		ScreenID: screen, CallerID: 20, FlowID: 30, StepID: step, CalleeID: 21,
		Count: count, TotalMS: total, MaxMS: max,
	}
}
