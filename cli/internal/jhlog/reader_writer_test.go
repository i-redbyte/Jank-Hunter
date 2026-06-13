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

func TestReadLegacyVersionOneBinary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.jhlog")
	var file bytes.Buffer
	legacyMagic := append([]byte{}, Magic...)
	legacyMagic[7] = 1
	file.Write(legacyMagic)
	writeLegacyV1Event(t, &file, Event{Type: EventSession, TimeMS: 3, Flags: uint64(FlagAppForeground), Session: &SessionEvent{AppVersionID: 1, BuildID: 2, DeviceID: 3, SDKInt: 35}})
	writeLegacyV1Event(t, &file, Event{Type: EventGauge, TimeMS: 5, Metric: &MetricEvent{MetricID: 10, Value: 42}})
	if err := os.WriteFile(path, file.Bytes(), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	log, err := ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if log.Version != 1 {
		t.Fatalf("version = %d, want legacy v1", log.Version)
	}
	if len(log.Events) != 2 {
		t.Fatalf("events = %d, want 2", len(log.Events))
	}
	if got := log.Events[1].Metric.Value; got != 42 {
		t.Fatalf("metric value = %d, want 42", got)
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

func writeLegacyV1Event(t *testing.T, out *bytes.Buffer, event Event) {
	t.Helper()
	var payload bytes.Buffer
	if err := encodePayload(&payload, event); err != nil {
		t.Fatalf("encodePayload() error = %v", err)
	}
	if err := writeUvarint(out, uint64(event.Type)); err != nil {
		t.Fatalf("write type: %v", err)
	}
	if err := writeUvarint(out, event.TimeMS); err != nil {
		t.Fatalf("write delta: %v", err)
	}
	if err := writeUvarint(out, event.Flags); err != nil {
		t.Fatalf("write flags: %v", err)
	}
	if err := writeUvarint(out, uint64(payload.Len())); err != nil {
		t.Fatalf("write payload length: %v", err)
	}
	if _, err := out.Write(payload.Bytes()); err != nil {
		t.Fatalf("write payload: %v", err)
	}
}
