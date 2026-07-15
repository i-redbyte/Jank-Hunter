package benchfixture

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

const (
	ownerIDBase         uint64 = 1_000
	metadataSchema             = 2
	finalControlRecords        = 2
)

type Profile struct {
	Name                   string
	OwnerDictionaryEntries int
	RuntimeCallEvents      int
	RuntimeUniqueEdges     int
	FlowEvents             int
	FlowTuples             int
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
	RuntimeUniqueEdges int    `json:"runtime_unique_edges"`
	FlowEvents         int    `json:"flow_events"`
	FlowTuples         int    `json:"flow_tuples"`
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
		FlowEvents:             2_000,
		FlowTuples:             64,
		SignalEvents:           400,
	},
	"representative": {
		Name:                   "representative",
		OwnerDictionaryEntries: 7_800,
		RuntimeCallEvents:      20_566,
		RuntimeUniqueEdges:     12_925,
		FlowEvents:             28_389,
		FlowTuples:             346,
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
	header.PID = 4242
	header.ProcessName = "main"
	header.SymbolNamespace = []byte("benchmark-v9")
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
			AppVersionID:     1,
			BuildID:          2,
			DeviceID:         3,
			SDKInt:           35,
			ProcessID:        4,
			AndroidReleaseID: 90,
			SecurityPatchID:  91,
			PrimaryABIID:     92,
			SupportedABIsID:  93,
			ManufacturerID:   94,
			BrandID:          95,
			HardwareID:       96,
			BoardID:          97,
			ProductID:        98,
		},
	}); err != nil {
		return Metadata{}, err
	}

	runtimeRemaining := profile.RuntimeCallEvents
	flowRemaining := profile.FlowEvents
	signalRemaining := profile.SignalEvents
	runtimeIndex := 0
	flowIndex := 0
	signalIndex := 0
	sequence := 0
	timeMS := uint64(1)

	for runtimeRemaining+flowRemaining+signalRemaining > 0 {
		timeMS += 2
		slot := sequence % 51
		sequence++
		var event jhlog.Event
		switch {
		case slot < 28 && flowRemaining > 0:
			event = flowEvent(flowIndex, profile, timeMS)
			flowIndex++
			flowRemaining--
		case slot < 49 && runtimeRemaining > 0:
			event = runtimeCallEvent(runtimeIndex, profile, timeMS)
			runtimeIndex++
			runtimeRemaining--
		case signalRemaining > 0:
			event = signalEvent(signalIndex, profile, timeMS)
			signalIndex++
			signalRemaining--
		case flowRemaining > 0:
			event = flowEvent(flowIndex, profile, timeMS)
			flowIndex++
			flowRemaining--
		default:
			event = runtimeCallEvent(runtimeIndex, profile, timeMS)
			runtimeIndex++
			runtimeRemaining--
		}
		if err := writer.WriteEvent(event); err != nil {
			return Metadata{}, err
		}
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
	semanticEvents := 1 + profile.RuntimeCallEvents + profile.FlowEvents + profile.SignalEvents
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
		RuntimeUniqueEdges: profile.RuntimeUniqueEdges,
		FlowEvents:         profile.FlowEvents,
		FlowTuples:         profile.FlowTuples,
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
	if profile.FlowEvents < 0 || profile.FlowTuples < 1 || profile.FlowTuples > profile.FlowEvents {
		return fmt.Errorf("invalid flow cardinality")
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
		{Kind: jhlog.DictFlow, ID: 70, Value: "benchmark.feed.refresh"},
		{Kind: jhlog.DictFlow, ID: 71, Value: "benchmark.checkout.open"},
		{Kind: jhlog.DictStep, ID: 72, Value: "network"},
		{Kind: jhlog.DictStep, ID: 73, Value: "render"},
		{Kind: jhlog.DictStep, ID: 74, Value: "callback"},
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

func flowEvent(index int, profile Profile, timeMS uint64) jhlog.Event {
	tuple := index % profile.FlowTuples
	screenID := 30 + uint64(tuple%2)
	ownerID := ownerIDBase + uint64(tuple%profile.OwnerDictionaryEntries)
	flowID := 70 + uint64(tuple%2)
	stepID := 72 + uint64(tuple%3)
	return jhlog.Event{
		Type:        jhlog.EventFlow,
		TimeMS:      timeMS,
		Attribution: attribution(screenID, ownerID, flowID, stepID),
		Flow: &jhlog.FlowEvent{
			ScreenID: screenID,
			OwnerID:  ownerID,
			FlowID:   flowID,
			StepID:   stepID,
		},
	}
}

func runtimeCallEvent(index int, profile Profile, timeMS uint64) jhlog.Event {
	edge := index % profile.RuntimeUniqueEdges
	caller := edge % profile.OwnerDictionaryEntries
	callee := (edge*31 + edge/profile.OwnerDictionaryEntries + 17) % profile.OwnerDictionaryEntries
	screenID := 30 + uint64(edge%2)
	callerID := ownerIDBase + uint64(caller)
	flowID := 70 + uint64(edge%2)
	stepID := 72 + uint64(edge%3)
	return jhlog.Event{
		Type:        jhlog.EventRuntimeCall,
		TimeMS:      timeMS,
		Flags:       uint64(jhlog.FlagAppForeground),
		Attribution: attribution(screenID, callerID, flowID, stepID),
		RuntimeCall: &jhlog.RuntimeCallEvent{
			ScreenID: screenID,
			CallerID: callerID,
			FlowID:   flowID,
			StepID:   stepID,
			CalleeID: ownerIDBase + uint64(callee),
			Count:    uint64(1 + index%500),
			TotalMS:  uint64(1 + index%4_000),
			MaxMS:    uint64(1 + index%250),
		},
	}
}

func signalEvent(index int, profile Profile, timeMS uint64) jhlog.Event {
	ownerID := ownerIDBase + uint64((index*13)%profile.OwnerDictionaryEntries)
	context := struct {
		screen uint64
		flow   uint64
		step   uint64
	}{
		screen: 30 + uint64(index%2),
		flow:   70 + uint64(index%2),
		step:   72 + uint64(index%3),
	}
	attr := attribution(context.screen, ownerID, context.flow, context.step)
	switch index % 9 {
	case 0:
		return jhlog.Event{Type: jhlog.EventHTTP, TimeMS: timeMS, Attribution: attr, Flags: uint64(jhlog.FlagHTTPTLS | jhlog.FlagAppForeground), HTTP: &jhlog.HTTPEvent{OwnerID: ownerID, RouteID: 20 + uint64(index%2), DurationMS: uint64(40 + index%1_500), DNSMS: 5, ConnectMS: 12, TTFBMS: uint64(20 + index%500), Status: jhlog.Status2xx, RxBytes: uint64(4_096 + index), TxBytes: 512}}
	case 1:
		return jhlog.Event{Type: jhlog.EventUIWindow, TimeMS: timeMS, Attribution: attr, Flags: uint64(jhlog.FlagThreadMain | jhlog.FlagAppForeground), UIWindow: &jhlog.UIWindowEvent{ScreenID: context.screen, WindowMS: 10_000, FrameCount: 580, JankCount: uint64(5 + index%60), P50MS: 12, P95MS: uint64(20 + index%45), P99MS: uint64(40 + index%90)}}
	case 2:
		return jhlog.Event{Type: jhlog.EventStall, TimeMS: timeMS, Attribution: attr, Flags: uint64(jhlog.FlagThreadMain | jhlog.FlagAppForeground), Stall: &jhlog.StallEvent{OwnerID: ownerID, StackID: 50, DurationMS: uint64(100 + index%1_200)}}
	case 3:
		return jhlog.Event{Type: jhlog.EventMemory, TimeMS: timeMS, Attribution: attr, Flags: uint64(jhlog.FlagAppForeground), Memory: &jhlog.MemoryEvent{PSSKB: uint64(150_000 + index%80_000), JavaHeapKB: uint64(70_000 + index%30_000), NativeHeapKB: uint64(25_000 + index%20_000)}}
	case 4:
		return jhlog.Event{Type: jhlog.EventRetained, TimeMS: timeMS, Attribution: attr, Retained: &jhlog.RetainedEvent{ScreenID: context.screen, OwnerID: ownerID, FlowID: context.flow, StepID: context.step, ClassID: 40 + uint64(index%2), HolderID: ownerID, AgeMS: uint64(15_000 + index%60_000), Count: uint64(1 + index%4)}}
	case 5:
		return jhlog.Event{Type: jhlog.EventCounter, TimeMS: timeMS, Attribution: attr, Metric: &jhlog.MetricEvent{MetricID: 60, Value: uint64(1 + index%20)}}
	case 6:
		return jhlog.Event{Type: jhlog.EventGauge, TimeMS: timeMS, Attribution: attr, Metric: &jhlog.MetricEvent{MetricID: 61, Value: uint64(5_000 + index%1_000)}}
	case 7:
		return jhlog.Event{Type: jhlog.EventLogSpam, TimeMS: timeMS, Attribution: attr, LogSpam: &jhlog.LogSpamEvent{ScreenID: context.screen, OwnerID: ownerID, FlowID: context.flow, StepID: context.step, SourceID: 80, Level: 3, Count: uint64(1 + index%40)}}
	default:
		return jhlog.Event{Type: jhlog.EventProblem, TimeMS: timeMS, Attribution: attr, Flags: uint64(jhlog.FlagAppForeground), Problem: &jhlog.ProblemEvent{ScreenID: context.screen, OwnerID: ownerID, FlowID: context.flow, StepID: context.step, KindID: 62 + uint64(index%2), WindowMS: 10_000, Count: uint64(1 + index%30), MaxMS: uint64(20 + index%200)}}
	}
}

func attribution(screenID, ownerID, flowID, stepID uint64) jhlog.AttributionContext {
	return jhlog.AttributionContext{
		Present: true,
		Screen:  jhlog.LocalSymbol(screenID),
		Owner:   jhlog.LocalSymbol(ownerID),
		Flow:    jhlog.LocalSymbol(flowID),
		Step:    jhlog.LocalSymbol(stepID),
	}
}
