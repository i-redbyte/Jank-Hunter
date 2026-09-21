package benchfixture

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

const (
	ownerIDBase         uint64 = 1_000
	metadataSchema      int    = 4
	finalControlRecords int    = 2
)

type Profile struct {
	Name                   string
	OwnerDictionaryEntries int
	RuntimeCallEvents      int
	RuntimeUniqueEdges     int
	AttributedEvents       int
	AttributionTuples      int
	SignalEvents           int
}

type Metadata struct {
	Schema             int    `json:"schema"`
	Profile            string `json:"profile"`
	Events             int    `json:"events"` // Known semantic data events, excluding dictionary and control records.
	DataRecords        int    `json:"data_records"`
	DictionaryEntries  int    `json:"dictionary_entries"`
	DictionaryRecords  int    `json:"dictionary_records"`
	ControlRecords     int    `json:"control_records"`
	TotalRecords       int    `json:"total_records"`
	RuntimeCallEvents  int    `json:"runtime_call_events"`
	RuntimeCallBlocks  int    `json:"runtime_call_blocks"`
	RuntimeUniqueEdges int    `json:"runtime_unique_edges"`
	AttributedEvents   int    `json:"attributed_events"`
	AttributionTuples  int    `json:"attribution_tuples"`
	SignalEvents       int    `json:"signal_events"`
	DurationMS         uint64 `json:"duration_ms"`
	CompressedBytes    int64  `json:"compressed_bytes"`
}

var profiles = map[string]Profile{
	"smoke": {
		Name:                   "smoke",
		OwnerDictionaryEntries: 256,
		RuntimeCallEvents:      1_500,
		RuntimeUniqueEdges:     700,
		AttributedEvents:       2_000,
		AttributionTuples:      64,
		SignalEvents:           400,
	},
	"representative": {
		Name:                   "representative",
		OwnerDictionaryEntries: 7_800,
		RuntimeCallEvents:      20_566,
		RuntimeUniqueEdges:     12_925,
		AttributedEvents:       28_389,
		AttributionTuples:      346,
		SignalEvents:           2_186,
	},
}

func ProfileByName(name string) (Profile, error) {
	profile, ok := profiles[name]
	if !ok {
		return Profile{}, fmt.Errorf("unknown benchmark fixture profile %q", name)
	}
	return profile, nil
}

