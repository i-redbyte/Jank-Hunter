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

func TestNetworkLoopMissingAttributionOnlyInDataQuality(t *testing.T) {
	t.Setenv("JH_LANG", "ru")
	dir := t.TempDir()
	input := filepath.Join(dir, "missing-owner.jhlog")
	h := jhlog.DefaultSegmentHeader()
	h.RunID = jhlog.ID128{1}
	h.ProcessInstanceID = jhlog.ID128{1}
	h.SessionID = jhlog.ID128{1}
	f, w, err := jhlog.CreateWithHeader(input, h)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.WriteEvent(jhlog.Event{Type: jhlog.EventSession, TimeMS: 1, Session: &jhlog.SessionEvent{CollectorFlags: uint64(jhlog.CollectorHTTP)}}); err != nil {
		t.Fatal(err)
	}
	d := jhlog.DictionaryEntry{Kind: jhlog.DictRoute, ID: 1, Value: "GET /config"}
	if err := w.WriteEvent(jhlog.Event{Type: jhlog.EventDictionary, Dictionary: &d}); err != nil {
		t.Fatal(err)
	}
	for bucket := uint64(0); bucket <= 48; bucket += 4 {
		for offset := uint64(0); offset < 3; offset++ {
			if err := w.WriteEvent(jhlog.Event{Type: jhlog.EventHTTP, TimeMS: bucket*1000 + 100 + offset*10, HTTP: &jhlog.HTTPEvent{RouteRef: jhlog.LocalSymbol(1), DurationMS: 150, DNSMS: 50, Status: jhlog.Status2xx}}); err != nil {
				t.Fatal(err)
			}
		}
	}
	w.SetQualitySnapshot(jhlog.QualitySnapshot{CapturedElapsedUS: 49001000, Counters: map[uint64]uint64{jhlog.QualityCollectionWindowStartElapsedMS: 1, jhlog.QualityCollectionWindowEndElapsedMS: 49001}})
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	paths := []string{input}
	summary, err := analyze.InspectFilesWithOptions("fixture", paths, analyze.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, compare := range []bool{false, true} {
		out := filepath.Join(dir, "report.html")
		if compare {
			model, err := mathanalysis.AnalyzeCompareWithSummaries(paths, paths, analyze.Options{}, summary, summary)
			if err != nil {
				t.Fatal(err)
			}
			err = WriteMathCompareWithOptions(out, model, ReportOptions{})
			if err != nil {
				t.Fatal(err)
			}
		} else {
			model, err := mathanalysis.AnalyzeInspectWithSummary(paths, analyze.Options{}, summary)
			if err != nil {
				t.Fatal(err)
			}
			if len(model.NetworkLoops) == 0 {
				t.Fatalf("no loop in fixture; timeline=%+v limits=%+v", model.Timeline[:min(5, len(model.Timeline))], model.CollectionLimits)
			}
			err = WriteMathInspectWithOptions(out, model, ReportOptions{})
			if err != nil {
				t.Fatal(err)
			}
		}
		data, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		html := string(data)
		start := strings.Index(html, `id="data-quality"`)
		if start < 0 {
			t.Fatal("missing quality section")
		}
		end := strings.Index(html[start:], "</section>")
		if end < 0 {
			t.Fatal("missing quality section end")
		}
		end += start
		for _, marker := range []string{"Контекст сетевых циклов", "ASM-хуки сетевого клиента"} {
			if !strings.Contains(html[start:end], marker) || strings.Contains(html[:start], marker) || strings.Contains(html[end:], marker) {
				t.Errorf("compare=%v: attribution diagnostic outside quality or absent: %q", compare, marker)
				if at := strings.Index(html[:start], marker); at >= 0 {
					t.Log(html[max(0, at-180):min(len(html), at+200)])
				}
				if at := strings.Index(html[end:], marker); at >= 0 {
					at += end
					t.Log(html[max(0, at-180):min(len(html), at+200)])
				}
			}
		}
	}
}
