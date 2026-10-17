package comparisoninput

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
)

func TestReadComparisonSnapshotHTMLValidatesEnvelopeAndLimits(t *testing.T) {
	document := comparisonInputTestDocument(t, analyze.ComparisonSnapshotInspect)
	valid := comparisonInputHTML(t, document)
	path := filepath.Join(t.TempDir(), "report.html")
	if err := os.WriteFile(path, valid, 0o600); err != nil {
		t.Fatal(err)
	}
	summary, err := ReadComparisonSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	if summary.HTTPP95MS != 123 || summary.DurationMS != 5_000 {
		t.Fatalf("summary = %+v", summary)
	}

	tests := []struct {
		name string
		html []byte
		want string
	}{
		{name: "missing", html: []byte("<html></html>"), want: "does not contain"},
		{name: "duplicate", html: append(append([]byte(nil), valid...), valid...), want: "more than one"},
		{name: "wrong type", html: []byte(strings.Replace(string(valid), `type="application/json"`, `type="text/javascript"`, 1)), want: "type"},
		{name: "deep JSON", html: []byte(`<script id="jankhunter-comparison-snapshot" type="application/json">` + strings.Repeat("[", 33) + strings.Repeat("]", 33) + `</script>`), want: "depth"},
		{name: "unterminated", html: []byte(`<script id="jankhunter-comparison-snapshot" type="application/json">{}`), want: "unterminated"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := parseComparisonSnapshotHTML(test.html)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestReadComparisonSnapshotRejectsAmbiguousCompareReport(t *testing.T) {
	document := comparisonInputTestDocument(t, analyze.ComparisonSnapshotCompare)
	document.Snapshots = append(document.Snapshots, document.Snapshots[0])
	path := filepath.Join(t.TempDir(), "compare.html")
	if err := os.WriteFile(path, comparisonInputHTML(t, document), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadComparisonSnapshot(path); err == nil || !strings.Contains(err.Error(), "compare report") {
		t.Fatalf("ambiguous report error = %v", err)
	}
}

func TestReadComparisonSnapshotRejectsOversizedHTMLBeforeReading(t *testing.T) {
	path := filepath.Join(t.TempDir(), "large.html")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(MaxHTMLInputBytes + 1); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadComparisonSnapshot(path); err == nil || !strings.Contains(err.Error(), "HTML input exceeds") {
		t.Fatalf("oversized error = %v", err)
	}
}

func TestParseComparisonSnapshotRejectsOversizedSnapshotElement(t *testing.T) {
	header := []byte(`<script id="jankhunter-comparison-snapshot" type="application/json">`)
	payload := make([]byte, len(header)+MaxSnapshotBytes+1+len(`</script>`))
	copy(payload, header)
	for index := len(header); index < len(header)+MaxSnapshotBytes+1; index++ {
		payload[index] = 'x'
	}
	copy(payload[len(header)+MaxSnapshotBytes+1:], `</script>`)
	if _, err := parseComparisonSnapshotHTML(payload); err == nil || !strings.Contains(err.Error(), "snapshot exceeds") {
		t.Fatalf("oversized snapshot error = %v", err)
	}
}

func FuzzParseComparisonSnapshotHTML(f *testing.F) {
	snapshot, err := analyze.NewComparisonSnapshot(analyze.Summary{Title: "fuzz"}, "2026-09-23T00:00:00Z")
	if err != nil {
		f.Fatal(err)
	}
	document := analyze.ComparisonSnapshotDocument{Schema: analyze.ComparisonSnapshotDocumentSchema, Kind: analyze.ComparisonSnapshotInspect, Snapshots: []analyze.ComparisonSnapshot{snapshot}}
	payload, err := json.Marshal(document)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(append(append([]byte(`<script id="jankhunter-comparison-snapshot" type="application/json">`), payload...), []byte(`</script>`)...))
	f.Add([]byte(`<script id="jankhunter-comparison-snapshot" type="application/json">{}</script>`))
	f.Fuzz(func(t *testing.T, input []byte) {
		if len(input) > 1<<20 {
			t.Skip()
		}
		_, _ = parseComparisonSnapshotHTML(input)
	})
}

func comparisonInputTestDocument(t testing.TB, kind string) analyze.ComparisonSnapshotDocument {
	t.Helper()
	snapshot, err := analyze.NewComparisonSnapshot(analyze.Summary{Title: "portable", LogCount: 1, EventCount: 10, DurationMS: 5_000, HTTPCount: 10, HTTPP95MS: 123}, "2026-09-23T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	return analyze.ComparisonSnapshotDocument{Schema: analyze.ComparisonSnapshotDocumentSchema, Kind: kind, Snapshots: []analyze.ComparisonSnapshot{snapshot}}
}

func comparisonInputHTML(t testing.TB, document analyze.ComparisonSnapshotDocument) []byte {
	t.Helper()
	payload, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	return append(append([]byte(`<html><body><script id="jankhunter-comparison-snapshot" type="application/json">`), payload...), []byte(`</script></body></html>`)...)
}
