package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
	"github.com/i-redbyte/jank-hunter/cli/internal/mathanalysis"
)

func TestAcquisitionMethodologyOnlyInMathematicalDataQuality(t *testing.T) {
	for _, known := range []bool{false, true} {
		name := "unknown"
		if known {
			name = "known"
		}
		t.Run(name, func(t *testing.T) {
			var paths []string
			for run := byte(1); run <= 2; run++ {
				path := filepath.Join(t.TempDir(), "input.jhlog")
				header := jhlog.DefaultSegmentHeader()
				header.RunID[0] = run
				if known {
					header.ProcessInstanceID[0] = 7
				}
				file, writer, err := jhlog.CreateWithHeader(path, header)
				if err != nil {
					t.Fatal(err)
				}
				for i := 0; i < 10; i++ {
					if err := writer.WriteEvent(jhlog.Event{Type: jhlog.EventHTTP, TimeMS: uint64(1000 + i*1000), HTTP: &jhlog.HTTPEvent{DurationMS: 100, Status: jhlog.Status2xx}}); err != nil {
						t.Fatal(err)
					}
				}
				if err := writer.CloseWithReason(jhlog.SegmentEndNormal); err != nil {
					t.Fatal(err)
				}
				if err := file.Close(); err != nil {
					t.Fatal(err)
				}
				paths = append(paths, path)
			}
			summary, err := analyze.InspectFilesWithOptions("acquisitions", paths, analyze.Options{})
			if err != nil {
				t.Fatal(err)
			}
			inspected, err := mathanalysis.AnalyzeInspectWithSummary(paths, analyze.Options{}, summary)
			if err != nil {
				t.Fatal(err)
			}
			compared, err := mathanalysis.AnalyzeCompareWithSummaries(paths, paths, analyze.Options{}, summary, summary)
			if err != nil {
				t.Fatal(err)
			}
			for _, compare := range []bool{false, true} {
				dir := t.TempDir()
				mathPath := filepath.Join(dir, "math.html")
				overview := filepath.Join(dir, "overview.html")
				if compare {
					err = WriteMathCompareWithOptions(mathPath, compared, ReportOptions{})
					if err != nil {
						t.Fatal(err)
					}
					err = WriteCompareReportWithOptions(overview, compared.Comparison, nil, nil, ReportOptions{})
				} else {
					err = WriteMathInspectWithOptions(mathPath, inspected, ReportOptions{})
					if err != nil {
						t.Fatal(err)
					}
					err = WriteInspectWithOptions(overview, summary, ReportOptions{})
				}
				if err != nil {
					t.Fatal(err)
				}
				markers := []string{"Единицы повторения", "Совмещённых временных шкал: 2", "Ротация не создаёт независимых повторов"}
				if known {
					markers = append(markers, "Групп сбора по RunID и ProcessInstanceID: 1")
				} else {
					markers = append(markers, "Независимость сборов неизвестна")
				}
				data, err := os.ReadFile(mathPath)
				if err != nil {
					t.Fatal(err)
				}
				html := string(data)
				start := strings.Index(html, `id="data-quality"`)
				if start < 0 {
					t.Fatal("missing data-quality")
				}
				end := strings.Index(html[start:], "</section>")
				if end < 0 {
					t.Fatal("missing section end")
				}
				end += start
				for _, marker := range markers {
					if !strings.Contains(html[start:end], marker) || strings.Contains(html[:start]+html[end:], marker) {
						t.Fatalf("marker outside quality section or absent: %s", marker)
					}
					assertHTMLNotContains(t, overview, marker)
				}
			}
		})
	}
}
