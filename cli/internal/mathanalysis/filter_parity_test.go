package mathanalysis

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func writeFilterParityFixture(t testing.TB, mapped, stable bool) (string, *analyze.NameMapping) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "filters.jhlog")
	var err error
	header := jhlog.DefaultSegmentHeader()
	values := []string{"GET /feed", "example.FeedScreen", "example.FeedOwner", "example.FeedClass", "example.FeedHolder", "example.FeedInitiator", "GET /other", "example.OtherScreen", "example.OtherOwner", "example.OtherClass", "example.OtherHolder", "example.OtherInitiator", "websocket.feedowner.reconnect.count"}
	var mapping *analyze.NameMapping
	if mapped {
		text := ""
		for i := range values {
			if i%6 != 0 && i < 12 {
				alias := fmt.Sprintf("obf%d", i)
				text += values[i] + " -> " + alias + ":\n"
				values[i] = alias
			}
		}
		mapPath := filepath.Join(dir, "mapping.txt")
		if err := os.WriteFile(mapPath, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256([]byte(text))
		header.BuildIdentity = jhlog.BuildIdentity{State: jhlog.BuildIdentityMapped, MappingSHA256: hex.EncodeToString(digest[:])}
		mapping, err = analyze.LoadNameMapping(mapPath)
		if err != nil {
			t.Fatal(err)
		}
	}
	file, writer, err := jhlog.CreateWithHeader(path, header)
	if err != nil {
		t.Fatal(err)
	}
	write := func(event jhlog.Event) {
		t.Helper()
		if err := writer.WriteEvent(event); err != nil {
			t.Fatal(err)
		}
	}
	originFor := func(id uint64) jhlog.SymbolOrigin {
		if id <= 12 && (id-1)%6 != 0 {
			return jhlog.SymbolOriginRuntimeClass
		}
		return jhlog.SymbolOriginSourceLabel
	}
	for i, value := range values {
		kind := jhlog.DictGeneric
		if stable {
			kind = jhlog.DictStableSymbol
		}
		write(jhlog.Event{Type: jhlog.EventDictionary, Dictionary: &jhlog.DictionaryEntry{Kind: kind, ID: uint64(i + 1), Value: value, Origin: originFor(uint64(i + 1))}})
	}
	ref := func(id uint64) jhlog.SymbolRef {
		if stable {
			return jhlog.SymbolRef{Stable: true, ID: id, Origin: originFor(id)}
		}
		return jhlog.SymbolRef{ID: id, Origin: originFor(id)}
	}
	for variant := uint64(0); variant < 2; variant++ {
		offset := variant * 6
		attr := jhlog.AttributionContext{Present: true, Screen: ref(offset + 2), Owner: ref(offset + 3)}
		for repeat := 0; repeat < 3; repeat++ {
			time := 1000 + variant*10000 + uint64(repeat)*1000
			write(jhlog.Event{Type: jhlog.EventHTTP, TimeMS: time, Attribution: attr, HTTP: &jhlog.HTTPEvent{RouteRef: ref(offset + 1), InitiatorRef: ref(offset + 6), DurationMS: 100, Status: jhlog.Status2xx}})
		}
		time := uint64(5000) + variant*10000
		write(jhlog.Event{Type: jhlog.EventUIWindow, TimeMS: time, Attribution: attr, UIWindow: typedUIWindow(1000, 10, 1, 25)})
		write(jhlog.Event{Type: jhlog.EventStall, TimeMS: time + 1, Attribution: attr, Stall: &jhlog.StallEvent{DurationMS: 250}})
		write(jhlog.Event{Type: jhlog.EventMemory, TimeMS: time + 2, Attribution: attr, Memory: &jhlog.MemoryEvent{PSSKB: 100 + variant}})
		write(jhlog.Event{Type: jhlog.EventRetained, TimeMS: time + 3, Attribution: attr, Retained: &jhlog.RetainedEvent{ClassRef: ref(offset + 4), HolderRef: ref(offset + 5), Count: 1, AgeMS: 1000}})
		write(jhlog.Event{Type: jhlog.EventContext, TimeMS: time + 4, Context: &jhlog.ContextEvent{AvailMemoryKB: 500, RxBytes: 100 + variant*100, TxBytes: 200 + variant*100}})
		write(jhlog.Event{Type: jhlog.EventGauge, TimeMS: time + 5, Metric: &jhlog.MetricEvent{MetricRef: ref(13), Value: 3}})
	}
	write(jhlog.Event{Type: jhlog.EventHTTP, TimeMS: 20000, HTTP: &jhlog.HTTPEvent{RouteRef: ref(1), DurationMS: 100, Status: jhlog.Status2xx}})
	if err := writer.CloseWithReason(jhlog.SegmentEndNormal); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return path, mapping
}

