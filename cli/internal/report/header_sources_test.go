package report

import (
	"html/template"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
)

func TestInspectHeaderPreservesEachInputPath(t *testing.T) {
	paths := []string{
		"/logs/first.jhlog",
		"/logs/release, candidate.jhlog",
		"/logs/<script>alert('file')</script>&.jhlog",
	}
	for _, test := range []struct {
		name  string
		paths []string
		title string
		want  []string
	}{
		{"multiple files", paths, strings.Join(paths, ", "), paths},
		{"custom title", nil, "Run A, foreground", []string{"Run A, foreground"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "inspect.html")
			if err := WriteInspectWithOptions(path, analyze.Summary{Title: test.title}, ReportOptions{
				SourcePaths: test.paths,
				GeneratedAt: "2026-09-24T17:40:30Z",
			}); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			header, _, ok := strings.Cut(string(data), "</header>")
			if !ok {
				t.Fatal("report header is missing")
			}
			if got := strings.Count(header, `<li class="report-source">`); got != len(test.want) {
				t.Fatalf("source rows = %d, want %d", got, len(test.want))
			}
			for _, source := range test.want {
				if !strings.Contains(header, `<li class="report-source">`+template.HTMLEscapeString(source)+`</li>`) {
					t.Errorf("source is not preserved as one escaped row: %q", source)
				}
			}
			if !strings.Contains(header, `<div class="report-created">Создан `) {
				t.Fatal("creation date must have its own row")
			}
		})
	}
}
