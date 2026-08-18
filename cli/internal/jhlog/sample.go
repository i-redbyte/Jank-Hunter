package jhlog

import (
	"os"

	"github.com/i-redbyte/jank-hunter/cli/internal/atomicfile"
)

func WriteSample(path string) error {
	return atomicfile.Write(path, 0o644, writeSample)
}

func writeSample(file *os.File) error {
	header := DefaultSegmentHeader()
	header.RunID[0] = 1
	header.ProcessInstanceID[0] = 1
	header.SessionID[0] = 1
	header.OSPID = 1
	header.IdentitySource = 1
	header.ProcessName = "main"
	header.SymbolNamespace = []byte{
		0x00, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77,
		0x88, 0x99, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff,
	}
	writer, err := NewWriterWithHeader(file, header)
	if err != nil {
		return err
	}

	entries := []DictionaryEntry{
		{Kind: DictAppVersion, ID: 1, Value: "0.1.0-debug"},
		{Kind: DictBuild, ID: 2, Value: "100"},
		{Kind: DictDevice, ID: 3, Value: "Pixel 8 / API 35"},
		{Kind: DictOwner, ID: 10, Value: "FeedRepository.refresh"},
		{Kind: DictOwner, ID: 11, Value: "CheckoutPresenter.render"},
		{Kind: DictOwner, ID: 12, Value: "CheckoutButton.onClick"},
		{Kind: DictOwner, ID: 13, Value: "CheckoutRepository.load"},
		{Kind: DictRoute, ID: 20, Value: "GET /feed"},
		{Kind: DictRoute, ID: 21, Value: "POST /checkout"},
		{Kind: DictScreen, ID: 30, Value: "FeedScreen"},
		{Kind: DictScreen, ID: 31, Value: "CheckoutScreen"},
		{Kind: DictClass, ID: 40, Value: "com.app.checkout.CheckoutActivity"},
		{Kind: DictOwner, ID: 41, Value: "CheckoutPresenter.render"},
		{Kind: DictClass, ID: 42, Value: "com.app.checkout.CheckoutBinding"},
		{Kind: DictClass, ID: 43, Value: "com.app.payments.PaymentListener"},
		{Kind: DictClass, ID: 44, Value: "com.app.checkout.CheckoutCacheEntry"},
		{Kind: DictOwner, ID: 45, Value: "CheckoutBindingCache.activeBinding"},
		{Kind: DictOwner, ID: 46, Value: "PaymentCallbackRegistry.listeners"},
		{Kind: DictOwner, ID: 47, Value: "CheckoutCache.entries"},
		{Kind: DictStack, ID: 50, Value: "CheckoutPresenter.renderItems"},
		{Kind: DictMetric, ID: 60, Value: "logs.warn.count"},
		{Kind: DictMetric, ID: 61, Value: "ui.fps_x100"},
		{Kind: DictMetric, ID: 62, Value: "ui_jank"},
		{Kind: DictLogSource, ID: 64, Value: "android.util.Log.w"},
		{Kind: DictFlow, ID: 65, Value: "checkout.open"},
		{Kind: DictStep, ID: 66, Value: "render_list"},
		{Kind: DictStep, ID: 67, Value: "network"},
		{Kind: DictStep, ID: 68, Value: "listener_callback"},
		{Kind: DictStep, ID: 69, Value: "cache_entries"},
		{Kind: DictGeneric, ID: 70, Value: "15"},
		{Kind: DictGeneric, ID: 71, Value: "2025-05-05"},
		{Kind: DictGeneric, ID: 72, Value: "arm64-v8a"},
		{Kind: DictGeneric, ID: 73, Value: "arm64-v8a,armeabi-v7a,armeabi"},
		{Kind: DictGeneric, ID: 74, Value: "Google"},
		{Kind: DictGeneric, ID: 75, Value: "google"},
		{Kind: DictGeneric, ID: 76, Value: "shiba"},
		{Kind: DictGeneric, ID: 77, Value: "shiba"},
		{Kind: DictGeneric, ID: 78, Value: "shiba"},
		{Kind: DictProcess, ID: 79, Value: "main"},
		{Kind: DictStableSymbol, ID: 0x1001, Value: "com.app.feed.FeedRepository.refresh"},
		{Kind: DictStableSymbol, ID: 0x1002, Value: "com.app.checkout.CheckoutPresenter.render"},
		{Kind: DictStableSymbol, ID: 0x1003, Value: "com.app.checkout.CheckoutRepository.load"},
		{Kind: DictStableSymbol, ID: 0x2001, Value: "com.app.checkout.CheckoutButton.onClick"},
		{Kind: DictStableSymbol, ID: 0x2002, Value: "com.app.checkout.CheckoutRepository.load"},
	}
	for i := range entries {
		entry := entries[i]
		if err := writer.WriteEvent(Event{Type: EventDictionary, Dictionary: &entry}); err != nil {
			return err
		}
	}

	context := func(screen, owner, flow, step uint64) AttributionContext {
		return AttributionContext{
			Present: true,
			Screen:  LocalSymbol(screen),
			Owner:   LocalSymbol(owner),
			Flow:    LocalSymbol(flow),
			Step:    LocalSymbol(step),
		}
	}
	events := []Event{
		{Type: EventSession, TimeMS: 1, Flags: uint64(FlagAppForeground), Session: &SessionEvent{
			AppVersionRef: LocalSymbol(1), BuildRef: LocalSymbol(2), DeviceRef: LocalSymbol(3), SDKInt: 35,
			AndroidReleaseRef: LocalSymbol(70), SecurityPatchRef: LocalSymbol(71), PrimaryABIRef: LocalSymbol(72),
			SupportedABIsRef: LocalSymbol(73), ManufacturerRef: LocalSymbol(74), BrandRef: LocalSymbol(75),
			HardwareRef: LocalSymbol(76), BoardRef: LocalSymbol(77), ProductRef: LocalSymbol(78),
			CollectorFlags: uint64(CollectorFPS | CollectorJankStats | CollectorProcessExit | CollectorIOTracing | CollectorSystemSampler | CollectorMainThreadStalls | CollectorRetainedObjects),
		}},
		{Type: EventProcessExit, TimeMS: 100, ProcessExit: &ProcessExitEvent{Reason: 6, TimestampUnixMS: 1_754_000_000_000, Importance: 100, PSSKB: 196_000, RSSKB: 244_000, ProcessRef: LocalSymbol(79)}},
		{Type: EventContext, TimeMS: 500, Flags: uint64(FlagAppForeground), Context: &ContextEvent{Network: NetworkWiFi, BatteryPct: 82, AvailMemoryKB: 2_018_304, TotalMemoryKB: 8_032_000, BatteryState: 2, BatteryTempDeciC: 320, NetworkValidated: true, RxBytes: 1_204_000, TxBytes: 93_000, FreeStorageKB: 48_000_000, TotalStorageKB: 118_000_000}},
		{Type: EventHTTP, TimeMS: 1_200, Attribution: context(0, 10, 0, 0), Flags: uint64(FlagHTTPReusedConnection | FlagHTTPTLS | FlagAppForeground), HTTP: &HTTPEvent{RouteRef: LocalSymbol(20), DurationMS: 184, DNSMS: 7, TTFBMS: 91, Status: Status2xx, RxBytes: 42_120, TxBytes: 740}},
		{Type: EventHTTP, TimeMS: 2_400, Attribution: context(0, 10, 0, 0), Flags: uint64(FlagHTTPTLS | FlagAppForeground), HTTP: &HTTPEvent{RouteRef: LocalSymbol(20), DurationMS: 612, DNSMS: 10, ConnectMS: 90, TTFBMS: 430, Status: Status2xx, RxBytes: 38_900, TxBytes: 730}},
		{Type: EventUIWindow, TimeMS: 10_000, Attribution: context(30, 0, 0, 0), Flags: uint64(FlagThreadMain | FlagAppForeground), UIWindow: sampleUIWindow(10_000, 580, 28)},
		{Type: EventGauge, TimeMS: 10_100, Metric: &MetricEvent{MetricRef: LocalSymbol(61), Value: 5_800}},
		{Type: EventRuntimeCall, TimeMS: 12_040, Attribution: AttributionContext{Present: true, Screen: LocalSymbol(31), Owner: StableSymbol(0x2001), Flow: LocalSymbol(65), Step: LocalSymbol(67)}, RuntimeCall: &RuntimeCallEvent{CalleeRef: StableSymbol(0x2002), Count: 8, TotalMS: 640, MaxMS: 240}},
		{Type: EventLogSpam, TimeMS: 12_100, Attribution: context(31, 12, 65, 67), LogSpam: &LogSpamEvent{SourceRef: LocalSymbol(64), Level: 5, Count: 12}},
		{Type: EventStall, TimeMS: 13_200, Attribution: context(31, 11, 65, 66), Flags: uint64(FlagThreadMain | FlagAppForeground), Stall: &StallEvent{StackRef: LocalSymbol(50), DurationMS: 1_240}},
		{Type: EventIO, TimeMS: 13_250, Attribution: context(31, 13, 65, 67), Flags: uint64(FlagThreadMain | FlagAppForeground), IO: &IOEvent{Operation: IOOperationDatabaseRead, DurationUS: 275_000, Bytes: 32_768}},
		{Type: EventMemory, TimeMS: 15_000, Flags: uint64(FlagAppForeground), Memory: &MemoryEvent{PSSKB: 188_240, JavaHeapKB: 90_412, NativeHeapKB: 38_112}},
		{Type: EventRetained, TimeMS: 21_000, Attribution: context(31, 41, 65, 66), Retained: &RetainedEvent{ClassRef: LocalSymbol(40), HolderRef: LocalSymbol(41), AgeMS: 15_000, Count: 2}},
		{Type: EventRetained, TimeMS: 21_400, Attribution: context(31, 45, 65, 66), Retained: &RetainedEvent{ClassRef: LocalSymbol(42), HolderRef: LocalSymbol(45), AgeMS: 45_000, Count: 1}},
		{Type: EventRetained, TimeMS: 21_800, Attribution: context(31, 46, 65, 68), Retained: &RetainedEvent{ClassRef: LocalSymbol(43), HolderRef: LocalSymbol(46), AgeMS: 30_000, Count: 1}},
		{Type: EventRetained, TimeMS: 21_900, Attribution: context(31, 47, 65, 69), Retained: &RetainedEvent{ClassRef: LocalSymbol(44), HolderRef: LocalSymbol(47), AgeMS: 20_000, Count: 3}},
		{Type: EventCounter, TimeMS: 22_000, Metric: &MetricEvent{MetricRef: LocalSymbol(60), Value: 17}},
		{Type: EventCounter, TimeMS: 22_100, Metric: &MetricEvent{MetricRef: StableSymbolInNamespace(0x1001, header.SymbolNamespace), Value: 1}},
		{Type: EventCounter, TimeMS: 22_200, Metric: &MetricEvent{MetricRef: StableSymbolInNamespace(0x1002, header.SymbolNamespace), Value: 1}},
		{Type: EventCounter, TimeMS: 22_300, Metric: &MetricEvent{MetricRef: StableSymbolInNamespace(0x1003, header.SymbolNamespace), Value: 1}},
		{Type: EventHTTP, TimeMS: 23_000, Attribution: context(0, 11, 0, 0), Flags: uint64(FlagHTTPFailed | FlagHTTPTLS | FlagAppForeground), HTTP: &HTTPEvent{RouteRef: LocalSymbol(21), DurationMS: 1_320, DNSMS: 9, TTFBMS: 1_140, Status: Status5xx, RxBytes: 1_024, TxBytes: 1_240}},
		{Type: EventUIWindow, TimeMS: 30_000, Attribution: context(31, 0, 0, 0), Flags: uint64(FlagThreadMain | FlagAppForeground), UIWindow: sampleUIWindow(10_000, 542, 62)},
		{Type: EventProblem, TimeMS: 30_001, Attribution: context(31, 11, 65, 66), Problem: &ProblemEvent{KindRef: LocalSymbol(62), WindowMS: 10_000, Count: 62, MaxMS: 48}},
		{Type: EventGauge, TimeMS: 30_100, Metric: &MetricEvent{MetricRef: LocalSymbol(61), Value: 5_420}},
	}
	for _, event := range events {
		if err := writer.WriteEvent(event); err != nil {
			return err
		}
	}
	return writer.Close()
}

func sampleUIWindow(windowMS, frames, jank uint64) *UIWindowEvent {
	buckets := make([]uint64, UIFrameHistogramBucketCount)
	buckets[1] = frames - jank
	buckets[8] = jank
	return &UIWindowEvent{
		WindowMS: windowMS, FrameCount: frames, JankCount: jank,
		Source: UIFrameSourceJankStats, FrameDeadlineUS: 16_667, FrameDurationBuckets: buckets,
	}
}
