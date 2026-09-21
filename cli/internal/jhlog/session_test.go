package jhlog

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSessionLogFilenameIsCanonicalAndNumeric(t *testing.T) {
	const firstRun = "01000000000000000000000000000000"
	const secondRun = "02000000000000000000000000000000"
	zero, ok := ParseSessionLogFilename("jh-session-log.2026-07-14." + firstRun + ".0.jhlog")
	if !ok || zero.DailySessionIndex != 0 || zero.SegmentIndex != 0 {
		t.Fatalf("canonical zero index = %+v, parsed=%t", zero, ok)
	}
	if zero.RunID[0] != 1 {
		t.Fatalf("run ID = %x, want first byte 01", zero.RunID)
	}
	two, ok := ParseSessionLogFilename(filepath.Join("logs", "jh-session-log.2026-07-14."+firstRun+".0-2.jhlog"))
	if !ok {
		t.Fatal("canonical filename was rejected")
	}
	ten, ok := ParseSessionLogFilename("jh-session-log.2026-07-14." + firstRun + ".0-10.jhlog")
	if !ok {
		t.Fatal("canonical filename with two-digit index was rejected")
	}
	nextSession, ok := ParseSessionLogFilename("jh-session-log.2026-07-14." + secondRun + ".1.jhlog")
	if !ok {
		t.Fatal("canonical filename for the next daily session was rejected")
	}
	nextDay, ok := ParseSessionLogFilename("jh-session-log.2026-07-15." + secondRun + ".0.jhlog")
	if !ok {
		t.Fatal("canonical filename on next day was rejected")
	}
	if two.Compare(ten) >= 0 || ten.Compare(nextSession) >= 0 || nextSession.Compare(nextDay) >= 0 {
		t.Fatalf("unexpected ordering: two=%+v ten=%+v nextSession=%+v nextDay=%+v", two, ten, nextSession, nextDay)
	}

	for _, path := range []string{
		"session-main-1000-1.jhlog",
		"jh-session-log.v2.2026-07-14." + firstRun + ".0.jhlog",
		"jh-session-log.20260714.1.jhlog",
		"jh-session-log.2026-7-14.1.jhlog",
		"jh-session-log.2026-02-30." + firstRun + ".1.jhlog",
		"jh-session-log.2026-07-14." + firstRun + ".01.jhlog",
		"jh-session-log.2026-07-14." + firstRun + ".0-0.jhlog",
		"jh-session-log.2026-07-14." + firstRun + ".0-01.jhlog",
		"jh-session-log.2026-07-14." + firstRun + ".0-1-2.jhlog",
		"jh-session-log.2026-07-14." + firstRun + ".one.jhlog",
		"jh-session-log.2026-07-14.0100000000000000000000000000000.1.jhlog",
		"jh-session-log.2026-07-14.0100000000000000000000000000000G.1.jhlog",
		"jh-session-log.2026-07-14.0100000000000000000000000000000A.1.jhlog",
		"jh-session-log.2026-07-14." + firstRun + ".1.jhlog.tmp",
	} {
		if parsed, ok := ParseSessionLogFilename(path); ok {
			t.Fatalf("noncanonical filename %q parsed as %+v", path, parsed)
		}
	}
}

func TestReadSessionHeaderReadsOnlyBoundedJH100Header(t *testing.T) {
	header := DefaultSegmentHeader()
	header.RunID[0] = 1
	header.ProcessInstanceID[0] = 2
	header.SessionID[0] = 3
	header.SegmentIndex = 10
	header.PreviousSegmentDigest = make([]byte, segmentDigestSize)
	header.ProcessName = "com.example:remote"
	header.SymbolNamespace = []byte("symbols-v1")
	raw, normalized, err := encodeFileHeader(header)
	if err != nil {
		t.Fatalf("encodeFileHeader() error = %v", err)
	}
	// The chunk/body is deliberately invalid. A bounded header read must not
	// inspect it or turn this metadata lookup into a whole-file parse.
	raw = append(raw, []byte("invalid chunk body")...)
	path := filepath.Join(t.TempDir(), "jh-session-log.2026-07-14.01000000000000000000000000000000.10.jhlog")
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
	path := filepath.Join(t.TempDir(), "jh-session-log.2026-07-14.01000000000000000000000000000000.1.jhlog")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := ReadSessionHeader(path)
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("ReadSessionHeader() error = %v, want bounded-length rejection", err)
	}
}

func TestCanonicalSessionFilenameMustMatchHeaderRunID(t *testing.T) {
	header := DefaultSegmentHeader()
	header.RunID[0] = 2
	path := filepath.Join(t.TempDir(), "jh-session-log.2026-07-14.01000000000000000000000000000000.0.jhlog")
	file, _, err := CreateWithHeader(path, header)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := ReadSessionHeader(path); err == nil || !strings.Contains(err.Error(), "filename run ID") {
		t.Fatalf("ReadSessionHeader() error = %v, want run-ID mismatch", err)
	}
	if _, err := StreamFileWithResult(path, nil); err == nil || !strings.Contains(err.Error(), "filename run ID") {
		t.Fatalf("StreamFileWithResult() error = %v, want run-ID mismatch", err)
	}
}
