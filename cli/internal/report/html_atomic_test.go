package report

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
)

func TestHTMLReportAtomicallyReplacesExistingOutput(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "report.html")
	if err := os.WriteFile(path, []byte("old report"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := WriteInspectWithOptions(path, analyze.Summary{Title: "atomic report"}, ReportOptions{}); err != nil {
		t.Fatal(err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) == "old report" {
		t.Fatal("report output was not replaced")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("mode = %o, want preserved 600", got)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() != filepath.Base(path) {
			t.Fatalf("unexpected temporary report output: %s", entry.Name())
		}
	}
}
