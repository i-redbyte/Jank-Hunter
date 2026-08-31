package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
)

func TestCriticalIORowsExplainSourceCoverageAndImportantStatus(t *testing.T) {
	summary := analyze.Summary{IOAnalysis: &analyze.IOAnalysis{Calls: []analyze.IOStats{
		{
			Operation: "file_sync", Source: "DraftStore.commit", MainThread: true,
			Count: 2, KnownByteOperations: 0, P95DurationUS: 2_000, MaxDurationUS: 3_000,
			Screen: "Composer", ContextOperation: "draft.save", Owner: "ComposerViewModel.save",
		},
		{
			Operation: "file_write", Source: "CacheStore.flush", Count: 60,
			KnownByteOperations: 60, Bytes: 184_320, MaxBytes: 4_096, BytesPerSecond: 2_048_000,
			P95DurationUS: 4_000, MaxDurationUS: 5_000, PeakOperationsPerSecond: 12,
		},
	}}}

	rows := criticalIORows(summary)
	if len(rows) != 2 {
		t.Fatalf("I/O rows = %+v", rows)
	}
	if rows[0].Source != "DraftStore.commit" || rows[0].Severity != "high" ||
		!strings.Contains(rows[0].Status, "главном потоке") ||
		rows[0].Context != "экран Composer · операция draft.save · источник ComposerViewModel.save" {
		t.Fatalf("sync row = %+v", rows[0])
	}
	if rows[1].ByteCoverage != "60 из 60 · 100.0%" ||
		rows[1].Throughput != "2.0 МБ/с" || !strings.Contains(rows[1].Status, "частые") {
		t.Fatalf("write row = %+v", rows[1])
	}
}

func TestInspectReportRendersSeparateCriticalIOAnalysis(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.html")
	call := analyze.IOStats{
		Operation: "file_read", Source: "AttachmentStore.read", MainThread: true,
		Count: 3, Failures: 1, KnownByteOperations: 2, Bytes: 2_097_152, MaxBytes: 1_048_576,
		P50DurationUS: 8_000, P95DurationUS: 24_000, MaxDurationUS: 30_000,
		TotalDurationUS: 54_000, BytesPerSecond: 38_836_148, PeakOperationsPerSecond: 3,
		Screen: "Attachment", Owner: "AttachmentViewModel.open",
	}
	summary := analyze.Summary{
		Title: "io.jhlog", LogCount: 1, EventCount: 4,
		CollectionQuality: sampleCollectionQuality(),
		IOAnalysis: &analyze.IOAnalysis{
			Operations: 3, Failures: 1, MainThreadOperations: 3, MainThreadDurationUS: 54_000,
			KnownByteOperations: 2, Bytes: 2_097_152, P50DurationUS: 8_000,
			P95DurationUS: 24_000, MaxDurationUS: 30_000, BytesPerSecond: 38_836_148,
			PeakOperationsPerSecond: 3, MaxConcurrency: 2, SourceCount: 1, Calls: []analyze.IOStats{call},
		},
	}
	if err := WriteInspectWithOptions(path, summary, ReportOptions{GeneratedAt: "2026-08-20T12:00:00+03:00"}); err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	html := string(payload)
	for _, want := range []string{
		`href="#io-analysis"`, `id="io-analysis"`, "Подробный анализ файловых операций",
		"AttachmentStore.read", "база данных исключена", "2 из 3 · 66.7%",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("report misses %q", want)
		}
	}
}
