package report

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
)

func TestRuntimeGraphJH100JSONAndHTMLGoldens(t *testing.T) {
	fixtures := []string{
		"runtime-graph-contextual-100",
		"runtime-graph-incomplete-100",
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

func TestIncompleteFixtureReportsTwentyPercentCompleteness(t *testing.T) {
	summary, err := analyze.InspectFilesWithOptions(
		"incomplete", []string{filepath.Join("testdata", "runtime-graph-incomplete-100.jhlog")}, analyze.Options{},
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
