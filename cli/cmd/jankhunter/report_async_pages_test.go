package main

import (
	"path/filepath"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/report"
)

func TestInspectReportSetAddsThreadAndWorkerPages(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inspect.html")
	summary := analyze.Summary{
		Title:          "async sample",
		ThreadAnalysis: &analyze.ThreadAnalysis{},
		WorkerAnalysis: &analyze.WorkerAnalysis{},
	}
	if err := writeInspectReportSetUsing(path, summary, nil, analyze.Options{}, report.ReportOptions{}, inspectReportSetWriters{
		primary: report.WriteInspectWithOptions,
		math:    report.WriteMathInspectWithOptions,
	}); err != nil {
		t.Fatal(err)
	}
	pages, err := readReportBundlePages(path)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"overview": false, "threads": false, "workers": false}
	for _, page := range pages {
		if _, ok := want[page.ID]; ok {
			want[page.ID] = true
		}
	}
	for id, found := range want {
		if !found {
			t.Errorf("missing report page %s", id)
		}
	}
}
