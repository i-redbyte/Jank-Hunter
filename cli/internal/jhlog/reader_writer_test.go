package jhlog

import (
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteSampleStreamsBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.jhlog")
	if err := WriteSample(path); err != nil {
		t.Fatalf("WriteSample() error = %v", err)
	}

	log, err := readLog(path)
	if err != nil {
		t.Fatalf("readLog() error = %v", err)
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

func TestFormatMagicGolden(t *testing.T) {
	want := []byte{'J', 'H', 'L', 'O', 'G', '\r', '\n', FormatVersion}
	if !bytes.Equal(Magic, want) {
		t.Fatalf("magic = %v, want %v", Magic, want)
	}
}

func readCompressedBody(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	if len(raw) < len(Magic) {
		t.Fatalf("file too small for magic: %d", len(raw))
	}
	reader, err := gzip.NewReader(bytes.NewReader(raw[len(Magic):]))
	if err != nil {
		t.Fatalf("NewReader(%s body) error = %v", path, err)
	}
	body, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll(%s body) error = %v", path, err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("Close(%s gzip body) error = %v", path, err)
	}
	return body
}

func TestStreamFileRequiresCompressedBinaryBody(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.jhlog")
	if err := WriteSample(path); err != nil {
		t.Fatalf("WriteSample() error = %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(sample) error = %v", err)
	}
	if len(raw) <= len(Magic) {
		t.Fatalf("expected sample body")
	}
	if raw[len(Magic)] != 0x1f || raw[len(Magic)+1] != 0x8b {
		t.Fatalf("sample body is not gzip-compressed")
	}

	log, err := readLog(path)
	if err != nil {
		t.Fatalf("readLog(sample) error = %v", err)
	}
	if len(log.Events) == 0 || log.Dict[20] != "GET /feed" {
		t.Fatalf("compressed body did not decode sample log: events=%d dict[20]=%q", len(log.Events), log.Dict[20])
	}

	streamed := 0
	if err := StreamFile(path, func(Event, map[uint64]string) error {
		streamed++
		return nil
	}); err != nil {
		t.Fatalf("StreamFile(sample) error = %v", err)
	}
	if streamed != len(log.Events) {
		t.Fatalf("streamed events = %d, want %d", streamed, len(log.Events))
	}
}

func TestStreamFileRejectsUncompressedBinaryBody(t *testing.T) {
	path := filepath.Join(t.TempDir(), "uncompressed.jhlog")
	raw := append(append([]byte{}, Magic...), byte(EventSession))
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := readLog(path)
	if err == nil {
		t.Fatalf("expected uncompressed body error")
	}
	if !strings.Contains(err.Error(), "compressed jhlog body") {
		t.Fatalf("unexpected error: %v", err)
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

	log, err := readLog(path)
	if err != nil {
		t.Fatalf("readLog() error = %v", err)
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

	log, err := readLog(path)
	if err != nil {
		t.Fatalf("readLog() error = %v", err)
	}
	var flow *FlowEvent
	var spam *LogSpamEvent
	var problem *ProblemEvent
	var spamFlags uint64
	var problemFlags uint64
	for _, event := range log.Events {
		if event.Flow != nil {
			flow = event.Flow
			if event.Flags&uint64(FlagHasStep) != 0 {
				t.Fatalf("unexpected step flag in %08b", event.Flags)
			}
		}
		if event.LogSpam != nil {
			spam = event.LogSpam
			spamFlags = event.Flags
		}
		if event.Problem != nil {
			problem = event.Problem
			problemFlags = event.Flags
		}
	}
	if flow == nil || flow.ScreenID != 1 || flow.OwnerID != 2 || flow.FlowID != 3 || flow.StepID != 0 {
		t.Fatalf("flow did not round-trip: %+v", flow)
	}
	if spam == nil || spam.ScreenID != 1 || spam.OwnerID != 2 || spam.FlowID != 3 || spam.SourceID != 4 || spam.Level != 5 || spam.Count != 9 {
		t.Fatalf("log spam did not round-trip: %+v", spam)
	}
	if spamFlags&uint64(FlagSameContext) == 0 {
		t.Fatalf("expected same-context flag for log spam, got %08b", spamFlags)
	}
	if problem == nil || problem.ScreenID != 1 || problem.OwnerID != 2 || problem.FlowID != 3 || problem.KindID != 5 || problem.WindowMS != 5000 || problem.MaxMS != 9 {
		t.Fatalf("problem did not round-trip: %+v", problem)
	}
	if problemFlags&uint64(FlagSameContext) == 0 {
		t.Fatalf("expected same-context flag for problem, got %08b", problemFlags)
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

	log, err := readLog(path)
	if err != nil {
		t.Fatalf("readLog() error = %v", err)
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

	log, err := readLog(path)
	if err != nil {
		t.Fatalf("readLog() error = %v", err)
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

func TestStreamReadToleratesPartialBinaryTail(t *testing.T) {
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

	log, err := readLog(path)
	if err != nil {
		t.Fatalf("readLog() error = %v", err)
	}
	if len(log.Events) == 0 {
		t.Fatalf("expected preserved events")
	}
	if len(log.Warnings) == 0 {
		t.Fatalf("expected partial-tail warning")
	}
}

func TestStreamFileWithWarningsToleratesPartialBinaryTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "partial-stream.jhlog")
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

	streamed := 0
	warnings, err := StreamFileWithWarnings(path, func(Event, map[uint64]string) error {
		streamed++
		return nil
	})
	if err != nil {
		t.Fatalf("StreamFileWithWarnings() error = %v", err)
	}
	if streamed == 0 {
		t.Fatalf("expected preserved streamed events")
	}
	if len(warnings) == 0 {
		t.Fatalf("expected partial-tail warning")
	}
	if !strings.Contains(strings.Join(warnings, "\n"), "ignored partial trailing compact event") {
		t.Fatalf("unexpected warnings: %+v", warnings)
	}
}

func TestStreamReadRejectsPreviousBinaryVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "previous.jhlog")
	previous := append([]byte{}, Magic...)
	previous[7] = byte(FormatVersion - 1)
	if err := os.WriteFile(path, previous, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := readLog(path)
	if err == nil {
		t.Fatalf("expected previous version error")
	}
	if !strings.Contains(err.Error(), "unsupported jhlog version") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStreamReadRejectsFutureBinaryVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "future.jhlog")
	future := append([]byte{}, Magic...)
	future[7] = byte(FormatVersion + 1)
	if err := os.WriteFile(path, future, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := readLog(path)
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

	body := readCompressedBody(t, path)
	if bytes.Contains(body, []byte("1234567890123")) {
		t.Fatalf("decimal dictionary value was written as raw UTF-8")
	}
	if bytes.Contains(body, []byte("2026-06-13")) {
		t.Fatalf("ISO date dictionary value was written as raw UTF-8")
	}
	if !bytes.Contains(body, []byte("com.myapp.feature.FeedRepository")) {
		t.Fatalf("ordinary text dictionary value should remain UTF-8")
	}

	log, err := readLog(path)
	if err != nil {
		t.Fatalf("readLog() error = %v", err)
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

func TestMetricAggregationMetadataRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metrics.jhlog")
	file, writer, err := Create(path)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := writer.WriteEvent(Event{
		Type: EventDictionary,
		Dictionary: &DictionaryEntry{
			Kind:  DictGeneric,
			ID:    1,
			Value: "memory.pss",
		},
	}); err != nil {
		t.Fatalf("WriteEvent(dictionary) error = %v", err)
	}
	if err := writer.WriteEvent(Event{
		Type: EventGauge,
		Metric: &MetricEvent{
			MetricID: 1,
			Value:    130,
			Count:    2,
			Sum:      260,
			Max:      160,
			Mode:     MetricModeAverage,
		},
	}); err != nil {
		t.Fatalf("WriteEvent(gauge) error = %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	log, err := readLog(path)
	if err != nil {
		t.Fatalf("readLog() error = %v", err)
	}
	var metric *MetricEvent
	for _, event := range log.Events {
		if event.Type == EventGauge {
			metric = event.Metric
			break
		}
	}
	if metric == nil {
		t.Fatalf("gauge event not found: %+v", log.Events)
	}
	if metric.Value != 130 || metric.Count != 2 || metric.Sum != 260 || metric.Max != 160 || metric.Mode != MetricModeAverage {
		t.Fatalf("metric metadata = %+v", metric)
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
			BatteryTempDeciC: -45,
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

	log, err := readLog(path)
	if err != nil {
		t.Fatalf("readLog() error = %v", err)
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
	if context.Network != NetworkVPN ||
		context.BatteryPct != 50 ||
		context.AvailMemoryKB != 1024 ||
		context.BatteryState != 3 ||
		context.BatteryTempDeciC != -45 ||
		context.RxBytes != 1000 ||
		context.TxBytes != 2000 ||
		context.TotalMemoryKB != 4096 ||
		context.FreeStorageKB != 8192 ||
		context.TotalStorageKB != 16384 {
		t.Fatalf("context payload did not round-trip: %+v", context)
	}
}

func readLog(path string) (Log, error) {
	log := Log{
		Source:  path,
		Version: FormatVersion,
		Dict:    map[uint64]string{},
		Kinds:   map[uint64]DictKind{},
	}
	warnings, err := StreamFileWithWarnings(path, func(event Event, _ map[uint64]string) error {
		if event.Dictionary != nil {
			log.Dict[event.Dictionary.ID] = event.Dictionary.Value
			log.Kinds[event.Dictionary.ID] = event.Dictionary.Kind
		}
		log.Events = append(log.Events, event)
		return nil
	})
	log.Warnings = warnings
	return log, err
}
