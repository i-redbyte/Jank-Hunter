package jhlog

import (
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteSampleReadFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.jhlog")
	if err := WriteSample(path); err != nil {
		t.Fatalf("WriteSample() error = %v", err)
	}

	log, err := ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if len(log.Events) == 0 {
		t.Fatalf("expected events")
	}
	if got := log.Dict[20]; got != "GET /feed" {
		t.Fatalf("dict[20] = %q", got)
	}
	if log.Events[len(log.Events)-1].TimeMS == 0 {
		t.Fatalf("expected monotonic event timestamps")
	}
	if log.Version != FormatVersion {
		t.Fatalf("log version = %d, want %d", log.Version, FormatVersion)
	}
	if got := log.Dict[4]; got != "main" {
		t.Fatalf("dict[4] = %q, want process name", got)
	}
	var context *ContextEvent
	var session *SessionEvent
	for _, event := range log.Events {
		if event.Session != nil {
			session = event.Session
		}
		if event.Context != nil {
			context = event.Context
			break
		}
	}
	if session == nil || session.ProcessID != 4 {
		t.Fatalf("expected session process id, got %+v", session)
	}
	if session.PrimaryABIID != 72 || session.SecurityPatchID != 71 {
		t.Fatalf("expected extended session device metadata, got %+v", session)
	}
	if session.DeviceRooted {
		t.Fatalf("sample session should be non-rooted: %+v", session)
	}
	if context == nil {
		t.Fatalf("expected context event")
	}
	if context.Network != NetworkWiFi || context.BatteryPct != 82 || !context.NetworkValidated {
		t.Fatalf("unexpected context event: %+v", context)
	}
	if context.TotalMemoryKB == 0 || context.FreeStorageKB == 0 {
		t.Fatalf("expected extended context memory/storage metadata: %+v", context)
	}
	var retained *RetainedEvent
	for _, event := range log.Events {
		if event.Retained != nil {
			retained = event.Retained
			break
		}
	}
	if retained == nil {
		t.Fatalf("expected retained event")
	}
	if retained.ScreenID != 31 || retained.OwnerID != 41 || retained.FlowID != 65 || retained.StepID != 66 || retained.ClassID != 40 || retained.HolderID != 41 {
		t.Fatalf("retained context did not round-trip: %+v", retained)
	}
}