func Write(path string, profile Profile) (Metadata, error) {
	if err := validateProfile(profile); err != nil {
		return Metadata{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return Metadata{}, err
	}

	header := jhlog.DefaultSegmentHeader()
	header.RunID = jhlog.ID128{0x10, 0x8a, 0x3c, 0x51, 0x92, 0x47, 0x4e, 0x11, 0xa4, 0xe3, 0x77, 0x20, 0x18, 0x04, 0x90, 0x01}
	header.ProcessInstanceID = jhlog.ID128{0x20, 0x8a, 0x3c, 0x51, 0x92, 0x47, 0x4e, 0x11, 0xa4, 0xe3, 0x77, 0x20, 0x18, 0x04, 0x90, 0x02}
	header.SessionID = jhlog.ID128{0x30, 0x8a, 0x3c, 0x51, 0x92, 0x47, 0x4e, 0x11, 0xa4, 0xe3, 0x77, 0x20, 0x18, 0x04, 0x90, 0x03}
	header.OSPID = 4242
	header.ProcessName = "main"
	header.SymbolNamespace = []byte("benchmark-jh100")
	file, writer, err := jhlog.CreateWithHeader(path, header)
	if err != nil {
		return Metadata{}, err
	}
	closed := false
	defer func() {
		if !closed {
			_ = file.Close()
		}
	}()

	core := coreDictionaryEntries()
	for index := range core {
		entry := core[index]
		if err := writer.WriteEvent(jhlog.Event{Type: jhlog.EventDictionary, Dictionary: &entry}); err != nil {
			return Metadata{}, err
		}
	}
	for index := 0; index < profile.OwnerDictionaryEntries; index++ {
		entry := jhlog.DictionaryEntry{
			Kind:  jhlog.DictOwner,
			ID:    ownerIDBase + uint64(index),
			Value: ownerName(index),
		}
		if err := writer.WriteEvent(jhlog.Event{Type: jhlog.EventDictionary, Dictionary: &entry}); err != nil {
			return Metadata{}, err
		}
	}

	if err := writer.WriteEvent(jhlog.Event{
		Type:   jhlog.EventSession,
		TimeMS: 1,
		Flags:  uint64(jhlog.FlagAppForeground),
		Session: &jhlog.SessionEvent{
			AppVersionRef:     jhlog.LocalSymbol(1),
			BuildRef:          jhlog.LocalSymbol(2),
			DeviceRef:         jhlog.LocalSymbol(3),
			SDKInt:            35,
			AndroidReleaseRef: jhlog.LocalSymbol(90),
			SecurityPatchRef:  jhlog.LocalSymbol(91),
			PrimaryABIRef:     jhlog.LocalSymbol(92),
			SupportedABIsRef:  jhlog.LocalSymbol(93),
			ManufacturerRef:   jhlog.LocalSymbol(94),
			BrandRef:          jhlog.LocalSymbol(95),
			HardwareRef:       jhlog.LocalSymbol(96),
			BoardRef:          jhlog.LocalSymbol(97),
			ProductRef:        jhlog.LocalSymbol(98),
		},
	}); err != nil {
		return Metadata{}, err
	}

	runtimeRemaining := profile.RuntimeCallEvents
	attributedRemaining := profile.AttributedEvents
	signalRemaining := profile.SignalEvents
	runtimeIndex := 0
	attributedIndex := 0
	signalIndex := 0
	sequence := 0
	timeMS := uint64(1)
	runtimeCallBlocks := 0
	runtimeBatch := make([]jhlog.Event, 0, jhlog.MaxRuntimeCallBlockRows)
	flushRuntimeBatch := func() error {
		if len(runtimeBatch) == 0 {
			return nil
		}
		if err := writer.WriteRuntimeCallBlock(runtimeBatch); err != nil {
			return err
		}
		runtimeCallBlocks++
		runtimeBatch = runtimeBatch[:0]
		return nil
	}

	for runtimeRemaining+attributedRemaining+signalRemaining > 0 {
		timeMS += 2
		slot := sequence % 51
		sequence++
		var event jhlog.Event
		switch {
		case slot < 28 && attributedRemaining > 0:
			event = attributedEvent(attributedIndex, profile, timeMS)
			attributedIndex++
			attributedRemaining--
		case slot < 49 && runtimeRemaining > 0:
			event = runtimeCallEvent(runtimeIndex, profile, timeMS)
			runtimeIndex++
			runtimeRemaining--
		case signalRemaining > 0:
			event = signalEvent(signalIndex, profile, timeMS)
			signalIndex++
			signalRemaining--
		case attributedRemaining > 0:
			event = attributedEvent(attributedIndex, profile, timeMS)
			attributedIndex++
			attributedRemaining--
		default:
			event = runtimeCallEvent(runtimeIndex, profile, timeMS)
			runtimeIndex++
			runtimeRemaining--
		}
		if event.Type == jhlog.EventRuntimeCall {
			if len(runtimeBatch) > 0 {
				event.TimeMS = runtimeBatch[0].TimeMS
			}
			runtimeBatch = append(runtimeBatch, event)
			if len(runtimeBatch) == jhlog.MaxRuntimeCallBlockRows {
				if err := flushRuntimeBatch(); err != nil {
					return Metadata{}, err
				}
			}
			continue
		}
		if err := flushRuntimeBatch(); err != nil {
			return Metadata{}, err
		}
		if err := writer.WriteEvent(event); err != nil {
			return Metadata{}, err
		}
	}
	if err := flushRuntimeBatch(); err != nil {
		return Metadata{}, err
	}

	if err := file.Close(); err != nil {
		return Metadata{}, err
	}
	closed = true
	stat, err := os.Stat(path)
	if err != nil {
		return Metadata{}, err
	}
	dictionaryEntries := len(core) + profile.OwnerDictionaryEntries
	semanticEvents := 1 + profile.RuntimeCallEvents + profile.AttributedEvents + profile.SignalEvents
	return Metadata{
		Schema:             metadataSchema,
		Profile:            profile.Name,
		Events:             semanticEvents,
		DataRecords:        semanticEvents,
		DictionaryEntries:  dictionaryEntries,
		DictionaryRecords:  dictionaryEntries,
		ControlRecords:     finalControlRecords,
		TotalRecords:       semanticEvents + dictionaryEntries + finalControlRecords,
		RuntimeCallEvents:  profile.RuntimeCallEvents,
		RuntimeCallBlocks:  runtimeCallBlocks,
		RuntimeUniqueEdges: profile.RuntimeUniqueEdges,
		AttributedEvents:   profile.AttributedEvents,
		AttributionTuples:  profile.AttributionTuples,
		SignalEvents:       profile.SignalEvents,
		DurationMS:         timeMS,
		CompressedBytes:    stat.Size(),
	}, nil
}

func validateProfile(profile Profile) error {
	if profile.Name == "" {
		return fmt.Errorf("benchmark fixture profile name is empty")
	}
	if profile.OwnerDictionaryEntries < 2 {
		return fmt.Errorf("benchmark fixture needs at least two owner entries")
	}
	if profile.RuntimeCallEvents < 0 || profile.RuntimeUniqueEdges < 1 || profile.RuntimeUniqueEdges > profile.RuntimeCallEvents {
		return fmt.Errorf("invalid runtime call cardinality")
	}
	if profile.AttributedEvents < 0 || profile.AttributionTuples < 1 || profile.AttributionTuples > profile.AttributedEvents {
		return fmt.Errorf("invalid attribution cardinality")
	}
	if profile.SignalEvents < 0 {
		return fmt.Errorf("invalid signal event count")
	}
	return nil
}

func coreDictionaryEntries() []jhlog.DictionaryEntry {
	return []jhlog.DictionaryEntry{
		{Kind: jhlog.DictAppVersion, ID: 1, Value: "9.0.0-benchmark"},
		{Kind: jhlog.DictBuild, ID: 2, Value: "900000"},
		{Kind: jhlog.DictDevice, ID: 3, Value: "Synthetic Device / API 35"},
		{Kind: jhlog.DictProcess, ID: 4, Value: "main"},
		{Kind: jhlog.DictRoute, ID: 20, Value: "GET /benchmark/feed"},
		{Kind: jhlog.DictRoute, ID: 21, Value: "POST /benchmark/checkout"},
		{Kind: jhlog.DictScreen, ID: 30, Value: "BenchmarkFeed"},
		{Kind: jhlog.DictScreen, ID: 31, Value: "BenchmarkCheckout"},
		{Kind: jhlog.DictClass, ID: 40, Value: "com.example.benchmark.RetainedActivity"},
		{Kind: jhlog.DictClass, ID: 41, Value: "com.example.benchmark.RetainedBinding"},
		{Kind: jhlog.DictStack, ID: 50, Value: "BenchmarkPresenter.renderItems"},
		{Kind: jhlog.DictMetric, ID: 60, Value: "benchmark.counter"},
		{Kind: jhlog.DictMetric, ID: 61, Value: "benchmark.gauge"},
		{Kind: jhlog.DictMetric, ID: 62, Value: "ui_jank"},
		{Kind: jhlog.DictMetric, ID: 63, Value: "main_thread_stall"},
		{Kind: jhlog.DictLogSource, ID: 80, Value: "android.util.Log.d"},
		{Kind: jhlog.DictGeneric, ID: 90, Value: "15"},
		{Kind: jhlog.DictGeneric, ID: 91, Value: "2026-01-01"},
		{Kind: jhlog.DictGeneric, ID: 92, Value: "arm64-v8a"},
		{Kind: jhlog.DictGeneric, ID: 93, Value: "arm64-v8a,armeabi-v7a"},
		{Kind: jhlog.DictGeneric, ID: 94, Value: "Synthetic"},
		{Kind: jhlog.DictGeneric, ID: 95, Value: "synthetic"},
		{Kind: jhlog.DictGeneric, ID: 96, Value: "virtual"},
		{Kind: jhlog.DictGeneric, ID: 97, Value: "virtual"},
		{Kind: jhlog.DictGeneric, ID: 98, Value: "virtual"},
	}
}

func ownerName(index int) string {
	return fmt.Sprintf(
		"com.example.feature%03d.pipeline.Repository%04d.loadPageWithCacheAndNetworkFallback$%016x",
		index%97,
		index,
		mix64(uint64(index)+0x9e3779b97f4a7c15),
	)
}

func mix64(value uint64) uint64 {
	value = (value ^ (value >> 30)) * 0xbf58476d1ce4e5b9
	value = (value ^ (value >> 27)) * 0x94d049bb133111eb
	return value ^ (value >> 31)
}

func attributedEvent(index int, profile Profile, timeMS uint64) jhlog.Event {
	tuple := index % profile.AttributionTuples
	screenID := 30 + uint64(tuple%2)
	ownerID := ownerIDBase + uint64(tuple%profile.OwnerDictionaryEntries)
	return jhlog.Event{
		Type:        jhlog.EventLogSpam,
		TimeMS:      timeMS,
		Attribution: attribution(screenID, ownerID, 0),
		LogSpam:     &jhlog.LogSpamEvent{SourceRef: jhlog.LocalSymbol(80), Level: 2, Count: 1},
	}
}

func runtimeCallEvent(index int, profile Profile, timeMS uint64) jhlog.Event {
	edge := index % profile.RuntimeUniqueEdges
	caller := edge % profile.OwnerDictionaryEntries
	callee := (edge*31 + edge/profile.OwnerDictionaryEntries + 17) % profile.OwnerDictionaryEntries
	screenID := 30 + uint64(edge%2)
	callerID := ownerIDBase + uint64(caller)
	return jhlog.Event{
		Type:        jhlog.EventRuntimeCall,
		TimeMS:      timeMS,
		Flags:       uint64(jhlog.FlagAppForeground),
		Attribution: attribution(screenID, callerID, 0),
		RuntimeCall: &jhlog.RuntimeCallEvent{
			CalleeRef: jhlog.LocalSymbol(ownerIDBase + uint64(callee)),
			Count:     uint64(1 + index%500),
			TotalMS:   uint64(1 + index%4_000),
			MaxMS:     uint64(1 + index%250),
		},
	}
}

func signalEvent(index int, profile Profile, timeMS uint64) jhlog.Event {
	ownerID := ownerIDBase + uint64((index*13)%profile.OwnerDictionaryEntries)
	context := struct {
		screen uint64
	}{
		screen: 30 + uint64(index%2),
	}
	attr := attribution(context.screen, ownerID, 0)
	switch index % 9 {
	case 0:
		return jhlog.Event{Type: jhlog.EventHTTP, TimeMS: timeMS, Attribution: attr, Flags: uint64(jhlog.FlagHTTPTLS | jhlog.FlagAppForeground), HTTP: &jhlog.HTTPEvent{RouteRef: jhlog.LocalSymbol(20 + uint64(index%2)), DurationMS: uint64(40 + index%1_500), DNSMS: 5, ConnectMS: 12, TTFBMS: uint64(20 + index%500), Status: jhlog.Status2xx, RxBytes: uint64(4_096 + index), TxBytes: 512}}
	case 1:
		return jhlog.Event{Type: jhlog.EventUIWindow, TimeMS: timeMS, Attribution: attr, Flags: uint64(jhlog.FlagThreadMain | jhlog.FlagAppForeground), UIWindow: benchmarkUIWindow(uint64(5 + index%60))}
	case 2:
		return jhlog.Event{Type: jhlog.EventStall, TimeMS: timeMS, Attribution: attr, Flags: uint64(jhlog.FlagThreadMain | jhlog.FlagAppForeground), Stall: &jhlog.StallEvent{StackRef: jhlog.LocalSymbol(50), DurationMS: uint64(100 + index%1_200)}}
	case 3:
		return jhlog.Event{Type: jhlog.EventMemory, TimeMS: timeMS, Attribution: attr, Flags: uint64(jhlog.FlagAppForeground), Memory: &jhlog.MemoryEvent{PSSKB: uint64(150_000 + index%80_000), JavaHeapKB: uint64(70_000 + index%30_000), NativeHeapKB: uint64(25_000 + index%20_000)}}
	case 4:
		return jhlog.Event{Type: jhlog.EventRetained, TimeMS: timeMS, Attribution: attr, Retained: &jhlog.RetainedEvent{ClassRef: jhlog.LocalSymbol(40 + uint64(index%2)), HolderRef: jhlog.LocalSymbol(ownerID), AgeMS: uint64(15_000 + index%60_000), Count: uint64(1 + index%4)}}
	case 5:
		return jhlog.Event{Type: jhlog.EventCounter, TimeMS: timeMS, Attribution: attr, Metric: &jhlog.MetricEvent{MetricRef: jhlog.LocalSymbol(60), Value: uint64(1 + index%20)}}
	case 6:
		return jhlog.Event{Type: jhlog.EventGauge, TimeMS: timeMS, Attribution: attr, Metric: &jhlog.MetricEvent{MetricRef: jhlog.LocalSymbol(61), Value: uint64(5_000 + index%1_000)}}
	case 7:
		return jhlog.Event{Type: jhlog.EventLogSpam, TimeMS: timeMS, Attribution: attr, LogSpam: &jhlog.LogSpamEvent{SourceRef: jhlog.LocalSymbol(80), Level: 3, Count: uint64(1 + index%40)}}
	default:
		return jhlog.Event{Type: jhlog.EventProblem, TimeMS: timeMS, Attribution: attr, Flags: uint64(jhlog.FlagAppForeground), Problem: &jhlog.ProblemEvent{KindRef: jhlog.LocalSymbol(62 + uint64(index%2)), WindowMS: 10_000, Count: uint64(1 + index%30), MaxMS: uint64(20 + index%200)}}
	}
}

func attribution(screenID, ownerID, operationID uint64) jhlog.AttributionContext {
	return jhlog.AttributionContext{
		Present:     true,
		Screen:      jhlog.LocalSymbol(screenID),
		Owner:       jhlog.LocalSymbol(ownerID),
		OperationID: operationID,
	}
}

func benchmarkUIWindow(jank uint64) *jhlog.UIWindowEvent {
	const frames = uint64(580)
	buckets := make([]uint64, jhlog.UIFrameHistogramBucketCount)
	buckets[1] = frames - jank
	buckets[8] = jank
	return &jhlog.UIWindowEvent{
		WindowMS: 10_000, FrameCount: frames, JankCount: jank,
		Source: jhlog.UIFrameSourceJankStats, FrameDeadlineUS: 16_667, FrameDurationBuckets: buckets,
	}
}
