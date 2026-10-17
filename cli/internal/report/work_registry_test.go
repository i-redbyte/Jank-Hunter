package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func TestRegistryWithoutAttemptsHasSchedulesCoverageAndSafeJobDetails(t *testing.T) {
	summary := analyze.Summary{WorkRegistry: &analyze.WorkRegistryAnalysis{Snapshots: []analyze.WorkRegistrySnapshot{{Scope: "scope", Process: "app", Complete: false, CoverageReason: "missing_packets",
		Jobs: []jhlog.WorkRegistryJob{{UUID: "job</script><img src=x>", State: 4}}}}}}
	path := filepath.Join(t.TempDir(), "registry.html")
	if err := WriteInspectWithOptions(path, summary, ReportOptions{}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	html := string(b)
	for _, want := range []string{`id="workers"`, `data-work-registry`, `Реестр заданий WorkManager`, `не гарантирует запуск`, `зависимостями`, `значения Data не записываются`} {
		if !strings.Contains(html, want) {
			t.Fatalf("missing %q", want)
		}
	}
	if strings.Contains(html, "job</script><img") {
		t.Fatal("unsafe registry content")
	}
}
