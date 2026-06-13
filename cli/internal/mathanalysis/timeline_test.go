package mathanalysis

import (
	"path/filepath"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func TestAnalyzeInspectBuildsTimelineBuckets(t *testing.T) {
	path := writeTimelineFixture(t)

	report, err := AnalyzeInspect([]string{path}, analyze.Options{})
	if err != nil {
		t.Fatalf("AnalyzeInspect() error = %v", err)
	}

	if got, want := len(report.Timeline), 4; got != want {
		t.Fatalf("len(Timeline) = %d, want %d", got, want)
	}
	if got, want := report.Timeline[0].HTTPCount, 1; got != want {
		t.Fatalf("bucket0 HTTPCount = %d, want %d", got, want)
	}
	if got, want := report.Timeline[0].HTTPAvgDurationMS, uint64(100); got != want {
		t.Fatalf("bucket0 HTTPAvgDurationMS = %d, want %d", got, want)
	}
	if got, want := report.Timeline[0].DNSCount, 1; got != want {
		t.Fatalf("bucket0 DNSCount = %d, want %d", got, want)
	}
	if got, want := report.Timeline[0].DNSDurationMS, uint64(5); got != want {
		t.Fatalf("bucket0 DNSDurationMS = %d, want %d", got, want)
	}

	bucket1 := report.Timeline[1]
	if got, want := bucket1.HTTPCount, 2; got != want {
		t.Fatalf("bucket1 HTTPCount = %d, want %d", got, want)
	}
	if got, want := bucket1.HTTPFailed, 1; got != want {
		t.Fatalf("bucket1 HTTPFailed = %d, want %d", got, want)
	}
	if got, want := bucket1.HTTPAvgDurationMS, uint64(250); got != want {
		t.Fatalf("bucket1 HTTPAvgDurationMS = %d, want %d", got, want)
	}
	if got, want := bucket1.HTTPP95DurationMS, uint64(300); got != want {
		t.Fatalf("bucket1 HTTPP95DurationMS = %d, want %d", got, want)
	}
	if got, want := bucket1.ConnectCount, 1; got != want {
		t.Fatalf("bucket1 ConnectCount = %d, want %d", got, want)
	}
	if got, want := bucket1.ConnectDurationMS, uint64(50); got != want {
		t.Fatalf("bucket1 ConnectDurationMS = %d, want %d", got, want)
	}
	if got, want := bucket1.TTFBMS, uint64(85); got != want {
		t.Fatalf("bucket1 TTFBMS = %d, want %d", got, want)
	}
	if got, want := bucket1.UIFrames, uint64(60); got != want {
		t.Fatalf("bucket1 UIFrames = %d, want %d", got, want)
	}
	if got, want := bucket1.UIJankyFrames, uint64(6); got != want {
		t.Fatalf("bucket1 UIJankyFrames = %d, want %d", got, want)
	}

	bucket2 := report.Timeline[2]
	if got, want := bucket2.MemoryPSSKB, uint64(123000); got != want {
		t.Fatalf("bucket2 MemoryPSSKB = %d, want %d", got, want)
	}
	if got, want := bucket2.AvailableMemoryKB, uint64(1000); got != want {
		t.Fatalf("bucket2 AvailableMemoryKB = %d, want %d", got, want)
	}

	bucket3 := report.Timeline[3]
	if got, want := bucket3.TrafficRxBytes, uint64(600); got != want {
		t.Fatalf("bucket3 TrafficRxBytes = %d, want %d", got, want)
	}
	if got, want := bucket3.TrafficTxBytes, uint64(60); got != want {
		t.Fatalf("bucket3 TrafficTxBytes = %d, want %d", got, want)
	}
	if got, want := bucket3.UIFrames, uint64(30); got != want {
		t.Fatalf("bucket3 UIFrames = %d, want %d", got, want)
	}

	if !hasSeries(report.Series, "HTTP запросы") {
		t.Fatalf("report.Series does not include HTTP запросы: %#v", report.Series)
	}
	if !hasSeries(report.Series, "Дельта RX трафика") {
		t.Fatalf("report.Series does not include Дельта RX трафика: %#v", report.Series)
	}
}

func TestAnalyzeInspectTimelineHonorsFilters(t *testing.T) {
	path := writeTimelineFixture(t)

	report, err := AnalyzeInspect([]string{path}, analyze.Options{Filter: analyze.Filter{RouteContains: "/missing"}})
	if err != nil {
		t.Fatalf("AnalyzeInspect() error = %v", err)
	}
	for _, bucket := range report.Timeline {
		if bucket.HTTPCount != 0 {
			t.Fatalf("filtered timeline has HTTPCount=%d in bucket %+v", bucket.HTTPCount, bucket)
		}
	}
	if hasSeries(report.Series, "HTTP запросы") {
		t.Fatalf("filtered report should not include HTTP request series: %#v", report.Series)
	}
}

func writeTimelineFixture(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "timeline.jhlog")
	file, writer, err := jhlog.Create(path)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	entries := []jhlog.DictionaryEntry{
		{Kind: jhlog.DictOwner, ID: 1, Value: "FeedRepository.refresh"},
		{Kind: jhlog.DictRoute, ID: 2, Value: "GET /feed"},
		{Kind: jhlog.DictScreen, ID: 3, Value: "FeedScreen"},
		{Kind: jhlog.DictAppVersion, ID: 4, Value: "1.0"},
		{Kind: jhlog.DictBuild, ID: 5, Value: "100"},
		{Kind: jhlog.DictDevice, ID: 6, Value: "Pixel"},
		{Kind: jhlog.DictProcess, ID: 7, Value: "main"},
	}
	for _, entry := range entries {
		if err := writer.WriteEvent(jhlog.Event{Type: jhlog.EventDictionary, Dictionary: &entry}); err != nil {
			t.Fatalf("WriteEvent(dictionary) error = %v", err)
		}
	}

	events := []jhlog.Event{
		{Type: jhlog.EventSession, TimeMS: 1, Session: &jhlog.SessionEvent{AppVersionID: 4, BuildID: 5, DeviceID: 6, SDKInt: 35, ProcessID: 7}},
		{Type: jhlog.EventHTTP, TimeMS: 100, HTTP: &jhlog.HTTPEvent{OwnerID: 1, RouteID: 2, DurationMS: 100, DNSMS: 5, TTFBMS: 20, Status: jhlog.Status2xx}},
		{Type: jhlog.EventHTTP, TimeMS: 1200, HTTP: &jhlog.HTTPEvent{OwnerID: 1, RouteID: 2, DurationMS: 200, DNSMS: 10, ConnectMS: 50, TTFBMS: 80, Status: jhlog.Status2xx}},
		{Type: jhlog.EventHTTP, TimeMS: 1500, Flags: uint64(jhlog.FlagHTTPFailed), HTTP: &jhlog.HTTPEvent{OwnerID: 1, RouteID: 2, DurationMS: 300, TTFBMS: 90, Status: jhlog.Status5xx}},
		{Type: jhlog.EventUIWindow, TimeMS: 1600, UIWindow: &jhlog.UIWindowEvent{ScreenID: 3, WindowMS: 1000, FrameCount: 60, JankCount: 6, P95MS: 22}},
		{Type: jhlog.EventMemory, TimeMS: 2400, Memory: &jhlog.MemoryEvent{PSSKB: 123000, JavaHeapKB: 32000, NativeHeapKB: 18000}},
		{Type: jhlog.EventContext, TimeMS: 2500, Context: &jhlog.ContextEvent{Network: jhlog.NetworkWiFi, BatteryPct: 90, AvailMemoryKB: 1000, RxBytes: 1000, TxBytes: 200}},
		{Type: jhlog.EventUIWindow, TimeMS: 3200, UIWindow: &jhlog.UIWindowEvent{ScreenID: 3, WindowMS: 500, FrameCount: 30, JankCount: 3, P95MS: 28}},
		{Type: jhlog.EventContext, TimeMS: 3500, Context: &jhlog.ContextEvent{Network: jhlog.NetworkWiFi, BatteryPct: 89, AvailMemoryKB: 900, RxBytes: 1600, TxBytes: 260}},
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

func hasSeries(series []Series, name string) bool {
	for _, item := range series {
		if item.Name == name {
			return true
		}
	}
	return false
}
