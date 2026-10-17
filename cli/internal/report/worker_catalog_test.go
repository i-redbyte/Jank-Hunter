package report

import (
	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCatalogWithoutWorkerExecutionsIsVisibleAndEscaped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.html")
	summary := analyze.Summary{WorkerCatalog: &analyze.WorkerCatalogAnalysis{Observations: []analyze.WorkerCatalogObservation{{Process: "app", Catalog: &jhlog.WorkerCatalogEvent{Schema: 1, Status: "available", Classes: []string{"app.NeverRun</script><img src=x>"}}}}}}
	if err := WriteInspectWithOptions(path, summary, ReportOptions{}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	html := string(data)
	for _, want := range []string{`id="workers"`, `Каталог классов`, `data-worker-catalog`, `До R8`, `не доказывает неиспользование`} {
		if !strings.Contains(html, want) {
			t.Fatalf("missing %q", want)
		}
	}
	if strings.Contains(html, "app.NeverRun</script><img") {
		t.Fatal("unsafe catalog class injection")
	}
}
