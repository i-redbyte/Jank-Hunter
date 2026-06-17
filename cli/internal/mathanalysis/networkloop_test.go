package mathanalysis

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func TestAnalyzeInspectDetectsDNSNetworkLoop(t *testing.T) {
	path := writeDNSLoopFixture(t, true)

	report, err := AnalyzeInspect([]string{path}, analyze.Options{})
	if err != nil {
		t.Fatalf("AnalyzeInspect() error = %v", err)
	}

	loop := findNetworkLoop(report.NetworkLoops, func(loop NetworkLoopFinding) bool {
		return loop.Route == "GET /config" &&
			loop.Owner == "ConfigRepository.refresh" &&
			containsToken(loop.Motif, "dns_high")
	})
	if loop == nil {
		t.Fatalf("DNS network loop was not detected: %+v", report.NetworkLoops)
	}
	if loop.PeriodMS < 3000 || loop.PeriodMS > 5000 {
		t.Fatalf("loop period = %d ms, want around 4000 ms", loop.PeriodMS)
	}
	if loop.Confidence <= 0.35 {
		t.Fatalf("loop confidence = %.3f, want > 0.35", loop.Confidence)
	}
	if len(loop.Path.Nodes) == 0 {
		t.Fatalf("loop path is empty: %+v", loop)
	}
}

func TestAnalyzeInspectDetectsDNSNetworkLoopWithAbsoluteOffset(t *testing.T) {
	path := writeDNSLoopFixtureWithBase(t, true, 12*60*60*1000)

	report, err := AnalyzeInspect([]string{path}, analyze.Options{})
	if err != nil {
		t.Fatalf("AnalyzeInspect() error = %v", err)
	}

	loop := findNetworkLoop(report.NetworkLoops, func(loop NetworkLoopFinding) bool {
		return loop.Route == "GET /config" &&
			loop.Owner == "ConfigRepository.refresh" &&
			containsToken(loop.Motif, "dns_high")
	})
	if loop == nil {
		t.Fatalf("DNS network loop was not detected: %+v", report.NetworkLoops)
	}
	if loop.FirstMS > 2*DefaultBucketMS {
		t.Fatalf("loop FirstMS = %d, want normalized offset near zero", loop.FirstMS)
	}
	if len(report.Timeline) > 32 {
		t.Fatalf("timeline buckets = %d, want compact normalized timeline", len(report.Timeline))
	}
}

func TestAnalyzeInspectDetectsMetricOnlyReconnectLoop(t *testing.T) {
	path := writeReconnectLoopFixture(t)

	report, err := AnalyzeInspect([]string{path}, analyze.Options{})
	if err != nil {
		t.Fatalf("AnalyzeInspect() error = %v", err)
	}

	loop := findNetworkLoop(report.NetworkLoops, func(loop NetworkLoopFinding) bool {
		return loop.Owner == "configrepository_refresh" &&
			containsToken(loop.Motif, "websocket_reconnect")
	})
	if loop == nil {
		t.Fatalf("websocket reconnect loop was not detected: %+v", report.NetworkLoops)
	}
	if loop.PeriodMS < 4000 || loop.PeriodMS > 6000 {
		t.Fatalf("loop period = %d ms, want around 5000 ms", loop.PeriodMS)
	}
}

func TestAnalyzeCompareReportsAppearedNetworkLoop(t *testing.T) {
	baseline := writeDNSLoopFixture(t, false)
	candidate := writeDNSLoopFixture(t, true)

	report, err := AnalyzeCompare([]string{baseline}, []string{candidate}, analyze.Options{})
	if err != nil {
		t.Fatalf("AnalyzeCompare() error = %v", err)
	}

	for _, delta := range report.NetworkLoopDeltas {
		if delta.Status == "появился" && delta.Route == "GET /config" && delta.CandidatePeriodMS > 0 {
			return
		}
	}
	t.Fatalf("appeared network loop delta was not reported: %+v", report.NetworkLoopDeltas)
}

func writeDNSLoopFixture(t *testing.T, loop bool) string {
	return writeDNSLoopFixtureWithBase(t, loop, 0)
}

func writeDNSLoopFixtureWithBase(t *testing.T, loop bool, baseMS uint64) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "dns-loop.jhlog")
	file, writer, err := jhlog.Create(path)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	for _, entry := range []jhlog.DictionaryEntry{
		{Kind: jhlog.DictOwner, ID: 1, Value: "ConfigRepository.refresh"},
		{Kind: jhlog.DictRoute, ID: 2, Value: "GET /config"},
	} {
		if err := writer.WriteEvent(jhlog.Event{Type: jhlog.EventDictionary, Dictionary: &entry}); err != nil {
			t.Fatalf("WriteEvent(dictionary) error = %v", err)
		}
	}

	if loop {
		for bucket := uint64(0); bucket <= 24; bucket += 4 {
			for offset := uint64(0); offset < 3; offset++ {
				event := jhlog.Event{
					Type:   jhlog.EventHTTP,
					TimeMS: baseMS + bucket*DefaultBucketMS + 100 + offset*80,
					HTTP: &jhlog.HTTPEvent{
						OwnerID:    1,
						RouteID:    2,
						DurationMS: 180,
						DNSMS:      70,
						TTFBMS:     80,
						Status:     jhlog.Status2xx,
					},
				}
				if err := writer.WriteEvent(event); err != nil {
					t.Fatalf("WriteEvent(http) error = %v", err)
				}
			}
		}
	} else {
		for _, timeMS := range []uint64{100, 11_000, 24_000} {
			event := jhlog.Event{
				Type:   jhlog.EventHTTP,
				TimeMS: baseMS + timeMS,
				HTTP: &jhlog.HTTPEvent{
					OwnerID:    1,
					RouteID:    2,
					DurationMS: 120,
					TTFBMS:     70,
					Status:     jhlog.Status2xx,
				},
			}
			if err := writer.WriteEvent(event); err != nil {
				t.Fatalf("WriteEvent(http) error = %v", err)
			}
		}
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	return path
}

func writeReconnectLoopFixture(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "reconnect-loop.jhlog")
	file, writer, err := jhlog.Create(path)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	entry := jhlog.DictionaryEntry{Kind: jhlog.DictMetric, ID: 10, Value: "websocket.configrepository_refresh.reconnect.count"}
	if err := writer.WriteEvent(jhlog.Event{Type: jhlog.EventDictionary, Dictionary: &entry}); err != nil {
		t.Fatalf("WriteEvent(dictionary) error = %v", err)
	}
	for bucket := uint64(0); bucket <= 30; bucket += 5 {
		event := jhlog.Event{
			Type:   jhlog.EventCounter,
			TimeMS: bucket * DefaultBucketMS,
			Metric: &jhlog.MetricEvent{
				MetricID: 10,
				Value:    1,
			},
		}
		if err := writer.WriteEvent(event); err != nil {
			t.Fatalf("WriteEvent(counter) error = %v", err)
		}
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	return path
}

func findNetworkLoop(loops []NetworkLoopFinding, predicate func(NetworkLoopFinding) bool) *NetworkLoopFinding {
	for index := range loops {
		if predicate(loops[index]) {
			return &loops[index]
		}
	}
	return nil
}

func containsToken(tokens []string, needle string) bool {
	for _, token := range tokens {
		if token == needle || strings.Contains(token, needle) {
			return true
		}
	}
	return false
}