func TestAllMathCollectorsUseSummaryFilterPopulation(t *testing.T) {
	for _, mode := range []struct {
		name           string
		mapped, stable bool
	}{{"local", false, false}, {"mapped", true, false}, {"stable", false, true}, {"mapped_stable", true, true}} {
		path, mapping := writeFilterParityFixture(t, mode.mapped, mode.stable)
		filters := []analyze.Filter{{}, {ScreenContains: "FeedScreen"}, {ScreenContains: "absent"}, {OwnerContains: "FeedOwner"}, {OwnerContains: "absent"}, {OwnerContains: "FeedInitiator"}, {OwnerContains: "FeedHolder"}, {RouteContains: "/feed"}, {RouteContains: "absent"}, {ClassContains: "FeedClass"}, {ClassContains: "absent"}, {OwnerContains: "unknown"}, {ScreenContains: "unknown"}}
		for mask := 1; mask < 16; mask++ {
			f := analyze.Filter{}
			if mask&1 != 0 {
				f.RouteContains = "/feed"
			}
			if mask&2 != 0 {
				f.ScreenContains = "FeedScreen"
			}
			if mask&4 != 0 {
				f.OwnerContains = "FeedOwner"
			}
			if mask&8 != 0 {
				f.ClassContains = "FeedClass"
			}
			filters = append(filters, f)
		}
		for i, filter := range filters {
			t.Run(fmt.Sprintf("%s/%d", mode.name, i), func(t *testing.T) {
				options := analyze.Options{Filter: filter, ObfuscationMap: mapping}
				summary, err := analyze.InspectFilesWithOptions("filter", []string{path}, options)
				if err != nil {
					t.Fatal(err)
				}
				inputs, err := analyzeMathInputs([]string{path}, options)
				if err != nil {
					t.Fatal(err)
				}
				wantHTTP, wantContextual, wantRetained := expectedParityPopulation(filter)
				if summary.HTTPCount != wantHTTP || summary.StallCount != wantContextual || summary.UIFrames != uint64(10*wantContextual) || summary.MemoryCount != wantContextual || summary.Retained != uint64(wantRetained) {
					t.Errorf("summary differs from fixture oracle: HTTP/stalls/UI/memory/retained=%d/%d/%d/%d/%d want=%d/%d/%d/%d/%d", summary.HTTPCount, summary.StallCount, summary.UIFrames, summary.MemoryCount, summary.Retained, wantHTTP, wantContextual, 10*wantContextual, wantContextual, wantRetained)
				}
				if summary.ContextCount != 2 || len(summary.Gauges) != 1 {
					t.Error("global context/custom metrics were filtered")
				}

				http, stalls, frames := 0, 0, uint64(0)
				for _, bucket := range inputs.Timeline {
					http += bucket.HTTPCount
					stalls += bucket.StallCount
					frames += bucket.UIFrames
				}
				if http != summary.HTTPCount || stalls != summary.StallCount || frames != summary.UIFrames {
					t.Errorf("filter=%+v: summary HTTP/stall/UI=%d/%d/%d math=%d/%d/%d", filter, summary.HTTPCount, summary.StallCount, summary.UIFrames, http, stalls, frames)
				}
				robustHTTP, robustMemory, robustRetained := 0, 0, 0
				for key, set := range inputs.RobustSamples {
					switch {
					case key.Dimension == "Маршрут" && key.Metric == "HTTP задержка":
						robustHTTP += set.seen
					case key.Metric == "PSS":
						robustMemory += set.seen
					case key.Metric == "Возраст удержанного объекта":
						robustRetained += set.seen
					}
				}
				if robustHTTP != summary.HTTPCount || robustMemory != summary.MemoryCount || robustRetained != int(summary.Retained) {
					t.Errorf("robust population HTTP/memory/retained=%d/%d/%d summary=%d/%d/%d", robustHTTP, robustMemory, robustRetained, summary.HTTPCount, summary.MemoryCount, summary.Retained)
				}
				routeTotal := 0.0
				for _, def := range inputs.RouteDefinitions {
					for _, value := range def.points {
						routeTotal += value
					}
				}
				if routeTotal != float64(summary.HTTPCount) {
					t.Errorf("route series population=%v summary HTTP=%d", routeTotal, summary.HTTPCount)
				}
			})
		}
	}
}

