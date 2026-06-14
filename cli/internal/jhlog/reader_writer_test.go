package jhlog

import (
	"bytes"
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