func TestReadFileSupportsCompressedBinaryBody(t *testing.T) {
	rawPath := filepath.Join(t.TempDir(), "raw.jhlog")
	if err := WriteSample(rawPath); err != nil {
		t.Fatalf("WriteSample() error = %v", err)
	}
	raw, err := os.ReadFile(rawPath)
	if err != nil {
		t.Fatalf("ReadFile(raw) error = %v", err)
	}
	if len(raw) <= len(Magic) {
		t.Fatalf("expected sample body")
	}

	compressedPath := filepath.Join(t.TempDir(), "compressed.jhlog")
	file, err := os.Create(compressedPath)
	if err != nil {
		t.Fatalf("Create(compressed) error = %v", err)
	}
	if _, err := file.Write(Magic); err != nil {
		t.Fatalf("Write(magic) error = %v", err)
	}
	gzipWriter := gzip.NewWriter(file)
	if _, err := gzipWriter.Write(raw[len(Magic):]); err != nil {
		t.Fatalf("Write(gzip body) error = %v", err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatalf("Close(gzip) error = %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close(file) error = %v", err)
	}

	log, err := ReadFile(compressedPath)
	if err != nil {
		t.Fatalf("ReadFile(compressed) error = %v", err)
	}
	if len(log.Events) == 0 || log.Dict[20] != "GET /feed" {
		t.Fatalf("compressed body did not decode sample log: events=%d dict[20]=%q", len(log.Events), log.Dict[20])
	}

	streamed := 0
	if err := StreamFile(compressedPath, func(Event, map[uint64]string) error {
		streamed++
		return nil
	}); err != nil {
		t.Fatalf("StreamFile(compressed) error = %v", err)
	}
	if streamed != len(log.Events) {
		t.Fatalf("streamed events = %d, want %d", streamed, len(log.Events))
	}
}

func TestSessionRootedUsesFlags(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session-rooted.jhlog")
	file, writer, err := Create(path)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := writer.WriteEvent(Event{
		Type:   EventSession,
		TimeMS: 1,
		Session: &SessionEvent{
			AppVersionID: 1,
			BuildID:      2,
			DeviceID:     3,
			SDKInt:       35,
			DeviceRooted: true,
		},
	}); err != nil {
		t.Fatalf("WriteEvent() error = %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	log, err := ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if len(log.Events) != 1 || log.Events[0].Session == nil {
		t.Fatalf("expected one session event, got %#v", log.Events)
	}
	event := log.Events[0]
	if event.Flags&uint64(FlagDeviceRooted) == 0 {
		t.Fatalf("expected rooted flag in %08b", event.Flags)
	}
	if !event.Session.DeviceRooted {
		t.Fatalf("session rooted flag did not round-trip: %+v", event.Session)
	}
}

func TestFlowLogSpamAndProblemUseContextFlags(t *testing.T) {
	path := filepath.Join(t.TempDir(), "flow-context.jhlog")
	file, writer, err := Create(path)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	entries := []DictionaryEntry{
		{Kind: DictScreen, ID: 1, Value: "Checkout"},
		{Kind: DictOwner, ID: 2, Value: "CheckoutPresenter"},
		{Kind: DictFlow, ID: 3, Value: "checkout.open"},
		{Kind: DictLogSource, ID: 4, Value: "android.util.Log.w"},
		{Kind: DictMetric, ID: 5, Value: "log_spam"},
	}
	for _, entry := range entries {
		if err := writer.WriteEvent(Event{Type: EventDictionary, Dictionary: &entry}); err != nil {
			t.Fatalf("WriteEvent(dictionary) error = %v", err)
		}
	}
	if err := writer.WriteEvent(Event{Type: EventFlow, TimeMS: 10, Flow: &FlowEvent{ScreenID: 1, OwnerID: 2, FlowID: 3}}); err != nil {
		t.Fatalf("WriteEvent(flow) error = %v", err)
	}
	if err := writer.WriteEvent(Event{Type: EventLogSpam, TimeMS: 20, LogSpam: &LogSpamEvent{ScreenID: 1, OwnerID: 2, FlowID: 3, SourceID: 4, Level: 5, Count: 9}}); err != nil {
		t.Fatalf("WriteEvent(log spam) error = %v", err)
	}
	if err := writer.WriteEvent(Event{Type: EventProblem, TimeMS: 30, Problem: &ProblemEvent{ScreenID: 1, OwnerID: 2, FlowID: 3, KindID: 5, WindowMS: 5000, Count: 9, MaxMS: 9}}); err != nil {
		t.Fatalf("WriteEvent(problem) error = %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	log, err := ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var flow *FlowEvent
	var spam *LogSpamEvent
	var problem *ProblemEvent
	for _, event := range log.Events {
		if event.Flow != nil {
			flow = event.Flow
			if event.Flags&uint64(FlagHasStep) != 0 {
				t.Fatalf("unexpected step flag in %08b", event.Flags)
			}
		}
		if event.LogSpam != nil {
			spam = event.LogSpam
		}
		if event.Problem != nil {
			problem = event.Problem
		}
	}
	if flow == nil || flow.ScreenID != 1 || flow.OwnerID != 2 || flow.FlowID != 3 || flow.StepID != 0 {
		t.Fatalf("flow did not round-trip: %+v", flow)
	}
	if spam == nil || spam.SourceID != 4 || spam.Level != 5 || spam.Count != 9 {
		t.Fatalf("log spam did not round-trip: %+v", spam)
	}
	if problem == nil || problem.KindID != 5 || problem.WindowMS != 5000 || problem.MaxMS != 9 {
		t.Fatalf("problem did not round-trip: %+v", problem)
	}
}

func TestRuntimeCallUsesCompactContextFlags(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime-call.jhlog")
	file, writer, err := Create(path)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	entries := []DictionaryEntry{
		{Kind: DictScreen, ID: 1, Value: "Checkout"},
		{Kind: DictOwner, ID: 2, Value: "CheckoutPresenter.open"},
		{Kind: DictOwner, ID: 3, Value: "CheckoutRepository.load"},
		{Kind: DictFlow, ID: 4, Value: "checkout.open"},
		{Kind: DictStep, ID: 5, Value: "network"},
	}
	for _, entry := range entries {
		if err := writer.WriteEvent(Event{Type: EventDictionary, Dictionary: &entry}); err != nil {
			t.Fatalf("WriteEvent(dictionary) error = %v", err)
		}
	}
	if err := writer.WriteEvent(Event{
		Type:   EventRuntimeCall,
		TimeMS: 40,
		RuntimeCall: &RuntimeCallEvent{
			ScreenID: 1,
			CallerID: 2,
			FlowID:   4,
			StepID:   5,
			CalleeID: 3,
			Count:    9,
			TotalMS:  72,
			MaxMS:    18,
		},
	}); err != nil {
		t.Fatalf("WriteEvent(runtime call) error = %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	log, err := ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var runtimeCall *RuntimeCallEvent
	var flags uint64
	for _, event := range log.Events {
		if event.RuntimeCall != nil {
			runtimeCall = event.RuntimeCall
			flags = event.Flags
		}
	}
	if runtimeCall == nil {
		t.Fatalf("expected runtime call event")
	}
	if flags&uint64(FlagHasScreen|FlagHasOwner|FlagHasFlow|FlagHasStep) == 0 {
		t.Fatalf("expected compact context flags, got %08b", flags)
	}
	if runtimeCall.CallerID != 2 || runtimeCall.CalleeID != 3 || runtimeCall.Count != 9 || runtimeCall.TotalMS != 72 || runtimeCall.MaxMS != 18 {
		t.Fatalf("runtime call did not round-trip: %+v", runtimeCall)
	}
}

func TestRetainedUsesCompactContextFlags(t *testing.T) {
	path := filepath.Join(t.TempDir(), "retained-context.jhlog")
	file, writer, err := Create(path)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	entries := []DictionaryEntry{
		{Kind: DictScreen, ID: 1, Value: "Checkout"},
		{Kind: DictOwner, ID: 2, Value: "CheckoutPresenter"},
		{Kind: DictFlow, ID: 3, Value: "checkout.open"},
		{Kind: DictStep, ID: 4, Value: "render"},
		{Kind: DictClass, ID: 5, Value: "com.app.CheckoutActivity"},
	}
	for _, entry := range entries {
		if err := writer.WriteEvent(Event{Type: EventDictionary, Dictionary: &entry}); err != nil {
			t.Fatalf("WriteEvent(dictionary) error = %v", err)
		}
	}
	if err := writer.WriteEvent(Event{
		Type:   EventRetained,
		TimeMS: 100,
		Retained: &RetainedEvent{
			ScreenID: 1,
			OwnerID:  2,
			FlowID:   3,
			StepID:   4,
			ClassID:  5,
			HolderID: 2,
			AgeMS:    30_000,
			Count:    2,
		},
	}); err != nil {
		t.Fatalf("WriteEvent(retained) error = %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	log, err := ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var retained *RetainedEvent
	var flags uint64
	for _, event := range log.Events {
		if event.Retained != nil {
			retained = event.Retained
			flags = event.Flags
		}
	}
	if retained == nil {
		t.Fatalf("expected retained event")
	}
	if flags&uint64(FlagHasScreen|FlagHasOwner|FlagHasFlow|FlagHasStep) == 0 {
		t.Fatalf("expected compact context flags, got %08b", flags)
	}
	if retained.ScreenID != 1 || retained.OwnerID != 2 || retained.FlowID != 3 || retained.StepID != 4 || retained.ClassID != 5 || retained.HolderID != 2 || retained.AgeMS != 30_000 || retained.Count != 2 {
		t.Fatalf("retained did not round-trip: %+v", retained)
	}
}

func TestReadFileToleratesPartialBinaryTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "partial.jhlog")
	if err := WriteSample(path); err != nil {
		t.Fatalf("WriteSample() error = %v", err)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("OpenFile() error = %v", err)
	}
	if _, err := file.Write([]byte{byte(EventHTTP), 0x80}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	log, err := ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if len(log.Events) == 0 {
		t.Fatalf("expected preserved events")
	}
	if len(log.Warnings) == 0 {
		t.Fatalf("expected partial-tail warning")
	}
}

func TestReadFileRejectsFutureBinaryVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "future.jhlog")
	future := append([]byte{}, Magic...)
	future[7] = byte(FormatVersion + 1)
	if err := os.WriteFile(path, future, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := ReadFile(path)
	if err == nil {
		t.Fatalf("expected future version error")
	}
	if !strings.Contains(err.Error(), "unsupported jhlog version") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDictionaryValueCodecsDecodeBCD(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bcd.jhlog")
	file, writer, err := Create(path)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	entries := []DictionaryEntry{
		{Kind: DictBuild, ID: 1, Value: "1234567890123"},
		{Kind: DictGeneric, ID: 2, Value: "2026-06-13"},
		{Kind: DictClass, ID: 3, Value: "com.myapp.feature.FeedRepository"},
	}
	for _, entry := range entries {
		if err := writer.WriteEvent(Event{Type: EventDictionary, Dictionary: &entry}); err != nil {
			t.Fatalf("WriteEvent(%q) error = %v", entry.Value, err)
		}
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(raw) error = %v", err)
	}
	if bytes.Contains(raw, []byte("1234567890123")) {
		t.Fatalf("decimal dictionary value was written as raw UTF-8")
	}
	if bytes.Contains(raw, []byte("2026-06-13")) {
		t.Fatalf("ISO date dictionary value was written as raw UTF-8")
	}
	if !bytes.Contains(raw, []byte("com.myapp.feature.FeedRepository")) {
		t.Fatalf("ordinary text dictionary value should remain UTF-8")
	}

	log, err := ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if got := log.Dict[1]; got != "1234567890123" {
		t.Fatalf("dict[1] = %q", got)
	}
	if got := log.Dict[2]; got != "2026-06-13" {
		t.Fatalf("dict[2] = %q", got)
	}
	if got := log.Dict[3]; got != "com.myapp.feature.FeedRepository" {
		t.Fatalf("dict[3] = %q", got)
	}
}

func TestContextBooleansUseFlags(t *testing.T) {
	path := filepath.Join(t.TempDir(), "context-flags.jhlog")
	file, writer, err := Create(path)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := writer.WriteEvent(Event{
		Type:   EventContext,
		TimeMS: 100,
		Flags:  uint64(FlagAppForeground),
		Context: &ContextEvent{
			Network:          NetworkVPN,
			BatteryPct:       50,
			AvailMemoryKB:    1024,
			BatteryState:     3,
			BatteryTempDeciC: 301,
			LowMemory:        true,
			NetworkMetered:   true,
			NetworkValidated: true,
			NetworkVPN:       true,
			RxBytes:          1000,
			TxBytes:          2000,
			TotalMemoryKB:    4096,
			FreeStorageKB:    8192,
			TotalStorageKB:   16384,
		},
	}); err != nil {
		t.Fatalf("WriteEvent() error = %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	log, err := ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if len(log.Events) != 1 || log.Events[0].Context == nil {
		t.Fatalf("expected one context event, got %#v", log.Events)
	}
	event := log.Events[0]
	for _, flag := range []Flag{FlagContextLowMemory, FlagNetworkMetered, FlagNetworkValidated, FlagNetworkVPN} {
		if event.Flags&uint64(flag) == 0 {
			t.Fatalf("expected flag %d in %08b", flag, event.Flags)
		}
	}
	context := event.Context
	if !context.LowMemory || !context.NetworkMetered || !context.NetworkValidated || !context.NetworkVPN {
		t.Fatalf("context booleans did not round-trip from flags: %+v", context)
	}
}
