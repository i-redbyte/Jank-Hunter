package mathanalysis

import (
	"math"
	"path/filepath"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func TestSummarizeRobustSetComputesDeterministicStats(t *testing.T) {
	set := &robustSampleSet{}
	for i := 1; i <= 100; i++ {
		set.add(float64(i))
	}

	stat := summarizeRobustSet(robustKey{Dimension: "Маршрут", Name: "GET /items", Metric: "HTTP задержка", Unit: "ms"}, set)

	assertFloat(t, stat.Median, 50.5)
	assertFloat(t, stat.P90, 90)
	assertFloat(t, stat.P95, 95)
	assertFloat(t, stat.P99, 99)
	assertFloat(t, stat.MAD, 25)
	assertFloat(t, stat.TrimmedMean, 50.5)
	if !stat.HasP95Confidence {
		t.Fatalf("expected p95 bootstrap CI")
	}
	if stat.SampleQuality != "хорошая" || stat.SampleQualitySeverity != "ok" {
		t.Fatalf("unexpected sample quality: %+v", stat)
	}
}

func TestCompareRobustSamplesComputesCliffsDelta(t *testing.T) {
	key := robustKey{Dimension: "Маршрут", Name: "GET /items", Metric: "HTTP задержка", Unit: "ms"}
	baseline := robustSampleMap{key: robustSet(1, 2, 3)}
	candidate := robustSampleMap{key: robustSet(2, 3, 4)}

	deltas := compareRobustSamples(baseline, candidate)
	if len(deltas) != 1 {
		t.Fatalf("len(deltas) = %d, want 1", len(deltas))
	}

	delta := deltas[0]
	assertFloat(t, delta.BaselineP95, 3)
	assertFloat(t, delta.CandidateP95, 4)
	assertFloat(t, delta.P95DeltaPct, 100.0/3.0)
	assertFloat(t, delta.CliffDelta, 5.0/9.0)
	if delta.EffectSize != "крупный" {
		t.Fatalf("EffectSize = %q, want крупный", delta.EffectSize)
	}
	if delta.Confidence != "низкое" {
		t.Fatalf("Confidence = %q, want низкое", delta.Confidence)
	}
}

func TestAnalyzeInspectBuildsRobustStats(t *testing.T) {
	path := writeRobustFixture(t)

	report, err := AnalyzeInspect([]string{path}, analyze.Options{})
	if err != nil {
		t.Fatalf("AnalyzeInspect() error = %v", err)
	}

	route := findRobustStat(report.RobustStats, "Маршрут", "GET /feed", "HTTP задержка")
	if route == nil {
		t.Fatalf("route robust stat not found: %#v", report.RobustStats)
	}
	if route.Count != 3 || route.P95 != 300 {
		t.Fatalf("unexpected route stat: %+v", *route)
	}

	screen := findRobustStat(report.RobustStats, "Экран", "FeedScreen", "UI p95 кадра")
	if screen == nil {
		t.Fatalf("screen robust stat not found: %#v", report.RobustStats)
	}
	if screen.Count != 2 || screen.P95 != 32 {
		t.Fatalf("unexpected screen stat: %+v", *screen)
	}

	owner := findRobustStat(report.RobustStats, "Источник", "FeedRepository.refresh", "Пауза главного потока")
	if owner == nil {
		t.Fatalf("owner robust stat not found: %#v", report.RobustStats)
	}
	if owner.Count != 1 || owner.P95 != 42 {
		t.Fatalf("unexpected owner stat: %+v", *owner)
	}

	gauge := findRobustStat(report.RobustStats, "Gauge-метрика", "executor.queue.depth", "Значение")
	if gauge == nil {
		t.Fatalf("gauge robust stat not found: %#v", report.RobustStats)
	}
	if gauge.Count != 2 || gauge.Median != 15 || gauge.P95 != 20 {
		t.Fatalf("unexpected gauge stat: %+v", *gauge)
	}
}

func TestAnalyzeInspectRobustStatsHonorClassFilter(t *testing.T) {
	path := writeRobustFixture(t)

	report, err := AnalyzeInspect([]string{path}, analyze.Options{
		Filter: analyze.Filter{ClassContains: "CheckoutActivity"},
	})
	if err != nil {
		t.Fatalf("AnalyzeInspect() error = %v", err)
	}

	checkout := findRobustStat(report.RobustStats, "Источник", "com.app.CheckoutActivity", "Возраст удержанного объекта")
	if checkout == nil {
		t.Fatalf("checkout retained stat not found: %#v", report.RobustStats)
	}
	if checkout.Count != 1 || checkout.P95 != 30_000 {
		t.Fatalf("unexpected checkout retained stat: %+v", *checkout)
	}
	if feed := findRobustStat(report.RobustStats, "Источник", "com.app.FeedActivity", "Возраст удержанного объекта"); feed != nil {
		t.Fatalf("class filter leaked feed retained stat: %+v", *feed)
	}
}

func robustSet(values ...float64) *robustSampleSet {
	set := &robustSampleSet{}
	for _, value := range values {
		set.add(value)
	}
	return set
}

func writeRobustFixture(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "robust.jhlog")
	file, writer, err := jhlog.Create(path)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	entries := []jhlog.DictionaryEntry{
		{Kind: jhlog.DictOwner, ID: 1, Value: "FeedRepository.refresh"},
		{Kind: jhlog.DictRoute, ID: 2, Value: "GET /feed"},
		{Kind: jhlog.DictScreen, ID: 3, Value: "FeedScreen"},
		{Kind: jhlog.DictMetric, ID: 4, Value: "executor.queue.depth"},
		{Kind: jhlog.DictClass, ID: 5, Value: "com.app.CheckoutActivity"},
		{Kind: jhlog.DictClass, ID: 6, Value: "com.app.FeedActivity"},
	}
	for _, entry := range entries {
		if err := writer.WriteEvent(jhlog.Event{Type: jhlog.EventDictionary, Dictionary: &entry}); err != nil {
			t.Fatalf("WriteEvent(dictionary) error = %v", err)
		}
	}
	events := []jhlog.Event{
		{Type: jhlog.EventHTTP, TimeMS: 100, HTTP: &jhlog.HTTPEvent{OwnerID: 1, RouteID: 2, DurationMS: 100, DNSMS: 5, ConnectMS: 10, Status: jhlog.Status2xx}},
		{Type: jhlog.EventHTTP, TimeMS: 200, HTTP: &jhlog.HTTPEvent{OwnerID: 1, RouteID: 2, DurationMS: 200, DNSMS: 7, ConnectMS: 12, Status: jhlog.Status2xx}},
		{Type: jhlog.EventHTTP, TimeMS: 300, HTTP: &jhlog.HTTPEvent{OwnerID: 1, RouteID: 2, DurationMS: 300, DNSMS: 9, ConnectMS: 14, Status: jhlog.Status2xx}},
		{Type: jhlog.EventUIWindow, TimeMS: 1100, UIWindow: &jhlog.UIWindowEvent{ScreenID: 3, WindowMS: 1000, FrameCount: 60, JankCount: 3, P95MS: 24}},
		{Type: jhlog.EventUIWindow, TimeMS: 2100, UIWindow: &jhlog.UIWindowEvent{ScreenID: 3, WindowMS: 1000, FrameCount: 60, JankCount: 9, P95MS: 32}},
		{Type: jhlog.EventStall, TimeMS: 2200, Stall: &jhlog.StallEvent{OwnerID: 1, DurationMS: 42}},
		{Type: jhlog.EventGauge, TimeMS: 2300, Metric: &jhlog.MetricEvent{MetricID: 4, Value: 10}},
		{Type: jhlog.EventGauge, TimeMS: 2400, Metric: &jhlog.MetricEvent{MetricID: 4, Value: 20}},
		{Type: jhlog.EventRetained, TimeMS: 2500, Retained: &jhlog.RetainedEvent{ClassID: 5, AgeMS: 30_000, Count: 1}},
		{Type: jhlog.EventRetained, TimeMS: 2600, Retained: &jhlog.RetainedEvent{ClassID: 6, AgeMS: 40_000, Count: 1}},
	}
	for _, event := range events {
		if err := writer.WriteEvent(event); err != nil {
			t.Fatalf("WriteEvent(%v) error = %v", event.Type, err)
		}
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	return path
}

func findRobustStat(stats []RobustStat, dimension, name, metric string) *RobustStat {
	for i := range stats {
		if stats[i].Dimension == dimension && stats[i].Name == name && stats[i].Metric == metric {
			return &stats[i]
		}
	}
	return nil
}

func assertFloat(t *testing.T, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 0.0001 {
		t.Fatalf("got %.6f, want %.6f", got, want)
	}
}
