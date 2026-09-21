package mathanalysis

import (
	"fmt"
	"runtime"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func collectSparseHTTPFixture(bucketCount int) (*routeSeriesCollector, *networkLoopCollector) {
	scale := timelineScale{bucketMS: DefaultBucketMS, bucketCount: bucketCount, hasData: true}
	routes := newRouteSeriesCollector(analyze.Options{}, scale)
	network := newNetworkLoopCollector(analyze.Options{}, scale)
	symbols := newMathSymbolResolver(analyze.Options{})
	dict := map[uint64]string{1: "Owner"}
	for i := 0; i < 100; i++ {
		id := uint64(i + 2)
		dict[id] = fmt.Sprintf("GET /route/%03d", i)
		event := jhlog.Event{Type: jhlog.EventHTTP, TimeMS: uint64(i) * DefaultBucketMS,
			Attribution: jhlog.AttributionContext{Present: true, Owner: jhlog.LocalSymbol(1)},
			HTTP: &jhlog.HTTPEvent{RouteRef: jhlog.LocalSymbol(id), DurationMS: 100, DNSMS: 10,
				ConnectMS: 20, Status: jhlog.Status5xx}}
		routes.add(event, dict, symbols)
		network.add(event, dict, symbols)
	}
	return routes, network
}

func TestSparseCollectionDoesNotAllocateEveryRouteTimesFullTimeline(t *testing.T) {
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	routes, network := collectSparseHTTPFixture(maxTimelineBuckets)
	runtime.ReadMemStats(&after)
	runtime.KeepAlive(routes)
	runtime.KeepAlive(network)
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 8*1024*1024 {
		t.Fatalf("100 sparse HTTP events on50000buckets allocated%dbytes before candidate selection, limit8MiB", allocated)
	}
	definitions := routes.definitions(3)
	if len(definitions) != 3 {
		t.Fatalf("selected%droute definitions", len(definitions))
	}
	for i, definition := range definitions {
		if len(definition.points) != maxTimelineBuckets || definition.points[i] != 1 {
			t.Fatalf("selected route%d lost its exact bucket", i)
		}
	}
}

func TestDenseSignalStorageStaysNearItsNumericPayload(t *testing.T) {
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	collector := newNetworkLoopCollector(analyze.Options{}, timelineScale{bucketMS: DefaultBucketMS, bucketCount: maxTimelineBuckets, hasData: true})
	for index := 0; index < maxTimelineBuckets; index++ {
		collector.addPoint("key", "signal", "route", index, 1)
	}
	runtime.ReadMemStats(&after)
	runtime.KeepAlive(collector)
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > uint64(maxTimelineBuckets)*8*2 {
		t.Fatalf("dense numeric storage allocated%dbytes for%dbytes of values", allocated, maxTimelineBuckets*8)
	}
}

func BenchmarkSparseRouteAndNetworkCollection(b *testing.B) {
	for _, buckets := range []int{500, maxTimelineBuckets} {
		b.Run(fmt.Sprintf("buckets_%d", buckets), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				routes, network := collectSparseHTTPFixture(buckets)
				runtime.KeepAlive(routes)
				runtime.KeepAlive(network)
			}
		})
	}
}
