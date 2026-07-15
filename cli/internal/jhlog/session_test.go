package jhlog

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSessionLogFilenameIsCanonicalAndNumeric(t *testing.T) {
	zero, ok := ParseSessionLogFilename("jh-session-log.2026-07-14.0.jhlog")
	if !ok || zero.Index != 0 {
		t.Fatalf("canonical zero index = %+v, parsed=%t", zero, ok)
	}
	two, ok := ParseSessionLogFilename(filepath.Join("logs", "jh-session-log.2026-07-14.2.jhlog"))
	if !ok {
		t.Fatal("canonical filename was rejected")
	}
	ten, ok := ParseSessionLogFilename("jh-session-log.2026-07-14.10.jhlog")
	if !ok {
		t.Fatal("canonical filename with two-digit index was rejected")
	}
	nextDay, ok := ParseSessionLogFilename("jh-session-log.2026-07-15.1.jhlog")
	if !ok {
		t.Fatal("canonical filename on next day was rejected")
	}
	if two.Compare(ten) >= 0 || ten.Compare(nextDay) >= 0 {
		t.Fatalf("unexpected ordering: two=%+v ten=%+v nextDay=%+v", two, ten, nextDay)
	}

	for _, path := range []string{
		"session-main-1000-1.jhlog",
		"jh-session-log.20260714.1.jhlog",
		"jh-session-log.2026-7-14.1.jhlog",
		"jh-session-log.2026-02-30.1.jhlog",
		"jh-session-log.2026-07-14.01.jhlog",
		"jh-session-log.2026-07-14.one.jhlog",
		"jh-session-log.2026-07-14.1.jhlog.tmp",
	} {
		if parsed, ok := ParseSessionLogFilename(path); ok {
			t.Fatalf("noncanonical filename %q parsed as %+v", path, parsed)
		}
	}
}

func TestReadSessionHeaderReadsOnlyBoundedV9Header(t *testing.T) {
	header := DefaultSegmentHeader()
	header.RunID[0] = 1
	header.ProcessInstanceID[0] = 2
	header.SessionID[0] = 3
	header.SegmentIndex = 10
	header.ProcessName = "com.example:remote"
	header.SymbolNamespace = []byte("symbols-v1")
	raw, normalized, err := encodeFileHeader(header)
	if err != nil {
		t.Fatalf("encodeFileHeader() error = %v", err)
	}
	// The chunk/body is deliberately invalid. A bounded header read must not
	// inspect it or turn this metadata lookup into a whole-file parse.
	raw = append(raw, []byte("invalid chunk body")...)
	path := filepath.Join(t.TempDir(), "jh-session-log.2026-07-14.10.jhlog")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	got, err := ReadSessionHeader(path)
	if err != nil {
		t.Fatalf("ReadSessionHeader() error = %v", err)
	}
	if got.SessionID != normalized.SessionID || got.ProcessInstanceID != normalized.ProcessInstanceID ||
		got.ProcessName != normalized.ProcessName || got.SegmentIndex != normalized.SegmentIndex {
		t.Fatalf("header = %+v, want %+v", got, normalized)
	}
}

func TestReadSessionHeaderRejectsUnboundedPayloadBeforeAllocation(t *testing.T) {
	raw := append([]byte(nil), Magic...)
	var fixed [8]byte
	binary.LittleEndian.PutUint32(fixed[:4], uint32(maxHeaderPayloadSize+1))
	raw = append(raw, fixed[:]...)
	path := filepath.Join(t.TempDir(), "jh-session-log.2026-07-14.1.jhlog")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := ReadSessionHeader(path)
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("ReadSessionHeader() error = %v, want bounded-length rejection", err)
	}
}
