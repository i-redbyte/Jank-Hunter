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

func TestHTTPCountCoverageDiagnosticsOnlyInMathematicalDataQuality(t *testing.T) {
	t.Setenv("JH_LANG", "ru")
	dir := t.TempDir()
	path := filepath.Join(dir, "unknown.jhlog")
	f, w, err := jhlog.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range []jhlog.Event{
		{Type: jhlog.EventSession, Session: &jhlog.SessionEvent{}},
		{Type: jhlog.EventMemory, TimeMS: 3000, Memory: &jhlog.MemoryEvent{PSSKB: 100}},
	} {
		if err := w.WriteEvent(event); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
	summary, err := analyze.InspectFilesWithOptions("counts", []string{path}, analyze.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, compare := range []bool{false, true} {
		out := filepath.Join(dir, "report.html")
		if compare {
			report, err := mathanalysis.AnalyzeCompareWithSummaries([]string{path}, []string{path}, analyze.Options{}, summary, summary)
			if err != nil {
				t.Fatal(err)
			}
			if err := WriteMathCompareWithOptions(out, report, ReportOptions{}); err != nil {
				t.Fatal(err)
			}
		} else {
			report, err := mathanalysis.AnalyzeInspectWithSummary([]string{path}, analyze.Options{}, summary)
			if err != nil {
				t.Fatal(err)
			}
			if err := WriteMathInspectWithOptions(out, report, ReportOptions{}); err != nil {
				t.Fatal(err)
			}
		}
		raw, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		html := string(raw)
		start := strings.Index(html, `id="data-quality"`)
		if start < 0 {
			t.Fatal("quality section missing")
		}
		end := strings.Index(html[start:], "</section>")
		if end < 0 {
			t.Fatal("quality section not closed")
		}
		end += start
		marker := "Наблюдение HTTP-счётчиков"
		if !strings.Contains(html[start:end], marker) || strings.Contains(html[:start], marker) || strings.Contains(html[end:], marker) {
			t.Errorf("compare=%v: count diagnostics missing or outside quality", compare)
		}
	}
}

func TestHTTPCountTableDoesNotDisplayUnknownZero(t *testing.T) {
	format, ok := reportTemplateFuncs()["httpCountValue"].(func(mathanalysis.TimelineBucket, int) string)
	if !ok {
		t.Fatal("network table has no availability-aware count formatter")
	}
	for _, tc := range []struct {
		state string
		value int
		want  string
	}{
		{mathanalysis.CountObserved, 0, "0"},
		{mathanalysis.CountMissing, 0, "н/д"},
		{mathanalysis.CountUnsupported, 0, "н/д"},
		{mathanalysis.CountMissing, 3, "≥ 3"},
	} {
		if got := format(mathanalysis.TimelineBucket{HTTPCountState: tc.state}, tc.value); got != tc.want {
			t.Errorf("%s count%d=%s want%s", tc.state, tc.value, got, tc.want)
		}
	}
}
