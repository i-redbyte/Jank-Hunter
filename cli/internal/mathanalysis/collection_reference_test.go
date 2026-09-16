package mathanalysis

import (
	"fmt"
	"math/rand"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func TestRouteCandidatesMatchDenseReferenceForSkewAndOutOfOrderBuckets(t *testing.T) {
	for seed := int64(1); seed <= 8; seed++ {
		for _, filter := range []string{"", "/route/1"} {
			scale := timelineScale{bucketMS: DefaultBucketMS, bucketCount: 37, hasData: true}
			collector := newRouteSeriesCollector(analyze.Options{Filter: analyze.Filter{RouteContains: filter}}, scale)
			symbols := newMathSymbolResolver(analyze.Options{})
			reference := map[string][]float64{}
			rng := rand.New(rand.NewSource(seed))
			for event := 0; event < 600; event++ {
				route := fmt.Sprintf("GET /route/%d", rng.Intn(12))
				if event%3 == 0 {
					route = "GET /route/0"
				}
				bucket := rng.Intn(41)
				collector.add(jhlog.Event{Type: jhlog.EventHTTP, TimeMS: uint64(bucket) * DefaultBucketMS,
					HTTP: &jhlog.HTTPEvent{RouteRef: jhlog.LocalSymbol(1)}}, map[uint64]string{1: route}, symbols)
				if bucket >= scale.bucketCount || !strings.Contains(strings.ToLower(route), filter) {
					continue
				}
				if reference[route] == nil {
					reference[route] = make([]float64, scale.bucketCount)
				}
				reference[route][bucket]++
			}
			keys := make([]string, 0, len(reference))
			totals := map[string]float64{}
			for route, values := range reference {
				keys = append(keys, route)
				for _, value := range values {
					totals[route] += value
				}
			}
			sort.Slice(keys, func(i, j int) bool {
				if totals[keys[i]] != totals[keys[j]] {
					return totals[keys[i]] > totals[keys[j]]
				}
				return keys[i] < keys[j]
			})
			if len(keys) > 3 {
				keys = keys[:3]
			}
			actual := collector.definitions(3)
			if len(actual) != len(keys) {
				t.Fatalf("seed%d: selected%d, want%d", seed, len(actual), len(keys))
			}
			for i, key := range keys {
				if actual[i].name != "Маршрут "+key+" запросы" || !reflect.DeepEqual(actual[i].points, reference[key]) {
					t.Fatalf("seed%d filter%q: selected route%q differs from dense reference", seed, filter, key)
				}
			}
		}
	}
}

func TestNetworkCandidatesMatchDenseReferenceIncludingRepeatedBuckets(t *testing.T) {
	scale := timelineScale{bucketMS: DefaultBucketMS, bucketCount: 64, hasData: true}
	collector := newNetworkLoopCollector(analyze.Options{}, scale)
	var expected []NetworkLoopFinding
	for key := 0; key < 12; key++ {
		name := fmt.Sprintf("route-%02d", key)
		dense := &networkLoopSignal{name: name, kind: "route", points: make([]float64, 64), tokens: map[int]map[string]int{}}
		for bucket := 60; bucket >= 0; bucket -= 4 {
			for repeat := 0; repeat < 3; repeat++ {
				collector.addPoint(name, name, "route", bucket, 1, "request", "route:"+name, "owner:owner")
				dense.points[bucket]++
				if dense.tokens[bucket] == nil {
					dense.tokens[bucket] = map[string]int{}
				}
				dense.tokens[bucket]["request"]++
				dense.tokens[bucket]["route:"+name]++
				dense.tokens[bucket]["owner:owner"]++
			}
		}
		if finding, ok := analyzeNetworkLoopSignal(dense, DefaultBucketMS); ok {
			expected = append(expected, finding)
		}
	}
	expected = selectNetworkLoops(expected)
	actual := selectNetworkLoops(collector.findings())
	if len(expected) != maxNetworkLoopFindings || !reflect.DeepEqual(actual, expected) {
		t.Fatalf("sparse candidate analysis differs from dense reference: got%+v want%+v", actual, expected)
	}
}
