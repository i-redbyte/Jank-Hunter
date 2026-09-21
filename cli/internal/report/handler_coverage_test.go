package report

import (
	"encoding/json"
	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/mathanalysis"
	"path/filepath"
	"testing"
)

func TestHandlerUnknownAttributionIsVisibleSeparatelyFromDeliveryCompleteness(t *testing.T) {
	quality := sampleCollectionQuality()
	if err := json.Unmarshal([]byte(`{"async_attribution":{"status":"unknown","handler_posts_without_context":7}}`), &quality); err != nil {
		t.Fatal(err)
	}
	summary := analyze.Summary{Title: "handler", CollectionQuality: quality}
	for _, name := range []string{"inspect", "compare", "math", "math-compare"} {
		path := filepath.Join(t.TempDir(), name+".html")
		var err error
		switch name {
		case "inspect":
			err = WriteInspectWithOptions(path, summary, ReportOptions{})
		case "compare":
			err = WriteCompareReportWithOptions(path, analyze.Compare(summary, summary), nil, nil, ReportOptions{})
		case "math":
			err = WriteMathInspectWithOptions(path, sampleMathReport(summary), ReportOptions{})
		case "math-compare":
			err = WriteMathCompareWithOptions(path, mathanalysis.CompareMathReport{Comparison: analyze.Compare(summary, summary)}, ReportOptions{})
		}
		if err != nil {
			t.Fatal(err)
		}
		if name == "inspect" || name == "compare" {
			assertHTMLNotContains(t, path, "Связь отправки с выполнением: unknown", "post без контекста: 7")
		} else {
			assertHTMLContains(t, path, "Связь отправки с выполнением: unknown", "post без контекста: 7")
		}
		assertHTMLNotContains(t, path, "полнота неизвестна")
	}
}
