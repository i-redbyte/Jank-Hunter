package report

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
)

func TestRuntimeGraphV9JSONAndHTMLGoldens(t *testing.T) {
	fixtures := []string{
		"runtime-graph-legacy-v9",
		"runtime-graph-buffered-v9",
		"runtime-graph-incomplete-v9",
	}
	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			base := filepath.Join("testdata", name)
			summary, err := analyze.InspectFilesWithOptions(name, []string{base + ".jhlog"}, analyze.Options{})
			if err != nil {
				t.Fatal(err)
			}
			actualJSON, err := json.MarshalIndent(summary, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			assertGoldenBytes(t, base+".golden.json", append(actualJSON, '\n'))

			actualHTML := filepath.Join(t.TempDir(), name+".html")
			if err := WriteInspectWithOptions(actualHTML, summary, ReportOptions{
				GeneratedAt: "2026-08-08T00:00:00Z",
			}); err != nil {
				t.Fatal(err)
			}
			actual, err := os.ReadFile(actualHTML)
			if err != nil {
				t.Fatal(err)
			}
			assertGoldenBytes(t, base+".golden.html", normalizeGoldenHTML(actual))
		})
	}
}

func normalizeGoldenHTML(payload []byte) []byte {
	lines := bytes.Split(payload, []byte{'\n'})
	for index := range lines {
		lines[index] = bytes.TrimRight(lines[index], " \t")
	}
	return bytes.Join(lines, []byte{'\n'})
}

func TestRuntimeGraphLegacyAndBufferedFixturesPreserveProjectedTotals(t *testing.T) {
	legacy, err := analyze.InspectFilesWithOptions(
		"legacy", []string{filepath.Join("testdata", "runtime-graph-legacy-v9.jhlog")}, analyze.Options{},
	)
	if err != nil {
		t.Fatal(err)
	}
	buffered, err := analyze.InspectFilesWithOptions(
		"buffered", []string{filepath.Join("testdata", "runtime-graph-buffered-v9.jhlog")}, analyze.Options{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(legacy.RuntimeCalls) != 1 || len(buffered.RuntimeCalls) != 2 {
		t.Fatalf("context split mismatch: legacy=%+v buffered=%+v", legacy.RuntimeCalls, buffered.RuntimeCalls)
	}
	legacyCount, legacyTotal, legacyMax := runtimeGraphTotals(legacy)
	bufferedCount, bufferedTotal, bufferedMax := runtimeGraphTotals(buffered)
	if legacyCount != bufferedCount || legacyTotal != bufferedTotal || legacyMax != bufferedMax {
		t.Fatalf("projected totals changed: legacy=%d/%d/%d buffered=%d/%d/%d",
			legacyCount, legacyTotal, legacyMax, bufferedCount, bufferedTotal, bufferedMax)
	}
}

func TestIncompleteFixtureReportsCircuitBreakerAndTwentyPercentCompleteness(t *testing.T) {
	summary, err := analyze.InspectFilesWithOptions(
		"incomplete", []string{filepath.Join("testdata", "runtime-graph-incomplete-v9.jhlog")}, analyze.Options{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if summary.CollectionQuality.RuntimeGraphCompletenessRatio != 0.2 || summary.CollectionQuality.Complete {
		t.Fatalf("incomplete fixture quality = %+v", summary.CollectionQuality)
	}
}

func assertGoldenBytes(t *testing.T, path string, actual []byte) {
	t.Helper()
	expected, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(expected, actual) {
		t.Fatalf("golden mismatch for %s; regenerate with `go run ./testdata/generate.go`", path)
	}
}

func runtimeGraphTotals(summary analyze.Summary) (count, total, max uint64) {
	for _, edge := range summary.RuntimeCalls {
		count += edge.Count
		total += edge.TotalMS
		if edge.MaxMS > max {
			max = edge.MaxMS
		}
	}
	return count, total, max
}
