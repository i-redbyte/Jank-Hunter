package report_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/benchfixture"
	"github.com/i-redbyte/jank-hunter/cli/internal/report"
)

func BenchmarkWriteInspectRepresentative(b *testing.B) {
	profile, err := benchfixture.ProfileByName("representative")
	if err != nil {
		b.Fatal(err)
	}
	directory := b.TempDir()
	logPath := filepath.Join(directory, "representative.jhlog")
	if _, err := benchfixture.Write(logPath, profile); err != nil {
		b.Fatal(err)
	}
	summary, err := analyze.InspectFilesWithOptions("benchmark", []string{logPath}, analyze.Options{})
	if err != nil {
		b.Fatal(err)
	}
	reportPath := filepath.Join(directory, "inspect.html")

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := report.WriteInspectWithOptions(reportPath, summary, report.ReportOptions{}); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	stat, err := os.Stat(reportPath)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportMetric(float64(stat.Size()), "html-bytes/op")
}
