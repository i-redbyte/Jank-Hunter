package mathanalysis

import (
	"fmt"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func BenchmarkMathCollectionPipeline(b *testing.B) {
	for _, input := range []struct {
		name                     string
		routes, buckets, repeats int
	}{
		{"sparse_long", 100, 50_000, 1}, {"dense_short", 8, 128, 8},
	} {
		b.Run(input.name, func(b *testing.B) {
			path := writeCollectionBenchmarkFixture(b, input.routes, input.buckets, input.repeats)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				collected, err := analyzeMathInputs([]string{path}, analyze.Options{})
				if err != nil {
					b.Fatal(err)
				}
				if len(collected.Timeline) != input.buckets || len(collected.RouteDefinitions) != 3 {
					b.Fatal("incomplete collection")
				}
				runtime.KeepAlive(collected)
			}
		})
	}
}

func writeCollectionBenchmarkFixture(t testing.TB, routes, buckets, repeats int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "collection.jhlog")
	file, writer, err := jhlog.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	entry := jhlog.DictionaryEntry{Kind: jhlog.DictOwner, ID: 1, Value: "Owner"}
	if err := writer.WriteEvent(jhlog.Event{Type: jhlog.EventDictionary, Dictionary: &entry}); err != nil {
		t.Fatal(err)
	}
	for route := 0; route < routes; route++ {
		entry := jhlog.DictionaryEntry{Kind: jhlog.DictRoute, ID: uint64(route + 2), Value: fmt.Sprintf("GET /route/%03d", route)}
		if err := writer.WriteEvent(jhlog.Event{Type: jhlog.EventDictionary, Dictionary: &entry}); err != nil {
			t.Fatal(err)
		}
	}
	for route := 0; route < routes; route++ {
		for repeat := 0; repeat < repeats; repeat++ {
			start, step := 0, 1
			if repeats == 1 {
				start = route * (buckets - 1) / (routes - 1)
				step = buckets
			}
			for bucket := start; bucket < buckets; bucket += step {
				event := jhlog.Event{Type: jhlog.EventHTTP, TimeMS: uint64(bucket) * DefaultBucketMS,
					Attribution: jhlog.AttributionContext{Present: true, Owner: jhlog.LocalSymbol(1)},
					HTTP:        &jhlog.HTTPEvent{RouteRef: jhlog.LocalSymbol(uint64(route + 2)), DurationMS: 100, DNSMS: 10, ConnectMS: 20, Status: jhlog.Status5xx}}
				if err := writer.WriteEvent(event); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}