func expectedParityPopulation(filter analyze.Filter) (http, contextual, retained int) {
	if filter == (analyze.Filter{}) {
		return 7, 2, 2
	}
	if filter.RouteContains == "absent" || filter.ScreenContains == "absent" || filter.OwnerContains == "absent" || filter.ClassContains == "absent" {
		return 0, 0, 0
	}
	if filter.OwnerContains == "unknown" || filter.ScreenContains == "unknown" {
		return 1, 0, 0
	}
	if filter.OwnerContains == "FeedInitiator" {
		return 3, 0, 0
	}
	if filter.OwnerContains == "FeedHolder" {
		return 0, 0, 1
	}
	if filter.ClassContains != "" {
		if filter.RouteContains != "" {
			return 0, 0, 0
		}
		return 0, 0, 1
	}
	if filter.RouteContains != "" {
		if filter.ScreenContains == "" && filter.OwnerContains == "" {
			return 4, 0, 0
		}
		return 3, 0, 0
	}
	return 3, 1, 1
}

func TestNetworkCollectorsPreserveGlobalMetricsAndFilteredHTTP(t *testing.T) {
	path, mapping := writeFilterParityFixture(t, false, true)
	for _, filter := range []analyze.Filter{{ScreenContains: "FeedScreen"}, {OwnerContains: "FeedInitiator"}, {ClassContains: "FeedClass"}, {RouteContains: "absent"}} {
		options := analyze.Options{Filter: filter, ObfuscationMap: mapping}
		collector := newNetworkLoopCollector(options, newTimelineScale(0, 30000, true))
		symbols := newMathSymbolResolver(options)
		err := jhlog.StreamFile(path, func(event jhlog.Event, dict map[uint64]string) error {
			symbols.observe(event)
			collector.add(event, dict, symbols)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		var http, global float64
		for key, signal := range collector.signals {
			if strings.HasPrefix(key, "route:") {
				http += signal.sparse.total
			}
			if strings.HasPrefix(key, "metric:") {
				global += signal.sparse.total
			}
		}
		want, _, _ := expectedParityPopulation(filter)
		if http != float64(want) || global != 6 {
			t.Errorf("filter=%+v HTTP/global metric=%v/%v want=%d/6", filter, http, global, want)
		}
	}
}

func BenchmarkMathFilterPopulation(b *testing.B) {
	path := writeCollectionBenchmarkFixture(b, 8, 128, 8)
	for _, tc := range []struct {
		name   string
		filter analyze.Filter
	}{{"owner_keep", analyze.Filter{OwnerContains: "owner"}}, {"owner_miss", analyze.Filter{OwnerContains: "missing"}}, {"route_keep", analyze.Filter{RouteContains: "/route/000"}}, {"screen_miss", analyze.Filter{ScreenContains: "FeedScreen"}}} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				result, err := analyzeMathInputs([]string{path}, analyze.Options{Filter: tc.filter})
				if err != nil {
					b.Fatal(err)
				}
				runtime.KeepAlive(result)
			}
		})
	}
}

func TestContextualFilterPredicateDoesNotAllocate(t *testing.T) {
	filter := analyze.Filter{RouteContains: "/feed", ScreenContains: "screen", OwnerContains: "initiator"}
	event := jhlog.Event{Type: jhlog.EventHTTP, Attribution: jhlog.AttributionContext{Present: true, Screen: jhlog.LocalSymbol(2), Owner: jhlog.LocalSymbol(3)}, HTTP: &jhlog.HTTPEvent{RouteRef: jhlog.LocalSymbol(1), InitiatorRef: jhlog.LocalSymbol(4)}}
	dict := map[uint64]string{1: "GET /feed", 2: "FeedScreen", 3: "FeedOwner", 4: "FeedInitiator"}
	symbols := newMathSymbolResolver(analyze.Options{})
	if !mathEventMatchesFilter(event, dict, filter, symbols) {
		t.Fatal("fixture must match alternate owner")
	}
	if allocations := testing.AllocsPerRun(1000, func() {
		if !mathEventMatchesFilter(event, dict, filter, symbols) {
			panic("lost match")
		}
	}); allocations != 0 {
		t.Fatalf("filter allocates %v times per event", allocations)
	}
}
